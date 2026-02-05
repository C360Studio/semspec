package graph

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
)

func init() {
	err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "graph",
		Category:    "entity",
		Version:     "v1",
		Description: "Entity payload for graph ingestion with triples",
		Factory:     func() any { return &EntityPayload{} },
	})
	if err != nil {
		panic("failed to register EntityPayload: " + err.Error())
	}
}

// EntityType is the message type for graph entity payloads.
var EntityType = message.Type{Domain: "graph", Category: "entity", Version: "v1"}

// EntityPayload implements message.Payload and graph.Graphable for entity ingestion.
type EntityPayload struct {
	EntityID_  string           `json:"id"`
	TripleData []message.Triple `json:"triples"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

func (e *EntityPayload) EntityID() string          { return e.EntityID_ }
func (e *EntityPayload) Triples() []message.Triple { return e.TripleData }
func (e *EntityPayload) Schema() message.Type      { return EntityType }

func (e *EntityPayload) Validate() error {
	if e.EntityID_ == "" {
		return errors.New("entity ID is required")
	}
	return nil
}

func (e *EntityPayload) MarshalJSON() ([]byte, error) {
	type Alias EntityPayload
	return json.Marshal((*Alias)(e))
}

func (e *EntityPayload) UnmarshalJSON(data []byte) error {
	type Alias EntityPayload
	return json.Unmarshal(data, (*Alias)(e))
}
