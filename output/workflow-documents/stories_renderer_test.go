package workflowdocuments

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestRenderStories_EmptyPlanReturnsEmpty(t *testing.T) {
	if got := RenderStories(nil); got != "" {
		t.Errorf("nil plan: got %q, want empty", got)
	}
	if got := RenderStories(&workflow.Plan{Slug: "p"}); got != "" {
		t.Errorf("plan without stories: got %q, want empty", got)
	}
}

// TestRenderStories_GroupsByRequirement pins the structural shape:
// stories are listed under H2 headings keyed to their parent
// Requirement; each Story block includes title, ID, intent, components,
// files_owned, and tasks. Mirrors RenderScenarios's grouping.
func TestRenderStories_GroupsByRequirement(t *testing.T) {
	plan := &workflow.Plan{
		Slug:  "p",
		Title: "Short title",
		Requirements: []workflow.Requirement{
			{ID: "req.p.1", Title: "Auth"},
			{ID: "req.p.2", Title: "Session"},
		},
		Stories: []workflow.Story{
			{
				ID: "story.p.1.1", RequirementIDs: []string{"req.p.1"}, ComponentName: "auth-service",
				Title:      "Lifecycle",
				Intent:     "Set up the auth lifecycle component.",
				Components: []string{"auth-service"},
				FilesOwned: []string{"src/auth.go", "src/auth_test.go"},
				Tasks: []workflow.Task{
					{ID: "task.p.1.1.1", Description: "write failing test"},
					{ID: "task.p.1.1.2", Description: "implement to pass", DependsOn: []string{"task.p.1.1.1"}},
				},
			},
			{
				ID: "story.p.2.1", RequirementIDs: []string{"req.p.2"}, ComponentName: "session-store",
				Title:      "Wire-up",
				DependsOn:  []string{"story.p.1.1"},
				FilesOwned: []string{"src/session.go"},
				Tasks: []workflow.Task{
					{ID: "task.p.2.1.1", Description: "wire session"},
				},
			},
		},
	}
	got := RenderStories(plan)
	if got == "" {
		t.Fatal("got empty render output for non-empty stories")
	}
	mustContain := []string{
		"# Stories: Short title",
		"**2 stories**",
		"## Auth",
		"`req.p.1` — 1 story(ies)",
		"### Lifecycle",
		"`story.p.1.1`",
		"Set up the auth lifecycle component.",
		"**Components:** auth-service",
		"**Files owned:**",
		"`src/auth.go`",
		"**Tasks:**",
		"`task.p.1.1.1` — write failing test",
		"## Session",
		"### Wire-up",
		"**Depends on:**",
		"`story.p.1.1`",
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in render output", want)
		}
	}
}

// TestRenderStories_OrphanStoriesGetSeparateSection guards the
// defensive fallback. Plan-reviewer R3 should reject Stories with
// empty RequirementID; if one slips through the renderer surfaces it
// under a dedicated heading rather than hiding it.
func TestRenderStories_OrphanStoriesGetSeparateSection(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "p",
		Stories: []workflow.Story{
			{ID: "story.p.x.1", Title: "Orphan"}, // no RequirementID
		},
	}
	got := RenderStories(plan)
	if !strings.Contains(got, "## Unassigned stories") {
		t.Error("orphan story should land under 'Unassigned stories' heading")
	}
}

// TestRenderStories_OverlongTitleFallsBackToSlug pins the smoke 6
// overlong-title behavior — same as the other renderers via the
// shared displayTitle helper.
func TestRenderStories_OverlongTitleFallsBackToSlug(t *testing.T) {
	verbose := strings.Repeat("x", maxDisplayTitleChars+1)
	plan := &workflow.Plan{
		Slug:    "compact-slug",
		Title:   verbose,
		Stories: []workflow.Story{{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "placeholder-component"}},
		Requirements: []workflow.Requirement{
			{ID: "r1", Title: "R1"},
		},
	}
	got := RenderStories(plan)
	if !strings.HasPrefix(got, "# Stories: compact-slug\n") {
		clip := 80
		if len(got) < clip {
			clip = len(got)
		}
		t.Errorf("overlong title should fall back to slug; got first %d chars: %q", clip, got[:clip])
	}
}
