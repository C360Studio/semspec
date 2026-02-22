package workflow

import (
	"encoding/json"
	"fmt"

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

	// ProjectID is the graph entity ID for the project (e.g., "semspec.local.project.default").
	// Used by plan-review-loop to pass project context to the plan-reviewer step.
	ProjectID string `json:"project_id,omitempty"`

	// ScopePatterns are file glob patterns from the plan's scope.include.
	// Used by task-review-loop to pass scope context to the task-reviewer step.
	ScopePatterns []string `json:"scope_patterns,omitempty"`

	// TraceID propagates trace context through the workflow.
	// Duplicated here (in addition to TriggerPayload.TraceID) because the
	// semstreams workflow-processor flattens Data into the merged payload—
	// fields NOT in the semstreams TriggerPayload struct are dropped.
	TraceID string `json:"trace_id,omitempty"`
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
