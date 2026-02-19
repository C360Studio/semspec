package workflow

import (
	"encoding/json"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
)

// TaskPayload represents a task request for the agentic-loop workflow.
// This is published to agent.task.workflow to trigger document generation.
type TaskPayload struct {
	// TaskID uniquely identifies this workflow task
	TaskID string `json:"task_id"`

	// WorkflowID identifies which workflow definition this task belongs to (for tracing)
	WorkflowID string `json:"workflow_id,omitempty"`

	// Role determines which agent behavior to use (developer, reviewer, planner, task-generator)
	Role string `json:"role"`

	// Model specifies which LLM model to use (optional, uses default if empty)
	Model string `json:"model,omitempty"`

	// FallbackChain contains ordered fallback models if primary fails.
	// Used by agentic-model to retry with alternatives.
	FallbackChain []string `json:"fallback_chain,omitempty"`

	// Capability indicates the semantic capability used for model selection.
	// For logging/tracing purposes. Values: planning, writing, coding, reviewing, fast.
	Capability string `json:"capability,omitempty"`

	// WorkflowSlug is the change slug for this workflow
	WorkflowSlug string `json:"workflow_slug"`

	// WorkflowStep is the current step (propose, design, spec, tasks)
	WorkflowStep string `json:"workflow_step"`

	// Title is the human-readable title for the change
	Title string `json:"title"`

	// Description is the original description provided by the user
	Description string `json:"description"`

	// Prompt is the full system prompt to send to the LLM
	Prompt string `json:"prompt"`

	// AutoContinue indicates if the workflow should automatically continue to next step
	AutoContinue bool `json:"auto_continue"`

	// UserID is the ID of the user who initiated the workflow
	UserID string `json:"user_id"`

	// ChannelType is the channel type for response routing (cli, slack, http)
	ChannelType string `json:"channel_type"`

	// ChannelID is the channel ID for response routing
	ChannelID string `json:"channel_id"`

	// PreviousEntities contains entity IDs from previous workflow steps for context
	PreviousEntities []string `json:"previous_entities,omitempty"`

	// RetryAttempt is the current retry attempt number (0 for first attempt)
	RetryAttempt int `json:"retry_attempt,omitempty"`

	// ValidationFeedback contains feedback from previous validation failure
	ValidationFeedback string `json:"validation_feedback,omitempty"`
}

// Schema returns the message type for this payload.
func (p *TaskPayload) Schema() message.Type {
	return WorkflowTaskType
}

// Validate validates the payload.
func (p *TaskPayload) Validate() error {
	if p.TaskID == "" {
		return &ValidationError{Field: "task_id", Message: "task_id is required"}
	}
	if p.Role == "" {
		return &ValidationError{Field: "role", Message: "role is required"}
	}
	if p.WorkflowSlug == "" {
		return &ValidationError{Field: "workflow_slug", Message: "workflow_slug is required"}
	}
	if p.WorkflowStep == "" {
		return &ValidationError{Field: "workflow_step", Message: "workflow_step is required"}
	}
	if p.Prompt == "" {
		return &ValidationError{Field: "prompt", Message: "prompt is required"}
	}
	return nil
}

// MarshalJSON marshals the payload to JSON.
func (p *TaskPayload) MarshalJSON() ([]byte, error) {
	type Alias TaskPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON unmarshals the payload from JSON.
func (p *TaskPayload) UnmarshalJSON(data []byte) error {
	type Alias TaskPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// WorkflowTaskPayload is an alias for TaskPayload for backward compatibility.
type WorkflowTaskPayload = TaskPayload //revive:disable-line

// WorkflowTaskType is the message type for workflow task payloads.
var WorkflowTaskType = message.Type{
	Domain:   "workflow",
	Category: "task",
	Version:  "v1",
}

// ValidationError represents a validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

func init() {
	// Register the workflow task payload type
	_ = component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "task",
		Version:     "v1",
		Description: "Workflow document generation task payload",
		Factory:     func() any { return &TaskPayload{} },
	})
}
