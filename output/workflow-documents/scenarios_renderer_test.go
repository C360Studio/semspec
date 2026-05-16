package workflowdocuments

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestRenderScenarios_NilOrEmptyReturnsEmpty(t *testing.T) {
	if got := RenderScenarios(nil); got != "" {
		t.Errorf("RenderScenarios(nil) = %q, want empty", got)
	}
	plan := &workflow.Plan{Slug: "test"}
	if got := RenderScenarios(plan); got != "" {
		t.Errorf("RenderScenarios(empty plan) = %q, want empty", got)
	}
}

func TestRenderScenarios_GroupedByRequirement(t *testing.T) {
	plan := &workflow.Plan{
		Slug:  "scenario-test",
		Title: "Auth",
		Requirements: []workflow.Requirement{
			{ID: "req-1", Title: "Login"},
			{ID: "req-2", Title: "Token"},
		},
		Scenarios: []workflow.Scenario{
			{ID: "s1", RequirementID: "req-1",
				Given: "user has valid credentials",
				When:  "they POST to /login",
				Then:  []string{"200 response", "JWT in body"}},
			{ID: "s2", RequirementID: "req-2",
				Given: "request has invalid JWT",
				When:  "protected route accessed",
				Then:  []string{"401 returned"}},
		},
	}
	md := RenderScenarios(plan)
	checks := map[string]bool{
		"# Scenarios: Auth":                    true,
		"**2 scenarios**":                      true,
		"## Login":                             true, // requirement title is the heading
		"## Token":                             true,
		"**Given** user has valid credentials": true,
		"**When** they POST to /login":         true,
		"- 200 response":                       true,
		"- JWT in body":                        true,
		"`req-1`":                              true,
		"`req-2`":                              true,
	}
	for needle, want := range checks {
		got := strings.Contains(md, needle)
		if got != want {
			t.Errorf("contains(%q) = %v, want %v", needle, got, want)
		}
	}
}

func TestRenderScenarios_OrphanScenarios(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "orphan-test",
		Scenarios: []workflow.Scenario{
			{ID: "s1", RequirementID: "", When: "no requirement linked"},
		},
	}
	md := RenderScenarios(plan)
	if !strings.Contains(md, "## Unassigned scenarios") {
		t.Errorf("should call out orphan scenarios. got:\n%s", md)
	}
}
