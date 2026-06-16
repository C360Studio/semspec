package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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
// and begins dispatching sandbox-executable QA requests to the server's exec
// infrastructure.
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

	// Only sandbox-executable modes run here. Full/e2e orchestration remains in
	// operator CI, so stale/invalid modes are acknowledged rather than
	// redelivered forever.
	if !evt.Mode.UsesSandboxTests() {
		h.logger.Debug("Skipping non-sandbox QARequestedEvent",
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

	h.logger.Info("Running sandbox QA",
		"slug", evt.Slug, "plan_id", evt.PlanID,
		"mode", evt.Mode,
		"run_id", runID, "test_command", evt.TestCommand,
		"trace_id", evt.TraceID)

	completed := h.runSandboxQA(ctx, evt, runID, start)
	if completed == nil {
		// runSandboxQA already nak'd — nothing further to do.
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
	h.logger.Info("Sandbox QA complete",
		"slug", evt.Slug, "run_id", runID,
		"mode", evt.Mode,
		"passed", completed.Passed, "duration_ms", completed.DurationMs)
}

// selectQAWorkDir resolves the directory the QA command runs in: the
// dedicated QA worktree when plan-manager created one and it resolves, else the
// repo root. resolve mirrors Server.worktreeFor — it returns "" when the
// worktree is absent. fellBack is true only when a workspace was requested but
// could not be resolved (the caller WARN-logs so the regression to the unmerged
// baseline is visible rather than silent).
func selectQAWorkDir(repoPath, workspace string, resolve func(string) string) (dir string, fellBack bool) {
	if workspace == "" {
		return repoPath, false
	}
	if wt := resolve(workspace); wt != "" {
		return wt, false
	}
	return repoPath, true
}

// runSandboxQA executes the test command and assembles the QACompletedEvent.
func (h *qaHandler) runSandboxQA(
	ctx context.Context,
	evt workflow.QARequestedEvent,
	runID string,
	start time.Time,
) *workflow.QACompletedEvent {
	if evt.TestCommand == "" {
		// plan-manager's EffectiveTestCommand() should have resolved this before
		// publishing; an empty command means project config is incomplete.
		h.logger.Error("QARequestedEvent has no test_command — cannot run sandbox QA",
			"slug", evt.Slug, "mode", evt.Mode)
		return &workflow.QACompletedEvent{
			Slug:        evt.Slug,
			PlanID:      evt.PlanID,
			RunID:       runID,
			Level:       evt.Mode,
			Passed:      false,
			DurationMs:  time.Since(start).Milliseconds(),
			RunnerError: fmt.Sprintf("no test_command configured at qa.level=%s", evt.Mode),
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

	// Run the test command in the QA worktree (a checkout of the assembled plan
	// branch, holding the merged per-requirement implementation) when plan-manager
	// provided one; otherwise fall back to the repo root. Without this, unit QA
	// runs against the pre-implementation main HEAD and is meaningless.
	workDir, fellBack := selectQAWorkDir(h.srv.repoPath, evt.Workspace, h.srv.worktreeFor)
	if fellBack {
		h.logger.Warn("QA worktree not found — running QA command against repo root (may be the unmerged baseline)",
			"slug", evt.Slug, "workspace", evt.Workspace)
	}
	qaEnv, cleanup, isolationSummary, err := prepareQAIsolation(evt.Slug, runID)
	if err != nil {
		h.logger.Error("Failed to prepare isolated QA cache", "slug", evt.Slug, "run_id", runID, "error", err)
		return &workflow.QACompletedEvent{
			Slug:        evt.Slug,
			PlanID:      evt.PlanID,
			RunID:       runID,
			Level:       evt.Mode,
			Passed:      false,
			DurationMs:  time.Since(start).Milliseconds(),
			RunnerError: fmt.Sprintf("prepare isolated QA cache: %v", err),
			TraceID:     evt.TraceID,
		}
	}
	defer cleanup()

	stdout, stderr, exitCode, timedOut := execCommandWithEnv(
		ctx, workDir, evt.TestCommand, timeout, h.srv.maxOutputBytes, qaEnv)

	combined := isolationSummary
	if stdout != "" {
		if combined != "" {
			combined += "\n"
		}
		combined += stdout
	}
	if stderr != "" {
		if combined != "" {
			combined += "\n"
		}
		combined += stderr
	}

	durationMs := time.Since(start).Milliseconds()

	// Archive the full output.
	artifactRelPath := filepath.Join(
		".semspec", "qa-artifacts", evt.Slug, runID, fmt.Sprintf("%s-test.log", evt.Mode))
	artifacts := h.archiveLog(evt.Slug, runID, artifactRelPath, combined, fmt.Sprintf("%s test output", evt.Mode))

	skippedEvidence := integrationSkippedTestEvidence(evt.Mode, workDir)

	// Build pass/fail result.
	passed := exitCode == 0 && !timedOut && len(skippedEvidence) == 0
	var failures []workflow.QAFailure
	if !passed {
		excerpt := combined
		if len(excerpt) > qaLogExcerptBytes {
			excerpt = excerpt[len(excerpt)-qaLogExcerptBytes:]
		}
		msg := fmt.Sprintf("test command failed (exit %d)", exitCode)
		if timedOut {
			msg = fmt.Sprintf("test command timed out after %s", timeout)
		} else if len(skippedEvidence) > 0 {
			msg = fmt.Sprintf("integration QA skipped test(s): %s", strings.Join(skippedEvidence, ", "))
		}
		failures = []workflow.QAFailure{
			{
				JobName:    string(evt.Mode),
				Message:    msg,
				LogExcerpt: excerpt,
			},
		}
	}

	return &workflow.QACompletedEvent{
		Slug:       evt.Slug,
		PlanID:     evt.PlanID,
		RunID:      runID,
		Level:      evt.Mode,
		Passed:     passed,
		Failures:   failures,
		Artifacts:  artifacts,
		DurationMs: durationMs,
		TraceID:    evt.TraceID,
	}
}

func prepareQAIsolation(slug, runID string) (env []string, cleanup func(), summary string, err error) {
	tmpRoot, err := os.MkdirTemp("", "semspec-qa-"+safeQAToken(slug)+"-"+safeQAToken(runID)+"-")
	if err != nil {
		return nil, func() {}, "", err
	}
	cleanup = func() {
		_ = os.RemoveAll(tmpRoot)
	}

	gradleHome := filepath.Join(tmpRoot, "gradle")
	mavenRepo := filepath.Join(tmpRoot, "m2", "repository")
	env = []string{
		"GRADLE_USER_HOME=" + gradleHome,
		"MAVEN_OPTS=-Dmaven.repo.local=" + mavenRepo,
	}
	summary = "QA cache isolation enabled: GRADLE_USER_HOME=" + gradleHome + " MAVEN_OPTS=-Dmaven.repo.local=" + mavenRepo
	return env, cleanup, summary, nil
}

func safeQAToken(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "run"
	}
	return b.String()
}

// archiveLog writes the combined test output to the artifact path under repoPath
// and returns a populated QAArtifactRef slice on success. On write failure the
// error is logged and an empty slice is returned — the caller still publishes
// QACompletedEvent without the artifact reference.
func (h *qaHandler) archiveLog(slug, runID, relPath, content, purpose string) []workflow.QAArtifactRef {
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
			Purpose: purpose,
		},
	}
}

func integrationSkippedTestEvidence(mode workflow.QALevel, workDir string) []string {
	if mode != workflow.QALevelIntegration || workDir == "" {
		return nil
	}
	roots := []string{
		filepath.Join(workDir, "build", "test-results"),
		filepath.Join(workDir, "target", "surefire-reports"),
		filepath.Join(workDir, "target", "failsafe-reports"),
	}
	var evidence []string
	for _, root := range roots {
		if _, err := os.Stat(root); err != nil {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || len(evidence) >= 5 {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if ext != ".xml" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			if strings.Contains(strings.ToLower(string(data)), "<skipped") {
				rel, relErr := filepath.Rel(workDir, path)
				if relErr != nil {
					rel = path
				}
				evidence = append(evidence, rel)
			}
			return nil
		})
	}
	return evidence
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
