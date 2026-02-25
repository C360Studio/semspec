package workflow

import (
	"encoding/json"
	"fmt"

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
// It embeds CallbackFields for async dispatch and provides access to
// both standard semstreams fields and semspec-specific Data fields.
//
// This type is used for RECEIVING triggers (via ParseNATSMessage).
// For SENDING triggers, use semstreams' TriggerPayload directly with
// custom fields marshaled into the Data blob.
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

// CallbackReceiver is implemented by any payload that embeds CallbackFields.
// ParseNATSMessage uses this to inject task_id and callback_subject from
// the AsyncTaskPayload envelope into the parsed result.
type CallbackReceiver interface {
	SetCallback(taskID, callbackSubject string)
}

// SetCallback implements CallbackReceiver for CallbackFields.
func (c *CallbackFields) SetCallback(taskID, callbackSubject string) {
	c.TaskID = taskID
	c.CallbackSubject = callbackSubject
}

// asyncTaskEnvelope is the minimal structure needed to extract callback fields
// and nested data from a workflow.async_task.v1 BaseMessage payload.
type asyncTaskEnvelope struct {
	TaskID          string          `json:"task_id"`
	CallbackSubject string          `json:"callback_subject,omitempty"`
	Data            json.RawMessage `json:"data,omitempty"`
}

// genericJSONEnvelope is the minimal structure for core.json.v1 payloads
// from workflow publish/call actions.
type genericJSONEnvelope struct {
	Data json.RawMessage `json:"data,omitempty"`
}

// ParseNATSMessage unmarshals a NATS message into the target type, handling
// three wire formats:
//
//  1. BaseMessage with AsyncTaskPayload (workflow.async_task.v1) — the
//     workflow-processor publish_async format. Callback fields are extracted
//     from the envelope and injected into T via CallbackReceiver. The actual
//     payload is in AsyncTaskPayload.Data.
//
//  2. BaseMessage with direct payload — standard component-to-component
//     format where the payload type matches T directly.
//
//  3. Raw JSON — legacy fallback for untyped messages.
func ParseNATSMessage[T any](data []byte) (*T, error) {
	var baseMsg message.BaseMessage
	if err := json.Unmarshal(data, &baseMsg); err == nil && baseMsg.Payload() != nil {
		msgType := baseMsg.Type()

		// Case 1: AsyncTaskPayload from workflow-processor publish_async
		if msgType.Domain == "workflow" && msgType.Category == "async_task" {
			payloadBytes, err := json.Marshal(baseMsg.Payload())
			if err != nil {
				return nil, fmt.Errorf("marshal async_task payload: %w", err)
			}
			var envelope asyncTaskEnvelope
			if err := json.Unmarshal(payloadBytes, &envelope); err != nil {
				return nil, fmt.Errorf("unmarshal async_task envelope: %w", err)
			}

			// Unmarshal the nested data into T
			var result T
			if len(envelope.Data) > 0 {
				if err := json.Unmarshal(envelope.Data, &result); err != nil {
					return nil, fmt.Errorf("unmarshal async_task data: %w", err)
				}
			}

			// Inject callback fields if T embeds CallbackFields
			if receiver, ok := any(&result).(CallbackReceiver); ok {
				receiver.SetCallback(envelope.TaskID, envelope.CallbackSubject)
			}
			return &result, nil
		}

		// Case 2: GenericJSONPayload from workflow publish/call actions
		if msgType.Domain == "core" && msgType.Category == "json" {
			payloadBytes, err := json.Marshal(baseMsg.Payload())
			if err != nil {
				return nil, fmt.Errorf("marshal core.json payload: %w", err)
			}
			var envelope genericJSONEnvelope
			if err := json.Unmarshal(payloadBytes, &envelope); err != nil {
				return nil, fmt.Errorf("unmarshal core.json envelope: %w", err)
			}
			var result T
			if len(envelope.Data) > 0 {
				if err := json.Unmarshal(envelope.Data, &result); err != nil {
					return nil, fmt.Errorf("unmarshal core.json data: %w", err)
				}
			}
			return &result, nil
		}

		// Case 3: Direct payload (e.g., workflow.trigger.v1 from workflow-api).
		// Extract raw payload JSON to preserve fields the registered factory
		// doesn't know about (e.g., TraceID is in semspec's TriggerPayload
		// but not in semstreams').
		var rawMsg struct {
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(data, &rawMsg); err == nil && len(rawMsg.Payload) > 0 {
			var result T
			if err := json.Unmarshal(rawMsg.Payload, &result); err != nil {
				return nil, fmt.Errorf("unmarshal BaseMessage payload: %w", err)
			}
			return &result, nil
		}
		// Fallback: use the registered type's marshal output
		payloadBytes, err := json.Marshal(baseMsg.Payload())
		if err != nil {
			return nil, fmt.Errorf("marshal BaseMessage payload: %w", err)
		}
		var result T
		if err := json.Unmarshal(payloadBytes, &result); err != nil {
			return nil, fmt.Errorf("unmarshal BaseMessage payload: %w", err)
		}
		return &result, nil
	}

	// Case 4: Raw JSON fallback
	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal raw message: %w", err)
	}
	return &result, nil
}
