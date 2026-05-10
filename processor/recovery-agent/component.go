package recoveryagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/internal/trajectory"
	"github.com/c360studio/semspec/model"
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
	decoder       *message.Decoder

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
	Req      *payloads.RecoveryRequested
	Model    string
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
	c := &Component{
		name:          componentName,
		config:        config,
		natsClient:    deps.NATSClient,
		logger:        logger,
		modelRegistry: deps.ModelRegistry,
		decoder:       message.NewDecoder(deps.PayloadRegistry),
		inFlight:      make(map[string]*inFlightDispatch),
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
			Name:        "recovery-complete",
			Direction:   component.DirectionOutput,
			Required:    false,
			Description: "Publishes RecoveryComplete with the chosen action + diagnosis",
			Config:      component.NATSPort{Subject: payloads.RecoveryCompleteSubjectPrefix + "*"},
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

	cap := capabilityForRequest(req)
	modelName := c.modelRegistry.Resolve(string(cap))
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
			"capability", cap,
			"fallback_model", modelName)
	}

	steps := c.fetchTrajectorySteps(ctx, req)
	userPrompt := buildUserPrompt(recoveryPromptInput{
		Layer:               string(req.Layer),
		Slug:                req.Slug,
		RequirementID:       req.RequirementID,
		TaskID:              req.TaskID,
		LoopID:              req.LoopID,
		PriorRecoveryID:     req.PriorRecoveryID,
		EscalationReason:    req.EscalationReason,
		LastFailureFeedback: req.LastFailureFeedback,
		TrajectorySteps:     steps,
	})

	taskID := fmt.Sprintf("recover-%s-%s", req.Slug, uuid.New().String())
	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleGeneral,
		Model:        modelName,
		Prompt:       userPrompt,
		WorkflowSlug: workflow.WorkflowSlugWedgeRecovery,
		WorkflowStep: stepRecover,
		Context: &agentic.ConstructedContext{
			Content: systemPrompt,
		},
		Metadata: map[string]any{
			"plan_slug":      req.Slug,
			"recovery_id":    req.RecoveryID,
			"layer":          string(req.Layer),
			"requirement_id": req.RequirementID,
			"task_id":        req.TaskID,
			"loop_id":        req.LoopID,
			"capability":     string(cap),
			"model":          modelName,
			"role":           "recovery-agent",
		},
	}

	baseMsg := message.NewBaseMessage(task.Schema(), task, componentName)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal task message: %w", err)
	}

	c.trackInFlight(taskID, req, modelName, cap)
	if err := c.natsClient.PublishToStream(ctx, agentTaskSubject, data); err != nil {
		c.untrackInFlight(taskID)
		return fmt.Errorf("publish task: %w", err)
	}

	c.dispatched.Add(1)
	c.logger.Info("Dispatched recovery agent",
		"slug", req.Slug,
		"recovery_id", req.RecoveryID,
		"task_id", taskID,
		"capability", cap,
		"model", modelName,
		"trajectory_steps", len(steps),
		"prompt_chars", len(userPrompt),
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

func (c *Component) trackInFlight(taskID string, req *payloads.RecoveryRequested, modelName string, cap model.Capability) {
	c.inFlightMu.Lock()
	defer c.inFlightMu.Unlock()
	if c.inFlight == nil {
		c.inFlight = make(map[string]*inFlightDispatch)
	}
	c.inFlight[taskID] = &inFlightDispatch{Req: req, Model: modelName, Capability: cap}
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

// handleLoopCompletion parses the recovery agent's submit_work output,
// persists the attempt in RECOVERY_STATES KV, and publishes
// RecoveryComplete on recovery.complete.<slug>. Best-effort: a KV-write
// or publish failure logs but doesn't block the next recovery.
func (c *Component) handleLoopCompletion(ctx context.Context, loop *agentic.LoopEntity) {
	disp := c.untrackInFlight(loop.TaskID)
	if disp == nil {
		return
	}
	c.updateLastActivity()

	// Build the wire payload with what the dispatcher always sets.
	complete := &payloads.RecoveryComplete{
		RecoveryID:          disp.Req.RecoveryID,
		Layer:               disp.Req.Layer,
		Slug:                disp.Req.Slug,
		RecoveryAgentLoopID: loop.ID,
		TraceID:             disp.Req.TraceID,
	}

	// Default-failure path: loop didn't reach OutcomeSuccess, or result
	// parsing rejected the output. Both produce a marker RecoveryComplete
	// so the escalating component's watcher still reconciles (with
	// recovery_succeeded=false), per ADR-037 design lock #3 — a distinct
	// recovery_failed signal beats silent drop.
	if loop.Outcome != agentic.OutcomeSuccess {
		c.completedFailed.Add(1)
		complete.Action = payloads.RecoveryActionEscalateHuman
		complete.Diagnosis = fmt.Sprintf("Recovery agent loop did not succeed (outcome=%s, error=%q). Original wedge: %s.",
			loop.Outcome, loop.Error, disp.Req.EscalationReason)
		complete.RecoverySucceeded = false
		c.logger.Warn("Recovery agent loop did not succeed",
			"slug", disp.Req.Slug,
			"recovery_id", disp.Req.RecoveryID,
			"task_id", loop.TaskID,
			"loop_id", loop.ID,
			"outcome", loop.Outcome,
			"error", loop.Error)
		c.persistAndPublish(ctx, disp.Req, complete)
		return
	}

	parsed, err := parseRecoveryResult(loop.Result)
	if err != nil {
		c.resultParseErrors.Add(1)
		complete.Action = payloads.RecoveryActionEscalateHuman
		complete.Diagnosis = fmt.Sprintf("Recovery agent returned an unparseable result (%v). Original wedge: %s.",
			err, disp.Req.EscalationReason)
		complete.RecoverySucceeded = false
		c.logger.Warn("Recovery result failed to parse",
			"slug", disp.Req.Slug,
			"recovery_id", disp.Req.RecoveryID,
			"task_id", loop.TaskID,
			"loop_id", loop.ID,
			"error", err,
			"raw_head", clip(loop.Result, 512))
		c.persistAndPublish(ctx, disp.Req, complete)
		return
	}

	// Success: copy parsed fields onto the wire payload.
	complete.Action = parsed.Action
	complete.Diagnosis = parsed.Diagnosis
	complete.RefinedPrompt = parsed.RefinedPrompt
	complete.ScopeChanges = parsed.ScopeChanges
	complete.RecoverySucceeded = parsed.RecoverySucceeded
	c.completedSuccess.Add(1)
	c.logger.Info("Recovery agent decision",
		"slug", disp.Req.Slug,
		"recovery_id", disp.Req.RecoveryID,
		"task_id", loop.TaskID,
		"loop_id", loop.ID,
		"capability", disp.Capability,
		"model", disp.Model,
		"action", complete.Action,
		"recovery_succeeded", complete.RecoverySucceeded)
	c.persistAndPublish(ctx, disp.Req, complete)
}

// persistAndPublish writes the recovery record to RECOVERY_STATES KV and
// publishes RecoveryComplete on recovery.complete.<slug>. Best-effort —
// either operation failing logs but does not block the other.
func (c *Component) persistAndPublish(ctx context.Context, req *payloads.RecoveryRequested, complete *payloads.RecoveryComplete) {
	if err := complete.Validate(); err != nil {
		c.logger.Warn("RecoveryComplete failed local validation; publishing anyway as failure marker",
			"slug", complete.Slug,
			"recovery_id", complete.RecoveryID,
			"error", err)
	}
	c.persistRecoveryState(ctx, req, complete)
	c.publishRecoveryComplete(ctx, complete)
}

// persistRecoveryState writes a single RECOVERY_STATES entry keyed by
// recovery_id. The value is a JSON blob containing both the requested
// and complete payloads — the watcher reading it has the full attempt
// record in one read.
func (c *Component) persistRecoveryState(ctx context.Context, req *payloads.RecoveryRequested, complete *payloads.RecoveryComplete) {
	if c.natsClient == nil {
		return
	}
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Warn("Cannot persist recovery state: no JetStream",
			"recovery_id", complete.RecoveryID, "error", err)
		return
	}
	// CreateOrUpdateKeyValue (not CreateKeyValue) — idempotent on existing
	// buckets. Bare CreateKeyValue errors with "bucket name already in use"
	// when another component / probe pre-touched the bucket, silently
	// skipping the write. Caught 2026-05-10 take 2 real-LLM @hard run: the
	// e2e scenario's RECOVERY_STATES poller created the bucket at startup,
	// so the recovery-agent's first persist attempt failed and the
	// diagnosis never landed on disk.
	kv, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: payloads.RecoveryStatesBucket,
	})
	if err != nil {
		c.logger.Warn("Cannot open RECOVERY_STATES bucket",
			"recovery_id", complete.RecoveryID, "error", err)
		return
	}
	record := struct {
		Requested *payloads.RecoveryRequested `json:"requested"`
		Complete  *payloads.RecoveryComplete  `json:"complete"`
		WrittenAt time.Time                   `json:"written_at"`
	}{Requested: req, Complete: complete, WrittenAt: time.Now()}
	data, err := json.Marshal(record)
	if err != nil {
		c.logger.Warn("Failed to marshal recovery state record",
			"recovery_id", complete.RecoveryID, "error", err)
		return
	}
	if _, err := kv.Put(ctx, complete.RecoveryID, data); err != nil {
		c.logger.Warn("Failed to write RECOVERY_STATES entry",
			"recovery_id", complete.RecoveryID, "error", err)
	}
}

// publishRecoveryComplete fires RecoveryComplete on recovery.complete.<slug>.
func (c *Component) publishRecoveryComplete(ctx context.Context, complete *payloads.RecoveryComplete) {
	baseMsg := message.NewBaseMessage(complete.Schema(), complete, componentName)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Warn("Failed to marshal RecoveryComplete",
			"recovery_id", complete.RecoveryID, "error", err)
		return
	}
	subject := payloads.RecoveryCompleteSubjectPrefix + complete.Slug
	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		c.logger.Warn("Failed to publish RecoveryComplete",
			"recovery_id", complete.RecoveryID,
			"subject", subject,
			"error", err)
		return
	}
	c.logger.Info("Published RecoveryComplete",
		"slug", complete.Slug,
		"recovery_id", complete.RecoveryID,
		"action", complete.Action,
		"recovery_succeeded", complete.RecoverySucceeded,
		"subject", subject)
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
