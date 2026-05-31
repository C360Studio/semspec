package planmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	projectmanager "github.com/c360studio/semspec/processor/project-manager"
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

	// Auto-archive any open ExecutionExhausted decisions whose subject just
	// resolved non-failing. The decision was raised to get human attention
	// because the agent was stuck; now that the requirement is unstuck
	// (completed via retry), keeping the record open would keep demanding
	// attention for a problem that's been resolved.
	if reqExec.Stage == "completed" && reqExec.RequirementID != "" {
		c.autoArchiveExhaustionDecisions(ctx, slug, reqExec.RequirementID, "requirement completed after exhaustion")
	}

	// Phase 5 infra_health tracking: when a requirement errors with the
	// infrastructure class, the plan has observed evidence that the sandbox
	// (or related infra) is not fully healthy. Advancing to InfraHealthCritical
	// deliberately — plan-manager can distinguish a transient one-off
	// ("degraded") from a persistent wedge ("critical") only with additional
	// signal (e.g. sandbox /admin/reconcile state, see follow-up). For MVP,
	// any infrastructure-class failure promotes to critical so retry endpoints
	// refuse with 409 until an operator has explicitly cleared the plan.
	if reqExec.Stage == "error" && reqExec.ErrorClass == workflow.ErrorClassInfrastructure {
		c.markPlanInfraCritical(ctx, slug, reqExec.RequirementID, reqExec.ErrorReason)
	}

	c.checkPlanConvergence(ctx, bucket, slug)

	// Re-fire scenario-orchestrate when a requirement reaches completed
	// stage so the orchestrator re-evaluates the DAG and dispatches any
	// newly-unblocked downstream requirements. checkPlanConvergence above
	// only handles ALL-terminal convergence; this handles intermediate
	// completions in chain-dep plans (the @hard regression case).
	//
	// Only fires when the plan is still in implementing (post-convergence
	// transitions skip this; we don't want to re-poke a plan that just
	// advanced to reviewing_qa or complete).
	if reqExec.Stage == "completed" {
		c.mu.RLock()
		ps := c.plans
		c.mu.RUnlock()
		if ps != nil {
			if plan, ok := ps.get(slug); ok && plan.EffectiveStatus() == workflow.StatusImplementing {
				if err := c.triggerScenarioOrchestrator(ctx, plan); err != nil {
					c.logger.Warn("Failed to re-fire scenario orchestrator after requirement completion",
						"slug", slug, "requirement_id", reqExec.RequirementID, "error", err)
				}
			}
		}
	}
}

// markPlanInfraCritical flips the plan's InfraHealth to critical after an
// infrastructure-class requirement error. Idempotent — already-critical
// plans are not re-saved. The UI and retry endpoints key off this so
// neither operates against a sandbox known to be wedged.
func (c *Component) markPlanInfraCritical(ctx context.Context, slug, reqID, reason string) {
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
	if plan.InfraHealth == workflow.InfraHealthCritical {
		return
	}
	prior := plan.InfraHealth
	plan.InfraHealth = workflow.InfraHealthCritical
	if err := ps.save(ctx, plan); err != nil {
		c.logger.Warn("Failed to persist InfraHealth=critical",
			"slug", slug, "requirement_id", reqID, "error", err)
		return
	}
	c.logger.Warn("Plan infrastructure health escalated to critical",
		"slug", slug,
		"requirement_id", reqID,
		"prior_infra_health", prior,
		"error_reason", reason,
	)
}

// autoArchiveExhaustionDecisions archives open ExecutionExhausted decisions
// for a requirement that has just resolved — i.e. the subject of the
// decision is no longer stuck, so the attention gate can close.
func (c *Component) autoArchiveExhaustionDecisions(ctx context.Context, slug, reqID, reason string) {
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

	now := time.Now()
	archived := 0
	for i := range plan.PlanDecisions {
		d := &plan.PlanDecisions[i]
		if d.Kind != workflow.PlanDecisionKindExecutionExhausted {
			continue
		}
		if d.Status != workflow.PlanDecisionStatusProposed && d.Status != workflow.PlanDecisionStatusUnderReview {
			continue
		}
		// Match by affected requirement id.
		hit := false
		for _, id := range d.AffectedReqIDs {
			if id == reqID {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		d.Status = workflow.PlanDecisionStatusArchived
		d.DecidedAt = &now
		if d.RejectionReasons == nil {
			d.RejectionReasons = map[string]string{}
		}
		d.RejectionReasons["auto_archive"] = reason
		archived++
	}

	if archived == 0 {
		return
	}

	if err := ps.save(ctx, plan); err != nil {
		c.logger.Warn("Failed to save plan after auto-archiving exhaustion decisions",
			"slug", slug, "requirement_id", reqID, "error", err)
		return
	}

	c.logger.Info("Auto-archived open exhaustion decisions",
		"slug", slug,
		"requirement_id", reqID,
		"archived", archived,
		"reason", reason,
	)
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

	completedCount, failedCount, failedIDs, err := c.countTerminalRequirements(ctx, bucket, slug)
	if err != nil {
		c.logger.Warn("Failed to count terminal requirements", "slug", slug, "error", err)
		return
	}
	failedCount = c.augmentFailedWithBlocked(plan, failedIDs, failedCount, completedCount, totalRequired)

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

	// Some requirements failed.
	//
	// Default (production / human-in-the-loop): stay in implementing, log the
	// stall, and wait for an operator to decide retry / complete-partial /
	// reject. The savePlanCached bump nudges the SSE watcher so the UI can
	// render the stall signal from ExecutionSummary.
	//
	// Autonomous mode (config.AutoRejectOnExhaustion=true OR per-plan
	// AutoRejectOnExhaustion=true): no human to decide, so auto-transition
	// to rejected. Set in E2E configs so Playwright fails fast on real
	// escalation rather than blocking the test budget on the same wedge
	// for 30+ minutes (caught take 23). Per-plan override lets specific
	// plans opt back into the production stall path even in fail-fast
	// fleets — used by iteration-exhaustion test scenarios that need to
	// verify the stall-and-retry recovery flow.
	if resolveAutoRejectOnExhaustion(plan, c.config) {
		summary := fmt.Sprintf("autonomous mode: %d/%d requirements failed; auto-rejecting (production mode would await human decision)",
			failedCount, totalRequired)
		c.logger.Warn("Auto-rejecting plan on requirement-failure convergence (AutoRejectOnExhaustion)",
			"slug", slug,
			"completed", completedCount,
			"failed", failedCount,
			"total", totalRequired)
		plan.LastError = summary
		now := time.Now()
		plan.LastErrorAt = &now
		if err := c.setPlanStatusCached(ctx, plan, workflow.StatusRejected); err != nil {
			c.logger.Error("Failed to auto-reject plan on exhaustion",
				"slug", slug, "error", err)
		}
		return
	}

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

	// Render the qa.yml. Two paths:
	//
	//   1. ADR-039 Phase 1c — when the plan's architecture selected
	//      services-orchestrated harness profiles and the operator did not
	//      opt out via qa_skip_service_injection, overwrite the workspace
	//      qa.yml with a catalog-injected workflow. The injection bundles the
	//      catalog-derived `services:` block with the act DooD-required
	//      `container:` block on the integration job
	//      ([[act-dood-services-require-container-block]]).
	//
	//   2. Fallback — call EnsureQAWorkflow, which writes the language-aware
	//      scaffold only when no qa.yml exists. Preserves operator-owned
	//      workflows and matches pre-Phase-1c behaviour.
	//
	// Both paths are non-fatal: a render or write failure logs a warning and
	// lets qa-runner surface the clearer act-side error.
	if !c.maybeRenderQAWithServices(plan, pc) {
		if err := projectmanager.EnsureQAWorkflow(c.resolveRepoRoot(), pc, c.logger); err != nil {
			c.logger.Warn("Failed to scaffold qa.yml before QA dispatch — qa-runner may fail with missing workflow",
				"slug", plan.Slug, "error", err)
		}
	}

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
// level=integration → StatusReadyForQA (qa-runner via act in a clean-room
//
//	runner against the rendered .github/workflows/qa.yml.
//	Per ADR-039, services-class harness profiles render
//	as qa.yml services: blocks from the catalog and
//	qa-runner brings them up. For testcontainers-class
//	profiles dev's TDD already exercised them via the
//	docker socket mounted on the sandbox, so qa-runner
//	doubles as a reproducibility gate catching "passes
//	with dev's working state, fails fresh checkout" cases.)
//
// level=full        → StatusReadyForQA (qa-runner runs the integration
//
//	job plus the e2e job from .github/workflows/qa.yml,
//	adding Playwright/browser flows on top of
//	integration tests)
//
// Two integration models coexist on the same qa.yml:
//   - services-class profiles (e.g. PX4 SITL via
//     mavlink.px4-sitl.mavsdk-smoke) introduce real services FIRST at
//     qa-runner — dev never saw them. ADR-039 wires the rendering.
//   - testcontainers-class profiles run in dev's TDD via the docker
//     socket AND again at qa-runner as a reproducibility check.
//
// Orchestration type lives on the catalog Profile (ADR-039 Phase 1a).
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
// and counts entries in terminal stages. Also returns the set of failed
// requirement IDs (failed OR error) so callers can compute the transitive
// blocked-by-failure set — when a requirement fails, every requirement that
// depends_on it (transitively) can never reach a successful terminal state
// because the orchestrator won't dispatch a req whose deps haven't completed.
// Without that signal, checkPlanConvergence sees terminalCount < totalRequired
// for a plan that can never make further progress and waits forever.
// Caught take 24 (2026-05-08): req 1 failed, req 2 (depends_on req 1) never
// dispatched, plan stuck in implementing because terminalCount=1 < total=2,
// AutoRejectOnExhaustion never fired.
func (c *Component) countTerminalRequirements(ctx context.Context, bucket jetstream.KeyValue, slug string) (completed, failed int, failedIDs map[string]bool, err error) {
	prefix := "req." + slug + "."
	failedIDs = make(map[string]bool)
	keys, err := bucket.Keys(ctx, jetstream.MetaOnly())
	if err != nil {
		if errors.Is(err, jetstream.ErrNoKeysFound) {
			return 0, 0, failedIDs, nil
		}
		return 0, 0, failedIDs, err
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
			if reqExec.RequirementID != "" {
				failedIDs[reqExec.RequirementID] = true
			}
		}
	}
	return completed, failed, failedIDs, nil
}

// augmentFailedWithBlocked rolls cascaded blocked-by-failure reqs into the
// failed count for convergence purposes. Logs at info when the cascade adds
// reqs (these are reqs the orchestrator can never dispatch, so plan-manager
// must stop waiting for them or the plan hangs in implementing forever).
// Returns the new failedCount; the caller passes other counts only for
// log context. Extracted from checkPlanConvergence to keep that function
// under the function-length lint limit.
func (c *Component) augmentFailedWithBlocked(plan *workflow.Plan, failedIDs map[string]bool, failedCount, completedCount, totalRequired int) int {
	blocked := countBlockedByFailure(plan, failedIDs)
	if blocked == 0 {
		return failedCount
	}
	c.logger.Info("Treating dependents-of-failed as terminal-failed for convergence",
		"slug", plan.Slug,
		"failed_directly", failedCount,
		"blocked_by_failure", blocked,
		"completed", completedCount,
		"total", totalRequired)
	return failedCount + blocked
}

// countBlockedByFailure walks the requirement DAG and returns the count of
// non-terminal requirements that transitively depend on a failed requirement.
// These reqs can never reach a successful terminal state (the orchestrator
// only dispatches reqs whose deps have completed), so they should be treated
// as terminal-equivalent for convergence purposes — otherwise the plan hangs
// in implementing waiting for them to start.
//
// Pure function over plan.Requirements + already-failed-IDs; no I/O, easy
// to unit-test. Iterative transitive closure: each pass adds reqs whose
// depends_on contains an already-blocked or already-failed ID. Stops when
// a pass adds nothing new — bounded at len(plan.Requirements) iterations.
func countBlockedByFailure(plan *workflow.Plan, failedIDs map[string]bool) int {
	if plan == nil || len(failedIDs) == 0 {
		return 0
	}
	// Start the blocked set with the failed IDs themselves so transitive
	// dependents see them. We'll subtract failed at the end so we only
	// return the count of NEW blocked-but-not-yet-counted reqs.
	blocked := make(map[string]bool, len(failedIDs))
	for id := range failedIDs {
		blocked[id] = true
	}

	// Iterate until no new additions. Worst case is a chain of N reqs where
	// each iteration adds one — bounded at len(plan.Requirements).
	for i := 0; i < len(plan.Requirements); i++ {
		added := false
		for _, req := range plan.Requirements {
			if blocked[req.ID] {
				continue
			}
			for _, dep := range req.DependsOn {
				if blocked[dep] {
					blocked[req.ID] = true
					added = true
					break
				}
			}
		}
		if !added {
			break
		}
	}

	// Subtract the failed IDs — caller already counted those.
	count := 0
	for id := range blocked {
		if !failedIDs[id] {
			count++
		}
	}
	return count
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
