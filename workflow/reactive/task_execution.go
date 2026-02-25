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
// Phase constants
// ---------------------------------------------------------------------------

// Phase constants for the task-execution-loop workflow.
// Unlike the shared OODA review loop, this workflow has a 3-stage pipeline:
// developer → structural validator → reviewer.
const (
	// TaskExecutionLoopWorkflowID is the unique identifier for the task execution loop.
	TaskExecutionLoopWorkflowID = "task-execution-loop"

	TaskExecPhaseDeveloping        = "developing"
	TaskExecPhaseValidating        = "validating"
	TaskExecPhaseValidationChecked = "validation_checked"
	TaskExecPhaseReviewing         = "reviewing"
	TaskExecPhaseEvaluated         = "evaluated"
	TaskExecPhaseDeveloperFailed   = "developer_failed"
	TaskExecPhaseReviewerFailed    = "reviewer_failed"
	TaskExecPhaseValidationError   = "validation_error"
)

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

		// Rule 2: develop — dispatch to developer agent (async).
		AddRule(reactiveEngine.NewRule("develop").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is developing", reactiveEngine.PhaseIs(TaskExecPhaseDeveloping)).
			When("no pending task", reactiveEngine.NoPendingTask()).
			PublishAsync("agent.task.development", taskExecBuildDeveloperPayload, "workflow.developer-result.v1", taskExecHandleDeveloperResult).
			MustBuild()).

		// Rule 3: validate — dispatch to structural validator (async).
		AddRule(reactiveEngine.NewRule("validate").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is validating", reactiveEngine.PhaseIs(TaskExecPhaseValidating)).
			When("no pending task", reactiveEngine.NoPendingTask()).
			PublishAsync("workflow.async.structural-validator", taskExecBuildValidationPayload, "workflow.validation-result.v1", taskExecHandleValidationResult).
			MustBuild()).

		// Rule 4: validation-passed — emit event and move to reviewing.
		AddRule(reactiveEngine.NewRule("validation-passed").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is validation_checked", reactiveEngine.PhaseIs(TaskExecPhaseValidationChecked)).
			When("validation passed", reactiveEngine.StateFieldEquals(validationPassedGetter, true)).
			PublishWithMutation("workflow.events.task.validation_passed", taskExecBuildValidationPassedEvent, taskExecMutateToReviewing).
			MustBuild()).

		// Rule 5: validation-failed-retry — retry developer with validation feedback.
		AddRule(reactiveEngine.NewRule("validation-failed-retry").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is validation_checked", reactiveEngine.PhaseIs(TaskExecPhaseValidationChecked)).
			When("validation failed", reactiveEngine.StateFieldEquals(validationPassedGetter, false)).
			When("under retry limit", reactiveEngine.IterationLessThan(maxIterations)).
			Mutate(taskExecMutateValidationFailedRetry).
			MustBuild()).

		// Rule 6: validation-failed-escalate — too many validation failures.
		AddRule(reactiveEngine.NewRule("validation-failed-escalate").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is validation_checked", reactiveEngine.PhaseIs(TaskExecPhaseValidationChecked)).
			When("validation failed", reactiveEngine.StateFieldEquals(validationPassedGetter, false)).
			When("at retry limit", reactiveEngine.Not(reactiveEngine.IterationLessThan(maxIterations))).
			PublishWithMutation("user.signal.escalate", taskExecBuildValidationEscalateEvent, taskExecMutateEscalation).
			MustBuild()).

		// Rule 7: review — dispatch to code reviewer (async).
		AddRule(reactiveEngine.NewRule("review").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is reviewing", reactiveEngine.PhaseIs(TaskExecPhaseReviewing)).
			When("no pending task", reactiveEngine.NoPendingTask()).
			PublishAsync("agent.task.review", taskExecBuildReviewPayload, "workflow.task-code-review-result.v1", taskExecHandleReviewResult).
			MustBuild()).

		// Rule 8: handle-approved — complete the workflow on approval.
		AddRule(reactiveEngine.NewRule("handle-approved").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is evaluated", reactiveEngine.PhaseIs(TaskExecPhaseEvaluated)).
			When("verdict is approved", reactiveEngine.StateFieldEquals(verdictGetter, "approved")).
			CompleteWithEvent("workflow.task.complete", taskExecBuildCompleteEvent).
			MustBuild()).

		// Rule 9: handle-fixable-retry — retry developer with reviewer feedback.
		AddRule(reactiveEngine.NewRule("handle-fixable-retry").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is evaluated", reactiveEngine.PhaseIs(TaskExecPhaseEvaluated)).
			When("verdict is not approved", reactiveEngine.StateFieldNotEquals(verdictGetter, "approved")).
			When("rejection is fixable", reactiveEngine.StateFieldEquals(rejectionGetter, "fixable")).
			When("under retry limit", reactiveEngine.IterationLessThan(maxIterations)).
			PublishWithMutation("workflow.events.task.rejection_categorized", taskExecBuildRejectionCategorizedEvent, taskExecMutateFixableRetry).
			MustBuild()).

		// Rule 10: handle-max-retries — fixable rejection exhausted retry budget.
		AddRule(reactiveEngine.NewRule("handle-max-retries").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is evaluated", reactiveEngine.PhaseIs(TaskExecPhaseEvaluated)).
			When("verdict is not approved", reactiveEngine.StateFieldNotEquals(verdictGetter, "approved")).
			When("rejection is fixable", reactiveEngine.StateFieldEquals(rejectionGetter, "fixable")).
			When("at retry limit", reactiveEngine.Not(reactiveEngine.IterationLessThan(maxIterations))).
			PublishWithMutation("user.signal.escalate", taskExecBuildMaxRetriesEscalateEvent, taskExecMutateEscalation).
			MustBuild()).

		// Rule 11: handle-misscoped — misscoped or architectural rejection → plan refinement.
		AddRule(reactiveEngine.NewRule("handle-misscoped").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is evaluated", reactiveEngine.PhaseIs(TaskExecPhaseEvaluated)).
			When("verdict is not approved", reactiveEngine.StateFieldNotEquals(verdictGetter, "approved")).
			When("rejection is misscoped or architectural", reactiveEngine.Or(
				reactiveEngine.StateFieldEquals(rejectionGetter, "misscoped"),
				reactiveEngine.StateFieldEquals(rejectionGetter, "architectural"),
			)).
			CompleteWithEvent("workflow.trigger.plan-refinement", taskExecBuildPlanRefinementTrigger).
			MustBuild()).

		// Rule 12: handle-too-big — too_big rejection → task decomposition.
		AddRule(reactiveEngine.NewRule("handle-too-big").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is evaluated", reactiveEngine.PhaseIs(TaskExecPhaseEvaluated)).
			When("verdict is not approved", reactiveEngine.StateFieldNotEquals(verdictGetter, "approved")).
			When("rejection is too_big", reactiveEngine.StateFieldEquals(rejectionGetter, "too_big")).
			CompleteWithEvent("workflow.trigger.task-decomposition", taskExecBuildTaskDecompositionTrigger).
			MustBuild()).

		// Rule 13: handle-unknown-rejection — unrecognised rejection type → escalate.
		AddRule(reactiveEngine.NewRule("handle-unknown-rejection").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is evaluated", reactiveEngine.PhaseIs(TaskExecPhaseEvaluated)).
			When("verdict is not approved", reactiveEngine.StateFieldNotEquals(verdictGetter, "approved")).
			When("rejection is unknown type", reactiveEngine.Not(reactiveEngine.Or(
				reactiveEngine.StateFieldEquals(rejectionGetter, "fixable"),
				reactiveEngine.StateFieldEquals(rejectionGetter, "misscoped"),
				reactiveEngine.StateFieldEquals(rejectionGetter, "architectural"),
				reactiveEngine.StateFieldEquals(rejectionGetter, "too_big"),
			))).
			PublishWithMutation("user.signal.escalate", taskExecBuildUnknownRejectionEscalateEvent, taskExecMutateEscalation).
			MustBuild()).

		// Rule 14: handle-error — any failure phase → emit error signal.
		AddRule(reactiveEngine.NewRule("handle-error").
			WatchKV(stateBucket, "task-execution.>").
			When("phase is error", reactiveEngine.PhaseIsAny(
				TaskExecPhaseDeveloperFailed,
				TaskExecPhaseReviewerFailed,
				TaskExecPhaseValidationError,
			)).
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

	state.Phase = TaskExecPhaseDeveloping
	return nil
}

// taskExecHandleDeveloperResult is the async callback mutator for the develop rule.
// It saves developer output and transitions to the validating phase.
var taskExecHandleDeveloperResult reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, result any) error {
	state, ok := ctx.State.(*TaskExecutionState)
	if !ok {
		return fmt.Errorf("developer callback: expected *TaskExecutionState, got %T", ctx.State)
	}
	if res, ok := result.(*DeveloperResult); ok {
		state.FilesModified = res.FilesModified
		state.DeveloperOutput = res.Output
		state.LLMRequestIDs = res.LLMRequestIDs
		state.Phase = TaskExecPhaseValidating
	} else {
		state.Phase = TaskExecPhaseDeveloperFailed
		state.Error = "unexpected developer result type"
	}
	return nil
}

// taskExecHandleValidationResult is the async callback mutator for the validate rule.
// It saves validation results and transitions to the validation_checked phase.
var taskExecHandleValidationResult reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, result any) error {
	state, ok := ctx.State.(*TaskExecutionState)
	if !ok {
		return fmt.Errorf("validation callback: expected *TaskExecutionState, got %T", ctx.State)
	}
	if res, ok := result.(*ValidationResult); ok {
		state.ValidationPassed = res.Passed
		state.ChecksRun = res.ChecksRun
		state.CheckResults = res.CheckResults
		state.Phase = TaskExecPhaseValidationChecked
	} else {
		state.Phase = TaskExecPhaseValidationError
		state.Error = "unexpected validation result type"
	}
	return nil
}

// taskExecHandleReviewResult is the async callback mutator for the review rule.
// It saves reviewer output and transitions to the evaluated phase.
var taskExecHandleReviewResult reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, result any) error {
	state, ok := ctx.State.(*TaskExecutionState)
	if !ok {
		return fmt.Errorf("reviewer callback: expected *TaskExecutionState, got %T", ctx.State)
	}
	if res, ok := result.(*TaskCodeReviewResult); ok {
		state.Verdict = res.Verdict
		state.RejectionType = res.RejectionType
		state.Feedback = res.Feedback
		state.Patterns = res.Patterns
		state.ReviewerLLMRequestIDs = res.LLMRequestIDs
		state.Phase = TaskExecPhaseEvaluated
	} else {
		state.Phase = TaskExecPhaseReviewerFailed
		state.Error = "unexpected reviewer result type"
	}
	return nil
}

// taskExecMutateToReviewing transitions from validation_checked to reviewing.
var taskExecMutateToReviewing reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*TaskExecutionState)
	if !ok {
		return fmt.Errorf("to-reviewing mutator: expected *TaskExecutionState, got %T", ctx.State)
	}
	state.Phase = TaskExecPhaseReviewing
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
	state.Phase = TaskExecPhaseDeveloping
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
	state.Phase = TaskExecPhaseDeveloping
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
// revision source (validation checks or reviewer findings).
func taskExecBuildDeveloperPayload(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*TaskExecutionState)
	if !ok {
		return nil, fmt.Errorf("developer payload: expected *TaskExecutionState, got %T", ctx.State)
	}

	req := &DeveloperRequest{
		Slug:             state.Slug,
		DeveloperTaskID:  state.TaskID,
		Model:            state.Model,
		ContextRequestID: state.ContextRequestID,
	}

	// On revision passes, inject feedback so the developer can address issues.
	if state.Iteration > 0 {
		req.Revision = true
		switch state.RevisionSource {
		case "validation":
			// Structural validation failed: include check results as context.
			var sb strings.Builder
			sb.WriteString("REVISION REQUEST: Your previous implementation failed structural validation.\n\n")
			sb.WriteString("## Validation Check Results\n")
			if len(state.CheckResults) > 0 {
				sb.Write(state.CheckResults)
			} else {
				sb.WriteString("(no detailed check results available)")
			}
			sb.WriteString("\n\n## Instructions\n")
			sb.WriteString("Fix the structural issues identified above and resubmit.")
			req.Feedback = sb.String()
			req.Prompt = req.Feedback
		case "review":
			// Reviewer issued a fixable rejection: include reviewer feedback.
			var sb strings.Builder
			sb.WriteString("REVISION REQUEST: Your previous implementation was rejected by the code reviewer.\n\n")
			sb.WriteString("## Reviewer Feedback\n")
			sb.WriteString(state.Feedback)
			sb.WriteString("\n\n## Instructions\n")
			sb.WriteString("Address ALL issues raised by the reviewer and resubmit.")
			req.Feedback = sb.String()
			req.Prompt = req.Feedback
		default:
			req.Prompt = state.Prompt
		}
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
