// Package cascade implements the dirty-cascade logic applied when a
// ChangeProposal is accepted, marking affected scenarios for re-execution.
package cascade

import (
	"fmt"

	"github.com/c360studio/semspec/workflow"
)

// Result summarizes the effect of accepting a ChangeProposal.
type Result struct {
	AffectedRequirementIDs []string
	AffectedScenarioIDs    []string
}

// ChangeProposal executes the dirty cascade when a ChangeProposal is accepted.
//
// Steps:
//  1. Filter scenarios to those whose RequirementID is in proposal.AffectedReqIDs.
//  2. Return a Result describing what changed.
//
// The function is pure business logic — it performs no I/O and can be tested
// without any infrastructure. The caller is responsible for loading the plan
// from KV and passing the current scenario list.
func ChangeProposal(proposal *workflow.ChangeProposal, scenarios []workflow.Scenario) (*Result, error) {
	if proposal == nil {
		return nil, fmt.Errorf("proposal is nil")
	}

	result := &Result{
		AffectedRequirementIDs: make([]string, 0, len(proposal.AffectedReqIDs)),
		AffectedScenarioIDs:    make([]string, 0),
	}

	// Copy affected requirement IDs into result and build a lookup set.
	// Scenario RequirementIDs in the KV plan match the raw (non-hashed) IDs stored
	// in the plan's Scenarios slice, so no hashing is needed here.
	affectedReqs := make(map[string]bool, len(proposal.AffectedReqIDs))
	for _, id := range proposal.AffectedReqIDs {
		affectedReqs[id] = true
		result.AffectedRequirementIDs = append(result.AffectedRequirementIDs, id)
	}

	if len(affectedReqs) == 0 {
		// No requirements affected — nothing to cascade.
		return result, nil
	}

	// Find scenarios belonging to affected requirements.
	for _, sc := range scenarios {
		if affectedReqs[sc.RequirementID] {
			result.AffectedScenarioIDs = append(result.AffectedScenarioIDs, sc.ID)
		}
	}

	return result, nil
}
