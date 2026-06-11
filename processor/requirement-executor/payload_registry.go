package requirementexecutor

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
)

// RegisterPayloads registers requirement-executor payload types with the
// supplied registry. Called from cmd/semspec/main.go bootstrap.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	return reg.Register(&payloadregistry.Registration{
		Domain:      "workflow",
		Category:    "requirement-execution",
		Version:     "v1",
		Description: "Requirement execution entity payload for graph ingestion",
		Factory:     func() any { return &RequirementExecutionPayload{} },
	})
}

// RequirementExecutionPayloadType is the message type for requirement execution entity payloads.
var RequirementExecutionPayloadType = message.Type{Domain: "workflow", Category: "requirement-execution", Version: "v1"}

// RequirementExecutionPayload implements message.Payload and wraps entity triples for graph ingestion.
type RequirementExecutionPayload struct {
	ID         string           `json:"id"`
	TripleData []message.Triple `json:"triples"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

// EntityID returns the entity identifier.
func (p *RequirementExecutionPayload) EntityID() string { return p.ID }

// Triples returns the graph triples for this entity.
func (p *RequirementExecutionPayload) Triples() []message.Triple { return p.TripleData }

// Schema returns the message type.
func (p *RequirementExecutionPayload) Schema() message.Type { return RequirementExecutionPayloadType }

// Validate ensures the payload has required fields.
func (p *RequirementExecutionPayload) Validate() error {
	if p.ID == "" {
		return errors.New("entity ID is required")
	}
	if len(p.TripleData) == 0 {
		return errors.New("at least one triple is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *RequirementExecutionPayload) MarshalJSON() ([]byte, error) {
	type Alias RequirementExecutionPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *RequirementExecutionPayload) UnmarshalJSON(data []byte) error {
	type Alias RequirementExecutionPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// publishEntity publishes the entity's triples to the graph using replace-own-predicates
// semantics via UpsertEntity. Failures are logged as warnings but do not propagate —
// graph ingest is best-effort for workflow state observability.
func (c *Component) publishEntity(ctx context.Context, entity interface {
	EntityID() string
	Triples() []message.Triple
}) {
	if err := c.tripleWriter.UpsertEntity(ctx, RequirementExecutionPayloadType, entity.EntityID(), entity.Triples()); err != nil {
		c.logger.Warn("Failed to upsert entity to graph",
			"entity_id", entity.EntityID(), "error", err)
	}
}
