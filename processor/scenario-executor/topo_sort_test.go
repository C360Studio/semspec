package scenarioexecutor

import (
	"testing"

	"github.com/c360studio/semspec/tools/decompose"
)

func TestTopoSort_SingleNode(t *testing.T) {
	dag := &decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "a", Prompt: "do a", Role: "dev", FileScope: []string{"a.go"}},
		},
	}
	sorted, err := topoSort(dag)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sorted) != 1 || sorted[0] != "a" {
		t.Fatalf("expected [a], got %v", sorted)
	}
}

func TestTopoSort_LinearChain(t *testing.T) {
	dag := &decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "a", Prompt: "first", Role: "dev", FileScope: []string{"a.go"}},
			{ID: "b", Prompt: "second", Role: "dev", DependsOn: []string{"a"}, FileScope: []string{"b.go"}},
			{ID: "c", Prompt: "third", Role: "dev", DependsOn: []string{"b"}, FileScope: []string{"c.go"}},
		},
	}
	sorted, err := topoSort(dag)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sorted) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(sorted))
	}
	if sorted[0] != "a" || sorted[1] != "b" || sorted[2] != "c" {
		t.Fatalf("expected [a b c], got %v", sorted)
	}
}

func TestTopoSort_Diamond(t *testing.T) {
	// a → b, a → c, b → d, c → d
	dag := &decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "a", Prompt: "root", Role: "dev", FileScope: []string{"a.go"}},
			{ID: "b", Prompt: "left", Role: "dev", DependsOn: []string{"a"}, FileScope: []string{"b.go"}},
			{ID: "c", Prompt: "right", Role: "dev", DependsOn: []string{"a"}, FileScope: []string{"c.go"}},
			{ID: "d", Prompt: "sink", Role: "dev", DependsOn: []string{"b", "c"}, FileScope: []string{"d.go"}},
		},
	}
	sorted, err := topoSort(dag)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sorted) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(sorted))
	}
	// a must come first, d must come last, b and c in between
	if sorted[0] != "a" {
		t.Fatalf("expected a first, got %v", sorted)
	}
	if sorted[3] != "d" {
		t.Fatalf("expected d last, got %v", sorted)
	}
}

func TestTopoSort_IndependentNodes(t *testing.T) {
	// All independent — slice order preserved.
	dag := &decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "x", Prompt: "p", Role: "dev", FileScope: []string{"x.go"}},
			{ID: "y", Prompt: "p", Role: "dev", FileScope: []string{"y.go"}},
			{ID: "z", Prompt: "p", Role: "dev", FileScope: []string{"z.go"}},
		},
	}
	sorted, err := topoSort(dag)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Slice order preserved for independent nodes.
	if sorted[0] != "x" || sorted[1] != "y" || sorted[2] != "z" {
		t.Fatalf("expected [x y z], got %v", sorted)
	}
}

func TestTopoSort_NilDAG(t *testing.T) {
	_, err := topoSort(nil)
	if err == nil {
		t.Fatal("expected error for nil DAG")
	}
}

func TestTopoSort_EmptyDAG(t *testing.T) {
	_, err := topoSort(&decompose.TaskDAG{})
	if err == nil {
		t.Fatal("expected error for empty DAG")
	}
}

func TestTopoSort_CycleDetection(t *testing.T) {
	// Note: TaskDAG.Validate() normally catches cycles before topoSort is called.
	// This tests the defensive guard in topoSort itself. We bypass Validate() here
	// by constructing the DAG directly.
	dag := &decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "a", Prompt: "p", Role: "dev", DependsOn: []string{"b"}, FileScope: []string{"a.go"}},
			{ID: "b", Prompt: "p", Role: "dev", DependsOn: []string{"a"}, FileScope: []string{"b.go"}},
		},
	}
	_, err := topoSort(dag)
	if err == nil {
		t.Fatal("expected error for cyclic DAG")
	}
}
