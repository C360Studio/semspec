package planapi

import (
	"context"
	"encoding/json"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/message"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

// triggerPartialRequirementGeneration publishes a RequirementGeneratorRequest with
// ReplaceRequirementIDs set so the requirement-generator regenerates only the
// rejected requirements rather than the full set.
func (c *Component) triggerPartialRequirementGeneration(ctx context.Context, plan *workflow.Plan, affectedIDs []string, reasons map[string]string) {
	req := &payloads.RequirementGeneratorRequest{
		ExecutionID:           uuid.New().String(),
		Slug:                  plan.Slug,
		Title:                 plan.Title,
		TraceID:               latestTraceID(plan),
		ReplaceRequirementIDs: affectedIDs,
		RejectionReasons:      reasons,
	}

	baseMsg := message.NewBaseMessage(req.Schema(), req, "plan-api")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Error("Failed to marshal partial requirement generator request",
			"slug", plan.Slug, "error", err)
		return
	}

	if c.natsClient == nil {
		c.logger.Warn("Cannot trigger partial requirement generation: NATS client not configured",
			"slug", plan.Slug)
		return
	}
	if err := c.natsClient.PublishToStream(ctx, "workflow.async.requirement-generator", data); err != nil {
		c.logger.Error("Failed to trigger partial requirement generation",
			"slug", plan.Slug, "error", err)
		return
	}

	c.logger.Info("Triggered partial requirement regeneration",
		"slug", plan.Slug, "affected_ids", affectedIDs)
}

// triggerRequirementGeneration publishes a RequirementGeneratorRequest to JetStream
// after a human approves a plan via POST /promote (round 1).
func (c *Component) triggerRequirementGeneration(ctx context.Context, plan *workflow.Plan) {
	req := &payloads.RequirementGeneratorRequest{
		ExecutionID: uuid.New().String(),
		Slug:        plan.Slug,
		Title:       plan.Title,
		TraceID:     latestTraceID(plan),
	}

	baseMsg := message.NewBaseMessage(req.Schema(), req, "plan-api")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Error("Failed to marshal requirement generator request",
			"slug", plan.Slug, "error", err)
		return
	}

	if c.natsClient == nil {
		c.logger.Warn("Cannot trigger requirement generation: NATS client not configured",
			"slug", plan.Slug)
		return
	}
	if err := c.natsClient.PublishToStream(ctx, "workflow.async.requirement-generator", data); err != nil {
		c.logger.Error("Failed to trigger requirement generation",
			"slug", plan.Slug, "error", err)
		return
	}

	c.logger.Info("Triggered requirement generation (human approval)",
		"slug", plan.Slug, "trace_id", req.TraceID)
}

// handleRequirementsGeneratedEvent updates plan status and dispatches scenario
// generation for each requirement. This handles both the auto-approve path
// (where plan-coordinator also dispatches — idempotent) and the manual approval
// path (where plan-coordinator has terminated and plan-api must dispatch).
func (c *Component) handleRequirementsGeneratedEvent(ctx context.Context, event *workflow.RequirementsGeneratedEvent) {
	if event.Slug == "" {
		c.logger.Warn("Requirements generated event missing slug")
		return
	}

	manager := c.newManager()
	if manager == nil {
		return
	}

	// Update plan.json status so HTTP API reflects the transition.
	if plan, err := manager.LoadPlan(ctx, event.Slug); err == nil {
		if err := manager.SetPlanStatus(ctx, plan, workflow.StatusRequirementsGenerated); err != nil {
			c.logger.Debug("Failed to transition plan to requirements_generated",
				"slug", event.Slug, "error", err)
		}
	}

	// Load requirements and dispatch scenario generation for each.
	requirements, err := manager.LoadRequirements(ctx, event.Slug)
	if err != nil {
		c.logger.Error("Failed to load requirements for scenario generation",
			"slug", event.Slug, "error", err)
		return
	}

	if len(requirements) == 0 {
		c.logger.Warn("No requirements found after generation event",
			"slug", event.Slug)
		return
	}

	for _, req := range requirements {
		c.triggerScenarioGeneration(ctx, event.Slug, req.ID, event.TraceID)
	}

	c.logger.Info("Dispatched scenario generation for all requirements",
		"slug", event.Slug,
		"requirement_count", len(requirements))
}

// handleScenariosGeneratedEvent updates plan status when scenarios are generated.
// Orchestration (dispatching reviewer round 2) is handled by plan-coordinator.
func (c *Component) handleScenariosGeneratedEvent(ctx context.Context, event *workflow.ScenariosGeneratedEvent) {
	if event.Slug == "" {
		c.logger.Warn("Scenarios generated event missing slug")
		return
	}

	manager := c.newManager()
	if manager == nil {
		return
	}

	// Update plan.json status so HTTP API reflects the transition.
	if plan, err := manager.LoadPlan(ctx, event.Slug); err == nil {
		if err := manager.SetPlanStatus(ctx, plan, workflow.StatusScenariosGenerated); err != nil {
			c.logger.Debug("Failed to transition plan to scenarios_generated",
				"slug", event.Slug, "error", err)
		}
	}
}

// triggerScenarioGeneration publishes a ScenarioGeneratorRequest for a single requirement.
func (c *Component) triggerScenarioGeneration(ctx context.Context, slug, requirementID, traceID string) {
	req := &payloads.ScenarioGeneratorRequest{
		ExecutionID:   uuid.New().String(),
		Slug:          slug,
		RequirementID: requirementID,
		TraceID:       traceID,
	}

	baseMsg := message.NewBaseMessage(req.Schema(), req, "plan-api")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Error("Failed to marshal scenario generator request",
			"slug", slug, "requirement_id", requirementID, "error", err)
		return
	}

	if c.natsClient == nil {
		return
	}
	if err := c.natsClient.PublishToStream(ctx, "workflow.async.scenario-generator", data); err != nil {
		c.logger.Error("Failed to trigger scenario generation",
			"slug", slug, "requirement_id", requirementID, "error", err)
		return
	}

	c.logger.Debug("Triggered scenario generation",
		"slug", slug, "requirement_id", requirementID)
}

// latestTraceID extracts the most recent trace ID from a plan's execution history.
func latestTraceID(plan *workflow.Plan) string {
	if len(plan.ExecutionTraceIDs) > 0 {
		return plan.ExecutionTraceIDs[len(plan.ExecutionTraceIDs)-1]
	}
	return ""
}

// Wire requirements/scenarios generated events into the event dispatcher.

func init() {
	// Register the new event types for BaseMessage deserialization.
	// These use simple struct payloads published by the requirement-generator
	// and scenario-generator components.
}

// dispatchCascadeEvent routes cascade events to status-update handlers.
// Called from processWorkflowEvent to handle the cascade subjects.
func (c *Component) dispatchCascadeEvent(ctx context.Context, msg jetstream.Msg) bool {
	switch msg.Subject() {
	case workflow.RequirementsGenerated.Pattern:
		event, err := payloads.ParseReactivePayload[workflow.RequirementsGeneratedEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse requirements generated event", "error", err)
			return true
		}
		c.handleRequirementsGeneratedEvent(ctx, event)
		return true

	case workflow.ScenariosGenerated.Pattern:
		event, err := payloads.ParseReactivePayload[workflow.ScenariosGeneratedEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse scenarios generated event", "error", err)
			return true
		}
		c.handleScenariosGeneratedEvent(ctx, event)
		return true
	}

	return false
}
