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
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// watchPlanStates watches PLAN_STATES for plans transitioning to "approved" and
// triggers requirement generation inline. Runs until ctx is cancelled.
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

		c.logger.Info("KV trigger: plan approved, generating requirements",
			"slug", plan.Slug)

		// Dispatch in a goroutine so the watcher loop is never blocked by LLM
		// calls. The generation function handles its own error logging and
		// publishes failure mutations on error.
		go c.generateFromKVTrigger(ctx, &plan)
	}
}

// generateFromKVTrigger builds a RequirementGeneratorRequest from the KV plan
// value and drives the full generation + publish cycle. This is the same path
// as the JetStream consumer, just entered from the KV watcher instead.
func (c *Component) generateFromKVTrigger(ctx context.Context, plan *workflow.Plan) {
	c.triggersProcessed.Add(1)
	c.updateLastActivity()

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

	llmCtx := c.buildLLMContext(ctx, trigger)

	requirements, err := c.generateRequirements(llmCtx, trigger)
	if err != nil {
		c.generationsFailed.Add(1)
		c.logger.Error("KV-triggered requirement generation failed",
			"slug", plan.Slug, "error", err)

		failReq, _ := json.Marshal(map[string]string{
			"slug":  plan.Slug,
			"phase": "requirements",
			"error": err.Error(),
		})
		if _, rerr := c.natsClient.RequestWithRetry(ctx, "plan.mutation.generation.failed", failReq, 10*time.Second, natsclient.DefaultRetryConfig()); rerr != nil {
			c.logger.Warn("Failed to publish generation failure mutation",
				"slug", plan.Slug, "error", rerr)
		}
		return
	}

	if err := c.publishResults(ctx, trigger, requirements); err != nil {
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to publish KV-triggered requirements",
			"slug", plan.Slug, "error", err)
		return
	}

	c.requirementsGenerated.Add(1)

	c.logger.Info("Requirements generated successfully via KV trigger",
		"slug", plan.Slug,
		"requirement_count", len(requirements))
}
