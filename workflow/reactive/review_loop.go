package reactive

import (
	"time"

	reactiveEngine "github.com/c360studio/semstreams/processor/reactive"
)

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
// All three workflows follow the same 7-rule structure:
//  1. accept-trigger — populate state from trigger message, phase -> generating
//  2. generate — dispatch to generator component (PublishAsync)
//  3. review — dispatch to reviewer component (PublishAsync)
//  4. handle-approved — publish approved event, complete execution
//  5. handle-revision — publish revision event, increment iteration, loop back
//  6. handle-escalation — publish escalation event, mark escalated
//  7. handle-error — publish error event, mark failed
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
	// and initialises execution metadata. Must set phase to ReviewPhaseGenerating.
	AcceptTrigger reactiveEngine.StateMutatorFunc

	// VerdictAccessor returns the verdict string from the typed workflow state.
	// Used by the shared builder to create verdictIs / verdictIsNot conditions
	// without knowing the concrete state type.
	VerdictAccessor func(state any) string

	// ── Generator (async dispatch) ──

	GeneratorSubject        string
	GeneratorResultTypeKey  string
	BuildGeneratorPayload   reactiveEngine.PayloadBuilderFunc
	MutateOnGeneratorResult reactiveEngine.StateMutatorFunc

	// ── Reviewer (async dispatch) ──

	ReviewerSubject        string
	ReviewerResultTypeKey  string
	BuildReviewerPayload   reactiveEngine.PayloadBuilderFunc
	MutateOnReviewerResult reactiveEngine.StateMutatorFunc

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
}

// ---------------------------------------------------------------------------
// BuildReviewLoopWorkflow
// ---------------------------------------------------------------------------

// BuildReviewLoopWorkflow constructs a reactive workflow definition using the
// standard OODA review loop pattern. The 7 rules are structurally identical
// across all review loops; only the subjects, payloads, and state types differ.
//
// Phase transitions:
//
//	trigger -> generating -> reviewing -> evaluated
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

		// Rule 2: generate — dispatch to generator when in generating phase.
		AddRule(reactiveEngine.NewRule("generate").
			WatchKV(cfg.StateBucket, cfg.KVKeyPattern).
			When("phase is generating", reactiveEngine.PhaseIs(ReviewPhaseGenerating)).
			When("no pending task", reactiveEngine.NoPendingTask()).
			PublishAsync(
				cfg.GeneratorSubject,
				cfg.BuildGeneratorPayload,
				cfg.GeneratorResultTypeKey,
				cfg.MutateOnGeneratorResult,
			).
			MustBuild()).

		// Rule 3: review — dispatch to reviewer when in reviewing phase.
		AddRule(reactiveEngine.NewRule("review").
			WatchKV(cfg.StateBucket, cfg.KVKeyPattern).
			When("phase is reviewing", reactiveEngine.PhaseIs(ReviewPhaseReviewing)).
			When("no pending task", reactiveEngine.NoPendingTask()).
			PublishAsync(
				cfg.ReviewerSubject,
				cfg.BuildReviewerPayload,
				cfg.ReviewerResultTypeKey,
				cfg.MutateOnReviewerResult,
			).
			MustBuild()).

		// Rule 4: handle-approved — complete when verdict is approved.
		AddRule(reactiveEngine.NewRule("handle-approved").
			WatchKV(cfg.StateBucket, cfg.KVKeyPattern).
			When("phase is evaluated", reactiveEngine.PhaseIs(ReviewPhaseEvaluated)).
			When("verdict is approved", verdictIs(cfg.VerdictAccessor, "approved")).
			CompleteWithEvent(
				cfg.ApprovedEventSubject,
				cfg.BuildApprovedEvent,
			).
			MustBuild()).

		// Rule 5: handle-revision — loop back when not approved and iterations remain.
		AddRule(reactiveEngine.NewRule("handle-revision").
			WatchKV(cfg.StateBucket, cfg.KVKeyPattern).
			When("phase is evaluated", reactiveEngine.PhaseIs(ReviewPhaseEvaluated)).
			When("verdict is not approved", verdictIsNot(cfg.VerdictAccessor, "approved")).
			When("iteration under max", reactiveEngine.IterationLessThan(cfg.MaxIterations)).
			PublishWithMutation(
				cfg.RevisionEventSubject,
				cfg.BuildRevisionEvent,
				cfg.MutateOnRevision,
			).
			MustBuild()).

		// Rule 6: handle-escalation — escalate when max iterations exceeded.
		AddRule(reactiveEngine.NewRule("handle-escalation").
			WatchKV(cfg.StateBucket, cfg.KVKeyPattern).
			When("phase is evaluated", reactiveEngine.PhaseIs(ReviewPhaseEvaluated)).
			When("verdict is not approved", verdictIsNot(cfg.VerdictAccessor, "approved")).
			When("at or over max iterations", reactiveEngine.Not(reactiveEngine.IterationLessThan(cfg.MaxIterations))).
			PublishWithMutation(
				cfg.EscalateSubject,
				cfg.BuildEscalateEvent,
				cfg.MutateOnEscalation,
			).
			MustBuild()).

		// Rule 7: handle-error — signal error on component failure phases.
		AddRule(reactiveEngine.NewRule("handle-error").
			WatchKV(cfg.StateBucket, cfg.KVKeyPattern).
			When("phase is a failure phase", reactiveEngine.PhaseIsAny(
				ReviewPhaseGeneratorFailed,
				ReviewPhaseReviewerFailed,
			)).
			PublishWithMutation(
				cfg.ErrorSubject,
				cfg.BuildErrorEvent,
				cfg.MutateOnError,
			).
			MustBuild()).
		MustBuild()
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
