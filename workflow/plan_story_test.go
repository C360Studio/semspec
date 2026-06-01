package workflow

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateStoryDAG(t *testing.T) {
	cases := []struct {
		name      string
		stories   []Story
		wantErr   error
		errPhrase string
	}{
		{
			name: "empty set is valid",
		},
		{
			name: "linear chain is valid",
			stories: []Story{
				{ID: "s1"},
				{ID: "s2", DependsOn: []string{"s1"}},
				{ID: "s3", DependsOn: []string{"s2"}},
			},
		},
		{
			name: "diamond is valid",
			stories: []Story{
				{ID: "s1"},
				{ID: "s2", DependsOn: []string{"s1"}},
				{ID: "s3", DependsOn: []string{"s1"}},
				{ID: "s4", DependsOn: []string{"s2", "s3"}},
			},
		},
		{
			name:      "self-dependency rejected",
			stories:   []Story{{ID: "s1", DependsOn: []string{"s1"}}},
			wantErr:   ErrInvalidStoryDAG,
			errPhrase: "depends on itself",
		},
		{
			name: "orphan reference rejected",
			stories: []Story{
				{ID: "s1", DependsOn: []string{"ghost"}},
			},
			wantErr:   ErrInvalidStoryDAG,
			errPhrase: "unknown story",
		},
		{
			name: "cycle rejected",
			stories: []Story{
				{ID: "s1", DependsOn: []string{"s2"}},
				{ID: "s2", DependsOn: []string{"s1"}},
			},
			wantErr:   ErrInvalidStoryDAG,
			errPhrase: "cycle",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateStoryDAG(tc.stories)
			if tc.wantErr == nil {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got %v, want errors.Is sentinel %v", err, tc.wantErr)
			}
			if tc.errPhrase != "" && err != nil && !strings.Contains(err.Error(), tc.errPhrase) {
				t.Errorf("error message %q missing phrase %q", err.Error(), tc.errPhrase)
			}
		})
	}
}

func TestValidateStory(t *testing.T) {
	cases := []struct {
		name      string
		story     Story
		wantErr   error
		errPhrase string
	}{
		{
			name:  "minimal pending story (Sarah in flight) is valid",
			story: Story{ID: "s1", RequirementID: "r1", Title: "T", Status: StoryStatusPending},
		},
		{
			name:  "empty status (freshly generated) is treated as pending",
			story: Story{ID: "s1", RequirementID: "r1", Title: "T"},
		},
		{
			name:      "missing ID rejected",
			story:     Story{RequirementID: "r1", Title: "T"},
			wantErr:   ErrInvalidStoryStructure,
			errPhrase: "missing ID",
		},
		{
			name:      "missing requirement_id rejected",
			story:     Story{ID: "s1", Title: "T"},
			wantErr:   ErrInvalidStoryStructure,
			errPhrase: "missing requirement_id",
		},
		{
			name:      "missing title rejected",
			story:     Story{ID: "s1", RequirementID: "r1", Title: "  "},
			wantErr:   ErrInvalidStoryStructure,
			errPhrase: "missing title",
		},
		{
			name: "ready story without files_owned rejected",
			story: Story{
				ID: "s1", RequirementID: "r1", Title: "T", Status: StoryStatusReady,
				Tasks: []Task{{ID: "t1", StoryID: "s1", Description: "x"}},
			},
			wantErr:   ErrInvalidStoryStructure,
			errPhrase: "empty files_owned",
		},
		{
			name: "ready story with docs-only files_owned rejected",
			story: Story{
				ID: "s1", RequirementID: "r1", Title: "T", Status: StoryStatusReady,
				FilesOwned: []string{"README.md", "docs/coverage.md"},
				Tasks:      []Task{{ID: "t1", StoryID: "s1", Description: "x"}},
			},
			wantErr:   ErrInvalidStoryStructure,
			errPhrase: "only documentation files",
		},
		{
			name: "ready story without tasks rejected",
			story: Story{
				ID: "s1", RequirementID: "r1", Title: "T", Status: StoryStatusReady,
				FilesOwned: []string{"src/x.go"},
			},
			wantErr:   ErrInvalidStoryStructure,
			errPhrase: "empty tasks",
		},
		{
			name: "ready story with source + companion doc + tasks is valid",
			story: Story{
				ID: "s1", RequirementID: "r1", Title: "T", Status: StoryStatusReady,
				FilesOwned: []string{"src/x.go", "README.md"},
				Tasks:      []Task{{ID: "t1", StoryID: "s1", Description: "x"}},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateStory(tc.story)
			if tc.wantErr == nil {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got %v, want sentinel %v", err, tc.wantErr)
			}
			if tc.errPhrase != "" && err != nil && !strings.Contains(err.Error(), tc.errPhrase) {
				t.Errorf("error message %q missing phrase %q", err.Error(), tc.errPhrase)
			}
		})
	}
}

func TestValidateTask(t *testing.T) {
	cases := []struct {
		name      string
		parent    string
		task      Task
		wantErr   error
		errPhrase string
	}{
		{
			name:   "minimal task is valid",
			parent: "s1",
			task:   Task{ID: "t1", StoryID: "s1", Description: "x"},
		},
		{
			name:      "missing ID rejected",
			parent:    "s1",
			task:      Task{StoryID: "s1", Description: "x"},
			wantErr:   ErrInvalidTaskStructure,
			errPhrase: "missing ID",
		},
		{
			name:      "wrong parent rejected",
			parent:    "s1",
			task:      Task{ID: "t1", StoryID: "s2", Description: "x"},
			wantErr:   ErrInvalidTaskStructure,
			errPhrase: `nested under story "s1"`,
		},
		{
			name:      "empty description rejected",
			parent:    "s1",
			task:      Task{ID: "t1", StoryID: "s1", Description: "  "},
			wantErr:   ErrInvalidTaskStructure,
			errPhrase: "missing description",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateTask(tc.parent, tc.task)
			if tc.wantErr == nil {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got %v, want sentinel %v", err, tc.wantErr)
			}
			if tc.errPhrase != "" && err != nil && !strings.Contains(err.Error(), tc.errPhrase) {
				t.Errorf("error message %q missing phrase %q", err.Error(), tc.errPhrase)
			}
		})
	}
}

func TestValidateTaskDAG(t *testing.T) {
	cases := []struct {
		name      string
		parent    string
		tasks     []Task
		wantErr   error
		errPhrase string
	}{
		{
			name:   "linear chain is valid",
			parent: "s1",
			tasks: []Task{
				{ID: "t1"},
				{ID: "t2", DependsOn: []string{"t1"}},
				{ID: "t3", DependsOn: []string{"t2"}},
			},
		},
		{
			name:      "self-dep rejected",
			parent:    "s1",
			tasks:     []Task{{ID: "t1", DependsOn: []string{"t1"}}},
			wantErr:   ErrInvalidTaskDAG,
			errPhrase: "depends on itself",
		},
		{
			name:      "orphan dep rejected",
			parent:    "s1",
			tasks:     []Task{{ID: "t1", DependsOn: []string{"ghost"}}},
			wantErr:   ErrInvalidTaskDAG,
			errPhrase: "unknown task",
		},
		{
			name:   "cycle rejected",
			parent: "s1",
			tasks: []Task{
				{ID: "t1", DependsOn: []string{"t2"}},
				{ID: "t2", DependsOn: []string{"t1"}},
			},
			wantErr:   ErrInvalidTaskDAG,
			errPhrase: "cycle",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateTaskDAG(tc.parent, tc.tasks)
			if tc.wantErr == nil {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got %v, want sentinel %v", err, tc.wantErr)
			}
			if tc.errPhrase != "" && err != nil && !strings.Contains(err.Error(), tc.errPhrase) {
				t.Errorf("error message %q missing phrase %q", err.Error(), tc.errPhrase)
			}
		})
	}
}

// TestValidateStoriesAggregate exercises the whole-set validator: per-story
// structural rules, cross-story DAG rules, and intra-story Task DAG rules
// all funnel through a single happy-path call and a few targeted failures.
func TestValidateStoriesAggregate(t *testing.T) {
	good := []Story{
		{ID: "s1", RequirementID: "r1", Title: "A", Status: StoryStatusReady,
			FilesOwned: []string{"src/a.go"},
			Tasks: []Task{
				{ID: "t1", StoryID: "s1", Description: "tests"},
				{ID: "t2", StoryID: "s1", Description: "impl", DependsOn: []string{"t1"}},
			}},
		{ID: "s2", RequirementID: "r1", Title: "B", Status: StoryStatusReady,
			FilesOwned: []string{"src/b.go"},
			DependsOn:  []string{"s1"},
			Tasks: []Task{
				{ID: "t3", StoryID: "s2", Description: "tests"},
			}},
	}
	if err := ValidateStories(good); err != nil {
		t.Fatalf("expected good set to validate: %v", err)
	}

	storyDAGBad := append([]Story(nil), good...)
	storyDAGBad[0].DependsOn = []string{"s2"} // creates s1↔s2 cycle
	if err := ValidateStories(storyDAGBad); !errors.Is(err, ErrInvalidStoryDAG) {
		t.Errorf("story DAG error not surfaced: %v", err)
	}

	taskDAGBad := []Story{
		{ID: "s1", RequirementID: "r1", Title: "A", Status: StoryStatusReady,
			FilesOwned: []string{"src/a.go"},
			Tasks: []Task{
				{ID: "t1", StoryID: "s1", Description: "x", DependsOn: []string{"t2"}},
				{ID: "t2", StoryID: "s1", Description: "y", DependsOn: []string{"t1"}},
			}},
	}
	if err := ValidateStories(taskDAGBad); !errors.Is(err, ErrInvalidTaskDAG) {
		t.Errorf("task DAG error not surfaced: %v", err)
	}

	storyStructBad := []Story{
		{ID: "s1", RequirementID: "r1", Title: "A", Status: StoryStatusReady,
			FilesOwned: []string{"README.md"}, // docs-only
			Tasks:      []Task{{ID: "t1", StoryID: "s1", Description: "x"}}},
	}
	if err := ValidateStories(storyStructBad); !errors.Is(err, ErrInvalidStoryStructure) {
		t.Errorf("story struct error not surfaced: %v", err)
	}
}

func TestValidateCapabilityCoverage(t *testing.T) {
	cases := []struct {
		name       string
		exp        *Exploration
		components []ComponentDef
		wantErr    error
		errPhrase  string
	}{
		{
			name: "nil exploration is valid",
		},
		{
			name: "empty exploration is valid",
			exp:  &Exploration{},
		},
		{
			name:       "no components is valid (pre-architecture phase)",
			exp:        &Exploration{Capabilities: []Capability{{Name: "auth"}}},
			components: nil,
		},
		{
			name: "every capability covered is valid",
			exp: &Exploration{Capabilities: []Capability{
				{Name: "auth"}, {Name: "session"},
			}},
			components: []ComponentDef{
				{Name: "auth-service", Capabilities: []string{"auth"}},
				{Name: "session-store", Capabilities: []string{"session"}},
			},
		},
		{
			name: "single component covers multiple capabilities is valid",
			exp: &Exploration{Capabilities: []Capability{
				{Name: "auth"}, {Name: "session"},
			}},
			components: []ComponentDef{
				{Name: "auth-and-session", Capabilities: []string{"auth", "session"}},
			},
		},
		{
			name: "unresolved capability rejected",
			exp: &Exploration{Capabilities: []Capability{
				{Name: "auth"}, {Name: "session"},
			}},
			components: []ComponentDef{
				{Name: "auth-service", Capabilities: []string{"auth"}},
			},
			wantErr:   ErrInvalidStoryStructure,
			errPhrase: `capability "session" has no component`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateCapabilityCoverage(tc.exp, tc.components)
			if tc.wantErr == nil {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got %v, want sentinel %v", err, tc.wantErr)
			}
			if tc.errPhrase != "" && err != nil && !strings.Contains(err.Error(), tc.errPhrase) {
				t.Errorf("error message %q missing phrase %q", err.Error(), tc.errPhrase)
			}
		})
	}
}

func TestValidateComponentImplementationFiles(t *testing.T) {
	cases := []struct {
		name       string
		components []ComponentDef
		wantErr    error
		errPhrase  string
	}{
		{
			name:       "empty set is valid (pre-PR2 plans)",
			components: nil,
		},
		{
			name: "all components have source files + capabilities",
			components: []ComponentDef{
				{Name: "a", ImplementationFiles: []string{"src/a.go"}, Capabilities: []string{"feature-a"}},
				{Name: "b", ImplementationFiles: []string{"src/b.go", "README.md"}, Capabilities: []string{"feature-b"}},
			},
		},
		{
			name: "unnamed component skipped (separate validator)",
			components: []ComponentDef{
				{Name: "", ImplementationFiles: nil},
				{Name: "a", ImplementationFiles: []string{"src/a.go"}, Capabilities: []string{"feature-a"}},
			},
		},
		{
			name: "empty files rejected",
			components: []ComponentDef{
				{Name: "a"},
			},
			wantErr:   ErrInvalidStoryStructure,
			errPhrase: "empty implementation_files",
		},
		{
			name: "docs-only files rejected",
			components: []ComponentDef{
				{Name: "a", ImplementationFiles: []string{"docs/x.md", "README.md"}},
			},
			wantErr:   ErrInvalidStoryStructure,
			errPhrase: "only documentation files",
		},
		{
			name: "empty capabilities rejected",
			components: []ComponentDef{
				{Name: "a", ImplementationFiles: []string{"src/a.go"}, Capabilities: nil},
			},
			wantErr:   ErrInvalidStoryStructure,
			errPhrase: "empty capabilities",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateComponentImplementationFiles(tc.components)
			if tc.wantErr == nil {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got %v, want sentinel %v", err, tc.wantErr)
			}
			if tc.errPhrase != "" && err != nil && !strings.Contains(err.Error(), tc.errPhrase) {
				t.Errorf("error message %q missing phrase %q", err.Error(), tc.errPhrase)
			}
		})
	}
}
