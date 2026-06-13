package planmanager

import (
	"reflect"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestAssemblyBranchOrder pins the owner-only, derivation-ordered enumeration
// that plan-level assembly merges (slice 3 of the branch-derivation fix).
func TestAssemblyBranchOrder(t *testing.T) {
	tests := []struct {
		name     string
		reqs     []workflow.Requirement
		stories  []workflow.Story
		want     []string
		fallback bool
	}{
		{
			name: "no stories -> every requirement, topo by DependsOn (back-compat)",
			reqs: []workflow.Requirement{
				{ID: "r1", DependsOn: []string{"r2"}},
				{ID: "r2"},
				{ID: "r3", DependsOn: []string{"r1"}},
			},
			want: []string{"r2", "r1", "r3"},
		},
		{
			name: "M:N owner-only — non-owner covered reqs excluded",
			reqs: []workflow.Requirement{
				{ID: "a1"}, {ID: "a2"}, {ID: "b1"}, {ID: "b2"},
			},
			stories: []workflow.Story{
				{ID: "story.A", RequirementIDs: []string{"a1", "a2"}},
				{ID: "story.B", RequirementIDs: []string{"b1", "b2"}},
			},
			// owners are a1 (min of a1,a2) and b1 (min of b1,b2); a2/b2 dropped.
			want: []string{"a1", "b1"},
		},
		{
			name: "story file-overlap edge orders owner branches (b derives from a)",
			reqs: []workflow.Requirement{
				{ID: "a1"}, {ID: "a2"}, {ID: "b1"},
			},
			stories: []workflow.Story{
				{ID: "story.A", RequirementIDs: []string{"a1", "a2"}},
				{ID: "story.B", RequirementIDs: []string{"b1"}, DependsOn: []string{"story.A"}},
			},
			// nodes {a1, b1}; b1 derives from owner(a) = a1 -> a1 before b1.
			want: []string{"a1", "b1"},
		},
		{
			name: "story-less requirement mixes with owners",
			reqs: []workflow.Requirement{
				{ID: "a1"}, {ID: "a2"}, {ID: "c1", DependsOn: []string{"a1"}},
			},
			stories: []workflow.Story{
				{ID: "story.A", RequirementIDs: []string{"a1", "a2"}},
				// c1 covered by no story -> its own node; depends on a1.
			},
			want: []string{"a1", "c1"},
		},
		{
			name: "cycle falls back to slice order",
			reqs: []workflow.Requirement{
				{ID: "r1", DependsOn: []string{"r2"}},
				{ID: "r2", DependsOn: []string{"r1"}},
			},
			want:     []string{"r1", "r2"},
			fallback: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, fallback := assemblyBranchOrder(tt.reqs, tt.stories)
			if fallback != tt.fallback {
				t.Errorf("usedFallback = %v, want %v", fallback, tt.fallback)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("assemblyBranchOrder() = %v, want %v", got, tt.want)
			}
		})
	}
}
