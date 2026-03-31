package payloads

import (
	"encoding/json"

	"github.com/c360studio/semstreams/component"
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

// MarshalJSON implements json.Marshaler.
func (r *PlannerResult) MarshalJSON() ([]byte, error) {
	type Alias PlannerResult
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *PlannerResult) UnmarshalJSON(data []byte) error {
	type Alias PlannerResult
	return json.Unmarshal(data, (*Alias)(r))
}

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

// MarshalJSON implements json.Marshaler.
func (r *ReviewResult) MarshalJSON() ([]byte, error) {
	type Alias ReviewResult
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *ReviewResult) UnmarshalJSON(data []byte) error {
	type Alias ReviewResult
	return json.Unmarshal(data, (*Alias)(r))
}

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

// MarshalJSON implements json.Marshaler.
func (r *TaskGeneratorResult) MarshalJSON() ([]byte, error) {
	type Alias TaskGeneratorResult
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *TaskGeneratorResult) UnmarshalJSON(data []byte) error {
	type Alias TaskGeneratorResult
	return json.Unmarshal(data, (*Alias)(r))
}

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

// MarshalJSON implements json.Marshaler.
func (r *TaskReviewResult) MarshalJSON() ([]byte, error) {
	type Alias TaskReviewResult
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *TaskReviewResult) UnmarshalJSON(data []byte) error {
	type Alias TaskReviewResult
	return json.Unmarshal(data, (*Alias)(r))
}

// ---------------------------------------------------------------------------
// Validation result
// ---------------------------------------------------------------------------

// CheckResult holds the outcome of a single checklist check execution.
type CheckResult struct {
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	Required bool   `json:"required"`
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	Duration string `json:"duration"`
}

// ValidationResult is the canonical payload for structural validation results.
// Published by the structural-validator component, consumed by the execution-orchestrator.
type ValidationResult struct {
	Slug         string        `json:"slug"`
	Passed       bool          `json:"passed"`
	ChecksRun    int           `json:"checks_run"`
	CheckResults []CheckResult `json:"check_results"`
	Warning      string        `json:"warning,omitempty"`
}

// Schema implements message.Payload.
func (r *ValidationResult) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "structural-validation-result", Version: "v1"}
}

// Validate implements message.Payload.
func (r *ValidationResult) Validate() error { return nil }

// MarshalJSON implements json.Marshaler.
func (r *ValidationResult) MarshalJSON() ([]byte, error) {
	type Alias ValidationResult
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *ValidationResult) UnmarshalJSON(data []byte) error {
	type Alias ValidationResult
	return json.Unmarshal(data, (*Alias)(r))
}

func init() {
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "structural-validation-result",
		Version:     "v1",
		Description: "Structural validation result — checklist execution summary",
		Factory:     func() any { return &ValidationResult{} },
	}); err != nil {
		panic("failed to register ValidationResult: " + err.Error())
	}
}

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

// MarshalJSON implements json.Marshaler.
func (r *DeveloperResult) MarshalJSON() ([]byte, error) {
	type Alias DeveloperResult
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *DeveloperResult) UnmarshalJSON(data []byte) error {
	type Alias DeveloperResult
	return json.Unmarshal(data, (*Alias)(r))
}

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

	// Review quality ratings (1-5 each). Populated by the submit_review tool.
	Q1Correctness  int `json:"q1_correctness,omitempty"`  // Acceptance criteria met?
	Q2Quality      int `json:"q2_quality,omitempty"`      // Patterns and SOPs followed?
	Q3Completeness int `json:"q3_completeness,omitempty"` // Edge cases, tests, docs covered?

	// Red team scoring — populated only when teams are enabled and a red team
	// challenge was included in the review context. Each score is 1-5.
	// Zero values indicate the reviewer did not score the red team.
	RedAccuracy     int    `json:"red_accuracy,omitempty"`     // Were the red team's issues real?
	RedThoroughness int    `json:"red_thoroughness,omitempty"` // Did they find what matters?
	RedFairness     int    `json:"red_fairness,omitempty"`     // Proportionate severity?
	RedFeedback     string `json:"red_feedback,omitempty"`     // Feedback on critique quality
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

// MarshalJSON implements json.Marshaler.
func (r *TaskCodeReviewResult) MarshalJSON() ([]byte, error) {
	type Alias TaskCodeReviewResult
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *TaskCodeReviewResult) UnmarshalJSON(data []byte) error {
	type Alias TaskCodeReviewResult
	return json.Unmarshal(data, (*Alias)(r))
}
