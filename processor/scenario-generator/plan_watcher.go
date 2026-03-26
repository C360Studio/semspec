package scenariogenerator

import (
	"context"
	"encoding/json"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// watchPlanStates watches PLAN_STATES for plans reaching requirements_generated.
// The KV value carries plan.Requirements inline — no follow-up query needed.
func (c *Component) watchPlanStates(ctx context.Context, js jetstream.JetStream) {
	bucket, err := workflow.WaitForKVBucket(ctx, js, c.config.PlanStateBucket)
	if err != nil {
		c.logger.Warn("PLAN_STATES not available, relying on async triggers",
			"bucket", c.config.PlanStateBucket, "error", err)
		return
	}

	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		c.logger.Warn("Failed to watch PLAN_STATES", "error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Watching PLAN_STATES for requirements_generated")

	for entry := range watcher.Updates() {
		if entry == nil {
			continue
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}

		var plan workflow.Plan
		if json.Unmarshal(entry.Value(), &plan) != nil {
			continue
		}
		if plan.Status != workflow.StatusRequirementsGenerated {
			continue
		}
		if len(plan.Requirements) == 0 {
			continue
		}

		c.logger.Info("KV trigger: generating scenarios",
			"slug", plan.Slug,
			"requirement_count", len(plan.Requirements))

		go c.generateScenariosFromKV(ctx, &plan)
	}
}

// generateScenariosFromKV generates scenarios for each requirement in the plan.
// Requirements are inline in the KV value — no additional query needed.
func (c *Component) generateScenariosFromKV(ctx context.Context, plan *workflow.Plan) {
	for _, req := range plan.Requirements {
		trigger := &payloads.ScenarioGeneratorRequest{
			Slug:                   plan.Slug,
			RequirementID:          req.ID,
			PlanGoal:               plan.Goal,
			PlanContext:            plan.Context,
			RequirementTitle:       req.Title,
			RequirementDescription: req.Description,
		}

		scenarios, err := c.generateScenarios(ctx, trigger)
		if err != nil {
			c.logger.Error("KV-triggered scenario generation failed",
				"slug", plan.Slug, "requirement_id", req.ID, "error", err)
			failReq, _ := json.Marshal(map[string]string{
				"slug": plan.Slug, "phase": "scenarios", "error": err.Error(),
			})
			_, _ = c.natsClient.RequestWithRetry(ctx, "plan.mutation.generation.failed",
				failReq, 10*time.Second, natsclient.DefaultRetryConfig())
			continue
		}

		if err := c.publishResults(ctx, trigger, scenarios); err != nil {
			c.logger.Error("Failed to send scenario mutation",
				"slug", plan.Slug, "requirement_id", req.ID, "error", err)
		}
	}
}
