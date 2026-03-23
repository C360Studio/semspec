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
	"github.com/c360studio/semspec/graph"
	"github.com/c360studio/semspec/prompt"
	promptdomain "github.com/c360studio/semspec/prompt/domain"
	wf "github.com/c360studio/semspec/vocabulary/workflow"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semspec/workflow/prompts"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	componentName    = "plan-coordinator"
	componentVersion = "0.3.0"

	// WorkflowSlugPlan identifies plan phase events in LoopCompletedEvent.
	WorkflowSlugPlan = "semspec-plan"

	// Phase values written to entity triples.
	// The plan-coordinator advances through these phases sequentially.
	// Rules react to terminal phases (approved, escalated, error) to set status.
	phaseFocusing               = "focusing"
	phasePlanning               = "planning"
	phaseSynthesizing           = "synthesizing"
	phasePlanned                = "planned"
	phaseGeneratingRequirements = "generating_requirements"
	phaseRequirementsGenerated  = "requirements_generated"
	phaseGeneratingScenarios    = "generating_scenarios"
	phaseScenariosGenerated     = "scenarios_generated"
	phaseReviewing              = "reviewing"
	phaseApproved               = "approved"
	phaseRevisionNeeded         = "revision_needed"
	phaseEscalated              = "escalated"
	phaseAwaitingHuman          = "awaiting_human"
	phaseError                  = "error"

	// Trigger and completion subjects.
	subjectCoordinationTrigger = "workflow.trigger.plan-coordinator"
	subjectLoopCompleted       = "agent.complete.>"

	// Downstream dispatch: typed requests to processing components.
	subjectPlannerAsync       = "workflow.async.planner"
	subjectReqGeneratorAsync  = "workflow.async.requirement-generator"
	subjectScenGeneratorAsync = "workflow.async.scenario-generator"
	subjectReviewerAsync      = "workflow.async.plan-reviewer"

	// Upstream completion events from generators (WORKFLOW stream).
	subjectReqsGenerated      = "workflow.events.requirements.generated"
	subjectScenariosGenerated = "workflow.events.scenarios.generated"

	// TaskID separator for encoding role::entityID.
	taskIDSep = "::"

	// Roles for TaskID encoding.
	rolePlanner       = "planner"
	roleReqGenerator  = "requirement-generator"
	roleScenGenerator = "scenario-generator"
	roleReviewer      = "reviewer"
)

// llmCompleter is the subset of the LLM client used by plan-coordinator.
type llmCompleter interface {
	Complete(ctx context.Context, req llm.Request) (*llm.Response, error)
}

// consumerInfo tracks a JetStream consumer created during Start so it can be
// stopped cleanly via StopConsumer rather than context cancellation.
type consumerInfo struct {
	streamName   string
	consumerName string
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

	inputPorts  []component.Port
	outputPorts []component.Port

	// activeCoordinations maps entityID → *coordinationExecution.
	activeCoordinations sync.Map

	// slugIndex maps slug → entityID for routing generator completion events
	// (generators publish events with slug, not entityID).
	slugIndex sync.Map

	// Lifecycle
	consumerInfos []consumerInfo
	wg            sync.WaitGroup
	running       bool
	mu            sync.RWMutex
	lifecycleMu   sync.Mutex

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
		llmClient:     llm.NewClient(model.Global(), llm.WithLogger(logger)),
		modelRegistry: model.Global(),
		assembler:     assembler,
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

	// Reconcile: recover in-flight coordinations from graph state.
	c.reconcileFromGraph(ctx)

	// Consumer 1: coordination triggers (WORKFLOW stream).
	triggerCfg := natsclient.StreamConsumerConfig{
		StreamName:    "WORKFLOW",
		ConsumerName:  "plan-coordinator-coordination-trigger",
		FilterSubject: subjectCoordinationTrigger,
		DeliverPolicy: "new",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       30 * time.Second,
		MaxAckPending: 1,
	}
	if err := c.natsClient.ConsumeStreamWithConfig(ctx, triggerCfg, c.handleTrigger); err != nil {
		return fmt.Errorf("consume coordination triggers: %w", err)
	}
	c.consumerInfos = append(c.consumerInfos, consumerInfo{
		streamName:   triggerCfg.StreamName,
		consumerName: triggerCfg.ConsumerName,
	})

	// Consumer 2: agentic loop completion events (AGENT stream).
	completionCfg := natsclient.StreamConsumerConfig{
		StreamName:    "AGENT",
		ConsumerName:  "plan-coordinator-loop-completions",
		FilterSubject: subjectLoopCompleted,
		DeliverPolicy: "new",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       30 * time.Second,
		MaxAckPending: 10,
	}
	if err := c.natsClient.ConsumeStreamWithConfig(ctx, completionCfg, c.handleLoopCompleted); err != nil {
		for _, info := range c.consumerInfos {
			c.natsClient.StopConsumer(info.streamName, info.consumerName)
		}
		c.consumerInfos = nil
		return fmt.Errorf("consume loop completions: %w", err)
	}
	c.consumerInfos = append(c.consumerInfos, consumerInfo{
		streamName:   completionCfg.StreamName,
		consumerName: completionCfg.ConsumerName,
	})

	// Consumer 3: requirements generated events (WORKFLOW stream).
	reqsCfg := natsclient.StreamConsumerConfig{
		StreamName:    "WORKFLOW",
		ConsumerName:  "plan-coordinator-reqs-generated",
		FilterSubject: subjectReqsGenerated,
		DeliverPolicy: "new",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       30 * time.Second,
		MaxAckPending: 5,
	}
	if err := c.natsClient.ConsumeStreamWithConfig(ctx, reqsCfg, c.handleGeneratorEvent); err != nil {
		for _, info := range c.consumerInfos {
			c.natsClient.StopConsumer(info.streamName, info.consumerName)
		}
		c.consumerInfos = nil
		return fmt.Errorf("consume requirements generated events: %w", err)
	}
	c.consumerInfos = append(c.consumerInfos, consumerInfo{
		streamName:   reqsCfg.StreamName,
		consumerName: reqsCfg.ConsumerName,
	})

	// Consumer 4: scenarios generated events (WORKFLOW stream).
	scenCfg := natsclient.StreamConsumerConfig{
		StreamName:    "WORKFLOW",
		ConsumerName:  "plan-coordinator-scenarios-generated",
		FilterSubject: subjectScenariosGenerated,
		DeliverPolicy: "new",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       30 * time.Second,
		MaxAckPending: 5,
	}
	if err := c.natsClient.ConsumeStreamWithConfig(ctx, scenCfg, c.handleGeneratorEvent); err != nil {
		for _, info := range c.consumerInfos {
			c.natsClient.StopConsumer(info.streamName, info.consumerName)
		}
		c.consumerInfos = nil
		return fmt.Errorf("consume scenarios generated events: %w", err)
	}
	c.consumerInfos = append(c.consumerInfos, consumerInfo{
		streamName:   scenCfg.StreamName,
		consumerName: scenCfg.ConsumerName,
	})

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

	for _, info := range c.consumerInfos {
		c.natsClient.StopConsumer(info.streamName, info.consumerName)
	}
	c.consumerInfos = nil

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

	c.mu.Lock()
	c.running = false
	c.mu.Unlock()

	return nil
}

// ---------------------------------------------------------------------------
// Startup reconciliation
// ---------------------------------------------------------------------------

// coordTerminalPhases are phases that indicate coordination is complete.
var coordTerminalPhases = map[string]bool{
	phaseApproved:    true,
	phaseEscalated:   true,
	phaseError:       true,
	phaseAwaitingHuman: true, // Coordinator terminated — plan-api handles from here.
}

// reconcileFromGraph queries ENTITY_STATES for active coordinations and
// rebuilds the in-memory sync.Map for event routing. This allows the
// coordinator to resume after a process restart.
func (c *Component) reconcileFromGraph(ctx context.Context) {
	reconcileCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	entities, err := c.tripleWriter.ReadEntitiesByPrefix(reconcileCtx,
		"local.semspec.workflow.plan.execution.", 100)
	if err != nil {
		c.logger.Info("No graph state to reconcile (expected on first start)",
			"error", err)
		return
	}

	recovered := 0
	for entityID, triples := range entities {
		phase := triples[wf.Phase]
		if coordTerminalPhases[phase] {
			continue
		}

		slug := triples[wf.Slug]
		exec := &coordinationExecution{
			EntityID:     entityID,
			CurrentPhase: phase,
			Slug:         slug,
			Title:        triples[wf.Title],
			ProjectID:    triples[wf.ProjectID],
			TraceID:      triples[wf.TraceID],
			RequestID:    triples["workflow.execution.request_id"],
		}

		if iter, ok := triples[wf.Iteration]; ok {
			fmt.Sscanf(iter, "%d", &exec.Iteration)
		}
		if round, ok := triples["workflow.coordination.review_round"]; ok {
			fmt.Sscanf(round, "%d", &exec.ReviewRound)
		}

		c.activeCoordinations.Store(entityID, exec)
		if slug != "" {
			c.slugIndex.Store(slug, entityID)
		}
		recovered++

		c.logger.Info("Recovered coordination from graph",
			"entity_id", entityID,
			"slug", slug,
			"phase", phase,
			"review_round", exec.ReviewRound,
		)
	}

	if recovered > 0 {
		c.logger.Info("Coordination reconciliation complete",
			"recovered", recovered,
			"total_entities", len(entities))
	}
}

// ---------------------------------------------------------------------------
// Trigger handler
// ---------------------------------------------------------------------------

// handleTrigger parses a coordination trigger, determines focus areas, and
// dispatches N planner agents in parallel.
func (c *Component) handleTrigger(ctx context.Context, msg jetstream.Msg) {
	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	trigger, err := payloads.ParseReactivePayload[payloads.PlanCoordinatorRequest](msg.Data())
	if err != nil {
		c.logger.Error("Failed to parse coordination trigger", "error", err)
		c.errors.Add(1)
		_ = msg.Nak()
		return
	}

	if trigger.Slug == "" {
		c.logger.Error("Trigger missing slug")
		c.errors.Add(1)
		_ = msg.Nak()
		return
	}
	if strings.Contains(trigger.Slug, taskIDSep) {
		c.logger.Error("Slug contains reserved separator", "slug", trigger.Slug, "separator", taskIDSep)
		c.errors.Add(1)
		_ = msg.Nak()
		return
	}

	// Best-effort semsource readiness check. If semsource isn't ready
	// within the budget, proceed with local graph only.
	if reg := graph.GlobalRegistry(); reg != nil && reg.SemsourceConfigured() {
		budget := c.config.GetSemsourceReadinessBudget()
		gateCtx, gateCancel := context.WithTimeout(context.Background(), budget)
		if err := reg.WaitForSemsource(gateCtx, budget); err != nil {
			c.logger.Warn("Semsource not ready, proceeding with local graph only",
				"error", err, "slug", trigger.Slug, "budget", budget)
		} else {
			c.logger.Info("Semsource ready", "slug", trigger.Slug)
		}
		gateCancel()
	}

	entityID := fmt.Sprintf("local.semspec.workflow.plan.execution.%s", trigger.Slug)

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
	c.slugIndex.Store(trigger.Slug, entityID)
	if _, loaded := c.activeCoordinations.LoadOrStore(entityID, exec); loaded {
		c.logger.Debug("Duplicate trigger for active coordination, skipping",
			"entity_id", entityID,
		)
		_ = msg.Ack()
		return
	}

	// Ack immediately — coordination work (LLM calls) runs well beyond 30s AckWait.
	_ = msg.Ack()

	// All coordination work uses a background context so it is not bounded by
	// the JetStream message delivery context or the 30s AckWait.
	workCtx, workCancel := context.WithTimeout(context.Background(), c.config.GetTimeout())
	defer workCancel()

	c.wg.Add(1)
	defer c.wg.Done()

	// Write initial entity triples.
	_ = c.tripleWriter.WriteTriple(workCtx, entityID, wf.Type, "coordination")
	if err := c.tripleWriter.WriteTriple(workCtx, entityID, wf.Phase, phaseFocusing); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseFocusing, "error", err)
	}
	_ = c.tripleWriter.WriteTriple(workCtx, entityID, wf.Slug, trigger.Slug)
	_ = c.tripleWriter.WriteTriple(workCtx, entityID, wf.Title, trigger.Title)
	_ = c.tripleWriter.WriteTriple(workCtx, entityID, wf.ProjectID, trigger.ProjectID)
	_ = c.tripleWriter.WriteTriple(workCtx, entityID, wf.TraceID, trigger.TraceID)

	// Publish initial entity snapshot for graph observability.
	c.publishEntity(workCtx, NewCoordinationEntity(exec).WithPhase(phaseFocusing))

	// Determine focus areas BEFORE acquiring exec.mu — the LLM call can take
	// 30+ seconds and we don't want to block the timeout callback.
	llmCtx := workCtx
	if trigger.TraceID != "" || trigger.LoopID != "" {
		llmCtx = llm.WithTraceContext(workCtx, llm.TraceContext{
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
		c.markErrorLocked(workCtx, exec, fmt.Sprintf("focus determination failed: %v", err))
		exec.mu.Unlock()
		return
	}

	// Lock for timeout + dispatch — no shared state was modified above.
	exec.mu.Lock()
	defer exec.mu.Unlock()

	c.startExecutionTimeoutLocked(exec)

	exec.FocusAreas = focuses
	if err := c.tripleWriter.WriteTriple(workCtx, entityID, wf.Phase, phasePlanning); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phasePlanning, "error", err)
	}
	_ = c.tripleWriter.WriteTriple(workCtx, entityID, "workflow.focus_areas", focusAreasJSON(focuses))
	_ = c.tripleWriter.WriteTriple(workCtx, entityID, "workflow.planner_count", len(focuses))

	c.logger.Info("Focus areas determined, dispatching planners",
		"entity_id", entityID,
		"focus_count", len(focuses),
	)

	// Dispatch N planner agents.
	c.dispatchPlannersLocked(workCtx, exec)
}

// ---------------------------------------------------------------------------
// Loop-completion handler
// ---------------------------------------------------------------------------

func (c *Component) handleLoopCompleted(ctx context.Context, msg jetstream.Msg) {
	var base message.BaseMessage
	if err := json.Unmarshal(msg.Data(), &base); err != nil {
		c.logger.Debug("Failed to unmarshal loop completed envelope", "error", err)
		c.errors.Add(1)
		_ = msg.Nak()
		return
	}

	event, ok := base.Payload().(*agentic.LoopCompletedEvent)
	if !ok {
		// Not a loop completed event — ack and ignore.
		_ = msg.Ack()
		return
	}

	// Extract role and entityID from TaskID encoding: "role::entityID"
	role, entityID := parseTaskID(event.TaskID)
	if entityID == "" {
		// Not our event — TaskID doesn't use our encoding.
		_ = msg.Ack()
		return
	}

	c.updateLastActivity()

	execVal, ok := c.activeCoordinations.Load(entityID)
	if !ok {
		c.logger.Debug("No active coordination for entity", "entity_id", entityID)
		_ = msg.Ack()
		return
	}
	exec := execVal.(*coordinationExecution)

	exec.mu.Lock()
	defer exec.mu.Unlock()

	_ = msg.Ack()

	switch {
	case strings.HasPrefix(role, rolePlanner):
		c.handlePlannerCompleteLocked(ctx, event, exec)
	case role == roleReviewer:
		c.handleReviewerCompleteLocked(ctx, event, exec)
	default:
		// Requirement/scenario generators signal via workflow.events.* subjects
		// (not LoopCompletedEvent), so they're handled by handleGeneratorEvent.
		c.logger.Debug("Unknown completion role", "role", role, "entity_id", entityID)
	}
}

// parseTaskID splits a "role::entityID" encoded TaskID.
// Returns empty strings if the encoding doesn't match.
func parseTaskID(taskID string) (role, entityID string) {
	parts := strings.SplitN(taskID, taskIDSep, 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
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

	// All planners complete — synthesize outside the lock so the timeout
	// callback can fire during the LLM call (same pattern as determineFocusAreas).
	c.advancePhase(ctx, exec, phaseSynthesizing)
	results := exec.collectResults()
	entityID := exec.EntityID
	traceID := exec.TraceID
	loopID := exec.LoopID
	exec.mu.Unlock()

	llmCtx := ctx
	if traceID != "" || loopID != "" {
		llmCtx = llm.WithTraceContext(ctx, llm.TraceContext{
			TraceID: traceID, LoopID: loopID,
		})
	}
	synthesized, synthLLMID, synthErr := c.synthesizeResults(llmCtx, results)

	exec.mu.Lock()
	if exec.terminated {
		return // Timeout fired while we were synthesizing.
	}
	if synthErr != nil {
		c.markErrorLocked(ctx, exec, fmt.Sprintf("synthesis failed: %v", synthErr))
		return
	}
	c.finishSynthesisLocked(ctx, exec, entityID, synthesized, synthLLMID)
}

// ---------------------------------------------------------------------------
// Synthesis
// ---------------------------------------------------------------------------

// finishSynthesisLocked writes synthesis results and advances to requirement generation.
// Called after the LLM synthesis call completes successfully.
// Caller must hold exec.mu.
func (c *Component) finishSynthesisLocked(ctx context.Context, exec *coordinationExecution, entityID string, synthesized *SynthesizedPlan, synthLLMID string) {
	exec.SynthesizedPlan = synthesized
	exec.SynthesisLLMID = synthLLMID

	_ = c.tripleWriter.WriteTriple(ctx, entityID, "workflow.plan_goal", synthesized.Goal)
	if synthesized.Context != "" {
		_ = c.tripleWriter.WriteTriple(ctx, entityID, "workflow.plan_context", synthesized.Context)
	}
	if scopeJSON, err := json.Marshal(synthesized.Scope); err == nil {
		_ = c.tripleWriter.WriteTriple(ctx, entityID, "workflow.plan_scope", string(scopeJSON))
	}

	c.advancePhase(ctx, exec, phasePlanned)

	c.logger.Info("Plan synthesized, dispatching reviewer (round 1)",
		"entity_id", entityID,
		"slug", exec.Slug,
		"planner_count", exec.ExpectedPlanners,
	)

	c.publishEntity(context.Background(), NewCoordinationEntity(exec).WithPhase(phasePlanned))

	// Dispatch plan reviewer (round 1). On approval, handleReviewerCompleteLocked
	// triggers requirement/scenario generation (round 2).
	exec.ReviewRound = 1
	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, "workflow.coordination.review_round", 1)
	c.dispatchReviewerLocked(ctx, exec)
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
	c.slugIndex.Delete(exec.Slug)
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

	for i, focus := range exec.FocusAreas {
		// TaskID encodes role::entityID for completion routing.
		// Include index to ensure uniqueness when multiple planners run in parallel.
		taskID := fmt.Sprintf("planner.%d%s%s", i, taskIDSep, exec.EntityID)
		exec.PlannerTaskIDs = append(exec.PlannerTaskIDs, taskID)

		// Build the planner request payload with TaskID for LoopCompletedEvent routing.
		req := &payloads.PlannerRequest{
			ExecutionID:      exec.EntityID,
			TaskID:           taskID,
			WorkflowSlug:     WorkflowSlugPlan,
			RequestID:        exec.RequestID,
			Slug:             exec.Slug,
			Title:            exec.Title,
			Description:      exec.Description,
			ProjectID:        exec.ProjectID,
			TraceID:          exec.TraceID,
			LoopID:           exec.LoopID,
			Prompt:           c.buildPlannerPrompt(exec, focus),
			Revision:         exec.Iteration > 0,
			PreviousFindings: exec.ReviewFeedback,
		}

		// Publish typed request to planner's async subject.
		// The planner component emits LoopCompletedEvent directly when done
		// (no separate TaskMessage/agentic-loop needed).
		if err := c.publishBaseMessage(ctx, subjectPlannerAsync, req); err != nil {
			c.logger.Error("Failed to publish planner request",
				"slug", exec.Slug, "focus", focus.Area, "error", err)
			c.markErrorLocked(ctx, exec, fmt.Sprintf("dispatch planner failed: %v", err))
			return
		}

		c.logger.Info("Dispatched planner",
			"slug", exec.Slug,
			"focus", focus.Area,
			"task_id", taskID,
		)
	}
}

// ---------------------------------------------------------------------------
// Pipeline step dispatchers
// ---------------------------------------------------------------------------

// dispatchRequirementGeneratorLocked dispatches the requirement generator
// after plan synthesis completes.
func (c *Component) dispatchRequirementGeneratorLocked(ctx context.Context, exec *coordinationExecution) {
	if exec.terminated {
		return
	}

	c.advancePhase(ctx, exec, phaseGeneratingRequirements)

	req := &payloads.RequirementGeneratorRequest{
		ExecutionID: exec.EntityID,
		Slug:        exec.Slug,
		Title:       exec.Title,
		TraceID:     exec.TraceID,
	}

	if err := c.publishBaseMessage(ctx, subjectReqGeneratorAsync, req); err != nil {
		c.logger.Error("Failed to dispatch requirement generator", "error", err)
		c.markErrorLocked(ctx, exec, fmt.Sprintf("dispatch requirement-generator failed: %v", err))
		return
	}

	c.logger.Info("Dispatched requirement-generator", "slug", exec.Slug)
}

// dispatchScenarioGeneratorLocked dispatches the scenario generator
// after requirements are generated.
func (c *Component) dispatchScenarioGeneratorLocked(ctx context.Context, exec *coordinationExecution) {
	if exec.terminated {
		return
	}

	c.advancePhase(ctx, exec, phaseGeneratingScenarios)

	// Load requirements from disk to fan out one scenario-generator per requirement.
	manager := workflow.NewManager(c.config.RepoPath)
	requirements, err := manager.LoadRequirements(ctx, exec.Slug)
	if err != nil {
		c.markErrorLocked(ctx, exec, fmt.Sprintf("load requirements for scenario generation: %v", err))
		return
	}
	if len(requirements) == 0 {
		c.markErrorLocked(ctx, exec, "no requirements found — cannot generate scenarios")
		return
	}

	for _, requirement := range requirements {
		req := &payloads.ScenarioGeneratorRequest{
			ExecutionID:   exec.EntityID,
			Slug:          exec.Slug,
			RequirementID: requirement.ID,
			TraceID:       exec.TraceID,
		}
		if err := c.publishBaseMessage(ctx, subjectScenGeneratorAsync, req); err != nil {
			c.logger.Error("Failed to dispatch scenario generator",
				"requirement_id", requirement.ID, "error", err)
			c.markErrorLocked(ctx, exec, fmt.Sprintf("dispatch scenario-generator failed: %v", err))
			return
		}
	}

	c.logger.Info("Dispatched scenario-generators",
		"slug", exec.Slug, "requirement_count", len(requirements))
}

// dispatchReviewerLocked dispatches the plan reviewer (el jefe) after
// scenarios are generated.
func (c *Component) dispatchReviewerLocked(ctx context.Context, exec *coordinationExecution) {
	if exec.terminated {
		return
	}

	taskID := fmt.Sprintf("%s%s%s", roleReviewer, taskIDSep, exec.EntityID)

	c.advancePhase(ctx, exec, phaseReviewing)

	req := &payloads.PlanReviewRequest{
		ExecutionID:  exec.EntityID,
		TaskID:       taskID,
		WorkflowSlug: WorkflowSlugPlan,
		RequestID:    exec.RequestID,
		Slug:         exec.Slug,
		ProjectID:    exec.ProjectID,
		TraceID:      exec.TraceID,
		LoopID:       exec.LoopID,
	}

	// Include synthesized plan content if available.
	if exec.SynthesizedPlan != nil {
		if planJSON, err := json.Marshal(exec.SynthesizedPlan); err == nil {
			req.PlanContent = planJSON
		}
	}

	if err := c.publishBaseMessage(ctx, subjectReviewerAsync, req); err != nil {
		c.logger.Error("Failed to dispatch reviewer", "error", err)
		c.markErrorLocked(ctx, exec, fmt.Sprintf("dispatch reviewer failed: %v", err))
		return
	}

	c.logger.Info("Dispatched reviewer (el jefe)",
		"slug", exec.Slug, "task_id", taskID)
}

// ---------------------------------------------------------------------------
// Pipeline step completion handlers
// ---------------------------------------------------------------------------

// handleGeneratorEvent routes generator completion events (from WORKFLOW stream)
// to the appropriate handler based on subject.
func (c *Component) handleGeneratorEvent(ctx context.Context, msg jetstream.Msg) {
	c.updateLastActivity()

	// Extract slug from payload to look up active coordination.
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(msg.Data(), &envelope); err != nil {
		c.logger.Debug("Failed to unmarshal generator event envelope", "error", err)
		_ = msg.Nak()
		return
	}

	var slugHolder struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(envelope.Payload, &slugHolder); err != nil || slugHolder.Slug == "" {
		c.logger.Debug("Generator event missing slug", "subject", msg.Subject())
		_ = msg.Ack()
		return
	}

	entityIDVal, ok := c.slugIndex.Load(slugHolder.Slug)
	if !ok {
		c.logger.Debug("No active coordination for slug", "slug", slugHolder.Slug)
		_ = msg.Ack()
		return
	}
	entityID := entityIDVal.(string)

	execVal, ok := c.activeCoordinations.Load(entityID)
	if !ok {
		_ = msg.Ack()
		return
	}
	exec := execVal.(*coordinationExecution)

	exec.mu.Lock()
	defer exec.mu.Unlock()

	if exec.terminated {
		_ = msg.Ack()
		return
	}

	_ = msg.Ack()

	switch msg.Subject() {
	case subjectReqsGenerated:
		// Guard: only accept if we're actually in the generating_requirements phase.
		if exec.CurrentPhase != phaseGeneratingRequirements {
			c.logger.Debug("Ignoring stale requirements event",
				"current_phase", exec.CurrentPhase, "slug", exec.Slug)
			return
		}
		c.handleReqsGeneratedLocked(ctx, exec, envelope.Payload)
	case subjectScenariosGenerated:
		// Guard: only accept if we're actually in the generating_scenarios phase.
		if exec.CurrentPhase != phaseGeneratingScenarios {
			c.logger.Debug("Ignoring stale/duplicate scenarios event",
				"current_phase", exec.CurrentPhase, "slug", exec.Slug)
			return
		}
		c.handleScenariosGeneratedLocked(ctx, exec, envelope.Payload)
	default:
		c.logger.Debug("Unknown generator event subject", "subject", msg.Subject())
	}
}

// handleReqsGeneratedLocked processes the RequirementsGeneratedEvent.
// Requirements are already saved to disk by the requirement-generator component.
func (c *Component) handleReqsGeneratedLocked(ctx context.Context, exec *coordinationExecution, payload json.RawMessage) {
	var event workflow.RequirementsGeneratedEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		c.logger.Warn("Failed to parse RequirementsGeneratedEvent", "error", err)
	}

	c.logger.Info("Requirements generated",
		"entity_id", exec.EntityID,
		"slug", exec.Slug,
		"requirement_count", event.RequirementCount,
	)

	c.advancePhase(ctx, exec, phaseRequirementsGenerated)

	// Advance to scenario generation.
	c.dispatchScenarioGeneratorLocked(ctx, exec)
}

// handleScenariosGeneratedLocked processes the ScenariosGeneratedEvent.
// Scenarios are already saved to disk by the scenario-generator component.
func (c *Component) handleScenariosGeneratedLocked(ctx context.Context, exec *coordinationExecution, payload json.RawMessage) {
	var event workflow.ScenariosGeneratedEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		c.logger.Warn("Failed to parse ScenariosGeneratedEvent", "error", err)
	}

	c.logger.Info("Scenarios generated",
		"entity_id", exec.EntityID,
		"slug", exec.Slug,
		"scenario_count", event.ScenarioCount,
	)

	c.advancePhase(ctx, exec, phaseScenariosGenerated)

	// Dispatch reviewer (round 2) — reviews requirements + scenarios together.
	exec.ReviewRound = 2
	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, "workflow.coordination.review_round", 2)
	c.dispatchReviewerLocked(ctx, exec)
}

// handleReviewerCompleteLocked processes reviewer (el jefe) completion.
func (c *Component) handleReviewerCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *coordinationExecution) {
	if exec.terminated {
		return
	}

	// Parse the reviewer verdict from the result.
	verdict, summary := c.parseReviewerVerdict(event.Result)

	c.logger.Info("Reviewer completed",
		"entity_id", exec.EntityID,
		"slug", exec.Slug,
		"verdict", verdict,
		"summary", summary,
	)

	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, "workflow.review.verdict", verdict)
	if summary != "" {
		_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, "workflow.review.summary", summary)
	}

	switch verdict {
	case "approved":
		c.handleReviewApprovalLocked(ctx, exec, verdict, summary)

	case "needs_changes":
		c.handleReviewRejectionLocked(ctx, exec, summary)

	default:
		c.logger.Warn("Unknown reviewer verdict, treating as error",
			"verdict", verdict, "slug", exec.Slug)
		c.markErrorLocked(ctx, exec, fmt.Sprintf("unknown reviewer verdict: %s", verdict))
	}
}

// handleReviewApprovalLocked processes an "approved" verdict from the reviewer.
// Round 1 approval triggers requirement/scenario generation (round 2).
// Round 2 approval means the plan is complete and ready for execution.
// Caller must hold exec.mu.
func (c *Component) handleReviewApprovalLocked(ctx context.Context, exec *coordinationExecution, verdict, summary string) {
	if exec.ReviewRound <= 1 {
		// Round 1: plan approved — start requirement/scenario generation.
		if c.config.AutoApprove {
			c.logger.Info("Round 1 approved (auto), starting requirement generation",
				"slug", exec.Slug)
			c.advancePhase(ctx, exec, phaseApproved)
			c.publishPlanApprovedEvent(ctx, exec, verdict, summary)
			c.dispatchRequirementGeneratorLocked(ctx, exec)
		} else {
			// Human gate: pause for human to review/CRUD plan before approving.
			c.advancePhase(ctx, exec, phaseAwaitingHuman)
			c.logger.Info("Round 1 awaiting human approval (plan review)",
				"slug", exec.Slug, "iteration", exec.Iteration)
			exec.terminated = true
			c.coordinationsCompleted.Add(1)
			c.cleanupExecutionLocked(exec)
		}
	} else {
		// Round 2: requirements + scenarios approved — plan is complete.
		c.advancePhase(ctx, exec, phaseApproved)
		c.publishPlanApprovedEvent(ctx, exec, verdict, summary)

		c.logger.Info("Round 2 approved, plan ready for execution",
			"slug", exec.Slug)

		exec.terminated = true
		c.coordinationsCompleted.Add(1)
		c.cleanupExecutionLocked(exec)
	}
}

// handleReviewRejectionLocked processes a "needs_changes" verdict.
// Round 1 rejection retries planning. Round 2 rejection retries
// requirement/scenario generation.
// Caller must hold exec.mu.
func (c *Component) handleReviewRejectionLocked(ctx context.Context, exec *coordinationExecution, summary string) {
	exec.Iteration++
	exec.ReviewFeedback = summary
	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Iteration, exec.Iteration)

	if exec.Iteration >= c.config.MaxReviewIterations {
		c.logger.Warn("Review budget exhausted, escalating",
			"slug", exec.Slug,
			"round", exec.ReviewRound,
			"iteration", exec.Iteration,
			"max", c.config.MaxReviewIterations,
		)
		c.advancePhase(ctx, exec, phaseEscalated)
		exec.terminated = true
		c.coordinationsFailed.Add(1)
		c.cleanupExecutionLocked(exec)
		return
	}

	if exec.ReviewRound <= 1 {
		// Round 1 rejection: retry planning.
		c.logger.Info("Round 1 reviewer requested changes, retrying planners",
			"slug", exec.Slug,
			"iteration", exec.Iteration,
			"max", c.config.MaxReviewIterations,
		)
		c.advancePhase(ctx, exec, phasePlanning)
		exec.CompletedResults = make(map[string]*workflow.PlannerResult)
		exec.PlannerTaskIDs = nil
		c.dispatchPlannersLocked(ctx, exec)
	} else {
		// Round 2 rejection: retry requirement/scenario generation.
		c.logger.Info("Round 2 reviewer requested changes, retrying requirement generation",
			"slug", exec.Slug,
			"iteration", exec.Iteration,
			"max", c.config.MaxReviewIterations,
		)
		c.advancePhase(ctx, exec, phaseGeneratingRequirements)
		c.dispatchRequirementGeneratorLocked(ctx, exec)
	}
}

// publishPlanApprovedEvent sends a typed PlanApprovedEvent to the WORKFLOW stream
// so plan-api can update the plan file on disk.
func (c *Component) publishPlanApprovedEvent(ctx context.Context, exec *coordinationExecution, verdict, summary string) {
	event := &workflow.PlanApprovedEvent{
		Slug:    exec.Slug,
		Verdict: verdict,
		Summary: summary,
	}

	subject := workflow.PlanApproved.Pattern

	// Wrap event in BaseMessage envelope for ParseReactivePayload compatibility.
	// We construct the envelope manually because PlanApprovedEvent doesn't implement
	// message.Payload. The ParseReactivePayload function only reads the "payload" field.
	payloadData, err := json.Marshal(event)
	if err != nil {
		c.logger.Error("Failed to marshal PlanApprovedEvent", "error", err)
		return
	}

	envelope := struct {
		ID      string          `json:"id"`
		Type    message.Type    `json:"type"`
		Payload json.RawMessage `json:"payload"`
		Meta    struct {
			CreatedAt int64  `json:"created_at"`
			Source    string `json:"source"`
		} `json:"meta"`
	}{
		ID:      fmt.Sprintf("plan-approved-%s", exec.Slug),
		Type:    message.Type{Domain: "workflow", Category: "plan-approved", Version: "v1"},
		Payload: payloadData,
		Meta: struct {
			CreatedAt int64  `json:"created_at"`
			Source    string `json:"source"`
		}{
			CreatedAt: time.Now().UnixMilli(),
			Source:    componentName,
		},
	}

	envelopeData, err := json.Marshal(envelope)
	if err != nil {
		c.logger.Error("Failed to marshal BaseMessage envelope", "error", err)
		return
	}

	if err := c.natsClient.PublishToStream(ctx, subject, envelopeData); err != nil {
		c.logger.Error("Failed to publish PlanApprovedEvent",
			"subject", subject, "slug", exec.Slug, "error", err)
		return
	}

	c.logger.Info("Published PlanApprovedEvent",
		"slug", exec.Slug, "subject", subject)
}

// triggerExecution publishes a scenario orchestration trigger to start the
// execution phase. The scenario-orchestrator picks this up and dispatches
// per-scenario execution.
func (c *Component) triggerExecution(ctx context.Context, exec *coordinationExecution) {
	subject := fmt.Sprintf("scenario.orchestrate.%s", exec.Slug)

	// Typed trigger — scenario-orchestrator loads scenarios from disk.
	trigger := &payloads.ScenarioOrchestrationTrigger{
		PlanSlug: exec.Slug,
		TraceID:  exec.TraceID,
	}

	baseMsg := message.NewBaseMessage(trigger.Schema(), trigger, componentName)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Error("Failed to marshal execution trigger", "error", err)
		return
	}

	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		c.logger.Error("Failed to trigger execution",
			"subject", subject, "slug", exec.Slug, "error", err)
		return
	}

	c.logger.Info("Triggered execution phase",
		"slug", exec.Slug, "subject", subject)
}

// parseReviewerVerdict extracts verdict and summary from a reviewer result.
func (c *Component) parseReviewerVerdict(result string) (verdict, summary string) {
	var parsed struct {
		Verdict string `json:"verdict"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		c.logger.Error("Failed to parse reviewer result, escalating", "error", err)
		return "escalated", fmt.Sprintf("reviewer result parse failed: %v", err)
	}
	if parsed.Verdict == "" {
		c.logger.Warn("Reviewer returned empty verdict, escalating")
		return "escalated", "reviewer returned empty verdict"
	}
	return parsed.Verdict, parsed.Summary
}

// ---------------------------------------------------------------------------
// Planner prompt helpers
// ---------------------------------------------------------------------------

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
// Uses explicit focuses if provided, otherwise calls the LLM.
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

	// Use LLM to determine focus areas.
	provider := c.resolveProvider()
	assembled := c.assembler.Assemble(&prompt.AssemblyContext{
		Role:     prompt.RolePlanCoordinator,
		Provider: provider,
		Domain:   "software",
	})
	systemPrompt := assembled.SystemMessage

	userPrompt := c.buildFocusUserPrompt(trigger, "")

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

// advancePhase writes a phase triple and updates the in-memory CurrentPhase tracker.
// Caller must hold exec.mu if exec is shared.
func (c *Component) advancePhase(ctx context.Context, exec *coordinationExecution, phase string) {
	exec.CurrentPhase = phase
	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phase); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phase, "error", err)
	}
}

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
