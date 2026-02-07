package workflow

import (
	"encoding/json"
	"log"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
)

// WorkflowTriggerPayload represents a trigger for the workflow processor.
// This is published to workflow.trigger.document-generation to start
// the full document generation workflow.
type WorkflowTriggerPayload struct {
	// WorkflowID identifies which workflow definition to execute
	WorkflowID string `json:"workflow_id"`

	// Slug is the workflow change slug
	Slug string `json:"slug"`

	// Title is the human-readable title for the change
	Title string `json:"title"`

	// Description is the original description provided by the user
	Description string `json:"description"`

	// Prompt is the full system prompt for the first step (proposal)
	Prompt string `json:"prompt"`

	// Model is the primary model to use
	Model string `json:"model"`

	// Auto indicates if the workflow should auto-continue through all steps
	Auto bool `json:"auto"`

	// UserID is the ID of the user who initiated the workflow
	UserID string `json:"user_id"`

	// ChannelType is the channel type for response routing
	ChannelType string `json:"channel_type"`

	// ChannelID is the channel ID for response routing
	ChannelID string `json:"channel_id"`
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
	if p.Slug == "" {
		return &ValidationError{Field: "slug", Message: "slug is required"}
	}
	if p.Description == "" {
		return &ValidationError{Field: "description", Message: "description is required"}
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
var WorkflowTriggerType = message.Type{
	Domain:   "workflow",
	Category: "trigger",
	Version:  "v1",
}

func init() {
	// Register the workflow trigger payload type
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "trigger",
		Version:     "v1",
		Description: "Workflow trigger payload for document generation",
		Factory:     func() any { return &WorkflowTriggerPayload{} },
	}); err != nil {
		log.Printf("ERROR: failed to register WorkflowTriggerPayload: %v", err)
	}
}
