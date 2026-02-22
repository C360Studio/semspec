package astindexer

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
)

func init() {
	err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "ast",
		Category:    "entity",
		Version:     "v1",
		Description: "AST code entity payload for graph ingestion",
		Factory:     func() any { return &ASTEntityPayload{} },
	})
	if err != nil {
		panic("failed to register ASTEntityPayload: " + err.Error())
	}
}

// ASTEntityType is the message type for AST entity payloads.
var ASTEntityType = message.Type{Domain: "ast", Category: "entity", Version: "v1"}

// ASTEntityPayload implements message.Payload and graph.Graphable for AST entity ingestion.
type ASTEntityPayload struct {
	ID         string           `json:"id"`
	TripleData []message.Triple `json:"triples"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

// EntityID returns the entity identifier for Graphable interface.
func (p *ASTEntityPayload) EntityID() string { return p.ID }

// Triples returns the entity triples for Graphable interface.
func (p *ASTEntityPayload) Triples() []message.Triple { return p.TripleData }

// Schema returns the message type for Payload interface.
func (p *ASTEntityPayload) Schema() message.Type { return ASTEntityType }

// Validate validates the payload for Payload interface.
func (p *ASTEntityPayload) Validate() error {
	if p.ID == "" {
		return errors.New("entity ID is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *ASTEntityPayload) MarshalJSON() ([]byte, error) {
	type Alias ASTEntityPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *ASTEntityPayload) UnmarshalJSON(data []byte) error {
	type Alias ASTEntityPayload
	return json.Unmarshal(data, (*Alias)(p))
}
