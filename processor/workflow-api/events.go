package workflowapi

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/nats-io/nats.go/jetstream"
)

// handleWorkflowEvents subscribes to workflow.events.> on JetStream and handles
// plan/task lifecycle events from semspec workflows (ADR-005, ADR-020).
//
// Events are dispatched by NATS subject rather than a payload "event" field,
// matching the typed subject split in workflow/subjects.go.
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
	// Uses wildcard to capture all per-event-type subjects under workflow.events.>
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name:          "workflow-api-events",
		FilterSubject: "workflow.events.>",
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

// processWorkflowEvent dispatches a workflow event by its NATS subject.
// Each event type publishes to a dedicated subject under workflow.events.<domain>.<action>.
func (c *Component) processWorkflowEvent(ctx context.Context, msg jetstream.Msg) {
	defer func() {
		if err := msg.Ack(); err != nil {
			c.logger.Warn("Failed to ACK workflow event", "error", err)
		}
	}()

	switch msg.Subject() {
	// Plan review events
	case workflow.PlanApproved.Pattern:
		event, err := workflow.ParseNATSMessage[workflow.PlanApprovedEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse plan approved event", "error", err)
			return
		}
		c.handlePlanApprovedEvent(ctx, event)

	case workflow.PlanRevisionNeeded.Pattern:
		event, err := workflow.ParseNATSMessage[workflow.PlanRevisionNeededEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse plan revision event", "error", err)
			return
		}
		c.logger.Info("Plan revision needed, workflow handling revision",
			"slug", event.Slug,
			"verdict", event.Verdict)

	case workflow.PlanReviewLoopComplete.Pattern:
		event, err := workflow.ParseNATSMessage[workflow.PlanReviewLoopCompleteEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse plan review complete event", "error", err)
			return
		}
		c.logger.Info("Plan review loop complete",
			"slug", event.Slug,
			"iterations", event.Iterations)

	// Task review events
	case workflow.TasksApproved.Pattern:
		event, err := workflow.ParseNATSMessage[workflow.TasksApprovedEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse tasks approved event", "error", err)
			return
		}
		c.logger.Info("Tasks approved by workflow",
			"slug", event.Slug,
			"task_count", event.TaskCount)

	case workflow.TaskReviewLoopComplete.Pattern:
		event, err := workflow.ParseNATSMessage[workflow.TaskReviewLoopCompleteEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse task review complete event", "error", err)
			return
		}
		c.logger.Info("Task review loop complete",
			"slug", event.Slug,
			"iterations", event.Iterations)

	// Task execution events
	case workflow.TaskExecutionComplete.Pattern:
		event, err := workflow.ParseNATSMessage[workflow.TaskExecutionCompleteEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse task execution complete event", "error", err)
			return
		}
		c.logger.Info("Task execution complete",
			"task_id", event.TaskID,
			"iterations", event.Iterations)

	default:
		c.logger.Debug("Unhandled workflow event",
			"subject", msg.Subject())
	}
}

// handlePlanApprovedEvent marks a plan as approved on disk when the
// plan-review-loop workflow's verdict_check step determines approval.
func (c *Component) handlePlanApprovedEvent(ctx context.Context, event *workflow.PlanApprovedEvent) {
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
