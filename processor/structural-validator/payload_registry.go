package structuralvalidator

import (
	"encoding/json"
	"fmt"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
)

// ValidationTrigger is published to workflow.async.structural-validator.
// It carries the slug and the list of files modified by the developer agent,
// used to determine which checklist checks are relevant to run.
//
// Embeds workflow.CallbackFields to support publish_async dispatch from the
// workflow-processor. When dispatched via a workflow step, the processor
// injects callback_subject and task_id so the structural-validator can
// publish an AsyncStepResult back.
type ValidationTrigger struct {
	workflow.CallbackFields

	Slug          string   `json:"slug"`
	FilesModified []string `json:"files_modified"`
	WorkflowID    string   `json:"workflow_id,omitempty"`
}

// Schema implements message.Payload.
func (p *ValidationTrigger) Schema() message.Type {
	return ValidationTriggerType
}

// Validate implements message.Payload.
func (p *ValidationTrigger) Validate() error {
	if p.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *ValidationTrigger) MarshalJSON() ([]byte, error) {
	type Alias ValidationTrigger
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *ValidationTrigger) UnmarshalJSON(data []byte) error {
	type Alias ValidationTrigger
	return json.Unmarshal(data, (*Alias)(p))
}

// ValidationResult is published to workflow.result.structural-validator.<slug>.
// It summarises which checks ran and whether all required checks passed.
type ValidationResult struct {
	Slug         string        `json:"slug"`
	Passed       bool          `json:"passed"`
	ChecksRun    int           `json:"checks_run"`
	CheckResults []CheckResult `json:"check_results"`
	Warning      string        `json:"warning,omitempty"`
}

// Schema implements message.Payload.
func (p *ValidationResult) Schema() message.Type {
	return ValidationResultType
}

// Validate implements message.Payload.
func (p *ValidationResult) Validate() error {
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *ValidationResult) MarshalJSON() ([]byte, error) {
	type Alias ValidationResult
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *ValidationResult) UnmarshalJSON(data []byte) error {
	type Alias ValidationResult
	return json.Unmarshal(data, (*Alias)(p))
}

// CheckResult holds the outcome of a single checklist check execution.
type CheckResult struct {
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	Required bool   `json:"required"`
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	Duration string `json:"duration"`
}

// ValidationTriggerType is the message type for validation triggers.
var ValidationTriggerType = message.Type{
	Domain:   "workflow",
	Category: "structural-validation-trigger",
	Version:  "v1",
}

// ValidationResultType is the message type for validation results.
var ValidationResultType = message.Type{
	Domain:   "workflow",
	Category: "structural-validation-result",
	Version:  "v1",
}

func init() {
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "structural-validation-trigger",
		Version:     "v1",
		Description: "Structural validation trigger — files modified by developer agent",
		Factory:     func() any { return &ValidationTrigger{} },
	}); err != nil {
		panic("failed to register ValidationTrigger: " + err.Error())
	}

	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "structural-validation-result",
		Version:     "v1",
		Description: "Structural validation result — checklist execution summary",
		Factory:     func() any { return &ValidationResult{} },
	}); err != nil {
		panic("failed to register ValidationResult: " + err.Error())
	}
}
