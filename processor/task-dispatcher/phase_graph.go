package taskdispatcher

import (
	"fmt"
	"sync"

	"github.com/c360studio/semspec/workflow"
)

// PhaseDependencyGraph manages phase dependencies and determines execution order.
// Follows the same pattern as DependencyGraph but operates on phases.
// All methods are safe for concurrent use.
type PhaseDependencyGraph struct {
	mu         sync.Mutex
	phases     map[string]*workflow.Phase
	inDegree   map[string]int      // Number of unmet dependencies
	dependents map[string][]string // Phases that depend on this phase
}

// NewPhaseDependencyGraph creates a dependency graph from a list of phases.
func NewPhaseDependencyGraph(phases []workflow.Phase) (*PhaseDependencyGraph, error) {
	g := &PhaseDependencyGraph{
		phases:     make(map[string]*workflow.Phase),
		inDegree:   make(map[string]int),
		dependents: make(map[string][]string),
	}

	// Index phases by ID
	for i := range phases {
		p := &phases[i]
		g.phases[p.ID] = p
		g.inDegree[p.ID] = 0
		g.dependents[p.ID] = nil
	}

	// Build dependency relationships
	for _, p := range phases {
		for _, depID := range p.DependsOn {
			if _, exists := g.phases[depID]; !exists {
				return nil, fmt.Errorf("phase %s depends on non-existent phase %s", p.ID, depID)
			}
			g.inDegree[p.ID]++
			g.dependents[depID] = append(g.dependents[depID], p.ID)
		}
	}

	// Detect cycles
	if err := g.detectCycles(); err != nil {
		return nil, err
	}

	return g, nil
}

// detectCycles uses Kahn's algorithm to detect cycles.
func (g *PhaseDependencyGraph) detectCycles() error {
	tempDegree := make(map[string]int)
	for id, deg := range g.inDegree {
		tempDegree[id] = deg
	}

	var queue []string
	for id, deg := range tempDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	processed := 0
	for len(queue) > 0 {
		phaseID := queue[0]
		queue = queue[1:]
		processed++

		for _, depID := range g.dependents[phaseID] {
			tempDegree[depID]--
			if tempDegree[depID] == 0 {
				queue = append(queue, depID)
			}
		}
	}

	if processed != len(g.phases) {
		return fmt.Errorf("circular phase dependency detected: %d phases could not be ordered", len(g.phases)-processed)
	}

	return nil
}

// GetReadyPhases returns all phases that have no unmet dependencies.
func (g *PhaseDependencyGraph) GetReadyPhases() []*workflow.Phase {
	g.mu.Lock()
	defer g.mu.Unlock()

	var ready []*workflow.Phase
	for id, deg := range g.inDegree {
		if deg == 0 {
			ready = append(ready, g.phases[id])
		}
	}
	return ready
}

// MarkCompleted marks a phase as completed and updates dependencies.
// Returns newly unblocked phases.
func (g *PhaseDependencyGraph) MarkCompleted(phaseID string) []*workflow.Phase {
	g.mu.Lock()
	defer g.mu.Unlock()

	var newlyReady []*workflow.Phase

	for _, depID := range g.dependents[phaseID] {
		g.inDegree[depID]--
		if g.inDegree[depID] == 0 {
			newlyReady = append(newlyReady, g.phases[depID])
		}
	}

	delete(g.inDegree, phaseID)

	return newlyReady
}

// IsEmpty returns true if all phases have been processed.
func (g *PhaseDependencyGraph) IsEmpty() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.inDegree) == 0
}

// RemainingCount returns the number of phases still pending.
func (g *PhaseDependencyGraph) RemainingCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.inDegree)
}
