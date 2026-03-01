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

	return c.publishGraphEntity(ctx, workflow.NewWorkflowEntityPayload(workflow.PhaseEntityType, entityID, triples))
}

// publishPlanEntity publishes a plan as a graph entity.
func (c *Component) publishPlanEntity(ctx context.Context, plan *workflow.Plan) error {
	entityID := workflow.PlanEntityID(plan.Slug)

	triples := []message.Triple{
		{Subject: entityID, Predicate: semspec.PlanTitle, Object: plan.Title},
		{Subject: entityID, Predicate: semspec.PlanSlug, Object: plan.Slug},
		{Subject: entityID, Predicate: semspec.PredicatePlanStatus, Object: string(plan.EffectiveStatus())},
		{Subject: entityID, Predicate: semspec.PlanCreatedAt, Object: plan.CreatedAt.Format(time.RFC3339)},
		{Subject: entityID, Predicate: semspec.DCTitle, Object: plan.Title},
	}

	if plan.Goal != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanGoal, Object: plan.Goal})
	}
	if plan.Context != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanContext, Object: plan.Context})
	}
	if plan.ProjectID != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.PlanProject, Object: plan.ProjectID})
	}

	return c.publishGraphEntity(ctx, workflow.NewWorkflowEntityPayload(workflow.EntityType, entityID, triples))
}

// publishTaskEntity publishes a task as a graph entity.
func (c *Component) publishTaskEntity(ctx context.Context, slug string, task *workflow.Task) error {
	entityID := workflow.TaskEntityID(slug, task.Sequence)
	planEntityID := workflow.PlanEntityID(slug)

	triples := []message.Triple{
		{Subject: entityID, Predicate: semspec.TaskTitle, Object: task.Description},
		{Subject: entityID, Predicate: semspec.TaskDescription, Object: task.Description},
		{Subject: entityID, Predicate: semspec.PredicateTaskStatus, Object: string(task.Status)},
		{Subject: entityID, Predicate: semspec.PredicateTaskType, Object: string(task.Type)},
		{Subject: entityID, Predicate: semspec.TaskOrder, Object: task.Sequence},
		{Subject: entityID, Predicate: semspec.TaskCreatedAt, Object: task.CreatedAt.Format(time.RFC3339)},
		{Subject: entityID, Predicate: semspec.TaskPlan, Object: planEntityID},
		{Subject: entityID, Predicate: semspec.DCTitle, Object: task.Description},
	}

	if task.PhaseID != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.TaskPhase, Object: task.PhaseID})
	}

	for _, ac := range task.AcceptanceCriteria {
		if ac.Given != "" {
			triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.TaskGiven, Object: ac.Given})
		}
		if ac.When != "" {
			triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.TaskWhen, Object: ac.When})
		}
		if ac.Then != "" {
			triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.TaskThen, Object: ac.Then})
		}
	}

	return c.publishGraphEntity(ctx, workflow.NewWorkflowEntityPayload(workflow.TaskEntityType, entityID, triples))
}

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

	return c.publishGraphEntity(ctx, workflow.NewWorkflowEntityPayload(workflow.ApprovalEntityType, entityID, triples))
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

	return c.publishGraphEntity(ctx, workflow.NewWorkflowEntityPayload(workflow.EntityType, planEntityID, triples))
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

	return c.publishGraphEntity(ctx, workflow.NewWorkflowEntityPayload(workflow.QuestionEntityType, entityID, triples))
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
	baseMsg := message.NewBaseMessage(payload.Schema(), payload, "workflow-api")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal graph entity: %w", err)
	}

	if err := c.natsClient.PublishToStream(ctx, graphIngestSubject, data); err != nil {
		return fmt.Errorf("publish to graph: %w", err)
	}

	return nil
}
