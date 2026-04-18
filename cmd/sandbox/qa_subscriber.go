package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// qaUnitConsumerName is the durable JetStream consumer name for the
	// sandbox's QARequestedEvent subscription. Durable so pending events
	// redeliver if the sandbox restarts mid-run.
	qaUnitConsumerName = "sandbox-qa-unit"

	// qaStreamName is the JetStream stream that carries workflow domain events.
	qaStreamName = "WORKFLOW"

	// qaAckWait is how long JetStream waits for an ack before redelivering.
	// Set generously to accommodate long test suites. MaxDeliver=3 gives up
	// after ~30 minutes of an unresponsive sandbox.
	qaAckWait = 10 * time.Minute

	// qaLogExcerptBytes is the maximum number of bytes kept in QAFailure.LogExcerpt.
	qaLogExcerptBytes = 2 * 1024
)

// startQASubscriber sets up the durable JetStream consumer for QARequestedEvent
// and begins dispatching unit-mode requests to the server's exec infrastructure.
// The consumer context must be cancelled on shutdown to stop delivery.
func startQASubscriber(ctx context.Context, srv *Server, natsClient *natsclient.Client, logger *slog.Logger) error {
	// Wait for the WORKFLOW stream to exist. plan-manager creates it at its
	// own startup; we race that creation when the whole stack comes up cold.
	js, err := natsClient.JetStream()
	if err != nil {
		return fmt.Errorf("acquire JetStream context: %w", err)
	}
	if _, err := workflow.WaitForStream(ctx, js, qaStreamName); err != nil {
		return fmt.Errorf("wait for stream %s: %w", qaStreamName, err)
	}

	cfg := natsclient.StreamConsumerConfig{
		StreamName:    qaStreamName,
		ConsumerName:  qaUnitConsumerName,
		FilterSubject: workflow.QARequested.Pattern,
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       qaAckWait,
		// MessageTimeout is the per-message client-side ctx deadline;
		// default is 30s which would cancel long test runs. Match AckWait.
		MessageTimeout: qaAckWait,
	}

	handler := &qaHandler{srv: srv, natsClient: natsClient, logger: logger}

	if err := natsClient.ConsumeStreamWithConfig(ctx, cfg, handler.handleMessage); err != nil {
		return fmt.Errorf("consume qa-requested events: %w", err)
	}

	logger.Info("QA subscriber started",
		"stream", qaStreamName,
		"consumer", qaUnitConsumerName,
		"subject", workflow.QARequested.Pattern)

	return nil
}

// qaHandler handles QARequestedEvent messages from JetStream.
type qaHandler struct {
	srv        *Server
	natsClient *natsclient.Client
	logger     *slog.Logger
}

// handleMessage is the push-based callback registered with ConsumeStreamWithConfig.
// It is called once per message delivery, including redeliveries.
func (h *qaHandler) handleMessage(ctx context.Context, msg jetstream.Msg) {
	// Two-step envelope unmarshal: BaseMessage wrapper → QARequestedEvent payload.
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(msg.Data(), &envelope); err != nil {
		h.logger.Error("Failed to parse QARequested BaseMessage envelope", "error", err)
		_ = msg.Term() // malformed — no point redelivering
		return
	}

	var evt workflow.QARequestedEvent
	if err := json.Unmarshal(envelope.Payload, &evt); err != nil {
		h.logger.Error("Failed to parse QARequestedEvent payload", "error", err)
		_ = msg.Term()
		return
	}

	// Only handle unit mode — qa-runner handles integration and full.
	if evt.Mode != workflow.QALevelUnit {
		h.logger.Debug("Skipping non-unit QARequestedEvent",
			"slug", evt.Slug, "mode", evt.Mode)
		_ = msg.Ack()
		return
	}

	if evt.Slug == "" || evt.PlanID == "" {
		h.logger.Error("QARequestedEvent missing required fields",
			"slug", evt.Slug, "plan_id", evt.PlanID)
		_ = msg.Term()
		return
	}

	runID := uuid.New().String()
	start := time.Now()

	h.logger.Info("Running unit QA",
		"slug", evt.Slug, "plan_id", evt.PlanID,
		"run_id", runID, "test_command", evt.TestCommand,
		"trace_id", evt.TraceID)

	completed := h.runUnitQA(ctx, evt, runID, start)
	if completed == nil {
		// runUnitQA already nak'd — nothing further to do.
		return
	}

	// Publish QACompletedEvent BEFORE acking so that if publish fails we nak
	// and JetStream redelivers, letting us retry the publish.
	if err := h.publishCompleted(ctx, completed); err != nil {
		h.logger.Error("Failed to publish QACompletedEvent — will redeliver",
			"slug", evt.Slug, "run_id", runID, "error", err)
		_ = msg.Nak()
		return
	}

	_ = msg.Ack()
	h.logger.Info("Unit QA complete",
		"slug", evt.Slug, "run_id", runID,
		"passed", completed.Passed, "duration_ms", completed.DurationMs)
}

// runUnitQA executes the test command and assembles the QACompletedEvent.
func (h *qaHandler) runUnitQA(
	ctx context.Context,
	evt workflow.QARequestedEvent,
	runID string,
	start time.Time,
) *workflow.QACompletedEvent {
	if evt.TestCommand == "" {
		// plan-manager's EffectiveTestCommand() should have resolved this before
		// publishing; an empty command means project config is incomplete.
		h.logger.Error("QARequestedEvent has no test_command — cannot run unit QA",
			"slug", evt.Slug)
		return &workflow.QACompletedEvent{
			Slug:        evt.Slug,
			PlanID:      evt.PlanID,
			RunID:       runID,
			Level:       workflow.QALevelUnit,
			Passed:      false,
			DurationMs:  time.Since(start).Milliseconds(),
			RunnerError: "no test_command configured at qa.level=unit",
			TraceID:     evt.TraceID,
		}
	}

	// Determine execution timeout.
	timeout := h.srv.maxTimeout
	if evt.TimeoutSeconds > 0 {
		d := time.Duration(evt.TimeoutSeconds) * time.Second
		if d < timeout {
			timeout = d
		}
	}

	// Run the test command from the workspace root (repoPath).
	stdout, stderr, exitCode, timedOut := execCommand(
		ctx, h.srv.repoPath, evt.TestCommand, timeout, h.srv.maxOutputBytes)

	combined := stdout
	if stderr != "" {
		if combined != "" {
			combined += "\n"
		}
		combined += stderr
	}

	durationMs := time.Since(start).Milliseconds()

	// Archive the full output.
	artifactRelPath := filepath.Join(
		".semspec", "qa-artifacts", evt.Slug, runID, "unit-test.log")
	artifacts := h.archiveLog(evt.Slug, runID, artifactRelPath, combined)

	// Build pass/fail result.
	passed := exitCode == 0 && !timedOut
	var failures []workflow.QAFailure
	if !passed {
		excerpt := combined
		if len(excerpt) > qaLogExcerptBytes {
			excerpt = excerpt[len(excerpt)-qaLogExcerptBytes:]
		}
		msg := fmt.Sprintf("test command failed (exit %d)", exitCode)
		if timedOut {
			msg = fmt.Sprintf("test command timed out after %s", timeout)
		}
		failures = []workflow.QAFailure{
			{
				JobName:    "unit",
				Message:    msg,
				LogExcerpt: excerpt,
			},
		}
	}

	return &workflow.QACompletedEvent{
		Slug:       evt.Slug,
		PlanID:     evt.PlanID,
		RunID:      runID,
		Level:      workflow.QALevelUnit,
		Passed:     passed,
		Failures:   failures,
		Artifacts:  artifacts,
		DurationMs: durationMs,
		TraceID:    evt.TraceID,
	}
}

// archiveLog writes the combined test output to the artifact path under repoPath
// and returns a populated QAArtifactRef slice on success. On write failure the
// error is logged and an empty slice is returned — the caller still publishes
// QACompletedEvent without the artifact reference.
func (h *qaHandler) archiveLog(slug, runID, relPath, content string) []workflow.QAArtifactRef {
	absPath := filepath.Join(h.srv.repoPath, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		h.logger.Warn("Failed to create qa-artifact directory — artifact will not be archived",
			"slug", slug, "run_id", runID, "path", absPath, "error", err)
		return nil
	}
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		h.logger.Warn("Failed to write qa-artifact log — artifact will not be archived",
			"slug", slug, "run_id", runID, "path", absPath, "error", err)
		return nil
	}
	return []workflow.QAArtifactRef{
		{
			Path:    relPath, // workspace-relative — consumers resolve against their own root
			Type:    "log",
			Purpose: "unit test output",
		},
	}
}

// publishCompleted wraps evt in a BaseMessage envelope and publishes it to
// the WORKFLOW stream on workflow.events.qa.completed.
func (h *qaHandler) publishCompleted(ctx context.Context, evt *workflow.QACompletedEvent) error {
	p := &payloads.QACompletedPayload{QACompletedEvent: *evt}
	baseMsg := message.NewBaseMessage(p.Schema(), p, "sandbox")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal QACompletedEvent: %w", err)
	}
	if err := h.natsClient.PublishToStream(ctx, workflow.QACompleted.Pattern, data); err != nil {
		return fmt.Errorf("publish to %s: %w", workflow.QACompleted.Pattern, err)
	}
	return nil
}
