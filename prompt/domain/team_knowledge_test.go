package domain

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/prompt"
)

// teamKnowledgeFragment finds the "software.shared.team-knowledge" fragment
// from Software(); fails the test if it isn't there. Avoids depending on
// fragment ordering in the slice.
func teamKnowledgeFragment(t *testing.T) *prompt.Fragment {
	t.Helper()
	for _, f := range Software() {
		if f.ID == "software.shared.team-knowledge" {
			return f
		}
	}
	t.Fatal("team-knowledge fragment not in Software()")
	return nil
}

func TestTeamKnowledge_RendersInjectionFormWhenSet(t *testing.T) {
	frag := teamKnowledgeFragment(t)
	ctx := &prompt.AssemblyContext{
		LessonsLearned: &prompt.LessonsLearned{
			Lessons: []prompt.LessonEntry{
				{
					Category:      "decomposer",
					Summary:       "Reviewer flagged that tests don't cover the failure path.",
					InjectionForm: "Always run go test ./... before submit_work; per-package builds miss broken consumers.",
					Role:          "developer",
				},
			},
		},
	}
	got := frag.ContentFunc(ctx)
	if !strings.Contains(got, "Always run go test ./...") {
		t.Errorf("InjectionForm should render preferentially:\n%s", got)
	}
	if strings.Contains(got, "Reviewer flagged") {
		t.Error("Summary leaked into output when InjectionForm was set")
	}
}

func TestTeamKnowledge_FallsBackToSummary(t *testing.T) {
	// Direct-write producers (plan-reviewer, qa-reviewer, structural) ship
	// lessons with no InjectionForm — the fragment must fall back to
	// Summary cleanly.
	frag := teamKnowledgeFragment(t)
	ctx := &prompt.AssemblyContext{
		LessonsLearned: &prompt.LessonsLearned{
			Lessons: []prompt.LessonEntry{
				{
					Category: "plan-review",
					Summary:  "Goal not specific enough.",
					Role:     "planner",
				},
			},
		},
	}
	got := frag.ContentFunc(ctx)
	if !strings.Contains(got, "Goal not specific enough") {
		t.Errorf("Summary fallback missing:\n%s", got)
	}
	if !strings.Contains(got, "[AVOID][planner]") {
		t.Error("Role tag missing")
	}
}

func TestTeamKnowledge_RendersGuidanceAlongsideAdvice(t *testing.T) {
	frag := teamKnowledgeFragment(t)
	ctx := &prompt.AssemblyContext{
		LessonsLearned: &prompt.LessonsLearned{
			Lessons: []prompt.LessonEntry{
				{
					Category:      "decomposer",
					InjectionForm: "Run validators after every change.",
					Role:          "developer",
					Guidance:      "See SOP 3.1 for the validator command list.",
				},
			},
		},
	}
	got := frag.ContentFunc(ctx)
	if !strings.Contains(got, "Run validators after every change") {
		t.Errorf("InjectionForm missing:\n%s", got)
	}
	if !strings.Contains(got, "GUIDANCE: See SOP 3.1") {
		t.Errorf("Guidance missing:\n%s", got)
	}
}

func TestTeamKnowledge_ConditionGate(t *testing.T) {
	frag := teamKnowledgeFragment(t)
	cases := []struct {
		name string
		ctx  *prompt.AssemblyContext
		want bool
	}{
		{"nil LessonsLearned", &prompt.AssemblyContext{}, false},
		{"empty Lessons", &prompt.AssemblyContext{LessonsLearned: &prompt.LessonsLearned{}}, false},
		{"populated", &prompt.AssemblyContext{LessonsLearned: &prompt.LessonsLearned{
			Lessons: []prompt.LessonEntry{{Summary: "x", Role: "developer"}},
		}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := frag.Condition(c.ctx); got != c.want {
				t.Errorf("Condition = %v, want %v", got, c.want)
			}
		})
	}
}
