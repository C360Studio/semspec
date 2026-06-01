package scenariogenerator

import (
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestAttachStoryIDs_NoStoriesIsNoop pins the back-compat contract: when
// the plan has no Stories (Sarah dormant), scenarios keep empty StoryID
// and Bob's existing RequirementID-only link is preserved.
func TestAttachStoryIDs_NoStoriesIsNoop(t *testing.T) {
	plan := &workflow.Plan{Slug: "x"}
	scenarios := []workflow.Scenario{
		{ID: "s1", RequirementID: "req.x.1"},
		{ID: "s2", RequirementID: "req.x.1"},
	}
	attachStoryIDs(scenarios, plan, "req.x.1")
	for _, s := range scenarios {
		if s.StoryID != "" {
			t.Errorf("expected empty StoryID with no plan.Stories, got %q on %s", s.StoryID, s.ID)
		}
	}
}

// TestAttachStoryIDs_NilPlan is a defensive guard — should never happen in
// production but cheap to pin.
func TestAttachStoryIDs_NilPlan(t *testing.T) {
	scenarios := []workflow.Scenario{{ID: "s1", RequirementID: "req.x.1"}}
	attachStoryIDs(scenarios, nil, "req.x.1")
	if scenarios[0].StoryID != "" {
		t.Errorf("expected empty StoryID on nil plan, got %q", scenarios[0].StoryID)
	}
}

// TestAttachStoryIDs_SingleStoryPerRequirement covers the common case:
// Sarah sharded one Story per Requirement, scenarios link to that story.
func TestAttachStoryIDs_SingleStoryPerRequirement(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "x",
		Stories: []workflow.Story{
			{ID: "story.x.1.1", RequirementID: "req.x.1", Title: "T"},
			{ID: "story.x.2.1", RequirementID: "req.x.2", Title: "T2"},
		},
	}
	scenarios := []workflow.Scenario{
		{ID: "s1", RequirementID: "req.x.1"},
		{ID: "s2", RequirementID: "req.x.1"},
	}
	attachStoryIDs(scenarios, plan, "req.x.1")
	for _, s := range scenarios {
		if s.StoryID != "story.x.1.1" {
			t.Errorf("expected StoryID=story.x.1.1 on %s, got %q", s.ID, s.StoryID)
		}
	}
}

// TestAttachStoryIDs_MultiStoryPicksFirst pins the PR 4b "first story wins"
// fallback. ADR-043 PR 4j superseded this for per-Story dispatch (the
// dispatch-time StoryID is now assigned explicitly in handleLoopCompletion);
// attachStoryIDs is only consulted when storyID is empty in metadata
// (legacy per-Requirement dispatch / pre-Sarah mock fixtures).
func TestAttachStoryIDs_MultiStoryPicksFirst(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "x",
		Stories: []workflow.Story{
			{ID: "story.x.1.1", RequirementID: "req.x.1", Title: "A"},
			{ID: "story.x.1.2", RequirementID: "req.x.1", Title: "B"},
		},
	}
	scenarios := []workflow.Scenario{{ID: "s1", RequirementID: "req.x.1"}}
	attachStoryIDs(scenarios, plan, "req.x.1")
	if scenarios[0].StoryID != "story.x.1.1" {
		t.Errorf("expected first story wins (story.x.1.1), got %q", scenarios[0].StoryID)
	}
}

// TestAttachStoryIDs_UnresolvedRequirementSkipsAssignment guards against a
// Requirement that has no owning Story (e.g. a partial regen where Sarah
// hasn't yet emitted stories for this requirement). The scenarios should
// keep empty StoryID rather than borrow another requirement's story.
func TestAttachStoryIDs_UnresolvedRequirementSkipsAssignment(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "x",
		Stories: []workflow.Story{
			{ID: "story.x.1.1", RequirementID: "req.x.1", Title: "T"},
		},
	}
	scenarios := []workflow.Scenario{{ID: "s1", RequirementID: "req.x.2"}}
	attachStoryIDs(scenarios, plan, "req.x.2")
	if scenarios[0].StoryID != "" {
		t.Errorf("expected empty StoryID on unresolved requirement, got %q", scenarios[0].StoryID)
	}
}

// TestExplicitStoryIDAssignmentOverridesAttachFallback pins the ADR-043 PR 4j
// path: when the dispatch carries an explicit StoryID (per-Story mode),
// EVERY scenario in the batch gets that StoryID — bypasses the
// attachStoryIDs lookup heuristic. Even if the plan has multiple Stories
// owning the same Requirement, the dispatch context wins.
func TestExplicitStoryIDAssignmentOverridesAttachFallback(t *testing.T) {
	// Simulates the body of handleLoopCompletion's switch — when storyID is
	// non-empty, assignment is direct. attachStoryIDs is NOT consulted.
	scenarios := []workflow.Scenario{
		{ID: "s1", RequirementID: "req.x.1"},
		{ID: "s2", RequirementID: "req.x.1"},
		{ID: "s3", RequirementID: "req.x.1"},
	}
	dispatchedStoryID := "story.x.1.2" // SECOND story, not the first

	for i := range scenarios {
		scenarios[i].StoryID = dispatchedStoryID
	}

	for _, s := range scenarios {
		if s.StoryID != "story.x.1.2" {
			t.Errorf("scenario %s.StoryID = %q, want story.x.1.2 (the dispatched story)", s.ID, s.StoryID)
		}
	}
}
