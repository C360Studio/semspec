package storypreparer

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestParseStoriesFromResult(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "x",
		Requirements: []workflow.Requirement{
			{ID: "requirement.x.1", Title: "First"},
			{ID: "requirement.x.2", Title: "Second"},
		},
	}

	cases := []struct {
		name      string
		input     string
		wantCount int
		wantErr   string
	}{
		{
			name:    "empty result rejected",
			input:   "",
			wantErr: "empty result",
		},
		{
			name:    "invalid JSON rejected",
			input:   "not json",
			wantErr: "parse stories JSON",
		},
		{
			name:    "missing stories key produces empty list — rejected",
			input:   `{}`,
			wantErr: "stories list is empty",
		},
		{
			name:    "empty stories array rejected",
			input:   `{"stories": []}`,
			wantErr: "stories list is empty",
		},
		{
			name:      "stories covering every requirement parse cleanly",
			input:     `{"stories":[{"label":"l1","requirement_index":0,"title":"T1","intent":"i","components":["c"],"files_owned":["src/x.go"],"depends_on_labels":[],"tasks":[{"label":"t1","description":"d","depends_on_labels":[]}]},{"label":"l2","requirement_index":1,"title":"T2","intent":"i","components":["c"],"files_owned":["src/y.go"],"depends_on_labels":[],"tasks":[{"label":"t2","description":"d","depends_on_labels":[]}]}]}`,
			wantCount: 2,
		},
		{
			// Pass-3 S-C2 / Pass-2 C5: Sarah must emit at least one story
			// per requirement. Pre-fix the parser passed on partial output,
			// scenario-generator's legacy fallback engaged for the
			// uncovered req, and execution-manager hard-failed later with
			// "no Stories on plan for requirement %s". The error message
			// names the uncovered requirement so Sarah's retry prompt
			// pinpoints the gap.
			name:    "partial coverage (2 reqs, 1 story) rejected — Pass-3 S-C2",
			input:   `{"stories":[{"label":"l1","requirement_index":0,"title":"T","intent":"i","components":["c"],"files_owned":["src/x.go"],"depends_on_labels":[],"tasks":[{"label":"t1","description":"d","depends_on_labels":[]}]}]}`,
			wantErr: "uncovered: [requirement.x.2]",
		},
		{
			name:    "requirement_index out of range rejected",
			input:   `{"stories":[{"label":"l1","requirement_index":5,"title":"T","intent":"","components":[],"files_owned":[],"depends_on_labels":[],"tasks":[]}]}`,
			wantErr: "requirement_index 5 out of range",
		},
		{
			name:    "missing story label rejected",
			input:   `{"stories":[{"label":"","requirement_index":0,"title":"T","intent":"","components":[],"files_owned":[],"depends_on_labels":[],"tasks":[]}]}`,
			wantErr: "missing label",
		},
		{
			name:    "duplicate story label rejected",
			input:   `{"stories":[{"label":"l1","requirement_index":0,"title":"T1","intent":"","components":[],"files_owned":[],"depends_on_labels":[],"tasks":[]},{"label":"l1","requirement_index":1,"title":"T2","intent":"","components":[],"files_owned":[],"depends_on_labels":[],"tasks":[]}]}`,
			wantErr: `label "l1" appears more than once`,
		},
		{
			name:    "unknown depends_on_labels rejected",
			input:   `{"stories":[{"label":"l1","requirement_index":0,"title":"T","intent":"","components":[],"files_owned":[],"depends_on_labels":["ghost"],"tasks":[]}]}`,
			wantErr: `references unknown label "ghost"`,
		},
		{
			name:    "duplicate task label rejected",
			input:   `{"stories":[{"label":"l1","requirement_index":0,"title":"T","intent":"","components":[],"files_owned":[],"depends_on_labels":[],"tasks":[{"label":"a","description":"d","depends_on_labels":[]},{"label":"a","description":"d","depends_on_labels":[]}]}]}`,
			wantErr: `task label "a" appears more than once`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseStoriesFromResult(tc.input, plan, plan.Slug)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error %q missing phrase %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tc.wantCount {
				t.Errorf("got %d stories, want %d", len(got), tc.wantCount)
			}
		})
	}
}

// TestResolveStoryLabels_LabelDepsRewriteToCanonicalIDs covers the core
// transformation: Sarah's depends_on_labels become canonical Story.ID refs
// in workflow.Story.DependsOn; intra-story task depends_on_labels become
// canonical Task.ID refs.
func TestResolveStoryLabels_LabelDepsRewriteToCanonicalIDs(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "x",
		Requirements: []workflow.Requirement{
			{ID: "requirement.x.1", Title: "Auth"},
			{ID: "requirement.x.2", Title: "Session"},
		},
	}
	input := []positionalStoryInput{
		{
			Label: "lifecycle", RequirementIndex: 0, Title: "Lifecycle",
			Tasks: []positionalTaskInput{
				{Label: "test", Description: "Write tests"},
				{Label: "impl", Description: "Implement", DependsOnLabels: []string{"test"}},
			},
		},
		{
			Label: "wire-up", RequirementIndex: 1, Title: "Wire-up",
			DependsOnLabels: []string{"lifecycle"},
			Tasks: []positionalTaskInput{
				{Label: "wire", Description: "Wire it"},
			},
		},
	}
	got, err := resolveStoryLabels(input, plan, "x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 stories, got %d", len(got))
	}
	// Canonical IDs: story.<slug>.<reqseq>.<storyseq>
	if got[0].ID != "story.x.1.1" {
		t.Errorf("story[0].ID = %q, want story.x.1.1", got[0].ID)
	}
	if got[1].ID != "story.x.2.1" {
		t.Errorf("story[1].ID = %q, want story.x.2.1", got[1].ID)
	}
	// RequirementIDs resolved from requirement_index (ADR-044: singleton-slice).
	if len(got[0].RequirementIDs) == 0 || got[0].RequirementIDs[0] != "requirement.x.1" {
		t.Errorf("story[0].RequirementIDs[0] = %q, want requirement.x.1 (got RequirementIDs=%v)", got[0].PrimaryRequirementID(), got[0].RequirementIDs)
	}
	// DependsOn label resolves to canonical story ID.
	if len(got[1].DependsOn) != 1 || got[1].DependsOn[0] != "story.x.1.1" {
		t.Errorf("story[1].DependsOn = %v, want [story.x.1.1]", got[1].DependsOn)
	}
	// Task canonical IDs + StoryID + intra-story DependsOn.
	if len(got[0].Tasks) != 2 {
		t.Fatalf("story[0] want 2 tasks, got %d", len(got[0].Tasks))
	}
	if got[0].Tasks[0].ID != "task.x.1.1.1" {
		t.Errorf("task[0].ID = %q, want task.x.1.1.1", got[0].Tasks[0].ID)
	}
	if got[0].Tasks[0].StoryID != "story.x.1.1" {
		t.Errorf("task[0].StoryID = %q, want story.x.1.1", got[0].Tasks[0].StoryID)
	}
	if len(got[0].Tasks[1].DependsOn) != 1 || got[0].Tasks[1].DependsOn[0] != "task.x.1.1.1" {
		t.Errorf("task[1].DependsOn = %v, want [task.x.1.1.1]", got[0].Tasks[1].DependsOn)
	}
}

// TestResolveStoryLabels_StoryseqIncrementsPerRequirement covers Sarah
// sharding ONE requirement into multiple stories — storyseq counter
// increments so the two stories get distinct IDs.
func TestResolveStoryLabels_StoryseqIncrementsPerRequirement(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "x",
		Requirements: []workflow.Requirement{
			{ID: "requirement.x.1", Title: "Big req"},
		},
	}
	input := []positionalStoryInput{
		{Label: "a", RequirementIndex: 0, Title: "A"},
		{Label: "b", RequirementIndex: 0, Title: "B"},
		{Label: "c", RequirementIndex: 0, Title: "C"},
	}
	got, err := resolveStoryLabels(input, plan, "x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"story.x.1.1", "story.x.1.2", "story.x.1.3"}
	for i, w := range want {
		if got[i].ID != w {
			t.Errorf("story[%d].ID = %q, want %q", i, got[i].ID, w)
		}
	}
}

func TestBuildPromptContext(t *testing.T) {
	plan := &workflow.Plan{
		Title:   "Test Plan",
		Goal:    "do the thing",
		Context: "because reasons",
		Exploration: &workflow.Exploration{
			Capabilities: []workflow.Capability{
				{Name: "auth", Description: "User auth flow"},
				{Name: "session", Description: "Session store"},
			},
		},
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{Name: "auth-service", Responsibility: "manages auth",
					ImplementationFiles: []string{"src/auth.go"},
					Capabilities:        []string{"auth"}},
			},
		},
		Requirements: []workflow.Requirement{
			{ID: "req.x.1", Title: "Login", Description: "Implement login",
				DependsOn: []string{"req.x.0"}},
		},
	}

	got := buildPromptContext(plan, "prior parse failed")

	if got.PlanTitle != "Test Plan" || got.PlanGoal != "do the thing" {
		t.Errorf("plan fields drifted: %+v", got)
	}
	if len(got.Capabilities) != 2 {
		t.Errorf("want 2 capabilities, got %d: %+v", len(got.Capabilities), got.Capabilities)
	}
	if len(got.ArchitectureComponents) != 1 {
		t.Errorf("want 1 component, got %d: %+v", len(got.ArchitectureComponents), got.ArchitectureComponents)
	}
	if got.ArchitectureComponents[0].Name != "auth-service" {
		t.Errorf("component name drifted: %+v", got.ArchitectureComponents[0])
	}
	if len(got.ArchitectureComponents[0].ImplementationFiles) != 1 ||
		got.ArchitectureComponents[0].ImplementationFiles[0] != "src/auth.go" {
		t.Errorf("implementation files drifted: %+v", got.ArchitectureComponents[0].ImplementationFiles)
	}
	if len(got.Requirements) != 1 || got.Requirements[0].ID != "req.x.1" {
		t.Errorf("requirements drifted: %+v", got.Requirements)
	}
	if len(got.Requirements[0].DependsOn) != 1 || got.Requirements[0].DependsOn[0] != "req.x.0" {
		t.Errorf("requirement depends_on drifted: %+v", got.Requirements[0].DependsOn)
	}
	if got.PreviousError != "prior parse failed" {
		t.Errorf("previous error drifted: %q", got.PreviousError)
	}
}

func TestBuildPromptContext_NilExploration(t *testing.T) {
	plan := &workflow.Plan{Title: "T"}
	got := buildPromptContext(plan, "")
	if len(got.Capabilities) != 0 {
		t.Errorf("want 0 capabilities on nil exploration, got %d", len(got.Capabilities))
	}
	if len(got.ArchitectureComponents) != 0 {
		t.Errorf("want 0 components on nil architecture, got %d", len(got.ArchitectureComponents))
	}
}

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	if c.MaxGenerationRetries != 2 {
		t.Errorf("expected default MaxGenerationRetries=2, got %d", c.MaxGenerationRetries)
	}
	if c.PlanStateBucket != "PLAN_STATES" {
		t.Errorf("expected PLAN_STATES bucket, got %q", c.PlanStateBucket)
	}
	if c.DefaultCapability != "planning" {
		t.Errorf("expected default capability=planning, got %q", c.DefaultCapability)
	}
	if c.RetryBackoffMs != 200 {
		t.Errorf("expected default RetryBackoffMs=200, got %d", c.RetryBackoffMs)
	}
}
