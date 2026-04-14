package cascade

import (
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestChangeProposal_NilProposal verifies that a nil proposal returns an error
// immediately, before any scenario filtering.
func TestChangeProposal_NilProposal(t *testing.T) {
	_, err := ChangeProposal(nil, nil)
	if err == nil {
		t.Fatal("expected error for nil proposal")
	}
}

// TestChangeProposal_NoAffectedRequirements verifies that an empty AffectedReqIDs
// slice results in an empty cascade result without examining scenarios.
func TestChangeProposal_NoAffectedRequirements(t *testing.T) {
	proposal := &workflow.ChangeProposal{
		ID:             "cp-1",
		AffectedReqIDs: []string{}, // empty — returns before filtering scenarios
	}

	result, err := ChangeProposal(proposal, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.AffectedScenarioIDs) != 0 {
		t.Errorf("AffectedScenarioIDs = %d, want 0", len(result.AffectedScenarioIDs))
	}
}
