package workflowvalidator

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/c360studio/semspec/workflow/validation"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
)

// ValidateRequest is the request payload for document validation.
type ValidateRequest struct {
	// Slug is the workflow plan slug
	Slug string `json:"slug"`

	// Document is the document type to validate (plan, tasks)
	Document string `json:"document"`

	// Content is the document content to validate
	// Either Content or Path must be provided
	Content string `json:"content,omitempty"`

	// Path is the file path to read content from
	// Either Content or Path must be provided
	Path string `json:"path,omitempty"`
}

// ValidateResponse is the response payload for document validation.
type ValidateResponse struct {
	// Valid indicates if validation passed
	Valid bool `json:"valid"`

	// DocumentType is the validated document type
	DocumentType string `json:"document_type"`

	// MissingSections lists missing or incomplete sections
	MissingSections []string `json:"missing_sections,omitempty"`

	// Warnings are non-blocking issues
	Warnings []string `json:"warnings,omitempty"`

	// SectionDetails provides status of each checked section
	SectionDetails map[string]string `json:"section_details,omitempty"`

	// Feedback is the formatted validation feedback for retry prompts
	Feedback string `json:"feedback,omitempty"`

	// Error is set if validation could not be performed
	Error string `json:"error,omitempty"`
}

// FromValidationResult converts a validation.ValidationResult to ValidateResponse.
func FromValidationResult(result *validation.ValidationResult) *ValidateResponse {
	return &ValidateResponse{
		Valid:           result.Valid,
		DocumentType:    string(result.DocumentType),
		MissingSections: result.MissingSections,
		Warnings:        result.Warnings,
		SectionDetails:  result.SectionDetails,
		Feedback:        result.FormatFeedback(),
	}
}

// Schema returns the message type for ValidateRequest.
func (p *ValidateRequest) Schema() message.Type {
	return ValidateRequestType
}

// Validate validates the ValidateRequest.
func (p *ValidateRequest) Validate() error {
	if p.Document == "" {
		return fmt.Errorf("document type is required")
	}
	if p.Content == "" && p.Path == "" {
		return fmt.Errorf("either content or path is required")
	}
	return nil
}

// MarshalJSON marshals the ValidateRequest to JSON.
func (p *ValidateRequest) MarshalJSON() ([]byte, error) {
	type Alias ValidateRequest
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON unmarshals the ValidateRequest from JSON.
func (p *ValidateRequest) UnmarshalJSON(data []byte) error {
	type Alias ValidateRequest
	return json.Unmarshal(data, (*Alias)(p))
}

// Schema returns the message type for ValidateResponse.
func (p *ValidateResponse) Schema() message.Type {
	return ValidateResponseType
}

// Validate validates the ValidateResponse.
func (p *ValidateResponse) Validate() error {
	return nil
}

// MarshalJSON marshals the ValidateResponse to JSON.
func (p *ValidateResponse) MarshalJSON() ([]byte, error) {
	type Alias ValidateResponse
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON unmarshals the ValidateResponse from JSON.
func (p *ValidateResponse) UnmarshalJSON(data []byte) error {
	type Alias ValidateResponse
	return json.Unmarshal(data, (*Alias)(p))
}

// ValidateRequestType is the message type for validation requests.
var ValidateRequestType = message.Type{
	Domain:   "workflow",
	Category: "validate.request",
	Version:  "v1",
}

// ValidateResponseType is the message type for validation responses.
var ValidateResponseType = message.Type{
	Domain:   "workflow",
	Category: "validate.response",
	Version:  "v1",
}

func init() {
	// Register the validation request payload type
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "validate.request",
		Version:     "v1",
		Description: "Workflow document validation request",
		Factory:     func() any { return &ValidateRequest{} },
	}); err != nil {
		log.Printf("ERROR: failed to register ValidateRequest: %v", err)
	}

	// Register the validation response payload type
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "validate.response",
		Version:     "v1",
		Description: "Workflow document validation response",
		Factory:     func() any { return &ValidateResponse{} },
	}); err != nil {
		log.Printf("ERROR: failed to register ValidateResponse: %v", err)
	}
}
