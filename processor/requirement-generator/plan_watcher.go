package requirementgenerator

// plan_watcher.go — KV twofer self-trigger for requirement generation.
//
// The requirement-generator watches PLAN_STATES for any plan that transitions
// to "approved" status. This means the plan-manager's KV write IS the trigger:
// no separate NATS publish or workflow step is needed to kick off generation.
//
// The existing JetStream consumer (workflow.async.requirement-generator) is kept
// as a backward-compatible fallback for any direct dispatches during migration.

import (
	"context"
	"encoding/json"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/nats-io/nats.go/jetstream"
)

// watchPlanStates watches PLAN_STATES for plans transitioning to "approved" and
// dispatches a requirement-generator agent loop. Runs until ctx is cancelled.
//
// js is obtained once in Start() and passed here to avoid a second JetStream()
// call, matching the pattern used by plan-manager's handleQuestionUpdates.
func (c *Component) watchPlanStates(ctx context.Context, js jetstream.JetStream) {
	bucket, err := workflow.WaitForKVBucket(ctx, js, c.config.PlanStateBucket)
	if err != nil {
		c.logger.Warn("PLAN_STATES bucket not available, will rely on async triggers only",
			"bucket", c.config.PlanStateBucket, "error", err)
		return
	}

	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		c.logger.Warn("Failed to watch PLAN_STATES, will rely on async triggers only",
			"bucket", c.config.PlanStateBucket, "error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Watching PLAN_STATES for approved plans",
		"bucket", c.config.PlanStateBucket)

	for entry := range watcher.Updates() {
		// nil entry signals end of initial values replay — skip silently.
		if entry == nil {
			continue
		}

		// Only react to puts; deletes and purges are irrelevant.
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}

		var plan workflow.Plan
		if err := json.Unmarshal(entry.Value(), &plan); err != nil {
			c.logger.Debug("Skipping unrecognised PLAN_STATES entry",
				"key", entry.Key(), "error", err)
			continue
		}

		if plan.Status != workflow.StatusApproved {
			continue
		}

		// Skip plans without a goal — the mutation handler will catch the
		// approved status again once the goal is filled in.
		if plan.Goal == "" {
			c.logger.Debug("Plan approved but goal not set yet, skipping KV trigger",
				"slug", plan.Slug)
			continue
		}

		// Claim the plan to prevent re-trigger on KV replay or concurrent watchers.
		if !workflow.ClaimPlanStatus(ctx, c.natsClient, plan.Slug, workflow.StatusGeneratingRequirements, c.logger) {
			continue
		}

		// Dispatch in a goroutine so the watcher loop is never blocked by
		// agent dispatch. The dispatch function handles its own error logging.
		go c.generateFromKVTrigger(ctx, &plan)
	}
}

// generateFromKVTrigger builds a RequirementGeneratorRequest from the KV plan
// value and dispatches a requirement-generator agent loop. This is the same path
// as the JetStream consumer, just entered from the KV watcher instead.
func (c *Component) generateFromKVTrigger(ctx context.Context, plan *workflow.Plan) {
	trigger := &payloads.RequirementGeneratorRequest{
		Slug:    plan.Slug,
		Title:   plan.Title,
		Goal:    plan.Goal,
		Context: plan.Context,
	}
	if len(plan.Scope.Include) > 0 || len(plan.Scope.Exclude) > 0 || len(plan.Scope.DoNotTouch) > 0 {
		scope := plan.Scope
		trigger.Scope = &scope
	}

	c.dispatchRequirementGenerator(ctx, trigger, "")
}
