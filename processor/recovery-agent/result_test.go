package recoveryagent

import (
	"errors"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow/payloads"
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
		raw := `{"action":"refine_prompt","diagnosis":"graph_search returned [project] org.sensorhub but agent ignored it.","refined_prompt":"Use the project entity from graph_search as the parent groupId.","recovery_succeeded":true}`
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

func TestBuildUserPromptIncludesContext(t *testing.T) {
	out := buildUserPrompt(recoveryPromptInput{
		Layer:               "phase_local",
		Slug:                "my-plan",
		TaskID:              "task-42",
		LoopID:              "loop-xyz",
		EscalationReason:    "fixable rejections exceeded TDD cycle budget",
		LastFailureFeedback: "Test failure: NullPointerException at line 17",
		TrajectorySteps:     []string{"model_call(planner)", "tool_call(bash) → ls", "tool_call(graph_search) → no hits"},
	})

	mustContain := []string{
		"phase_local",
		"my-plan",
		"task-42",
		"loop-xyz",
		"fixable rejections exceeded TDD cycle budget",
		"NullPointerException",
		"tool_call(bash)",
		"submit_work",
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("expected prompt to contain %q\nfull prompt:\n%s", want, out)
		}
	}
}

func TestBuildUserPromptHandlesEmptyTrajectory(t *testing.T) {
	out := buildUserPrompt(recoveryPromptInput{
		Layer:            "phase_local",
		Slug:             "no-traj",
		EscalationReason: "iter=50 budget exhausted",
	})
	if !strings.Contains(out, "no trajectory available") {
		t.Errorf("expected fallback notice when trajectory is empty\nfull prompt:\n%s", out)
	}
}
