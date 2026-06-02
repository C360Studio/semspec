package planreviewer

import "testing"

// TestPhaseToRole pins the mapping that routes plan-review findings to
// the pipeline role responsible for the offending phase. Lesson-learning
// counts roll up by role, so a missing phase silently misattributes
// failures to "planner" (the default) and the wrong role gets the
// lesson update.
//
// The "stories" case was added with Train D step 3 (P4-C3 — new R2
// re-entry case). Pre-this-commit, story-phase findings landed in the
// default branch and credited Sarah's bugs to the planner role. Caught
// by the pre-commit reviewer of Train D step 4 — same shape as P4-C3.
func TestPhaseToRole(t *testing.T) {
	cases := map[string]string{
		"plan":         "planner",
		"requirements": "requirement-generator",
		"architecture": "architect",
		"stories":      "story-preparer",
		"scenarios":    "scenario-generator",
		"":             "planner", // empty phase falls to default
		"unknown":      "planner", // unrecognized phase falls to default
	}
	for phase, want := range cases {
		got := phaseToRole(phase)
		if got != want {
			t.Errorf("phaseToRole(%q) = %q, want %q", phase, got, want)
		}
	}
}
