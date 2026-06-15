package requirementexecutor

import "fmt"

// TaskDAG represents a directed acyclic graph of subtasks.
//
// Originally lived in `tools/decompose` when the decomposer LLM tool authored
// the structure. ADR-043 PR 4f replaced the LLM path with synthesis from
// Sarah-prepared Stories; PR 4g relocated the types here since requirement-
// executor is now the sole consumer.
type TaskDAG struct {
	Nodes []TaskNode `json:"nodes"`
}

// TaskNode represents a single subtask in a DAG.
type TaskNode struct {
	ID          string   `json:"id"`
	Prompt      string   `json:"prompt"`
	Role        string   `json:"role"`
	DependsOn   []string `json:"depends_on"`
	FileScope   []string `json:"file_scope"`
	ScenarioIDs []string `json:"scenario_ids,omitempty"`
}

const (
	maxDAGNodes = 100
	// maxFileScopeEntries bounds a node's file_scope, which is the ownership
	// TERRITORY (the Move-3 write-boundary / Story.FilesOwned), not a per-node
	// worklist — work size is bounded by maxDAGNodes and the per-Task
	// decomposition, never by this. A cohesive ADR-049 component legitimately
	// owns many files (the live OSH MAVSDK driver is ~52: class-per-command +
	// class-per-output), so the cap must clear a realistic single-component
	// surface. Raised 50→100 after the 2026-06-14 mavlink-hard run auto-rejected
	// on "file_scope exceeds maximum entry count (52 > 50)" before any dev loop
	// ran — the work there was already decomposed into 5 Tasks; the cap was the
	// only thing rejecting it, and it was conflating territory size with work
	// size.
	maxFileScopeEntries = 100
)

// Validate checks the DAG for structural correctness: non-empty, no duplicates,
// valid dependency references, no cycles, and bounded file scope.
func (d *TaskDAG) Validate() error {
	if len(d.Nodes) == 0 {
		return fmt.Errorf("dag must contain at least one node")
	}
	if len(d.Nodes) > maxDAGNodes {
		return fmt.Errorf("dag exceeds maximum node count (%d > %d)", len(d.Nodes), maxDAGNodes)
	}

	nodeIndex := make(map[string]struct{}, len(d.Nodes))
	for _, n := range d.Nodes {
		if _, exists := nodeIndex[n.ID]; exists {
			return fmt.Errorf("duplicate node ID %q", n.ID)
		}
		nodeIndex[n.ID] = struct{}{}
	}

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

	if err := detectCycles(d.Nodes); err != nil {
		return err
	}

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

func detectCycles(nodes []TaskNode) error {
	adj := make(map[string][]string, len(nodes))
	for _, n := range nodes {
		adj[n.ID] = n.DependsOn
	}
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(nodes))

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
		}
		color[id] = black
		return nil
	}

	for _, n := range nodes {
		if color[n.ID] == white {
			if err := visit(n.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func containsPathTraversal(entry string) bool {
	for _, part := range splitPathComponents(entry) {
		if part == ".." {
			return true
		}
	}
	return false
}

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
