package scenariogenerator

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
)

// TestBuildScenariosMutationPayload_PerStoryCarriesStoryID pins that when
// the trigger carries a StoryID (ADR-043 PR 4j per-Story dispatch), the
// JSON-encoded mutation payload sent to plan-manager includes the
// story_id field. Plan-manager's handleScenariosMutation uses this field
// to scope its merge wipe so concurrent Stories' scenarios do not clobber
// each other. Closes go-reviewer Pass-2 C2 (producer side).
func TestBuildScenariosMutationPayload_PerStoryCarriesStoryID(t *testing.T) {
	trigger := &payloads.ScenarioGeneratorRequest{
		Slug:          "demo",
		RequirementID: "req.demo.1",
		StoryID:       "story.demo.1.1",
		TraceID:       "trace-xyz",
	}
	scenarios := []workflow.Scenario{
		{ID: "scen.1", RequirementID: "req.demo.1", StoryID: "story.demo.1.1"},
	}

	data, err := buildScenariosMutationPayload(trigger, scenarios)
	if err != nil {
		t.Fatalf("buildScenariosMutationPayload: %v", err)
	}

	var got scenariosMutationPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Slug != "demo" {
		t.Errorf("Slug = %q, want demo", got.Slug)
	}
	if got.RequirementID != "req.demo.1" {
		t.Errorf("RequirementID = %q, want req.demo.1", got.RequirementID)
	}
	if got.StoryID != "story.demo.1.1" {
		t.Errorf("StoryID = %q, want story.demo.1.1", got.StoryID)
	}
	if len(got.Scenarios) != 1 {
		t.Errorf("Scenarios = %d, want 1", len(got.Scenarios))
	}
	if got.TraceID != "trace-xyz" {
		t.Errorf("TraceID = %q, want trace-xyz", got.TraceID)
	}
}

// TestBuildScenariosMutationPayload_LegacyOmitsStoryID pins back-compat:
// when the trigger has no StoryID (pre-Sarah plans, mock fixtures), the
// wire JSON OMITS the story_id field entirely — plan-manager's handler
// then falls through to wipe-by-RequirementID, preserving pre-ADR-043
// behavior for those callers.
func TestBuildScenariosMutationPayload_LegacyOmitsStoryID(t *testing.T) {
	trigger := &payloads.ScenarioGeneratorRequest{
		Slug:          "legacy",
		RequirementID: "req.legacy.1",
		// StoryID intentionally empty — legacy per-Requirement dispatch.
	}
	scenarios := []workflow.Scenario{
		{ID: "scen.legacy.1", RequirementID: "req.legacy.1"},
	}

	data, err := buildScenariosMutationPayload(trigger, scenarios)
	if err != nil {
		t.Fatalf("buildScenariosMutationPayload: %v", err)
	}

	// Verify the field is omitted from the wire bytes (not just zero-valued).
	// The omitempty contract is load-bearing: plan-manager treats absent and
	// empty identically, but operator/log readability prefers omission so
	// legacy dispatches don't carry an empty `story_id:""` noise field.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, present := raw["story_id"]; present {
		t.Errorf("story_id should be omitted from wire JSON when empty; got bytes: %s", string(data))
	}
}
