package revieworchestrator

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
		Category:    "review-execution",
		Version:     "v1",
		Description: "Review execution entity payload for graph ingestion",
		Factory:     func() any { return &ReviewExecutionPayload{} },
	}); err != nil {
		panic("failed to register ReviewExecutionPayload: " + err.Error())
	}
}

// ReviewExecutionPayloadType is the message type for review execution entity payloads.
var ReviewExecutionPayloadType = message.Type{Domain: "workflow", Category: "review-execution", Version: "v1"}

// ReviewExecutionPayload implements message.Payload and wraps entity triples for graph ingestion.
type ReviewExecutionPayload struct {
	ID         string           `json:"id"`
	TripleData []message.Triple `json:"triples"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

// EntityID returns the entity identifier.
func (p *ReviewExecutionPayload) EntityID() string { return p.ID }

// Triples returns the graph triples for this entity.
func (p *ReviewExecutionPayload) Triples() []message.Triple { return p.TripleData }

// Schema returns the message type.
func (p *ReviewExecutionPayload) Schema() message.Type { return ReviewExecutionPayloadType }

// Validate ensures the payload has required fields.
func (p *ReviewExecutionPayload) Validate() error {
	if p.ID == "" {
		return errors.New("entity ID is required")
	}
	if len(p.TripleData) == 0 {
		return errors.New("at least one triple is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *ReviewExecutionPayload) MarshalJSON() ([]byte, error) {
	type Alias ReviewExecutionPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *ReviewExecutionPayload) UnmarshalJSON(data []byte) error {
	type Alias ReviewExecutionPayload
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

	payload := &ReviewExecutionPayload{
		ID:         entity.EntityID(),
		TripleData: entity.Triples(),
		UpdatedAt:  time.Now(),
	}

	msg := message.NewBaseMessage(ReviewExecutionPayloadType, payload, componentName)
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
