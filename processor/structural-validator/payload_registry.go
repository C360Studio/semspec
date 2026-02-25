package structuralvalidator

import (
	"encoding/json"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
)

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

// ValidationResultType is the message type for validation results.
var ValidationResultType = message.Type{
	Domain:   "workflow",
	Category: "structural-validation-result",
	Version:  "v1",
}

func init() {
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
