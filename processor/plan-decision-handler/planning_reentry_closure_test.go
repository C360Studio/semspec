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
		{ID: "consumer", DependsOn: []string{"contract"}},
		{ID: "unrelated"},
	}

	expandPlanningReentryClosure(result, requirements)

	want := []string{"consumer", "contract"}
	if !reflect.DeepEqual(result.AffectedRequirementIDs, want) {
		t.Fatalf("AffectedRequirementIDs = %v, want %v", result.AffectedRequirementIDs, want)
	}
}

func TestExpandPlanningReentryClosure_RequirementChangeUnchanged(t *testing.T) {
	result := &cascade.Result{
		Kind:                   workflow.PlanDecisionKindRequirementChange,
		AffectedRequirementIDs: []string{"contract"},
	}
	requirements := []workflow.Requirement{
		{ID: "consumer", DependsOn: []string{"contract"}},
	}

	expandPlanningReentryClosure(result, requirements)

	want := []string{"contract"}
	if !reflect.DeepEqual(result.AffectedRequirementIDs, want) {
		t.Fatalf("AffectedRequirementIDs = %v, want unchanged %v", result.AffectedRequirementIDs, want)
	}
}
