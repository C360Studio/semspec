// Package reactive defines typed reactive workflow definitions for semspec.
//
// This package provides Go-native workflow definitions that replace the JSON-based
// workflow configurations in configs/workflows/. Each workflow is built using the
// semstreams reactive engine's fluent builder API (ADR-021), enabling compile-time
// type safety for all data flows.
//
// Key design decisions:
//   - Clean typed payloads per component (no more generic TriggerPayload)
//   - ExecutionID field on all payloads enables KV state updates
//   - ParseReactivePayload[T] provides clean parsing for reactive engine messages
package reactive

import (
	"encoding/json"
	"fmt"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/message"
)

// ---------------------------------------------------------------------------
// ParseReactivePayload — clean parser for reactive engine messages
// ---------------------------------------------------------------------------

// ParseReactivePayload parses a NATS message dispatched by the reactive engine.
// This handles the reactive engine's format: BaseMessage with typed payload.
func ParseReactivePayload[T any](data []byte) (*T, error) {
	// Extract raw payload from BaseMessage wrapper
	var rawMsg struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &rawMsg); err != nil {
		return nil, fmt.Errorf("unmarshal BaseMessage: %w", err)
	}
	if len(rawMsg.Payload) == 0 {
		return nil, fmt.Errorf("empty payload in BaseMessage")
	}

	var result T
	if err := json.Unmarshal(rawMsg.Payload, &result); err != nil {
		return nil, fmt.Errorf("unmarshal payload into %T: %w", result, err)
	}
	return &result, nil
}

// ---------------------------------------------------------------------------
// Plan review loop payloads
// ---------------------------------------------------------------------------

// PlannerRequest is the typed payload sent to the planner component.
// Replaces the generic workflow.TriggerPayload for planner dispatch.
type PlannerRequest struct {
	ExecutionID string `json:"execution_id,omitempty"`
	RequestID     string   `json:"request_id"`
	Slug          string   `json:"slug"`
	Title         string   `json:"title"`
	Description   string   `json:"description,omitempty"`
	ProjectID     string   `json:"project_id,omitempty"`
	TraceID       string   `json:"trace_id,omitempty"`
	LoopID        string   `json:"loop_id,omitempty"`
	Prompt        string   `json:"prompt,omitempty"`
	Role          string   `json:"role,omitempty"`
	Model         string   `json:"model,omitempty"`
	ScopePatterns []string `json:"scope_patterns,omitempty"`
	Auto          bool     `json:"auto,omitempty"`

	// Revision indicates this is a retry with reviewer feedback.
	Revision         bool   `json:"revision,omitempty"`
	PreviousFindings string `json:"previous_findings,omitempty"`
}

// Schema implements message.Payload.
func (r *PlannerRequest) Schema() message.Type {
	return PlannerRequestType
}

// Validate implements message.Payload.
func (r *PlannerRequest) Validate() error {
	if r.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// PlannerRequestType is the message type for planner requests.
var PlannerRequestType = message.Type{
	Domain:   "workflow",
	Category: "planner-request",
	Version:  "v1",
}

// PlanReviewRequest is the typed payload sent to the plan-reviewer component.
// Replaces the PlanReviewTrigger from the plan-reviewer package.
type PlanReviewRequest struct {
	ExecutionID string `json:"execution_id,omitempty"`
	RequestID     string          `json:"request_id"`
	Slug          string          `json:"slug"`
	ProjectID     string          `json:"project_id,omitempty"`
	PlanContent   json.RawMessage `json:"plan_content"`
	ScopePatterns []string        `json:"scope_patterns,omitempty"`
	SOPContext    string          `json:"sop_context,omitempty"`

	// Trace context
	TraceID string `json:"trace_id,omitempty"`
	LoopID  string `json:"loop_id,omitempty"`
}

// Schema implements message.Payload.
func (r *PlanReviewRequest) Schema() message.Type {
	return PlanReviewRequestType
}

// Validate implements message.Payload.
func (r *PlanReviewRequest) Validate() error {
	if r.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	if r.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}
	return nil
}

// PlanReviewRequestType is the message type for plan review requests.
var PlanReviewRequestType = message.Type{
	Domain:   "workflow",
	Category: "plan-review-request",
	Version:  "v1",
}

// ---------------------------------------------------------------------------
// Phase review loop payloads
// ---------------------------------------------------------------------------

// PhaseGeneratorRequest is the typed payload sent to the phase-generator component.
// Replaces the generic workflow.TriggerPayload for phase generation dispatch.
type PhaseGeneratorRequest struct {
	ExecutionID string `json:"execution_id,omitempty"`
	RequestID     string   `json:"request_id"`
	Slug          string   `json:"slug"`
	Title         string   `json:"title"`
	Description   string   `json:"description,omitempty"`
	ProjectID     string   `json:"project_id,omitempty"`
	TraceID       string   `json:"trace_id,omitempty"`
	LoopID        string   `json:"loop_id,omitempty"`
	Prompt        string   `json:"prompt,omitempty"`
	Role          string   `json:"role,omitempty"`
	Model         string   `json:"model,omitempty"`
	ScopePatterns []string `json:"scope_patterns,omitempty"`

	// Revision indicates this is a retry with reviewer feedback.
	Revision         bool   `json:"revision,omitempty"`
	PreviousFindings string `json:"previous_findings,omitempty"`
}

// Schema implements message.Payload.
func (r *PhaseGeneratorRequest) Schema() message.Type {
	return PhaseGeneratorRequestType
}

// Validate implements message.Payload.
func (r *PhaseGeneratorRequest) Validate() error {
	if r.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// PhaseGeneratorRequestType is the message type for phase generator requests.
var PhaseGeneratorRequestType = message.Type{
	Domain:   "workflow",
	Category: "phase-generator-request",
	Version:  "v1",
}

// PhaseReviewRequest is the typed payload sent to the plan-reviewer component
// for phase review. Uses the same reviewer as plan review but with phase content.
type PhaseReviewRequest struct {
	ExecutionID string `json:"execution_id,omitempty"`
	RequestID     string          `json:"request_id"`
	Slug          string          `json:"slug"`
	ProjectID     string          `json:"project_id,omitempty"`
	PlanContent   json.RawMessage `json:"plan_content"` // Phases content (reuses plan_content field name for reviewer compatibility)
	ScopePatterns []string        `json:"scope_patterns,omitempty"`
	SOPContext    string          `json:"sop_context,omitempty"`

	// Trace context
	TraceID string `json:"trace_id,omitempty"`
	LoopID  string `json:"loop_id,omitempty"`
}

// Schema implements message.Payload.
func (r *PhaseReviewRequest) Schema() message.Type {
	return PhaseReviewRequestType
}

// Validate implements message.Payload.
func (r *PhaseReviewRequest) Validate() error {
	if r.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	if r.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}
	return nil
}

// PhaseReviewRequestType is the message type for phase review requests.
var PhaseReviewRequestType = message.Type{
	Domain:   "workflow",
	Category: "phase-review-request",
	Version:  "v1",
}

// ---------------------------------------------------------------------------
// Task review loop payloads
// ---------------------------------------------------------------------------

// TaskGeneratorRequest is the typed payload sent to the task-generator component.
// Replaces the generic workflow.TriggerPayload for task generation dispatch.
type TaskGeneratorRequest struct {
	ExecutionID string `json:"execution_id,omitempty"`
	RequestID     string   `json:"request_id"`
	Slug          string   `json:"slug"`
	Title         string   `json:"title"`
	Description   string   `json:"description,omitempty"`
	ProjectID     string   `json:"project_id,omitempty"`
	TraceID       string   `json:"trace_id,omitempty"`
	LoopID        string   `json:"loop_id,omitempty"`
	Prompt        string   `json:"prompt,omitempty"`
	Role          string   `json:"role,omitempty"`
	Model         string   `json:"model,omitempty"`
	ScopePatterns []string `json:"scope_patterns,omitempty"`

	// Revision indicates this is a retry with reviewer feedback.
	Revision         bool   `json:"revision,omitempty"`
	PreviousFindings string `json:"previous_findings,omitempty"`
}

// Schema implements message.Payload.
func (r *TaskGeneratorRequest) Schema() message.Type {
	return TaskGeneratorRequestType
}

// Validate implements message.Payload.
func (r *TaskGeneratorRequest) Validate() error {
	if r.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// TaskGeneratorRequestType is the message type for task generator requests.
var TaskGeneratorRequestType = message.Type{
	Domain:   "workflow",
	Category: "task-generator-request",
	Version:  "v1",
}

// TaskReviewRequest is the typed payload sent to the task-reviewer component.
// Purpose-built for the task-reviewer component.
type TaskReviewRequest struct {
	ExecutionID string `json:"execution_id,omitempty"`
	RequestID     string          `json:"request_id"`
	Slug          string          `json:"slug"`
	ProjectID     string          `json:"project_id,omitempty"`
	Tasks         []workflow.Task `json:"tasks"`
	ScopePatterns []string        `json:"scope_patterns,omitempty"`
	SOPContext    string          `json:"sop_context,omitempty"`

	// Trace context
	TraceID string `json:"trace_id,omitempty"`
	LoopID  string `json:"loop_id,omitempty"`
}

// Schema implements message.Payload.
func (r *TaskReviewRequest) Schema() message.Type {
	return TaskReviewRequestType
}

// Validate implements message.Payload.
func (r *TaskReviewRequest) Validate() error {
	if r.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	if r.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}
	return nil
}

// TaskReviewRequestType is the message type for task review requests.
var TaskReviewRequestType = message.Type{
	Domain:   "workflow",
	Category: "task-review-request",
	Version:  "v1",
}

// ---------------------------------------------------------------------------
// Task execution loop payloads
// ---------------------------------------------------------------------------

// DeveloperRequest is the typed payload sent to the developer agent.
// Used in the task-execution-loop workflow.
type DeveloperRequest struct {
	ExecutionID string `json:"execution_id,omitempty"`
	RequestID        string   `json:"request_id,omitempty"`
	Slug             string   `json:"slug"`
	DeveloperTaskID  string   `json:"developer_task_id"` // distinct from CallbackFields.TaskID
	Model            string   `json:"model,omitempty"`
	Prompt           string   `json:"prompt,omitempty"`
	ContextRequestID string   `json:"context_request_id,omitempty"`
	ScopePatterns    []string `json:"scope_patterns,omitempty"`

	// Trace context
	TraceID string `json:"trace_id,omitempty"`
	LoopID  string `json:"loop_id,omitempty"`

	// Revision feedback from reviewer
	Revision bool   `json:"revision,omitempty"`
	Feedback string `json:"feedback,omitempty"`
}

// Schema implements message.Payload.
func (r *DeveloperRequest) Schema() message.Type {
	return DeveloperRequestType
}

// Validate implements message.Payload.
func (r *DeveloperRequest) Validate() error {
	if r.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// DeveloperRequestType is the message type for developer requests.
var DeveloperRequestType = message.Type{
	Domain:   "workflow",
	Category: "developer-request",
	Version:  "v1",
}

// ValidationRequest is the typed payload sent to the structural-validator component.
// Purpose-built for the structural-validator component.
type ValidationRequest struct {
	ExecutionID   string   `json:"execution_id,omitempty"`
	Slug          string   `json:"slug"`
	FilesModified []string `json:"files_modified"`

	// Trace context
	TraceID string `json:"trace_id,omitempty"`
}

// Schema implements message.Payload.
func (r *ValidationRequest) Schema() message.Type {
	return ValidationRequestType
}

// Validate implements message.Payload.
func (r *ValidationRequest) Validate() error {
	if r.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// ValidationRequestType is the message type for validation requests.
var ValidationRequestType = message.Type{
	Domain:   "workflow",
	Category: "validation-request",
	Version:  "v1",
}

// TaskCodeReviewRequest is the typed payload sent to the task code reviewer.
// Used in the task-execution-loop for reviewing developer output.
type TaskCodeReviewRequest struct {
	ExecutionID   string          `json:"execution_id,omitempty"`
	RequestID     string          `json:"request_id,omitempty"`
	Slug          string          `json:"slug"`
	DeveloperTask string          `json:"developer_task_id,omitempty"`
	Output        json.RawMessage `json:"output,omitempty"` // Developer output to review
	ScopePatterns []string        `json:"scope_patterns,omitempty"`
	SOPContext    string          `json:"sop_context,omitempty"`

	// Trace context
	TraceID string `json:"trace_id,omitempty"`
	LoopID  string `json:"loop_id,omitempty"`
}

// Schema implements message.Payload.
func (r *TaskCodeReviewRequest) Schema() message.Type {
	return TaskCodeReviewRequestType
}

// Validate implements message.Payload.
func (r *TaskCodeReviewRequest) Validate() error {
	if r.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// TaskCodeReviewRequestType is the message type for task code review requests.
var TaskCodeReviewRequestType = message.Type{
	Domain:   "workflow",
	Category: "task-code-review-request",
	Version:  "v1",
}

// ---------------------------------------------------------------------------
// Plan coordinator payloads
// ---------------------------------------------------------------------------

// PlanCoordinatorRequest is the typed payload sent to the plan-coordinator component.
type PlanCoordinatorRequest struct {
	ExecutionID string   `json:"execution_id,omitempty"`
	RequestID   string   `json:"request_id"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	FocusAreas  []string `json:"focus_areas,omitempty"`
	MaxPlanners int      `json:"max_planners,omitempty"`
	ProjectID   string   `json:"project_id,omitempty"`
	TraceID     string   `json:"trace_id,omitempty"`
	LoopID      string   `json:"loop_id,omitempty"`
}

// Schema implements message.Payload.
func (r *PlanCoordinatorRequest) Schema() message.Type {
	return PlanCoordinatorRequestType
}

// Validate implements message.Payload.
func (r *PlanCoordinatorRequest) Validate() error {
	if r.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	if r.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}
	if r.MaxPlanners < 0 || r.MaxPlanners > 3 {
		return fmt.Errorf("max_planners must be 0-3")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *PlanCoordinatorRequest) MarshalJSON() ([]byte, error) {
	type Alias PlanCoordinatorRequest
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *PlanCoordinatorRequest) UnmarshalJSON(data []byte) error {
	type Alias PlanCoordinatorRequest
	return json.Unmarshal(data, (*Alias)(r))
}

// PlanCoordinatorRequestType is the message type for plan coordinator requests.
var PlanCoordinatorRequestType = message.Type{
	Domain:   "workflow",
	Category: "plan-coordinator-request",
	Version:  "v1",
}

// ---------------------------------------------------------------------------
// Task dispatcher payloads
// ---------------------------------------------------------------------------

// TaskDispatchRequest is the typed payload sent to the task-dispatcher component.
type TaskDispatchRequest struct {
	ExecutionID string `json:"execution_id,omitempty"`
	RequestID   string `json:"request_id"`
	Slug        string `json:"slug"`
	BatchID     string `json:"batch_id"`
	TraceID     string `json:"trace_id,omitempty"`
	LoopID      string `json:"loop_id,omitempty"`
}

// Schema implements message.Payload.
func (r *TaskDispatchRequest) Schema() message.Type {
	return TaskDispatchRequestType
}

// Validate implements message.Payload.
func (r *TaskDispatchRequest) Validate() error {
	if r.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	if r.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}
	if r.BatchID == "" {
		return fmt.Errorf("batch_id is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *TaskDispatchRequest) MarshalJSON() ([]byte, error) {
	type Alias TaskDispatchRequest
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *TaskDispatchRequest) UnmarshalJSON(data []byte) error {
	type Alias TaskDispatchRequest
	return json.Unmarshal(data, (*Alias)(r))
}

// TaskDispatchRequestType is the message type for task dispatch requests.
var TaskDispatchRequestType = message.Type{
	Domain:   "workflow",
	Category: "task-dispatch-request",
	Version:  "v1",
}

// ---------------------------------------------------------------------------
// Question answerer payloads (moved from answerer package for proper typing)
// ---------------------------------------------------------------------------

// QuestionAnswerRequest is the typed payload sent to the question-answerer component.
// This enables clean reactive workflow dispatch for question answering.
type QuestionAnswerRequest struct {
	ExecutionID  string `json:"execution_id,omitempty"`
	TaskID       string `json:"task_id"`
	QuestionID   string `json:"question_id"`
	Topic        string `json:"topic"`
	Question     string `json:"question"`
	Capability   string `json:"capability,omitempty"`
	ReplySubject string `json:"reply_subject,omitempty"`
	TraceID      string `json:"trace_id,omitempty"`
	LoopID       string `json:"loop_id,omitempty"`
}

// Schema implements message.Payload.
func (r *QuestionAnswerRequest) Schema() message.Type {
	return QuestionAnswerRequestType
}

// Validate implements message.Payload.
func (r *QuestionAnswerRequest) Validate() error {
	if r.QuestionID == "" {
		return fmt.Errorf("question_id is required")
	}
	if r.Question == "" {
		return fmt.Errorf("question is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *QuestionAnswerRequest) MarshalJSON() ([]byte, error) {
	type Alias QuestionAnswerRequest
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *QuestionAnswerRequest) UnmarshalJSON(data []byte) error {
	type Alias QuestionAnswerRequest
	return json.Unmarshal(data, (*Alias)(r))
}

// QuestionAnswerRequestType is the message type for question answer requests.
var QuestionAnswerRequestType = message.Type{
	Domain:   "workflow",
	Category: "question-answer-request",
	Version:  "v1",
}

// ---------------------------------------------------------------------------
// Context builder payloads (for proper typing of context build requests)
// ---------------------------------------------------------------------------

// ContextBuildRequest is the typed payload sent to the context-builder component.
// This replaces direct use of contextbuilder.ContextBuildRequest for reactive dispatch.
type ContextBuildRequest struct {
	ExecutionID     string   `json:"execution_id,omitempty"`
	RequestID       string   `json:"request_id"`
	TaskType        string   `json:"task_type"`
	Topic           string   `json:"topic,omitempty"`
	Keywords        []string `json:"keywords,omitempty"`
	ScopePatterns   []string `json:"scope_patterns,omitempty"`
	MaxTokens       int      `json:"max_tokens,omitempty"`
	IncludeSOPs     bool     `json:"include_sops,omitempty"`
	IncludeEntities bool     `json:"include_entities,omitempty"`
	TraceID         string   `json:"trace_id,omitempty"`
}

// Schema implements message.Payload.
func (r *ContextBuildRequest) Schema() message.Type {
	return ContextBuildRequestType
}

// Validate implements message.Payload.
func (r *ContextBuildRequest) Validate() error {
	if r.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}
	if r.TaskType == "" {
		return fmt.Errorf("task_type is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *ContextBuildRequest) MarshalJSON() ([]byte, error) {
	type Alias ContextBuildRequest
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *ContextBuildRequest) UnmarshalJSON(data []byte) error {
	type Alias ContextBuildRequest
	return json.Unmarshal(data, (*Alias)(r))
}

// ContextBuildRequestType is the message type for context build requests.
var ContextBuildRequestType = message.Type{
	Domain:   "context",
	Category: "build-request",
	Version:  "v1",
}
