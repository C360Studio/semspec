package webingester

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
)

func init() {
	err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "web",
		Category:    "entity",
		Version:     "v1",
		Description: "Web source entity payload for graph ingestion",
		Factory:     func() any { return &WebEntityPayload{} },
	})
	if err != nil {
		panic("failed to register WebEntityPayload: " + err.Error())
	}
}

// WebEntityType is the message type for web entity payloads.
var WebEntityType = message.Type{Domain: "web", Category: "entity", Version: "v1"}

// WebEntityPayload implements message.Payload and graph.Graphable for web source entity ingestion.
type WebEntityPayload struct {
	EntityID_  string           `json:"id"`
	TripleData []message.Triple `json:"triples"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

// EntityID returns the entity identifier for Graphable interface.
func (p *WebEntityPayload) EntityID() string { return p.EntityID_ }

// Triples returns the entity triples for Graphable interface.
func (p *WebEntityPayload) Triples() []message.Triple { return p.TripleData }

// Schema returns the message type for Payload interface.
func (p *WebEntityPayload) Schema() message.Type { return WebEntityType }

// Validate validates the payload for Payload interface.
func (p *WebEntityPayload) Validate() error {
	if p.EntityID_ == "" {
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
