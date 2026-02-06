package rdfexport

import (
	"encoding/json"
	"errors"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
)

func init() {
	err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "rdf",
		Category:    "export",
		Version:     "v1",
		Description: "RDF export payload containing serialized entity data",
		Factory:     func() any { return &RDFExportPayload{} },
	})
	if err != nil {
		panic("failed to register RDFExportPayload: " + err.Error())
	}
}

// RDFExportType is the message type for RDF export payloads.
var RDFExportType = message.Type{Domain: "rdf", Category: "export", Version: "v1"}

// RDFExportPayload represents serialized RDF output from the rdf-export component.
type RDFExportPayload struct {
	EntityID string `json:"entity_id"`
	Format   string `json:"format"`  // turtle, ntriples, jsonld
	Profile  string `json:"profile"` // minimal, bfo, cco
	Content  string `json:"content"` // serialized RDF
}

// Schema returns the message type for Payload interface.
func (p *RDFExportPayload) Schema() message.Type { return RDFExportType }

// Validate validates the payload for Payload interface.
func (p *RDFExportPayload) Validate() error {
	if p.EntityID == "" {
		return errors.New("entity_id is required")
	}
	if p.Format == "" {
		return errors.New("format is required")
	}
	if p.Content == "" {
		return errors.New("content is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *RDFExportPayload) MarshalJSON() ([]byte, error) {
	type Alias RDFExportPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *RDFExportPayload) UnmarshalJSON(data []byte) error {
	type Alias RDFExportPayload
	return json.Unmarshal(data, (*Alias)(p))
}
