package workflow

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"
)

// Integration tests exercise full workflows without external dependencies.
// They verify that Plan and Task operations work correctly together.
// Run with: go test -race ./workflow/... to verify no data races.

// largeTaskCount is used for tests that verify behavior with many tasks.
const largeTaskCount = 50

// setupPlanWithGoalContext is a test helper that creates a plan with goal and context.
func setupPlanWithGoalContext(t *testing.T, m *Manager, slug, title, goal, ctx string) *Plan {
	t.Helper()
	bgCtx := context.Background()
	plan, err := m.CreatePlan(bgCtx, slug, title)
	if err != nil {
		t.Fatalf("CreatePlan(%q) failed: %v", slug, err)
	}
	plan.Goal = goal
	plan.Context = ctx
	if err := m.SavePlan(bgCtx, plan); err != nil {
		t.Fatalf("SavePlan(%q) failed: %v", slug, err)
	}
	return plan
}

// setupPlanWithTasks is a test helper that creates a plan and manually creates tasks.
func setupPlanWithTasks(t *testing.T, m *Manager, slug, title string, taskDescriptions []string) (*Plan, []Task) {
	t.Helper()
	ctx := context.Background()
	plan := setupPlanWithGoalContext(t, m, slug, title, "Test goal", "Test context")

	var tasks []Task
	for i, desc := range taskDescriptions {
		task, err := CreateTask(plan.ID, plan.Slug, i+1, desc)
		if err != nil {
			t.Fatalf("CreateTask failed: %v", err)
		}
		tasks = append(tasks, *task)
	}

	if err := m.SaveTasks(ctx, tasks, plan.Slug); err != nil {
		t.Fatalf("SaveTasks(%q) failed: %v", slug, err)
	}
	return plan, tasks
}

// TestIntegration_PlanToTaskWorkflow tests the complete flow:
// CreatePlan → Set Goal/Context/Scope → Create tasks manually → Verify tasks.
func TestIntegration_PlanToTaskWorkflow(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// 1. Create plan
	plan, err := m.CreatePlan(ctx, "auth-feature", "Add authentication")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	// 2. Populate plan fields
	plan.Goal = "Implement JWT-based authentication for all /api routes"
	plan.Context = "API lacks authentication, all endpoints are public"
	plan.Scope = Scope{
		Include:    []string{"api/", "internal/auth/"},
		Exclude:    []string{"vendor/", "docs/"},
		DoNotTouch: []string{"config.yaml", ".env"},
	}

	if err := m.SavePlan(ctx, plan); err != nil {
		t.Fatalf("SavePlan failed: %v", err)
	}

	// 3. Create tasks manually (in production, task-generator component does this via LLM)
	var tasks []Task
	taskDescriptions := []string{
		"Add auth middleware to intercept requests",
		"Create login endpoint at /api/auth/login",
		"Create token refresh endpoint at /api/auth/refresh",
		"Add user session management",
		"Write integration tests for auth flow",
	}
	for i, desc := range taskDescriptions {
		task, err := CreateTask(plan.ID, plan.Slug, i+1, desc)
		if err != nil {
			t.Fatalf("CreateTask failed: %v", err)
		}
		tasks = append(tasks, *task)
	}

	if err := m.SaveTasks(ctx, tasks, plan.Slug); err != nil {
		t.Fatalf("SaveTasks failed: %v", err)
	}

	// 4. Verify task count
	if len(tasks) != 5 {
		t.Fatalf("expected 5 tasks, got %d", len(tasks))
	}

	// 5. Verify task structure
	for i, task := range tasks {
		expectedID := "task.auth-feature." + itoa(i+1)
		if task.ID != expectedID {
			t.Errorf("task[%d].ID = %q, want %q", i, task.ID, expectedID)
		}
		if task.PlanID != plan.ID {
			t.Errorf("task[%d].PlanID = %q, want %q", i, task.PlanID, plan.ID)
		}
		if task.Sequence != i+1 {
			t.Errorf("task[%d].Sequence = %d, want %d", i, task.Sequence, i+1)
		}
		if task.Status != TaskStatusPending {
			t.Errorf("task[%d].Status = %q, want pending", i, task.Status)
		}
	}

	// 6. Verify tasks are persisted
	loaded, err := m.LoadTasks(ctx, "auth-feature")
	if err != nil {
		t.Fatalf("LoadTasks failed: %v", err)
	}
	if len(loaded) != 5 {
		t.Fatalf("loaded %d tasks, want 5", len(loaded))
	}

	// 7. Verify plan can be loaded with all fields
	loadedPlan, err := m.LoadPlan(ctx, "auth-feature")
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}
	if loadedPlan.Goal != plan.Goal {
		t.Errorf("Goal not persisted correctly")
	}
	if loadedPlan.Context != plan.Context {
		t.Errorf("Context not persisted correctly")
	}
	if len(loadedPlan.Scope.Include) != 2 {
		t.Errorf("Scope.Include not persisted correctly, got %d", len(loadedPlan.Scope.Include))
	}
}

// TestIntegration_TaskExecutionWorkflow tests the full task execution lifecycle:
// Create tasks → Update all to in_progress → Complete → Verify.
func TestIntegration_TaskExecutionWorkflow(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Setup plan with tasks
	taskDescs := []string{"First step", "Second step", "Third step"}
	_, _ = setupPlanWithTasks(t, m, "exec-workflow", "Execution Workflow Test", taskDescs)

	// Execute task lifecycle for each task
	tasks, _ := m.LoadTasks(ctx, "exec-workflow")
	for _, task := range tasks {
		// Start task
		if err := m.UpdateTaskStatus(ctx, "exec-workflow", task.ID, TaskStatusInProgress); err != nil {
			t.Fatalf("failed to start task %s: %v", task.ID, err)
		}

		// Complete task
		if err := m.UpdateTaskStatus(ctx, "exec-workflow", task.ID, TaskStatusCompleted); err != nil {
			t.Fatalf("failed to complete task %s: %v", task.ID, err)
		}
	}

	// Verify all completed
	finalTasks, _ := m.LoadTasks(ctx, "exec-workflow")
	for _, task := range finalTasks {
		if task.Status != TaskStatusCompleted {
			t.Errorf("task %s status = %q, want completed", task.ID, task.Status)
		}
		if task.CompletedAt == nil {
			t.Errorf("task %s CompletedAt should be set", task.ID)
		}
	}
}

// TestIntegration_MultiPlanIsolation tests that multiple plans with same task
// descriptions don't interfere with each other.
func TestIntegration_MultiPlanIsolation(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Create two plans with identical task descriptions
	taskDescs := []string{"Setup database", "Create API endpoint", "Write tests"}

	_, _ = setupPlanWithTasks(t, m, "feature-a", "Feature A", taskDescs)
	_, _ = setupPlanWithTasks(t, m, "feature-b", "Feature B", taskDescs)

	// Modify tasks in plan1
	m.UpdateTaskStatus(ctx, "feature-a", "task.feature-a.1", TaskStatusInProgress)
	m.UpdateTaskStatus(ctx, "feature-a", "task.feature-a.1", TaskStatusCompleted)

	// Verify plan2 tasks are unaffected
	tasks2, _ := m.LoadTasks(ctx, "feature-b")
	for _, task := range tasks2 {
		if task.Status != TaskStatusPending {
			t.Errorf("feature-b task %s was modified: %q", task.ID, task.Status)
		}
	}

	// Verify plan1 task was actually modified
	tasks1, _ := m.LoadTasks(ctx, "feature-a")
	if tasks1[0].Status != TaskStatusCompleted {
		t.Errorf("feature-a task[0] should be completed: %q", tasks1[0].Status)
	}
	// Other tasks in plan1 should still be pending
	if tasks1[1].Status != TaskStatusPending {
		t.Errorf("feature-a task[1] should be pending: %q", tasks1[1].Status)
	}
}

// TestIntegration_PlanPromotion tests the full promotion workflow:
// Create → Populate Goal/Context → Promote → Create Tasks.
func TestIntegration_PlanPromotion(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Create exploration
	plan, _ := m.CreatePlan(ctx, "promote-test", "Promotion Test")

	if plan.Committed {
		t.Error("new plan should start uncommitted")
	}

	// Populate fields
	plan.Goal = "Determine best approach for the feature"
	plan.Context = "Exploring options for implementation"
	plan.Scope = Scope{
		Include: []string{"src/", "lib/"},
	}
	m.SavePlan(ctx, plan)

	// Promote to committed
	if err := m.PromotePlan(ctx, plan); err != nil {
		t.Fatalf("PromotePlan failed: %v", err)
	}

	// Verify promotion
	if !plan.Committed {
		t.Error("plan should be committed after promotion")
	}
	if plan.CommittedAt == nil {
		t.Error("CommittedAt should be set after promotion")
	}

	// Create tasks for committed plan (in production, task-generator does this)
	taskDescs := []string{"Research options", "Prototype solution", "Review with team"}
	var tasks []Task
	for i, desc := range taskDescs {
		task, _ := CreateTask(plan.ID, plan.Slug, i+1, desc)
		tasks = append(tasks, *task)
	}
	m.SaveTasks(ctx, tasks, plan.Slug)

	// Verify tasks were created
	loaded, _ := m.LoadTasks(ctx, "promote-test")
	if len(loaded) != 3 {
		t.Errorf("expected 3 tasks, got %d", len(loaded))
	}

	// Verify persistence of committed status
	loadedPlan, _ := m.LoadPlan(ctx, "promote-test")
	if !loadedPlan.Committed {
		t.Error("committed status not persisted")
	}
}

// TestIntegration_ListWithMixedStates tests listing plans in various states.
func TestIntegration_ListWithMixedStates(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Create uncommitted plan
	plan1, _ := m.CreatePlan(ctx, "uncommitted", "Uncommitted Plan")
	plan1.Goal = "Just exploring"
	m.SavePlan(ctx, plan1)

	// Create and commit plan
	plan2, _ := m.CreatePlan(ctx, "committed", "Committed Plan")
	plan2.Goal = "Ready to execute"
	plan2.Context = "Implementation ready"
	m.SavePlan(ctx, plan2)
	m.PromotePlan(ctx, plan2)

	// Create tasks for committed plan
	task1, _ := CreateTask(plan2.ID, plan2.Slug, 1, "Do the thing")
	m.SaveTasks(ctx, []Task{*task1}, plan2.Slug)

	// Create plan with partial task execution
	plan3, _ := m.CreatePlan(ctx, "partial", "Partial Execution")
	plan3.Goal = "Multi-step feature"
	m.SavePlan(ctx, plan3)
	m.PromotePlan(ctx, plan3)

	// Create multiple tasks
	var partialTasks []Task
	for i, desc := range []string{"Step one", "Step two", "Step three"} {
		task, _ := CreateTask(plan3.ID, plan3.Slug, i+1, desc)
		partialTasks = append(partialTasks, *task)
	}
	m.SaveTasks(ctx, partialTasks, plan3.Slug)

	// Complete first task
	m.UpdateTaskStatus(ctx, "partial", "task.partial.1", TaskStatusInProgress)
	m.UpdateTaskStatus(ctx, "partial", "task.partial.1", TaskStatusCompleted)

	// List all plans
	result, err := m.ListPlans(ctx)
	if err != nil {
		t.Fatalf("ListPlans failed: %v", err)
	}

	if len(result.Plans) != 3 {
		t.Fatalf("expected 3 plans, got %d", len(result.Plans))
	}

	// Verify states
	planMap := make(map[string]*Plan)
	for _, p := range result.Plans {
		planMap[p.Slug] = p
	}

	if planMap["uncommitted"].Committed {
		t.Error("uncommitted plan should not be committed")
	}
	if !planMap["committed"].Committed {
		t.Error("committed plan should be committed")
	}
	if !planMap["partial"].Committed {
		t.Error("partial plan should be committed")
	}

	// Verify task states for partial plan
	tasks, _ := m.LoadTasks(ctx, "partial")
	if tasks[0].Status != TaskStatusCompleted {
		t.Errorf("partial task[0] = %q, want completed", tasks[0].Status)
	}
	if tasks[1].Status != TaskStatusPending {
		t.Errorf("partial task[1] = %q, want pending", tasks[1].Status)
	}
}

// TestIntegration_ConcurrentPlanOperations tests concurrent operations on different plans.
// Run with: go test -race ./workflow/... to verify no data races.
func TestIntegration_ConcurrentPlanOperations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Create multiple plans with tasks
	slugs := []string{"concurrent-a", "concurrent-b", "concurrent-c"}
	taskDescs := []string{"Step one", "Step two"}
	for _, slug := range slugs {
		setupPlanWithTasks(t, m, slug, "Concurrent "+slug, taskDescs)
	}

	// Concurrently update tasks across all plans
	var wg sync.WaitGroup
	errCh := make(chan error, len(slugs)*2)

	for _, slug := range slugs {
		wg.Add(1)
		go func(s string) {
			defer wg.Done()
			// Start task 1
			if err := m.UpdateTaskStatus(ctx, s, "task."+s+".1", TaskStatusInProgress); err != nil {
				errCh <- err
				return
			}
			// Complete task 1
			if err := m.UpdateTaskStatus(ctx, s, "task."+s+".1", TaskStatusCompleted); err != nil {
				errCh <- err
			}
		}(slug)
	}

	wg.Wait()
	close(errCh)

	// Check for errors
	for err := range errCh {
		t.Errorf("concurrent operation failed: %v", err)
	}

	// Verify all plans are correctly updated
	for _, slug := range slugs {
		tasks, _ := m.LoadTasks(ctx, slug)
		if tasks[0].Status != TaskStatusCompleted {
			t.Errorf("%s task[0] = %q, want completed", slug, tasks[0].Status)
		}
		if tasks[1].Status != TaskStatusPending {
			t.Errorf("%s task[1] = %q, want pending", slug, tasks[1].Status)
		}
	}
}

// TestIntegration_TaskReplacement tests that saving new tasks replaces existing ones.
func TestIntegration_TaskReplacement(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, _ := m.CreatePlan(ctx, "regen", "Task Replacement Test")
	plan.Goal = "Test task replacement"
	m.SavePlan(ctx, plan)

	// Create initial tasks
	var tasks1 []Task
	for i, desc := range []string{"Original task one", "Original task two"} {
		task, _ := CreateTask(plan.ID, plan.Slug, i+1, desc)
		tasks1 = append(tasks1, *task)
	}
	m.SaveTasks(ctx, tasks1, plan.Slug)

	loaded1, _ := m.LoadTasks(ctx, "regen")
	if len(loaded1) != 2 {
		t.Fatalf("initial: expected 2 tasks, got %d", len(loaded1))
	}

	// Save new tasks (replaces old ones)
	var tasks2 []Task
	for i, desc := range []string{"New task one", "New task two", "New task three"} {
		task, _ := CreateTask(plan.ID, plan.Slug, i+1, desc)
		tasks2 = append(tasks2, *task)
	}
	m.SaveTasks(ctx, tasks2, plan.Slug)

	// Verify loaded tasks match new set
	loaded, _ := m.LoadTasks(ctx, "regen")
	if len(loaded) != 3 {
		t.Fatalf("replaced: expected 3 tasks, got %d", len(loaded))
	}
	if loaded[0].Description != "New task one" {
		t.Errorf("loaded[0].Description = %q", loaded[0].Description)
	}
	if loaded[2].Description != "New task three" {
		t.Errorf("loaded[2].Description = %q", loaded[2].Description)
	}
}

// TestIntegration_EmptyTaskList tests saving and loading an empty task list.
func TestIntegration_EmptyTaskList(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, _ := m.CreatePlan(ctx, "empty-tasks", "Empty Tasks")
	plan.Goal = "Plan with no tasks"
	m.SavePlan(ctx, plan)

	// Save empty task list
	if err := m.SaveTasks(ctx, []Task{}, plan.Slug); err != nil {
		t.Fatalf("SaveTasks failed: %v", err)
	}

	// Verify tasks file is empty array
	loaded, _ := m.LoadTasks(ctx, "empty-tasks")
	if len(loaded) != 0 {
		t.Errorf("loaded %d tasks, want 0", len(loaded))
	}
}

// TestIntegration_GetTaskFromLargeList tests GetTask performance with many tasks.
func TestIntegration_GetTaskFromLargeList(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Create plan
	plan, _ := m.CreatePlan(ctx, "large-list", "Large Task List")
	plan.Goal = "Test with many tasks"
	m.SavePlan(ctx, plan)

	// Create largeTaskCount tasks
	var tasks []Task
	for i := 1; i <= largeTaskCount; i++ {
		task, _ := CreateTask(plan.ID, plan.Slug, i, "Task number "+itoa(i))
		tasks = append(tasks, *task)
	}
	m.SaveTasks(ctx, tasks, plan.Slug)

	// Get a task from the middle
	middleIdx := largeTaskCount / 2
	task, err := m.GetTask(ctx, "large-list", "task.large-list."+itoa(middleIdx))
	if err != nil {
		t.Fatalf("GetTask middle failed: %v", err)
	}
	if task.Sequence != middleIdx {
		t.Errorf("Sequence = %d, want %d", task.Sequence, middleIdx)
	}
	if task.Description != "Task number "+itoa(middleIdx) {
		t.Errorf("Description = %q, want %q", task.Description, "Task number "+itoa(middleIdx))
	}

	// Get the last task
	task, err = m.GetTask(ctx, "large-list", "task.large-list."+itoa(largeTaskCount))
	if err != nil {
		t.Fatalf("GetTask last failed: %v", err)
	}
	if task.Sequence != largeTaskCount {
		t.Errorf("last Sequence = %d, want %d", task.Sequence, largeTaskCount)
	}
}

// TestIntegration_FilesystemStructure verifies the expected filesystem layout.
func TestIntegration_FilesystemStructure(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Create plan with tasks
	plan, _ := m.CreatePlan(ctx, "fs-test", "Filesystem Test")
	plan.Goal = "Test filesystem layout"
	m.SavePlan(ctx, plan)

	// Create and save tasks
	task, _ := CreateTask(plan.ID, plan.Slug, 1, "First task")
	m.SaveTasks(ctx, []Task{*task}, plan.Slug)

	// Verify expected paths exist
	expectedFiles := []string{
		".semspec/changes/fs-test/plan.json",
		".semspec/changes/fs-test/tasks.json",
	}

	for _, relPath := range expectedFiles {
		fullPath := tmpDir + "/" + relPath
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			t.Errorf("expected file not found: %s", relPath)
		}
	}
}
