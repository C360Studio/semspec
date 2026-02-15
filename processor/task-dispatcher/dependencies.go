package taskdispatcher

import (
	"fmt"
	"sync"

	"github.com/c360studio/semspec/workflow"
)

// DependencyGraph manages task dependencies and determines execution order.
// All methods are safe for concurrent use.
type DependencyGraph struct {
	mu         sync.Mutex
	tasks      map[string]*workflow.Task
	inDegree   map[string]int      // Number of unmet dependencies
	dependents map[string][]string // Tasks that depend on this task
}

// NewDependencyGraph creates a dependency graph from a list of tasks.
func NewDependencyGraph(tasks []workflow.Task) (*DependencyGraph, error) {
	g := &DependencyGraph{
		tasks:      make(map[string]*workflow.Task),
		inDegree:   make(map[string]int),
		dependents: make(map[string][]string),
	}

	// Index tasks by ID
	for i := range tasks {
		t := &tasks[i]
		g.tasks[t.ID] = t
		g.inDegree[t.ID] = 0
		g.dependents[t.ID] = nil
	}

	// Build dependency relationships
	for _, t := range tasks {
		for _, depID := range t.DependsOn {
			// Validate that the dependency exists
			if _, exists := g.tasks[depID]; !exists {
				return nil, fmt.Errorf("task %s depends on non-existent task %s", t.ID, depID)
			}
			g.inDegree[t.ID]++
			g.dependents[depID] = append(g.dependents[depID], t.ID)
		}
	}

	// Detect cycles using topological sort
	if err := g.detectCycles(); err != nil {
		return nil, err
	}

	return g, nil
}

// detectCycles uses Kahn's algorithm to detect cycles in the dependency graph.
func (g *DependencyGraph) detectCycles() error {
	// Copy inDegree for cycle detection
	tempDegree := make(map[string]int)
	for id, deg := range g.inDegree {
		tempDegree[id] = deg
	}

	// Find all tasks with no dependencies
	var queue []string
	for id, deg := range tempDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	processed := 0
	for len(queue) > 0 {
		taskID := queue[0]
		queue = queue[1:]
		processed++

		// Reduce in-degree of dependents
		for _, depID := range g.dependents[taskID] {
			tempDegree[depID]--
			if tempDegree[depID] == 0 {
				queue = append(queue, depID)
			}
		}
	}

	if processed != len(g.tasks) {
		return fmt.Errorf("circular dependency detected: %d tasks could not be ordered", len(g.tasks)-processed)
	}

	return nil
}

// GetReadyTasks returns all tasks that have no unmet dependencies.
func (g *DependencyGraph) GetReadyTasks() []*workflow.Task {
	g.mu.Lock()
	defer g.mu.Unlock()

	var ready []*workflow.Task
	for id, deg := range g.inDegree {
		if deg == 0 {
			ready = append(ready, g.tasks[id])
		}
	}
	return ready
}

// MarkCompleted marks a task as completed and updates dependencies.
// Returns newly unblocked tasks.
func (g *DependencyGraph) MarkCompleted(taskID string) []*workflow.Task {
	g.mu.Lock()
	defer g.mu.Unlock()

	var newlyReady []*workflow.Task

	// Update all tasks that depend on this one
	for _, depID := range g.dependents[taskID] {
		g.inDegree[depID]--
		if g.inDegree[depID] == 0 {
			newlyReady = append(newlyReady, g.tasks[depID])
		}
	}

	// Remove from graph
	delete(g.inDegree, taskID)

	return newlyReady
}

// IsEmpty returns true if all tasks have been processed.
func (g *DependencyGraph) IsEmpty() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.inDegree) == 0
}

// RemainingCount returns the number of tasks still pending.
func (g *DependencyGraph) RemainingCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.inDegree)
}

// GetTask returns a task by ID.
func (g *DependencyGraph) GetTask(id string) *workflow.Task {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.tasks[id]
}

// GetAllTasks returns all tasks in the graph.
func (g *DependencyGraph) GetAllTasks() []*workflow.Task {
	g.mu.Lock()
	defer g.mu.Unlock()

	tasks := make([]*workflow.Task, 0, len(g.tasks))
	for _, t := range g.tasks {
		tasks = append(tasks, t)
	}
	return tasks
}

// TopologicalOrder returns tasks in topological order (dependencies first).
// Note: This method acquires the mutex and makes a snapshot of the graph state.
// It should only be called for informational purposes, not during concurrent dispatch.
func (g *DependencyGraph) TopologicalOrder() []*workflow.Task {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Copy inDegree for ordering
	tempDegree := make(map[string]int)
	for id, deg := range g.inDegree {
		tempDegree[id] = deg
	}

	// Copy dependents for ordering
	tempDependents := make(map[string][]string)
	for id, deps := range g.dependents {
		tempDependents[id] = append([]string(nil), deps...)
	}

	var order []*workflow.Task
	var queue []string

	// Find all tasks with no dependencies
	for id, deg := range tempDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	for len(queue) > 0 {
		taskID := queue[0]
		queue = queue[1:]
		order = append(order, g.tasks[taskID])

		// Reduce in-degree of dependents
		for _, depID := range tempDependents[taskID] {
			tempDegree[depID]--
			if tempDegree[depID] == 0 {
				queue = append(queue, depID)
			}
		}
	}

	return order
}
