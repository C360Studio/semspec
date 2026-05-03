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

func TestNormalizeVerdict(t *testing.T) {
	errorFinding := PlanReviewFinding{
		SOPID: "scope.path-validation", SOPTitle: "Scope Path Validation",
		Severity: "error", Status: "violation", Category: "completeness",
		Issue: "Scope references non-existent path 'internal-auth'",
	}
	warningFinding := PlanReviewFinding{
		SOPID: "sop.style", SOPTitle: "Style",
		Severity: "warning", Status: "violation",
	}
	compliantFinding := PlanReviewFinding{
		SOPID: "sop.test", SOPTitle: "Test",
		Severity: "info", Status: "compliant",
	}

	tests := []struct {
		name         string
		inputVerdict string
		findings     []PlanReviewFinding
		want         string
	}{
		{
			name:         "approved with error finding gets upgraded to needs_changes",
			inputVerdict: "approved",
			findings:     []PlanReviewFinding{errorFinding},
			want:         "needs_changes",
		},
		{
			name:         "approved with no error findings stays approved",
			inputVerdict: "approved",
			findings:     []PlanReviewFinding{compliantFinding},
			want:         "approved",
		},
		{
			name:         "approved with only warning findings stays approved",
			inputVerdict: "approved",
			findings:     []PlanReviewFinding{warningFinding},
			want:         "approved",
		},
		{
			name:         "needs_changes with no error findings gets downgraded to approved",
			inputVerdict: "needs_changes",
			findings:     []PlanReviewFinding{compliantFinding},
			want:         "approved",
		},
		{
			name:         "needs_changes with error findings stays needs_changes",
			inputVerdict: "needs_changes",
			findings:     []PlanReviewFinding{errorFinding},
			want:         "needs_changes",
		},
		{
			name:         "approved with empty findings stays approved",
			inputVerdict: "approved",
			findings:     nil,
			want:         "approved",
		},
		{
			name:         "needs_changes with empty findings gets downgraded to approved",
			inputVerdict: "needs_changes",
			findings:     nil,
			want:         "approved",
		},
		{
			name:         "approved with mixed findings (one error) gets upgraded",
			inputVerdict: "approved",
			findings:     []PlanReviewFinding{compliantFinding, warningFinding, errorFinding},
			want:         "needs_changes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &PlanReviewResult{
				Verdict:  tt.inputVerdict,
				Findings: tt.findings,
			}
			r.NormalizeVerdict()
			if r.Verdict != tt.want {
				t.Errorf("NormalizeVerdict(): verdict = %q, want %q", r.Verdict, tt.want)
			}
		})
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
