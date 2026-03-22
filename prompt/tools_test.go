package prompt

import (
	"slices"
	"testing"
)

func TestFilterTools_Builder(t *testing.T) {
	allTools := []string{
		"bash", "submit_work", "ask_question",
		"graph_search", "graph_query",
		"decompose_task", "spawn_agent",
	}

	tools := FilterTools(allTools, RoleBuilder)

	// Builder gets: bash, submit_work
	want := []string{"bash", "submit_work"}
	for _, w := range want {
		if !slices.Contains(tools, w) {
			t.Errorf("builder should have %q", w)
		}
	}

	// Builder does NOT get: graph_search, graph_query, decompose_task, spawn_agent, ask_question
	deny := []string{"graph_search", "graph_query", "decompose_task", "spawn_agent", "ask_question"}
	for _, d := range deny {
		if slices.Contains(tools, d) {
			t.Errorf("builder should NOT have %q", d)
		}
	}
}

func TestFilterTools_Tester(t *testing.T) {
	allTools := []string{
		"bash", "submit_work", "ask_question",
		"graph_search", "graph_query",
		"decompose_task", "spawn_agent",
	}

	tools := FilterTools(allTools, RoleTester)

	// Tester gets: bash, submit_work
	want := []string{"bash", "submit_work"}
	for _, w := range want {
		if !slices.Contains(tools, w) {
			t.Errorf("tester should have %q", w)
		}
	}

	// Tester does NOT get: graph_search, graph_query, decompose_task, spawn_agent
	deny := []string{"graph_search", "graph_query", "decompose_task", "spawn_agent"}
	for _, d := range deny {
		if slices.Contains(tools, d) {
			t.Errorf("tester should NOT have %q", d)
		}
	}
}

func TestFilterTools_Reviewer(t *testing.T) {
	allTools := []string{
		"bash", "submit_work", "ask_question",
		"graph_search", "graph_query",
		"review_scenario", "decompose_task",
	}

	tools := FilterTools(allTools, RoleReviewer)

	// Reviewer gets: bash, submit_work, graph_search, graph_query
	want := []string{"bash", "submit_work", "graph_search", "graph_query"}
	for _, w := range want {
		if !slices.Contains(tools, w) {
			t.Errorf("reviewer should have %q", w)
		}
	}

	// Reviewer does NOT get: ask_question, review_scenario, decompose_task
	deny := []string{"ask_question", "review_scenario", "decompose_task"}
	for _, d := range deny {
		if slices.Contains(tools, d) {
			t.Errorf("reviewer should NOT have %q", d)
		}
	}
}

func TestFilterTools_Planner(t *testing.T) {
	allTools := []string{
		"bash", "submit_work",
		"graph_search", "graph_query", "graph_summary",
		"decompose_task", "spawn_agent",
	}

	tools := FilterTools(allTools, RolePlanner)

	// Planner gets: bash, graph_search, graph_query, graph_summary
	want := []string{"bash", "graph_search", "graph_query", "graph_summary"}
	for _, w := range want {
		if !slices.Contains(tools, w) {
			t.Errorf("planner should have %q", w)
		}
	}

	// Planner does NOT get: submit_work, decompose_task, spawn_agent
	deny := []string{"submit_work", "decompose_task", "spawn_agent"}
	for _, d := range deny {
		if slices.Contains(tools, d) {
			t.Errorf("planner should NOT have %q", d)
		}
	}
}

func TestFilterTools_Coordinator(t *testing.T) {
	allTools := []string{
		"bash", "submit_work",
		"spawn_agent", "decompose_task",
	}

	tools := FilterTools(allTools, RoleCoordinator)

	// Coordinator gets: spawn_agent ONLY
	want := []string{"spawn_agent"}
	for _, w := range want {
		if !slices.Contains(tools, w) {
			t.Errorf("coordinator should have %q", w)
		}
	}

	if len(tools) != 1 {
		t.Errorf("coordinator should have exactly 1 tool, got %d: %v", len(tools), tools)
	}
}

func TestFilterTools_DeveloperBackwardCompat(t *testing.T) {
	allTools := []string{
		"bash", "submit_work",
		"decompose_task", "spawn_agent",
	}

	tools := FilterTools(allTools, RoleDeveloper)

	// Developer (deprecated) gets bash, submit_work, decompose_task, spawn_agent
	want := []string{"bash", "submit_work", "decompose_task", "spawn_agent"}
	for _, w := range want {
		if !slices.Contains(tools, w) {
			t.Errorf("developer (compat) should have %q", w)
		}
	}
	if len(tools) != len(allTools) {
		t.Errorf("developer (compat) should get all %d tools, got %d: %v", len(allTools), len(tools), tools)
	}
}

func TestFilterTools_UnknownRole(t *testing.T) {
	allTools := []string{"bash", "submit_work", "spawn_agent"}

	tools := FilterTools(allTools, Role("unknown"))
	if len(tools) != len(allTools) {
		t.Errorf("unknown role should get all %d tools, got %d", len(allTools), len(tools))
	}
}
