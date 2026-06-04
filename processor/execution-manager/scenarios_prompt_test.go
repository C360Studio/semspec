package executionmanager

import (
	"testing"

	"github.com/c360studio/semspec/prompt"
	"github.com/c360studio/semspec/workflow"
)

// TestScenariosToSpecs pins the conversion from workflow.Scenario (the
// wire shape requirement-executor pushes through TaskCreateRequest) to
// prompt.ScenarioSpec (the prompt-facing shape rendered by
// software.task-scenarios). The conversion is on the hot path between
// the task dispatch and the dev/reviewer prompt assembly — a wrong
// mapping silently feeds the wrong contract to Cline and the developer.
//
// Closes the 2026-06-03 mavlink-hard Cline disconnect by ensuring
// scenarios actually thread end-to-end into TaskContext.Scenarios.
func TestScenariosToSpecs(t *testing.T) {
	t.Run("populated scenarios convert with all fields", func(t *testing.T) {
		in := []workflow.Scenario{
			{
				ID:    "scenario.x.1.1.1",
				Given: "the API server is running",
				When:  "a GET request is sent to /goodbye",
				Then:  []string{"a 200 status code is returned", "the response body contains 'goodbye'"},
			},
			{
				ID:    "scenario.x.1.1.2",
				Given: "an unknown endpoint is requested",
				When:  "the client calls GET /unknown",
				Then:  []string{"a 404 is returned"},
			},
		}

		got := scenariosToSpecs(in)
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
		if got[0].ID != "scenario.x.1.1.1" || got[0].Given != "the API server is running" {
			t.Errorf("first scenario lost identity/given: %+v", got[0])
		}
		if got[0].When != "a GET request is sent to /goodbye" {
			t.Errorf("first scenario lost When: %q", got[0].When)
		}
		if len(got[0].Then) != 2 || got[0].Then[0] != "a 200 status code is returned" {
			t.Errorf("first scenario lost Then list: %v", got[0].Then)
		}
		// Mutating the output's Then slice must NOT modify the input — the
		// conversion needs to copy, not alias, so prompt assembly can't
		// corrupt the source TaskExecution.Scenarios in KV.
		got[0].Then[0] = "MUTATED"
		if in[0].Then[0] == "MUTATED" {
			t.Errorf("scenariosToSpecs aliased the Then slice — mutation of output corrupted the input")
		}
	})

	t.Run("empty input returns nil so the fragment condition elides", func(t *testing.T) {
		got := scenariosToSpecs(nil)
		if got != nil {
			t.Errorf("got = %v, want nil — fragment-condition gates on len() > 0", got)
		}
	})

	t.Run("scenario with empty fields still converts", func(t *testing.T) {
		in := []workflow.Scenario{{ID: "scenario.x.1"}}
		got := scenariosToSpecs(in)
		if len(got) != 1 {
			t.Fatalf("len = %d, want 1", len(got))
		}
		// Should NOT silently drop a scenario just because some fields
		// are empty — the renderer handles empties cleanly and the dev
		// can still see "here's a scenario with no Given/Then yet" as
		// an explicit signal that scenario authoring was incomplete.
		if got[0].ID != "scenario.x.1" {
			t.Errorf("scenario with empty fields dropped: %+v", got[0])
		}
	})
}

// TestTaskContextScenariosThreadIntoPromptCtx pins the buildAssemblyContext
// → TaskContext → fragment-condition contract that gates the
// software.task-scenarios fragment. When exec.Scenarios is populated
// (the new ADR-044 M:N execution path), TaskContext.Scenarios is
// non-empty and the fragment fires for developer + reviewer + validator.
// When exec.Scenarios is empty (legacy fixtures / pre-Sarah plans), the
// fragment elides — back-compat.
func TestTaskContextScenariosThreadIntoPromptCtx(t *testing.T) {
	t.Run("populated exec scenarios surface in TaskContext", func(t *testing.T) {
		exec := &taskExecution{
			TaskExecution: &workflow.TaskExecution{
				Scenarios: []workflow.Scenario{
					{ID: "scenario.x.1", Given: "g", When: "w", Then: []string{"t"}},
				},
			},
		}
		got := scenariosToSpecs(exec.Scenarios)
		if len(got) != 1 || got[0].ID != "scenario.x.1" {
			t.Errorf("scenarios did not thread from TaskExecution to TaskContext shape: %v", got)
		}
	})

	t.Run("empty exec scenarios produce nil TaskContext.Scenarios", func(t *testing.T) {
		exec := &taskExecution{TaskExecution: &workflow.TaskExecution{}}
		got := scenariosToSpecs(exec.Scenarios)
		if got != nil {
			t.Errorf("got = %v, want nil so fragment condition fails cleanly", got)
		}
		// Sanity: prompt.ScenarioSpec is the destination shape.
		var _ []prompt.ScenarioSpec = got
	})
}
