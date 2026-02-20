package workflow

import (
	"encoding/json"

	"github.com/c360studio/semstreams/message"
)

// TriggerPayload represents a trigger for the workflow processor.
// This matches the semstreams TriggerPayload format with well-known fields
// at the top level and semspec-specific fields in the Data blob.
type TriggerPayload struct {
	// CallbackFields supports workflow-processor async dispatch.
	// When present, the component publishes AsyncStepResult to CallbackSubject.
	CallbackFields

	// WorkflowID identifies which workflow definition to execute
	WorkflowID string `json:"workflow_id"`

	// Well-known agent fields (accessible via ${trigger.payload.*})
	Role   string `json:"role,omitempty"`
	Model  string `json:"model,omitempty"`
	Prompt string `json:"prompt,omitempty"`

	// Well-known routing fields
	UserID      string `json:"user_id,omitempty"`
	ChannelType string `json:"channel_type,omitempty"`
	ChannelID   string `json:"channel_id,omitempty"`

	// Request tracking
	RequestID string `json:"request_id,omitempty"`

	// Trace context for trajectory tracking
	TraceID string `json:"trace_id,omitempty"`
	LoopID  string `json:"loop_id,omitempty"`

	// Semspec-specific fields in data blob
	Data *TriggerData `json:"data,omitempty"`
}

// TriggerData contains semspec-specific workflow fields.
// These are accessible via ${trigger.payload.slug}, ${trigger.payload.title}, etc.
type TriggerData struct {
	// Slug is the workflow change slug (e.g., "add-user-authentication")
	Slug string `json:"slug,omitempty"`

	// Title is the human-readable title for the change
	Title string `json:"title,omitempty"`

	// Description is the original description provided by the user
	Description string `json:"description,omitempty"`

	// Auto indicates if the workflow should auto-continue through all steps
	Auto bool `json:"auto,omitempty"`
}

// Schema returns the message type for TriggerPayload.
func (p *TriggerPayload) Schema() message.Type {
	return WorkflowTriggerType
}

// Validate validates the TriggerPayload.
func (p *TriggerPayload) Validate() error {
	if p.WorkflowID == "" {
		return &ValidationError{Field: "workflow_id", Message: "workflow_id is required"}
	}
	if p.Data == nil || p.Data.Slug == "" {
		return &ValidationError{Field: "data.slug", Message: "data.slug is required"}
	}
	if p.Data.Description == "" {
		return &ValidationError{Field: "data.description", Message: "data.description is required"}
	}
	return nil
}

// MarshalJSON marshals the TriggerPayload to JSON.
func (p *TriggerPayload) MarshalJSON() ([]byte, error) {
	type Alias TriggerPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON unmarshals the TriggerPayload from JSON.
func (p *TriggerPayload) UnmarshalJSON(data []byte) error {
	type Alias TriggerPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// WorkflowTriggerPayload is an alias for TriggerPayload for backward compatibility.
type WorkflowTriggerPayload = TriggerPayload //revive:disable-line

// WorkflowTriggerData is an alias for TriggerData for backward compatibility.
type WorkflowTriggerData = TriggerData //revive:disable-line

// WorkflowTriggerType is the message type for workflow trigger payloads.
// Note: Registration is handled by semstreams workflow processor.
var WorkflowTriggerType = message.Type{
	Domain:   "workflow",
	Category: "trigger",
	Version:  "v1",
}
