package planmanager

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestRefreshApprovedReviewMetadata pins the ADR-051 Slice 5 helper: advancing
// past a review gate must overwrite a stale rejection verdict, clear stale
// findings, and restamp reviewed_at.
func TestRefreshApprovedReviewMetadata(t *testing.T) {
	t.Run("clears stale rejection and stamps approved", func(t *testing.T) {
		plan := &workflow.Plan{
			ReviewVerdict:           "needs_changes",
			ReviewSummary:           "old rejection",
			ReviewFindings:          json.RawMessage(`[{"severity":"error"}]`),
			ReviewFormattedFindings: "Action: fix something",
		}
		refreshApprovedReviewMetadata(plan, "scenarios approved")

		if plan.ReviewVerdict != "approved" {
			t.Errorf("ReviewVerdict = %q, want approved", plan.ReviewVerdict)
		}
		if plan.ReviewSummary != "scenarios approved" {
			t.Errorf("ReviewSummary = %q, want the new summary", plan.ReviewSummary)
		}
		if plan.ReviewFindings != nil {
			t.Error("ReviewFindings should be cleared on advance")
		}
		if plan.ReviewFormattedFindings != "" {
			t.Error("ReviewFormattedFindings should be cleared on advance")
		}
		if plan.ReviewedAt == nil {
			t.Error("ReviewedAt should be restamped")
		}
	})

	t.Run("empty summary preserves the existing one", func(t *testing.T) {
		plan := &workflow.Plan{ReviewVerdict: "needs_changes", ReviewSummary: "prior approving summary"}
		refreshApprovedReviewMetadata(plan, "")
		if plan.ReviewVerdict != "approved" {
			t.Errorf("ReviewVerdict = %q, want approved", plan.ReviewVerdict)
		}
		if plan.ReviewSummary != "prior approving summary" {
			t.Errorf("ReviewSummary = %q, want preserved", plan.ReviewSummary)
		}
	})
}

// TestHandleScenariosReviewedMutation_ClearsStaleMetadata is the regression for
// the mavlink-run bug: a plan frozen at an earlier round's needs_changes verdict
// after it advanced. Advancing to scenarios_reviewed must restamp approved.
func TestHandleScenariosReviewedMutation_ClearsStaleMetadata(t *testing.T) {
	ctx := context.Background()
	c := setupTestComponent(t)
	plan := setupTestPlan(t, c, "stale-meta")
	plan.Status = workflow.StatusReviewingScenarios
	plan.ReviewVerdict = "needs_changes"
	plan.ReviewFindings = json.RawMessage(`[{"severity":"error","issue":"stale"}]`)
	plan.ReviewFormattedFindings = "stale finding"
	if err := c.plans.save(ctx, plan); err != nil {
		t.Fatalf("save: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"slug": "stale-meta", "summary": "all scenarios sound"})
	if resp := c.handleScenariosReviewedMutation(ctx, body); !resp.Success {
		t.Fatalf("mutation failed: %s", resp.Error)
	}

	got, _ := c.plans.get("stale-meta")
	if got.ReviewVerdict != "approved" {
		t.Errorf("ReviewVerdict = %q, want approved (stale needs_changes must not persist)", got.ReviewVerdict)
	}
	if got.ReviewFindings != nil || got.ReviewFormattedFindings != "" {
		t.Error("stale findings must be cleared on advance")
	}
	if got.ReviewedAt == nil {
		t.Error("ReviewedAt must be restamped on advance")
	}
}
