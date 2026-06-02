package cascade

import (
	"reflect"
	"sort"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestPlanDecision_StoryReprepare_ExplicitStoryIDs is the headline Train C
// step 3 contract: when proposal.Kind=story_reprepare AND
// proposal.AffectedStoryIDs is populated, the cascade dirty-marks ONLY
// those Stories + scenarios attached to them. Sibling Stories on the same
// Requirement are NOT touched.
func TestPlanDecision_StoryReprepare_ExplicitStoryIDs(t *testing.T) {
	proposal := &workflow.PlanDecision{
		ID:               "plan-decision.demo.recovery.abc12345",
		Kind:             workflow.PlanDecisionKindStoryReprepare,
		AffectedReqIDs:   []string{"req.demo.1"},
		AffectedStoryIDs: []string{"story.demo.1.2"}, // only Story 2
	}
	stories := []workflow.Story{
		{ID: "story.demo.1.1", RequirementID: "req.demo.1", Title: "Story 1 (untouched)"},
		{ID: "story.demo.1.2", RequirementID: "req.demo.1", Title: "Story 2 (targeted)"},
		{ID: "story.demo.1.3", RequirementID: "req.demo.1", Title: "Story 3 (untouched)"},
	}
	scenarios := []workflow.Scenario{
		{ID: "scen.demo.1.1", RequirementID: "req.demo.1", StoryID: "story.demo.1.1"},
		{ID: "scen.demo.1.2", RequirementID: "req.demo.1", StoryID: "story.demo.1.2"},
		{ID: "scen.demo.1.3", RequirementID: "req.demo.1", StoryID: "story.demo.1.3"},
	}

	result, err := PlanDecision(proposal, stories, scenarios)
	if err != nil {
		t.Fatalf("PlanDecision: %v", err)
	}

	if want := []string{"story.demo.1.2"}; !reflect.DeepEqual(result.AffectedStoryIDs, want) {
		t.Errorf("AffectedStoryIDs = %v, want %v", result.AffectedStoryIDs, want)
	}
	if want := []string{"scen.demo.1.2"}; !reflect.DeepEqual(result.AffectedScenarioIDs, want) {
		t.Errorf("AffectedScenarioIDs = %v, want %v (sibling Stories' scenarios must NOT be dirty)", result.AffectedScenarioIDs, want)
	}
}

// TestPlanDecision_StoryReprepare_FallbackToReqScope pins the back-compat
// path: when proposal.AffectedStoryIDs is empty but Kind=story_reprepare
// AND AffectedReqIDs is populated, the cascade dirty-marks every Story
// under those Requirements. Whole-Requirement re-prep — coarser but still
// reaches Sarah.
func TestPlanDecision_StoryReprepare_FallbackToReqScope(t *testing.T) {
	proposal := &workflow.PlanDecision{
		ID:             "plan-decision.demo.recovery.def67890",
		Kind:           workflow.PlanDecisionKindStoryReprepare,
		AffectedReqIDs: []string{"req.demo.1"},
		// AffectedStoryIDs intentionally empty
	}
	stories := []workflow.Story{
		{ID: "story.demo.1.1", RequirementID: "req.demo.1"},
		{ID: "story.demo.1.2", RequirementID: "req.demo.1"},
		{ID: "story.demo.2.1", RequirementID: "req.demo.2"}, // different req — must NOT match
	}
	scenarios := []workflow.Scenario{
		{ID: "scen.demo.1.1", RequirementID: "req.demo.1", StoryID: "story.demo.1.1"},
		{ID: "scen.demo.1.2", RequirementID: "req.demo.1", StoryID: "story.demo.1.2"},
		{ID: "scen.demo.2.1", RequirementID: "req.demo.2", StoryID: "story.demo.2.1"},
	}

	result, err := PlanDecision(proposal, stories, scenarios)
	if err != nil {
		t.Fatalf("PlanDecision: %v", err)
	}

	sort.Strings(result.AffectedStoryIDs)
	sort.Strings(result.AffectedScenarioIDs)
	wantStories := []string{"story.demo.1.1", "story.demo.1.2"}
	wantScenarios := []string{"scen.demo.1.1", "scen.demo.1.2"}
	if !reflect.DeepEqual(result.AffectedStoryIDs, wantStories) {
		t.Errorf("AffectedStoryIDs = %v, want %v", result.AffectedStoryIDs, wantStories)
	}
	if !reflect.DeepEqual(result.AffectedScenarioIDs, wantScenarios) {
		t.Errorf("AffectedScenarioIDs = %v, want %v (different-req scenarios must NOT cascade)", result.AffectedScenarioIDs, wantScenarios)
	}
}

// TestPlanDecision_RequirementChangePreservesBackCompat pins the existing
// scenarios-only cascade for Kind=requirement_change (the pre-Train-C
// contract). Stories are passed in but must NOT be dirty-marked by this
// kind — that's story_reprepare's job. Pre-Train-C behavior preserved.
func TestPlanDecision_RequirementChangePreservesBackCompat(t *testing.T) {
	proposal := &workflow.PlanDecision{
		ID:             "plan-decision.demo.qa.abc12345",
		Kind:           workflow.PlanDecisionKindRequirementChange,
		AffectedReqIDs: []string{"req.demo.1"},
	}
	stories := []workflow.Story{
		{ID: "story.demo.1.1", RequirementID: "req.demo.1"},
	}
	scenarios := []workflow.Scenario{
		{ID: "scen.demo.1.1", RequirementID: "req.demo.1", StoryID: "story.demo.1.1"},
		{ID: "scen.demo.2.1", RequirementID: "req.demo.2", StoryID: "story.demo.2.1"},
	}

	result, err := PlanDecision(proposal, stories, scenarios)
	if err != nil {
		t.Fatalf("PlanDecision: %v", err)
	}

	if len(result.AffectedStoryIDs) != 0 {
		t.Errorf("AffectedStoryIDs = %v, want empty (requirement_change does not dirty-mark Stories)", result.AffectedStoryIDs)
	}
	if want := []string{"scen.demo.1.1"}; !reflect.DeepEqual(result.AffectedScenarioIDs, want) {
		t.Errorf("AffectedScenarioIDs = %v, want %v (scenarios-only cascade)", result.AffectedScenarioIDs, want)
	}
}

// TestPlanDecision_UnsetKindPreservesBackCompat pins that legacy
// PlanDecision records without a Kind field (pre-Kind enum, default
// zero-value) get the same scenarios-only cascade as
// Kind=requirement_change. plan-manager's wire-shape default is
// requirement_change but cascade.go itself must tolerate the empty case
// too.
func TestPlanDecision_UnsetKindPreservesBackCompat(t *testing.T) {
	proposal := &workflow.PlanDecision{
		ID: "plan-decision.legacy.1",
		// Kind intentionally unset (zero-value "")
		AffectedReqIDs: []string{"req.legacy.1"},
	}
	scenarios := []workflow.Scenario{
		{ID: "scen.legacy.1", RequirementID: "req.legacy.1"},
	}

	result, err := PlanDecision(proposal, nil, scenarios)
	if err != nil {
		t.Fatalf("PlanDecision: %v", err)
	}

	if want := []string{"scen.legacy.1"}; !reflect.DeepEqual(result.AffectedScenarioIDs, want) {
		t.Errorf("AffectedScenarioIDs = %v, want %v (legacy unset-Kind ⇒ scenarios-only)", result.AffectedScenarioIDs, want)
	}
}

// TestPlanDecision_ExecutionExhaustedIsNoOp pins that the terminal kind
// produces an empty cascade. Callers don't typically invoke PlanDecision
// for execution_exhausted (the auto-archive path is separate), but the
// function must tolerate it as a defensive no-op.
func TestPlanDecision_ExecutionExhaustedIsNoOp(t *testing.T) {
	proposal := &workflow.PlanDecision{
		ID:             "plan-decision.demo.exhausted.1",
		Kind:           workflow.PlanDecisionKindExecutionExhausted,
		AffectedReqIDs: []string{"req.demo.1"},
	}
	stories := []workflow.Story{{ID: "story.demo.1.1", RequirementID: "req.demo.1"}}
	scenarios := []workflow.Scenario{{ID: "scen.demo.1.1", RequirementID: "req.demo.1", StoryID: "story.demo.1.1"}}

	result, err := PlanDecision(proposal, stories, scenarios)
	if err != nil {
		t.Fatalf("PlanDecision: %v", err)
	}

	if len(result.AffectedStoryIDs) != 0 {
		t.Errorf("AffectedStoryIDs = %v, want empty for execution_exhausted no-op", result.AffectedStoryIDs)
	}
	if len(result.AffectedScenarioIDs) != 0 {
		t.Errorf("AffectedScenarioIDs = %v, want empty for execution_exhausted no-op", result.AffectedScenarioIDs)
	}
	if want := []string{"req.demo.1"}; !reflect.DeepEqual(result.AffectedRequirementIDs, want) {
		t.Errorf("AffectedRequirementIDs = %v, want %v (telemetry context preserved)", result.AffectedRequirementIDs, want)
	}
}
