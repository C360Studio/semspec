package domain

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/prompt"
)

func TestSoftwareFragments(t *testing.T) {
	fragments := Software()
	if len(fragments) == 0 {
		t.Fatal("expected non-empty software fragments")
	}

	// Check all fragments have IDs
	ids := make(map[string]bool)
	for _, f := range fragments {
		if f.ID == "" {
			t.Error("fragment with empty ID")
		}
		if ids[f.ID] {
			t.Errorf("duplicate fragment ID: %s", f.ID)
		}
		ids[f.ID] = true
	}

	// Verify key fragments exist
	required := []string{
		"software.developer.system-base",
		"software.developer.tool-directive",
		"software.planner.system-base",
		"software.plan-reviewer.system-base",
		"software.reviewer.system-base",
		"software.requirement-generator.system-base",
		"software.scenario-generator.system-base",
		"software.task-generator.system-base",
		"software.phase-generator.system-base",
		"software.gap-detection",
	}
	for _, id := range required {
		if !ids[id] {
			t.Errorf("missing required fragment: %s", id)
		}
	}
}

func TestSoftwareDeveloperAssembly(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)
	r.Register(prompt.ToolGuidanceFragment(prompt.DefaultToolGuidance()))

	a := prompt.NewAssembler(r)
	result := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleDeveloper,
		Provider:       prompt.ProviderAnthropic,
		AvailableTools: []string{"file_read", "file_write", "git_diff"},
		SupportsTools:  true,
	})

	if !strings.Contains(result.SystemMessage, "developer implementing code changes") {
		t.Error("expected developer identity in system message")
	}
	if !strings.Contains(result.SystemMessage, "file_write") {
		t.Error("expected tool directive mentioning file_write")
	}
	if !strings.Contains(result.SystemMessage, "<identity>") {
		t.Error("expected XML formatting for Anthropic provider")
	}
}

func TestSoftwarePlannerAssembly(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)

	a := prompt.NewAssembler(r)
	result := a.Assemble(&prompt.AssemblyContext{
		Role:     prompt.RolePlanner,
		Provider: prompt.ProviderOpenAI,
	})

	if !strings.Contains(result.SystemMessage, "finalizing a development plan") {
		t.Error("expected planner identity")
	}
	if !strings.Contains(result.SystemMessage, "## Identity") {
		t.Error("expected markdown formatting for OpenAI")
	}
	// Should not contain developer-only fragments
	if strings.Contains(result.SystemMessage, "file_write tool") {
		t.Error("planner should not see developer tool directive")
	}
}

func TestSoftwareReviewerAssembly(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)

	a := prompt.NewAssembler(r)
	result := a.Assemble(&prompt.AssemblyContext{
		Role:     prompt.RoleReviewer,
		Provider: prompt.ProviderOllama,
	})

	if !strings.Contains(result.SystemMessage, "code reviewer") {
		t.Error("expected reviewer identity")
	}
	if !strings.Contains(result.SystemMessage, "READ-ONLY access") {
		t.Error("expected read-only notice in reviewer prompt")
	}
}

func TestSoftwarePlanReviewerAssembly(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)

	a := prompt.NewAssembler(r)
	result := a.Assemble(&prompt.AssemblyContext{
		Role:     prompt.RolePlanReviewer,
		Provider: prompt.ProviderAnthropic,
	})

	if !strings.Contains(result.SystemMessage, "plan reviewer") {
		t.Error("expected plan reviewer identity")
	}
	if !strings.Contains(result.SystemMessage, "needs_changes") {
		t.Error("expected verdict criteria in plan reviewer prompt")
	}
}

func TestSoftwareGapDetectionShared(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)

	roles := []prompt.Role{
		prompt.RoleDeveloper,
		prompt.RolePlanner,
		prompt.RoleReviewer,
		prompt.RolePlanReviewer,
	}

	for _, role := range roles {
		a := prompt.NewAssembler(r)
		result := a.Assemble(&prompt.AssemblyContext{Role: role})

		if !strings.Contains(result.SystemMessage, "Knowledge Gaps") {
			t.Errorf("role %s should have gap detection", role)
		}
	}
}

func TestSoftwareRetryFragment(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)

	a := prompt.NewAssembler(r)

	// Without feedback — retry directive should not appear
	result := a.Assemble(&prompt.AssemblyContext{
		Role: prompt.RoleDeveloper,
	})
	if strings.Contains(result.SystemMessage, "Previous Feedback") {
		t.Error("retry directive should not appear without feedback")
	}

	// With feedback — retry directive should appear
	result = a.Assemble(&prompt.AssemblyContext{
		Role: prompt.RoleDeveloper,
		TaskContext: &prompt.TaskContext{
			Feedback: "Missing error handling in auth middleware",
		},
	})
	if !strings.Contains(result.SystemMessage, "Missing error handling in auth middleware") {
		t.Error("expected feedback content in retry prompt")
	}
}
