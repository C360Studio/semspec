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

// loadNonTerminalReqExecFromKV fetches an in-flight requirement execution for
// the given slug + requirement ID directly via the deterministic KV key. It is
// used by the task-completion watcher as a durable fallback when activeExecs
// has lost the owner for a terminal node completion, which can happen after a
// QA-recovery reopens a completed requirement under the execution-manager's
// hashed req.run entity ID.
//
// Returns nil + nil for missing or terminal entries. Returns nil + error only
// for KV transport failure.
func (c *Component) loadNonTerminalReqExecFromKV(ctx context.Context, slug, requirementID string) (*requirementExecution, error) {
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
		if isKVNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get %s: %w", key, err)
	}

	var reqExec workflow.RequirementExecution
	if err := json.Unmarshal(entry.Value, &reqExec); err != nil {
		c.logger.Debug("Skipping corrupt EXECUTION_STATES entry during task-completion owner recovery",
			"key", key, "error", err)
		return nil, nil
	}
	if workflow.IsTerminalReqStage(reqExec.Stage) {
		return nil, nil
	}
	return c.rebuildExecFromKV(key, &reqExec), nil
}

// loadCompletedReqExecFromKV fetches a completed requirement execution
// from EXECUTION_STATES. ADR-044 M:N dedup uses this as the evidence
// source when a non-owner requirement advances past a Story already
// completed by its deterministic owner. Failed/error/in-flight entries
// are not valid proof of shipped work.
func (c *Component) loadCompletedReqExecFromKV(ctx context.Context, slug, requirementID string) (*requirementExecution, error) {
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
		if isKVNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get %s: %w", key, err)
	}

	var reqExec workflow.RequirementExecution
	if err := json.Unmarshal(entry.Value, &reqExec); err != nil {
		c.logger.Debug("Skipping corrupt EXECUTION_STATES entry during completed-owner evidence load",
			"key", key, "error", err)
		return nil, nil
	}
	if reqExec.Stage != phaseCompleted {
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
	// resumeFromRecoveryLocked now calls reopenOwnedStoriesForRecoveryLocked
	// at its own top (before branch recreation + re-decompose), so both the
	// QA-recovery path (here) and the dev-gate / iteration-exhaustion path
	// (handlePlanDecisionAccepted) reset owned stories. The explicit
	// reopenOwnedStoriesForRecoveryLocked call that previously appeared here
	// was moved into resumeFromRecoveryLocked to close the ADR-049 dev-gate
	// fast-fail gap — the ordering invariant (reset before plan reload) is
	// preserved because the reopen is the first thing resumeFromRecoveryLocked
	// does.
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

// storyStatusWalkToReady returns the sequence of StoryStatus transitions
// required to walk from `from` to StoryStatusReady via the valid state machine
// edges. Used by reopenOwnedStoriesForRecoveryLocked to reset in-flight owned
// stories that a recovery resume must reclaim.
//
// Transition chains (smallest to largest):
//
//	Complete  → Ready
//	Pending   → Ready
//	Failed    → Pending → Ready
//	Executing → Failed  → Pending → Ready
//
// Returns nil when `from` is already Ready (no steps needed). The steps are
// applied in order via sequential ClaimStoryStatus calls; each step must
// succeed before the next is attempted. This avoids inventing a new state-
// machine shortcut transition and instead threads through existing valid edges —
// keeping the plan-manager handler (handleStoryStatusMutation) unchanged.
//
// Pure function — no side effects.
func storyStatusWalkToReady(from workflow.StoryStatus) []workflow.StoryStatus {
	switch from {
	case workflow.StoryStatusReady:
		return nil // already dispatchable, nothing to do
	case workflow.StoryStatusPending:
		return []workflow.StoryStatus{workflow.StoryStatusReady}
	case workflow.StoryStatusComplete:
		return []workflow.StoryStatus{workflow.StoryStatusReady}
	case workflow.StoryStatusFailed:
		return []workflow.StoryStatus{workflow.StoryStatusPending, workflow.StoryStatusReady}
	case workflow.StoryStatusExecuting:
		return []workflow.StoryStatus{
			workflow.StoryStatusFailed,
			workflow.StoryStatusPending,
			workflow.StoryStatusReady,
		}
	default:
		return nil
	}
}

// storiesToReopenForRecovery returns the IDs of Stories covering reqID that
// reqID owns (the deterministic M:N owner) and that need to be walked back to
// Ready before a recovery resume. This covers all non-ready states that would
// cause false-completion on re-dispatch:
//
//   - Complete: QA-recovery shape — req completed + plan-level QA rejected.
//   - Executing: dev-gate fast-fail shape (ADR-049 Move-3) — the executor
//     transitioned the req to awaiting-recovery while the Story was mid-
//     dispatch. Without resetting, ClaimStoryStatus(→Executing) is rejected on
//     resume (Executing→Executing is invalid), causing false-skip/false-complete.
//   - Failed: story failed before the requirement reached terminal state.
//   - Pending: Pending→Executing is also an invalid transition per CanTransitionTo
//     (Pending→{Ready,Failed} only). A story stranded at Pending — e.g. by a
//     partial walk from a prior recovery attempt — must be walked to Ready so the
//     executor can claim it.
//
// Only Ready stories are excluded — they are already dispatchable (Ready→Executing
// is the expected claim). Non-owned Stories are also excluded — resetting one would
// let a non-owner run the dev loop, inverting the M:N reservation.
//
// Pure function — the side-effecting walk lives in reopenOwnedStoriesForRecoveryLocked.
func storiesToReopenForRecovery(plan *workflow.Plan, reqID string) []string {
	if plan == nil {
		return nil
	}
	var ids []string
	for _, s := range plan.StoriesForRequirement(reqID) {
		if workflow.DeterministicStoryOwner(s) != reqID {
			continue // non-owner: M:N reservation must not be violated
		}
		switch s.Status {
		case workflow.StoryStatusComplete,
			workflow.StoryStatusExecuting,
			workflow.StoryStatusFailed,
			workflow.StoryStatusPending:
			ids = append(ids, s.ID)
			// Ready is the only dispatchable state; no walk needed.
		}
	}
	return ids
}

// storyReopenResult summarises whether the reopen walk succeeded for all
// candidate stories owned by a requirement.
type storyReopenResult struct {
	// candidates is the number of stories that needed a walk (len(ids) from
	// storiesToReopenForRecovery). Zero means no walk was needed.
	candidates int
	// reopened is the number that reached StoryStatusReady.
	reopened int
}

// allReady reports whether every candidate story reached Ready.
func (r storyReopenResult) allReady() bool {
	return r.candidates == 0 || r.reopened == r.candidates
}

// reopenStoriesFromPlan is the pure inner kernel of the story-walk logic. It
// walks each story in `ids` from its current status to Ready using the supplied
// claimer, and returns how many reached Ready. Extracted so tests can call it
// directly with a fake claimer and a synthetic plan — without needing a live
// NATS substrate or loadPlanFromKV. The method reopenOwnedStoriesForRecoveryLocked
// is the production entry-point that adds plan-loading and logging.
//
// Self-healing across cycles (C1): the walk for each story is derived from the
// status read out of `plan` on THIS call. Production reloads the plan from KV on
// every recovery cycle (reopenOwnedStoriesForRecoveryLocked), so a prior partial
// walk that stranded a story at e.g. Pending is re-derived from Pending and
// resumed on the next call — no in-loop state is carried between calls.
func reopenStoriesFromPlan(
	ctx context.Context,
	slug string,
	plan *workflow.Plan,
	ids []string,
	claimer func(ctx context.Context, slug, storyID string, target workflow.StoryStatus) bool,
) (reopened, candidates int) {
	candidates = len(ids)
	for _, storyID := range ids {
		story, ok := findStoryByID(plan, storyID)
		if !ok {
			continue
		}
		walkedOK := true
		for _, step := range storyStatusWalkToReady(story.Status) {
			if !claimer(ctx, slug, storyID, step) {
				walkedOK = false
				break
			}
		}
		if walkedOK {
			reopened++
		}
	}
	return reopened, candidates
}

// reopenOwnedStoriesForRecoveryLocked walks the owner's non-ready Stories back
// to Ready via sequential claim mutations. Covers four shapes:
//
//   - Pending   → Ready (1 hop — e.g. stranded by prior partial walk; C2 fix)
//   - Complete  → Ready (1 hop — QA-recovery)
//   - Failed    → Pending → Ready (2 hops)
//   - Executing → Failed → Pending → Ready (3 hops — dev-gate fast-fail shape)
//
// Self-healing across cycles (C1 fix): delegates to reopenStoriesFromPlan which
// tracks the story's evolving status locally so subsequent hops start from where
// the last one landed. A prior partial walk that stranded a story at Pending is
// resumed from Pending→Ready rather than re-attempting the full original chain.
//
// No-op without a NATS client (unit-test substrate) — returns allReady so
// resumeFromRecoveryLocked proceeds normally in tests. The walk logic is
// exercised separately via reopenStoriesFromPlan tests with an injected fake
// claimer. When a NATS client is present and not all stories reached Ready,
// returns the counts so resumeFromRecoveryLocked can defer instead of dispatching
// into a guaranteed false-complete.
//
// Uses c.storyStatusClaimer (seam) rather than workflow.ClaimStoryStatus
// directly so tests can inject a fake that enforces CanTransitionTo and asserts
// the full walk sequence without a live plan-manager.
//
// Called from resumeFromRecoveryLocked (which serves BOTH the
// iteration-exhaustion / dev-gate path AND the QA-recovery path).
//
// TODO(R2): a persistent story walk failure (e.g. plan-manager rejects the
// transition because it already moved on) can strand the exec indefinitely
// unless the outer recovery-timeout fires. A future improvement is to detect
// N consecutive plan-load-or-walk failures for the same exec and fall through
// to markFailedLocked. Track with a per-exec counter; N=3 is a reasonable
// starting point. Low urgency: the recovery timer already caps the max wait.
//
// TODO(ps.save): handleStoryStatusMutation in plan-manager saves the plan on
// every story transition (mutations.go:848). For the 3-hop Executing walk this
// means 3 consecutive KV writes to PLAN_STATES within milliseconds. A future
// optimisation is to batch the walk steps into a single mutation request (e.g. a
// "walk_to_ready" mutation kind) so plan-manager applies all transitions
// atomically. Low urgency: the current 3-hop path is correct and plan-manager's
// per-write latency is negligible relative to LLM dispatch.
func (c *Component) reopenOwnedStoriesForRecoveryLocked(ctx context.Context, exec *requirementExecution) storyReopenResult {
	if c.natsClient == nil {
		// Unit-test path: no NATS, no story state to reset. Report allReady
		// so the caller proceeds to re-decompose. The walk logic is exercised
		// separately via reopenStoriesFromPlan tests with an injected fake claimer.
		return storyReopenResult{}
	}
	plan, err := c.loadPlanFromKV(ctx, exec.Slug)
	if err != nil || plan == nil {
		c.logger.Warn("recovery: could not load plan to reopen owned stories",
			"slug", exec.Slug, "requirement_id", exec.RequirementID, "error", err)
		// Plan-load failure: treat as no candidates so caller can still proceed
		// (degraded mode — better than wedging the exec permanently).
		return storyReopenResult{}
	}
	ids := storiesToReopenForRecovery(plan, exec.RequirementID)
	if len(ids) == 0 {
		return storyReopenResult{}
	}

	slug := exec.Slug
	reqID := exec.RequirementID
	reopened, candidates := reopenStoriesFromPlan(ctx, slug, plan, ids, func(ctx context.Context, slug, storyID string, target workflow.StoryStatus) bool {
		ok := c.storyStatusClaimer(ctx, slug, storyID, target)
		if !ok {
			c.logger.Warn("recovery: story status walk step rejected — will defer resume to retry",
				"slug", slug, "requirement_id", reqID,
				"story_id", storyID, "target", target)
		}
		return ok
	})
	result := storyReopenResult{candidates: candidates, reopened: reopened}

	switch {
	case result.reopened < result.candidates:
		c.logger.Warn("recovery: not all owned stories walked to ready — deferring resume to next cycle",
			"slug", exec.Slug, "requirement_id", exec.RequirementID,
			"reopened", result.reopened, "candidates", result.candidates)
	default:
		c.logger.Info("recovery: owned stories walked to ready for re-execution",
			"slug", exec.Slug, "requirement_id", exec.RequirementID,
			"reopened", result.reopened, "candidates", result.candidates)
	}
	return result
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
