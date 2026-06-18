package planmanager

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
)

func TestPlanStoreSaveAndGetIsolateCachedPlanFromCallerMutation(t *testing.T) {
	ctx := context.Background()
	c := setupTestComponent(t)
	slug := "isolation"
	now := time.Now()
	plan := &workflow.Plan{
		ID:          workflow.PlanEntityID(slug),
		Slug:        slug,
		Title:       slug,
		Status:      workflow.StatusImplementing,
		Constraints: []string{"preserve baseline"},
		Scope: workflow.Scope{
			Create:  []string{"src/required.go"},
			Include: []string{"README.md"},
		},
		Contract: &workflow.ContractPacket{
			ID:      workflow.PlanContractID(slug),
			Version: 1,
			Scope: workflow.ContractScopeSnapshot{
				Create: []string{"src/required.go"},
			},
			CreatedAt: now,
		},
		Requirements: []workflow.Requirement{
			{ID: "req.isolation.1", DependsOn: []string{"req.seed"}},
		},
		Stories: []workflow.Story{
			{ID: "story.isolation.1", RequirementIDs: []string{"req.isolation.1"}},
		},
		Scenarios: []workflow.Scenario{
			{ID: "scenario.isolation.1", RequirementID: "req.isolation.1", StoryID: "story.isolation.1", Then: []string{"passes"}},
		},
		PlanDecisions: []workflow.PlanDecision{
			{ID: "plan-decision.isolation.1", Status: workflow.PlanDecisionStatusProposed},
		},
	}
	if err := c.plans.save(ctx, plan); err != nil {
		t.Fatalf("save: %v", err)
	}

	plan.PlanDecisions[0].Status = workflow.PlanDecisionStatusAccepted
	plan.Requirements[0].DependsOn[0] = "mutated-after-save"
	plan.Contract.Scope.Create[0] = "mutated-after-save.go"

	first, ok := c.plans.get(slug)
	if !ok {
		t.Fatal("plan missing after save")
	}
	if first.PlanDecisions[0].Status != workflow.PlanDecisionStatusProposed {
		t.Fatalf("decision status after caller mutation = %s, want proposed", first.PlanDecisions[0].Status)
	}
	if first.Requirements[0].DependsOn[0] != "req.seed" {
		t.Fatalf("depends_on after caller mutation = %v, want req.seed", first.Requirements[0].DependsOn)
	}
	if first.Contract.Scope.Create[0] != "src/required.go" {
		t.Fatalf("contract scope after caller mutation = %v, want src/required.go", first.Contract.Scope.Create)
	}

	first.PlanDecisions[0].Status = workflow.PlanDecisionStatusAccepted
	first.Requirements[0].DependsOn[0] = "mutated-after-get"
	first.Contract.Scope.Create[0] = "mutated-after-get.go"

	second, ok := c.plans.get(slug)
	if !ok {
		t.Fatal("plan missing on second get")
	}
	if second.PlanDecisions[0].Status != workflow.PlanDecisionStatusProposed {
		t.Fatalf("decision status after get mutation = %s, want proposed", second.PlanDecisions[0].Status)
	}
	if second.Requirements[0].DependsOn[0] != "req.seed" {
		t.Fatalf("depends_on after get mutation = %v, want req.seed", second.Requirements[0].DependsOn)
	}
	if second.Contract.Scope.Create[0] != "src/required.go" {
		t.Fatalf("contract scope after get mutation = %v, want src/required.go", second.Contract.Scope.Create)
	}
}

func TestPlanDecisionAcceptMutationResetFailureLeavesPersistedPlanUnchanged(t *testing.T) {
	ctx := context.Background()
	c := setupTestComponent(t)
	slug := "failed-accept"
	decisionID := "plan-decision.failed-accept.arch"
	plan := &workflow.Plan{
		ID:     workflow.PlanEntityID(slug),
		Slug:   slug,
		Title:  slug,
		Status: workflow.StatusImplementing,
		Architecture: &workflow.ArchitectureDocument{
			DataFlow: "existing architecture",
		},
		Contract: &workflow.ContractPacket{
			ID:        workflow.PlanContractID(slug),
			Version:   1,
			CreatedAt: time.Now(),
		},
		Requirements: []workflow.Requirement{
			{ID: "req.failed-accept.1", Title: "unaffected"},
		},
		Stories: []workflow.Story{
			{ID: "story.failed-accept.1", RequirementIDs: []string{"req.failed-accept.1"}},
		},
		Scenarios: []workflow.Scenario{
			{ID: "scenario.failed-accept.1", RequirementID: "req.failed-accept.1", StoryID: "story.failed-accept.1"},
		},
		PlanDecisions: []workflow.PlanDecision{
			{
				ID:         decisionID,
				PlanID:     workflow.PlanEntityID(slug),
				Kind:       workflow.PlanDecisionKindArchitectureRevise,
				Title:      "Revise architecture",
				Rationale:  "No scoped requirements and no whole-phase reset evidence.",
				Status:     workflow.PlanDecisionStatusProposed,
				ProposedBy: "recovery-agent",
				ContractImpact: &workflow.ContractImpact{
					Kind:    workflow.ContractImpactChange,
					Summary: "Architecture must change, but the reset scope is invalid.",
				},
			},
		},
	}
	if err := c.plans.save(ctx, plan); err != nil {
		t.Fatalf("save: %v", err)
	}

	resp := c.handlePlanDecisionAcceptMutation(ctx, marshalJSON(t, planDecisionAcceptRequest{
		Slug:       slug,
		ProposalID: decisionID,
		AcceptedBy: "auto:recovery",
	}))
	if resp.Success {
		t.Fatal("handlePlanDecisionAcceptMutation succeeded; want reset-scope failure")
	}

	got, ok := c.plans.get(slug)
	if !ok {
		t.Fatal("plan missing after failed accept")
	}
	if got.Status != workflow.StatusImplementing {
		t.Fatalf("status after failed accept = %s, want implementing", got.Status)
	}
	if got.PlanDecisions[0].Status != workflow.PlanDecisionStatusProposed {
		t.Fatalf("decision status after failed accept = %s, want proposed", got.PlanDecisions[0].Status)
	}
	if got.PlanDecisions[0].DecidedAt != nil {
		t.Fatalf("DecidedAt after failed accept = %v, want nil", got.PlanDecisions[0].DecidedAt)
	}
	if got.Architecture == nil || got.Architecture.DataFlow != "existing architecture" {
		t.Fatalf("Architecture after failed accept = %+v, want original", got.Architecture)
	}
	if got.Contract == nil {
		t.Fatal("Contract missing after failed accept")
	}
	if len(got.Contract.Amendments) != 0 {
		t.Fatalf("contract amendments after failed accept = %d, want 0", len(got.Contract.Amendments))
	}
	if len(got.Stories) != 1 || got.Stories[0].ID != "story.failed-accept.1" {
		t.Fatalf("stories after failed accept = %+v, want original story", got.Stories)
	}
	if len(got.Scenarios) != 1 || got.Scenarios[0].ID != "scenario.failed-accept.1" {
		t.Fatalf("scenarios after failed accept = %+v, want original scenario", got.Scenarios)
	}
}
