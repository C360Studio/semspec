package payloads

import (
	"encoding/json"
	"fmt"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/message"
)

// QA phase payloads — target-project test execution gate between reviewing_rollup
// and complete. qa-runner consumes QARequestedPayload and publishes QACompletedPayload.
// qa-reviewer consumes QACompletedPayload (failure path) to emit ChangeProposals.

// QARequestedType is the message type for QA execution requests.
var QARequestedType = message.Type{
	Domain:   "workflow",
	Category: "qa-requested",
	Version:  "v1",
}

// QARequestedPayload wraps workflow.QARequestedEvent to satisfy message.Payload
// for publishing via message.NewBaseMessage.
type QARequestedPayload struct {
	workflow.QARequestedEvent
}

// Schema implements message.Payload.
func (p *QARequestedPayload) Schema() message.Type { return QARequestedType }

// Validate implements message.Payload.
func (p *QARequestedPayload) Validate() error {
	// ValidateSlug rejects empty, path-traversal-shaped, and non-slug-pattern
	// values — the same guard plan-manager applies at creation.
	if err := workflow.ValidateSlug(p.Slug); err != nil {
		return fmt.Errorf("slug: %w", err)
	}
	if p.WorkspaceHostPath == "" {
		return fmt.Errorf("workspace_host_path is required")
	}
	// Mode must be one of the executor-bound levels. none and synthesis don't
	// publish QARequestedEvent — they skip QA or go straight to reviewing_qa.
	if !p.Mode.IsValid() || p.Mode == workflow.QALevelNone || p.Mode == workflow.QALevelSynthesis {
		return fmt.Errorf("mode must be unit|integration|full, got %q", p.Mode)
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *QARequestedPayload) MarshalJSON() ([]byte, error) {
	type Alias workflow.QARequestedEvent
	return json.Marshal((*Alias)(&p.QARequestedEvent))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *QARequestedPayload) UnmarshalJSON(data []byte) error {
	type Alias workflow.QARequestedEvent
	return json.Unmarshal(data, (*Alias)(&p.QARequestedEvent))
}

// QACompletedType is the message type for QA execution result events.
var QACompletedType = message.Type{
	Domain:   "workflow",
	Category: "qa-completed",
	Version:  "v1",
}

// QACompletedPayload wraps workflow.QACompletedEvent to satisfy message.Payload
// for publishing via message.NewBaseMessage.
type QACompletedPayload struct {
	workflow.QACompletedEvent
}

// Schema implements message.Payload.
func (p *QACompletedPayload) Schema() message.Type { return QACompletedType }

// Validate implements message.Payload.
func (p *QACompletedPayload) Validate() error {
	if err := workflow.ValidateSlug(p.Slug); err != nil {
		return fmt.Errorf("slug: %w", err)
	}
	if p.RunID == "" {
		return fmt.Errorf("run_id is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *QACompletedPayload) MarshalJSON() ([]byte, error) {
	type Alias workflow.QACompletedEvent
	return json.Marshal((*Alias)(&p.QACompletedEvent))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *QACompletedPayload) UnmarshalJSON(data []byte) error {
	type Alias workflow.QACompletedEvent
	return json.Unmarshal(data, (*Alias)(&p.QACompletedEvent))
}
