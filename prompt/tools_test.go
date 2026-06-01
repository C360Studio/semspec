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
		"write_todos",
	}

	tools := FilterTools(allTools, RoleDeveloper)

	// Developer gets bash, submit_work, web tools, write_todos.
	// Graph tools removed 2026-05-12 — see tool_filter.go header
	// comment. NOT ask_question/decompose_task/spawn_agent either.
	want := []string{"bash", "submit_work", "web_search", "http_request", "write_todos"}
	for _, w := range want {
		if !slices.Contains(tools, w) {
			t.Errorf("developer should have %q", w)
		}
	}
	unwant := []string{"ask_question", "decompose_task", "spawn_agent", "graph_search", "graph_query", "graph_summary"}
	for _, u := range unwant {
		if slices.Contains(tools, u) {
			t.Errorf("developer should NOT have %q", u)
		}
	}
}

// TestFilterTools_WriteTodos_RoleScope pins which roles get write_todos
// per docs/structured-output-levels.md role map. Builder + Architect +
// LessonDecomposer have multi-step / multi-iteration work where
// cross-iteration memory pays off. Generators (planner / req-gen /
// scen-gen) do single-shot dispatches and write_todos misfits their
// shape — they get scratchpad instead (see
// TestFilterTools_Scratchpad_RoleScope).
func TestFilterTools_WriteTodos_RoleScope(t *testing.T) {
	allTools := []string{"bash", "submit_work", "write_todos"}

	hasWriteTodos := []Role{RoleDeveloper, RoleArchitect, RoleLessonDecomposer}
	for _, role := range hasWriteTodos {
		got := FilterTools(allTools, role)
		if !slices.Contains(got, "write_todos") {
			t.Errorf("role %q should have write_todos; got %v", role, got)
		}
	}

	missingWriteTodos := []Role{
		RolePlanner, RoleRequirementGenerator, RoleScenarioGenerator,
		RolePlanReviewer, RoleReviewer, RoleValidator,
	}
	for _, role := range missingWriteTodos {
		got := FilterTools(allTools, role)
		if slices.Contains(got, "write_todos") {
			t.Errorf("role %q should NOT have write_todos; got %v", role, got)
		}
	}
}

// TestFilterTools_Scratchpad_RoleScope pins broad scratchpad availability.
// Per the user's 2026-05-12 direction, scratchpad is an INTERNAL tool —
// not Goodhart-relevant — and should be available to any role that
// might need a "think before commit" runway. The semstreams ask plus
// the in-codebase persona guidance say generators are the highest-value
// users; we also expose it to reviewers and Builder so the runway is
// available when those roles need it (the trajectory cost is a single
// tool call). Reflects 2026-05-12 wiring decision.
func TestFilterTools_Scratchpad_RoleScope(t *testing.T) {
	allTools := []string{"bash", "submit_work", "scratchpad"}

	hasScratchpad := []Role{
		RoleDeveloper, RoleArchitect, RoleLessonDecomposer,
		RolePlanner, RoleRequirementGenerator, RoleScenarioGenerator,
		RolePlanReviewer, RoleTaskReviewer,
		RoleScenarioReviewer, RolePlanQAReviewer, RoleReviewer,
	}
	for _, role := range hasScratchpad {
		got := FilterTools(allTools, role)
		if !slices.Contains(got, "scratchpad") {
			t.Errorf("role %q should have scratchpad; got %v", role, got)
		}
	}

	// RoleValidator stays narrow — it runs the structural checklist
	// against the worktree and submits a verdict. No reasoning runway
	// expected.
	got := FilterTools(allTools, RoleValidator)
	if slices.Contains(got, "scratchpad") {
		t.Errorf("validator should NOT have scratchpad; got %v", got)
	}
}

func TestFilterTools_Reviewer(t *testing.T) {
	allTools := []string{
		"bash", "submit_work", "ask_question",
		"graph_search", "graph_query",
		"review_scenario", "decompose_task",
	}

	tools := FilterTools(allTools, RoleReviewer)

	want := []string{"bash", "submit_work"}
	for _, w := range want {
		if !slices.Contains(tools, w) {
			t.Errorf("reviewer should have %q", w)
		}
	}

	// Graph tools removed 2026-05-12 — see tool_filter.go header.
	deny := []string{"ask_question", "review_scenario", "decompose_task", "graph_search", "graph_query"}
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

	want := []string{"bash", "submit_work", "web_search", "http_request"}
	for _, w := range want {
		if !slices.Contains(tools, w) {
			t.Errorf("planner should have %q", w)
		}
	}

	// Graph tools removed 2026-05-12 — see tool_filter.go header.
	deny := []string{"decompose_task", "spawn_agent", "graph_search", "graph_query", "graph_summary"}
	for _, d := range deny {
		if slices.Contains(tools, d) {
			t.Errorf("planner should NOT have %q", d)
		}
	}
}

// TestFilterTools_NoRoleGetsGraphTools pins the 2026-05-12 decision to
// remove graph_search/graph_query/graph_summary from every role's
// AllowExact. Across 4+ tracked real-LLM @hard runs, agents used these
// zero or near-zero times and bailed to bash on errors (ADR-036). The
// tools stay registered in tools/workflow/register.go for future use,
// but no role surfaces them. Regression guard if a future PR re-adds
// them without revisiting the decision.
func TestFilterTools_NoRoleGetsGraphTools(t *testing.T) {
	allTools := []string{"bash", "submit_work", "graph_search", "graph_query", "graph_summary"}

	rolesWithAllowlist := []Role{
		RolePlanner, RoleDeveloper, RoleArchitect, RoleValidator,
		RoleReviewer, RolePlanReviewer, RoleTaskReviewer,
		RoleRequirementGenerator, RoleScenarioGenerator, RoleScenarioReviewer,
		RolePlanQAReviewer, RoleLessonDecomposer, RoleRecoveryAgent,
	}
	for _, role := range rolesWithAllowlist {
		got := FilterTools(allTools, role)
		for _, tool := range []string{"graph_search", "graph_query", "graph_summary"} {
			if slices.Contains(got, tool) {
				t.Errorf("role %q must not receive %q from FilterTools (removed 2026-05-12); got %v", role, tool, got)
			}
		}
	}
}

// TestFilterTools_NoRoleGetsSpawnAgent pins Phase 3 of the task-11 worktree
// audit: spawn_agent has been deleted as a tool, so no role's explicit
// allowlist must return it. Regression guard for a future change that
// reintroduces spawn_agent to a role without rewiring the runtime tool
// registration — which would cause agents to receive prompt guidance for a
// tool they cannot actually call.
//
// Scope: only covers roles with explicit AllowExact entries in tool_filter.go.
// RoleArchitect/RoleQA fall through to the unknown-role default (all tools
// returned verbatim); that behavior is documented by TestFilterTools_UnknownRole
// and is orthogonal to the spawn_agent-dead-code question — in production,
// availableToolNames() no longer contains "spawn_agent" at all.
func TestFilterTools_NoRoleGetsSpawnAgent(t *testing.T) {
	allTools := []string{"bash", "submit_work", "spawn_agent", "decompose_task"}

	rolesWithAllowlist := []Role{
		RolePlanner, RoleDeveloper, RoleValidator, RoleReviewer,
		RolePlanReviewer, RoleTaskReviewer,
		RoleRequirementGenerator, RoleScenarioGenerator, RoleScenarioReviewer,
		RolePlanQAReviewer,
	}
	for _, role := range rolesWithAllowlist {
		got := FilterTools(allTools, role)
		if slices.Contains(got, "spawn_agent") {
			t.Errorf("role %q must not receive spawn_agent from FilterTools; got %v", role, got)
		}
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
