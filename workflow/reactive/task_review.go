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
// TaskReviewState
// ---------------------------------------------------------------------------

// TaskReviewState is the typed KV state for the task-review-loop reactive workflow.
// It embeds ExecutionState for base lifecycle fields and adds task-generation-specific data.
type TaskReviewState struct {
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

	// Generator output saved by taskReviewHandleGeneratorResult.
	TasksContent  json.RawMessage `json:"tasks_content,omitempty"`
	TaskCount     int             `json:"task_count,omitempty"`
	LLMRequestIDs []string        `json:"llm_request_ids,omitempty"`

	// Reviewer output saved by taskReviewHandleReviewerResult.
	Verdict               string          `json:"verdict,omitempty"`
	Summary               string          `json:"summary,omitempty"`
	Findings              json.RawMessage `json:"findings,omitempty"`
	FormattedFindings     string          `json:"formatted_findings,omitempty"`
	ReviewerLLMRequestIDs []string        `json:"reviewer_llm_request_ids,omitempty"`
}

// GetExecutionState implements reactiveEngine.StateAccessor.
// This lets the engine read/write the embedded ExecutionState without reflection.
func (s *TaskReviewState) GetExecutionState() *reactiveEngine.ExecutionState {
	return &s.ExecutionState
}

// ---------------------------------------------------------------------------
// Event payload types
// ---------------------------------------------------------------------------

// TasksApprovedPayload wraps workflow.TasksApprovedEvent and satisfies message.Payload.
// The JSON wire format is identical to TasksApprovedEvent so downstream handlers
// reading "workflow.events.tasks.approved" receive the expected field names.
type TasksApprovedPayload struct {
	workflow.TasksApprovedEvent
}

// Schema implements message.Payload.
func (p *TasksApprovedPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "tasks-approved", Version: "v1"}
}

// Validate implements message.Payload.
func (p *TasksApprovedPayload) Validate() error {
	if p.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// MarshalJSON marshals using the embedded event's fields, not the wrapper's.
func (p *TasksApprovedPayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.TasksApprovedEvent)
}

// UnmarshalJSON unmarshals directly into the embedded event.
func (p *TasksApprovedPayload) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &p.TasksApprovedEvent)
}

// TasksRevisionPayload wraps workflow.TasksRevisionNeededEvent and satisfies message.Payload.
type TasksRevisionPayload struct {
	workflow.TasksRevisionNeededEvent
}

// Schema implements message.Payload.
func (p *TasksRevisionPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "tasks-revision-needed", Version: "v1"}
}

// Validate implements message.Payload.
func (p *TasksRevisionPayload) Validate() error {
	if p.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// MarshalJSON marshals using the embedded event's fields.
func (p *TasksRevisionPayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.TasksRevisionNeededEvent)
}

// UnmarshalJSON unmarshals directly into the embedded event.
func (p *TasksRevisionPayload) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &p.TasksRevisionNeededEvent)
}

// TaskEscalatePayload wraps workflow.EscalationEvent and satisfies message.Payload.
type TaskEscalatePayload struct {
	workflow.EscalationEvent
}

// Schema implements message.Payload.
func (p *TaskEscalatePayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "task-escalation", Version: "v1"}
}

// Validate implements message.Payload.
func (p *TaskEscalatePayload) Validate() error {
	if p.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// MarshalJSON marshals using the embedded event's fields.
func (p *TaskEscalatePayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.EscalationEvent)
}

// UnmarshalJSON unmarshals directly into the embedded event.
func (p *TaskEscalatePayload) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &p.EscalationEvent)
}

// TaskErrorPayload wraps workflow.UserSignalErrorEvent and satisfies message.Payload.
type TaskErrorPayload struct {
	workflow.UserSignalErrorEvent
}

// Schema implements message.Payload.
func (p *TaskErrorPayload) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "task-error", Version: "v1"}
}

// Validate implements message.Payload.
func (p *TaskErrorPayload) Validate() error {
	if p.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// MarshalJSON marshals using the embedded event's fields.
func (p *TaskErrorPayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.UserSignalErrorEvent)
}

// UnmarshalJSON unmarshals directly into the embedded event.
func (p *TaskErrorPayload) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &p.UserSignalErrorEvent)
}

// ---------------------------------------------------------------------------
// BuildTaskReviewWorkflow
// ---------------------------------------------------------------------------

// BuildTaskReviewWorkflow constructs the task-review-loop reactive workflow
// using the shared OODA review loop builder.
func BuildTaskReviewWorkflow(stateBucket string) *reactiveEngine.Definition {
	return BuildReviewLoopWorkflow(ReviewLoopConfig{
		WorkflowID:    "task-review-loop",
		Description:   "Generate and iteratively review tasks until approved or max iterations exceeded.",
		StateBucket:   stateBucket,
		MaxIterations: 3,
		Timeout:       30 * time.Minute,
		StateFactory:  func() any { return &TaskReviewState{} },

		TriggerStream:         "WORKFLOW",
		TriggerSubject:        "workflow.trigger.task-review-loop",
		TriggerMessageFactory: func() any { return &workflow.TriggerPayload{} },
		StateLookupKey: func(msg any) string {
			trigger, ok := msg.(*workflow.TriggerPayload)
			if !ok {
				return ""
			}
			return "task-review." + trigger.Slug
		},
		KVKeyPattern: "task-review.*",

		AcceptTrigger: taskReviewAcceptTrigger,
		VerdictAccessor: func(state any) string {
			if s, ok := state.(*TaskReviewState); ok {
				return s.Verdict
			}
			return ""
		},

		GeneratorSubject:        "workflow.async.task-generator",
		GeneratorResultTypeKey:  "workflow.task-generator-result.v1",
		BuildGeneratorPayload:   taskReviewBuildGeneratorPayload,
		MutateOnGeneratorResult: taskReviewHandleGeneratorResult,

		ReviewerSubject:        "workflow.async.task-reviewer",
		ReviewerResultTypeKey:  "workflow.task-review-result.v1",
		BuildReviewerPayload:   taskReviewBuildReviewerPayload,
		MutateOnReviewerResult: taskReviewHandleReviewerResult,

		ApprovedEventSubject: "workflow.events.tasks.approved",
		BuildApprovedEvent:   taskReviewBuildApprovedEvent,

		RevisionEventSubject: "workflow.events.tasks.revision_needed",
		BuildRevisionEvent:   taskReviewBuildRevisionEvent,
		MutateOnRevision:     taskReviewHandleRevision,

		EscalateSubject:    "user.signal.escalate",
		BuildEscalateEvent: taskReviewBuildEscalateEvent,
		MutateOnEscalation: taskReviewHandleEscalation,

		ErrorSubject:    "user.signal.error",
		BuildErrorEvent: taskReviewBuildErrorEvent,
		MutateOnError:   taskReviewHandleError,
	})
}

// ---------------------------------------------------------------------------
// Mutators
// ---------------------------------------------------------------------------

// taskReviewAcceptTrigger populates TaskReviewState from the incoming TriggerPayload
// and transitions to the "generating" phase.
var taskReviewAcceptTrigger reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*TaskReviewState)
	if !ok {
		return fmt.Errorf("accept-trigger: expected *TaskReviewState, got %T", ctx.State)
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
		state.ID = "task-review." + trigger.Slug
		state.WorkflowID = "task-review-loop"
		state.Status = reactiveEngine.StatusRunning
		now := time.Now()
		state.CreatedAt = now
		state.UpdatedAt = now
	}

	state.Phase = ReviewPhaseGenerating
	return nil
}

// taskReviewHandleGeneratorResult is the async callback mutator for the generate rule.
// It saves the task generator output and transitions to the reviewing phase.
var taskReviewHandleGeneratorResult reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, result any) error {
	state, ok := ctx.State.(*TaskReviewState)
	if !ok {
		return fmt.Errorf("generate callback: expected *TaskReviewState, got %T", ctx.State)
	}
	if res, ok := result.(*TaskGeneratorResult); ok {
		state.TasksContent = res.Tasks
		state.TaskCount = res.TaskCount
		state.LLMRequestIDs = res.LLMRequestIDs
		state.Phase = ReviewPhaseReviewing
	} else {
		state.Phase = ReviewPhaseGeneratorFailed
		state.Error = "unexpected task generator result type"
	}
	return nil
}

// taskReviewHandleReviewerResult is the async callback mutator for the review rule.
// It saves the reviewer output and transitions to the evaluated phase.
var taskReviewHandleReviewerResult reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, result any) error {
	state, ok := ctx.State.(*TaskReviewState)
	if !ok {
		return fmt.Errorf("review callback: expected *TaskReviewState, got %T", ctx.State)
	}
	if res, ok := result.(*TaskReviewResult); ok {
		state.Verdict = res.Verdict
		state.Summary = res.Summary
		state.Findings = res.Findings
		state.FormattedFindings = res.FormattedFindings
		state.ReviewerLLMRequestIDs = res.LLMRequestIDs
		state.Phase = ReviewPhaseEvaluated
	} else {
		state.Phase = ReviewPhaseReviewerFailed
		state.Error = "unexpected task reviewer result type"
	}
	return nil
}

// taskReviewHandleRevision increments the iteration counter, clears the previous
// verdict, and transitions back to the generating phase for another attempt.
var taskReviewHandleRevision reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*TaskReviewState)
	if !ok {
		return fmt.Errorf("revision mutator: expected *TaskReviewState, got %T", ctx.State)
	}
	reactiveEngine.IncrementIteration(state)
	// Clear stale generator and reviewer output from the previous iteration.
	state.TasksContent = nil
	state.TaskCount = 0
	state.LLMRequestIDs = nil
	state.Findings = nil
	state.FormattedFindings = ""
	state.ReviewerLLMRequestIDs = nil
	// Note: Verdict already cleared below. Summary preserved for revision prompt.
	state.Verdict = ""
	state.Phase = ReviewPhaseGenerating
	return nil
}

// taskReviewHandleEscalation marks the execution as escalated.
var taskReviewHandleEscalation reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*TaskReviewState)
	if !ok {
		return fmt.Errorf("escalation mutator: expected *TaskReviewState, got %T", ctx.State)
	}
	reactiveEngine.EscalateExecution(state, "max task review iterations exceeded")
	return nil
}

// taskReviewHandleError marks the execution as failed.
var taskReviewHandleError reactiveEngine.StateMutatorFunc = func(ctx *reactiveEngine.RuleContext, _ any) error {
	state, ok := ctx.State.(*TaskReviewState)
	if !ok {
		return fmt.Errorf("error mutator: expected *TaskReviewState, got %T", ctx.State)
	}
	errMsg := state.Error
	if errMsg == "" {
		errMsg = "task review step failed in phase: " + state.Phase
	}
	reactiveEngine.FailExecution(state, errMsg)
	return nil
}

// ---------------------------------------------------------------------------
// Payload builders
// ---------------------------------------------------------------------------

// taskReviewBuildGeneratorPayload constructs a TaskGeneratorRequest from state.
// When state.Iteration > 0, the prompt is augmented with reviewer feedback so
// the generator can address specific findings on the revision pass.
func taskReviewBuildGeneratorPayload(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*TaskReviewState)
	if !ok {
		return nil, fmt.Errorf("generator payload: expected *TaskReviewState, got %T", ctx.State)
	}

	req := &TaskGeneratorRequest{
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
	// task generator can directly address the identified issues.
	if state.Iteration > 0 {
		req.Revision = true
		var sb strings.Builder
		sb.WriteString("REVISION REQUEST: Your previous tasks were rejected by the reviewer.\n\n")
		sb.WriteString("## Review Summary\n")
		sb.WriteString(state.Summary)
		sb.WriteString("\n\n## Specific Findings\n")
		sb.WriteString(state.FormattedFindings)
		sb.WriteString("\n\n## Instructions\n")
		sb.WriteString("Address ALL issues raised by the reviewer and produce improved tasks.")
		req.PreviousFindings = sb.String()
		req.Prompt = req.PreviousFindings
	} else {
		req.Prompt = state.Prompt
	}

	return req, nil
}

// taskReviewBuildReviewerPayload constructs a TaskReviewRequest from state.
func taskReviewBuildReviewerPayload(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*TaskReviewState)
	if !ok {
		return nil, fmt.Errorf("reviewer payload: expected *TaskReviewState, got %T", ctx.State)
	}

	// Unmarshal tasks from raw JSON into typed structs for the component.
	var tasks []workflow.Task
	if len(state.TasksContent) > 0 {
		if err := json.Unmarshal(state.TasksContent, &tasks); err != nil {
			return nil, fmt.Errorf("unmarshal tasks for reviewer: %w", err)
		}
	}

	return &TaskReviewRequest{
		RequestID:     state.RequestID,
		Slug:          state.Slug,
		ProjectID:     state.ProjectID,
		Tasks:         tasks,
		ScopePatterns: state.ScopePatterns,
		TraceID:       state.TraceID,
		LoopID:        state.LoopID,
	}, nil
}

// taskReviewBuildApprovedEvent constructs a TasksApprovedPayload from state.
func taskReviewBuildApprovedEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*TaskReviewState)
	if !ok {
		return nil, fmt.Errorf("approved event: expected *TaskReviewState, got %T", ctx.State)
	}

	return &TasksApprovedPayload{
		TasksApprovedEvent: workflow.TasksApprovedEvent{
			Slug:          state.Slug,
			Verdict:       state.Verdict,
			Summary:       state.Summary,
			TaskCount:     state.TaskCount,
			LLMRequestIDs: state.ReviewerLLMRequestIDs,
		},
	}, nil
}

// taskReviewBuildRevisionEvent constructs a TasksRevisionPayload from state.
func taskReviewBuildRevisionEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*TaskReviewState)
	if !ok {
		return nil, fmt.Errorf("revision event: expected *TaskReviewState, got %T", ctx.State)
	}

	return &TasksRevisionPayload{
		TasksRevisionNeededEvent: workflow.TasksRevisionNeededEvent{
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

// taskReviewBuildEscalateEvent constructs a TaskEscalatePayload from state.
func taskReviewBuildEscalateEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*TaskReviewState)
	if !ok {
		return nil, fmt.Errorf("escalate event: expected *TaskReviewState, got %T", ctx.State)
	}

	return &TaskEscalatePayload{
		EscalationEvent: workflow.EscalationEvent{
			Slug:              state.Slug,
			Reason:            "max task review iterations exceeded",
			LastVerdict:       state.Verdict,
			LastFindings:      state.Findings,
			FormattedFindings: state.FormattedFindings,
			Iteration:         state.Iteration,
		},
	}, nil
}

// taskReviewBuildErrorEvent constructs a TaskErrorPayload from state.
func taskReviewBuildErrorEvent(ctx *reactiveEngine.RuleContext) (message.Payload, error) {
	state, ok := ctx.State.(*TaskReviewState)
	if !ok {
		return nil, fmt.Errorf("error event: expected *TaskReviewState, got %T", ctx.State)
	}

	errMsg := state.Error
	if errMsg == "" {
		errMsg = "task review step failed in phase: " + state.Phase
	}

	return &TaskErrorPayload{
		UserSignalErrorEvent: workflow.UserSignalErrorEvent{
			Slug:  state.Slug,
			Error: errMsg,
		},
	}, nil
}
