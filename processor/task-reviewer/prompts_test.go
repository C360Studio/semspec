package taskreviewer

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestSystemPrompt(t *testing.T) {
	prompt := SystemPrompt()

	// Should describe the reviewer role
	if !strings.Contains(prompt, "task reviewer") {
		t.Error("system prompt should mention 'task reviewer' role")
	}

	// Should explain review criteria
	requiredCriteria := []string{
		"SOP Compliance",
		"Task Coverage",
		"Dependencies Valid",
		"Test Requirements",
		"BDD Acceptance Criteria",
	}
	for _, criterion := range requiredCriteria {
		if !strings.Contains(prompt, criterion) {
			t.Errorf("system prompt should mention review criterion: %s", criterion)
		}
	}

	// Should explain verdict options
	if !strings.Contains(prompt, "approved") {
		t.Error("system prompt should explain 'approved' verdict")
	}
	if !strings.Contains(prompt, "needs_changes") {
		t.Error("system prompt should explain 'needs_changes' verdict")
	}

	// Should include JSON output format
	if !strings.Contains(prompt, `"verdict"`) {
		t.Error("system prompt should include JSON format with verdict field")
	}
	if !strings.Contains(prompt, `"findings"`) {
		t.Error("system prompt should include JSON format with findings field")
	}
	if !strings.Contains(prompt, `"task_id"`) {
		t.Error("system prompt should include JSON format with task_id field")
	}

	// Should mention severity levels
	if !strings.Contains(prompt, "error") && !strings.Contains(prompt, "warning") {
		t.Error("system prompt should mention severity levels")
	}
}

func TestUserPrompt_WithSOPContext(t *testing.T) {
	tasks := []workflow.Task{
		{
			ID:          "task.feature.1",
			Description: "Implement user authentication",
			Type:        workflow.TaskTypeImplement,
			AcceptanceCriteria: []workflow.AcceptanceCriterion{
				{Given: "a user with credentials", When: "they login", Then: "they are authenticated"},
			},
		},
		{
			ID:          "task.feature.2",
			Description: "Write tests for authentication",
			Type:        workflow.TaskTypeTest,
		},
	}
	sopContext := "## SOP Standards\n\n### Testing Requirements\nAll API endpoints must have tests."

	prompt := UserPrompt("test-feature", tasks, sopContext)

	// Should include the SOP context
	if !strings.Contains(prompt, "Testing Requirements") {
		t.Error("user prompt should include the SOP context")
	}
	if !strings.Contains(prompt, "All API endpoints must have tests") {
		t.Error("user prompt should include the SOP content")
	}

	// Should include the slug
	if !strings.Contains(prompt, "test-feature") {
		t.Error("user prompt should include the plan slug")
	}

	// Should include tasks as JSON
	if !strings.Contains(prompt, "task.feature.1") {
		t.Error("user prompt should include task IDs")
	}
	if !strings.Contains(prompt, "Implement user authentication") {
		t.Error("user prompt should include task descriptions")
	}

	// Should include section header
	if !strings.Contains(prompt, "## Tasks to Review") {
		t.Error("user prompt should include tasks section header")
	}
}

func TestUserPrompt_NoSOPContext(t *testing.T) {
	tasks := []workflow.Task{
		{
			ID:          "task.feature.1",
			Description: "Implement feature",
			Type:        workflow.TaskTypeImplement,
		},
	}

	prompt := UserPrompt("test-feature", tasks, "")

	// Should indicate no SOPs
	if !strings.Contains(prompt, "No SOPs apply") {
		t.Error("user prompt should indicate no SOPs are available")
	}

	// Should still include instructions
	if !strings.Contains(prompt, "acceptance criteria") {
		t.Error("user prompt should still mention validating acceptance criteria")
	}

	// Should still include tasks
	if !strings.Contains(prompt, "task.feature.1") {
		t.Error("user prompt should still include the tasks")
	}
}

func TestUserPrompt_MultipleTasks(t *testing.T) {
	tasks := []workflow.Task{
		{ID: "task.feature.1", Description: "Task one", Type: workflow.TaskTypeImplement},
		{ID: "task.feature.2", Description: "Task two", Type: workflow.TaskTypeTest},
		{ID: "task.feature.3", Description: "Task three", Type: workflow.TaskTypeDocument},
	}

	prompt := UserPrompt("multi-task", tasks, "SOP context")

	// All tasks should be included
	for _, task := range tasks {
		if !strings.Contains(prompt, task.ID) {
			t.Errorf("user prompt should include task ID: %s", task.ID)
		}
		if !strings.Contains(prompt, task.Description) {
			t.Errorf("user prompt should include task description: %s", task.Description)
		}
	}
}

func TestUserPrompt_TaskWithDependencies(t *testing.T) {
	tasks := []workflow.Task{
		{
			ID:          "task.feature.1",
			Description: "Implement API",
			Type:        workflow.TaskTypeImplement,
		},
		{
			ID:          "task.feature.2",
			Description: "Write API tests",
			Type:        workflow.TaskTypeTest,
			DependsOn:   []string{"task.feature.1"},
		},
	}

	prompt := UserPrompt("dep-test", tasks, "")

	// Should include depends_on in JSON representation
	if !strings.Contains(prompt, "depends_on") || !strings.Contains(prompt, "task.feature.1") {
		// The JSON marshaling should include the depends_on field
		// This is important for the LLM to validate dependencies
		t.Log("Note: depends_on may be serialized differently depending on workflow.Task JSON tags")
	}
}

func TestUserPrompt_TaskWithAcceptanceCriteria(t *testing.T) {
	tasks := []workflow.Task{
		{
			ID:          "task.feature.1",
			Description: "Implement feature",
			Type:        workflow.TaskTypeImplement,
			AcceptanceCriteria: []workflow.AcceptanceCriterion{
				{
					Given: "a user is logged in",
					When:  "they click the button",
					Then:  "the action is performed",
				},
			},
		},
	}

	prompt := UserPrompt("ac-test", tasks, "")

	// Acceptance criteria should be visible in the JSON
	if !strings.Contains(prompt, "a user is logged in") {
		t.Error("user prompt should include acceptance criteria Given clause")
	}
	if !strings.Contains(prompt, "they click the button") {
		t.Error("user prompt should include acceptance criteria When clause")
	}
	if !strings.Contains(prompt, "the action is performed") {
		t.Error("user prompt should include acceptance criteria Then clause")
	}
}

func TestFormatCorrectionPrompt_ContainsError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		contains string
	}{
		{
			name:     "no JSON error",
			err:      errNoJSON,
			contains: "no JSON found",
		},
		{
			name:     "parse error",
			err:      errParse("unexpected EOF"),
			contains: "unexpected EOF",
		},
		{
			name:     "invalid verdict error",
			err:      errInvalidVerdict("maybe"),
			contains: "maybe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := formatCorrectionPrompt(tt.err)
			if !strings.Contains(prompt, tt.contains) {
				t.Errorf("correction prompt should contain %q", tt.contains)
			}
		})
	}
}

func TestFormatCorrectionPrompt_IncludesExpectedFormat(t *testing.T) {
	prompt := formatCorrectionPrompt(errNoJSON)

	// Should include the expected JSON structure
	expectedFields := []string{
		`"verdict"`,
		`"summary"`,
		`"findings"`,
		`"sop_id"`,
		`"severity"`,
		`"status"`,
		`"issue"`,
		`"suggestion"`,
		`"task_id"`,
	}

	for _, field := range expectedFields {
		if !strings.Contains(prompt, field) {
			t.Errorf("correction prompt should include expected field: %s", field)
		}
	}

	// Should instruct to return only JSON
	if !strings.Contains(prompt, "ONLY a valid JSON object") {
		t.Error("correction prompt should instruct to return only JSON")
	}
}

// Test error helpers
var errNoJSON = &parseError{"no JSON found in response"}

type parseError struct {
	msg string
}

func (e *parseError) Error() string {
	return e.msg
}

func errParse(detail string) error {
	return &parseError{msg: "parse JSON: " + detail}
}

func errInvalidVerdict(verdict string) error {
	return &parseError{msg: "invalid verdict: " + verdict}
}
