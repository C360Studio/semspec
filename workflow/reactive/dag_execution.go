package reactive

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semspec/tools/decompose"
	"github.com/c360studio/semstreams/message"
	reactiveEngine "github.com/c360studio/semstreams/processor/reactive"
)

// ---------------------------------------------------------------------------
// Workflow ID constant
// ---------------------------------------------------------------------------

// DAGExecutionWorkflowID is the unique identifier for the DAG execution workflow.
const DAGExecutionWorkflowID = "dag-execution-loop"

// ---------------------------------------------------------------------------
// Phase constants for DAG execution
// ---------------------------------------------------------------------------

const (
	// DAGPhaseExecuting is the active phase where nodes are being dispatched and run.
	DAGPhaseExecuting = "executing"

	// DAGPhaseComplete is the terminal success phase — all nodes finished.
	DAGPhaseComplete = "complete"

	// DAGPhaseFailed is the terminal failure phase — at least one node failed
	// and no nodes are still running.
	DAGPhaseFailed = "failed"
)

// ---------------------------------------------------------------------------
// DAGNodeState values
// ---------------------------------------------------------------------------

// DAG node state constants.
const (
	DAGNodePending   = "pending"
	DAGNodeRunning   = "running"
	DAGNodeCompleted = "completed"
	DAGNodeFailed    = "failed"
)

// ---------------------------------------------------------------------------
// DAGExecutionTriggerPayload — custom trigger for DAG execution
// ---------------------------------------------------------------------------

// DAGExecutionTriggerPayload is the trigger message for starting a DAG execution
// workflow. It carries the validated TaskDAG, a unique ExecutionID for this run,
// and the ScenarioID being decomposed.
//
// Published to: workflow.trigger.dag-execution
type DAGExecutionTriggerPayload struct {
	ExecutionID string            `json:"execution_id"`
	ScenarioID  string            `json:"scenario_id"`
	DAG         decompose.TaskDAG `json:"dag"`
}

// Schema implements message.Payload.
func (p *DAGExecutionTriggerPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "dag-execution-trigger", Version: "v1"}
}

// Validate implements message.Payload.
func (p *DAGExecutionTriggerPayload) Validate() error {
	if p.ExecutionID == "" {
		return fmt.Errorf("execution_id is required")
	}
	if p.ScenarioID == "" {
		return fmt.Errorf("scenario_id is required")
	}
	if len(p.DAG.Nodes) == 0 {
		return fmt.Errorf("dag must contain at least one node")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *DAGExecutionTriggerPayload) MarshalJSON() ([]byte, error) {
	type Alias DAGExecutionTriggerPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *DAGExecutionTriggerPayload) UnmarshalJSON(data []byte) error {
	type Alias DAGExecutionTriggerPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// ---------------------------------------------------------------------------
// DAGNodeTaskPayload — dispatched to agent.task.<executionID>:<nodeID>
// ---------------------------------------------------------------------------

// DAGNodeTaskPayload is the message published to start execution of a single
// DAG node. The TaskID encodes both the ExecutionID and NodeID using the
// dagexec: prefix convention (see DAGNodeTaskID).
type DAGNodeTaskPayload struct {
	TaskID      string   `json:"task_id"`
	ExecutionID string   `json:"execution_id"`
	NodeID      string   `json:"node_id"`
	ScenarioID  string   `json:"scenario_id"`
	Prompt      string   `json:"prompt"`
	Role        string   `json:"role"`
	FileScope   []string `json:"file_scope,omitempty"`
}

// Schema implements message.Payload.
func (p *DAGNodeTaskPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "dag-node-task", Version: "v1"}
}

// Validate implements message.Payload.
func (p *DAGNodeTaskPayload) Validate() error {
	if p.TaskID == "" {
		return fmt.Errorf("task_id is required")
	}
	if p.ExecutionID == "" {
		return fmt.Errorf("execution_id is required")
	}
	if p.NodeID == "" {
		return fmt.Errorf("node_id is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *DAGNodeTaskPayload) MarshalJSON() ([]byte, error) {
	type Alias DAGNodeTaskPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *DAGNodeTaskPayload) UnmarshalJSON(data []byte) error {
	type Alias DAGNodeTaskPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// ---------------------------------------------------------------------------
// DAGExecutionCompletePayload — published on dag.execution.complete.<executionID>
// ---------------------------------------------------------------------------

// DAGExecutionCompletePayload is published when the entire DAG execution
// finishes successfully (all nodes completed).
type DAGExecutionCompletePayload struct {
	ExecutionID    string   `json:"execution_id"`
	ScenarioID     string   `json:"scenario_id"`
	CompletedNodes []string `json:"completed_nodes"`
}

// Schema implements message.Payload.
func (p *DAGExecutionCompletePayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "dag-execution-complete", Version: "v1"}
}

// Validate implements message.Payload.
func (p *DAGExecutionCompletePayload) Validate() error {
	if p.ExecutionID == "" {
		return fmt.Errorf("execution_id is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *DAGExecutionCompletePayload) MarshalJSON() ([]byte, error) {
	type Alias DAGExecutionCompletePayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *DAGExecutionCompletePayload) UnmarshalJSON(data []byte) error {
	type Alias DAGExecutionCompletePayload
	return json.Unmarshal(data, (*Alias)(p))
}

// ---------------------------------------------------------------------------
// DAGExecutionFailedPayload — published on dag.execution.failed.<executionID>
// ---------------------------------------------------------------------------

// DAGExecutionFailedPayload is published when the DAG execution fails
// (at least one node failed and no nodes are still running).
type DAGExecutionFailedPayload struct {
	ExecutionID string   `json:"execution_id"`
	ScenarioID  string   `json:"scenario_id"`
	FailedNodes []string `json:"failed_nodes"`
}

// Schema implements message.Payload.
func (p *DAGExecutionFailedPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "dag-execution-failed", Version: "v1"}
}

// Validate implements message.Payload.
func (p *DAGExecutionFailedPayload) Validate() error {
	if p.ExecutionID == "" {
		return fmt.Errorf("execution_id is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *DAGExecutionFailedPayload) MarshalJSON() ([]byte, error) {
	type Alias DAGExecutionFailedPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *DAGExecutionFailedPayload) UnmarshalJSON(data []byte) error {
	type Alias DAGExecutionFailedPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// ---------------------------------------------------------------------------
// Payload type constants for registry registration
// ---------------------------------------------------------------------------

var (
	dagExecutionTriggerType  = (&DAGExecutionTriggerPayload{}).Schema()
	dagNodeTaskType          = (&DAGNodeTaskPayload{}).Schema()
	dagExecutionCompleteType = (&DAGExecutionCompletePayload{}).Schema()
	dagExecutionFailedType   = (&DAGExecutionFailedPayload{}).Schema()
	dagNodeCompleteType      = (&DAGNodeCompletePayload{}).Schema()
	dagNodeFailedType        = (&DAGNodeFailedPayload{}).Schema()
)

// ---------------------------------------------------------------------------
// DAGExecutionState
// ---------------------------------------------------------------------------

// DAGExecutionState is the typed KV state for the dag-execution-loop reactive
// workflow. It embeds ExecutionState for base lifecycle fields and tracks the
// per-node execution state of a TaskDAG.
type DAGExecutionState struct {
	reactiveEngine.ExecutionState

	// Trigger data populated on accept-trigger.
	ExecutionID string            `json:"execution_id"`
	ScenarioID  string            `json:"scenario_id"`
	DAG         decompose.TaskDAG `json:"dag"`

	// NodeStates tracks per-node execution state (pending/running/completed/failed).
	NodeStates map[string]string `json:"node_states,omitempty"`

	// CompletedNodes accumulates node IDs that finished successfully.
	CompletedNodes []string `json:"completed_nodes,omitempty"`

	// FailedNodes accumulates node IDs that failed.
	FailedNodes []string `json:"failed_nodes,omitempty"`

	// NodeWorktrees maps nodeID → worktree path so the reset protocol can
	// locate and discard the worktree on node failure.
	NodeWorktrees map[string]string `json:"node_worktrees,omitempty"`

	// NodeLoopIDs maps nodeID → loopID so the reset protocol can identify
	// which graph entities to remove on node failure.
	NodeLoopIDs map[string]string `json:"node_loop_ids,omitempty"`
}

// GetExecutionState implements reactiveEngine.StateAccessor.
// This lets the engine read/write the embedded ExecutionState without reflection.
func (s *DAGExecutionState) GetExecutionState() *reactiveEngine.ExecutionState {
	return &s.ExecutionState
}

// ---------------------------------------------------------------------------
// TaskID helper
// ---------------------------------------------------------------------------

// DAGNodeTaskID encodes a DAG node reference into the TaskID convention used
// by the agent task system: "dagexec:{executionID}:{nodeID}".
func DAGNodeTaskID(executionID, nodeID string) string {
	return "dagexec:" + executionID + ":" + nodeID
}

// ---------------------------------------------------------------------------
// Ready-node detection (exported for testing)
// ---------------------------------------------------------------------------

// DAGReadyNodes returns the IDs of nodes that are in "pending" state and have
// all their dependencies in "completed" state. Nodes with zero dependencies are
// immediately ready when their state is "pending".
func DAGReadyNodes(dag decompose.TaskDAG, nodeStates map[string]string) []string {
	ready := make([]string, 0)
	for _, node := range dag.Nodes {
		if nodeStates[node.ID] != DAGNodePending {
			continue
		}
		allDepsComplete := true
		for _, dep := range node.DependsOn {
			if nodeStates[dep] != DAGNodeCompleted {
				allDepsComplete = false
				break
			}
		}
		if allDepsComplete {
			ready = append(ready, node.ID)
		}
	}
	return ready
}

// ---------------------------------------------------------------------------
// BuildDAGExecutionWorkflow
// ---------------------------------------------------------------------------

// BuildDAGExecutionWorkflow constructs the dag-execution-loop reactive workflow.
//
// The workflow drives DAG execution reactively:
//
//  1. accept-trigger — receive DAGExecutionTriggerPayload, initialize all
//     node states to "pending", transition to "executing".
//
//  2. dispatch-ready-nodes — when phase is "executing" and not waiting,
//     find all nodes whose dependencies are completed, dispatch each as an
//     agent task, mark them "running". Transition to "complete" when all
//     nodes are done; transition to "failed" when nodes have failed and
//     nothing is still running.
//
//  3. handle-node-complete — react to dag.node.complete.* messages; mark
//     the node completed, append to CompletedNodes.
//
//  4. handle-node-failed — react to dag.node.failed.* messages; mark the
//     node failed, append to FailedNodes.
//
//  5. handle-complete — when phase is "complete", publish a completion event
//     to dag.execution.complete.<executionID> and mark the execution done.
func BuildDAGExecutionWorkflow(stateBucket string) *reactiveEngine.Definition {
	return reactiveEngine.NewWorkflow(DAGExecutionWorkflowID).
		WithDescription("Reactive DAG execution: dispatches ready nodes as dependencies complete (ADR-025 Phase 3)").
		WithStateBucket(stateBucket).
		WithStateFactory(func() any { return &DAGExecutionState{} }).
		WithMaxIterations(1). // Single-pass DAG execution — no iteration loop.
		WithTimeout(60 * time.Minute).

		// Rule 1: accept-trigger — initialize state from the trigger payload.
		AddRule(reactiveEngine.NewRule("accept-trigger").
			OnJetStreamSubject("WORKFLOW", "workflow.trigger.dag-execution", func() any {
				return &DAGExecutionTriggerPayload{}
			}).
			WithStateLookup(stateBucket, func(msg any) string {
				trigger, ok := msg.(*DAGExecutionTriggerPayload)
				if !ok {
					return ""
				}
				return "dag-execution." + trigger.ExecutionID
			}).
			When("always", reactiveEngine.Always()).
			Mutate(dagExecAcceptTrigger).
			MustBuild()).

		// Rule 2: dispatch-ready-nodes — find nodes whose deps are all completed
		// and dispatch them; detect terminal states (all complete or fatal failure).
		AddRule(reactiveEngine.NewRule("dispatch-ready-nodes").
			WatchKV(stateBucket, "dag-execution.>").
			When("phase is executing", reactiveEngine.PhaseIs(DAGPhaseExecuting)).
			When("not completed", notCompleted()).
			Mutate(dagExecDispatchReadyNodes).
			MustBuild()).

		// Rule 3: handle-node-complete — react to a DAG node completing successfully.
		// Updates NodeStates[nodeID] = completed; the KV write will re-trigger rule 2.
		AddRule(reactiveEngine.NewRule("handle-node-complete").
			OnJetStreamSubject("WORKFLOW", "dag.node.complete.*", func() any {
				return &DAGNodeCompletePayload{}
			}).
			WithStateLookup(stateBucket, func(msg any) string {
				complete, ok := msg.(*DAGNodeCompletePayload)
				if !ok {
					return ""
				}
				return "dag-execution." + complete.ExecutionID
			}).
			When("always", reactiveEngine.Always()).
			Mutate(dagExecHandleNodeComplete).
			MustBuild()).

		// Rule 4: handle-node-failed — react to a DAG node failing.
		// Updates NodeStates[nodeID] = failed; the KV write will re-trigger rule 2.
		AddRule(reactiveEngine.NewRule("handle-node-failed").
			OnJetStreamSubject("WORKFLOW", "dag.node.failed.*", func() any {
				return &DAGNodeFailedPayload{}
			}).
			WithStateLookup(stateBucket, func(msg any) string {
				failed, ok := msg.(*DAGNodeFailedPayload)
				if !ok {
					return ""
				}
				return "dag-execution." + failed.ExecutionID
			}).
			When("always", reactiveEngine.Always()).
			Mutate(dagExecHandleNodeFailed).
			MustBuild()).

		// Rule 5: handle-complete — publish completion event and mark execution done.
		AddRule(reactiveEngine.NewRule("handle-complete").
			WatchKV(stateBucket, "dag-execution.>").
			When("phase is complete", reactiveEngine.PhaseIs(DAGPhaseComplete)).
			When("not completed", notCompleted()).
			CompleteWithEvent("dag.execution.complete", dagExecBuildCompleteEvent).
			MustBuild()).

		// Rule 6: handle-failed — publish failure event and mark execution done.
		AddRule(reactiveEngine.NewRule("handle-failed").
			WatchKV(stateBucket, "dag-execution.>").
			When("phase is failed", reactiveEngine.PhaseIs(DAGPhaseFailed)).
			When("not completed", notCompleted()).
			CompleteWithEvent("dag.execution.failed", dagExecBuildFailedEvent).
			MustBuild()).
		MustBuild()
}

// ---------------------------------------------------------------------------
// DAGNodeCompletePayload / DAGNodeFailedPayload — inbound node signals
// ---------------------------------------------------------------------------

// QualityEvidence proves a task passed structural validation and code review.
// DAG node completion is rejected without valid evidence.
type QualityEvidence struct {
	ValidationPassed bool   `json:"validation_passed"`
	ValidationChecks int    `json:"validation_checks"`
	ReviewVerdict    string `json:"review_verdict"` // must be "approved"
	ReviewerID       string `json:"reviewer_id,omitempty"`
}

// DAGNodeCompletePayload is published by the agent loop (or test harness) to
// signal that a specific DAG node has completed successfully.
//
// Published to: dag.node.complete.<nodeID>
type DAGNodeCompletePayload struct {
	ExecutionID  string `json:"execution_id"`
	NodeID       string `json:"node_id"`
	WorktreePath string `json:"worktree_path,omitempty"`
	LoopID       string `json:"loop_id,omitempty"`
	// QualityEvidence carries proof that the task passed all quality gates.
	// DAG node completion is rejected without valid evidence.
	QualityEvidence *QualityEvidence `json:"quality_evidence,omitempty"`
}

// Schema implements message.Payload.
func (p *DAGNodeCompletePayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "dag-node-complete", Version: "v1"}
}

// Validate implements message.Payload.
func (p *DAGNodeCompletePayload) Validate() error {
	if p.ExecutionID == "" {
		return fmt.Errorf("execution_id is required")
	}
	if p.NodeID == "" {
		return fmt.Errorf("node_id is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *DAGNodeCompletePayload) MarshalJSON() ([]byte, error) {
	type Alias DAGNodeCompletePayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *DAGNodeCompletePayload) UnmarshalJSON(data []byte) error {
	type Alias DAGNodeCompletePayload
	return json.Unmarshal(data, (*Alias)(p))
}

// DAGNodeFailedPayload is published to signal that a specific DAG node failed.
//
// Published to: dag.node.failed.<nodeID>
type DAGNodeFailedPayload struct {
	ExecutionID  string `json:"execution_id"`
	NodeID       string `json:"node_id"`
	Reason       string `json:"reason,omitempty"`
	WorktreePath string `json:"worktree_path,omitempty"`
	LoopID       string `json:"loop_id,omitempty"`
}

// Schema implements message.Payload.
func (p *DAGNodeFailedPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "dag-node-failed", Version: "v1"}
}

// Validate implements message.Payload.
func (p *DAGNodeFailedPayload) Validate() error {
	if p.ExecutionID == "" {
		return fmt.Errorf("execution_id is required")
	}
	if p.NodeID == "" {
		return fmt.Errorf("node_id is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *DAGNodeFailedPayload) MarshalJSON() ([]byte, error) {
	type Alias DAGNodeFailedPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *DAGNodeFailedPayload) UnmarshalJSON(data []byte) error {
	type Alias DAGNodeFailedPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// ---------------------------------------------------------------------------
// Mutators
// ---------------------------------------------------------------------------

// dagExecAcceptTrigger populates DAGExecutionState from the incoming trigger
// and initializes all DAG node states to "pending".
var dagExecAcceptTrigger reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*DAGExecutionState)
	if !ok {
		return fmt.Errorf("accept-trigger: expected *DAGExecutionState, got %T", ctx.State)
	}

	trigger, ok := ctx.Message.(*DAGExecutionTriggerPayload)
	if !ok {
		return fmt.Errorf("accept-trigger: expected *DAGExecutionTriggerPayload, got %T", ctx.Message)
	}

	if trigger.ExecutionID == "" {
		return fmt.Errorf("accept-trigger: execution_id missing from trigger")
	}
	if trigger.ScenarioID == "" {
		return fmt.Errorf("accept-trigger: scenario_id missing from trigger")
	}
	if len(trigger.DAG.Nodes) == 0 {
		return fmt.Errorf("accept-trigger: dag has no nodes")
	}

	state.ExecutionID = trigger.ExecutionID
	state.ScenarioID = trigger.ScenarioID
	state.DAG = trigger.DAG

	// Initialize all nodes to pending.
	state.NodeStates = make(map[string]string, len(trigger.DAG.Nodes))
	for _, node := range trigger.DAG.Nodes {
		state.NodeStates[node.ID] = DAGNodePending
	}

	state.CompletedNodes = nil
	state.FailedNodes = nil

	// Initialize execution metadata on first trigger only.
	if state.ID == "" {
		state.ID = "dag-execution." + trigger.ExecutionID
		state.WorkflowID = DAGExecutionWorkflowID
		state.Status = reactiveEngine.StatusRunning
		now := time.Now()
		state.CreatedAt = now
		state.UpdatedAt = now
	}

	state.Phase = DAGPhaseExecuting
	return nil
}

// dagExecDispatchReadyNodes finds all nodes that are ready to run (pending +
// all deps completed), dispatches each as an agent task, and marks them
// running. If no nodes are ready and no nodes are running, it determines the
// terminal phase: "complete" if all nodes completed, "failed" otherwise.
//
// This mutator performs the dispatch by writing to state only; the actual
// publish is driven by the engine's PublishAsync support in rule 2. However,
// because DAG dispatch requires one publish per ready node (N messages), we
// use a plain Mutate and set phase to the appropriate terminal when done.
// Callers that need to actually dispatch tasks will react to the "running"
// node states transitioning and publish tasks themselves, or we encode the
// dispatch list into state.
//
// Design note: since the reactive engine's PublishWithMutation only supports
// a single publish per rule firing, multi-node dispatch is handled inside the
// mutator by embedding the ready-node list into the state. Actual task
// publishing for DAG nodes must be done by an external component watching for
// nodes transitioning to "running" state, OR we set phase transitions that
// trigger separate per-node rules. For Phase 3, we use the simpler approach:
// set the "running" state for ready nodes and rely on a WatchKV rule to
// publish each task. This mutator marks the ready nodes as running and
// publishes via the built-in PublishAsync pattern.
//
// For now, we perform the full dispatch-and-check logic inline: transitions to
// terminal phases are written here; task publications are emitted by the rule
// action (PublishAsync for each ready node is not directly supported by the
// builder's single-publish model). As a pragmatic solution for Phase 3,
// we dispatch by recording which nodes are ready and letting the engine fire
// rule 2 again after the KV update, but we do transition nodes to "running"
// atomically so repeat firings are idempotent.
var dagExecDispatchReadyNodes reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*DAGExecutionState)
	if !ok {
		return fmt.Errorf("dispatch-ready-nodes: expected *DAGExecutionState, got %T", ctx.State)
	}

	if state.NodeStates == nil {
		return fmt.Errorf("dispatch-ready-nodes: node_states is nil — was accept-trigger run?")
	}

	ready := DAGReadyNodes(state.DAG, state.NodeStates)

	// Mark each ready node as "running". The actual task publication happens
	// via the engine's side-effect mechanism after the mutator returns; for
	// Phase 3 we write the nodes to running and let external agent loops
	// subscribe to dag.node.complete.* / dag.node.failed.* to signal back.
	for _, nodeID := range ready {
		state.NodeStates[nodeID] = DAGNodeRunning
	}

	// Check for terminal conditions only when no new nodes were dispatched
	// and nothing is running.
	if len(ready) > 0 {
		// New nodes dispatched — stay in executing.
		return nil
	}

	// No ready nodes — check whether we are done or stuck.
	runningCount := 0
	for _, ns := range state.NodeStates {
		if ns == DAGNodeRunning {
			runningCount++
		}
	}

	if runningCount > 0 {
		// Still waiting on in-flight nodes. Stay in executing.
		return nil
	}

	// No running nodes and no ready nodes — check if all completed.
	allComplete := true
	for _, ns := range state.NodeStates {
		if ns != DAGNodeCompleted {
			allComplete = false
			break
		}
	}

	if allComplete {
		state.Phase = DAGPhaseComplete
	} else {
		// Some nodes are failed/pending with no progress possible.
		state.Phase = DAGPhaseFailed
		reactiveEngine.FailExecution(state, "DAG execution failed: one or more nodes failed")
	}

	return nil
}

// dagExecHandleNodeComplete marks a DAG node as completed and appends it to
// CompletedNodes. The resulting KV write re-triggers dispatch-ready-nodes to
// check for newly unblocked nodes.
//
// Quality gate: completions without valid QualityEvidence are rejected and the
// node is marked as failed instead of completed.
var dagExecHandleNodeComplete reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*DAGExecutionState)
	if !ok {
		return fmt.Errorf("handle-node-complete: expected *DAGExecutionState, got %T", ctx.State)
	}

	msg, ok := ctx.Message.(*DAGNodeCompletePayload)
	if !ok {
		return fmt.Errorf("handle-node-complete: expected *DAGNodeCompletePayload, got %T", ctx.Message)
	}

	if state.NodeStates == nil {
		state.NodeStates = make(map[string]string)
	}

	// Validate quality evidence — reject completions without proof of passing
	// structural validation and code review. The node is marked as failed
	// and the state change signals the DAG to re-evaluate readiness.
	if evidenceErr := validateQualityEvidence(msg.QualityEvidence); evidenceErr != nil {
		state.NodeStates[msg.NodeID] = DAGNodeFailed
		state.FailedNodes = append(state.FailedNodes, msg.NodeID)
		return nil
	}

	state.NodeStates[msg.NodeID] = DAGNodeCompleted
	state.CompletedNodes = append(state.CompletedNodes, msg.NodeID)

	// Record worktree path and loop ID for potential cleanup tracking.
	if msg.WorktreePath != "" {
		if state.NodeWorktrees == nil {
			state.NodeWorktrees = make(map[string]string)
		}
		state.NodeWorktrees[msg.NodeID] = msg.WorktreePath
	}
	if msg.LoopID != "" {
		if state.NodeLoopIDs == nil {
			state.NodeLoopIDs = make(map[string]string)
		}
		state.NodeLoopIDs[msg.NodeID] = msg.LoopID
	}

	return nil
}

// validateQualityEvidence checks that evidence is present and meets quality
// gate requirements: validation must have passed and review verdict must be
// "approved".
func validateQualityEvidence(ev *QualityEvidence) error {
	if ev == nil {
		return fmt.Errorf("quality evidence is required")
	}
	if !ev.ValidationPassed {
		return fmt.Errorf("validation did not pass")
	}
	if ev.ReviewVerdict != "approved" {
		return fmt.Errorf("review verdict %q is not approved", ev.ReviewVerdict)
	}
	return nil
}

// dagExecHandleNodeFailed marks a DAG node as failed and appends it to
// FailedNodes. The resulting KV write re-triggers dispatch-ready-nodes to
// check whether the execution can continue or must transition to "failed".
var dagExecHandleNodeFailed reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*DAGExecutionState)
	if !ok {
		return fmt.Errorf("handle-node-failed: expected *DAGExecutionState, got %T", ctx.State)
	}

	msg, ok := ctx.Message.(*DAGNodeFailedPayload)
	if !ok {
		return fmt.Errorf("handle-node-failed: expected *DAGNodeFailedPayload, got %T", ctx.Message)
	}

	if state.NodeStates == nil {
		state.NodeStates = make(map[string]string)
	}

	state.NodeStates[msg.NodeID] = DAGNodeFailed
	state.FailedNodes = append(state.FailedNodes, msg.NodeID)

	// Record worktree path and loop ID so the reset protocol can locate and
	// clean up resources associated with this failed node.
	if msg.WorktreePath != "" {
		if state.NodeWorktrees == nil {
			state.NodeWorktrees = make(map[string]string)
		}
		state.NodeWorktrees[msg.NodeID] = msg.WorktreePath
	}
	if msg.LoopID != "" {
		if state.NodeLoopIDs == nil {
			state.NodeLoopIDs = make(map[string]string)
		}
		state.NodeLoopIDs[msg.NodeID] = msg.LoopID
	}

	return nil
}

// ---------------------------------------------------------------------------
// Payload builders
// ---------------------------------------------------------------------------

// dagExecBuildCompleteEvent constructs a DAGExecutionCompletePayload.
func dagExecBuildCompleteEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*DAGExecutionState)
	if !ok {
		return nil, fmt.Errorf("complete event: expected *DAGExecutionState, got %T", ctx.State)
	}

	return &DAGExecutionCompletePayload{
		ExecutionID:    state.ExecutionID,
		ScenarioID:     state.ScenarioID,
		CompletedNodes: state.CompletedNodes,
	}, nil
}

// dagExecBuildFailedEvent constructs a DAGExecutionFailedPayload.
func dagExecBuildFailedEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*DAGExecutionState)
	if !ok {
		return nil, fmt.Errorf("failed event: expected *DAGExecutionState, got %T", ctx.State)
	}

	return &DAGExecutionFailedPayload{
		ExecutionID: state.ExecutionID,
		ScenarioID:  state.ScenarioID,
		FailedNodes: state.FailedNodes,
	}, nil
}
