package reactive

import (
	"testing"
	"time"

	"github.com/c360studio/semspec/tools/decompose"
	reactiveEngine "github.com/c360studio/semstreams/processor/reactive"
)

// ---------------------------------------------------------------------------
// Definition-level tests
// ---------------------------------------------------------------------------

func TestDAGExecutionWorkflow_Definition(t *testing.T) {
	def := BuildDAGExecutionWorkflow(testStateBucket)

	if def.ID != DAGExecutionWorkflowID {
		t.Errorf("expected ID %q, got %q", DAGExecutionWorkflowID, def.ID)
	}
	if def.ID != "dag-execution-loop" {
		t.Errorf("expected literal ID 'dag-execution-loop', got %q", def.ID)
	}

	expectedRules := []struct {
		id         string
		actionType reactiveEngine.ActionType
	}{
		{"accept-trigger", reactiveEngine.ActionMutate},
		{"dispatch-ready-nodes", reactiveEngine.ActionMutate},
		{"handle-node-complete", reactiveEngine.ActionMutate},
		{"handle-node-failed", reactiveEngine.ActionMutate},
		{"handle-complete", reactiveEngine.ActionComplete},
		{"handle-failed", reactiveEngine.ActionComplete},
	}

	if len(def.Rules) != len(expectedRules) {
		t.Fatalf("expected %d rules, got %d", len(expectedRules), len(def.Rules))
	}

	for i, want := range expectedRules {
		rule := def.Rules[i]
		if rule.ID != want.id {
			t.Errorf("rule[%d]: expected ID %q, got %q", i, want.id, rule.ID)
		}
		if rule.Action.Type != want.actionType {
			t.Errorf("rule[%d] %q: expected action type %v, got %v",
				i, want.id, want.actionType, rule.Action.Type)
		}
	}

	if def.StateBucket != testStateBucket {
		t.Errorf("expected state bucket %q, got %q", testStateBucket, def.StateBucket)
	}
}

func TestDAGExecutionWorkflow_StateFactory(t *testing.T) {
	def := BuildDAGExecutionWorkflow(testStateBucket)

	got := def.StateFactory()
	if got == nil {
		t.Fatal("StateFactory returned nil")
	}
	_, ok := got.(*DAGExecutionState)
	if !ok {
		t.Errorf("StateFactory returned wrong type: %T", got)
	}
}

// ---------------------------------------------------------------------------
// accept-trigger tests
// ---------------------------------------------------------------------------

func TestDAGExecution_AcceptTrigger_InitializesState(t *testing.T) {
	def := BuildDAGExecutionWorkflow(testStateBucket)
	rule := findRule(t, def, "accept-trigger")

	dag := decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "node-a", Prompt: "do A", Role: "developer", DependsOn: nil, FileScope: []string{"src/node-a/**"}},
			{ID: "node-b", Prompt: "do B", Role: "developer", DependsOn: []string{"node-a"}, FileScope: []string{"src/node-b/**"}},
		},
	}
	trigger := &DAGExecutionTriggerPayload{
		ExecutionID: "exec-001",
		ScenarioID:  "scenario-x",
		DAG:         dag,
	}

	state := &DAGExecutionState{}
	ctx := &reactiveEngine.RuleContext{State: state, Message: trigger}

	// Condition must pass.
	for _, cond := range rule.Conditions {
		if !cond.Evaluate(ctx) {
			t.Errorf("condition %q should pass", cond.Description)
		}
	}

	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	if state.ExecutionID != "exec-001" {
		t.Errorf("expected ExecutionID 'exec-001', got %q", state.ExecutionID)
	}
	if state.ScenarioID != "scenario-x" {
		t.Errorf("expected ScenarioID 'scenario-x', got %q", state.ScenarioID)
	}
	if len(state.DAG.Nodes) != 2 {
		t.Errorf("expected 2 DAG nodes, got %d", len(state.DAG.Nodes))
	}
	if state.Phase != DAGPhaseExecuting {
		t.Errorf("expected phase %q, got %q", DAGPhaseExecuting, state.Phase)
	}
	if state.ID != "dag-execution.exec-001" {
		t.Errorf("expected ID 'dag-execution.exec-001', got %q", state.ID)
	}
	if state.WorkflowID != DAGExecutionWorkflowID {
		t.Errorf("expected WorkflowID %q, got %q", DAGExecutionWorkflowID, state.WorkflowID)
	}
	if state.Status != reactiveEngine.StatusRunning {
		t.Errorf("expected StatusRunning, got %v", state.Status)
	}

	// All nodes should start as pending.
	for _, node := range dag.Nodes {
		if state.NodeStates[node.ID] != DAGNodePending {
			t.Errorf("node %q: expected %q, got %q", node.ID, DAGNodePending, state.NodeStates[node.ID])
		}
	}
}

func TestDAGExecution_AcceptTrigger_SecondTriggerPreservesID(t *testing.T) {
	def := BuildDAGExecutionWorkflow(testStateBucket)
	rule := findRule(t, def, "accept-trigger")

	dag := singleNodeDAG("node-a")
	trigger := &DAGExecutionTriggerPayload{
		ExecutionID: "exec-002",
		ScenarioID:  "scenario-y",
		DAG:         dag,
	}

	state := &DAGExecutionState{}
	state.ID = "dag-execution.exec-002"
	state.WorkflowID = DAGExecutionWorkflowID

	ctx := &reactiveEngine.RuleContext{State: state, Message: trigger}
	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	if state.ID != "dag-execution.exec-002" {
		t.Errorf("ID should be preserved on re-trigger, got %q", state.ID)
	}
}

func TestDAGExecution_AcceptTrigger_MissingExecutionID(t *testing.T) {
	def := BuildDAGExecutionWorkflow(testStateBucket)
	rule := findRule(t, def, "accept-trigger")

	trigger := &DAGExecutionTriggerPayload{
		ExecutionID: "",
		ScenarioID:  "scenario-x",
		DAG:         singleNodeDAG("node-a"),
	}
	state := &DAGExecutionState{}
	ctx := &reactiveEngine.RuleContext{State: state, Message: trigger}

	if err := rule.Action.MutateState(ctx, nil); err == nil {
		t.Fatal("expected error for missing execution_id")
	}
}

// ---------------------------------------------------------------------------
// DAGReadyNodes unit tests (pure logic — no engine involvement)
// ---------------------------------------------------------------------------

func TestDAGReadyNodes_NoDepsIsImmediatelyReady(t *testing.T) {
	dag := decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "a", DependsOn: nil, FileScope: []string{"src/a/**"}},
			{ID: "b", DependsOn: nil, FileScope: []string{"src/b/**"}},
		},
	}
	nodeStates := map[string]string{
		"a": DAGNodePending,
		"b": DAGNodePending,
	}

	ready := DAGReadyNodes(dag, nodeStates)
	if len(ready) != 2 {
		t.Errorf("expected 2 ready nodes, got %d: %v", len(ready), ready)
	}
}

func TestDAGReadyNodes_DepCompletedMakesNodeReady(t *testing.T) {
	dag := decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "a", DependsOn: nil, FileScope: []string{"src/a/**"}},
			{ID: "b", DependsOn: []string{"a"}, FileScope: []string{"src/b/**"}},
		},
	}
	nodeStates := map[string]string{
		"a": DAGNodeCompleted,
		"b": DAGNodePending,
	}

	ready := DAGReadyNodes(dag, nodeStates)
	if len(ready) != 1 || ready[0] != "b" {
		t.Errorf("expected ['b'], got %v", ready)
	}
}

func TestDAGReadyNodes_PendingDepBlocksNode(t *testing.T) {
	dag := decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "a", DependsOn: nil, FileScope: []string{"src/a/**"}},
			{ID: "b", DependsOn: []string{"a"}, FileScope: []string{"src/b/**"}},
		},
	}
	nodeStates := map[string]string{
		"a": DAGNodePending, // not completed yet
		"b": DAGNodePending,
	}

	ready := DAGReadyNodes(dag, nodeStates)
	// Only "a" has no deps and is pending — "b" is blocked.
	if len(ready) != 1 || ready[0] != "a" {
		t.Errorf("expected only ['a'], got %v", ready)
	}
}

func TestDAGReadyNodes_RunningDepBlocksNode(t *testing.T) {
	dag := decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "a", DependsOn: nil, FileScope: []string{"src/a/**"}},
			{ID: "b", DependsOn: []string{"a"}, FileScope: []string{"src/b/**"}},
		},
	}
	nodeStates := map[string]string{
		"a": DAGNodeRunning, // in-flight, not completed
		"b": DAGNodePending,
	}

	ready := DAGReadyNodes(dag, nodeStates)
	// "a" is running (not pending), "b" dep not completed → nothing ready.
	if len(ready) != 0 {
		t.Errorf("expected no ready nodes, got %v", ready)
	}
}

func TestDAGReadyNodes_MultipleDepsMustAllBeComplete(t *testing.T) {
	dag := decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "a", DependsOn: nil, FileScope: []string{"src/a/**"}},
			{ID: "b", DependsOn: nil, FileScope: []string{"src/b/**"}},
			{ID: "c", DependsOn: []string{"a", "b"}, FileScope: []string{"src/c/**"}},
		},
	}

	t.Run("only one dep complete — not ready", func(t *testing.T) {
		nodeStates := map[string]string{
			"a": DAGNodeCompleted,
			"b": DAGNodeRunning,
			"c": DAGNodePending,
		}
		ready := DAGReadyNodes(dag, nodeStates)
		for _, id := range ready {
			if id == "c" {
				t.Error("node 'c' should not be ready when 'b' is not completed")
			}
		}
	})

	t.Run("both deps complete — ready", func(t *testing.T) {
		nodeStates := map[string]string{
			"a": DAGNodeCompleted,
			"b": DAGNodeCompleted,
			"c": DAGNodePending,
		}
		ready := DAGReadyNodes(dag, nodeStates)
		found := false
		for _, id := range ready {
			if id == "c" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected 'c' to be ready, got %v", ready)
		}
	})
}

// ---------------------------------------------------------------------------
// dispatch-ready-nodes rule tests
// ---------------------------------------------------------------------------

func TestDAGExecution_DispatchReadyNodes_AllNodesComplete_TransitionsToComplete(t *testing.T) {
	def := BuildDAGExecutionWorkflow(testStateBucket)
	rule := findRule(t, def, "dispatch-ready-nodes")

	state := dagExecExecutingState("exec-010", singleNodeDAG("a"))
	state.NodeStates["a"] = DAGNodeCompleted
	state.CompletedNodes = []string{"a"}

	ctx := &reactiveEngine.RuleContext{State: state}
	assertAllConditionsPass(t, rule, ctx)

	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	if state.Phase != DAGPhaseComplete {
		t.Errorf("expected phase %q, got %q", DAGPhaseComplete, state.Phase)
	}
}

func TestDAGExecution_DispatchReadyNodes_FailedNodeNoRunning_TransitionsToFailed(t *testing.T) {
	def := BuildDAGExecutionWorkflow(testStateBucket)
	rule := findRule(t, def, "dispatch-ready-nodes")

	dag := decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "a", DependsOn: nil, FileScope: []string{"src/a/**"}},
			{ID: "b", DependsOn: []string{"a"}, FileScope: []string{"src/b/**"}},
		},
	}
	state := dagExecExecutingState("exec-011", dag)
	state.NodeStates["a"] = DAGNodeFailed
	state.NodeStates["b"] = DAGNodePending // blocked by failed dep
	state.FailedNodes = []string{"a"}

	ctx := &reactiveEngine.RuleContext{State: state}

	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	if state.Phase != DAGPhaseFailed {
		t.Errorf("expected phase %q, got %q", DAGPhaseFailed, state.Phase)
	}
}

func TestDAGExecution_DispatchReadyNodes_FailedNodeWithRunningNode_StaysExecuting(t *testing.T) {
	def := BuildDAGExecutionWorkflow(testStateBucket)
	rule := findRule(t, def, "dispatch-ready-nodes")

	dag := decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "a", DependsOn: nil, FileScope: []string{"src/a/**"}},
			{ID: "b", DependsOn: nil, FileScope: []string{"src/b/**"}},
		},
	}
	state := dagExecExecutingState("exec-012", dag)
	state.NodeStates["a"] = DAGNodeFailed  // failed
	state.NodeStates["b"] = DAGNodeRunning // still running

	ctx := &reactiveEngine.RuleContext{State: state}

	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	// Should stay in executing — "b" is still running.
	if state.Phase != DAGPhaseExecuting {
		t.Errorf("expected phase %q (running node present), got %q", DAGPhaseExecuting, state.Phase)
	}
}

func TestDAGExecution_DispatchReadyNodes_ReadyNodeTransitionsToRunning(t *testing.T) {
	def := BuildDAGExecutionWorkflow(testStateBucket)
	rule := findRule(t, def, "dispatch-ready-nodes")

	dag := singleNodeDAG("node-alpha")
	state := dagExecExecutingState("exec-013", dag)
	// node-alpha is pending with no deps — should become running.

	ctx := &reactiveEngine.RuleContext{State: state}

	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	if state.NodeStates["node-alpha"] != DAGNodeRunning {
		t.Errorf("expected node-alpha to be %q, got %q", DAGNodeRunning, state.NodeStates["node-alpha"])
	}
	// Phase stays executing while nodes are in-flight.
	if state.Phase != DAGPhaseExecuting {
		t.Errorf("expected phase to remain %q, got %q", DAGPhaseExecuting, state.Phase)
	}
}

func TestDAGExecution_DispatchReadyNodes_DoesNotMatchWhenNotExecuting(t *testing.T) {
	def := BuildDAGExecutionWorkflow(testStateBucket)
	rule := findRule(t, def, "dispatch-ready-nodes")

	state := dagExecExecutingState("exec-014", singleNodeDAG("a"))
	state.Phase = DAGPhaseComplete

	ctx := &reactiveEngine.RuleContext{State: state}
	assertSomeConditionFails(t, rule, ctx)
}

// ---------------------------------------------------------------------------
// handle-node-complete rule tests
// ---------------------------------------------------------------------------

func TestDAGExecution_HandleNodeComplete_MarksNodeCompleted(t *testing.T) {
	def := BuildDAGExecutionWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-node-complete")

	state := dagExecExecutingState("exec-020", singleNodeDAG("a"))
	state.NodeStates["a"] = DAGNodeRunning

	msg := &DAGNodeCompletePayload{
		ExecutionID: "exec-020",
		NodeID:      "a",
		QualityEvidence: &QualityEvidence{
			ValidationPassed: true,
			ValidationChecks: 3,
			ReviewVerdict:    "approved",
		},
	}
	ctx := &reactiveEngine.RuleContext{State: state, Message: msg}

	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	if state.NodeStates["a"] != DAGNodeCompleted {
		t.Errorf("expected node 'a' to be %q, got %q", DAGNodeCompleted, state.NodeStates["a"])
	}
	if len(state.CompletedNodes) != 1 || state.CompletedNodes[0] != "a" {
		t.Errorf("expected CompletedNodes=['a'], got %v", state.CompletedNodes)
	}
}

func TestDAGExecution_HandleNodeComplete_AppendsToCompletedNodes(t *testing.T) {
	def := BuildDAGExecutionWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-node-complete")

	dag := decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "a", DependsOn: nil, FileScope: []string{"src/a/**"}},
			{ID: "b", DependsOn: nil, FileScope: []string{"src/b/**"}},
		},
	}
	state := dagExecExecutingState("exec-021", dag)
	state.NodeStates["a"] = DAGNodeCompleted
	state.CompletedNodes = []string{"a"}
	state.NodeStates["b"] = DAGNodeRunning

	msg := &DAGNodeCompletePayload{
		ExecutionID: "exec-021",
		NodeID:      "b",
		QualityEvidence: &QualityEvidence{
			ValidationPassed: true,
			ValidationChecks: 2,
			ReviewVerdict:    "approved",
		},
	}
	ctx := &reactiveEngine.RuleContext{State: state, Message: msg}

	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	if len(state.CompletedNodes) != 2 {
		t.Errorf("expected 2 completed nodes, got %d: %v", len(state.CompletedNodes), state.CompletedNodes)
	}
}

// ---------------------------------------------------------------------------
// Quality gate tests for handle-node-complete
// ---------------------------------------------------------------------------

func TestDAGExecution_HandleNodeComplete_RejectsNilQualityEvidence(t *testing.T) {
	def := BuildDAGExecutionWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-node-complete")

	state := dagExecExecutingState("exec-qg-1", singleNodeDAG("a"))
	state.NodeStates["a"] = DAGNodeRunning

	msg := &DAGNodeCompletePayload{ExecutionID: "exec-qg-1", NodeID: "a"}
	ctx := &reactiveEngine.RuleContext{State: state, Message: msg}

	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	if state.NodeStates["a"] != DAGNodeFailed {
		t.Errorf("expected node 'a' to be %q (nil evidence), got %q", DAGNodeFailed, state.NodeStates["a"])
	}
	if len(state.FailedNodes) != 1 || state.FailedNodes[0] != "a" {
		t.Errorf("expected FailedNodes=['a'], got %v", state.FailedNodes)
	}
	if len(state.CompletedNodes) != 0 {
		t.Errorf("expected no CompletedNodes, got %v", state.CompletedNodes)
	}
}

func TestDAGExecution_HandleNodeComplete_RejectsValidationNotPassed(t *testing.T) {
	def := BuildDAGExecutionWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-node-complete")

	state := dagExecExecutingState("exec-qg-2", singleNodeDAG("a"))
	state.NodeStates["a"] = DAGNodeRunning

	msg := &DAGNodeCompletePayload{
		ExecutionID: "exec-qg-2",
		NodeID:      "a",
		QualityEvidence: &QualityEvidence{
			ValidationPassed: false,
			ReviewVerdict:    "approved",
		},
	}
	ctx := &reactiveEngine.RuleContext{State: state, Message: msg}

	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	if state.NodeStates["a"] != DAGNodeFailed {
		t.Errorf("expected node 'a' to be %q (validation failed), got %q", DAGNodeFailed, state.NodeStates["a"])
	}
}

func TestDAGExecution_HandleNodeComplete_RejectsUnapprovedReview(t *testing.T) {
	def := BuildDAGExecutionWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-node-complete")

	state := dagExecExecutingState("exec-qg-3", singleNodeDAG("a"))
	state.NodeStates["a"] = DAGNodeRunning

	msg := &DAGNodeCompletePayload{
		ExecutionID: "exec-qg-3",
		NodeID:      "a",
		QualityEvidence: &QualityEvidence{
			ValidationPassed: true,
			ValidationChecks: 3,
			ReviewVerdict:    "needs_changes",
		},
	}
	ctx := &reactiveEngine.RuleContext{State: state, Message: msg}

	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	if state.NodeStates["a"] != DAGNodeFailed {
		t.Errorf("expected node 'a' to be %q (unapproved review), got %q", DAGNodeFailed, state.NodeStates["a"])
	}
}

func TestDAGExecution_HandleNodeComplete_AcceptsValidEvidence(t *testing.T) {
	def := BuildDAGExecutionWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-node-complete")

	state := dagExecExecutingState("exec-qg-4", singleNodeDAG("a"))
	state.NodeStates["a"] = DAGNodeRunning

	msg := &DAGNodeCompletePayload{
		ExecutionID: "exec-qg-4",
		NodeID:      "a",
		QualityEvidence: &QualityEvidence{
			ValidationPassed: true,
			ValidationChecks: 3,
			ReviewVerdict:    "approved",
			ReviewerID:       "code-reviewer-1",
		},
	}
	ctx := &reactiveEngine.RuleContext{State: state, Message: msg}

	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	if state.NodeStates["a"] != DAGNodeCompleted {
		t.Errorf("expected node 'a' to be %q, got %q", DAGNodeCompleted, state.NodeStates["a"])
	}
	if len(state.CompletedNodes) != 1 || state.CompletedNodes[0] != "a" {
		t.Errorf("expected CompletedNodes=['a'], got %v", state.CompletedNodes)
	}
}

// ---------------------------------------------------------------------------
// handle-node-failed rule tests
// ---------------------------------------------------------------------------

func TestDAGExecution_HandleNodeFailed_MarksNodeFailed(t *testing.T) {
	def := BuildDAGExecutionWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-node-failed")

	state := dagExecExecutingState("exec-030", singleNodeDAG("a"))
	state.NodeStates["a"] = DAGNodeRunning

	msg := &DAGNodeFailedPayload{ExecutionID: "exec-030", NodeID: "a", Reason: "process exited 1"}
	ctx := &reactiveEngine.RuleContext{State: state, Message: msg}

	if err := rule.Action.MutateState(ctx, nil); err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	if state.NodeStates["a"] != DAGNodeFailed {
		t.Errorf("expected node 'a' to be %q, got %q", DAGNodeFailed, state.NodeStates["a"])
	}
	if len(state.FailedNodes) != 1 || state.FailedNodes[0] != "a" {
		t.Errorf("expected FailedNodes=['a'], got %v", state.FailedNodes)
	}
}

// ---------------------------------------------------------------------------
// handle-complete rule tests
// ---------------------------------------------------------------------------

func TestDAGExecution_HandleComplete_ConditionsMatchOnlyWhenComplete(t *testing.T) {
	def := BuildDAGExecutionWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-complete")

	t.Run("matches complete phase", func(t *testing.T) {
		state := dagExecCompleteState("exec-040")
		ctx := &reactiveEngine.RuleContext{State: state}
		assertAllConditionsPass(t, rule, ctx)
	})

	t.Run("does not match executing phase", func(t *testing.T) {
		state := dagExecExecutingState("exec-041", singleNodeDAG("a"))
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})

	t.Run("does not match when already terminal", func(t *testing.T) {
		state := dagExecCompleteState("exec-042")
		// Simulate engine having completed this execution (sets status to terminal).
		state.Status = reactiveEngine.StatusCompleted
		ctx := &reactiveEngine.RuleContext{State: state}
		assertSomeConditionFails(t, rule, ctx)
	})
}

func TestDAGExecution_HandleComplete_BuildsCompleteEvent(t *testing.T) {
	def := BuildDAGExecutionWorkflow(testStateBucket)
	rule := findRule(t, def, "handle-complete")

	state := dagExecCompleteState("exec-050")
	state.ScenarioID = "scenario-z"
	state.CompletedNodes = []string{"a", "b"}

	ctx := &reactiveEngine.RuleContext{State: state}
	payload, err := rule.Action.BuildPayload(ctx)
	if err != nil {
		t.Fatalf("BuildPayload failed: %v", err)
	}

	event, ok := payload.(*DAGExecutionCompletePayload)
	if !ok {
		t.Fatalf("expected *DAGExecutionCompletePayload, got %T", payload)
	}
	if event.ExecutionID != "exec-050" {
		t.Errorf("expected ExecutionID 'exec-050', got %q", event.ExecutionID)
	}
	if event.ScenarioID != "scenario-z" {
		t.Errorf("expected ScenarioID 'scenario-z', got %q", event.ScenarioID)
	}
	if len(event.CompletedNodes) != 2 {
		t.Errorf("expected 2 completed nodes, got %d", len(event.CompletedNodes))
	}
}

// ---------------------------------------------------------------------------
// DAGNodeTaskID helper test
// ---------------------------------------------------------------------------

func TestDAGNodeTaskID(t *testing.T) {
	id := DAGNodeTaskID("exec-001", "node-a")
	expected := "dagexec:exec-001:node-a"
	if id != expected {
		t.Errorf("expected %q, got %q", expected, id)
	}
}

// ---------------------------------------------------------------------------
// GetExecutionState test
// ---------------------------------------------------------------------------

func TestDAGExecutionState_GetExecutionState(t *testing.T) {
	state := &DAGExecutionState{}
	state.ID = "dag-execution.exec-001"
	state.Phase = DAGPhaseExecuting

	es := state.GetExecutionState()
	if es == nil {
		t.Fatal("GetExecutionState returned nil")
	}
	if es.ID != "dag-execution.exec-001" {
		t.Errorf("expected ID 'dag-execution.exec-001', got %q", es.ID)
	}
	if es.Phase != DAGPhaseExecuting {
		t.Errorf("expected phase %q, got %q", DAGPhaseExecuting, es.Phase)
	}

	// Mutation through the pointer should be reflected in state.
	es.Phase = DAGPhaseComplete
	if state.Phase != DAGPhaseComplete {
		t.Error("mutation through GetExecutionState() should update state.Phase")
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// singleNodeDAG builds a minimal TaskDAG with a single node and no dependencies.
func singleNodeDAG(nodeID string) decompose.TaskDAG {
	return decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: nodeID, Prompt: "do " + nodeID, Role: "developer", DependsOn: nil, FileScope: []string{"src/" + nodeID + "/**"}},
		},
	}
}

// dagExecExecutingState builds a DAGExecutionState in the "executing" phase
// with all nodes initialized to "pending".
func dagExecExecutingState(executionID string, dag decompose.TaskDAG) *DAGExecutionState {
	nodeStates := make(map[string]string, len(dag.Nodes))
	for _, n := range dag.Nodes {
		nodeStates[n.ID] = DAGNodePending
	}

	return &DAGExecutionState{
		ExecutionState: reactiveEngine.ExecutionState{
			ID:         "dag-execution." + executionID,
			WorkflowID: DAGExecutionWorkflowID,
			Phase:      DAGPhaseExecuting,
			Status:     reactiveEngine.StatusRunning,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		},
		ExecutionID: executionID,
		ScenarioID:  "scenario-test",
		DAG:         dag,
		NodeStates:  nodeStates,
	}
}

// dagExecCompleteState builds a DAGExecutionState in the "complete" phase.
func dagExecCompleteState(executionID string) *DAGExecutionState {
	return &DAGExecutionState{
		ExecutionState: reactiveEngine.ExecutionState{
			ID:         "dag-execution." + executionID,
			WorkflowID: DAGExecutionWorkflowID,
			Phase:      DAGPhaseComplete,
			Status:     reactiveEngine.StatusRunning,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		},
		ExecutionID: executionID,
		ScenarioID:  "scenario-test",
	}
}
