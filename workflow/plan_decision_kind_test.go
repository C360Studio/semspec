package workflow

import "testing"

// TestPlanDecisionKind_IsValid pins the closed enum. Train C step 1
// added PlanDecisionKindStoryReprepare; the IsValid switch must accept
// it so the wire parser at plan-manager/mutations.go's
// handlePlanDecisionAddMutation doesn't reject incoming story_reprepare
// decisions as malformed.
func TestPlanDecisionKind_IsValid(t *testing.T) {
	cases := map[PlanDecisionKind]bool{
		PlanDecisionKindRequirementChange:  true,
		PlanDecisionKindExecutionExhausted: true,
		PlanDecisionKindStoryReprepare:     true,
		PlanDecisionKindArchitectureRevise: true,
		PlanDecisionKindAssemblyConflict:   true,
		PlanDecisionKindScopeIncomplete:    true,
		"":                                 false,
		"not-a-kind":                       false,
		"requirement-change":               false, // hyphen not underscore
	}
	for k, want := range cases {
		got := k.IsValid()
		if got != want {
			t.Errorf("PlanDecisionKind(%q).IsValid() = %v, want %v", k, got, want)
		}
	}
}

// TestPlanDecisionKind_String round-trips the enum's wire shape.
func TestPlanDecisionKind_String(t *testing.T) {
	if got := PlanDecisionKindStoryReprepare.String(); got != "story_reprepare" {
		t.Errorf("PlanDecisionKindStoryReprepare.String() = %q, want story_reprepare", got)
	}
}
