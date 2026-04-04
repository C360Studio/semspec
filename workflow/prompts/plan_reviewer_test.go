package prompts

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPlanReviewerUserPrompt_Round0_NoCompleteness(t *testing.T) {
	prompt := PlanReviewerUserPrompt("test-plan", `{"goal":"test"}`, true, 0)

	if strings.Contains(prompt, "Completeness Criteria") {
		t.Error("round=0 should NOT include completeness criteria (backwards compat)")
	}
	if !strings.Contains(prompt, "against the project standards") {
		t.Error("hasStandards=true should mention reviewing against standards")
	}
	if strings.Contains(prompt, "No project standards are configured") {
		t.Error("hasStandards=true should NOT include no-standards message")
	}
	if !strings.Contains(prompt, "test-plan") {
		t.Error("plan slug should be present")
	}
}

func TestPlanReviewerUserPrompt_Round1_Completeness(t *testing.T) {
	prompt := PlanReviewerUserPrompt("test-plan", `{"goal":"test"}`, false, 1)

	if !strings.Contains(prompt, "Completeness Criteria (Round 1") {
		t.Error("round=1 should include R1 completeness criteria")
	}
	if !strings.Contains(prompt, "Goal clarity") {
		t.Error("R1 should check goal clarity")
	}
	if !strings.Contains(prompt, "Context sufficiency") {
		t.Error("R1 should check context sufficiency")
	}
	if !strings.Contains(prompt, "Scope validity") {
		t.Error("R1 should check scope validity")
	}
	if strings.Contains(prompt, "Goal coverage") {
		t.Error("R1 should NOT include R2 criteria like 'Goal coverage'")
	}
}

func TestPlanReviewerUserPrompt_Round2_Completeness(t *testing.T) {
	prompt := PlanReviewerUserPrompt("test-plan", `{"goal":"test"}`, false, 2)

	if !strings.Contains(prompt, "Completeness Criteria (Round 2") {
		t.Error("round=2 should include R2 completeness criteria")
	}
	if !strings.Contains(prompt, "Goal coverage") {
		t.Error("R2 should check goal coverage")
	}
	if !strings.Contains(prompt, "Scenario coverage") {
		t.Error("R2 should check requirement→scenario coverage")
	}
	if !strings.Contains(prompt, "Dependency validity") {
		t.Error("R2 should check dependency validity")
	}
	if !strings.Contains(prompt, "orphaned scenarios") {
		t.Error("R2 should check for orphaned scenarios")
	}
	if !strings.Contains(prompt, "Scope alignment") {
		t.Error("R2 should check scope alignment")
	}
	if strings.Contains(prompt, "Goal clarity") {
		t.Error("R2 should NOT include R1 criteria like 'Goal clarity'")
	}
}

func TestPlanReviewerUserPrompt_CompletenessInstructions(t *testing.T) {
	prompt := PlanReviewerUserPrompt("test-plan", `{"goal":"test"}`, false, 1)

	if !strings.Contains(prompt, "completeness criteria") {
		t.Error("round>0 should instruct to evaluate completeness criteria")
	}
}

func TestPlanReviewFinding_CategoryField(t *testing.T) {
	finding := PlanReviewFinding{
		SOPID:    "completeness.goal",
		SOPTitle: "Goal Clarity",
		Severity: "error",
		Status:   "violation",
		Category: "completeness",
		Issue:    "Goal is too vague",
	}

	data, err := json.Marshal(finding)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed PlanReviewFinding
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed.Category != "completeness" {
		t.Errorf("Category = %q, want 'completeness'", parsed.Category)
	}
}

func TestPlanReviewFinding_CategoryOmittedWhenEmpty(t *testing.T) {
	finding := PlanReviewFinding{
		SOPID:    "sop.test",
		SOPTitle: "Test SOP",
		Severity: "error",
		Status:   "violation",
	}

	data, err := json.Marshal(finding)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if strings.Contains(string(data), "category") {
		t.Error("category should be omitted when empty (omitempty tag)")
	}
}

func TestErrorFindings_IncludesCompleteness(t *testing.T) {
	result := &PlanReviewResult{
		Verdict: "needs_changes",
		Summary: "Completeness issues found",
		Findings: []PlanReviewFinding{
			{
				SOPID:    "sop.test",
				SOPTitle: "Test SOP",
				Severity: "warning",
				Status:   "compliant",
				Category: "sop",
			},
			{
				SOPID:    "completeness.goal",
				SOPTitle: "Goal Clarity",
				Severity: "error",
				Status:   "violation",
				Category: "completeness",
				Issue:    "Goal is too vague",
			},
		},
	}

	errors := result.ErrorFindings()
	if len(errors) != 1 {
		t.Fatalf("ErrorFindings() count = %d, want 1", len(errors))
	}
	if errors[0].Category != "completeness" {
		t.Errorf("ErrorFindings()[0].Category = %q, want 'completeness'", errors[0].Category)
	}
}
