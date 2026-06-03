package planmanager

import (
	"context"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestHandleStoryStatusMutation_ReservationPattern pins the ADR-044 M:N
// reservation contract enforced by plan-manager's handleStoryStatusMutation.
// First-arrival wins (ready→executing succeeds); second-arrival loses
// (executing→executing rejected as invalid transition) and falls through
// the executor's dedup branch to skip dispatch.
func TestHandleStoryStatusMutation_ReservationPattern(t *testing.T) {
	ctx := context.Background()
	c := setupTestComponent(t)

	slug := "reservation"
	plan := setupTestPlan(t, c, slug)
	plan.Stories = []workflow.Story{
		{
			ID:             "story.reservation.1.1",
			ComponentName:  "shared",
			RequirementIDs: []string{"req.reservation.1", "req.reservation.2"},
			Title:          "Cohesive Story under M:N",
			Status:         workflow.StoryStatusReady,
		},
	}
	_ = c.plans.save(ctx, plan)

	// First executor wins the reservation.
	resp1 := c.handleStoryStatusMutation(ctx, marshalJSON(t, StoryStatusMutationRequest{
		Slug: slug, StoryID: "story.reservation.1.1", Target: workflow.StoryStatusExecuting,
	}))
	if !resp1.Success {
		t.Fatalf("first claim should succeed: %v", resp1.Error)
	}

	// Second executor loses — executing→executing is not a valid transition.
	resp2 := c.handleStoryStatusMutation(ctx, marshalJSON(t, StoryStatusMutationRequest{
		Slug: slug, StoryID: "story.reservation.1.1", Target: workflow.StoryStatusExecuting,
	}))
	if resp2.Success {
		t.Errorf("second claim should fail (executing→executing is invalid), got success")
	}
	if resp2.Error == "" {
		t.Error("rejection error message empty")
	}

	// First executor advances to complete.
	resp3 := c.handleStoryStatusMutation(ctx, marshalJSON(t, StoryStatusMutationRequest{
		Slug: slug, StoryID: "story.reservation.1.1", Target: workflow.StoryStatusComplete,
	}))
	if !resp3.Success {
		t.Errorf("executing→complete should succeed: %v", resp3.Error)
	}

	got, _ := c.plans.get(slug)
	if len(got.Stories) != 1 || got.Stories[0].Status != workflow.StoryStatusComplete {
		t.Errorf("Story.Status = %q, want complete", got.Stories[0].Status)
	}
	if got.Stories[0].UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be bumped on transition")
	}
}

// TestHandleStoryStatusMutation_EmptyStatusTreatedAsReady pins the Sarah
// omitempty emission shape: a freshly-emitted Story has Status="" which
// the handler treats as Ready (Sarah only emits Stories that passed her
// readiness gate). Without this, the first executor's claim would fail
// because "" → Executing is not in the CanTransitionTo table.
func TestHandleStoryStatusMutation_EmptyStatusTreatedAsReady(t *testing.T) {
	ctx := context.Background()
	c := setupTestComponent(t)

	slug := "empty-status"
	plan := setupTestPlan(t, c, slug)
	plan.Stories = []workflow.Story{
		{ID: "story.empty-status.1", ComponentName: "x",
			RequirementIDs: []string{"req.empty-status.1"}, Title: "T",
			// Status intentionally empty — Sarah omitempty shape.
		},
	}
	_ = c.plans.save(ctx, plan)

	resp := c.handleStoryStatusMutation(ctx, marshalJSON(t, StoryStatusMutationRequest{
		Slug: slug, StoryID: "story.empty-status.1", Target: workflow.StoryStatusExecuting,
	}))
	if !resp.Success {
		t.Errorf("empty Status should be treated as Ready and accept Executing claim: %v", resp.Error)
	}
}

// TestHandleStoryStatusMutation_RejectionPaths covers the validation
// error branches (missing fields, invalid target, missing story, missing
// plan) so a regression in one of them doesn't slip through silently.
func TestHandleStoryStatusMutation_RejectionPaths(t *testing.T) {
	ctx := context.Background()
	c := setupTestComponent(t)
	plan := setupTestPlan(t, c, "reject")
	plan.Stories = []workflow.Story{
		{ID: "story.reject.1", ComponentName: "x",
			RequirementIDs: []string{"req.reject.1"}, Title: "T",
			Status: workflow.StoryStatusReady},
	}
	_ = c.plans.save(ctx, plan)

	cases := []struct {
		name string
		req  StoryStatusMutationRequest
	}{
		{"missing slug", StoryStatusMutationRequest{StoryID: "x", Target: workflow.StoryStatusExecuting}},
		{"missing story_id", StoryStatusMutationRequest{Slug: "reject", Target: workflow.StoryStatusExecuting}},
		{"missing target", StoryStatusMutationRequest{Slug: "reject", StoryID: "x"}},
		{"invalid target value", StoryStatusMutationRequest{Slug: "reject", StoryID: "x", Target: workflow.StoryStatus("garbage")}},
		{"unknown story", StoryStatusMutationRequest{Slug: "reject", StoryID: "story.nope", Target: workflow.StoryStatusExecuting}},
		{"unknown plan", StoryStatusMutationRequest{Slug: "no-such-plan", StoryID: "x", Target: workflow.StoryStatusExecuting}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := c.handleStoryStatusMutation(ctx, marshalJSON(t, tc.req))
			if resp.Success {
				t.Errorf("%s: expected failure, got Success=true", tc.name)
			}
		})
	}
}
