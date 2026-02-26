package reactive

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	workflow "github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/phases"
	"github.com/c360studio/semstreams/message"
	reactiveEngine "github.com/c360studio/semstreams/processor/reactive"
)

// ---------------------------------------------------------------------------
// PhaseReviewState
// ---------------------------------------------------------------------------

// PhaseReviewState is the typed KV state for the phase-review-loop reactive workflow.
// It embeds ExecutionState for base lifecycle fields and adds phase-generation-specific data.
type PhaseReviewState struct {
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

	// Generator output saved by phaseReviewHandleGeneratorResult.
	PhasesContent json.RawMessage `json:"phases_content,omitempty"`
	PhaseCount    int             `json:"phase_count,omitempty"`
	LLMRequestIDs []string        `json:"llm_request_ids,omitempty"`

	// Reviewer output saved by phaseReviewHandleReviewerResult.
	Verdict               string          `json:"verdict,omitempty"`
	Summary               string          `json:"summary,omitempty"`
	Findings              json.RawMessage `json:"findings,omitempty"`
	FormattedFindings     string          `json:"formatted_findings,omitempty"`
	ReviewerLLMRequestIDs []string        `json:"reviewer_llm_request_ids,omitempty"`
}

// GetExecutionState implements reactiveEngine.StateAccessor.
// This lets the engine read/write the embedded ExecutionState without reflection.
func (s *PhaseReviewState) GetExecutionState() *reactiveEngine.ExecutionState {
	return &s.ExecutionState
}

// ---------------------------------------------------------------------------
// Event payload types
// ---------------------------------------------------------------------------

// PhasesApprovedPayload wraps workflow.PhasesApprovedEvent and satisfies message.Payload.
// The JSON wire format is identical to PhasesApprovedEvent so downstream handlers
// reading "workflow.events.phases.approved" receive the expected field names.
type PhasesApprovedPayload struct {
	workflow.PhasesApprovedEvent
}

// Schema implements message.Payload.
func (p *PhasesApprovedPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "phases-approved", Version: "v1"}
}

// Validate implements message.Payload.
func (p *PhasesApprovedPayload) Validate() error {
	if p.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// MarshalJSON marshals using the embedded event's fields, not the wrapper's.
func (p *PhasesApprovedPayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.PhasesApprovedEvent)
}

// UnmarshalJSON unmarshals directly into the embedded event.
func (p *PhasesApprovedPayload) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &p.PhasesApprovedEvent)
}

// PhasesRevisionPayload wraps workflow.PhasesRevisionNeededEvent and satisfies message.Payload.
type PhasesRevisionPayload struct {
	workflow.PhasesRevisionNeededEvent
}

// Schema implements message.Payload.
func (p *PhasesRevisionPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "phases-revision-needed", Version: "v1"}
}

// Validate implements message.Payload.
func (p *PhasesRevisionPayload) Validate() error {
	if p.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// MarshalJSON marshals using the embedded event's fields.
func (p *PhasesRevisionPayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.PhasesRevisionNeededEvent)
}

// UnmarshalJSON unmarshals directly into the embedded event.
func (p *PhasesRevisionPayload) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &p.PhasesRevisionNeededEvent)
}

// PhaseEscalatePayload wraps workflow.EscalationEvent and satisfies message.Payload.
type PhaseEscalatePayload struct {
	workflow.EscalationEvent
}

// Schema implements message.Payload.
func (p *PhaseEscalatePayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "phase-escalation", Version: "v1"}
}

// Validate implements message.Payload.
func (p *PhaseEscalatePayload) Validate() error {
	if p.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// MarshalJSON marshals using the embedded event's fields.
func (p *PhaseEscalatePayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.EscalationEvent)
}

// UnmarshalJSON unmarshals directly into the embedded event.
func (p *PhaseEscalatePayload) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &p.EscalationEvent)
}

// PhaseErrorPayload wraps workflow.UserSignalErrorEvent and satisfies message.Payload.
type PhaseErrorPayload struct {
	workflow.UserSignalErrorEvent
}

// Schema implements message.Payload.
func (p *PhaseErrorPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "phase-error", Version: "v1"}
}

// Validate implements message.Payload.
func (p *PhaseErrorPayload) Validate() error {
	if p.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// MarshalJSON marshals using the embedded event's fields.
func (p *PhaseErrorPayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.UserSignalErrorEvent)
}

// UnmarshalJSON unmarshals directly into the embedded event.
func (p *PhaseErrorPayload) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &p.UserSignalErrorEvent)
}

// ---------------------------------------------------------------------------
// BuildPhaseReviewWorkflow
// ---------------------------------------------------------------------------

// BuildPhaseReviewWorkflow constructs the phase-review-loop reactive workflow
// using the shared OODA review loop builder with the Participant pattern.
//
// Components set their completion phases directly via StateManager.Transition():
//   - phase-generator sets phase to "phases-generated" when generation completes
//   - plan-reviewer sets phase to "reviewed" when review completes
func BuildPhaseReviewWorkflow(stateBucket string) *reactiveEngine.Definition {
	return BuildReviewLoopWorkflow(ReviewLoopConfig{
		WorkflowID:    "phase-review-loop",
		Description:   "Generate and iteratively review phases until approved or max iterations exceeded.",
		StateBucket:   stateBucket,
		MaxIterations: 3,
		Timeout:       30 * time.Minute,
		StateFactory:  func() any { return &PhaseReviewState{} },

		TriggerStream:         "WORKFLOW",
		TriggerSubject:        "workflow.trigger.phase-review-loop",
		TriggerMessageFactory: func() any { return &workflow.TriggerPayload{} },
		StateLookupKey: func(msg any) string {
			trigger, ok := msg.(*workflow.TriggerPayload)
			if !ok {
				return ""
			}
			return "phase-review." + trigger.Slug
		},
		KVKeyPattern: "phase-review.*",

		AcceptTrigger: phaseReviewAcceptTrigger,
		VerdictAccessor: func(state any) string {
			if s, ok := state.(*PhaseReviewState); ok {
				return s.Verdict
			}
			return ""
		},

		// Generator (phase-generator) - Participant pattern.
		GeneratorSubject:         "workflow.async.phase-generator",
		BuildGeneratorPayload:    phaseReviewBuildGeneratorPayload,
		GeneratingPhase:          phases.PhaseGenerating,
		GeneratorDispatchedPhase: phases.PhaseGeneratingDispatched,
		GeneratorCompletedPhase:  phases.PhasesGenerated,

		// Reviewer (plan-reviewer, reused) - Participant pattern.
		ReviewerSubject:         "workflow.async.plan-reviewer",
		BuildReviewerPayload:    phaseReviewBuildReviewerPayload,
		ReviewingPhase:          phases.PhaseReviewing,
		ReviewerDispatchedPhase: phases.PhaseReviewingDispatched,
		ReviewerCompletedPhase:  phases.PhaseReviewed,
		EvaluatedPhase:          phases.PhaseEvaluated,

		// Failure phases.
		GeneratorFailedPhase: phases.PhaseGeneratorFailed,
		ReviewerFailedPhase:  phases.PhaseReviewerFailed,

		// Events.
		ApprovedEventSubject: "workflow.events.phases.approved",
		BuildApprovedEvent:   phaseReviewBuildApprovedEvent,

		RevisionEventSubject: "workflow.events.phases.revision_needed",
		BuildRevisionEvent:   phaseReviewBuildRevisionEvent,
		MutateOnRevision:     phaseReviewHandleRevision,

		EscalateSubject:    "user.signal.escalate",
		BuildEscalateEvent: phaseReviewBuildEscalateEvent,
		MutateOnEscalation: phaseReviewHandleEscalation,

		ErrorSubject:    "user.signal.error",
		BuildErrorEvent: phaseReviewBuildErrorEvent,
		MutateOnError:   phaseReviewHandleError,
	})
}

// ---------------------------------------------------------------------------
// Mutators
// ---------------------------------------------------------------------------

// phaseReviewAcceptTrigger populates PhaseReviewState from the incoming TriggerPayload
// and transitions to the "generating" phase.
var phaseReviewAcceptTrigger reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*PhaseReviewState)
	if !ok {
		return fmt.Errorf("accept-trigger: expected *PhaseReviewState, got %T", ctx.State)
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
		state.ID = "phase-review." + trigger.Slug
		state.WorkflowID = "phase-review-loop"
		state.Status = reactiveEngine.StatusRunning
		now := time.Now()
		state.CreatedAt = now
		state.UpdatedAt = now
	}

	state.Phase = phases.PhaseGenerating
	return nil
}

// phaseReviewHandleRevision increments the iteration counter, clears the previous
// verdict, and transitions back to the generating phase for another attempt.
// Note: In the Participant pattern, components update state directly via StateManager.
// The old callback mutators (phaseReviewHandleGeneratorResult, phaseReviewHandleReviewerResult)
// are no longer needed - phase-generator sets phase to "phases-generated", reviewer sets phase to "reviewed".
var phaseReviewHandleRevision reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*PhaseReviewState)
	if !ok {
		return fmt.Errorf("revision mutator: expected *PhaseReviewState, got %T", ctx.State)
	}
	reactiveEngine.IncrementIteration(state)
	// Clear stale generator and reviewer output from the previous iteration.
	state.PhasesContent = nil
	state.PhaseCount = 0
	state.LLMRequestIDs = nil
	state.Findings = nil
	state.FormattedFindings = ""
	state.ReviewerLLMRequestIDs = nil
	// Note: Verdict already cleared below. Summary preserved for revision prompt.
	state.Verdict = ""
	state.Phase = phases.PhaseGenerating
	return nil
}

// phaseReviewHandleEscalation marks the execution as escalated.
var phaseReviewHandleEscalation reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*PhaseReviewState)
	if !ok {
		return fmt.Errorf("escalation mutator: expected *PhaseReviewState, got %T", ctx.State)
	}
	reactiveEngine.EscalateExecution(state, "max phase review iterations exceeded")
	return nil
}

// phaseReviewHandleError marks the execution as failed.
var phaseReviewHandleError reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*PhaseReviewState)
	if !ok {
		return fmt.Errorf("error mutator: expected *PhaseReviewState, got %T", ctx.State)
	}
	errMsg := state.Error
	if errMsg == "" {
		errMsg = "phase review step failed in phase: " + state.Phase
	}
	reactiveEngine.FailExecution(state, errMsg)
	return nil
}

// ---------------------------------------------------------------------------
// Payload builders
// ---------------------------------------------------------------------------

// phaseReviewBuildGeneratorPayload constructs a PhaseGeneratorRequest from state.
// When state.Iteration > 0, the prompt is augmented with reviewer feedback so
// the phase generator can address specific findings on the revision pass.
func phaseReviewBuildGeneratorPayload(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*PhaseReviewState)
	if !ok {
		return nil, fmt.Errorf("generator payload: expected *PhaseReviewState, got %T", ctx.State)
	}

	req := &PhaseGeneratorRequest{
		ExecutionID:   state.ID, // Required for Participant pattern state updates
		RequestID:     state.RequestID,
		Slug:          state.Slug,
		Title:         state.Title,
		Description:   state.Description,
		ProjectID:     state.ProjectID,
		TraceID:       state.TraceID,
		LoopID:        state.LoopID,
		Role:          state.Role,
		ScopePatterns: state.ScopePatterns,
	}

	// On revision passes, inject reviewer feedback into the prompt so the
	// phase generator can directly address the identified issues.
	if state.Iteration > 0 {
		req.Revision = true
		var sb strings.Builder
		sb.WriteString("REVISION REQUEST: Your previous phases were rejected by the reviewer.\n\n")
		sb.WriteString("## Review Summary\n")
		sb.WriteString(state.Summary)
		sb.WriteString("\n\n## Specific Findings\n")
		sb.WriteString(state.FormattedFindings)
		sb.WriteString("\n\n## Instructions\n")
		sb.WriteString("Address ALL issues raised by the reviewer and produce improved phases.")
		req.PreviousFindings = sb.String()
		req.Prompt = req.PreviousFindings
	} else {
		req.Prompt = state.Prompt
	}

	return req, nil
}

// phaseReviewBuildReviewerPayload constructs a PhaseReviewRequest from state.
// The plan-reviewer component is reused; PlanContent carries the phases content
// for review (the reviewer treats it generically as content to evaluate).
func phaseReviewBuildReviewerPayload(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*PhaseReviewState)
	if !ok {
		return nil, fmt.Errorf("reviewer payload: expected *PhaseReviewState, got %T", ctx.State)
	}

	return &PhaseReviewRequest{
		ExecutionID:   state.ID, // Required for Participant pattern state updates
		RequestID:     state.RequestID,
		Slug:          state.Slug,
		ProjectID:     state.ProjectID,
		PlanContent:   state.PhasesContent,
		ScopePatterns: state.ScopePatterns,
		TraceID:       state.TraceID,
		LoopID:        state.LoopID,
	}, nil
}

// phaseReviewBuildApprovedEvent constructs a PhasesApprovedPayload from state.
func phaseReviewBuildApprovedEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*PhaseReviewState)
	if !ok {
		return nil, fmt.Errorf("approved event: expected *PhaseReviewState, got %T", ctx.State)
	}

	return &PhasesApprovedPayload{
		PhasesApprovedEvent: workflow.PhasesApprovedEvent{
			Slug:          state.Slug,
			Verdict:       state.Verdict,
			Summary:       state.Summary,
			Findings:      state.Findings,
			LLMRequestIDs: state.ReviewerLLMRequestIDs,
		},
	}, nil
}

// phaseReviewBuildRevisionEvent constructs a PhasesRevisionPayload from state.
func phaseReviewBuildRevisionEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*PhaseReviewState)
	if !ok {
		return nil, fmt.Errorf("revision event: expected *PhaseReviewState, got %T", ctx.State)
	}

	return &PhasesRevisionPayload{
		PhasesRevisionNeededEvent: workflow.PhasesRevisionNeededEvent{
			Slug:              state.Slug,
			Iteration:         state.Iteration,
			Verdict:           state.Verdict,
			Findings:          state.Findings,
			FormattedFindings: state.FormattedFindings,
			Summary:           state.Summary,
			LLMRequestIDs:     state.ReviewerLLMRequestIDs,
		},
	}, nil
}

// phaseReviewBuildEscalateEvent constructs a PhaseEscalatePayload from state.
func phaseReviewBuildEscalateEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*PhaseReviewState)
	if !ok {
		return nil, fmt.Errorf("escalate event: expected *PhaseReviewState, got %T", ctx.State)
	}

	return &PhaseEscalatePayload{
		EscalationEvent: workflow.EscalationEvent{
			Slug:              state.Slug,
			Reason:            "max phase review iterations exceeded",
			LastVerdict:       state.Verdict,
			LastFindings:      state.Findings,
			FormattedFindings: state.FormattedFindings,
			Iteration:         state.Iteration,
		},
	}, nil
}

// phaseReviewBuildErrorEvent constructs a PhaseErrorPayload from state.
func phaseReviewBuildErrorEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*PhaseReviewState)
	if !ok {
		return nil, fmt.Errorf("error event: expected *PhaseReviewState, got %T", ctx.State)
	}

	errMsg := state.Error
	if errMsg == "" {
		errMsg = "phase review step failed in phase: " + state.Phase
	}

	return &PhaseErrorPayload{
		UserSignalErrorEvent: workflow.UserSignalErrorEvent{
			Slug:  state.Slug,
			Error: errMsg,
		},
	}, nil
}
