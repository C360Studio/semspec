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
			story: Story{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "comp-a", Title: "T", Status: StoryStatusPending},
		},
		{
			name: "empty status (Sarah signed off via omitempty) — readiness invariants apply",
			story: Story{
				ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "comp-a", Title: "T",
				FilesOwned: []string{"src/a.go"},
				Tasks:      []Task{{ID: "t1", StoryID: "s1", Description: "impl"}},
			},
		},
		{
			name:      "empty status + empty files_owned rejected (Train D — Pass-3 S-C1 / Pass-4 P4-C4)",
			story:     Story{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "comp-a", Title: "T"},
			wantErr:   ErrInvalidStoryStructure,
			errPhrase: "empty files_owned",
		},
		{
			name: "empty status + empty tasks rejected (Train D — Pass-3 S-C1 / Pass-4 P4-C4)",
			story: Story{
				ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "comp-a", Title: "T",
				FilesOwned: []string{"src/a.go"},
			},
			wantErr:   ErrInvalidStoryStructure,
			errPhrase: "empty tasks",
		},
		{
			name: "empty status + docs-only files_owned rejected (Train D — Pass-3 S-C1 / Pass-4 P4-C4)",
			story: Story{
				ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "comp-a", Title: "T",
				FilesOwned: []string{"docs/notes.md"},
				Tasks:      []Task{{ID: "t1", StoryID: "s1", Description: "impl"}},
			},
			wantErr:   ErrInvalidStoryStructure,
			errPhrase: "only documentation files",
		},
		{
			name:      "missing ID rejected",
			story:     Story{RequirementIDs: []string{"r1"}, ComponentName: "comp-a", Title: "T"},
			wantErr:   ErrInvalidStoryStructure,
			errPhrase: "missing ID",
		},
		{
			name:      "missing requirement_ids rejected (ADR-044)",
			story:     Story{ID: "s1", ComponentName: "comp-a", Title: "T"},
			wantErr:   ErrInvalidStoryStructure,
			errPhrase: "missing requirement_ids",
		},
		{
			name:      "missing component_name rejected (ADR-044)",
			story:     Story{ID: "s1", RequirementIDs: []string{"r1"}, Title: "T"},
			wantErr:   ErrInvalidStoryStructure,
			errPhrase: "missing component_name",
		},
		{
			name:      "missing title rejected",
			story:     Story{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "comp-a", Title: "  "},
			wantErr:   ErrInvalidStoryStructure,
			errPhrase: "missing title",
		},
		{
			name: "ready story without files_owned rejected",
			story: Story{
				ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "comp-a", Title: "T", Status: StoryStatusReady,
				Tasks: []Task{{ID: "t1", StoryID: "s1", Description: "x"}},
			},
			wantErr:   ErrInvalidStoryStructure,
			errPhrase: "empty files_owned",
		},
		{
			name: "ready story with docs-only files_owned rejected",
			story: Story{
				ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "comp-a", Title: "T", Status: StoryStatusReady,
				FilesOwned: []string{"README.md", "docs/coverage.md"},
				Tasks:      []Task{{ID: "t1", StoryID: "s1", Description: "x"}},
			},
			wantErr:   ErrInvalidStoryStructure,
			errPhrase: "only documentation files",
		},
		{
			name: "ready story without tasks rejected",
			story: Story{
				ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "comp-a", Title: "T", Status: StoryStatusReady,
				FilesOwned: []string{"src/x.go"},
			},
			wantErr:   ErrInvalidStoryStructure,
			errPhrase: "empty tasks",
		},
		{
			name: "ready story with source + companion doc + tasks is valid",
			story: Story{
				ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "comp-a", Title: "T", Status: StoryStatusReady,
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
		{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "comp-a", Title: "A", Status: StoryStatusReady,
			FilesOwned: []string{"src/a.go"},
			Tasks: []Task{
				{ID: "t1", StoryID: "s1", Description: "tests"},
				{ID: "t2", StoryID: "s1", Description: "impl", DependsOn: []string{"t1"}},
			}},
		{ID: "s2", RequirementIDs: []string{"r1"}, ComponentName: "comp-b", Title: "B", Status: StoryStatusReady,
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
		{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "comp-a", Title: "A", Status: StoryStatusReady,
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
		{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "comp-a", Title: "A", Status: StoryStatusReady,
			FilesOwned: []string{"README.md"}, // docs-only
			Tasks:      []Task{{ID: "t1", StoryID: "s1", Description: "x"}}},
	}
	if err := ValidateStories(storyStructBad); !errors.Is(err, ErrInvalidStoryStructure) {
		t.Errorf("story struct error not surfaced: %v", err)
	}

	// Aggregate hook for file-ownership: two stories share a file but neither
	// depends on the other. ValidateStories must surface
	// ErrInvalidStoryFileOwnership (not the per-Story / DAG error classes).
	fileRace := []Story{
		{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "comp-a", Title: "A", Status: StoryStatusReady,
			FilesOwned: []string{"src/x.go"},
			Tasks:      []Task{{ID: "t1", StoryID: "s1", Description: "tests"}}},
		{ID: "s2", RequirementIDs: []string{"r2"}, ComponentName: "comp-b", Title: "B", Status: StoryStatusReady,
			FilesOwned: []string{"src/x.go"}, // same file, no depends_on edge
			Tasks:      []Task{{ID: "t2", StoryID: "s2", Description: "tests"}}},
	}
	if err := ValidateStories(fileRace); !errors.Is(err, ErrInvalidStoryFileOwnership) {
		t.Errorf("file-ownership race error not surfaced by ValidateStories: %v", err)
	}
}

// TestValidateStoryFileOwnership covers ADR-043 issue #88: when two Stories
// share a file in FilesOwned, the DependsOn DAG must sequence them — otherwise
// the scenario-orchestrator's parallel dispatch races on the shared file.
//
// Smoke 9 (2026-06-02 hybrid-gpt5 mavlink-hard, plan ebe27a10f9e4) had
// stories 1.1, 2.1, 3.1 all owning the same 7-file MAVSDK driver set with
// 2.1 and 3.1 both depending only on 1.1 — at dispatch time, 2.1 and 3.1
// would race-write UnmannedSystem.java. The aborted run never reached that
// point, but this validator now catches the shape at plan-time.
func TestValidateStoryFileOwnership(t *testing.T) {
	t.Run("empty input is valid", func(t *testing.T) {
		if err := ValidateStoryFileOwnership(nil); err != nil {
			t.Errorf("nil stories should validate: %v", err)
		}
		if err := ValidateStoryFileOwnership([]Story{}); err != nil {
			t.Errorf("empty stories should validate: %v", err)
		}
	})

	t.Run("single story validates", func(t *testing.T) {
		stories := []Story{
			{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "comp-a", Title: "A",
				FilesOwned: []string{"src/x.go"}},
		}
		if err := ValidateStoryFileOwnership(stories); err != nil {
			t.Errorf("single story should validate: %v", err)
		}
	})

	t.Run("disjoint files validate", func(t *testing.T) {
		stories := []Story{
			{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "comp-a", FilesOwned: []string{"src/a.go"}},
			{ID: "s2", RequirementIDs: []string{"r2"}, ComponentName: "comp-b", FilesOwned: []string{"src/b.go"}},
		}
		if err := ValidateStoryFileOwnership(stories); err != nil {
			t.Errorf("disjoint files should validate: %v", err)
		}
	})

	t.Run("shared file with direct dependency validates", func(t *testing.T) {
		stories := []Story{
			{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "comp-a", FilesOwned: []string{"src/x.go"}},
			{ID: "s2", RequirementIDs: []string{"r2"}, ComponentName: "comp-b", FilesOwned: []string{"src/x.go"},
				DependsOn: []string{"s1"}},
		}
		if err := ValidateStoryFileOwnership(stories); err != nil {
			t.Errorf("shared file with direct dependency should validate: %v", err)
		}
	})

	t.Run("shared file with transitive dependency validates", func(t *testing.T) {
		// s1 → s2 → s3 ; s1 and s3 share a file but s3 transitively
		// depends on s1 via s2. Safe to dispatch serially.
		stories := []Story{
			{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "comp-a", FilesOwned: []string{"src/x.go"}},
			{ID: "s2", RequirementIDs: []string{"r2"}, ComponentName: "comp-b", FilesOwned: []string{"src/y.go"},
				DependsOn: []string{"s1"}},
			{ID: "s3", RequirementIDs: []string{"r3"}, ComponentName: "comp-c", FilesOwned: []string{"src/x.go"},
				DependsOn: []string{"s2"}},
		}
		if err := ValidateStoryFileOwnership(stories); err != nil {
			t.Errorf("transitive dependency should validate: %v", err)
		}
	})

	t.Run("shared file without dependency fails", func(t *testing.T) {
		stories := []Story{
			{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "comp-a", FilesOwned: []string{"src/x.go"}},
			{ID: "s2", RequirementIDs: []string{"r2"}, ComponentName: "comp-b", FilesOwned: []string{"src/x.go"}},
		}
		err := ValidateStoryFileOwnership(stories)
		if !errors.Is(err, ErrInvalidStoryFileOwnership) {
			t.Fatalf("expected ErrInvalidStoryFileOwnership, got %v", err)
		}
		if !strings.Contains(err.Error(), "src/x.go") {
			t.Errorf("error should name the shared file: %v", err)
		}
		if !strings.Contains(err.Error(), "s1") || !strings.Contains(err.Error(), "s2") {
			t.Errorf("error should name both stories: %v", err)
		}
	})

	t.Run("siblings sharing files via same parent fail", func(t *testing.T) {
		// This is the smoke-9 shape: s2 and s3 both depend_on s1, share a
		// file, but do NOT depend on each other. Parallel dispatch would race.
		stories := []Story{
			{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "comp-a", FilesOwned: []string{"src/base.go"}},
			{ID: "s2", RequirementIDs: []string{"r2"}, ComponentName: "comp-b", FilesOwned: []string{"src/shared.go"},
				DependsOn: []string{"s1"}},
			{ID: "s3", RequirementIDs: []string{"r3"}, ComponentName: "comp-c", FilesOwned: []string{"src/shared.go"},
				DependsOn: []string{"s1"}},
		}
		err := ValidateStoryFileOwnership(stories)
		if !errors.Is(err, ErrInvalidStoryFileOwnership) {
			t.Fatalf("expected smoke-9 shape to fail validation, got %v", err)
		}
	})

	t.Run("multiple shared files all surface in error", func(t *testing.T) {
		stories := []Story{
			{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "comp-a",
				FilesOwned: []string{"src/x.go", "src/y.go", "src/z.go"}},
			{ID: "s2", RequirementIDs: []string{"r2"}, ComponentName: "comp-b",
				FilesOwned: []string{"src/x.go", "src/y.go"}},
		}
		err := ValidateStoryFileOwnership(stories)
		if !errors.Is(err, ErrInvalidStoryFileOwnership) {
			t.Fatalf("expected error, got %v", err)
		}
		// Both shared files should appear in the message (sorted).
		if !strings.Contains(err.Error(), "src/x.go") || !strings.Contains(err.Error(), "src/y.go") {
			t.Errorf("error should list all shared files: %v", err)
		}
	})

	t.Run("path normalization catches equivalent spellings", func(t *testing.T) {
		// `src/x.go` and `./src/x.go` should normalize to the same path
		// and the overlap should be detected.
		stories := []Story{
			{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "comp-a", FilesOwned: []string{"src/x.go"}},
			{ID: "s2", RequirementIDs: []string{"r2"}, ComponentName: "comp-b", FilesOwned: []string{"./src/x.go"}},
		}
		err := ValidateStoryFileOwnership(stories)
		if !errors.Is(err, ErrInvalidStoryFileOwnership) {
			t.Fatalf("non-canonical paths should still detect overlap, got %v", err)
		}
	})

	t.Run("diamond closure validates", func(t *testing.T) {
		// Diamond:
		//   s1 ──→ s2 ──→ s4
		//    └──→ s3 ────┘
		// s1 and s4 share a file; s4 transitively depends on s1 via both
		// s2 and s3. Reachability through either path satisfies the check.
		stories := []Story{
			{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "comp-a", FilesOwned: []string{"src/x.go"}},
			{ID: "s2", RequirementIDs: []string{"r2"}, ComponentName: "comp-b", FilesOwned: []string{"src/a.go"},
				DependsOn: []string{"s1"}},
			{ID: "s3", RequirementIDs: []string{"r3"}, ComponentName: "comp-c", FilesOwned: []string{"src/b.go"},
				DependsOn: []string{"s1"}},
			{ID: "s4", RequirementIDs: []string{"r4"}, ComponentName: "comp-d", FilesOwned: []string{"src/x.go"},
				DependsOn: []string{"s2", "s3"}},
		}
		if err := ValidateStoryFileOwnership(stories); err != nil {
			t.Errorf("diamond closure should validate s1↔s4 via s2 OR s3: %v", err)
		}
	})

	t.Run("empty FilesOwned does not trip validation", func(t *testing.T) {
		// ValidateStory enforces non-empty FilesOwned on signed-off Stories,
		// but ValidateStoryFileOwnership is called with raw input — including
		// possibly pending stories. Empty FilesOwned must be treated as "no
		// overlap candidate" and pass through this validator silently.
		stories := []Story{
			{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "comp-a", FilesOwned: nil},
			{ID: "s2", RequirementIDs: []string{"r2"}, ComponentName: "comp-b", FilesOwned: []string{"src/x.go"}},
		}
		if err := ValidateStoryFileOwnership(stories); err != nil {
			t.Errorf("empty FilesOwned should pass file-ownership check: %v", err)
		}
	})
}

func TestResolveCapabilityIndices(t *testing.T) {
	exp := &Exploration{Capabilities: []Capability{
		{Name: "mavsdk-lifecycle"}, {Name: "typed-control-streams"}, {Name: "raw-mavlink-io"},
	}}

	t.Run("indices resolve to canonical names (mismatch-proof)", func(t *testing.T) {
		comps := []ComponentDef{{Name: "driver", CapabilityIndices: []int{0, 2}}}
		if err := ResolveCapabilityIndices(exp, comps); err != nil {
			t.Fatal(err)
		}
		got := comps[0].Capabilities
		if len(got) != 2 || got[0] != "mavsdk-lifecycle" || got[1] != "raw-mavlink-io" {
			t.Fatalf("got %v, want canonical names from indices", got)
		}
		// And coverage now sees canonical names — no paraphrase possible.
		comps2 := []ComponentDef{{Name: "driver", CapabilityIndices: []int{0, 1, 2}}}
		_ = ResolveCapabilityIndices(exp, comps2)
		if err := ValidateCapabilityCoverage(exp, comps2); err != nil {
			t.Fatalf("full index coverage should pass: %v", err)
		}
	})

	t.Run("out-of-range index rejected", func(t *testing.T) {
		comps := []ComponentDef{{Name: "driver", CapabilityIndices: []int{0, 5}}}
		err := ResolveCapabilityIndices(exp, comps)
		if err == nil || !errors.Is(err, ErrInvalidStoryStructure) {
			t.Fatalf("expected out-of-range rejection, got %v", err)
		}
	})

	t.Run("dedups repeated indices", func(t *testing.T) {
		comps := []ComponentDef{{Name: "driver", CapabilityIndices: []int{1, 1, 1}}}
		_ = ResolveCapabilityIndices(exp, comps)
		if len(comps[0].Capabilities) != 1 {
			t.Fatalf("expected dedup to 1, got %v", comps[0].Capabilities)
		}
	})

	t.Run("no indices leaves authored Capabilities untouched (back-compat)", func(t *testing.T) {
		comps := []ComponentDef{{Name: "driver", Capabilities: []string{"legacy-name"}}}
		if err := ResolveCapabilityIndices(exp, comps); err != nil {
			t.Fatal(err)
		}
		if len(comps[0].Capabilities) != 1 || comps[0].Capabilities[0] != "legacy-name" {
			t.Fatalf("back-compat authored names should be preserved, got %v", comps[0].Capabilities)
		}
	})
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
			errPhrase: `UNCOVERED (add each to some component's capabilities list): session`,
		},
		{
			name: "multiple uncovered are ALL reported (complete hint, not first-only)",
			exp: &Exploration{Capabilities: []Capability{
				{Name: "osh-mavsdk-driver"}, {Name: "cs-api-telemetry"},
				{Name: "cs-api-control"}, {Name: "raw-mavlink-fallback"},
			}},
			components: []ComponentDef{
				{Name: "driver", Capabilities: []string{"cs-api-control"}},
			},
			wantErr: ErrInvalidStoryStructure,
			// all three uncovered named + full declared set + current mapping
			errPhrase: "osh-mavsdk-driver, cs-api-telemetry, raw-mavlink-fallback",
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
			name: "no capability (neither indices nor names) rejected",
			components: []ComponentDef{
				{Name: "a", ImplementationFiles: []string{"src/a.go"}, Capabilities: nil},
			},
			wantErr:   ErrInvalidStoryStructure,
			errPhrase: "empty capability_indices",
		},
		{
			name: "capability via indices (no names yet) is accepted",
			components: []ComponentDef{
				{Name: "a", ImplementationFiles: []string{"src/a.go"}, CapabilityIndices: []int{0}},
			},
			wantErr: nil,
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

// TestValidateUpstreamImports pins the deterministic non-gameable backstop: a
// code-symbol API whose import is missing OR lacks a package qualifier is
// rejected (the 2026-06-07 bare-"System" wedge + the empty-import recovery loop),
// while qualified imports and non-code-symbol kinds pass.
func TestValidateUpstreamImports(t *testing.T) {
	mk := func(kind, imp string) []UpstreamResolution {
		return []UpstreamResolution{{Name: "MAVSDK", APIs: []APISurface{{Symbol: "System", Kind: kind, Import: imp}}}}
	}
	cases := []struct {
		name      string
		res       []UpstreamResolution
		wantError bool
	}{
		{"bare import on class rejected", mk("class", "System"), true},
		{"qualified java import ok", mk("class", "io.mavsdk.System"), false},
		{"qualified go import ok", mk("type", "github.com/foo/bar.Client"), false},
		{"qualified c++ import ok", mk("class", "mavsdk::System"), false},
		{"empty import on code symbol rejected (hard gate 2026-06-07)", mk("class", ""), true},
		{"empty import on non-code-symbol kind ok", mk("message", ""), false},
		{"empty import on config_field ok", mk("config_field", ""), false},
		{"bare import on non-code-symbol kind ok", mk("message", "HEARTBEAT"), false},
		{"bare import on config_field ok", mk("config_field", "timeout_ms"), false},
		{"nil resolutions ok", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateUpstreamImports(tc.res)
			if tc.wantError && err == nil {
				t.Errorf("expected rejection, got nil")
			}
			if !tc.wantError && err != nil {
				t.Errorf("expected ok, got %v", err)
			}
		})
	}
}
