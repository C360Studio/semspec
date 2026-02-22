package taskreviewer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestTaskReviewTrigger_Validate(t *testing.T) {
	tests := []struct {
		name    string
		trigger TaskReviewTrigger
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid trigger",
			trigger: TaskReviewTrigger{
				RequestID: "req-123",
				Slug:      "test-feature",
				Tasks: []workflow.Task{
					{ID: "task.test-feature.1", Description: "Test task"},
				},
			},
			wantErr: false,
		},
		{
			name: "missing request_id",
			trigger: TaskReviewTrigger{
				Slug: "test-feature",
				Tasks: []workflow.Task{
					{ID: "task.test-feature.1", Description: "Test task"},
				},
			},
			wantErr: true,
			errMsg:  "request_id is required",
		},
		{
			name: "missing slug",
			trigger: TaskReviewTrigger{
				RequestID: "req-123",
				Tasks: []workflow.Task{
					{ID: "task.test-feature.1", Description: "Test task"},
				},
			},
			wantErr: true,
			errMsg:  "slug is required",
		},
		{
			name: "missing tasks",
			trigger: TaskReviewTrigger{
				RequestID: "req-123",
				Slug:      "test-feature",
				Tasks:     []workflow.Task{},
			},
			wantErr: true,
			errMsg:  "tasks are required",
		},
		{
			name: "nil tasks",
			trigger: TaskReviewTrigger{
				RequestID: "req-123",
				Slug:      "test-feature",
			},
			wantErr: true,
			errMsg:  "tasks are required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.trigger.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error = %q, want containing %q", err.Error(), tt.errMsg)
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestTaskReviewTrigger_Schema(t *testing.T) {
	trigger := &TaskReviewTrigger{}
	schema := trigger.Schema()

	if schema.Domain != "workflow" {
		t.Errorf("Domain = %q, want %q", schema.Domain, "workflow")
	}
	if schema.Category != "task-review-trigger" {
		t.Errorf("Category = %q, want %q", schema.Category, "task-review-trigger")
	}
	if schema.Version != "v1" {
		t.Errorf("Version = %q, want %q", schema.Version, "v1")
	}
}

func TestTaskReviewTrigger_JSONRoundTrip(t *testing.T) {
	original := &TaskReviewTrigger{
		RequestID: "req-123",
		Slug:      "test-feature",
		ProjectID: "proj-456",
		Tasks: []workflow.Task{
			{
				ID:          "task.test-feature.1",
				Description: "Implement feature",
				Type:        workflow.TaskTypeImplement,
			},
		},
		ScopePatterns: []string{"api/*.go"},
		SOPContext:    "SOP content here",
		TraceID:       "trace-abc",
		LoopID:        "loop-xyz",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded TaskReviewTrigger
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.RequestID != original.RequestID {
		t.Errorf("RequestID = %q, want %q", decoded.RequestID, original.RequestID)
	}
	if decoded.Slug != original.Slug {
		t.Errorf("Slug = %q, want %q", decoded.Slug, original.Slug)
	}
	if decoded.TraceID != original.TraceID {
		t.Errorf("TraceID = %q, want %q", decoded.TraceID, original.TraceID)
	}
	if len(decoded.Tasks) != len(original.Tasks) {
		t.Errorf("Tasks count = %d, want %d", len(decoded.Tasks), len(original.Tasks))
	}
}

func TestTaskReviewResult_Schema(t *testing.T) {
	result := &TaskReviewResult{}
	schema := result.Schema()

	if schema.Domain != "workflow" {
		t.Errorf("Domain = %q, want %q", schema.Domain, "workflow")
	}
	if schema.Category != "task-review-result" {
		t.Errorf("Category = %q, want %q", schema.Category, "task-review-result")
	}
	if schema.Version != "v1" {
		t.Errorf("Version = %q, want %q", schema.Version, "v1")
	}
}

func TestTaskReviewResult_JSONRoundTrip(t *testing.T) {
	original := &TaskReviewResult{
		RequestID: "req-123",
		Slug:      "test-feature",
		Verdict:   "needs_changes",
		Summary:   "Tasks are missing test coverage",
		Findings: []TaskReviewFinding{
			{
				SOPID:      "source.doc.sops.testing",
				SOPTitle:   "Testing Standards",
				Severity:   "error",
				Status:     "violation",
				Issue:      "No test task found",
				Suggestion: "Add a test task",
				TaskID:     "task.test-feature.1",
			},
		},
		FormattedFindings: "### Violations\n- Testing issue",
		Status:            "completed",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded TaskReviewResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Verdict != original.Verdict {
		t.Errorf("Verdict = %q, want %q", decoded.Verdict, original.Verdict)
	}
	if decoded.FormattedFindings != original.FormattedFindings {
		t.Errorf("FormattedFindings = %q, want %q", decoded.FormattedFindings, original.FormattedFindings)
	}
	if len(decoded.Findings) != len(original.Findings) {
		t.Fatalf("Findings count = %d, want %d", len(decoded.Findings), len(original.Findings))
	}
	if decoded.Findings[0].SOPID != original.Findings[0].SOPID {
		t.Errorf("Finding SOPID = %q, want %q", decoded.Findings[0].SOPID, original.Findings[0].SOPID)
	}
}

func TestLLMTaskReviewResult_IsApproved(t *testing.T) {
	tests := []struct {
		name    string
		verdict string
		want    bool
	}{
		{"approved", "approved", true},
		{"needs_changes", "needs_changes", false},
		{"empty", "", false},
		{"invalid", "invalid", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &LLMTaskReviewResult{Verdict: tt.verdict}
			if got := result.IsApproved(); got != tt.want {
				t.Errorf("IsApproved() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLLMTaskReviewResult_ErrorFindings(t *testing.T) {
	result := &LLMTaskReviewResult{
		Verdict: "needs_changes",
		Findings: []TaskReviewFinding{
			{SOPID: "sop1", Severity: "error", Status: "violation"},
			{SOPID: "sop2", Severity: "warning", Status: "violation"},
			{SOPID: "sop3", Severity: "error", Status: "compliant"},
			{SOPID: "sop4", Severity: "error", Status: "violation"},
			{SOPID: "sop5", Severity: "info", Status: "not_applicable"},
		},
	}

	errors := result.ErrorFindings()
	if len(errors) != 2 {
		t.Fatalf("ErrorFindings() count = %d, want 2", len(errors))
	}

	// Should only include error-severity violations
	for _, f := range errors {
		if f.Severity != "error" {
			t.Errorf("Finding severity = %q, want %q", f.Severity, "error")
		}
		if f.Status != "violation" {
			t.Errorf("Finding status = %q, want %q", f.Status, "violation")
		}
	}
}

func TestLLMTaskReviewResult_ErrorFindings_Empty(t *testing.T) {
	result := &LLMTaskReviewResult{
		Verdict:  "approved",
		Findings: []TaskReviewFinding{},
	}

	errors := result.ErrorFindings()
	if len(errors) != 0 {
		t.Errorf("ErrorFindings() count = %d, want 0", len(errors))
	}
}

func TestLLMTaskReviewResult_FormatFindings_Empty(t *testing.T) {
	result := &LLMTaskReviewResult{
		Verdict:  "approved",
		Findings: []TaskReviewFinding{},
	}

	formatted := result.FormatFindings()
	if formatted != "No findings." {
		t.Errorf("FormatFindings() = %q, want %q", formatted, "No findings.")
	}
}

func TestLLMTaskReviewResult_FormatFindings_Violations(t *testing.T) {
	result := &LLMTaskReviewResult{
		Verdict: "needs_changes",
		Findings: []TaskReviewFinding{
			{
				SOPID:      "source.doc.sops.testing",
				SOPTitle:   "Testing Standards",
				Severity:   "error",
				Status:     "violation",
				Issue:      "No test task exists",
				Suggestion: "Add a test task covering API endpoints",
				TaskID:     "task.feature.1",
			},
		},
	}

	formatted := result.FormatFindings()

	// Should contain violations section
	if !strings.Contains(formatted, "### Violations") {
		t.Error("formatted should contain '### Violations' header")
	}
	// Should contain severity in uppercase
	if !strings.Contains(formatted, "[ERROR]") {
		t.Error("formatted should contain '[ERROR]' severity marker")
	}
	// Should contain SOP ID
	if !strings.Contains(formatted, "source.doc.sops.testing") {
		t.Error("formatted should contain the SOP ID")
	}
	// Should contain SOP title
	if !strings.Contains(formatted, "Testing Standards") {
		t.Error("formatted should contain the SOP title")
	}
	// Should contain issue
	if !strings.Contains(formatted, "No test task exists") {
		t.Error("formatted should contain the issue description")
	}
	// Should contain suggestion
	if !strings.Contains(formatted, "Add a test task covering API endpoints") {
		t.Error("formatted should contain the suggestion")
	}
	// Should contain task ID
	if !strings.Contains(formatted, "task.feature.1") {
		t.Error("formatted should contain the task ID")
	}
	// Should NOT be JSON (raw keys)
	if strings.Contains(formatted, `"sop_id"`) {
		t.Error("formatted should not contain raw JSON keys")
	}
}

func TestLLMTaskReviewResult_FormatFindings_Compliant(t *testing.T) {
	result := &LLMTaskReviewResult{
		Verdict: "approved",
		Findings: []TaskReviewFinding{
			{
				SOPID:    "source.doc.sops.testing",
				SOPTitle: "Testing Standards",
				Severity: "info",
				Status:   "compliant",
			},
			{
				SOPID:    "source.doc.sops.coding",
				SOPTitle: "Coding Standards",
				Severity: "info",
				Status:   "compliant",
			},
		},
	}

	formatted := result.FormatFindings()

	// Should contain compliant section
	if !strings.Contains(formatted, "### Compliant") {
		t.Error("formatted should contain '### Compliant' header")
	}
	// Should contain SOP IDs
	if !strings.Contains(formatted, "source.doc.sops.testing") {
		t.Error("formatted should contain the testing SOP ID")
	}
	if !strings.Contains(formatted, "source.doc.sops.coding") {
		t.Error("formatted should contain the coding SOP ID")
	}
	// Should NOT contain violations section
	if strings.Contains(formatted, "### Violations") {
		t.Error("formatted should not contain '### Violations' when there are no violations")
	}
}

func TestLLMTaskReviewResult_FormatFindings_Mixed(t *testing.T) {
	result := &LLMTaskReviewResult{
		Verdict: "needs_changes",
		Findings: []TaskReviewFinding{
			{
				SOPID:    "source.doc.sops.testing",
				SOPTitle: "Testing Standards",
				Severity: "error",
				Status:   "violation",
				Issue:    "Missing tests",
			},
			{
				SOPID:    "source.doc.sops.coding",
				SOPTitle: "Coding Standards",
				Severity: "info",
				Status:   "compliant",
			},
			{
				SOPID:    "source.doc.sops.deploy",
				SOPTitle: "Deployment Standards",
				Severity: "info",
				Status:   "not_applicable",
			},
		},
	}

	formatted := result.FormatFindings()

	// Should have both sections
	if !strings.Contains(formatted, "### Violations") {
		t.Error("formatted should contain '### Violations' header")
	}
	if !strings.Contains(formatted, "### Compliant") {
		t.Error("formatted should contain '### Compliant' header")
	}
}
