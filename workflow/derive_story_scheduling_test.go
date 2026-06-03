package workflow

import (
	"errors"
	"sort"
	"strings"
	"testing"
)

// sortedDeps returns a sorted copy of a story's DependsOn for deterministic
// assertion comparison.
func sortedDeps(s Story) []string {
	if len(s.DependsOn) == 0 {
		return nil
	}
	out := append([]string(nil), s.DependsOn...)
	sort.Strings(out)
	return out
}

func TestDeriveStoryScheduling_Empty(t *testing.T) {
	// Empty input → nil, no-op.
	if err := DeriveStoryScheduling(nil, nil); err != nil {
		t.Fatalf("nil input: unexpected error: %v", err)
	}
	if err := DeriveStoryScheduling([]Story{}, []Requirement{}); err != nil {
		t.Fatalf("empty input: unexpected error: %v", err)
	}
}

func TestDeriveStoryScheduling_Single(t *testing.T) {
	// Single story with no requirements → nil, no DependsOn added.
	stories := []Story{
		{ID: "s1", ComponentName: "comp-a", RequirementIDs: []string{"req.1"},
			FilesOwned: []string{"src/a.go"}},
	}
	reqs := []Requirement{{ID: "req.1"}}
	if err := DeriveStoryScheduling(stories, reqs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stories[0].DependsOn) != 0 {
		t.Errorf("single story DependsOn should be empty, got %v", stories[0].DependsOn)
	}
}

// TestDeriveStoryScheduling_Pass1_AllCoverers is the canonical ADR-044 Pass 1
// case from the spec:
//
//	Reqs:  R1, R2 (no deps); R4 (deps=[R1, R2])
//	Stories:
//	  s1 covers [R1]
//	  s2 covers [R1, R2]      ← overlaps s1 on R1
//	  s4 covers [R4]
//
//	Pass-1 edges for s4 (closure = {R1, R2}):
//	  coverers(R1) = {s1, s2}  → s4.DependsOn ⊇ {s1, s2}
//	  coverers(R2) = {s2}      → s4.DependsOn ⊇ {s2}
//	  Result: s4.DependsOn = {s1, s2}
func TestDeriveStoryScheduling_Pass1_AllCoverers(t *testing.T) {
	reqs := []Requirement{
		{ID: "R1"},
		{ID: "R2"},
		{ID: "R4", DependsOn: []string{"R1", "R2"}},
	}
	stories := []Story{
		{ID: "s1", ComponentName: "comp-a", RequirementIDs: []string{"R1"},
			FilesOwned: []string{"src/a.go"}},
		{ID: "s2", ComponentName: "comp-b", RequirementIDs: []string{"R1", "R2"},
			FilesOwned: []string{"src/b.go"}},
		{ID: "s4", ComponentName: "comp-c", RequirementIDs: []string{"R4"},
			FilesOwned: []string{"src/c.go"}},
	}

	if err := DeriveStoryScheduling(stories, reqs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// s1 and s2 cover no prereqs (R1, R2 have none) → no DependsOn edges.
	if len(stories[0].DependsOn) != 0 {
		t.Errorf("s1 should have no DependsOn, got %v", stories[0].DependsOn)
	}
	if len(stories[1].DependsOn) != 0 {
		t.Errorf("s2 should have no DependsOn, got %v", stories[1].DependsOn)
	}

	// s4 must wait for ALL coverers of R1 and R2.
	deps := sortedDeps(stories[2])
	want := []string{"s1", "s2"}
	sort.Strings(want)
	if len(deps) != len(want) {
		t.Errorf("s4.DependsOn = %v, want %v", deps, want)
		return
	}
	for i, d := range deps {
		if d != want[i] {
			t.Errorf("s4.DependsOn[%d] = %q, want %q", i, d, want[i])
		}
	}
}

func TestDeriveStoryScheduling_Pass1_NoEdgeWhenSameReq(t *testing.T) {
	// Two stories covering the same req with no prereqs — no edges should
	// appear between them from Pass 1 alone.
	reqs := []Requirement{
		{ID: "R1"},
	}
	stories := []Story{
		{ID: "s1", ComponentName: "comp-a", RequirementIDs: []string{"R1"},
			FilesOwned: []string{"src/a.go"}},
		{ID: "s2", ComponentName: "comp-b", RequirementIDs: []string{"R1"},
			FilesOwned: []string{"src/b.go"}},
	}

	if err := DeriveStoryScheduling(stories, reqs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No file overlap, no prereqs → no edges.
	if len(stories[0].DependsOn) != 0 {
		t.Errorf("s1 DependsOn should be empty, got %v", stories[0].DependsOn)
	}
	if len(stories[1].DependsOn) != 0 {
		t.Errorf("s2 DependsOn should be empty, got %v", stories[1].DependsOn)
	}
}

func TestDeriveStoryScheduling_Pass2_SameComponentConflict(t *testing.T) {
	// Two stories anchoring the same component and sharing files → error.
	reqs := []Requirement{{ID: "R1"}, {ID: "R2"}}
	stories := []Story{
		{ID: "s1", ComponentName: "comp-a", RequirementIDs: []string{"R1"},
			FilesOwned: []string{"src/shared.go"}},
		{ID: "s2", ComponentName: "comp-a", RequirementIDs: []string{"R2"},
			FilesOwned: []string{"src/shared.go"}},
	}

	err := DeriveStoryScheduling(stories, reqs)
	if !errors.Is(err, ErrSameComponentFileConflict) {
		t.Fatalf("want ErrSameComponentFileConflict, got %v", err)
	}
	if !strings.Contains(err.Error(), "s1") || !strings.Contains(err.Error(), "s2") {
		t.Errorf("error should name both stories: %v", err)
	}
	if !strings.Contains(err.Error(), "comp-a") {
		t.Errorf("error should name the component: %v", err)
	}
}

func TestDeriveStoryScheduling_Pass2_DifferentComponentSerialize(t *testing.T) {
	// Two stories on different components sharing a file → lower-ID-first
	// serialization edge added.
	reqs := []Requirement{{ID: "R1"}, {ID: "R2"}}
	stories := []Story{
		// Intentionally name s2 first in the slice so order-independence is tested.
		{ID: "s2", ComponentName: "comp-b", RequirementIDs: []string{"R2"},
			FilesOwned: []string{"src/shared.go", "src/b.go"}},
		{ID: "s1", ComponentName: "comp-a", RequirementIDs: []string{"R1"},
			FilesOwned: []string{"src/shared.go", "src/a.go"}},
	}

	if err := DeriveStoryScheduling(stories, reqs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// s1 < s2 lexicographically → s2 depends on s1.
	findStory := func(id string) *Story {
		for i := range stories {
			if stories[i].ID == id {
				return &stories[i]
			}
		}
		return nil
	}
	s1 := findStory("s1")
	s2 := findStory("s2")
	if s1 == nil || s2 == nil {
		t.Fatal("story not found")
	}
	if len(s1.DependsOn) != 0 {
		t.Errorf("s1 should have no DependsOn (it's lower ID), got %v", s1.DependsOn)
	}
	found := false
	for _, d := range s2.DependsOn {
		if d == "s1" {
			found = true
		}
	}
	if !found {
		t.Errorf("s2.DependsOn should contain s1, got %v", s2.DependsOn)
	}
}

// TestDeriveStoryScheduling_Pass3_CyclicPartition is the Codex example from
// ADR-044 §"Pass 3":
//
//	Reqs:  R_A → R_B → R_C   (linear chain)
//	Stories:
//	  s1 covers [R_B]
//	  s2 covers [R_A, R_C]
//
//	Pass-1 edges:
//	  s1.DependsOn ⊇ {s2}   (R_B's prereq R_A is covered by s2)
//	  s2.DependsOn ⊇ {s1}   (R_C's prereq R_B is covered by s1)
//	  → Cycle s1 ↔ s2 → ErrCoveragePartitionCyclic
func TestDeriveStoryScheduling_Pass3_CyclicPartition(t *testing.T) {
	reqs := []Requirement{
		{ID: "R_A"},
		{ID: "R_B", DependsOn: []string{"R_A"}},
		{ID: "R_C", DependsOn: []string{"R_B"}},
	}
	stories := []Story{
		{ID: "s1", ComponentName: "comp-a", RequirementIDs: []string{"R_B"},
			FilesOwned: []string{"src/b.go"}},
		{ID: "s2", ComponentName: "comp-b", RequirementIDs: []string{"R_A", "R_C"},
			FilesOwned: []string{"src/a.go"}},
	}

	err := DeriveStoryScheduling(stories, reqs)
	if !errors.Is(err, ErrCoveragePartitionCyclic) {
		t.Fatalf("want ErrCoveragePartitionCyclic, got %v", err)
	}
	if !strings.Contains(err.Error(), "s1") || !strings.Contains(err.Error(), "s2") {
		t.Errorf("error should name both stories: %v", err)
	}
}

func TestDeriveStoryScheduling_Idempotent(t *testing.T) {
	// Calling DeriveStoryScheduling twice should yield the same DependsOn set
	// with no duplicates.
	reqs := []Requirement{
		{ID: "R1"},
		{ID: "R2"},
		{ID: "R3", DependsOn: []string{"R1", "R2"}},
	}
	stories := []Story{
		{ID: "s1", ComponentName: "comp-a", RequirementIDs: []string{"R1"},
			FilesOwned: []string{"src/a.go"}},
		{ID: "s2", ComponentName: "comp-b", RequirementIDs: []string{"R2"},
			FilesOwned: []string{"src/b.go"}},
		{ID: "s3", ComponentName: "comp-c", RequirementIDs: []string{"R3"},
			FilesOwned: []string{"src/c.go"}},
	}

	if err := DeriveStoryScheduling(stories, reqs); err != nil {
		t.Fatalf("first call: %v", err)
	}
	firstDeps := sortedDeps(stories[2])

	// Second call — must not add duplicates.
	if err := DeriveStoryScheduling(stories, reqs); err != nil {
		t.Fatalf("second call: %v", err)
	}
	secondDeps := sortedDeps(stories[2])

	if len(firstDeps) != len(secondDeps) {
		t.Errorf("idempotence broken: first=%v second=%v", firstDeps, secondDeps)
		return
	}
	for i, d := range firstDeps {
		if d != secondDeps[i] {
			t.Errorf("idempotence broken at [%d]: first=%q second=%q", i, d, secondDeps[i])
		}
	}
}

func TestDeriveStoryScheduling_CrossComponentPrereqNoDoubleEdge(t *testing.T) {
	// Pass 1 orders s1 before s2 via prereq. Pass 2 sees overlap but the
	// pair is already ordered — no double edge should appear.
	//
	// Reqs: R1; R2 (deps=[R1])
	// s1 covers R1, s2 covers R2; same file "src/shared.go".
	// After Pass 1: s2.DependsOn = [s1].
	// Pass 2: alreadyOrdered → skip.
	reqs := []Requirement{
		{ID: "R1"},
		{ID: "R2", DependsOn: []string{"R1"}},
	}
	stories := []Story{
		{ID: "s1", ComponentName: "comp-a", RequirementIDs: []string{"R1"},
			FilesOwned: []string{"src/shared.go"}},
		{ID: "s2", ComponentName: "comp-b", RequirementIDs: []string{"R2"},
			FilesOwned: []string{"src/shared.go"}},
	}

	if err := DeriveStoryScheduling(stories, reqs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	findStory := func(id string) *Story {
		for i := range stories {
			if stories[i].ID == id {
				return &stories[i]
			}
		}
		return nil
	}
	s1 := findStory("s1")
	s2 := findStory("s2")
	if s1 == nil || s2 == nil {
		t.Fatal("story not found")
	}

	if len(s1.DependsOn) != 0 {
		t.Errorf("s1 should have no DependsOn, got %v", s1.DependsOn)
	}
	if len(s2.DependsOn) != 1 || s2.DependsOn[0] != "s1" {
		t.Errorf("s2.DependsOn should be [s1], got %v", s2.DependsOn)
	}
}

func TestDeriveStoryScheduling_Pass2_NoEdgeWhenDisjointFiles(t *testing.T) {
	// Two stories on different components with disjoint files → no edges.
	reqs := []Requirement{{ID: "R1"}, {ID: "R2"}}
	stories := []Story{
		{ID: "s1", ComponentName: "comp-a", RequirementIDs: []string{"R1"},
			FilesOwned: []string{"src/a.go"}},
		{ID: "s2", ComponentName: "comp-b", RequirementIDs: []string{"R2"},
			FilesOwned: []string{"src/b.go"}},
	}

	if err := DeriveStoryScheduling(stories, reqs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stories[0].DependsOn) != 0 {
		t.Errorf("s1 DependsOn should be empty, got %v", stories[0].DependsOn)
	}
	if len(stories[1].DependsOn) != 0 {
		t.Errorf("s2 DependsOn should be empty, got %v", stories[1].DependsOn)
	}
}
