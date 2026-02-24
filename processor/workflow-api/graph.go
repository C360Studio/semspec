package workflowapi

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

// publishPhaseEntity publishes a phase as a graph entity.
func (c *Component) publishPhaseEntity(ctx context.Context, slug string, phase *workflow.Phase) error {
	entityID := workflow.PhaseEntityID(slug, phase.Sequence)
	planEntityID := workflow.PlanEntityID(slug)

	triples := []message.Triple{
		{Subject: entityID, Predicate: semspec.PhaseName, Object: phase.Name},
		{Subject: entityID, Predicate: semspec.PhaseStatus, Object: string(phase.Status)},
		{Subject: entityID, Predicate: semspec.PhaseSequence, Object: phase.Sequence},
		{Subject: entityID, Predicate: semspec.PhasePlanID, Object: planEntityID},
		{Subject: entityID, Predicate: semspec.PhaseCreatedAt, Object: phase.CreatedAt.Format(time.RFC3339)},
		{Subject: entityID, Predicate: semspec.DCTitle, Object: phase.Name},
	}

	if phase.Description != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PhaseDescription, Object: phase.Description})
	}

	for _, depID := range phase.DependsOn {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PhaseDependsOn, Object: depID})
	}

	if phase.Approved {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PhaseApproved, Object: true})
	}
	if phase.ApprovedBy != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PhaseApprovedBy, Object: phase.ApprovedBy})
	}
	if phase.ApprovedAt != nil {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PhaseApprovedAt, Object: phase.ApprovedAt.Format(time.RFC3339)})
	}
	if phase.StartedAt != nil {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PhaseStartedAt, Object: phase.StartedAt.Format(time.RFC3339)})
	}
	if phase.CompletedAt != nil {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PhaseCompletedAt, Object: phase.CompletedAt.Format(time.RFC3339)})
	}

	if phase.AgentConfig != nil {
		if phase.AgentConfig.Model != "" {
			triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PhaseModel, Object: phase.AgentConfig.Model})
		}
		if phase.AgentConfig.MaxConcurrent > 0 {
			triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PhaseMaxConcurrent, Object: phase.AgentConfig.MaxConcurrent})
		}
	}

	return c.publishGraphEntity(ctx, &workflow.PhaseEntityPayload{
		ID:         entityID,
		TripleData: triples,
		UpdatedAt:  time.Now(),
	}, workflow.PhaseEntityType)
}

// publishApprovalEntity publishes an approval decision to the graph.
func (c *Component) publishApprovalEntity(ctx context.Context, targetType, targetID, decision, approvedBy, reason string) error {
	entityID := fmt.Sprintf("semspec.approval.%s", uuid.New().String())

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

	return c.publishGraphEntity(ctx, &workflow.ApprovalEntityPayload{
		ID:         entityID,
		TripleData: triples,
		UpdatedAt:  time.Now(),
	}, workflow.ApprovalEntityType)
}

// publishPhaseStatusUpdate publishes a phase status change to the graph.
func (c *Component) publishPhaseStatusUpdate(ctx context.Context, slug string, phase *workflow.Phase) error {
	// Re-publish the full phase entity with updated status
	return c.publishPhaseEntity(ctx, slug, phase)
}

// publishPlanPhasesLink publishes PlanHasPhases and PlanPhase predicates on the plan entity.
func (c *Component) publishPlanPhasesLink(ctx context.Context, slug string, phases []workflow.Phase) error {
	planEntityID := workflow.PlanEntityID(slug)

	triples := []message.Triple{
		{Subject: planEntityID, Predicate: semspec.PlanHasPhases, Object: true},
	}

	for _, p := range phases {
		phaseEntityID := workflow.PhaseEntityID(slug, p.Sequence)
		triples = append(triples, message.Triple{Subject: planEntityID, Predicate: semspec.PlanPhase, Object: phaseEntityID})
	}

	return c.publishGraphEntity(ctx, &workflow.PlanEntityPayload{
		ID:         planEntityID,
		TripleData: triples,
		UpdatedAt:  time.Now(),
	}, workflow.EntityType)
}

// publishGraphEntity marshals and publishes a graph entity to JetStream.
func (c *Component) publishGraphEntity(ctx context.Context, payload message.Payload, msgType message.Type) error {
	baseMsg := message.NewBaseMessage(msgType, payload, "workflow-api")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal graph entity: %w", err)
	}

	if err := c.natsClient.PublishToStream(ctx, graphIngestSubject, data); err != nil {
		return fmt.Errorf("publish to graph: %w", err)
	}

	return nil
}
