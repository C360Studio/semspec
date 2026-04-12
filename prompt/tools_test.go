package prompt

import (
	"slices"
	"strings"
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

func TestToolGuidance_SmallModel_SubmitWorkHint(t *testing.T) {
	r := NewRegistry()
	r.Register(ToolGuidanceFragment(DefaultToolGuidance()))

	a := NewAssembler(r)

	// Small model (32k) with submit_work should get the reinforcement.
	result := a.Assemble(&AssemblyContext{
		Role:           RoleReviewer,
		Provider:       ProviderOllama,
		MaxTokens:      32768,
		AvailableTools: []string{"bash", "submit_work"},
	})
	if result.SystemMessage == "" {
		t.Fatal("expected non-empty system message for small model with tools")
	}
	if !strings.Contains(result.SystemMessage, "submit_work function") {
		t.Error("small model should get submit_work reinforcement in tool guidance")
	}
	if !strings.Contains(result.SystemMessage, "Do NOT write JSON as text") {
		t.Error("small model should get anti-text-output warning")
	}

	// Small model without submit_work should NOT get the reinforcement.
	result = a.Assemble(&AssemblyContext{
		Role:           RoleReviewer,
		Provider:       ProviderOllama,
		MaxTokens:      32768,
		AvailableTools: []string{"bash"},
	})
	if strings.Contains(result.SystemMessage, "submit_work function") {
		t.Error("small model without submit_work should NOT get submit_work reinforcement")
	}

	// Large model should get full guidance, not the compact path.
	result = a.Assemble(&AssemblyContext{
		Role:           RoleReviewer,
		Provider:       ProviderOllama,
		MaxTokens:      131072,
		AvailableTools: []string{"bash", "submit_work"},
	})
	if strings.Contains(result.SystemMessage, "Do NOT write JSON as text") {
		t.Error("large model should NOT get small-model submit_work reinforcement")
	}
}

func TestFilterTools_UnknownRole(t *testing.T) {
	allTools := []string{"bash", "submit_work", "spawn_agent"}

	tools := FilterTools(allTools, Role("unknown"))
	if len(tools) != len(allTools) {
		t.Errorf("unknown role should get all %d tools, got %d", len(allTools), len(tools))
	}
}
