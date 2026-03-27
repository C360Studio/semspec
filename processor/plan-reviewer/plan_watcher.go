package planreviewer

// plan_watcher.go — KV twofer self-trigger for plan review.
//
// The plan-reviewer watches PLAN_STATES for two state transitions:
//
//   - "drafted" → Round 1: review the plan document itself (goal, context, scope).
//     On approval: send plan.mutation.reviewed + plan.mutation.approved so the
//     plan advances to "approved" and requirement generation kicks off.
//     On rejection: send plan.mutation.generation.failed with reviewer feedback.
//
//   - "scenarios_generated" → Round 2: review requirements + scenarios holistically.
//     On approval: send plan.mutation.ready_for_execution so the plan enters
//     execution. On rejection: send plan.mutation.generation.failed.
//
// The existing JetStream consumer (workflow.async.plan-reviewer) is kept as a
// backward-compatible fallback for any direct dispatches during migration.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// reviewRound labels the two review passes for logging clarity.
type reviewRound int

const (
	roundDraftReview     reviewRound = 1
	roundScenariosReview reviewRound = 2
)

// watchPlanStates watches PLAN_STATES for plan transitions that require a review
// pass. Runs until ctx is cancelled.
//
// js is obtained once in Start() and passed here to avoid a second JetStream()
// call, matching the pattern used by requirement-generator's watchPlanStates.
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

	c.logger.Info("Watching PLAN_STATES for drafted and scenarios_generated plans",
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

		switch plan.Status {
		case workflow.StatusDrafted:
			if plan.Goal == "" {
				c.logger.Debug("Plan drafted but goal not set yet, skipping KV trigger",
					"slug", plan.Slug)
				continue
			}
			// Claim the plan to prevent re-trigger on KV replay or concurrent watchers.
			if !workflow.ClaimPlanStatus(ctx, c.natsClient, plan.Slug, workflow.StatusReviewingDraft, c.logger) {
				continue
			}
			go c.reviewFromKV(ctx, &plan, roundDraftReview)

		case workflow.StatusScenariosGenerated:
			if len(plan.Requirements) == 0 {
				c.logger.Debug("Plan scenarios_generated but no requirements inline, skipping KV trigger",
					"slug", plan.Slug)
				continue
			}
			// Claim the plan to prevent re-trigger on KV replay or concurrent watchers.
			if !workflow.ClaimPlanStatus(ctx, c.natsClient, plan.Slug, workflow.StatusReviewingScenarios, c.logger) {
				continue
			}
			go c.reviewFromKV(ctx, &plan, roundScenariosReview)
		}
	}
}

// reviewFromKV drives the full review + mutation cycle for a plan that
// transitioned via KV write. Uses the same LLM path as the JetStream consumer.
func (c *Component) reviewFromKV(ctx context.Context, plan *workflow.Plan, round reviewRound) {
	c.reviewsProcessed.Add(1)
	c.updateLastActivity()

	// Serialise the plan as the review content so the LLM can reason over all fields.
	planContent, err := json.Marshal(plan)
	if err != nil {
		c.reviewsFailed.Add(1)
		c.logger.Error("KV review: failed to marshal plan",
			"slug", plan.Slug, "round", round, "error", err)
		return
	}

	// Build a minimal PlanReviewRequest — ExecutionID and TaskID are empty so
	// publishResult is a no-op (no orchestrator is waiting for a LoopCompletedEvent).
	trigger := &payloads.PlanReviewRequest{
		RequestID:   fmt.Sprintf("kv-r%d-%s-%d", round, plan.Slug, time.Now().UnixNano()),
		Slug:        plan.Slug,
		PlanContent: json.RawMessage(planContent),
		// SOPContext, TraceID, LoopID, ExecutionID, TaskID intentionally empty.
	}

	result, _, err := c.reviewPlan(ctx, trigger)
	if err != nil {
		c.reviewsFailed.Add(1)
		c.logger.Error("KV review: LLM review failed",
			"slug", plan.Slug, "round", round, "error", err)
		c.sendGenerationFailed(ctx, plan.Slug, round, err.Error())
		return
	}

	c.logger.Info("KV review complete",
		"slug", plan.Slug,
		"round", round,
		"verdict", result.Verdict,
		"summary", result.Summary)

	if result.IsApproved() {
		c.reviewsApproved.Add(1)
		if err := c.sendApprovalMutations(ctx, plan.Slug, result.Summary, round); err != nil {
			c.logger.Warn("KV review: failed to send approval mutations",
				"slug", plan.Slug, "round", round, "error", err)
		}
	} else {
		c.reviewsRejected.Add(1)
		feedback := fmt.Sprintf("Round %d review rejected: %s", round, result.Summary)
		c.sendGenerationFailed(ctx, plan.Slug, round, feedback)
	}
}

// sendApprovalMutations sends the mutation sequence that advances the plan
// after a successful review, depending on which round just passed.
//
// Round 1 (drafted → reviewed → approved): two sequential mutations.
// Round 2 (scenarios_generated → ready_for_execution): one mutation.
func (c *Component) sendApprovalMutations(ctx context.Context, slug string, summary string, round reviewRound) error {
	retryConfig := natsclient.DefaultRetryConfig()
	timeout := 10 * time.Second

	switch round {
	case roundDraftReview:
		// Step 1: mark the plan as reviewed with the reviewer's summary.
		reviewedReq, _ := json.Marshal(map[string]string{
			"slug":    slug,
			"verdict": "approved",
			"summary": summary,
		})
		if _, err := c.natsClient.RequestWithRetry(ctx, "plan.mutation.reviewed", reviewedReq, timeout, retryConfig); err != nil {
			return fmt.Errorf("send reviewed mutation: %w", err)
		}

		// Step 2: approve only when auto_approve is enabled. When disabled,
		// the plan stays at StatusReviewed and waits for human approval
		// via the /promote endpoint.
		if c.config.IsAutoApprove() {
			approvedReq, _ := json.Marshal(map[string]string{"slug": slug})
			if _, err := c.natsClient.RequestWithRetry(ctx, "plan.mutation.approved", approvedReq, timeout, retryConfig); err != nil {
				return fmt.Errorf("send approved mutation: %w", err)
			}
			c.logger.Info("KV review round 1: sent reviewed + approved mutations", "slug", slug)
		} else {
			c.logger.Info("KV review round 1: sent reviewed mutation (auto_approve=false, awaiting human approval)", "slug", slug)
		}

	case roundScenariosReview:
		// Advance directly to ready_for_execution — execution pipeline picks up from here.
		readyReq, _ := json.Marshal(map[string]string{"slug": slug})
		if _, err := c.natsClient.RequestWithRetry(ctx, "plan.mutation.ready_for_execution", readyReq, timeout, retryConfig); err != nil {
			return fmt.Errorf("send ready_for_execution mutation: %w", err)
		}
		c.logger.Info("KV review round 2: sent ready_for_execution mutation", "slug", slug)
	}

	return nil
}

// sendGenerationFailed publishes a generation.failed mutation so the plan-manager
// marks the plan rejected and surfaces the reviewer's feedback.
func (c *Component) sendGenerationFailed(ctx context.Context, slug string, round reviewRound, feedback string) {
	phase := fmt.Sprintf("review-round-%d", round)
	failReq, _ := json.Marshal(map[string]string{
		"slug":  slug,
		"phase": phase,
		"error": feedback,
	})
	if _, err := c.natsClient.RequestWithRetry(ctx, "plan.mutation.generation.failed", failReq,
		10*time.Second, natsclient.DefaultRetryConfig()); err != nil {
		c.logger.Warn("KV review: failed to publish generation.failed mutation",
			"slug", slug, "round", round, "error", err)
	}
}
