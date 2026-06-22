package planmanager

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
)

func TestHandleGitHubPRFeedbackMutation_AutoStartsAffectedRequirements(t *testing.T) {
	ctx := context.Background()
	c := setupTestComponent(t)
	slug := "pr-feedback-reentry"
	plan := &workflow.Plan{
		ID:     workflow.PlanEntityID(slug),
		Slug:   slug,
		Title:  slug,
		Status: workflow.StatusAwaitingReview,
		GitHub: &workflow.GitHubMetadata{
			Repository: "C360Studio/semspec",
			PRNumber:   263,
		},
		Requirements: []workflow.Requirement{
			{ID: "req.pr.1", Title: "Address review feedback", Status: workflow.RequirementStatusActive},
			{ID: "req.pr.2", Title: "Unaffected deprecated work", Status: workflow.RequirementStatusDeprecated},
		},
		Scenarios: []workflow.Scenario{
			{ID: "scen.pr.1", RequirementID: "req.pr.1"},
			{ID: "scen.pr.2", RequirementID: "req.pr.2"},
		},
	}
	if err := c.plans.save(ctx, plan); err != nil {
		t.Fatalf("save: %v", err)
	}
	c.execBucket = resetKVStub{keys: []string{"req." + slug + ".req.pr.1"}}
	c.reqResetSender = func(context.Context, string) error {
		return nil
	}

	published := false
	c.orchestratorTriggerPublisher = func(_ context.Context, got *payloads.ScenarioOrchestrationTrigger) error {
		published = true
		if got.PlanSlug != slug {
			t.Fatalf("published slug = %q, want %q", got.PlanSlug, slug)
		}
		if len(got.ForceRequirementIDs) != 1 || got.ForceRequirementIDs[0] != "req.pr.1" {
			t.Fatalf("ForceRequirementIDs = %v, want req.pr.1", got.ForceRequirementIDs)
		}
		return nil
	}

	body, err := json.Marshal(payloads.GitHubPRFeedbackRequest{
		Slug:     slug,
		PRNumber: 263,
		ReviewID: 9001,
		Reviewer: "reviewer",
		State:    "CHANGES_REQUESTED",
		Body:     "Please adjust the first requirement.",
	})
	if err != nil {
		t.Fatalf("marshal feedback: %v", err)
	}

	resp := c.handleGitHubPRFeedbackMutation(ctx, body)
	if !resp.Success {
		t.Fatalf("handleGitHubPRFeedbackMutation failed: %s", resp.Error)
	}
	if !published {
		t.Fatal("expected PR feedback recovery to publish scenario orchestrator trigger")
	}
	got, ok := c.plans.get(slug)
	if !ok {
		t.Fatal("plan missing after PR feedback")
	}
	if got.EffectiveStatus() != workflow.StatusImplementing {
		t.Fatalf("status = %s, want implementing", got.EffectiveStatus())
	}
	if got.GitHub.PRRevision != 1 {
		t.Fatalf("PRRevision = %d, want 1", got.GitHub.PRRevision)
	}
}
