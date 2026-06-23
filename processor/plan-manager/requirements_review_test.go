package planmanager

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestHandleRequirementsReviewedMutation pins the ADR-051 Slice 4 machine gate:
// reviewing_requirements → requirements_reviewed always advances (no auto_approve
// fork) and refreshes the operative approved verdict (Slice 5).
func TestHandleRequirementsReviewedMutation(t *testing.T) {
	ctx := context.Background()

	t.Run("advances reviewing_requirements to requirements_reviewed", func(t *testing.T) {
		c := setupTestComponent(t)
		plan := setupTestPlan(t, c, "req-ok")
		plan.Status = workflow.StatusReviewingRequirements
		plan.ReviewVerdict = "needs_changes"
		plan.ReviewFindings = json.RawMessage(`[{"severity":"error"}]`)
		if err := c.plans.save(ctx, plan); err != nil {
			t.Fatalf("save: %v", err)
		}

		body, _ := json.Marshal(map[string]string{"slug": "req-ok", "summary": "requirements are complete"})
		resp := c.handleRequirementsReviewedMutation(ctx, body)
		if !resp.Success {
			t.Fatalf("mutation failed: %s", resp.Error)
		}

		got, _ := c.plans.get("req-ok")
		if got.Status != workflow.StatusRequirementsReviewed {
			t.Errorf("status = %s, want requirements_reviewed", got.Status)
		}
		if got.ReviewVerdict != "approved" {
			t.Errorf("ReviewVerdict = %q, want approved (Slice 5 refresh)", got.ReviewVerdict)
		}
		if got.ReviewFindings != nil {
			t.Error("stale findings must be cleared on advance (Slice 5)")
		}
	})

	t.Run("rejects when plan is not in reviewing_requirements", func(t *testing.T) {
		c := setupTestComponent(t)
		plan := setupTestPlan(t, c, "req-wrong-state")
		plan.Status = workflow.StatusRequirementsGenerated // never claimed for review
		if err := c.plans.save(ctx, plan); err != nil {
			t.Fatalf("save: %v", err)
		}
		body, _ := json.Marshal(map[string]string{"slug": "req-wrong-state"})
		resp := c.handleRequirementsReviewedMutation(ctx, body)
		if resp.Success {
			t.Fatal("mutation should fail from requirements_generated (not a reviewing state)")
		}
		if !strings.Contains(resp.Error, "invalid transition") {
			t.Errorf("error = %q, want invalid transition", resp.Error)
		}
	})
}

// TestHandleRevisionMutation_RequirementsRound pins the round-4 reject path: a
// rejected requirements review re-runs the requirement-generator only —
// reviewing_requirements → approved with Requirements cleared.
func TestHandleRevisionMutation_RequirementsRound(t *testing.T) {
	ctx := context.Background()

	t.Run("round 4 reject re-enters approved and clears requirements", func(t *testing.T) {
		c := setupRevisionComponent(t, 3)
		plan := setupTestPlan(t, c, "req-reject")
		plan.Status = workflow.StatusReviewingRequirements
		plan.Requirements = []workflow.Requirement{{ID: "req.demo.1", Title: "R"}}
		plan.ReviewIteration = 0
		if err := c.plans.save(ctx, plan); err != nil {
			t.Fatalf("save: %v", err)
		}

		findings := makeFindings(t, []workflow.PlanReviewFinding{
			{Severity: "error", Status: "violation", Phase: "requirements", SOPID: "completeness.goal"},
		})
		data := marshalRevision(t, RevisionMutationRequest{
			Slug:     "req-reject",
			Round:    4,
			Verdict:  "needs_changes",
			Summary:  "over-bundled",
			Findings: findings,
		})

		resp := c.handleRevisionMutation(ctx, data)
		if !resp.Success {
			t.Fatalf("revision mutation failed: %s", resp.Error)
		}

		got, _ := c.plans.get("req-reject")
		if got.Status != workflow.StatusApproved {
			t.Errorf("status = %s, want approved (re-run req-gen)", got.Status)
		}
		if got.Requirements != nil {
			t.Error("Requirements should be cleared so the generator regenerates")
		}
		if got.ReviewIteration != 1 {
			t.Errorf("ReviewIteration = %d, want 1", got.ReviewIteration)
		}
	})

	t.Run("round 4 requires reviewing_requirements status", func(t *testing.T) {
		c := setupRevisionComponent(t, 3)
		plan := setupTestPlan(t, c, "req-reject-wrong-state")
		plan.Status = workflow.StatusReviewingArchitecture // wrong reviewing state for round 4
		if err := c.plans.save(ctx, plan); err != nil {
			t.Fatalf("save: %v", err)
		}
		data := marshalRevision(t, RevisionMutationRequest{
			Slug:     "req-reject-wrong-state",
			Round:    4,
			Verdict:  "needs_changes",
			Summary:  "x",
			Findings: makeFindings(t, []workflow.PlanReviewFinding{{Severity: "error", Status: "violation", Phase: "requirements", SOPID: "x"}}),
		})
		resp := c.handleRevisionMutation(ctx, data)
		if resp.Success {
			t.Fatal("round 4 revision should fail when the plan is not in reviewing_requirements")
		}
		if !strings.Contains(resp.Error, "reviewing_requirements") {
			t.Errorf("error = %q, want it to name the required reviewing_requirements status", resp.Error)
		}
	})
}

// TestReenterRequirementsPhase pins the shared rollback helper used by both the
// R2 requirements cascade and the round-4 requirements-review reject.
func TestReenterRequirementsPhase(t *testing.T) {
	plan := &workflow.Plan{
		Slug:         "demo",
		Requirements: []workflow.Requirement{{ID: "req.demo.1"}},
		Architecture: &workflow.ArchitectureDocument{Decisions: []workflow.ArchDecision{{Title: "x"}}},
		Stories:      []workflow.Story{{ID: "story.demo.1.1"}},
		Scenarios:    []workflow.Scenario{{ID: "scen.demo.1"}},
	}
	got := reenterRequirementsPhase(plan)
	if got != workflow.StatusApproved {
		t.Errorf("status = %s, want approved", got)
	}
	if plan.Requirements != nil || plan.Architecture != nil || plan.Stories != nil || plan.Scenarios != nil {
		t.Error("Requirements and all downstream artifacts should be cleared")
	}
}
