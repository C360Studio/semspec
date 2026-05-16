package planmanager

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestHandleQAVerdictMutation_PersistsVerdictSummary covers the needs_changes
// path because (a) it doesn't depend on the sandbox required by the approved
// branch's assembleRequirementBranches step and (b) the persistence logic
// runs identically in both branches — the summary is set after the verdict
// switch and before ps.save.
func TestHandleQAVerdictMutation_PersistsVerdictSummary(t *testing.T) {
	ctx := context.Background()
	c := setupTestComponent(t)

	slug := "verdict-persist"
	plan := setupTestPlan(t, c, slug)
	plan.Status = workflow.StatusReviewingQA
	if err := c.plans.save(ctx, plan); err != nil {
		t.Fatalf("save plan: %v", err)
	}

	event := workflow.QAVerdictEvent{
		Slug:    slug,
		Level:   workflow.QALevelIntegration,
		Verdict: workflow.QAVerdictNeedsChanges,
		Summary: "Integration tier surfaced a coverage gap.",
		Dimensions: workflow.QAVerdictDimensions{
			RequirementFulfillment: "All four requirements implemented.",
			Coverage:               "Missing assertions for the 5xx path.",
		},
	}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	resp := c.handleQAVerdictMutation(ctx, data)
	if !resp.Success {
		t.Fatalf("mutation failed: %s", resp.Error)
	}

	stored, ok := c.plans.get(slug)
	if !ok {
		t.Fatal("plan missing from store after mutation")
	}
	if stored.QAVerdictSummary == nil {
		t.Fatal("Plan.QAVerdictSummary was not persisted")
	}
	if stored.QAVerdictSummary.Verdict != workflow.QAVerdictNeedsChanges {
		t.Errorf("Verdict = %q, want needs_changes", stored.QAVerdictSummary.Verdict)
	}
	if stored.QAVerdictSummary.Level != workflow.QALevelIntegration {
		t.Errorf("Level = %q, want integration", stored.QAVerdictSummary.Level)
	}
	if stored.QAVerdictSummary.Summary != "Integration tier surfaced a coverage gap." {
		t.Errorf("Summary = %q, want full summary", stored.QAVerdictSummary.Summary)
	}
	if stored.QAVerdictSummary.Dimensions.Coverage != "Missing assertions for the 5xx path." {
		t.Errorf("Dimensions.Coverage = %q, want full coverage text",
			stored.QAVerdictSummary.Dimensions.Coverage)
	}
	if stored.QAVerdictSummary.RecordedAt.IsZero() {
		t.Error("RecordedAt should be set by plan-manager (zero indicates field not populated)")
	}
}
