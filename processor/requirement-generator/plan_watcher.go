package requirementgenerator

// plan_watcher.go — KV twofer self-trigger for requirement generation.
//
// The requirement-generator watches PLAN_STATES for any plan that transitions
// to "approved" or "changed" status. This means the plan-manager's KV write IS
// the trigger: no separate NATS publish or workflow step is needed to kick off
// generation.
//
// "approved" triggers full requirement generation for a new plan.
// "changed" triggers partial regeneration ��� only deprecated requirements are
// replaced, while active requirements are preserved.

import (
	"context"
	"encoding/json"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/nats-io/nats.go/jetstream"
)

// watchPlanStates watches PLAN_STATES for plans transitioning to "approved" or
// "changed" and dispatches a requirement-generator agent loop. Runs until ctx
// is cancelled.
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

	c.logger.Info("Watching PLAN_STATES for approved/changed plans",
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

		if plan.Status != workflow.StatusApproved && plan.Status != workflow.StatusChanged {
			continue
		}

		// Skip plans without a goal — the mutation handler will catch the
		// approved status again once the goal is filled in.
		if plan.Goal == "" {
			c.logger.Debug("Plan approved/changed but goal not set yet, skipping KV trigger",
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
//
// For "changed" plans with deprecated requirements, it builds a partial regen
// trigger: only deprecated requirements are regenerated, active ones preserved.
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

	// Partial regen: if plan has deprecated requirements, only regenerate those.
	var deprecatedIDs []string
	var activeReqs []workflow.Requirement
	for _, r := range plan.Requirements {
		if r.Status == workflow.RequirementStatusDeprecated {
			deprecatedIDs = append(deprecatedIDs, r.ID)
		} else if r.Status == workflow.RequirementStatusActive {
			activeReqs = append(activeReqs, r)
		}
	}

	// Guard: a "changed" plan with no deprecated requirements is a no-op.
	// This avoids an accidental full regeneration that would discard active requirements.
	// Since the claim already moved the plan to generating_requirements, we must
	// reject it so it doesn't get stuck in that state.
	if plan.Status == workflow.StatusChanged && len(deprecatedIDs) == 0 {
		c.logger.Warn("Plan is in 'changed' status but has no deprecated requirements, rejecting",
			"slug", plan.Slug)
		c.sendGenerationFailed(ctx, plan.Slug, "changed plan has no deprecated requirements")
		return
	}

	if len(deprecatedIDs) > 0 {
		trigger.ReplaceRequirementIDs = deprecatedIDs
		trigger.ExistingRequirements = activeReqs
		trigger.RejectionReasons = findRejectionReasons(plan, deprecatedIDs)
		c.logger.Info("Building partial regen trigger",
			"slug", plan.Slug,
			"deprecated", len(deprecatedIDs),
			"active", len(activeReqs))
	}

	c.dispatchRequirementGenerator(ctx, trigger, "", plan.ReviewFormattedFindings)
}

// findRejectionReasons collects per-requirement rejection reasons from accepted
// PlanDecisions, filtered to only the deprecated requirement IDs. Walks
// proposals in reverse (most recent first) and falls back to the proposal's
// rationale when per-requirement reasons are not set.
func findRejectionReasons(plan *workflow.Plan, deprecatedIDs []string) map[string]string {
	needed := make(map[string]bool, len(deprecatedIDs))
	for _, id := range deprecatedIDs {
		needed[id] = true
	}

	reasons := make(map[string]string, len(deprecatedIDs))

	// Walk proposals in reverse (most recent first).
	for i := len(plan.PlanDecisions) - 1; i >= 0; i-- {
		cp := &plan.PlanDecisions[i]
		if cp.Status != workflow.PlanDecisionStatusAccepted {
			continue
		}
		for _, id := range cp.AffectedReqIDs {
			if !needed[id] || reasons[id] != "" {
				continue
			}
			if r, ok := cp.RejectionReasons[id]; ok && r != "" {
				reasons[id] = r
			} else if cp.Rationale != "" {
				reasons[id] = cp.Rationale
			}
		}
	}

	return reasons
}
