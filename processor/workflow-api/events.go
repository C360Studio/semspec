package workflowapi

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/nats-io/nats.go/jetstream"
)

// workflowEvent represents an event published to workflow.events by workflows.
// The plan-review-loop workflow publishes events for plan lifecycle transitions.
type workflowEvent struct {
	Event    string `json:"event"`
	Slug     string `json:"slug"`
	Verdict  string `json:"verdict,omitempty"`
	Summary  string `json:"summary,omitempty"`
	Findings string `json:"findings,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// handleWorkflowEvents subscribes to workflow.events on JetStream and handles
// plan lifecycle events from the plan-review-loop workflow (ADR-005).
//
// Events handled:
//   - plan_approved: marks the plan as approved on disk via Manager.ApprovePlan
//   - plan_revision_needed: logs for observability (workflow handles revision)
//   - plan_review_loop_complete: logs completion
func (c *Component) handleWorkflowEvents(ctx context.Context, js jetstream.JetStream) {
	// Get the WORKFLOW stream
	stream, err := js.Stream(ctx, c.config.EventStreamName)
	if err != nil {
		c.logger.Error("Failed to get workflow events stream, plan auto-approval disabled",
			"stream", c.config.EventStreamName,
			"error", err)
		return
	}

	// Create a durable consumer for workflow events.
	// Durable so we don't miss events if workflow-api restarts.
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name:          "workflow-api-events",
		FilterSubject: "workflow.events",
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverNewPolicy,
	})
	if err != nil {
		c.logger.Error("Failed to create workflow events consumer, plan auto-approval disabled",
			"error", err)
		return
	}

	c.logger.Info("Workflow events subscriber started")

	for {
		select {
		case <-ctx.Done():
			c.logger.Debug("Workflow events subscriber stopping")
			return
		default:
		}

		// Fetch messages with a short timeout so we check ctx.Done regularly
		msgs, err := consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
		if err != nil {
			if ctx.Err() != nil {
				return // Context cancelled, shutting down
			}
			// Transient fetch errors are normal (timeouts, etc.)
			continue
		}

		for msg := range msgs.Messages() {
			c.processWorkflowEvent(ctx, msg)
		}
	}
}

// processWorkflowEvent handles a single workflow event message.
func (c *Component) processWorkflowEvent(ctx context.Context, msg jetstream.Msg) {
	defer func() {
		if err := msg.Ack(); err != nil {
			c.logger.Warn("Failed to ACK workflow event", "error", err)
		}
	}()

	event, err := workflow.ParseNATSMessage[workflowEvent](msg.Data())
	if err != nil {
		c.logger.Warn("Failed to parse workflow event", "error", err)
		return
	}

	switch event.Event {
	case "plan_approved":
		c.handlePlanApprovedEvent(ctx, event)

	case "plan_revision_needed":
		c.logger.Info("Plan revision needed, workflow handling revision",
			"slug", event.Slug,
			"verdict", event.Verdict)

	case "plan_review_loop_complete":
		c.logger.Info("Plan review loop complete",
			"slug", event.Slug)

	default:
		c.logger.Debug("Unhandled workflow event",
			"event", event.Event,
			"slug", event.Slug)
	}
}

// handlePlanApprovedEvent marks a plan as approved on disk when the
// plan-review-loop workflow's verdict_check step determines approval.
func (c *Component) handlePlanApprovedEvent(ctx context.Context, event *workflowEvent) {
	if event.Slug == "" {
		c.logger.Warn("Plan approved event missing slug")
		return
	}

	manager := c.newManager()
	if manager == nil {
		c.logger.Error("Failed to create manager for plan approval",
			"slug", event.Slug)
		return
	}

	plan, err := manager.LoadPlan(ctx, event.Slug)
	if err != nil {
		c.logger.Error("Failed to load plan for approval",
			"slug", event.Slug,
			"error", err)
		return
	}

	// Store review verdict before approving
	if event.Summary != "" {
		plan.ReviewVerdict = "approved"
		plan.ReviewSummary = event.Summary
		now := time.Now()
		plan.ReviewedAt = &now
	}

	if err := manager.ApprovePlan(ctx, plan); err != nil {
		// ErrAlreadyApproved is not an error — idempotent
		if errors.Is(err, workflow.ErrAlreadyApproved) {
			c.logger.Debug("Plan already approved",
				"slug", event.Slug)
			return
		}
		c.logger.Error("Failed to approve plan from workflow event",
			"slug", event.Slug,
			"error", err)
		return
	}

	c.logger.Info("Plan auto-approved by workflow",
		"slug", event.Slug,
		"verdict", event.Verdict,
		"summary", event.Summary)
}

// newManager creates a workflow Manager for filesystem operations.
func (c *Component) newManager() *workflow.Manager {
	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			c.logger.Error("Failed to get working directory", "error", err)
			return nil
		}
	}
	return workflow.NewManager(repoRoot)
}
