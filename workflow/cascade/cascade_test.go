package cascade

import (
	"context"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestChangeProposal_NilProposal verifies that a nil proposal returns an error
// immediately, before any KV access.
func TestChangeProposal_NilProposal(t *testing.T) {
	_, err := ChangeProposal(context.Background(), nil, "test", nil)
	if err == nil {
		t.Fatal("expected error for nil proposal")
	}
}

// TestChangeProposal_NoAffectedRequirements verifies that an empty AffectedReqIDs
// slice results in an empty cascade result without touching the KV store.
func TestChangeProposal_NoAffectedRequirements(t *testing.T) {
	// Empty AffectedReqIDs causes early return before any KV access — no KV needed.
	slug := "cascade-test"

	proposal := &workflow.ChangeProposal{
		ID:             "cp-1",
		AffectedReqIDs: []string{}, // empty — returns before LoadScenarios
	}

	result, err := ChangeProposal(context.Background(), nil, slug, proposal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.AffectedScenarioIDs) != 0 {
		t.Errorf("AffectedScenarioIDs = %d, want 0", len(result.AffectedScenarioIDs))
	}
}
