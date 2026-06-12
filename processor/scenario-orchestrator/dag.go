package scenarioorchestrator

import (
	"github.com/c360studio/semspec/workflow"
)

// requirementComplete returns true when the requirement has reached
// terminal "completed" stage in EXECUTION_STATES.
//
// Source of truth: req_completion_watcher caches each req.<slug>.<reqID>
// entry from EXECUTION_STATES whose Stage == "completed". The caller passes
// the resulting set of reqIDs through completedReqIDs.
//
// scenario.status is intentionally NOT consulted — historically it was the
// gating field but nothing in processor/ writes it (verdict translation is
// missing), which left chain-dep requirements starved. Using the same
// signal plan-manager's checkPlanConvergence already uses keeps the two
// gates consistent.
func requirementComplete(reqID string, completedReqIDs map[string]bool) bool {
	return completedReqIDs[reqID]
}

// filterReadyRequirements applies requirement-DAG gating and returns
// requirements that are ready for execution:
//
//  1. The requirement is not yet complete (stage != "completed" in EXECUTION_STATES).
//  2. All DependsOn requirements ARE complete.
//
// Requirements without DependsOn are dispatched as long as they are not
// already complete.
//
// Parameters:
//   - requirements: all requirements for the plan.
//   - completedReqIDs: set of reqIDs whose EXECUTION_STATES stage == "completed".
//     Caller (scenario-orchestrator) builds this from its req_completion_watcher
//     cache; tests pass it directly.
//
// Returns the subset of requirements that should be dispatched.
func filterReadyRequirements(
	requirements []workflow.Requirement,
	completedReqIDs map[string]bool,
) []workflow.Requirement {
	if len(requirements) == 0 {
		return nil
	}

	// Pre-compute which requirements are fully complete. Equivalent to
	// completedReqIDs filtered to the requirement IDs we know about, but the
	// indirection keeps requirementComplete() a clear named predicate.
	complete := make(map[string]bool, len(requirements))
	for _, r := range requirements {
		complete[r.ID] = requirementComplete(r.ID, completedReqIDs)
	}

	// For each requirement, determine if all its upstream deps are complete
	// AND it has pending work (not already complete itself).
	var ready []workflow.Requirement
	for _, req := range requirements {
		// Skip already-complete requirements.
		if complete[req.ID] {
			continue
		}
		if !depsComplete(req, complete) {
			continue
		}
		// Has pending work and all deps satisfied.
		ready = append(ready, req)
	}

	return ready
}

// filterByM2NStoryReservations applies ADR-044's M:N story-ownership gate
// on top of filterReadyRequirements. The contract:
//
//  1. Each Story has a deterministic "owner" requirement = the
//     lexicographically smallest req ID in Story.RequirementIDs. Under
//     the canonical cohesive-component shape (smoke 9 / mavlink-hard: 1
//     Story covering N requirements), this picks exactly one of those
//     N requirements to actually dispatch the dev loop.
//  2. A non-owner requirement may dispatch ONLY when every covering
//     Story it does NOT own has reached Story.Status == complete. The
//     post-completion executor's Tier-1 dedup at component.go:dispatchCurrentStoryLocked
//     immediately fires markCompletedLocked without re-running the dev
//     loop — correct, because the work has actually shipped under the
//     owner's executor.
//
// Without this gate, the post-claim-rejection path in requirement-executor
// would call markCompletedLocked for a non-owner requirement BEFORE the
// owner's Story has shipped, producing a false-positive completion that
// QA-reviewer's capability-evidence rollup consumes as ground truth.
//
// When stories is empty (legacy plans pre-Sarah, mock fixtures without
// Stories), the gate is a no-op pass-through.
//
// Story.Status transitions to Complete are observed when plan-manager
// re-fires scenario.orchestrate.<slug> on plan KV updates — the next
// sweep sees Status=complete and releases the non-owner.
func filterByM2NStoryReservations(ready []workflow.Requirement, stories []workflow.Story) []workflow.Requirement {
	if len(ready) == 0 || len(stories) == 0 {
		return ready
	}

	// Index Stories by every requirement they cover (M:N).
	storiesByReq := make(map[string][]workflow.Story, len(stories))
	for _, s := range stories {
		for _, rid := range s.RequirementIDs {
			storiesByReq[rid] = append(storiesByReq[rid], s)
		}
	}

	out := make([]workflow.Requirement, 0, len(ready))
	for _, req := range ready {
		gated := false
		for _, s := range storiesByReq[req.ID] {
			if workflow.DeterministicStoryOwner(s) == req.ID {
				continue // we own this Story — dispatch normally
			}
			if s.Status == workflow.StoryStatusComplete {
				continue // owner already shipped this Story — we can dispatch and Tier-1 fast-completes
			}
			// Non-owner Story not yet complete — defer until owner ships.
			gated = true
			break
		}
		if !gated {
			out = append(out, req)
		}
	}
	return out
}

// depsComplete returns true when every requirement listed in req.DependsOn is
// present in the complete map and marked true.
func depsComplete(req workflow.Requirement, complete map[string]bool) bool {
	for _, depID := range req.DependsOn {
		if !complete[depID] {
			return false
		}
	}
	return true
}
