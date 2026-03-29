package scenariogenerator

import (
	"context"
	"encoding/json"

	"github.com/c360studio/semspec/workflow"
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

		// Claim the plan to prevent re-trigger on partial scenario saves.
		if !workflow.ClaimPlanStatus(ctx, c.natsClient, plan.Slug, workflow.StatusGeneratingScenarios, c.logger) {
			continue
		}

		go c.generateScenariosFromKV(ctx, &plan)
	}
}

// generateScenariosFromKV dispatches scenario-generator agent loops for each
// requirement in the plan. Requirements are inline in the KV value — no
// additional query needed.
func (c *Component) generateScenariosFromKV(ctx context.Context, plan *workflow.Plan) {
	for _, req := range plan.Requirements {
		c.dispatchScenarioGenerator(ctx,
			plan.Slug,
			req.ID,
			req.Title,
			req.Description,
			plan.Goal,
			plan.Context,
		)
	}
}
