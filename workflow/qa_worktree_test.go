package workflow

import "testing"

func TestQAWorktreeID(t *testing.T) {
	tests := []struct {
		name string
		slug string
		want string
	}{
		{"simple", "auth", "qa-auth"},
		{"hyphenated", "mavlink-hard", "qa-mavlink-hard"},
		{"numeric", "feature-2", "qa-feature-2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := QAWorktreeID(tt.slug); got != tt.want {
				t.Errorf("QAWorktreeID(%q) = %q, want %q", tt.slug, got, tt.want)
			}
		})
	}
}

// TestQAWorktreeID_Deterministic guards the property the data-plane fix relies
// on: plan-manager, qa-reviewer, and the sandbox unit runner must derive the
// SAME worktree id from the slug, or they would inspect different trees.
func TestQAWorktreeID_Deterministic(t *testing.T) {
	if QAWorktreeID("plan-x") != QAWorktreeID("plan-x") {
		t.Fatal("QAWorktreeID is not deterministic for the same slug")
	}
	if QAWorktreeID("a") == QAWorktreeID("b") {
		t.Fatal("QAWorktreeID collides across distinct slugs")
	}
}
