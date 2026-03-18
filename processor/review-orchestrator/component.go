// Package revieworchestrator provides a component that orchestrates plan, phase,
// and task review loops. It replaces the three separate reactive review loops
// (plan-review-loop, phase-review-loop, task-review-loop) with a single
// component that:
//
//  1. Subscribes to trigger subjects for each review type.
//  2. Writes execution state as entity triples to the graph via graph.mutation.triple.add.
//  3. Dispatches generator and reviewer agents by publishing TaskMessage payloads.
//  4. Handles loop-completion events to advance the review lifecycle.
//  5. Increments iterations on rejection or escalates when the budget is exhausted.
//
// State lives as entity triples in ENTITY_STATES. No typed Go structs are
// stored in KV — the component keeps lightweight in-memory tracking (sync.Map)
// for routing completion events back to the correct handler.
//
// Terminal status transitions (approved→completed, escalated→escalated,
// error→failed) are owned by the JSON rule processor, NOT by this component.
// This component writes only workflow.phase; the rules react to phase changes
// and set workflow.status + publish events. This avoids dual-write races.
//
// Entity ID constraint: slugs must NOT contain dots, because the NATS wildcard
// pattern local.semspec.workflow.*-review.execution.* treats each dot-separated
// token as a subject level. A slug with dots would break rule matching.
package revieworchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	wf "github.com/c360studio/semspec/vocabulary/workflow"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

const (
	componentName    = "review-orchestrator"
	componentVersion = "0.1.0"

	// WorkflowSlugPlanReview identifies the plan review workflow in agent TaskMessages.
	WorkflowSlugPlanReview = "semspec-plan-review"
	// WorkflowSlugPhaseReview identifies the phase review workflow in agent TaskMessages.
	WorkflowSlugPhaseReview = "semspec-phase-review"
	// WorkflowSlugTaskReview identifies the task review workflow in agent TaskMessages.
	WorkflowSlugTaskReview = "semspec-task-review"

	// WorkflowStep constants identify which stage of the review loop emitted an event.
	workflowStepGenerate = "generate"
	workflowStepReview   = "review"

	// reviewType constants for the three review kinds managed by this component.
	reviewTypePlanReview  = "plan-review"
	reviewTypePhaseReview = "phase-review"
	reviewTypeTaskReview  = "task-review"

	// Trigger subjects for each review loop.
	subjectPlanReviewTrigger  = "workflow.trigger.plan-review-loop"
	subjectPhaseReviewTrigger = "workflow.trigger.phase-review-loop"
	subjectTaskReviewTrigger  = "workflow.trigger.task-review-loop"

	// subjectLoopCompleted is the JetStream subject for agentic loop completion events.
	subjectLoopCompleted = "agentic.loop_completed.v1"

	// Downstream dispatch subjects.
	subjectPlannerAsync        = "workflow.async.planner"
	subjectPhaseGeneratorAsync = "workflow.async.phase-generator"
	subjectTaskGeneratorAsync  = "workflow.async.task-generator"
	subjectPlanReviewerAsync   = "workflow.async.plan-reviewer"
	subjectTaskReviewerAsync   = "workflow.async.task-reviewer"
)

// Component orchestrates plan, phase, and task review loops.
type Component struct {
	config       Config
	natsClient   *natsclient.Client
	logger       *slog.Logger
	platform     component.PlatformMeta
	tripleWriter *graphutil.TripleWriter

	inputPorts  []component.Port
	outputPorts []component.Port

	// activeReviews maps entityID → *reviewExecution.
	// Also indexed by generatorTaskID and reviewerTaskID for O(1) lookup on
	// loop-completion events (see taskIDIndex).
	activeReviews sync.Map

	// taskIDIndex maps TaskID (used in dispatched TaskMessages) → entityID.
	// This allows handleLoopCompleted to find the correct reviewExecution
	// without iterating the entire activeReviews map.
	taskIDIndex sync.Map

	// Lifecycle
	shutdown      chan struct{}
	wg            sync.WaitGroup
	running       bool
	mu            sync.RWMutex
	lifecycleMu   sync.Mutex
	subscriptions []*natsclient.Subscription

	// Metrics
	triggersProcessed atomic.Int64
	reviewsCompleted  atomic.Int64
	reviewsEscalated  atomic.Int64
	reviewsApproved   atomic.Int64
	errors            atomic.Int64
	lastActivityMu    sync.RWMutex
	lastActivity      time.Time
}

// NewComponent creates a new review-orchestrator from raw JSON config.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var cfg Config
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal review-orchestrator config: %w", err)
	}
	cfg = cfg.withDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("component", componentName)

	c := &Component{
		config:     cfg,
		natsClient: deps.NATSClient,
		logger:     logger,
		platform:   deps.Platform,
		shutdown:   make(chan struct{}),
		tripleWriter: &graphutil.TripleWriter{
			NATSClient:    deps.NATSClient,
			Logger:        logger,
			ComponentName: componentName,
		},
	}

	// Build ports from config.
	for _, p := range cfg.Ports.Inputs {
		c.inputPorts = append(c.inputPorts, component.BuildPortFromDefinition(
			component.PortDefinition{Name: p.Name, Subject: p.Subject, Type: p.Type, StreamName: p.StreamName},
			component.DirectionInput,
		))
	}
	for _, p := range cfg.Ports.Outputs {
		c.outputPorts = append(c.outputPorts, component.BuildPortFromDefinition(
			component.PortDefinition{Name: p.Name, Subject: p.Subject, Type: p.Type, StreamName: p.StreamName},
			component.DirectionOutput,
		))
	}

	return c, nil
}

// Initialize prepares the component. No-op for this component.
func (c *Component) Initialize() error {
	return nil
}

// Start begins consuming trigger events and loop-completion events.
func (c *Component) Start(ctx context.Context) error {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()

	c.mu.RLock()
	if c.running {
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()

	c.logger.Info("Starting review-orchestrator")

	triggerHandler := func(reviewType string) func(context.Context, *nats.Msg) {
		return func(ctx context.Context, msg *nats.Msg) {
			c.wg.Add(1)
			defer c.wg.Done()
			select {
			case <-c.shutdown:
				return
			default:
			}
			c.handleTrigger(ctx, msg, reviewType)
		}
	}

	completionHandler := func(ctx context.Context, msg *nats.Msg) {
		c.wg.Add(1)
		defer c.wg.Done()
		select {
		case <-c.shutdown:
			return
		default:
		}
		c.handleLoopCompleted(ctx, msg)
	}

	for _, port := range c.inputPorts {
		subject := graphutil.PortSubject(port)
		if subject == "" {
			continue
		}

		var handler func(context.Context, *nats.Msg)
		switch subject {
		case subjectPlanReviewTrigger:
			handler = triggerHandler(reviewTypePlanReview)
		case subjectPhaseReviewTrigger:
			handler = triggerHandler(reviewTypePhaseReview)
		case subjectTaskReviewTrigger:
			handler = triggerHandler(reviewTypeTaskReview)
		case subjectLoopCompleted:
			handler = completionHandler
		default:
			c.logger.Debug("Skipping unrecognized input port", "subject", subject)
			continue
		}

		sub, err := c.natsClient.Subscribe(ctx, subject, handler)
		if err != nil {
			return fmt.Errorf("subscribe to %s: %w", subject, err)
		}
		c.subscriptions = append(c.subscriptions, sub)
		c.logger.Debug("Subscribed", "subject", subject)
	}

	c.mu.Lock()
	c.running = true
	c.mu.Unlock()

	return nil
}

// Stop performs graceful shutdown: signals in-flight handlers to stop, waits for
// them to drain (up to the given timeout), then unsubscribes.
func (c *Component) Stop(timeout time.Duration) error {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()

	c.mu.RLock()
	if !c.running {
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()

	c.logger.Info("Stopping review-orchestrator",
		"triggers_processed", c.triggersProcessed.Load(),
		"reviews_approved", c.reviewsApproved.Load(),
		"reviews_escalated", c.reviewsEscalated.Load(),
	)

	// Signal in-flight handlers to stop.
	close(c.shutdown)

	// Wait for in-flight handlers to finish, with timeout.
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	select {
	case <-done:
		c.logger.Debug("All in-flight handlers drained")
	case <-time.After(timeout):
		c.logger.Warn("Timed out waiting for in-flight handlers to drain")
	}

	// Cancel any active execution timeouts.
	c.activeReviews.Range(func(_, value any) bool {
		exec := value.(*reviewExecution)
		exec.mu.Lock()
		if exec.timeoutTimer != nil {
			exec.timeoutTimer.stop()
		}
		exec.mu.Unlock()
		return true
	})

	for _, sub := range c.subscriptions {
		if err := sub.Unsubscribe(); err != nil {
			c.logger.Debug("Unsubscribe error", "error", err)
		}
	}
	c.subscriptions = nil

	c.mu.Lock()
	c.running = false
	c.mu.Unlock()

	return nil
}

// ---------------------------------------------------------------------------
// Trigger handler
// ---------------------------------------------------------------------------

// handleTrigger parses a review loop trigger, writes initial entity triples,
// and dispatches the first generator agent.
func (c *Component) handleTrigger(ctx context.Context, msg *nats.Msg, reviewType string) {
	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	// Parse the trigger payload. The reactive engine wraps it in a BaseMessage.
	trigger, err := payloads.ParseReactivePayload[workflow.TriggerPayload](msg.Data)
	if err != nil {
		c.logger.Error("Failed to parse trigger", "review_type", reviewType, "error", err)
		c.errors.Add(1)
		return
	}

	if trigger.Slug == "" {
		c.logger.Error("Trigger missing slug", "review_type", reviewType)
		c.errors.Add(1)
		return
	}

	executionID := fmt.Sprintf("local.semspec.workflow.%s.execution.%s", reviewType, trigger.Slug)

	c.logger.Info("Review trigger received",
		"review_type", reviewType,
		"slug", trigger.Slug,
		"entity_id", executionID,
		"trace_id", trigger.TraceID,
	)

	// Initialise in-memory execution state.
	exec := &reviewExecution{
		EntityID:      executionID,
		ReviewType:    reviewType,
		Slug:          trigger.Slug,
		Iteration:     0,
		MaxIterations: c.config.MaxIterations,
		Title:         trigger.Title,
		Description:   trigger.Description,
		ProjectID:     trigger.ProjectID,
		Prompt:        trigger.Prompt,
		ScopePatterns: trigger.ScopePatterns,
		TraceID:       trigger.TraceID,
		LoopID:        trigger.LoopID,
		RequestID:     trigger.RequestID,
		Auto:          trigger.Auto,
	}

	// Deduplicate: if an execution for this entityID already exists, skip.
	if _, loaded := c.activeReviews.LoadOrStore(executionID, exec); loaded {
		c.logger.Debug("Duplicate trigger for active review, skipping",
			"entity_id", executionID,
			"review_type", reviewType,
		)
		return
	}

	// Write initial entity triples.
	// NOTE: workflow.status is NOT written here — the rule processor owns status
	// transitions. This component writes only workflow.phase.
	_ = c.tripleWriter.WriteTriple(ctx, executionID, wf.Type, reviewType)
	if err := c.tripleWriter.WriteTriple(ctx, executionID, wf.Phase, "generating"); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", "generating", "error", err)
	}
	_ = c.tripleWriter.WriteTriple(ctx, executionID, wf.Slug, trigger.Slug)
	_ = c.tripleWriter.WriteTriple(ctx, executionID, wf.Title, trigger.Title)
	_ = c.tripleWriter.WriteTriple(ctx, executionID, wf.Description, trigger.Description)
	_ = c.tripleWriter.WriteTriple(ctx, executionID, wf.ProjectID, trigger.ProjectID)
	_ = c.tripleWriter.WriteTriple(ctx, executionID, wf.Iteration, 0)
	_ = c.tripleWriter.WriteTriple(ctx, executionID, wf.MaxIterations, c.config.MaxIterations)
	_ = c.tripleWriter.WriteTriple(ctx, executionID, wf.ExecutionID, executionID)
	_ = c.tripleWriter.WriteTriple(ctx, executionID, wf.TraceID, trigger.TraceID)
	if trigger.Prompt != "" {
		_ = c.tripleWriter.WriteTriple(ctx, executionID, wf.Prompt, trigger.Prompt)
	}

	// Publish initial entity snapshot for graph observability.
	c.publishEntity(ctx, NewReviewExecutionEntity(exec).WithPhase("generating"))

	// Lock before timeout and dispatch to prevent race where timeout fires
	// before we finish initializing the execution.
	exec.mu.Lock()
	defer exec.mu.Unlock()
	c.startExecutionTimeout(exec)
	c.dispatchGeneratorLocked(ctx, exec)
}

// ---------------------------------------------------------------------------
// Loop-completion handler
// ---------------------------------------------------------------------------

// handleLoopCompleted routes an agentic loop completion event to the
// appropriate sub-handler based on WorkflowSlug and WorkflowStep.
func (c *Component) handleLoopCompleted(ctx context.Context, msg *nats.Msg) {
	var base message.BaseMessage
	if err := json.Unmarshal(msg.Data, &base); err != nil {
		c.logger.Debug("Failed to unmarshal loop completed envelope", "error", err)
		c.errors.Add(1)
		return
	}

	event, ok := base.Payload().(*agentic.LoopCompletedEvent)
	if !ok {
		// Not a LoopCompletedEvent — ignore.
		return
	}

	// Filter to events belonging to this component's review workflows.
	switch event.WorkflowSlug {
	case WorkflowSlugPlanReview, WorkflowSlugPhaseReview, WorkflowSlugTaskReview:
		// handled below
	default:
		return
	}

	c.updateLastActivity()

	// Resolve the entityID from the TaskID using the secondary index.
	entityIDVal, ok := c.taskIDIndex.Load(event.TaskID)
	if !ok {
		c.logger.Debug("Loop completed for unknown task ID, may have already been handled",
			"task_id", event.TaskID,
			"workflow_slug", event.WorkflowSlug,
			"workflow_step", event.WorkflowStep,
		)
		return
	}
	entityID := entityIDVal.(string)

	execVal, ok := c.activeReviews.Load(entityID)
	if !ok {
		c.logger.Debug("No active review for entity", "entity_id", entityID)
		return
	}
	exec := execVal.(*reviewExecution)

	// Lock the execution for the duration of the handler.
	exec.mu.Lock()
	defer exec.mu.Unlock()

	if exec.terminated {
		return
	}

	c.logger.Info("Loop completion received",
		"review_type", exec.ReviewType,
		"slug", exec.Slug,
		"workflow_step", event.WorkflowStep,
		"iteration", exec.Iteration,
	)

	switch event.WorkflowStep {
	case workflowStepGenerate:
		c.handleGeneratorCompleteLocked(ctx, event, exec)
	case workflowStepReview:
		c.handleReviewerCompleteLocked(ctx, event, exec)
	default:
		c.logger.Debug("Unknown workflow step",
			"step", event.WorkflowStep,
			"entity_id", entityID,
		)
	}
}

// ---------------------------------------------------------------------------
// Generator-complete handler
// ---------------------------------------------------------------------------

// handleGeneratorCompleteLocked processes a generator agent completion event.
// It parses the generator output, writes content triples, and dispatches
// the reviewer agent.
//
// Caller must hold exec.mu.
func (c *Component) handleGeneratorCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *reviewExecution) {
	// Remove the generator task from the index — it has served its purpose.
	c.taskIDIndex.Delete(exec.GeneratorTaskID)

	// Parse the generator result from the loop's Result field.
	content, llmRequestIDs := c.parseGeneratorResult(event.Result, exec)

	// Update in-memory execution state with generator output.
	exec.PlanContent = content
	exec.LLMRequestIDs = llmRequestIDs

	// Write content triples.
	if len(content) > 0 {
		_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.PlanContent, string(content))
	}
	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, "planned"); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", "planned", "error", err)
	}

	// Dispatch reviewer.
	c.dispatchReviewerLocked(ctx, exec)
}

// parseGeneratorResult extracts content and LLM request IDs from the raw result
// string, dispatching to the correct payload type based on review type.
func (c *Component) parseGeneratorResult(result string, exec *reviewExecution) (json.RawMessage, []string) {
	switch exec.ReviewType {
	case reviewTypePlanReview:
		var r payloads.PlannerResult
		if err := json.Unmarshal([]byte(result), &r); err != nil {
			c.logger.Warn("Failed to parse planner result, using raw", "slug", exec.Slug, "error", err)
			return json.RawMessage(result), nil
		}
		return r.Content, r.LLMRequestIDs

	case reviewTypePhaseReview:
		var r payloads.PhaseGeneratorResult
		if err := json.Unmarshal([]byte(result), &r); err != nil {
			c.logger.Warn("Failed to parse phase-generator result, using raw", "slug", exec.Slug, "error", err)
			return json.RawMessage(result), nil
		}
		return r.Phases, r.LLMRequestIDs

	case reviewTypeTaskReview:
		var r payloads.TaskGeneratorResult
		if err := json.Unmarshal([]byte(result), &r); err != nil {
			c.logger.Warn("Failed to parse task-generator result, using raw", "slug", exec.Slug, "error", err)
			return json.RawMessage(result), nil
		}
		return r.Tasks, r.LLMRequestIDs

	default:
		return json.RawMessage(result), nil
	}
}

// ---------------------------------------------------------------------------
// Reviewer-complete handler
// ---------------------------------------------------------------------------

// handleReviewerCompleteLocked processes a reviewer agent completion event.
// On approval it marks the execution complete. On rejection with remaining
// budget it increments the iteration and re-dispatches the generator.
// On rejection with exhausted budget it escalates.
//
// Caller must hold exec.mu.
func (c *Component) handleReviewerCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *reviewExecution) {
	// Remove the reviewer task from the index.
	c.taskIDIndex.Delete(exec.ReviewerTaskID)

	// Parse the reviewer result.
	verdict, summary, findings, formattedFindings, reviewerLLMRequestIDs := c.parseReviewerResult(event.Result, exec)

	// Update in-memory execution state.
	exec.Verdict = verdict
	exec.Summary = summary
	exec.Findings = findings
	exec.FormattedFindings = formattedFindings
	exec.ReviewerLLMRequestIDs = reviewerLLMRequestIDs

	// Write verdict triples.
	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Verdict, verdict)
	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Summary, summary)
	if len(findings) > 0 {
		_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Findings, string(findings))
	}
	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, "reviewed"); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", "reviewed", "error", err)
	}

	c.logger.Info("Review verdict",
		"review_type", exec.ReviewType,
		"slug", exec.Slug,
		"verdict", verdict,
		"iteration", exec.Iteration,
		"max_iterations", exec.MaxIterations,
	)

	if verdict == "approved" {
		c.markApprovedLocked(ctx, exec)
		return
	}

	// Rejected — decide whether to retry or escalate.
	if exec.Iteration+1 < exec.MaxIterations {
		c.startRevision(ctx, exec)
	} else {
		c.markEscalatedLocked(ctx, exec)
	}
}

// parseReviewerResult extracts verdict, summary, findings, and LLM request IDs
// from the raw result string.
func (c *Component) parseReviewerResult(result string, exec *reviewExecution) (verdict, summary string, findings json.RawMessage, formattedFindings string, llmRequestIDs []string) {
	switch exec.ReviewType {
	case reviewTypePlanReview, reviewTypePhaseReview:
		var r payloads.ReviewResult
		if err := json.Unmarshal([]byte(result), &r); err != nil {
			c.logger.Warn("Failed to parse review result, defaulting to rejected for safety",
				"slug", exec.Slug, "error", err)
			return "rejected", "parse failure — could not read reviewer response", nil, "", nil
		}
		return r.Verdict, r.Summary, r.Findings, r.FormattedFindings, r.LLMRequestIDs

	case reviewTypeTaskReview:
		var r payloads.TaskReviewResult
		if err := json.Unmarshal([]byte(result), &r); err != nil {
			c.logger.Warn("Failed to parse task review result, defaulting to rejected for safety",
				"slug", exec.Slug, "error", err)
			return "rejected", "parse failure — could not read reviewer response", nil, "", nil
		}
		return r.Verdict, r.Summary, r.Findings, r.FormattedFindings, r.LLMRequestIDs

	default:
		return "rejected", "unknown review type — cannot approve", nil, "", nil
	}
}

// ---------------------------------------------------------------------------
// Terminal state handlers
// ---------------------------------------------------------------------------

// markApprovedLocked transitions the execution to the approved terminal state.
// Only writes workflow.phase — the rule processor owns workflow.status.
// Caller must hold exec.mu.
func (c *Component) markApprovedLocked(ctx context.Context, exec *reviewExecution) {
	if exec.terminated {
		return
	}
	exec.terminated = true

	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, "approved"); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", "approved", "error", err)
	}

	c.reviewsApproved.Add(1)
	c.reviewsCompleted.Add(1)

	c.logger.Info("Review approved",
		"review_type", exec.ReviewType,
		"slug", exec.Slug,
		"iteration", exec.Iteration,
	)

	c.publishEntity(context.Background(), NewReviewExecutionEntity(exec).WithPhase("approved"))
	c.cleanupExecutionLocked(exec)
}

// markEscalatedLocked transitions the execution to the escalated terminal state.
// Only writes workflow.phase — the rule processor owns workflow.status.
// Caller must hold exec.mu.
func (c *Component) markEscalatedLocked(ctx context.Context, exec *reviewExecution) {
	if exec.terminated {
		return
	}
	exec.terminated = true

	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, "escalated"); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", "escalated", "error", err)
	}

	c.reviewsEscalated.Add(1)
	c.reviewsCompleted.Add(1)

	c.logger.Info("Review escalated — max iterations exceeded",
		"review_type", exec.ReviewType,
		"slug", exec.Slug,
		"iteration", exec.Iteration,
		"max_iterations", exec.MaxIterations,
	)

	c.publishEntity(context.Background(), NewReviewExecutionEntity(exec).WithPhase("escalated"))
	c.cleanupExecutionLocked(exec)
}

// markErrorLocked transitions the execution to the error terminal state.
// Only writes workflow.phase — the rule processor owns workflow.status.
// Caller must hold exec.mu.
func (c *Component) markErrorLocked(ctx context.Context, exec *reviewExecution, reason string) {
	if exec.terminated {
		return
	}
	exec.terminated = true

	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, "error"); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", "error", "error", err)
	}
	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.ErrorReason, reason)

	c.errors.Add(1)
	c.reviewsCompleted.Add(1)

	c.logger.Error("Review failed",
		"review_type", exec.ReviewType,
		"slug", exec.Slug,
		"reason", reason,
	)

	c.publishEntity(context.Background(), NewReviewExecutionEntity(exec).WithPhase("error").WithErrorReason(reason))
	c.cleanupExecutionLocked(exec)
}

// cleanupExecutionLocked removes execution from activeReviews, cleans up the taskID
// index entries, and cancels the timeout timer.
// Caller must hold exec.mu.
func (c *Component) cleanupExecutionLocked(exec *reviewExecution) {
	if exec.timeoutTimer != nil {
		exec.timeoutTimer.stop()
	}
	c.taskIDIndex.Delete(exec.GeneratorTaskID)
	c.taskIDIndex.Delete(exec.ReviewerTaskID)
	c.activeReviews.Delete(exec.EntityID)
}

// startRevision increments the iteration counter, clears stale generator and
// reviewer output, and re-dispatches the generator with revision context.
func (c *Component) startRevision(ctx context.Context, exec *reviewExecution) {
	exec.Iteration++
	exec.PlanContent = nil
	exec.LLMRequestIDs = nil
	exec.ReviewerLLMRequestIDs = nil
	// Keep Summary and FormattedFindings — the generator prompt builder uses them.

	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Iteration, exec.Iteration)
	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, "generating"); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", "generating", "error", err)
	}

	c.logger.Info("Starting revision",
		"review_type", exec.ReviewType,
		"slug", exec.Slug,
		"new_iteration", exec.Iteration,
	)

	c.dispatchGeneratorLocked(ctx, exec)
}

// ---------------------------------------------------------------------------
// Per-execution timeout
// ---------------------------------------------------------------------------

// startExecutionTimeout starts a timer that marks the execution as errored if
// it does not complete within the configured timeout.
//
// Caller must hold exec.mu.
func (c *Component) startExecutionTimeout(exec *reviewExecution) {
	timeout := c.config.GetTimeout()

	timer := time.AfterFunc(timeout, func() {
		c.logger.Warn("Execution timed out",
			"entity_id", exec.EntityID,
			"review_type", exec.ReviewType,
			"slug", exec.Slug,
			"timeout", timeout,
		)
		exec.mu.Lock()
		defer exec.mu.Unlock()
		c.markErrorLocked(context.Background(), exec, fmt.Sprintf("execution timed out after %s", timeout))
	})

	exec.timeoutTimer = &timeoutHandle{
		stop: func() { timer.Stop() },
	}
}

// ---------------------------------------------------------------------------
// Agent dispatch helpers
// ---------------------------------------------------------------------------

// dispatchGeneratorLocked builds and publishes a generator TaskMessage for the
// current iteration of the given review execution.
//
// Caller must hold exec.mu.
func (c *Component) dispatchGeneratorLocked(ctx context.Context, exec *reviewExecution) {
	taskID := fmt.Sprintf("generator-%s-%s", exec.EntityID, uuid.New().String())
	exec.GeneratorTaskID = taskID
	c.taskIDIndex.Store(taskID, exec.EntityID)

	subject, payload, workflowSlug := c.buildGeneratorPayload(exec)

	// Inject TaskID + WorkflowSlug so generators can emit LoopCompletedEvent.
	switch p := payload.(type) {
	case *payloads.PlannerRequest:
		p.TaskID = taskID
		p.WorkflowSlug = workflowSlug
	case *payloads.PhaseGeneratorRequest:
		p.TaskID = taskID
		p.WorkflowSlug = workflowSlug
	case *payloads.TaskGeneratorRequest:
		p.TaskID = taskID
		p.WorkflowSlug = workflowSlug
	}

	// Publish typed request to the generator's async subject.
	// Generator components (planner, phase-generator, task-generator) consume
	// from these subjects — they read the typed payload, not the TaskMessage.
	if err := c.publishBaseMessage(ctx, subject, payload); err != nil {
		c.logger.Error("Failed to publish generator request",
			"subject", subject, "review_type", exec.ReviewType, "slug", exec.Slug, "error", err)
		c.errors.Add(1)
		return
	}

	// NOTE: No TaskMessage to agent.task.general — the generator component
	// (planner, phase-generator, task-generator) emits LoopCompletedEvent
	// directly after completing its work. The TaskID is passed in the typed
	// payload so it can echo it back.

	c.logger.Info("Dispatched generator",
		"review_type", exec.ReviewType,
		"slug", exec.Slug,
		"iteration", exec.Iteration,
		"task_id", taskID,
		"subject", subject,
	)
}

// buildGeneratorPayload constructs the typed request payload, applying revision
// context if this is a retry iteration.
func (c *Component) buildGeneratorPayload(exec *reviewExecution) (subject string, payload message.Payload, workflowSlug string) {
	prompt := exec.Prompt
	isRevision := exec.Iteration > 0
	if isRevision {
		prompt = exec.Prompt + "\n\n---\n\n" + exec.buildRevisionContext()
	}

	switch exec.ReviewType {
	case reviewTypePlanReview:
		workflowSlug = WorkflowSlugPlanReview
		subject = subjectPlannerAsync
		payload = &payloads.PlannerRequest{
			ExecutionID:      exec.EntityID,
			RequestID:        exec.RequestID,
			Slug:             exec.Slug,
			Title:            exec.Title,
			Description:      exec.Description,
			ProjectID:        exec.ProjectID,
			TraceID:          exec.TraceID,
			LoopID:           exec.LoopID,
			ScopePatterns:    exec.ScopePatterns,
			Auto:             exec.Auto,
			Prompt:           prompt,
			Revision:         isRevision,
			PreviousFindings: revisionFindings(isRevision, exec),
		}

	case reviewTypePhaseReview:
		workflowSlug = WorkflowSlugPhaseReview
		subject = subjectPhaseGeneratorAsync
		payload = &payloads.PhaseGeneratorRequest{
			ExecutionID:      exec.EntityID,
			RequestID:        exec.RequestID,
			Slug:             exec.Slug,
			Title:            exec.Title,
			Description:      exec.Description,
			ProjectID:        exec.ProjectID,
			TraceID:          exec.TraceID,
			LoopID:           exec.LoopID,
			ScopePatterns:    exec.ScopePatterns,
			Prompt:           prompt,
			Revision:         isRevision,
			PreviousFindings: revisionFindings(isRevision, exec),
		}

	case reviewTypeTaskReview:
		workflowSlug = WorkflowSlugTaskReview
		subject = subjectTaskGeneratorAsync
		payload = &payloads.TaskGeneratorRequest{
			ExecutionID:      exec.EntityID,
			RequestID:        exec.RequestID,
			Slug:             exec.Slug,
			Title:            exec.Title,
			Description:      exec.Description,
			ProjectID:        exec.ProjectID,
			TraceID:          exec.TraceID,
			LoopID:           exec.LoopID,
			ScopePatterns:    exec.ScopePatterns,
			Prompt:           prompt,
			Revision:         isRevision,
			PreviousFindings: revisionFindings(isRevision, exec),
		}
	}
	return
}

// revisionFindings returns the revision context string if this is a retry, empty otherwise.
func revisionFindings(isRevision bool, exec *reviewExecution) string {
	if isRevision {
		return exec.buildRevisionContext()
	}
	return ""
}

// dispatchReviewerLocked builds and publishes a reviewer TaskMessage for the
// current iteration of the given review execution.
//
// Caller must hold exec.mu.
func (c *Component) dispatchReviewerLocked(ctx context.Context, exec *reviewExecution) {
	taskID := fmt.Sprintf("reviewer-%s-%s", exec.EntityID, uuid.New().String())
	exec.ReviewerTaskID = taskID
	c.taskIDIndex.Store(taskID, exec.EntityID)

	var subject string
	var payload message.Payload
	var workflowSlug string

	switch exec.ReviewType {
	case reviewTypePlanReview:
		workflowSlug = WorkflowSlugPlanReview
		subject = subjectPlanReviewerAsync
		payload = &payloads.PlanReviewRequest{
			ExecutionID:   exec.EntityID,
			RequestID:     exec.RequestID,
			Slug:          exec.Slug,
			ProjectID:     exec.ProjectID,
			PlanContent:   exec.PlanContent,
			ScopePatterns: exec.ScopePatterns,
			TraceID:       exec.TraceID,
			LoopID:        exec.LoopID,
		}

	case reviewTypePhaseReview:
		// Phase review reuses the plan-reviewer component with phase content.
		workflowSlug = WorkflowSlugPhaseReview
		subject = subjectPlanReviewerAsync
		payload = &payloads.PhaseReviewRequest{
			ExecutionID:   exec.EntityID,
			RequestID:     exec.RequestID,
			Slug:          exec.Slug,
			ProjectID:     exec.ProjectID,
			PlanContent:   exec.PlanContent,
			ScopePatterns: exec.ScopePatterns,
			TraceID:       exec.TraceID,
			LoopID:        exec.LoopID,
		}

	case reviewTypeTaskReview:
		workflowSlug = WorkflowSlugTaskReview
		subject = subjectTaskReviewerAsync
		payload = &payloads.TaskReviewRequest{
			ExecutionID:   exec.EntityID,
			RequestID:     exec.RequestID,
			Slug:          exec.Slug,
			ProjectID:     exec.ProjectID,
			ScopePatterns: exec.ScopePatterns,
			TraceID:       exec.TraceID,
			LoopID:        exec.LoopID,
		}
	}

	// Inject TaskID + WorkflowSlug so reviewers can emit LoopCompletedEvent.
	switch p := payload.(type) {
	case *payloads.PlanReviewRequest:
		p.TaskID = taskID
		p.WorkflowSlug = workflowSlug
	case *payloads.PhaseReviewRequest:
		p.TaskID = taskID
		p.WorkflowSlug = workflowSlug
	case *payloads.TaskReviewRequest:
		p.TaskID = taskID
		p.WorkflowSlug = workflowSlug
	}

	// Publish typed request to the reviewer's async subject.
	if err := c.publishBaseMessage(ctx, subject, payload); err != nil {
		c.logger.Error("Failed to publish reviewer request",
			"subject", subject, "review_type", exec.ReviewType, "slug", exec.Slug, "error", err)
		c.errors.Add(1)
		return
	}

	// NOTE: No TaskMessage to agent.task.reviewer — the reviewer component
	// emits LoopCompletedEvent directly. TaskID is in the typed payload.

	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, "reviewing"); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", "reviewing", "error", err)
	}

	c.logger.Info("Dispatched reviewer",
		"review_type", exec.ReviewType,
		"slug", exec.Slug,
		"iteration", exec.Iteration,
		"task_id", taskID,
		"subject", subject,
	)
}

// ---------------------------------------------------------------------------
// Triple and task publishing helpers
// ---------------------------------------------------------------------------

// publishBaseMessage wraps a payload in a BaseMessage and publishes it to a
// JetStream stream via PublishToStream (delivery-acknowledged, trace-propagated).
// Used for typed requests to workflow.async.* subjects (WORKFLOW stream).
func (c *Component) publishBaseMessage(ctx context.Context, subject string, payload message.Payload) error {
	baseMsg := message.NewBaseMessage(payload.Schema(), payload, componentName)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal base message: %w", err)
	}

	if c.natsClient != nil {
		if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
			return fmt.Errorf("publish to %s: %w", subject, err)
		}
	}
	return nil
}

// publishTask wraps a TaskMessage in a BaseMessage and publishes it to a
// JetStream stream via PublishToStream. Used for agent.task.* subjects (AGENT stream).
func (c *Component) publishTask(ctx context.Context, subject string, task *agentic.TaskMessage) {
	baseMsg := message.NewBaseMessage(task.Schema(), task, componentName)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Debug("Failed to marshal task message", "error", err)
		c.errors.Add(1)
		return
	}

	if c.natsClient != nil {
		if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
			c.logger.Debug("Failed to publish task", "subject", subject, "error", err)
			c.errors.Add(1)
		}
	}
}

// ---------------------------------------------------------------------------
// Utility helpers
// ---------------------------------------------------------------------------

func (c *Component) updateLastActivity() {
	c.lastActivityMu.Lock()
	c.lastActivity = time.Now()
	c.lastActivityMu.Unlock()
}

func (c *Component) getLastActivity() time.Time {
	c.lastActivityMu.RLock()
	defer c.lastActivityMu.RUnlock()
	return c.lastActivity
}

// ---------------------------------------------------------------------------
// component.Discoverable interface
// ---------------------------------------------------------------------------

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        componentName,
		Type:        "processor",
		Description: "Orchestrates plan, phase, and task review loops using entity triples and agent dispatch",
		Version:     componentVersion,
	}
}

// InputPorts returns the component's input port definitions.
func (c *Component) InputPorts() []component.Port {
	return c.inputPorts
}

// OutputPorts returns the component's output port definitions.
func (c *Component) OutputPorts() []component.Port {
	return c.outputPorts
}

// ConfigSchema returns the JSON schema for this component's configuration.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return reviewOrchestratorSchema
}

// Health returns the current health status of the component.
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	running := c.running
	c.mu.RUnlock()

	if running {
		return component.HealthStatus{
			Healthy:    true,
			Status:     "healthy",
			LastCheck:  time.Now(),
			ErrorCount: int(c.errors.Load()),
		}
	}
	return component.HealthStatus{Status: "stopped"}
}

// DataFlow returns current flow metrics for the component.
func (c *Component) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{
		LastActivity: c.getLastActivity(),
	}
}
