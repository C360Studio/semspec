package planmanager

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestHandleArchitectureReviewedMutation pins the ADR-051 Slice 3 machine gate:
// reviewing_architecture → architecture_reviewed always advances (no
// auto_approve fork) and the operative approved verdict overwrites any stale
// rejection metadata from a prior architecture revision.
func TestHandleArchitectureReviewedMutation(t *testing.T) {
	ctx := context.Background()

	t.Run("advances reviewing_architecture to architecture_reviewed", func(t *testing.T) {
		c := setupTestComponent(t)
		plan := setupTestPlan(t, c, "arch-ok")
		plan.Status = workflow.StatusReviewingArchitecture
		// Stale rejection metadata from a prior architecture revision round.
		plan.ReviewVerdict = "needs_changes"
		plan.ReviewSummary = "old rejection"
		if err := c.plans.save(ctx, plan); err != nil {
			t.Fatalf("save: %v", err)
		}

		body, _ := json.Marshal(map[string]string{"slug": "arch-ok", "summary": "architecture is sound"})
		resp := c.handleArchitectureReviewedMutation(ctx, body)
		if !resp.Success {
			t.Fatalf("mutation failed: %s", resp.Error)
		}

		got, ok := c.plans.get("arch-ok")
		if !ok {
			t.Fatal("plan not found after mutation")
		}
		if got.Status != workflow.StatusArchitectureReviewed {
			t.Errorf("status = %s, want architecture_reviewed", got.Status)
		}
		if got.ReviewVerdict != "approved" {
			t.Errorf("ReviewVerdict = %q, want approved (stale rejection must be overwritten)", got.ReviewVerdict)
		}
		if got.ReviewSummary != "architecture is sound" {
			t.Errorf("ReviewSummary = %q, want the approval summary", got.ReviewSummary)
		}
	})

	t.Run("rejects when plan is not in reviewing_architecture", func(t *testing.T) {
		c := setupTestComponent(t)
		plan := setupTestPlan(t, c, "arch-wrong-state")
		plan.Status = workflow.StatusArchitectureGenerated // never claimed for review
		if err := c.plans.save(ctx, plan); err != nil {
			t.Fatalf("save: %v", err)
		}

		body, _ := json.Marshal(map[string]string{"slug": "arch-wrong-state"})
		resp := c.handleArchitectureReviewedMutation(ctx, body)
		if resp.Success {
			t.Fatal("mutation should fail from architecture_generated (not a reviewing state)")
		}
		if !strings.Contains(resp.Error, "invalid transition") {
			t.Errorf("error = %q, want invalid transition", resp.Error)
		}
	})

	t.Run("rejects missing slug", func(t *testing.T) {
		c := setupTestComponent(t)
		resp := c.handleArchitectureReviewedMutation(ctx, []byte(`{}`))
		if resp.Success {
			t.Fatal("mutation should fail with empty slug")
		}
	})
}

// TestHandleRevisionMutation_ArchitectureRound pins the round-3 reject path:
// a rejected architecture review re-runs the architect only —
// reviewing_architecture → requirements_generated with the prior architecture
// captured for revision and Architecture cleared so the generator regenerates.
func TestHandleRevisionMutation_ArchitectureRound(t *testing.T) {
	ctx := context.Background()

	t.Run("round 3 reject re-enters requirements_generated and rolls back architecture", func(t *testing.T) {
		c := setupRevisionComponent(t, 3)
		plan := setupTestPlan(t, c, "arch-reject")
		plan.Status = workflow.StatusReviewingArchitecture
		plan.Architecture = &workflow.ArchitectureDocument{Decisions: []workflow.ArchDecision{{Title: "use Go"}}}
		plan.ReviewIteration = 0
		if err := c.plans.save(ctx, plan); err != nil {
			t.Fatalf("save: %v", err)
		}

		findings := makeFindings(t, []workflow.PlanReviewFinding{
			{Severity: "error", Status: "violation", Phase: "architecture", SOPID: "architecture.component_missing_implementation_files"},
		})
		data := marshalRevision(t, RevisionMutationRequest{
			Slug:     "arch-reject",
			Round:    3,
			Verdict:  "needs_changes",
			Summary:  "facade risk",
			Findings: findings,
		})

		resp := c.handleRevisionMutation(ctx, data)
		if !resp.Success {
			t.Fatalf("revision mutation failed: %s", resp.Error)
		}

		got, _ := c.plans.get("arch-reject")
		if got.Status != workflow.StatusRequirementsGenerated {
			t.Errorf("status = %s, want requirements_generated (re-run the architect)", got.Status)
		}
		if got.Architecture != nil {
			t.Error("Architecture should be cleared so the generator regenerates")
		}
		if !strings.Contains(got.PreviousArchitectureJSON, "use Go") {
			t.Errorf("PreviousArchitectureJSON should capture the prior architecture, got %q", got.PreviousArchitectureJSON)
		}
		if got.ReviewIteration != 1 {
			t.Errorf("ReviewIteration = %d, want 1", got.ReviewIteration)
		}
	})

	t.Run("round 3 requires reviewing_architecture status", func(t *testing.T) {
		c := setupRevisionComponent(t, 3)
		plan := setupTestPlan(t, c, "arch-reject-wrong-state")
		plan.Status = workflow.StatusReviewingScenarios // wrong reviewing state for round 3
		if err := c.plans.save(ctx, plan); err != nil {
			t.Fatalf("save: %v", err)
		}

		data := marshalRevision(t, RevisionMutationRequest{
			Slug:     "arch-reject-wrong-state",
			Round:    3,
			Verdict:  "needs_changes",
			Summary:  "x",
			Findings: makeFindings(t, []workflow.PlanReviewFinding{{Severity: "error", Status: "violation", Phase: "architecture", SOPID: "x"}}),
		})
		resp := c.handleRevisionMutation(ctx, data)
		if resp.Success {
			t.Fatal("round 3 revision should fail when the plan is not in reviewing_architecture")
		}
		if !strings.Contains(resp.Error, "reviewing_architecture") {
			t.Errorf("error = %q, want it to name the required reviewing_architecture status", resp.Error)
		}
	})
}

// TestReenterArchitecturePhase pins the shared rollback helper used by both the
// R2 architecture cascade and the round-3 architecture-review reject.
func TestReenterArchitecturePhase(t *testing.T) {
	t.Run("captures prior architecture and clears downstream", func(t *testing.T) {
		plan := &workflow.Plan{
			Slug:         "demo",
			Architecture: &workflow.ArchitectureDocument{Decisions: []workflow.ArchDecision{{Title: "use Go"}}},
			Stories:      []workflow.Story{{ID: "story.demo.1.1"}},
			Scenarios:    []workflow.Scenario{{ID: "scen.demo.1"}},
		}
		got := reenterArchitecturePhase(plan)
		if got != workflow.StatusRequirementsGenerated {
			t.Errorf("status = %s, want requirements_generated", got)
		}
		if plan.Architecture != nil {
			t.Error("Architecture should be cleared")
		}
		if !strings.Contains(plan.PreviousArchitectureJSON, "use Go") {
			t.Errorf("PreviousArchitectureJSON = %q, want captured prior architecture", plan.PreviousArchitectureJSON)
		}
		if plan.Stories != nil || plan.Scenarios != nil {
			t.Error("Stories and Scenarios should be cleared")
		}
	})

	t.Run("nil architecture clears stale carry-over", func(t *testing.T) {
		plan := &workflow.Plan{
			Slug:                     "demo",
			PreviousArchitectureJSON: `{"stale":"x"}`,
		}
		reenterArchitecturePhase(plan)
		if plan.PreviousArchitectureJSON != "" {
			t.Errorf("stale PreviousArchitectureJSON survived nil-architecture rollback: %q", plan.PreviousArchitectureJSON)
		}
	})
}
