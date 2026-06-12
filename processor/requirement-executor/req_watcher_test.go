package requirementexecutor

import "testing"

// TestSelectReqBranchBase pins the branch-derivation precedence that is the
// whole point of the DependsOn-driven fix: an orchestrator-resolved base (the
// dependent's prerequisite ref) must win over the plan base and HEAD, so a
// dependent requirement forks from its prereqs' work instead of the plan base.
// A regression here silently re-introduces the parallel-shared-file assembly
// conflict that motivated the fix.
func TestSelectReqBranchBase(t *testing.T) {
	tests := []struct {
		name       string
		planBranch string
		baseBranch string
		want       string
	}{
		{
			name: "no plan, no base -> HEAD (DAG root, non-GitHub plan)",
			want: "HEAD",
		},
		{
			name:       "plan base only -> plan base (DAG root, GitHub plan)",
			planBranch: "semspec/plan-demo",
			want:       "semspec/plan-demo",
		},
		{
			name:       "resolved base wins over plan base (dependent forks from prereq)",
			planBranch: "semspec/plan-demo",
			baseBranch: "semspec/requirement-a1",
			want:       "semspec/requirement-a1",
		},
		{
			name:       "resolved base wins over empty plan base",
			baseBranch: "semspec/reqbase-d1",
			want:       "semspec/reqbase-d1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := selectReqBranchBase(tt.planBranch, tt.baseBranch); got != tt.want {
				t.Errorf("selectReqBranchBase(%q, %q) = %q, want %q",
					tt.planBranch, tt.baseBranch, got, tt.want)
			}
		})
	}
}
