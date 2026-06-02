package recoveryagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/internal/trajectory"
	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/prompt"
	promptdomain "github.com/c360studio/semspec/prompt/domain"
	"github.com/c360studio/semspec/tools/terminal"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	ssmodel "github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	componentName    = "recovery-agent"
	componentVersion = "0.1.0"

	// agentTaskSubject is the NATS subject the agentic-loop processor
	// picks up for our dispatched LLM tasks. Single segment after
	// "agent.task." matches the agentic-loop's agent.task.* subscription.
	agentTaskSubject = "agent.task.recovery-agent"

	// stepRecover is the WorkflowStep stamped on the dispatched TaskMessage
	// and matched in the AGENT_LOOPS watcher. One step per request today.
	stepRecover = "recover"

	// planDecisionAddSubject is plan-manager's request/reply mutation for
	// adding a PlanDecision to a plan. Same wire shape qa-reviewer +
	// req-executor use to surface their proposals to humans + change-
	// proposal-handler.
	planDecisionAddSubject = "plan.mutation.plan_decision.add"
)

// Component implements the recovery-agent processor.
//
// Lifecycle:
//
//	Start  → subscribes to recovery.requested.> on the WORKFLOW stream and
//	         opens an AGENT_LOOPS KV watcher for our dispatched loops.
//	OnMsg  → fetches the wedged agent's trajectory, assembles a prompt,
//	         publishes an agentic.TaskMessage on agent.task.recovery-agent.
//	OnLoop → parses submit_work output, persists RECOVERY_STATES KV
//	         entry, publishes RecoveryComplete on recovery.complete.<slug>.
//	Stop   → cancels the context and waits for in-flight handlers.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	// Dispatch + watcher dependencies.
	modelRegistry ssmodel.RegistryReader
	toolRegistry  component.ToolRegistryReader
	decoder       *message.Decoder

	// assembler builds the recovery-agent's system + user message from
	// persona fragments (RoleRecoveryAgent in prompt/domain/software.go).
	// Replaces the legacy hand-rolled systemPrompt + buildUserPrompt
	// strings in prompt.go that bypassed the persona pipeline entirely.
	// Wired 2026-05-11 take 11 cleanup.
	assembler *prompt.Assembler

	// inFlight maps dispatched TaskID → originating request. Populated
	// when handleMessage publishes the LLM task; consumed by the
	// AGENT_LOOPS watcher on terminal state. A nil/empty map is fine —
	// any loop completion without a matching dispatch is silently
	// ignored (replay or a foreign loop with our slug).
	inFlightMu sync.Mutex
	inFlight   map[string]*inFlightDispatch

	// Lifecycle.
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics. Atomic ints — only one consumer per metric, no labels.
	requestsReceived  atomic.Int64
	requestsSkipped   atomic.Int64
	parseErrors       atomic.Int64
	dispatched        atomic.Int64
	dispatchFailures  atomic.Int64
	completedSuccess  atomic.Int64
	completedFailed   atomic.Int64
	resultParseErrors atomic.Int64

	lastActivityMu sync.RWMutex
	lastActivity   time.Time
}

// inFlightDispatch carries a dispatched request's originating payload plus
// the resolved model. The model is captured at dispatch time so completion
// log lines can attribute the recovery to a (model, capability) tuple.
type inFlightDispatch struct {
	Req        *payloads.RecoveryRequested
	Model      string
	Capability model.Capability
}

// NewComponent constructs a recovery-agent Component from raw JSON config
// and semstreams dependencies.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	defaults := DefaultConfig()
	if config.StreamName == "" {
		config.StreamName = defaults.StreamName
	}
	if config.ConsumerName == "" {
		config.ConsumerName = defaults.ConsumerName
	}
	if config.FilterSubject == "" {
		config.FilterSubject = defaults.FilterSubject
	}
	if config.TrajectoryStepLimit <= 0 {
		config.TrajectoryStepLimit = defaults.TrajectoryStepLimit
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	logger := deps.GetLogger()

	// Build the persona-fragment registry the recovery-agent dispatches
	// through. Same shape as planner/qa-reviewer/etc — domain fragments
	// + tool-guidance + manifest helpers. RoleRecoveryAgent fragments
	// live in promptdomain.Software().
	registry := prompt.NewRegistry()
	registry.RegisterAll(promptdomain.Software()...)
	registry.Register(prompt.ToolGuidanceFragment(prompt.DefaultToolGuidance()))

	c := &Component{
		name:          componentName,
		config:        config,
		natsClient:    deps.NATSClient,
		logger:        logger,
		modelRegistry: deps.ModelRegistry,
		toolRegistry:  deps.ToolRegistry,
		decoder:       message.NewDecoder(deps.PayloadRegistry),
		inFlight:      make(map[string]*inFlightDispatch),
		assembler:     prompt.NewAssembler(registry),
	}
	return c, nil
}

// Initialize prepares the component for startup.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized recovery-agent",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"filter", c.config.FilterSubject,
		"enabled", c.config.Enabled,
		"trajectory_step_limit", c.config.TrajectoryStepLimit,
	)
	return nil
}

// Start begins consuming RecoveryRequested messages and watching
// AGENT_LOOPS for completion of our dispatched loops.
func (c *Component) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("component already running")
	}
	if c.natsClient == nil {
		c.mu.Unlock()
		return fmt.Errorf("NATS client required")
	}

	c.running = true
	c.startTime = time.Now()

	subCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.mu.Unlock()

	cfg := natsclient.StreamConsumerConfig{
		StreamName:    c.config.StreamName,
		ConsumerName:  c.config.ConsumerName,
		FilterSubject: c.config.FilterSubject,
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       30 * time.Second,
	}
	if err := c.natsClient.ConsumeStreamWithConfig(subCtx, cfg, c.handleMessagePush); err != nil {
		c.mu.Lock()
		c.running = false
		c.cancel = nil
		c.mu.Unlock()
		cancel()
		return fmt.Errorf("consume recovery requests: %w", err)
	}

	go c.watchLoopCompletions(subCtx)

	c.logger.Info("recovery-agent started",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"subject", c.config.FilterSubject,
		"enabled", c.config.Enabled,
	)
	return nil
}

// Stop gracefully stops the component.
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil
	}

	cancel := c.cancel
	c.running = false
	c.cancel = nil
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	c.logger.Info("recovery-agent stopped",
		"requests_received", c.requestsReceived.Load(),
		"requests_skipped", c.requestsSkipped.Load(),
		"parse_errors", c.parseErrors.Load(),
		"dispatched", c.dispatched.Load(),
		"dispatch_failures", c.dispatchFailures.Load(),
		"completed_success", c.completedSuccess.Load(),
		"completed_failed", c.completedFailed.Load(),
		"result_parse_errors", c.resultParseErrors.Load(),
	)
	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        componentName,
		Type:        "processor",
		Description: "ADR-037 stage 1: manager-role wedge recovery dispatcher",
		Version:     componentVersion,
	}
}

// InputPorts returns the input port definitions.
func (c *Component) InputPorts() []component.Port {
	return []component.Port{
		{
			Name:        "recovery-requests",
			Direction:   component.DirectionInput,
			Required:    true,
			Description: "RecoveryRequested events from escalating components",
			Config:      component.NATSPort{Subject: c.config.FilterSubject},
		},
	}
}

// OutputPorts returns the output port definitions.
func (c *Component) OutputPorts() []component.Port {
	return []component.Port{
		{
			Name:        "recovery-task",
			Direction:   component.DirectionOutput,
			Required:    false,
			Description: "Dispatches one-shot recovery agent tasks via agentic-loop",
			Config:      component.NATSPort{Subject: agentTaskSubject},
		},
		{
			Name:        "recovery-plan-decision",
			Direction:   component.DirectionOutput,
			Required:    false,
			Description: "Emits a workflow.PlanDecision (kind=requirement_change|execution_exhausted) carrying the recovery agent's chosen action + diagnosis. Routes through plan.mutation.plan_decision.add — same wire qa-reviewer + req-executor use.",
			Config:      component.NATSPort{Subject: planDecisionAddSubject},
		},
	}
}

// ConfigSchema returns the configuration schema.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return recoveryAgentSchema
}

// Health returns the current health status.
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	running := c.running
	startTime := c.startTime
	c.mu.RUnlock()

	status := "stopped"
	if running {
		status = "running"
	}
	return component.HealthStatus{
		Healthy:    running,
		LastCheck:  time.Now(),
		ErrorCount: int(c.parseErrors.Load()) + int(c.dispatchFailures.Load()) + int(c.resultParseErrors.Load()),
		Uptime:     time.Since(startTime),
		Status:     status,
	}
}

// DataFlow returns current data flow metrics.
func (c *Component) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{LastActivity: c.getLastActivity()}
}

// handleMessagePush is the push-based callback for ConsumeStreamWithConfig.
func (c *Component) handleMessagePush(ctx context.Context, msg jetstream.Msg) {
	c.handleMessage(ctx, msg)
}

// handleMessage parses a RecoveryRequested payload, fetches the wedged
// agent's trajectory (if available), assembles a prompt, and dispatches
// a one-shot LLM task. The result is consumed asynchronously by the
// AGENT_LOOPS watcher.
//
// All branches ack the message — a publish/dispatch failure must not
// block the escalation flow that produced the request.
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	c.requestsReceived.Add(1)
	c.updateLastActivity()

	if !c.config.Enabled {
		c.requestsSkipped.Add(1)
		c.ackOrWarn(msg, "skipped (disabled)")
		return
	}

	req, ok := c.parseRequest(msg)
	if !ok {
		return
	}

	if err := c.dispatchRecovery(ctx, req); err != nil {
		c.dispatchFailures.Add(1)
		c.logger.Error("Recovery dispatch failed",
			"slug", req.Slug,
			"recovery_id", req.RecoveryID,
			"error", err)
	}
	c.ackOrWarn(msg, "processed")
}

// parseRequest unwraps the BaseMessage envelope and returns the typed
// payload. Any failure increments parseErrors, acks the message (so it
// isn't redelivered forever), and returns ok=false.
func (c *Component) parseRequest(msg jetstream.Msg) (*payloads.RecoveryRequested, bool) {
	if c.decoder == nil {
		c.parseErrors.Add(1)
		c.logger.Error("Decoder not initialised — payload registry missing")
		c.ackOrWarn(msg, "no decoder")
		return nil, false
	}
	base, err := c.decoder.Decode(msg.Data())
	if err != nil {
		c.parseErrors.Add(1)
		c.logger.Error("Failed to decode RecoveryRequested envelope", "error", err)
		c.ackOrWarn(msg, "malformed envelope")
		return nil, false
	}
	req, ok := base.Payload().(*payloads.RecoveryRequested)
	if !ok {
		c.parseErrors.Add(1)
		c.logger.Error("Unexpected payload type", "type", fmt.Sprintf("%T", base.Payload()))
		c.ackOrWarn(msg, "wrong type")
		return nil, false
	}
	if err := req.Validate(); err != nil {
		c.parseErrors.Add(1)
		c.logger.Error("Invalid RecoveryRequested payload", "error", err)
		c.ackOrWarn(msg, "invalid payload")
		return nil, false
	}
	return req, true
}

// capabilityForRequest picks the right capability based on layer + scope.
// Execution-phase wedges (RequirementID set) use execution_wedge_recovery;
// plan-phase wedges use plan_wedge_recovery; coordinator-layer requests
// use coordinator_recovery. The model registry resolves each capability
// to a concrete model with optional fallback chain.
func capabilityForRequest(req *payloads.RecoveryRequested) model.Capability {
	if req.Layer == payloads.RecoveryLayerCoordinator {
		return model.CapabilityCoordinatorRecovery
	}
	if req.RequirementID != "" {
		return model.CapabilityExecutionWedgeRecovery
	}
	return model.CapabilityPlanWedgeRecovery
}

// dispatchRecovery is the load-bearing path: fetch trajectory, build prompt,
// resolve model, publish TaskMessage.
func (c *Component) dispatchRecovery(ctx context.Context, req *payloads.RecoveryRequested) error {
	if c.modelRegistry == nil {
		c.logger.Warn("Recovery dispatch skipped: model registry not wired",
			"slug", req.Slug, "recovery_id", req.RecoveryID)
		return nil
	}

	capName := capabilityForRequest(req)
	modelName := c.modelRegistry.Resolve(string(capName))
	if modelName == "" {
		// Fall back to plan-review-class reasoning if the recovery
		// capability isn't configured for this deployment. Recovery still
		// runs; operators see the warn and configure on demand.
		modelName = c.modelRegistry.Resolve(string(model.CapabilityPlanReview))
		if modelName == "" {
			modelName = c.modelRegistry.Resolve(string(model.CapabilityReviewing))
		}
		c.logger.Warn("Recovery capability not configured; falling back",
			"slug", req.Slug,
			"capability", capName,
			"fallback_model", modelName)
	}

	steps := c.fetchTrajectorySteps(ctx, req)
	recCtx := &prompt.RecoveryPromptContext{
		Layer:               string(req.Layer),
		Slug:                req.Slug,
		RequirementID:       req.RequirementID,
		TaskID:              req.TaskID,
		LoopID:              req.LoopID,
		PriorRecoveryID:     req.PriorRecoveryID,
		EscalationReason:    req.EscalationReason,
		LastFailureFeedback: req.LastFailureFeedback,
		TrajectorySteps:     steps,
	}

	var (
		endpoint  *ssmodel.EndpointConfig
		maxTokens int
	)
	if c.modelRegistry != nil {
		if ep := c.modelRegistry.GetEndpoint(modelName); ep != nil {
			endpoint = ep
			maxTokens = ep.MaxTokens
		}
	}

	availableTools := prompt.FilterTools(recoveryAvailableToolNames(), prompt.RoleRecoveryAgent)
	asmCtx := &prompt.AssemblyContext{
		Role:              prompt.RoleRecoveryAgent,
		Provider:          resolveProvider(modelName),
		HasResponseFormat: terminal.EndpointSupportsResponseFormatGated(endpoint, nil),
		Domain:            "software",
		AvailableTools:    availableTools,
		SupportsTools:     true,
		MaxTokens:         maxTokens,
		Persona:           prompt.GlobalPersonas().ForRole(prompt.RoleRecoveryAgent),
		Vocabulary:        prompt.GlobalPersonas().Vocabulary(),
		Recovery:          recCtx,
	}

	assembled := c.assembler.Assemble(asmCtx)
	if assembled.RenderError != nil {
		c.logger.Error("recovery-agent prompt render failed",
			"slug", req.Slug, "recovery_id", req.RecoveryID, "error", assembled.RenderError)
		return fmt.Errorf("assemble recovery prompt: %w", assembled.RenderError)
	}

	taskID := fmt.Sprintf("recover-%s-%s", req.Slug, uuid.New().String())
	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleGeneral,
		Model:        modelName,
		Prompt:       assembled.UserMessage,
		Tools:        terminal.ToolsForEndpoint(c.toolRegistry, "review", endpoint, availableTools...),
		ToolChoice:   prompt.ResolveToolChoice(prompt.RoleRecoveryAgent, availableTools),
		WorkflowSlug: workflow.WorkflowSlugWedgeRecovery,
		WorkflowStep: stepRecover,
		Context: &agentic.ConstructedContext{
			Content: assembled.SystemMessage,
		},
		Metadata: map[string]any{
			"plan_slug":      req.Slug,
			"recovery_id":    req.RecoveryID,
			"layer":          string(req.Layer),
			"requirement_id": req.RequirementID,
			"task_id":        req.TaskID,
			"loop_id":        req.LoopID,
			"capability":     string(capName),
			"model":          modelName,
			"role":           string(prompt.RoleRecoveryAgent),
		},
	}

	baseMsg := message.NewBaseMessage(task.Schema(), task, componentName)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal task message: %w", err)
	}

	c.trackInFlight(taskID, req, modelName, capName)
	if err := c.natsClient.PublishToStream(ctx, agentTaskSubject, data); err != nil {
		c.untrackInFlight(taskID)
		return fmt.Errorf("publish task: %w", err)
	}

	c.dispatched.Add(1)
	c.logger.Info("Dispatched recovery agent",
		"slug", req.Slug,
		"recovery_id", req.RecoveryID,
		"task_id", taskID,
		"capability", capName,
		"model", modelName,
		"trajectory_steps", len(steps),
		"prompt_chars", len(assembled.UserMessage),
	)
	return nil
}

// fetchTrajectorySteps pulls the wedged agent's trajectory and pre-
// summarises each step. Returns nil if the request had no loop_id (plan-
// phase wedges today) or if the fetch failed — the prompt builder
// surfaces a "trajectory unavailable" notice in that case.
func (c *Component) fetchTrajectorySteps(ctx context.Context, req *payloads.RecoveryRequested) []string {
	if req.LoopID == "" {
		return nil
	}
	traj, err := trajectory.Fetch(ctx, c.natsClient, req.LoopID, c.config.TrajectoryStepLimit)
	if err != nil {
		c.logger.Warn("Recovery trajectory fetch failed; proceeding with feedback-only diagnosis",
			"slug", req.Slug,
			"loop_id", req.LoopID,
			"error", err)
		return nil
	}
	return summarizeTrajectory(traj, c.config.TrajectoryStepLimit)
}

func (c *Component) trackInFlight(taskID string, req *payloads.RecoveryRequested, modelName string, capName model.Capability) {
	c.inFlightMu.Lock()
	defer c.inFlightMu.Unlock()
	if c.inFlight == nil {
		c.inFlight = make(map[string]*inFlightDispatch)
	}
	c.inFlight[taskID] = &inFlightDispatch{Req: req, Model: modelName, Capability: capName}
}

func (c *Component) untrackInFlight(taskID string) *inFlightDispatch {
	c.inFlightMu.Lock()
	defer c.inFlightMu.Unlock()
	if c.inFlight == nil {
		return nil
	}
	disp := c.inFlight[taskID]
	delete(c.inFlight, taskID)
	return disp
}

// watchLoopCompletions watches the AGENT_LOOPS KV bucket for terminal
// state on dispatched recovery loops. Filters by WorkflowSlug so this
// component only acts on its own dispatches.
func (c *Component) watchLoopCompletions(ctx context.Context) {
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Warn("Cannot watch AGENT_LOOPS: no JetStream", "error", err)
		return
	}
	bucket, err := workflow.WaitForKVBucket(ctx, js, "AGENT_LOOPS")
	if err != nil {
		c.logger.Warn("AGENT_LOOPS bucket not available — recovery loop watcher disabled", "error", err)
		return
	}
	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		c.logger.Warn("Failed to watch AGENT_LOOPS", "error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Recovery loop completion watcher started")

	for entry := range watcher.Updates() {
		if entry == nil {
			continue
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}
		var loop agentic.LoopEntity
		if err := json.Unmarshal(entry.Value(), &loop); err != nil {
			continue
		}
		if !loop.State.IsTerminal() {
			continue
		}
		if loop.WorkflowSlug != workflow.WorkflowSlugWedgeRecovery {
			continue
		}
		c.handleLoopCompletion(ctx, &loop)
	}
}

// handleLoopCompletion parses the recovery agent's submit_work output and
// emits a PlanDecision via the standard plan.mutation.plan_decision.add
// wire (same shape qa-reviewer + req-executor use). The PlanDecision
// carries the diagnosis as Rationale and routes to change-proposal-handler
// for human-or-auto-accept review and cascade re-run.
//
// Action → PlanDecision kind mapping:
//
//	refine_prompt | narrow_scope | split_req → kind=requirement_change
//	  (cascade dirty-marks the req; on accept the affected scenarios
//	  re-run, and the recovery rationale is available as the dev's
//	  supplementary feedback on retry — wired in (a3))
//	escalate_human | mark_unrecoverable → kind=execution_exhausted
//	  (terminal record; req-executor may have already emitted one for
//	  retry-budget exhaustion — recovery's adds the diagnostic enrichment)
//
// Default-failure paths (loop crashed, parse failed, no action picked)
// emit a kind=execution_exhausted PlanDecision with diagnostic rationale —
// the human still gets a record of the recovery attempt and its outcome.
func (c *Component) handleLoopCompletion(ctx context.Context, loop *agentic.LoopEntity) {
	disp := c.untrackInFlight(loop.TaskID)
	if disp == nil {
		return
	}
	c.updateLastActivity()

	action, diagnosis, succeeded := c.deriveDecision(loop, disp.Req.EscalationReason)

	c.logger.Info("Recovery agent decision",
		"slug", disp.Req.Slug,
		"recovery_id", disp.Req.RecoveryID,
		"task_id", loop.TaskID,
		"loop_id", loop.ID,
		"capability", disp.Capability,
		"model", disp.Model,
		"action", action,
		"recovery_succeeded", succeeded)

	c.emitPlanDecision(ctx, disp.Req, loop, action, diagnosis, succeeded)
}

// deriveDecision collapses the loop outcome + parse result into the action
// + diagnosis + succeeded triple the PlanDecision uses. Three branches:
// loop-didn't-succeed (outcome != success) → escalate_human marker;
// parse failed → escalate_human marker; success → parsed values.
//
// Returns escalate_human as the safe default for failure paths so the
// PlanDecision lifecycle still reconciles (per ADR-037 stage-1 design
// lock #3 — distinct failure signal beats silent drop).
func (c *Component) deriveDecision(loop *agentic.LoopEntity, escalationReason string) (payloads.RecoveryActionKind, string, bool) {
	if loop.Outcome != agentic.OutcomeSuccess {
		c.completedFailed.Add(1)
		c.logger.Warn("Recovery agent loop did not succeed",
			"task_id", loop.TaskID,
			"loop_id", loop.ID,
			"outcome", loop.Outcome,
			"error", loop.Error)
		return payloads.RecoveryActionEscalateHuman,
			fmt.Sprintf("Recovery agent loop did not succeed (outcome=%s, error=%q). Original wedge: %s.",
				loop.Outcome, loop.Error, escalationReason),
			false
	}

	parsed, err := parseRecoveryResult(loop.Result)
	if err != nil {
		c.resultParseErrors.Add(1)
		c.logger.Warn("Recovery result failed to parse",
			"task_id", loop.TaskID,
			"loop_id", loop.ID,
			"error", err,
			"raw_head", clip(loop.Result, 512))
		return payloads.RecoveryActionEscalateHuman,
			fmt.Sprintf("Recovery agent returned an unparseable result (%v). Original wedge: %s.",
				err, escalationReason),
			false
	}

	c.completedSuccess.Add(1)
	return parsed.Action, parsed.Diagnosis, parsed.RecoverySucceeded
}

// recoveryActionToPlanDecisionKind maps a RecoveryAction to the
// PlanDecision kind that drives the cascade.
//
//   - refine_prompt / narrow_scope / split_req → requirement_change.
//     Cascade dirty-marks scenarios for the affected requirement; the
//     executor re-runs with the same Story DAG.
//   - story_reprepare → story_reprepare. Distinct from requirement_change
//     because the cascade target is Stories + Scenarios (not just
//     Scenarios) AND plan-manager drives stories_generated →
//     preparing_stories on accept so Sarah actually re-runs with
//     Story.RecoveryHint set. Pre-Train-C, story_reprepare mapped to
//     requirement_change, which silently degraded into a scenarios-only
//     cascade that left Sarah's stories unchanged.
//   - escalate_human / mark_unrecoverable → execution_exhausted. Terminal;
//     plan-manager auto-archives when the subject req reaches a non-failed
//     terminal state.
func recoveryActionToPlanDecisionKind(action payloads.RecoveryActionKind) workflow.PlanDecisionKind {
	switch action {
	case payloads.RecoveryActionRefinePrompt,
		payloads.RecoveryActionNarrowScope,
		payloads.RecoveryActionSplitReq:
		return workflow.PlanDecisionKindRequirementChange
	case payloads.RecoveryActionStoryReprepare:
		return workflow.PlanDecisionKindStoryReprepare
	case payloads.RecoveryActionEscalateHuman, payloads.RecoveryActionMarkUnrecoverable:
		return workflow.PlanDecisionKindExecutionExhausted
	default:
		// Defensive — should be impossible after parseRecoveryResult's closed-set
		// validation. Fall back to terminal so an unknown action doesn't trigger
		// an unintended cascade.
		return workflow.PlanDecisionKindExecutionExhausted
	}
}

// buildRecoveryPlanDecision is the pure-function shape of the recovery
// PlanDecision construction. Extracted so unit tests can verify the
// AffectedReqIDs propagation contract (plan-scoped list preferred over
// single requirement ID, both empty leaves field nil for human review)
// without needing a real natsClient. Called only from emitPlanDecision in
// production. now is passed in rather than taking time.Now() inside so
// tests get deterministic CreatedAt values.
func buildRecoveryPlanDecision(req *payloads.RecoveryRequested, loop *agentic.LoopEntity, action payloads.RecoveryActionKind, diagnosis string, succeeded bool, now time.Time) workflow.PlanDecision {
	kind := recoveryActionToPlanDecisionKind(action)

	title := fmt.Sprintf("Recovery: %s", action)
	if req.RequirementID != "" {
		title = fmt.Sprintf("Recovery for %s: %s", req.RequirementID, action)
	}

	rationale := buildRecoveryRationale(req, loop, action, diagnosis, succeeded)

	// Prefer the plan-scoped affected list (populated by plan-manager on QA
	// verdict wedges where the qa-reviewer's verdict applies to all
	// assembled requirements) over the single requirement ID (populated by
	// execution-manager iteration exhaustion). Both empty leaves
	// AffectedReqIDs nil — the auto-accept watcher then leaves the decision
	// for human review (the correct outcome for plan-review wedges where
	// the plan itself is wrong, not any specific requirement).
	var affectedReqs []string
	switch {
	case len(req.AffectedRequirementIDs) > 0:
		affectedReqs = append(affectedReqs, req.AffectedRequirementIDs...)
	case req.RequirementID != "":
		affectedReqs = []string{req.RequirementID}
	}

	decisionID := fmt.Sprintf("plan-decision.%s.recovery.%s", req.Slug, req.RecoveryID[:8])

	return workflow.PlanDecision{
		ID:             decisionID,
		PlanID:         workflow.PlanEntityID(req.Slug),
		Kind:           kind,
		Title:          title,
		Rationale:      rationale,
		Status:         workflow.PlanDecisionStatusProposed,
		ProposedBy:     componentName,
		AffectedReqIDs: affectedReqs,
		CreatedAt:      now,
	}
}

// emitPlanDecision sends a workflow.PlanDecision via plan.mutation.
// plan_decision.add (same wire shape used by qa-reviewer + req-executor).
// Best-effort: failure logs but does not block subsequent recovery work.
func (c *Component) emitPlanDecision(ctx context.Context, req *payloads.RecoveryRequested, loop *agentic.LoopEntity, action payloads.RecoveryActionKind, diagnosis string, succeeded bool) {
	if c.natsClient == nil {
		return
	}

	decision := buildRecoveryPlanDecision(req, loop, action, diagnosis, succeeded, time.Now())

	addReq := struct {
		Slug     string                `json:"slug"`
		Decision workflow.PlanDecision `json:"decision"`
	}{Slug: req.Slug, Decision: decision}

	data, err := json.Marshal(addReq)
	if err != nil {
		c.logger.Warn("Failed to marshal PlanDecision add request",
			"recovery_id", req.RecoveryID, "error", err)
		return
	}

	respData, err := c.natsClient.RequestWithRetry(ctx, planDecisionAddSubject, data, 5*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		c.logger.Warn("Failed to publish recovery PlanDecision",
			"recovery_id", req.RecoveryID,
			"subject", planDecisionAddSubject,
			"error", err)
		return
	}

	var resp struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(respData, &resp); err != nil || !resp.Success {
		c.logger.Warn("plan-manager rejected recovery PlanDecision",
			"recovery_id", req.RecoveryID,
			"error", resp.Error,
			"unmarshal_err", err)
		return
	}

	c.logger.Info("Emitted recovery PlanDecision",
		"slug", req.Slug,
		"recovery_id", req.RecoveryID,
		"decision_id", decision.ID,
		"kind", decision.Kind,
		"action", action,
		"affected_reqs", decision.AffectedReqIDs)
}

// buildRecoveryRationale assembles the PlanDecision Rationale text. The
// rationale is what humans (and future change-proposal-handler apply
// logic in (a3)) read to decide accept/reject. Structured-but-prose so
// both consumers can extract what they need:
//
//   - Action: <kind> — what the recovery agent recommended
//   - Recovery succeeded: <bool> — agent's confidence the action will help
//   - Diagnosis: <multi-line text> — the actual analysis
//   - Original wedge: <reason> — what triggered recovery in the first place
//   - Recovery agent loop: <id> — for trajectory deep-link in UI
func buildRecoveryRationale(req *payloads.RecoveryRequested, loop *agentic.LoopEntity, action payloads.RecoveryActionKind, diagnosis string, succeeded bool) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Recommended action: %s\n", action)
	fmt.Fprintf(&sb, "Recovery agent confidence: recovery_succeeded=%t\n", succeeded)
	if req.EscalationReason != "" {
		fmt.Fprintf(&sb, "Original wedge: %s\n", req.EscalationReason)
	}
	if loop != nil && loop.ID != "" {
		fmt.Fprintf(&sb, "Recovery agent trajectory: %s\n", loop.ID)
	}
	if req.LoopID != "" {
		fmt.Fprintf(&sb, "Wedged agent trajectory: %s\n", req.LoopID)
	}
	sb.WriteString("\nDiagnosis:\n")
	sb.WriteString(diagnosis)
	return sb.String()
}

// ackOrWarn acks the JetStream message and warns on failure.
func (c *Component) ackOrWarn(msg jetstream.Msg, disposition string) {
	if err := msg.Ack(); err != nil {
		c.logger.Warn("Failed to ACK message", "disposition", disposition, "error", err)
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

// clip truncates s to at most n runes, appending "…" when truncated.
// Local to recovery-agent to avoid a one-shot dependency on internal/trajectory's
// unexported clip; both happen to share the helper shape.
func clip(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// recoveryAvailableToolNames returns the full tool set the recovery agent
// knows about; FilterTools(role=RoleRecoveryAgent) narrows to the actual
// wire palette (submit_work + scratchpad — see prompt/tool_filter.go).
func recoveryAvailableToolNames() []string {
	return []string{"submit_work", "scratchpad"}
}

// resolveProvider maps a model string to a prompt.Provider. Mirrors the
// same helper in requirement-executor; small enough to duplicate rather
// than pull into prompt/.
func resolveProvider(modelStr string) prompt.Provider {
	switch {
	case strings.Contains(modelStr, "claude"):
		return prompt.ProviderAnthropic
	case strings.Contains(modelStr, "gpt"),
		strings.Contains(modelStr, "o1"),
		strings.Contains(modelStr, "o3"):
		return prompt.ProviderOpenAI
	default:
		return prompt.ProviderOllama
	}
}
