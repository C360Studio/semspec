package taskdispatcher

import (
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestNewDependencyGraph_NoDependencies(t *testing.T) {
	tasks := []workflow.Task{
		{ID: "task.test.1", Description: "First task"},
		{ID: "task.test.2", Description: "Second task"},
		{ID: "task.test.3", Description: "Third task"},
	}

	graph, err := NewDependencyGraph(tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All tasks should be ready immediately
	ready := graph.GetReadyTasks()
	if len(ready) != 3 {
		t.Errorf("expected 3 ready tasks, got %d", len(ready))
	}

	if graph.RemainingCount() != 3 {
		t.Errorf("expected 3 remaining, got %d", graph.RemainingCount())
	}
}

func TestNewDependencyGraph_LinearDependencies(t *testing.T) {
	tasks := []workflow.Task{
		{ID: "task.test.1", Description: "First task", DependsOn: nil},
		{ID: "task.test.2", Description: "Second task", DependsOn: []string{"task.test.1"}},
		{ID: "task.test.3", Description: "Third task", DependsOn: []string{"task.test.2"}},
	}

	graph, err := NewDependencyGraph(tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only task 1 should be ready
	ready := graph.GetReadyTasks()
	if len(ready) != 1 {
		t.Fatalf("expected 1 ready task, got %d", len(ready))
	}
	if ready[0].ID != "task.test.1" {
		t.Errorf("expected task.test.1 to be ready, got %s", ready[0].ID)
	}

	// Complete task 1, task 2 should become ready
	newlyReady := graph.MarkCompleted("task.test.1")
	if len(newlyReady) != 1 {
		t.Fatalf("expected 1 newly ready task, got %d", len(newlyReady))
	}
	if newlyReady[0].ID != "task.test.2" {
		t.Errorf("expected task.test.2 to become ready, got %s", newlyReady[0].ID)
	}

	// Complete task 2, task 3 should become ready
	newlyReady = graph.MarkCompleted("task.test.2")
	if len(newlyReady) != 1 {
		t.Fatalf("expected 1 newly ready task, got %d", len(newlyReady))
	}
	if newlyReady[0].ID != "task.test.3" {
		t.Errorf("expected task.test.3 to become ready, got %s", newlyReady[0].ID)
	}

	// Complete task 3, graph should be empty
	graph.MarkCompleted("task.test.3")
	if !graph.IsEmpty() {
		t.Errorf("expected graph to be empty")
	}
}

func TestNewDependencyGraph_MultipleDependencies(t *testing.T) {
	// Task 3 depends on both task 1 and task 2
	tasks := []workflow.Task{
		{ID: "task.test.1", Description: "First task"},
		{ID: "task.test.2", Description: "Second task"},
		{ID: "task.test.3", Description: "Third task", DependsOn: []string{"task.test.1", "task.test.2"}},
	}

	graph, err := NewDependencyGraph(tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Tasks 1 and 2 should be ready
	ready := graph.GetReadyTasks()
	if len(ready) != 2 {
		t.Errorf("expected 2 ready tasks, got %d", len(ready))
	}

	// Complete task 1, task 3 should NOT be ready yet
	newlyReady := graph.MarkCompleted("task.test.1")
	if len(newlyReady) != 0 {
		t.Errorf("expected 0 newly ready tasks, got %d", len(newlyReady))
	}

	// Complete task 2, now task 3 should be ready
	newlyReady = graph.MarkCompleted("task.test.2")
	if len(newlyReady) != 1 {
		t.Fatalf("expected 1 newly ready task, got %d", len(newlyReady))
	}
	if newlyReady[0].ID != "task.test.3" {
		t.Errorf("expected task.test.3 to become ready, got %s", newlyReady[0].ID)
	}
}

func TestNewDependencyGraph_CircularDependency(t *testing.T) {
	tasks := []workflow.Task{
		{ID: "task.test.1", DependsOn: []string{"task.test.3"}},
		{ID: "task.test.2", DependsOn: []string{"task.test.1"}},
		{ID: "task.test.3", DependsOn: []string{"task.test.2"}},
	}

	_, err := NewDependencyGraph(tasks)
	if err == nil {
		t.Error("expected error for circular dependency")
	}
}

func TestNewDependencyGraph_NonExistentDependency(t *testing.T) {
	tasks := []workflow.Task{
		{ID: "task.test.1", DependsOn: []string{"task.test.nonexistent"}},
	}

	_, err := NewDependencyGraph(tasks)
	if err == nil {
		t.Error("expected error for non-existent dependency")
	}
}

func TestNewDependencyGraph_TopologicalOrder(t *testing.T) {
	tasks := []workflow.Task{
		{ID: "task.test.3", DependsOn: []string{"task.test.1", "task.test.2"}},
		{ID: "task.test.1"},
		{ID: "task.test.2", DependsOn: []string{"task.test.1"}},
	}

	graph, err := NewDependencyGraph(tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	order := graph.TopologicalOrder()
	if len(order) != 3 {
		t.Fatalf("expected 3 tasks in order, got %d", len(order))
	}

	// Task 1 must come before task 2 and task 3
	// Task 2 must come before task 3
	taskIndex := make(map[string]int)
	for i, task := range order {
		taskIndex[task.ID] = i
	}

	if taskIndex["task.test.1"] >= taskIndex["task.test.2"] {
		t.Errorf("task.test.1 should come before task.test.2")
	}
	if taskIndex["task.test.1"] >= taskIndex["task.test.3"] {
		t.Errorf("task.test.1 should come before task.test.3")
	}
	if taskIndex["task.test.2"] >= taskIndex["task.test.3"] {
		t.Errorf("task.test.2 should come before task.test.3")
	}
}

func TestDependencyGraph_ConcurrentAccess(t *testing.T) {
	tasks := []workflow.Task{
		{ID: "task.test.1"},
		{ID: "task.test.2"},
		{ID: "task.test.3", DependsOn: []string{"task.test.1", "task.test.2"}},
	}

	graph, err := NewDependencyGraph(tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Simulate concurrent access
	done := make(chan bool, 3)

	go func() {
		graph.GetReadyTasks()
		done <- true
	}()

	go func() {
		graph.MarkCompleted("task.test.1")
		done <- true
	}()

	go func() {
		graph.IsEmpty()
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		<-done
	}
}

func TestNewDependencyGraph_EmptyTaskList(t *testing.T) {
	tasks := []workflow.Task{}

	graph, err := NewDependencyGraph(tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !graph.IsEmpty() {
		t.Error("expected empty graph for empty task list")
	}

	ready := graph.GetReadyTasks()
	if len(ready) != 0 {
		t.Errorf("expected 0 ready tasks, got %d", len(ready))
	}

	if graph.RemainingCount() != 0 {
		t.Errorf("expected 0 remaining, got %d", graph.RemainingCount())
	}
}

func TestDependencyGraph_GetTask(t *testing.T) {
	tasks := []workflow.Task{
		{ID: "task.test.1", Description: "First task"},
		{ID: "task.test.2", Description: "Second task"},
	}

	graph, err := NewDependencyGraph(tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	task := graph.GetTask("task.test.1")
	if task == nil {
		t.Fatal("expected task to be found")
	}
	if task.Description != "First task" {
		t.Errorf("expected 'First task', got %s", task.Description)
	}

	// Non-existent task
	task = graph.GetTask("task.test.nonexistent")
	if task != nil {
		t.Error("expected nil for non-existent task")
	}
}

func TestDependencyGraph_GetAllTasks(t *testing.T) {
	tasks := []workflow.Task{
		{ID: "task.test.1"},
		{ID: "task.test.2"},
		{ID: "task.test.3"},
	}

	graph, err := NewDependencyGraph(tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	allTasks := graph.GetAllTasks()
	if len(allTasks) != 3 {
		t.Errorf("expected 3 tasks, got %d", len(allTasks))
	}

	// Verify all tasks are present
	taskIDs := make(map[string]bool)
	for _, task := range allTasks {
		taskIDs[task.ID] = true
	}

	for _, expected := range []string{"task.test.1", "task.test.2", "task.test.3"} {
		if !taskIDs[expected] {
			t.Errorf("missing task %s", expected)
		}
	}
}

func TestDependencyGraph_RepeatedMarkCompleted(t *testing.T) {
	tasks := []workflow.Task{
		{ID: "task.test.1"},
		{ID: "task.test.2", DependsOn: []string{"task.test.1"}},
	}

	graph, err := NewDependencyGraph(tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First completion
	newlyReady := graph.MarkCompleted("task.test.1")
	if len(newlyReady) != 1 {
		t.Errorf("expected 1 newly ready, got %d", len(newlyReady))
	}

	// Second completion of same task should be safe (no-op)
	newlyReady = graph.MarkCompleted("task.test.1")
	if len(newlyReady) != 0 {
		t.Errorf("expected 0 newly ready on repeat, got %d", len(newlyReady))
	}
}

func TestDependencyGraph_SelfDependency(t *testing.T) {
	tasks := []workflow.Task{
		{ID: "task.test.1", DependsOn: []string{"task.test.1"}},
	}

	_, err := NewDependencyGraph(tasks)
	if err == nil {
		t.Error("expected error for self-dependency (circular)")
	}
}
