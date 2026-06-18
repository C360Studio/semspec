package planmanager

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
)

func TestScopeIncompleteAcceptEffectsWritesGuidanceAndAutoStartsRetry(t *testing.T) {
	ctx := context.Background()
	c := setupTestComponent(t)
	plan := &workflow.Plan{
		ID:     workflow.PlanEntityID("scope-retry"),
		Slug:   "scope-retry",
		Title:  "scope-retry",
		Status: workflow.StatusRejected,
		Requirements: []workflow.Requirement{
			{ID: "req.scope-retry.1", Title: "first"},
			{ID: "req.scope-retry.2", Title: "second"},
		},
		Stories: []workflow.Story{
			{ID: "story.scope-retry.1.1", RequirementIDs: []string{"req.scope-retry.1"}, Status: workflow.StoryStatusComplete},
			{ID: "story.scope-retry.2.1", RequirementIDs: []string{"req.scope-retry.2"}, Status: workflow.StoryStatusComplete},
		},
		Scenarios: []workflow.Scenario{
			{ID: "scen.scope-retry.1", RequirementID: "req.scope-retry.1", StoryID: "story.scope-retry.1.1"},
		},
		PlanDecisions: []workflow.PlanDecision{
			{
				ID:             "plan-decision.scope-retry.1",
				PlanID:         workflow.PlanEntityID("scope-retry"),
				Kind:           workflow.PlanDecisionKindScopeIncomplete,
				Title:          "Declared scope.create files not delivered",
				Rationale:      "Level-0 completeness gate: missing src/required.go",
				AffectedReqIDs: []string{"req.scope-retry.1"},
				Status:         workflow.PlanDecisionStatusProposed,
				ProposedBy:     "plan-manager",
				ContractImpact: &workflow.ContractImpact{
					Kind:    workflow.ContractImpactPreserve,
					Summary: "Declared scope remains required.",
				},
			},
		},
	}
	if err := c.plans.save(ctx, plan); err != nil {
		t.Fatalf("save: %v", err)
	}

	published := false
	c.orchestratorTriggerPublisher = func(_ context.Context, got *payloads.ScenarioOrchestrationTrigger) error {
		published = true
		if got.PlanSlug != plan.Slug {
			t.Fatalf("published slug = %q, want %q", got.PlanSlug, plan.Slug)
		}
		if len(got.ForceRequirementIDs) != 1 || got.ForceRequirementIDs[0] != "req.scope-retry.1" {
			t.Fatalf("ForceRequirementIDs = %v, want affected req", got.ForceRequirementIDs)
		}
		return nil
	}

	body, err := json.Marshal(planDecisionAcceptRequest{
		Slug:       plan.Slug,
		ProposalID: "plan-decision.scope-retry.1",
		AcceptedBy: "auto:recovery",
	})
	if err != nil {
		t.Fatalf("marshal accept request: %v", err)
	}
	resp := c.handlePlanDecisionAcceptMutation(ctx, body)
	if !resp.Success {
		t.Fatalf("handlePlanDecisionAcceptMutation failed: %s", resp.Error)
	}
	if !published {
		t.Fatal("expected accepted scope_incomplete retry to publish orchestrator trigger")
	}

	got, ok := c.plans.get(plan.Slug)
	if !ok {
		t.Fatal("plan missing after accept")
	}
	if got.EffectiveStatus() != workflow.StatusImplementing {
		t.Fatalf("status = %s, want implementing", got.EffectiveStatus())
	}
	if got.PlanDecisions[0].Status != workflow.PlanDecisionStatusAccepted {
		t.Fatalf("decision status = %s, want accepted", got.PlanDecisions[0].Status)
	}
	if got.Stories[0].Status != workflow.StoryStatusReady {
		t.Fatalf("affected story status = %s, want ready", got.Stories[0].Status)
	}
	if got.Stories[1].Status != workflow.StoryStatusComplete {
		t.Fatalf("unaffected story status = %s, want complete", got.Stories[1].Status)
	}
	hint := got.Requirements[0].RecoveryHint
	if !strings.Contains(hint, "missing src/required.go") {
		t.Fatalf("RecoveryHint = %q, want missing file rationale", hint)
	}
	if !strings.Contains(hint, "declared scope.create files above remain required") {
		t.Fatalf("RecoveryHint = %q, want explicit scope guidance", hint)
	}
	if got.Requirements[1].RecoveryHint != "" {
		t.Fatalf("unaffected RecoveryHint = %q, want empty", got.Requirements[1].RecoveryHint)
	}
}
