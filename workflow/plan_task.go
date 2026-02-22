package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TasksJSONFile is the filename for machine-readable task storage (JSON format).
// This is the primary storage format used by the workflow system.
// Note: TasksFile ("tasks.md") in structure.go is for human-readable display.
const TasksJSONFile = "tasks.json"

// Sentinel errors for task operations.
var (
	ErrTaskNotFound      = errors.New("task not found")
	ErrInvalidTransition = errors.New("invalid status transition")
	ErrDescriptionRequired = errors.New("description is required")
)

// taskLocks provides per-slug mutex for safe concurrent task updates.
// This prevents race conditions when multiple goroutines update tasks
// for the same slug simultaneously.
var (
	taskLocksMu sync.Mutex
	taskLocks   = make(map[string]*sync.Mutex)
)

// getTaskLock returns a mutex for the given slug, creating one if needed.
func getTaskLock(slug string) *sync.Mutex {
	taskLocksMu.Lock()
	defer taskLocksMu.Unlock()

	if taskLocks[slug] == nil {
		taskLocks[slug] = &sync.Mutex{}
	}
	return taskLocks[slug]
}

// CreateTask creates a new Task with the given parameters.
func CreateTask(planID, planSlug string, seq int, description string) (*Task, error) {
	if err := ValidateSlug(planSlug); err != nil {
		return nil, err
	}

	return &Task{
		ID:                 TaskEntityID(planSlug, seq),
		PlanID:             planID,
		Sequence:           seq,
		Description:        description,
		Type:               TaskTypeImplement, // Default type
		AcceptanceCriteria: []AcceptanceCriterion{},
		Status:             TaskStatusPending,
		CreatedAt:          time.Now(),
	}, nil
}

// SaveTasks saves tasks to .semspec/projects/default/plans/{slug}/tasks.json.
func (m *Manager) SaveTasks(ctx context.Context, tasks []Task, slug string) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return err
	}

	tasksPath := filepath.Join(m.ProjectPlanPath(DefaultProjectSlug, slug), TasksJSONFile)

	// Ensure directory exists
	dir := filepath.Dir(tasksPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal tasks: %w", err)
	}

	if err := os.WriteFile(tasksPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write tasks: %w", err)
	}

	return nil
}

// LoadTasks loads tasks from .semspec/projects/default/plans/{slug}/tasks.json.
func (m *Manager) LoadTasks(ctx context.Context, slug string) ([]Task, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	tasksPath := filepath.Join(m.ProjectPlanPath(DefaultProjectSlug, slug), TasksJSONFile)

	data, err := os.ReadFile(tasksPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []Task{}, nil
		}
		return nil, fmt.Errorf("failed to read tasks: %w", err)
	}

	var tasks []Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, fmt.Errorf("failed to parse tasks: %w", err)
	}

	return tasks, nil
}

// UpdateTaskStatus updates the status of a task by ID.
// This operation is thread-safe and uses per-slug locking to prevent
// race conditions when multiple goroutines update tasks concurrently.
func (m *Manager) UpdateTaskStatus(ctx context.Context, slug, taskID string, status TaskStatus) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}

	if !status.IsValid() {
		return fmt.Errorf("%w: invalid status %q", ErrInvalidTransition, status)
	}

	// Check context cancellation before acquiring lock
	if err := ctx.Err(); err != nil {
		return err
	}

	// Acquire per-slug lock to prevent race conditions
	lock := getTaskLock(slug)
	lock.Lock()
	defer lock.Unlock()

	// Check context cancellation after acquiring lock
	if err := ctx.Err(); err != nil {
		return err
	}

	tasks, err := m.LoadTasks(ctx, slug)
	if err != nil {
		return err
	}

	found := false
	now := time.Now()

	for i := range tasks {
		if tasks[i].ID == taskID {
			if !tasks[i].Status.CanTransitionTo(status) {
				return fmt.Errorf("%w: cannot transition from %s to %s",
					ErrInvalidTransition, tasks[i].Status, status)
			}
			tasks[i].Status = status
			if status == TaskStatusInProgress && tasks[i].StartedAt == nil {
				tasks[i].StartedAt = &now
			}
			if status == TaskStatusCompleted || status == TaskStatusFailed {
				tasks[i].CompletedAt = &now
			}
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}

	return m.SaveTasks(ctx, tasks, slug)
}

// GetTask retrieves a single task by ID.
func (m *Manager) GetTask(ctx context.Context, slug, taskID string) (*Task, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	tasks, err := m.LoadTasks(ctx, slug)
	if err != nil {
		return nil, err
	}

	for i := range tasks {
		if tasks[i].ID == taskID {
			return &tasks[i], nil
		}
	}

	return nil, fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
}

// Sentinel errors for task approval operations.
var (
	ErrTaskNotPendingApproval = errors.New("task is not pending approval")
	ErrRejectionReasonRequired = errors.New("rejection reason is required")
)

// SubmitTaskForApproval transitions a task from pending to pending_approval status.
// This marks the task as ready for human review.
func (m *Manager) SubmitTaskForApproval(ctx context.Context, slug, taskID string) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	lock := getTaskLock(slug)
	lock.Lock()
	defer lock.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	tasks, err := m.LoadTasks(ctx, slug)
	if err != nil {
		return err
	}

	found := false
	for i := range tasks {
		if tasks[i].ID == taskID {
			if !tasks[i].Status.CanTransitionTo(TaskStatusPendingApproval) {
				return fmt.Errorf("%w: cannot submit task from status %s",
					ErrInvalidTransition, tasks[i].Status)
			}
			tasks[i].Status = TaskStatusPendingApproval
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}

	return m.SaveTasks(ctx, tasks, slug)
}

// SubmitAllTasksForApproval transitions all pending tasks to pending_approval status.
// Returns the number of tasks submitted.
func (m *Manager) SubmitAllTasksForApproval(ctx context.Context, slug string) (int, error) {
	if err := ValidateSlug(slug); err != nil {
		return 0, err
	}

	if err := ctx.Err(); err != nil {
		return 0, err
	}

	lock := getTaskLock(slug)
	lock.Lock()
	defer lock.Unlock()

	if err := ctx.Err(); err != nil {
		return 0, err
	}

	tasks, err := m.LoadTasks(ctx, slug)
	if err != nil {
		return 0, err
	}

	count := 0
	for i := range tasks {
		if tasks[i].Status == TaskStatusPending {
			tasks[i].Status = TaskStatusPendingApproval
			count++
		}
	}

	if count > 0 {
		if err := m.SaveTasks(ctx, tasks, slug); err != nil {
			return 0, err
		}
	}

	return count, nil
}

// ApproveTask approves an individual task for execution.
// The task must be in pending_approval status.
// approvedBy identifies who approved the task (user ID, "system", etc.)
func (m *Manager) ApproveTask(ctx context.Context, slug, taskID, approvedBy string) (*Task, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	lock := getTaskLock(slug)
	lock.Lock()
	defer lock.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	tasks, err := m.LoadTasks(ctx, slug)
	if err != nil {
		return nil, err
	}

	var approvedTask *Task
	now := time.Now()

	for i := range tasks {
		if tasks[i].ID == taskID {
			if tasks[i].Status != TaskStatusPendingApproval {
				return nil, fmt.Errorf("%w: task status is %s",
					ErrTaskNotPendingApproval, tasks[i].Status)
			}
			tasks[i].Status = TaskStatusApproved
			tasks[i].ApprovedBy = approvedBy
			tasks[i].ApprovedAt = &now
			// Clear any previous rejection reason
			tasks[i].RejectionReason = ""
			approvedTask = &tasks[i]
			break
		}
	}

	if approvedTask == nil {
		return nil, fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}

	if err := m.SaveTasks(ctx, tasks, slug); err != nil {
		return nil, err
	}

	return approvedTask, nil
}

// RejectTask rejects an individual task with a reason.
// The task must be in pending_approval status.
// The reason is required and explains why the task was rejected.
func (m *Manager) RejectTask(ctx context.Context, slug, taskID, reason string) (*Task, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	if reason == "" {
		return nil, ErrRejectionReasonRequired
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	lock := getTaskLock(slug)
	lock.Lock()
	defer lock.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	tasks, err := m.LoadTasks(ctx, slug)
	if err != nil {
		return nil, err
	}

	var rejectedTask *Task

	for i := range tasks {
		if tasks[i].ID == taskID {
			if tasks[i].Status != TaskStatusPendingApproval {
				return nil, fmt.Errorf("%w: task status is %s",
					ErrTaskNotPendingApproval, tasks[i].Status)
			}
			tasks[i].Status = TaskStatusRejected
			tasks[i].RejectionReason = reason
			// Clear approval fields
			tasks[i].ApprovedBy = ""
			tasks[i].ApprovedAt = nil
			rejectedTask = &tasks[i]
			break
		}
	}

	if rejectedTask == nil {
		return nil, fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}

	if err := m.SaveTasks(ctx, tasks, slug); err != nil {
		return nil, err
	}

	return rejectedTask, nil
}

// ApproveAllTasks approves all tasks in pending_approval status.
// Returns the list of approved tasks.
func (m *Manager) ApproveAllTasks(ctx context.Context, slug, approvedBy string) ([]Task, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	lock := getTaskLock(slug)
	lock.Lock()
	defer lock.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	tasks, err := m.LoadTasks(ctx, slug)
	if err != nil {
		return nil, err
	}

	var approved []Task
	now := time.Now()

	for i := range tasks {
		if tasks[i].Status == TaskStatusPendingApproval {
			tasks[i].Status = TaskStatusApproved
			tasks[i].ApprovedBy = approvedBy
			tasks[i].ApprovedAt = &now
			tasks[i].RejectionReason = ""
			approved = append(approved, tasks[i])
		}
	}

	if len(approved) > 0 {
		if err := m.SaveTasks(ctx, tasks, slug); err != nil {
			return nil, err
		}
	}

	return approved, nil
}

// ResubmitTask resubmits a rejected task for approval after editing.
// The task must be in rejected status.
func (m *Manager) ResubmitTask(ctx context.Context, slug, taskID string) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	lock := getTaskLock(slug)
	lock.Lock()
	defer lock.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	tasks, err := m.LoadTasks(ctx, slug)
	if err != nil {
		return err
	}

	found := false
	for i := range tasks {
		if tasks[i].ID == taskID {
			if tasks[i].Status != TaskStatusRejected {
				return fmt.Errorf("%w: cannot resubmit task from status %s",
					ErrInvalidTransition, tasks[i].Status)
			}
			// Transition rejected → pending, then pending → pending_approval
			tasks[i].Status = TaskStatusPendingApproval
			// Keep rejection reason for reference but clear approval fields
			tasks[i].ApprovedBy = ""
			tasks[i].ApprovedAt = nil
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}

	return m.SaveTasks(ctx, tasks, slug)
}

// CreateTaskRequest contains parameters for creating a task manually.
type CreateTaskRequest struct {
	Description        string
	Type               TaskType
	AcceptanceCriteria []AcceptanceCriterion
	Files              []string
	DependsOn          []string
}

// UpdateTaskRequest contains parameters for updating a task.
// All fields are optional - only non-nil fields will be updated.
type UpdateTaskRequest struct {
	Description        *string
	Type               *TaskType
	AcceptanceCriteria []AcceptanceCriterion
	Files              []string
	DependsOn          []string
	Sequence           *int
}

// CreateTaskManual creates a task manually (not via LLM).
// The task is created in pending status and sequence is auto-incremented.
func (m *Manager) CreateTaskManual(ctx context.Context, slug string, req CreateTaskRequest) (*Task, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	if req.Description == "" {
		return nil, ErrDescriptionRequired
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Verify plan exists
	plan, err := m.LoadPlan(ctx, slug)
	if err != nil {
		return nil, err
	}

	// Acquire lock for thread safety
	lock := getTaskLock(slug)
	lock.Lock()
	defer lock.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Load existing tasks to determine next sequence
	tasks, err := m.LoadTasks(ctx, slug)
	if err != nil {
		return nil, err
	}

	// Calculate next sequence number
	nextSeq := len(tasks) + 1

	// Create the task
	task := &Task{
		ID:                 TaskEntityID(slug, nextSeq),
		PlanID:             plan.ID,
		Sequence:           nextSeq,
		Description:        req.Description,
		Type:               req.Type,
		AcceptanceCriteria: req.AcceptanceCriteria,
		Files:              req.Files,
		DependsOn:          req.DependsOn,
		Status:             TaskStatusPending,
		CreatedAt:          time.Now(),
	}

	// Default type if not specified
	if task.Type == "" {
		task.Type = TaskTypeImplement
	}

	// Initialize empty slices if nil
	if task.AcceptanceCriteria == nil {
		task.AcceptanceCriteria = []AcceptanceCriterion{}
	}
	if task.Files == nil {
		task.Files = []string{}
	}
	if task.DependsOn == nil {
		task.DependsOn = []string{}
	}

	// Append to tasks and save
	tasks = append(tasks, *task)
	if err := m.SaveTasks(ctx, tasks, slug); err != nil {
		return nil, err
	}

	return task, nil
}

// UpdateTask updates an existing task.
// Returns error if task is in_progress, completed, or failed (state guard).
func (m *Manager) UpdateTask(ctx context.Context, slug, taskID string, req UpdateTaskRequest) (*Task, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Acquire lock for thread safety
	lock := getTaskLock(slug)
	lock.Lock()
	defer lock.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	tasks, err := m.LoadTasks(ctx, slug)
	if err != nil {
		return nil, err
	}

	// Find the task
	var taskIndex = -1
	for i := range tasks {
		if tasks[i].ID == taskID {
			taskIndex = i
			break
		}
	}

	if taskIndex == -1 {
		return nil, fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}

	task := &tasks[taskIndex]

	// State guard: cannot update task if status is in_progress, completed, or failed
	if task.Status == TaskStatusInProgress || task.Status == TaskStatusCompleted || task.Status == TaskStatusFailed {
		return nil, fmt.Errorf("%w: cannot update task with status %s", ErrInvalidTransition, task.Status)
	}

	// Apply updates
	if req.Description != nil {
		task.Description = *req.Description
	}
	if req.Type != nil {
		task.Type = *req.Type
	}
	if req.AcceptanceCriteria != nil {
		task.AcceptanceCriteria = req.AcceptanceCriteria
	}
	if req.Files != nil {
		task.Files = req.Files
	}
	if req.DependsOn != nil {
		task.DependsOn = req.DependsOn
	}

	// Handle sequence reordering
	if req.Sequence != nil {
		newSeq := *req.Sequence
		oldSeq := task.Sequence

		if newSeq != oldSeq && newSeq >= 1 && newSeq <= len(tasks) {
			// Reorder tasks
			tasks = reorderTasks(tasks, oldSeq, newSeq)
		}
	}

	// Save updated tasks
	if err := m.SaveTasks(ctx, tasks, slug); err != nil {
		return nil, err
	}

	// Return the updated task
	for i := range tasks {
		if tasks[i].ID == taskID {
			return &tasks[i], nil
		}
	}

	return task, nil
}

// DeleteTask deletes a task.
// Returns error if task is in_progress, completed, or failed (state guard).
// Renumbers remaining task sequences and removes deleted task from other tasks' DependsOn.
func (m *Manager) DeleteTask(ctx context.Context, slug, taskID string) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	// Acquire lock for thread safety
	lock := getTaskLock(slug)
	lock.Lock()
	defer lock.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	tasks, err := m.LoadTasks(ctx, slug)
	if err != nil {
		return err
	}

	// Find the task to delete
	var deleteIndex = -1
	for i := range tasks {
		if tasks[i].ID == taskID {
			deleteIndex = i
			break
		}
	}

	if deleteIndex == -1 {
		return fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}

	task := &tasks[deleteIndex]

	// State guard: cannot delete task if status is in_progress, completed, or failed
	if task.Status == TaskStatusInProgress || task.Status == TaskStatusCompleted || task.Status == TaskStatusFailed {
		return fmt.Errorf("%w: cannot delete task with status %s", ErrInvalidTransition, task.Status)
	}

	// Remove the task
	tasks = append(tasks[:deleteIndex], tasks[deleteIndex+1:]...)

	// Renumber sequences (but keep original IDs for stability)
	for i := range tasks {
		tasks[i].Sequence = i + 1
	}

	// Remove deleted task from DependsOn lists
	for i := range tasks {
		newDeps := []string{}
		for _, depID := range tasks[i].DependsOn {
			if depID != taskID {
				newDeps = append(newDeps, depID)
			}
		}
		tasks[i].DependsOn = newDeps
	}

	// Save updated tasks
	return m.SaveTasks(ctx, tasks, slug)
}

// reorderTasks reorders tasks when a task's sequence changes.
// oldSeq and newSeq are 1-indexed positions.
func reorderTasks(tasks []Task, oldSeq, newSeq int) []Task {
	if oldSeq == newSeq || oldSeq < 1 || newSeq < 1 || oldSeq > len(tasks) || newSeq > len(tasks) {
		return tasks
	}

	// Convert to 0-indexed
	oldIdx := oldSeq - 1
	newIdx := newSeq - 1

	// Extract the task being moved
	moving := tasks[oldIdx]

	// Remove from old position
	tasks = append(tasks[:oldIdx], tasks[oldIdx+1:]...)

	// Insert at new position
	result := make([]Task, 0, len(tasks)+1)
	result = append(result, tasks[:newIdx]...)
	result = append(result, moving)
	result = append(result, tasks[newIdx:]...)

	// Renumber all sequences
	for i := range result {
		result[i].Sequence = i + 1
		// Keep original ID - sequences are just for ordering, ID doesn't change
	}

	return result
}
