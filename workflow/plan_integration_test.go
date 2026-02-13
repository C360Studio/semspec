package workflow

import (
	"context"
	"os"
	"sync"
	"testing"
)

// Integration tests exercise full workflows without external dependencies.
// They verify that Plan and Task operations work correctly together.

// TestIntegration_PlanToTaskWorkflow tests the complete flow:
// CreatePlan → Set Execution → GenerateTasksFromPlan → Verify tasks.
func TestIntegration_PlanToTaskWorkflow(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// 1. Create plan
	plan, err := m.CreatePlan(ctx, "auth-feature", "Add authentication")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	// 2. Populate SMEAC fields
	plan.Situation = "API lacks authentication, all endpoints are public"
	plan.Mission = "Implement JWT-based authentication for all /api routes"
	plan.Execution = `1. Add auth middleware to intercept requests
2. Create login endpoint at /api/auth/login
3. Create token refresh endpoint at /api/auth/refresh
4. Add user session management
5. Write integration tests for auth flow`
	plan.Constraints.In = []string{"api/", "internal/auth/"}
	plan.Constraints.Out = []string{"vendor/", "docs/"}
	plan.Constraints.DoNotTouch = []string{"config.yaml", ".env"}
	plan.Coordination = "Sync with frontend team for token format"

	if err := m.SavePlan(ctx, plan); err != nil {
		t.Fatalf("SavePlan failed: %v", err)
	}

	// 3. Generate tasks from execution
	tasks, err := m.GenerateTasksFromPlan(ctx, plan)
	if err != nil {
		t.Fatalf("GenerateTasksFromPlan failed: %v", err)
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
	if loadedPlan.Mission != plan.Mission {
		t.Errorf("Mission not persisted correctly")
	}
	if len(loadedPlan.Constraints.In) != 2 {
		t.Errorf("Constraints.In not persisted correctly")
	}
}

// TestIntegration_TaskExecutionWorkflow tests the full task execution lifecycle:
// Create tasks → Update all to in_progress → Complete → Verify.
func TestIntegration_TaskExecutionWorkflow(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Setup
	plan, _ := m.CreatePlan(ctx, "exec-workflow", "Execution Workflow Test")
	plan.Execution = `1. First step
2. Second step
3. Third step`
	m.SavePlan(ctx, plan)
	m.GenerateTasksFromPlan(ctx, plan)

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

	// Create two plans with identical execution steps
	execution := `1. Setup database
2. Create API endpoint
3. Write tests`

	plan1, _ := m.CreatePlan(ctx, "feature-a", "Feature A")
	plan1.Execution = execution
	m.SavePlan(ctx, plan1)
	m.GenerateTasksFromPlan(ctx, plan1)

	plan2, _ := m.CreatePlan(ctx, "feature-b", "Feature B")
	plan2.Execution = execution
	m.SavePlan(ctx, plan2)
	m.GenerateTasksFromPlan(ctx, plan2)

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
// Create → Populate SMEAC → Promote → GenerateTasks.
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
	plan.Situation = "Exploring options"
	plan.Mission = "Determine best approach"
	plan.Execution = `1. Research options
2. Prototype solution
3. Review with team`
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

	// Generate tasks from committed plan
	tasks, err := m.GenerateTasksFromPlan(ctx, plan)
	if err != nil {
		t.Fatalf("GenerateTasksFromPlan failed: %v", err)
	}
	if len(tasks) != 3 {
		t.Errorf("expected 3 tasks, got %d", len(tasks))
	}

	// Verify persistence of committed status
	loaded, _ := m.LoadPlan(ctx, "promote-test")
	if !loaded.Committed {
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
	plan1.Situation = "Just exploring"
	m.SavePlan(ctx, plan1)

	// Create and commit plan
	plan2, _ := m.CreatePlan(ctx, "committed", "Committed Plan")
	plan2.Situation = "Ready to execute"
	plan2.Execution = "1. Do the thing"
	m.SavePlan(ctx, plan2)
	m.PromotePlan(ctx, plan2)
	m.GenerateTasksFromPlan(ctx, plan2)

	// Create plan with partial task execution
	plan3, _ := m.CreatePlan(ctx, "partial", "Partial Execution")
	plan3.Execution = "1. Step one\n2. Step two\n3. Step three"
	m.SavePlan(ctx, plan3)
	m.PromotePlan(ctx, plan3)
	m.GenerateTasksFromPlan(ctx, plan3)
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
func TestIntegration_ConcurrentPlanOperations(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Create multiple plans
	slugs := []string{"concurrent-a", "concurrent-b", "concurrent-c"}
	for _, slug := range slugs {
		plan, _ := m.CreatePlan(ctx, slug, "Concurrent "+slug)
		plan.Execution = "1. Step one\n2. Step two"
		m.SavePlan(ctx, plan)
		m.GenerateTasksFromPlan(ctx, plan)
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

// TestIntegration_TaskRegeneration tests that regenerating tasks replaces existing ones.
func TestIntegration_TaskRegeneration(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, _ := m.CreatePlan(ctx, "regen", "Regeneration Test")
	plan.Execution = "1. Original task one\n2. Original task two"
	m.SavePlan(ctx, plan)

	// Generate initial tasks
	tasks1, _ := m.GenerateTasksFromPlan(ctx, plan)
	if len(tasks1) != 2 {
		t.Fatalf("initial: expected 2 tasks, got %d", len(tasks1))
	}

	// Modify execution
	plan.Execution = "1. New task one\n2. New task two\n3. New task three"
	m.SavePlan(ctx, plan)

	// Regenerate tasks
	tasks2, _ := m.GenerateTasksFromPlan(ctx, plan)
	if len(tasks2) != 3 {
		t.Fatalf("regenerated: expected 3 tasks, got %d", len(tasks2))
	}

	// Verify loaded tasks match regenerated
	loaded, _ := m.LoadTasks(ctx, "regen")
	if len(loaded) != 3 {
		t.Fatalf("loaded: expected 3 tasks, got %d", len(loaded))
	}
	if loaded[0].Description != "New task one" {
		t.Errorf("loaded[0].Description = %q", loaded[0].Description)
	}
	if loaded[2].Description != "New task three" {
		t.Errorf("loaded[2].Description = %q", loaded[2].Description)
	}
}

// TestIntegration_EmptyExecutionNoTasks tests that empty execution produces no tasks.
func TestIntegration_EmptyExecutionNoTasks(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, _ := m.CreatePlan(ctx, "empty-exec", "Empty Execution")
	// Execution is empty by default

	tasks, err := m.GenerateTasksFromPlan(ctx, plan)
	if err != nil {
		t.Fatalf("GenerateTasksFromPlan failed: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}

	// Verify tasks file is empty array
	loaded, _ := m.LoadTasks(ctx, "empty-exec")
	if len(loaded) != 0 {
		t.Errorf("loaded %d tasks, want 0", len(loaded))
	}
}

// TestIntegration_PlanWithOnlyNonNumberedContent tests execution with no numbered items.
func TestIntegration_PlanWithOnlyNonNumberedContent(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, _ := m.CreatePlan(ctx, "no-numbers", "No Numbered Items")
	plan.Execution = `This is just prose describing what to do.
- Bullet point one
- Bullet point two
* Another format
No actual numbered tasks here.`
	m.SavePlan(ctx, plan)

	tasks, err := m.GenerateTasksFromPlan(ctx, plan)
	if err != nil {
		t.Fatalf("GenerateTasksFromPlan failed: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks from non-numbered content, got %d", len(tasks))
	}
}

// TestIntegration_GetTaskFromLargeList tests GetTask performance with many tasks.
func TestIntegration_GetTaskFromLargeList(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	plan, _ := m.CreatePlan(ctx, "large-list", "Large Task List")

	// Create 50 numbered items
	var lines []string
	for i := 1; i <= 50; i++ {
		lines = append(lines, itoa(i)+". Task number "+itoa(i))
	}
	plan.Execution = ""
	for _, line := range lines {
		plan.Execution += line + "\n"
	}
	m.SavePlan(ctx, plan)
	m.GenerateTasksFromPlan(ctx, plan)

	// Get a task from the middle
	task, err := m.GetTask(ctx, "large-list", "task.large-list.25")
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if task.Sequence != 25 {
		t.Errorf("Sequence = %d, want 25", task.Sequence)
	}
	if task.Description != "Task number 25" {
		t.Errorf("Description = %q", task.Description)
	}

	// Get the last task
	task, err = m.GetTask(ctx, "large-list", "task.large-list.50")
	if err != nil {
		t.Fatalf("GetTask last failed: %v", err)
	}
	if task.Sequence != 50 {
		t.Errorf("last Sequence = %d, want 50", task.Sequence)
	}
}

// TestIntegration_FilesystemStructure verifies the expected filesystem layout.
func TestIntegration_FilesystemStructure(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Create plan with tasks
	plan, _ := m.CreatePlan(ctx, "fs-test", "Filesystem Test")
	plan.Execution = "1. First task"
	m.SavePlan(ctx, plan)
	m.GenerateTasksFromPlan(ctx, plan)

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
