package prompt

import (
	"strings"
	"testing"
)

func TestDefaultToolGuidance(t *testing.T) {
	guidance := DefaultToolGuidance()
	if len(guidance) == 0 {
		t.Fatal("expected non-empty default tool guidance")
	}

	names := make(map[string]bool)
	for _, g := range guidance {
		if g.Name == "" {
			t.Error("tool guidance with empty name")
		}
		if g.Guidance == "" {
			t.Errorf("tool %q has empty guidance", g.Name)
		}
		names[g.Name] = true
	}

	required := []string{"file_read", "file_write", "file_list", "git_status", "workflow_query_graph"}
	for _, name := range required {
		if !names[name] {
			t.Errorf("missing required tool guidance for %q", name)
		}
	}
}

func TestToolGuidanceFragment(t *testing.T) {
	fragment := ToolGuidanceFragment(DefaultToolGuidance())

	if fragment.ID != "core.tool-guidance" {
		t.Errorf("expected ID 'core.tool-guidance', got %q", fragment.ID)
	}
	if fragment.Category != CategoryToolGuidance {
		t.Errorf("expected CategoryToolGuidance, got %d", fragment.Category)
	}

	// Should not activate with single tool
	ctx := &AssemblyContext{AvailableTools: []string{"file_read"}}
	if fragment.Condition(ctx) {
		t.Error("should not activate with single tool")
	}

	// Should activate with multiple tools
	ctx.AvailableTools = []string{"file_read", "file_write"}
	if !fragment.Condition(ctx) {
		t.Error("should activate with multiple tools")
	}

	// Content should list available tools
	ctx.Role = RoleDeveloper
	content := fragment.ContentFunc(ctx)
	if !strings.Contains(content, "file_read") {
		t.Error("expected file_read in guidance content")
	}
	if !strings.Contains(content, "file_write") {
		t.Error("expected file_write in guidance content")
	}
}

func TestToolGuidanceRoleFiltering(t *testing.T) {
	guidance := DefaultToolGuidance()
	fragment := ToolGuidanceFragment(guidance)

	// Reviewer should not see file_write guidance
	ctx := &AssemblyContext{
		Role:           RoleReviewer,
		AvailableTools: []string{"file_read", "file_write", "git_diff"},
	}
	content := fragment.ContentFunc(ctx)

	if strings.Contains(content, "file_write") {
		t.Error("reviewer should not see file_write guidance")
	}
	if !strings.Contains(content, "file_read") {
		t.Error("reviewer should see file_read guidance")
	}
}

func TestFilterTools(t *testing.T) {
	allTools := []string{
		"file_read", "file_write", "file_list",
		"git_status", "git_diff", "git_commit",
		"workflow_query_graph", "workflow_read_document",
		"decompose_task", "spawn_agent",
	}

	t.Run("developer gets all tools", func(t *testing.T) {
		tools := FilterTools(allTools, RoleDeveloper)
		if len(tools) != len(allTools) {
			t.Errorf("expected %d tools for developer, got %d", len(allTools), len(tools))
		}
	})

	t.Run("reviewer gets read-only tools", func(t *testing.T) {
		tools := FilterTools(allTools, RoleReviewer)
		for _, tool := range tools {
			if tool == "file_write" || tool == "git_commit" {
				t.Errorf("reviewer should not have %q", tool)
			}
		}
		if !contains(tools, "file_read") {
			t.Error("reviewer should have file_read")
		}
		if !contains(tools, "git_diff") {
			t.Error("reviewer should have git_diff")
		}
	})

	t.Run("planner gets limited tools", func(t *testing.T) {
		tools := FilterTools(allTools, RolePlanner)
		if contains(tools, "file_write") {
			t.Error("planner should not have file_write")
		}
		if !contains(tools, "file_read") {
			t.Error("planner should have file_read")
		}
		if !contains(tools, "workflow_query_graph") {
			t.Error("planner should have workflow_query_graph")
		}
	})

	t.Run("unknown role gets all tools", func(t *testing.T) {
		tools := FilterTools(allTools, Role("unknown"))
		if len(tools) != len(allTools) {
			t.Errorf("unknown role should get all %d tools, got %d", len(allTools), len(tools))
		}
	})
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
