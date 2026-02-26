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
// Workflow ID constant
// ---------------------------------------------------------------------------

// TaskExecutionLoopWorkflowID is the unique identifier for the task execution loop.
const TaskExecutionLoopWorkflowID = "task-execution-loop"

// ---------------------------------------------------------------------------
// TaskExecutionState
// ---------------------------------------------------------------------------

// TaskExecutionState is the typed KV state for the task-execution-loop reactive
// workflow. It embeds ExecutionState for base lifecycle fields and adds
// task-execution-specific data for each stage of the pipeline.
type TaskExecutionState struct {
	reactiveEngine.ExecutionState

	// Trigger data populated on accept-trigger.
	Slug             string `json:"slug"`
	TaskID           string `json:"task_id"`
	Model            string `json:"model,omitempty"`
	Prompt           string `json:"prompt,omitempty"`
	ContextRequestID string `json:"context_request_id,omitempty"`

	// Developer output saved by taskExecHandleDeveloperResult.
	FilesModified   []string        `json:"files_modified,omitempty"`
	DeveloperOutput json.RawMessage `json:"developer_output,omitempty"`
	LLMRequestIDs   []string        `json:"llm_request_ids,omitempty"`

	// Validation output saved by taskExecHandleValidationResult.
	ValidationPassed bool            `json:"validation_passed"`
	ChecksRun        int             `json:"checks_run"`
	CheckResults     json.RawMessage `json:"check_results,omitempty"`

	// Reviewer output saved by taskExecHandleReviewResult.
	Verdict               string          `json:"verdict,omitempty"`
	RejectionType         string          `json:"rejection_type,omitempty"`
	Feedback              string          `json:"feedback,omitempty"`
	Patterns              json.RawMessage `json:"patterns,omitempty"`
	ReviewerLLMRequestIDs []string        `json:"reviewer_llm_request_ids,omitempty"`

	// RevisionSource distinguishes why we are returning to the developing phase.
	// "validation" means the structural validator rejected; "review" means the
	// reviewer issued a fixable rejection. The developer payload builder uses
	// this to include the appropriate feedback in the revision prompt.
	RevisionSource string `json:"revision_source,omitempty"` // "validation" | "review"
}

// GetExecutionState implements reactiveEngine.StateAccessor.
// This lets the engine read/write the embedded ExecutionState without reflection.
func (s *TaskExecutionState) GetExecutionState() *reactiveEngine.ExecutionState {
	return &s.ExecutionState
}

// ---------------------------------------------------------------------------
// Event payload types
// ---------------------------------------------------------------------------

// TaskValidationPassedPayload wraps workflow.StructuralValidationPassedEvent
// and satisfies message.Payload.
type TaskValidationPassedPayload struct {
	workflow.StructuralValidationPassedEvent
}

// Schema implements message.Payload.
func (p *TaskValidationPassedPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "task-validation-passed", Version: "v1"}
}

// Validate implements message.Payload.
func (p *TaskValidationPassedPayload) Validate() error {
	if p.TaskID == "" {
		return fmt.Errorf("task_id is required")
	}
	return nil
}

// TaskRejectionCategorizedPayload wraps workflow.RejectionCategorizedEvent
// and satisfies message.Payload.
type TaskRejectionCategorizedPayload struct {
	workflow.RejectionCategorizedEvent
}

// Schema implements message.Payload.
func (p *TaskRejectionCategorizedPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "task-rejection-categorized", Version: "v1"}
}

// Validate implements message.Payload.
func (p *TaskRejectionCategorizedPayload) Validate() error {
	if p.Type == "" {
		return fmt.Errorf("rejection type is required")
	}
	return nil
}

// TaskCompletePayload wraps workflow.TaskExecutionCompleteEvent
// and satisfies message.Payload.
type TaskCompletePayload struct {
	workflow.TaskExecutionCompleteEvent
}

// Schema implements message.Payload.
func (p *TaskCompletePayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "task-execution-complete", Version: "v1"}
}

// Validate implements message.Payload.
func (p *TaskCompletePayload) Validate() error {
	if p.TaskID == "" {
		return fmt.Errorf("task_id is required")
	}
	return nil
}

// TaskExecEscalatePayload wraps workflow.EscalationEvent and satisfies message.Payload.
type TaskExecEscalatePayload struct {
	workflow.EscalationEvent
}

// Schema implements message.Payload.
func (p *TaskExecEscalatePayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "task-exec-escalate", Version: "v1"}
}

// Validate implements message.Payload.
func (p *TaskExecEscalatePayload) Validate() error {
	if p.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	if p.Reason == "" {
		return fmt.Errorf("reason is required")
	}
	return nil
}

// TaskExecErrorPayload wraps workflow.UserSignalErrorEvent and satisfies message.Payload.
type TaskExecErrorPayload struct {
	workflow.UserSignalErrorEvent
}

// Schema implements message.Payload.
func (p *TaskExecErrorPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "task-exec-error", Version: "v1"}
}

// Validate implements message.Payload.
func (p *TaskExecErrorPayload) Validate() error {
	if p.Error == "" {
		return fmt.Errorf("error message is required")
	}
	return nil
}

// PlanRefinementTriggerPayload is published when the reviewer rejects a task
// as misscoped or architectural — the plan itself needs rework.
type PlanRefinementTriggerPayload struct {
	OriginalTaskID string `json:"original_task_id"`
	Feedback       string `json:"feedback"`
	PlanSlug       string `json:"plan_slug"`
	RejectionType  string `json:"rejection_type"`
}

// Schema implements message.Payload.
func (p *PlanRefinementTriggerPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "plan-refinement-trigger", Version: "v1"}
}

// Validate implements message.Payload.
func (p *PlanRefinementTriggerPayload) Validate() error {
	if p.PlanSlug == "" {
		return fmt.Errorf("plan_slug is required")
	}
	return nil
}

// TaskDecompositionTriggerPayload is published when the reviewer rejects a task
// as too_big — the task needs to be split into smaller pieces.
type TaskDecompositionTriggerPayload struct {
	OriginalTaskID string `json:"original_task_id"`
	Feedback       string `json:"feedback"`
	PlanSlug       string `json:"plan_slug"`
}

// Schema implements message.Payload.
func (p *TaskDecompositionTriggerPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "task-decomposition-trigger", Version: "v1"}
}

// Validate implements message.Payload.
func (p *TaskDecompositionTriggerPayload) Validate() error {
	if p.PlanSlug == "" {
		return fmt.Errorf("plan_slug is required")
	}
	return nil
}

// ---------------------------------------------------------------------------
// BuildTaskExecutionLoopWorkflow
// ---------------------------------------------------------------------------

// BuildTaskExecutionLoopWorkflow constructs the task-execution-loop reactive
// workflow. Unlike the shared OODA review loop, this is a 3-stage pipeline:
//
//  1. Developer agent produces code changes.
//  2. Structural validator checks that the changes compile / pass basic checks.
//  3. Reviewer agent performs a code-quality review with typed rejection categories.
//
// Rejection categories route to different outcomes:
//   - "fixable"       → retry developer up to maxIterations
//   - "misscoped"     → trigger plan refinement (exit)
//   - "architectural" → trigger plan refinement (exit)
//   - "too_big"       → trigger task decomposition (exit)
//   - other           → escalate (exit)
func BuildTaskExecutionLoopWorkflow(stateBucket string) *reactiveEngine.Definition {
	// maxIterations is the TOTAL retry budget shared across both validation failures
	// and reviewer fixable rejections. A task that fails validation twice has only
	// one remaining attempt for a reviewer fixable rejection.
	maxIterations := 3

	// Accessor helpers used in condition builders.
	verdictGetter := func(state any) string {
		if s, ok := state.(*TaskExecutionState); ok {
			return s.Verdict
		}
		return ""
	}
	rejectionGetter := func(state any) string {
		if s, ok := state.(*TaskExecutionState); ok {
			return s.RejectionType
		}
		return ""
	}
	validationPassedGetter := func(state any) bool {
		if s, ok := state.(*TaskExecutionState); ok {
			return s.ValidationPassed
		}
		return false
	}

	return reactiveEngine.NewWorkflow(TaskExecutionLoopWorkflowID).
		WithDescription("Developer → Structural Validation → Reviewer pipeline for task execution").
		WithStateBucket(stateBucket).
		WithStateFactory(func() any { return &TaskExecutionState{} }).
		WithMaxIterations(maxIterations).
		WithTimeout(30 * time.Minute).

		// Rule 1: accept-trigger — populate state from the JetStream trigger message.
		AddRule(reactiveEngine.NewRule("accept-trigger").
			OnJetStreamSubject("WORKFLOW", "workflow.trigger.task-execution-loop", func() any { return &workflow.TriggerPayload{} }).
			WithStateLookup(stateBucket, func(msg any) string {
				trigger, ok := msg.(*workflow.TriggerPayload)
				if !ok {
					return ""
				}
				taskID := extractTaskIDFromTrigger(trigger)
				return "task-execution." + trigger.Slug + "." + taskID
			}).
			When("always", reactiveEngine.Always()).
			Mutate(taskExecAcceptTrigger).
			MustBuild()).

		// dispatch-develop — fire-and-forget dispatch to developer agent.
		AddRule(reactiveEngine.NewRule("dispatch-develop").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is developing", reactiveEngine.PhaseIs(phases.TaskExecDeveloping)).
			PublishWithMutation("agent.task.development", taskExecBuildDeveloperPayload, setPhase(phases.TaskExecDevelopingDispatched)).
			MustBuild()).

		// develop-completed — react to developer setting "developed" phase.
		AddRule(reactiveEngine.NewRule("develop-completed").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is developed", reactiveEngine.PhaseIs(phases.TaskExecDeveloped)).
			Mutate(setPhase(phases.TaskExecValidating)).
			MustBuild()).

		// dispatch-validate — fire-and-forget dispatch to structural validator.
		AddRule(reactiveEngine.NewRule("dispatch-validate").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is validating", reactiveEngine.PhaseIs(phases.TaskExecValidating)).
			PublishWithMutation("workflow.async.structural-validator", taskExecBuildValidationPayload, setPhase(phases.TaskExecValidatingDispatched)).
			MustBuild()).

		// validate-completed — react to validator setting "validated" phase.
		AddRule(reactiveEngine.NewRule("validate-completed").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is validated", reactiveEngine.PhaseIs(phases.TaskExecValidated)).
			Mutate(setPhase(phases.TaskExecValidationChecked)).
			MustBuild()).

		// validation-passed — emit event and move to reviewing.
		AddRule(reactiveEngine.NewRule("validation-passed").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is validation_checked", reactiveEngine.PhaseIs(phases.TaskExecValidationChecked)).
			When("validation passed", stateFieldEquals(validationPassedGetter, true)).
			PublishWithMutation("workflow.events.task.validation_passed", taskExecBuildValidationPassedEvent, taskExecMutateToReviewing).
			MustBuild()).

		// validation-failed-retry — retry developer with validation feedback.
		AddRule(reactiveEngine.NewRule("validation-failed-retry").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is validation_checked", reactiveEngine.PhaseIs(phases.TaskExecValidationChecked)).
			When("validation failed", stateFieldEquals(validationPassedGetter, false)).
			When("under retry limit", reactiveEngine.ConditionHelpers.IterationLessThan(maxIterations)).
			Mutate(taskExecMutateValidationFailedRetry).
			MustBuild()).

		// validation-failed-escalate — too many validation failures.
		// The not-completed condition prevents infinite re-firing.
		AddRule(reactiveEngine.NewRule("validation-failed-escalate").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is validation_checked", reactiveEngine.PhaseIs(phases.TaskExecValidationChecked)).
			When("validation failed", stateFieldEquals(validationPassedGetter, false)).
			When("at retry limit", reactiveEngine.Not(reactiveEngine.ConditionHelpers.IterationLessThan(maxIterations))).
			When("not completed", notCompleted()).
			PublishWithMutation("user.signal.escalate", taskExecBuildValidationEscalateEvent, taskExecMutateEscalation).
			MustBuild()).

		// dispatch-review — fire-and-forget dispatch to code reviewer.
		AddRule(reactiveEngine.NewRule("dispatch-review").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is reviewing", reactiveEngine.PhaseIs(phases.TaskExecReviewing)).
			PublishWithMutation("agent.task.review", taskExecBuildReviewPayload, setPhase(phases.TaskExecReviewingDispatched)).
			MustBuild()).

		// review-completed — react to reviewer setting "reviewed" phase.
		AddRule(reactiveEngine.NewRule("review-completed").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is reviewed", reactiveEngine.PhaseIs(phases.TaskExecReviewed)).
			Mutate(setPhase(phases.TaskExecEvaluated)).
			MustBuild()).

		// handle-approved — complete the workflow on approval.
		// The not-completed condition prevents infinite re-firing.
		AddRule(reactiveEngine.NewRule("handle-approved").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is evaluated", reactiveEngine.PhaseIs(phases.TaskExecEvaluated)).
			When("verdict is approved", stateFieldEquals(verdictGetter, "approved")).
			When("not completed", notCompleted()).
			CompleteWithEvent("workflow.task.complete", taskExecBuildCompleteEvent).
			MustBuild()).

		// handle-fixable-retry — retry developer with reviewer feedback.
		AddRule(reactiveEngine.NewRule("handle-fixable-retry").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is evaluated", reactiveEngine.PhaseIs(phases.TaskExecEvaluated)).
			When("verdict is not approved", stateFieldNotEquals(verdictGetter, "approved")).
			When("rejection is fixable", stateFieldEquals(rejectionGetter, "fixable")).
			When("under retry limit", reactiveEngine.ConditionHelpers.IterationLessThan(maxIterations)).
			PublishWithMutation("workflow.events.task.rejection_categorized", taskExecBuildRejectionCategorizedEvent, taskExecMutateFixableRetry).
			MustBuild()).

		// handle-max-retries — fixable rejection exhausted retry budget.
		// The not-completed condition prevents infinite re-firing.
		AddRule(reactiveEngine.NewRule("handle-max-retries").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is evaluated", reactiveEngine.PhaseIs(phases.TaskExecEvaluated)).
			When("verdict is not approved", stateFieldNotEquals(verdictGetter, "approved")).
			When("rejection is fixable", stateFieldEquals(rejectionGetter, "fixable")).
			When("at retry limit", reactiveEngine.Not(reactiveEngine.ConditionHelpers.IterationLessThan(maxIterations))).
			When("not completed", notCompleted()).
			PublishWithMutation("user.signal.escalate", taskExecBuildMaxRetriesEscalateEvent, taskExecMutateEscalation).
			MustBuild()).

		// handle-misscoped — misscoped or architectural rejection → plan refinement.
		// The not-completed condition prevents infinite re-firing.
		AddRule(reactiveEngine.NewRule("handle-misscoped").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is evaluated", reactiveEngine.PhaseIs(phases.TaskExecEvaluated)).
			When("verdict is not approved", stateFieldNotEquals(verdictGetter, "approved")).
			When("rejection is misscoped or architectural", reactiveEngine.Or(
				stateFieldEquals(rejectionGetter, "misscoped"),
				stateFieldEquals(rejectionGetter, "architectural"),
			)).
			When("not completed", notCompleted()).
			CompleteWithEvent("workflow.trigger.plan-refinement", taskExecBuildPlanRefinementTrigger).
			MustBuild()).

		// handle-too-big — too_big rejection → task decomposition.
		// The not-completed condition prevents infinite re-firing.
		AddRule(reactiveEngine.NewRule("handle-too-big").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is evaluated", reactiveEngine.PhaseIs(phases.TaskExecEvaluated)).
			When("verdict is not approved", stateFieldNotEquals(verdictGetter, "approved")).
			When("rejection is too_big", stateFieldEquals(rejectionGetter, "too_big")).
			When("not completed", notCompleted()).
			CompleteWithEvent("workflow.trigger.task-decomposition", taskExecBuildTaskDecompositionTrigger).
			MustBuild()).

		// handle-unknown-rejection — unrecognised rejection type → escalate.
		// The not-completed condition prevents infinite re-firing.
		AddRule(reactiveEngine.NewRule("handle-unknown-rejection").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is evaluated", reactiveEngine.PhaseIs(phases.TaskExecEvaluated)).
			When("verdict is not approved", stateFieldNotEquals(verdictGetter, "approved")).
			When("rejection is unknown type", reactiveEngine.Not(reactiveEngine.Or(
				stateFieldEquals(rejectionGetter, "fixable"),
				stateFieldEquals(rejectionGetter, "misscoped"),
				stateFieldEquals(rejectionGetter, "architectural"),
				stateFieldEquals(rejectionGetter, "too_big"),
			))).
			When("not completed", notCompleted()).
			PublishWithMutation("user.signal.escalate", taskExecBuildUnknownRejectionEscalateEvent, taskExecMutateEscalation).
			MustBuild()).

		// handle-error — any failure phase → emit error signal.
		// The not-completed condition prevents infinite re-firing.
		AddRule(reactiveEngine.NewRule("handle-error").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is error", reactiveEngine.ConditionHelpers.PhaseIn(
				phases.TaskExecDeveloperFailed,
				phases.TaskExecReviewerFailed,
				phases.TaskExecValidationError,
			)).
			When("not completed", notCompleted()).
			PublishWithMutation("user.signal.error", taskExecBuildErrorEvent, taskExecMutateError).
			MustBuild()).
		MustBuild()
}

// ---------------------------------------------------------------------------
// Helper: extract task_id from trigger Data blob
// ---------------------------------------------------------------------------

// extractTaskIDFromTrigger extracts the task_id from the trigger's Data blob.
// The task-dispatcher encodes task_id in the Data JSON since TriggerPayload
// does not have a dedicated TaskID field.
func extractTaskIDFromTrigger(trigger *workflow.TriggerPayload) string {
	if len(trigger.Data) == 0 {
		return ""
	}
	var data struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(trigger.Data, &data); err != nil {
		return ""
	}
	return data.TaskID
}

// ---------------------------------------------------------------------------
// Mutators
// ---------------------------------------------------------------------------

// taskExecAcceptTrigger populates TaskExecutionState from the incoming
// TriggerPayload and transitions to the "developing" phase.
var taskExecAcceptTrigger reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*TaskExecutionState)
	if !ok {
		return fmt.Errorf("accept-trigger: expected *TaskExecutionState, got %T", ctx.State)
	}

	trigger, ok := ctx.Message.(*workflow.TriggerPayload)
	if !ok {
		return fmt.Errorf("accept-trigger: expected *workflow.TriggerPayload, got %T", ctx.Message)
	}

	// Extract task_id from the Data blob.
	taskID := extractTaskIDFromTrigger(trigger)
	if taskID == "" {
		return fmt.Errorf("accept-trigger: task_id missing from trigger data")
	}

	// Extract additional task-execution-specific fields from Data.
	var taskData struct {
		Model            string `json:"model"`
		ContextRequestID string `json:"context_request_id"`
	}
	if len(trigger.Data) > 0 {
		_ = json.Unmarshal(trigger.Data, &taskData)
	}

	// Populate state from trigger fields.
	state.Slug = trigger.Slug
	state.TaskID = taskID
	state.Prompt = trigger.Prompt
	state.Model = taskData.Model
	state.ContextRequestID = taskData.ContextRequestID

	// Initialise execution metadata on first trigger only.
	if state.ID == "" {
		state.ID = "task-execution." + trigger.Slug + "." + taskID
		state.WorkflowID = TaskExecutionLoopWorkflowID
		state.Status = reactiveEngine.StatusRunning
		now := time.Now()
		state.CreatedAt = now
		state.UpdatedAt = now
	}

	state.Phase = phases.TaskExecDeveloping
	return nil
}

// Note: In the Participant pattern, components update state directly via StateManager.
// The old callback mutators (taskExecHandleDeveloperResult, taskExecHandleValidationResult,
// taskExecHandleReviewResult) are no longer needed - components set their completion phases
// directly, and the workflow reacts to those phase changes.

// taskExecMutateToReviewing transitions from validation_checked to reviewing.
var taskExecMutateToReviewing reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*TaskExecutionState)
	if !ok {
		return fmt.Errorf("to-reviewing mutator: expected *TaskExecutionState, got %T", ctx.State)
	}
	state.Phase = phases.TaskExecReviewing
	return nil
}

// taskExecMutateValidationFailedRetry increments the iteration and returns
// to the developing phase with "validation" as the revision source.
var taskExecMutateValidationFailedRetry reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*TaskExecutionState)
	if !ok {
		return fmt.Errorf("validation-failed-retry mutator: expected *TaskExecutionState, got %T", ctx.State)
	}
	reactiveEngine.IncrementIteration(state)
	state.RevisionSource = "validation"
	state.Phase = phases.TaskExecDeveloping
	// Clear stale validation results to prevent confusion in state snapshots.
	state.ValidationPassed = false
	state.ChecksRun = 0
	state.CheckResults = nil
	return nil
}

// taskExecMutateFixableRetry increments the iteration and returns to the
// developing phase with "review" as the revision source.
var taskExecMutateFixableRetry reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*TaskExecutionState)
	if !ok {
		return fmt.Errorf("fixable-retry mutator: expected *TaskExecutionState, got %T", ctx.State)
	}
	reactiveEngine.IncrementIteration(state)
	state.RevisionSource = "review"
	state.Verdict = ""
	state.RejectionType = ""
	state.Patterns = nil
	// Note: Feedback intentionally preserved for developer revision prompt.
	state.Phase = phases.TaskExecDeveloping
	return nil
}

// taskExecMutateEscalation marks the execution as escalated.
var taskExecMutateEscalation reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*TaskExecutionState)
	if !ok {
		return fmt.Errorf("escalation mutator: expected *TaskExecutionState, got %T", ctx.State)
	}
	reactiveEngine.EscalateExecution(state, "task execution exhausted retry budget")
	return nil
}

// taskExecMutateError marks the execution as failed.
var taskExecMutateError reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*TaskExecutionState)
	if !ok {
		return fmt.Errorf("error mutator: expected *TaskExecutionState, got %T", ctx.State)
	}
	errMsg := state.Error
	if errMsg == "" {
		errMsg = "task execution step failed in phase: " + state.Phase
	}
	reactiveEngine.FailExecution(state, errMsg)
	return nil
}

// ---------------------------------------------------------------------------
// Payload builders
// ---------------------------------------------------------------------------

// taskExecBuildDeveloperPayload constructs a DeveloperRequest from state.
// On revision passes, the prompt is augmented with feedback from the
// revision source (validation checks or reviewer findings), along with
// the original task prompt and the previous developer output for context.
func taskExecBuildDeveloperPayload(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*TaskExecutionState)
	if !ok {
		return nil, fmt.Errorf("developer payload: expected *TaskExecutionState, got %T", ctx.State)
	}

	req := &DeveloperRequest{
		ExecutionID:      state.ID, // Required for Participant pattern state updates
		Slug:             state.Slug,
		DeveloperTaskID:  state.TaskID,
		Model:            state.Model,
		ContextRequestID: state.ContextRequestID,
	}

	// On revision passes, inject feedback so the developer can address issues.
	// CRITICAL: Include the original task prompt and previous response so the
	// LLM has full context of what was requested and what it produced.
	if state.Iteration > 0 {
		req.Revision = true
		var sb strings.Builder

		// Always start with the original task for context
		sb.WriteString("# Original Task\n\n")
		sb.WriteString(state.Prompt)
		sb.WriteString("\n\n")

		// Include what the developer produced previously
		sb.WriteString("# Your Previous Response\n\n")
		if len(state.DeveloperOutput) > 0 {
			// DeveloperOutput is stored as JSON-encoded string, unmarshal it
			var prevOutput string
			if err := json.Unmarshal(state.DeveloperOutput, &prevOutput); err == nil {
				sb.WriteString(prevOutput)
			} else {
				// Fallback: use raw JSON if unmarshal fails
				sb.Write(state.DeveloperOutput)
			}
		} else {
			sb.WriteString("(previous response not available)")
		}
		sb.WriteString("\n\n")

		// Include files that were modified
		if len(state.FilesModified) > 0 {
			sb.WriteString("# Files Modified\n\n")
			for _, f := range state.FilesModified {
				sb.WriteString("- ")
				sb.WriteString(f)
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}

		switch state.RevisionSource {
		case "validation":
			// Structural validation failed: include check results as context.
			sb.WriteString("# Revision Required: Structural Validation Failed\n\n")
			sb.WriteString("Your previous implementation failed structural validation checks.\n\n")
			sb.WriteString("## Validation Check Results\n\n")
			if len(state.CheckResults) > 0 {
				sb.Write(state.CheckResults)
			} else {
				sb.WriteString("(no detailed check results available)")
			}
			sb.WriteString("\n\n## Instructions\n\n")
			sb.WriteString("Review the validation errors above and fix the issues in your implementation. ")
			sb.WriteString("Make sure your code passes all validation checks before resubmitting.")
		case "review":
			// Reviewer issued a fixable rejection: include reviewer feedback.
			sb.WriteString("# Revision Required: Code Review Rejection\n\n")
			sb.WriteString("Your previous implementation was rejected by the code reviewer.\n\n")
			sb.WriteString("## Reviewer Feedback\n\n")
			sb.WriteString(state.Feedback)
			sb.WriteString("\n\n## Instructions\n\n")
			sb.WriteString("Address ALL issues raised by the reviewer and resubmit your implementation.")
		default:
			sb.WriteString("# Revision Required\n\n")
			sb.WriteString("Please revise your previous implementation.")
		}

		req.Feedback = sb.String()
		req.Prompt = req.Feedback
	} else {
		req.Prompt = state.Prompt
	}

	return req, nil
}

// taskExecBuildValidationPayload constructs a ValidationRequest from state.
func taskExecBuildValidationPayload(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*TaskExecutionState)
	if !ok {
		return nil, fmt.Errorf("validation payload: expected *TaskExecutionState, got %T", ctx.State)
	}

	return &ValidationRequest{
		ExecutionID:   state.ID, // Required for Participant pattern state updates
		Slug:          state.Slug,
		FilesModified: state.FilesModified,
	}, nil
}

// taskExecBuildReviewPayload constructs a TaskCodeReviewRequest from state.
func taskExecBuildReviewPayload(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*TaskExecutionState)
	if !ok {
		return nil, fmt.Errorf("review payload: expected *TaskExecutionState, got %T", ctx.State)
	}

	return &TaskCodeReviewRequest{
		ExecutionID:   state.ID, // Required for Participant pattern state updates
		Slug:          state.Slug,
		DeveloperTask: state.TaskID,
		Output:        state.DeveloperOutput,
	}, nil
}

// taskExecBuildValidationPassedEvent constructs a TaskValidationPassedPayload.
func taskExecBuildValidationPassedEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*TaskExecutionState)
	if !ok {
		return nil, fmt.Errorf("validation-passed event: expected *TaskExecutionState, got %T", ctx.State)
	}

	return &TaskValidationPassedPayload{
		StructuralValidationPassedEvent: workflow.StructuralValidationPassedEvent{
			TaskID:    state.TaskID,
			ChecksRun: state.ChecksRun,
		},
	}, nil
}

// taskExecBuildRejectionCategorizedEvent constructs a TaskRejectionCategorizedPayload.
func taskExecBuildRejectionCategorizedEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*TaskExecutionState)
	if !ok {
		return nil, fmt.Errorf("rejection-categorized event: expected *TaskExecutionState, got %T", ctx.State)
	}

	return &TaskRejectionCategorizedPayload{
		RejectionCategorizedEvent: workflow.RejectionCategorizedEvent{
			Type: state.RejectionType,
		},
	}, nil
}

// taskExecBuildCompleteEvent constructs a TaskCompletePayload.
func taskExecBuildCompleteEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*TaskExecutionState)
	if !ok {
		return nil, fmt.Errorf("complete event: expected *TaskExecutionState, got %T", ctx.State)
	}

	return &TaskCompletePayload{
		TaskExecutionCompleteEvent: workflow.TaskExecutionCompleteEvent{
			TaskID:     state.TaskID,
			Iterations: state.Iteration,
		},
	}, nil
}

// taskExecBuildValidationEscalateEvent constructs a TaskExecEscalatePayload for
// validation-failure escalation.
func taskExecBuildValidationEscalateEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*TaskExecutionState)
	if !ok {
		return nil, fmt.Errorf("validation escalate event: expected *TaskExecutionState, got %T", ctx.State)
	}

	reason := fmt.Sprintf("task %q failed structural validation after %d iteration(s)", state.TaskID, state.Iteration+1)
	return &TaskExecEscalatePayload{
		EscalationEvent: workflow.EscalationEvent{
			Slug:      state.Slug,
			TaskID:    state.TaskID,
			Reason:    reason,
			Iteration: state.Iteration,
		},
	}, nil
}

// taskExecBuildMaxRetriesEscalateEvent constructs a TaskExecEscalatePayload for
// reviewer max-retries escalation.
func taskExecBuildMaxRetriesEscalateEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*TaskExecutionState)
	if !ok {
		return nil, fmt.Errorf("max-retries escalate event: expected *TaskExecutionState, got %T", ctx.State)
	}

	reason := fmt.Sprintf("task %q exceeded max reviewer retries (%d)", state.TaskID, state.Iteration)
	return &TaskExecEscalatePayload{
		EscalationEvent: workflow.EscalationEvent{
			Slug:         state.Slug,
			TaskID:       state.TaskID,
			Reason:       reason,
			LastVerdict:  state.Verdict,
			LastFeedback: state.Feedback,
			Iteration:    state.Iteration,
		},
	}, nil
}

// taskExecBuildUnknownRejectionEscalateEvent constructs a TaskExecEscalatePayload
// for an unrecognised rejection type.
func taskExecBuildUnknownRejectionEscalateEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*TaskExecutionState)
	if !ok {
		return nil, fmt.Errorf("unknown-rejection escalate event: expected *TaskExecutionState, got %T", ctx.State)
	}

	reason := fmt.Sprintf("task %q rejected with unknown rejection type %q", state.TaskID, state.RejectionType)
	return &TaskExecEscalatePayload{
		EscalationEvent: workflow.EscalationEvent{
			Slug:         state.Slug,
			TaskID:       state.TaskID,
			Reason:       reason,
			LastVerdict:  state.Verdict,
			LastFeedback: state.Feedback,
			Iteration:    state.Iteration,
		},
	}, nil
}

// taskExecBuildPlanRefinementTrigger constructs a PlanRefinementTriggerPayload
// for misscoped or architectural rejections.
func taskExecBuildPlanRefinementTrigger(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*TaskExecutionState)
	if !ok {
		return nil, fmt.Errorf("plan-refinement trigger: expected *TaskExecutionState, got %T", ctx.State)
	}

	return &PlanRefinementTriggerPayload{
		OriginalTaskID: state.TaskID,
		Feedback:       state.Feedback,
		PlanSlug:       state.Slug,
		RejectionType:  state.RejectionType,
	}, nil
}

// taskExecBuildTaskDecompositionTrigger constructs a TaskDecompositionTriggerPayload
// for too_big rejections.
func taskExecBuildTaskDecompositionTrigger(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*TaskExecutionState)
	if !ok {
		return nil, fmt.Errorf("task-decomposition trigger: expected *TaskExecutionState, got %T", ctx.State)
	}

	return &TaskDecompositionTriggerPayload{
		OriginalTaskID: state.TaskID,
		Feedback:       state.Feedback,
		PlanSlug:       state.Slug,
	}, nil
}

// taskExecBuildErrorEvent constructs a TaskExecErrorPayload from state.
func taskExecBuildErrorEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*TaskExecutionState)
	if !ok {
		return nil, fmt.Errorf("error event: expected *TaskExecutionState, got %T", ctx.State)
	}

	errMsg := state.Error
	if errMsg == "" {
		errMsg = "task execution step failed in phase: " + state.Phase
	}

	return &TaskExecErrorPayload{
		UserSignalErrorEvent: workflow.UserSignalErrorEvent{
			Slug:   state.Slug,
			TaskID: state.TaskID,
			Error:  errMsg,
		},
	}, nil
}
