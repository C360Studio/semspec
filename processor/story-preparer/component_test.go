package storypreparer

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

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
			name:      "single story parses cleanly",
			input:     `{"stories":[{"id":"story.x.1.1","requirement_id":"req.x.1","title":"T","intent":"i","components":["c"],"files_owned":["src/x.go"],"depends_on":[],"tasks":[{"id":"task.x.1.1.1","story_id":"story.x.1.1","description":"d","depends_on":[]}]}]}`,
			wantCount: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseStoriesFromResult(tc.input)
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
	if c.Enabled {
		t.Errorf("ADR-043 PR 3 ships dormant — DefaultConfig().Enabled must be false, got true")
	}
	if c.MaxGenerationRetries != 2 {
		t.Errorf("expected default MaxGenerationRetries=2, got %d", c.MaxGenerationRetries)
	}
	if c.PlanStateBucket != "PLAN_STATES" {
		t.Errorf("expected PLAN_STATES bucket, got %q", c.PlanStateBucket)
	}
}
