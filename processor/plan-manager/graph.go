package planmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/message"
	"github.com/google/uuid"
)

const graphIngestSubject = "graph.ingest.entity"

// publishApprovalEntity publishes an approval decision to the graph.
// Approvals are write-once (fresh uuid entity ID per call), so the
// append-merge behaviour of graph.ingest.entity is harmless here — there is
// no stale-scalar problem when the entity is never mutated after creation.
func (c *Component) publishApprovalEntity(ctx context.Context, targetType, targetID, decision, approvedBy, reason string) error {
	entityID := workflow.ApprovalEntityID(uuid.New().String())

	triples := []message.Triple{
		{Subject: entityID, Predicate: semspec.ApprovalTargetType, Object: targetType},
		{Subject: entityID, Predicate: semspec.ApprovalTargetID, Object: targetID},
		{Subject: entityID, Predicate: semspec.ApprovalDecision, Object: decision},
		{Subject: entityID, Predicate: semspec.ApprovalCreatedAt, Object: time.Now().Format(time.RFC3339)},
		{Subject: entityID, Predicate: semspec.DCTitle, Object: fmt.Sprintf("%s %s", targetType, decision)},
	}

	if approvedBy != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.ApprovalApprovedBy, Object: approvedBy})
	}
	if reason != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.ApprovalReason, Object: reason})
	}

	return c.publishGraphEntity(ctx, workflow.NewEntityPayload(workflow.ApprovalEntityType, entityID, triples))
}

// publishGraphEntity marshals and publishes a graph entity to JetStream.
// Used by publishApprovalEntity (write-once entities; append-merge is safe).
func (c *Component) publishGraphEntity(ctx context.Context, payload message.Payload) error {
	if c.natsClient == nil {
		return nil
	}

	baseMsg := message.NewBaseMessage(payload.Schema(), payload, "plan-manager")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal graph entity: %w", err)
	}

	if err := c.natsClient.PublishToStream(ctx, graphIngestSubject, data); err != nil {
		return fmt.Errorf("publish to graph: %w", err)
	}

	return nil
}
