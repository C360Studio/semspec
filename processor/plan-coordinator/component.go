// Package plancoordinator provides a processor that coordinates concurrent planners
// for parallel plan generation.
//
// The plan-coordinator orchestrates a fan-out/fan-in pipeline:
//
//  1. Receives a coordination trigger with title, description, and optional focus areas.
//  2. Determines focus areas (via explicit list or LLM-driven analysis).
//  3. Dispatches N planner agents in parallel (one per focus area).
//  4. Collects planner results via agentic.loop_completed events.
//  5. When all planners complete, synthesizes results into a unified plan.
//  6. Writes terminal phase triple — rules handle status transitions.
//
// State lives as entity triples in ENTITY_STATES. No typed Go structs are
// stored in KV — the component keeps lightweight in-memory tracking (sync.Map)
// for routing completion events back to the correct coordination execution.
//
// Terminal status transitions (completed, failed) are owned by the JSON rule
// processor, NOT by this component. This component writes only workflow.phase;
// rules react and set workflow.status + publish events.
package plancoordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/prompt"
	promptdomain "github.com/c360studio/semspec/prompt/domain"
	contextbuilder "github.com/c360studio/semspec/processor/context-builder"
	"github.com/c360studio/semspec/processor/contexthelper"
	wf "github.com/c360studio/semspec/vocabulary/workflow"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semspec/workflow/prompts"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

const (
	componentName    = "plan-coordinator"
	componentVersion = "0.2.0"

	// WorkflowSlugCoordination identifies coordination events in LoopCompletedEvent.
	WorkflowSlugCoordination = "semspec-coordination"

	// Workflow step constants for planner agent dispatch.
	workflowStepPlan = "plan"

	// Phase values written to entity triples.
	phaseFocusing     = "focusing"
	phasePlanning     = "planning"
	phaseSynthesizing = "synthesizing"
	phaseCompleted    = "completed"
	phaseError        = "error"

	// Trigger and completion subjects.
	subjectCoordinationTrigger = "workflow.trigger.plan-coordinator"
	subjectLoopCompleted       = "agentic.loop_completed.v1"

	// Downstream dispatch: typed planner request + agentic task.
	subjectPlannerAsync = "workflow.async.planner"
	subjectAgentPlanner = "agent.task.general"
)

// llmCompleter is the subset of the LLM client used by plan-coordinator.
type llmCompleter interface {
	Complete(ctx context.Context, req llm.Request) (*llm.Response, error)
}

// Component orchestrates parallel planner coordination.
type Component struct {
	config       Config
	natsClient   *natsclient.Client
	logger       *slog.Logger
	platform     component.PlatformMeta
	tripleWriter *graphutil.TripleWriter

	llmClient     llmCompleter
	modelRegistry *model.Registry
	assembler     *prompt.Assembler
	contextHelper *contexthelper.Helper

	inputPorts  []component.Port
	outputPorts []component.Port

	// activeCoordinations maps entityID → *coordinationExecution.
	activeCoordinations sync.Map

	// taskIDIndex maps TaskID (used in dispatched TaskMessages) → entityID.
	taskIDIndex sync.Map

	// Lifecycle
	shutdown      chan struct{}
	wg            sync.WaitGroup
	running       bool
	mu            sync.RWMutex
	lifecycleMu   sync.Mutex
	subscriptions []*natsclient.Subscription

	// Metrics
	triggersProcessed      atomic.Int64
	coordinationsCompleted atomic.Int64
	coordinationsFailed    atomic.Int64
	errors                 atomic.Int64
	lastActivityMu         sync.RWMutex
	lastActivity           time.Time
}

// NewComponent creates a new plan-coordinator from raw JSON config.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var cfg Config
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal plan-coordinator config: %w", err)
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

	ctxHelper := contexthelper.New(deps.NATSClient, contexthelper.Config{
		SubjectPrefix: cfg.ContextSubjectPrefix,
		Timeout:       cfg.GetContextTimeout(),
		SourceName:    componentName,
	}, logger)

	// Initialize prompt assembler with software domain
	registry := prompt.NewRegistry()
	registry.RegisterAll(promptdomain.Software()...)
	registry.Register(prompt.ToolGuidanceFragment(prompt.DefaultToolGuidance()))
	assembler := prompt.NewAssembler(registry)

	c := &Component{
		config:        cfg,
		natsClient:    deps.NATSClient,
		logger:        logger,
		platform:      deps.Platform,
		llmClient:     llm.NewClient(model.Global(), llm.WithLogger(logger), llm.WithCallStore(llm.GlobalCallStore())),
		modelRegistry: model.Global(),
		assembler:     assembler,
		contextHelper: ctxHelper,
		shutdown:      make(chan struct{}),
		tripleWriter: &graphutil.TripleWriter{
			NATSClient:    deps.NATSClient,
			Logger:        logger,
			ComponentName: componentName,
		},
	}

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

// Initialize prepares the component.
func (c *Component) Initialize() error { return nil }

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

	c.logger.Info("Starting plan-coordinator")

	// Start context helper for centralized context building.
	if err := c.contextHelper.Start(ctx); err != nil {
		return fmt.Errorf("start context helper: %w", err)
	}

	triggerHandler := func(ctx context.Context, msg *nats.Msg) {
		c.wg.Add(1)
		defer c.wg.Done()
		select {
		case <-c.shutdown:
			return
		default:
		}
		c.handleTrigger(ctx, msg)
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
		case subjectCoordinationTrigger:
			handler = triggerHandler
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

// Stop performs graceful shutdown.
func (c *Component) Stop(timeout time.Duration) error {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()

	c.mu.RLock()
	if !c.running {
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()

	c.logger.Info("Stopping plan-coordinator",
		"triggers_processed", c.triggersProcessed.Load(),
		"coordinations_completed", c.coordinationsCompleted.Load(),
		"coordinations_failed", c.coordinationsFailed.Load(),
	)

	close(c.shutdown)

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

	// Cancel any active coordination timeouts.
	c.activeCoordinations.Range(func(_, value any) bool {
		exec := value.(*coordinationExecution)
		exec.mu.Lock()
		if exec.timeoutTimer != nil {
			exec.timeoutTimer.stop()
		}
		exec.mu.Unlock()
		return true
	})

	c.contextHelper.Stop()

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

// handleTrigger parses a coordination trigger, determines focus areas, and
// dispatches N planner agents in parallel.
func (c *Component) handleTrigger(ctx context.Context, msg *nats.Msg) {
	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	trigger, err := payloads.ParseReactivePayload[payloads.PlanCoordinatorRequest](msg.Data)
	if err != nil {
		c.logger.Error("Failed to parse coordination trigger", "error", err)
		c.errors.Add(1)
		return
	}

	if trigger.Slug == "" {
		c.logger.Error("Trigger missing slug")
		c.errors.Add(1)
		return
	}

	entityID := fmt.Sprintf("local.semspec.workflow.coordination.execution.%s", trigger.Slug)

	c.logger.Info("Coordination trigger received",
		"slug", trigger.Slug,
		"entity_id", entityID,
		"trace_id", trigger.TraceID,
		"explicit_focuses", trigger.FocusAreas,
	)

	exec := &coordinationExecution{
		EntityID:         entityID,
		Slug:             trigger.Slug,
		Title:            trigger.Title,
		Description:      trigger.Description,
		ProjectID:        trigger.ProjectID,
		TraceID:          trigger.TraceID,
		LoopID:           trigger.LoopID,
		RequestID:        trigger.RequestID,
		CompletedResults: make(map[string]*workflow.PlannerResult),
	}

	// Deduplicate: if a coordination for this entityID already exists, skip.
	if _, loaded := c.activeCoordinations.LoadOrStore(entityID, exec); loaded {
		c.logger.Debug("Duplicate trigger for active coordination, skipping",
			"entity_id", entityID,
		)
		return
	}

	// Write initial entity triples.
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.Type, "coordination")
	if err := c.tripleWriter.WriteTriple(ctx, entityID, wf.Phase, phaseFocusing); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseFocusing, "error", err)
	}
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.Slug, trigger.Slug)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.Title, trigger.Title)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.ProjectID, trigger.ProjectID)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.TraceID, trigger.TraceID)

	// Publish initial entity snapshot for graph observability.
	c.publishEntity(ctx, NewCoordinationEntity(exec).WithPhase(phaseFocusing))

	// Determine focus areas BEFORE acquiring exec.mu — the LLM call can take
	// 30+ seconds and we don't want to block the timeout callback.
	llmCtx := ctx
	if trigger.TraceID != "" || trigger.LoopID != "" {
		llmCtx = llm.WithTraceContext(ctx, llm.TraceContext{
			TraceID: trigger.TraceID,
			LoopID:  trigger.LoopID,
		})
	}

	focuses, err := c.determineFocusAreas(llmCtx, trigger)
	if err != nil {
		c.logger.Error("Focus determination failed",
			"entity_id", entityID,
			"slug", trigger.Slug,
			"error", err,
		)
		exec.mu.Lock()
		c.markErrorLocked(ctx, exec, fmt.Sprintf("focus determination failed: %v", err))
		exec.mu.Unlock()
		return
	}

	// Lock for timeout + dispatch — no shared state was modified above.
	exec.mu.Lock()
	defer exec.mu.Unlock()

	c.startExecutionTimeoutLocked(exec)

	exec.FocusAreas = focuses
	if err := c.tripleWriter.WriteTriple(ctx, entityID, wf.Phase, phasePlanning); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phasePlanning, "error", err)
	}
	_ = c.tripleWriter.WriteTriple(ctx, entityID, "workflow.focus_areas", focusAreasJSON(focuses))
	_ = c.tripleWriter.WriteTriple(ctx, entityID, "workflow.planner_count", len(focuses))

	c.logger.Info("Focus areas determined, dispatching planners",
		"entity_id", entityID,
		"focus_count", len(focuses),
	)

	// Dispatch N planner agents.
	c.dispatchPlannersLocked(ctx, exec)
}

// ---------------------------------------------------------------------------
// Loop-completion handler
// ---------------------------------------------------------------------------

func (c *Component) handleLoopCompleted(ctx context.Context, msg *nats.Msg) {
	var base message.BaseMessage
	if err := json.Unmarshal(msg.Data, &base); err != nil {
		c.logger.Debug("Failed to unmarshal loop completed envelope", "error", err)
		c.errors.Add(1)
		return
	}

	event, ok := base.Payload().(*agentic.LoopCompletedEvent)
	if !ok {
		return
	}

	if event.WorkflowSlug != WorkflowSlugCoordination {
		return
	}

	c.updateLastActivity()

	// Resolve the entityID from the TaskID using the secondary index.
	entityIDVal, ok := c.taskIDIndex.Load(event.TaskID)
	if !ok {
		c.logger.Debug("Loop completed for unknown task ID",
			"task_id", event.TaskID,
			"workflow_step", event.WorkflowStep,
		)
		return
	}
	entityID := entityIDVal.(string)

	execVal, ok := c.activeCoordinations.Load(entityID)
	if !ok {
		c.logger.Debug("No active coordination for entity", "entity_id", entityID)
		return
	}
	exec := execVal.(*coordinationExecution)

	exec.mu.Lock()
	defer exec.mu.Unlock()

	c.handlePlannerCompleteLocked(ctx, event, exec)
}

// ---------------------------------------------------------------------------
// Planner-complete handler
// ---------------------------------------------------------------------------

// handlePlannerCompleteLocked processes a planner agent completion. When all
// planners have completed, it triggers synthesis.
//
// Caller must hold exec.mu.
func (c *Component) handlePlannerCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *coordinationExecution) {
	// Guard against racing with timeout — if already terminated, skip.
	if exec.terminated {
		return
	}

	// Remove this task from the index.
	c.taskIDIndex.Delete(event.TaskID)

	// Parse the planner result.
	result, llmRequestIDs := c.parsePlannerResult(event.Result, event.TaskID)
	if result != nil {
		exec.CompletedResults[event.TaskID] = result
	}
	exec.LLMRequestIDs = append(exec.LLMRequestIDs, llmRequestIDs...)

	c.logger.Info("Planner completed",
		"entity_id", exec.EntityID,
		"task_id", event.TaskID,
		"completed", len(exec.CompletedResults),
		"expected", exec.ExpectedPlanners,
	)

	// Check if all planners are done.
	if !exec.allPlannersComplete() {
		return
	}

	// All planners complete — synthesize.
	c.synthesizeAndCompleteLocked(ctx, exec)
}

// ---------------------------------------------------------------------------
// Synthesis
// ---------------------------------------------------------------------------

// synthesizeAndCompleteLocked combines all planner results and writes the
// terminal phase triple.
//
// Caller must hold exec.mu.
func (c *Component) synthesizeAndCompleteLocked(ctx context.Context, exec *coordinationExecution) {
	if exec.terminated {
		return
	}
	exec.terminated = true

	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseSynthesizing); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseSynthesizing, "error", err)
	}

	results := exec.collectResults()

	llmCtx := ctx
	if exec.TraceID != "" || exec.LoopID != "" {
		llmCtx = llm.WithTraceContext(ctx, llm.TraceContext{
			TraceID: exec.TraceID,
			LoopID:  exec.LoopID,
		})
	}

	synthesized, synthLLMID, err := c.synthesizeResults(llmCtx, results)
	if err != nil {
		c.logger.Error("Synthesis failed",
			"entity_id", exec.EntityID,
			"error", err,
		)
		c.markErrorLocked(ctx, exec, fmt.Sprintf("synthesis failed: %v", err))
		return
	}

	exec.SynthesizedPlan = synthesized
	exec.SynthesisLLMID = synthLLMID

	// Write synthesis result triples.
	// Note: workflow.plan_goal, workflow.plan_context, workflow.plan_scope have no vocabulary
	// constants yet — these are coordinator-specific predicates.
	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, "workflow.plan_goal", synthesized.Goal)
	if synthesized.Context != "" {
		_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, "workflow.plan_context", synthesized.Context)
	}
	if scopeJSON, err := json.Marshal(synthesized.Scope); err == nil {
		_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, "workflow.plan_scope", string(scopeJSON))
	}

	// Mark completed — rules handle status transition.
	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseCompleted); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseCompleted, "error", err)
	}

	c.coordinationsCompleted.Add(1)

	c.logger.Info("Coordination completed",
		"entity_id", exec.EntityID,
		"slug", exec.Slug,
		"planner_count", exec.ExpectedPlanners,
	)

	c.publishEntity(context.Background(), NewCoordinationEntity(exec).WithPhase(phaseCompleted))
	c.cleanupExecutionLocked(exec)
}

// ---------------------------------------------------------------------------
// Terminal state handlers
// ---------------------------------------------------------------------------

// markErrorLocked transitions to the error terminal state.
// Caller must hold exec.mu.
func (c *Component) markErrorLocked(ctx context.Context, exec *coordinationExecution, reason string) {
	if exec.terminated {
		return
	}
	exec.terminated = true

	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseError); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseError, "error", err)
	}
	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.ErrorReason, reason)

	c.errors.Add(1)
	c.coordinationsFailed.Add(1)

	c.logger.Error("Coordination failed",
		"entity_id", exec.EntityID,
		"slug", exec.Slug,
		"reason", reason,
	)

	c.publishEntity(context.Background(), NewCoordinationEntity(exec).WithPhase(phaseError).WithErrorReason(reason))
	c.cleanupExecutionLocked(exec)
}

// cleanupExecutionLocked removes execution from maps and cancels timeout.
// Caller must hold exec.mu.
func (c *Component) cleanupExecutionLocked(exec *coordinationExecution) {
	if exec.timeoutTimer != nil {
		exec.timeoutTimer.stop()
	}
	for _, taskID := range exec.PlannerTaskIDs {
		c.taskIDIndex.Delete(taskID)
	}
	c.activeCoordinations.Delete(exec.EntityID)
}

// ---------------------------------------------------------------------------
// Per-execution timeout
// ---------------------------------------------------------------------------

// startExecutionTimeoutLocked starts a timer that marks the execution as errored
// if it does not complete within the configured timeout.
//
// Caller must hold exec.mu.
func (c *Component) startExecutionTimeoutLocked(exec *coordinationExecution) {
	timeout := c.config.GetTimeout()

	timer := time.AfterFunc(timeout, func() {
		c.logger.Warn("Coordination timed out",
			"entity_id", exec.EntityID,
			"slug", exec.Slug,
			"timeout", timeout,
		)
		exec.mu.Lock()
		defer exec.mu.Unlock()
		c.markErrorLocked(context.Background(), exec, fmt.Sprintf("coordination timed out after %s", timeout))
	})

	exec.timeoutTimer = &timeoutHandle{
		stop: func() { timer.Stop() },
	}
}

// ---------------------------------------------------------------------------
// Agent dispatch: Planners
// ---------------------------------------------------------------------------

// dispatchPlannersLocked dispatches one planner agent per focus area.
//
// Caller must hold exec.mu.
func (c *Component) dispatchPlannersLocked(ctx context.Context, exec *coordinationExecution) {
	exec.ExpectedPlanners = len(exec.FocusAreas)
	exec.PlannerTaskIDs = make([]string, 0, exec.ExpectedPlanners)

	for _, focus := range exec.FocusAreas {
		taskID := fmt.Sprintf("planner-%s-%s", exec.EntityID, uuid.New().String())
		exec.PlannerTaskIDs = append(exec.PlannerTaskIDs, taskID)
		c.taskIDIndex.Store(taskID, exec.EntityID)

		// Build the planner request payload.
		req := &payloads.PlannerRequest{
			ExecutionID: exec.EntityID,
			RequestID:   exec.RequestID,
			Slug:        exec.Slug,
			Title:       exec.Title,
			Description: exec.Description,
			ProjectID:   exec.ProjectID,
			TraceID:     exec.TraceID,
			LoopID:      exec.LoopID,
			Prompt:      c.buildPlannerPrompt(exec, focus),
		}

		// Publish typed request to planner's async subject.
		if err := c.publishBaseMessage(ctx, subjectPlannerAsync, req); err != nil {
			c.logger.Error("Failed to publish planner request",
				"slug", exec.Slug, "focus", focus.Area, "error", err)
			c.markErrorLocked(ctx, exec, fmt.Sprintf("dispatch planner failed: %v", err))
			return
		}

		// Publish TaskMessage for agentic-loop tracking.
		task := &agentic.TaskMessage{
			TaskID:       taskID,
			Role:         agentic.RoleGeneral,
			Model:        c.config.Model,
			WorkflowSlug: WorkflowSlugCoordination,
			WorkflowStep: workflowStepPlan,
		}
		if err := c.publishTask(ctx, subjectAgentPlanner, task); err != nil {
			c.logger.Error("Failed to publish planner task message",
				"slug", exec.Slug, "focus", focus.Area, "error", err)
			c.markErrorLocked(ctx, exec, fmt.Sprintf("dispatch planner task failed: %v", err))
			return
		}

		c.logger.Info("Dispatched planner",
			"slug", exec.Slug,
			"focus", focus.Area,
			"task_id", taskID,
		)
	}
}

// buildPlannerPrompt constructs the prompt for a focused planner.
func (c *Component) buildPlannerPrompt(exec *coordinationExecution, focus *FocusArea) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Focus Area: %s\n", focus.Area))
	if focus.Description != "" {
		sb.WriteString(fmt.Sprintf("Description: %s\n", focus.Description))
	}
	if len(focus.Hints) > 0 {
		sb.WriteString(fmt.Sprintf("Hints: %s\n", strings.Join(focus.Hints, ", ")))
	}
	sb.WriteString(fmt.Sprintf("\nTitle: %s\n", exec.Title))
	if exec.Description != "" {
		sb.WriteString(fmt.Sprintf("Description: %s\n", exec.Description))
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// Focus area determination
// ---------------------------------------------------------------------------

// determineFocusAreas decides what focus areas to use for planning.
// Uses explicit focuses if provided, otherwise calls the LLM with
// graph context from the context-builder.
func (c *Component) determineFocusAreas(ctx context.Context, trigger *payloads.PlanCoordinatorRequest) ([]*FocusArea, error) {
	// If explicit focuses provided, use them.
	if len(trigger.FocusAreas) > 0 {
		focuses := make([]*FocusArea, len(trigger.FocusAreas))
		for i, f := range trigger.FocusAreas {
			focuses[i] = &FocusArea{
				Area:        f,
				Description: fmt.Sprintf("Analyze from %s perspective", f),
			}
		}
		return focuses, nil
	}

	// Request coordination context from context-builder (graph-first).
	var graphContext string
	resp := c.contextHelper.BuildContextGraceful(ctx, &contextbuilder.ContextBuildRequest{
		TaskType: contextbuilder.TaskTypePlanning,
		Topic:    trigger.Title,
	})
	if resp != nil {
		graphContext = contexthelper.FormatContextResponse(resp)
		c.logger.Info("Built coordination context via context-builder",
			"title", trigger.Title,
			"entities", len(resp.Entities),
			"documents", len(resp.Documents),
			"tokens_used", resp.TokensUsed)
	}

	// Use LLM to determine focus areas.
	provider := c.resolveProvider()
	assembled := c.assembler.Assemble(&prompt.AssemblyContext{
		Role:     prompt.RolePlanCoordinator,
		Provider: provider,
		Domain:   "software",
	})
	systemPrompt := assembled.SystemMessage

	userPrompt := c.buildFocusUserPrompt(trigger, graphContext)

	content, _, err := c.callLLM(ctx, systemPrompt, userPrompt)
	if err != nil {
		c.logger.Warn("Failed to determine focus areas via LLM, falling back to single planner",
			"error", err)
		return []*FocusArea{{
			Area:        "general",
			Description: "General analysis of the task",
		}}, nil
	}

	focuses, err := c.parseFocusAreas(content)
	if err != nil {
		c.logger.Warn("Failed to parse focus areas, falling back to single planner",
			"error", err)
		return []*FocusArea{{
			Area:        "general",
			Description: "General analysis of the task",
		}}, nil
	}

	// Limit to max concurrent planners.
	maxPlanners := c.config.MaxConcurrentPlanners
	if trigger.MaxPlanners > 0 && trigger.MaxPlanners < maxPlanners {
		maxPlanners = trigger.MaxPlanners
	}
	if len(focuses) > maxPlanners {
		focuses = focuses[:maxPlanners]
	}

	return focuses, nil
}

// buildFocusUserPrompt constructs the user prompt for focus area determination.
func (c *Component) buildFocusUserPrompt(trigger *payloads.PlanCoordinatorRequest, graphContext string) string {
	contextSection := ""
	if graphContext != "" {
		contextSection = fmt.Sprintf(`

## Codebase Context

The following context from the knowledge graph provides information about the existing codebase structure:

%s

`, graphContext)
	}

	return fmt.Sprintf(`Analyze this task and determine the optimal focus areas for planning:

**Title:** %s
**Description:** %s
%s
Based on the task complexity, decide:
1. How many planners to spawn (1-3)
2. What focus areas each should cover

Respond with a JSON object:
`+"```json"+`
{
  "focus_areas": [
    {
      "area": "focus area name",
      "description": "what to analyze",
      "hints": ["file patterns", "keywords"]
    }
  ]
}
`+"```", trigger.Title, trigger.Description, contextSection)
}

// parseFocusAreas extracts focus areas from LLM response.
func (c *Component) parseFocusAreas(content string) ([]*FocusArea, error) {
	jsonContent := llm.ExtractJSON(content)
	if jsonContent == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var resp struct {
		FocusAreas []struct {
			Area        string   `json:"area"`
			Description string   `json:"description"`
			Hints       []string `json:"hints,omitempty"`
		} `json:"focus_areas"`
	}

	if err := json.Unmarshal([]byte(jsonContent), &resp); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	if len(resp.FocusAreas) == 0 {
		return nil, fmt.Errorf("no focus areas in response")
	}

	focuses := make([]*FocusArea, len(resp.FocusAreas))
	for i, fa := range resp.FocusAreas {
		focuses[i] = &FocusArea{
			Area:        fa.Area,
			Description: fa.Description,
			Hints:       fa.Hints,
		}
	}

	return focuses, nil
}

// ---------------------------------------------------------------------------
// Planner result parsing
// ---------------------------------------------------------------------------

// parsePlannerResult extracts a PlannerResult from the loop completion event's
// raw result string.
func (c *Component) parsePlannerResult(result, taskID string) (*workflow.PlannerResult, []string) {
	var payload PlannerResultPayload
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		c.logger.Warn("Failed to parse planner result, using raw",
			"task_id", taskID, "error", err)
		// Fall back to treating the raw result as the goal.
		return &workflow.PlannerResult{
			PlannerID: taskID,
			Goal:      result,
		}, nil
	}

	return &workflow.PlannerResult{
		PlannerID: taskID,
		Goal:      payload.Goal,
		Context:   payload.Context,
		Scope: workflow.Scope{
			Include:    payload.Scope.Include,
			Exclude:    payload.Scope.Exclude,
			DoNotTouch: payload.Scope.DoNotTouch,
		},
	}, nil
}

// ---------------------------------------------------------------------------
// Synthesis
// ---------------------------------------------------------------------------

// synthesizeResults combines multiple planner results into a unified plan.
func (c *Component) synthesizeResults(ctx context.Context, results []workflow.PlannerResult) (*SynthesizedPlan, string, error) {
	if len(results) == 1 {
		return &SynthesizedPlan{
			Goal:    results[0].Goal,
			Context: results[0].Context,
			Scope:   results[0].Scope,
		}, "", nil
	}

	systemPrompt := "You are synthesizing multiple planning perspectives into a unified development plan."
	userPrompt := prompts.PlanCoordinatorSynthesisPrompt(results)

	content, llmRequestID, err := c.callLLM(ctx, systemPrompt, userPrompt)
	if err != nil {
		c.logger.Warn("Synthesis LLM call failed, falling back to simple merge", "error", err)
		return c.simpleMerge(results), "", nil
	}

	synthesized, err := c.parseSynthesizedPlan(content)
	if err != nil {
		c.logger.Warn("Synthesis parse failed, falling back to simple merge", "error", err)
		return c.simpleMerge(results), llmRequestID, nil
	}

	if synthesized.Goal == "" {
		c.logger.Warn("Synthesis returned empty goal, falling back to simple merge")
		return c.simpleMerge(results), llmRequestID, nil
	}

	return synthesized, llmRequestID, nil
}

// simpleMerge performs a basic merge of planner results.
func (c *Component) simpleMerge(results []workflow.PlannerResult) *SynthesizedPlan {
	var goals, contexts []string
	var include, exclude, doNotTouch []string

	for _, r := range results {
		goals = append(goals, fmt.Sprintf("[%s] %s", r.FocusArea, r.Goal))
		if r.Context != "" {
			contexts = append(contexts, fmt.Sprintf("[%s] %s", r.FocusArea, r.Context))
		}
		include = append(include, r.Scope.Include...)
		exclude = append(exclude, r.Scope.Exclude...)
		doNotTouch = append(doNotTouch, r.Scope.DoNotTouch...)
	}

	return &SynthesizedPlan{
		Goal:    strings.Join(goals, "\n\n"),
		Context: strings.Join(contexts, "\n\n"),
		Scope: workflow.Scope{
			Include:    unique(include),
			Exclude:    unique(exclude),
			DoNotTouch: unique(doNotTouch),
		},
	}
}

// parseSynthesizedPlan extracts a synthesized plan from LLM response.
func (c *Component) parseSynthesizedPlan(content string) (*SynthesizedPlan, error) {
	jsonContent := llm.ExtractJSON(content)
	if jsonContent == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var parsed struct {
		Goal    string `json:"goal"`
		Context string `json:"context"`
		Scope   struct {
			Include    []string `json:"include,omitempty"`
			Exclude    []string `json:"exclude,omitempty"`
			DoNotTouch []string `json:"do_not_touch,omitempty"`
		} `json:"scope"`
	}

	if err := json.Unmarshal([]byte(jsonContent), &parsed); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	return &SynthesizedPlan{
		Goal:    parsed.Goal,
		Context: parsed.Context,
		Scope: workflow.Scope{
			Include:    parsed.Scope.Include,
			Exclude:    parsed.Scope.Exclude,
			DoNotTouch: parsed.Scope.DoNotTouch,
		},
	}, nil
}

// ---------------------------------------------------------------------------
// LLM and prompt helpers
// ---------------------------------------------------------------------------

// callLLM makes an LLM API call.
func (c *Component) callLLM(ctx context.Context, systemPrompt, userPrompt string) (string, string, error) {
	capability := c.config.DefaultCapability
	if capability == "" {
		capability = string(model.CapabilityPlanning)
	}

	temperature := 0.7
	resp, err := c.llmClient.Complete(ctx, llm.Request{
		Capability: capability,
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: &temperature,
		MaxTokens:   4096,
	})
	if err != nil {
		return "", "", fmt.Errorf("LLM completion: %w", err)
	}

	return resp.Content, resp.RequestID, nil
}

// resolveProvider determines the LLM provider for prompt formatting.
func (c *Component) resolveProvider() prompt.Provider {
	capability := c.config.DefaultCapability
	if capability == "" {
		capability = string(model.CapabilityPlanning)
	}
	modelName := c.modelRegistry.Resolve(model.Capability(capability))
	if endpoint := c.modelRegistry.GetEndpoint(modelName); endpoint != nil {
		return prompt.Provider(endpoint.Provider)
	}
	return prompt.ProviderOllama
}

// loadPrompt loads a custom prompt from file or returns the default.
func (c *Component) loadPrompt(configPath, defaultPrompt string) string {
	if configPath == "" {
		return defaultPrompt
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		c.logger.Warn("Failed to load custom prompt, using default",
			"path", configPath, "error", err)
		return defaultPrompt
	}
	return string(content)
}

// ---------------------------------------------------------------------------
// Triple and task publishing helpers
// ---------------------------------------------------------------------------

// publishBaseMessage wraps a payload in a BaseMessage and publishes to JetStream.
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

// publishTask wraps a TaskMessage in a BaseMessage and publishes to JetStream.
// Returns an error on failure so callers can fail fast.
func (c *Component) publishTask(ctx context.Context, subject string, task *agentic.TaskMessage) error {
	baseMsg := message.NewBaseMessage(task.Schema(), task, componentName)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal task message: %w", err)
	}

	if c.natsClient != nil {
		if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
			return fmt.Errorf("publish task to %s: %w", subject, err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Coordinator result payload
// ---------------------------------------------------------------------------

// CoordinatorResultType is the message type for coordinator results.
var CoordinatorResultType = message.Type{Domain: "workflow", Category: "coordinator-result", Version: "v1"}

// CoordinatorResult is the result payload for plan coordination.
type CoordinatorResult struct {
	RequestID     string   `json:"request_id"`
	TraceID       string   `json:"trace_id,omitempty"`
	Slug          string   `json:"slug"`
	PlannerCount  int      `json:"planner_count"`
	Status        string   `json:"status"`
	LLMRequestIDs []string `json:"llm_request_ids,omitempty"`
}

// Schema implements message.Payload.
func (r *CoordinatorResult) Schema() message.Type { return CoordinatorResultType }

// Validate implements message.Payload.
func (r *CoordinatorResult) Validate() error { return nil }

// MarshalJSON implements json.Marshaler.
func (r *CoordinatorResult) MarshalJSON() ([]byte, error) {
	type Alias CoordinatorResult
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *CoordinatorResult) UnmarshalJSON(data []byte) error {
	type Alias CoordinatorResult
	return json.Unmarshal(data, (*Alias)(r))
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

// unique returns unique strings from a slice.
func unique(strs []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(strs))
	for _, s := range strs {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// component.Discoverable interface
// ---------------------------------------------------------------------------

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        componentName,
		Type:        "processor",
		Description: "Coordinates parallel planners for plan generation with focus areas and synthesis",
		Version:     componentVersion,
	}
}

// InputPorts returns the component's declared input ports.
func (c *Component) InputPorts() []component.Port { return c.inputPorts }

// OutputPorts returns the component's declared output ports.
func (c *Component) OutputPorts() []component.Port { return c.outputPorts }

// ConfigSchema returns the JSON schema for this component's configuration.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return configSchema
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
