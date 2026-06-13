package workflow

import (
	"reflect"
	"testing"
)

// TestResolveRequirementBranchPrereqs pins the translation at the heart of the
// branch-derivation fix: a requirement's branch must derive from the union of
// its own semantic prereqs AND the owners of the Story.DependsOn edges (which
// carry the Pass-2 file-overlap serialization that never reaches
// Requirement.DependsOn) — all mapped to OWNER branches under M:N.
func TestResolveRequirementBranchPrereqs(t *testing.T) {
	tests := []struct {
		name    string
		req     Requirement
		stories []Story
		want    []string
	}{
		{
			name:    "no deps, no stories -> empty",
			req:     Requirement{ID: "r1"},
			stories: nil,
			want:    nil,
		},
		{
			name: "requirement semantic dep, uncovered -> itself",
			req:  Requirement{ID: "r2", DependsOn: []string{"r1"}},
			// no stories cover r1, so ownerRequirementFor(r1)=r1
			want: []string{"r1"},
		},
		{
			// THE LOAD-BEARING CASE: story-B depends_on story-A (a Pass-2 shared-
			// README edge). req b1's branch must derive from story-A's OWNER (a1),
			// even though Requirement.DependsOn is empty.
			name: "story file-overlap edge -> prereq story owner",
			req:  Requirement{ID: "b1"},
			stories: []Story{
				{ID: "story.A", RequirementIDs: []string{"a1", "a2"}, DependsOn: nil},
				{ID: "story.B", RequirementIDs: []string{"b1", "b2"}, DependsOn: []string{"story.A"}},
			},
			want: []string{"a1"}, // DeterministicStoryOwner(story.A) = min(a1,a2) = a1
		},
		{
			// A requirement-level dep that points at a NON-owner req (a2) must
			// resolve to that req's covering-story owner (a1), since a2's branch
			// is an empty fast-complete.
			name: "requirement dep on non-owner -> owner branch",
			req:  Requirement{ID: "c1", DependsOn: []string{"a2"}},
			stories: []Story{
				{ID: "story.A", RequirementIDs: []string{"a1", "a2"}},
				{ID: "story.C", RequirementIDs: []string{"c1"}},
			},
			want: []string{"a1"},
		},
		{
			// Both sources reaching the same owner de-dupe to one entry.
			name: "union de-dupes",
			req:  Requirement{ID: "b1", DependsOn: []string{"a1"}},
			stories: []Story{
				{ID: "story.A", RequirementIDs: []string{"a1"}},
				{ID: "story.B", RequirementIDs: []string{"b1"}, DependsOn: []string{"story.A"}},
			},
			want: []string{"a1"},
		},
		{
			// Multiple prereqs -> sorted, self excluded.
			name: "multiple prereqs sorted, self excluded",
			req:  Requirement{ID: "d1", DependsOn: []string{"c1", "a1", "d1"}},
			stories: []Story{
				{ID: "story.A", RequirementIDs: []string{"a1"}},
				{ID: "story.C", RequirementIDs: []string{"c1"}},
				{ID: "story.D", RequirementIDs: []string{"d1"}},
			},
			want: []string{"a1", "c1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveRequirementBranchPrereqs(tt.req, tt.stories)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ResolveRequirementBranchPrereqs() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestDependentBranchSubtree pins the reverse branch-derivation closure used by
// the recovery-path invalidation (P3): when a prerequisite re-runs after
// completing, every requirement that (transitively) forked from it must be
// reset and re-derived.
func TestDependentBranchSubtree(t *testing.T) {
	tests := []struct {
		name     string
		reopened string
		reqs     []Requirement
		stories  []Story
		want     []string
	}{
		{
			name:     "no dependents -> empty",
			reopened: "a1",
			reqs:     []Requirement{{ID: "a1"}, {ID: "z1"}},
			want:     nil,
		},
		{
			name:     "linear chain a1<-b1<-c1 -> {b1,c1}",
			reopened: "a1",
			reqs: []Requirement{
				{ID: "a1"},
				{ID: "b1", DependsOn: []string{"a1"}},
				{ID: "c1", DependsOn: []string{"b1"}},
			},
			want: []string{"b1", "c1"},
		},
		{
			name:     "chain reopened in the middle -> only downstream",
			reopened: "b1",
			reqs: []Requirement{
				{ID: "a1"},
				{ID: "b1", DependsOn: []string{"a1"}},
				{ID: "c1", DependsOn: []string{"b1"}},
			},
			want: []string{"c1"}, // a1 is upstream, not invalidated
		},
		{
			name:     "diamond: b1,b2 on a1; d1 on b1,b2 -> {b1,b2,d1}",
			reopened: "a1",
			reqs: []Requirement{
				{ID: "a1"},
				{ID: "b1", DependsOn: []string{"a1"}},
				{ID: "b2", DependsOn: []string{"a1"}},
				{ID: "d1", DependsOn: []string{"b1", "b2"}},
			},
			want: []string{"b1", "b2", "d1"},
		},
		{
			name:     "Story file-overlap edge (only on Story.DependsOn) is followed",
			reopened: "a1",
			reqs:     []Requirement{{ID: "a1"}, {ID: "a2"}, {ID: "b1"}},
			stories: []Story{
				{ID: "story.A", RequirementIDs: []string{"a1", "a2"}},
				{ID: "story.B", RequirementIDs: []string{"b1"}, DependsOn: []string{"story.A"}},
			},
			// b1 derives from owner(story.A)=a1 via the Pass-2 edge -> invalidated.
			want: []string{"b1"},
		},
		{
			name:     "cycle is guarded (no infinite loop)",
			reopened: "a1",
			reqs: []Requirement{
				{ID: "a1"},
				{ID: "b1", DependsOn: []string{"a1", "c1"}},
				{ID: "c1", DependsOn: []string{"b1"}},
			},
			want: []string{"b1", "c1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DependentBranchSubtree(tt.reopened, tt.reqs, tt.stories)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DependentBranchSubtree(%q) = %v, want %v", tt.reopened, got, tt.want)
			}
		})
	}
}

func TestOwnerRequirementFor(t *testing.T) {
	stories := []Story{
		{ID: "story.A", RequirementIDs: []string{"a2", "a1"}}, // owner = a1 (min)
	}
	if got := ownerRequirementFor("a2", stories); got != "a1" {
		t.Errorf("ownerRequirementFor(a2) = %q, want a1 (owner of covering story)", got)
	}
	if got := ownerRequirementFor("z9", stories); got != "z9" {
		t.Errorf("ownerRequirementFor(z9) = %q, want z9 (uncovered -> itself)", got)
	}
}
