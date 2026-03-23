package planapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/google/uuid"
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
		Name:          "plan-api-events",
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

	case workflow.TaskExecutionComplete.Pattern:
		c.dispatchTaskEvent(ctx, msg)

	case workflow.ScenarioExecutionComplete.Pattern:
		c.dispatchScenarioEvent(ctx, msg)

	default:
		// ADR-026: Check cascade events before logging as unhandled.
		if !c.dispatchCascadeEvent(ctx, msg) {
			c.logger.Debug("Unhandled workflow event", "subject", msg.Subject())
		}
	}
}

// dispatchPlanReviewEvent routes plan-review domain events to their handlers.
func (c *Component) dispatchPlanReviewEvent(ctx context.Context, msg jetstream.Msg) {
	switch msg.Subject() {
	case workflow.PlanApproved.Pattern:
		event, err := payloads.ParseReactivePayload[workflow.PlanApprovedEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse plan approved event", "error", err)
			return
		}
		c.handlePlanApprovedEvent(ctx, event)

	case workflow.PlanRevisionNeeded.Pattern:
		event, err := payloads.ParseReactivePayload[workflow.PlanRevisionNeededEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse plan revision event", "error", err)
			return
		}
		c.handlePlanRevisionNeededEvent(ctx, event)

	case workflow.PlanReviewLoopComplete.Pattern:
		event, err := payloads.ParseReactivePayload[workflow.PlanReviewLoopCompleteEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse plan review complete event", "error", err)
			return
		}
		c.logger.Info("Plan review loop complete", "slug", event.Slug, "iterations", event.Iterations)
	}
}

// dispatchTaskEvent routes task-execution domain events to their handlers.
func (c *Component) dispatchTaskEvent(ctx context.Context, msg jetstream.Msg) {
	switch msg.Subject() {
	case workflow.TaskExecutionComplete.Pattern:
		event, err := payloads.ParseReactivePayload[workflow.TaskExecutionCompleteEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse task execution complete event", "error", err)
			return
		}
		c.handleTaskExecutionCompleteEvent(ctx, event)
	}
}

// dispatchScenarioEvent routes scenario-execution domain events to their handlers.
func (c *Component) dispatchScenarioEvent(ctx context.Context, msg jetstream.Msg) {
	var event workflow.ScenarioExecutionCompleteEvent
	if err := json.Unmarshal(msg.Data(), &event); err != nil {
		c.logger.Warn("Failed to parse scenario execution complete event", "error", err)
		return
	}
	c.handleScenarioExecutionCompleteEvent(ctx, &event)
}

// workflowSlugRollupReview is the workflow slug written into dispatched rollup
// tasks so that handleRollupCompletions can filter agent.complete.> events.
const workflowSlugRollupReview = "semspec-plan-rollup"

// handleScenarioExecutionCompleteEvent logs the scenario completion, then checks
// whether all scenarios for the plan are now terminal. When all are done, it
// transitions the plan to reviewing_rollup and dispatches the rollup reviewer.
func (c *Component) handleScenarioExecutionCompleteEvent(ctx context.Context, event *workflow.ScenarioExecutionCompleteEvent) {
	c.logger.Info("Scenario execution complete",
		"slug", event.Slug,
		"scenario_id", event.ScenarioID,
		"outcome", event.Outcome,
		"node_count", event.NodeCount,
		"files_modified", len(event.FilesModified),
	)

	manager := c.newManager()
	if manager == nil {
		return
	}

	plan, err := manager.LoadPlan(ctx, event.Slug)
	if err != nil {
		c.logger.Warn("Failed to load plan for scenario completion check",
			"slug", event.Slug, "error", err)
		return
	}

	// Only gate plan rollup from the implementing state.
	if plan.Status != workflow.StatusImplementing {
		return
	}

	scenarios, err := manager.LoadScenarios(ctx, event.Slug)
	if err != nil {
		c.logger.Warn("Failed to load scenarios for completion check",
			"slug", event.Slug, "error", err)
		return
	}

	if len(scenarios) == 0 {
		c.logger.Debug("No scenarios found for plan, skipping rollup check",
			"slug", event.Slug)
		return
	}

	// Scenario.Status tracks BDD verification state (pending/passing/failing/skipped),
	// not execution completion. We count terminal ScenarioExecutionCompleteEvents
	// against the total scenario count. For a reliable count we check the event log
	// via the message-logger (or use an in-memory counter). Because plan-api may
	// restart between events, we use a simpler heuristic: wait until the event that
	// just arrived is for the last untracked scenario.
	//
	// Strategy: query message-logger for distinct scenario_ids that have completed,
	// compare count against total scenarios. If they match, all are done.
	//
	// For now, use the simple approach: if the ScenarioID in this event is the last
	// scenario in the list (chronologically we will receive one event per scenario),
	// count how many unique completions we have seen by inspecting logged messages.
	// Since we cannot reliably count without a persistent counter, we use a
	// "count completed via graph or message-logger" approach after the MVP.
	//
	// MVP: treat the scenario file as the source of truth for total count, and
	// track in-process completions using the rollupTaskIndex (which maps slug →
	// counted events). This is safe within a single process lifetime. On restart,
	// we rely on the scenario statuses on disk (which are updated post-execution).
	//
	// KNOWN LIMITATION: If the process restarts mid-execution, the in-memory
	// counter resets. The rollup will not fire until another ScenarioExecutionComplete
	// event arrives. Plans that complete before a restart require manual intervention
	// to trigger rollup. This is acceptable for the MVP and will be addressed by
	// persisting scenario execution status to disk (ADR follow-up).

	// Use slug-keyed completion counter stored in rollupTaskIndex.
	// We repurpose the map: "counter.<slug>" → completed count (as string, but we
	// store an int64 pointer via atomic to avoid locking).
	// Actually, we store a *[]string of completed scenario IDs.
	counterKey := "completed-scenarios." + event.Slug
	existing, _ := c.rollupTaskIndex.LoadOrStore(counterKey, &[]string{})
	completedIDs := existing.(*[]string)

	// Append this scenario ID if not already tracked.
	alreadyCounted := false
	for _, id := range *completedIDs {
		if id == event.ScenarioID {
			alreadyCounted = true
			break
		}
	}
	if !alreadyCounted {
		updated := append(*completedIDs, event.ScenarioID)
		c.rollupTaskIndex.Store(counterKey, &updated)
		completedIDs = &updated
	}

	c.logger.Debug("Scenario completion tracked",
		"slug", event.Slug,
		"completed", len(*completedIDs),
		"total", len(scenarios),
	)

	if len(*completedIDs) < len(scenarios) {
		return
	}

	// All scenarios have reported completion. Transition to reviewing_rollup.
	c.logger.Info("All scenarios complete, transitioning to rollup review",
		"slug", event.Slug,
		"scenarios", len(scenarios),
	)

	if err := manager.SetPlanStatus(ctx, plan, workflow.StatusReviewingRollup); err != nil {
		c.logger.Error("Failed to set plan status to reviewing_rollup",
			"slug", event.Slug, "error", err)
		return
	}

	// Publish updated plan entity to graph (best-effort).
	if pubErr := c.publishPlanEntity(ctx, plan); pubErr != nil {
		c.logger.Warn("Failed to publish plan entity after rollup transition",
			"slug", event.Slug, "error", pubErr)
	}

	c.dispatchPlanRollupReview(ctx, plan, scenarios, manager)
}

// dispatchPlanRollupReview dispatches the plan-level rollup review through the
// existing plan-reviewer component. It builds a PlanReviewRequest with the
// rollup context (requirements + scenario outcomes) as the plan content.
func (c *Component) dispatchPlanRollupReview(ctx context.Context, plan *workflow.Plan, scenarios []workflow.Scenario, manager *workflow.Manager) {
	requirements, _ := manager.LoadRequirements(ctx, plan.Slug)

	// Build rollup content summarizing requirements and scenario outcomes.
	var rollupContent strings.Builder
	rollupContent.WriteString(fmt.Sprintf("# Plan Rollup Review: %s\n\n", plan.Title))
	rollupContent.WriteString(fmt.Sprintf("**Goal**: %s\n\n", plan.Goal))

	rollupContent.WriteString("## Requirements\n\n")
	for _, r := range requirements {
		rollupContent.WriteString(fmt.Sprintf("- **%s**: %s (status: %s)\n", r.ID, r.Title, r.Status))
	}

	rollupContent.WriteString("\n## Scenario Outcomes\n\n")
	for _, s := range scenarios {
		rollupContent.WriteString(fmt.Sprintf("- **%s**: Given %s, When %s, Then %s (status: %s)\n",
			s.ID, s.Given, s.When, strings.Join(s.Then, "; "), s.Status))
	}

	taskID := fmt.Sprintf("rollup-%s-%s", plan.Slug, uuid.New().String())

	req := &payloads.PlanReviewRequest{
		ExecutionID:  fmt.Sprintf("rollup.%s", plan.Slug),
		TaskID:       taskID,
		WorkflowSlug: workflowSlugRollupReview,
		RequestID:    uuid.New().String(),
		Slug:         plan.Slug,
		TraceID:      latestTraceID(plan),
		PlanContent:  []byte(rollupContent.String()),
	}

	baseMsg := message.NewBaseMessage(req.Schema(), req, "plan-api")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Error("Failed to marshal rollup review request", "slug", plan.Slug, "error", err)
		return
	}

	if c.natsClient == nil {
		c.logger.Warn("Cannot dispatch rollup review: NATS client not configured", "slug", plan.Slug)
		return
	}

	if err := c.natsClient.PublishToStream(ctx, "workflow.async.plan-reviewer", data); err != nil {
		c.logger.Error("Failed to publish rollup review request",
			"slug", plan.Slug, "error", err)
		return
	}

	// Track the task ID so we can route the completion event back.
	c.rollupTaskIndex.Store(taskID, plan.Slug)

	c.logger.Info("Dispatched plan rollup review via plan-reviewer",
		"slug", plan.Slug,
		"task_id", taskID,
		"requirements", len(requirements),
		"scenarios", len(scenarios),
	)
}

// rollupReviewerTools returns the tool names available to the rollup reviewer.
func (c *Component) rollupReviewerTools() []string {
	return []string{"file_read", "file_list", "git_diff", "git_log"}
}

// handleRollupCompletions subscribes to agent.complete.> on JetStream and
// handles completion events for plan rollup review tasks.
func (c *Component) handleRollupCompletions(ctx context.Context, js jetstream.JetStream) {
	agentStream, err := js.Stream(ctx, "AGENT")
	if err != nil {
		c.logger.Warn("Failed to get AGENT stream, rollup completion handling disabled",
			"error", err)
		return
	}

	consumer, err := agentStream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name:          "plan-api-rollup-completions",
		FilterSubject: "agent.complete.>",
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverNewPolicy,
	})
	if err != nil {
		c.logger.Warn("Failed to create rollup completions consumer, rollup handling disabled",
			"error", err)
		return
	}

	c.logger.Info("Rollup completion subscriber started")

	for {
		select {
		case <-ctx.Done():
			c.logger.Debug("Rollup completion subscriber stopping")
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
			c.processRollupCompletionMsg(ctx, msg)
		}
	}
}

// processRollupCompletionMsg processes a single agent.complete.> message,
// filtering for rollup review completions and routing them to the handler.
func (c *Component) processRollupCompletionMsg(ctx context.Context, msg jetstream.Msg) {
	defer func() {
		if err := msg.Ack(); err != nil {
			c.logger.Warn("Failed to ACK rollup completion", "error", err)
		}
	}()

	var base message.BaseMessage
	if err := json.Unmarshal(msg.Data(), &base); err != nil {
		c.logger.Debug("Failed to unmarshal rollup completion envelope", "error", err)
		return
	}

	event, ok := base.Payload().(*agentic.LoopCompletedEvent)
	if !ok {
		return // Not a LoopCompletedEvent; ignore.
	}

	if event.WorkflowSlug != workflowSlugRollupReview {
		return // Not a rollup review completion; ignore.
	}

	slugVal, ok := c.rollupTaskIndex.Load(event.TaskID)
	if !ok {
		c.logger.Debug("Rollup completion for unknown task ID",
			"task_id", event.TaskID,
			"workflow_slug", event.WorkflowSlug)
		return
	}
	slug := slugVal.(string)

	c.handlePlanRollupCompleteEvent(ctx, slug, event)
}

// handlePlanRollupCompleteEvent processes the rollup review result and
// transitions the plan to complete (or leaves it in reviewing_rollup for
// human attention when the verdict is "needs_attention").
func (c *Component) handlePlanRollupCompleteEvent(ctx context.Context, slug string, event *agentic.LoopCompletedEvent) {
	manager := c.newManager()
	if manager == nil {
		c.logger.Error("Failed to create manager for rollup completion", "slug", slug)
		return
	}

	plan, err := manager.LoadPlan(ctx, slug)
	if err != nil {
		c.logger.Error("Failed to load plan for rollup completion", "slug", slug, "error", err)
		return
	}

	if plan.Status != workflow.StatusReviewingRollup {
		c.logger.Debug("Plan not in reviewing_rollup state, ignoring rollup completion",
			"slug", slug, "status", plan.Status)
		return
	}

	// Parse the rollup verdict from the LLM result.
	var result struct {
		Verdict        string   `json:"verdict"`
		Summary        string   `json:"summary"`
		AttentionItems []string `json:"attention_items"`
		Confidence     float64  `json:"confidence"`
	}
	if event.Result != "" {
		_ = json.Unmarshal([]byte(event.Result), &result)
	}

	if result.Verdict == "approved" || result.Verdict == "" {
		// Store the rollup summary in the plan's ReviewSummary field.
		if result.Summary != "" {
			plan.ReviewSummary = result.Summary
		}
		if err := manager.SetPlanStatus(ctx, plan, workflow.StatusComplete); err != nil {
			c.logger.Error("Failed to complete plan after rollup approval",
				"slug", slug, "error", err)
			return
		}

		// Publish updated plan entity to graph (best-effort).
		if pubErr := c.publishPlanEntity(ctx, plan); pubErr != nil {
			c.logger.Warn("Failed to publish completed plan entity",
				"slug", slug, "error", pubErr)
		}

		c.logger.Info("Plan rollup approved, plan complete",
			"slug", slug,
			"summary_length", len(result.Summary),
			"confidence", result.Confidence,
		)
	} else {
		// "needs_attention" — leave plan in reviewing_rollup so a human can act.
		c.logger.Warn("Plan rollup needs attention",
			"slug", slug,
			"verdict", result.Verdict,
			"attention_items", len(result.AttentionItems),
		)
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

	// Persist complete LLM call history from the accumulated iteration history.
	// The approved event carries ALL iterations (rejections + final approval),
	// built from KV state. This avoids a race condition where the planner's
	// concurrent plan.json save could overwrite intermediate revision entries.
	if len(event.IterationHistory) > 0 {
		if plan.LLMCallHistory == nil {
			plan.LLMCallHistory = &workflow.LLMCallHistory{}
		}
		plan.LLMCallHistory.PlanReview = event.IterationHistory
	} else if len(event.LLMRequestIDs) > 0 {
		// Fallback for events without accumulated history (e.g., first-pass approval
		// with no rejections, or older event format).
		if plan.LLMCallHistory == nil {
			plan.LLMCallHistory = &workflow.LLMCallHistory{}
		}
		plan.LLMCallHistory.PlanReview = append(plan.LLMCallHistory.PlanReview, workflow.IterationCalls{
			Iteration:     plan.ReviewIteration + 1,
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

	// Requirement/scenario generation is orchestrated by plan-coordinator,
	// not plan-api. The coordinator dispatches generators on review approval.
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
		Name:          "plan-api-user-signals",
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
		event, err := payloads.ParseReactivePayload[workflow.EscalationEvent](msg.Data())
		if err != nil {
			c.logger.Warn("Failed to parse escalation event", "error", err)
			return
		}
		c.handleEscalationEvent(ctx, event)

	case workflow.UserSignalError.Pattern:
		event, err := payloads.ParseReactivePayload[workflow.UserSignalErrorEvent](msg.Data())
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

// handleTaskExecutionCompleteEvent updates a task's status and checks whether all
// tasks in the plan are now terminal. If so, transitions the plan to StatusComplete.
func (c *Component) handleTaskExecutionCompleteEvent(ctx context.Context, event *workflow.TaskExecutionCompleteEvent) {
	slug := workflow.ExtractSlugFromTaskID(event.TaskID)
	if slug == "" {
		c.logger.Info("Task execution complete (no plan slug)", "task_id", event.TaskID)
		return
	}

	manager := c.newManager()
	if manager == nil {
		c.logger.Error("Failed to create manager for task execution complete", "task_id", event.TaskID)
		return
	}

	plan, err := manager.LoadPlan(ctx, slug)
	if err != nil {
		c.logger.Warn("Failed to load plan for task completion check",
			"slug", slug, "task_id", event.TaskID, "error", err)
		return
	}

	// Only check for completion if the plan is currently implementing.
	if plan.Status != workflow.StatusImplementing {
		c.logger.Info("Task execution complete",
			"task_id", event.TaskID, "plan_status", plan.Status)
		return
	}

	tasks, err := manager.LoadTasks(ctx, slug)
	if err != nil {
		c.logger.Warn("Failed to load tasks for completion check",
			"slug", slug, "error", err)
		return
	}

	allDone := true
	for _, t := range tasks {
		if t.Status != workflow.TaskStatusCompleted && t.Status != workflow.TaskStatusFailed {
			allDone = false
			break
		}
	}

	if !allDone {
		c.logger.Info("Task execution complete, plan still in progress",
			"task_id", event.TaskID, "slug", slug)
		return
	}

	if err := manager.SetPlanStatus(ctx, plan, workflow.StatusComplete); err != nil {
		c.logger.Error("Failed to transition plan to complete",
			"slug", slug, "error", err)
		return
	}

	c.logger.Info("All tasks done, plan marked complete", "slug", slug)

	// Publish updated plan entity to graph (best-effort)
	if pubErr := c.publishPlanEntity(ctx, plan); pubErr != nil {
		c.logger.Warn("Failed to publish completed plan entity", "slug", slug, "error", pubErr)
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

	// Plan-review-loop escalation.
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
