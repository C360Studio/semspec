package payloads

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/message"
)

// QA phase payloads — target-project test execution gate before complete. The
// sandbox consumes QARequestedPayload (unit mode) and publishes
// QACompletedPayload; qa-reviewer consumes QACompletedPayload (failure path) to
// emit PlanDecisions. Heavier tiers run in the operator's CI, not a semspec executor.

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
	// Mode must be the sandbox-executed unit level. none and synthesis don't
	// publish QARequestedEvent (they skip QA or go straight to reviewing_qa);
	// heavier tiers run in the operator's CI, not via a semspec executor.
	if p.Mode != workflow.QALevelUnit {
		return fmt.Errorf("mode must be unit, got %q", p.Mode)
	}
	// Workspace, when present, is the QA worktree's sandbox task_id. Reject
	// path-traversal-shaped values so a malformed event cannot escape the
	// sandbox worktree root when worktreeFor joins it onto worktreeRoot.
	if p.Workspace != "" &&
		(strings.Contains(p.Workspace, "..") ||
			strings.Contains(p.Workspace, "/") ||
			strings.Contains(p.Workspace, "\\")) {
		return fmt.Errorf("workspace contains path separators: %q", p.Workspace)
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
