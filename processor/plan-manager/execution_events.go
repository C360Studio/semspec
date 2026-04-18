package planmanager

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/retry"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

// watchExecutionCompletions watches the EXECUTION_STATES KV bucket for
// requirement execution entries reaching terminal state. When all requirements
// for a plan reach terminal state, the plan transitions:
//   - all completed → target chosen by plan.QALevel (reviewing_qa / ready_for_qa / complete)
//   - any failed    → stays in StatusImplementing (user decides: retry/complete/reject)
//
// On startup, the KV watcher replays all historical entries before a nil
// sentinel. Plan-level transitions are deferred until after replay completes
// to prevent stale pre-crash terminal entries from incorrectly killing plans.
func (c *Component) watchExecutionCompletions(ctx context.Context) {
	// Retry bucket acquisition — execution-manager may create the bucket
	// after plan-manager starts. Without retry, the watcher is permanently
	// disabled and plans never transition from implementing → complete.
	bucket, err := retry.DoWithResult(ctx, retry.Quick(), func() (jetstream.KeyValue, error) {
		return c.getExecBucket(ctx)
	})
	if err != nil {
		c.logger.Warn("EXECUTION_STATES bucket not available after retries — plan completion watcher disabled",
			"error", err)
		return
	}

	watcher, err := bucket.Watch(ctx, "req.>")
	if err != nil {
		c.logger.Warn("Failed to watch EXECUTION_STATES req entries — plan completion watcher disabled",
			"error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Plan completion watcher started (watching EXECUTION_STATES req.>)")

	replayDone := false
	for entry := range watcher.Updates() {
		if entry == nil {
			// End of initial KV replay. All historical entries have been
			// delivered; subsequent entries are live updates.
			replayDone = true
			c.logger.Info("EXECUTION_STATES replay complete, checking convergence")
			c.checkPostReplayConvergence(ctx, bucket)
			continue
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}

		c.handleRequirementStateChange(ctx, bucket, entry, replayDone)
	}
}

// handleRequirementStateChange processes a single EXECUTION_STATES KV update
// for a requirement entry (key: req.<slug>.<reqID>).
// During initial KV replay (replayDone=false), terminal entries are logged but
// do not trigger plan-level state transitions — those are deferred to
// checkPostReplayConvergence after the replay completes.
func (c *Component) handleRequirementStateChange(ctx context.Context, bucket jetstream.KeyValue, entry jetstream.KeyValueEntry, replayDone bool) {
	var reqExec workflow.RequirementExecution
	if err := json.Unmarshal(entry.Value(), &reqExec); err != nil {
		c.logger.Debug("Failed to unmarshal requirement execution", "key", entry.Key(), "error", err)
		return
	}

	// Only act on terminal stages.
	if !isTerminalStage(reqExec.Stage) {
		return
	}

	slug := reqExec.Slug
	if slug == "" {
		// Parse slug from key: req.<slug>.<reqID>
		slug = slugFromKey(entry.Key())
	}
	if slug == "" {
		return
	}

	// During initial KV replay, log terminal entries but skip plan-level
	// transitions. checkPostReplayConvergence handles catch-up after replay.
	if !replayDone {
		c.logger.Debug("Replay: skipping plan transition for terminal requirement",
			"slug", slug, "key", entry.Key(), "stage", reqExec.Stage)
		return
	}

	c.checkPlanConvergence(ctx, bucket, slug)
}

// checkPlanConvergence evaluates whether a plan's requirements have all reached
// terminal state and takes the appropriate action. Called both from live KV
// updates and from post-replay convergence checks.
//
// Three outcomes:
//   - Not all terminal: log progress, return (no transition)
//   - All terminal, none failed: transition to reviewing_rollup
//   - All terminal, some failed: stay in implementing, log stall for user action
func (c *Component) checkPlanConvergence(ctx context.Context, bucket jetstream.KeyValue, slug string) {
	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()
	if ps == nil {
		return
	}

	plan, ok := ps.get(slug)
	if !ok {
		return
	}

	// Only transition from implementing.
	if plan.EffectiveStatus() != workflow.StatusImplementing {
		return
	}

	// Count terminal requirements by scanning the bucket.
	totalRequired := len(plan.Requirements)
	if totalRequired == 0 {
		return
	}

	completedCount, failedCount, err := c.countTerminalRequirements(ctx, bucket, slug)
	if err != nil {
		c.logger.Warn("Failed to count terminal requirements", "slug", slug, "error", err)
		return
	}

	terminalCount := completedCount + failedCount
	if terminalCount < totalRequired {
		c.logger.Debug("Requirements still in progress",
			"slug", slug,
			"completed", completedCount,
			"failed", failedCount,
			"total", totalRequired)
		return
	}

	// All requirements are terminal.
	if failedCount == 0 {
		// Branch on the plan's QA level (snapshotted at plan creation).
		level := plan.EffectiveQALevel()
		target := c.targetForQALevel(level, plan, slug)

		c.logger.Info("All requirements completed — transitioning to review",
			"slug", slug,
			"completed", completedCount,
			"qa_level", level,
			"target", target)

		if err := c.setPlanStatusCached(ctx, plan, target); err != nil {
			// Safety fallback: route to awaiting_review or complete based on config.
			c.logger.Warn("Review transition failed, routing based on review gate config",
				"slug", slug, "error", err)
			fallback := workflow.StatusComplete
			if c.shouldGateReview(plan) {
				fallback = workflow.StatusAwaitingReview
			}
			if err := c.setPlanStatusCached(ctx, plan, fallback); err != nil {
				c.logger.Error("Failed to transition plan", "slug", slug, "target", fallback, "error", err)
			}
			return
		}
		// If we routed to ready_for_qa, fire the executor dispatch.
		c.publishQARequestIfNeeded(ctx, plan)
		return
	}

	// Some requirements failed — don't auto-reject.
	// Stay in implementing and let the user decide: retry, complete partial, or reject.
	c.logger.Info("Requirements finished with failures — awaiting user decision",
		"slug", slug,
		"completed", completedCount,
		"failed", failedCount,
		"total", totalRequired)

	// Bump the plan KV revision so SSE watchers receive an updated event that
	// includes the populated ExecutionSummary (stall signal for the frontend).
	if err := c.savePlanCached(ctx, plan); err != nil {
		c.logger.Warn("Failed to save plan for SSE stall notification", "slug", slug, "error", err)
	}
}

// publishQARequestIfNeeded fires when a plan is routed to ready_for_qa. It
// publishes a QARequestedEvent with the appropriate Mode so the matching
// executor (sandbox for unit, qa-runner for integration/full) picks it up.
// No-op when plan.Status != ready_for_qa or natsClient is nil (tests).
func (c *Component) publishQARequestIfNeeded(ctx context.Context, plan *workflow.Plan) {
	if plan == nil || plan.Status != workflow.StatusReadyForQA || c.natsClient == nil {
		return
	}

	level := plan.EffectiveQALevel()
	if !level.UsesQARunner() && !level.UsesSandboxTests() {
		return
	}

	// qa-runner bind-mounts the workspace via the Docker socket — it needs
	// the HOST path, not semspec's container path. Without this, act will
	// try to bind a container-local path on the host and silently produce
	// an empty workspace.
	workspaceHost := c.resolveProjectHostPath()
	if workspaceHost == "" {
		c.logger.Error("Refusing to publish QARequestedEvent — PROJECT_HOST_PATH unset",
			"slug", plan.Slug, "level", level,
			"hint", "set PROJECT_HOST_PATH on the semspec service so qa-runner can resolve the host workspace")
		return
	}

	// Load project config for test command (language-aware default).
	pc := workflow.LoadProjectConfigFromDisk(c.resolveRepoRoot())

	req := &payloads.QARequestedPayload{
		QARequestedEvent: workflow.QARequestedEvent{
			Slug:              plan.Slug,
			PlanID:            plan.ID,
			Mode:              level,
			WorkspaceHostPath: workspaceHost,
			WorkflowPath:      ".github/workflows/qa.yml",
			TestCommand:       pc.EffectiveTestCommand(),
			TraceID:           uuid.New().String(),
		},
	}

	// Validate before publishing — catches slug path-traversal, empty
	// workspace, bad Mode, etc. before the wire.
	if err := req.Validate(); err != nil {
		c.logger.Error("Refusing to publish invalid QARequestedEvent",
			"slug", plan.Slug, "error", err)
		return
	}

	baseMsg := message.NewBaseMessage(req.Schema(), req, "plan-manager")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Error("Failed to marshal QARequestedEvent", "slug", plan.Slug, "error", err)
		return
	}

	if err := c.natsClient.PublishToStream(ctx, "workflow.events.qa.requested", data); err != nil {
		c.logger.Error("Failed to publish QARequestedEvent", "slug", plan.Slug, "error", err)
		return
	}

	c.logger.Info("Published QARequestedEvent",
		"slug", plan.Slug, "mode", level, "test_command", req.TestCommand)
}

// targetForQALevel chooses the post-implementing status based on the plan's
// QA level. Called at implementing convergence and force-complete.
//
// level=none       → StatusComplete (or StatusAwaitingReview when gated)
// level=synthesis  → StatusReadyForQA (qa-reviewer claims it, no tests run)
// level=unit        → StatusReadyForQA (sandbox runs project tests first)
// level=integration → StatusReadyForQA (qa-runner via act)
// level=full        → StatusReadyForQA (qa-runner + e2e — TODO Phase 7)
//
// qa-reviewer owns the ready_for_qa → reviewing_qa transition via
// plan.mutation.qa.start, mirroring plan-reviewer's mutation-driven shape.
// For non-synthesis levels, publishQARequestIfNeeded additionally fires a
// QARequestedEvent so the executor runs before qa-reviewer claims the plan.
func (c *Component) targetForQALevel(level workflow.QALevel, plan *workflow.Plan, _ string) workflow.Status {
	switch level {
	case workflow.QALevelNone:
		if c.shouldGateReview(plan) {
			return workflow.StatusAwaitingReview
		}
		return workflow.StatusComplete
	default:
		return workflow.StatusReadyForQA
	}
}

// checkPostReplayConvergence runs after the initial EXECUTION_STATES KV replay
// completes. It checks all plans in StatusImplementing for convergence,
// catching the case where a plan legitimately completed before a crash but
// the status transition was never persisted.
func (c *Component) checkPostReplayConvergence(ctx context.Context, bucket jetstream.KeyValue) {
	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()
	if ps == nil {
		return
	}

	for _, plan := range ps.list() {
		if plan.EffectiveStatus() != workflow.StatusImplementing {
			continue
		}
		c.checkPlanConvergence(ctx, bucket, plan.Slug)
	}
}

// countTerminalRequirements scans EXECUTION_STATES for all req.<slug>.* keys
// and counts entries in terminal stages.
func (c *Component) countTerminalRequirements(ctx context.Context, bucket jetstream.KeyValue, slug string) (completed, failed int, err error) {
	prefix := "req." + slug + "."
	keys, err := bucket.Keys(ctx, jetstream.MetaOnly())
	if err != nil {
		if errors.Is(err, jetstream.ErrNoKeysFound) {
			return 0, 0, nil
		}
		return 0, 0, err
	}

	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		entry, err := bucket.Get(ctx, key)
		if err != nil {
			continue
		}
		var reqExec workflow.RequirementExecution
		if err := json.Unmarshal(entry.Value(), &reqExec); err != nil {
			continue
		}
		switch reqExec.Stage {
		case "completed":
			completed++
		case "failed", "error":
			failed++
		}
	}
	return completed, failed, nil
}

// isTerminalStage returns true for requirement execution stages that indicate
// the requirement will not progress further.
func isTerminalStage(stage string) bool {
	return stage == "completed" || stage == "failed" || stage == "error"
}

// slugFromKey extracts the plan slug from a KV key formatted as req.<slug>.<reqID>.
func slugFromKey(key string) string {
	parts := strings.SplitN(key, ".", 3)
	if len(parts) < 3 {
		return ""
	}
	return parts[1]
}
