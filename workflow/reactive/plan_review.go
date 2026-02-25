package reactive

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	workflow "github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/message"
	reactiveEngine "github.com/c360studio/semstreams/processor/reactive"
)

// ---------------------------------------------------------------------------
// PlanReviewState
// ---------------------------------------------------------------------------

// PlanReviewState is the typed KV state for the plan-review-loop reactive workflow.
// It embeds ExecutionState for base lifecycle fields and adds plan-specific data.
type PlanReviewState struct {
	reactiveEngine.ExecutionState

	// Trigger data populated on accept-trigger.
	Slug          string   `json:"slug"`
	Title         string   `json:"title"`
	Description   string   `json:"description,omitempty"`
	ProjectID     string   `json:"project_id,omitempty"`
	RequestID     string   `json:"request_id,omitempty"`
	TraceID       string   `json:"trace_id,omitempty"`
	LoopID        string   `json:"loop_id,omitempty"`
	Role          string   `json:"role,omitempty"`
	Prompt        string   `json:"prompt,omitempty"`
	ScopePatterns []string `json:"scope_patterns,omitempty"`
	Auto          bool     `json:"auto,omitempty"`

	// Generator output saved by planReviewHandlePlannerResult.
	PlanContent   json.RawMessage `json:"plan_content,omitempty"`
	LLMRequestIDs []string        `json:"llm_request_ids,omitempty"`

	// Reviewer output saved by planReviewHandleReviewerResult.
	Verdict               string          `json:"verdict,omitempty"`
	Summary               string          `json:"summary,omitempty"`
	Findings              json.RawMessage `json:"findings,omitempty"`
	FormattedFindings     string          `json:"formatted_findings,omitempty"`
	ReviewerLLMRequestIDs []string        `json:"reviewer_llm_request_ids,omitempty"`
}

// GetExecutionState implements reactiveEngine.StateAccessor.
// This lets the engine read/write the embedded ExecutionState without reflection.
func (s *PlanReviewState) GetExecutionState() *reactiveEngine.ExecutionState {
	return &s.ExecutionState
}

// ---------------------------------------------------------------------------
// Event payload types
// ---------------------------------------------------------------------------

// PlanApprovedPayload wraps workflow.PlanApprovedEvent and satisfies message.Payload.
// The JSON wire format is identical to PlanApprovedEvent so downstream handlers
// reading "workflow.events.plan.approved" receive the expected field names.
type PlanApprovedPayload struct {
	workflow.PlanApprovedEvent
}

// Schema implements message.Payload.
func (p *PlanApprovedPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "plan-approved", Version: "v1"}
}

// Validate implements message.Payload.
func (p *PlanApprovedPayload) Validate() error {
	if p.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// MarshalJSON marshals using the embedded event's fields, not the wrapper's.
func (p *PlanApprovedPayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.PlanApprovedEvent)
}

// UnmarshalJSON unmarshals directly into the embedded event.
func (p *PlanApprovedPayload) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &p.PlanApprovedEvent)
}

// PlanRevisionPayload wraps workflow.PlanRevisionNeededEvent and satisfies message.Payload.
type PlanRevisionPayload struct {
	workflow.PlanRevisionNeededEvent
}

// Schema implements message.Payload.
func (p *PlanRevisionPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "plan-revision-needed", Version: "v1"}
}

// Validate implements message.Payload.
func (p *PlanRevisionPayload) Validate() error {
	if p.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// MarshalJSON marshals using the embedded event's fields.
func (p *PlanRevisionPayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.PlanRevisionNeededEvent)
}

// UnmarshalJSON unmarshals directly into the embedded event.
func (p *PlanRevisionPayload) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &p.PlanRevisionNeededEvent)
}

// PlanEscalatePayload wraps workflow.EscalationEvent and satisfies message.Payload.
type PlanEscalatePayload struct {
	workflow.EscalationEvent
}

// Schema implements message.Payload.
func (p *PlanEscalatePayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "plan-escalation", Version: "v1"}
}

// Validate implements message.Payload.
func (p *PlanEscalatePayload) Validate() error {
	if p.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// MarshalJSON marshals using the embedded event's fields.
func (p *PlanEscalatePayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.EscalationEvent)
}

// UnmarshalJSON unmarshals directly into the embedded event.
func (p *PlanEscalatePayload) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &p.EscalationEvent)
}

// PlanErrorPayload wraps workflow.UserSignalErrorEvent and satisfies message.Payload.
type PlanErrorPayload struct {
	workflow.UserSignalErrorEvent
}

// Schema implements message.Payload.
func (p *PlanErrorPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "plan-error", Version: "v1"}
}

// Validate implements message.Payload.
func (p *PlanErrorPayload) Validate() error {
	if p.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// MarshalJSON marshals using the embedded event's fields.
func (p *PlanErrorPayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.UserSignalErrorEvent)
}

// UnmarshalJSON unmarshals directly into the embedded event.
func (p *PlanErrorPayload) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &p.UserSignalErrorEvent)
}

// ---------------------------------------------------------------------------
// BuildPlanReviewWorkflow
// ---------------------------------------------------------------------------

// BuildPlanReviewWorkflow constructs the plan-review-loop reactive workflow
// using the shared OODA review loop builder.
func BuildPlanReviewWorkflow(stateBucket string) *reactiveEngine.Definition {
	return BuildReviewLoopWorkflow(ReviewLoopConfig{
		WorkflowID:    "plan-review-loop",
		Description:   "Generate and iteratively review a plan until approved or max iterations exceeded.",
		StateBucket:   stateBucket,
		MaxIterations: 3,
		Timeout:       30 * time.Minute,
		StateFactory:  func() any { return &PlanReviewState{} },

		TriggerStream:         "WORKFLOW",
		TriggerSubject:        "workflow.trigger.plan-review-loop",
		TriggerMessageFactory: func() any { return &workflow.TriggerPayload{} },
		StateLookupKey: func(msg any) string {
			trigger, ok := msg.(*workflow.TriggerPayload)
			if !ok {
				return ""
			}
			return "plan-review." + trigger.Slug
		},
		KVKeyPattern: "plan-review.*",

		AcceptTrigger: planReviewAcceptTrigger,
		VerdictAccessor: func(state any) string {
			if s, ok := state.(*PlanReviewState); ok {
				return s.Verdict
			}
			return ""
		},

		GeneratorSubject:        "workflow.async.planner",
		GeneratorResultTypeKey:  "workflow.planner-result.v1",
		BuildGeneratorPayload:   planReviewBuildPlannerPayload,
		MutateOnGeneratorResult: planReviewHandlePlannerResult,

		ReviewerSubject:        "workflow.async.plan-reviewer",
		ReviewerResultTypeKey:  "workflow.review-result.v1",
		BuildReviewerPayload:   planReviewBuildReviewerPayload,
		MutateOnReviewerResult: planReviewHandleReviewerResult,

		ApprovedEventSubject: "workflow.events.plan.approved",
		BuildApprovedEvent:   planReviewBuildApprovedEvent,

		RevisionEventSubject: "workflow.events.plan.revision_needed",
		BuildRevisionEvent:   planReviewBuildRevisionEvent,
		MutateOnRevision:     planReviewHandleRevision,

		EscalateSubject:    "user.signal.escalate",
		BuildEscalateEvent: planReviewBuildEscalateEvent,
		MutateOnEscalation: planReviewHandleEscalation,

		ErrorSubject:    "user.signal.error",
		BuildErrorEvent: planReviewBuildErrorEvent,
		MutateOnError:   planReviewHandleError,
	})
}

// ---------------------------------------------------------------------------
// Mutators
// ---------------------------------------------------------------------------

// planReviewAcceptTrigger populates PlanReviewState from the incoming TriggerPayload
// and transitions to the "generating" phase.
var planReviewAcceptTrigger reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*PlanReviewState)
	if !ok {
		return fmt.Errorf("accept-trigger: expected *PlanReviewState, got %T", ctx.State)
	}

	trigger, ok := ctx.Message.(*workflow.TriggerPayload)
	if !ok {
		return fmt.Errorf("accept-trigger: expected *workflow.TriggerPayload, got %T", ctx.Message)
	}

	// Populate state from trigger fields.
	state.Slug = trigger.Slug
	state.Title = trigger.Title
	state.Description = trigger.Description
	state.ProjectID = trigger.ProjectID
	state.RequestID = trigger.RequestID
	state.TraceID = trigger.TraceID
	state.LoopID = trigger.LoopID
	state.Role = trigger.Role
	state.Prompt = trigger.Prompt
	state.ScopePatterns = trigger.ScopePatterns
	state.Auto = trigger.Auto

	// Initialise execution metadata if first trigger.
	if state.ID == "" {
		state.ID = "plan-review." + trigger.Slug
		state.WorkflowID = "plan-review-loop"
		state.Status = reactiveEngine.StatusRunning
		now := time.Now()
		state.CreatedAt = now
		state.UpdatedAt = now
	}

	state.Phase = ReviewPhaseGenerating
	return nil
}

// planReviewHandlePlannerResult is the async callback mutator for the generate rule.
// It saves the planner output and transitions to the reviewing phase.
var planReviewHandlePlannerResult reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, result any) error {
	state, ok := ctx.State.(*PlanReviewState)
	if !ok {
		return fmt.Errorf("generate callback: expected *PlanReviewState, got %T", ctx.State)
	}
	if res, ok := result.(*PlannerResult); ok {
		state.PlanContent = res.Content
		state.LLMRequestIDs = res.LLMRequestIDs
		state.Phase = ReviewPhaseReviewing
	} else {
		state.Phase = ReviewPhaseGeneratorFailed
		state.Error = "unexpected planner result type"
	}
	return nil
}

// planReviewHandleReviewerResult is the async callback mutator for the review rule.
// It saves the reviewer output and transitions to the evaluated phase.
var planReviewHandleReviewerResult reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, result any) error {
	state, ok := ctx.State.(*PlanReviewState)
	if !ok {
		return fmt.Errorf("review callback: expected *PlanReviewState, got %T", ctx.State)
	}
	if res, ok := result.(*ReviewResult); ok {
		state.Verdict = res.Verdict
		state.Summary = res.Summary
		state.Findings = res.Findings
		state.FormattedFindings = res.FormattedFindings
		state.ReviewerLLMRequestIDs = res.LLMRequestIDs
		state.Phase = ReviewPhaseEvaluated
	} else {
		state.Phase = ReviewPhaseReviewerFailed
		state.Error = "unexpected reviewer result type"
	}
	return nil
}

// planReviewHandleRevision increments the iteration counter, clears the previous
// verdict, and transitions back to the generating phase for another attempt.
var planReviewHandleRevision reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*PlanReviewState)
	if !ok {
		return fmt.Errorf("revision mutator: expected *PlanReviewState, got %T", ctx.State)
	}
	reactiveEngine.IncrementIteration(state)
	// Clear stale generator and reviewer output from the previous iteration.
	state.PlanContent = nil
	state.LLMRequestIDs = nil
	state.Findings = nil
	state.FormattedFindings = ""
	state.ReviewerLLMRequestIDs = nil
	// Note: Verdict already cleared below. Summary preserved for revision prompt.
	state.Verdict = ""
	state.Phase = ReviewPhaseGenerating
	return nil
}

// planReviewHandleEscalation marks the execution as escalated.
var planReviewHandleEscalation reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*PlanReviewState)
	if !ok {
		return fmt.Errorf("escalation mutator: expected *PlanReviewState, got %T", ctx.State)
	}
	reactiveEngine.EscalateExecution(state, "max plan review iterations exceeded")
	return nil
}

// planReviewHandleError marks the execution as failed.
var planReviewHandleError reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*PlanReviewState)
	if !ok {
		return fmt.Errorf("error mutator: expected *PlanReviewState, got %T", ctx.State)
	}
	errMsg := state.Error
	if errMsg == "" {
		errMsg = "plan review step failed in phase: " + state.Phase
	}
	reactiveEngine.FailExecution(state, errMsg)
	return nil
}

// ---------------------------------------------------------------------------
// Payload builders
// ---------------------------------------------------------------------------

// planReviewBuildPlannerPayload constructs a PlannerRequest from state.
// When state.Iteration > 0, the prompt is augmented with reviewer feedback so
// the planner can address specific findings on the revision pass.
func planReviewBuildPlannerPayload(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*PlanReviewState)
	if !ok {
		return nil, fmt.Errorf("planner payload: expected *PlanReviewState, got %T", ctx.State)
	}

	req := &PlannerRequest{
		RequestID:     state.RequestID,
		Slug:          state.Slug,
		Title:         state.Title,
		Description:   state.Description,
		ProjectID:     state.ProjectID,
		TraceID:       state.TraceID,
		LoopID:        state.LoopID,
		Role:          state.Role,
		ScopePatterns: state.ScopePatterns,
		Auto:          state.Auto,
	}

	// On revision passes, inject reviewer feedback into the prompt so the
	// planner can directly address the identified issues.
	if state.Iteration > 0 {
		req.Revision = true
		var sb strings.Builder
		sb.WriteString("REVISION REQUEST: Your previous plan was rejected by the reviewer.\n\n")
		sb.WriteString("## Review Summary\n")
		sb.WriteString(state.Summary)
		sb.WriteString("\n\n## Specific Findings\n")
		sb.WriteString(state.FormattedFindings)
		sb.WriteString("\n\n## Instructions\n")
		sb.WriteString("Address ALL issues raised by the reviewer and produce an improved plan.")
		req.PreviousFindings = sb.String()
		req.Prompt = req.PreviousFindings
	} else {
		req.Prompt = state.Prompt
	}

	return req, nil
}

// planReviewBuildReviewerPayload constructs a PlanReviewRequest from state.
func planReviewBuildReviewerPayload(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*PlanReviewState)
	if !ok {
		return nil, fmt.Errorf("reviewer payload: expected *PlanReviewState, got %T", ctx.State)
	}

	return &PlanReviewRequest{
		RequestID:     state.RequestID,
		Slug:          state.Slug,
		ProjectID:     state.ProjectID,
		PlanContent:   state.PlanContent,
		ScopePatterns: state.ScopePatterns,
		TraceID:       state.TraceID,
		LoopID:        state.LoopID,
	}, nil
}

// planReviewBuildApprovedEvent constructs a PlanApprovedPayload from state.
func planReviewBuildApprovedEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*PlanReviewState)
	if !ok {
		return nil, fmt.Errorf("approved event: expected *PlanReviewState, got %T", ctx.State)
	}

	return &PlanApprovedPayload{
		PlanApprovedEvent: workflow.PlanApprovedEvent{
			Slug:          state.Slug,
			Verdict:       state.Verdict,
			Summary:       state.Summary,
			LLMRequestIDs: state.ReviewerLLMRequestIDs,
		},
	}, nil
}

// planReviewBuildRevisionEvent constructs a PlanRevisionPayload from state.
func planReviewBuildRevisionEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*PlanReviewState)
	if !ok {
		return nil, fmt.Errorf("revision event: expected *PlanReviewState, got %T", ctx.State)
	}

	return &PlanRevisionPayload{
		PlanRevisionNeededEvent: workflow.PlanRevisionNeededEvent{
			Slug:          state.Slug,
			Iteration:     state.Iteration,
			Verdict:       state.Verdict,
			Findings:      state.Findings,
			LLMRequestIDs: state.ReviewerLLMRequestIDs,
		},
	}, nil
}

// planReviewBuildEscalateEvent constructs a PlanEscalatePayload from state.
func planReviewBuildEscalateEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*PlanReviewState)
	if !ok {
		return nil, fmt.Errorf("escalate event: expected *PlanReviewState, got %T", ctx.State)
	}

	return &PlanEscalatePayload{
		EscalationEvent: workflow.EscalationEvent{
			Slug:              state.Slug,
			Reason:            "max plan review iterations exceeded",
			LastVerdict:       state.Verdict,
			LastFindings:      state.Findings,
			FormattedFindings: state.FormattedFindings,
			Iteration:         state.Iteration,
		},
	}, nil
}

// planReviewBuildErrorEvent constructs a PlanErrorPayload from state.
func planReviewBuildErrorEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*PlanReviewState)
	if !ok {
		return nil, fmt.Errorf("error event: expected *PlanReviewState, got %T", ctx.State)
	}

	errMsg := state.Error
	if errMsg == "" {
		errMsg = "plan review step failed in phase: " + state.Phase
	}

	return &PlanErrorPayload{
		UserSignalErrorEvent: workflow.UserSignalErrorEvent{
			Slug:  state.Slug,
			Error: errMsg,
		},
	}, nil
}
