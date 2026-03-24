package scenarioorchestrator

import "github.com/c360studio/semspec/workflow"

// requirementComplete returns true when every scenario belonging to req is in a
// terminal-passing state (passing or skipped).
//
// A requirement with no scenarios is considered incomplete because a
// requirement without any verification scenarios cannot be definitively
// satisfied.
func requirementComplete(reqID string, reqScenarios map[string][]workflow.Scenario) bool {
	ss, ok := reqScenarios[reqID]
	if !ok || len(ss) == 0 {
		return false
	}
	for _, s := range ss {
		if s.Status != workflow.ScenarioStatusPassing && s.Status != workflow.ScenarioStatusSkipped {
			return false
		}
	}
	return true
}

// filterReadyRequirements applies requirement-DAG gating and returns
// requirements that are ready for execution:
//
//  1. All DependsOn requirements of that requirement are complete
//     (every scenario passing or skipped).
//  2. The requirement has at least one pending/dirty scenario (not already complete).
//
// Requirements without DependsOn (root requirements) are always unblocked by
// upstream; they are dispatched as long as they have pending scenarios.
//
// Parameters:
//   - requirements: all requirements for the plan.
//   - allScenarios: all scenarios for the plan (used to compute completion).
//
// Returns the subset of requirements that should be dispatched.
func filterReadyRequirements(
	requirements []workflow.Requirement,
	allScenarios []workflow.Scenario,
) []workflow.Requirement {
	if len(requirements) == 0 {
		return nil
	}

	// Group all scenarios by their RequirementID.
	reqScenarios := make(map[string][]workflow.Scenario, len(requirements))
	for _, s := range allScenarios {
		reqScenarios[s.RequirementID] = append(reqScenarios[s.RequirementID], s)
	}

	// Pre-compute which requirements are fully complete.
	complete := make(map[string]bool, len(requirements))
	for _, r := range requirements {
		complete[r.ID] = requirementComplete(r.ID, reqScenarios)
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
		// Has pending scenarios and all deps satisfied.
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
