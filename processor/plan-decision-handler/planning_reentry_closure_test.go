package changeproposalhandler

import (
	"reflect"
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/cascade"
)

func TestExpandPlanningReentryClosure_ArchitectureRevise(t *testing.T) {
	result := &cascade.Result{
		Kind:                   workflow.PlanDecisionKindArchitectureRevise,
		AffectedRequirementIDs: []string{"contract"},
	}
	requirements := []workflow.Requirement{
		{ID: "bootstrap"},
		{ID: "contract", DependsOn: []string{"bootstrap"}},
		{ID: "control"},
		{ID: "consumer", DependsOn: []string{"contract"}},
		{ID: "unrelated"},
	}
	stories := []workflow.Story{
		{ID: "story.mapper", RequirementIDs: []string{"contract", "control"}},
		{ID: "story.unrelated", RequirementIDs: []string{"unrelated"}},
	}

	expandPlanningReentryClosure(result, requirements, stories, nil)

	want := []string{"consumer", "contract", "control"}
	if !reflect.DeepEqual(result.AffectedRequirementIDs, want) {
		t.Fatalf("AffectedRequirementIDs = %v, want %v", result.AffectedRequirementIDs, want)
	}
}

func TestExpandPlanningReentryClosure_RequirementChangeAddsDependentScenarios(t *testing.T) {
	result := &cascade.Result{
		Kind:                   workflow.PlanDecisionKindRequirementChange,
		AffectedRequirementIDs: []string{"contract"},
		AffectedScenarioIDs:    []string{"scenario.contract"},
	}
	requirements := []workflow.Requirement{
		{ID: "contract"},
		{ID: "consumer", DependsOn: []string{"contract"}},
		{ID: "unrelated"},
	}
	scenarios := []workflow.Scenario{
		{ID: "scenario.contract", RequirementID: "contract"},
		{ID: "scenario.consumer", RequirementID: "consumer"},
		{ID: "scenario.unrelated", RequirementID: "unrelated"},
	}

	expandPlanningReentryClosure(result, requirements, nil, scenarios)

	wantReqs := []string{"consumer", "contract"}
	if !reflect.DeepEqual(result.AffectedRequirementIDs, wantReqs) {
		t.Fatalf("AffectedRequirementIDs = %v, want %v", result.AffectedRequirementIDs, wantReqs)
	}
	wantScenarios := []string{"scenario.contract", "scenario.consumer"}
	if !reflect.DeepEqual(result.AffectedScenarioIDs, wantScenarios) {
		t.Fatalf("AffectedScenarioIDs = %v, want %v", result.AffectedScenarioIDs, wantScenarios)
	}
}
