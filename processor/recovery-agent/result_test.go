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

// Note: the user-prompt content tests previously here (TestBuildUserPrompt*)
// moved to prompt/domain/software_render_test.go when the recovery-agent
// dispatch was wired through the assembler — 2026-05-11. The renderer
// lives in prompt/domain now; testing it in the package that owns it is
// the natural shape and removes the recovery-agent test file's dependency
// on internal prompt construction.
