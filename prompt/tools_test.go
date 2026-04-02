package prompt

import (
	"slices"
	"testing"
)

func TestFilterTools_Developer(t *testing.T) {
	allTools := []string{
		"bash", "submit_work", "ask_question",
		"graph_search", "graph_query", "graph_summary",
		"web_search", "http_request",
		"decompose_task", "spawn_agent",
	}

	tools := FilterTools(allTools, RoleDeveloper)

	// Developer gets bash, submit_work, graph + web tools — NOT ask_question/decompose_task/spawn_agent
	want := []string{"bash", "submit_work", "graph_search", "graph_query", "graph_summary", "web_search", "http_request"}
	for _, w := range want {
		if !slices.Contains(tools, w) {
			t.Errorf("developer should have %q", w)
		}
	}
	unwant := []string{"ask_question", "decompose_task", "spawn_agent"}
	for _, u := range unwant {
		if slices.Contains(tools, u) {
			t.Errorf("developer should NOT have %q", u)
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

	want := []string{"bash", "submit_work", "graph_search", "graph_query"}
	for _, w := range want {
		if !slices.Contains(tools, w) {
			t.Errorf("reviewer should have %q", w)
		}
	}

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
		"web_search", "http_request",
		"decompose_task", "spawn_agent",
	}

	tools := FilterTools(allTools, RolePlanner)

	want := []string{"bash", "submit_work", "graph_search", "graph_query", "graph_summary", "web_search", "http_request"}
	for _, w := range want {
		if !slices.Contains(tools, w) {
			t.Errorf("planner should have %q", w)
		}
	}

	deny := []string{"decompose_task", "spawn_agent"}
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

func TestFilterTools_UnknownRole(t *testing.T) {
	allTools := []string{"bash", "submit_work", "spawn_agent"}

	tools := FilterTools(allTools, Role("unknown"))
	if len(tools) != len(allTools) {
		t.Errorf("unknown role should get all %d tools, got %d", len(allTools), len(tools))
	}
}
