package planmanager

import (
	"reflect"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestEnsureScopeCreateCoversStories_AddsMissingPaths pins the load-bearing
// behavior: when a Story.FilesOwned path isn't already in scope.Include or
// scope.Create, the helper appends it to Create. Result is sorted.
func TestEnsureScopeCreateCoversStories_AddsMissingPaths(t *testing.T) {
	scope := workflow.Scope{Include: []string{"existing.go"}}
	stories := []workflow.Story{
		{ID: "s1", FilesOwned: []string{"src/b.go", "src/a.go"}},
		{ID: "s2", FilesOwned: []string{"src/c.go"}},
	}
	got := ensureScopeCreateCoversStories(scope, stories)
	want := []string{"src/a.go", "src/b.go", "src/c.go"}
	if !reflect.DeepEqual(got.Create, want) {
		t.Errorf("Create = %v, want %v", got.Create, want)
	}
	// Include must be unchanged.
	if !reflect.DeepEqual(got.Include, []string{"existing.go"}) {
		t.Errorf("Include = %v, want [existing.go]", got.Include)
	}
}

// TestEnsureScopeCreateCoversStories_SkipsPathsAlreadyInInclude pins the
// "files that exist already don't need creation intent" semantic — the planner
// can keep authoring scope.Include for existing-but-modified files; the
// derivation must not duplicate them as Create entries.
func TestEnsureScopeCreateCoversStories_SkipsPathsAlreadyInInclude(t *testing.T) {
	scope := workflow.Scope{Include: []string{"src/main.go"}}
	stories := []workflow.Story{
		{ID: "s1", FilesOwned: []string{"src/main.go", "src/new.go"}},
	}
	got := ensureScopeCreateCoversStories(scope, stories)
	want := []string{"src/new.go"}
	if !reflect.DeepEqual(got.Create, want) {
		t.Errorf("Create = %v, want %v — src/main.go was already in Include, must NOT be duplicated to Create", got.Create, want)
	}
}

// TestEnsureScopeCreateCoversStories_PreservesExistingCreate pins
// idempotency: re-running the derivation over a scope that already has
// Create entries (from a prior Sarah pass or operator edit) doesn't drop
// them. The function is union-style, not overwrite.
func TestEnsureScopeCreateCoversStories_PreservesExistingCreate(t *testing.T) {
	scope := workflow.Scope{Create: []string{"src/operator-added.go"}}
	stories := []workflow.Story{
		{ID: "s1", FilesOwned: []string{"src/sarah-new.go"}},
	}
	got := ensureScopeCreateCoversStories(scope, stories)
	want := []string{"src/operator-added.go", "src/sarah-new.go"}
	if !reflect.DeepEqual(got.Create, want) {
		t.Errorf("Create = %v, want %v", got.Create, want)
	}
}

// TestEnsureScopeCreateCoversStories_NoStoriesNoChange covers the empty
// Story list case (e.g. mid-flight regen) — Create must stay as it was.
func TestEnsureScopeCreateCoversStories_NoStoriesNoChange(t *testing.T) {
	scope := workflow.Scope{Create: []string{"src/a.go"}}
	got := ensureScopeCreateCoversStories(scope, nil)
	want := []string{"src/a.go"}
	if !reflect.DeepEqual(got.Create, want) {
		t.Errorf("Create = %v, want %v", got.Create, want)
	}
}

// TestEnsureScopeCreateCoversStories_DropsEmptyAndDeduplicates pins
// defensive handling: empty path strings (legacy / drift) are skipped,
// duplicates across stories collapse to one entry.
func TestEnsureScopeCreateCoversStories_DropsEmptyAndDeduplicates(t *testing.T) {
	scope := workflow.Scope{}
	stories := []workflow.Story{
		{ID: "s1", FilesOwned: []string{"src/shared.go", "", "src/a.go"}},
		{ID: "s2", FilesOwned: []string{"src/shared.go"}}, // duplicate of s1
	}
	got := ensureScopeCreateCoversStories(scope, stories)
	want := []string{"src/a.go", "src/shared.go"}
	if !reflect.DeepEqual(got.Create, want) {
		t.Errorf("Create = %v, want %v (empty path skipped, src/shared.go deduplicated)", got.Create, want)
	}
}
