package scenarioorchestrator

import "github.com/c360studio/semspec/workflow"

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
