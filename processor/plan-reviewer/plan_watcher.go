package planreviewer

// plan_watcher.go — KV twofer self-trigger for plan review.
//
// The plan-reviewer watches PLAN_STATES for two state transitions:
//
//   - "drafted" → Round 1: review the plan document itself (goal, context, scope).
//     On approval: send plan.mutation.reviewed + plan.mutation.approved so the
//     plan advances to "approved" and requirement generation kicks off.
//     On rejection: send plan.mutation.revision so the plan-manager can retry
//     or escalate based on the iteration cap (ADR-029).
//
//   - "scenarios_generated" → Round 2: review requirements + scenarios holistically.
//     On approval: send plan.mutation.ready_for_execution so the plan enters
//     execution. On rejection: send plan.mutation.revision.
//
// Each review dispatches a reviewer agent via agentic-dispatch. The completion
// is handled by watchLoopCompletions() in component.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/prompts"
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
		if entry == nil {
			continue
		}
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
			if !workflow.ClaimPlanStatus(ctx, c.natsClient, plan.Slug, workflow.StatusReviewingDraft, c.logger) {
				continue
			}
			// Serialise the plan as review content for the agent.
			planContent, err := json.Marshal(plan)
			if err != nil {
				c.logger.Error("Failed to marshal plan for review", "slug", plan.Slug, "error", err)
				continue
			}
			go c.dispatchReviewer(ctx, plan.Slug, string(planContent), roundDraftReview)

		case workflow.StatusScenariosGenerated:
			if len(plan.Requirements) == 0 {
				c.logger.Debug("Plan scenarios_generated but no requirements inline, skipping KV trigger",
					"slug", plan.Slug)
				continue
			}
			if !workflow.ClaimPlanStatus(ctx, c.natsClient, plan.Slug, workflow.StatusReviewingScenarios, c.logger) {
				continue
			}
			planContent, err := json.Marshal(plan)
			if err != nil {
				c.logger.Error("Failed to marshal plan for review", "slug", plan.Slug, "error", err)
				continue
			}
			go c.dispatchReviewer(ctx, plan.Slug, string(planContent), roundScenariosReview)
		}
	}
}

// sendApprovalMutations sends the mutation sequence that advances the plan
// after a successful review, depending on which round just passed.
func (c *Component) sendApprovalMutations(ctx context.Context, slug string, summary string, round reviewRound) error {
	retryConfig := natsclient.DefaultRetryConfig()
	timeout := 10 * time.Second

	switch round {
	case roundDraftReview:
		reviewedReq, _ := json.Marshal(map[string]string{
			"slug":    slug,
			"verdict": "approved",
			"summary": summary,
		})
		if _, err := c.natsClient.RequestWithRetry(ctx, "plan.mutation.reviewed", reviewedReq, timeout, retryConfig); err != nil {
			return fmt.Errorf("send reviewed mutation: %w", err)
		}

		if c.config.IsAutoApprove() {
			approvedReq, _ := json.Marshal(map[string]string{"slug": slug})
			if _, err := c.natsClient.RequestWithRetry(ctx, "plan.mutation.approved", approvedReq, timeout, retryConfig); err != nil {
				return fmt.Errorf("send approved mutation: %w", err)
			}
			c.logger.Info("Review round 1: sent reviewed + approved mutations", "slug", slug)
		} else {
			c.logger.Info("Review round 1: sent reviewed mutation (auto_approve=false, awaiting human approval)", "slug", slug)
		}

	case roundScenariosReview:
		if c.config.IsAutoApprove() {
			readyReq, _ := json.Marshal(map[string]string{"slug": slug})
			if _, err := c.natsClient.RequestWithRetry(ctx, "plan.mutation.ready_for_execution", readyReq, timeout, retryConfig); err != nil {
				return fmt.Errorf("send ready_for_execution mutation: %w", err)
			}
			c.logger.Info("Review round 2: sent ready_for_execution mutation", "slug", slug)
		} else {
			reviewedReq, _ := json.Marshal(map[string]string{"slug": slug, "summary": summary})
			if _, err := c.natsClient.RequestWithRetry(ctx, "plan.mutation.scenarios.reviewed", reviewedReq, timeout, retryConfig); err != nil {
				return fmt.Errorf("send scenarios_reviewed mutation: %w", err)
			}
			c.logger.Info("Review round 2: sent scenarios_reviewed mutation (auto_approve=false, awaiting human approval)", "slug", slug)
		}
	}

	return nil
}

// sendRevisionMutation publishes a plan.mutation.revision request so the plan-manager
// can increment the iteration counter and decide whether to retry or escalate.
// Falls back to sendGenerationFailed if the mutation request fails (plan must not get stuck).
func (c *Component) sendRevisionMutation(ctx context.Context, slug string, round reviewRound, result *prompts.PlanReviewResult) {
	findingsJSON, err := json.Marshal(result.Findings)
	if err != nil {
		c.logger.Error("Failed to marshal review findings for revision mutation",
			"slug", slug, "round", round, "error", err)
		c.sendGenerationFailed(ctx, slug, round, fmt.Sprintf("failed to marshal findings: %v", err))
		return
	}

	revReq, _ := json.Marshal(map[string]any{
		"slug":     slug,
		"round":    int(round),
		"verdict":  result.Verdict,
		"summary":  result.Summary,
		"findings": json.RawMessage(findingsJSON),
	})
	resp, err := c.natsClient.RequestWithRetry(ctx, "plan.mutation.revision", revReq,
		10*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		c.logger.Error("Failed to send revision mutation, falling back to generation.failed",
			"slug", slug, "round", round, "error", err)
		c.sendGenerationFailed(ctx, slug, round, fmt.Sprintf("Round %d review rejected: %s", round, result.Summary))
		return
	}

	var mutResp struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(resp, &mutResp); err != nil || !mutResp.Success {
		c.logger.Error("Revision mutation rejected, falling back to generation.failed",
			"slug", slug, "round", round,
			"resp_error", mutResp.Error, "unmarshal_error", err)
		c.sendGenerationFailed(ctx, slug, round,
			fmt.Sprintf("Round %d revision mutation rejected: %s", round, mutResp.Error))
	}
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
		c.logger.Warn("Failed to publish generation.failed mutation",
			"slug", slug, "round", round, "error", err)
	}
}
