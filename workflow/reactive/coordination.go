package reactive

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/phases"
	"github.com/c360studio/semstreams/message"
	reactiveEngine "github.com/c360studio/semstreams/processor/reactive"
)

// CoordinationLoopWorkflowID is the unique identifier for the coordination loop.
const CoordinationLoopWorkflowID = "coordination-loop"

// ---------------------------------------------------------------------------
// CoordinationState
// ---------------------------------------------------------------------------

// CoordinationState is the typed KV state for the coordination-loop reactive workflow.
// It embeds ExecutionState for base lifecycle fields and adds coordination-specific
// data for parallel planner fan-out, result merging, and synthesis.
type CoordinationState struct {
	reactiveEngine.ExecutionState

	// Trigger data populated on accept-trigger.
	Slug          string   `json:"slug"`
	Title         string   `json:"title"`
	Description   string   `json:"description,omitempty"`
	ProjectID     string   `json:"project_id,omitempty"`
	RequestID     string   `json:"request_id,omitempty"`
	TraceID       string   `json:"trace_id,omitempty"`
	LoopID        string   `json:"loop_id,omitempty"`
	FocusAreas    []string `json:"focus_areas,omitempty"`
	ScopePatterns []string `json:"scope_patterns,omitempty"`
	MaxPlanners   int      `json:"max_planners,omitempty"`
	Auto          bool     `json:"auto,omitempty"`

	// Focus determination output set by the focus handler.
	Focuses []CoordinationFocus `json:"focuses,omitempty"`

	// Planner tracking (parallel fan-out).
	PlannerCount   int                        `json:"planner_count"`
	PlannerResults map[string]*PlannerOutcome  `json:"planner_results,omitempty"`
	LLMRequestIDs  []string                   `json:"llm_request_ids,omitempty"`

	// Synthesis output set by the synthesis handler.
	SynthesizedPlan json.RawMessage `json:"synthesized_plan,omitempty"`
}

// GetExecutionState implements reactiveEngine.StateAccessor.
func (s *CoordinationState) GetExecutionState() *reactiveEngine.ExecutionState {
	return &s.ExecutionState
}

// CoordinationFocus represents a planning focus area determined by the coordinator.
type CoordinationFocus struct {
	Area        string   `json:"area"`
	Description string   `json:"description"`
	Hints       []string `json:"hints,omitempty"`
}

// PlannerOutcome captures the result from a single parallel planner.
type PlannerOutcome struct {
	PlannerID    string          `json:"planner_id"`
	FocusArea    string          `json:"focus_area"`
	Status       string          `json:"status"` // completed, failed
	Result       json.RawMessage `json:"result,omitempty"`
	Error        string          `json:"error,omitempty"`
	LLMRequestID string          `json:"llm_request_id,omitempty"`
}

// ---------------------------------------------------------------------------
// Payload types for coordination dispatch
// ---------------------------------------------------------------------------

// CoordinationPlannerMessage is published by the focus handler to dispatch
// individual planner work. The plan-coordinator's planner consumer picks these up.
type CoordinationPlannerMessage struct {
	ExecutionID      string `json:"execution_id"`
	PlannerID        string `json:"planner_id"`
	Slug             string `json:"slug"`
	Title            string `json:"title"`
	FocusArea        string `json:"focus_area"`
	FocusDescription string `json:"focus_description"`
	Hints            []string `json:"hints,omitempty"`
	TraceID          string `json:"trace_id,omitempty"`
	LoopID           string `json:"loop_id,omitempty"`
}

// CoordinationPlannerMessageType is the message type for planner dispatch messages.
var CoordinationPlannerMessageType = message.Type{
	Domain: "workflow", Category: "coordination-planner-message", Version: "v1",
}

// Schema implements message.Payload.
func (m *CoordinationPlannerMessage) Schema() message.Type {
	return CoordinationPlannerMessageType
}

// Validate implements message.Payload.
func (m *CoordinationPlannerMessage) Validate() error {
	if m.ExecutionID == "" {
		return fmt.Errorf("execution_id is required")
	}
	if m.PlannerID == "" {
		return fmt.Errorf("planner_id is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (m *CoordinationPlannerMessage) MarshalJSON() ([]byte, error) {
	type Alias CoordinationPlannerMessage
	return json.Marshal((*Alias)(m))
}

// UnmarshalJSON implements json.Unmarshaler.
func (m *CoordinationPlannerMessage) UnmarshalJSON(data []byte) error {
	type Alias CoordinationPlannerMessage
	return json.Unmarshal(data, (*Alias)(m))
}

// CoordinationPlannerResult is published by each planner handler to report
// its outcome. The reactive engine merges these into CoordinationState via a rule,
// acting as the single KV writer to avoid CAS conflicts.
type CoordinationPlannerResult struct {
	ExecutionID  string          `json:"execution_id"`
	PlannerID    string          `json:"planner_id"`
	Slug         string          `json:"slug"`
	FocusArea    string          `json:"focus_area"`
	Status       string          `json:"status"` // completed, failed
	Result       json.RawMessage `json:"result,omitempty"`
	Error        string          `json:"error,omitempty"`
	LLMRequestID string          `json:"llm_request_id,omitempty"`
}

// CoordinationPlannerResultType is the message type for planner result messages.
var CoordinationPlannerResultType = message.Type{
	Domain: "workflow", Category: "coordination-planner-result", Version: "v1",
}

// Schema implements message.Payload.
func (r *CoordinationPlannerResult) Schema() message.Type {
	return CoordinationPlannerResultType
}

// Validate implements message.Payload.
func (r *CoordinationPlannerResult) Validate() error {
	if r.ExecutionID == "" {
		return fmt.Errorf("execution_id is required")
	}
	if r.PlannerID == "" {
		return fmt.Errorf("planner_id is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *CoordinationPlannerResult) MarshalJSON() ([]byte, error) {
	type Alias CoordinationPlannerResult
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *CoordinationPlannerResult) UnmarshalJSON(data []byte) error {
	type Alias CoordinationPlannerResult
	return json.Unmarshal(data, (*Alias)(r))
}

// CoordinationSynthesisRequest is dispatched by the engine to the synthesis handler.
type CoordinationSynthesisRequest struct {
	ExecutionID string `json:"execution_id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	ProjectID   string `json:"project_id,omitempty"`
	TraceID     string `json:"trace_id,omitempty"`
	LoopID      string `json:"loop_id,omitempty"`
}

// CoordinationSynthesisRequestType is the message type for synthesis requests.
var CoordinationSynthesisRequestType = message.Type{
	Domain: "workflow", Category: "coordination-synthesis-request", Version: "v1",
}

// Schema implements message.Payload.
func (r *CoordinationSynthesisRequest) Schema() message.Type {
	return CoordinationSynthesisRequestType
}

// Validate implements message.Payload.
func (r *CoordinationSynthesisRequest) Validate() error {
	if r.ExecutionID == "" {
		return fmt.Errorf("execution_id is required")
	}
	if r.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *CoordinationSynthesisRequest) MarshalJSON() ([]byte, error) {
	type Alias CoordinationSynthesisRequest
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *CoordinationSynthesisRequest) UnmarshalJSON(data []byte) error {
	type Alias CoordinationSynthesisRequest
	return json.Unmarshal(data, (*Alias)(r))
}

// ---------------------------------------------------------------------------
// Event payloads
// ---------------------------------------------------------------------------

// CoordinationCompletedPayload wraps the coordination completed event.
type CoordinationCompletedPayload struct {
	Slug          string   `json:"slug"`
	RequestID     string   `json:"request_id,omitempty"`
	TraceID       string   `json:"trace_id,omitempty"`
	PlannerCount  int      `json:"planner_count"`
	LLMRequestIDs []string `json:"llm_request_ids,omitempty"`
}

// Schema implements message.Payload.
func (p *CoordinationCompletedPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "coordination-completed", Version: "v1"}
}

// Validate implements message.Payload.
func (p *CoordinationCompletedPayload) Validate() error {
	if p.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *CoordinationCompletedPayload) MarshalJSON() ([]byte, error) {
	type Alias CoordinationCompletedPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *CoordinationCompletedPayload) UnmarshalJSON(data []byte) error {
	type Alias CoordinationCompletedPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// CoordinationErrorPayload wraps the coordination error event.
type CoordinationErrorPayload struct {
	workflow.UserSignalErrorEvent
}

// Schema implements message.Payload.
func (p *CoordinationErrorPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "coordination-error", Version: "v1"}
}

// Validate implements message.Payload.
func (p *CoordinationErrorPayload) Validate() error {
	if p.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// MarshalJSON marshals using the embedded event's fields.
func (p *CoordinationErrorPayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.UserSignalErrorEvent)
}

// UnmarshalJSON unmarshals directly into the embedded event.
func (p *CoordinationErrorPayload) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &p.UserSignalErrorEvent)
}

// ---------------------------------------------------------------------------
// Mutators
// ---------------------------------------------------------------------------

// coordinationAcceptTrigger populates CoordinationState from the incoming trigger
// and transitions to the "focusing" phase.
var coordinationAcceptTrigger reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*CoordinationState)
	if !ok {
		return fmt.Errorf("accept-trigger: expected *CoordinationState, got %T", ctx.State)
	}

	trigger, ok := ctx.Message.(*PlanCoordinatorRequest)
	if !ok {
		return fmt.Errorf("accept-trigger: expected *PlanCoordinatorRequest, got %T", ctx.Message)
	}

	state.Slug = trigger.Slug
	state.Title = trigger.Title
	state.Description = trigger.Description
	state.ProjectID = trigger.ProjectID
	state.RequestID = trigger.RequestID
	state.TraceID = trigger.TraceID
	state.LoopID = trigger.LoopID
	state.FocusAreas = trigger.FocusAreas
	state.MaxPlanners = trigger.MaxPlanners
	state.PlannerResults = make(map[string]*PlannerOutcome)

	if state.ID == "" {
		state.ID = "coordination." + trigger.Slug
		state.WorkflowID = CoordinationLoopWorkflowID
		state.Status = reactiveEngine.StatusRunning
		now := time.Now()
		state.CreatedAt = now
		state.UpdatedAt = now
	}

	state.Phase = phases.CoordinationFocusing
	return nil
}

// coordinationMergePlannerResult merges a single planner result into the
// coordination state. If all planners have reported, transitions to synthesizing.
// The reactive engine is the single KV writer — no CAS conflicts.
var coordinationMergePlannerResult reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*CoordinationState)
	if !ok {
		return fmt.Errorf("planner-result: expected *CoordinationState, got %T", ctx.State)
	}

	result, ok := ctx.Message.(*CoordinationPlannerResult)
	if !ok {
		return fmt.Errorf("planner-result: expected *CoordinationPlannerResult, got %T", ctx.Message)
	}

	if state.PlannerResults == nil {
		state.PlannerResults = make(map[string]*PlannerOutcome)
	}

	state.PlannerResults[result.PlannerID] = &PlannerOutcome{
		PlannerID:    result.PlannerID,
		FocusArea:    result.FocusArea,
		Status:       result.Status,
		Result:       result.Result,
		Error:        result.Error,
		LLMRequestID: result.LLMRequestID,
	}

	if result.LLMRequestID != "" {
		state.LLMRequestIDs = append(state.LLMRequestIDs, result.LLMRequestID)
	}

	// Check if all planners have reported.
	if allPlannersDone(state) {
		// Check if any planner succeeded.
		hasSuccess := false
		for _, p := range state.PlannerResults {
			if p.Status == "completed" {
				hasSuccess = true
				break
			}
		}
		if hasSuccess {
			state.Phase = phases.CoordinationSynthesizing
		} else {
			state.Phase = phases.CoordinationPlannersFailed
			state.Error = "all planners failed"
		}
	}

	return nil
}

// coordinationHandleError marks the coordination as failed.
var coordinationHandleError reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*CoordinationState)
	if !ok {
		return fmt.Errorf("error mutator: expected *CoordinationState, got %T", ctx.State)
	}
	errMsg := state.Error
	if errMsg == "" {
		errMsg = "coordination failed in phase: " + state.Phase
	}
	reactiveEngine.FailExecution(state, errMsg)
	return nil
}

// ---------------------------------------------------------------------------
// Payload builders
// ---------------------------------------------------------------------------

// coordinationBuildFocusPayload constructs a PlanCoordinatorRequest from state
// for dispatch to the focus handler.
func coordinationBuildFocusPayload(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*CoordinationState)
	if !ok {
		return nil, fmt.Errorf("focus payload: expected *CoordinationState, got %T", ctx.State)
	}

	return &PlanCoordinatorRequest{
		ExecutionID: state.ID,
		RequestID:   state.RequestID,
		Slug:        state.Slug,
		Title:       state.Title,
		Description: state.Description,
		FocusAreas:  state.FocusAreas,
		MaxPlanners: state.MaxPlanners,
		ProjectID:   state.ProjectID,
		TraceID:     state.TraceID,
		LoopID:      state.LoopID,
	}, nil
}

// coordinationBuildSynthesisPayload constructs a CoordinationSynthesisRequest
// from state for dispatch to the synthesis handler.
func coordinationBuildSynthesisPayload(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*CoordinationState)
	if !ok {
		return nil, fmt.Errorf("synthesis payload: expected *CoordinationState, got %T", ctx.State)
	}

	return &CoordinationSynthesisRequest{
		ExecutionID: state.ID,
		Slug:        state.Slug,
		Title:       state.Title,
		ProjectID:   state.ProjectID,
		TraceID:     state.TraceID,
		LoopID:      state.LoopID,
	}, nil
}

// coordinationBuildCompletedEvent constructs a completed event payload.
func coordinationBuildCompletedEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*CoordinationState)
	if !ok {
		return nil, fmt.Errorf("completed event: expected *CoordinationState, got %T", ctx.State)
	}

	return &CoordinationCompletedPayload{
		Slug:          state.Slug,
		RequestID:     state.RequestID,
		TraceID:       state.TraceID,
		PlannerCount:  state.PlannerCount,
		LLMRequestIDs: state.LLMRequestIDs,
	}, nil
}

// coordinationBuildErrorEvent constructs an error event payload.
func coordinationBuildErrorEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*CoordinationState)
	if !ok {
		return nil, fmt.Errorf("error event: expected *CoordinationState, got %T", ctx.State)
	}

	errMsg := state.Error
	if errMsg == "" {
		errMsg = "coordination failed in phase: " + state.Phase
	}

	return &CoordinationErrorPayload{
		UserSignalErrorEvent: workflow.UserSignalErrorEvent{
			Slug:  state.Slug,
			Error: errMsg,
		},
	}, nil
}

// ---------------------------------------------------------------------------
// Condition helpers
// ---------------------------------------------------------------------------

// allPlannersDone checks if all expected planners have reported results.
func allPlannersDone(state *CoordinationState) bool {
	if state.PlannerCount == 0 {
		return false
	}
	return len(state.PlannerResults) >= state.PlannerCount
}
