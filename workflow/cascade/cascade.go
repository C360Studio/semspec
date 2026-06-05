// Package cascade implements the dirty-cascade logic applied when a
// PlanDecision is accepted, marking affected scenarios + stories for
// re-execution / re-prep.
package cascade

import (
	"fmt"

	"github.com/c360studio/semspec/workflow"
)

// Result summarizes the effect of accepting a PlanDecision.
type Result struct {
	// Kind echoes the accepted proposal's kind so downstream consumers of the
	// PlanDecisionAcceptedEvent can branch on it without re-loading the plan.
	// Load-bearing for architecture_revise: the requirement-executor abandons
	// (rather than resumes) in-flight execs for the slug when it sees this kind,
	// since the plan is restarting from the architect — resuming the wedged exec
	// would race the re-run.
	Kind workflow.PlanDecisionKind

	AffectedRequirementIDs []string
	// AffectedStoryIDs lists Story IDs the cascade dirty-marked. Populated
	// for Kind=story_reprepare (the cascade target is Stories + Scenarios)
	// or, in back-compat mode, when the proposal's AffectedReqIDs match
	// Stories under those Requirements. Empty for requirement_change /
	// execution_exhausted cascades that don't touch Sarah's layer.
	AffectedStoryIDs    []string
	AffectedScenarioIDs []string
}

// PlanDecision executes the dirty cascade when a PlanDecision is accepted.
//
// Cascade shape branches on proposal.Kind:
//
//   - Kind=story_reprepare: dirty-mark the Stories named in
//     proposal.AffectedStoryIDs (Train C — Story-level granularity),
//     PLUS every scenario whose StoryID matches. If AffectedStoryIDs is
//     empty, falls back to "all Stories under AffectedReqIDs" so the
//     cascade still reaches Sarah's layer.
//   - Kind=requirement_change (or unset for back-compat): the existing
//     scenarios-only cascade — filter scenarios to those whose
//     RequirementID is in proposal.AffectedReqIDs. Stories untouched.
//   - Kind=execution_exhausted: terminal acknowledgement; cascade is a
//     no-op (callers shouldn't invoke PlanDecision for this kind, but
//     the function returns empty Result safely if they do).
//   - Kind=architecture_revise: no-op. The plan-manager accept handler
//     already wiped Architecture + Stories + Scenarios and reset all
//     requirement executions inline; the re-run regenerates everything
//     from the architect down, so there is nothing to dirty-mark.
//
// Pure business logic — no I/O. Caller loads the plan from KV and passes
// stories + scenarios. Returns the IDs that were dirty-marked so
// downstream consumers (plan-manager status transition, executor reset)
// can scope their actions.
func PlanDecision(proposal *workflow.PlanDecision, stories []workflow.Story, scenarios []workflow.Scenario) (*Result, error) {
	if proposal == nil {
		return nil, fmt.Errorf("proposal is nil")
	}

	result := &Result{
		Kind:                   proposal.Kind,
		AffectedRequirementIDs: make([]string, 0, len(proposal.AffectedReqIDs)),
		AffectedStoryIDs:       make([]string, 0),
		AffectedScenarioIDs:    make([]string, 0),
	}

	// Always record the Requirement IDs the proposal targets — they're
	// the entry-point context for downstream consumers regardless of Kind.
	affectedReqs := make(map[string]bool, len(proposal.AffectedReqIDs))
	for _, id := range proposal.AffectedReqIDs {
		affectedReqs[id] = true
		result.AffectedRequirementIDs = append(result.AffectedRequirementIDs, id)
	}

	switch proposal.Kind {
	case workflow.PlanDecisionKindStoryReprepare:
		affectedStories := storiesForReprepare(proposal, stories, affectedReqs)
		result.AffectedStoryIDs = append(result.AffectedStoryIDs, affectedStories...)

		affectedStorySet := make(map[string]bool, len(affectedStories))
		for _, id := range affectedStories {
			affectedStorySet[id] = true
		}
		// Scenarios are dirty when their parent Story is dirty. Falls back
		// to "scenarios for affected reqs" when no Story IDs were resolved
		// (e.g. legacy plan with empty plan.Stories but Kind=story_reprepare
		// — should be rare; preserves a useful cascade rather than no-op).
		for _, sc := range scenarios {
			switch {
			case len(affectedStorySet) > 0 && affectedStorySet[sc.StoryID]:
				result.AffectedScenarioIDs = append(result.AffectedScenarioIDs, sc.ID)
			case len(affectedStorySet) == 0 && affectedReqs[sc.RequirementID]:
				result.AffectedScenarioIDs = append(result.AffectedScenarioIDs, sc.ID)
			}
		}

	case workflow.PlanDecisionKindExecutionExhausted:
		// Terminal kind — cascade is a no-op beyond recording the
		// AffectedRequirementIDs already populated above for caller
		// telemetry. Stories + Scenarios stay empty.

	case workflow.PlanDecisionKindArchitectureRevise:
		// The plan-manager accept handler already wiped Architecture +
		// Stories + Scenarios and reset all requirement executions inline,
		// then drove implementing → requirements_generated. There is nothing
		// left for the dirty-cascade to mark — the re-run regenerates every
		// entity from the architect down. No-op beyond the recorded
		// AffectedRequirementIDs (caller telemetry).

	default:
		// Kind=requirement_change OR unset (back-compat with pre-Kind records).
		// Scenarios-only cascade matching pre-Train-C behavior.
		if len(affectedReqs) == 0 {
			return result, nil
		}
		for _, sc := range scenarios {
			if affectedReqs[sc.RequirementID] {
				result.AffectedScenarioIDs = append(result.AffectedScenarioIDs, sc.ID)
			}
		}
	}

	return result, nil
}

// storiesForReprepare resolves the Story IDs the cascade should dirty-mark
// for a story_reprepare proposal. Three input shapes:
//
//   - proposal.AffectedStoryIDs populated → use those directly (the
//     recovery-agent threaded them through from the wedged exec's cursor).
//   - proposal.AffectedStoryIDs empty AND AffectedReqIDs populated → walk
//     plan.Stories and select every Story that covers any affected requirement
//     (checks RequirementIDs M:N slice). Whole-Requirement re-prep — coarser
//     but still reaches Sarah.
//   - Both empty → return nil. Caller's downstream behavior is "no-op
//     cascade"; plan-manager treats that as human-review territory.
func storiesForReprepare(proposal *workflow.PlanDecision, stories []workflow.Story, affectedReqs map[string]bool) []string {
	if len(proposal.AffectedStoryIDs) > 0 {
		out := make([]string, 0, len(proposal.AffectedStoryIDs))
		out = append(out, proposal.AffectedStoryIDs...)
		return out
	}
	if len(affectedReqs) == 0 {
		return nil
	}
	out := make([]string, 0, len(stories))
	for _, s := range stories {
		for _, rid := range s.RequirementIDs {
			if affectedReqs[rid] {
				out = append(out, s.ID)
				break
			}
		}
	}
	return out
}
