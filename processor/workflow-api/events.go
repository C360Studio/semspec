package workflowapi

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/reactive"
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
	case workflow.PlanApproved.Pattern,
		workflow.PlanRevisionNeeded.Pattern,
		workflow.PlanReviewLoopComplete.Pattern:
		c.dispatchPlanReviewEvent(ctx, msg)

	case workflow.PhasesApproved.Pattern,
		workflow.PhasesRevisionNeeded.Pattern,
		workflow.PhaseReviewLoopComplete.Pattern:
		c.dispatchPhaseReviewEvent(ctx, msg)

	case workflow.TasksApproved.Pattern,
		workflow.TasksRevisionNeeded.Pattern,
		workflow.TaskReviewLoopComplete.Pattern,
		workflow.TaskExecutionComplete.Pattern:
		c.dispatchTaskEvent(ctx, msg)

	default:
		c.logger.Debug("Unhandled workflow event", "subject", msg.Subject())
	}
}

// dispatchPlanReviewEvent routes plan-review domain events to their handlers.
func (c *Component) dispatchPlanReviewEvent(ctx context.Context, msg jetstream.Msg) {
	switch msg.Subject() {
	case workflow.PlanApproved.Pattern:
		event, err := reactive.ParseReactivePayload[workflow.PlanApprovedEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse plan approved event", "error", err)
			return
		}
		c.handlePlanApprovedEvent(ctx, event)

	case workflow.PlanRevisionNeeded.Pattern:
		event, err := reactive.ParseReactivePayload[workflow.PlanRevisionNeededEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse plan revision event", "error", err)
			return
		}
		c.handlePlanRevisionNeededEvent(ctx, event)

	case workflow.PlanReviewLoopComplete.Pattern:
		event, err := reactive.ParseReactivePayload[workflow.PlanReviewLoopCompleteEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse plan review complete event", "error", err)
			return
		}
		c.logger.Info("Plan review loop complete", "slug", event.Slug, "iterations", event.Iterations)
	}
}

// dispatchPhaseReviewEvent routes phase-review domain events to their handlers.
func (c *Component) dispatchPhaseReviewEvent(ctx context.Context, msg jetstream.Msg) {
	switch msg.Subject() {
	case workflow.PhasesApproved.Pattern:
		event, err := reactive.ParseReactivePayload[workflow.PhasesApprovedEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse phases approved event", "error", err)
			return
		}
		c.handlePhasesApprovedEvent(ctx, event)

	case workflow.PhasesRevisionNeeded.Pattern:
		event, err := reactive.ParseReactivePayload[workflow.PhasesRevisionNeededEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse phases revision event", "error", err)
			return
		}
		c.handlePhasesRevisionNeededEvent(ctx, event)

	case workflow.PhaseReviewLoopComplete.Pattern:
		event, err := reactive.ParseReactivePayload[workflow.PhaseReviewLoopCompleteEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse phase review complete event", "error", err)
			return
		}
		c.logger.Info("Phase review loop complete", "slug", event.Slug, "iterations", event.Iterations)
	}
}

// dispatchTaskEvent routes task-review and task-execution domain events to their handlers.
func (c *Component) dispatchTaskEvent(ctx context.Context, msg jetstream.Msg) {
	switch msg.Subject() {
	case workflow.TasksApproved.Pattern:
		event, err := reactive.ParseReactivePayload[workflow.TasksApprovedEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse tasks approved event", "error", err)
			return
		}
		c.handleTasksApprovedEvent(ctx, event)

	case workflow.TasksRevisionNeeded.Pattern:
		event, err := reactive.ParseReactivePayload[workflow.TasksRevisionNeededEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse tasks revision event", "error", err)
			return
		}
		c.handleTasksRevisionNeededEvent(ctx, event)

	case workflow.TaskReviewLoopComplete.Pattern:
		event, err := reactive.ParseReactivePayload[workflow.TaskReviewLoopCompleteEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse task review complete event", "error", err)
			return
		}
		c.logger.Info("Task review loop complete", "slug", event.Slug, "iterations", event.Iterations)

	case workflow.TaskExecutionComplete.Pattern:
		event, err := reactive.ParseReactivePayload[workflow.TaskExecutionCompleteEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse task execution complete event", "error", err)
			return
		}
		c.logger.Info("Task execution complete", "task_id", event.TaskID, "iterations", event.Iterations)
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

	// Persist LLM call history for this iteration
	if len(event.LLMRequestIDs) > 0 {
		if plan.LLMCallHistory == nil {
			plan.LLMCallHistory = &workflow.LLMCallHistory{}
		}
		plan.LLMCallHistory.PlanReview = append(plan.LLMCallHistory.PlanReview, workflow.IterationCalls{
			Iteration:     plan.ReviewIteration + 1, // final approved iteration
			LLMRequestIDs: event.LLMRequestIDs,
			Verdict:       "approved",
		})
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

	// Publish plan entity to graph (best-effort)
	if pubErr := c.publishPlanEntity(ctx, plan); pubErr != nil {
		c.logger.Warn("Failed to publish plan entity", "slug", event.Slug, "error", pubErr)
	}

	// Publish plan approval entity to graph (best-effort)
	planEntityID := workflow.PlanEntityID(event.Slug)
	if pubErr := c.publishApprovalEntity(ctx, "plan", planEntityID, "approved", "workflow", ""); pubErr != nil {
		c.logger.Warn("Failed to publish plan approval entity", "slug", event.Slug, "error", pubErr)
	}

	c.logger.Info("Plan auto-approved by workflow",
		"slug", event.Slug,
		"verdict", event.Verdict,
		"summary", event.Summary)
}

// handleUserSignals subscribes to user.signal.> on the USER JetStream stream
// and handles escalation and error signals from workflow loops.
//
// When a workflow exhausts its retry budget (e.g., plan-review-loop hits
// max_iterations), it publishes to user.signal.escalate. This handler
// transitions the plan to rejected status so the user gets actionable feedback
// instead of a silent dead letter.
func (c *Component) handleUserSignals(ctx context.Context, js jetstream.JetStream) {
	stream, err := js.Stream(ctx, c.config.UserStreamName)
	if err != nil {
		c.logger.Warn("Failed to get USER stream, escalation handling disabled",
			"stream", c.config.UserStreamName,
			"error", err)
		return
	}

	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name:          "workflow-api-user-signals",
		FilterSubject: "user.signal.>",
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverNewPolicy,
	})
	if err != nil {
		c.logger.Warn("Failed to create user signals consumer, escalation handling disabled",
			"error", err)
		return
	}

	c.logger.Info("User signals subscriber started")

	for {
		select {
		case <-ctx.Done():
			c.logger.Debug("User signals subscriber stopping")
			return
		default:
		}

		msgs, err := consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}

		for msg := range msgs.Messages() {
			c.processUserSignal(ctx, msg)
		}
	}
}

// processUserSignal dispatches a user signal by its NATS subject.
func (c *Component) processUserSignal(ctx context.Context, msg jetstream.Msg) {
	defer func() {
		if err := msg.Ack(); err != nil {
			c.logger.Warn("Failed to ACK user signal", "error", err)
		}
	}()

	switch msg.Subject() {
	case workflow.UserEscalation.Pattern:
		event, err := reactive.ParseReactivePayload[workflow.EscalationEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse escalation event", "error", err)
			return
		}
		c.handleEscalationEvent(ctx, event)

	case workflow.UserSignalError.Pattern:
		event, err := reactive.ParseReactivePayload[workflow.UserSignalErrorEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse user signal error event", "error", err)
			return
		}
		c.handleErrorEvent(ctx, event)

	default:
		c.logger.Debug("Unhandled user signal",
			"subject", msg.Subject())
	}
}

// handlePlanRevisionNeededEvent persists the reviewer's findings into the plan
// so the current review state is visible via GET /plans/{slug} at any time.
// Without this, findings only exist in transient NATS messages.
func (c *Component) handlePlanRevisionNeededEvent(ctx context.Context, event *workflow.PlanRevisionNeededEvent) {
	c.logger.Info("Plan revision needed, persisting review findings",
		"slug", event.Slug,
		"iteration", event.Iteration,
		"verdict", event.Verdict)

	if event.Slug == "" {
		c.logger.Warn("Plan revision event missing slug")
		return
	}

	manager := c.newManager()
	if manager == nil {
		c.logger.Error("Failed to create manager for revision handling",
			"slug", event.Slug)
		return
	}

	plan, err := manager.LoadPlan(ctx, event.Slug)
	if err != nil {
		c.logger.Error("Failed to load plan for revision",
			"slug", event.Slug,
			"error", err)
		return
	}

	// Update review state with the latest findings from this iteration.
	plan.ReviewVerdict = event.Verdict
	now := time.Now()
	plan.ReviewedAt = &now
	plan.ReviewIteration = event.Iteration
	if len(event.Findings) > 0 {
		plan.ReviewFindings = event.Findings
	}

	// Persist LLM call history for this revision iteration
	if len(event.LLMRequestIDs) > 0 {
		if plan.LLMCallHistory == nil {
			plan.LLMCallHistory = &workflow.LLMCallHistory{}
		}
		plan.LLMCallHistory.PlanReview = append(plan.LLMCallHistory.PlanReview, workflow.IterationCalls{
			Iteration:     event.Iteration,
			LLMRequestIDs: event.LLMRequestIDs,
			Verdict:       event.Verdict,
		})
	}

	if err := manager.SavePlan(ctx, plan); err != nil {
		c.logger.Error("Failed to save plan with revision findings",
			"slug", event.Slug,
			"error", err)
		return
	}

	// Publish plan entity to graph with updated review state (best-effort)
	if pubErr := c.publishPlanEntity(ctx, plan); pubErr != nil {
		c.logger.Warn("Failed to publish plan entity", "slug", event.Slug, "error", pubErr)
	}
}

// handleTasksApprovedEvent persists task review approval state into the plan so
// the UI can show task_review_verdict="approved" alongside the review findings.
// Previously this was a log-only handler; now it writes to TaskReview* fields.
func (c *Component) handleTasksApprovedEvent(ctx context.Context, event *workflow.TasksApprovedEvent) {
	if event.Slug == "" {
		c.logger.Warn("Tasks approved event missing slug")
		return
	}

	manager := c.newManager()
	if manager == nil {
		c.logger.Error("Failed to create manager for task approval",
			"slug", event.Slug)
		return
	}

	plan, err := manager.LoadPlan(ctx, event.Slug)
	if err != nil {
		c.logger.Error("Failed to load plan for task approval",
			"slug", event.Slug,
			"error", err)
		return
	}

	// Persist task review approval state.
	plan.TaskReviewVerdict = "approved"
	if event.Summary != "" {
		plan.TaskReviewSummary = event.Summary
	}
	now := time.Now()
	plan.TaskReviewedAt = &now

	if len(event.Findings) > 0 {
		plan.TaskReviewFindings = event.Findings
	}
	if event.FormattedFindings != "" {
		plan.TaskReviewFormattedFindings = event.FormattedFindings
	}

	// Also mark tasks as approved (existing fields).
	plan.TasksApproved = true
	plan.TasksApprovedAt = &now
	plan.Status = workflow.StatusTasksApproved

	// Persist LLM call history for this task review iteration
	if len(event.LLMRequestIDs) > 0 {
		if plan.LLMCallHistory == nil {
			plan.LLMCallHistory = &workflow.LLMCallHistory{}
		}
		plan.LLMCallHistory.TaskReview = append(plan.LLMCallHistory.TaskReview, workflow.IterationCalls{
			Iteration:     plan.TaskReviewIteration + 1, // final approved iteration
			LLMRequestIDs: event.LLMRequestIDs,
			Verdict:       "approved",
		})
	}

	if err := manager.SavePlan(ctx, plan); err != nil {
		c.logger.Error("Failed to save plan with task approval",
			"slug", event.Slug,
			"error", err)
		return
	}

	// Publish individual task entities to graph (best-effort)
	tasks, taskErr := manager.LoadTasks(ctx, event.Slug)
	if taskErr == nil {
		for i := range tasks {
			if pubErr := c.publishTaskEntity(ctx, event.Slug, &tasks[i]); pubErr != nil {
				c.logger.Warn("Failed to publish task entity to graph", "task_id", tasks[i].ID, "error", pubErr)
			}
		}
	} else {
		c.logger.Warn("Failed to load tasks for graph publishing", "slug", event.Slug, "error", taskErr)
	}

	// Publish tasks approval entity to graph (best-effort)
	planEntityID := workflow.PlanEntityID(event.Slug)
	if pubErr := c.publishApprovalEntity(ctx, "plan_tasks", planEntityID, "approved", "workflow", ""); pubErr != nil {
		c.logger.Warn("Failed to publish tasks approval entity", "slug", event.Slug, "error", pubErr)
	}

	c.logger.Info("Tasks approved by workflow, persisted to plan",
		"slug", event.Slug,
		"task_count", event.TaskCount,
		"verdict", event.Verdict)
}

// handleTasksRevisionNeededEvent persists the task reviewer's findings into the
// plan so the current task review state is visible via GET /plans/{slug} at any
// time. Mirrors handlePlanRevisionNeededEvent but writes to TaskReview* fields.
func (c *Component) handleTasksRevisionNeededEvent(ctx context.Context, event *workflow.TasksRevisionNeededEvent) {
	c.logger.Info("Task revision needed, persisting task review findings",
		"slug", event.Slug,
		"iteration", event.Iteration,
		"verdict", event.Verdict)

	if event.Slug == "" {
		c.logger.Warn("Task revision event missing slug")
		return
	}

	manager := c.newManager()
	if manager == nil {
		c.logger.Error("Failed to create manager for task revision handling",
			"slug", event.Slug)
		return
	}

	plan, err := manager.LoadPlan(ctx, event.Slug)
	if err != nil {
		c.logger.Error("Failed to load plan for task revision",
			"slug", event.Slug,
			"error", err)
		return
	}

	// Update task review state with the latest findings from this iteration.
	plan.TaskReviewVerdict = event.Verdict
	now := time.Now()
	plan.TaskReviewedAt = &now
	plan.TaskReviewIteration = event.Iteration
	if len(event.Findings) > 0 {
		plan.TaskReviewFindings = event.Findings
	}
	if event.FormattedFindings != "" {
		plan.TaskReviewFormattedFindings = event.FormattedFindings
	}
	if event.Summary != "" {
		plan.TaskReviewSummary = event.Summary
	}

	// Persist LLM call history for this task revision iteration
	if len(event.LLMRequestIDs) > 0 {
		if plan.LLMCallHistory == nil {
			plan.LLMCallHistory = &workflow.LLMCallHistory{}
		}
		plan.LLMCallHistory.TaskReview = append(plan.LLMCallHistory.TaskReview, workflow.IterationCalls{
			Iteration:     event.Iteration,
			LLMRequestIDs: event.LLMRequestIDs,
			Verdict:       event.Verdict,
		})
	}

	if err := manager.SavePlan(ctx, plan); err != nil {
		c.logger.Error("Failed to save plan with task revision findings",
			"slug", event.Slug,
			"error", err)
		return
	}
}

// handlePhasesApprovedEvent persists the phase review approval state and transitions
// the plan to phases_approved status.
func (c *Component) handlePhasesApprovedEvent(ctx context.Context, event *workflow.PhasesApprovedEvent) {
	if event.Slug == "" {
		c.logger.Warn("Phases approved event missing slug")
		return
	}

	manager := c.newManager()
	if manager == nil {
		c.logger.Error("Failed to create manager for phase approval",
			"slug", event.Slug)
		return
	}

	plan, err := manager.LoadPlan(ctx, event.Slug)
	if err != nil {
		c.logger.Error("Failed to load plan for phase approval",
			"slug", event.Slug,
			"error", err)
		return
	}

	// Persist phase review approval state.
	plan.PhaseReviewVerdict = "approved"
	if event.Summary != "" {
		plan.PhaseReviewSummary = event.Summary
	}
	now := time.Now()
	plan.PhaseReviewedAt = &now

	if len(event.Findings) > 0 {
		plan.PhaseReviewFindings = event.Findings
	}

	// Mark phases as approved.
	plan.PhasesApproved = true
	plan.PhasesApprovedAt = &now
	plan.Status = workflow.StatusPhasesApproved

	// Persist LLM call history for this phase review iteration.
	if len(event.LLMRequestIDs) > 0 {
		if plan.LLMCallHistory == nil {
			plan.LLMCallHistory = &workflow.LLMCallHistory{}
		}
		plan.LLMCallHistory.PhaseReview = append(plan.LLMCallHistory.PhaseReview, workflow.IterationCalls{
			Iteration:     plan.PhaseReviewIteration + 1,
			LLMRequestIDs: event.LLMRequestIDs,
			Verdict:       "approved",
		})
	}

	if err := manager.SavePlan(ctx, plan); err != nil {
		c.logger.Error("Failed to save plan with phase approval",
			"slug", event.Slug,
			"error", err)
		return
	}

	// Publish phase entities and plan link to graph (best-effort)
	phases, err := manager.LoadPhases(ctx, event.Slug)
	if err == nil && len(phases) > 0 {
		for i := range phases {
			if pubErr := c.publishPhaseEntity(ctx, event.Slug, &phases[i]); pubErr != nil {
				c.logger.Warn("Failed to publish phase entity to graph", "phase_id", phases[i].ID, "error", pubErr)
			}
		}
		if pubErr := c.publishPlanPhasesLink(ctx, event.Slug, phases); pubErr != nil {
			c.logger.Warn("Failed to publish plan phases link", "slug", event.Slug, "error", pubErr)
		}
	}

	// Publish approval entity for phase plan
	planEntityID := workflow.PlanEntityID(event.Slug)
	if pubErr := c.publishApprovalEntity(ctx, "plan_phases", planEntityID, "approved", "workflow", ""); pubErr != nil {
		c.logger.Warn("Failed to publish phases approval entity", "slug", event.Slug, "error", pubErr)
	}

	c.logger.Info("Phases approved by workflow, persisted to plan",
		"slug", event.Slug,
		"verdict", event.Verdict)
}

// handlePhasesRevisionNeededEvent persists the phase reviewer's findings into the
// plan so the current phase review state is visible via GET /plans/{slug}.
func (c *Component) handlePhasesRevisionNeededEvent(ctx context.Context, event *workflow.PhasesRevisionNeededEvent) {
	c.logger.Info("Phase revision needed, persisting phase review findings",
		"slug", event.Slug,
		"iteration", event.Iteration,
		"verdict", event.Verdict)

	if event.Slug == "" {
		c.logger.Warn("Phase revision event missing slug")
		return
	}

	manager := c.newManager()
	if manager == nil {
		c.logger.Error("Failed to create manager for phase revision handling",
			"slug", event.Slug)
		return
	}

	plan, err := manager.LoadPlan(ctx, event.Slug)
	if err != nil {
		c.logger.Error("Failed to load plan for phase revision",
			"slug", event.Slug,
			"error", err)
		return
	}

	// Update phase review state with the latest findings from this iteration.
	plan.PhaseReviewVerdict = event.Verdict
	now := time.Now()
	plan.PhaseReviewedAt = &now
	plan.PhaseReviewIteration = event.Iteration
	if len(event.Findings) > 0 {
		plan.PhaseReviewFindings = event.Findings
	}
	if event.FormattedFindings != "" {
		plan.PhaseReviewFormattedFindings = event.FormattedFindings
	}
	if event.Summary != "" {
		plan.PhaseReviewSummary = event.Summary
	}

	// Persist LLM call history for this phase revision iteration.
	if len(event.LLMRequestIDs) > 0 {
		if plan.LLMCallHistory == nil {
			plan.LLMCallHistory = &workflow.LLMCallHistory{}
		}
		plan.LLMCallHistory.PhaseReview = append(plan.LLMCallHistory.PhaseReview, workflow.IterationCalls{
			Iteration:     event.Iteration,
			LLMRequestIDs: event.LLMRequestIDs,
			Verdict:       event.Verdict,
		})
	}

	if err := manager.SavePlan(ctx, plan); err != nil {
		c.logger.Error("Failed to save plan with phase revision findings",
			"slug", event.Slug,
			"error", err)
		return
	}
}

// handleEscalationEvent dispatches escalation signals to the appropriate handler
// based on whether it's a task-level or plan-level escalation.
func (c *Component) handleEscalationEvent(ctx context.Context, event *workflow.EscalationEvent) {
	c.logger.Error("Workflow escalation — max retries exhausted, needs human review",
		"slug", event.Slug,
		"task_id", event.TaskID,
		"reason", event.Reason,
		"last_verdict", event.LastVerdict)

	if event.TaskID != "" {
		c.handleTaskEscalation(ctx, event)
		return
	}

	if event.Slug != "" {
		c.handlePlanEscalation(ctx, event)
		return
	}

	c.logger.Warn("Escalation event missing both slug and task_id, cannot persist")
}

// handlePlanEscalation transitions a plan to rejected status when a workflow
// exhausts its retry budget. This ensures the operator sees a terminal state
// with the escalation reason instead of an indefinite "in progress" status.
func (c *Component) handlePlanEscalation(ctx context.Context, event *workflow.EscalationEvent) {
	manager := c.newManager()
	if manager == nil {
		c.logger.Error("Failed to create manager for plan escalation",
			"slug", event.Slug)
		return
	}

	plan, err := manager.LoadPlan(ctx, event.Slug)
	if err != nil {
		c.logger.Error("Failed to load plan for escalation",
			"slug", event.Slug,
			"error", err)
		return
	}

	now := time.Now()

	// Distinguish plan-review vs phase-review vs task-review escalation.
	// Use iteration counters to determine which loop was active when escalation fired.
	if plan.TaskReviewIteration > 0 {
		// Task-review-loop escalation — write to TaskReview* fields.
		plan.TaskReviewVerdict = "escalated"
		plan.TaskReviewSummary = event.Reason
		plan.TaskReviewedAt = &now
		if len(event.LastFindings) > 0 {
			plan.TaskReviewFindings = event.LastFindings
		}
		if event.FormattedFindings != "" {
			plan.TaskReviewFormattedFindings = event.FormattedFindings
		}
		if event.Iteration > 0 {
			plan.TaskReviewIteration = event.Iteration
		}
	} else if plan.PhaseReviewIteration > 0 {
		// Phase-review-loop escalation — write to PhaseReview* fields.
		plan.PhaseReviewVerdict = "escalated"
		plan.PhaseReviewSummary = event.Reason
		plan.PhaseReviewedAt = &now
		if len(event.LastFindings) > 0 {
			plan.PhaseReviewFindings = event.LastFindings
		}
		if event.FormattedFindings != "" {
			plan.PhaseReviewFormattedFindings = event.FormattedFindings
		}
		if event.Iteration > 0 {
			plan.PhaseReviewIteration = event.Iteration
		}
	} else {
		// Plan-review-loop escalation — existing behavior.
		plan.ReviewVerdict = "escalated"
		plan.ReviewSummary = event.Reason
		plan.ReviewedAt = &now
		if len(event.LastFindings) > 0 {
			plan.ReviewFindings = event.LastFindings
		}
		if event.FormattedFindings != "" {
			plan.ReviewFormattedFindings = event.FormattedFindings
		}
		if event.Iteration > 0 {
			plan.ReviewIteration = event.Iteration
		}
	}

	// Transition to rejected — the plan needs human intervention.
	currentStatus := plan.EffectiveStatus()
	if !currentStatus.CanTransitionTo(workflow.StatusRejected) {
		c.logger.Warn("Cannot transition plan to rejected from current status",
			"slug", event.Slug,
			"current_status", currentStatus)
		return
	}

	plan.Status = workflow.StatusRejected
	if err := manager.SavePlan(ctx, plan); err != nil {
		c.logger.Error("Failed to save escalated plan",
			"slug", event.Slug,
			"error", err)
		return
	}

	c.logger.Info("Plan marked as rejected due to escalation",
		"slug", event.Slug,
		"previous_status", currentStatus,
		"reason", event.Reason)
}

// handleTaskEscalation marks an individual task as failed when a task execution
// or review loop exhausts its retry budget. The plan stays in its current state
// so other tasks can continue — only the individual task is affected.
func (c *Component) handleTaskEscalation(ctx context.Context, event *workflow.EscalationEvent) {
	// Resolve slug: prefer event.Slug, fall back to extracting from task entity ID.
	slug := event.Slug
	if slug == "" {
		slug = workflow.ExtractSlugFromTaskID(event.TaskID)
	}
	if slug == "" {
		c.logger.Warn("Task escalation: cannot resolve slug",
			"task_id", event.TaskID)
		return
	}

	manager := c.newManager()
	if manager == nil {
		c.logger.Error("Failed to create manager for task escalation",
			"slug", slug, "task_id", event.TaskID)
		return
	}

	tasks, err := manager.LoadTasks(ctx, slug)
	if err != nil {
		c.logger.Error("Failed to load tasks for escalation",
			"slug", slug, "error", err)
		return
	}

	found := false
	now := time.Now()
	for i := range tasks {
		if tasks[i].ID == event.TaskID {
			tasks[i].Status = workflow.TaskStatusFailed
			tasks[i].EscalationReason = event.Reason
			tasks[i].EscalationFeedback = event.LastFeedback
			tasks[i].EscalationIteration = event.Iteration
			tasks[i].EscalatedAt = &now
			tasks[i].CompletedAt = &now
			found = true
			break
		}
	}

	if !found {
		c.logger.Warn("Task not found for escalation",
			"slug", slug, "task_id", event.TaskID)
		return
	}

	if err := manager.SaveTasks(ctx, tasks, slug); err != nil {
		c.logger.Error("Failed to save tasks after escalation",
			"slug", slug, "error", err)
		return
	}

	c.logger.Info("Task marked as failed due to escalation",
		"slug", slug, "task_id", event.TaskID, "reason", event.Reason)
}

// handleErrorEvent annotates a plan and/or task with the latest error from a
// workflow step failure (LLM call failed, validation error, etc).
// This is annotation only — it does NOT transition any state. The operator can
// see what went wrong, but the workflow may still have retry budget remaining.
func (c *Component) handleErrorEvent(ctx context.Context, event *workflow.UserSignalErrorEvent) {
	c.logger.Error("Workflow step failed",
		"slug", event.Slug,
		"task_id", event.TaskID,
		"error", event.Error)

	manager := c.newManager()
	if manager == nil {
		return
	}

	now := time.Now()

	// Annotate the task if we have a task_id.
	if event.TaskID != "" {
		slug := event.Slug
		if slug == "" {
			slug = workflow.ExtractSlugFromTaskID(event.TaskID)
		}
		if slug != "" {
			tasks, err := manager.LoadTasks(ctx, slug)
			if err != nil {
				c.logger.Warn("Failed to load tasks for error annotation",
					"slug", slug, "error", err)
			} else {
				for i := range tasks {
					if tasks[i].ID == event.TaskID {
						tasks[i].LastError = event.Error
						tasks[i].LastErrorAt = &now
						if err := manager.SaveTasks(ctx, tasks, slug); err != nil {
							c.logger.Warn("Failed to save task error annotation",
								"slug", slug, "error", err)
						}
						break
					}
				}
			}
		}
	}

	// Annotate the plan if we have a slug.
	slug := event.Slug
	if slug == "" && event.TaskID != "" {
		slug = workflow.ExtractSlugFromTaskID(event.TaskID)
	}
	if slug != "" {
		plan, err := manager.LoadPlan(ctx, slug)
		if err != nil {
			c.logger.Warn("Failed to load plan for error annotation",
				"slug", slug, "error", err)
			return
		}
		plan.LastError = event.Error
		plan.LastErrorAt = &now
		if err := manager.SavePlan(ctx, plan); err != nil {
			c.logger.Warn("Failed to save plan error annotation",
				"slug", slug, "error", err)
		}
	}
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
