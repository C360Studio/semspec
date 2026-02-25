package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
		{TaskStatusPendingApproval, true},
		{TaskStatusApproved, true},
		{TaskStatusRejected, true},
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
		{TaskStatusPending, TaskStatusPendingApproval, true},
		{TaskStatusPending, TaskStatusInProgress, true}, // legacy backward compat
		{TaskStatusPending, TaskStatusFailed, true},
		{TaskStatusPending, TaskStatusCompleted, false},
		{TaskStatusPending, TaskStatusPending, false},
		{TaskStatusPending, TaskStatusApproved, false},
		{TaskStatusPending, TaskStatusRejected, false},

		// From pending_approval
		{TaskStatusPendingApproval, TaskStatusApproved, true},
		{TaskStatusPendingApproval, TaskStatusRejected, true},
		{TaskStatusPendingApproval, TaskStatusPending, false},
		{TaskStatusPendingApproval, TaskStatusInProgress, false},
		{TaskStatusPendingApproval, TaskStatusCompleted, false},
		{TaskStatusPendingApproval, TaskStatusFailed, false},

		// From approved
		{TaskStatusApproved, TaskStatusInProgress, true},
		{TaskStatusApproved, TaskStatusPending, false},
		{TaskStatusApproved, TaskStatusCompleted, false},
		{TaskStatusApproved, TaskStatusFailed, false},

		// From rejected
		{TaskStatusRejected, TaskStatusPending, true}, // can re-edit
		{TaskStatusRejected, TaskStatusApproved, false},
		{TaskStatusRejected, TaskStatusInProgress, false},
		{TaskStatusRejected, TaskStatusCompleted, false},

		// From in_progress
		{TaskStatusInProgress, TaskStatusCompleted, true},
		{TaskStatusInProgress, TaskStatusFailed, true},
		{TaskStatusInProgress, TaskStatusPending, false},
		{TaskStatusInProgress, TaskStatusInProgress, false},
		{TaskStatusInProgress, TaskStatusApproved, false},

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
		{TaskStatusPendingApproval, "pending_approval"},
		{TaskStatusApproved, "approved"},
		{TaskStatusRejected, "rejected"},
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
	expectedID := PlanEntityID("test-feature")
	if plan.ID != expectedID {
		t.Errorf("plan.ID = %q, want %q", plan.ID, expectedID)
	}
	if plan.Slug != "test-feature" {
		t.Errorf("plan.Slug = %q, want %q", plan.Slug, "test-feature")
	}
	if plan.Title != "Add test feature" {
		t.Errorf("plan.Title = %q, want %q", plan.Title, "Add test feature")
	}
	if plan.Approved {
		t.Error("new plan should have Approved=false")
	}
	if plan.ApprovedAt != nil {
		t.Error("new plan should have ApprovedAt=nil")
	}
	if plan.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}

	// Verify file was created in project-based path
	planPath := filepath.Join(tmpDir, ".semspec", "projects", "default", "plans", "test-feature", "plan.json")
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

	// Create directory and write malformed JSON at project-based path
	planPath := filepath.Join(tmpDir, ".semspec", "projects", "default", "plans", "malformed")
	os.MkdirAll(planPath, 0755)
	os.WriteFile(filepath.Join(planPath, "plan.json"), []byte("{invalid json"), 0644)

	_, err := m.LoadPlan(ctx, "malformed")
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
	// Should be a parse error, not ErrPlanNotFound
	if errors.Is(err, ErrPlanNotFound) {
		t.Error("expected parse error, not ErrPlanNotFound")
	}
}

func TestManager_ApprovePlan(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, err := m.CreatePlan(ctx, "test-approve", "Approve test")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	if plan.Approved {
		t.Error("plan should start unapproved")
	}

	// Approve it
	err = m.ApprovePlan(ctx, plan)
	if err != nil {
		t.Fatalf("ApprovePlan failed: %v", err)
	}

	if !plan.Approved {
		t.Error("plan should be approved after approval")
	}
	if plan.ApprovedAt == nil {
		t.Error("ApprovedAt should be set after approval")
	}

	// Verify persistence
	loaded, err := m.LoadPlan(ctx, "test-approve")
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}
	if !loaded.Approved {
		t.Error("loaded plan should be approved")
	}
	if loaded.ApprovedAt == nil {
		t.Error("loaded plan should have ApprovedAt set")
	}
}

func TestManager_ApprovePlan_AlreadyApproved(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, _ := m.CreatePlan(ctx, "already-approved", "Already approved")
	m.ApprovePlan(ctx, plan)

	err := m.ApprovePlan(ctx, plan)
	if !errors.Is(err, ErrAlreadyApproved) {
		t.Errorf("expected ErrAlreadyApproved, got %v", err)
	}
}

func TestManager_ApprovePlan_SetsStatus(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, err := m.CreatePlan(ctx, "test-status", "Status test")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	if err := m.ApprovePlan(ctx, plan); err != nil {
		t.Fatalf("ApprovePlan failed: %v", err)
	}

	if plan.Status != StatusApproved {
		t.Errorf("Status = %q, want %q", plan.Status, StatusApproved)
	}

	loaded, err := m.LoadPlan(ctx, "test-status")
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}
	if loaded.Status != StatusApproved {
		t.Errorf("loaded Status = %q, want %q", loaded.Status, StatusApproved)
	}
}

func TestManager_ApproveTasksPlan(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, err := m.CreatePlan(ctx, "test-tasks-approve", "Tasks approval test")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	// Approve the plan first
	if err := m.ApprovePlan(ctx, plan); err != nil {
		t.Fatalf("ApprovePlan failed: %v", err)
	}

	// Set tasks_generated status (simulating what task-generator does)
	plan.Status = StatusTasksGenerated
	if err := m.SavePlan(ctx, plan); err != nil {
		t.Fatalf("SavePlan failed: %v", err)
	}

	// Approve tasks
	if err := m.ApproveTasksPlan(ctx, plan); err != nil {
		t.Fatalf("ApproveTasksPlan failed: %v", err)
	}

	if !plan.TasksApproved {
		t.Error("TasksApproved should be true after approval")
	}
	if plan.TasksApprovedAt == nil {
		t.Error("TasksApprovedAt should be set after approval")
	}
	if plan.Status != StatusTasksApproved {
		t.Errorf("Status = %q, want %q", plan.Status, StatusTasksApproved)
	}

	// Verify persistence
	loaded, err := m.LoadPlan(ctx, "test-tasks-approve")
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}
	if !loaded.TasksApproved {
		t.Error("loaded TasksApproved should be true")
	}
	if loaded.TasksApprovedAt == nil {
		t.Error("loaded TasksApprovedAt should be set")
	}
	if loaded.Status != StatusTasksApproved {
		t.Errorf("loaded Status = %q, want %q", loaded.Status, StatusTasksApproved)
	}
}

func TestManager_ApproveTasksPlan_AlreadyApproved(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, _ := m.CreatePlan(ctx, "already-tasks-approved", "Already tasks approved")
	_ = m.ApprovePlan(ctx, plan)
	plan.Status = StatusTasksGenerated
	_ = m.SavePlan(ctx, plan)
	_ = m.ApproveTasksPlan(ctx, plan)

	err := m.ApproveTasksPlan(ctx, plan)
	if !errors.Is(err, ErrTasksAlreadyApproved) {
		t.Errorf("expected ErrTasksAlreadyApproved, got %v", err)
	}
}

func TestManager_ApproveTasksPlan_NotApproved(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, _ := m.CreatePlan(ctx, "not-approved", "Not approved")

	err := m.ApproveTasksPlan(ctx, plan)
	if err == nil {
		t.Error("expected error for unapproved plan, got nil")
	}
}

func TestManager_SetPlanStatus(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, err := m.CreatePlan(ctx, "test-set-status", "Set status test")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	// Approve plan to get to StatusApproved
	if err := m.ApprovePlan(ctx, plan); err != nil {
		t.Fatalf("ApprovePlan failed: %v", err)
	}

	// Valid transition: approved → phases_generated
	if err := m.SetPlanStatus(ctx, plan, StatusPhasesGenerated); err != nil {
		t.Fatalf("SetPlanStatus to phases_generated failed: %v", err)
	}
	if plan.Status != StatusPhasesGenerated {
		t.Errorf("Status = %q, want %q", plan.Status, StatusPhasesGenerated)
	}

	// Invalid transition: phases_generated → implementing (should fail)
	err = m.SetPlanStatus(ctx, plan, StatusImplementing)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestPlan_EffectiveStatus(t *testing.T) {
	tests := []struct {
		name     string
		plan     Plan
		expected Status
	}{
		{
			name:     "explicit status takes priority",
			plan:     Plan{Status: StatusTasksGenerated, Approved: true},
			expected: StatusTasksGenerated,
		},
		{
			name:     "infers tasks_approved from boolean",
			plan:     Plan{TasksApproved: true, Approved: true},
			expected: StatusTasksApproved,
		},
		{
			name:     "infers approved from boolean",
			plan:     Plan{Approved: true},
			expected: StatusApproved,
		},
		{
			name:     "infers reviewed from needs_changes verdict",
			plan:     Plan{ReviewVerdict: "needs_changes"},
			expected: StatusReviewed,
		},
		{
			name:     "infers reviewed from approved verdict",
			plan:     Plan{ReviewVerdict: "approved"},
			expected: StatusReviewed,
		},
		{
			name:     "infers drafted from goal+context",
			plan:     Plan{Goal: "do something", Context: "why it matters"},
			expected: StatusDrafted,
		},
		{
			name:     "defaults to created",
			plan:     Plan{},
			expected: StatusCreated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.plan.EffectiveStatus()
			if result != tt.expected {
				t.Errorf("EffectiveStatus() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestStatus_TasksGenerated_Valid(t *testing.T) {
	if !StatusTasksGenerated.IsValid() {
		t.Error("StatusTasksGenerated should be valid")
	}
	if !StatusTasksApproved.IsValid() {
		t.Error("StatusTasksApproved should be valid")
	}
}

func TestStatus_CanTransitionTo_NewStates(t *testing.T) {
	// approved → phases_generated
	if !StatusApproved.CanTransitionTo(StatusPhasesGenerated) {
		t.Error("approved should transition to phases_generated")
	}
	// approved → tasks_generated should NOT work (must go through phases)
	if StatusApproved.CanTransitionTo(StatusTasksGenerated) {
		t.Error("approved should NOT transition directly to tasks_generated")
	}
	// phases_generated → phases_approved
	if !StatusPhasesGenerated.CanTransitionTo(StatusPhasesApproved) {
		t.Error("phases_generated should transition to phases_approved")
	}
	// phases_approved → tasks_generated
	if !StatusPhasesApproved.CanTransitionTo(StatusTasksGenerated) {
		t.Error("phases_approved should transition to tasks_generated")
	}
	// tasks_generated → tasks_approved
	if !StatusTasksGenerated.CanTransitionTo(StatusTasksApproved) {
		t.Error("tasks_generated should transition to tasks_approved")
	}
	// tasks_generated → implementing should NOT work
	if StatusTasksGenerated.CanTransitionTo(StatusImplementing) {
		t.Error("tasks_generated should NOT transition directly to implementing")
	}
	// tasks_approved → implementing
	if !StatusTasksApproved.CanTransitionTo(StatusImplementing) {
		t.Error("tasks_approved should transition to implementing")
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

	// Create a directory with malformed JSON at project-based path
	malformedPath := filepath.Join(tmpDir, ".semspec", "projects", "default", "plans", "malformed")
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

func TestCreateTask(t *testing.T) {
	task, err := CreateTask(PlanEntityID("test"), "test", 1, "Do something")
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	if task.ID != TaskEntityID("test", 1) {
		t.Errorf("ID = %q, want %q", task.ID, TaskEntityID("test", 1))
	}
	if task.PlanID != PlanEntityID("test") {
		t.Errorf("PlanID = %q, want %q", task.PlanID, PlanEntityID("test"))
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
	_, err := CreateTask(PlanEntityID("test"), "../invalid", 1, "Do something")
	if !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("expected ErrInvalidSlug, got %v", err)
	}
}

func TestManager_SaveAndLoadTasks(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Ensure the plan directory exists
	m.CreatePlan(ctx, "test-tasks", "Test tasks")

	task1, _ := CreateTask(PlanEntityID("test-tasks"), "test-tasks", 1, "First task")
	task2, _ := CreateTask(PlanEntityID("test-tasks"), "test-tasks", 2, "Second task")
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

	if loaded[0].ID != TaskEntityID("test-tasks", 1) {
		t.Errorf("loaded[0].ID = %q", loaded[0].ID)
	}
	if loaded[1].ID != TaskEntityID("test-tasks", 2) {
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

	// Create directory and write malformed JSON at project-based path
	planPath := filepath.Join(tmpDir, ".semspec", "projects", "default", "plans", "malformed")
	os.MkdirAll(planPath, 0755)
	os.WriteFile(filepath.Join(planPath, "tasks.json"), []byte("[invalid json"), 0644)

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
	task, _ := CreateTask(PlanEntityID("test-update"), "test-update", 1, "Task one")
	m.SaveTasks(ctx, []Task{*task}, "test-update")

	// Transition to in_progress
	err := m.UpdateTaskStatus(ctx, "test-update", TaskEntityID("test-update", 1), TaskStatusInProgress)
	if err != nil {
		t.Fatalf("UpdateTaskStatus to in_progress failed: %v", err)
	}

	loaded, _ := m.LoadTasks(ctx, "test-update")
	if loaded[0].Status != TaskStatusInProgress {
		t.Errorf("status = %q, want %q", loaded[0].Status, TaskStatusInProgress)
	}

	// Transition to completed
	err = m.UpdateTaskStatus(ctx, "test-update", TaskEntityID("test-update", 1), TaskStatusCompleted)
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
	task, _ := CreateTask(PlanEntityID("test-invalid"), "test-invalid", 1, "Task one")
	m.SaveTasks(ctx, []Task{*task}, "test-invalid")

	// Try to skip in_progress and go directly to completed
	err := m.UpdateTaskStatus(ctx, "test-invalid", TaskEntityID("test-invalid", 1), TaskStatusCompleted)
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

	err := m.UpdateTaskStatus(ctx, "test-notfound", TaskEntityID("test-notfound", 999), TaskStatusInProgress)
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
		task, _ := CreateTask(PlanEntityID("test-concurrent"), "test-concurrent", i, "Task")
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
			taskID := TaskEntityID("test-concurrent", taskNum)
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
	task1, _ := CreateTask(PlanEntityID("test-get"), "test-get", 1, "First")
	task2, _ := CreateTask(PlanEntityID("test-get"), "test-get", 2, "Second")
	m.SaveTasks(ctx, []Task{*task1, *task2}, "test-get")

	task, err := m.GetTask(ctx, "test-get", TaskEntityID("test-get", 2))
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

	_, err := m.GetTask(ctx, "test-get-nf", TaskEntityID("test-get-nf", 999))
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}

// Note: Task generation is now done by the task-generator component via LLM.
// The Manager.GenerateTasksFromPlan function was removed as part of the
// Goal/Context/Scope refactor. Tasks are created manually in tests or
// by the task-generator component in production.

func TestPlan_JSON(t *testing.T) {
	now := time.Now()
	plan := Plan{
		ID:       PlanEntityID("test"),
		Slug:     "test",
		Title:    "Test Plan",
		Approved: true,
		Goal:     "Implement feature X",
		Context:  "Current system lacks feature X",
		Scope: Scope{
			Include:    []string{"api/", "lib/"},
			Exclude:    []string{"vendor/"},
			DoNotTouch: []string{"config.yaml"},
		},
		CreatedAt:  now,
		ApprovedAt: &now,
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
	if !decoded.Approved {
		t.Errorf("Approved should be true")
	}
	if decoded.Goal != plan.Goal {
		t.Errorf("Goal mismatch")
	}
	if decoded.Context != plan.Context {
		t.Errorf("Context mismatch")
	}
	if len(decoded.Scope.Include) != 2 {
		t.Errorf("Scope.Include length = %d, want 2", len(decoded.Scope.Include))
	}
	if decoded.ApprovedAt == nil {
		t.Error("ApprovedAt should not be nil")
	}
}

func TestPlan_ExecutionTraceIDs_JSON(t *testing.T) {
	now := time.Now()
	plan := Plan{
		ID:                PlanEntityID("test"),
		Slug:              "test",
		Title:             "Test Plan",
		Goal:              "Implement feature X",
		CreatedAt:         now,
		ExecutionTraceIDs: []string{"trace-abc123", "trace-def456"},
	}

	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Verify the JSON contains execution_trace_ids
	jsonStr := string(data)
	if !contains(jsonStr, "execution_trace_ids") {
		t.Error("JSON should contain execution_trace_ids field")
	}
	if !contains(jsonStr, "trace-abc123") {
		t.Error("JSON should contain trace-abc123")
	}

	var decoded Plan
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if len(decoded.ExecutionTraceIDs) != 2 {
		t.Errorf("ExecutionTraceIDs length = %d, want 2", len(decoded.ExecutionTraceIDs))
	}
	if decoded.ExecutionTraceIDs[0] != "trace-abc123" {
		t.Errorf("ExecutionTraceIDs[0] = %q, want %q", decoded.ExecutionTraceIDs[0], "trace-abc123")
	}
}

func TestPlan_ExecutionTraceIDs_OmitEmpty(t *testing.T) {
	plan := Plan{
		ID:    PlanEntityID("test"),
		Slug:  "test",
		Title: "Test Plan",
		Goal:  "Implement feature X",
	}

	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Verify the JSON does NOT contain execution_trace_ids when empty (omitempty)
	jsonStr := string(data)
	if contains(jsonStr, "execution_trace_ids") {
		t.Error("JSON should NOT contain execution_trace_ids field when empty")
	}
}

// contains is a simple helper for checking if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsImpl(s, substr))
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestTask_JSON(t *testing.T) {
	now := time.Now()
	task := Task{
		ID:          TaskEntityID("test", 1),
		PlanID:      PlanEntityID("test"),
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

	err = m.UpdateTaskStatus(ctx, "test", TaskEntityID("test", 1), TaskStatusInProgress)
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
	plan.Goal = "Updated goal"
	plan.Context = "Updated context"

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
	if loaded.Goal != "Updated goal" {
		t.Errorf("Goal = %q, want %q", loaded.Goal, "Updated goal")
	}
	if loaded.Context != "Updated context" {
		t.Errorf("Context = %q, want %q", loaded.Context, "Updated context")
	}
}

// TestManager_SavePlan_ContextCancellation tests that SavePlan respects context.
func TestManager_SavePlan_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	plan := &Plan{
		ID:    PlanEntityID("test"),
		Slug:  "test",
		Title: "Test",
	}

	err := m.SavePlan(ctx, plan)
	if err == nil {
		t.Error("SavePlan should fail with cancelled context")
	}
}

// TestPlan_FieldMutations tests modifying plan fields, save, reload, verify.
// Tests both new (Goal/Context/Scope) and legacy fields for backwards compatibility.
func TestPlan_FieldMutations(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Create plan
	plan, err := m.CreatePlan(ctx, "mutations", "Mutations Test")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	// Test Goal/Context/Scope fields
	plan.Goal = "Implement OAuth 2.0 refresh token flow"
	plan.Context = "Complex multi-service architecture with auth issues"
	plan.Scope = Scope{
		Include:    []string{"api/auth/*", "internal/token/*"},
		Exclude:    []string{"vendor/*"},
		DoNotTouch: []string{"config.yaml"},
	}

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
	if loaded.Goal != plan.Goal {
		t.Errorf("Goal = %q, want %q", loaded.Goal, plan.Goal)
	}
	if loaded.Context != plan.Context {
		t.Errorf("Context = %q, want %q", loaded.Context, plan.Context)
	}
	if len(loaded.Scope.Include) != len(plan.Scope.Include) {
		t.Errorf("Scope.Include length = %d, want %d", len(loaded.Scope.Include), len(plan.Scope.Include))
	}
	if len(loaded.Scope.Exclude) != len(plan.Scope.Exclude) {
		t.Errorf("Scope.Exclude length = %d, want %d", len(loaded.Scope.Exclude), len(plan.Scope.Exclude))
	}
	if len(loaded.Scope.DoNotTouch) != len(plan.Scope.DoNotTouch) {
		t.Errorf("Scope.DoNotTouch length = %d, want %d", len(loaded.Scope.DoNotTouch), len(plan.Scope.DoNotTouch))
	}
}

// TestScope_Mutations tests modifying Include/Exclude/DoNotTouch arrays.
func TestScope_Mutations(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, err := m.CreatePlan(ctx, "scope-mut", "Scope Mutation")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	// Verify initial empty state
	if len(plan.Scope.Include) != 0 {
		t.Errorf("initial Include = %v, want empty", plan.Scope.Include)
	}

	// Modify scope
	plan.Scope.Include = []string{"api/", "lib/auth/", "internal/middleware/"}
	plan.Scope.Exclude = []string{"vendor/", "third_party/"}
	plan.Scope.DoNotTouch = []string{"config.yaml", ".env", "secrets/"}

	// Save and reload
	if err := m.SavePlan(ctx, plan); err != nil {
		t.Fatalf("SavePlan failed: %v", err)
	}

	loaded, err := m.LoadPlan(ctx, "scope-mut")
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}

	// Verify scope
	if len(loaded.Scope.Include) != 3 {
		t.Errorf("Include length = %d, want 3", len(loaded.Scope.Include))
	}
	if len(loaded.Scope.Exclude) != 2 {
		t.Errorf("Exclude length = %d, want 2", len(loaded.Scope.Exclude))
	}
	if len(loaded.Scope.DoNotTouch) != 3 {
		t.Errorf("DoNotTouch length = %d, want 3", len(loaded.Scope.DoNotTouch))
	}

	// Verify specific values
	if loaded.Scope.Include[0] != "api/" {
		t.Errorf("Include[0] = %q, want %q", loaded.Scope.Include[0], "api/")
	}
	if loaded.Scope.DoNotTouch[2] != "secrets/" {
		t.Errorf("DoNotTouch[2] = %q, want %q", loaded.Scope.DoNotTouch[2], "secrets/")
	}
}

// setupTestPlanWithTasks creates a plan and populates it with n tasks.
// Returns the task slice for use in test assertions.
func setupTestPlanWithTasks(ctx context.Context, t *testing.T, m *Manager, slug, title string, n int) []Task {
	t.Helper()
	if _, err := m.CreatePlan(ctx, slug, title); err != nil {
		t.Fatalf("CreatePlan(%s) failed: %v", slug, err)
	}
	tasks := make([]Task, 0, n)
	for i := 1; i <= n; i++ {
		task, _ := CreateTask(PlanEntityID(slug), slug, i, title+" Task "+itoa(i))
		tasks = append(tasks, *task)
	}
	if err := m.SaveTasks(ctx, tasks, slug); err != nil {
		t.Fatalf("SaveTasks(%s) failed: %v", slug, err)
	}
	return tasks
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
	setupTestPlanWithTasks(ctx, t, m, "read-plan", "Read Plan", 5)
	setupTestPlanWithTasks(ctx, t, m, "write-plan", "Write Plan", 5)

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
			taskID := TaskEntityID("write-plan", taskNum)
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
	task, _ := CreateTask(PlanEntityID("lifecycle"), "lifecycle", 1, "Complete lifecycle")
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
	err := m.UpdateTaskStatus(ctx, "lifecycle", TaskEntityID("lifecycle", 1), TaskStatusInProgress)
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
	err = m.UpdateTaskStatus(ctx, "lifecycle", TaskEntityID("lifecycle", 1), TaskStatusCompleted)
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
	err = m.UpdateTaskStatus(ctx, "lifecycle", TaskEntityID("lifecycle", 1), TaskStatusInProgress)
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
	task, _ := CreateTask(PlanEntityID("failed-lifecycle"), "failed-lifecycle", 1, "Will fail")
	m.SaveTasks(ctx, []Task{*task}, "failed-lifecycle")

	// Transition to in_progress
	m.UpdateTaskStatus(ctx, "failed-lifecycle", TaskEntityID("failed-lifecycle", 1), TaskStatusInProgress)

	// Transition to failed
	err := m.UpdateTaskStatus(ctx, "failed-lifecycle", TaskEntityID("failed-lifecycle", 1), TaskStatusFailed)
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
	err = m.UpdateTaskStatus(ctx, "failed-lifecycle", TaskEntityID("failed-lifecycle", 1), TaskStatusCompleted)
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
	task, _ := CreateTask(PlanEntityID("direct-fail"), "direct-fail", 1, "Skip to failed")
	m.SaveTasks(ctx, []Task{*task}, "direct-fail")

	// Can go directly from pending to failed
	err := m.UpdateTaskStatus(ctx, "direct-fail", TaskEntityID("direct-fail", 1), TaskStatusFailed)
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
	task, _ := CreateTask(PlanEntityID("criteria"), "criteria", 1, "Task with criteria")
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
	task, _ := CreateTask(PlanEntityID("files-mod"), "files-mod", 1, "Task with files")
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
		ID:    PlanEntityID("test"),
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

	err := m.UpdateTaskStatus(ctx, "../invalid", TaskEntityID("test", 1), TaskStatusInProgress)
	if !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("expected ErrInvalidSlug, got %v", err)
	}
}

// TestManager_GetTask_InvalidSlug tests that GetTask validates slug.
func TestManager_GetTask_InvalidSlug(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	_, err := m.GetTask(ctx, "../invalid", TaskEntityID("test", 1))
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
	task, _ := CreateTask(PlanEntityID("inv-status"), "inv-status", 1, "Task")
	m.SaveTasks(ctx, []Task{*task}, "inv-status")

	err := m.UpdateTaskStatus(ctx, "inv-status", TaskEntityID("inv-status", 1), TaskStatus("unknown"))
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition for invalid status, got %v", err)
	}
}

// ============================================================================
// Task Approval Tests
// ============================================================================

// TestManager_SubmitTaskForApproval tests submitting a single task for approval.
func TestManager_SubmitTaskForApproval(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "submit-test", "Submit Test")
	task, _ := CreateTask(PlanEntityID("submit-test"), "submit-test", 1, "Task 1")
	m.SaveTasks(ctx, []Task{*task}, "submit-test")

	// Submit for approval
	err := m.SubmitTaskForApproval(ctx, "submit-test", TaskEntityID("submit-test", 1))
	if err != nil {
		t.Fatalf("SubmitTaskForApproval failed: %v", err)
	}

	// Verify status
	loaded, _ := m.LoadTasks(ctx, "submit-test")
	if loaded[0].Status != TaskStatusPendingApproval {
		t.Errorf("status = %q, want %q", loaded[0].Status, TaskStatusPendingApproval)
	}
}

// TestManager_SubmitTaskForApproval_NotPending tests submitting a non-pending task.
func TestManager_SubmitTaskForApproval_NotPending(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "submit-notpending", "Submit Not Pending")
	task, _ := CreateTask(PlanEntityID("submit-notpending"), "submit-notpending", 1, "Task 1")
	task.Status = TaskStatusInProgress // Already in progress
	m.SaveTasks(ctx, []Task{*task}, "submit-notpending")

	err := m.SubmitTaskForApproval(ctx, "submit-notpending", TaskEntityID("submit-notpending", 1))
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}

// TestManager_SubmitTaskForApproval_NotFound tests submitting a non-existent task.
func TestManager_SubmitTaskForApproval_NotFound(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "submit-nf", "Submit Not Found")
	m.SaveTasks(ctx, []Task{}, "submit-nf")

	err := m.SubmitTaskForApproval(ctx, "submit-nf", TaskEntityID("submit-nf", 999))
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}

// TestManager_SubmitAllTasksForApproval tests submitting all pending tasks.
func TestManager_SubmitAllTasksForApproval(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "submit-all", "Submit All")
	task1, _ := CreateTask(PlanEntityID("submit-all"), "submit-all", 1, "Task 1")
	task2, _ := CreateTask(PlanEntityID("submit-all"), "submit-all", 2, "Task 2")
	task3, _ := CreateTask(PlanEntityID("submit-all"), "submit-all", 3, "Task 3")
	task3.Status = TaskStatusInProgress // Already started, should be skipped
	m.SaveTasks(ctx, []Task{*task1, *task2, *task3}, "submit-all")

	count, err := m.SubmitAllTasksForApproval(ctx, "submit-all")
	if err != nil {
		t.Fatalf("SubmitAllTasksForApproval failed: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}

	// Verify statuses
	loaded, _ := m.LoadTasks(ctx, "submit-all")
	if loaded[0].Status != TaskStatusPendingApproval {
		t.Errorf("task 1 status = %q, want %q", loaded[0].Status, TaskStatusPendingApproval)
	}
	if loaded[1].Status != TaskStatusPendingApproval {
		t.Errorf("task 2 status = %q, want %q", loaded[1].Status, TaskStatusPendingApproval)
	}
	if loaded[2].Status != TaskStatusInProgress {
		t.Errorf("task 3 status = %q, want %q (should be unchanged)", loaded[2].Status, TaskStatusInProgress)
	}
}

// TestManager_ApproveTask tests approving a single task.
func TestManager_ApproveTask(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "approve-single", "Approve Single")
	task, _ := CreateTask(PlanEntityID("approve-single"), "approve-single", 1, "Task 1")
	task.Status = TaskStatusPendingApproval
	m.SaveTasks(ctx, []Task{*task}, "approve-single")

	approved, err := m.ApproveTask(ctx, "approve-single", TaskEntityID("approve-single", 1), "user-123")
	if err != nil {
		t.Fatalf("ApproveTask failed: %v", err)
	}

	// Verify returned task
	if approved.Status != TaskStatusApproved {
		t.Errorf("approved status = %q, want %q", approved.Status, TaskStatusApproved)
	}
	if approved.ApprovedBy != "user-123" {
		t.Errorf("approved_by = %q, want %q", approved.ApprovedBy, "user-123")
	}
	if approved.ApprovedAt == nil {
		t.Error("approved_at should be set")
	}

	// Verify persistence
	loaded, _ := m.LoadTasks(ctx, "approve-single")
	if loaded[0].Status != TaskStatusApproved {
		t.Errorf("loaded status = %q, want %q", loaded[0].Status, TaskStatusApproved)
	}
	if loaded[0].ApprovedBy != "user-123" {
		t.Errorf("loaded approved_by = %q, want %q", loaded[0].ApprovedBy, "user-123")
	}
	if loaded[0].ApprovedAt == nil {
		t.Error("loaded approved_at should be set")
	}
}

// TestManager_ApproveTask_NotPendingApproval tests approving a task not in pending_approval.
func TestManager_ApproveTask_NotPendingApproval(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "approve-wrong-status", "Approve Wrong Status")
	task, _ := CreateTask(PlanEntityID("approve-wrong-status"), "approve-wrong-status", 1, "Task 1")
	// Status is pending, not pending_approval
	m.SaveTasks(ctx, []Task{*task}, "approve-wrong-status")

	_, err := m.ApproveTask(ctx, "approve-wrong-status", TaskEntityID("approve-wrong-status", 1), "user")
	if !errors.Is(err, ErrTaskNotPendingApproval) {
		t.Errorf("expected ErrTaskNotPendingApproval, got %v", err)
	}
}

// TestManager_ApproveTask_NotFound tests approving a non-existent task.
func TestManager_ApproveTask_NotFound(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "approve-nf", "Approve Not Found")
	m.SaveTasks(ctx, []Task{}, "approve-nf")

	_, err := m.ApproveTask(ctx, "approve-nf", TaskEntityID("approve-nf", 999), "user")
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}

// TestManager_RejectTask tests rejecting a single task.
func TestManager_RejectTask(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "reject-single", "Reject Single")
	task, _ := CreateTask(PlanEntityID("reject-single"), "reject-single", 1, "Task 1")
	task.Status = TaskStatusPendingApproval
	m.SaveTasks(ctx, []Task{*task}, "reject-single")

	rejected, err := m.RejectTask(ctx, "reject-single", TaskEntityID("reject-single", 1), "Acceptance criteria unclear")
	if err != nil {
		t.Fatalf("RejectTask failed: %v", err)
	}

	// Verify returned task
	if rejected.Status != TaskStatusRejected {
		t.Errorf("rejected status = %q, want %q", rejected.Status, TaskStatusRejected)
	}
	if rejected.RejectionReason != "Acceptance criteria unclear" {
		t.Errorf("rejection_reason = %q, want %q", rejected.RejectionReason, "Acceptance criteria unclear")
	}

	// Verify persistence
	loaded, _ := m.LoadTasks(ctx, "reject-single")
	if loaded[0].Status != TaskStatusRejected {
		t.Errorf("loaded status = %q, want %q", loaded[0].Status, TaskStatusRejected)
	}
	if loaded[0].RejectionReason != "Acceptance criteria unclear" {
		t.Errorf("loaded rejection_reason = %q", loaded[0].RejectionReason)
	}
}

// TestManager_RejectTask_NoReason tests rejecting without a reason.
func TestManager_RejectTask_NoReason(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "reject-no-reason", "Reject No Reason")
	task, _ := CreateTask(PlanEntityID("reject-no-reason"), "reject-no-reason", 1, "Task 1")
	task.Status = TaskStatusPendingApproval
	m.SaveTasks(ctx, []Task{*task}, "reject-no-reason")

	_, err := m.RejectTask(ctx, "reject-no-reason", TaskEntityID("reject-no-reason", 1), "")
	if !errors.Is(err, ErrRejectionReasonRequired) {
		t.Errorf("expected ErrRejectionReasonRequired, got %v", err)
	}
}

// TestManager_RejectTask_NotPendingApproval tests rejecting a task not in pending_approval.
func TestManager_RejectTask_NotPendingApproval(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "reject-wrong-status", "Reject Wrong Status")
	task, _ := CreateTask(PlanEntityID("reject-wrong-status"), "reject-wrong-status", 1, "Task 1")
	m.SaveTasks(ctx, []Task{*task}, "reject-wrong-status")

	_, err := m.RejectTask(ctx, "reject-wrong-status", TaskEntityID("reject-wrong-status", 1), "reason")
	if !errors.Is(err, ErrTaskNotPendingApproval) {
		t.Errorf("expected ErrTaskNotPendingApproval, got %v", err)
	}
}

// TestManager_ApproveAllTasks tests approving all pending_approval tasks.
func TestManager_ApproveAllTasks(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "approve-all", "Approve All")
	task1, _ := CreateTask(PlanEntityID("approve-all"), "approve-all", 1, "Task 1")
	task1.Status = TaskStatusPendingApproval
	task2, _ := CreateTask(PlanEntityID("approve-all"), "approve-all", 2, "Task 2")
	task2.Status = TaskStatusPendingApproval
	task3, _ := CreateTask(PlanEntityID("approve-all"), "approve-all", 3, "Task 3")
	task3.Status = TaskStatusApproved // Already approved, should be skipped
	m.SaveTasks(ctx, []Task{*task1, *task2, *task3}, "approve-all")

	approved, err := m.ApproveAllTasks(ctx, "approve-all", "batch-user")
	if err != nil {
		t.Fatalf("ApproveAllTasks failed: %v", err)
	}
	if len(approved) != 2 {
		t.Errorf("approved count = %d, want 2", len(approved))
	}

	// Verify all approved tasks have correct fields
	for _, task := range approved {
		if task.Status != TaskStatusApproved {
			t.Errorf("task %s status = %q, want %q", task.ID, task.Status, TaskStatusApproved)
		}
		if task.ApprovedBy != "batch-user" {
			t.Errorf("task %s approved_by = %q, want %q", task.ID, task.ApprovedBy, "batch-user")
		}
	}

	// Verify persistence
	loaded, _ := m.LoadTasks(ctx, "approve-all")
	for _, task := range loaded {
		if task.Status != TaskStatusApproved {
			t.Errorf("loaded task %s status = %q, want %q", task.ID, task.Status, TaskStatusApproved)
		}
	}
}

// TestManager_ResubmitTask tests resubmitting a rejected task.
func TestManager_ResubmitTask(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "resubmit", "Resubmit")
	task, _ := CreateTask(PlanEntityID("resubmit"), "resubmit", 1, "Task 1")
	task.Status = TaskStatusRejected
	task.RejectionReason = "Original rejection reason"
	m.SaveTasks(ctx, []Task{*task}, "resubmit")

	err := m.ResubmitTask(ctx, "resubmit", TaskEntityID("resubmit", 1))
	if err != nil {
		t.Fatalf("ResubmitTask failed: %v", err)
	}

	// Verify status is now pending_approval
	loaded, _ := m.LoadTasks(ctx, "resubmit")
	if loaded[0].Status != TaskStatusPendingApproval {
		t.Errorf("status = %q, want %q", loaded[0].Status, TaskStatusPendingApproval)
	}
	// Rejection reason should be preserved for reference
	if loaded[0].RejectionReason != "Original rejection reason" {
		t.Errorf("rejection_reason should be preserved, got %q", loaded[0].RejectionReason)
	}
}

// TestManager_ResubmitTask_NotRejected tests resubmitting a task that isn't rejected.
func TestManager_ResubmitTask_NotRejected(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "resubmit-notrej", "Resubmit Not Rejected")
	task, _ := CreateTask(PlanEntityID("resubmit-notrej"), "resubmit-notrej", 1, "Task 1")
	task.Status = TaskStatusPending
	m.SaveTasks(ctx, []Task{*task}, "resubmit-notrej")

	err := m.ResubmitTask(ctx, "resubmit-notrej", TaskEntityID("resubmit-notrej", 1))
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}

// TestTask_ApprovalLifecycle tests the full approval lifecycle:
// pending → pending_approval → approved → in_progress → completed
func TestTask_ApprovalLifecycle(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "approval-lifecycle", "Approval Lifecycle")
	task, _ := CreateTask(PlanEntityID("approval-lifecycle"), "approval-lifecycle", 1, "Task 1")
	m.SaveTasks(ctx, []Task{*task}, "approval-lifecycle")
	taskID := TaskEntityID("approval-lifecycle", 1)

	// Step 1: Submit for approval (pending → pending_approval)
	if err := m.SubmitTaskForApproval(ctx, "approval-lifecycle", taskID); err != nil {
		t.Fatalf("SubmitTaskForApproval failed: %v", err)
	}

	loaded, _ := m.LoadTasks(ctx, "approval-lifecycle")
	if loaded[0].Status != TaskStatusPendingApproval {
		t.Errorf("after submit: status = %q, want %q", loaded[0].Status, TaskStatusPendingApproval)
	}

	// Step 2: Approve (pending_approval → approved)
	_, err := m.ApproveTask(ctx, "approval-lifecycle", taskID, "approver")
	if err != nil {
		t.Fatalf("ApproveTask failed: %v", err)
	}

	loaded, _ = m.LoadTasks(ctx, "approval-lifecycle")
	if loaded[0].Status != TaskStatusApproved {
		t.Errorf("after approve: status = %q, want %q", loaded[0].Status, TaskStatusApproved)
	}

	// Step 3: Start execution (approved → in_progress)
	if err := m.UpdateTaskStatus(ctx, "approval-lifecycle", taskID, TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus to in_progress failed: %v", err)
	}

	loaded, _ = m.LoadTasks(ctx, "approval-lifecycle")
	if loaded[0].Status != TaskStatusInProgress {
		t.Errorf("after start: status = %q, want %q", loaded[0].Status, TaskStatusInProgress)
	}

	// Step 4: Complete (in_progress → completed)
	if err := m.UpdateTaskStatus(ctx, "approval-lifecycle", taskID, TaskStatusCompleted); err != nil {
		t.Fatalf("UpdateTaskStatus to completed failed: %v", err)
	}

	loaded, _ = m.LoadTasks(ctx, "approval-lifecycle")
	if loaded[0].Status != TaskStatusCompleted {
		t.Errorf("after complete: status = %q, want %q", loaded[0].Status, TaskStatusCompleted)
	}
}

// TestTask_RejectionCycle tests the rejection and resubmission cycle:
// pending → pending_approval → rejected → pending_approval → approved
func TestTask_RejectionCycle(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "rejection-cycle", "Rejection Cycle")
	task, _ := CreateTask(PlanEntityID("rejection-cycle"), "rejection-cycle", 1, "Task 1")
	m.SaveTasks(ctx, []Task{*task}, "rejection-cycle")
	taskID := TaskEntityID("rejection-cycle", 1)

	// Step 1: Submit for approval
	m.SubmitTaskForApproval(ctx, "rejection-cycle", taskID)

	// Step 2: Reject
	_, err := m.RejectTask(ctx, "rejection-cycle", taskID, "Needs more detail")
	if err != nil {
		t.Fatalf("RejectTask failed: %v", err)
	}

	loaded, _ := m.LoadTasks(ctx, "rejection-cycle")
	if loaded[0].Status != TaskStatusRejected {
		t.Errorf("after reject: status = %q, want %q", loaded[0].Status, TaskStatusRejected)
	}

	// Step 3: Resubmit
	if err := m.ResubmitTask(ctx, "rejection-cycle", taskID); err != nil {
		t.Fatalf("ResubmitTask failed: %v", err)
	}

	loaded, _ = m.LoadTasks(ctx, "rejection-cycle")
	if loaded[0].Status != TaskStatusPendingApproval {
		t.Errorf("after resubmit: status = %q, want %q", loaded[0].Status, TaskStatusPendingApproval)
	}

	// Step 4: Approve this time
	_, err = m.ApproveTask(ctx, "rejection-cycle", taskID, "approver")
	if err != nil {
		t.Fatalf("ApproveTask failed: %v", err)
	}

	loaded, _ = m.LoadTasks(ctx, "rejection-cycle")
	if loaded[0].Status != TaskStatusApproved {
		t.Errorf("after approve: status = %q, want %q", loaded[0].Status, TaskStatusApproved)
	}
}

// TestTask_ApprovalClearsRejection tests that approval clears previous rejection reason.
func TestTask_ApprovalClearsRejection(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "clear-rejection", "Clear Rejection")
	task, _ := CreateTask(PlanEntityID("clear-rejection"), "clear-rejection", 1, "Task 1")
	task.Status = TaskStatusPendingApproval
	task.RejectionReason = "Previous rejection"
	m.SaveTasks(ctx, []Task{*task}, "clear-rejection")

	_, err := m.ApproveTask(ctx, "clear-rejection", TaskEntityID("clear-rejection", 1), "user")
	if err != nil {
		t.Fatalf("ApproveTask failed: %v", err)
	}

	loaded, _ := m.LoadTasks(ctx, "clear-rejection")
	if loaded[0].RejectionReason != "" {
		t.Errorf("rejection_reason should be cleared, got %q", loaded[0].RejectionReason)
	}
}

// TestTask_JSON_WithApprovalFields tests JSON serialization of approval fields.
func TestTask_JSON_WithApprovalFields(t *testing.T) {
	now := time.Now()
	task := Task{
		ID:              TaskEntityID("test", 1),
		PlanID:          PlanEntityID("test"),
		Sequence:        1,
		Description:     "Do something",
		Status:          TaskStatusApproved,
		CreatedAt:       now,
		ApprovedBy:      "user-123",
		ApprovedAt:      &now,
		RejectionReason: "", // Empty after approval
	}

	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Task
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ApprovedBy != "user-123" {
		t.Errorf("ApprovedBy = %q, want %q", decoded.ApprovedBy, "user-123")
	}
	if decoded.ApprovedAt == nil {
		t.Error("ApprovedAt should not be nil")
	}
}

// ============================================================================
// Phase 4: Task CRUD Tests (TDD)
// ============================================================================

// TestManager_CreateTaskManual tests creating a task manually without LLM.
func TestManager_CreateTaskManual(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Create a plan
	plan, err := m.CreatePlan(ctx, "manual-task", "Manual Task Plan")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	// Create a manual task
	req := CreateTaskRequest{
		Description: "Implement user authentication",
		Type:        TaskTypeImplement,
		AcceptanceCriteria: []AcceptanceCriterion{
			{Given: "valid credentials", When: "logging in", Then: "user is authenticated"},
		},
		Files:     []string{"api/auth.go", "api/auth_test.go"},
		DependsOn: []string{},
	}

	task, err := m.CreateTaskManual(ctx, plan.Slug, req)
	if err != nil {
		t.Fatalf("CreateTaskManual failed: %v", err)
	}

	// Verify task fields
	if task.Description != req.Description {
		t.Errorf("Description = %q, want %q", task.Description, req.Description)
	}
	if task.Type != req.Type {
		t.Errorf("Type = %q, want %q", task.Type, req.Type)
	}
	if len(task.AcceptanceCriteria) != 1 {
		t.Errorf("AcceptanceCriteria length = %d, want 1", len(task.AcceptanceCriteria))
	}
	if len(task.Files) != 2 {
		t.Errorf("Files length = %d, want 2", len(task.Files))
	}
	if task.Status != TaskStatusPending {
		t.Errorf("Status = %q, want %q", task.Status, TaskStatusPending)
	}
	if task.Sequence != 1 {
		t.Errorf("Sequence = %d, want 1", task.Sequence)
	}

	// Verify persistence
	loaded, err := m.LoadTasks(ctx, plan.Slug)
	if err != nil {
		t.Fatalf("LoadTasks failed: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 task, got %d", len(loaded))
	}
	if loaded[0].ID != task.ID {
		t.Errorf("loaded task ID = %q, want %q", loaded[0].ID, task.ID)
	}
}

// TestManager_CreateTaskManual_SequenceIncrement tests that sequence auto-increments.
func TestManager_CreateTaskManual_SequenceIncrement(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, _ := m.CreatePlan(ctx, "seq-test", "Sequence Test")

	// Create first task
	req1 := CreateTaskRequest{Description: "Task 1", Type: TaskTypeImplement}
	task1, _ := m.CreateTaskManual(ctx, plan.Slug, req1)

	// Create second task
	req2 := CreateTaskRequest{Description: "Task 2", Type: TaskTypeTest}
	task2, _ := m.CreateTaskManual(ctx, plan.Slug, req2)

	// Verify sequences
	if task1.Sequence != 1 {
		t.Errorf("task1 Sequence = %d, want 1", task1.Sequence)
	}
	if task2.Sequence != 2 {
		t.Errorf("task2 Sequence = %d, want 2", task2.Sequence)
	}

	// Verify persistence
	loaded, _ := m.LoadTasks(ctx, plan.Slug)
	if len(loaded) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(loaded))
	}
}

// TestManager_CreateTaskManual_InvalidSlug tests creating task with invalid slug.
func TestManager_CreateTaskManual_InvalidSlug(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	req := CreateTaskRequest{Description: "Task", Type: TaskTypeImplement}
	_, err := m.CreateTaskManual(ctx, "../invalid", req)
	if !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("expected ErrInvalidSlug, got %v", err)
	}
}

// TestManager_CreateTaskManual_PlanNotFound tests creating task for non-existent plan.
func TestManager_CreateTaskManual_PlanNotFound(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	req := CreateTaskRequest{Description: "Task", Type: TaskTypeImplement}
	_, err := m.CreateTaskManual(ctx, "nonexistent", req)
	if !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("expected ErrPlanNotFound, got %v", err)
	}
}

// TestManager_CreateTaskManual_EmptyDescription tests creating task without description.
func TestManager_CreateTaskManual_EmptyDescription(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	m.CreatePlan(ctx, "desc-test", "Description Test")

	req := CreateTaskRequest{Description: "", Type: TaskTypeImplement}
	_, err := m.CreateTaskManual(ctx, "desc-test", req)
	if !errors.Is(err, ErrDescriptionRequired) {
		t.Errorf("expected ErrDescriptionRequired, got %v", err)
	}
}

// TestManager_UpdateTask tests updating task fields.
func TestManager_UpdateTask(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, _ := m.CreatePlan(ctx, "update-test", "Update Test")
	createReq := CreateTaskRequest{
		Description: "Original description",
		Type:        TaskTypeImplement,
		Files:       []string{"file1.go"},
	}
	task, _ := m.CreateTaskManual(ctx, plan.Slug, createReq)

	// Update the task
	newDesc := "Updated description"
	newType := TaskTypeTest
	updateReq := UpdateTaskRequest{
		Description: &newDesc,
		Type:        &newType,
		Files:       []string{"file1.go", "file2.go"},
		AcceptanceCriteria: []AcceptanceCriterion{
			{Given: "code changes", When: "tests run", Then: "all pass"},
		},
	}

	updated, err := m.UpdateTask(ctx, plan.Slug, task.ID, updateReq)
	if err != nil {
		t.Fatalf("UpdateTask failed: %v", err)
	}

	// Verify updates
	if updated.Description != newDesc {
		t.Errorf("Description = %q, want %q", updated.Description, newDesc)
	}
	if updated.Type != newType {
		t.Errorf("Type = %q, want %q", updated.Type, newType)
	}
	if len(updated.Files) != 2 {
		t.Errorf("Files length = %d, want 2", len(updated.Files))
	}
	if len(updated.AcceptanceCriteria) != 1 {
		t.Errorf("AcceptanceCriteria length = %d, want 1", len(updated.AcceptanceCriteria))
	}

	// Verify persistence
	loaded, _ := m.LoadTasks(ctx, plan.Slug)
	if loaded[0].Description != newDesc {
		t.Errorf("persisted Description = %q, want %q", loaded[0].Description, newDesc)
	}
}

// TestManager_UpdateTask_Sequence tests updating task sequence for reordering.
func TestManager_UpdateTask_Sequence(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, _ := m.CreatePlan(ctx, "reorder-test", "Reorder Test")

	// Create three tasks
	req1 := CreateTaskRequest{Description: "Task 1", Type: TaskTypeImplement}
	task1, _ := m.CreateTaskManual(ctx, plan.Slug, req1)
	req2 := CreateTaskRequest{Description: "Task 2", Type: TaskTypeImplement}
	task2, _ := m.CreateTaskManual(ctx, plan.Slug, req2)
	req3 := CreateTaskRequest{Description: "Task 3", Type: TaskTypeImplement}
	task3, _ := m.CreateTaskManual(ctx, plan.Slug, req3)

	// Move task3 to position 1 (should swap with task1)
	newSeq := 1
	updateReq := UpdateTaskRequest{Sequence: &newSeq}
	updated, err := m.UpdateTask(ctx, plan.Slug, task3.ID, updateReq)
	if err != nil {
		t.Fatalf("UpdateTask failed: %v", err)
	}

	if updated.Sequence != 1 {
		t.Errorf("updated Sequence = %d, want 1", updated.Sequence)
	}

	// Verify all sequences
	loaded, _ := m.LoadTasks(ctx, plan.Slug)
	if len(loaded) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(loaded))
	}

	// Find task by ID and verify sequence
	for _, task := range loaded {
		if task.ID == task3.ID && task.Sequence != 1 {
			t.Errorf("task3 Sequence = %d, want 1", task.Sequence)
		}
		if task.ID == task1.ID && task.Sequence != 2 {
			t.Errorf("task1 Sequence = %d, want 2", task.Sequence)
		}
		if task.ID == task2.ID && task.Sequence != 3 {
			t.Errorf("task2 Sequence = %d, want 3", task.Sequence)
		}
	}
}

// TestManager_UpdateTask_BlockedDuringExecution tests state guards.
func TestManager_UpdateTask_BlockedDuringExecution(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, _ := m.CreatePlan(ctx, "blocked-test", "Blocked Test")
	req := CreateTaskRequest{Description: "Task", Type: TaskTypeImplement}
	task, _ := m.CreateTaskManual(ctx, plan.Slug, req)

	// Transition to in_progress
	m.UpdateTaskStatus(ctx, plan.Slug, task.ID, TaskStatusInProgress)

	// Try to update - should fail
	newDesc := "Updated"
	updateReq := UpdateTaskRequest{Description: &newDesc}
	_, err := m.UpdateTask(ctx, plan.Slug, task.ID, updateReq)
	if err == nil {
		t.Error("expected error when updating in_progress task")
	}
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}

	// Same for completed
	m.UpdateTaskStatus(ctx, plan.Slug, task.ID, TaskStatusCompleted)
	_, err = m.UpdateTask(ctx, plan.Slug, task.ID, updateReq)
	if err == nil {
		t.Error("expected error when updating completed task")
	}
}

// TestManager_UpdateTask_NotFound tests updating non-existent task.
func TestManager_UpdateTask_NotFound(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, _ := m.CreatePlan(ctx, "notfound-test", "Not Found Test")

	newDesc := "Updated"
	updateReq := UpdateTaskRequest{Description: &newDesc}
	_, err := m.UpdateTask(ctx, plan.Slug, TaskEntityID(plan.Slug, 999), updateReq)
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}

// TestManager_DeleteTask tests deleting a task.
func TestManager_DeleteTask(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, _ := m.CreatePlan(ctx, "delete-test", "Delete Test")

	// Create two tasks
	req1 := CreateTaskRequest{Description: "Task 1", Type: TaskTypeImplement}
	task1, _ := m.CreateTaskManual(ctx, plan.Slug, req1)
	req2 := CreateTaskRequest{Description: "Task 2", Type: TaskTypeImplement}
	task2, _ := m.CreateTaskManual(ctx, plan.Slug, req2)

	// Delete task1
	err := m.DeleteTask(ctx, plan.Slug, task1.ID)
	if err != nil {
		t.Fatalf("DeleteTask failed: %v", err)
	}

	// Verify only task2 remains
	loaded, _ := m.LoadTasks(ctx, plan.Slug)
	if len(loaded) != 1 {
		t.Fatalf("expected 1 task, got %d", len(loaded))
	}
	if loaded[0].ID != task2.ID {
		t.Errorf("remaining task ID = %q, want %q", loaded[0].ID, task2.ID)
	}

	// Verify sequence was renumbered
	if loaded[0].Sequence != 1 {
		t.Errorf("remaining task Sequence = %d, want 1", loaded[0].Sequence)
	}
}

// TestManager_DeleteTask_BlockedDuringExecution tests state guards.
func TestManager_DeleteTask_BlockedDuringExecution(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, _ := m.CreatePlan(ctx, "delete-blocked", "Delete Blocked")
	req := CreateTaskRequest{Description: "Task", Type: TaskTypeImplement}
	task, _ := m.CreateTaskManual(ctx, plan.Slug, req)

	// Transition to in_progress
	m.UpdateTaskStatus(ctx, plan.Slug, task.ID, TaskStatusInProgress)

	// Try to delete - should fail
	err := m.DeleteTask(ctx, plan.Slug, task.ID)
	if err == nil {
		t.Error("expected error when deleting in_progress task")
	}
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}

	// Same for completed
	m.UpdateTaskStatus(ctx, plan.Slug, task.ID, TaskStatusCompleted)
	err = m.DeleteTask(ctx, plan.Slug, task.ID)
	if err == nil {
		t.Error("expected error when deleting completed task")
	}
}

// TestManager_DeleteTask_NotFound tests deleting non-existent task.
func TestManager_DeleteTask_NotFound(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, _ := m.CreatePlan(ctx, "delete-nf", "Delete Not Found")

	err := m.DeleteTask(ctx, plan.Slug, TaskEntityID(plan.Slug, 999))
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}

// TestManager_DeleteTask_UpdatesDependencies tests that deletion cleans up dependencies.
func TestManager_DeleteTask_UpdatesDependencies(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, _ := m.CreatePlan(ctx, "delete-deps", "Delete Dependencies")

	// Create task1
	req1 := CreateTaskRequest{Description: "Task 1", Type: TaskTypeImplement}
	task1, _ := m.CreateTaskManual(ctx, plan.Slug, req1)

	// Create task2 that depends on task1
	req2 := CreateTaskRequest{
		Description: "Task 2",
		Type:        TaskTypeTest,
		DependsOn:   []string{task1.ID},
	}
	_, _ = m.CreateTaskManual(ctx, plan.Slug, req2)

	// Delete task1
	err := m.DeleteTask(ctx, plan.Slug, task1.ID)
	if err != nil {
		t.Fatalf("DeleteTask failed: %v", err)
	}

	// Verify task2's DependsOn was cleared
	loaded, _ := m.LoadTasks(ctx, plan.Slug)
	if len(loaded) != 1 {
		t.Fatalf("expected 1 task, got %d", len(loaded))
	}
	if len(loaded[0].DependsOn) != 0 {
		t.Errorf("DependsOn should be cleared, got %v", loaded[0].DependsOn)
	}
}

// TestManager_UpdatePlan tests updating plan fields.
func TestManager_UpdatePlan(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, err := m.CreatePlan(ctx, "test-update", "Original Title")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	plan.Goal = "Original goal"
	plan.Context = "Original context"
	if err := m.SavePlan(ctx, plan); err != nil {
		t.Fatalf("SavePlan failed: %v", err)
	}

	// Single-field updates
	updated := assertUpdatePlanTitle(t, ctx, m, plan.Slug, "Updated Title", "Original goal")
	assertUpdatePlanGoal(t, ctx, m, plan.Slug, "New goal", updated.Title)
	assertUpdatePlanContext(t, ctx, m, plan.Slug, "New context")

	// Multi-field update
	title2, goal2, context2 := "Title 2", "Goal 2", "Context 2"
	updated, err = m.UpdatePlan(ctx, plan.Slug, UpdatePlanRequest{
		Title:   &title2,
		Goal:    &goal2,
		Context: &context2,
	})
	if err != nil {
		t.Fatalf("UpdatePlan (multi-field) failed: %v", err)
	}
	if updated.Title != title2 || updated.Goal != goal2 || updated.Context != context2 {
		t.Error("Multiple field update failed")
	}

	// Verify changes persist across a fresh load.
	assertPlanFieldsPersisted(t, ctx, m, plan.Slug, title2, goal2, context2)
}

// assertUpdatePlanTitle updates only the title and verifies the other fields are unchanged.
// Returns the updated plan.
func assertUpdatePlanTitle(t *testing.T, ctx context.Context, m *Manager, slug, newTitle, wantGoal string) *Plan {
	t.Helper()
	updated, err := m.UpdatePlan(ctx, slug, UpdatePlanRequest{Title: &newTitle})
	if err != nil {
		t.Fatalf("UpdatePlan (title) failed: %v", err)
	}
	if updated.Title != newTitle {
		t.Errorf("Title = %q, want %q", updated.Title, newTitle)
	}
	if updated.Goal != wantGoal {
		t.Errorf("Goal changed unexpectedly: %q", updated.Goal)
	}
	return updated
}

// assertUpdatePlanGoal updates only the goal and verifies the title is unchanged.
func assertUpdatePlanGoal(t *testing.T, ctx context.Context, m *Manager, slug, newGoal, wantTitle string) {
	t.Helper()
	updated, err := m.UpdatePlan(ctx, slug, UpdatePlanRequest{Goal: &newGoal})
	if err != nil {
		t.Fatalf("UpdatePlan (goal) failed: %v", err)
	}
	if updated.Goal != newGoal {
		t.Errorf("Goal = %q, want %q", updated.Goal, newGoal)
	}
	if updated.Title != wantTitle {
		t.Error("Title should remain unchanged")
	}
}

// assertUpdatePlanContext updates only the context field and verifies the result.
func assertUpdatePlanContext(t *testing.T, ctx context.Context, m *Manager, slug, newContext string) {
	t.Helper()
	updated, err := m.UpdatePlan(ctx, slug, UpdatePlanRequest{Context: &newContext})
	if err != nil {
		t.Fatalf("UpdatePlan (context) failed: %v", err)
	}
	if updated.Context != newContext {
		t.Errorf("Context = %q, want %q", updated.Context, newContext)
	}
}

// assertPlanFieldsPersisted loads the plan from disk and verifies the given field values.
func assertPlanFieldsPersisted(t *testing.T, ctx context.Context, m *Manager, slug, wantTitle, wantGoal, wantContext string) {
	t.Helper()
	loaded, err := m.LoadPlan(ctx, slug)
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}
	if loaded.Title != wantTitle {
		t.Errorf("Persisted title = %q, want %q", loaded.Title, wantTitle)
	}
	if loaded.Goal != wantGoal {
		t.Errorf("Persisted goal = %q, want %q", loaded.Goal, wantGoal)
	}
	if loaded.Context != wantContext {
		t.Errorf("Persisted context = %q, want %q", loaded.Context, wantContext)
	}
}

// TestManager_UpdatePlan_NotFound tests updating non-existent plan.
func TestManager_UpdatePlan_NotFound(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	title := "New Title"
	req := UpdatePlanRequest{Title: &title}
	_, err := m.UpdatePlan(ctx, "nonexistent", req)
	if !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("expected ErrPlanNotFound, got %v", err)
	}
}

// TestManager_UpdatePlan_StateGuards tests state guards for updating plans.
func TestManager_UpdatePlan_StateGuards(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	tests := []struct {
		name    string
		status  Status
		wantErr error
	}{
		{"created-allowed", StatusCreated, nil},
		{"drafted-allowed", StatusDrafted, nil},
		{"reviewed-allowed", StatusReviewed, nil},
		{"approved-allowed", StatusApproved, nil},
		{"tasks-generated-allowed", StatusTasksGenerated, nil},
		{"tasks-approved-allowed", StatusTasksApproved, nil},
		{"implementing-blocked", StatusImplementing, ErrPlanNotUpdatable},
		{"complete-blocked", StatusComplete, ErrPlanNotUpdatable},
		{"archived-blocked", StatusArchived, ErrPlanNotUpdatable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create plan with specific status
			slug := "plan-" + strings.ReplaceAll(tt.name, "_", "-")
			plan, err := m.CreatePlan(ctx, slug, "Test Plan")
			if err != nil {
				t.Fatalf("CreatePlan failed: %v", err)
			}

			plan.Status = tt.status
			err = m.SavePlan(ctx, plan)
			if err != nil {
				t.Fatalf("SavePlan failed: %v", err)
			}

			// Try to update
			newTitle := "Updated Title"
			req := UpdatePlanRequest{Title: &newTitle}
			_, err = m.UpdatePlan(ctx, plan.Slug, req)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestManager_UpdatePlan_BlockedByTasks tests update blocked by task state.
func TestManager_UpdatePlan_BlockedByTasks(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Test 1: Plan with only pending tasks should be updatable
	plan1, err := m.CreatePlan(ctx, "task-pending-ok", "Task Pending OK")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	req1 := CreateTaskRequest{Description: "Pending task", Type: TaskTypeImplement}
	_, _ = m.CreateTaskManual(ctx, plan1.Slug, req1)

	newTitle := "Updated Title"
	updateReq := UpdatePlanRequest{Title: &newTitle}
	_, err = m.UpdatePlan(ctx, plan1.Slug, updateReq)
	if err != nil {
		t.Errorf("should allow update with pending task, got: %v", err)
	}

	// Test 2: Plan with in_progress task should be blocked
	plan2, err := m.CreatePlan(ctx, "task-inprogress-blocked", "Task InProgress Blocked")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	req2 := CreateTaskRequest{Description: "In progress task", Type: TaskTypeImplement}
	task2, _ := m.CreateTaskManual(ctx, plan2.Slug, req2)
	m.UpdateTaskStatus(ctx, plan2.Slug, task2.ID, TaskStatusInProgress)

	updateReq = UpdatePlanRequest{Title: &newTitle}
	_, err = m.UpdatePlan(ctx, plan2.Slug, updateReq)
	if !errors.Is(err, ErrPlanNotUpdatable) {
		t.Errorf("expected ErrPlanNotUpdatable with in_progress task, got: %v", err)
	}

	// Test 3: Plan with completed task should be blocked
	plan3, err := m.CreatePlan(ctx, "task-completed-blocked", "Task Completed Blocked")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	req3 := CreateTaskRequest{Description: "Completed task", Type: TaskTypeImplement}
	task3, _ := m.CreateTaskManual(ctx, plan3.Slug, req3)
	m.UpdateTaskStatus(ctx, plan3.Slug, task3.ID, TaskStatusInProgress)
	m.UpdateTaskStatus(ctx, plan3.Slug, task3.ID, TaskStatusCompleted)

	updateReq = UpdatePlanRequest{Title: &newTitle}
	_, err = m.UpdatePlan(ctx, plan3.Slug, updateReq)
	if !errors.Is(err, ErrPlanNotUpdatable) {
		t.Errorf("expected ErrPlanNotUpdatable with completed task, got: %v", err)
	}

	// Test 4: Plan with failed task should be blocked
	plan4, err := m.CreatePlan(ctx, "task-failed-blocked", "Task Failed Blocked")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	req4 := CreateTaskRequest{Description: "Failed task", Type: TaskTypeImplement}
	task4, _ := m.CreateTaskManual(ctx, plan4.Slug, req4)
	m.UpdateTaskStatus(ctx, plan4.Slug, task4.ID, TaskStatusInProgress)
	m.UpdateTaskStatus(ctx, plan4.Slug, task4.ID, TaskStatusFailed)

	updateReq = UpdatePlanRequest{Title: &newTitle}
	_, err = m.UpdatePlan(ctx, plan4.Slug, updateReq)
	if !errors.Is(err, ErrPlanNotUpdatable) {
		t.Errorf("expected ErrPlanNotUpdatable with failed task, got: %v", err)
	}
}

// TestManager_DeletePlan tests deleting a plan.
func TestManager_DeletePlan(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Create plan
	plan, err := m.CreatePlan(ctx, "test-delete", "Test Delete")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	// Verify plan exists
	planPath := filepath.Join(tmpDir, ".semspec", "projects", "default", "plans", plan.Slug)
	if _, err := os.Stat(planPath); os.IsNotExist(err) {
		t.Fatal("plan directory should exist")
	}

	// Delete plan
	err = m.DeletePlan(ctx, plan.Slug)
	if err != nil {
		t.Fatalf("DeletePlan failed: %v", err)
	}

	// Verify plan directory removed
	if _, err := os.Stat(planPath); !os.IsNotExist(err) {
		t.Error("plan directory should be removed")
	}

	// Verify LoadPlan returns not found
	_, err = m.LoadPlan(ctx, plan.Slug)
	if !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("expected ErrPlanNotFound after delete, got: %v", err)
	}
}

// TestManager_DeletePlan_NotFound tests deleting non-existent plan.
func TestManager_DeletePlan_NotFound(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	err := m.DeletePlan(ctx, "nonexistent")
	if !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("expected ErrPlanNotFound, got %v", err)
	}
}

// TestManager_DeletePlan_StateGuards tests state guards for deleting plans.
func TestManager_DeletePlan_StateGuards(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	tests := []struct {
		name    string
		status  Status
		wantErr error
	}{
		{"created-allowed", StatusCreated, nil},
		{"drafted-allowed", StatusDrafted, nil},
		{"reviewed-allowed", StatusReviewed, nil},
		{"approved-allowed", StatusApproved, nil},
		{"tasks-generated-allowed", StatusTasksGenerated, nil},
		{"tasks-approved-allowed", StatusTasksApproved, nil},
		{"implementing-blocked", StatusImplementing, ErrPlanNotDeletable},
		{"complete-blocked", StatusComplete, ErrPlanNotDeletable},
		{"archived-blocked", StatusArchived, ErrPlanNotDeletable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create plan with specific status
			slug := "delplan-" + strings.ReplaceAll(tt.name, "_", "-")
			plan, err := m.CreatePlan(ctx, slug, "Test Plan")
			if err != nil {
				t.Fatalf("CreatePlan failed: %v", err)
			}

			plan.Status = tt.status
			err = m.SavePlan(ctx, plan)
			if err != nil {
				t.Fatalf("SavePlan failed: %v", err)
			}

			// Try to delete
			err = m.DeletePlan(ctx, plan.Slug)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
				// Verify plan still exists when delete should fail
				if _, statErr := os.Stat(m.ProjectPlanPath("default", plan.Slug)); os.IsNotExist(statErr) {
					t.Error("plan should still exist after failed delete")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestManager_DeletePlan_BlockedByTasks tests delete blocked by task state.
func TestManager_DeletePlan_BlockedByTasks(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, err := m.CreatePlan(ctx, "delete-task-blocked", "Delete Task Blocked")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	// Create in_progress task
	req := CreateTaskRequest{Description: "In progress", Type: TaskTypeImplement}
	task, _ := m.CreateTaskManual(ctx, plan.Slug, req)
	m.UpdateTaskStatus(ctx, plan.Slug, task.ID, TaskStatusInProgress)

	// Delete should be blocked
	err = m.DeletePlan(ctx, plan.Slug)
	if !errors.Is(err, ErrPlanNotDeletable) {
		t.Errorf("expected ErrPlanNotDeletable with in_progress task, got: %v", err)
	}

	// Verify plan still exists
	planPath := m.ProjectPlanPath("default", plan.Slug)
	if _, err := os.Stat(planPath); os.IsNotExist(err) {
		t.Error("plan should still exist after failed delete")
	}
}

// TestManager_ArchivePlan_StatusUpdate tests archiving a plan via status update.
func TestManager_ArchivePlan_StatusUpdate(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Create plan
	plan, err := m.CreatePlan(ctx, "test-archive", "Test Archive")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	// Archive plan
	err = m.ArchivePlan(ctx, plan.Slug)
	if err != nil {
		t.Fatalf("ArchivePlan failed: %v", err)
	}

	// Verify plan still exists but status is archived
	loaded, err := m.LoadPlan(ctx, plan.Slug)
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}

	if loaded.Status != StatusArchived {
		t.Errorf("Status = %q, want %q", loaded.Status, StatusArchived)
	}

	// Verify plan directory still exists
	planPath := m.ProjectPlanPath("default", plan.Slug)
	if _, err := os.Stat(planPath); os.IsNotExist(err) {
		t.Error("plan directory should still exist after archive")
	}
}

// TestManager_ArchivePlan_NotFound tests archiving non-existent plan.
func TestManager_ArchivePlan_NotFound(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	err := m.ArchivePlan(ctx, "nonexistent")
	if !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("expected ErrPlanNotFound, got %v", err)
	}
}
