package workflow

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateStoriesAgainstPlan(t *testing.T) {
	plan := &Plan{
		Architecture: &ArchitectureDocument{ComponentBoundaries: []ComponentDef{
			{Name: "core", ImplementationFiles: []string{"src/main/java/Foo.java", "build.gradle"}},
			{Name: "edge", ImplementationFiles: []string{"src/main/java/Bar.java"}},
		}},
		Requirements: []Requirement{{ID: "requirement.x.1"}, {ID: "requirement.x.2"}},
	}
	// base returns a single coherent story; mods tweaks it per case.
	base := func(mods func(*Story)) []Story {
		s := Story{
			ID:             "story.x.1.1",
			ComponentName:  "core",
			RequirementIDs: []string{"requirement.x.1"},
			FilesOwned:     []string{"src/main/java/Foo.java"},
		}
		if mods != nil {
			mods(&s)
		}
		return []Story{s}
	}

	tests := []struct {
		name      string
		plan      *Plan
		stories   []Story
		wantErr   bool
		wantInErr string
	}{
		{name: "coherent story passes", plan: plan, stories: base(nil)},
		{name: "companion test path counts as owned", plan: plan, stories: base(func(s *Story) {
			s.FilesOwned = []string{"src/test/java/FooTest.java"}
		})},
		{name: "unresolved component", plan: plan, stories: base(func(s *Story) {
			s.ComponentName = "ghost"
		}), wantErr: true, wantInErr: "unresolved_component"},
		{name: "requirement orphan", plan: plan, stories: base(func(s *Story) {
			s.RequirementIDs = []string{"requirement.x.99"}
		}), wantErr: true, wantInErr: "requirement_orphan"},
		{name: "files owned outside component", plan: plan, stories: base(func(s *Story) {
			s.FilesOwned = []string{"src/main/java/Other.java"}
		}), wantErr: true, wantInErr: "files_owned_outside_component"},
		{name: "pending story is skipped even when it would violate", plan: plan, stories: base(func(s *Story) {
			s.ComponentName = "ghost"
			s.Status = StoryStatusPending
		})},
		{name: "no architecture is permissive", plan: &Plan{Requirements: plan.Requirements}, stories: base(func(s *Story) {
			s.ComponentName = "ghost"
			s.FilesOwned = []string{"anything.go"}
		})},
		{name: "no requirements is permissive", plan: &Plan{Architecture: plan.Architecture}, stories: base(func(s *Story) {
			s.RequirementIDs = []string{"requirement.x.99"}
		})},
		{name: "cross-component file widening is rejected", plan: plan, stories: base(func(s *Story) {
			s.FilesOwned = []string{"src/main/java/Bar.java"} // owned by edge, not core
		}), wantErr: true, wantInErr: "files_owned_outside_component"},
		{name: "build-manifest owned by the component passes (topology deferred to R2)", plan: plan, stories: base(func(s *Story) {
			s.FilesOwned = []string{"build.gradle"}
		})},
		{name: "multi-story: per-story isolation flags the orphaned one", plan: plan, stories: []Story{
			{ID: "story.x.1.1", ComponentName: "core", RequirementIDs: []string{"requirement.x.1"}, FilesOwned: []string{"src/main/java/Foo.java"}},
			{ID: "story.x.2.1", ComponentName: "edge", RequirementIDs: []string{"requirement.x.99"}, FilesOwned: []string{"src/main/java/Bar.java"}},
		}, wantErr: true, wantInErr: "requirement_orphan"},
		{name: "multi-story: all coherent passes", plan: plan, stories: []Story{
			{ID: "story.x.1.1", ComponentName: "core", RequirementIDs: []string{"requirement.x.1"}, FilesOwned: []string{"src/main/java/Foo.java"}},
			{ID: "story.x.2.1", ComponentName: "edge", RequirementIDs: []string{"requirement.x.2"}, FilesOwned: []string{"src/main/java/Bar.java"}},
		}},
		{name: "nil plan", plan: nil, stories: base(nil)},
		{name: "empty stories", plan: plan, stories: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStoriesAgainstPlan(tt.plan, tt.stories)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("expected nil, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !errors.Is(err, ErrInvalidStoryStructure) {
				t.Errorf("error should wrap ErrInvalidStoryStructure: %v", err)
			}
			if tt.wantInErr != "" && !strings.Contains(err.Error(), tt.wantInErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantInErr)
			}
		})
	}
}
