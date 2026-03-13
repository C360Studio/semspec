package changeproposalhandler

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
)

func init() {
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "cascade-execution",
		Version:     "v1",
		Description: "Cascade execution entity payload for graph ingestion",
		Factory:     func() any { return &CascadePayload{} },
	}); err != nil {
		panic("failed to register CascadePayload: " + err.Error())
	}
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

// publishEntity publishes the entity's triples to the graph ingest subject.
// Failures are logged as warnings but do not propagate — graph ingest is best-effort
// for workflow state observability.
func (c *Component) publishEntity(ctx context.Context, entity interface {
	EntityID() string
	Triples() []message.Triple
}) {
	if c.natsClient == nil {
		return
	}

	payload := &CascadePayload{
		ID:         entity.EntityID(),
		TripleData: entity.Triples(),
		UpdatedAt:  time.Now(),
	}

	msg := message.NewBaseMessage(CascadePayloadType, payload, "change-proposal-handler")
	data, err := json.Marshal(msg)
	if err != nil {
		c.logger.Warn("Failed to marshal entity for graph ingest",
			"entity_id", entity.EntityID(), "error", err)
		return
	}

	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Warn("Failed to get JetStream for entity publish",
			"entity_id", entity.EntityID(), "error", err)
		return
	}

	if _, err := js.Publish(ctx, graphIngestSubject, data); err != nil {
		c.logger.Warn("Failed to publish entity to graph",
			"entity_id", entity.EntityID(), "error", err)
	}
}

// graphIngestSubject is the NATS subject for graph entity ingestion.
const graphIngestSubject = "graph.ingest.entity"
