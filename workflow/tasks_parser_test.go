package workflow

import (
	"testing"
)

func TestParseTasks(t *testing.T) {
	content := `# Implementation Tasks

## Backend

- [ ] Add refresh_token field to User model
- [x] Implement token refresh endpoint
- [ ] Add token expiry validation

## Frontend

- [ ] Create refresh token logic
- [ ] Update auth context
`

	tasks, err := ParseTasks(content)
	if err != nil {
		t.Fatalf("ParseTasks() error = %v", err)
	}

	if len(tasks) != 5 {
		t.Errorf("expected 5 tasks, got %d", len(tasks))
	}

	// Check first task
	if tasks[0].ID != "1.1" {
		t.Errorf("task 0 ID = %q, want %q", tasks[0].ID, "1.1")
	}
	if tasks[0].Section != "Backend" {
		t.Errorf("task 0 Section = %q, want %q", tasks[0].Section, "Backend")
	}
	if tasks[0].Description != "Add refresh_token field to User model" {
		t.Errorf("task 0 Description = %q, want %q", tasks[0].Description, "Add refresh_token field to User model")
	}
	if tasks[0].Completed {
		t.Error("task 0 should not be completed")
	}

	// Check completed task
	if !tasks[1].Completed {
		t.Error("task 1 should be completed")
	}
	if tasks[1].ID != "1.2" {
		t.Errorf("task 1 ID = %q, want %q", tasks[1].ID, "1.2")
	}

	// Check frontend section
	if tasks[3].Section != "Frontend" {
		t.Errorf("task 3 Section = %q, want %q", tasks[3].Section, "Frontend")
	}
	if tasks[3].ID != "2.1" {
		t.Errorf("task 3 ID = %q, want %q", tasks[3].ID, "2.1")
	}
}

func TestParseTasksEmpty(t *testing.T) {
	tasks, err := ParseTasks("")
	if err != nil {
		t.Fatalf("ParseTasks() error = %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks for empty content, got %d", len(tasks))
	}
}

func TestParseTasksNoSections(t *testing.T) {
	content := `# Tasks

- [ ] Task one
- [x] Task two
`

	tasks, err := ParseTasks(content)
	if err != nil {
		t.Fatalf("ParseTasks() error = %v", err)
	}

	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}

	// Tasks without explicit sections should use default
	if tasks[0].Section != "Tasks" {
		t.Errorf("task 0 Section = %q, want %q", tasks[0].Section, "Tasks")
	}
}

func TestGetTaskStats(t *testing.T) {
	tasks := []ParsedTask{
		{ID: "1.1", Completed: false},
		{ID: "1.2", Completed: true},
		{ID: "2.1", Completed: true},
		{ID: "2.2", Completed: false},
	}

	total, completed := GetTaskStats(tasks)

	if total != 4 {
		t.Errorf("total = %d, want 4", total)
	}
	if completed != 2 {
		t.Errorf("completed = %d, want 2", completed)
	}
}
