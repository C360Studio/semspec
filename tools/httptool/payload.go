package httptool

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
)

// webEntityType is the message type for web entity payloads published to
// graph.ingest.entity. Re-homed from processor/web-ingester after that
// component was removed in WS-25 (httptool now writes to the graph directly).
var webEntityType = message.Type{Domain: "web", Category: "entity", Version: "v1"}

// webEntityPayload implements message.Payload and graph.Graphable for web
// source entity ingestion. Schema unchanged from the old web-ingester
// package — graph-ingest still reads payloads of this domain/category/version.
type webEntityPayload struct {
	ID         string           `json:"id"`
	TripleData []message.Triple `json:"triples"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

// EntityID returns the entity identifier for Graphable interface.
func (p *webEntityPayload) EntityID() string { return p.ID }

// Triples returns the entity triples for Graphable interface.
func (p *webEntityPayload) Triples() []message.Triple { return p.TripleData }

// Schema returns the message type for Payload interface.
func (p *webEntityPayload) Schema() message.Type { return webEntityType }

// Validate validates the payload for Payload interface.
func (p *webEntityPayload) Validate() error {
	if p.ID == "" {
		return errors.New("entity ID is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *webEntityPayload) MarshalJSON() ([]byte, error) {
	type Alias webEntityPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *webEntityPayload) UnmarshalJSON(data []byte) error {
	type Alias webEntityPayload
	return json.Unmarshal(data, (*Alias)(p))
}

func init() {
	if err := payloadregistry.Register(&payloadregistry.Registration{
		Domain:      "web",
		Category:    "entity",
		Version:     "v1",
		Description: "Web source entity payload for graph ingestion",
		Factory:     func() any { return &webEntityPayload{} },
	}); err != nil {
		panic("failed to register webEntityPayload: " + err.Error())
	}
}
