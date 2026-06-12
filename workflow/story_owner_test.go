package workflow

import "testing"

func TestDeterministicStoryOwner(t *testing.T) {
	tests := []struct {
		name   string
		reqIDs []string
		want   string
	}{
		{"single", []string{"r1"}, "r1"},
		{"smallest_wins", []string{"r3", "r1", "r2"}, "r1"},
		// Lexicographic, NOT numeric: "r10" < "r2" because '1' < '2'. Pins the
		// exact tie-break the scenario-orchestrator reservation also relies on.
		{"lexicographic_not_numeric", []string{"r10", "r2"}, "r10"},
		{"empty", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DeterministicStoryOwner(Story{RequirementIDs: tt.reqIDs}); got != tt.want {
				t.Errorf("DeterministicStoryOwner(%v) = %q, want %q", tt.reqIDs, got, tt.want)
			}
		})
	}
}

// TestDeterministicStoryOwner_DoesNotMutateInput guards that owner selection
// doesn't sort the Story's RequirementIDs in place (which would scramble the
// M:N join order downstream).
func TestDeterministicStoryOwner_DoesNotMutateInput(t *testing.T) {
	s := Story{RequirementIDs: []string{"r3", "r1", "r2"}}
	_ = DeterministicStoryOwner(s)
	if s.RequirementIDs[0] != "r3" {
		t.Errorf("input RequirementIDs mutated: %v", s.RequirementIDs)
	}
}
