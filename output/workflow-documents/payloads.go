package workflowdocuments

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
)

// DocumentOutputPayload represents a document to be written to disk.
// This is published to output.workflow.documents by the workflow processor.
type DocumentOutputPayload struct {
	// Slug is the workflow change slug (used for directory path)
	Slug string `json:"slug"`

	// Document is the document type (proposal, design, spec, tasks)
	Document string `json:"document"`

	// Content is the structured JSON content from LLM output
	Content DocumentContent `json:"content"`

	// EntityID is the graph entity ID for this document
	EntityID string `json:"entity_id,omitempty"`

	// UserID is the user who initiated the workflow
	UserID string `json:"user_id,omitempty"`

	// ChannelType for response routing
	ChannelType string `json:"channel_type,omitempty"`

	// ChannelID for response routing
	ChannelID string `json:"channel_id,omitempty"`
}

// DocumentContent represents the structured content from LLM output.
// This is the JSON format that LLMs produce, which gets transformed to markdown.
type DocumentContent struct {
	// Title is the document title
	Title string `json:"title"`

	// Sections contains the document sections as key-value pairs
	// Keys are section names (why, what_changes, impact, etc.)
	// Values can be strings or nested structures
	Sections map[string]any `json:"sections"`

	// Status is the workflow status (proposed, designed, specified, etc.)
	Status string `json:"status,omitempty"`

	// Metadata contains additional metadata
	Metadata map[string]any `json:"metadata,omitempty"`
}

// DocumentWrittenPayload is published when a document is successfully written.
type DocumentWrittenPayload struct {
	// Slug is the workflow change slug
	Slug string `json:"slug"`

	// Document is the document type
	Document string `json:"document"`

	// Path is the full file path where the document was written
	Path string `json:"path"`

	// EntityID is the graph entity ID
	EntityID string `json:"entity_id,omitempty"`
}

// Schema returns the message type for DocumentOutputPayload.
func (p *DocumentOutputPayload) Schema() message.Type {
	return DocumentOutputType
}

// Validate validates the DocumentOutputPayload.
func (p *DocumentOutputPayload) Validate() error {
	if p.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	if p.Document == "" {
		return fmt.Errorf("document type is required")
	}
	return nil
}

// MarshalJSON marshals the DocumentOutputPayload to JSON.
func (p *DocumentOutputPayload) MarshalJSON() ([]byte, error) {
	type Alias DocumentOutputPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON unmarshals the DocumentOutputPayload from JSON.
func (p *DocumentOutputPayload) UnmarshalJSON(data []byte) error {
	type Alias DocumentOutputPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// Schema returns the message type for DocumentWrittenPayload.
func (p *DocumentWrittenPayload) Schema() message.Type {
	return DocumentWrittenType
}

// Validate validates the DocumentWrittenPayload.
func (p *DocumentWrittenPayload) Validate() error {
	if p.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	if p.Document == "" {
		return fmt.Errorf("document type is required")
	}
	if p.Path == "" {
		return fmt.Errorf("path is required")
	}
	return nil
}

// MarshalJSON marshals the DocumentWrittenPayload to JSON.
func (p *DocumentWrittenPayload) MarshalJSON() ([]byte, error) {
	type Alias DocumentWrittenPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON unmarshals the DocumentWrittenPayload from JSON.
func (p *DocumentWrittenPayload) UnmarshalJSON(data []byte) error {
	type Alias DocumentWrittenPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// DocumentOutputType is the message type for document output payloads.
var DocumentOutputType = message.Type{
	Domain:   "workflow",
	Category: "document.output",
	Version:  "v1",
}

// DocumentWrittenType is the message type for document written notifications.
var DocumentWrittenType = message.Type{
	Domain:   "workflow",
	Category: "document.written",
	Version:  "v1",
}

func init() {
	// Register the document output payload type
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "document.output",
		Version:     "v1",
		Description: "Workflow document output for file export",
		Factory:     func() any { return &DocumentOutputPayload{} },
	}); err != nil {
		log.Printf("ERROR: failed to register DocumentOutputPayload: %v", err)
	}

	// Register the document written payload type
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "document.written",
		Version:     "v1",
		Description: "Notification when workflow document is written to disk",
		Factory:     func() any { return &DocumentWrittenPayload{} },
	}); err != nil {
		log.Printf("ERROR: failed to register DocumentWrittenPayload: %v", err)
	}
}
