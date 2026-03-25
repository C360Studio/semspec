// coordinator provides a plan-coordination pipeline embedded within plan-api.
// It orchestrates a fan-out/fan-in pipeline:
//
//  1. Receives a coordination trigger (via NATS or direct call).
//  2. Determines focus areas (via explicit list or LLM-driven analysis).
//  3. Dispatches N planner agents in parallel (one per focus area).
//  4. Collects planner results via agentic.loop_completed events.
//  5. When all planners complete, synthesizes results into a unified plan.
//  6. Writes terminal phase triple — rules handle status transitions.
//
// State lives as entity triples in ENTITY_STATES. No typed Go structs are
// stored in KV — the coordinator keeps lightweight in-memory tracking (sync.Map)
// for routing completion events back to the correct coordination execution.
package planmanager

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

	"github.com/c360studio/semspec/graph"
	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/model"
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

func init() {
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "coordinator-result",
		Version:     "v1",
		Description: "Plan coordinator result with planner count and status",
		Factory:     func() any { return &CoordinatorResult{} },
	}); err != nil {
		panic("failed to register coordinator result payload: " + err.Error())
	}
}

const (
	// WorkflowSlugPlan identifies plan phase events in LoopCompletedEvent.
	WorkflowSlugPlan = "semspec-plan"

	// Phase values written to entity triples.
	// The coordinator advances through these phases sequentially.
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

// coordinatorLLMCompleter is the subset of the LLM client used by the coordinator.
type coordinatorLLMCompleter interface {
	Complete(ctx context.Context, req llm.Request) (*llm.Response, error)
}

// coordinatorConsumerInfo tracks a JetStream consumer created during Start so it can be
// stopped cleanly via StopConsumer rather than context cancellation.
type coordinatorConsumerInfo struct {
	streamName   string
	consumerName string
}

// CoordinatorConfig holds coordinator-specific configuration fields.
// It is a subset of the plan-api Config, extracted for clarity.
type CoordinatorConfig struct {
	MaxConcurrentPlanners    int
	TimeoutSeconds           int
	MaxReviewIterations      int
	AutoApprove              *bool
	Model                    string
	DefaultCapability        string
	RepoPath                 string
	SemsourceReadinessBudget string
	Prompts                  *PromptsConfig
}

// IsAutoApprove returns whether the human approval gate should be skipped.
func (c *CoordinatorConfig) IsAutoApprove() bool {
	if c.AutoApprove == nil {
		return true
	}
	return *c.AutoApprove
}

// GetTimeout returns the coordination timeout as a duration.
func (c *CoordinatorConfig) GetTimeout() time.Duration {
	if c.TimeoutSeconds <= 0 {
		return 30 * time.Minute
	}
	return time.Duration(c.TimeoutSeconds) * time.Second
}

// GetSemsourceReadinessBudget returns the parsed semsource readiness budget.
func (c *CoordinatorConfig) GetSemsourceReadinessBudget() time.Duration {
	if c.SemsourceReadinessBudget == "" {
		return 2 * time.Second
	}
	d, err := time.ParseDuration(c.SemsourceReadinessBudget)
	if err != nil || d <= 0 {
		return 2 * time.Second
	}
	return d
}

// coordinator orchestrates parallel planner coordination.
// It is unexported — internal to plan-api.
type coordinator struct {
	config       CoordinatorConfig
	natsClient   *natsclient.Client
	logger       *slog.Logger
	tripleWriter *graphutil.TripleWriter
	plans        *planStore

	llmClient     coordinatorLLMCompleter
	modelRegistry *model.Registry
	assembler     *prompt.Assembler

	// activeCoordinations maps entityID → *coordinationExecution.
	activeCoordinations sync.Map

	// slugIndex maps slug → entityID for routing generator completion events
	// (generators publish events with slug, not entityID).
	slugIndex sync.Map

	// Lifecycle
	consumerInfos []coordinatorConsumerInfo
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

// newCoordinator creates a new coordinator with explicit dependencies.
func newCoordinator(cfg CoordinatorConfig, natsClient *natsclient.Client, logger *slog.Logger) *coordinator {
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("component", coordinatorName)

	// Apply defaults.
	if cfg.MaxConcurrentPlanners <= 0 {
		cfg.MaxConcurrentPlanners = 3
	}
	if cfg.MaxReviewIterations <= 0 {
		cfg.MaxReviewIterations = 3
	}
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 1800
	}
	if cfg.Model == "" {
		cfg.Model = "default"
	}
	if cfg.DefaultCapability == "" {
		cfg.DefaultCapability = "planning"
	}
	if cfg.RepoPath == "" {
		cfg.RepoPath = os.Getenv("SEMSPEC_REPO_PATH")
		if cfg.RepoPath == "" {
			cfg.RepoPath = "."
		}
	}
	if cfg.AutoApprove == nil {
		defaultTrue := true
		cfg.AutoApprove = &defaultTrue
	}

	// Initialize prompt assembler with software domain.
	registry := prompt.NewRegistry()
	registry.RegisterAll(promptdomain.Software()...)
	registry.Register(prompt.ToolGuidanceFragment(prompt.DefaultToolGuidance()))
	assembler := prompt.NewAssembler(registry)

	return &coordinator{
		config:        cfg,
		natsClient:    natsClient,
		logger:        logger,
		llmClient:     llm.NewClient(model.Global(), llm.WithLogger(logger)),
		modelRegistry: model.Global(),
		assembler:     assembler,
		tripleWriter: &graphutil.TripleWriter{
			NATSClient:    natsClient,
			Logger:        logger,
			ComponentName: coordinatorName,
		},
	}
}

// Start begins consuming trigger events and loop-completion events from NATS.
// It also reconciles any in-flight coordinations from graph state.
func (co *coordinator) Start(ctx context.Context) error {
	co.lifecycleMu.Lock()
	defer co.lifecycleMu.Unlock()

	co.mu.RLock()
	if co.running {
		co.mu.RUnlock()
		return nil
	}
	co.mu.RUnlock()

	co.logger.Info("Starting plan-coordinator")

	// Reconcile: recover in-flight coordinations from graph state.
	co.reconcileFromGraph(ctx)

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
	if err := co.natsClient.ConsumeStreamWithConfig(ctx, triggerCfg, co.handleTrigger); err != nil {
		return fmt.Errorf("consume coordination triggers: %w", err)
	}
	co.consumerInfos = append(co.consumerInfos, coordinatorConsumerInfo{
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
	if err := co.natsClient.ConsumeStreamWithConfig(ctx, completionCfg, co.handleLoopCompleted); err != nil {
		for _, info := range co.consumerInfos {
			co.natsClient.StopConsumer(info.streamName, info.consumerName)
		}
		co.consumerInfos = nil
		return fmt.Errorf("consume loop completions: %w", err)
	}
	co.consumerInfos = append(co.consumerInfos, coordinatorConsumerInfo{
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
	if err := co.natsClient.ConsumeStreamWithConfig(ctx, reqsCfg, co.handleGeneratorEvent); err != nil {
		for _, info := range co.consumerInfos {
			co.natsClient.StopConsumer(info.streamName, info.consumerName)
		}
		co.consumerInfos = nil
		return fmt.Errorf("consume requirements generated events: %w", err)
	}
	co.consumerInfos = append(co.consumerInfos, coordinatorConsumerInfo{
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
	if err := co.natsClient.ConsumeStreamWithConfig(ctx, scenCfg, co.handleGeneratorEvent); err != nil {
		for _, info := range co.consumerInfos {
			co.natsClient.StopConsumer(info.streamName, info.consumerName)
		}
		co.consumerInfos = nil
		return fmt.Errorf("consume scenarios generated events: %w", err)
	}
	co.consumerInfos = append(co.consumerInfos, coordinatorConsumerInfo{
		streamName:   scenCfg.StreamName,
		consumerName: scenCfg.ConsumerName,
	})

	co.mu.Lock()
	co.running = true
	co.mu.Unlock()

	return nil
}

// Stop performs graceful shutdown.
func (co *coordinator) Stop() {
	co.lifecycleMu.Lock()
	defer co.lifecycleMu.Unlock()

	co.mu.RLock()
	if !co.running {
		co.mu.RUnlock()
		return
	}
	co.mu.RUnlock()

	co.logger.Info("Stopping plan-coordinator",
		"triggers_processed", co.triggersProcessed.Load(),
		"coordinations_completed", co.coordinationsCompleted.Load(),
		"coordinations_failed", co.coordinationsFailed.Load(),
	)

	for _, info := range co.consumerInfos {
		co.natsClient.StopConsumer(info.streamName, info.consumerName)
	}
	co.consumerInfos = nil

	done := make(chan struct{})
	go func() {
		co.wg.Wait()
		close(done)
	}()

	timeout := 10 * time.Second
	select {
	case <-done:
		co.logger.Debug("All in-flight handlers drained")
	case <-time.After(timeout):
		co.logger.Warn("Timed out waiting for in-flight handlers to drain")
	}

	// Cancel any active coordination timeouts.
	co.activeCoordinations.Range(func(_, value any) bool {
		exec := value.(*coordinationExecution)
		exec.mu.Lock()
		if exec.timeoutTimer != nil {
			exec.timeoutTimer.stop()
		}
		exec.mu.Unlock()
		return true
	})

	co.mu.Lock()
	co.running = false
	co.mu.Unlock()
}

// Cancel terminates an active coordination by slug with the given reason.
// This is called when a plan is promoted via the REST API.
func (co *coordinator) Cancel(slug, reason string) {
	if co == nil {
		return
	}
	entityIDVal, ok := co.slugIndex.Load(slug)
	if !ok {
		return
	}
	entityID := entityIDVal.(string)

	execVal, ok := co.activeCoordinations.Load(entityID)
	if !ok {
		return
	}
	exec := execVal.(*coordinationExecution)

	exec.mu.Lock()
	defer exec.mu.Unlock()

	if exec.terminated {
		return
	}

	co.markErrorLocked(context.Background(), exec, reason)
}

// StartCoordination initiates a coordination pipeline directly (in-process),
// bypassing NATS. Used by the plan-api HTTP handler on plan creation.
func (co *coordinator) StartCoordination(
	ctx context.Context,
	slug, title, description, projectID, traceID, loopID, requestID string,
	focusAreas []string,
) {
	entityID := fmt.Sprintf("%s.exec.plan.run.%s", workflow.EntityPrefix(), slug)

	exec := &coordinationExecution{
		EntityID:         entityID,
		Slug:             slug,
		Title:            title,
		Description:      description,
		ProjectID:        projectID,
		TraceID:          traceID,
		LoopID:           loopID,
		RequestID:        requestID,
		CompletedResults: make(map[string]*workflow.PlannerResult),
	}

	// Deduplicate: if a coordination for this entityID already exists, skip.
	co.slugIndex.Store(slug, entityID)
	if _, loaded := co.activeCoordinations.LoadOrStore(entityID, exec); loaded {
		co.logger.Debug("Duplicate StartCoordination for active coordination, skipping",
			"entity_id", entityID,
		)
		return
	}

	co.triggersProcessed.Add(1)
	co.updateLastActivity()

	// Build a synthetic trigger for shared pipeline logic.
	trigger := &payloads.PlanCoordinatorRequest{
		RequestID:   requestID,
		Slug:        slug,
		Title:       title,
		Description: description,
		ProjectID:   projectID,
		TraceID:     traceID,
		LoopID:      loopID,
		FocusAreas:  focusAreas,
	}

	// All coordination work uses a background context so it is not bounded by
	// the caller's HTTP request context.
	workCtx, workCancel := context.WithTimeout(context.Background(), co.config.GetTimeout())

	co.wg.Add(1)
	go func() {
		defer co.wg.Done()
		defer workCancel()
		co.runCoordinationPipeline(workCtx, exec, trigger)
	}()
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
func (co *coordinator) reconcileFromGraph(ctx context.Context) {
	reconcileCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	entities, err := co.tripleWriter.ReadEntitiesByPrefix(reconcileCtx,
		workflow.EntityPrefix()+".exec.plan.run.", 100)
	if err != nil {
		co.logger.Info("No graph state to reconcile (expected on first start)",
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

		co.activeCoordinations.Store(entityID, exec)
		if slug != "" {
			co.slugIndex.Store(slug, entityID)
		}
		recovered++

		co.logger.Info("Recovered coordination from graph",
			"entity_id", entityID,
			"slug", slug,
			"phase", phase,
			"review_round", exec.ReviewRound,
		)
	}

	if recovered > 0 {
		co.logger.Info("Coordination reconciliation complete",
			"recovered", recovered,
			"total_entities", len(entities))
	}
}

// ---------------------------------------------------------------------------
// Trigger handler (NATS entry point — kept for backward compatibility)
// ---------------------------------------------------------------------------

// handleTrigger parses a coordination trigger from NATS, determines focus areas, and
// dispatches N planner agents in parallel.
func (co *coordinator) handleTrigger(ctx context.Context, msg jetstream.Msg) {
	co.triggersProcessed.Add(1)
	co.updateLastActivity()

	trigger, err := payloads.ParseReactivePayload[payloads.PlanCoordinatorRequest](msg.Data())
	if err != nil {
		co.logger.Error("Failed to parse coordination trigger", "error", err)
		co.errors.Add(1)
		_ = msg.Nak()
		return
	}

	if trigger.Slug == "" {
		co.logger.Error("Trigger missing slug")
		co.errors.Add(1)
		_ = msg.Nak()
		return
	}
	if strings.Contains(trigger.Slug, taskIDSep) {
		co.logger.Error("Slug contains reserved separator", "slug", trigger.Slug, "separator", taskIDSep)
		co.errors.Add(1)
		_ = msg.Nak()
		return
	}

	// Best-effort semsource readiness check.
	if reg := graph.GlobalRegistry(); reg != nil && reg.SemsourceConfigured() {
		budget := co.config.GetSemsourceReadinessBudget()
		gateCtx, gateCancel := context.WithTimeout(context.Background(), budget)
		if err := reg.WaitForSemsource(gateCtx, budget); err != nil {
			co.logger.Warn("Semsource not ready, proceeding with local graph only",
				"error", err, "slug", trigger.Slug, "budget", budget)
		} else {
			co.logger.Info("Semsource ready", "slug", trigger.Slug)
		}
		gateCancel()
	}

	entityID := fmt.Sprintf("%s.exec.plan.run.%s", workflow.EntityPrefix(), trigger.Slug)

	co.logger.Info("Coordination trigger received",
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
	co.slugIndex.Store(trigger.Slug, entityID)
	if _, loaded := co.activeCoordinations.LoadOrStore(entityID, exec); loaded {
		co.logger.Debug("Duplicate trigger for active coordination, skipping",
			"entity_id", entityID,
		)
		_ = msg.Ack()
		return
	}

	// Ack immediately — coordination work (LLM calls) runs well beyond 30s AckWait.
	_ = msg.Ack()

	// All coordination work uses a background context so it is not bounded by
	// the JetStream message delivery context or the 30s AckWait.
	workCtx, workCancel := context.WithTimeout(context.Background(), co.config.GetTimeout())

	co.wg.Add(1)
	go func() {
		defer co.wg.Done()
		defer workCancel()
		co.runCoordinationPipeline(workCtx, exec, trigger)
	}()
}

// runCoordinationPipeline executes the full coordination pipeline starting from
// focus area determination. Called by both handleTrigger and StartCoordination.
func (co *coordinator) runCoordinationPipeline(ctx context.Context, exec *coordinationExecution, trigger *payloads.PlanCoordinatorRequest) {
	entityID := exec.EntityID

	// Write initial entity triples.
	_ = co.tripleWriter.WriteTriple(ctx, entityID, wf.Type, "coordination")
	if err := co.tripleWriter.WriteTriple(ctx, entityID, wf.Phase, phaseFocusing); err != nil {
		co.logger.Error("Failed to write phase triple", "phase", phaseFocusing, "error", err)
	}
	_ = co.tripleWriter.WriteTriple(ctx, entityID, wf.Slug, trigger.Slug)
	_ = co.tripleWriter.WriteTriple(ctx, entityID, wf.Title, trigger.Title)
	_ = co.tripleWriter.WriteTriple(ctx, entityID, wf.ProjectID, trigger.ProjectID)
	_ = co.tripleWriter.WriteTriple(ctx, entityID, wf.TraceID, trigger.TraceID)

	// Publish initial entity snapshot for graph observability.
	co.publishEntity(ctx, NewCoordinationEntity(exec).WithPhase(phaseFocusing))

	// Determine focus areas BEFORE acquiring exec.mu — the LLM call can take
	// 30+ seconds and we don't want to block the timeout callback.
	llmCtx := ctx
	if trigger.TraceID != "" || trigger.LoopID != "" {
		llmCtx = llm.WithTraceContext(ctx, llm.TraceContext{
			TraceID: trigger.TraceID,
			LoopID:  trigger.LoopID,
		})
	}

	focuses, err := co.determineFocusAreas(llmCtx, trigger)
	if err != nil {
		co.logger.Error("Focus determination failed",
			"entity_id", entityID,
			"slug", trigger.Slug,
			"error", err,
		)
		exec.mu.Lock()
		co.markErrorLocked(ctx, exec, fmt.Sprintf("focus determination failed: %v", err))
		exec.mu.Unlock()
		return
	}

	// Lock for timeout + dispatch — no shared state was modified above.
	exec.mu.Lock()
	defer exec.mu.Unlock()

	co.startExecutionTimeoutLocked(exec)

	exec.FocusAreas = focuses
	if err := co.tripleWriter.WriteTriple(ctx, entityID, wf.Phase, phasePlanning); err != nil {
		co.logger.Error("Failed to write phase triple", "phase", phasePlanning, "error", err)
	}
	_ = co.tripleWriter.WriteTriple(ctx, entityID, "workflow.focus_areas", focusAreasJSON(focuses))
	_ = co.tripleWriter.WriteTriple(ctx, entityID, "workflow.planner_count", len(focuses))

	co.logger.Info("Focus areas determined, dispatching planners",
		"entity_id", entityID,
		"focus_count", len(focuses),
	)

	// Dispatch N planner agents.
	co.dispatchPlannersLocked(ctx, exec)
}

// ---------------------------------------------------------------------------
// Loop-completion handler
// ---------------------------------------------------------------------------

func (co *coordinator) handleLoopCompleted(ctx context.Context, msg jetstream.Msg) {
	var base message.BaseMessage
	if err := json.Unmarshal(msg.Data(), &base); err != nil {
		co.logger.Debug("Failed to unmarshal loop completed envelope", "error", err)
		co.errors.Add(1)
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
	role, entityID := parseCoordTaskID(event.TaskID)
	if entityID == "" {
		// Not our event — TaskID doesn't use our encoding.
		_ = msg.Ack()
		return
	}

	co.updateLastActivity()

	execVal, ok := co.activeCoordinations.Load(entityID)
	if !ok {
		co.logger.Debug("No active coordination for entity", "entity_id", entityID)
		_ = msg.Ack()
		return
	}
	exec := execVal.(*coordinationExecution)

	exec.mu.Lock()
	defer exec.mu.Unlock()

	_ = msg.Ack()

	switch {
	case strings.HasPrefix(role, rolePlanner):
		co.handlePlannerCompleteLocked(ctx, event, exec)
	case role == roleReviewer:
		co.handleReviewerCompleteLocked(ctx, event, exec)
	default:
		co.logger.Debug("Unknown completion role", "role", role, "entity_id", entityID)
	}
}

// parseCoordTaskID splits a "role::entityID" encoded TaskID.
// Returns empty strings if the encoding doesn't match.
func parseCoordTaskID(taskID string) (role, entityID string) {
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
func (co *coordinator) handlePlannerCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *coordinationExecution) {
	// Guard against racing with timeout — if already terminated, skip.
	if exec.terminated {
		return
	}

	// Parse the planner result.
	result, llmRequestIDs := co.parsePlannerResult(event.Result, event.TaskID)
	if result != nil {
		exec.CompletedResults[event.TaskID] = result
	}
	exec.LLMRequestIDs = append(exec.LLMRequestIDs, llmRequestIDs...)

	co.logger.Info("Planner completed",
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
	// callback can fire during the LLM call.
	co.advancePhase(ctx, exec, phaseSynthesizing)
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
	synthesized, synthLLMID, synthErr := co.synthesizeResults(llmCtx, results)

	exec.mu.Lock()
	if exec.terminated {
		return // Timeout fired while we were synthesizing.
	}
	if synthErr != nil {
		co.markErrorLocked(ctx, exec, fmt.Sprintf("synthesis failed: %v", synthErr))
		return
	}
	co.finishSynthesisLocked(ctx, exec, entityID, synthesized, synthLLMID)
}

// ---------------------------------------------------------------------------
// Synthesis
// ---------------------------------------------------------------------------

// finishSynthesisLocked writes synthesis results and advances to requirement generation.
// Caller must hold exec.mu.
func (co *coordinator) finishSynthesisLocked(ctx context.Context, exec *coordinationExecution, entityID string, synthesized *SynthesizedPlan, synthLLMID string) {
	exec.SynthesizedPlan = synthesized
	exec.SynthesisLLMID = synthLLMID

	_ = co.tripleWriter.WriteTriple(ctx, entityID, "workflow.plan_goal", synthesized.Goal)
	if synthesized.Context != "" {
		_ = co.tripleWriter.WriteTriple(ctx, entityID, "workflow.plan_context", synthesized.Context)
	}
	if scopeJSON, err := json.Marshal(synthesized.Scope); err == nil {
		_ = co.tripleWriter.WriteTriple(ctx, entityID, "workflow.plan_scope", string(scopeJSON))
	}

	// Write synthesized plan content to plan.json (single writer for Goal/Context/Scope).
	// The coordinator is the authoritative writer for these fields; the planner no longer writes to disk.
	if plan, ok := co.plans.get(exec.Slug); ok {
		plan.Goal = synthesized.Goal
		plan.Context = synthesized.Context
		plan.Scope = synthesized.Scope
		if saveErr := co.plans.save(ctx, plan); saveErr != nil {
			co.logger.Warn("Failed to save synthesized plan",
				"slug", exec.Slug, "error", saveErr)
		}
	} else {
		co.logger.Warn("Plan not found in cache for synthesis save",
			"slug", exec.Slug)
	}

	co.advancePhase(ctx, exec, phasePlanned)

	co.logger.Info("Plan synthesized, dispatching reviewer (round 1)",
		"entity_id", entityID,
		"slug", exec.Slug,
		"planner_count", exec.ExpectedPlanners,
	)

	co.publishEntity(context.Background(), NewCoordinationEntity(exec).WithPhase(phasePlanned))

	// Dispatch plan reviewer (round 1).
	exec.ReviewRound = 1
	_ = co.tripleWriter.WriteTriple(ctx, exec.EntityID, "workflow.coordination.review_round", 1)
	co.dispatchReviewerLocked(ctx, exec)
}

// ---------------------------------------------------------------------------
// Terminal state handlers
// ---------------------------------------------------------------------------

// markErrorLocked transitions to the error terminal state.
// Caller must hold exec.mu.
func (co *coordinator) markErrorLocked(ctx context.Context, exec *coordinationExecution, reason string) {
	if exec.terminated {
		return
	}
	exec.terminated = true

	if err := co.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseError); err != nil {
		co.logger.Error("Failed to write phase triple", "phase", phaseError, "error", err)
	}
	_ = co.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.ErrorReason, reason)

	co.errors.Add(1)
	co.coordinationsFailed.Add(1)

	co.logger.Error("Coordination failed",
		"entity_id", exec.EntityID,
		"slug", exec.Slug,
		"reason", reason,
	)

	co.publishEntity(context.Background(), NewCoordinationEntity(exec).WithPhase(phaseError).WithErrorReason(reason))
	co.cleanupExecutionLocked(exec)
}

// cleanupExecutionLocked removes execution from maps and cancels timeout.
// Caller must hold exec.mu.
func (co *coordinator) cleanupExecutionLocked(exec *coordinationExecution) {
	if exec.timeoutTimer != nil {
		exec.timeoutTimer.stop()
	}
	co.slugIndex.Delete(exec.Slug)
	co.activeCoordinations.Delete(exec.EntityID)
}

// ---------------------------------------------------------------------------
// Per-execution timeout
// ---------------------------------------------------------------------------

// startExecutionTimeoutLocked starts a timer that marks the execution as errored
// if it does not complete within the configured timeout.
//
// Caller must hold exec.mu.
func (co *coordinator) startExecutionTimeoutLocked(exec *coordinationExecution) {
	timeout := co.config.GetTimeout()

	timer := time.AfterFunc(timeout, func() {
		co.logger.Warn("Coordination timed out",
			"entity_id", exec.EntityID,
			"slug", exec.Slug,
			"timeout", timeout,
		)
		exec.mu.Lock()
		defer exec.mu.Unlock()
		co.markErrorLocked(context.Background(), exec, fmt.Sprintf("coordination timed out after %s", timeout))
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
func (co *coordinator) dispatchPlannersLocked(ctx context.Context, exec *coordinationExecution) {
	exec.ExpectedPlanners = len(exec.FocusAreas)
	exec.PlannerTaskIDs = make([]string, 0, exec.ExpectedPlanners)

	for i, focus := range exec.FocusAreas {
		taskID := fmt.Sprintf("planner.%d%s%s", i, taskIDSep, exec.EntityID)
		exec.PlannerTaskIDs = append(exec.PlannerTaskIDs, taskID)

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
			Prompt:           co.buildPlannerPrompt(exec, focus),
			Revision:         exec.Iteration > 0,
			PreviousFindings: exec.ReviewFeedback,
		}

		// On revision, populate PreviousPlanJSON so the planner doesn't need to
		// read plan.json from disk. The synthesized plan from the previous iteration
		// is what the reviewer rejected — provide it as context for the retry.
		if exec.Iteration > 0 && exec.SynthesizedPlan != nil {
			if planJSON, err := json.Marshal(exec.SynthesizedPlan); err == nil {
				req.PreviousPlanJSON = string(planJSON)
			}
		}

		if err := co.publishBaseMessage(ctx, subjectPlannerAsync, req); err != nil {
			co.logger.Error("Failed to publish planner request",
				"slug", exec.Slug, "focus", focus.Area, "error", err)
			co.markErrorLocked(ctx, exec, fmt.Sprintf("dispatch planner failed: %v", err))
			return
		}

		co.logger.Info("Dispatched planner",
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
func (co *coordinator) dispatchRequirementGeneratorLocked(ctx context.Context, exec *coordinationExecution) {
	if exec.terminated {
		return
	}

	co.advancePhase(ctx, exec, phaseGeneratingRequirements)

	req := &payloads.RequirementGeneratorRequest{
		ExecutionID: exec.EntityID,
		Slug:        exec.Slug,
		Title:       exec.Title,
		TraceID:     exec.TraceID,
	}

	// Populate plan content from synthesized plan so downstream components
	// don't need to read plan.json from disk.
	if exec.SynthesizedPlan != nil {
		req.Goal = exec.SynthesizedPlan.Goal
		req.Context = exec.SynthesizedPlan.Context
		req.Scope = &exec.SynthesizedPlan.Scope
	}

	if err := co.publishBaseMessage(ctx, subjectReqGeneratorAsync, req); err != nil {
		co.logger.Error("Failed to dispatch requirement generator", "error", err)
		co.markErrorLocked(ctx, exec, fmt.Sprintf("dispatch requirement-generator failed: %v", err))
		return
	}

	co.logger.Info("Dispatched requirement-generator", "slug", exec.Slug)
}

// dispatchScenarioGeneratorLocked dispatches the scenario generator
// after requirements are generated.
func (co *coordinator) dispatchScenarioGeneratorLocked(ctx context.Context, exec *coordinationExecution) {
	if exec.terminated {
		return
	}

	co.advancePhase(ctx, exec, phaseGeneratingScenarios)

	// Load requirements from disk to fan out one scenario-generator per requirement.
	requirements, err := workflow.LoadRequirements(ctx, co.tripleWriter, exec.Slug)
	if err != nil {
		co.markErrorLocked(ctx, exec, fmt.Sprintf("load requirements for scenario generation: %v", err))
		return
	}
	if len(requirements) == 0 {
		co.markErrorLocked(ctx, exec, "no requirements found — cannot generate scenarios")
		return
	}

	for _, requirement := range requirements {
		req := &payloads.ScenarioGeneratorRequest{
			ExecutionID:   exec.EntityID,
			Slug:          exec.Slug,
			RequirementID: requirement.ID,
			TraceID:       exec.TraceID,
		}

		// Populate plan content so scenario-generator doesn't need to read plan.json.
		if exec.SynthesizedPlan != nil {
			req.PlanGoal = exec.SynthesizedPlan.Goal
			req.PlanContext = exec.SynthesizedPlan.Context
		}

		if err := co.publishBaseMessage(ctx, subjectScenGeneratorAsync, req); err != nil {
			co.logger.Error("Failed to dispatch scenario generator",
				"requirement_id", requirement.ID, "error", err)
			co.markErrorLocked(ctx, exec, fmt.Sprintf("dispatch scenario-generator failed: %v", err))
			return
		}
	}

	co.logger.Info("Dispatched scenario-generators",
		"slug", exec.Slug, "requirement_count", len(requirements))
}

// dispatchReviewerLocked dispatches the plan reviewer after
// scenarios are generated.
func (co *coordinator) dispatchReviewerLocked(ctx context.Context, exec *coordinationExecution) {
	if exec.terminated {
		return
	}

	taskID := fmt.Sprintf("%s%s%s", roleReviewer, taskIDSep, exec.EntityID)

	co.advancePhase(ctx, exec, phaseReviewing)

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

	if err := co.publishBaseMessage(ctx, subjectReviewerAsync, req); err != nil {
		co.logger.Error("Failed to dispatch reviewer", "error", err)
		co.markErrorLocked(ctx, exec, fmt.Sprintf("dispatch reviewer failed: %v", err))
		return
	}

	co.logger.Info("Dispatched reviewer (el jefe)",
		"slug", exec.Slug, "task_id", taskID)
}

// ---------------------------------------------------------------------------
// Pipeline step completion handlers
// ---------------------------------------------------------------------------

// handleGeneratorEvent routes generator completion events (from WORKFLOW stream)
// to the appropriate handler based on subject.
func (co *coordinator) handleGeneratorEvent(ctx context.Context, msg jetstream.Msg) {
	co.updateLastActivity()

	// Extract slug from payload to look up active coordination.
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(msg.Data(), &envelope); err != nil {
		co.logger.Debug("Failed to unmarshal generator event envelope", "error", err)
		_ = msg.Nak()
		return
	}

	var slugHolder struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(envelope.Payload, &slugHolder); err != nil || slugHolder.Slug == "" {
		co.logger.Debug("Generator event missing slug", "subject", msg.Subject())
		_ = msg.Ack()
		return
	}

	entityIDVal, ok := co.slugIndex.Load(slugHolder.Slug)
	if !ok {
		co.logger.Debug("No active coordination for slug", "slug", slugHolder.Slug)
		_ = msg.Ack()
		return
	}
	entityID := entityIDVal.(string)

	execVal, ok := co.activeCoordinations.Load(entityID)
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
		if exec.CurrentPhase != phaseGeneratingRequirements {
			co.logger.Debug("Ignoring stale requirements event",
				"current_phase", exec.CurrentPhase, "slug", exec.Slug)
			return
		}
		co.handleReqsGeneratedLocked(ctx, exec, envelope.Payload)
	case subjectScenariosGenerated:
		if exec.CurrentPhase != phaseGeneratingScenarios {
			co.logger.Debug("Ignoring stale/duplicate scenarios event",
				"current_phase", exec.CurrentPhase, "slug", exec.Slug)
			return
		}
		co.handleScenariosGeneratedLocked(ctx, exec, envelope.Payload)
	default:
		co.logger.Debug("Unknown generator event subject", "subject", msg.Subject())
	}
}

// handleReqsGeneratedLocked processes the RequirementsGeneratedEvent.
func (co *coordinator) handleReqsGeneratedLocked(ctx context.Context, exec *coordinationExecution, payload json.RawMessage) {
	var event workflow.RequirementsGeneratedEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		co.logger.Warn("Failed to parse RequirementsGeneratedEvent", "error", err)
	}

	co.logger.Info("Requirements generated",
		"entity_id", exec.EntityID,
		"slug", exec.Slug,
		"requirement_count", event.RequirementCount,
	)

	co.advancePhase(ctx, exec, phaseRequirementsGenerated)

	// Advance to scenario generation.
	co.dispatchScenarioGeneratorLocked(ctx, exec)
}

// handleScenariosGeneratedLocked processes the ScenariosGeneratedEvent.
func (co *coordinator) handleScenariosGeneratedLocked(ctx context.Context, exec *coordinationExecution, payload json.RawMessage) {
	var event workflow.ScenariosGeneratedEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		co.logger.Warn("Failed to parse ScenariosGeneratedEvent", "error", err)
	}

	co.logger.Info("Scenarios generated",
		"entity_id", exec.EntityID,
		"slug", exec.Slug,
		"scenario_count", event.ScenarioCount,
	)

	co.advancePhase(ctx, exec, phaseScenariosGenerated)

	if co.config.IsAutoApprove() {
		exec.ReviewRound = 2
		_ = co.tripleWriter.WriteTriple(ctx, exec.EntityID, "workflow.coordination.review_round", 2)
		co.dispatchReviewerLocked(ctx, exec)
	} else {
		co.advancePhase(ctx, exec, phaseAwaitingHuman)
		co.logger.Info("Round 2 awaiting human approval (requirements + scenarios review)",
			"slug", exec.Slug)
		exec.terminated = true
		co.coordinationsCompleted.Add(1)
		co.cleanupExecutionLocked(exec)
	}
}

// handleReviewerCompleteLocked processes reviewer completion.
func (co *coordinator) handleReviewerCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *coordinationExecution) {
	if exec.terminated {
		return
	}

	verdict, summary := co.parseReviewerVerdict(event.Result)

	co.logger.Info("Reviewer completed",
		"entity_id", exec.EntityID,
		"slug", exec.Slug,
		"verdict", verdict,
		"summary", summary,
	)

	_ = co.tripleWriter.WriteTriple(ctx, exec.EntityID, "workflow.review.verdict", verdict)
	if summary != "" {
		_ = co.tripleWriter.WriteTriple(ctx, exec.EntityID, "workflow.review.summary", summary)
	}

	switch verdict {
	case "approved":
		co.handleReviewApprovalLocked(ctx, exec, verdict, summary)

	case "needs_changes":
		co.handleReviewRejectionLocked(ctx, exec, summary)

	default:
		co.logger.Warn("Unknown reviewer verdict, treating as error",
			"verdict", verdict, "slug", exec.Slug)
		co.markErrorLocked(ctx, exec, fmt.Sprintf("unknown reviewer verdict: %s", verdict))
	}
}

// handleReviewApprovalLocked processes an "approved" verdict from the reviewer.
// Caller must hold exec.mu.
func (co *coordinator) handleReviewApprovalLocked(ctx context.Context, exec *coordinationExecution, verdict, summary string) {
	if exec.ReviewRound <= 1 {
		if co.config.IsAutoApprove() {
			co.logger.Info("Round 1 approved (auto), starting requirement generation",
				"slug", exec.Slug)
			co.advancePhase(ctx, exec, phaseApproved)
			co.publishPlanApprovedEvent(ctx, exec, verdict, summary)
			co.dispatchRequirementGeneratorLocked(ctx, exec)
		} else {
			co.advancePhase(ctx, exec, phaseAwaitingHuman)
			co.logger.Info("Round 1 awaiting human approval (plan review)",
				"slug", exec.Slug, "iteration", exec.Iteration)
			exec.terminated = true
			co.coordinationsCompleted.Add(1)
			co.cleanupExecutionLocked(exec)
		}
	} else {
		// Round 2: requirements + scenarios approved — plan ready for execution.
		co.advancePhase(ctx, exec, phaseApproved)
		co.publishPlanApprovedEvent(ctx, exec, verdict, summary)

		if err := co.plans.setStatus(ctx, exec.Slug, workflow.StatusReadyForExecution); err != nil {
			co.logger.Warn("Failed to set plan to ready_for_execution",
				"slug", exec.Slug, "error", err)
		} else {
			co.logger.Info("Round 2 approved, plan ready for execution",
				"slug", exec.Slug)
		}

		exec.terminated = true
		co.coordinationsCompleted.Add(1)
		co.cleanupExecutionLocked(exec)
	}
}

// handleReviewRejectionLocked processes a "needs_changes" verdict.
// Caller must hold exec.mu.
func (co *coordinator) handleReviewRejectionLocked(ctx context.Context, exec *coordinationExecution, summary string) {
	exec.Iteration++
	exec.ReviewFeedback = summary
	_ = co.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Iteration, exec.Iteration)

	if exec.Iteration >= co.config.MaxReviewIterations {
		co.logger.Warn("Review budget exhausted, escalating",
			"slug", exec.Slug,
			"round", exec.ReviewRound,
			"iteration", exec.Iteration,
			"max", co.config.MaxReviewIterations,
		)
		co.advancePhase(ctx, exec, phaseEscalated)
		exec.terminated = true
		co.coordinationsFailed.Add(1)
		co.cleanupExecutionLocked(exec)
		return
	}

	if exec.ReviewRound <= 1 {
		co.logger.Info("Round 1 reviewer requested changes, retrying planners",
			"slug", exec.Slug,
			"iteration", exec.Iteration,
			"max", co.config.MaxReviewIterations,
		)
		co.advancePhase(ctx, exec, phasePlanning)
		exec.CompletedResults = make(map[string]*workflow.PlannerResult)
		exec.PlannerTaskIDs = nil
		co.dispatchPlannersLocked(ctx, exec)
	} else {
		co.logger.Info("Round 2 reviewer requested changes, retrying requirement generation",
			"slug", exec.Slug,
			"iteration", exec.Iteration,
			"max", co.config.MaxReviewIterations,
		)
		co.advancePhase(ctx, exec, phaseGeneratingRequirements)
		co.dispatchRequirementGeneratorLocked(ctx, exec)
	}
}

// publishPlanApprovedEvent sends a typed PlanApprovedEvent to the WORKFLOW stream.
func (co *coordinator) publishPlanApprovedEvent(ctx context.Context, exec *coordinationExecution, verdict, summary string) {
	event := &workflow.PlanApprovedEvent{
		Slug:    exec.Slug,
		Verdict: verdict,
		Summary: summary,
	}

	subject := workflow.PlanApproved.Pattern

	payloadData, err := json.Marshal(event)
	if err != nil {
		co.logger.Error("Failed to marshal PlanApprovedEvent", "error", err)
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
			Source:    coordinatorName,
		},
	}

	envelopeData, err := json.Marshal(envelope)
	if err != nil {
		co.logger.Error("Failed to marshal BaseMessage envelope", "error", err)
		return
	}

	if err := co.natsClient.PublishToStream(ctx, subject, envelopeData); err != nil {
		co.logger.Error("Failed to publish PlanApprovedEvent",
			"subject", subject, "slug", exec.Slug, "error", err)
		return
	}

	co.logger.Info("Published PlanApprovedEvent",
		"slug", exec.Slug, "subject", subject)
}

// parseReviewerVerdict extracts verdict and summary from a reviewer result.
func (co *coordinator) parseReviewerVerdict(result string) (verdict, summary string) {
	var parsed struct {
		Verdict string `json:"verdict"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		co.logger.Error("Failed to parse reviewer result, escalating", "error", err)
		return "escalated", fmt.Sprintf("reviewer result parse failed: %v", err)
	}
	if parsed.Verdict == "" {
		co.logger.Warn("Reviewer returned empty verdict, escalating")
		return "escalated", "reviewer returned empty verdict"
	}
	return parsed.Verdict, parsed.Summary
}

// ---------------------------------------------------------------------------
// Planner prompt helpers
// ---------------------------------------------------------------------------

func (co *coordinator) buildPlannerPrompt(exec *coordinationExecution, focus *FocusArea) string {
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

func (co *coordinator) determineFocusAreas(ctx context.Context, trigger *payloads.PlanCoordinatorRequest) ([]*FocusArea, error) {
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

	provider := co.resolveProvider()
	assembled := co.assembler.Assemble(&prompt.AssemblyContext{
		Role:     prompt.RolePlanCoordinator,
		Provider: provider,
		Domain:   "software",
	})
	systemPrompt := assembled.SystemMessage

	userPrompt := co.buildFocusUserPrompt(trigger, "")

	content, _, err := co.callLLM(ctx, systemPrompt, userPrompt)
	if err != nil {
		co.logger.Warn("Failed to determine focus areas via LLM, falling back to single planner",
			"error", err)
		return []*FocusArea{{
			Area:        "general",
			Description: "General analysis of the task",
		}}, nil
	}

	focuses, err := co.parseFocusAreas(content)
	if err != nil {
		co.logger.Warn("Failed to parse focus areas, falling back to single planner",
			"error", err)
		return []*FocusArea{{
			Area:        "general",
			Description: "General analysis of the task",
		}}, nil
	}

	maxPlanners := co.config.MaxConcurrentPlanners
	if trigger.MaxPlanners > 0 && trigger.MaxPlanners < maxPlanners {
		maxPlanners = trigger.MaxPlanners
	}
	if len(focuses) > maxPlanners {
		focuses = focuses[:maxPlanners]
	}

	return focuses, nil
}

func (co *coordinator) buildFocusUserPrompt(trigger *payloads.PlanCoordinatorRequest, graphContext string) string {
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

func (co *coordinator) parseFocusAreas(content string) ([]*FocusArea, error) {
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

func (co *coordinator) parsePlannerResult(result, taskID string) (*workflow.PlannerResult, []string) {
	var payload PlannerResultPayload
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		co.logger.Warn("Failed to parse planner result, using raw",
			"task_id", taskID, "error", err)
		return &workflow.PlannerResult{
			PlannerID: taskID,
			Goal:      result,
		}, nil
	}

	// Prefer nested content (current planner format) over top-level fields (legacy).
	goal := payload.Goal
	planCtx := payload.Context
	scope := payload.Scope
	if payload.Content != nil && payload.Content.Goal != "" {
		goal = payload.Content.Goal
		planCtx = payload.Content.Context
		scope = payload.Content.Scope
	}

	return &workflow.PlannerResult{
		PlannerID: taskID,
		Goal:      goal,
		Context:   planCtx,
		Scope: workflow.Scope{
			Include:    scope.Include,
			Exclude:    scope.Exclude,
			DoNotTouch: scope.DoNotTouch,
		},
	}, nil
}

// ---------------------------------------------------------------------------
// Synthesis
// ---------------------------------------------------------------------------

func (co *coordinator) synthesizeResults(ctx context.Context, results []workflow.PlannerResult) (*SynthesizedPlan, string, error) {
	if len(results) == 1 {
		return &SynthesizedPlan{
			Goal:    results[0].Goal,
			Context: results[0].Context,
			Scope:   results[0].Scope,
		}, "", nil
	}

	systemPrompt := "You are synthesizing multiple planning perspectives into a unified development plan."
	userPrompt := prompts.PlanCoordinatorSynthesisPrompt(results)

	content, llmRequestID, err := co.callLLM(ctx, systemPrompt, userPrompt)
	if err != nil {
		co.logger.Warn("Synthesis LLM call failed, falling back to simple merge", "error", err)
		return co.simpleMerge(results), "", nil
	}

	synthesized, err := co.parseSynthesizedPlan(content)
	if err != nil {
		co.logger.Warn("Synthesis parse failed, falling back to simple merge", "error", err)
		return co.simpleMerge(results), llmRequestID, nil
	}

	if synthesized.Goal == "" {
		co.logger.Warn("Synthesis returned empty goal, falling back to simple merge")
		return co.simpleMerge(results), llmRequestID, nil
	}

	return synthesized, llmRequestID, nil
}

func (co *coordinator) simpleMerge(results []workflow.PlannerResult) *SynthesizedPlan {
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
			Include:    coordUnique(include),
			Exclude:    coordUnique(exclude),
			DoNotTouch: coordUnique(doNotTouch),
		},
	}
}

func (co *coordinator) parseSynthesizedPlan(content string) (*SynthesizedPlan, error) {
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

func (co *coordinator) callLLM(ctx context.Context, systemPrompt, userPrompt string) (string, string, error) {
	capability := co.config.DefaultCapability
	if capability == "" {
		capability = string(model.CapabilityPlanning)
	}

	temperature := 0.7
	resp, err := co.llmClient.Complete(ctx, llm.Request{
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

func (co *coordinator) resolveProvider() prompt.Provider {
	capability := co.config.DefaultCapability
	if capability == "" {
		capability = string(model.CapabilityPlanning)
	}
	modelName := co.modelRegistry.Resolve(model.Capability(capability))
	if endpoint := co.modelRegistry.GetEndpoint(modelName); endpoint != nil {
		return prompt.Provider(endpoint.Provider)
	}
	return prompt.ProviderOllama
}

// ---------------------------------------------------------------------------
// Triple publishing helpers
// ---------------------------------------------------------------------------

func (co *coordinator) publishBaseMessage(ctx context.Context, subject string, payload message.Payload) error {
	baseMsg := message.NewBaseMessage(payload.Schema(), payload, coordinatorName)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal base message: %w", err)
	}

	if co.natsClient != nil {
		if err := co.natsClient.PublishToStream(ctx, subject, data); err != nil {
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
func (co *coordinator) advancePhase(ctx context.Context, exec *coordinationExecution, phase string) {
	exec.CurrentPhase = phase
	if err := co.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phase); err != nil {
		co.logger.Error("Failed to write phase triple", "phase", phase, "error", err)
	}
}

func (co *coordinator) updateLastActivity() {
	co.lastActivityMu.Lock()
	co.lastActivity = time.Now()
	co.lastActivityMu.Unlock()
}

// coordUnique returns unique strings from a slice.
// Named with coord prefix to avoid conflict with any other unique() in the package.
func coordUnique(strs []string) []string {
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
