// Package reactive defines typed reactive workflow definitions for semspec.
//
// This package provides Go-native workflow definitions that replace the JSON-based
// workflow configurations in configs/workflows/. Each workflow is built using the
// semstreams reactive engine's fluent builder API (ADR-021), enabling compile-time
// type safety for all data flows.
//
// Key design decisions:
//   - Clean typed payloads per component (no more generic TriggerPayload)
//   - Callback embeddable provides callback support matching workflow.CallbackFields API
//   - ParseReactivePayload[T] replaces the multi-format ParseNATSMessage[T] shim
package reactive

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	reactiveEngine "github.com/c360studio/semstreams/processor/reactive"
)

// ---------------------------------------------------------------------------
// Callback — embeddable callback support for reactive payloads
// ---------------------------------------------------------------------------

// Callback provides callback support for reactive workflow payloads.
// Embed this in any request payload that is dispatched via PublishAsync.
//
// It implements reactiveEngine.CallbackInjectable so the reactive engine's
// dispatcher can inject callback fields before publishing. It also provides
// HasCallback/PublishCallbackSuccess/PublishCallbackFailure convenience methods
// matching the workflow.CallbackFields API for smooth component migration.
//
// Wire format is identical to both reactive.CallbackFields and workflow.CallbackFields:
//
//	{"task_id":"...","callback_subject":"...","execution_id":"..."}
type Callback struct {
	TaskID          string `json:"task_id,omitempty"`
	CallbackSubject string `json:"callback_subject,omitempty"`
	ExecutionID     string `json:"execution_id,omitempty"`
}

// InjectCallback implements reactiveEngine.CallbackInjectable.
// Called by the reactive engine dispatcher before publishing the payload.
func (c *Callback) InjectCallback(fields reactiveEngine.CallbackFields) {
	c.TaskID = fields.TaskID
	c.CallbackSubject = fields.CallbackSubject
	c.ExecutionID = fields.ExecutionID
}

// HasCallback returns true if callback fields were injected by the reactive engine.
func (c *Callback) HasCallback() bool {
	return c.CallbackSubject != "" && c.TaskID != ""
}

// SetCallback implements the CallbackReceiver interface used by ParseNATSMessage.
// This enables backward compatibility during migration — components using
// ParseNATSMessage[T] will still get callback fields injected.
func (c *Callback) SetCallback(taskID, callbackSubject string) {
	c.TaskID = taskID
	c.CallbackSubject = callbackSubject
}

// PublishCallbackSuccess publishes a successful AsyncStepResult to the callback
// subject. The result is wrapped in BaseMessage so the reactive engine's
// callback handler can deserialize it via the global payload registry.
func (c *Callback) PublishCallbackSuccess(ctx context.Context, nc *natsclient.Client, output any) error {
	return c.publishCallback(ctx, nc, "success", output, "")
}

// PublishCallbackFailure publishes a failed AsyncStepResult to the callback subject.
func (c *Callback) PublishCallbackFailure(ctx context.Context, nc *natsclient.Client, errMsg string) error {
	return c.publishCallback(ctx, nc, "failed", nil, errMsg)
}

func (c *Callback) publishCallback(ctx context.Context, nc *natsclient.Client, status string, output any, errMsg string) error {
	if !c.HasCallback() {
		return fmt.Errorf("no callback configured")
	}

	var outputJSON json.RawMessage
	if output != nil {
		data, err := json.Marshal(output)
		if err != nil {
			return fmt.Errorf("marshal callback output: %w", err)
		}
		outputJSON = data
	}

	result := &workflow.AsyncStepResult{
		TaskID:      c.TaskID,
		ExecutionID: c.ExecutionID,
		Status:      status,
		Output:      outputJSON,
		Error:       errMsg,
	}

	// Wrap in BaseMessage — the reactive callback handler expects this envelope.
	baseMsg := message.NewBaseMessage(result.Schema(), result, "semspec")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal callback BaseMessage: %w", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		return fmt.Errorf("get jetstream for callback: %w", err)
	}

	if _, err := js.Publish(ctx, c.CallbackSubject, data); err != nil {
		return fmt.Errorf("publish callback to %s: %w", c.CallbackSubject, err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// ParseReactivePayload — clean parser for reactive engine messages
// ---------------------------------------------------------------------------

// ParseReactivePayload parses a NATS message dispatched by the reactive engine.
// Unlike ParseNATSMessage which handles 4 legacy wire formats, this handles only
// the reactive engine's format: BaseMessage with typed payload.
//
// Components migrating to reactive payloads should use this instead of
// workflow.ParseNATSMessage[T].
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
	Callback

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
	Callback

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
	Callback

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
	Callback

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
	Callback

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
	Callback

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
	Callback

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
	Callback

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
	Callback

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
