package requirementexecutor

import (
	"fmt"

	"github.com/c360studio/semspec/tools/decompose"
)

// topoSort returns node IDs in topological order using Kahn's algorithm (BFS).
// For nodes with equal in-degree, slice order is preserved (stable sort).
// The input DAG must have already passed Validate() (no cycles, valid deps).
func topoSort(dag *decompose.TaskDAG) ([]string, error) {
	if dag == nil || len(dag.Nodes) == 0 {
		return nil, fmt.Errorf("empty DAG")
	}

	// Build in-degree map and adjacency list (dependsOn → blocks).
	inDegree := make(map[string]int, len(dag.Nodes))
	dependents := make(map[string][]string, len(dag.Nodes))
	for _, n := range dag.Nodes {
		inDegree[n.ID] = len(n.DependsOn)
		for _, dep := range n.DependsOn {
			dependents[dep] = append(dependents[dep], n.ID)
		}
	}

	// Seed the queue with zero-in-degree nodes in slice order.
	queue := make([]string, 0)
	for _, n := range dag.Nodes {
		if inDegree[n.ID] == 0 {
			queue = append(queue, n.ID)
		}
	}

	sorted := make([]string, 0, len(dag.Nodes))
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		sorted = append(sorted, id)

		for _, dep := range dependents[id] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if len(sorted) != len(dag.Nodes) {
		return nil, fmt.Errorf("cycle detected: sorted %d of %d nodes", len(sorted), len(dag.Nodes))
	}

	return sorted, nil
}
