package requirementexecutor

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestBuildReviewPrompt_PerStoryScopeFiltersOtherStoriesScenarios is the
// headline regression test for go-reviewer Pass-1 finding C5.
//
// Pre-fix, buildReviewPrompt iterated exec.Scenarios unfiltered while
// buildRequirementReviewContext (the system message) used the per-Story
// scoped subset. For Story-1 of a multi-Story requirement, the reviewer's
// user prompt asked it to verify scenarios from Stories 2 and 3 — which
// the developer never authored. The reviewer then rejected the dev work
// for "missing" scenarios. The bug was invisible in smoke 6 because
// every fixture had exactly one Story per Requirement.
//
// Post-fix, the caller (dispatchRequirementReviewerLocked) resolves
// scopeScenariosToCurrentStory ONCE and passes the same scoped list to
// both buildReviewPrompt and buildRequirementReviewContext — guaranteeing
// the prompt and context agree on the verdict surface.
func TestBuildReviewPrompt_PerStoryScopeFiltersOtherStoriesScenarios(t *testing.T) {
	c := &Component{}
	exec := &requirementExecution{
		Title: "multi-Story req",
		Scenarios: []workflow.Scenario{
			{ID: "scn.story1.1", Given: "g", When: "w", Then: []string{"t"}, Tags: []string{workflow.TierUnit}, StoryID: "story.demo.1.1"},
			{ID: "scn.story2.1", Given: "g", When: "w", Then: []string{"t"}, Tags: []string{workflow.TierUnit}, StoryID: "story.demo.1.2"},
			{ID: "scn.story3.1", Given: "g", When: "w", Then: []string{"t"}, Tags: []string{workflow.TierUnit}, StoryID: "story.demo.1.3"},
		},
		SortedStoryIDs:  []string{"story.demo.1.1", "story.demo.1.2", "story.demo.1.3"},
		CurrentStoryIdx: 0, // currently reviewing Story 1
	}

	scoped := scopeScenariosToCurrentStory(exec)
	prompt := c.buildReviewPrompt(exec, scoped)

	// Story 1 scenario must appear; Stories 2 and 3 must NOT.
	if !strings.Contains(prompt, "scn.story1.1") {
		t.Errorf("Story 1 reviewer prompt missing its own scenario scn.story1.1; got:\n%s", prompt)
	}
	if strings.Contains(prompt, "scn.story2.1") {
		t.Errorf("Story 1 reviewer prompt LEAKED Story 2 scenario scn.story2.1 — pre-fix C5 shape; got:\n%s", prompt)
	}
	if strings.Contains(prompt, "scn.story3.1") {
		t.Errorf("Story 1 reviewer prompt LEAKED Story 3 scenario scn.story3.1 — pre-fix C5 shape; got:\n%s", prompt)
	}
}

// TestBuildReviewPrompt_LegacyNoStoriesShowsAllScenarios pins the
// back-compat semantic: when SortedStoryIDs is empty (pre-Sarah plans /
// mock fixtures), scopeScenariosToCurrentStory returns exec.Scenarios
// unfiltered — and the reviewer prompt shows every scenario, matching
// pre-ADR-043 behavior.
func TestBuildReviewPrompt_LegacyNoStoriesShowsAllScenarios(t *testing.T) {
	c := &Component{}
	exec := &requirementExecution{
		Title: "legacy req",
		Scenarios: []workflow.Scenario{
			{ID: "scn.legacy.1", Given: "g", When: "w", Then: []string{"t"}, Tags: []string{workflow.TierUnit}},
			{ID: "scn.legacy.2", Given: "g", When: "w", Then: []string{"t"}, Tags: []string{workflow.TierUnit}},
		},
		// SortedStoryIDs empty — legacy mode.
	}

	scoped := scopeScenariosToCurrentStory(exec)
	prompt := c.buildReviewPrompt(exec, scoped)

	if !strings.Contains(prompt, "scn.legacy.1") {
		t.Errorf("legacy reviewer prompt missing scn.legacy.1; got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "scn.legacy.2") {
		t.Errorf("legacy reviewer prompt missing scn.legacy.2; got:\n%s", prompt)
	}
}
