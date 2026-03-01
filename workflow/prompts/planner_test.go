package prompts

import (
	"strings"
	"testing"
)

func TestPlannerSystemPrompt(t *testing.T) {
	prompt := PlannerSystemPrompt()

	// Should include key sections
	sections := []string{
		"Your Objective",
		"Process",
		"Asking Questions",
		"Response Format",
		"Guidelines",
		"Tools Available",
		"Knowledge Gaps", // From GapDetectionInstructions
	}

	for _, section := range sections {
		if !strings.Contains(prompt, section) {
			t.Errorf("PlannerSystemPrompt missing section: %s", section)
		}
	}

	// Should include gap detection format
	if !strings.Contains(prompt, "<gap>") {
		t.Error("PlannerSystemPrompt should include gap detection XML format")
	}

	// Should include "committed" status in output
	if !strings.Contains(prompt, `"status": "committed"`) {
		t.Error("PlannerSystemPrompt should show committed status in output format")
	}

	// Should include output JSON structure
	requiredFields := []string{
		"goal",
		"context",
		"scope",
		"include",
		"exclude",
		"do_not_touch",
	}
	for _, field := range requiredFields {
		if !strings.Contains(prompt, field) {
			t.Errorf("PlannerSystemPrompt missing output field: %s", field)
		}
	}

	// Should mention both exploration and fresh start paths
	if !strings.Contains(prompt, "exploration") {
		t.Error("PlannerSystemPrompt should mention exploration path")
	}
	if !strings.Contains(prompt, "fresh") {
		t.Error("PlannerSystemPrompt should mention fresh start path")
	}

	// Should include key tools
	tools := []string{
		"file_read",
		"file_list",
		"workflow_read_document",
		"workflow_query_graph",
	}
	for _, tool := range tools {
		if !strings.Contains(prompt, tool) {
			t.Errorf("PlannerSystemPrompt missing tool: %s", tool)
		}
	}
}

func TestPlannerPromptWithTitle(t *testing.T) {
	title := "implement user authentication"
	prompt := PlannerPromptWithTitle(title)

	// Should include the title
	if !strings.Contains(prompt, title) {
		t.Errorf("PlannerPromptWithTitle should include title: %s", title)
	}

	// Should mention committed plan
	if !strings.Contains(prompt, "committed plan") {
		t.Error("PlannerPromptWithTitle should mention committed plan")
	}

	// Should mention Goal/Context/Scope output
	if !strings.Contains(prompt, "Goal/Context/Scope") {
		t.Error("PlannerPromptWithTitle should reference Goal/Context/Scope structure")
	}
}

func TestPlannerPromptFromExploration(t *testing.T) {
	slug := "auth-refresh"
	goal := "Add JWT refresh token endpoint"
	context := "Current auth system lacks token refresh"
	scope := []string{"api/auth/", "internal/jwt/"}

	prompt := PlannerPromptFromExploration(slug, goal, context, scope)

	// Should include all provided information
	if !strings.Contains(prompt, slug) {
		t.Errorf("PlannerPromptFromExploration should include slug: %s", slug)
	}
	if !strings.Contains(prompt, goal) {
		t.Errorf("PlannerPromptFromExploration should include goal: %s", goal)
	}
	if !strings.Contains(prompt, context) {
		t.Errorf("PlannerPromptFromExploration should include context: %s", context)
	}

	// Should include scope items
	for _, s := range scope {
		if !strings.Contains(prompt, s) {
			t.Errorf("PlannerPromptFromExploration should include scope item: %s", s)
		}
	}

	// Should mention finalizing exploration
	if !strings.Contains(prompt, "Finalize") && !strings.Contains(prompt, "finalize") {
		t.Error("PlannerPromptFromExploration should mention finalizing")
	}

	// Should mention committed plan
	if !strings.Contains(prompt, "committed") {
		t.Error("PlannerPromptFromExploration should mention committed plan")
	}
}

func TestPlannerPromptFromExploration_EmptyScope(t *testing.T) {
	slug := "test-slug"
	goal := "Test goal"
	context := "Test context"
	scope := []string{}

	prompt := PlannerPromptFromExploration(slug, goal, context, scope)

	// Should handle empty scope gracefully
	if !strings.Contains(prompt, "none defined") {
		t.Error("PlannerPromptFromExploration should show 'none defined' for empty scope")
	}
}

func TestPlannerPromptFromExploration_SingleScopeItem(t *testing.T) {
	slug := "single-scope"
	goal := "Single scope goal"
	context := "Single scope context"
	scope := []string{"src/single/"}

	prompt := PlannerPromptFromExploration(slug, goal, context, scope)

	// Should include the single scope item
	if !strings.Contains(prompt, "src/single/") {
		t.Error("PlannerPromptFromExploration should include single scope item")
	}

	// Should not show "none defined"
	if strings.Contains(prompt, "none defined") {
		t.Error("PlannerPromptFromExploration should not show 'none defined' when scope is provided")
	}
}
