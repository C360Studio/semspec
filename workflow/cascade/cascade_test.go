package cascade

import (
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestPlanDecision_NilProposal verifies that a nil proposal returns an error
// immediately, before any scenario filtering.
func TestPlanDecision_NilProposal(t *testing.T) {
	_, err := PlanDecision(nil, nil)
	if err == nil {
		t.Fatal("expected error for nil proposal")
	}
}

// TestPlanDecision_NoAffectedRequirements verifies that an empty AffectedReqIDs
// slice results in an empty cascade result without examining scenarios.
func TestPlanDecision_NoAffectedRequirements(t *testing.T) {
	proposal := &workflow.PlanDecision{
		ID:             "cp-1",
		AffectedReqIDs: []string{}, // empty — returns before filtering scenarios
	}

	result, err := PlanDecision(proposal, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.AffectedScenarioIDs) != 0 {
		t.Errorf("AffectedScenarioIDs = %d, want 0", len(result.AffectedScenarioIDs))
	}
}
