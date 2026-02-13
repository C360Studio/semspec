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
		ID:                 "task.test.1",
		PlanID:             "plan.test",
		Sequence:           1,
		Description:        "Do something",
		AcceptanceCriteria: []string{"Tests pass", "Docs updated"},
		Files:              []string{"api/handler.go"},
		Status:             TaskStatusCompleted,
		CreatedAt:          now,
		CompletedAt:        &now,
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
