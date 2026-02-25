package reactive

import (
	"encoding/json"

	"github.com/c360studio/semstreams/message"
)

// Result types for reactive workflow callback deserialization.
// These types represent the output from each component, used by the reactive
// engine's MutateState callback to update typed workflow state.
//
// The JSON tags match what each component currently publishes in their
// PublishCallbackSuccess output, ensuring wire compatibility during migration.

// ---------------------------------------------------------------------------
// Planner result
// ---------------------------------------------------------------------------

// PlannerResult is the output from the planner component callback.
// Fields match processor/planner.Result JSON tags.
type PlannerResult struct {
	RequestID     string          `json:"request_id"`
	Slug          string          `json:"slug"`
	Content       json.RawMessage `json:"content"`
	Status        string          `json:"status"`
	LLMRequestIDs []string        `json:"llm_request_ids,omitempty"`
}

// Schema implements message.Payload.
func (r *PlannerResult) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "planner-result", Version: "v1"}
}

// Validate implements message.Payload.
func (r *PlannerResult) Validate() error { return nil }

// ---------------------------------------------------------------------------
// Review result (shared by plan-reviewer for both plan and phase reviews)
// ---------------------------------------------------------------------------

// ReviewResult is the output from the plan-reviewer component callback.
// Used for both plan review and phase review (same reviewer, different inputs).
// Fields match processor/plan-reviewer.PlanReviewResult JSON tags.
type ReviewResult struct {
	RequestID         string          `json:"request_id"`
	Slug              string          `json:"slug"`
	Verdict           string          `json:"verdict"`
	Summary           string          `json:"summary"`
	Findings          json.RawMessage `json:"findings"`
	FormattedFindings string          `json:"formatted_findings"`
	Status            string          `json:"status"`
	LLMRequestIDs     []string        `json:"llm_request_ids,omitempty"`
}

// Schema implements message.Payload.
func (r *ReviewResult) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "review-result", Version: "v1"}
}

// Validate implements message.Payload.
func (r *ReviewResult) Validate() error { return nil }

// IsApproved returns true if the review verdict is "approved".
func (r *ReviewResult) IsApproved() bool {
	return r.Verdict == "approved"
}

// ---------------------------------------------------------------------------
// Phase generator result
// ---------------------------------------------------------------------------

// PhaseGeneratorResult is the output from the phase-generator component callback.
type PhaseGeneratorResult struct {
	RequestID     string          `json:"request_id"`
	Slug          string          `json:"slug"`
	Phases        json.RawMessage `json:"phases"`
	PhaseCount    int             `json:"phase_count"`
	Status        string          `json:"status"`
	LLMRequestIDs []string        `json:"llm_request_ids,omitempty"`
}

// Schema implements message.Payload.
func (r *PhaseGeneratorResult) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "phase-generator-result", Version: "v1"}
}

// Validate implements message.Payload.
func (r *PhaseGeneratorResult) Validate() error { return nil }

// ---------------------------------------------------------------------------
// Task generator result
// ---------------------------------------------------------------------------

// TaskGeneratorResult is the output from the task-generator component callback.
type TaskGeneratorResult struct {
	RequestID     string          `json:"request_id"`
	Slug          string          `json:"slug"`
	Tasks         json.RawMessage `json:"tasks"`
	TaskCount     int             `json:"task_count"`
	Status        string          `json:"status"`
	LLMRequestIDs []string        `json:"llm_request_ids,omitempty"`
}

// Schema implements message.Payload.
func (r *TaskGeneratorResult) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "task-generator-result", Version: "v1"}
}

// Validate implements message.Payload.
func (r *TaskGeneratorResult) Validate() error { return nil }

// ---------------------------------------------------------------------------
// Task review result
// ---------------------------------------------------------------------------

// TaskReviewResult is the output from the task-reviewer component callback.
// Fields match processor/task-reviewer.TaskReviewResult JSON tags.
type TaskReviewResult struct {
	RequestID         string          `json:"request_id"`
	Slug              string          `json:"slug"`
	Verdict           string          `json:"verdict"`
	Summary           string          `json:"summary"`
	Findings          json.RawMessage `json:"findings"`
	FormattedFindings string          `json:"formatted_findings"`
	Status            string          `json:"status"`
	LLMRequestIDs     []string        `json:"llm_request_ids,omitempty"`
}

// Schema implements message.Payload.
func (r *TaskReviewResult) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "task-review-result", Version: "v1"}
}

// Validate implements message.Payload.
func (r *TaskReviewResult) Validate() error { return nil }

// IsApproved returns true if the review verdict is "approved".
func (r *TaskReviewResult) IsApproved() bool {
	return r.Verdict == "approved"
}

// ---------------------------------------------------------------------------
// Validation result
// ---------------------------------------------------------------------------

// ValidationResult is the output from the structural-validator component callback.
// Fields match processor/structural-validator.ValidationResult JSON tags.
type ValidationResult struct {
	Slug         string          `json:"slug"`
	Passed       bool            `json:"passed"`
	ChecksRun    int             `json:"checks_run"`
	CheckResults json.RawMessage `json:"check_results"`
	Warning      string          `json:"warning,omitempty"`
}

// Schema implements message.Payload.
func (r *ValidationResult) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "validation-result", Version: "v1"}
}

// Validate implements message.Payload.
func (r *ValidationResult) Validate() error { return nil }

// ---------------------------------------------------------------------------
// Developer result
// ---------------------------------------------------------------------------

// DeveloperResult is the output from the developer agent callback.
type DeveloperResult struct {
	RequestID     string          `json:"request_id,omitempty"`
	Slug          string          `json:"slug"`
	TaskID        string          `json:"developer_task_id,omitempty"`
	Status        string          `json:"status"`
	FilesModified []string        `json:"files_modified,omitempty"`
	Output        json.RawMessage `json:"output,omitempty"`
	LLMRequestIDs []string        `json:"llm_request_ids,omitempty"`
}

// Schema implements message.Payload.
func (r *DeveloperResult) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "developer-result", Version: "v1"}
}

// Validate implements message.Payload.
func (r *DeveloperResult) Validate() error { return nil }

// ---------------------------------------------------------------------------
// Task code review result
// ---------------------------------------------------------------------------

// TaskCodeReviewResult is the output from the task code reviewer callback.
type TaskCodeReviewResult struct {
	RequestID     string          `json:"request_id,omitempty"`
	Slug          string          `json:"slug"`
	Verdict       string          `json:"verdict"`
	RejectionType string          `json:"rejection_type,omitempty"`
	Feedback      string          `json:"feedback,omitempty"`
	Patterns      json.RawMessage `json:"patterns,omitempty"`
	LLMRequestIDs []string        `json:"llm_request_ids,omitempty"`
}

// Schema implements message.Payload.
func (r *TaskCodeReviewResult) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "task-code-review-result", Version: "v1"}
}

// Validate implements message.Payload.
func (r *TaskCodeReviewResult) Validate() error { return nil }

// IsApproved returns true if the code review verdict is "approved".
func (r *TaskCodeReviewResult) IsApproved() bool {
	return r.Verdict == "approved"
}
