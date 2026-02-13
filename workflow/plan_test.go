package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestTaskStatus_IsValid(t *testing.T) {
	tests := []struct {
		status TaskStatus
		want   bool
	}{
		{TaskStatusPending, true},
		{TaskStatusInProgress, true},
		{TaskStatusCompleted, true},
		{TaskStatusFailed, true},
		{TaskStatus("unknown"), false},
		{TaskStatus(""), false},
	}

	for _, tt := range tests {
		name := string(tt.status)
		if name == "" {
			name = "empty_status"
		}
		t.Run(name, func(t *testing.T) {
			if got := tt.status.IsValid(); got != tt.want {
				t.Errorf("TaskStatus(%q).IsValid() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestTaskStatus_CanTransitionTo(t *testing.T) {
	tests := []struct {
		from TaskStatus
		to   TaskStatus
		want bool
	}{
		// From pending
		{TaskStatusPending, TaskStatusInProgress, true},
		{TaskStatusPending, TaskStatusFailed, true},
		{TaskStatusPending, TaskStatusCompleted, false},
		{TaskStatusPending, TaskStatusPending, false},

		// From in_progress
		{TaskStatusInProgress, TaskStatusCompleted, true},
		{TaskStatusInProgress, TaskStatusFailed, true},
		{TaskStatusInProgress, TaskStatusPending, false},
		{TaskStatusInProgress, TaskStatusInProgress, false},

		// From completed (terminal)
		{TaskStatusCompleted, TaskStatusPending, false},
		{TaskStatusCompleted, TaskStatusInProgress, false},
		{TaskStatusCompleted, TaskStatusFailed, false},

		// From failed (terminal)
		{TaskStatusFailed, TaskStatusPending, false},
		{TaskStatusFailed, TaskStatusInProgress, false},
		{TaskStatusFailed, TaskStatusCompleted, false},
	}

	for _, tt := range tests {
		name := string(tt.from) + "_to_" + string(tt.to)
		t.Run(name, func(t *testing.T) {
			if got := tt.from.CanTransitionTo(tt.to); got != tt.want {
				t.Errorf("TaskStatus(%q).CanTransitionTo(%q) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestTaskStatus_String(t *testing.T) {
	tests := []struct {
		status TaskStatus
		want   string
	}{
		{TaskStatusPending, "pending"},
		{TaskStatusInProgress, "in_progress"},
		{TaskStatusCompleted, "completed"},
		{TaskStatusFailed, "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("TaskStatus.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateSlug(t *testing.T) {
	tests := []struct {
		name    string
		slug    string
		wantErr error
	}{
		{"valid_simple", "test", nil},
		{"valid_with_hyphens", "test-feature", nil},
		{"valid_with_numbers", "test123", nil},
		{"valid_mixed", "auth-refresh-2", nil},
		{"empty", "", ErrSlugRequired},
		{"path_traversal_dots", "../etc/passwd", ErrInvalidSlug},
		{"path_traversal_slash", "foo/bar", ErrInvalidSlug},
		{"path_traversal_backslash", "foo\\bar", ErrInvalidSlug},
		{"uppercase", "TestFeature", ErrInvalidSlug},
		{"starts_with_hyphen", "-test", ErrInvalidSlug},
		{"ends_with_hyphen", "test-", ErrInvalidSlug},
		{"special_chars", "test@feature", ErrInvalidSlug},
		{"spaces", "test feature", ErrInvalidSlug},
		{"single_char", "a", nil},
		{"two_chars", "ab", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSlug(tt.slug)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateSlug(%q) = %v, want nil", tt.slug, err)
				}
			} else {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("ValidateSlug(%q) = %v, want %v", tt.slug, err, tt.wantErr)
				}
			}
		})
	}
}

func TestManager_CreatePlan(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, err := m.CreatePlan(ctx, "test-feature", "Add test feature")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	// Verify plan structure
	if plan.ID != "plan.test-feature" {
		t.Errorf("plan.ID = %q, want %q", plan.ID, "plan.test-feature")
	}
	if plan.Slug != "test-feature" {
		t.Errorf("plan.Slug = %q, want %q", plan.Slug, "test-feature")
	}
	if plan.Title != "Add test feature" {
		t.Errorf("plan.Title = %q, want %q", plan.Title, "Add test feature")
	}
	if plan.Committed {
		t.Error("new plan should have Committed=false")
	}
	if plan.CommittedAt != nil {
		t.Error("new plan should have CommittedAt=nil")
	}
	if plan.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}

	// Verify file was created
	planPath := filepath.Join(tmpDir, ".semspec", "changes", "test-feature", "plan.json")
	if _, err := os.Stat(planPath); os.IsNotExist(err) {
		t.Error("plan.json was not created")
	}
}

func TestManager_CreatePlan_Validation(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	_, err := m.CreatePlan(ctx, "", "Title")
	if !errors.Is(err, ErrSlugRequired) {
		t.Errorf("expected ErrSlugRequired, got %v", err)
	}

	_, err = m.CreatePlan(ctx, "slug", "")
	if !errors.Is(err, ErrTitleRequired) {
		t.Errorf("expected ErrTitleRequired, got %v", err)
	}

	_, err = m.CreatePlan(ctx, "../path/traversal", "Title")
	if !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("expected ErrInvalidSlug for path traversal, got %v", err)
	}
}

func TestManager_CreatePlan_AlreadyExists(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	_, err := m.CreatePlan(ctx, "existing", "First plan")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	_, err = m.CreatePlan(ctx, "existing", "Second plan")
	if !errors.Is(err, ErrPlanExists) {
		t.Errorf("expected ErrPlanExists, got %v", err)
	}
}

func TestManager_LoadPlan(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Create a plan
	created, err := m.CreatePlan(ctx, "test-load", "Test load plan")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	// Load it back
	loaded, err := m.LoadPlan(ctx, "test-load")
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}

	if loaded.ID != created.ID {
		t.Errorf("ID mismatch: got %q, want %q", loaded.ID, created.ID)
	}
	if loaded.Title != created.Title {
		t.Errorf("Title mismatch: got %q, want %q", loaded.Title, created.Title)
	}
}

func TestManager_LoadPlan_NotFound(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	_, err := m.LoadPlan(ctx, "nonexistent")
	if !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("expected ErrPlanNotFound, got %v", err)
	}
}

func TestManager_LoadPlan_PathTraversal(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	_, err := m.LoadPlan(ctx, "../../../etc/passwd")
	if !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("expected ErrInvalidSlug for path traversal, got %v", err)
	}
}

func TestManager_LoadPlan_MalformedJSON(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Create directory and write malformed JSON
	changePath := filepath.Join(tmpDir, ".semspec", "changes", "malformed")
	os.MkdirAll(changePath, 0755)
	os.WriteFile(filepath.Join(changePath, "plan.json"), []byte("{invalid json"), 0644)

	_, err := m.LoadPlan(ctx, "malformed")
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
	// Should be a parse error, not ErrPlanNotFound
	if errors.Is(err, ErrPlanNotFound) {
		t.Error("expected parse error, not ErrPlanNotFound")
	}
}

func TestManager_PromotePlan(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, err := m.CreatePlan(ctx, "test-promote", "Promote test")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	if plan.Committed {
		t.Error("plan should start uncommitted")
	}

	// Promote it
	err = m.PromotePlan(ctx, plan)
	if err != nil {
		t.Fatalf("PromotePlan failed: %v", err)
	}

	if !plan.Committed {
		t.Error("plan should be committed after promotion")
	}
	if plan.CommittedAt == nil {
		t.Error("CommittedAt should be set after promotion")
	}

	// Verify persistence
	loaded, err := m.LoadPlan(ctx, "test-promote")
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}
	if !loaded.Committed {
		t.Error("loaded plan should be committed")
	}
	if loaded.CommittedAt == nil {
		t.Error("loaded plan should have CommittedAt set")
	}
}

func TestManager_PromotePlan_AlreadyCommitted(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, _ := m.CreatePlan(ctx, "already-committed", "Already committed")
	m.PromotePlan(ctx, plan)

	err := m.PromotePlan(ctx, plan)
	if !errors.Is(err, ErrAlreadyCommitted) {
		t.Errorf("expected ErrAlreadyCommitted, got %v", err)
	}
}

func TestManager_PlanExists(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	if m.PlanExists("nonexistent") {
		t.Error("PlanExists should return false for nonexistent plan")
	}

	if m.PlanExists("../path/traversal") {
		t.Error("PlanExists should return false for invalid slug")
	}

	m.CreatePlan(ctx, "exists", "Plan exists")

	if !m.PlanExists("exists") {
		t.Error("PlanExists should return true for existing plan")
	}
}

func TestManager_ListPlans(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Create some plans
	m.CreatePlan(ctx, "plan1", "Plan 1")
	m.CreatePlan(ctx, "plan2", "Plan 2")

	result, err := m.ListPlans(ctx)
	if err != nil {
		t.Fatalf("ListPlans failed: %v", err)
	}

	if len(result.Plans) != 2 {
		t.Errorf("expected 2 plans, got %d", len(result.Plans))
	}
	if len(result.Errors) != 0 {
		t.Errorf("expected no errors, got %v", result.Errors)
	}
}

func TestManager_ListPlans_PartialErrors(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Create a valid plan
	m.CreatePlan(ctx, "valid", "Valid plan")

	// Create a directory with malformed JSON
	malformedPath := filepath.Join(tmpDir, ".semspec", "changes", "malformed")
	os.MkdirAll(malformedPath, 0755)
	os.WriteFile(filepath.Join(malformedPath, "plan.json"), []byte("{invalid}"), 0644)

	result, err := m.ListPlans(ctx)
	if err != nil {
		t.Fatalf("ListPlans failed: %v", err)
	}

	if len(result.Plans) != 1 {
		t.Errorf("expected 1 valid plan, got %d", len(result.Plans))
	}
	if len(result.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(result.Errors))
	}
}

func TestManager_ListPlans_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := m.ListPlans(ctx)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestParseTasksFromExecution(t *testing.T) {
	execution := `1. Add auth middleware to protect /api routes
2. Create refresh token endpoint at /api/auth/refresh
3. Update integration tests for new auth flow`

	tasks, err := ParseTasksFromExecution("plan.auth-refresh", "auth-refresh", execution)
	if err != nil {
		t.Fatalf("ParseTasksFromExecution failed: %v", err)
	}

	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}

	// Check first task
	if tasks[0].ID != "task.auth-refresh.1" {
		t.Errorf("task[0].ID = %q, want %q", tasks[0].ID, "task.auth-refresh.1")
	}
	if tasks[0].Sequence != 1 {
		t.Errorf("task[0].Sequence = %d, want 1", tasks[0].Sequence)
	}
	if tasks[0].Description != "Add auth middleware to protect /api routes" {
		t.Errorf("task[0].Description = %q", tasks[0].Description)
	}
	if tasks[0].Status != TaskStatusPending {
		t.Errorf("task[0].Status = %q, want %q", tasks[0].Status, TaskStatusPending)
	}
	if tasks[0].PlanID != "plan.auth-refresh" {
		t.Errorf("task[0].PlanID = %q, want %q", tasks[0].PlanID, "plan.auth-refresh")
	}

	// Check sequence numbers
	if tasks[1].ID != "task.auth-refresh.2" {
		t.Errorf("task[1].ID = %q, want %q", tasks[1].ID, "task.auth-refresh.2")
	}
	if tasks[2].ID != "task.auth-refresh.3" {
		t.Errorf("task[2].ID = %q, want %q", tasks[2].ID, "task.auth-refresh.3")
	}
}

func TestParseTasksFromExecution_AutoIncrement(t *testing.T) {
	// Test that we use auto-incrementing sequence, not parsed numbers
	// This prevents duplicates when input has non-sequential numbers
	execution := `5. First task (numbered 5)
10. Second task (numbered 10)
5. Third task (also numbered 5 - would be duplicate)`

	tasks, err := ParseTasksFromExecution("plan.test", "test", execution)
	if err != nil {
		t.Fatalf("ParseTasksFromExecution failed: %v", err)
	}

	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}

	// Verify auto-incrementing sequence (1, 2, 3) not parsed numbers (5, 10, 5)
	if tasks[0].Sequence != 1 || tasks[0].ID != "task.test.1" {
		t.Errorf("task[0] should have sequence 1, got %d (ID: %s)", tasks[0].Sequence, tasks[0].ID)
	}
	if tasks[1].Sequence != 2 || tasks[1].ID != "task.test.2" {
		t.Errorf("task[1] should have sequence 2, got %d (ID: %s)", tasks[1].Sequence, tasks[1].ID)
	}
	if tasks[2].Sequence != 3 || tasks[2].ID != "task.test.3" {
		t.Errorf("task[2] should have sequence 3, got %d (ID: %s)", tasks[2].Sequence, tasks[2].ID)
	}
}

func TestParseTasksFromExecution_ParenthesesFormat(t *testing.T) {
	execution := `1) First task
2) Second task`

	tasks, err := ParseTasksFromExecution("plan.test", "test", execution)
	if err != nil {
		t.Fatalf("ParseTasksFromExecution failed: %v", err)
	}

	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}

	if tasks[0].Description != "First task" {
		t.Errorf("task[0].Description = %q", tasks[0].Description)
	}
}

func TestParseTasksFromExecution_MixedContent(t *testing.T) {
	execution := `Here's the plan:

1. First task to do
Some explanation text

2. Second task
More details here

- Bullet point (ignored)
* Another bullet (ignored)

3. Third task`

	tasks, err := ParseTasksFromExecution("plan.mixed", "mixed", execution)
	if err != nil {
		t.Fatalf("ParseTasksFromExecution failed: %v", err)
	}

	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}

	if tasks[0].Description != "First task to do" {
		t.Errorf("task[0].Description = %q", tasks[0].Description)
	}
	if tasks[1].Description != "Second task" {
		t.Errorf("task[1].Description = %q", tasks[1].Description)
	}
	if tasks[2].Description != "Third task" {
		t.Errorf("task[2].Description = %q", tasks[2].Description)
	}
}

func TestParseTasksFromExecution_EmptyExecution(t *testing.T) {
	tasks, err := ParseTasksFromExecution("plan.empty", "empty", "")
	if err != nil {
		t.Fatalf("ParseTasksFromExecution failed: %v", err)
	}

	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestParseTasksFromExecution_InvalidSlug(t *testing.T) {
	_, err := ParseTasksFromExecution("plan.test", "../invalid", "1. Task")
	if !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("expected ErrInvalidSlug, got %v", err)
	}
}

func TestCreateTask(t *testing.T) {
	task, err := CreateTask("plan.test", "test", 1, "Do something")
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	if task.ID != "task.test.1" {
		t.Errorf("ID = %q, want %q", task.ID, "task.test.1")
	}
	if task.PlanID != "plan.test" {
		t.Errorf("PlanID = %q, want %q", task.PlanID, "plan.test")
	}
	if task.Sequence != 1 {
		t.Errorf("Sequence = %d, want 1", task.Sequence)
	}
	if task.Description != "Do something" {
		t.Errorf("Description = %q", task.Description)
	}
	if task.Status != TaskStatusPending {
		t.Errorf("Status = %q, want %q", task.Status, TaskStatusPending)
	}
	if task.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
}

func TestCreateTask_InvalidSlug(t *testing.T) {
	_, err := CreateTask("plan.test", "../invalid", 1, "Do something")
	if !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("expected ErrInvalidSlug, got %v", err)
	}
}

func TestManager_SaveAndLoadTasks(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Ensure the change directory exists
	m.CreatePlan(ctx, "test-tasks", "Test tasks")

	task1, _ := CreateTask("plan.test-tasks", "test-tasks", 1, "First task")
	task2, _ := CreateTask("plan.test-tasks", "test-tasks", 2, "Second task")
	tasks := []Task{*task1, *task2}

	err := m.SaveTasks(ctx, tasks, "test-tasks")
	if err != nil {
		t.Fatalf("SaveTasks failed: %v", err)
	}

	loaded, err := m.LoadTasks(ctx, "test-tasks")
	if err != nil {
		t.Fatalf("LoadTasks failed: %v", err)
	}

	if len(loaded) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(loaded))
	}

	if loaded[0].ID != "task.test-tasks.1" {
		t.Errorf("loaded[0].ID = %q", loaded[0].ID)
	}
	if loaded[1].ID != "task.test-tasks.2" {
		t.Errorf("loaded[1].ID = %q", loaded[1].ID)
	}
}

func TestManager_LoadTasks_Empty(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	tasks, err := m.LoadTasks(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("LoadTasks should not error for missing file: %v", err)
	}

	if len(tasks) != 0 {
		t.Errorf("expected empty slice, got %d tasks", len(tasks))
	}
}

func TestManager_LoadTasks_MalformedJSON(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Create directory and write malformed JSON
	changePath := filepath.Join(tmpDir, ".semspec", "changes", "malformed")
	os.MkdirAll(changePath, 0755)
	os.WriteFile(filepath.Join(changePath, "tasks.json"), []byte("[invalid json"), 0644)

	_, err := m.LoadTasks(ctx, "malformed")
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestManager_UpdateTaskStatus(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "test-update", "Test update")
	task, _ := CreateTask("plan.test-update", "test-update", 1, "Task one")
	m.SaveTasks(ctx, []Task{*task}, "test-update")

	// Transition to in_progress
	err := m.UpdateTaskStatus(ctx, "test-update", "task.test-update.1", TaskStatusInProgress)
	if err != nil {
		t.Fatalf("UpdateTaskStatus to in_progress failed: %v", err)
	}

	loaded, _ := m.LoadTasks(ctx, "test-update")
	if loaded[0].Status != TaskStatusInProgress {
		t.Errorf("status = %q, want %q", loaded[0].Status, TaskStatusInProgress)
	}

	// Transition to completed
	err = m.UpdateTaskStatus(ctx, "test-update", "task.test-update.1", TaskStatusCompleted)
	if err != nil {
		t.Fatalf("UpdateTaskStatus to completed failed: %v", err)
	}

	loaded, _ = m.LoadTasks(ctx, "test-update")
	if loaded[0].Status != TaskStatusCompleted {
		t.Errorf("status = %q, want %q", loaded[0].Status, TaskStatusCompleted)
	}
	if loaded[0].CompletedAt == nil {
		t.Error("CompletedAt should be set when completed")
	}
}

func TestManager_UpdateTaskStatus_InvalidTransition(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "test-invalid", "Test invalid transition")
	task, _ := CreateTask("plan.test-invalid", "test-invalid", 1, "Task one")
	m.SaveTasks(ctx, []Task{*task}, "test-invalid")

	// Try to skip in_progress and go directly to completed
	err := m.UpdateTaskStatus(ctx, "test-invalid", "task.test-invalid.1", TaskStatusCompleted)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestManager_UpdateTaskStatus_NotFound(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "test-notfound", "Test not found")
	m.SaveTasks(ctx, []Task{}, "test-notfound")

	err := m.UpdateTaskStatus(ctx, "test-notfound", "task.test-notfound.999", TaskStatusInProgress)
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestManager_UpdateTaskStatus_Concurrent(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "test-concurrent", "Test concurrent updates")

	// Create multiple tasks
	var tasks []Task
	for i := 1; i <= 10; i++ {
		task, _ := CreateTask("plan.test-concurrent", "test-concurrent", i, "Task")
		tasks = append(tasks, *task)
	}
	m.SaveTasks(ctx, tasks, "test-concurrent")

	// Update all tasks concurrently
	var wg sync.WaitGroup
	errCh := make(chan error, 10)

	for i := 1; i <= 10; i++ {
		wg.Add(1)
		go func(taskNum int) {
			defer wg.Done()
			taskID := "task.test-concurrent." + itoa(taskNum)
			if err := m.UpdateTaskStatus(ctx, "test-concurrent", taskID, TaskStatusInProgress); err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	// Check for errors
	for err := range errCh {
		t.Errorf("concurrent update failed: %v", err)
	}

	// Verify all tasks were updated
	loaded, _ := m.LoadTasks(ctx, "test-concurrent")
	for i, task := range loaded {
		if task.Status != TaskStatusInProgress {
			t.Errorf("task %d status = %q, want %q", i+1, task.Status, TaskStatusInProgress)
		}
	}
}

func TestManager_GetTask(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "test-get", "Test get")
	task1, _ := CreateTask("plan.test-get", "test-get", 1, "First")
	task2, _ := CreateTask("plan.test-get", "test-get", 2, "Second")
	m.SaveTasks(ctx, []Task{*task1, *task2}, "test-get")

	task, err := m.GetTask(ctx, "test-get", "task.test-get.2")
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}

	if task.Description != "Second" {
		t.Errorf("Description = %q, want %q", task.Description, "Second")
	}
}

func TestManager_GetTask_NotFound(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "test-get-nf", "Test get not found")
	m.SaveTasks(ctx, []Task{}, "test-get-nf")

	_, err := m.GetTask(ctx, "test-get-nf", "task.test-get-nf.999")
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestManager_GenerateTasksFromPlan(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, _ := m.CreatePlan(ctx, "test-generate", "Generate tasks test")
	plan.Execution = `1. First step
2. Second step
3. Third step`
	m.SavePlan(ctx, plan)

	tasks, err := m.GenerateTasksFromPlan(ctx, plan)
	if err != nil {
		t.Fatalf("GenerateTasksFromPlan failed: %v", err)
	}

	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}

	// Verify tasks were saved
	loaded, _ := m.LoadTasks(ctx, "test-generate")
	if len(loaded) != 3 {
		t.Errorf("expected 3 saved tasks, got %d", len(loaded))
	}
}

func TestPlan_JSON(t *testing.T) {
	now := time.Now()
	plan := Plan{
		ID:        "plan.test",
		Slug:      "test",
		Title:     "Test Plan",
		Committed: true,
		Situation: "Current state",
		Mission:   "Objective",
		Execution: "1. Step one\n2. Step two",
		Constraints: Constraints{
			In:         []string{"api/", "lib/"},
			Out:        []string{"vendor/"},
			DoNotTouch: []string{"config.yaml"},
		},
		Coordination: "Sync via PR",
		CreatedAt:    now,
		CommittedAt:  &now,
	}

	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Plan
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != plan.ID {
		t.Errorf("ID mismatch")
	}
	if !decoded.Committed {
		t.Errorf("Committed should be true")
	}
	if len(decoded.Constraints.In) != 2 {
		t.Errorf("Constraints.In length = %d, want 2", len(decoded.Constraints.In))
	}
	if decoded.CommittedAt == nil {
		t.Error("CommittedAt should not be nil")
	}
}

func TestTask_JSON(t *testing.T) {
	now := time.Now()
	task := Task{
		ID:          "task.test.1",
		PlanID:      "plan.test",
		Sequence:    1,
		Description: "Do something",
		Type:        TaskTypeImplement,
		AcceptanceCriteria: []AcceptanceCriterion{
			{Given: "tests exist", When: "running tests", Then: "tests pass"},
			{Given: "code changes", When: "reviewing docs", Then: "docs are updated"},
		},
		Files:       []string{"api/handler.go"},
		Status:      TaskStatusCompleted,
		CreatedAt:   now,
		CompletedAt: &now,
	}

	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Task
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != task.ID {
		t.Errorf("ID mismatch")
	}
	if decoded.Status != TaskStatusCompleted {
		t.Errorf("Status = %q, want %q", decoded.Status, TaskStatusCompleted)
	}
	if len(decoded.AcceptanceCriteria) != 2 {
		t.Errorf("AcceptanceCriteria length = %d, want 2", len(decoded.AcceptanceCriteria))
	}
	if decoded.AcceptanceCriteria[0].Given != "tests exist" {
		t.Errorf("AcceptanceCriteria[0].Given = %q, want %q", decoded.AcceptanceCriteria[0].Given, "tests exist")
	}
	if decoded.Type != TaskTypeImplement {
		t.Errorf("Type = %q, want %q", decoded.Type, TaskTypeImplement)
	}
	if decoded.CompletedAt == nil {
		t.Error("CompletedAt should not be nil")
	}
}

func TestContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// All context-aware operations should fail
	_, err := m.CreatePlan(ctx, "test", "Test")
	if err == nil {
		t.Error("CreatePlan should fail with cancelled context")
	}

	_, err = m.LoadPlan(ctx, "test")
	if err == nil {
		t.Error("LoadPlan should fail with cancelled context")
	}

	err = m.SaveTasks(ctx, []Task{}, "test")
	if err == nil {
		t.Error("SaveTasks should fail with cancelled context")
	}

	_, err = m.LoadTasks(ctx, "test")
	if err == nil {
		t.Error("LoadTasks should fail with cancelled context")
	}

	err = m.UpdateTaskStatus(ctx, "test", "task.test.1", TaskStatusInProgress)
	if err == nil {
		t.Error("UpdateTaskStatus should fail with cancelled context")
	}
}

// ============================================================================
// Additional edge case tests for Plan/Task mutations and validation
// ============================================================================

// TestManager_SavePlan_Direct tests saving a modified plan directly.
func TestManager_SavePlan_Direct(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Create plan
	plan, err := m.CreatePlan(ctx, "test-direct-save", "Test Direct Save")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	// Modify fields
	plan.Situation = "Updated situation"
	plan.Mission = "Updated mission"

	// Save directly
	err = m.SavePlan(ctx, plan)
	if err != nil {
		t.Fatalf("SavePlan failed: %v", err)
	}

	// Reload and verify
	loaded, err := m.LoadPlan(ctx, "test-direct-save")
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}
	if loaded.Situation != "Updated situation" {
		t.Errorf("Situation = %q, want %q", loaded.Situation, "Updated situation")
	}
	if loaded.Mission != "Updated mission" {
		t.Errorf("Mission = %q, want %q", loaded.Mission, "Updated mission")
	}
}

// TestManager_SavePlan_ContextCancellation tests that SavePlan respects context.
func TestManager_SavePlan_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	plan := &Plan{
		ID:    "plan.test",
		Slug:  "test",
		Title: "Test",
	}

	err := m.SavePlan(ctx, plan)
	if err == nil {
		t.Error("SavePlan should fail with cancelled context")
	}
}

// TestPlan_FieldMutations tests modifying SMEAC fields, save, reload, verify.
func TestPlan_FieldMutations(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Create plan
	plan, err := m.CreatePlan(ctx, "mutations", "Mutations Test")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	// Modify all SMEAC fields
	plan.Situation = "Complex multi-service architecture with auth issues"
	plan.Mission = "Implement OAuth 2.0 refresh token flow"
	plan.Execution = "1. Add token refresh endpoint\n2. Update middleware\n3. Add tests"
	plan.Coordination = "Daily sync with frontend team at 10am"

	// Save
	if err := m.SavePlan(ctx, plan); err != nil {
		t.Fatalf("SavePlan failed: %v", err)
	}

	// Reload
	loaded, err := m.LoadPlan(ctx, "mutations")
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}

	// Verify all fields
	if loaded.Situation != plan.Situation {
		t.Errorf("Situation = %q, want %q", loaded.Situation, plan.Situation)
	}
	if loaded.Mission != plan.Mission {
		t.Errorf("Mission = %q, want %q", loaded.Mission, plan.Mission)
	}
	if loaded.Execution != plan.Execution {
		t.Errorf("Execution = %q, want %q", loaded.Execution, plan.Execution)
	}
	if loaded.Coordination != plan.Coordination {
		t.Errorf("Coordination = %q, want %q", loaded.Coordination, plan.Coordination)
	}
}

// TestConstraints_Mutations tests modifying In/Out/DoNotTouch arrays.
func TestConstraints_Mutations(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, err := m.CreatePlan(ctx, "constraints-mut", "Constraints Mutation")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	// Verify initial empty state
	if len(plan.Constraints.In) != 0 {
		t.Errorf("initial In = %v, want empty", plan.Constraints.In)
	}

	// Modify constraints
	plan.Constraints.In = []string{"api/", "lib/auth/", "internal/middleware/"}
	plan.Constraints.Out = []string{"vendor/", "third_party/"}
	plan.Constraints.DoNotTouch = []string{"config.yaml", ".env", "secrets/"}

	// Save and reload
	if err := m.SavePlan(ctx, plan); err != nil {
		t.Fatalf("SavePlan failed: %v", err)
	}

	loaded, err := m.LoadPlan(ctx, "constraints-mut")
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}

	// Verify constraints
	if len(loaded.Constraints.In) != 3 {
		t.Errorf("In length = %d, want 3", len(loaded.Constraints.In))
	}
	if len(loaded.Constraints.Out) != 2 {
		t.Errorf("Out length = %d, want 2", len(loaded.Constraints.Out))
	}
	if len(loaded.Constraints.DoNotTouch) != 3 {
		t.Errorf("DoNotTouch length = %d, want 3", len(loaded.Constraints.DoNotTouch))
	}

	// Verify specific values
	if loaded.Constraints.In[0] != "api/" {
		t.Errorf("In[0] = %q, want %q", loaded.Constraints.In[0], "api/")
	}
	if loaded.Constraints.DoNotTouch[2] != "secrets/" {
		t.Errorf("DoNotTouch[2] = %q, want %q", loaded.Constraints.DoNotTouch[2], "secrets/")
	}
}

// TestParseTasksFromExecution_Unicode tests parsing tasks with Unicode characters.
func TestParseTasksFromExecution_Unicode(t *testing.T) {
	execution := `1. 日本語でタスクを書く
2. Add émojis and àccénts 🎉
3. Привет мир - Russian greeting
4. 中文描述任务`

	tasks, err := ParseTasksFromExecution("plan.unicode", "unicode", execution)
	if err != nil {
		t.Fatalf("ParseTasksFromExecution failed: %v", err)
	}

	if len(tasks) != 4 {
		t.Fatalf("expected 4 tasks, got %d", len(tasks))
	}

	// Verify Unicode content is preserved
	if tasks[0].Description != "日本語でタスクを書く" {
		t.Errorf("task[0].Description = %q", tasks[0].Description)
	}
	if tasks[1].Description != "Add émojis and àccénts 🎉" {
		t.Errorf("task[1].Description = %q", tasks[1].Description)
	}
	if tasks[2].Description != "Привет мир - Russian greeting" {
		t.Errorf("task[2].Description = %q", tasks[2].Description)
	}
	if tasks[3].Description != "中文描述任务" {
		t.Errorf("task[3].Description = %q", tasks[3].Description)
	}
}

// TestParseTasksFromExecution_LongDescriptions tests very long task descriptions.
func TestParseTasksFromExecution_LongDescriptions(t *testing.T) {
	// Create a very long description (1000+ chars)
	longDesc := "Implement a comprehensive authentication system that handles " +
		"user registration, login, password reset, email verification, " +
		"two-factor authentication, session management, OAuth 2.0 integration, " +
		"SAML support, LDAP connectivity, rate limiting, brute force protection, " +
		"audit logging, compliance reporting, and integration with external identity providers"

	execution := "1. " + longDesc + "\n2. Short task"

	tasks, err := ParseTasksFromExecution("plan.long", "long", execution)
	if err != nil {
		t.Fatalf("ParseTasksFromExecution failed: %v", err)
	}

	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}

	if tasks[0].Description != longDesc {
		t.Errorf("long description not preserved, got length %d", len(tasks[0].Description))
	}
}

// TestParseTasksFromExecution_MixedLineEndings tests \r\n, \n, \r handling.
func TestParseTasksFromExecution_MixedLineEndings(t *testing.T) {
	// Note: Go's strings.Split splits on \n, \r\n will leave \r attached
	// This test verifies that we handle reasonable line endings
	tests := []struct {
		name      string
		slug      string
		execution string
		wantCount int
	}{
		{"unix_lf", "unix-lf", "1. First\n2. Second\n3. Third", 3},
		{"windows_crlf", "windows-crlf", "1. First\r\n2. Second\r\n3. Third", 3},
		{"old_mac_cr", "old-mac-cr", "1. First\r2. Second\r3. Third", 1}, // splits as single line
		{"mixed", "mixed", "1. First\n2. Second\r\n3. Third\n4. Fourth", 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tasks, err := ParseTasksFromExecution("plan."+tt.slug, tt.slug, tt.execution)
			if err != nil {
				t.Fatalf("ParseTasksFromExecution failed: %v", err)
			}

			if len(tasks) != tt.wantCount {
				t.Errorf("got %d tasks, want %d; input was: %q", len(tasks), tt.wantCount, tt.execution)
			}
		})
	}
}

// TestManager_ConcurrentReadWrite tests that concurrent reads from different
// plans don't interfere with writes to other plans.
// Note: Concurrent reads and writes to the SAME plan may see partial writes
// since file I/O is not atomic. This is acceptable for the CLI use case.
// Run with: go test -race ./workflow/... to verify no data races.
func TestManager_ConcurrentReadWrite(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Create separate plans for reading and writing
	if _, err := m.CreatePlan(ctx, "read-plan", "Read Plan"); err != nil {
		t.Fatalf("CreatePlan(read-plan) failed: %v", err)
	}
	readTasks := []Task{}
	for i := 1; i <= 5; i++ {
		task, _ := CreateTask("plan.read-plan", "read-plan", i, "Read Task "+itoa(i))
		readTasks = append(readTasks, *task)
	}
	if err := m.SaveTasks(ctx, readTasks, "read-plan"); err != nil {
		t.Fatalf("SaveTasks(read-plan) failed: %v", err)
	}

	if _, err := m.CreatePlan(ctx, "write-plan", "Write Plan"); err != nil {
		t.Fatalf("CreatePlan(write-plan) failed: %v", err)
	}
	writeTasks := []Task{}
	for i := 1; i <= 5; i++ {
		task, _ := CreateTask("plan.write-plan", "write-plan", i, "Write Task "+itoa(i))
		writeTasks = append(writeTasks, *task)
	}
	if err := m.SaveTasks(ctx, writeTasks, "write-plan"); err != nil {
		t.Fatalf("SaveTasks(write-plan) failed: %v", err)
	}

	// Run concurrent reads from read-plan and writes to write-plan
	var wg sync.WaitGroup
	// Buffer sized for worst case: 5 readers * 10 iterations + 5 writers = 55 potential errors
	errCh := make(chan error, 55)

	// 5 readers reading from read-plan (no writes to this plan)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				tasks, err := m.LoadTasks(ctx, "read-plan")
				if err != nil {
					errCh <- err
					return
				}
				if len(tasks) != 5 {
					errCh <- errors.New("unexpected task count during read")
					return
				}
			}
		}()
	}

	// 5 writers updating write-plan
	for i := 1; i <= 5; i++ {
		wg.Add(1)
		go func(taskNum int) {
			defer wg.Done()
			taskID := "task.write-plan." + itoa(taskNum)
			if err := m.UpdateTaskStatus(ctx, "write-plan", taskID, TaskStatusInProgress); err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	// Check for errors
	for err := range errCh {
		t.Errorf("concurrent operation failed: %v", err)
	}

	// Verify final state
	finalReadTasks, _ := m.LoadTasks(ctx, "read-plan")
	for _, task := range finalReadTasks {
		if task.Status != TaskStatusPending {
			t.Errorf("read-plan task should be pending: %s = %s", task.ID, task.Status)
		}
	}

	finalWriteTasks, _ := m.LoadTasks(ctx, "write-plan")
	for _, task := range finalWriteTasks {
		if task.Status != TaskStatusInProgress {
			t.Errorf("write-plan task should be in_progress: %s = %s", task.ID, task.Status)
		}
	}
}

// TestTask_LifecycleSequence tests pending→in_progress→completed full cycle.
func TestTask_LifecycleSequence(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "lifecycle", "Lifecycle Test")
	task, _ := CreateTask("plan.lifecycle", "lifecycle", 1, "Complete lifecycle")
	m.SaveTasks(ctx, []Task{*task}, "lifecycle")

	// Verify initial state
	loaded, _ := m.LoadTasks(ctx, "lifecycle")
	if loaded[0].Status != TaskStatusPending {
		t.Errorf("initial status = %q, want %q", loaded[0].Status, TaskStatusPending)
	}
	if loaded[0].CompletedAt != nil {
		t.Error("CompletedAt should be nil initially")
	}

	// Transition to in_progress
	err := m.UpdateTaskStatus(ctx, "lifecycle", "task.lifecycle.1", TaskStatusInProgress)
	if err != nil {
		t.Fatalf("transition to in_progress failed: %v", err)
	}

	loaded, _ = m.LoadTasks(ctx, "lifecycle")
	if loaded[0].Status != TaskStatusInProgress {
		t.Errorf("status after start = %q, want %q", loaded[0].Status, TaskStatusInProgress)
	}
	if loaded[0].CompletedAt != nil {
		t.Error("CompletedAt should still be nil for in_progress")
	}

	// Transition to completed
	err = m.UpdateTaskStatus(ctx, "lifecycle", "task.lifecycle.1", TaskStatusCompleted)
	if err != nil {
		t.Fatalf("transition to completed failed: %v", err)
	}

	loaded, _ = m.LoadTasks(ctx, "lifecycle")
	if loaded[0].Status != TaskStatusCompleted {
		t.Errorf("status after completion = %q, want %q", loaded[0].Status, TaskStatusCompleted)
	}
	if loaded[0].CompletedAt == nil {
		t.Error("CompletedAt should be set after completion")
	} else if loaded[0].CompletedAt.Before(loaded[0].CreatedAt) {
		t.Error("CompletedAt should not be before CreatedAt")
	}

	// Verify terminal state - cannot transition further
	err = m.UpdateTaskStatus(ctx, "lifecycle", "task.lifecycle.1", TaskStatusInProgress)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition from completed, got %v", err)
	}
}

// TestTask_FailedLifecycle tests pending→in_progress→failed cycle.
func TestTask_FailedLifecycle(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "failed-lifecycle", "Failed Lifecycle")
	task, _ := CreateTask("plan.failed-lifecycle", "failed-lifecycle", 1, "Will fail")
	m.SaveTasks(ctx, []Task{*task}, "failed-lifecycle")

	// Transition to in_progress
	m.UpdateTaskStatus(ctx, "failed-lifecycle", "task.failed-lifecycle.1", TaskStatusInProgress)

	// Transition to failed
	err := m.UpdateTaskStatus(ctx, "failed-lifecycle", "task.failed-lifecycle.1", TaskStatusFailed)
	if err != nil {
		t.Fatalf("transition to failed: %v", err)
	}

	loaded, _ := m.LoadTasks(ctx, "failed-lifecycle")
	if loaded[0].Status != TaskStatusFailed {
		t.Errorf("status = %q, want %q", loaded[0].Status, TaskStatusFailed)
	}
	if loaded[0].CompletedAt == nil {
		t.Error("CompletedAt should be set on failure")
	} else if loaded[0].CompletedAt.Before(loaded[0].CreatedAt) {
		t.Error("CompletedAt should not be before CreatedAt")
	}

	// Verify terminal state
	err = m.UpdateTaskStatus(ctx, "failed-lifecycle", "task.failed-lifecycle.1", TaskStatusCompleted)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition from failed, got %v", err)
	}
}

// TestTask_DirectToFailed tests pending→failed direct transition.
func TestTask_DirectToFailed(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "direct-fail", "Direct Fail")
	task, _ := CreateTask("plan.direct-fail", "direct-fail", 1, "Skip to failed")
	m.SaveTasks(ctx, []Task{*task}, "direct-fail")

	// Can go directly from pending to failed
	err := m.UpdateTaskStatus(ctx, "direct-fail", "task.direct-fail.1", TaskStatusFailed)
	if err != nil {
		t.Fatalf("pending→failed should be allowed: %v", err)
	}

	loaded, _ := m.LoadTasks(ctx, "direct-fail")
	if loaded[0].Status != TaskStatusFailed {
		t.Errorf("status = %q, want %q", loaded[0].Status, TaskStatusFailed)
	}
}

// TestTask_AcceptanceCriteriaModification tests updating acceptance criteria.
func TestTask_AcceptanceCriteriaModification(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "criteria", "Criteria Test")
	task, _ := CreateTask("plan.criteria", "criteria", 1, "Task with criteria")
	m.SaveTasks(ctx, []Task{*task}, "criteria")

	// Load, modify, save
	tasks, _ := m.LoadTasks(ctx, "criteria")
	tasks[0].AcceptanceCriteria = []AcceptanceCriterion{
		{Given: "code changes", When: "running unit tests", Then: "all unit tests pass"},
		{Given: "system deployed", When: "running integration tests", Then: "integration tests pass"},
		{Given: "new features", When: "reviewing docs", Then: "documentation is updated"},
		{Given: "PR submitted", When: "code review requested", Then: "code review is approved"},
	}

	if err := m.SaveTasks(ctx, tasks, "criteria"); err != nil {
		t.Fatalf("SaveTasks failed: %v", err)
	}

	// Reload and verify
	loaded, _ := m.LoadTasks(ctx, "criteria")
	if len(loaded[0].AcceptanceCriteria) != 4 {
		t.Errorf("AcceptanceCriteria length = %d, want 4", len(loaded[0].AcceptanceCriteria))
	}
	if loaded[0].AcceptanceCriteria[0].Then != "all unit tests pass" {
		t.Errorf("AcceptanceCriteria[0].Then = %q", loaded[0].AcceptanceCriteria[0].Then)
	}
}

// TestTask_FilesModification tests updating the files array.
func TestTask_FilesModification(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "files-mod", "Files Modification")
	task, _ := CreateTask("plan.files-mod", "files-mod", 1, "Task with files")
	m.SaveTasks(ctx, []Task{*task}, "files-mod")

	// Load, modify, save
	tasks, _ := m.LoadTasks(ctx, "files-mod")
	tasks[0].Files = []string{
		"api/handler.go",
		"api/handler_test.go",
		"internal/auth/middleware.go",
	}

	if err := m.SaveTasks(ctx, tasks, "files-mod"); err != nil {
		t.Fatalf("SaveTasks failed: %v", err)
	}

	// Reload and verify
	loaded, _ := m.LoadTasks(ctx, "files-mod")
	if len(loaded[0].Files) != 3 {
		t.Errorf("Files length = %d, want 3", len(loaded[0].Files))
	}
	if loaded[0].Files[2] != "internal/auth/middleware.go" {
		t.Errorf("Files[2] = %q", loaded[0].Files[2])
	}
}

// TestManager_SavePlan_InvalidSlug tests that SavePlan validates slug.
func TestManager_SavePlan_InvalidSlug(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan := &Plan{
		ID:    "plan.test",
		Slug:  "../invalid",
		Title: "Test",
	}

	err := m.SavePlan(ctx, plan)
	if !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("expected ErrInvalidSlug, got %v", err)
	}
}

// TestManager_LoadTasks_InvalidSlug tests that LoadTasks validates slug.
func TestManager_LoadTasks_InvalidSlug(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	_, err := m.LoadTasks(ctx, "../invalid")
	if !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("expected ErrInvalidSlug, got %v", err)
	}
}

// TestManager_SaveTasks_InvalidSlug tests that SaveTasks validates slug.
func TestManager_SaveTasks_InvalidSlug(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	err := m.SaveTasks(ctx, []Task{}, "../invalid")
	if !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("expected ErrInvalidSlug, got %v", err)
	}
}

// TestManager_UpdateTaskStatus_InvalidSlug tests that UpdateTaskStatus validates slug.
func TestManager_UpdateTaskStatus_InvalidSlug(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	err := m.UpdateTaskStatus(ctx, "../invalid", "task.test.1", TaskStatusInProgress)
	if !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("expected ErrInvalidSlug, got %v", err)
	}
}

// TestManager_GetTask_InvalidSlug tests that GetTask validates slug.
func TestManager_GetTask_InvalidSlug(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	_, err := m.GetTask(ctx, "../invalid", "task.test.1")
	if !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("expected ErrInvalidSlug, got %v", err)
	}
}

// TestManager_UpdateTaskStatus_InvalidStatus tests invalid status values.
func TestManager_UpdateTaskStatus_InvalidStatus(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "inv-status", "Invalid Status")
	task, _ := CreateTask("plan.inv-status", "inv-status", 1, "Task")
	m.SaveTasks(ctx, []Task{*task}, "inv-status")

	err := m.UpdateTaskStatus(ctx, "inv-status", "task.inv-status.1", TaskStatus("unknown"))
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition for invalid status, got %v", err)
	}
}
