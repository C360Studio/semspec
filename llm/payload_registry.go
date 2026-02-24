package llm

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
)

func init() {
	err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "llm",
		Category:    "call",
		Version:     "v1",
		Description: "LLM call entity payload for graph ingestion",
		Factory:     func() any { return &LLMCallPayload{} },
	})
	if err != nil {
		panic("failed to register LLMCallPayload: " + err.Error())
	}
}

// LLMCallType is the message type for LLM call payloads.
var LLMCallType = message.Type{Domain: "llm", Category: "call", Version: "v1"}

// LLMCallPayload implements message.Payload and graph.Graphable.
type LLMCallPayload struct {
	ID         string           `json:"id"`
	TripleData []message.Triple `json:"triples"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

// EntityID returns the entity identifier.
func (p *LLMCallPayload) EntityID() string {
	return p.ID
}

// Triples returns the graph triples for this entity.
func (p *LLMCallPayload) Triples() []message.Triple {
	return p.TripleData
}

// Schema returns the message type.
func (p *LLMCallPayload) Schema() message.Type {
	return LLMCallType
}

// Validate ensures the payload has required fields.
func (p *LLMCallPayload) Validate() error {
	if p.ID == "" {
		return errors.New("entity ID is required")
	}
	if len(p.TripleData) == 0 {
		return errors.New("at least one triple is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler for the Payload interface.
func (p *LLMCallPayload) MarshalJSON() ([]byte, error) {
	type Alias LLMCallPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler for the Payload interface.
func (p *LLMCallPayload) UnmarshalJSON(data []byte) error {
	type Alias LLMCallPayload
	aux := (*Alias)(p)
	return json.Unmarshal(data, aux)
}
