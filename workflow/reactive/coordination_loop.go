package reactive

import (
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/phases"
	reactiveEngine "github.com/c360studio/semstreams/processor/reactive"
)

// Ensure workflow package is referenced for error event types.
var _ = workflow.UserSignalErrorEvent{}

// BuildCoordinationLoopWorkflow constructs the coordination-loop reactive workflow.
//
// Unlike the OODA review loop, the coordination loop has a fan-out/fan-in pattern:
//
//  1. accept-trigger — populate state from trigger, phase → focusing
//  2. dispatch-focus — dispatch to focus handler, phase → focus_dispatched
//  3. [focus handler determines focuses, dispatches N planner messages, sets phase → planners_dispatched]
//  4. planner-result — merge each planner result into state (engine is single KV writer)
//     when all planners done → phase = synthesizing
//  5. dispatch-synthesis — dispatch to synthesis handler, phase → synthesis_dispatched
//  6. [synthesis handler synthesizes, saves plan, sets phase → synthesized]
//  7. coordination-complete — complete execution, publish completed event
//  8. handle-error — failure phases → publish error, mark failed
//
// The reactive engine acts as the single KV writer for planner results,
// eliminating CAS retry complexity for concurrent planner updates.
func BuildCoordinationLoopWorkflow(stateBucket string) *reactiveEngine.Definition {
	kvPattern := "coordination.*"

	return reactiveEngine.NewWorkflow(CoordinationLoopWorkflowID).
		WithDescription("Coordinate parallel planners with fan-out/fan-in pattern for plan generation.").
		WithStateBucket(stateBucket).
		WithStateFactory(func() any { return &CoordinationState{} }).
		WithMaxIterations(1). // No iteration loop — single pass fan-out/fan-in.
		WithTimeout(10 * time.Minute).

		// Rule 1: accept-trigger — populate state from incoming trigger.
		AddRule(reactiveEngine.NewRule("accept-trigger").
			OnJetStreamSubject("WORKFLOW", "workflow.trigger.plan-coordinator", func() any { return &PlanCoordinatorRequest{} }).
			WithStateLookup(stateBucket, func(msg any) string {
				trigger, ok := msg.(*PlanCoordinatorRequest)
				if !ok {
					return ""
				}
				return "coordination." + trigger.Slug
			}).
			When("always", reactiveEngine.Always()).
			Mutate(coordinationAcceptTrigger).
			MustBuild()).

		// Rule 2: dispatch-focus — fire-and-forget dispatch to focus handler.
		// Transitions to focus_dispatched to prevent re-dispatch.
		AddRule(reactiveEngine.NewRule("dispatch-focus").
			WatchKV(stateBucket, kvPattern).
			When("phase is focusing", reactiveEngine.PhaseIs(phases.CoordinationFocusing)).
			PublishWithMutation(
				"workflow.async.coordination-focus",
				coordinationBuildFocusPayload,
				setPhase(phases.CoordinationFocusDispatched),
			).
			MustBuild()).

		// Rule 3: planner-result — merge each planner result into state.
		// The engine processes results sequentially (single KV writer).
		// When all planners have reported, the mutator transitions to synthesizing
		// (or planners_failed if all failed).
		AddRule(reactiveEngine.NewRule("planner-result").
			OnJetStreamSubject("WORKFLOW", "workflow.result.coordination-planner.*", func() any { return &CoordinationPlannerResult{} }).
			WithStateLookup(stateBucket, func(msg any) string {
				result, ok := msg.(*CoordinationPlannerResult)
				if !ok {
					return ""
				}
				return "coordination." + result.Slug
			}).
			When("always", reactiveEngine.Always()).
			Mutate(coordinationMergePlannerResult).
			MustBuild()).

		// Rule 4: dispatch-synthesis — dispatch to synthesis handler.
		// Transitions to synthesis_dispatched to prevent re-dispatch.
		AddRule(reactiveEngine.NewRule("dispatch-synthesis").
			WatchKV(stateBucket, kvPattern).
			When("phase is synthesizing", reactiveEngine.PhaseIs(phases.CoordinationSynthesizing)).
			PublishWithMutation(
				"workflow.async.coordination-synthesis",
				coordinationBuildSynthesisPayload,
				setPhase(phases.CoordinationSynthesisDispatched),
			).
			MustBuild()).

		// Rule 5: coordination-complete — complete when synthesis is done.
		AddRule(reactiveEngine.NewRule("coordination-complete").
			WatchKV(stateBucket, kvPattern).
			When("phase is synthesized", reactiveEngine.PhaseIs(phases.CoordinationSynthesized)).
			When("not completed", notCompleted()).
			CompleteWithEvent(
				"workflow.events.coordination.completed",
				coordinationBuildCompletedEvent,
			).
			MustBuild()).

		// Rule 6: handle-error — signal error on failure phases.
		AddRule(reactiveEngine.NewRule("handle-error").
			WatchKV(stateBucket, kvPattern).
			When("phase is a failure phase", reactiveEngine.ConditionHelpers.PhaseIn(
				phases.CoordinationFocusFailed,
				phases.CoordinationPlannersFailed,
				phases.CoordinationSynthesisFailed,
			)).
			When("not completed", notCompleted()).
			PublishWithMutation(
				"user.signal.error",
				coordinationBuildErrorEvent,
				coordinationHandleError,
			).
			MustBuild()).
		MustBuild()
}
