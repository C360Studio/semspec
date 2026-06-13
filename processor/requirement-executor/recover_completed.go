package requirementexecutor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow"
	"github.com/nats-io/nats.go/jetstream"
)

// loadTerminalReqExecFromKV fetches a terminal-state requirement execution
// for the given slug + requirement ID directly via the deterministic KV
// key. Used by handlePlanDecisionAccepted when findAwaitingByRequirement
// returns nil — covers the QA-rejection wedge shape where the
// requirement's per-req gates (dev + reviewer) approved but the plan-level
// QA verdict rejected.
//
// Returns nil + nil when no matching entry exists or stage is non-
// terminal. Returns nil + error only on KV transport failure so the
// caller can log and skip; non-fatal because the broader chain has other
// failure surfaces (timeout fallbacks in plan-decision-handler).
//
// The returned *requirementExecution is rebuilt via rebuildExecFromKV — it
// carries DAG, node index, retry counters, branch name, and the storeKey
// the resume path needs. Caller MUST insert it into c.activeExecs before
// calling resumeFromRecoveryLocked; the resume path expects the exec to
// be cache-resident so the dispatched decomposer's completion callback
// can route back.
func (c *Component) loadTerminalReqExecFromKV(ctx context.Context, slug, requirementID string) (*requirementExecution, error) {
	if c.natsClient == nil {
		return nil, nil
	}

	bucket, err := c.natsClient.GetKeyValueBucket(ctx, "EXECUTION_STATES")
	if err != nil {
		return nil, fmt.Errorf("get EXECUTION_STATES bucket: %w", err)
	}
	kvStore := c.natsClient.NewKVStore(bucket)

	key := workflow.RequirementExecutionKey(slug, requirementID)
	entry, err := kvStore.Get(ctx, key)
	if err != nil {
		// Distinguish "no entry" from "transport failure". natsclient maps the
		// NATS KV not-found path to a wrapped jetstream.ErrKeyNotFound; treat
		// any not-found-shaped error as a soft miss.
		if isKVNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get %s: %w", key, err)
	}

	var reqExec workflow.RequirementExecution
	if err := json.Unmarshal(entry.Value, &reqExec); err != nil {
		c.logger.Debug("Skipping corrupt EXECUTION_STATES entry during QA-recovery load",
			"key", key, "error", err)
		return nil, nil
	}
	if !workflow.IsTerminalReqStage(reqExec.Stage) {
		// Caller already tried the awaiting path. If we're here and the KV
		// entry is mid-flight, the activeExecs cache lookup just hadn't
		// landed yet — better to no-op than race the lifecycle.
		return nil, nil
	}
	return c.rebuildExecFromKV(key, &reqExec), nil
}

// countAcceptedRecoveryCyclesForReq returns how many recovery-agent
// PlanDecisions have already been accepted for this requirement on this
// plan. Used as a defense-in-depth budget gate for the QA-recovery path —
// the per-exec recoveryRestarts counter resets on every KV reload (it's
// not persisted in workflow.RequirementExecution), so without this check
// a thrashing QA verdict (needs_changes → recover → still flaky → repeat)
// could loop indefinitely.
//
// Returns 0 on any load/parse failure so a transient KV error doesn't
// false-fail the recovery — the outer Playwright/operator budget is the
// last-line guard.
func (c *Component) countAcceptedRecoveryCyclesForReq(ctx context.Context, slug, requirementID string) int {
	if c.natsClient == nil {
		return 0
	}
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Debug("QA-recovery budget: jetstream unavailable; allowing resume",
			"slug", slug, "requirement_id", requirementID, "error", err)
		return 0
	}
	bucket, err := js.KeyValue(ctx, "PLAN_STATES")
	if err != nil {
		c.logger.Debug("QA-recovery budget: PLAN_STATES bucket unavailable; allowing resume",
			"slug", slug, "requirement_id", requirementID, "error", err)
		return 0
	}
	entry, err := bucket.Get(ctx, slug)
	if err != nil {
		c.logger.Debug("QA-recovery budget: plan not in PLAN_STATES; allowing resume",
			"slug", slug, "requirement_id", requirementID, "error", err)
		return 0
	}
	var plan struct {
		PlanDecisions []workflow.PlanDecision `json:"plan_decisions"`
	}
	if err := json.Unmarshal(entry.Value(), &plan); err != nil {
		c.logger.Debug("QA-recovery budget: plan JSON unmarshal failed; allowing resume",
			"slug", slug, "requirement_id", requirementID, "error", err)
		return 0
	}
	count := 0
	for _, dec := range plan.PlanDecisions {
		if dec.ProposedBy != "recovery-agent" {
			continue
		}
		if dec.Status != workflow.PlanDecisionStatusAccepted {
			continue
		}
		for _, affected := range dec.AffectedReqIDs {
			if affected == requirementID {
				count++
				break
			}
		}
	}
	return count
}

// resumeTerminalForRecoveryLocked transitions a terminal requirement
// execution back through the recovery flow. Mirrors the awaiting_recovery
// shape: marks the exec awaiting + records the proposal context, then
// defers to the existing resumeFromRecoveryLocked which does the actual
// DAG reset, branch reset, and decomposer dispatch.
//
// The caller must hold exec.mu and have inserted exec into c.activeExecs.
// Splitting this out keeps the new code path's diff small and makes the
// "state mark before resume" the only NEW behavior — the resume logic
// itself is unchanged.
func (c *Component) resumeTerminalForRecoveryLocked(ctx context.Context, exec *requirementExecution, proposalID string) {
	exec.awaitingRecovery = true
	exec.recoveryReason = fmt.Sprintf("QA-recovery: completed req re-dispatched (proposal %s)", proposalID)
	c.logger.Info("Resuming completed requirement from QA-recovery",
		"entity_id", exec.EntityID,
		"slug", exec.Slug,
		"requirement_id", exec.RequirementID,
		"prior_stage", "terminal",
		"proposal_id", proposalID,
	)
	// Re-open the M:N Stories this requirement owns so the resumed dev loop
	// re-does work instead of skipping ("Story already complete"). Without this,
	// QA-recovery on an M:N-complete plan is a no-op: the req re-dispatches, every
	// Story is skipped, the req re-marks complete, and the plan bounces back to QA
	// unchanged. Must run BEFORE resumeFromRecoveryLocked so the reset stories are
	// visible when dispatchSynthesizerLocked reloads the plan from KV.
	c.reopenOwnedStoriesForRecoveryLocked(ctx, exec)
	c.resumeFromRecoveryLocked(ctx, exec)
	// Invalidate the dependent branch subtree AFTER resumeFromRecoveryLocked has
	// moved this requirement off "completed" (sendReqPhase → decomposing). That
	// ordering is load-bearing: the orchestrator reconciles its completed-set
	// before gating each sweep, so once this owner shows non-completed the
	// branch-prereq gate HOLDS the dependents until it re-completes — they cannot
	// re-dispatch against the owner's pre-rebuild branch. Doing the reset here
	// (not in plan-manager's accept path) is what makes that ordering hold:
	// plan-manager would reset dependents while this owner was still "completed",
	// opening a window for a stale re-dispatch.
	c.invalidateDependentBranchSubtreeLocked(ctx, exec)
}

// invalidateDependentBranchSubtreeLocked resets every requirement whose branch
// DERIVES (transitively) from exec — deleting its execution state and stale
// branches — so the orchestrator re-dispatches it and it re-forks from exec's
// rebuilt branch instead of staying stale (the P3 recovery-staleness bug). The
// branch-derivation gate then re-sequences the subtree behind exec's
// re-completion. Best-effort: a failed reset logs and continues; the pre-QA
// assembly conflict gate still catches any dependent this misses, so a transient
// failure degrades to a detected conflict, never silent corruption.
func (c *Component) invalidateDependentBranchSubtreeLocked(ctx context.Context, exec *requirementExecution) {
	if c.natsClient == nil {
		return
	}
	plan, err := c.loadPlanFromKV(ctx, exec.Slug)
	if err != nil || plan == nil {
		c.logger.Warn("QA-recovery: could not load plan to invalidate dependent branch subtree",
			"slug", exec.Slug, "requirement_id", exec.RequirementID, "error", err)
		return
	}
	c.resetDependentBranchSubtree(ctx, exec.Slug, exec.RequirementID, plan)
}

// coAffectedDependents returns the subset of affectedReqIDs that DERIVE
// (transitively) from another requirement in the same recovery event. Those must
// NOT be directly resumed: the prerequisite's resume cascade resets and
// re-derives them, whereas a direct resume would recreate their branch from the
// prerequisite's PRE-rebuild base. Best-effort — a plan-load failure or a
// single-requirement event returns nil (every affected req resumes directly).
func (c *Component) coAffectedDependents(ctx context.Context, slug string, affectedReqIDs []string) map[string]bool {
	if len(affectedReqIDs) < 2 {
		return nil
	}
	plan, err := c.loadPlanFromKV(ctx, slug)
	if err != nil || plan == nil {
		return nil
	}
	return coAffectedDependentSet(affectedReqIDs, plan.Requirements, plan.Stories)
}

// coAffectedDependentSet is the pure core of coAffectedDependents: the affected
// requirements that derive (transitively) from another affected requirement.
func coAffectedDependentSet(affectedReqIDs []string, reqs []workflow.Requirement, stories []workflow.Story) map[string]bool {
	affected := make(map[string]bool, len(affectedReqIDs))
	for _, id := range affectedReqIDs {
		affected[id] = true
	}
	skip := make(map[string]bool)
	for _, owner := range affectedReqIDs {
		for _, dep := range workflow.DependentBranchSubtree(owner, reqs, stories) {
			if affected[dep] {
				skip[dep] = true
			}
		}
	}
	return skip
}

// resetDependentBranchSubtree resets every requirement whose branch derives from
// reopenedID — deleting its execution state (req.reset) and its stale branches —
// so the orchestrator re-dispatches and re-derives them. Split from the plan
// load so the dep-enumeration + branch-delete wiring is unit-testable with a
// stub sandbox.
func (c *Component) resetDependentBranchSubtree(ctx context.Context, slug, reopenedID string, plan *workflow.Plan) {
	deps := workflow.DependentBranchSubtree(reopenedID, plan.Requirements, plan.Stories)
	if len(deps) == 0 {
		return
	}
	for _, dep := range deps {
		// Tear down any LIVE exec for this dependent first. A dependent that was
		// still mid-execution when its prerequisite reopened is building on the
		// now-stale base; leaving it in activeExecs would make the req_watcher
		// duplicate guard swallow the fresh re-dispatch (it keys on EntityID), so
		// the stale loop would run to completion and the re-derived one never
		// starts. Removing it lets the orchestrator's re-dispatch claim cleanly.
		c.abandonLiveExecForRequirement(slug, dep)
		if err := c.sendReqReset(ctx, workflow.RequirementExecutionKey(slug, dep)); err != nil {
			c.logger.Warn("Failed to reset dependent requirement for re-derivation",
				"slug", slug, "requirement_id", dep, "error", err)
		}
		if c.sandbox != nil {
			for _, branch := range []string{"semspec/requirement-" + dep, "semspec/reqbase-" + dep} {
				if delErr := c.sandbox.DeleteBranch(ctx, branch); delErr != nil &&
					!strings.Contains(delErr.Error(), "server error 404") {
					c.logger.Warn("Failed to delete stale dependent branch on recovery",
						"slug", slug, "branch", branch, "error", delErr)
				}
			}
		}
	}
	c.logger.Info("Invalidated dependent branch subtree for re-derivation on QA-recovery",
		"slug", slug, "reopened", reopenedID, "dependents", deps)
}

// storiesToReopenForRecovery returns the IDs of complete Stories covering reqID
// that reqID owns (the deterministic M:N owner). These are the Stories a
// QA-recovery resume must reset complete → ready so the dev loop re-runs rather
// than skipping. Non-owned Stories are excluded — resetting one would let a
// non-owner run the dev loop, inverting the M:N reservation; the non-owner
// instead re-skips (Story stays complete) and fast-completes via Tier-1 dedup
// once the owner re-ships. Non-complete Stories are excluded (nothing to
// re-open). Pure function — the side-effecting reopen lives in
// reopenOwnedStoriesForRecoveryLocked.
func storiesToReopenForRecovery(plan *workflow.Plan, reqID string) []string {
	if plan == nil {
		return nil
	}
	var ids []string
	for _, s := range plan.StoriesForRequirement(reqID) {
		if s.Status == workflow.StoryStatusComplete && workflow.DeterministicStoryOwner(s) == reqID {
			ids = append(ids, s.ID)
		}
	}
	return ids
}

// reopenOwnedStoriesForRecoveryLocked resets the owner's complete M:N Stories to
// ready via the cross-component story-status mutation. No-op without a NATS
// client (unit-test substrate) or when the plan can't be loaded. Idempotent
// across recovery cycles: candidates are pre-filtered to complete Stories, so a
// re-run reopens nothing once a Story has already moved on to ready/executing.
func (c *Component) reopenOwnedStoriesForRecoveryLocked(ctx context.Context, exec *requirementExecution) {
	if c.natsClient == nil {
		return
	}
	plan, err := c.loadPlanFromKV(ctx, exec.Slug)
	if err != nil || plan == nil {
		c.logger.Warn("QA-recovery: could not load plan to reopen owned stories",
			"slug", exec.Slug, "requirement_id", exec.RequirementID, "error", err)
		return
	}
	ids := storiesToReopenForRecovery(plan, exec.RequirementID)
	reopened := 0
	for _, storyID := range ids {
		if workflow.ClaimStoryStatus(ctx, c.natsClient, exec.Slug, storyID, workflow.StoryStatusReady, c.logger) {
			reopened++
		}
	}
	switch {
	case len(ids) > 0 && reopened < len(ids):
		// A reopen was expected but did not fully land (NATS error or a rejected
		// mutation). Proceeding lets the un-reopened stories re-skip and the
		// requirement re-complete without work — silently resurrecting the no-op
		// this function exists to fix. Surface it so it's diagnosable.
		c.logger.Warn("QA-recovery: not all owned stories reopened — recovery may be a partial no-op",
			"slug", exec.Slug, "requirement_id", exec.RequirementID,
			"reopened", reopened, "candidates", len(ids))
	case reopened > 0:
		c.logger.Info("QA-recovery: reopened owned M:N stories for re-execution",
			"slug", exec.Slug, "requirement_id", exec.RequirementID,
			"reopened", reopened, "candidates", len(ids))
	}
}

// isKVNotFound returns true when err is the soft "no entry" shape returned
// by NATS KV Get. Mirrors the existing pattern in question-manager
// (component.go:258, 310): accept both the jetstream sentinel and the
// string-flattened variant some wrappers emit.
func isKVNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return true
	}
	return strings.Contains(err.Error(), "key not found")
}
