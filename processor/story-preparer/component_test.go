package storypreparer

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// twoCompPlan builds a plan with 2 capabilities, 2 requirements (1:1),
// and 2 architecture components (1:1 with capabilities/files). Used as
// the canonical "disjoint components" fixture for parser tests.
func twoCompPlan() *workflow.Plan {
	return &workflow.Plan{
		Slug: "x",
		Exploration: &workflow.Exploration{
			Capabilities: []workflow.Capability{
				{Name: "auth"},
				{Name: "session"},
			},
		},
		Requirements: []workflow.Requirement{
			{ID: "requirement.x.1", Title: "First", CapabilityName: "auth"},
			{ID: "requirement.x.2", Title: "Second", CapabilityName: "session"},
		},
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{Name: "auth-service", ImplementationFiles: []string{"src/auth.go"}, Capabilities: []string{"auth"}},
				{Name: "session-store", ImplementationFiles: []string{"src/session.go"}, Capabilities: []string{"session"}},
			},
		},
	}
}

func TestParseStoriesFromResult(t *testing.T) {
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
			// ADR-044: one Story per component, covering its requirement +
			// capability. Two components → two stories, full coverage.
			name:      "two-component plan with full coverage parses cleanly",
			input:     `{"stories":[{"label":"auth","component_name":"auth-service","requirement_indices":[0],"capability_indices":[0],"title":"Auth","tasks":[{"label":"t1","description":"d"}]},{"label":"sess","component_name":"session-store","requirement_indices":[1],"capability_indices":[1],"title":"Sess","tasks":[{"label":"t1","description":"d"}]}]}`,
			wantCount: 2,
		},
		{
			// Coverage closure: a Story set missing a Requirement is
			// rejected so Sarah's retry pinpoints the gap.
			name:    "partial requirement coverage rejected",
			input:   `{"stories":[{"label":"auth","component_name":"auth-service","requirement_indices":[0],"capability_indices":[0,1],"title":"T","tasks":[{"label":"t1","description":"d"}]}]}`,
			wantErr: "do not cover every requirement",
		},
		{
			// Coverage closure: a Story set missing a Capability is
			// rejected.
			name:    "partial capability coverage rejected",
			input:   `{"stories":[{"label":"both","component_name":"auth-service","requirement_indices":[0,1],"capability_indices":[0],"title":"T","tasks":[{"label":"t1","description":"d"}]}]}`,
			wantErr: "do not cover every capability",
		},
		{
			name:    "requirement_index out of range rejected",
			input:   `{"stories":[{"label":"l1","component_name":"auth-service","requirement_indices":[5],"capability_indices":[0],"title":"T","tasks":[]}]}`,
			wantErr: "requirement_index 5 out of range",
		},
		{
			name:    "capability_index out of range rejected",
			input:   `{"stories":[{"label":"l1","component_name":"auth-service","requirement_indices":[0],"capability_indices":[9],"title":"T","tasks":[]}]}`,
			wantErr: "capability_index 9 out of range",
		},
		{
			name:    "missing component_name rejected",
			input:   `{"stories":[{"label":"l1","requirement_indices":[0],"capability_indices":[0],"title":"T","tasks":[]}]}`,
			wantErr: "missing component_name",
		},
		{
			name:    "unresolved component_name rejected",
			input:   `{"stories":[{"label":"l1","component_name":"ghost","requirement_indices":[0],"capability_indices":[0],"title":"T","tasks":[]}]}`,
			wantErr: `component_name "ghost" does not resolve`,
		},
		{
			name:    "empty requirement_indices rejected",
			input:   `{"stories":[{"label":"l1","component_name":"auth-service","requirement_indices":[],"capability_indices":[0],"title":"T","tasks":[]}]}`,
			wantErr: "requirement_indices is empty",
		},
		{
			name:    "duplicate requirement_index rejected",
			input:   `{"stories":[{"label":"l1","component_name":"auth-service","requirement_indices":[0,0],"capability_indices":[0],"title":"T","tasks":[]}]}`,
			wantErr: "requirement_index 0 listed more than once",
		},
		{
			name:    "missing story label rejected",
			input:   `{"stories":[{"label":"","component_name":"auth-service","requirement_indices":[0],"capability_indices":[0],"title":"T","tasks":[]}]}`,
			wantErr: "missing label",
		},
		{
			name:    "duplicate story label rejected",
			input:   `{"stories":[{"label":"l1","component_name":"auth-service","requirement_indices":[0],"capability_indices":[0],"title":"T1","tasks":[]},{"label":"l1","component_name":"session-store","requirement_indices":[1],"capability_indices":[1],"title":"T2","tasks":[]}]}`,
			wantErr: `label "l1" appears more than once`,
		},
		{
			name:    "duplicate task label rejected",
			input:   `{"stories":[{"label":"auth","component_name":"auth-service","requirement_indices":[0],"capability_indices":[0],"title":"T","tasks":[{"label":"a","description":"d"},{"label":"a","description":"d"}]},{"label":"sess","component_name":"session-store","requirement_indices":[1],"capability_indices":[1],"title":"T","tasks":[{"label":"t1","description":"d"}]}]}`,
			wantErr: `task label "a" appears more than once`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := twoCompPlan()
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

// TestResolveStoryLabels_MNCoverage_CohesiveComponent is the
// ADR-044 mavlink-hard case: one cohesive component covers N
// capabilities + N requirements. Sarah emits ONE Story covering
// all of them. FilesOwned is derived from the component (no union).
func TestResolveStoryLabels_MNCoverage_CohesiveComponent(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "mav",
		Exploration: &workflow.Exploration{
			Capabilities: []workflow.Capability{
				{Name: "mavsdk-lifecycle"},
				{Name: "mavsdk-cs-telemetry"},
				{Name: "mavsdk-cs-control"},
			},
		},
		Requirements: []workflow.Requirement{
			{ID: "requirement.mav.1", Title: "Lifecycle", CapabilityName: "mavsdk-lifecycle"},
			{ID: "requirement.mav.2", Title: "Telemetry", CapabilityName: "mavsdk-cs-telemetry"},
			{ID: "requirement.mav.3", Title: "Control", CapabilityName: "mavsdk-cs-control"},
		},
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{Name: "mavsdk-driver",
					ImplementationFiles: []string{"src/Driver.java", "src/Lifecycle.java", "src/Telemetry.java"},
					Capabilities:        []string{"mavsdk-lifecycle", "mavsdk-cs-telemetry", "mavsdk-cs-control"}},
			},
		},
	}
	input := []positionalStoryInput{
		{
			Label:              "driver",
			ComponentName:      "mavsdk-driver",
			RequirementIndices: []int{0, 1, 2},
			CapabilityIndices:  []int{0, 1, 2},
			Title:              "Cohesive MAVSDK driver",
			Tasks:              []positionalTaskInput{{Label: "t1", Description: "implement"}},
		},
	}
	got, err := resolveStoryLabels(input, plan, "mav")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 cohesive Story, got %d", len(got))
	}
	s := got[0]
	if s.ComponentName != "mavsdk-driver" {
		t.Errorf("ComponentName = %q, want mavsdk-driver", s.ComponentName)
	}
	if len(s.RequirementIDs) != 3 {
		t.Errorf("RequirementIDs = %v, want all 3", s.RequirementIDs)
	}
	if len(s.CapabilityNames) != 3 {
		t.Errorf("CapabilityNames = %v, want all 3", s.CapabilityNames)
	}
	// FilesOwned must be derived from component.ImplementationFiles (no
	// union, no Sarah authorship).
	if len(s.FilesOwned) != 3 || s.FilesOwned[0] != "src/Driver.java" {
		t.Errorf("FilesOwned = %v, want component.implementation_files exactly", s.FilesOwned)
	}
	// Single Story → no DependsOn edges from Pass 1 (no other coverers).
	if len(s.DependsOn) != 0 {
		t.Errorf("Single Story should have no DependsOn, got %v", s.DependsOn)
	}
}

// TestResolveStoryLabels_DerivesSchedulingFromRequirementDAG covers the
// canonical case where Story B's requirement depends on Story A's: the
// system populates Story.DependsOn via DeriveStoryScheduling.
func TestResolveStoryLabels_DerivesSchedulingFromRequirementDAG(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "x",
		Exploration: &workflow.Exploration{
			Capabilities: []workflow.Capability{{Name: "auth"}, {Name: "session"}},
		},
		Requirements: []workflow.Requirement{
			{ID: "requirement.x.1", Title: "Auth", CapabilityName: "auth"},
			{ID: "requirement.x.2", Title: "Session", CapabilityName: "session", DependsOn: []string{"requirement.x.1"}},
		},
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{Name: "auth-service", ImplementationFiles: []string{"src/auth.go"}, Capabilities: []string{"auth"}},
				{Name: "session-store", ImplementationFiles: []string{"src/session.go"}, Capabilities: []string{"session"}},
			},
		},
	}
	input := []positionalStoryInput{
		{Label: "auth", ComponentName: "auth-service",
			RequirementIndices: []int{0}, CapabilityIndices: []int{0},
			Title: "Auth", Tasks: []positionalTaskInput{{Label: "t", Description: "d"}}},
		{Label: "sess", ComponentName: "session-store",
			RequirementIndices: []int{1}, CapabilityIndices: []int{1},
			Title: "Sess", Tasks: []positionalTaskInput{{Label: "t", Description: "d"}}},
	}
	got, err := resolveStoryLabels(input, plan, "x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Session Story should depend on Auth Story via the derived semantic edge.
	sess := got[1]
	if len(sess.DependsOn) != 1 || sess.DependsOn[0] != got[0].ID {
		t.Errorf("session.DependsOn = %v, want [%s] (derived from R DAG)", sess.DependsOn, got[0].ID)
	}
}

// TestResolveStoryLabels_TaskLabelsResolveToCanonicalIDs pins the intra-Story
// task DependsOn label resolution under ADR-044 (task labels still rewrite
// to canonical Task.ID even though cross-Story DependsOn is now system-derived).
func TestResolveStoryLabels_TaskLabelsResolveToCanonicalIDs(t *testing.T) {
	plan := twoCompPlan()
	input := []positionalStoryInput{
		{Label: "auth", ComponentName: "auth-service",
			RequirementIndices: []int{0}, CapabilityIndices: []int{0},
			Title: "Auth",
			Tasks: []positionalTaskInput{
				{Label: "test", Description: "Write tests"},
				{Label: "impl", Description: "Implement", DependsOnLabels: []string{"test"}},
			}},
		{Label: "sess", ComponentName: "session-store",
			RequirementIndices: []int{1}, CapabilityIndices: []int{1},
			Title: "Sess",
			Tasks: []positionalTaskInput{{Label: "wire", Description: "Wire"}}},
	}
	got, err := resolveStoryLabels(input, plan, "x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Canonical IDs: story.<slug>.<reqseq>.<storyseq> (primary req = first listed)
	if got[0].ID != "story.x.1.1" {
		t.Errorf("story[0].ID = %q, want story.x.1.1", got[0].ID)
	}
	if got[1].ID != "story.x.2.1" {
		t.Errorf("story[1].ID = %q, want story.x.2.1", got[1].ID)
	}
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
		t.Errorf("task[1].DependsOn = %v, want [task.x.1.1.1] (intra-Story label resolution)", got[0].Tasks[1].DependsOn)
	}
}

// TestResolveStoryLabels_StoryseqIncrementsPerPrimaryReq covers the edge case
// where two stories anchor different components but happen to list the same
// requirement first (same primary reqseq). The storyseq counter increments
// so they get distinct canonical IDs.
func TestResolveStoryLabels_StoryseqIncrementsPerPrimaryReq(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "x",
		Exploration: &workflow.Exploration{
			Capabilities: []workflow.Capability{{Name: "a"}, {Name: "b"}},
		},
		Requirements: []workflow.Requirement{
			{ID: "requirement.x.1", Title: "shared", CapabilityName: "a"},
			{ID: "requirement.x.2", Title: "second", CapabilityName: "b"},
		},
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{Name: "compA", ImplementationFiles: []string{"src/a.go"}, Capabilities: []string{"a"}},
				{Name: "compB", ImplementationFiles: []string{"src/b.go"}, Capabilities: []string{"b"}},
			},
		},
	}
	input := []positionalStoryInput{
		{Label: "first", ComponentName: "compA", RequirementIndices: []int{0}, CapabilityIndices: []int{0},
			Title: "A", Tasks: []positionalTaskInput{{Label: "t", Description: "d"}}},
		{Label: "second", ComponentName: "compB", RequirementIndices: []int{0, 1}, CapabilityIndices: []int{1},
			Title: "B", Tasks: []positionalTaskInput{{Label: "t", Description: "d"}}},
	}
	got, err := resolveStoryLabels(input, plan, "x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Both stories have primary req = requirement.x.1 → storyseq increments.
	if got[0].ID != "story.x.1.1" || got[1].ID != "story.x.1.2" {
		t.Errorf("got IDs %q, %q — want story.x.1.1, story.x.1.2", got[0].ID, got[1].ID)
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
