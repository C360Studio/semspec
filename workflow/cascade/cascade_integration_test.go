//go:build integration

package cascade

import (
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// The scenarios used across all integration test cases.
var integrationScenarios = []workflow.Scenario{
	{ID: "sc-1", RequirementID: "req-1"},
	{ID: "sc-2", RequirementID: "req-1"},
	{ID: "sc-3", RequirementID: "req-2"},
}

func TestChangeProposal_AffectsOneRequirement(t *testing.T) {
	proposal := &workflow.ChangeProposal{
		ID:             "cp-1",
		AffectedReqIDs: []string{"req-1"}, // affects sc-1, sc-2
	}

	result, err := ChangeProposal(proposal, integrationScenarios)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.AffectedRequirementIDs) != 1 {
		t.Errorf("AffectedRequirementIDs = %d, want 1", len(result.AffectedRequirementIDs))
	}
	if len(result.AffectedScenarioIDs) != 2 {
		t.Errorf("AffectedScenarioIDs = %d, want 2 (sc-1, sc-2)", len(result.AffectedScenarioIDs))
	}
}

func TestChangeProposal_AffectsAllRequirements(t *testing.T) {
	proposal := &workflow.ChangeProposal{
		ID:             "cp-1",
		AffectedReqIDs: []string{"req-1", "req-2"},
	}

	result, err := ChangeProposal(proposal, integrationScenarios)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.AffectedScenarioIDs) != 3 {
		t.Errorf("AffectedScenarioIDs = %d, want 3", len(result.AffectedScenarioIDs))
	}
}

func TestChangeProposal_NoMatchingScenarios(t *testing.T) {
	proposal := &workflow.ChangeProposal{
		ID:             "cp-1",
		AffectedReqIDs: []string{"req-nonexistent"},
	}

	result, err := ChangeProposal(proposal, integrationScenarios)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.AffectedScenarioIDs) != 0 {
		t.Errorf("AffectedScenarioIDs = %d, want 0", len(result.AffectedScenarioIDs))
	}
}
