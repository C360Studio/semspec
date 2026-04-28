package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

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
