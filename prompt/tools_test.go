package prompt

import (
	"slices"
	"testing"
)

func TestFilterTools_Builder(t *testing.T) {
	allTools := []string{
		"file_read", "file_write", "file_list",
		"git_status", "git_diff", "git_commit",
		"exec", "graph_query",
		"decompose_task", "spawn_agent",
	}

	tools := FilterTools(allTools, RoleBuilder)

	// Builder gets: file_read, file_write, file_list, git_status, git_diff
	want := []string{"file_read", "file_write", "file_list", "git_status", "git_diff"}
	for _, w := range want {
		if !slices.Contains(tools, w) {
			t.Errorf("builder should have %q", w)
		}
	}

	// Builder does NOT get: git_commit, exec, graph_query, decompose_task, spawn_agent
	deny := []string{"git_commit", "exec", "graph_query", "decompose_task", "spawn_agent"}
	for _, d := range deny {
		if slices.Contains(tools, d) {
			t.Errorf("builder should NOT have %q", d)
		}
	}
}

func TestFilterTools_Tester(t *testing.T) {
	allTools := []string{
		"file_read", "file_write", "file_list",
		"git_status", "git_diff", "git_commit",
		"exec", "graph_query",
		"decompose_task", "spawn_agent",
	}

	tools := FilterTools(allTools, RoleTester)

	// Tester gets: file_read, file_write, file_list, exec
	want := []string{"file_read", "file_write", "file_list", "exec"}
	for _, w := range want {
		if !slices.Contains(tools, w) {
			t.Errorf("tester should have %q", w)
		}
	}

	// Tester does NOT get: git_*, graph_query, decompose_task, spawn_agent
	deny := []string{"git_status", "git_diff", "git_commit", "graph_query", "decompose_task", "spawn_agent"}
	for _, d := range deny {
		if slices.Contains(tools, d) {
			t.Errorf("tester should NOT have %q", d)
		}
	}
}

func TestFilterTools_Reviewer(t *testing.T) {
	allTools := []string{
		"file_read", "file_write", "file_list",
		"git_status", "git_diff", "git_commit",
		"exec", "review_scenario",
	}

	tools := FilterTools(allTools, RoleReviewer)

	// Reviewer gets: file_read, file_list, git_diff, review_scenario
	want := []string{"file_read", "file_list", "git_diff", "review_scenario"}
	for _, w := range want {
		if !slices.Contains(tools, w) {
			t.Errorf("reviewer should have %q", w)
		}
	}

	// Reviewer does NOT get: file_write, git_status, git_commit, exec
	deny := []string{"file_write", "git_status", "git_commit", "exec"}
	for _, d := range deny {
		if slices.Contains(tools, d) {
			t.Errorf("reviewer should NOT have %q", d)
		}
	}
}

func TestFilterTools_Planner(t *testing.T) {
	allTools := []string{
		"file_read", "file_write", "file_list",
		"git_log", "git_status", "graph_query",
		"exec", "decompose_task",
	}

	tools := FilterTools(allTools, RolePlanner)

	// Planner gets: file_read, file_list, git_log, graph_query
	want := []string{"file_read", "file_list", "git_log", "graph_query"}
	for _, w := range want {
		if !slices.Contains(tools, w) {
			t.Errorf("planner should have %q", w)
		}
	}

	// Planner does NOT get: file_write, exec, decompose_task
	deny := []string{"file_write", "exec", "decompose_task"}
	for _, d := range deny {
		if slices.Contains(tools, d) {
			t.Errorf("planner should NOT have %q", d)
		}
	}
}

func TestFilterTools_Coordinator(t *testing.T) {
	allTools := []string{
		"file_read", "file_write", "file_list",
		"git_status", "exec",
		"spawn_agent", "query_agent_tree",
	}

	tools := FilterTools(allTools, RoleCoordinator)

	// Coordinator gets: spawn_agent, query_agent_tree ONLY
	want := []string{"spawn_agent", "query_agent_tree"}
	for _, w := range want {
		if !slices.Contains(tools, w) {
			t.Errorf("coordinator should have %q", w)
		}
	}

	if len(tools) != 2 {
		t.Errorf("coordinator should have exactly 2 tools, got %d: %v", len(tools), tools)
	}
}

func TestFilterTools_DeveloperBackwardCompat(t *testing.T) {
	allTools := []string{
		"file_read", "file_write", "file_list",
		"git_status", "git_diff",
		"decompose_task", "spawn_agent",
	}

	tools := FilterTools(allTools, RoleDeveloper)

	// Developer (deprecated) still gets file_* and git_* prefixes + exact tools
	if len(tools) != len(allTools) {
		t.Errorf("developer (compat) should get all %d tools, got %d: %v", len(allTools), len(tools), tools)
	}
}

func TestFilterTools_UnknownRole(t *testing.T) {
	allTools := []string{"file_read", "exec", "spawn_agent"}

	tools := FilterTools(allTools, Role("unknown"))
	if len(tools) != len(allTools) {
		t.Errorf("unknown role should get all %d tools, got %d", len(allTools), len(tools))
	}
}
