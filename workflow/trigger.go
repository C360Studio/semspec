package workflow

import (
	"encoding/json"

	"github.com/c360studio/semstreams/message"
)

// WorkflowTriggerPayload represents a trigger for the workflow processor.
// This matches the semstreams TriggerPayload format with well-known fields
// at the top level and semspec-specific fields in the Data blob.
type WorkflowTriggerPayload struct {
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

	// Semspec-specific fields in data blob
	Data *WorkflowTriggerData `json:"data,omitempty"`
}

// WorkflowTriggerData contains semspec-specific workflow fields.
// These are accessible via ${trigger.payload.slug}, ${trigger.payload.title}, etc.
type WorkflowTriggerData struct {
	// Slug is the workflow change slug (e.g., "add-user-authentication")
	Slug string `json:"slug,omitempty"`

	// Title is the human-readable title for the change
	Title string `json:"title,omitempty"`

	// Description is the original description provided by the user
	Description string `json:"description,omitempty"`

	// Auto indicates if the workflow should auto-continue through all steps
	Auto bool `json:"auto,omitempty"`
}

// Schema returns the message type for WorkflowTriggerPayload.
func (p *WorkflowTriggerPayload) Schema() message.Type {
	return WorkflowTriggerType
}

// Validate validates the WorkflowTriggerPayload.
func (p *WorkflowTriggerPayload) Validate() error {
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

// MarshalJSON marshals the WorkflowTriggerPayload to JSON.
func (p *WorkflowTriggerPayload) MarshalJSON() ([]byte, error) {
	type Alias WorkflowTriggerPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON unmarshals the WorkflowTriggerPayload from JSON.
func (p *WorkflowTriggerPayload) UnmarshalJSON(data []byte) error {
	type Alias WorkflowTriggerPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// WorkflowTriggerType is the message type for workflow trigger payloads.
// Note: Registration is handled by semstreams workflow processor.
var WorkflowTriggerType = message.Type{
	Domain:   "workflow",
	Category: "trigger",
	Version:  "v1",
}
