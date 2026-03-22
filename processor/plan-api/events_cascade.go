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

// triggerRequirementGeneration publishes a RequirementGeneratorRequest to JetStream
// after a plan is approved. This starts the ADR-026 auto-cascade:
// plan approved -> requirements generated -> scenarios generated -> ready for execution.
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

	if err := c.natsClient.PublishToStream(ctx, "workflow.async.requirement-generator", data); err != nil {
		c.logger.Error("Failed to trigger requirement generation",
			"slug", plan.Slug, "error", err)
		return
	}

	c.logger.Info("Triggered requirement generation cascade",
		"slug", plan.Slug, "trace_id", req.TraceID)
}

// handleRequirementsGeneratedEvent fires scenario generation for each requirement,
// then transitions the plan to scenarios_generated -> ready_for_execution.
// Also updates plan.json status to requirements_generated for HTTP API visibility.
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

	// Load the requirements that were just generated.
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

	// Dispatch scenario generation for each requirement.
	for _, req := range requirements {
		c.triggerScenarioGeneration(ctx, event.Slug, req.ID, event.TraceID)
	}

	c.logger.Info("Dispatched scenario generation for all requirements",
		"slug", event.Slug,
		"requirement_count", len(requirements))
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

	if err := c.natsClient.PublishToStream(ctx, "workflow.async.scenario-generator", data); err != nil {
		c.logger.Error("Failed to trigger scenario generation",
			"slug", slug, "requirement_id", requirementID, "error", err)
		return
	}

	c.logger.Debug("Triggered scenario generation",
		"slug", slug, "requirement_id", requirementID)
}

// handleScenariosGeneratedEvent transitions the plan to ready_for_execution
// after all scenarios have been generated.
func (c *Component) handleScenariosGeneratedEvent(ctx context.Context, event *workflow.ScenariosGeneratedEvent) {
	if event.Slug == "" {
		c.logger.Warn("Scenarios generated event missing slug")
		return
	}

	manager := c.newManager()
	if manager == nil {
		return
	}

	plan, err := manager.LoadPlan(ctx, event.Slug)
	if err != nil {
		c.logger.Error("Failed to load plan for ready-for-execution transition",
			"slug", event.Slug, "error", err)
		return
	}

	// Transition through scenarios_generated first (some tests check intermediate statuses).
	if err := manager.SetPlanStatus(ctx, plan, workflow.StatusScenariosGenerated); err != nil {
		c.logger.Debug("Failed to transition plan to scenarios_generated",
			"slug", event.Slug, "error", err)
		// Reload in case another handler already advanced the status.
		plan, err = manager.LoadPlan(ctx, event.Slug)
		if err != nil {
			return
		}
	}

	if err := manager.SetPlanStatus(ctx, plan, workflow.StatusReadyForExecution); err != nil {
		c.logger.Error("Failed to transition plan to ready_for_execution",
			"slug", event.Slug, "error", err)
		return
	}

	c.logger.Info("Plan ready for execution — cascade complete",
		"slug", event.Slug,
		"scenario_count", event.ScenarioCount)
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

// registerCascadeEventRoutes adds the cascade event subjects to the event dispatcher.
// Called from processWorkflowEvent to handle the new ADR-026 subjects.
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
