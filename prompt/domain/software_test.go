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
		"software.developer.role-context",
		"software.shared.submit-work-directive",
		"software.shared.prior-work-directive",
		"software.planner.system-base",
		"software.plan-reviewer.system-base",
		"software.reviewer.system-base",
		"software.requirement-generator.system-base",
		"software.scenario-generator.system-base",
		"software.task-generator.system-base",
		"software.provider.ollama-tool-enforcement",
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
		AvailableTools: []string{"bash", "submit_work", "graph_search"},
		SupportsTools:  true,
	})

	if !strings.Contains(result.SystemMessage, "developer implementing code changes") {
		t.Error("expected developer identity in system message")
	}
	if !strings.Contains(result.SystemMessage, "bash") {
		t.Error("expected tool directive mentioning bash")
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

	if !strings.Contains(result.SystemMessage, "planner exploring a problem space") {
		t.Error("expected planner identity")
	}
	if !strings.Contains(result.SystemMessage, "## Identity") {
		t.Error("expected markdown formatting for OpenAI")
	}
	// Should not contain developer-only fragments
	if strings.Contains(result.SystemMessage, "You MUST use bash to create or modify implementation") {
		t.Error("planner should not see builder tool directive")
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
	if !strings.Contains(result.SystemMessage, "READ-ONLY access via bash") {
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
	// Pins the bug-#2 leverage-point fix from the 2026-05-03 openrouter @easy run:
	// reviewer must encode plan defects as findings, not only in summary.
	// Drop this rule and the verdict normalization stops being self-consistent.
	if !strings.Contains(result.SystemMessage, "findings drive the verdict") {
		t.Error("expected explicit 'findings drive the verdict' rule in plan-reviewer output-format fragment")
	}
	if !strings.Contains(result.SystemMessage, `severity="error"`) {
		t.Error("expected explicit severity=error guidance for plan defects")
	}
}

// TestSoftwareGapDetectionRemoved verifies gap detection is NOT in prompts
// (removed — Q&A system handles questions via ask_question tool).
func TestSoftwareGapDetectionRemoved(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)

	a := prompt.NewAssembler(r)
	result := a.Assemble(&prompt.AssemblyContext{Role: prompt.RolePlanner})

	if strings.Contains(result.SystemMessage, "Knowledge Gaps") {
		t.Error("gap detection should NOT be in prompts (removed)")
	}
}

func TestSoftwareSubmitWorkDirective(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)

	a := prompt.NewAssembler(r)

	// Non-developer roles should get the shared submit_work directive.
	for _, role := range []prompt.Role{
		prompt.RolePlanReviewer,
		prompt.RoleReviewer,
		prompt.RoleArchitect,
		prompt.RoleRequirementGenerator,
		prompt.RoleScenarioGenerator,
	} {
		result := a.Assemble(&prompt.AssemblyContext{Role: role, Provider: prompt.ProviderOllama})
		if !strings.Contains(result.SystemMessage, "MUST call the submit_work function") {
			t.Errorf("role %s should have submit_work directive", role)
		}
	}

	// Developer has its own tool directive — should NOT get the shared one.
	result := a.Assemble(&prompt.AssemblyContext{Role: prompt.RoleDeveloper, Provider: prompt.ProviderOllama})
	if strings.Contains(result.SystemMessage, "MUST call the submit_work function") {
		t.Error("developer should not get shared submit_work directive (has its own)")
	}
}

// TestSoftwareOrientationGraphFirst pins the dial-#6 fix: the orientation
// fragment must hard-direct agents to graph_summary when graph tools are
// available (the 2026-04-28 Gemini @easy run made 0 graph_* calls because
// the prompt said "graph_summary OR a few bash commands"). Personas without
// graph_summary in their allowlist get the legacy bash-only orientation.
func TestSoftwareOrientationGraphFirst(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)
	a := prompt.NewAssembler(r)

	withGraph := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleDeveloper,
		Provider:       prompt.ProviderOpenAI,
		AvailableTools: []string{"bash", "submit_work", "graph_summary", "graph_search", "graph_query"},
	})
	// Reasons-not-rules orientation: explain why graph indexing matters and
	// distinguish entity IDs from filesystem paths. The earlier "Iteration 1
	// MUST call graph_summary" framing produced cargo-cult behavior on small
	// models (architect at qwen3:14b@temp0.6 passed entity IDs to bash as
	// paths four iterations in a row, never recovered). Reasons let small
	// models pick the right tool from understanding instead of compliance.
	if !strings.Contains(withGraph.SystemMessage, "Semantic Knowledge Graph") {
		t.Error("orientation must establish that the graph is an SKG of THIS workspace (not generic)")
	}
	if !strings.Contains(withGraph.SystemMessage, "semsource") {
		t.Error("orientation must name semsource as the curator so agents know the graph is live and authoritative")
	}
	if !strings.Contains(withGraph.SystemMessage, "shared, durable memory") {
		t.Error("orientation must establish the graph as cross-agent shared memory — that's the why-they-should-care")
	}
	if !strings.Contains(withGraph.SystemMessage, "graph indexes the workspace") {
		t.Error("graph-equipped persona should explain WHY the graph matters (indexing), not prescribe MUST")
	}
	if !strings.Contains(withGraph.SystemMessage, "Entity IDs are graph keys") {
		t.Error("orientation must distinguish entity IDs from filesystem paths to prevent cargo-culting IDs into bash args")
	}
	if strings.Contains(withGraph.SystemMessage, "graph_summary or a few bash commands") {
		t.Error("the soft 'or a few bash commands' phrasing must not survive — that's exactly what produced the 0-graph-call run")
	}
	if strings.Contains(withGraph.SystemMessage, "Iteration 1 MUST call graph_summary") {
		t.Error("the prescriptive 'MUST call' framing is the Goodhart trap that produced entity-ID-as-bash-path cargo-culting")
	}

	withoutGraph := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleReviewer,
		Provider:       prompt.ProviderOpenAI,
		AvailableTools: []string{"bash", "submit_work"},
	})
	if strings.Contains(withoutGraph.SystemMessage, "graph_summary") {
		t.Error("personas without graph_summary in their allowlist must not be told to call it")
	}
	if !strings.Contains(withoutGraph.SystemMessage, "Orient yourself briefly") {
		t.Error("non-graph personas should still get bash-orientation guidance")
	}
}

// TestSoftwareRequirementGeneratorFilesOwned pins the dial-#1 prompt landing
// in the right persona. The first attempt edited workflow/prompts/
// requirement_generator.go which was dead code and never reached Gemini —
// the live persona is in this domain registry. This test fails if a future
// refactor drops the files_owned guidance.
func TestSoftwareRequirementGeneratorFilesOwned(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)
	a := prompt.NewAssembler(r)

	result := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleRequirementGenerator,
		Provider:       prompt.ProviderOpenAI,
		AvailableTools: []string{"submit_work", "graph_summary"},
	})

	mustContain := []string{
		"files_owned",
		"depends_on",
		"Partition files across requirements",
		"merge fails",
		"prefer ONE requirement that owns BOTH",
		// Fan-in guidance for shared registration files (main.go, app.tsx, etc.).
		// Without it, qwen3-class models put main.go in every requirement's
		// files_owned in parallel and burn the retry budget on the same mistake.
		"Shared registration files",
		"fan-in",
		"final \"wire-up\" requirement",
		// 3-req example in the output-format fragment must mention the pattern.
		"fan-in pattern",
		"feature requirements DO NOT list main.go",
		// First-conflict-only caveat lets retries see the whole partition,
		// not just the validator's first complaint.
		"only reports the FIRST conflicting pair",
	}
	for _, want := range mustContain {
		if !strings.Contains(result.SystemMessage, want) {
			t.Errorf("requirement-generator persona missing %q — dial #1 prompt did not land", want)
		}
	}
}

func TestSoftwareOllamaProviderHint(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)

	a := prompt.NewAssembler(r)

	// Ollama provider should get the Ollama-specific tool enforcement.
	result := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleReviewer,
		Provider:       prompt.ProviderOllama,
		AvailableTools: []string{"bash", "submit_work"},
	})
	if !strings.Contains(result.SystemMessage, "function-calling tools available") {
		t.Error("Ollama reviewer should get ollama-tool-enforcement hint")
	}

	// Non-Ollama provider should not get it.
	result = a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleReviewer,
		Provider:       prompt.ProviderAnthropic,
		AvailableTools: []string{"bash", "submit_work"},
	})
	if strings.Contains(result.SystemMessage, "function-calling tools available") {
		t.Error("Anthropic reviewer should not get ollama-tool-enforcement hint")
	}
}

// TestSoftwareUserPromptCoverage asserts that every role whose component
// dispatches via the prompt registry (assembled.UserMessage) has a
// CategoryUserPrompt fragment registered. Adding a new component that uses the
// registry path without a user-prompt fragment would otherwise only fail at
// runtime with an empty user message.
func TestSoftwareUserPromptCoverage(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)

	rolesNeedingUserPrompt := []prompt.Role{
		prompt.RolePlanner,
		prompt.RoleRequirementGenerator,
		prompt.RoleScenarioGenerator,
		prompt.RoleArchitect,
		prompt.RolePlanReviewer,
		prompt.RolePlanQAReviewer,
	}

	for _, role := range rolesNeedingUserPrompt {
		if r.UserPromptFragmentFor(role) == nil {
			t.Errorf("role %q has no CategoryUserPrompt fragment registered — components dispatching this role would emit an empty user message", role)
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
