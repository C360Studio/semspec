package cascade

import (
	"context"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func setupCascadeFixture(t *testing.T) (*workflow.Manager, string) {
	t.Helper()
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "cascade-test"

	if _, err := m.CreatePlan(ctx, slug, "Cascade Test Plan"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	// Seed requirements
	reqs := []workflow.Requirement{
		{ID: "req-1", PlanID: workflow.PlanEntityID(slug), Title: "Auth", Status: workflow.RequirementStatusActive},
		{ID: "req-2", PlanID: workflow.PlanEntityID(slug), Title: "Logging", Status: workflow.RequirementStatusActive},
	}
	if err := m.SaveRequirements(ctx, reqs, slug); err != nil {
		t.Fatalf("SaveRequirements: %v", err)
	}

	// Seed scenarios linked to requirements
	scenarios := []workflow.Scenario{
		{ID: "sc-1", RequirementID: "req-1", Given: "a user"},
		{ID: "sc-2", RequirementID: "req-1", Given: "a token"},
		{ID: "sc-3", RequirementID: "req-2", Given: "log files"},
	}
	if err := m.SaveScenarios(ctx, scenarios, slug); err != nil {
		t.Fatalf("SaveScenarios: %v", err)
	}

	// Seed tasks linked to scenarios
	tasks := []workflow.Task{
		{ID: "task-1", ScenarioIDs: []string{"sc-1"}, Status: workflow.TaskStatusPending},
		{ID: "task-2", ScenarioIDs: []string{"sc-1", "sc-2"}, Status: workflow.TaskStatusPending},
		{ID: "task-3", ScenarioIDs: []string{"sc-3"}, Status: workflow.TaskStatusPending},
		{ID: "task-4", ScenarioIDs: []string{"sc-2"}, Status: workflow.TaskStatusCompleted}, // terminal
		{ID: "task-5", ScenarioIDs: []string{"sc-1"}, Status: workflow.TaskStatusFailed},    // terminal
	}
	if err := m.SaveTasks(ctx, tasks, slug); err != nil {
		t.Fatalf("SaveTasks: %v", err)
	}

	return m, slug
}

func TestChangeProposal_NilProposal(t *testing.T) {
	m := workflow.NewManager(t.TempDir())
	_, err := ChangeProposal(context.Background(), m, "test", nil)
	if err == nil {
		t.Fatal("expected error for nil proposal")
	}
}

func TestChangeProposal_NoAffectedRequirements(t *testing.T) {
	m, slug := setupCascadeFixture(t)

	proposal := &workflow.ChangeProposal{
		ID:             "cp-1",
		AffectedReqIDs: []string{}, // empty
	}

	result, err := ChangeProposal(context.Background(), m, slug, proposal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TasksDirtied != 0 {
		t.Errorf("TasksDirtied = %d, want 0", result.TasksDirtied)
	}
}

func TestChangeProposal_AffectsOneRequirement(t *testing.T) {
	m, slug := setupCascadeFixture(t)

	proposal := &workflow.ChangeProposal{
		ID:             "cp-1",
		AffectedReqIDs: []string{"req-1"}, // affects sc-1, sc-2 → task-1, task-2 (not task-4/5 which are terminal)
	}

	result, err := ChangeProposal(context.Background(), m, slug, proposal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.AffectedRequirementIDs) != 1 {
		t.Errorf("AffectedRequirementIDs = %d, want 1", len(result.AffectedRequirementIDs))
	}
	if len(result.AffectedScenarioIDs) != 2 {
		t.Errorf("AffectedScenarioIDs = %d, want 2 (sc-1, sc-2)", len(result.AffectedScenarioIDs))
	}
	if result.TasksDirtied != 2 {
		t.Errorf("TasksDirtied = %d, want 2 (task-1, task-2)", result.TasksDirtied)
	}
	if len(result.AffectedTaskIDs) != 2 {
		t.Errorf("AffectedTaskIDs = %d, want 2", len(result.AffectedTaskIDs))
	}

	// Verify tasks are actually persisted as dirty
	tasks, err := m.LoadTasks(context.Background(), slug)
	if err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}
	for _, task := range tasks {
		switch task.ID {
		case "task-1", "task-2":
			if task.Status != workflow.TaskStatusDirty {
				t.Errorf("task %s status = %q, want dirty", task.ID, task.Status)
			}
		case "task-3":
			if task.Status != workflow.TaskStatusPending {
				t.Errorf("task-3 should remain pending, got %q", task.Status)
			}
		case "task-4":
			if task.Status != workflow.TaskStatusCompleted {
				t.Errorf("task-4 should remain completed, got %q", task.Status)
			}
		case "task-5":
			if task.Status != workflow.TaskStatusFailed {
				t.Errorf("task-5 should remain failed, got %q", task.Status)
			}
		}
	}
}

func TestChangeProposal_AffectsAllRequirements(t *testing.T) {
	m, slug := setupCascadeFixture(t)

	proposal := &workflow.ChangeProposal{
		ID:             "cp-1",
		AffectedReqIDs: []string{"req-1", "req-2"},
	}

	result, err := ChangeProposal(context.Background(), m, slug, proposal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.AffectedScenarioIDs) != 3 {
		t.Errorf("AffectedScenarioIDs = %d, want 3", len(result.AffectedScenarioIDs))
	}
	// task-1 (sc-1), task-2 (sc-1,sc-2), task-3 (sc-3) = 3 non-terminal
	if result.TasksDirtied != 3 {
		t.Errorf("TasksDirtied = %d, want 3", result.TasksDirtied)
	}
}

func TestChangeProposal_NoMatchingScenarios(t *testing.T) {
	m, slug := setupCascadeFixture(t)

	proposal := &workflow.ChangeProposal{
		ID:             "cp-1",
		AffectedReqIDs: []string{"req-nonexistent"},
	}

	result, err := ChangeProposal(context.Background(), m, slug, proposal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.AffectedScenarioIDs) != 0 {
		t.Errorf("AffectedScenarioIDs = %d, want 0", len(result.AffectedScenarioIDs))
	}
	if result.TasksDirtied != 0 {
		t.Errorf("TasksDirtied = %d, want 0", result.TasksDirtied)
	}
}

func TestChangeProposal_AlreadyDirtyTaskNotCounted(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "already-dirty"
	if _, err := m.CreatePlan(ctx, slug, "Already Dirty"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	reqs := []workflow.Requirement{
		{ID: "req-1", PlanID: workflow.PlanEntityID(slug), Title: "R1", Status: workflow.RequirementStatusActive},
	}
	if err := m.SaveRequirements(ctx, reqs, slug); err != nil {
		t.Fatalf("SaveRequirements: %v", err)
	}

	scenarios := []workflow.Scenario{
		{ID: "sc-1", RequirementID: "req-1", Given: "s1"},
	}
	if err := m.SaveScenarios(ctx, scenarios, slug); err != nil {
		t.Fatalf("SaveScenarios: %v", err)
	}

	// Task is already dirty
	tasks := []workflow.Task{
		{ID: "task-1", ScenarioIDs: []string{"sc-1"}, Status: workflow.TaskStatusDirty},
	}
	if err := m.SaveTasks(ctx, tasks, slug); err != nil {
		t.Fatalf("SaveTasks: %v", err)
	}

	proposal := &workflow.ChangeProposal{
		ID:             "cp-1",
		AffectedReqIDs: []string{"req-1"},
	}

	result, err := ChangeProposal(ctx, m, slug, proposal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Task is affected but already dirty — TasksDirtied should be 0
	if result.TasksDirtied != 0 {
		t.Errorf("TasksDirtied = %d, want 0 (already dirty)", result.TasksDirtied)
	}
	// But it should still be in AffectedTaskIDs
	if len(result.AffectedTaskIDs) != 1 {
		t.Errorf("AffectedTaskIDs = %d, want 1", len(result.AffectedTaskIDs))
	}
}

func TestChangeProposal_TerminalTasksSkipped(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "terminal-skip"
	if _, err := m.CreatePlan(ctx, slug, "Terminal Skip"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	reqs := []workflow.Requirement{
		{ID: "req-1", PlanID: workflow.PlanEntityID(slug), Title: "R1", Status: workflow.RequirementStatusActive},
	}
	if err := m.SaveRequirements(ctx, reqs, slug); err != nil {
		t.Fatalf("SaveRequirements: %v", err)
	}

	scenarios := []workflow.Scenario{
		{ID: "sc-1", RequirementID: "req-1", Given: "s1"},
	}
	if err := m.SaveScenarios(ctx, scenarios, slug); err != nil {
		t.Fatalf("SaveScenarios: %v", err)
	}

	// All tasks are terminal
	tasks := []workflow.Task{
		{ID: "task-1", ScenarioIDs: []string{"sc-1"}, Status: workflow.TaskStatusCompleted},
		{ID: "task-2", ScenarioIDs: []string{"sc-1"}, Status: workflow.TaskStatusFailed},
	}
	if err := m.SaveTasks(ctx, tasks, slug); err != nil {
		t.Fatalf("SaveTasks: %v", err)
	}

	proposal := &workflow.ChangeProposal{
		ID:             "cp-1",
		AffectedReqIDs: []string{"req-1"},
	}

	result, err := ChangeProposal(ctx, m, slug, proposal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.TasksDirtied != 0 {
		t.Errorf("TasksDirtied = %d, want 0 (all terminal)", result.TasksDirtied)
	}
	if len(result.AffectedTaskIDs) != 0 {
		t.Errorf("AffectedTaskIDs = %d, want 0 (terminal tasks not affected)", len(result.AffectedTaskIDs))
	}
}

func TestIsTerminalTaskStatus(t *testing.T) {
	tests := []struct {
		status   workflow.TaskStatus
		terminal bool
	}{
		{workflow.TaskStatusCompleted, true},
		{workflow.TaskStatusFailed, true},
		{workflow.TaskStatusPending, false},
		{workflow.TaskStatusDirty, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := isTerminalTaskStatus(tt.status); got != tt.terminal {
				t.Errorf("isTerminalTaskStatus(%q) = %v, want %v", tt.status, got, tt.terminal)
			}
		})
	}
}

func TestTaskOverlapsScenarios(t *testing.T) {
	affected := map[string]bool{"sc-1": true, "sc-3": true}

	tests := []struct {
		name     string
		task     workflow.Task
		overlaps bool
	}{
		{"single match", workflow.Task{ScenarioIDs: []string{"sc-1"}}, true},
		{"no match", workflow.Task{ScenarioIDs: []string{"sc-2"}}, false},
		{"partial match", workflow.Task{ScenarioIDs: []string{"sc-2", "sc-3"}}, true},
		{"empty scenarios", workflow.Task{ScenarioIDs: nil}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := taskOverlapsScenarios(&tt.task, affected); got != tt.overlaps {
				t.Errorf("taskOverlapsScenarios = %v, want %v", got, tt.overlaps)
			}
		})
	}
}
