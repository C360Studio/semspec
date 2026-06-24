package planmanager

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
)

// TestPlanHasAcceptedStoryReprepareDecision locks the signal that distinguishes a
// SCOPED recovery re-review (auto-advance) from everything else. Only an accepted
// story_reprepare qualifies — whole-phase architecture_revise must NOT (it keeps
// the human gate), and a first-ever R2 has no decision.
func TestPlanHasAcceptedStoryReprepareDecision(t *testing.T) {
	tests := []struct {
		name      string
		decisions []workflow.PlanDecision
		want      bool
	}{
		{"none", nil, false},
		{"accepted story_reprepare", []workflow.PlanDecision{{Status: workflow.PlanDecisionStatusAccepted, Kind: workflow.PlanDecisionKindStoryReprepare}}, true},
		{"accepted architecture_revise (whole-phase — must NOT auto-advance)", []workflow.PlanDecision{{Status: workflow.PlanDecisionStatusAccepted, Kind: workflow.PlanDecisionKindArchitectureRevise}}, false},
		{"accepted scope_incomplete (never reaches this handler)", []workflow.PlanDecision{{Status: workflow.PlanDecisionStatusAccepted, Kind: workflow.PlanDecisionKindScopeIncomplete}}, false},
		{"proposed (not accepted) story_reprepare", []workflow.PlanDecision{{Status: workflow.PlanDecisionStatusProposed, Kind: workflow.PlanDecisionKindStoryReprepare}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := planHasAcceptedStoryReprepareDecision(&workflow.Plan{PlanDecisions: tt.decisions}); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestHandleScenariosReviewedMutation_RecoveryReReviewAutoAdvances is the #295
// regression: a story_reprepare recovery re-runs R2 → scenarios_reviewed, and
// in auto_approve=false the plan must NOT wedge awaiting a human approval that a
// recovery cycle should not re-require. It must auto-advance and re-dispatch
// execution. Reproduces the mavlink-hard wedge with NO LLM.
func TestHandleScenariosReviewedMutation_RecoveryReReviewAutoAdvances(t *testing.T) {
	c := setupTestComponent(t)
	slug := "scen-recovery-reentry"

	plan := setupTestPlan(t, c, slug)
	plan.Status = workflow.StatusReviewingScenarios
	plan.Approved = true
	plan.Requirements = []workflow.Requirement{{ID: "requirement.1", Title: "x"}}
	plan.Stories = []workflow.Story{{ID: "story.1", RequirementIDs: []string{"requirement.1"}, Status: workflow.StoryStatusReady}}
	plan.Scenarios = []workflow.Scenario{{ID: "scenario.1", RequirementID: "requirement.1", StoryID: "story.1"}}
	plan.PlanDecisions = []workflow.PlanDecision{{Status: workflow.PlanDecisionStatusAccepted, Kind: workflow.PlanDecisionKindStoryReprepare}}
	_ = c.plans.save(context.Background(), plan)

	published := false
	c.orchestratorTriggerPublisher = func(_ context.Context, got *payloads.ScenarioOrchestrationTrigger) error {
		published = true
		if got.PlanSlug != slug {
			t.Fatalf("published slug = %q, want %q", got.PlanSlug, slug)
		}
		return nil
	}

	data, _ := json.Marshal(map[string]string{"slug": slug, "summary": "re-reviewed"})
	resp := c.handleScenariosReviewedMutation(context.Background(), data)
	if !resp.Success {
		t.Fatalf("mutation failed: %s", resp.Error)
	}
	if !published {
		t.Fatal("recovery re-review must auto-dispatch execution, not wedge at scenarios_reviewed (#295)")
	}
	got, _ := c.plans.get(slug)
	if got.Status == workflow.StatusScenariosReviewed {
		t.Errorf("plan stuck at scenarios_reviewed; want advanced past it (ready_for_execution/implementing)")
	}
}

// TestHandleScenariosReviewedMutation_InitialR2AwaitsApproval guards the
// unchanged path: with NO recovery decision (the initial R2), auto_approve=false
// must still await human approval and NOT auto-dispatch.
func TestHandleScenariosReviewedMutation_InitialR2AwaitsApproval(t *testing.T) {
	c := setupTestComponent(t)
	slug := "scen-initial"

	plan := setupTestPlan(t, c, slug)
	plan.Status = workflow.StatusReviewingScenarios
	plan.Requirements = []workflow.Requirement{{ID: "requirement.1", Title: "x"}}
	plan.Scenarios = []workflow.Scenario{{ID: "scenario.1", RequirementID: "requirement.1"}}
	_ = c.plans.save(context.Background(), plan)

	published := false
	c.orchestratorTriggerPublisher = func(_ context.Context, _ *payloads.ScenarioOrchestrationTrigger) error {
		published = true
		return nil
	}

	data, _ := json.Marshal(map[string]string{"slug": slug})
	resp := c.handleScenariosReviewedMutation(context.Background(), data)
	if !resp.Success {
		t.Fatalf("mutation failed: %s", resp.Error)
	}
	if published {
		t.Error("initial R2 (no recovery decision) must await approval, not auto-dispatch")
	}
	got, _ := c.plans.get(slug)
	if got.Status != workflow.StatusScenariosReviewed {
		t.Errorf("status = %q, want scenarios_reviewed (awaiting approval)", got.Status)
	}
}

// TestHandleScenariosReviewedMutation_WholePhaseArchReviseAwaits guards the
// product-risk boundary (review of #295): a whole-phase architecture_revise
// regenerates the entire plan, so under auto_approve=false it must NOT bypass the
// human gate — it keeps awaiting approval even though it is a recovery.
func TestHandleScenariosReviewedMutation_WholePhaseArchReviseAwaits(t *testing.T) {
	c := setupTestComponent(t)
	slug := "scen-arch-revise"

	plan := setupTestPlan(t, c, slug)
	plan.Status = workflow.StatusReviewingScenarios
	plan.Approved = true
	plan.Requirements = []workflow.Requirement{{ID: "requirement.1", Title: "x"}}
	plan.Scenarios = []workflow.Scenario{{ID: "scenario.1", RequirementID: "requirement.1"}}
	plan.PlanDecisions = []workflow.PlanDecision{{Status: workflow.PlanDecisionStatusAccepted, Kind: workflow.PlanDecisionKindArchitectureRevise}}
	_ = c.plans.save(context.Background(), plan)

	published := false
	c.orchestratorTriggerPublisher = func(_ context.Context, _ *payloads.ScenarioOrchestrationTrigger) error {
		published = true
		return nil
	}

	data, _ := json.Marshal(map[string]string{"slug": slug})
	resp := c.handleScenariosReviewedMutation(context.Background(), data)
	if !resp.Success {
		t.Fatalf("mutation failed: %s", resp.Error)
	}
	if published {
		t.Error("whole-phase architecture_revise must NOT auto-dispatch — a regenerated plan keeps the human gate")
	}
	got, _ := c.plans.get(slug)
	if got.Status != workflow.StatusScenariosReviewed {
		t.Errorf("status = %q, want scenarios_reviewed (awaiting approval)", got.Status)
	}
}
