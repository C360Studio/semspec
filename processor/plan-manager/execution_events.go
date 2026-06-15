package planmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	projectmanager "github.com/c360studio/semspec/processor/project-manager"
	"github.com/c360studio/semspec/tools/sandbox"
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
		c.handleConvergenceAllSucceeded(ctx, plan, slug, completedCount)
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

// handleConvergenceAllSucceeded runs the implementing-convergence success arm:
// all requirements reached terminal-success, so assemble their branches, stage
// the QA worktree, and advance the plan to its QA target. Split out of
// checkPlanConvergence to keep each function focused (and under the length cap).
func (c *Component) handleConvergenceAllSucceeded(ctx context.Context, plan *workflow.Plan, slug string, completedCount int) {
	// Branch on the plan's QA level (snapshotted at plan creation).
	level := plan.EffectiveQALevel()
	target := c.targetForQALevel(level, plan, slug)

	// Assemble the per-requirement branches into the plan branch and stage a
	// dedicated QA worktree from it BEFORE QA runs, so both the unit-test runner
	// and the release-gate Murat loop inspect the merged implementation instead
	// of the pre-implementation main HEAD (the data-plane fix). Only when QA will
	// actually run (ready_for_qa); the none/gated path goes straight to
	// complete/awaiting_review and relies on the approve-time assemble. On
	// conflict/failure we route to recovery or stall and do NOT advance to QA
	// against an unmerged tree.
	if target == workflow.StatusReadyForQA {
		if err := c.assembleAndStageQAWorktree(ctx, plan); err != nil {
			c.routeAssemblyConflict(ctx, plan, err)
			return
		}
	}

	c.logger.Info("All requirements completed — transitioning to review",
		"slug", slug,
		"completed", completedCount,
		"qa_level", level,
		"target", target,
		"assembled_branch", plan.AssembledBranch)

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
}

// routeAssemblyConflict handles a failed pre-QA assemble/stage. The plan never
// advanced out of implementing — we must NOT proceed to QA against an unmerged
// tree.
//
// A merge conflict (two truly-parallel branches edited the same file with no
// DependsOn edge) is a planning-partition defect, not an execution failure: all
// requirements passed review, the branches just can't merge. In an unattended
// run firing recovery's escalate_human has no fallback — the decision sits
// unhandled and the plan idles at implementing until the Playwright timeout
// (~65 min), failing late with no actionable signal (issue #176). So we instead
// FAIL FAST and TERMINAL: record a distinct assembly_conflict PlanDecision
// naming the conflicting branch + files (so dashboards/recovery don't conflate
// it with execution_exhausted) and transition the plan to rejected, where the
// defect surfaces immediately and the operator's existing /retry endpoints
// apply. Prevention is the file-ownership gates (#175); this is the honest
// backstop when an undeclared shared file slips through.
//
// Any other failure (sandbox unreachable, worktree staging) is a transient
// stall: surface LastError and bump the SSE revision so the UI shows the stall;
// an operator retry or the next execution event re-drives convergence. We
// deliberately do NOT fail the plan for an infra blip.
func (c *Component) routeAssemblyConflict(ctx context.Context, plan *workflow.Plan, assembleErr error) {
	// Runs from the EXECUTION_STATES watcher (checkPlanConvergence), which is
	// lock-free like the sibling stall/infra-critical saves here. The
	// implementing-status guard keeps the overlap with concurrent qa.start/verdict
	// mutations narrow; a rare interleave is last-writer-wins on LastError, not
	// state corruption.
	now := time.Now()
	plan.LastError = fmt.Sprintf("pre-QA assembly failed: %v", assembleErr)
	plan.LastErrorAt = &now

	if errors.Is(assembleErr, sandbox.ErrMergeBranchesConflict) {
		c.failPlanOnAssemblyConflict(ctx, plan, assembleErr, now)
		return
	}

	if saveErr := c.savePlanCached(ctx, plan); saveErr != nil {
		c.logger.Error("Failed to persist LastError after pre-QA assembly failure",
			"slug", plan.Slug, "error", saveErr)
	}
	c.logger.Error("Pre-QA assembly failed (non-conflict) — plan stalls in implementing pending retry",
		"slug", plan.Slug, "error", assembleErr)
}

// failPlanOnAssemblyConflict records an assembly_conflict PlanDecision and
// transitions the plan to terminal rejected (issue #176). assembleErr already
// names the conflicting branch + files (see assembleRequirementBranches), so its
// message is the actionable signal carried into both LastError and the decision
// rationale. The decision is authored by plan-manager (ProposedBy != the
// recovery-agent gate), so the auto-accept watcher ignores it — it is a terminal
// record, not a cascade trigger.
func (c *Component) failPlanOnAssemblyConflict(ctx context.Context, plan *workflow.Plan, assembleErr error, now time.Time) {
	affected := make([]string, 0, len(plan.Requirements))
	for _, r := range plan.Requirements {
		affected = append(affected, r.ID)
	}

	decision := workflow.PlanDecision{
		ID:             fmt.Sprintf("plan-decision.%s.%d", plan.Slug, len(plan.PlanDecisions)+1),
		PlanID:         workflow.PlanEntityID(plan.Slug),
		Kind:           workflow.PlanDecisionKindAssemblyConflict,
		Title:          "Plan-level merge conflict at branch assembly",
		Rationale:      fmt.Sprintf("Execution succeeded but the requirement branches could not be merged: %v. This is a planning-partition defect — two parallel branches edited the same file with no ownership/DependsOn edge. Re-run with corrected file ownership (the offending file must be declared on exactly one component, or shared owners serialized).", assembleErr),
		Status:         workflow.PlanDecisionStatusProposed,
		ProposedBy:     "plan-manager",
		AffectedReqIDs: affected,
		CreatedAt:      now,
	}
	plan.PlanDecisions = append(plan.PlanDecisions, decision)

	c.logger.Error("Pre-QA plan-level merge conflict — failing plan terminally (planning-partition defect)",
		"slug", plan.Slug, "decision_kind", string(decision.Kind), "error", assembleErr)

	if err := c.setPlanStatusCached(ctx, plan, workflow.StatusRejected); err != nil {
		// setPlanStatusCached persists the plan (including the appended decision
		// and LastError). On failure, fall back to a direct save so the decision
		// and LastError are not lost even if the status transition didn't apply.
		c.logger.Error("Failed to transition plan to rejected after assembly conflict",
			"slug", plan.Slug, "error", err)
		if saveErr := c.savePlanCached(ctx, plan); saveErr != nil {
			c.logger.Error("Failed to persist assembly-conflict decision after transition failure",
				"slug", plan.Slug, "error", saveErr)
		}
	}
}

// publishQARequestIfNeeded fires when a plan is routed to ready_for_qa. For
// sandbox-executed QA levels it publishes a QARequestedEvent so the sandbox
// runs the project's configured QA command before qa-reviewer interprets.
// synthesis/none don't dispatch (synthesis is claimed directly by qa-reviewer;
// none skips QA). Full/e2e orchestration remains operator CI for MVP.
// No-op when plan.Status != ready_for_qa or natsClient is nil (tests).
func (c *Component) publishQARequestIfNeeded(ctx context.Context, plan *workflow.Plan) {
	if plan == nil || plan.Status != workflow.StatusReadyForQA || c.natsClient == nil {
		return
	}

	level := plan.EffectiveQALevel()
	if !level.UsesSandboxTests() {
		return
	}

	// Load project config for test command (language-aware default).
	pc := workflow.LoadProjectConfigFromDisk(c.resolveRepoRoot())

	// Emit the operator's CI contract (.github/workflows/qa.yml). When the
	// architecture selected services-orchestrated harness profiles, render the
	// catalog-injected services block (ADR-039 Phase 1c) so the operator's CI
	// can stand up the live integration targets semspec's sandbox cannot;
	// otherwise scaffold the language-aware default without clobbering an
	// operator-owned workflow. Non-fatal: a failure just logs.
	if !c.maybeRenderQAWithServices(plan, pc) {
		if err := projectmanager.EnsureQAWorkflow(c.resolveRepoRoot(), pc, c.logger); err != nil {
			c.logger.Warn("Failed to scaffold operator qa.yml", "slug", plan.Slug, "error", err)
		}
	}

	req := &payloads.QARequestedPayload{
		QARequestedEvent: workflow.QARequestedEvent{
			Slug:        plan.Slug,
			PlanID:      plan.ID,
			Mode:        level,
			Workspace:   workflow.QAWorktreeID(plan.Slug),
			TestCommand: pc.EffectiveTestCommand(),
			TraceID:     uuid.New().String(),
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
// level=none      → StatusComplete (or StatusAwaitingReview when gated)
// level=synthesis → StatusReadyForQA (qa-reviewer claims it directly, no tests run)
// level=unit/integration → StatusReadyForQA (the sandbox runs project QA first via
//
//	a QARequestedEvent, then qa-reviewer interprets the result)
//
// Full/e2e orchestration is NOT executed by semspec — it runs in the operator's
// CI against the emitted qa.yml. qa-reviewer owns the ready_for_qa →
// reviewing_qa transition via plan.mutation.qa.start; executable levels also
// fire a QARequestedEvent so the sandbox runs before qa-reviewer claims it.
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
