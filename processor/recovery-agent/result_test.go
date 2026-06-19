package recoveryagent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/agentic"
)

func TestParseRecoveryResult(t *testing.T) {
	t.Run("happy path escalate_human", func(t *testing.T) {
		raw := `{"action":"escalate_human","diagnosis":"Agent kept retrying sed in-place; correct fix is heredoc rewrite. No programmatic refinement fits.","recovery_succeeded":false}`
		got, err := parseRecoveryResult(raw)
		if err != nil {
			t.Fatalf("expected ok, got error: %v", err)
		}
		if got.Action != payloads.RecoveryActionEscalateHuman {
			t.Errorf("Action: got %q, want escalate_human", got.Action)
		}
		if got.RecoverySucceeded {
			t.Error("RecoverySucceeded: expected false for escalate_human")
		}
	})

	t.Run("happy path refine_prompt", func(t *testing.T) {
		raw := `{"action":"refine_prompt","diagnosis":"graph_search returned [project] org.sensorhub but agent ignored it.","refined_prompt":"Use the project entity from graph_search as the parent groupId.","contract_impact":{"kind":"preserve","summary":"same contract, sharper prompt","affected_ids":["req.demo.1"]},"recovery_succeeded":true}`
		got, err := parseRecoveryResult(raw)
		if err != nil {
			t.Fatalf("expected ok, got error: %v", err)
		}
		if got.Action != payloads.RecoveryActionRefinePrompt {
			t.Errorf("Action: got %q, want refine_prompt", got.Action)
		}
		if got.RefinedPrompt == "" {
			t.Error("RefinedPrompt: expected populated")
		}
		if got.ContractImpact == nil || got.ContractImpact.Kind != workflow.ContractImpactPreserve {
			t.Fatalf("ContractImpact = %#v, want preserve", got.ContractImpact)
		}
		if len(got.ContractImpact.AffectedIDs) != 1 || got.ContractImpact.AffectedIDs[0] != "req.demo.1" {
			t.Fatalf("ContractImpact.AffectedIDs = %v, want [req.demo.1]", got.ContractImpact.AffectedIDs)
		}
	})

	// Markdown-fence stripping — LLMs commonly return ```json ... ```. Caught
	// 2026-05-10 take 6 gemini @hard: the recovery agent's gemini-pro response
	// landed fence-wrapped, json.Unmarshal failed with `invalid character '\`'`,
	// recovery fell back to the parse-failure marker and the diagnosis was lost.
	// jsonutil.ExtractJSON handles this; the test pins that integration.
	t.Run("strips markdown json fence", func(t *testing.T) {
		raw := "```json\n" +
			`{"action":"escalate_human","diagnosis":"agent thrashing in bash quoting loop; refined prompt would not help here.","recovery_succeeded":false}` +
			"\n```"
		got, err := parseRecoveryResult(raw)
		if err != nil {
			t.Fatalf("expected fence to be stripped, got error: %v", err)
		}
		if got.Action != payloads.RecoveryActionEscalateHuman {
			t.Errorf("Action: got %q, want escalate_human", got.Action)
		}
		if !strings.Contains(got.Diagnosis, "thrashing") {
			t.Errorf("diagnosis content lost across fence strip: %q", got.Diagnosis)
		}
	})

	t.Run("strips bare-fence wrapper", func(t *testing.T) {
		raw := "```\n" +
			`{"action":"mark_unrecoverable","diagnosis":"upstream artifact does not exist on Maven Central or any configured mirror.","recovery_succeeded":false}` +
			"\n```"
		got, err := parseRecoveryResult(raw)
		if err != nil {
			t.Fatalf("expected bare fence to be stripped, got error: %v", err)
		}
		if got.Action != payloads.RecoveryActionMarkUnrecoverable {
			t.Errorf("Action: got %q, want mark_unrecoverable", got.Action)
		}
	})

	// ADR-043 PR 4i — story_reprepare is the new action class for wedges
	// whose root cause is Sarah's Story-shaping (wrong task DAG, missing
	// files_owned, mis-selected components). Reaches back to story-preparer
	// for a re-prep cycle. Callers materialize once execution dispatches
	// per-Story (PR 4h).
	t.Run("happy path story_reprepare", func(t *testing.T) {
		raw := `{"action":"story_reprepare","diagnosis":"Sarah's task DAG missed integration smoke; dev loop has no scaffold to verify against PX4 SITL.","recovery_succeeded":true}`
		got, err := parseRecoveryResult(raw)
		if err != nil {
			t.Fatalf("expected ok, got error: %v", err)
		}
		if got.Action != payloads.RecoveryActionStoryReprepare {
			t.Errorf("Action: got %q, want story_reprepare", got.Action)
		}
		if !strings.Contains(got.Diagnosis, "Sarah") {
			t.Errorf("diagnosis content lost: %q", got.Diagnosis)
		}
	})

	// architecture_revise is the heaviest action: the architecture itself is
	// the wedge root (mis-resolved upstream dep, wrong component boundary).
	// No extra fields beyond diagnosis — the diagnosis becomes the architect's
	// revision feedback when plan-manager re-runs the architecture.
	t.Run("happy path architecture_revise", func(t *testing.T) {
		raw := `{"action":"architecture_revise","diagnosis":"Winston pinned io.mavsdk:mavsdk:3.16.0 but the driver requires the 2.x API; every dev cycle re-hallucinates the dep coords.","recovery_succeeded":true}`
		got, err := parseRecoveryResult(raw)
		if err != nil {
			t.Fatalf("expected ok, got error: %v", err)
		}
		if got.Action != payloads.RecoveryActionArchitectureRevise {
			t.Errorf("Action: got %q, want architecture_revise", got.Action)
		}
		if !strings.Contains(got.Diagnosis, "mavsdk") {
			t.Errorf("diagnosis content lost: %q", got.Diagnosis)
		}
	})

	// Action-omission safety net (go-reviewer / run #7): mid-tier models
	// sometimes write the refined_prompt payload but drop the `action` label.
	// When the payload is unambiguous (refined_prompt present), infer
	// refine_prompt instead of terminal-failing the wedge.
	t.Run("infers refine_prompt when action omitted but refined_prompt present", func(t *testing.T) {
		raw := `{"diagnosis":"Agent created the test in org/sensorhub/driver/mavsdk instead of org/sensorhub/impl/sensor/mavsdk; needs explicit file paths.","recovery_succeeded":true,"refined_prompt":"Create the source files at src/main/java/org/sensorhub/impl/sensor/mavsdk/ per the architecture."}`
		got, err := parseRecoveryResult(raw)
		if err != nil {
			t.Fatalf("expected inference to succeed, got error: %v", err)
		}
		if got.Action != payloads.RecoveryActionRefinePrompt {
			t.Errorf("Action: got %q, want refine_prompt (inferred)", got.Action)
		}
		if !strings.Contains(got.RefinedPrompt, "impl/sensor/mavsdk") {
			t.Errorf("refined_prompt content lost: %q", got.RefinedPrompt)
		}
		// Mirror the run-#7 incident shape: recovery_succeeded was true.
		if !got.RecoverySucceeded {
			t.Error("RecoverySucceeded should survive inference (incident had it true)")
		}
	})

	// 2026-06-13 gemini-pro mavlink-hard run 2: the agent omitted `action` AND
	// wrote the fix prose under the non-schema field `feedback` (not
	// refined_prompt) → the run-#7 net (keyed on refined_prompt) missed it →
	// execution_exhausted → plan rejected, despite a recoverable wedge. Adopt
	// `feedback` as the refined prompt so the inference still fires.
	t.Run("infers refine_prompt from feedback when action+refined_prompt omitted", func(t *testing.T) {
		raw := `{"diagnosis":"Tests only assert isConnected() instead of exercising the When/Then telemetry and control steps.","feedback":"Rewrite the tests to assert the actual telemetry datastream values and control command results per the acceptance scenarios.","recovery_succeeded":true}`
		got, err := parseRecoveryResult(raw)
		if err != nil {
			t.Fatalf("expected feedback to be adopted as refined_prompt, got error: %v", err)
		}
		if got.Action != payloads.RecoveryActionRefinePrompt {
			t.Errorf("Action: got %q, want refine_prompt (inferred from feedback)", got.Action)
		}
		if !strings.Contains(got.RefinedPrompt, "telemetry datastream") {
			t.Errorf("feedback prose not adopted into refined_prompt: %q", got.RefinedPrompt)
		}
	})

	t.Run("salvages diagnosis from feedback-only review-shaped result", func(t *testing.T) {
		raw := `{"verdict":"rejected","summary":"Build failed before tests.","feedback":"The architecture mis-resolved org.sensorhub:sensorhub-core:2.0.1 as a source_build without a build-native acquisition path. Revise the architecture to provide a resolvable artifact or composite build contract.","findings":[],"rejection_type":"fixable","scenario_verdicts":[]}`
		got, err := parseRecoveryResult(raw)
		if err != nil {
			t.Fatalf("expected #235 feedback-only recovery result to parse, got error: %v", err)
		}
		if got.Action != payloads.RecoveryActionRefinePrompt {
			t.Errorf("Action: got %q, want refine_prompt inferred from feedback", got.Action)
		}
		if !strings.Contains(got.Diagnosis, "source_build") {
			t.Errorf("diagnosis did not fall back to feedback: %q", got.Diagnosis)
		}
		if got.Diagnosis != got.RefinedPrompt {
			t.Errorf("feedback fallback should populate diagnosis and refined_prompt; diagnosis=%q refined_prompt=%q", got.Diagnosis, got.RefinedPrompt)
		}
	})

	// refined_prompt still wins when BOTH it and feedback are present (the
	// schema-correct field takes precedence; feedback is only a fallback).
	t.Run("prefers refined_prompt over feedback when both present", func(t *testing.T) {
		raw := `{"diagnosis":"d","recovery_succeeded":true,"refined_prompt":"SCHEMA prompt","feedback":"fallback prose"}`
		got, err := parseRecoveryResult(raw)
		if err != nil {
			t.Fatalf("expected ok, got error: %v", err)
		}
		if got.RefinedPrompt != "SCHEMA prompt" {
			t.Errorf("refined_prompt should win over feedback; got %q", got.RefinedPrompt)
		}
	})

	// But an action-less result with NO unambiguous payload still errors —
	// scope_changes is ambiguous (narrow_scope vs split_req) so it is NOT
	// inferred, and diagnosis-only stays human-gated.
	t.Run("does not infer from scope_changes (ambiguous)", func(t *testing.T) {
		raw := `{"diagnosis":"too broad","recovery_succeeded":true,"scope_changes":{"keep":["a.go"]}}`
		_, err := parseRecoveryResult(raw)
		if !errors.Is(err, errResultMissingAction) {
			t.Errorf("expected errResultMissingAction (scope_changes is ambiguous), got %v", err)
		}
	})

	// Both payloads present + no action: refined_prompt wins (refine_prompt
	// maps to the same requirement_change kind as narrow_scope/split_req, so
	// this picks the unambiguous one and the downstream cascade is identical).
	t.Run("infers refine_prompt when both refined_prompt and scope_changes present", func(t *testing.T) {
		raw := `{"diagnosis":"d","recovery_succeeded":true,"refined_prompt":"do X at path Y","scope_changes":{"keep":["a.go"]}}`
		got, err := parseRecoveryResult(raw)
		if err != nil {
			t.Fatalf("expected inference to succeed, got error: %v", err)
		}
		if got.Action != payloads.RecoveryActionRefinePrompt {
			t.Errorf("Action: got %q, want refine_prompt (refined_prompt is unambiguous)", got.Action)
		}
	})

	cases := []struct {
		name string
		raw  string
		want error
	}{
		{"empty input", "", errResultEmpty},
		{"whitespace only", "   \n\t", errResultEmpty},
		{"missing action", `{"diagnosis":"x"}`, errResultMissingAction},
		{"missing diagnosis", `{"action":"escalate_human"}`, errResultMissingDiag},
		{"invalid action", `{"action":"bump_model","diagnosis":"need a smarter model"}`, errResultInvalidAction},
		{"invalid contract impact", `{"action":"story_reprepare","diagnosis":"x","contract_impact":{"kind":"rewrite_everything"},"recovery_succeeded":true}`, errResultInvalidImpact},
		{"refine without refined_prompt", `{"action":"refine_prompt","diagnosis":"x"}`, errResultRefineNeedsPrmt},
		{"refine with whitespace refined_prompt", `{"action":"refine_prompt","diagnosis":"x","refined_prompt":"   \n  "}`, errResultRefineNeedsPrmt},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseRecoveryResult(tc.raw)
			if err == nil {
				t.Fatalf("expected error %v, got nil", tc.want)
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("error chain does not contain %v; got %v", tc.want, err)
			}
		})
	}
}

func TestRecoverySubmitWorkSchemaMatchesParserContract(t *testing.T) {
	tools := recoverySubmitTools(&recoveryTestToolRegistry{
		tools: []agentic.ToolDefinition{{
			Name:        "submit_work",
			Description: "Submit recovery decision",
			Parameters:  map[string]any{"type": "object"},
		}},
	}, nil, "submit_work")
	if len(tools) != 1 {
		t.Fatalf("got %d tools, want 1 submit_work", len(tools))
	}
	props := schemaProps(t, tools[0].Parameters)

	structFields := jsonFields(reflect.TypeOf(rawRecoveryResult{}))
	for field := range structFields {
		if field == "feedback" {
			if _, ok := props[field]; ok {
				t.Fatalf("feedback is a parser fallback for non-schema review-shaped output; it must not be in the recovery submit_work schema")
			}
			continue
		}
		if _, ok := props[field]; !ok {
			t.Errorf("rawRecoveryResult field %q missing from recovery submit_work schema", field)
		}
	}
	for field := range props {
		if !structFields[field] {
			t.Errorf("recovery submit_work schema property %q has no rawRecoveryResult field", field)
		}
	}
	for _, reviewOnly := range []string{"verdict", "findings", "rejection_type", "scenario_verdicts"} {
		if _, ok := props[reviewOnly]; ok {
			t.Errorf("recovery submit_work schema includes review-only field %q", reviewOnly)
		}
	}
}

type recoveryTestToolRegistry struct {
	tools []agentic.ToolDefinition
}

func (r *recoveryTestToolRegistry) ListTools() []agentic.ToolDefinition {
	return r.tools
}

func (r *recoveryTestToolRegistry) Execute(_ context.Context, _ agentic.ToolCall) (agentic.ToolResult, error) {
	return agentic.ToolResult{}, nil
}

func schemaProps(t *testing.T, schema map[string]any) map[string]any {
	t.Helper()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema missing properties")
	}
	return props
}

func jsonFields(typ reflect.Type) map[string]bool {
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	fields := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name != "" {
			fields[name] = true
		}
	}
	return fields
}

// Note: the user-prompt content tests previously here (TestBuildUserPrompt*)
// moved to prompt/domain/software_render_test.go when the recovery-agent
// dispatch was wired through the assembler — 2026-05-11. The renderer
// lives in prompt/domain now; testing it in the package that owns it is
// the natural shape and removes the recovery-agent test file's dependency
// on internal prompt construction.
