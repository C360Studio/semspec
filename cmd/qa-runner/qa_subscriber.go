package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// qaRunnerConsumerName is the durable JetStream consumer name for the
	// qa-runner's QARequestedEvent subscription. Durable so pending events
	// redeliver if qa-runner restarts mid-run.
	qaRunnerConsumerName = "qa-runner-integration-full"

	// qaStreamName is the JetStream stream that carries workflow domain events.
	qaStreamName = "WORKFLOW"

	// qaAckWait is how long JetStream waits for an ack before redelivering.
	// Set generously to accommodate long act-based test suites. MaxDeliver=3
	// gives up after ~30 minutes if qa-runner becomes unresponsive.
	qaAckWait = 10 * time.Minute
)

// startQASubscriber sets up the durable JetStream consumer for QARequestedEvent
// and begins dispatching integration/full-mode requests. The consumer context
// must be cancelled on shutdown to stop delivery.
func startQASubscriber(
	ctx context.Context,
	natsClient *natsclient.Client,
	projectHostPath string,
	defaultTimeout time.Duration,
	logger *slog.Logger,
) error {
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
		ConsumerName:  qaRunnerConsumerName,
		FilterSubject: workflow.QARequested.Pattern,
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       qaAckWait,
		// MessageTimeout is the per-message client-side ctx deadline.
		// Must exceed act's run time or the handler's ctx cancels mid-run and
		// act is killed before tests finish. Matches AckWait so semstreams
		// and JetStream agree on how long a single QA run may take.
		MessageTimeout: qaAckWait,
	}

	handler := &qaHandler{
		natsClient:      natsClient,
		projectHostPath: projectHostPath,
		defaultTimeout:  defaultTimeout,
		logger:          logger,
	}

	if err := natsClient.ConsumeStreamWithConfig(ctx, cfg, handler.handleMessage); err != nil {
		return fmt.Errorf("consume qa-requested events: %w", err)
	}

	logger.Info("QA subscriber started",
		"stream", qaStreamName,
		"consumer", qaRunnerConsumerName,
		"subject", workflow.QARequested.Pattern)

	return nil
}

// qaHandler handles QARequestedEvent messages from JetStream.
type qaHandler struct {
	natsClient      *natsclient.Client
	projectHostPath string
	defaultTimeout  time.Duration
	logger          *slog.Logger
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

	// Only handle integration and full modes — sandbox handles unit.
	if evt.Mode != workflow.QALevelIntegration && evt.Mode != workflow.QALevelFull {
		h.logger.Debug("Skipping non-integration/full QARequestedEvent",
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

	h.logger.Info("Received QA request",
		"slug", evt.Slug,
		"plan_id", evt.PlanID,
		"mode", evt.Mode,
		"workspace_host_path", evt.WorkspaceHostPath,
		"workflow_path", evt.WorkflowPath,
		"trace_id", evt.TraceID)

	completed := h.handleQARequested(ctx, evt)

	// Publish QACompletedEvent BEFORE acking so that if publish fails we nak
	// and JetStream redelivers, letting us retry the publish.
	if err := h.publishCompleted(ctx, completed); err != nil {
		h.logger.Error("Failed to publish QACompletedEvent — will redeliver",
			"slug", evt.Slug, "run_id", completed.RunID, "error", err)
		_ = msg.Nak()
		return
	}

	_ = msg.Ack()
	h.logger.Info("QA complete",
		"slug", evt.Slug,
		"run_id", completed.RunID,
		"mode", evt.Mode,
		"passed", completed.Passed)
}

// handleQARequested invokes act against the project's qa workflow and returns
// a populated QACompletedEvent. Delegates to runQA in runner.go.
func (h *qaHandler) handleQARequested(ctx context.Context, evt workflow.QARequestedEvent) *workflow.QACompletedEvent {
	return h.runQA(ctx, evt)
}

// publishCompleted wraps evt in a BaseMessage envelope and publishes it to
// the WORKFLOW stream on workflow.events.qa.completed.
func (h *qaHandler) publishCompleted(ctx context.Context, evt *workflow.QACompletedEvent) error {
	p := &payloads.QACompletedPayload{QACompletedEvent: *evt}
	baseMsg := message.NewBaseMessage(p.Schema(), p, "qa-runner")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal QACompletedEvent: %w", err)
	}
	if err := h.natsClient.PublishToStream(ctx, workflow.QACompleted.Pattern, data); err != nil {
		return fmt.Errorf("publish to %s: %w", workflow.QACompleted.Pattern, err)
	}
	return nil
}
