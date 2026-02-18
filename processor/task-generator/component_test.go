package taskgenerator

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/workflow"
)

func TestExtractJSON_MarkdownCodeBlock(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "json code block with language tag",
			input: `Here's the task list:

` + "```json" + `
{"tasks": [{"description": "test task", "type": "implement"}]}
` + "```" + `

Let me know if you need more.`,
		},
		{
			name: "code block without language tag",
			input: `Tasks:

` + "```" + `
{"tasks": []}
` + "```",
		},
		{
			name:  "json code block with whitespace",
			input: "```json\n  {\"tasks\": [{\"description\": \"task1\"}]}  \n```",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := llm.ExtractJSON(tt.input)
			if got == "" {
				t.Fatal("expected JSON, got empty string")
			}
			var parsed map[string]any
			if err := json.Unmarshal([]byte(got), &parsed); err != nil {
				t.Errorf("result is not valid JSON: %v", err)
			}
		})
	}
}

func TestExtractJSON_RawJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "raw JSON object",
			input: `{"tasks": [{"description": "test", "type": "implement"}]}`,
		},
		{
			name:  "JSON with surrounding text",
			input: `I'll create the following tasks: {"tasks": [{"description": "task 1"}]} That's the list.`,
		},
		{
			name: "multiline raw JSON",
			input: `{
  "tasks": [
    {"description": "first task"},
    {"description": "second task"}
  ]
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := llm.ExtractJSON(tt.input)
			if got == "" {
				t.Fatal("expected JSON, got empty string")
			}
			var parsed map[string]any
			if err := json.Unmarshal([]byte(got), &parsed); err != nil {
				t.Errorf("result is not valid JSON: %v", err)
			}
		})
	}
}

func TestExtractJSON_NoJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "no JSON content",
			input: "Just some plain text without any JSON.",
		},
		{
			name:  "empty string",
			input: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := llm.ExtractJSON(tt.input)
			if got != "" {
				t.Errorf("expected empty string, got %q", got)
			}
		})
	}
}

func TestParseTasksFromResponse_ValidJSON(t *testing.T) {
	c := &Component{}
	slug := "test-feature"

	content := `{
		"tasks": [
			{
				"description": "Create user authentication module",
				"type": "implement",
				"acceptance_criteria": [
					{
						"given": "a user with valid credentials",
						"when": "they submit the login form",
						"then": "they are redirected to the dashboard"
					}
				],
				"files": ["auth/login.go", "auth/handler.go"]
			},
			{
				"description": "Add password validation",
				"type": "test",
				"acceptance_criteria": [
					{
						"given": "a password shorter than 8 characters",
						"when": "submitted",
						"then": "validation error is returned"
					}
				]
			}
		]
	}`

	tasks, err := c.parseTasksFromResponse(content, slug)
	if err != nil {
		t.Fatalf("parseTasksFromResponse() error = %v", err)
	}

	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}

	// Check first task
	task1 := tasks[0]
	if task1.ID != workflow.TaskEntityID("test-feature", 1) {
		t.Errorf("task1.ID = %q, want %q", task1.ID, workflow.TaskEntityID("test-feature", 1))
	}
	if task1.PlanID != workflow.PlanEntityID("test-feature") {
		t.Errorf("task1.PlanID = %q, want %q", task1.PlanID, workflow.PlanEntityID("test-feature"))
	}
	if task1.Sequence != 1 {
		t.Errorf("task1.Sequence = %d, want %d", task1.Sequence, 1)
	}
	if task1.Description != "Create user authentication module" {
		t.Errorf("task1.Description = %q", task1.Description)
	}
	if task1.Type != workflow.TaskTypeImplement {
		t.Errorf("task1.Type = %q, want %q", task1.Type, workflow.TaskTypeImplement)
	}
	if task1.Status != workflow.TaskStatusPending {
		t.Errorf("task1.Status = %q, want %q", task1.Status, workflow.TaskStatusPending)
	}
	if len(task1.AcceptanceCriteria) != 1 {
		t.Fatalf("expected 1 acceptance criterion, got %d", len(task1.AcceptanceCriteria))
	}
	ac := task1.AcceptanceCriteria[0]
	if ac.Given != "a user with valid credentials" {
		t.Errorf("ac.Given = %q", ac.Given)
	}
	if ac.When != "they submit the login form" {
		t.Errorf("ac.When = %q", ac.When)
	}
	if ac.Then != "they are redirected to the dashboard" {
		t.Errorf("ac.Then = %q", ac.Then)
	}
	if len(task1.Files) != 2 {
		t.Errorf("expected 2 files, got %d", len(task1.Files))
	}

	// Check second task
	task2 := tasks[1]
	if task2.ID != workflow.TaskEntityID("test-feature", 2) {
		t.Errorf("task2.ID = %q, want %q", task2.ID, workflow.TaskEntityID("test-feature", 2))
	}
	if task2.Type != workflow.TaskTypeTest {
		t.Errorf("task2.Type = %q, want %q", task2.Type, workflow.TaskTypeTest)
	}
}

func TestParseTasksFromResponse_EmptyTasks(t *testing.T) {
	c := &Component{}
	content := `{"tasks": []}`

	tasks, err := c.parseTasksFromResponse(content, "empty-slug")
	if err != nil {
		t.Fatalf("parseTasksFromResponse() error = %v", err)
	}

	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestParseTasksFromResponse_DefaultType(t *testing.T) {
	c := &Component{}
	// Task without type should default to "implement"
	content := `{
		"tasks": [
			{
				"description": "Task without explicit type",
				"acceptance_criteria": []
			}
		]
	}`

	tasks, err := c.parseTasksFromResponse(content, "default-type")
	if err != nil {
		t.Fatalf("parseTasksFromResponse() error = %v", err)
	}

	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	if tasks[0].Type != workflow.TaskTypeImplement {
		t.Errorf("task.Type = %q, want default %q", tasks[0].Type, workflow.TaskTypeImplement)
	}
}

func TestParseTasksFromResponse_WithMarkdownBlock(t *testing.T) {
	c := &Component{}
	// Test that it handles content wrapped in markdown code blocks
	content := "Here are the generated tasks:\n\n```json\n" +
		`{"tasks": [{"description": "Markdown wrapped task", "type": "test"}]}` +
		"\n```\n\nLet me know if you need changes."

	tasks, err := c.parseTasksFromResponse(content, "markdown-slug")
	if err != nil {
		t.Fatalf("parseTasksFromResponse() error = %v", err)
	}

	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	if tasks[0].Description != "Markdown wrapped task" {
		t.Errorf("task.Description = %q", tasks[0].Description)
	}
}

func TestParseTasksFromResponse_InvalidJSON(t *testing.T) {
	c := &Component{}

	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "no JSON at all",
			content: "Just plain text without JSON",
		},
		{
			name:    "malformed JSON",
			content: `{"tasks": [{"description": incomplete`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.parseTasksFromResponse(tt.content, "error-slug")
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestParseTasksFromResponse_MultipleAcceptanceCriteria(t *testing.T) {
	c := &Component{}
	content := `{
		"tasks": [
			{
				"description": "Task with multiple criteria",
				"type": "implement",
				"acceptance_criteria": [
					{
						"given": "condition 1",
						"when": "action 1",
						"then": "result 1"
					},
					{
						"given": "condition 2",
						"when": "action 2",
						"then": "result 2"
					},
					{
						"given": "condition 3",
						"when": "action 3",
						"then": "result 3"
					}
				]
			}
		]
	}`

	tasks, err := c.parseTasksFromResponse(content, "multi-ac")
	if err != nil {
		t.Fatalf("parseTasksFromResponse() error = %v", err)
	}

	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	if len(tasks[0].AcceptanceCriteria) != 3 {
		t.Errorf("expected 3 acceptance criteria, got %d", len(tasks[0].AcceptanceCriteria))
	}
}

func TestExtractJSON_ComplexLLMResponse(t *testing.T) {
	// Simulate a realistic LLM response
	input := `Based on the plan's goal and context, I've generated the following tasks:

` + "```json" + `
{
  "tasks": [
    {
      "description": "Implement JWT token generation endpoint",
      "type": "implement",
      "acceptance_criteria": [
        {
          "given": "valid user credentials",
          "when": "POST /api/auth/token is called",
          "then": "a valid JWT is returned with 200 status"
        }
      ],
      "files": ["api/auth/handler.go", "api/auth/jwt.go"]
    },
    {
      "description": "Add token refresh mechanism",
      "type": "implement",
      "acceptance_criteria": [
        {
          "given": "an expired JWT with valid refresh token",
          "when": "POST /api/auth/refresh is called",
          "then": "a new JWT is issued"
        }
      ],
      "files": ["api/auth/refresh.go"]
    }
  ]
}
` + "```" + `

These tasks follow the BDD acceptance criteria format and target the files specified in the plan's scope.`

	got := llm.ExtractJSON(input)
	if got == "" {
		t.Fatal("ExtractJSON() returned empty string")
	}

	// Verify it's valid JSON that can be unmarshaled
	c := &Component{}
	tasks, err := c.parseTasksFromResponse(got, "complex-test")
	if err != nil {
		t.Fatalf("failed to parse extracted JSON: %v", err)
	}

	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks from complex response, got %d", len(tasks))
	}
}
