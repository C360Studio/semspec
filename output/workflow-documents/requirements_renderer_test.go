package workflowdocuments

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestRenderRequirements_NilOrEmptyReturnsEmpty(t *testing.T) {
	if got := RenderRequirements(nil); got != "" {
		t.Errorf("RenderRequirements(nil) = %q, want empty", got)
	}
	plan := &workflow.Plan{Slug: "test"}
	if got := RenderRequirements(plan); got != "" {
		t.Errorf("RenderRequirements(plan with no requirements) = %q, want empty", got)
	}
}

func TestRenderRequirements_FullList(t *testing.T) {
	plan := &workflow.Plan{
		Slug:  "req-test",
		Title: "Auth feature",
		Requirements: []workflow.Requirement{
			{ID: "req-1", Title: "Login endpoint", Description: "POST /login", Status: "active",
				FilesOwned: []string{"src/auth/login.go", "src/auth/login_test.go"}},
			{ID: "req-2", Title: "Token validation", Description: "JWT validation middleware",
				FilesOwned: []string{"src/auth/middleware.go"}, DependsOn: []string{"req-1"}},
		},
		Scenarios: []workflow.Scenario{
			{ID: "s1", RequirementID: "req-1"},
			{ID: "s2", RequirementID: "req-1"},
			{ID: "s3", RequirementID: "req-2"},
		},
	}
	md := RenderRequirements(plan)
	checks := map[string]bool{
		"# Requirements: Auth feature":  true,
		"**2 requirements**":            true,
		"## Login endpoint":             true,
		"## Token validation":           true,
		"src/auth/login.go":             true,
		"**Depends on:** req-1":         true,
		"**Verified by 2 scenario(s)**": true, // req-1 has 2 scenarios
		"**Verified by 1 scenario(s)**": true, // req-2 has 1
		"## Dependency graph":           true,
		"```mermaid":                    true,
		"req-1 --> req-2":               true,
	}
	for needle, want := range checks {
		got := strings.Contains(md, needle)
		if got != want {
			t.Errorf("contains(%q) = %v, want %v", needle, got, want)
		}
	}
}

func TestRenderRequirements_NoDepsSkipsMermaid(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "no-deps",
		Requirements: []workflow.Requirement{
			{ID: "r1", Title: "Standalone"},
		},
	}
	md := RenderRequirements(plan)
	if strings.Contains(md, "## Dependency graph") {
		t.Error("should skip mermaid section when no depends_on edges")
	}
}
