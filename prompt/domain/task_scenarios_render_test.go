package domain

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/prompt"
)

// TestRenderTaskScenarios_DeveloperFraming pins that the developer-role
// rendering frames scenarios as a test-writing contract — instructing
// the dev to write tests against each scenario's Given/When/Then, not
// against their interpretation of the task title.
//
// Closes the 2026-06-03 mavlink-hard disconnect on the dev side: pre-fix
// the dev's role-context fragment said "follow ALL requirements from
// SOPs in the task context" but no SOPs were ever threaded into the
// context, so the dev wrote tests against whatever the task title
// suggested. The scenario contract was invisible to the dev and only
// surfaced when the req-level reviewer rejected everything.
func TestRenderTaskScenarios_DeveloperFraming(t *testing.T) {
	scenarios := []prompt.ScenarioSpec{
		{
			ID:    "scenario.x.1.1.1",
			Given: "valid URL 'udp://:14540'",
			When:  "driver configuration is validated",
			Then:  []string{"connection string defaults to 'udp://:14540'", "server port defaults to 50051"},
		},
	}

	out := renderTaskScenarios(prompt.RoleDeveloper, scenarios)

	mustContain(t, out, "ACCEPTANCE SCENARIOS", "header missing — fragment can't be visually parsed from the assembled prompt")
	mustContain(t, out, "your tests MUST exercise the given/when/then", "developer rendering must instruct dev to ground tests in scenarios, not in the task title")
	mustContain(t, out, "scenario.x.1.1.1", "scenario_id missing — dev cannot reference it in test names")
	mustContain(t, out, "valid URL 'udp://:14540'", "Given clause missing — dev cannot test the specified input")
	mustContain(t, out, "driver configuration is validated", "When clause missing — dev cannot test the trigger")
	mustContain(t, out, "connection string defaults to 'udp://:14540'", "Then assertion 1 missing — dev cannot verify the contract")
	mustContain(t, out, "server port defaults to 50051", "Then assertion 2 missing — multi-assertion scenarios drop information")
}

// TestRenderTaskScenarios_ReviewerFraming pins that the per-task code-
// reviewer rendering shifts the framing to verification: each scenario
// must have a test exercising its Given/When/Then before the reviewer
// can approve. This is the load-bearing fix for the Cline contract gap.
func TestRenderTaskScenarios_ReviewerFraming(t *testing.T) {
	scenarios := []prompt.ScenarioSpec{
		{
			ID:    "scenario.x.1.1.1",
			Given: "the API server is running",
			When:  "a GET request is sent to /goodbye",
			Then:  []string{"a 200 status code is returned"},
		},
	}

	out := renderTaskScenarios(prompt.RoleReviewer, scenarios)

	mustContain(t, out, "ACCEPTANCE SCENARIOS", "header missing")
	mustContain(t, out, "developer's test suite MUST contain a test exercising its Given/When/Then", "reviewer framing must enforce per-scenario test existence — the load-bearing fix")
	mustContain(t, out, "verdict=rejected", "verdict instruction missing — reviewer needs explicit rejection routing")
	mustContain(t, out, "rejection_type=fixable", "rejection_type missing — pre-fix Cline gave the wrong type")
	mustContain(t, out, "CANNOT approve if any scenario lacks a test", "integrity rule missing — Cline must NOT approve scenario-violating code")
	mustContain(t, out, "scenario.x.1.1.1", "scenario_id missing — reviewer cannot quote the failing scenario in feedback")
	mustContain(t, out, "a 200 status code is returned", "Then assertion missing — reviewer cannot verify the contract")
}

// TestRenderTaskScenarios_ValidatorFraming pins the lighter validator
// framing — validator runs structural / type / lint checks and the
// scenarios are surfaced as a confirmation hint, not a hard verdict
// criterion (which is the per-task reviewer's job).
func TestRenderTaskScenarios_ValidatorFraming(t *testing.T) {
	scenarios := []prompt.ScenarioSpec{{ID: "scenario.x.1", Given: "g", When: "w", Then: []string{"t"}}}

	out := renderTaskScenarios(prompt.RoleValidator, scenarios)

	mustContain(t, out, "ACCEPTANCE SCENARIOS", "header missing")
	mustContain(t, out, "structural-validator should confirm", "validator framing should be advisory, not verdict-shaping")
}

// TestRenderTaskScenarios_EmptyElides pins that the fragment-condition
// gate falls clean when scenarios is empty — the renderer returns "" and
// the assembled prompt has no orphan ACCEPTANCE SCENARIOS header. This
// preserves back-compat for mock fixtures and legacy plans that don't
// have a per-task scenario binding.
func TestRenderTaskScenarios_EmptyElides(t *testing.T) {
	out := renderTaskScenarios(prompt.RoleDeveloper, nil)
	if out != "" {
		t.Errorf("renderTaskScenarios(empty) = %q, want \"\"; fragment condition should gate on len() > 0 but empty input should also render clean", out)
	}

	out2 := renderTaskScenarios(prompt.RoleReviewer, []prompt.ScenarioSpec{})
	if out2 != "" {
		t.Errorf("renderTaskScenarios(zero-len) = %q, want \"\"", out2)
	}
}

// TestRenderTaskScenarios_PartialFieldsRenderCleanly pins that the
// renderer handles scenarios with missing fields gracefully — no panic,
// no broken markdown headers — so that authoring incompleteness doesn't
// crash the prompt assembly path.
func TestRenderTaskScenarios_PartialFieldsRenderCleanly(t *testing.T) {
	scenarios := []prompt.ScenarioSpec{
		{ID: "scenario.x.1"},                                          // no Given/When/Then
		{ID: "scenario.x.2", Given: "g only"},                         // only Given
		{Given: "g", When: "w", Then: []string{"t"}},                  // no ID
		{ID: "scenario.x.3", Given: "g", When: "w", Then: []string{}}, // empty Then list
	}

	out := renderTaskScenarios(prompt.RoleDeveloper, scenarios)

	if !strings.Contains(out, "scenario.x.1") {
		t.Errorf("scenario with only ID was dropped — should render as header-only")
	}
	if !strings.Contains(out, "(unnamed scenario)") {
		t.Errorf("scenario with no ID should render as '(unnamed scenario)' so the dev can see it's there")
	}
	if !strings.Contains(out, "g only") {
		t.Errorf("scenario with only Given dropped — should render the available field")
	}
	if strings.Contains(out, "panic") {
		t.Errorf("partial scenarios caused panic-shaped output: %s", out)
	}
}

func mustContain(t *testing.T, haystack, needle, why string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("output missing required substring %q (%s)\n--- output ---\n%s", needle, why, haystack)
	}
}

// TestTaskScenariosFragment_AssemblerWiring runs the new
// software.task-scenarios fragment through the FULL Software() pipeline
// to verify (a) the fragment is registered in Software(), (b) the
// Condition fires only when TaskContext is non-nil with a non-empty
// Scenarios slice, and (c) the Roles filter gates to Developer +
// Validator + Reviewer.
//
// These assembler-level assertions catch regressions the unit tests on
// renderTaskScenarios cannot — e.g. dropping the fragment from the
// Software() slice, mis-tagging its Roles, or breaking the Condition
// closure would leave renderTaskScenarios green but silently regress
// the load-bearing Cline contract delivery on the assembled prompt.
func TestTaskScenariosFragment_AssemblerWiring(t *testing.T) {
	t.Parallel()
	assembler := buildProductionPipeline(Software())

	// Match on the SCENARIO BODY (Given line) rather than the "ACCEPTANCE
	// SCENARIOS" header — the header substring also appears in role-context
	// hint text ("read any ACCEPTANCE SCENARIOS section above") which would
	// false-positive the wrong-role assertions. The Given clause "the system
	// is in state X" only appears when the fragment actually rendered the
	// scenarios array — that's the load-bearing signal.
	const scenarioGivenLine = "**Given:** the system is in state X"
	const scenarioIDLine = "scenario.x.1.1.1"

	taskCtxWithScenarios := func() *prompt.TaskContext {
		return &prompt.TaskContext{
			Scenarios: []prompt.ScenarioSpec{
				{
					ID:    "scenario.x.1.1.1",
					Given: "the system is in state X",
					When:  "the user submits Y",
					Then:  []string{"the response is Z"},
				},
			},
		}
	}

	fires := []prompt.Role{
		prompt.RoleDeveloper,
		prompt.RoleValidator,
		prompt.RoleReviewer,
	}
	for _, role := range fires {
		role := role
		t.Run("fires/"+string(role), func(t *testing.T) {
			t.Parallel()
			result := assembler.Assemble(&prompt.AssemblyContext{
				Role:           role,
				Provider:       prompt.ProviderAnthropic,
				AvailableTools: prompt.FilterTools(allTools, role),
				SupportsTools:  true,
				TaskContext:    taskCtxWithScenarios(),
			})
			if !strings.Contains(result.SystemMessage, scenarioGivenLine) {
				t.Errorf("expected scenario Given clause %q in assembled prompt for role %s (fragment did not fire or is not registered in Software())\n--- prompt ---\n%s", scenarioGivenLine, role, result.SystemMessage)
			}
			if !strings.Contains(result.SystemMessage, scenarioIDLine) {
				t.Errorf("scenario_id %q missing from assembled prompt for role %s — the Condition fired but the ContentFunc did not render scenarios", scenarioIDLine, role)
			}
		})
	}

	skips := []prompt.Role{
		prompt.RolePlanner,
		prompt.RolePlanReviewer,
		prompt.RoleArchitect,
		prompt.RoleScenarioGenerator,
		prompt.RoleScenarioReviewer,
		prompt.RoleStoryPreparer,
		prompt.RoleRequirementGenerator,
		prompt.RoleTaskReviewer,
		prompt.RoleQA,
		prompt.RolePlanQAReviewer,
	}
	for _, role := range skips {
		role := role
		t.Run("skips/wrong-role/"+string(role), func(t *testing.T) {
			t.Parallel()
			result := assembler.Assemble(&prompt.AssemblyContext{
				Role:           role,
				Provider:       prompt.ProviderAnthropic,
				AvailableTools: prompt.FilterTools(allTools, role),
				SupportsTools:  true,
				TaskContext:    taskCtxWithScenarios(),
			})
			if strings.Contains(result.SystemMessage, scenarioGivenLine) {
				t.Errorf("scenario body leaked into role %s prompt — the fragment Roles filter is wrong; only Developer/Validator/Reviewer should see scenarios", role)
			}
		})
	}

	t.Run("skips/nil-task-context", func(t *testing.T) {
		t.Parallel()
		result := assembler.Assemble(&prompt.AssemblyContext{
			Role:           prompt.RoleDeveloper,
			Provider:       prompt.ProviderAnthropic,
			AvailableTools: prompt.FilterTools(allTools, prompt.RoleDeveloper),
			SupportsTools:  true,
			TaskContext:    nil,
		})
		if strings.Contains(result.SystemMessage, scenarioGivenLine) {
			t.Errorf("scenario body rendered with nil TaskContext — Condition closure crashed past the nil guard")
		}
	})

	t.Run("skips/empty-scenarios", func(t *testing.T) {
		t.Parallel()
		result := assembler.Assemble(&prompt.AssemblyContext{
			Role:           prompt.RoleDeveloper,
			Provider:       prompt.ProviderAnthropic,
			AvailableTools: prompt.FilterTools(allTools, prompt.RoleDeveloper),
			SupportsTools:  true,
			TaskContext:    &prompt.TaskContext{}, // Scenarios = nil
		})
		if strings.Contains(result.SystemMessage, scenarioGivenLine) {
			t.Errorf("scenario body rendered with empty Scenarios — back-compat broken for legacy fixtures without per-task scenario binding")
		}
	})
}
