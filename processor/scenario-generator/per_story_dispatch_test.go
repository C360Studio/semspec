package scenariogenerator

import (
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
)

// TestBuildStoryScopedRequest pins the wire-shape contract for ADR-043
// PR 4j per-Story dispatch: the ScenarioGeneratorRequest carries every
// Story field Bob's prompt needs (StoryID, StoryTitle, StoryIntent,
// StoryFilesOwned, StoryComponents) alongside the existing Requirement
// context.
func TestBuildStoryScopedRequest(t *testing.T) {
	plan := &workflow.Plan{Slug: "x", Goal: "G", Context: "C"}
	req := workflow.Requirement{ID: "req.x.1", Title: "T", Description: "D"}
	story := workflow.Story{
		ID:             "story.x.1.1",
		RequirementIDs: []string{"req.x.1"},
		ComponentName:  "comp-a",
		Title:          "Story title",
		Intent:         "Story intent",
		FilesOwned:     []string{"src/a.go", "src/a_test.go"},
	}
	required := []payloads.RequiredTier{{Tag: "@unit"}}

	got := buildStoryScopedRequest(plan, req, story, required, "arch-ctx")

	if got.StoryID != "story.x.1.1" {
		t.Errorf("StoryID = %q, want story.x.1.1", got.StoryID)
	}
	if got.StoryTitle != "Story title" {
		t.Errorf("StoryTitle = %q, want 'Story title'", got.StoryTitle)
	}
	if got.StoryIntent != "Story intent" {
		t.Errorf("StoryIntent = %q, want 'Story intent'", got.StoryIntent)
	}
	if len(got.StoryFilesOwned) != 2 || got.StoryFilesOwned[0] != "src/a.go" {
		t.Errorf("StoryFilesOwned = %v, want [src/a.go src/a_test.go]", got.StoryFilesOwned)
	}
	if got.StoryComponentName != "comp-a" {
		t.Errorf("StoryComponentName = %q, want comp-a", got.StoryComponentName)
	}
	// Requirement context still travels alongside Story context.
	if got.RequirementID != "req.x.1" {
		t.Errorf("RequirementID = %q, want req.x.1", got.RequirementID)
	}
	if got.PlanGoal != "G" {
		t.Errorf("PlanGoal = %q, want G", got.PlanGoal)
	}
	if got.ArchitectureContext != "arch-ctx" {
		t.Errorf("ArchitectureContext = %q, want arch-ctx", got.ArchitectureContext)
	}
}

// TestBuildRequirementScopedRequest_NoStoryFields pins the legacy
// back-compat path: per-Requirement dispatch emits a payload with all
// StoryXxx fields empty.
func TestBuildRequirementScopedRequest_NoStoryFields(t *testing.T) {
	plan := &workflow.Plan{Slug: "x"}
	req := workflow.Requirement{ID: "req.x.1"}
	required := []payloads.RequiredTier{{Tag: "@unit"}}

	got := buildRequirementScopedRequest(plan, req, required, "")

	if got.StoryID != "" {
		t.Errorf("legacy dispatch should leave StoryID empty, got %q", got.StoryID)
	}
	if got.StoryTitle != "" || got.StoryIntent != "" || len(got.StoryFilesOwned) != 0 || got.StoryComponentName != "" {
		t.Errorf("legacy dispatch should leave all Story fields empty; got %+v", got)
	}
	if got.RequirementID != "req.x.1" {
		t.Errorf("RequirementID = %q, want req.x.1", got.RequirementID)
	}
}

// TestBuildStoryScopedRequest_FilesOwnedCloned guards against aliasing —
// if a caller mutates Story.FilesOwned after build, the captured payload
// must NOT change. (ADR-044: ComponentName is a string, so it's
// value-copied; no aliasing risk.)
func TestBuildStoryScopedRequest_FilesOwnedCloned(t *testing.T) {
	files := []string{"src/a.go"}
	story := workflow.Story{ID: "story.x.1.1", FilesOwned: files, ComponentName: "comp-a"}
	got := buildStoryScopedRequest(&workflow.Plan{}, workflow.Requirement{}, story, nil, "")

	files[0] = "src/MUTATED.go"

	if got.StoryFilesOwned[0] != "src/a.go" {
		t.Errorf("StoryFilesOwned aliased: got %q after caller mutated source slice", got.StoryFilesOwned[0])
	}
	if got.StoryComponentName != "comp-a" {
		t.Errorf("StoryComponentName = %q, want comp-a", got.StoryComponentName)
	}
}
