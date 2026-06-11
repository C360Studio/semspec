package changeproposalhandler

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
)

// RegisterPayloads registers plan-decision-handler payload types with the
// supplied registry. Called from cmd/semspec/main.go bootstrap.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	return reg.Register(&payloadregistry.Registration{
		Domain:      "workflow",
		Category:    "cascade-execution",
		Version:     "v1",
		Description: "Cascade execution entity payload for graph ingestion",
		Factory:     func() any { return &CascadePayload{} },
	})
}

// CascadePayloadType is the message type for cascade entity payloads.
var CascadePayloadType = message.Type{Domain: "workflow", Category: "cascade-execution", Version: "v1"}

// CascadePayload implements message.Payload and wraps entity triples for graph ingestion.
type CascadePayload struct {
	ID         string           `json:"id"`
	TripleData []message.Triple `json:"triples"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

// EntityID returns the entity identifier.
func (p *CascadePayload) EntityID() string { return p.ID }

// Triples returns the graph triples for this entity.
func (p *CascadePayload) Triples() []message.Triple { return p.TripleData }

// Schema returns the message type.
func (p *CascadePayload) Schema() message.Type { return CascadePayloadType }

// Validate ensures the payload has required fields.
func (p *CascadePayload) Validate() error {
	if p.ID == "" {
		return errors.New("entity ID is required")
	}
	if len(p.TripleData) == 0 {
		return errors.New("at least one triple is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *CascadePayload) MarshalJSON() ([]byte, error) {
	type Alias CascadePayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *CascadePayload) UnmarshalJSON(data []byte) error {
	type Alias CascadePayload
	return json.Unmarshal(data, (*Alias)(p))
}

// publishEntity publishes the entity's triples to the graph using replace-own-predicates
// semantics via UpsertEntity. Failures are logged as warnings but do not propagate —
// graph ingest is best-effort for workflow state observability.
func (c *Component) publishEntity(ctx context.Context, entity interface {
	EntityID() string
	Triples() []message.Triple
}) {
	if err := c.tripleWriter.UpsertEntity(ctx, CascadePayloadType, entity.EntityID(), entity.Triples()); err != nil {
		c.logger.Warn("Failed to upsert entity to graph",
			"entity_id", entity.EntityID(), "error", err)
	}
}
