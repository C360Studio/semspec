package decompose_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"

	"github.com/c360studio/semspec/tools/decompose"
)

// -- helpers --

// makeCall builds a ToolCall for decompose_task with the given arguments.
func makeCall(id, loopID, traceID string, args map[string]any) agentic.ToolCall {
	return agentic.ToolCall{
		ID:        id,
		Name:      "decompose_task",
		Arguments: args,
		LoopID:    loopID,
		TraceID:   traceID,
	}
}

// mustUnmarshalDAGResponse unmarshals the Content field into the response
// envelope and returns the embedded dag.
func mustUnmarshalDAGResponse(t *testing.T, content string) decompose.TaskDAG {
	t.Helper()
	var envelope struct {
		Goal string            `json:"goal"`
		DAG  decompose.TaskDAG `json:"dag"`
	}
	if err := json.Unmarshal([]byte(content), &envelope); err != nil {
		t.Fatalf("unmarshal response envelope from %q: %v", content, err)
	}
	return envelope.DAG
}

// nodes builds the []any structure that Execute expects — matching what the
// JSON unmarshaller produces when decoding tool call arguments.
func nodes(ns ...map[string]any) []any {
	out := make([]any, len(ns))
	for i, n := range ns {
		out[i] = n
	}
	return out
}

// node is a convenience builder for a single node map with file_scope and optional deps.
func node(id, prompt, role string, deps ...string) map[string]any {
	m := map[string]any{
		"id":         id,
		"prompt":     prompt,
		"role":       role,
		"file_scope": []any{"src/" + id + "/**"},
	}
	if len(deps) > 0 {
		raw := make([]any, len(deps))
		for i, d := range deps {
			raw[i] = d
		}
		m["depends_on"] = raw
	}
	return m
}

// nodeWithScope builds a node map with explicit file scope entries.
func nodeWithScope(id, prompt, role string, scope []string, deps ...string) map[string]any {
	rawScope := make([]any, len(scope))
	for i, s := range scope {
		rawScope[i] = s
	}
	m := map[string]any{
		"id":         id,
		"prompt":     prompt,
		"role":       role,
		"file_scope": rawScope,
	}
	if len(deps) > 0 {
		raw := make([]any, len(deps))
		for i, d := range deps {
			raw[i] = d
		}
		m["depends_on"] = raw
	}
	return m
}

// -- tests --

func TestExecutor_ValidDAGWithDependencies_ReturnsValidatedJSON(t *testing.T) {
	t.Parallel()

	exec := decompose.NewExecutor()
	call := makeCall("call-1", "loop-1", "trace-1", map[string]any{
		"goal": "Build a market analysis report",
		"nodes": nodes(
			node("node-1", "Research current market data", "researcher"),
			node("node-2", "Analyze findings from research", "analyst", "node-1"),
		),
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("Execute() result.Error = %q, want empty", result.Error)
	}
	if result.CallID != "call-1" {
		t.Errorf("CallID = %q, want %q", result.CallID, "call-1")
	}

	dag := mustUnmarshalDAGResponse(t, result.Content)
	if len(dag.Nodes) != 2 {
		t.Fatalf("dag.Nodes len = %d, want 2", len(dag.Nodes))
	}
	if dag.Nodes[0].ID != "node-1" {
		t.Errorf("Nodes[0].ID = %q, want %q", dag.Nodes[0].ID, "node-1")
	}
	if dag.Nodes[1].ID != "node-2" {
		t.Errorf("Nodes[1].ID = %q, want %q", dag.Nodes[1].ID, "node-2")
	}
	if len(dag.Nodes[1].DependsOn) != 1 || dag.Nodes[1].DependsOn[0] != "node-1" {
		t.Errorf("Nodes[1].DependsOn = %v, want [node-1]", dag.Nodes[1].DependsOn)
	}
	// Verify file_scope is preserved in the response.
	if len(dag.Nodes[0].FileScope) == 0 {
		t.Errorf("Nodes[0].FileScope is empty, want at least one entry")
	}
}

func TestExecutor_LinearChain_Valid(t *testing.T) {
	t.Parallel()

	exec := decompose.NewExecutor()
	call := makeCall("call-2", "loop-1", "", map[string]any{
		"goal": "Process data pipeline",
		"nodes": nodes(
			node("a", "Step A", "worker"),
			node("b", "Step B", "worker", "a"),
			node("c", "Step C", "worker", "b"),
		),
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("Execute() result.Error = %q, want empty for linear chain", result.Error)
	}

	dag := mustUnmarshalDAGResponse(t, result.Content)
	if len(dag.Nodes) != 3 {
		t.Fatalf("dag.Nodes len = %d, want 3", len(dag.Nodes))
	}
}

func TestExecutor_ParallelTasksWithSharedDependency_Valid(t *testing.T) {
	t.Parallel()

	// A and B are independent; C depends on both.
	exec := decompose.NewExecutor()
	call := makeCall("call-3", "loop-1", "", map[string]any{
		"goal": "Parallel research then synthesis",
		"nodes": nodes(
			node("a", "Research topic A", "researcher"),
			node("b", "Research topic B", "researcher"),
			node("c", "Synthesize A and B", "analyst", "a", "b"),
		),
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("Execute() result.Error = %q, want empty for parallel tasks", result.Error)
	}

	dag := mustUnmarshalDAGResponse(t, result.Content)
	if len(dag.Nodes) != 3 {
		t.Fatalf("dag.Nodes len = %d, want 3", len(dag.Nodes))
	}
	last := dag.Nodes[2]
	if len(last.DependsOn) != 2 {
		t.Errorf("Nodes[2].DependsOn len = %d, want 2", len(last.DependsOn))
	}
}

func TestExecutor_MissingGoal_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := decompose.NewExecutor()
	call := makeCall("call-4", "loop-1", "", map[string]any{
		"nodes": nodes(node("a", "Do something", "worker")),
		// no "goal"
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("Execute() result.Error is empty, want error about missing goal")
	}
	if result.Content != "" {
		t.Errorf("Execute() result.Content = %q, want empty on error", result.Content)
	}
}

func TestExecutor_MissingNodes_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := decompose.NewExecutor()
	call := makeCall("call-5", "loop-1", "", map[string]any{
		"goal": "Do something without nodes",
		// no "nodes"
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("Execute() result.Error is empty, want error about missing nodes")
	}
}

func TestExecutor_EmptyNodesArray_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := decompose.NewExecutor()
	call := makeCall("call-6", "loop-1", "", map[string]any{
		"goal":  "Empty decomposition",
		"nodes": []any{},
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("Execute() result.Error is empty, want error about empty nodes array")
	}
}

func TestExecutor_DuplicateNodeIDs_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := decompose.NewExecutor()
	call := makeCall("call-7", "loop-1", "", map[string]any{
		"goal": "Duplicate IDs",
		"nodes": nodes(
			node("dup", "First task", "worker"),
			node("dup", "Second task with same ID", "worker"),
		),
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("Execute() result.Error is empty, want error about duplicate node IDs")
	}
	if !strings.Contains(result.Error, "dup") {
		t.Errorf("result.Error = %q, want mention of duplicate ID %q", result.Error, "dup")
	}
}

func TestExecutor_InvalidDependencyReference_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := decompose.NewExecutor()
	call := makeCall("call-8", "loop-1", "", map[string]any{
		"goal": "Node depends on non-existent node",
		"nodes": nodes(
			node("a", "Valid node", "worker"),
			node("b", "Depends on ghost", "worker", "ghost-node"),
		),
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("Execute() result.Error is empty, want error about unknown dependency")
	}
	if !strings.Contains(result.Error, "ghost-node") {
		t.Errorf("result.Error = %q, want mention of unknown node %q", result.Error, "ghost-node")
	}
}

func TestExecutor_SelfReference_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := decompose.NewExecutor()
	call := makeCall("call-9", "loop-1", "", map[string]any{
		"goal": "Node depends on itself",
		"nodes": nodes(
			node("self-loop", "Task that needs itself", "worker", "self-loop"),
		),
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("Execute() result.Error is empty, want error about self-reference")
	}
	if !strings.Contains(result.Error, "self-loop") {
		t.Errorf("result.Error = %q, want mention of self-referencing node %q", result.Error, "self-loop")
	}
}

func TestExecutor_CycleTwoNodes_ReturnsError(t *testing.T) {
	t.Parallel()

	// A depends on B, B depends on A.
	exec := decompose.NewExecutor()
	call := makeCall("call-10", "loop-1", "", map[string]any{
		"goal": "Cycle between two nodes",
		"nodes": nodes(
			node("a", "Task A", "worker", "b"),
			node("b", "Task B", "worker", "a"),
		),
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("Execute() result.Error is empty, want cycle detection error")
	}
	if !strings.Contains(strings.ToLower(result.Error), "cycle") {
		t.Errorf("result.Error = %q, want mention of cycle", result.Error)
	}
}

func TestExecutor_ResultCarriesLoopAndTraceIDs(t *testing.T) {
	t.Parallel()

	exec := decompose.NewExecutor()
	call := makeCall("call-11", "loop-xyz", "trace-abc", map[string]any{
		"goal":  "Propagation check",
		"nodes": nodes(node("n1", "Single node", "worker")),
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.LoopID != "loop-xyz" {
		t.Errorf("LoopID = %q, want %q", result.LoopID, "loop-xyz")
	}
	if result.TraceID != "trace-abc" {
		t.Errorf("TraceID = %q, want %q", result.TraceID, "trace-abc")
	}
}

func TestExecutor_ListTools_ReturnsOneDefinition(t *testing.T) {
	t.Parallel()

	exec := decompose.NewExecutor()
	tools := exec.ListTools()

	if len(tools) != 1 {
		t.Fatalf("ListTools() returned %d definitions, want 1", len(tools))
	}

	def := tools[0]
	if def.Name != "decompose_task" {
		t.Errorf("tool Name = %q, want %q", def.Name, "decompose_task")
	}
	if def.Description == "" {
		t.Error("tool Description is empty")
	}
	if def.Parameters == nil {
		t.Fatal("tool Parameters is nil")
	}

	required, ok := def.Parameters["required"].([]string)
	if !ok {
		t.Fatalf("Parameters[required] type = %T, want []string", def.Parameters["required"])
	}
	if len(required) != 2 {
		t.Fatalf("required len = %d, want 2", len(required))
	}
	wantRequired := map[string]bool{"goal": true, "nodes": true}
	for _, r := range required {
		if !wantRequired[r] {
			t.Errorf("unexpected required field %q", r)
		}
	}

	// Verify the node schema contains file_scope in required fields.
	props, ok := def.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("Parameters.properties is not a map")
	}
	nodesSchema, ok := props["nodes"].(map[string]any)
	if !ok {
		t.Fatal("nodes property is not a map")
	}
	items, ok := nodesSchema["items"].(map[string]any)
	if !ok {
		t.Fatal("nodes.items is not a map")
	}
	nodeRequired, ok := items["required"].([]string)
	if !ok {
		t.Fatalf("nodes.items.required type = %T, want []string", items["required"])
	}
	wantNodeRequired := map[string]bool{"id": true, "prompt": true, "role": true, "file_scope": true}
	for _, r := range nodeRequired {
		if !wantNodeRequired[r] {
			t.Errorf("unexpected node required field %q", r)
		}
	}
	if len(nodeRequired) != len(wantNodeRequired) {
		t.Errorf("node required fields = %v, want %v", nodeRequired, []string{"id", "prompt", "role", "file_scope"})
	}

	// Verify file_scope property is present in node schema.
	nodeProps, ok := items["properties"].(map[string]any)
	if !ok {
		t.Fatal("nodes.items.properties is not a map")
	}
	if _, exists := nodeProps["file_scope"]; !exists {
		t.Error("nodes.items.properties does not contain file_scope")
	}
}

func TestExecutor_GoalReturnedInResponse(t *testing.T) {
	t.Parallel()

	exec := decompose.NewExecutor()
	call := makeCall("call-12", "loop-1", "", map[string]any{
		"goal":  "Build a knowledge graph",
		"nodes": nodes(node("n1", "Gather sources", "researcher")),
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("Execute() result.Error = %q, want empty", result.Error)
	}

	var envelope struct {
		Goal string `json:"goal"`
	}
	if err := json.Unmarshal([]byte(result.Content), &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if envelope.Goal != "Build a knowledge graph" {
		t.Errorf("envelope.Goal = %q, want %q", envelope.Goal, "Build a knowledge graph")
	}
}

// -- FileScope-specific executor tests --

func TestExecutor_MissingFileScope_ReturnsError(t *testing.T) {
	t.Parallel()

	// Build a node without file_scope field.
	nodeNoScope := map[string]any{
		"id":     "a",
		"prompt": "Do something",
		"role":   "worker",
		// no file_scope
	}

	exec := decompose.NewExecutor()
	call := makeCall("call-fs-1", "loop-1", "", map[string]any{
		"goal":  "Task without file scope",
		"nodes": []any{nodeNoScope},
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("Execute() result.Error is empty, want error about missing file_scope")
	}
	if !strings.Contains(result.Error, "file_scope") {
		t.Errorf("result.Error = %q, want mention of file_scope", result.Error)
	}
}

func TestExecutor_FileScopePathTraversal_ReturnsError(t *testing.T) {
	t.Parallel()

	nodeWithTraversal := nodeWithScope("a", "Do something", "worker", []string{"../../../etc/passwd"})

	exec := decompose.NewExecutor()
	call := makeCall("call-fs-2", "loop-1", "", map[string]any{
		"goal":  "Path traversal attempt",
		"nodes": []any{nodeWithTraversal},
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("Execute() result.Error is empty, want error about path traversal")
	}
	if !strings.Contains(result.Error, "path traversal") {
		t.Errorf("result.Error = %q, want mention of path traversal", result.Error)
	}
}

func TestExecutor_FileScopePropagatedToDAGResponse(t *testing.T) {
	t.Parallel()

	scope := []string{"src/auth/*.go", "pkg/session/store.go"}
	exec := decompose.NewExecutor()
	call := makeCall("call-fs-3", "loop-1", "", map[string]any{
		"goal":  "Implement auth",
		"nodes": []any{nodeWithScope("auth", "Implement login flow", "developer", scope)},
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("Execute() result.Error = %q, want empty", result.Error)
	}

	dag := mustUnmarshalDAGResponse(t, result.Content)
	if len(dag.Nodes) != 1 {
		t.Fatalf("dag.Nodes len = %d, want 1", len(dag.Nodes))
	}
	got := dag.Nodes[0].FileScope
	if len(got) != 2 {
		t.Fatalf("FileScope len = %d, want 2", len(got))
	}
	if got[0] != "src/auth/*.go" {
		t.Errorf("FileScope[0] = %q, want %q", got[0], "src/auth/*.go")
	}
	if got[1] != "pkg/session/store.go" {
		t.Errorf("FileScope[1] = %q, want %q", got[1], "pkg/session/store.go")
	}
}

func TestExecutor_FileScopeInvalidArrayElementType_ReturnsError(t *testing.T) {
	t.Parallel()

	// file_scope contains a non-string element.
	nodeWithBadScope := map[string]any{
		"id":         "a",
		"prompt":     "Do something",
		"role":       "worker",
		"file_scope": []any{42, "src/valid.go"}, // first entry is an int
	}

	exec := decompose.NewExecutor()
	call := makeCall("call-fs-4", "loop-1", "", map[string]any{
		"goal":  "Bad file scope",
		"nodes": []any{nodeWithBadScope},
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("Execute() result.Error is empty, want error about non-string file_scope element")
	}
	if !strings.Contains(result.Error, "file_scope") {
		t.Errorf("result.Error = %q, want mention of file_scope", result.Error)
	}
}
