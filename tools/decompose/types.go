// Package decompose implements the decompose_task tool executor.
// It is a passthrough tool — the LLM provides the DAG structure in its tool
// call arguments, the executor validates it and echoes it back. The parent
// agent then decides whether to spawn nodes individually or delegate to the
// DAG execution workflow.
package decompose

import (
	"fmt"
)

// TaskDAG represents a directed acyclic graph of subtasks.
type TaskDAG struct {
	Nodes []TaskNode `json:"nodes"`
}

// TaskNode represents a single subtask in a DAG.
type TaskNode struct {
	ID        string   `json:"id"`
	Prompt    string   `json:"prompt"`
	Role      string   `json:"role"`
	DependsOn []string `json:"depends_on"`
	FileScope []string `json:"file_scope"` // Files or globs this task may touch
}

// Validate checks the DAG for structural correctness.
// It verifies that:
//   - At least one node is present
//   - All node IDs are unique
//   - All DependsOn references point to existing node IDs
//   - No node references itself as a dependency
//   - The graph contains no cycles (via depth-first search)
//   - Each node declares at least one FileScope entry (non-empty, no "..", max 50)
//
// maxDAGNodes caps the number of nodes accepted in a single DAG to prevent
// resource exhaustion from malformed or adversarial LLM responses.
const maxDAGNodes = 100

// maxFileScopeEntries caps the number of file scope entries per node.
const maxFileScopeEntries = 50

// Validate checks the DAG for structural correctness: non-empty, no duplicates,
// valid dependency references, no cycles, and bounded file scope.
func (d *TaskDAG) Validate() error {
	if len(d.Nodes) == 0 {
		return fmt.Errorf("dag must contain at least one node")
	}
	if len(d.Nodes) > maxDAGNodes {
		return fmt.Errorf("dag exceeds maximum node count (%d > %d)", len(d.Nodes), maxDAGNodes)
	}

	// Build an index of node IDs for O(1) membership checks.
	nodeIndex := make(map[string]struct{}, len(d.Nodes))
	for _, n := range d.Nodes {
		if _, exists := nodeIndex[n.ID]; exists {
			return fmt.Errorf("duplicate node ID %q", n.ID)
		}
		nodeIndex[n.ID] = struct{}{}
	}

	// Validate dependency references and self-references.
	for _, n := range d.Nodes {
		for _, dep := range n.DependsOn {
			if dep == n.ID {
				return fmt.Errorf("node %q depends on itself", n.ID)
			}
			if _, exists := nodeIndex[dep]; !exists {
				return fmt.Errorf("node %q depends on unknown node %q", n.ID, dep)
			}
		}
	}

	// Build an adjacency list for cycle detection.
	adj := make(map[string][]string, len(d.Nodes))
	for _, n := range d.Nodes {
		adj[n.ID] = n.DependsOn
	}

	// Detect cycles via recursive DFS with three-color marking:
	//   white (0) = unvisited, gray (1) = in current path, black (2) = done.
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(d.Nodes))

	var visit func(id string) error
	visit = func(id string) error {
		color[id] = gray
		for _, dep := range adj[id] {
			switch color[dep] {
			case gray:
				return fmt.Errorf("cycle detected: node %q and node %q are in a cycle", id, dep)
			case white:
				if err := visit(dep); err != nil {
					return err
				}
			}
			// black: already fully explored, no cycle through this path
		}
		color[id] = black
		return nil
	}

	for _, n := range d.Nodes {
		if color[n.ID] == white {
			if err := visit(n.ID); err != nil {
				return err
			}
		}
	}

	// Validate FileScope for each node: must be non-empty, no path traversal, max 50 entries.
	for _, n := range d.Nodes {
		if len(n.FileScope) == 0 {
			return fmt.Errorf("node %q: file_scope must contain at least one entry", n.ID)
		}
		if len(n.FileScope) > maxFileScopeEntries {
			return fmt.Errorf("node %q: file_scope exceeds maximum entry count (%d > %d)", n.ID, len(n.FileScope), maxFileScopeEntries)
		}
		for i, entry := range n.FileScope {
			if entry == "" {
				return fmt.Errorf("node %q: file_scope[%d] must not be empty", n.ID, i)
			}
			if containsPathTraversal(entry) {
				return fmt.Errorf("node %q: file_scope[%d] %q contains path traversal", n.ID, i, entry)
			}
		}
	}

	return nil
}

// containsPathTraversal returns true if the given path entry contains ".."
// as a path component, indicating an attempt to escape the repository root.
func containsPathTraversal(entry string) bool {
	for _, part := range splitPathComponents(entry) {
		if part == ".." {
			return true
		}
	}
	return false
}

// splitPathComponents splits a path by both "/" and "\" to handle cross-platform
// glob patterns and detect ".." components regardless of separator used.
func splitPathComponents(path string) []string {
	parts := make([]string, 0)
	current := make([]byte, 0, len(path))
	for i := 0; i < len(path); i++ {
		if path[i] == '/' || path[i] == '\\' {
			if len(current) > 0 {
				parts = append(parts, string(current))
				current = current[:0]
			}
		} else {
			current = append(current, path[i])
		}
	}
	if len(current) > 0 {
		parts = append(parts, string(current))
	}
	return parts
}
