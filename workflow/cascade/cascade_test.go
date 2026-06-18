package cascade

import (
	"reflect"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestPlanDecision_NilProposal verifies that a nil proposal returns an error
// immediately, before any scenario filtering.
func TestPlanDecision_NilProposal(t *testing.T) {
	_, err := PlanDecision(nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for nil proposal")
	}
}

// TestPlanDecision_NoAffectedRequirements verifies that an empty AffectedReqIDs
// slice results in an empty cascade result without examining scenarios.
func TestPlanDecision_NoAffectedRequirements(t *testing.T) {
	proposal := &workflow.PlanDecision{
		ID:             "cp-1",
		AffectedReqIDs: []string{}, // empty — returns before filtering scenarios
	}

	result, err := PlanDecision(proposal, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.AffectedScenarioIDs) != 0 {
		t.Errorf("AffectedScenarioIDs = %d, want 0", len(result.AffectedScenarioIDs))
	}
}

// TestPlanDecision_ArchitectureRevise_NoOp verifies the architecture_revise
// cascade is a no-op even with Stories + Scenarios present: the plan-manager
// accept handler applies the planning re-entry inline, so the cascade must not
// dirty-mark anything (it records only the affected reqs for telemetry). Without
// an explicit case this kind would fall through to the requirement_change
// default and wrongly dirty-mark scenarios.
func TestPlanDecision_ArchitectureRevise_NoOp(t *testing.T) {
	proposal := &workflow.PlanDecision{
		ID:             "plan-decision.slug.recovery.abcd1234",
		Kind:           workflow.PlanDecisionKindArchitectureRevise,
		AffectedReqIDs: []string{"req-0"},
	}
	stories := []workflow.Story{{ID: "story-1", RequirementIDs: []string{"req-0"}}}
	scenarios := []workflow.Scenario{{ID: "scenario-1", RequirementID: "req-0", StoryID: "story-1"}}

	result, err := PlanDecision(proposal, stories, scenarios)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.AffectedScenarioIDs) != 0 {
		t.Errorf("AffectedScenarioIDs = %d, want 0 (cascade is a no-op for architecture_revise)", len(result.AffectedScenarioIDs))
	}
	if len(result.AffectedStoryIDs) != 0 {
		t.Errorf("AffectedStoryIDs = %d, want 0", len(result.AffectedStoryIDs))
	}
	if len(result.AffectedRequirementIDs) != 1 {
		t.Errorf("AffectedRequirementIDs = %d, want 1 (telemetry only)", len(result.AffectedRequirementIDs))
	}
	// Kind must be echoed onto the Result so the PlanDecisionAcceptedEvent
	// carries it — the requirement-executor branches on it to abandon (not
	// resume) in-flight execs.
	if result.Kind != workflow.PlanDecisionKindArchitectureRevise {
		t.Errorf("Result.Kind = %q, want architecture_revise", result.Kind)
	}
}

// TestPlanDecision_ScopeIncomplete_NoOp verifies the Level-0 completeness
// recovery kind does not fall through to requirement_change's scenarios-only
// cascade. Plan-manager owns the retry reset and missing-file guidance.
func TestPlanDecision_ScopeIncomplete_NoOp(t *testing.T) {
	proposal := &workflow.PlanDecision{
		ID:             "plan-decision.slug.scope-incomplete.1",
		Kind:           workflow.PlanDecisionKindScopeIncomplete,
		AffectedReqIDs: []string{"req-0"},
	}
	stories := []workflow.Story{{ID: "story-1", RequirementIDs: []string{"req-0"}}}
	scenarios := []workflow.Scenario{{ID: "scenario-1", RequirementID: "req-0", StoryID: "story-1"}}

	result, err := PlanDecision(proposal, stories, scenarios)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.AffectedScenarioIDs) != 0 {
		t.Errorf("AffectedScenarioIDs = %d, want 0 (plan-manager owns scope_incomplete retry)", len(result.AffectedScenarioIDs))
	}
	if len(result.AffectedStoryIDs) != 0 {
		t.Errorf("AffectedStoryIDs = %d, want 0", len(result.AffectedStoryIDs))
	}
	if len(result.AffectedRequirementIDs) != 1 {
		t.Errorf("AffectedRequirementIDs = %d, want 1 (telemetry only)", len(result.AffectedRequirementIDs))
	}
	if result.Kind != workflow.PlanDecisionKindScopeIncomplete {
		t.Errorf("Result.Kind = %q, want scope_incomplete", result.Kind)
	}
}

func TestExpandRequirementClosure_DownstreamOnly(t *testing.T) {
	requirements := []workflow.Requirement{
		{ID: "req.bootstrap"},
		{ID: "req.contract", DependsOn: []string{"req.bootstrap"}},
		{ID: "req.consumer", DependsOn: []string{"req.contract"}},
		{ID: "req.unrelated"},
	}

	got := ExpandRequirementClosure(requirements, []string{"req.contract"})
	want := []string{"req.consumer", "req.contract"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExpandRequirementClosure = %v, want %v", got, want)
	}

	got = ExpandRequirementClosure(requirements, []string{"req.consumer"})
	want = []string{"req.consumer"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("leaf closure = %v, want %v", got, want)
	}
}
