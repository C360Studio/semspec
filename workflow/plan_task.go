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
	ErrTaskNotFound       = errors.New("task not found")
	ErrInvalidTransition  = errors.New("invalid task status transition")
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
		ID:                 fmt.Sprintf("task.%s.%d", planSlug, seq),
		PlanID:             planID,
		Sequence:           seq,
		Description:        description,
		Type:               TaskTypeImplement, // Default type
		AcceptanceCriteria: []AcceptanceCriterion{},
		Status:             TaskStatusPending,
		CreatedAt:          time.Now(),
	}, nil
}

// SaveTasks saves tasks to .semspec/changes/{slug}/tasks.json.
func (m *Manager) SaveTasks(ctx context.Context, tasks []Task, slug string) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return err
	}

	tasksPath := filepath.Join(m.ChangePath(slug), TasksJSONFile)

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

// LoadTasks loads tasks from .semspec/changes/{slug}/tasks.json.
func (m *Manager) LoadTasks(ctx context.Context, slug string) ([]Task, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	tasksPath := filepath.Join(m.ChangePath(slug), TasksJSONFile)

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

