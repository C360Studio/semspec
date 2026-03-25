package executionmanager

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
		Category:    "task-execution",
		Version:     "v1",
		Description: "Task execution entity payload for graph ingestion",
		Factory:     func() any { return &TaskExecutionPayload{} },
	}); err != nil {
		panic("failed to register TaskExecutionPayload: " + err.Error())
	}
}

// TaskExecutionPayloadType is the message type for task execution entity payloads.
var TaskExecutionPayloadType = message.Type{Domain: "workflow", Category: "task-execution", Version: "v1"}

// TaskExecutionPayload implements message.Payload and wraps entity triples for graph ingestion.
type TaskExecutionPayload struct {
	ID         string           `json:"id"`
	TripleData []message.Triple `json:"triples"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

// EntityID returns the entity identifier.
func (p *TaskExecutionPayload) EntityID() string { return p.ID }

// Triples returns the graph triples for this entity.
func (p *TaskExecutionPayload) Triples() []message.Triple { return p.TripleData }

// Schema returns the message type.
func (p *TaskExecutionPayload) Schema() message.Type { return TaskExecutionPayloadType }

// Validate ensures the payload has required fields.
func (p *TaskExecutionPayload) Validate() error {
	if p.ID == "" {
		return errors.New("entity ID is required")
	}
	if len(p.TripleData) == 0 {
		return errors.New("at least one triple is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *TaskExecutionPayload) MarshalJSON() ([]byte, error) {
	type Alias TaskExecutionPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *TaskExecutionPayload) UnmarshalJSON(data []byte) error {
	type Alias TaskExecutionPayload
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

	payload := &TaskExecutionPayload{
		ID:         entity.EntityID(),
		TripleData: entity.Triples(),
		UpdatedAt:  time.Now(),
	}

	msg := message.NewBaseMessage(TaskExecutionPayloadType, payload, componentName)
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
