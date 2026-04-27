package webingest

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
)

// WebEntityType is the message type for web entity payloads published to
// graph.ingest.entity. Re-homed from processor/web-ingester after that
// component was removed in WS-25 (httptool now writes to the graph directly).
var WebEntityType = message.Type{Domain: "web", Category: "entity", Version: "v1"}

// WebEntityPayload implements message.Payload and graph.Graphable for web
// source entity ingestion. Schema unchanged from the old web-ingester
// package — graph-ingest still reads payloads of this domain/category/version.
type WebEntityPayload struct {
	ID         string           `json:"id"`
	TripleData []message.Triple `json:"triples"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

// EntityID returns the entity identifier for Graphable interface.
func (p *WebEntityPayload) EntityID() string { return p.ID }

// Triples returns the entity triples for Graphable interface.
func (p *WebEntityPayload) Triples() []message.Triple { return p.TripleData }

// Schema returns the message type for Payload interface.
func (p *WebEntityPayload) Schema() message.Type { return WebEntityType }

// Validate validates the payload for Payload interface.
func (p *WebEntityPayload) Validate() error {
	if p.ID == "" {
		return errors.New("entity ID is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *WebEntityPayload) MarshalJSON() ([]byte, error) {
	type Alias WebEntityPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *WebEntityPayload) UnmarshalJSON(data []byte) error {
	type Alias WebEntityPayload
	return json.Unmarshal(data, (*Alias)(p))
}

func init() {
	if err := payloadregistry.Register(&payloadregistry.Registration{
		Domain:      "web",
		Category:    "entity",
		Version:     "v1",
		Description: "Web source entity payload for graph ingestion",
		Factory:     func() any { return &WebEntityPayload{} },
	}); err != nil {
		panic("failed to register WebEntityPayload: " + err.Error())
	}
}
