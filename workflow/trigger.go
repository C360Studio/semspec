package workflow

import (
	"encoding/json"

	"github.com/c360studio/semstreams/message"
)

// WorkflowTriggerType is the message type for workflow trigger payloads.
// Uses semstreams' canonical workflow.trigger.v1 type.
var WorkflowTriggerType = message.Type{
	Domain:   "workflow",
	Category: "trigger",
	Version:  "v1",
}

// TriggerPayload is semspec's view of workflow trigger data.
// It provides access to both standard semstreams fields and semspec-specific Data fields.
//
// For SENDING triggers, use semstreams' TriggerPayload directly with
// custom fields marshaled into the Data blob.
// For RECEIVING triggers, use reactive.ParseReactivePayload[T].
type TriggerPayload struct {
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

	// Semspec-specific fields (populated from Data blob)
	Slug          string   `json:"slug,omitempty"`
	Title         string   `json:"title,omitempty"`
	Description   string   `json:"description,omitempty"`
	Auto          bool     `json:"auto,omitempty"`
	ProjectID     string   `json:"project_id,omitempty"`
	ScopePatterns []string `json:"scope_patterns,omitempty"`
	TraceID       string   `json:"trace_id,omitempty"`
	LoopID        string   `json:"loop_id,omitempty"`

	// Data holds any additional custom fields as raw JSON
	Data json.RawMessage `json:"data,omitempty"`
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
	if p.Slug == "" {
		return &ValidationError{Field: "slug", Message: "slug is required"}
	}
	return nil
}

// MarshalJSON marshals the TriggerPayload to JSON.
func (p *TriggerPayload) MarshalJSON() ([]byte, error) {
	type Alias TriggerPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON unmarshals the TriggerPayload from JSON.
// It handles both flattened and nested Data structures.
func (p *TriggerPayload) UnmarshalJSON(data []byte) error {
	type Alias TriggerPayload
	if err := json.Unmarshal(data, (*Alias)(p)); err != nil {
		return err
	}

	// If semspec-specific fields are empty but Data is present,
	// try to extract them from the Data blob (handles nested case)
	if p.Slug == "" && len(p.Data) > 0 {
		var nested struct {
			Slug          string   `json:"slug,omitempty"`
			Title         string   `json:"title,omitempty"`
			Description   string   `json:"description,omitempty"`
			Auto          bool     `json:"auto,omitempty"`
			ProjectID     string   `json:"project_id,omitempty"`
			ScopePatterns []string `json:"scope_patterns,omitempty"`
			TraceID       string   `json:"trace_id,omitempty"`
		}
		if err := json.Unmarshal(p.Data, &nested); err == nil {
			if nested.Slug != "" {
				p.Slug = nested.Slug
			}
			if nested.Title != "" {
				p.Title = nested.Title
			}
			if nested.Description != "" {
				p.Description = nested.Description
			}
			if nested.Auto {
				p.Auto = nested.Auto
			}
			if nested.ProjectID != "" {
				p.ProjectID = nested.ProjectID
			}
			if len(nested.ScopePatterns) > 0 {
				p.ScopePatterns = nested.ScopePatterns
			}
			if nested.TraceID != "" && p.TraceID == "" {
				p.TraceID = nested.TraceID
			}
		}
	}
	return nil
}

// WorkflowTriggerPayload is an alias for TriggerPayload for backward compatibility.
type WorkflowTriggerPayload = TriggerPayload //revive:disable-line

// MarshalTriggerData creates a json.RawMessage containing semspec-specific
// workflow fields. This is used when sending triggers via semstreams'
// TriggerPayload.Data field.
func MarshalTriggerData(slug, title, description, traceID, projectID string, scopePatterns []string, auto bool) json.RawMessage {
	data := map[string]any{
		"slug":        slug,
		"title":       title,
		"description": description,
		"trace_id":    traceID,
	}
	if projectID != "" {
		data["project_id"] = projectID
	}
	if len(scopePatterns) > 0 {
		data["scope_patterns"] = scopePatterns
	}
	if auto {
		data["auto"] = auto
	}
	blob, _ := json.Marshal(data)
	return blob
}

// NewSemstreamsTrigger creates a TriggerPayload with semspec fields populated
// both as top-level fields and in the Data blob for backward compatibility.
func NewSemstreamsTrigger(workflowID, role, prompt, requestID, slug, title, description, traceID, projectID string, scopePatterns []string, auto bool) *TriggerPayload {
	return &TriggerPayload{
		WorkflowID:    workflowID,
		Role:          role,
		Prompt:        prompt,
		RequestID:     requestID,
		Slug:          slug,
		Title:         title,
		Description:   description,
		TraceID:       traceID,
		ProjectID:     projectID,
		ScopePatterns: scopePatterns,
		Auto:          auto,
		Data:          MarshalTriggerData(slug, title, description, traceID, projectID, scopePatterns, auto),
	}
}

