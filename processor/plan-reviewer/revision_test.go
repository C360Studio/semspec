package planreviewer

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semspec/workflow/prompts"
)

func TestRevisionPayloadMarshal(t *testing.T) {
	// Verify the revision mutation payload is correctly structured
	// for the plan-manager's RevisionMutationRequest.
	result := &prompts.PlanReviewResult{
		Verdict: "needs_changes",
		Summary: "Goal is too vague",
		Findings: []prompts.PlanReviewFinding{
			{
				SOPID:      "completeness.goal",
				SOPTitle:   "Goal Clarity",
				Severity:   "error",
				Status:     "violation",
				Issue:      "Goal lacks specificity",
				Suggestion: "Define the endpoint behavior clearly",
			},
		},
	}

	findingsJSON, err := json.Marshal(result.Findings)
	if err != nil {
		t.Fatalf("marshal findings: %v", err)
	}

	// This mirrors the payload construction in sendRevisionMutation.
	payload := map[string]any{
		"slug":     "test-plan",
		"round":    int(roundDraftReview),
		"verdict":  result.Verdict,
		"summary":  result.Summary,
		"findings": json.RawMessage(findingsJSON),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	// Verify the plan-manager can unmarshal it.
	var parsed struct {
		Slug     string          `json:"slug"`
		Round    int             `json:"round"`
		Verdict  string          `json:"verdict"`
		Summary  string          `json:"summary"`
		Findings json.RawMessage `json:"findings"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if parsed.Slug != "test-plan" {
		t.Errorf("Slug = %q, want test-plan", parsed.Slug)
	}
	if parsed.Round != 1 {
		t.Errorf("Round = %d, want 1", parsed.Round)
	}
	if parsed.Verdict != "needs_changes" {
		t.Errorf("Verdict = %q, want needs_changes", parsed.Verdict)
	}
	if parsed.Summary != "Goal is too vague" {
		t.Errorf("Summary = %q, want 'Goal is too vague'", parsed.Summary)
	}

	// Verify findings can be deserialized back.
	var findings []prompts.PlanReviewFinding
	if err := json.Unmarshal(parsed.Findings, &findings); err != nil {
		t.Fatalf("unmarshal findings: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings count = %d, want 1", len(findings))
	}
	if findings[0].Issue != "Goal lacks specificity" {
		t.Errorf("findings[0].Issue = %q, want 'Goal lacks specificity'", findings[0].Issue)
	}
}

func TestRevisionPayloadRound2(t *testing.T) {
	result := &prompts.PlanReviewResult{
		Verdict: "needs_changes",
		Summary: "Missing requirement coverage",
		Findings: []prompts.PlanReviewFinding{
			{
				SOPID:    "completeness.coverage",
				SOPTitle: "Requirement Coverage",
				Severity: "error",
				Status:   "violation",
				Issue:    "Not all requirements have scenarios",
			},
		},
	}

	findingsJSON, err := json.Marshal(result.Findings)
	if err != nil {
		t.Fatalf("marshal findings: %v", err)
	}

	payload := map[string]any{
		"slug":     "test-plan",
		"round":    int(roundScenariosReview),
		"verdict":  result.Verdict,
		"summary":  result.Summary,
		"findings": json.RawMessage(findingsJSON),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	var parsed struct {
		Round int `json:"round"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.Round != 2 {
		t.Errorf("Round = %d, want 2", parsed.Round)
	}
}

func TestReviewRoundConstants(t *testing.T) {
	// Verify round constants match the plan-manager's expected values.
	if int(roundDraftReview) != 1 {
		t.Errorf("roundDraftReview = %d, want 1", roundDraftReview)
	}
	if int(roundScenariosReview) != 2 {
		t.Errorf("roundScenariosReview = %d, want 2", roundScenariosReview)
	}
}
