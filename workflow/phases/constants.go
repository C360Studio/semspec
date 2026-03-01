// Package phases provides workflow phase constants for the reactive workflow engine.
//
// In the Participant pattern, components update workflow state directly via StateManager.
// Components transition to their "completion" phase after storing results. The reactive
// engine watches KV for phase changes and fires rules to advance the workflow.
//
// Phase flow for plan-review workflow:
//
//	generating -> planned -> reviewing -> reviewed -> evaluated
//	                                                    ├── approved -> complete
//	                                                    ├── needs_changes (iter < max) -> generating
//	                                                    └── needs_changes (iter >= max) -> escalated
package phases

// Plan review workflow phases.
//
// Components: planner, plan-reviewer
//
// Phase flow (Participant pattern):
//
//	generating -> planning (dispatched) -> planned (planner done) ->
//	reviewing -> reviewing_dispatched -> reviewed (reviewer done) ->
//	evaluated -> decision (approved/revision/escalation)
const (
	// PlanGenerating is the initial phase when a plan review starts.
	// The engine will dispatch to the planner when it sees this phase.
	PlanGenerating = "generating"

	// PlanPlanning indicates the engine has dispatched to the planner.
	// This prevents re-dispatch while waiting for the planner.
	PlanPlanning = "planning"

	// PlanPlanned is set by the planner when plan generation completes.
	// The state contains PlanContent and LLMRequestIDs.
	PlanPlanned = "planned"

	// PlanReviewing indicates the plan is ready for review.
	// The engine will dispatch to the plan-reviewer when it sees this phase.
	PlanReviewing = "reviewing"

	// PlanReviewingDispatched indicates the engine has dispatched to the reviewer.
	// This prevents re-dispatch while waiting for the reviewer.
	PlanReviewingDispatched = "reviewing_dispatched"

	// PlanReviewed is set by the plan-reviewer when review completes.
	// The state contains Verdict, Summary, and Findings.
	PlanReviewed = "reviewed"

	// PlanEvaluated indicates the review has been evaluated.
	// The engine checks the verdict and takes appropriate action.
	PlanEvaluated = "evaluated"

	// PlanGeneratorFailed indicates the planner failed.
	PlanGeneratorFailed = "generator_failed"

	// PlanReviewerFailed indicates the plan-reviewer failed.
	PlanReviewerFailed = "reviewer_failed"
)

// Phase review workflow phases.
//
// Components: phase-generator, plan-reviewer (reused for phase review)
//
// Phase flow (Participant pattern):
//
//	generating -> generating_dispatched -> phases-generated ->
//	reviewing -> reviewing_dispatched -> reviewed ->
//	evaluated -> decision
const (
	// PhaseGenerating is the initial phase when phase generation starts.
	PhaseGenerating = "generating"

	// PhaseGeneratingDispatched indicates dispatch to phase-generator.
	PhaseGeneratingDispatched = "generating_dispatched"

	// PhasesGenerated is set by the phase-generator when generation completes.
	PhasesGenerated = "phases-generated"

	// PhaseReviewing indicates phases are ready for review.
	PhaseReviewing = "reviewing"

	// PhaseReviewingDispatched indicates dispatch to reviewer.
	PhaseReviewingDispatched = "reviewing_dispatched"

	// PhaseReviewed is set by the reviewer when phase review completes.
	PhaseReviewed = "reviewed"

	// PhaseEvaluated indicates the review has been evaluated.
	PhaseEvaluated = "evaluated"

	// PhaseGeneratorFailed indicates the phase-generator failed.
	PhaseGeneratorFailed = "generator_failed"

	// PhaseReviewerFailed indicates the phase reviewer failed.
	PhaseReviewerFailed = "reviewer_failed"
)

// Task review workflow phases.
//
// Components: task-generator, task-reviewer
//
// Phase flow (Participant pattern):
//
//	generating -> generating_dispatched -> tasks-generated ->
//	reviewing -> reviewing_dispatched -> tasks-reviewed ->
//	evaluated -> decision
const (
	// TaskGenerating is the initial phase when task generation starts.
	TaskGenerating = "generating"

	// TaskGeneratingDispatched indicates dispatch to task-generator.
	TaskGeneratingDispatched = "generating_dispatched"

	// TasksGenerated is set by the task-generator when generation completes.
	TasksGenerated = "tasks-generated"

	// TaskReviewing indicates tasks are ready for review.
	TaskReviewing = "reviewing"

	// TaskReviewingDispatched indicates dispatch to task-reviewer.
	TaskReviewingDispatched = "reviewing_dispatched"

	// TasksReviewed is set by the task-reviewer when review completes.
	TasksReviewed = "tasks-reviewed"

	// TaskEvaluated indicates the review has been evaluated.
	TaskEvaluated = "evaluated"

	// TaskGeneratorFailed indicates the task-generator failed.
	TaskGeneratorFailed = "generator_failed"

	// TaskReviewerFailed indicates the task-reviewer failed.
	TaskReviewerFailed = "reviewer_failed"
)

// Task execution workflow phases.
//
// Components: developer (external), structural-validator, code-reviewer
//
// Phase flow (Participant pattern):
//
//	developing -> developing_dispatched -> developed ->
//	validating -> validating_dispatched -> validated ->
//	reviewing -> reviewing_dispatched -> reviewed ->
//	evaluated -> decision
const (
	// TaskExecDeveloping indicates a task is ready for development.
	TaskExecDeveloping = "developing"

	// TaskExecDevelopingDispatched indicates dispatch to developer.
	TaskExecDevelopingDispatched = "developing_dispatched"

	// TaskExecDeveloped is set by the developer when development completes.
	TaskExecDeveloped = "developed"

	// TaskExecValidating indicates code is ready for validation.
	TaskExecValidating = "validating"

	// TaskExecValidatingDispatched indicates dispatch to structural-validator.
	TaskExecValidatingDispatched = "validating_dispatched"

	// TaskExecValidated is set by the structural-validator when validation completes.
	TaskExecValidated = "validated"

	// TaskExecValidationChecked indicates validation has been evaluated.
	// The engine checks whether validation passed and takes appropriate action.
	TaskExecValidationChecked = "validation_checked"

	// TaskExecReviewing indicates code is ready for review.
	TaskExecReviewing = "reviewing"

	// TaskExecReviewingDispatched indicates dispatch to code-reviewer.
	TaskExecReviewingDispatched = "reviewing_dispatched"

	// TaskExecReviewed is set by the code-reviewer when review completes.
	TaskExecReviewed = "reviewed"

	// TaskExecEvaluated indicates the review has been evaluated.
	TaskExecEvaluated = "evaluated"

	// TaskExecDeveloperFailed indicates the developer failed.
	TaskExecDeveloperFailed = "developer_failed"

	// TaskExecValidationError indicates validation encountered an error.
	TaskExecValidationError = "validation_error"

	// TaskExecReviewerFailed indicates the code-reviewer failed.
	TaskExecReviewerFailed = "reviewer_failed"
)

// Plan coordination workflow phases.
//
// Components: plan-coordinator (with parallel planner fan-out)
//
// Phase flow (KV-backed coordination loop):
//
//	focusing -> focus_dispatched -> focused ->
//	planners_dispatched -> [CAS updates from parallel planners] ->
//	synthesizing -> synthesis_dispatched -> synthesized -> completed
const (
	// CoordinationFocusing is the initial phase when coordination starts.
	// The engine will dispatch to the focus handler when it sees this phase.
	CoordinationFocusing = "focusing"

	// CoordinationFocusDispatched indicates the engine has dispatched to the focus handler.
	CoordinationFocusDispatched = "focus_dispatched"

	// CoordinationFocused is set by the focus handler when focus determination completes.
	// The handler also dispatches N planner messages and transitions directly to
	// CoordinationPlannersDispatched.
	CoordinationFocused = "focused"

	// CoordinationPlannersDispatched indicates all planner messages have been dispatched.
	// The engine checks allPlannersDone() to advance to synthesizing.
	CoordinationPlannersDispatched = "planners_dispatched"

	// CoordinationSynthesizing indicates all planners completed and synthesis should start.
	CoordinationSynthesizing = "synthesizing"

	// CoordinationSynthesisDispatched indicates the engine has dispatched to the synthesis handler.
	CoordinationSynthesisDispatched = "synthesis_dispatched"

	// CoordinationSynthesized is set by the synthesis handler when synthesis completes.
	CoordinationSynthesized = "synthesized"

	// CoordinationFocusFailed indicates focus determination failed.
	CoordinationFocusFailed = "focus_failed"

	// CoordinationPlannersFailed indicates all planners failed (no usable results).
	CoordinationPlannersFailed = "planners_failed"

	// CoordinationSynthesisFailed indicates synthesis failed.
	CoordinationSynthesisFailed = "synthesis_failed"
)

// Task dispatch workflow phases.
//
// Components: task-dispatcher
const (
	// DispatchPending is the initial phase when dispatch starts.
	DispatchPending = "pending"

	// DispatchDispatched is set by the task-dispatcher when dispatch completes.
	DispatchDispatched = "dispatched"

	// DispatchFailed indicates the dispatcher failed.
	DispatchFailed = "failed"
)

// Verdict constants shared across workflows.
const (
	VerdictApproved     = "approved"
	VerdictNeedsChanges = "needs_changes"
	VerdictRejected     = "rejected"
)
