// Package cascade implements the dirty-cascade logic applied when a
// ChangeProposal is accepted, marking affected scenarios for re-execution.
package cascade

import (
	"context"
	"fmt"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
)

// Result summarizes the effect of accepting a ChangeProposal.
type Result struct {
	AffectedRequirementIDs []string
	AffectedScenarioIDs    []string
}

// ChangeProposal executes the dirty cascade when a ChangeProposal is accepted.
//
// Steps:
//  1. Load all scenarios for the plan; filter to those whose RequirementID is in proposal.AffectedReqIDs.
//  2. Return a Result describing what changed.
//
// The function is deliberately free of NATS or reactive-engine dependencies so it can be called
// directly from HTTP handlers and tested without infrastructure.
func ChangeProposal(ctx context.Context, tw *graphutil.TripleWriter, slug string, proposal *workflow.ChangeProposal) (*Result, error) {
	if proposal == nil {
		return nil, fmt.Errorf("proposal is nil")
	}

	result := &Result{
		AffectedRequirementIDs: make([]string, 0, len(proposal.AffectedReqIDs)),
		AffectedScenarioIDs:    make([]string, 0),
	}

	// Copy affected requirement IDs into result and build a lookup set
	// using hashed IDs so they match the hashed RequirementID returned by
	// LoadScenarios (entity IDs are hashed in the triple store).
	affectedReqs := make(map[string]bool, len(proposal.AffectedReqIDs))
	for _, id := range proposal.AffectedReqIDs {
		hashed := workflow.HashInstanceID(id)
		affectedReqs[hashed] = true
		result.AffectedRequirementIDs = append(result.AffectedRequirementIDs, id)
	}

	if len(affectedReqs) == 0 {
		// No requirements affected — nothing to cascade.
		return result, nil
	}

	// Step 1: Find scenarios belonging to affected requirements.
	allScenarios, err := workflow.LoadScenarios(ctx, tw, slug)
	if err != nil {
		return nil, fmt.Errorf("load scenarios: %w", err)
	}

	for _, sc := range allScenarios {
		if affectedReqs[sc.RequirementID] {
			result.AffectedScenarioIDs = append(result.AffectedScenarioIDs, sc.ID)
		}
	}

	return result, nil
}
