package sourceingester

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
)

func init() {
	err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "source",
		Category:    "entity",
		Version:     "v1",
		Description: "Document source entity payload for graph ingestion",
		Factory:     func() any { return &SourceEntityPayload{} },
	})
	if err != nil {
		panic("failed to register SourceEntityPayload: " + err.Error())
	}
}

// SourceEntityType is the message type for source entity payloads.
var SourceEntityType = message.Type{Domain: "source", Category: "entity", Version: "v1"}

// SourceEntityPayload implements message.Payload and graph.Graphable for source entity ingestion.
type SourceEntityPayload struct {
	EntityID_  string           `json:"id"`
	TripleData []message.Triple `json:"triples"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

// EntityID returns the entity identifier for Graphable interface.
func (p *SourceEntityPayload) EntityID() string { return p.EntityID_ }

// Triples returns the entity triples for Graphable interface.
func (p *SourceEntityPayload) Triples() []message.Triple { return p.TripleData }

// Schema returns the message type for Payload interface.
func (p *SourceEntityPayload) Schema() message.Type { return SourceEntityType }

// Validate validates the payload for Payload interface.
func (p *SourceEntityPayload) Validate() error {
	if p.EntityID_ == "" {
		return errors.New("entity ID is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *SourceEntityPayload) MarshalJSON() ([]byte, error) {
	type Alias SourceEntityPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *SourceEntityPayload) UnmarshalJSON(data []byte) error {
	type Alias SourceEntityPayload
	return json.Unmarshal(data, (*Alias)(p))
}
