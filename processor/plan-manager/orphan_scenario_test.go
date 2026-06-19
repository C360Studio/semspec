package planmanager

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestDropOrphanScenarios pins the pure filter behind #80: scenarios whose
// StoryID is no longer in the Story set are dropped; empty-StoryID legacy
// scenarios are always preserved.
func TestDropOrphanScenarios(t *testing.T) {
	stories := []workflow.Story{{ID: "C"}, {ID: "D"}}
	scenarios := []workflow.Scenario{
		{ID: "s1", StoryID: "A"}, // orphan — old story
		{ID: "s2", StoryID: "C"}, // valid — current story
		{ID: "s3", StoryID: ""},  // legacy — preserved
		{ID: "s4", StoryID: "B"}, // orphan — old story
	}
	got := dropOrphanScenarios(scenarios, stories)

	gotIDs := map[string]bool{}
	for _, s := range got {
		gotIDs[s.ID] = true
	}
	if len(got) != 2 || !gotIDs["s2"] || !gotIDs["s3"] {
		t.Fatalf("dropOrphanScenarios kept %v, want [s2 (valid), s3 (legacy)]", gotIDs)
	}
	if gotIDs["s1"] || gotIDs["s4"] {
		t.Errorf("orphan scenarios pinned to removed stories were not dropped: %v", gotIDs)
	}

	// Empty input is returned untouched.
	if out := dropOrphanScenarios(nil, stories); len(out) != 0 {
		t.Errorf("dropOrphanScenarios(nil) = %v, want empty", out)
	}
}

// TestHandleStoriesMutation_DropsOrphanScenarios proves the filter is WIRED into
// the full-replace path: when Sarah re-emits Stories with new IDs, scenarios
// pinned to the old Story IDs are dropped while a legacy empty-StoryID scenario
// survives (#80).
func TestHandleStoriesMutation_DropsOrphanScenarios(t *testing.T) {
	ctx := context.Background()
	c := setupTestComponent(t)
	plan := setupTestPlan(t, c, "orphan-scen")
	plan.Status = workflow.StatusPreparingStories
	// Prior Story shape A/B with scenarios, plus a legacy empty-StoryID scenario.
	plan.Stories = []workflow.Story{{ID: "story.orphan-scen.old.A"}, {ID: "story.orphan-scen.old.B"}}
	plan.Scenarios = []workflow.Scenario{
		{ID: "scen.A", StoryID: "story.orphan-scen.old.A", RequirementID: "req.orphan-scen.1"},
		{ID: "scen.B", StoryID: "story.orphan-scen.old.B", RequirementID: "req.orphan-scen.1"},
		{ID: "scen.legacy", StoryID: "", RequirementID: "req.orphan-scen.1"},
	}
	_ = c.plans.save(ctx, plan)

	// Sarah re-emits a fresh Story set with DIFFERENT IDs.
	req := storiesMutationRequest{
		Slug:       "orphan-scen",
		Stories:    []workflow.Story{validStory("story.orphan-scen.1.1", "req.orphan-scen.1", "Implement core")},
		StoryCount: 1,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if resp := c.handleStoriesMutation(ctx, data); !resp.Success {
		t.Fatalf("handleStoriesMutation failed: %s", resp.Error)
	}

	got, ok := c.plans.get("orphan-scen")
	if !ok {
		t.Fatal("plan not found after mutation")
	}
	ids := map[string]bool{}
	for _, s := range got.Scenarios {
		ids[s.ID] = true
	}
	if ids["scen.A"] || ids["scen.B"] {
		t.Errorf("orphan scenarios (old Story IDs) survived the Story replacement: %v", ids)
	}
	if !ids["scen.legacy"] {
		t.Errorf("legacy empty-StoryID scenario was wrongly dropped: %v", ids)
	}
}
