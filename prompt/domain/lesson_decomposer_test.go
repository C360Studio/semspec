package domain

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/prompt"
)

func TestRenderLessonDecomposerPrompt_FullContext(t *testing.T) {
	got := renderLessonDecomposerPrompt(&prompt.LessonDecomposerPromptContext{
		Verdict:         "rejected",
		Feedback:        "Tests cover happy path only; nil case missing.",
		Source:          "execution-manager",
		TargetRole:      "developer",
		DeveloperLoopID: "dev-loop-abc",
		ReviewerLoopID:  "rev-loop-xyz",
		DeveloperSteps: []prompt.TrajectoryStepSummary{
			{Index: 0, Summary: "tool_call(bash) → ok"},
			{Index: 1, Summary: "tool_call(bash) → ok"},
			{Index: 12, Summary: "tool_call(submit_work) → ok"},
		},
		ReviewerSteps: []prompt.TrajectoryStepSummary{
			{Index: 5, Summary: "model_call(claude-sonnet)"},
		},
		Scenario: &prompt.DecomposerScenarioContext{
			ID:    "scn-42",
			Given: "an empty input list",
			When:  "the function is called",
			Then:  []string{"it returns nil", "it logs nothing"},
		},
		FilesModified:       []string{"pkg/foo/handler.go", "pkg/foo/handler_test.go"},
		WorktreeDiffSummary: "M pkg/foo/handler.go\nM pkg/foo/handler_test.go",
		CommitSHA:           "deadbeef",
		ExistingLessons: []string{
			"Skipping nil-checks for slice receivers",
			"Tests asserting on implementation rather than behaviour",
		},
		CategoryCatalog: []string{
			"missing_tests: Required tests not written",
			"sop_violation: SOP rule not followed",
		},
	})

	mustContain := []string{
		"## Incident",
		"rejected rejection",
		"execution-manager",
		`"developer"`,

		"## Reviewer Feedback (verbatim)",
		"Tests cover happy path only",
		"Do not parrot this back",

		"## Scenario the work was supposed to satisfy",
		"**ID:** scn-42",
		"**Given:** an empty input list",
		"**When:** the function is called",
		"  - it returns nil",
		"  - it logs nothing",

		"## Developer Trajectory (loop dev-loop-abc)",
		"- [0] tool_call(bash)",
		"- [12] tool_call(submit_work)",

		"## Reviewer Trajectory (loop rev-loop-xyz)",
		"- [5] model_call(claude-sonnet)",

		"## Worktree State at Rejection",
		"M pkg/foo/handler.go",

		"## Files Modified by Developer",
		"- pkg/foo/handler.go",
		"- pkg/foo/handler_test.go",

		"## Available Error Categories",
		"- missing_tests:",
		"- sop_violation:",

		"## Existing Lessons for the developer role",
		"- Skipping nil-checks for slice receivers",

		"## Commit SHA",
		"deadbeef",

		"## Your Task",
		"Produce ONE Lesson via submit_work",
		"Cite at least one of `evidence_steps`",
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Errorf("rendered prompt missing %q\n--- prompt ---\n%s", want, got)
		}
	}
}

func TestRenderLessonDecomposerPrompt_MinimalContext(t *testing.T) {
	got := renderLessonDecomposerPrompt(&prompt.LessonDecomposerPromptContext{
		Verdict:         "rejected",
		Feedback:        "fail",
		DeveloperLoopID: "dev-loop-abc",
	})

	// Must still produce a usable prompt without optional sections.
	mustContain := []string{"## Incident", "## Reviewer Feedback", "## Your Task"}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Errorf("minimal prompt missing %q", want)
		}
	}

	// No scenario / steps / categories / lessons → those sections must NOT
	// render. Otherwise the prompt advertises sections the agent expects
	// content under.
	mustNotContain := []string{
		"## Scenario the work was supposed to satisfy",
		"## Worktree State at Rejection",
		"## Files Modified by Developer",
		"## Available Error Categories",
		"## Existing Lessons",
		"## Commit SHA",
		"## Reviewer Trajectory",
	}
	for _, dont := range mustNotContain {
		if strings.Contains(got, dont) {
			t.Errorf("minimal prompt should not contain %q\n--- prompt ---\n%s", dont, got)
		}
	}

	// Defaults applied when Source/TargetRole are empty.
	if !strings.Contains(got, "execution-manager") {
		t.Error("Source default to execution-manager when empty")
	}
	if !strings.Contains(got, `"developer"`) {
		t.Error("TargetRole default to developer when empty")
	}
}

func TestRenderLessonDecomposerPrompt_TrajectoryUnavailableMessage(t *testing.T) {
	// When DeveloperLoopID is set but no steps were captured, render a
	// degraded-mode notice instead of a blank Trajectory section.
	got := renderLessonDecomposerPrompt(&prompt.LessonDecomposerPromptContext{
		Verdict:         "rejected",
		Feedback:        "fail",
		DeveloperLoopID: "dev-loop-missing",
	})

	if !strings.Contains(got, "trajectory could not be retrieved") {
		t.Errorf("expected degraded-mode notice, got:\n%s", got)
	}
	if strings.Contains(got, "Cite step indices in") {
		t.Error("degraded mode must not advertise step citations the prompt cannot back up")
	}
}

func TestRenderLessonDecomposerPrompt_NoLoopAtAll(t *testing.T) {
	// Edge case: legacy/forwarded payload with neither developer nor
	// reviewer loop. The renderer must not output a phantom "(loop )" header.
	got := renderLessonDecomposerPrompt(&prompt.LessonDecomposerPromptContext{
		Verdict:  "rejected",
		Feedback: "fail",
	})
	if strings.Contains(got, "## Developer Trajectory") {
		t.Errorf("no developer loop ID should suppress Trajectory section, got:\n%s", got)
	}
	if strings.Contains(got, "(loop )") {
		t.Error("no phantom (loop ) header allowed")
	}
}

func TestLessonDecomposerFragments_RegisterUnique(t *testing.T) {
	// Registry panics on duplicate user-prompt registration. Confirm the
	// decomposer's user-prompt fragment lands cleanly when Software() is
	// registered fresh.
	r := prompt.NewRegistry()
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("Software() registration panicked: %v", rec)
		}
	}()
	r.RegisterAll(Software()...)

	if got := r.UserPromptFragmentFor(prompt.RoleLessonDecomposer); got == nil {
		t.Fatal("expected user-prompt fragment for RoleLessonDecomposer")
	} else if got.ID != "software.lesson-decomposer.user-prompt" {
		t.Errorf("user-prompt fragment ID = %q", got.ID)
	}
}

func TestLessonDecomposerFragments_RoleScoping(t *testing.T) {
	// All decomposer fragments must be gated to RoleLessonDecomposer only —
	// otherwise developer/reviewer prompts would leak Detail/InjectionForm
	// guidance that doesn't apply to them.
	for _, f := range lessonDecomposerFragments() {
		if len(f.Roles) == 0 {
			t.Errorf("fragment %q has no role gating", f.ID)
			continue
		}
		for _, role := range f.Roles {
			if role != prompt.RoleLessonDecomposer {
				t.Errorf("fragment %q targets role %q, want only RoleLessonDecomposer", f.ID, role)
			}
		}
	}
}

func TestLessonDecomposerFragments_Categories(t *testing.T) {
	// Sanity-check that the fragment set covers the four required slots:
	// system-base, role-context, user-prompt, output-format.
	wantCategories := map[prompt.Category]string{
		prompt.CategorySystemBase:   "system-base",
		prompt.CategoryRoleContext:  "role-context",
		prompt.CategoryUserPrompt:   "user-prompt",
		prompt.CategoryOutputFormat: "output-format",
	}
	gotCategories := make(map[prompt.Category]bool)
	for _, f := range lessonDecomposerFragments() {
		gotCategories[f.Category] = true
	}
	for cat, name := range wantCategories {
		if !gotCategories[cat] {
			t.Errorf("missing fragment with category %s (%v)", name, cat)
		}
	}
}

func TestRenderLessonDecomposerPrompt_PartialScenario(t *testing.T) {
	// Scenario with only Then assertions still renders cleanly (no orphan
	// "Given:" / "When:" labels).
	got := renderLessonDecomposerPrompt(&prompt.LessonDecomposerPromptContext{
		Verdict:  "rejected",
		Feedback: "x",
		Scenario: &prompt.DecomposerScenarioContext{
			Then: []string{"the response status is 200"},
		},
	})
	if !strings.Contains(got, "## Scenario the work was supposed to satisfy") {
		t.Error("expected scenario section when Then is non-empty")
	}
	if strings.Contains(got, "**Given:**") {
		t.Error("Given label should not render when Given is empty")
	}
	if strings.Contains(got, "**When:**") {
		t.Error("When label should not render when When is empty")
	}
}
