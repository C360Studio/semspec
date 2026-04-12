package planmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semspec/tools/decompose"
	"github.com/c360studio/semspec/vocabulary/semspec"
	wf "github.com/c360studio/semspec/vocabulary/workflow"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/message"
	"github.com/google/uuid"
)

const graphIngestSubject = "graph.ingest.entity"

// publishApprovalEntity publishes an approval decision to the graph.
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

// publishQuestionEntity publishes a question as a graph entity.
func (c *Component) publishQuestionEntity(ctx context.Context, q *workflow.Question) error {
	entityID := workflow.QuestionEntityID(q.ID)

	triples := []message.Triple{
		{Subject: entityID, Predicate: semspec.QuestionContent, Object: q.Question},
		{Subject: entityID, Predicate: semspec.QuestionTopic, Object: q.Topic},
		{Subject: entityID, Predicate: semspec.QuestionFromAgent, Object: q.FromAgent},
		{Subject: entityID, Predicate: semspec.QuestionStatus, Object: string(q.Status)},
		{Subject: entityID, Predicate: semspec.QuestionUrgency, Object: string(q.Urgency)},
		{Subject: entityID, Predicate: semspec.QuestionCreatedAt, Object: q.CreatedAt.Format(time.RFC3339)},
		{Subject: entityID, Predicate: semspec.DCTitle, Object: truncateForTitle(q.Question, 100)},
	}

	// Conditional fields
	if q.Context != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionContext, Object: q.Context})
	}
	if q.BlockedLoopID != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionBlockedLoopID, Object: q.BlockedLoopID})
	}
	if q.TraceID != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionTraceID, Object: q.TraceID})
	}
	if q.PlanSlug != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionPlanSlug, Object: q.PlanSlug})
		// Derive plan entity ID from slug
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionPlanID, Object: workflow.PlanEntityID(q.PlanSlug)})
	}
	if q.TaskID != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionTaskID, Object: q.TaskID})
	}
	if q.PhaseID != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionPhaseID, Object: q.PhaseID})
	}
	if q.AssignedTo != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionAssignedTo, Object: q.AssignedTo})
	}

	// Answer fields (when answered)
	if q.Answer != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionAnswer, Object: q.Answer})
	}
	if q.AnsweredBy != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionAnsweredBy, Object: q.AnsweredBy})
	}
	if q.AnswererType != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionAnswererType, Object: q.AnswererType})
	}
	if q.AnsweredAt != nil {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionAnsweredAt, Object: q.AnsweredAt.Format(time.RFC3339)})
	}
	if q.Confidence != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionConfidence, Object: q.Confidence})
	}
	if q.Sources != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionSources, Object: q.Sources})
	}

	return c.publishGraphEntity(ctx, workflow.NewEntityPayload(workflow.QuestionEntityType, entityID, triples))
}

// publishDAGNodeEntity publishes a DAG execution node as a graph entity.
func (c *Component) publishDAGNodeEntity(ctx context.Context, executionID string, node *decompose.TaskNode) error {
	entityID := workflow.DAGNodeEntityID(executionID, node.ID)
	execEntityID := fmt.Sprintf("%s.exec.scenario.run.%s", workflow.EntityPrefix(), executionID)

	triples := []message.Triple{
		{Subject: entityID, Predicate: wf.DAGNodeID, Object: node.ID},
		{Subject: entityID, Predicate: wf.DAGNodePrompt, Object: node.Prompt},
		{Subject: entityID, Predicate: wf.DAGNodeRole, Object: node.Role},
		{Subject: entityID, Predicate: wf.DAGNodeStatus, Object: "pending"},
		{Subject: entityID, Predicate: wf.RelScenario, Object: execEntityID},
	}

	// File scope — one triple per path.
	for _, f := range node.FileScope {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: wf.DAGNodeFileScope, Object: f})
	}

	// Dependency edges to sibling DAG nodes
	for _, depID := range node.DependsOn {
		depEntityID := workflow.DAGNodeEntityID(executionID, depID)
		triples = append(triples, message.Triple{Subject: entityID, Predicate: wf.DAGNodeDependsOn, Object: depEntityID})
	}

	return c.publishGraphEntity(ctx, workflow.NewEntityPayload(workflow.DAGNodeEntityType, entityID, triples))
}

// truncateForTitle truncates a string for use as a DCTitle predicate value.
func truncateForTitle(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}

// publishGraphEntity marshals and publishes a graph entity to JetStream.
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
