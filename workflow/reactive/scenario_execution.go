package reactive

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/c360studio/semspec/tools/decompose"
	"github.com/c360studio/semstreams/message"
	reactiveEngine "github.com/c360studio/semstreams/processor/reactive"
)

// ---------------------------------------------------------------------------
// Workflow ID constant
// ---------------------------------------------------------------------------

// ScenarioExecutionWorkflowID is the unique identifier for the scenario execution workflow.
const ScenarioExecutionWorkflowID = "scenario-execution-loop"

// ---------------------------------------------------------------------------
// Phase constants for scenario execution
// ---------------------------------------------------------------------------

const (
	// ScenarioPhaseDecomposing is the phase where the LLM decomposes the scenario
	// into a TaskDAG via the decompose_task tool.
	ScenarioPhaseDecomposing = "decomposing"

	// ScenarioPhaseDecomposed is the transient phase set after the decompose request
	// is dispatched. The dispatch-decompose-dispatched rule prevents re-firing.
	ScenarioPhaseDecomposed = "decomposed"

	// ScenarioPhaseExecuting is the phase where the DAG execution workflow runs.
	ScenarioPhaseExecuting = "executing"

	// ScenarioPhaseComplete is the terminal success phase — DAG completed.
	ScenarioPhaseComplete = "complete"

	// ScenarioPhaseFailed is the terminal failure phase — DAG failed or error.
	ScenarioPhaseFailed = "failed"
)

// ---------------------------------------------------------------------------
// ScenarioExecutionTriggerPayload — trigger for the scenario-execution-loop
// ---------------------------------------------------------------------------

// ScenarioExecutionTriggerPayload is the trigger message for starting a scenario
// execution workflow. It carries the ScenarioID, the decomposition prompt, the
// agent role, and the model to use for decomposition.
//
// Published to: workflow.trigger.scenario-execution-loop
type ScenarioExecutionTriggerPayload struct {
	ScenarioID string `json:"scenario_id"`
	Prompt     string `json:"prompt"`
	Role       string `json:"role,omitempty"`
	Model      string `json:"model,omitempty"`
	TraceID    string `json:"trace_id,omitempty"`
}

// Schema implements message.Payload.
func (p *ScenarioExecutionTriggerPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "scenario-execution-trigger", Version: "v1"}
}

// Validate implements message.Payload.
func (p *ScenarioExecutionTriggerPayload) Validate() error {
	if p.ScenarioID == "" {
		return fmt.Errorf("scenario_id is required")
	}
	if p.Prompt == "" {
		return fmt.Errorf("prompt is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *ScenarioExecutionTriggerPayload) MarshalJSON() ([]byte, error) {
	type Alias ScenarioExecutionTriggerPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *ScenarioExecutionTriggerPayload) UnmarshalJSON(data []byte) error {
	type Alias ScenarioExecutionTriggerPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// ---------------------------------------------------------------------------
// ScenarioDecomposeRequest — dispatched to the agentic loop for decomposition
// ---------------------------------------------------------------------------

// ScenarioDecomposeRequest is published to trigger the agentic loop that calls
// the decompose_task tool. The agentic loop runs the LLM, which calls
// decompose_task to produce a TaskDAG, and publishes the result back on
// scenario.decomposed.<scenarioID>.
//
// Published to: workflow.async.scenario-decomposer
type ScenarioDecomposeRequest struct {
	ExecutionID string `json:"execution_id"`
	ScenarioID  string `json:"scenario_id"`
	Prompt      string `json:"prompt"`
	Role        string `json:"role,omitempty"`
	Model       string `json:"model,omitempty"`
	TraceID     string `json:"trace_id,omitempty"`
}

// Schema implements message.Payload.
func (r *ScenarioDecomposeRequest) Schema() message.Type {
	return ScenarioDecomposeRequestType
}

// Validate implements message.Payload.
func (r *ScenarioDecomposeRequest) Validate() error {
	if r.ScenarioID == "" {
		return fmt.Errorf("scenario_id is required")
	}
	if r.Prompt == "" {
		return fmt.Errorf("prompt is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *ScenarioDecomposeRequest) MarshalJSON() ([]byte, error) {
	type Alias ScenarioDecomposeRequest
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *ScenarioDecomposeRequest) UnmarshalJSON(data []byte) error {
	type Alias ScenarioDecomposeRequest
	return json.Unmarshal(data, (*Alias)(r))
}

// ScenarioDecomposeRequestType is the message type for scenario decompose requests.
var ScenarioDecomposeRequestType = message.Type{
	Domain:   "workflow",
	Category: "scenario-decompose-request",
	Version:  "v1",
}

// ---------------------------------------------------------------------------
// ScenarioDecomposedPayload — result of successful decomposition
// ---------------------------------------------------------------------------

// ScenarioDecomposedPayload is published by the agentic loop (or test harness)
// to signal that decomposition is complete and carries the resulting TaskDAG.
//
// Published to: scenario.decomposed.<scenarioID>
type ScenarioDecomposedPayload struct {
	ExecutionID string            `json:"execution_id"`
	ScenarioID  string            `json:"scenario_id"`
	DAG         decompose.TaskDAG `json:"dag"`
}

// Schema implements message.Payload.
func (p *ScenarioDecomposedPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "scenario-decomposed", Version: "v1"}
}

// Validate implements message.Payload.
func (p *ScenarioDecomposedPayload) Validate() error {
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
func (p *ScenarioDecomposedPayload) MarshalJSON() ([]byte, error) {
	type Alias ScenarioDecomposedPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *ScenarioDecomposedPayload) UnmarshalJSON(data []byte) error {
	type Alias ScenarioDecomposedPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// ---------------------------------------------------------------------------
// ScenarioCompletePayload — terminal success event
// ---------------------------------------------------------------------------

// ScenarioCompletePayload is published when the scenario execution completes
// successfully (all DAG nodes passed).
//
// Published to: scenario.complete.<scenarioID>
type ScenarioCompletePayload struct {
	ScenarioID     string   `json:"scenario_id"`
	DAGExecutionID string   `json:"dag_execution_id"`
	CompletedNodes []string `json:"completed_nodes,omitempty"`
}

// Schema implements message.Payload.
func (p *ScenarioCompletePayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "scenario-complete", Version: "v1"}
}

// Validate implements message.Payload.
func (p *ScenarioCompletePayload) Validate() error {
	if p.ScenarioID == "" {
		return fmt.Errorf("scenario_id is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *ScenarioCompletePayload) MarshalJSON() ([]byte, error) {
	type Alias ScenarioCompletePayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *ScenarioCompletePayload) UnmarshalJSON(data []byte) error {
	type Alias ScenarioCompletePayload
	return json.Unmarshal(data, (*Alias)(p))
}

// ---------------------------------------------------------------------------
// ScenarioFailedPayload — terminal failure event
// ---------------------------------------------------------------------------

// ScenarioFailedPayload is published when the scenario execution fails (at least
// one DAG node failed, or decomposition failed).
//
// Published to: scenario.failed.<scenarioID>
type ScenarioFailedPayload struct {
	ScenarioID     string   `json:"scenario_id"`
	DAGExecutionID string   `json:"dag_execution_id,omitempty"`
	FailedNodes    []string `json:"failed_nodes,omitempty"`
	Reason         string   `json:"reason,omitempty"`

	// RequeueAsUnmet instructs the scenario-orchestrator to re-queue this
	// scenario as unmet so it is eligible for fresh decomposition. Set when
	// the DAG execution failed and the failure is believed to be recoverable
	// by re-planning.
	RequeueAsUnmet bool `json:"requeue_as_unmet"`
}

// Schema implements message.Payload.
func (p *ScenarioFailedPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "scenario-failed", Version: "v1"}
}

// Validate implements message.Payload.
func (p *ScenarioFailedPayload) Validate() error {
	if p.ScenarioID == "" {
		return fmt.Errorf("scenario_id is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *ScenarioFailedPayload) MarshalJSON() ([]byte, error) {
	type Alias ScenarioFailedPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *ScenarioFailedPayload) UnmarshalJSON(data []byte) error {
	type Alias ScenarioFailedPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// ---------------------------------------------------------------------------
// Payload type constants for registry registration
// ---------------------------------------------------------------------------

var (
	scenarioExecutionTriggerType = (&ScenarioExecutionTriggerPayload{}).Schema()
	scenarioDecomposeRequestType = ScenarioDecomposeRequestType
	scenarioDecomposedType       = (&ScenarioDecomposedPayload{}).Schema()
	scenarioCompleteType         = (&ScenarioCompletePayload{}).Schema()
	scenarioFailedType           = (&ScenarioFailedPayload{}).Schema()
)

// ---------------------------------------------------------------------------
// ScenarioExecutionState
// ---------------------------------------------------------------------------

// ScenarioExecutionState is the typed KV state for the scenario-execution-loop
// reactive workflow. It embeds ExecutionState for base lifecycle fields and tracks
// the decomposition and DAG execution progress for a single Scenario.
type ScenarioExecutionState struct {
	reactiveEngine.ExecutionState

	// Trigger data populated on accept-trigger.
	ScenarioID string `json:"scenario_id"`
	Prompt     string `json:"prompt"`
	Role       string `json:"role,omitempty"`
	Model      string `json:"model,omitempty"`
	TraceID    string `json:"trace_id,omitempty"`

	// DAGExecutionID is set after decomposition completes and the DAG execution
	// workflow is triggered. Used to correlate dag.execution.complete.* messages.
	DAGExecutionID string `json:"dag_execution_id,omitempty"`

	// DecomposedDAG is the validated TaskDAG from the decompose_task tool.
	DecomposedDAG decompose.TaskDAG `json:"decomposed_dag,omitempty"`

	// CompletedNodes is populated from the dag.execution.complete event.
	CompletedNodes []string `json:"completed_nodes,omitempty"`

	// FailedNodes is populated from the dag.execution.failed event.
	FailedNodes []string `json:"failed_nodes,omitempty"`

	// RequeueAsUnmet signals that when this scenario transitions to "failed",
	// the scenario-orchestrator should re-queue it as unmet for fresh
	// decomposition on the next planning cycle. Set when the DAG execution
	// fails and recovery via re-decomposition is appropriate.
	RequeueAsUnmet bool `json:"requeue_as_unmet,omitempty"`
}

// GetExecutionState implements reactiveEngine.StateAccessor.
// This lets the engine read/write the embedded ExecutionState without reflection.
func (s *ScenarioExecutionState) GetExecutionState() *reactiveEngine.ExecutionState {
	return &s.ExecutionState
}

// ---------------------------------------------------------------------------
// BuildScenarioExecutionWorkflow
// ---------------------------------------------------------------------------

// BuildScenarioExecutionWorkflow constructs the scenario-execution-loop reactive
// workflow. The workflow drives the full lifecycle of executing a single Scenario:
//
//  1. accept-trigger — receive ScenarioExecutionTriggerPayload, initialize state,
//     transition to "decomposing" phase.
//
//  2. dispatch-decompose — when phase is "decomposing", publish a
//     ScenarioDecomposeRequest to the agentic loop. The agentic loop calls
//     the decompose_task tool (LLM provides the DAG). Transition to "decomposed"
//     to prevent re-firing while awaiting the result.
//
//  3. handle-decomposed — react to scenario.decomposed.* messages; validate the
//     DAG and trigger the dag-execution-loop. Transition to "executing".
//
//  4. handle-dag-complete — react to dag.execution.complete.* messages; store
//     the completed node list and transition to "complete".
//
//  5. handle-dag-failed — react to dag.execution.failed.* messages; store the
//     failed node list and transition to "failed".
//
//  6. handle-complete — when phase is "complete" and not yet terminal, publish
//     a ScenarioCompletePayload and mark the execution done.
//
//  7. handle-failed — when phase is "failed" and not yet terminal, publish a
//     ScenarioFailedPayload and mark the execution done.
func BuildScenarioExecutionWorkflow(stateBucket string) *reactiveEngine.Definition {
	return reactiveEngine.NewWorkflow(ScenarioExecutionWorkflowID).
		WithDescription("Reactive scenario execution: decomposes a Scenario into a TaskDAG and drives execution (ADR-025 Phase 4)").
		WithStateBucket(stateBucket).
		WithStateFactory(func() any { return &ScenarioExecutionState{} }).
		WithMaxIterations(1). // Single-pass: one decompose, one DAG execution.
		WithTimeout(90 * time.Minute).

		// Rule 1: accept-trigger — initialize state from the trigger payload.
		AddRule(reactiveEngine.NewRule("accept-trigger").
			OnJetStreamSubject("WORKFLOW", "workflow.trigger.scenario-execution-loop", func() any {
				return &ScenarioExecutionTriggerPayload{}
			}).
			WithStateLookup(stateBucket, func(msg any) string {
				trigger, ok := msg.(*ScenarioExecutionTriggerPayload)
				if !ok {
					return ""
				}
				return "scenario-execution." + trigger.ScenarioID
			}).
			When("always", reactiveEngine.Always()).
			Mutate(scenarioExecAcceptTrigger).
			MustBuild()).

		// Rule 2: dispatch-decompose — send decompose request to the agentic loop.
		// Fires once when phase transitions to "decomposing".
		// PublishWithMutation sends the request and transitions phase to "decomposed"
		// atomically, preventing duplicate dispatches on re-watch.
		AddRule(reactiveEngine.NewRule("dispatch-decompose").
			WatchKV(stateBucket, "scenario-execution.>").
			When("phase is decomposing", reactiveEngine.PhaseIs(ScenarioPhaseDecomposing)).
			PublishWithMutation("workflow.async.scenario-decomposer", scenarioExecBuildDecomposeRequest, setPhase(ScenarioPhaseDecomposed)).
			MustBuild()).

		// Rule 3: handle-decomposed — react to the decompose result arriving on
		// scenario.decomposed.*, validate the DAG, and trigger the DAG execution
		// workflow by publishing a DAGExecutionTriggerPayload.
		// PublishWithMutation ensures the DAG trigger is published atomically
		// with the phase transition to "executing".
		AddRule(reactiveEngine.NewRule("handle-decomposed").
			OnJetStreamSubject("WORKFLOW", "scenario.decomposed.*", func() any {
				return &ScenarioDecomposedPayload{}
			}).
			WithStateLookup(stateBucket, func(msg any) string {
				result, ok := msg.(*ScenarioDecomposedPayload)
				if !ok {
					return ""
				}
				return "scenario-execution." + result.ScenarioID
			}).
			When("always", reactiveEngine.Always()).
			PublishWithMutation("workflow.trigger.dag-execution", scenarioExecBuildDAGTrigger, scenarioExecHandleDecomposed).
			MustBuild()).

		// Rule 4: handle-dag-complete — react to dag.execution.complete.* from the
		// DAG execution workflow. Transitions to "complete" phase.
		AddRule(reactiveEngine.NewRule("handle-dag-complete").
			OnJetStreamSubject("WORKFLOW", "dag.execution.complete.*", func() any {
				return &DAGExecutionCompletePayload{}
			}).
			WithStateLookup(stateBucket, func(msg any) string {
				complete, ok := msg.(*DAGExecutionCompletePayload)
				if !ok {
					return ""
				}
				// The DAGExecutionCompletePayload carries ScenarioID to route
				// back to the correct scenario-execution state entry.
				return "scenario-execution." + complete.ScenarioID
			}).
			When("always", reactiveEngine.Always()).
			Mutate(scenarioExecHandleDAGComplete).
			MustBuild()).

		// Rule 5: handle-dag-failed — react to dag.execution.failed.* from the
		// DAG execution workflow. Transitions to "failed" phase.
		AddRule(reactiveEngine.NewRule("handle-dag-failed").
			OnJetStreamSubject("WORKFLOW", "dag.execution.failed.*", func() any {
				return &DAGExecutionFailedPayload{}
			}).
			WithStateLookup(stateBucket, func(msg any) string {
				failed, ok := msg.(*DAGExecutionFailedPayload)
				if !ok {
					return ""
				}
				return "scenario-execution." + failed.ScenarioID
			}).
			When("always", reactiveEngine.Always()).
			Mutate(scenarioExecHandleDAGFailed).
			MustBuild()).

		// Rule 6: handle-complete — publish ScenarioCompletePayload and mark done.
		AddRule(reactiveEngine.NewRule("handle-complete").
			WatchKV(stateBucket, "scenario-execution.>").
			When("phase is complete", reactiveEngine.PhaseIs(ScenarioPhaseComplete)).
			When("not completed", notCompleted()).
			CompleteWithEvent("scenario.complete", scenarioExecBuildCompleteEvent).
			MustBuild()).

		// Rule 7: handle-failed — publish ScenarioFailedPayload and mark done.
		AddRule(reactiveEngine.NewRule("handle-failed").
			WatchKV(stateBucket, "scenario-execution.>").
			When("phase is failed", reactiveEngine.PhaseIs(ScenarioPhaseFailed)).
			When("not completed", notCompleted()).
			CompleteWithEvent("scenario.failed", scenarioExecBuildFailedEvent).
			MustBuild()).
		MustBuild()
}

// ---------------------------------------------------------------------------
// Mutators
// ---------------------------------------------------------------------------

// scenarioExecAcceptTrigger populates ScenarioExecutionState from the incoming
// trigger and transitions to the "decomposing" phase.
var scenarioExecAcceptTrigger reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*ScenarioExecutionState)
	if !ok {
		return fmt.Errorf("accept-trigger: expected *ScenarioExecutionState, got %T", ctx.State)
	}

	trigger, ok := ctx.Message.(*ScenarioExecutionTriggerPayload)
	if !ok {
		return fmt.Errorf("accept-trigger: expected *ScenarioExecutionTriggerPayload, got %T", ctx.Message)
	}

	if trigger.ScenarioID == "" {
		return fmt.Errorf("accept-trigger: scenario_id missing from trigger")
	}
	if trigger.Prompt == "" {
		return fmt.Errorf("accept-trigger: prompt missing from trigger")
	}

	state.ScenarioID = trigger.ScenarioID
	state.Prompt = trigger.Prompt
	state.Role = trigger.Role
	state.Model = trigger.Model
	state.TraceID = trigger.TraceID

	// Initialize execution metadata on first trigger only.
	if state.ID == "" {
		state.ID = "scenario-execution." + trigger.ScenarioID
		state.WorkflowID = ScenarioExecutionWorkflowID
		state.Status = reactiveEngine.StatusRunning
		now := time.Now()
		state.CreatedAt = now
		state.UpdatedAt = now
	}

	state.Phase = ScenarioPhaseDecomposing
	return nil
}

// scenarioExecHandleDecomposed receives the decomposition result, validates the DAG,
// generates a unique DAG execution ID, and transitions to "executing".
// The DAG execution workflow is triggered by the PublishWithMutation on Rule 3.
var scenarioExecHandleDecomposed reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*ScenarioExecutionState)
	if !ok {
		return fmt.Errorf("handle-decomposed: expected *ScenarioExecutionState, got %T", ctx.State)
	}

	// Phase guard: ignore late/duplicate decomposition results.
	if state.Phase != ScenarioPhaseDecomposed && state.Phase != ScenarioPhaseDecomposing {
		return nil
	}

	result, ok := ctx.Message.(*ScenarioDecomposedPayload)
	if !ok {
		return fmt.Errorf("handle-decomposed: expected *ScenarioDecomposedPayload, got %T", ctx.Message)
	}

	// Validate the DAG before accepting it.
	if err := result.DAG.Validate(); err != nil {
		reactiveEngine.FailExecution(state, fmt.Sprintf("invalid DAG from decompose_task: %v", err))
		state.Phase = ScenarioPhaseFailed
		return nil
	}

	state.DecomposedDAG = result.DAG
	state.DAGExecutionID = uuid.New().String()

	state.Phase = ScenarioPhaseExecuting
	return nil
}

// scenarioExecHandleDAGComplete receives the DAG completion event and transitions
// the scenario to "complete".
var scenarioExecHandleDAGComplete reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*ScenarioExecutionState)
	if !ok {
		return fmt.Errorf("handle-dag-complete: expected *ScenarioExecutionState, got %T", ctx.State)
	}

	msg, ok := ctx.Message.(*DAGExecutionCompletePayload)
	if !ok {
		return fmt.Errorf("handle-dag-complete: expected *DAGExecutionCompletePayload, got %T", ctx.Message)
	}

	// Ignore stale events from a previous DAG execution.
	if state.DAGExecutionID != "" && msg.ExecutionID != state.DAGExecutionID {
		return nil
	}

	state.CompletedNodes = msg.CompletedNodes
	state.Phase = ScenarioPhaseComplete
	return nil
}

// scenarioExecHandleDAGFailed receives the DAG failure event and transitions
// the scenario to "failed".
var scenarioExecHandleDAGFailed reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*ScenarioExecutionState)
	if !ok {
		return fmt.Errorf("handle-dag-failed: expected *ScenarioExecutionState, got %T", ctx.State)
	}

	msg, ok := ctx.Message.(*DAGExecutionFailedPayload)
	if !ok {
		return fmt.Errorf("handle-dag-failed: expected *DAGExecutionFailedPayload, got %T", ctx.Message)
	}

	// Ignore stale events from a previous DAG execution.
	if state.DAGExecutionID != "" && msg.ExecutionID != state.DAGExecutionID {
		return nil
	}

	state.FailedNodes = msg.FailedNodes
	reactiveEngine.FailExecution(state, fmt.Sprintf("DAG execution failed: %d nodes failed", len(msg.FailedNodes)))
	state.Phase = ScenarioPhaseFailed
	// Signal the scenario-orchestrator to re-queue this scenario as unmet so it
	// is eligible for fresh decomposition on the next planning cycle.
	state.RequeueAsUnmet = true
	return nil
}

// ---------------------------------------------------------------------------
// Payload builders
// ---------------------------------------------------------------------------

// scenarioExecBuildDecomposeRequest constructs a ScenarioDecomposeRequest from state.
// Called by dispatch-decompose rule's PublishWithMutation.
func scenarioExecBuildDecomposeRequest(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*ScenarioExecutionState)
	if !ok {
		return nil, fmt.Errorf("decompose request: expected *ScenarioExecutionState, got %T", ctx.State)
	}

	return &ScenarioDecomposeRequest{
		ExecutionID: state.ID,
		ScenarioID:  state.ScenarioID,
		Prompt:      state.Prompt,
		Role:        state.Role,
		Model:       state.Model,
		TraceID:     state.TraceID,
	}, nil
}

// scenarioExecBuildDAGTrigger constructs a DAGExecutionTriggerPayload from state.
// Called by handle-decomposed rule's PublishWithMutation to start the DAG execution workflow.
func scenarioExecBuildDAGTrigger(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*ScenarioExecutionState)
	if !ok {
		return nil, fmt.Errorf("DAG trigger: expected *ScenarioExecutionState, got %T", ctx.State)
	}

	return &DAGExecutionTriggerPayload{
		ExecutionID: state.DAGExecutionID,
		ScenarioID:  state.ScenarioID,
		DAG:         state.DecomposedDAG,
	}, nil
}

// scenarioExecBuildCompleteEvent constructs a ScenarioCompletePayload.
func scenarioExecBuildCompleteEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*ScenarioExecutionState)
	if !ok {
		return nil, fmt.Errorf("complete event: expected *ScenarioExecutionState, got %T", ctx.State)
	}

	return &ScenarioCompletePayload{
		ScenarioID:     state.ScenarioID,
		DAGExecutionID: state.DAGExecutionID,
		CompletedNodes: state.CompletedNodes,
	}, nil
}

// scenarioExecBuildFailedEvent constructs a ScenarioFailedPayload.
func scenarioExecBuildFailedEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*ScenarioExecutionState)
	if !ok {
		return nil, fmt.Errorf("failed event: expected *ScenarioExecutionState, got %T", ctx.State)
	}

	return &ScenarioFailedPayload{
		ScenarioID:     state.ScenarioID,
		DAGExecutionID: state.DAGExecutionID,
		FailedNodes:    state.FailedNodes,
		Reason:         state.Error,
		RequeueAsUnmet: state.RequeueAsUnmet,
	}, nil
}
