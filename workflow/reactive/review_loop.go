package reactive

import (
	"fmt"
	"time"

	reactiveEngine "github.com/c360studio/semstreams/processor/reactive"
)

// Ensure time is used (will be used by workflow definitions).
var _ = time.Second

// ---------------------------------------------------------------------------
// Shared OODA review loop phases
// ---------------------------------------------------------------------------

// Phase constants shared by all OODA review loop workflows (plan-review-loop,
// phase-review-loop, task-review-loop).
const (
	ReviewPhaseGenerating      = "generating"
	ReviewPhaseReviewing       = "reviewing"
	ReviewPhaseEvaluated       = "evaluated"
	ReviewPhaseGeneratorFailed = "generator_failed"
	ReviewPhaseReviewerFailed  = "reviewer_failed"
)

// ---------------------------------------------------------------------------
// ReviewLoopConfig
// ---------------------------------------------------------------------------

// ReviewLoopConfig parameterizes the OODA review loop pattern shared by
// plan-review-loop, phase-review-loop, and task-review-loop.
//
// The Participant pattern uses fire-and-forget dispatch with KV-watch reactions:
//
//  1. accept-trigger — populate state from trigger message, phase -> generating
//  2. dispatch-generator — publish to generator, phase -> dispatched
//  3. generator-completed — react to generator setting completion phase -> reviewing
//  4. dispatch-reviewer — publish to reviewer, phase -> dispatched
//  5. reviewer-completed — react to reviewer setting completion phase -> evaluated
//  6. handle-approved — publish approved event, complete execution
//  7. handle-revision — publish revision event, increment iteration, loop back
//  8. handle-escalation — publish escalation event, mark escalated
//  9. handle-error — publish error event, mark failed
//
// Components set their completion phases directly via StateManager.Transition().
// The engine watches KV for phase changes and fires the appropriate rules.
type ReviewLoopConfig struct {
	// ── Workflow metadata ──

	WorkflowID    string
	Description   string
	StateBucket   string
	MaxIterations int
	Timeout       time.Duration
	StateFactory  func() any

	// ── Trigger ──

	TriggerStream         string
	TriggerSubject        string
	TriggerMessageFactory func() any
	StateLookupKey        func(msg any) string

	// KVKeyPattern is the glob for KV-watch rules (e.g., "plan-review.*").
	KVKeyPattern string

	// AcceptTrigger populates workflow state from the incoming trigger message
	// and initialises execution metadata. Must set phase to GeneratingPhase.
	AcceptTrigger reactiveEngine.StateMutatorFunc

	// VerdictAccessor returns the verdict string from the typed workflow state.
	// Used by the shared builder to create verdictIs / verdictIsNot conditions
	// without knowing the concrete state type.
	VerdictAccessor func(state any) string

	// ── Generator (Participant pattern) ──

	GeneratorSubject      string // Subject to publish to (fire-and-forget)
	BuildGeneratorPayload reactiveEngine.PayloadBuilderFunc

	// GeneratingPhase is the phase that triggers dispatch (e.g., "generating").
	GeneratingPhase string
	// GeneratorDispatchedPhase is the phase set after dispatch (e.g., "planning").
	GeneratorDispatchedPhase string
	// GeneratorCompletedPhase is the phase set by the generator component (e.g., "planned").
	GeneratorCompletedPhase string

	// ── Reviewer (Participant pattern) ──

	ReviewerSubject      string // Subject to publish to (fire-and-forget)
	BuildReviewerPayload reactiveEngine.PayloadBuilderFunc

	// ReviewingPhase is the phase that triggers dispatch (e.g., "reviewing").
	ReviewingPhase string
	// ReviewerDispatchedPhase is the phase set after dispatch (e.g., "reviewing_dispatched").
	ReviewerDispatchedPhase string
	// ReviewerCompletedPhase is the phase set by the reviewer component (e.g., "reviewed").
	ReviewerCompletedPhase string
	// EvaluatedPhase is the phase set after reviewer completion (e.g., "evaluated").
	EvaluatedPhase string

	// ── Events ──

	ApprovedEventSubject string
	BuildApprovedEvent   reactiveEngine.PayloadBuilderFunc

	RevisionEventSubject string
	BuildRevisionEvent   reactiveEngine.PayloadBuilderFunc
	MutateOnRevision     reactiveEngine.StateMutatorFunc

	EscalateSubject    string
	BuildEscalateEvent reactiveEngine.PayloadBuilderFunc
	MutateOnEscalation reactiveEngine.StateMutatorFunc

	ErrorSubject    string
	BuildErrorEvent reactiveEngine.PayloadBuilderFunc
	MutateOnError   reactiveEngine.StateMutatorFunc

	// ── Failure phases ──

	GeneratorFailedPhase string // e.g., "generator_failed"
	ReviewerFailedPhase  string // e.g., "reviewer_failed"
}

// ---------------------------------------------------------------------------
// BuildReviewLoopWorkflow
// ---------------------------------------------------------------------------

// BuildReviewLoopWorkflow constructs a reactive workflow definition using the
// Participant pattern. Components set their completion phases directly via
// StateManager.Transition(), and the engine watches KV to fire rules.
//
// Phase transitions (Participant pattern):
//
//	trigger -> generating -> dispatched -> planned (component sets) ->
//	reviewing -> dispatched -> reviewed (component sets) -> evaluated
//	                                      |-- approved  -> complete
//	                                      |-- rejected (iter < max) -> generating (revision)
//	                                      '-- rejected (iter >= max) -> escalated
//	         -> generator_failed -> failed
//	         -> reviewer_failed  -> failed
func BuildReviewLoopWorkflow(cfg ReviewLoopConfig) *reactiveEngine.Definition {
	return reactiveEngine.NewWorkflow(cfg.WorkflowID).
		WithDescription(cfg.Description).
		WithStateBucket(cfg.StateBucket).
		WithStateFactory(cfg.StateFactory).
		WithMaxIterations(cfg.MaxIterations).
		WithTimeout(cfg.Timeout).

		// Rule 1: accept-trigger — populate state from incoming trigger message.
		AddRule(reactiveEngine.NewRule("accept-trigger").
			OnJetStreamSubject(cfg.TriggerStream, cfg.TriggerSubject, cfg.TriggerMessageFactory).
			WithStateLookup(cfg.StateBucket, cfg.StateLookupKey).
			When("always", reactiveEngine.Always()).
			Mutate(cfg.AcceptTrigger).
			MustBuild()).

		// Rule 2: dispatch-generator — fire-and-forget dispatch to generator.
		// Transitions to dispatched phase to prevent re-dispatch.
		AddRule(reactiveEngine.NewRule("dispatch-generator").
			WatchKV(cfg.StateBucket, cfg.KVKeyPattern).
			When("phase is generating", reactiveEngine.PhaseIs(cfg.GeneratingPhase)).
			PublishWithMutation(
				cfg.GeneratorSubject,
				cfg.BuildGeneratorPayload,
				setPhase(cfg.GeneratorDispatchedPhase),
			).
			MustBuild()).

		// Rule 3: generator-completed — react to generator setting completion phase.
		// Advances workflow to the reviewing phase.
		AddRule(reactiveEngine.NewRule("generator-completed").
			WatchKV(cfg.StateBucket, cfg.KVKeyPattern).
			When("phase is generator-completed", reactiveEngine.PhaseIs(cfg.GeneratorCompletedPhase)).
			Mutate(setPhase(cfg.ReviewingPhase)).
			MustBuild()).

		// Rule 4: dispatch-reviewer — fire-and-forget dispatch to reviewer.
		// Transitions to dispatched phase to prevent re-dispatch.
		AddRule(reactiveEngine.NewRule("dispatch-reviewer").
			WatchKV(cfg.StateBucket, cfg.KVKeyPattern).
			When("phase is reviewing", reactiveEngine.PhaseIs(cfg.ReviewingPhase)).
			PublishWithMutation(
				cfg.ReviewerSubject,
				cfg.BuildReviewerPayload,
				setPhase(cfg.ReviewerDispatchedPhase),
			).
			MustBuild()).

		// Rule 5: reviewer-completed — react to reviewer setting completion phase.
		// Advances workflow to the evaluated phase for decision-making.
		AddRule(reactiveEngine.NewRule("reviewer-completed").
			WatchKV(cfg.StateBucket, cfg.KVKeyPattern).
			When("phase is reviewer-completed", reactiveEngine.PhaseIs(cfg.ReviewerCompletedPhase)).
			Mutate(setPhase(cfg.EvaluatedPhase)).
			MustBuild()).

		// Rule 6: handle-approved — complete when verdict is approved.
		// The not-completed condition prevents infinite re-firing since CompleteWithEvent
		// doesn't change the phase, only the status.
		AddRule(reactiveEngine.NewRule("handle-approved").
			WatchKV(cfg.StateBucket, cfg.KVKeyPattern).
			When("phase is evaluated", reactiveEngine.PhaseIs(cfg.EvaluatedPhase)).
			When("verdict is approved", verdictIs(cfg.VerdictAccessor, "approved")).
			When("not completed", notCompleted()).
			CompleteWithEvent(
				cfg.ApprovedEventSubject,
				cfg.BuildApprovedEvent,
			).
			MustBuild()).

		// Rule 7: handle-revision — loop back when not approved and iterations remain.
		AddRule(reactiveEngine.NewRule("handle-revision").
			WatchKV(cfg.StateBucket, cfg.KVKeyPattern).
			When("phase is evaluated", reactiveEngine.PhaseIs(cfg.EvaluatedPhase)).
			When("verdict is not approved", verdictIsNot(cfg.VerdictAccessor, "approved")).
			When("iteration under max", reactiveEngine.ConditionHelpers.IterationLessThan(cfg.MaxIterations)).
			PublishWithMutation(
				cfg.RevisionEventSubject,
				cfg.BuildRevisionEvent,
				cfg.MutateOnRevision,
			).
			MustBuild()).

		// Rule 8: handle-escalation — escalate when max iterations exceeded.
		// The not-completed condition prevents infinite re-firing.
		AddRule(reactiveEngine.NewRule("handle-escalation").
			WatchKV(cfg.StateBucket, cfg.KVKeyPattern).
			When("phase is evaluated", reactiveEngine.PhaseIs(cfg.EvaluatedPhase)).
			When("verdict is not approved", verdictIsNot(cfg.VerdictAccessor, "approved")).
			When("at or over max iterations", reactiveEngine.Not(reactiveEngine.ConditionHelpers.IterationLessThan(cfg.MaxIterations))).
			When("not completed", notCompleted()).
			PublishWithMutation(
				cfg.EscalateSubject,
				cfg.BuildEscalateEvent,
				cfg.MutateOnEscalation,
			).
			MustBuild()).

		// Rule 9: handle-error — signal error on component failure phases.
		// The not-completed condition prevents infinite re-firing.
		AddRule(reactiveEngine.NewRule("handle-error").
			WatchKV(cfg.StateBucket, cfg.KVKeyPattern).
			When("phase is a failure phase", reactiveEngine.ConditionHelpers.PhaseIn(
				cfg.GeneratorFailedPhase,
				cfg.ReviewerFailedPhase,
			)).
			When("not completed", notCompleted()).
			PublishWithMutation(
				cfg.ErrorSubject,
				cfg.BuildErrorEvent,
				cfg.MutateOnError,
			).
			MustBuild()).
		MustBuild()
}

// setPhase returns a StateMutatorFunc that sets the execution phase.
// Used by the workflow builder to create phase transition mutators.
func setPhase(phase string) reactiveEngine.StateMutatorFunc {
	return func(ctx *reactiveEngine.RuleContext, _ any) error {
		if ctx.State == nil {
			return fmt.Errorf("setPhase: state is nil")
		}
		// Use the StateAccessor interface if available.
		if accessor, ok := ctx.State.(reactiveEngine.StateAccessor); ok {
			accessor.GetExecutionState().Phase = phase
			return nil
		}
		return fmt.Errorf("setPhase: state does not implement StateAccessor")
	}
}

// ---------------------------------------------------------------------------
// Shared condition helpers
// ---------------------------------------------------------------------------

// verdictIs returns a ConditionFunc that checks the state verdict equals v.
// The accessor is provided per-workflow to handle typed state assertion.
func verdictIs(accessor func(state any) string, v string) reactiveEngine.ConditionFunc {
	return func(ctx *reactiveEngine.RuleContext) bool {
		if ctx.State == nil {
			return false
		}
		return accessor(ctx.State) == v
	}
}

// verdictIsNot returns a ConditionFunc that checks the state verdict does not equal v.
func verdictIsNot(accessor func(state any) string, v string) reactiveEngine.ConditionFunc {
	return func(ctx *reactiveEngine.RuleContext) bool {
		if ctx.State == nil {
			return false
		}
		return accessor(ctx.State) != v
	}
}

// notCompleted returns a ConditionFunc that checks the execution is not in a terminal state.
// This prevents terminal rules (handle-approved, handle-escalation, handle-error) from
// re-firing after they complete the execution, since CompleteWithEvent/PublishWithMutation
// changes status but not phase.
// Terminal states include: completed, failed, escalated, timed_out.
func notCompleted() reactiveEngine.ConditionFunc {
	return func(ctx *reactiveEngine.RuleContext) bool {
		return !reactiveEngine.IsTerminal(ctx.State)
	}
}

// stateFieldEquals returns a ConditionFunc that checks if a state field equals the expected value.
// This is a generic version that works with any comparable type.
func stateFieldEquals[T comparable](getter func(state any) T, expected T) reactiveEngine.ConditionFunc {
	return func(ctx *reactiveEngine.RuleContext) bool {
		if ctx.State == nil {
			return false
		}
		return getter(ctx.State) == expected
	}
}

// stateFieldNotEquals returns a ConditionFunc that checks if a state field does not equal the value.
func stateFieldNotEquals[T comparable](getter func(state any) T, value T) reactiveEngine.ConditionFunc {
	return func(ctx *reactiveEngine.RuleContext) bool {
		if ctx.State == nil {
			return false
		}
		return getter(ctx.State) != value
	}
}
