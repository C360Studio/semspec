package taskreviewer

import (
	"log/slog"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestValidateBasicTaskStructure_AllTasksHaveAcceptanceCriteria(t *testing.T) {
	c := &Component{logger: slog.Default()}

	tasks := []workflow.Task{
		{
			ID:          "task.feature.1",
			Description: "Implement feature",
			Type:        workflow.TaskTypeImplement,
			AcceptanceCriteria: []workflow.AcceptanceCriterion{
				{Given: "condition", When: "action", Then: "result"},
			},
		},
		{
			ID:          "task.feature.2",
			Description: "Write tests",
			Type:        workflow.TaskTypeTest,
			AcceptanceCriteria: []workflow.AcceptanceCriterion{
				{Given: "setup", When: "test runs", Then: "passes"},
			},
		},
	}

	result, err := c.validateBasicTaskStructure(tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Verdict != "approved" {
		t.Errorf("Verdict = %q, want %q", result.Verdict, "approved")
	}
	if len(result.Findings) != 0 {
		t.Errorf("Findings count = %d, want 0", len(result.Findings))
	}
	if result.Summary == "" {
		t.Error("Summary should not be empty")
	}
}

func TestValidateBasicTaskStructure_MissingAcceptanceCriteria(t *testing.T) {
	c := &Component{logger: slog.Default()}

	tasks := []workflow.Task{
		{
			ID:          "task.feature.1",
			Description: "Implement feature",
			Type:        workflow.TaskTypeImplement,
			AcceptanceCriteria: []workflow.AcceptanceCriterion{
				{Given: "condition", When: "action", Then: "result"},
			},
		},
		{
			ID:                 "task.feature.2",
			Description:        "Write tests",
			Type:               workflow.TaskTypeTest,
			AcceptanceCriteria: []workflow.AcceptanceCriterion{}, // Empty!
		},
	}

	result, err := c.validateBasicTaskStructure(tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Verdict != "needs_changes" {
		t.Errorf("Verdict = %q, want %q", result.Verdict, "needs_changes")
	}
	if len(result.Findings) != 1 {
		t.Fatalf("Findings count = %d, want 1", len(result.Findings))
	}

	finding := result.Findings[0]
	if finding.Severity != "error" {
		t.Errorf("Finding severity = %q, want %q", finding.Severity, "error")
	}
	if finding.Status != "violation" {
		t.Errorf("Finding status = %q, want %q", finding.Status, "violation")
	}
	if finding.TaskID != "task.feature.2" {
		t.Errorf("Finding TaskID = %q, want %q", finding.TaskID, "task.feature.2")
	}
}

func TestValidateBasicTaskStructure_AllTasksMissingAcceptanceCriteria(t *testing.T) {
	c := &Component{logger: slog.Default()}

	tasks := []workflow.Task{
		{
			ID:                 "task.feature.1",
			Description:        "Task 1",
			AcceptanceCriteria: nil,
		},
		{
			ID:                 "task.feature.2",
			Description:        "Task 2",
			AcceptanceCriteria: []workflow.AcceptanceCriterion{},
		},
		{
			ID:          "task.feature.3",
			Description: "Task 3",
			// AcceptanceCriteria not set
		},
	}

	result, err := c.validateBasicTaskStructure(tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Verdict != "needs_changes" {
		t.Errorf("Verdict = %q, want %q", result.Verdict, "needs_changes")
	}
	if len(result.Findings) != 3 {
		t.Errorf("Findings count = %d, want 3", len(result.Findings))
	}

	// Each finding should reference the correct task
	taskIDs := make(map[string]bool)
	for _, f := range result.Findings {
		taskIDs[f.TaskID] = true
	}
	for _, task := range tasks {
		if !taskIDs[task.ID] {
			t.Errorf("Missing finding for task: %s", task.ID)
		}
	}
}

func TestValidateBasicTaskStructure_EmptyTasks(t *testing.T) {
	c := &Component{logger: slog.Default()}

	tasks := []workflow.Task{}

	result, err := c.validateBasicTaskStructure(tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Empty tasks should be approved (no violations)
	if result.Verdict != "approved" {
		t.Errorf("Verdict = %q, want %q", result.Verdict, "approved")
	}
}

func TestValidateBasicTaskStructure_FindingFields(t *testing.T) {
	c := &Component{logger: slog.Default()}

	tasks := []workflow.Task{
		{
			ID:                 "task.test.1",
			Description:        "Missing criteria",
			AcceptanceCriteria: nil,
		},
	}

	result, err := c.validateBasicTaskStructure(tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Findings) != 1 {
		t.Fatalf("Findings count = %d, want 1", len(result.Findings))
	}

	f := result.Findings[0]

	// Check all fields are populated correctly
	if f.SOPID != "builtin.acceptance-criteria" {
		t.Errorf("SOPID = %q, want %q", f.SOPID, "builtin.acceptance-criteria")
	}
	if f.SOPTitle != "Acceptance Criteria Requirement" {
		t.Errorf("SOPTitle = %q, want %q", f.SOPTitle, "Acceptance Criteria Requirement")
	}
	if f.Issue == "" {
		t.Error("Issue should not be empty")
	}
	if f.Suggestion == "" {
		t.Error("Suggestion should not be empty")
	}
	if f.Suggestion == "" || f.Issue == "" {
		t.Error("Both Issue and Suggestion should be populated")
	}
}

func TestParseReviewFromResponse_ComplexResponse(t *testing.T) {
	c := &Component{logger: slog.Default()}

	// Simulate a realistic LLM response with multiple findings
	content := `Based on my review of the tasks against the SOPs, here's my assessment:

` + "```json" + `
{
  "verdict": "needs_changes",
  "summary": "Tasks are missing test coverage and documentation requirements per SOPs",
  "findings": [
    {
      "sop_id": "source.doc.sops.api-testing",
      "sop_title": "API Testing Standards",
      "severity": "error",
      "status": "violation",
      "issue": "SOP requires tests for all API endpoints, but no test task exists",
      "suggestion": "Add a task with type='test' targeting the API files"
    },
    {
      "sop_id": "source.doc.sops.documentation",
      "sop_title": "Documentation Standards",
      "severity": "warning",
      "status": "violation",
      "issue": "API changes should be documented",
      "suggestion": "Consider adding a documentation task"
    },
    {
      "sop_id": "source.doc.sops.coding",
      "sop_title": "Coding Standards",
      "severity": "info",
      "status": "compliant",
      "issue": "",
      "suggestion": ""
    }
  ]
}
` + "```" + `

The main blocking issue is the missing test task. Once that's added, the review can proceed.`

	result, err := c.parseReviewFromResponse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Verdict != "needs_changes" {
		t.Errorf("Verdict = %q, want %q", result.Verdict, "needs_changes")
	}
	if len(result.Findings) != 3 {
		t.Fatalf("Findings count = %d, want 3", len(result.Findings))
	}

	// Check error findings
	errors := result.ErrorFindings()
	if len(errors) != 1 {
		t.Errorf("ErrorFindings count = %d, want 1", len(errors))
	}
	if errors[0].SOPID != "source.doc.sops.api-testing" {
		t.Errorf("Error finding SOPID = %q, want %q", errors[0].SOPID, "source.doc.sops.api-testing")
	}
}

func TestParseReviewFromResponse_ApprovedWithCompliantFindings(t *testing.T) {
	c := &Component{logger: slog.Default()}

	content := `{
  "verdict": "approved",
  "summary": "All tasks meet SOP requirements",
  "findings": [
    {
      "sop_id": "source.doc.sops.testing",
      "sop_title": "Testing Standards",
      "severity": "info",
      "status": "compliant"
    },
    {
      "sop_id": "source.doc.sops.coding",
      "sop_title": "Coding Standards",
      "severity": "info",
      "status": "compliant"
    }
  ]
}`

	result, err := c.parseReviewFromResponse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsApproved() {
		t.Error("IsApproved() should return true")
	}
	if len(result.Findings) != 2 {
		t.Errorf("Findings count = %d, want 2", len(result.Findings))
	}
	if len(result.ErrorFindings()) != 0 {
		t.Errorf("ErrorFindings count = %d, want 0", len(result.ErrorFindings()))
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "missing stream_name",
			config: Config{
				StreamName:     "",
				ConsumerName:   "consumer",
				TriggerSubject: "trigger",
			},
			wantErr: true,
		},
		{
			name: "missing consumer_name",
			config: Config{
				StreamName:     "stream",
				ConsumerName:   "",
				TriggerSubject: "trigger",
			},
			wantErr: true,
		},
		{
			name: "missing trigger_subject",
			config: Config{
				StreamName:     "stream",
				ConsumerName:   "consumer",
				TriggerSubject: "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.StreamName != "WORKFLOW" {
		t.Errorf("StreamName = %q, want %q", config.StreamName, "WORKFLOW")
	}
	if config.ConsumerName != "task-reviewer" {
		t.Errorf("ConsumerName = %q, want %q", config.ConsumerName, "task-reviewer")
	}
	if config.TriggerSubject != "workflow.async.task-reviewer" {
		t.Errorf("TriggerSubject = %q, want %q", config.TriggerSubject, "workflow.async.task-reviewer")
	}
	if config.DefaultCapability != "reviewing" {
		t.Errorf("DefaultCapability = %q, want %q", config.DefaultCapability, "reviewing")
	}
}

func TestConfig_GetContextTimeout(t *testing.T) {
	tests := []struct {
		name          string
		timeout       string
		wantSeconds   int
		wantMinimum30 bool
	}{
		{"default 60s", "60s", 60, true},
		{"30s", "30s", 30, true},
		{"invalid", "invalid", 30, true}, // Should default to 30s
		{"empty", "", 30, true},          // Should default to 30s
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Config{ContextTimeout: tt.timeout}
			duration := config.GetContextTimeout()

			seconds := int(duration.Seconds())
			if tt.wantMinimum30 && seconds < 30 {
				t.Errorf("GetContextTimeout() = %v, want at least 30s", duration)
			}
		})
	}
}
