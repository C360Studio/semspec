package requirementexecutor

import (
	"strings"
	"testing"
)

func dag(nodes ...TaskNode) TaskDAG {
	return TaskDAG{Nodes: nodes}
}

func taskNode(id, prompt, role string, deps ...string) TaskNode {
	return TaskNode{
		ID:        id,
		Prompt:    prompt,
		Role:      role,
		DependsOn: deps,
		FileScope: []string{"src/" + id + "/**"},
	}
}

func TestValidate_SingleNode_Valid(t *testing.T) {
	t.Parallel()
	d := dag(taskNode("a", "Do something", "worker"))
	if err := d.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil for single valid node", err)
	}
}

func TestValidate_LinearChain_Valid(t *testing.T) {
	t.Parallel()
	d := dag(
		taskNode("a", "Step A", "worker"),
		taskNode("b", "Step B", "worker", "a"),
		taskNode("c", "Step C", "worker", "b"),
	)
	if err := d.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil for linear chain", err)
	}
}

func TestValidate_ParallelNodesWithSharedDep_Valid(t *testing.T) {
	t.Parallel()
	d := dag(
		taskNode("root", "Root task", "planner"),
		taskNode("a", "Branch A", "worker", "root"),
		taskNode("b", "Branch B", "worker", "root"),
		taskNode("merge", "Merge results", "analyst", "a", "b"),
	)
	if err := d.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil for diamond DAG", err)
	}
}

func TestValidate_EmptyNodes_ReturnsError(t *testing.T) {
	t.Parallel()
	d := TaskDAG{}
	if err := d.Validate(); err == nil {
		t.Error("Validate() = nil, want error for empty nodes")
	}
}

func TestValidate_DuplicateNodeIDs_ReturnsError(t *testing.T) {
	t.Parallel()
	d := dag(
		taskNode("dup", "First", "worker"),
		taskNode("dup", "Second with same ID", "worker"),
	)
	err := d.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error for duplicate node IDs")
	}
	if !strings.Contains(err.Error(), "dup") {
		t.Errorf("error = %q, want mention of duplicate ID %q", err.Error(), "dup")
	}
}

func TestValidate_InvalidDependencyRef_ReturnsError(t *testing.T) {
	t.Parallel()
	d := dag(
		taskNode("a", "Valid", "worker"),
		taskNode("b", "Depends on ghost", "worker", "nonexistent"),
	)
	err := d.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error for unknown dependency")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error = %q, want mention of unknown node %q", err.Error(), "nonexistent")
	}
}

func TestValidate_SelfReference_ReturnsError(t *testing.T) {
	t.Parallel()
	d := dag(taskNode("loop", "Self-referencing", "worker", "loop"))
	err := d.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error for self-reference")
	}
	if !strings.Contains(err.Error(), "loop") {
		t.Errorf("error = %q, want mention of self-referencing node %q", err.Error(), "loop")
	}
}

func TestValidate_SimpleCycleTwoNodes_ReturnsError(t *testing.T) {
	t.Parallel()
	d := dag(
		taskNode("a", "Task A", "worker", "b"),
		taskNode("b", "Task B", "worker", "a"),
	)
	err := d.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want cycle detection error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "cycle") {
		t.Errorf("error = %q, want mention of cycle", err.Error())
	}
}

func TestValidate_ThreeNodeCycle_ReturnsError(t *testing.T) {
	t.Parallel()
	d := dag(
		taskNode("a", "Task A", "worker", "c"),
		taskNode("b", "Task B", "worker", "a"),
		taskNode("c", "Task C", "worker", "b"),
	)
	err := d.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want cycle detection error for 3-node cycle")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "cycle") {
		t.Errorf("error = %q, want mention of cycle", err.Error())
	}
}

func TestValidate_MultipleDisconnectedComponents_Valid(t *testing.T) {
	t.Parallel()
	d := dag(
		taskNode("chain1-a", "Chain 1 Step A", "worker"),
		taskNode("chain1-b", "Chain 1 Step B", "worker", "chain1-a"),
		taskNode("chain2-a", "Chain 2 Step A", "analyst"),
		taskNode("chain2-b", "Chain 2 Step B", "analyst", "chain2-a"),
	)
	if err := d.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil for disconnected valid DAG", err)
	}
}

func TestValidate_FileScope_MissingEntries_ReturnsError(t *testing.T) {
	t.Parallel()
	d := dag(TaskNode{ID: "a", Prompt: "Do something", Role: "worker"})
	err := d.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error for missing file_scope")
	}
	if !strings.Contains(err.Error(), "file_scope") {
		t.Errorf("error = %q, want mention of file_scope", err.Error())
	}
	if !strings.Contains(err.Error(), `"a"`) {
		t.Errorf("error = %q, want mention of node ID %q", err.Error(), "a")
	}
}

func TestValidate_FileScope_EmptySlice_ReturnsError(t *testing.T) {
	t.Parallel()
	d := dag(TaskNode{ID: "a", Prompt: "Do something", Role: "worker", FileScope: []string{}})
	err := d.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error for empty file_scope slice")
	}
	if !strings.Contains(err.Error(), "file_scope") {
		t.Errorf("error = %q, want mention of file_scope", err.Error())
	}
}

func TestValidate_FileScope_EmptyStringEntry_ReturnsError(t *testing.T) {
	t.Parallel()
	d := dag(TaskNode{ID: "a", Prompt: "Do something", Role: "worker", FileScope: []string{"src/main.go", ""}})
	err := d.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error for empty file_scope entry")
	}
	if !strings.Contains(err.Error(), "file_scope") {
		t.Errorf("error = %q, want mention of file_scope", err.Error())
	}
}

func TestValidate_FileScope_PathTraversal_ReturnsError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		entry string
	}{
		{"double dot prefix", "../secrets.go"},
		{"double dot in path", "src/../../../etc/passwd"},
		{"double dot component", "foo/../../bar"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := dag(TaskNode{ID: "a", Prompt: "Do something", Role: "worker", FileScope: []string{tc.entry}})
			err := d.Validate()
			if err == nil {
				t.Fatalf("Validate() = nil, want error for path traversal entry %q", tc.entry)
			}
			if !strings.Contains(err.Error(), "path traversal") {
				t.Errorf("error = %q, want mention of path traversal", err.Error())
			}
		})
	}
}

func TestValidate_FileScope_ValidGlobPatterns_NoError(t *testing.T) {
	t.Parallel()
	d := dag(TaskNode{
		ID: "a", Prompt: "Do something", Role: "worker",
		FileScope: []string{
			"src/auth/*.go",
			"pkg/utils/hash.go",
			"internal/**/*.go",
			"cmd/semspec/main.go",
		},
	})
	if err := d.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil for valid glob patterns", err)
	}
}

func TestValidate_FileScope_MaxEntriesExceeded_ReturnsError(t *testing.T) {
	t.Parallel()
	scope := make([]string, maxFileScopeEntries+1)
	for i := range scope {
		scope[i] = "src/file" + strings.Repeat("x", i) + ".go"
	}
	d := dag(TaskNode{ID: "a", Prompt: "Do something", Role: "worker", FileScope: scope})
	err := d.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error for exceeding max file_scope entries")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("error = %q, want mention of exceeds maximum", err.Error())
	}
}

func TestValidate_FileScope_ExactlyMaxEntries_Valid(t *testing.T) {
	t.Parallel()
	scope := make([]string, maxFileScopeEntries)
	for i := range scope {
		scope[i] = "src/file" + strings.Repeat("x", i) + ".go"
	}
	d := dag(TaskNode{ID: "a", Prompt: "Do something", Role: "worker", FileScope: scope})
	if err := d.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil for exactly %d file_scope entries", err, maxFileScopeEntries)
	}
}

func TestValidate_FileScope_MultipleNodes_AllMustHaveScope(t *testing.T) {
	t.Parallel()
	d := dag(
		TaskNode{ID: "a", Prompt: "Task A", Role: "worker", FileScope: []string{"src/a.go"}},
		TaskNode{ID: "b", Prompt: "Task B", Role: "worker", DependsOn: []string{"a"}},
	)
	err := d.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error when second node has no file_scope")
	}
	if !strings.Contains(err.Error(), `"b"`) {
		t.Errorf("error = %q, want mention of node ID %q", err.Error(), "b")
	}
}
