package reactive

import (
	"encoding/json"
	"fmt"
	"time"

	workflow "github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/phases"
	"github.com/c360studio/semstreams/message"
	reactiveEngine "github.com/c360studio/semstreams/processor/reactive"
)

// ---------------------------------------------------------------------------
// Workflow ID constant
// ---------------------------------------------------------------------------

// ChangeProposalLoopWorkflowID is the unique identifier for the change-proposal-loop.
const ChangeProposalLoopWorkflowID = "change-proposal-loop"

// ---------------------------------------------------------------------------
// ChangeProposalState
// ---------------------------------------------------------------------------

// ChangeProposalState is the typed KV state for the change-proposal-loop reactive
// workflow. It embeds ExecutionState for base lifecycle fields and adds
// ChangeProposal-specific data for each stage of the OODA pipeline.
type ChangeProposalState struct {
	reactiveEngine.ExecutionState

	// Trigger data populated on accept-trigger.
	Slug       string `json:"slug"`
	ProposalID string `json:"proposal_id"`
	ProjectID  string `json:"project_id,omitempty"`
	TraceID    string `json:"trace_id,omitempty"`

	// Reviewer output saved by the change-proposal-reviewer component.
	Verdict  string `json:"verdict,omitempty"`  // "accepted" | "rejected"
	Feedback string `json:"feedback,omitempty"` // reviewer rationale

	// Cascade tracking populated after accepted verdict.
	AffectedTaskIDs []string `json:"affected_task_ids,omitempty"`
}

// GetExecutionState implements reactiveEngine.StateAccessor.
func (s *ChangeProposalState) GetExecutionState() *reactiveEngine.ExecutionState {
	return &s.ExecutionState
}

// ---------------------------------------------------------------------------
// Event payload types
// ---------------------------------------------------------------------------

// ChangeProposalAcceptedPayload is published when a proposal cascade completes.
type ChangeProposalAcceptedPayload struct {
	ProposalID      string   `json:"proposal_id"`
	PlanID          string   `json:"plan_id"`
	Slug            string   `json:"slug"`
	AffectedTaskIDs []string `json:"affected_task_ids"`
}

// Schema implements message.Payload.
func (p *ChangeProposalAcceptedPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "change-proposal-accepted", Version: "v1"}
}

// Validate implements message.Payload.
func (p *ChangeProposalAcceptedPayload) Validate() error {
	if p.ProposalID == "" {
		return fmt.Errorf("proposal_id is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *ChangeProposalAcceptedPayload) MarshalJSON() ([]byte, error) {
	type Alias ChangeProposalAcceptedPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *ChangeProposalAcceptedPayload) UnmarshalJSON(data []byte) error {
	type Alias ChangeProposalAcceptedPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// ChangeProposalRejectedPayload is published when a proposal is rejected.
type ChangeProposalRejectedPayload struct {
	ProposalID string `json:"proposal_id"`
	PlanID     string `json:"plan_id"`
	Slug       string `json:"slug"`
	Feedback   string `json:"feedback,omitempty"`
}

// Schema implements message.Payload.
func (p *ChangeProposalRejectedPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "change-proposal-rejected", Version: "v1"}
}

// Validate implements message.Payload.
func (p *ChangeProposalRejectedPayload) Validate() error {
	if p.ProposalID == "" {
		return fmt.Errorf("proposal_id is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *ChangeProposalRejectedPayload) MarshalJSON() ([]byte, error) {
	type Alias ChangeProposalRejectedPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *ChangeProposalRejectedPayload) UnmarshalJSON(data []byte) error {
	type Alias ChangeProposalRejectedPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// ChangeProposalEscalatePayload is published when the review loop exceeds retry budget.
type ChangeProposalEscalatePayload struct {
	workflow.EscalationEvent
}

// Schema implements message.Payload.
func (p *ChangeProposalEscalatePayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "change-proposal-escalate", Version: "v1"}
}

// Validate implements message.Payload.
func (p *ChangeProposalEscalatePayload) Validate() error {
	if p.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *ChangeProposalEscalatePayload) MarshalJSON() ([]byte, error) {
	type Alias ChangeProposalEscalatePayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *ChangeProposalEscalatePayload) UnmarshalJSON(data []byte) error {
	type Alias ChangeProposalEscalatePayload
	return json.Unmarshal(data, (*Alias)(p))
}

// ChangeProposalErrorPayload is published on unexpected errors.
type ChangeProposalErrorPayload struct {
	workflow.UserSignalErrorEvent
}

// Schema implements message.Payload.
func (p *ChangeProposalErrorPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "change-proposal-error", Version: "v1"}
}

// Validate implements message.Payload.
func (p *ChangeProposalErrorPayload) Validate() error {
	if p.Error == "" {
		return fmt.Errorf("error message is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *ChangeProposalErrorPayload) MarshalJSON() ([]byte, error) {
	type Alias ChangeProposalErrorPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *ChangeProposalErrorPayload) UnmarshalJSON(data []byte) error {
	type Alias ChangeProposalErrorPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// ---------------------------------------------------------------------------
// BuildChangeProposalLoopWorkflow
// ---------------------------------------------------------------------------

// BuildChangeProposalLoopWorkflow constructs the change-proposal-loop reactive
// workflow. This is an OODA loop that manages the lifecycle of a ChangeProposal:
//
//  1. Observe:  Proposal trigger received → populate state.
//  2. Orient:   Dispatch to change-proposal-reviewer (LLM or human gate).
//  3. Decide:   Evaluate the reviewer's verdict (accepted/rejected).
//  4. Act:      Accepted → dispatch cascade handler, publish task.dirty events.
//     Rejected → archive proposal, publish rejected event.
func BuildChangeProposalLoopWorkflow(stateBucket string) *reactiveEngine.Definition {
	maxIterations := 1 // ChangeProposal review is a one-shot decision.

	verdictGetter := func(state any) string {
		if s, ok := state.(*ChangeProposalState); ok {
			return s.Verdict
		}
		return ""
	}

	return reactiveEngine.NewWorkflow(ChangeProposalLoopWorkflowID).
		WithDescription("OODA loop for ChangeProposal review and cascade").
		WithStateBucket(stateBucket).
		WithStateFactory(func() any { return &ChangeProposalState{} }).
		WithMaxIterations(maxIterations).
		WithTimeout(15 * time.Minute).

		// Rule 1: accept-trigger — populate state from the JetStream trigger message.
		AddRule(reactiveEngine.NewRule("accept-trigger").
			OnJetStreamSubject("WORKFLOW", "workflow.trigger.change-proposal-loop", func() any { return &workflow.TriggerPayload{} }).
			WithStateLookup(stateBucket, func(msg any) string {
				trigger, ok := msg.(*workflow.TriggerPayload)
				if !ok {
					return ""
				}
				return "change-proposal." + trigger.Slug + "." + trigger.ProposalID
			}).
			When("always", reactiveEngine.Always()).
			Mutate(changeProposalAcceptTrigger).
			MustBuild()).

		// Rule 2: dispatch-review — fire-and-forget dispatch to the reviewer.
		AddRule(reactiveEngine.NewRule("dispatch-review").
			WatchKV(stateBucket, "change-proposal.>").
			When("phase is reviewing", reactiveEngine.PhaseIs(phases.ChangeProposalReviewing)).
			PublishWithMutation("workflow.async.change-proposal-reviewer", changeProposalBuildReviewPayload, setPhase(phases.ChangeProposalReviewingDispatched)).
			MustBuild()).

		// Rule 3: review-completed — react to reviewer setting "reviewed" phase.
		AddRule(reactiveEngine.NewRule("review-completed").
			WatchKV(stateBucket, "change-proposal.>").
			When("phase is reviewed", reactiveEngine.PhaseIs(phases.ChangeProposalReviewed)).
			Mutate(setPhase(phases.ChangeProposalEvaluated)).
			MustBuild()).

		// Rule 4: handle-accepted — dispatch cascade to the cascade handler component.
		// The not-completed condition prevents infinite re-firing.
		AddRule(reactiveEngine.NewRule("handle-accepted").
			WatchKV(stateBucket, "change-proposal.>").
			When("phase is evaluated", reactiveEngine.PhaseIs(phases.ChangeProposalEvaluated)).
			When("verdict is accepted", stateFieldEquals(verdictGetter, "accepted")).
			When("not completed", notCompleted()).
			PublishWithMutation("workflow.async.change-proposal-cascade", changeProposalBuildCascadePayload, setPhase(phases.ChangeProposalCascading)).
			MustBuild()).

		// Rule 5: cascade-completed — react to cascade handler setting "cascade_complete" phase.
		// The not-completed condition prevents infinite re-firing.
		AddRule(reactiveEngine.NewRule("cascade-completed").
			WatchKV(stateBucket, "change-proposal.>").
			When("phase is cascade_complete", reactiveEngine.PhaseIs(phases.ChangeProposalCascadeComplete)).
			When("not completed", notCompleted()).
			CompleteWithEvent("workflow.events.change_proposal.accepted", changeProposalBuildAcceptedEvent).
			MustBuild()).

		// Rule 6: handle-rejected — archive proposal on rejection.
		// The not-completed condition prevents infinite re-firing.
		AddRule(reactiveEngine.NewRule("handle-rejected").
			WatchKV(stateBucket, "change-proposal.>").
			When("phase is evaluated", reactiveEngine.PhaseIs(phases.ChangeProposalEvaluated)).
			When("verdict is not accepted", stateFieldNotEquals(verdictGetter, "accepted")).
			When("not completed", notCompleted()).
			CompleteWithEvent("workflow.events.change_proposal.rejected", changeProposalBuildRejectedEvent).
			MustBuild()).

		// Rule 7: handle-error — emit error signal on any failure phase.
		// The not-completed condition prevents infinite re-firing.
		AddRule(reactiveEngine.NewRule("handle-error").
			WatchKV(stateBucket, "change-proposal.>").
			When("phase is error", reactiveEngine.ConditionHelpers.PhaseIn(
				phases.ChangeProposalReviewerFailed,
				phases.ChangeProposalCascadeFailed,
			)).
			When("not completed", notCompleted()).
			PublishWithMutation("user.signal.error", changeProposalBuildErrorEvent, changeProposalMutateError).
			MustBuild()).
		MustBuild()
}

// ---------------------------------------------------------------------------
// Mutators
// ---------------------------------------------------------------------------

// changeProposalAcceptTrigger populates ChangeProposalState from the incoming
// TriggerPayload and transitions to the "reviewing" phase.
var changeProposalAcceptTrigger reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*ChangeProposalState)
	if !ok {
		return fmt.Errorf("accept-trigger: expected *ChangeProposalState, got %T", ctx.State)
	}

	trigger, ok := ctx.Message.(*workflow.TriggerPayload)
	if !ok {
		return fmt.Errorf("accept-trigger: expected *workflow.TriggerPayload, got %T", ctx.Message)
	}

	if trigger.ProposalID == "" {
		return fmt.Errorf("accept-trigger: proposal_id missing from trigger")
	}
	if trigger.Slug == "" {
		return fmt.Errorf("accept-trigger: slug missing from trigger")
	}

	state.Slug = trigger.Slug
	state.ProposalID = trigger.ProposalID
	state.ProjectID = trigger.ProjectID
	state.TraceID = trigger.TraceID

	if state.ID == "" {
		state.ID = "change-proposal." + trigger.Slug + "." + trigger.ProposalID
		state.WorkflowID = ChangeProposalLoopWorkflowID
		state.Status = reactiveEngine.StatusRunning
		now := time.Now()
		state.CreatedAt = now
		state.UpdatedAt = now
	}

	state.Phase = phases.ChangeProposalReviewing
	return nil
}

// changeProposalMutateError marks the execution as failed.
var changeProposalMutateError reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*ChangeProposalState)
	if !ok {
		return fmt.Errorf("error mutator: expected *ChangeProposalState, got %T", ctx.State)
	}
	errMsg := state.Error
	if errMsg == "" {
		errMsg = "change-proposal workflow step failed in phase: " + state.Phase
	}
	reactiveEngine.FailExecution(state, errMsg)
	return nil
}

// ---------------------------------------------------------------------------
// Payload builders
// ---------------------------------------------------------------------------

// changeProposalBuildReviewPayload constructs a ChangeProposalReviewRequest from state.
func changeProposalBuildReviewPayload(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*ChangeProposalState)
	if !ok {
		return nil, fmt.Errorf("review payload: expected *ChangeProposalState, got %T", ctx.State)
	}

	return &ChangeProposalReviewRequest{
		ExecutionID: state.ID,
		ProposalID:  state.ProposalID,
		PlanID:      state.ProjectID,
		Slug:        state.Slug,
		TraceID:     state.TraceID,
	}, nil
}

// changeProposalBuildCascadePayload constructs a CascadeRequest dispatched to the
// cascade handler component when a proposal is accepted.
func changeProposalBuildCascadePayload(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*ChangeProposalState)
	if !ok {
		return nil, fmt.Errorf("cascade payload: expected *ChangeProposalState, got %T", ctx.State)
	}

	return &ChangeProposalCascadeRequest{
		ExecutionID: state.ID,
		ProposalID:  state.ProposalID,
		Slug:        state.Slug,
		TraceID:     state.TraceID,
	}, nil
}

// changeProposalBuildAcceptedEvent constructs a ChangeProposalAcceptedPayload
// for the workflow completion event.
func changeProposalBuildAcceptedEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*ChangeProposalState)
	if !ok {
		return nil, fmt.Errorf("accepted event: expected *ChangeProposalState, got %T", ctx.State)
	}

	return &ChangeProposalAcceptedPayload{
		ProposalID:      state.ProposalID,
		PlanID:          state.ProjectID,
		Slug:            state.Slug,
		AffectedTaskIDs: state.AffectedTaskIDs,
	}, nil
}

// changeProposalBuildRejectedEvent constructs a ChangeProposalRejectedPayload.
func changeProposalBuildRejectedEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*ChangeProposalState)
	if !ok {
		return nil, fmt.Errorf("rejected event: expected *ChangeProposalState, got %T", ctx.State)
	}

	return &ChangeProposalRejectedPayload{
		ProposalID: state.ProposalID,
		PlanID:     state.ProjectID,
		Slug:       state.Slug,
		Feedback:   state.Feedback,
	}, nil
}

// changeProposalBuildErrorEvent constructs a ChangeProposalErrorPayload from state.
func changeProposalBuildErrorEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*ChangeProposalState)
	if !ok {
		return nil, fmt.Errorf("error event: expected *ChangeProposalState, got %T", ctx.State)
	}

	errMsg := state.Error
	if errMsg == "" {
		errMsg = "change-proposal workflow step failed in phase: " + state.Phase
	}

	return &ChangeProposalErrorPayload{
		UserSignalErrorEvent: workflow.UserSignalErrorEvent{
			Slug:  state.Slug,
			Error: errMsg,
		},
	}, nil
}
