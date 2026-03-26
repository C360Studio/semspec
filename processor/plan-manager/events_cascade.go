package planmanager

import (
	"context"
	"encoding/json"
	"time"

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
		Goal:                  plan.Goal,
		Context:               plan.Context,
		Scope:                 &plan.Scope,
		ExistingRequirements:  c.requirements.listByPlan(plan.Slug),
	}

	baseMsg := message.NewBaseMessage(req.Schema(), req, "plan-manager")
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
		Goal:        plan.Goal,
		Context:     plan.Context,
		Scope:       &plan.Scope,
	}

	baseMsg := message.NewBaseMessage(req.Schema(), req, "plan-manager")
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

	// Single-writer: persist requirements through the store (cache + triples).
	if len(event.Requirements) > 0 {
		if err := c.requirements.saveAll(ctx, event.Requirements, event.Slug); err != nil {
			c.logger.Error("Failed to save requirements from generator",
				"slug", event.Slug, "error", err)
			return
		}
	}

	// Advance plan status.
	var planGoal, planContext string
	if plan, err := c.loadPlanCached(ctx, event.Slug); err == nil {
		planGoal = plan.Goal
		planContext = plan.Context
		if err := c.setPlanStatusCached(ctx, plan, workflow.StatusRequirementsGenerated); err != nil {
			c.logger.Debug("Failed to transition plan to requirements_generated",
				"slug", event.Slug, "error", err)
		}
	}

	// Dispatch scenario generation from cache (now populated).
	requirements := c.requirements.listByPlan(event.Slug)
	if len(requirements) == 0 {
		c.logger.Warn("No requirements in cache after save — skipping scenario dispatch",
			"slug", event.Slug)
		return
	}

	for _, req := range requirements {
		c.triggerScenarioGeneration(ctx, event.Slug, req.ID, event.TraceID, planGoal, planContext)
	}

	c.logger.Info("Dispatched scenario generation for all requirements",
		"slug", event.Slug,
		"requirement_count", len(requirements))
}

// handleScenariosForRequirementGenerated persists scenarios for a single requirement
// and checks convergence. When all requirements have scenarios, it advances the plan
// to scenarios_generated and publishes a ScenariosGeneratedEvent.
func (c *Component) handleScenariosForRequirementGenerated(ctx context.Context, event *workflow.ScenariosForRequirementGeneratedEvent) {
	if event.Slug == "" || event.RequirementID == "" {
		c.logger.Warn("ScenariosForRequirement event missing slug or requirement_id")
		return
	}

	// Single-writer: persist scenarios through the store (cache + triples).
	if len(event.Scenarios) > 0 {
		if err := c.scenarios.saveAll(ctx, event.Scenarios, event.Slug); err != nil {
			c.logger.Error("Failed to save scenarios from generator",
				"slug", event.Slug, "requirement_id", event.RequirementID, "error", err)
			return
		}
	}

	c.logger.Info("Saved scenarios for requirement",
		"slug", event.Slug,
		"requirement_id", event.RequirementID,
		"scenario_count", len(event.Scenarios))

	// Check convergence: do all requirements have at least one scenario?
	requirements := c.requirements.listByPlan(event.Slug)
	if len(requirements) == 0 {
		c.logger.Debug("No requirements in cache — cannot check scenario convergence",
			"slug", event.Slug)
		return
	}

	for _, req := range requirements {
		if len(c.scenarios.listByRequirement(req.ID)) == 0 {
			return // not all requirements covered yet
		}
	}

	// All requirements covered — advance status and publish aggregate event.
	totalScenarios := 0
	for _, req := range requirements {
		totalScenarios += len(c.scenarios.listByRequirement(req.ID))
	}

	if plan, err := c.loadPlanCached(ctx, event.Slug); err == nil {
		if err := c.setPlanStatusCached(ctx, plan, workflow.StatusScenariosGenerated); err != nil {
			c.logger.Debug("Failed to transition plan to scenarios_generated",
				"slug", event.Slug, "error", err)
		}
	}

	c.publishScenariosGeneratedEvent(ctx, event.Slug, totalScenarios, event.TraceID)
	c.logger.Info("All requirements have scenarios — advanced to scenarios_generated",
		"slug", event.Slug,
		"requirement_count", len(requirements),
		"scenario_count", totalScenarios)
}

// handleScenariosGeneratedEvent updates plan status when the aggregate scenarios event fires.
// This is published by plan-manager itself after convergence, consumed by the coordinator.
func (c *Component) handleScenariosGeneratedEvent(ctx context.Context, event *workflow.ScenariosGeneratedEvent) {
	if event.Slug == "" {
		c.logger.Warn("Scenarios generated event missing slug")
		return
	}

	// Update plan status so HTTP API reflects the transition.
	if plan, err := c.loadPlanCached(ctx, event.Slug); err == nil {
		if err := c.setPlanStatusCached(ctx, plan, workflow.StatusScenariosGenerated); err != nil {
			c.logger.Debug("Failed to transition plan to scenarios_generated",
				"slug", event.Slug, "error", err)
		}
	}
}

// handleGenerationFailed marks a plan as rejected when a generator fails after all retries.
func (c *Component) handleGenerationFailed(ctx context.Context, event *workflow.GenerationFailedEvent) {
	if event.Slug == "" {
		return
	}
	c.logger.Error("Generation failed",
		"slug", event.Slug, "phase", event.Phase, "error", event.Error)

	if plan, err := c.loadPlanCached(ctx, event.Slug); err == nil {
		plan.LastError = event.Error
		now := time.Now()
		plan.LastErrorAt = &now
		if err := c.setPlanStatusCached(ctx, plan, workflow.StatusRejected); err != nil {
			c.logger.Error("Failed to mark plan as rejected after generation failure",
				"slug", event.Slug, "error", err)
		}
	}
}

// publishScenariosGeneratedEvent publishes the aggregate ScenariosGeneratedEvent
// after convergence (all requirements have scenarios).
func (c *Component) publishScenariosGeneratedEvent(ctx context.Context, slug string, scenarioCount int, traceID string) {
	event := &workflow.ScenariosGeneratedEvent{
		Slug:          slug,
		ScenarioCount: scenarioCount,
		TraceID:       traceID,
	}

	// Use a simple inline payload that satisfies message.Payload.
	baseMsg := message.NewBaseMessage(message.Type{
		Domain: "workflow", Category: "scenarios-generated", Version: "v1",
	}, &scenariosGeneratedPayloadWrapper{event: event}, "plan-manager")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Error("Failed to marshal ScenariosGeneratedEvent", "slug", slug, "error", err)
		return
	}

	if c.natsClient == nil {
		return
	}
	if err := c.natsClient.PublishToStream(ctx, workflow.ScenariosGenerated.Pattern, data); err != nil {
		c.logger.Error("Failed to publish ScenariosGeneratedEvent", "slug", slug, "error", err)
	}
}

// triggerScenarioGeneration publishes a ScenarioGeneratorRequest for a single requirement.
// All data is carried in the payload so the scenario-generator needs no graph reads.
func (c *Component) triggerScenarioGeneration(ctx context.Context, slug, requirementID, traceID, planGoal, planContext string) {
	// Look up requirement from cache to carry title/description.
	var reqTitle, reqDesc string
	if req, ok := c.requirements.get(requirementID); ok {
		reqTitle = req.Title
		reqDesc = req.Description
	}

	req := &payloads.ScenarioGeneratorRequest{
		ExecutionID:            uuid.New().String(),
		Slug:                   slug,
		RequirementID:          requirementID,
		TraceID:                traceID,
		PlanGoal:               planGoal,
		PlanContext:            planContext,
		RequirementTitle:       reqTitle,
		RequirementDescription: reqDesc,
	}

	baseMsg := message.NewBaseMessage(req.Schema(), req, "plan-manager")
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

	case workflow.ScenariosForRequirementGenerated.Pattern:
		event, err := payloads.ParseReactivePayload[workflow.ScenariosForRequirementGeneratedEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse scenarios-for-requirement event", "error", err)
			return true
		}
		c.handleScenariosForRequirementGenerated(ctx, event)
		return true

	case workflow.ScenariosGenerated.Pattern:
		event, err := payloads.ParseReactivePayload[workflow.ScenariosGeneratedEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse scenarios generated event", "error", err)
			return true
		}
		c.handleScenariosGeneratedEvent(ctx, event)
		return true

	case workflow.GenerationFailed.Pattern:
		event, err := payloads.ParseReactivePayload[workflow.GenerationFailedEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse generation failed event", "error", err)
			return true
		}
		c.handleGenerationFailed(ctx, event)
		return true
	}

	return false
}

// scenariosGeneratedPayloadWrapper satisfies message.Payload for publishing
// the aggregate ScenariosGeneratedEvent from plan-manager.
type scenariosGeneratedPayloadWrapper struct {
	event *workflow.ScenariosGeneratedEvent
}

func (p *scenariosGeneratedPayloadWrapper) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "scenarios-generated", Version: "v1"}
}
func (p *scenariosGeneratedPayloadWrapper) Validate() error  { return nil }
func (p *scenariosGeneratedPayloadWrapper) EntityID() string { return "" }
func (p *scenariosGeneratedPayloadWrapper) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.event)
}
func (p *scenariosGeneratedPayloadWrapper) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, p.event)
}
