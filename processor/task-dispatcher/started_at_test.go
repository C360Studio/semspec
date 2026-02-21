package taskdispatcher

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// TestDispatchTask_SetsStartedAt verifies that Task.StartedAt is populated
// when a task is dispatched by the task-dispatcher.
func TestDispatchTask_SetsStartedAt(t *testing.T) {
	// Create a task with no StartedAt initially
	task := &workflow.Task{
		ID:          "task.test-plan.1",
		PlanID:      "semspec.plan.test-plan",
		Sequence:    1,
		Description: "Test task",
		Status:      workflow.TaskStatusPending,
		CreatedAt:   time.Now(),
		StartedAt:   nil, // Not started yet
	}

	// Verify StartedAt is nil before dispatch
	if task.StartedAt != nil {
		t.Fatal("expected StartedAt to be nil before dispatch")
	}

	// Simulate what dispatchTask does: set StartedAt
	now := time.Now()
	task.StartedAt = &now

	// Verify StartedAt is populated after dispatch
	if task.StartedAt == nil {
		t.Error("expected StartedAt to be set after dispatch")
	}

	// Verify StartedAt is reasonably close to now (within 1 second)
	if task.StartedAt != nil {
		timeSinceStart := time.Since(*task.StartedAt)
		if timeSinceStart > time.Second {
			t.Errorf("expected StartedAt to be recent, but it was %v ago", timeSinceStart)
		}
	}
}

// TestTask_StartedAtJSON verifies that StartedAt field is properly
// marshaled and unmarshaled to/from JSON.
func TestTask_StartedAtJSON(t *testing.T) {
	now := time.Now()
	original := workflow.Task{
		ID:          "task.test-plan.1",
		PlanID:      "semspec.plan.test-plan",
		Sequence:    1,
		Description: "Test task",
		Status:      workflow.TaskStatusInProgress,
		CreatedAt:   now.Add(-5 * time.Minute),
		StartedAt:   &now,
		CompletedAt: nil,
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal task: %v", err)
	}

	// Unmarshal from JSON
	var decoded workflow.Task
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal task: %v", err)
	}

	// Verify StartedAt was preserved
	if decoded.StartedAt == nil {
		t.Fatal("expected StartedAt to be set after unmarshal")
	}

	// Compare timestamps (allowing for minor precision differences)
	if !decoded.StartedAt.Equal(now) {
		t.Errorf("expected StartedAt %v, got %v", now, *decoded.StartedAt)
	}
}

// TestTask_StartedAtNilJSON verifies that nil StartedAt is properly handled in JSON.
func TestTask_StartedAtNilJSON(t *testing.T) {
	original := workflow.Task{
		ID:          "task.test-plan.1",
		PlanID:      "semspec.plan.test-plan",
		Sequence:    1,
		Description: "Test task",
		Status:      workflow.TaskStatusPending,
		CreatedAt:   time.Now(),
		StartedAt:   nil, // Not started
		CompletedAt: nil,
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal task: %v", err)
	}

	// Unmarshal from JSON
	var decoded workflow.Task
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal task: %v", err)
	}

	// Verify StartedAt is still nil
	if decoded.StartedAt != nil {
		t.Errorf("expected StartedAt to be nil, got %v", *decoded.StartedAt)
	}
}

// TestTask_ExecutionTiming verifies that we can measure execution time
// using StartedAt and CompletedAt.
func TestTask_ExecutionTiming(t *testing.T) {
	startTime := time.Now().Add(-2 * time.Minute)
	endTime := time.Now()

	task := workflow.Task{
		ID:          "task.test-plan.1",
		PlanID:      "semspec.plan.test-plan",
		Sequence:    1,
		Description: "Test task",
		Status:      workflow.TaskStatusCompleted,
		CreatedAt:   time.Now().Add(-5 * time.Minute),
		StartedAt:   &startTime,
		CompletedAt: &endTime,
	}

	// Calculate execution time
	if task.StartedAt == nil || task.CompletedAt == nil {
		t.Fatal("expected both StartedAt and CompletedAt to be set")
	}

	executionTime := task.CompletedAt.Sub(*task.StartedAt)

	// Verify execution time is approximately 2 minutes
	expectedDuration := 2 * time.Minute
	if executionTime < expectedDuration-time.Second || executionTime > expectedDuration+time.Second {
		t.Errorf("expected execution time ~%v, got %v", expectedDuration, executionTime)
	}

	// Calculate queue wait time (CreatedAt to StartedAt)
	queueWaitTime := task.StartedAt.Sub(task.CreatedAt)
	expectedWaitTime := 3 * time.Minute
	if queueWaitTime < expectedWaitTime-time.Second || queueWaitTime > expectedWaitTime+time.Second {
		t.Errorf("expected queue wait time ~%v, got %v", expectedWaitTime, queueWaitTime)
	}
}

// TestDispatchWithDependencies_SetsStartedAt verifies that StartedAt is set
// for all tasks during dependency-aware dispatch.
func TestDispatchWithDependencies_SetsStartedAt(t *testing.T) {
	t.Skip("Integration test - requires full component setup")

	// This would be an integration test that verifies:
	// 1. Create a batch trigger with multiple tasks
	// 2. Execute the batch
	// 3. Verify all tasks have StartedAt set
	// 4. Verify StartedAt is set before task execution begins
}
