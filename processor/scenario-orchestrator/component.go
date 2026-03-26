// Package scenarioorchestrator provides the scenario-orchestrator component.
// It is the entry point for ADR-025 Phase 4 scenario execution.
//
// The orchestrator:
//  1. Receives an orchestration trigger for a plan (on scenario.orchestrate.<planSlug>).
//  2. Queries the graph for all Scenarios in the plan with status=pending or status=dirty.
//  3. Triggers a scenario-execution-loop workflow for each unmet Scenario.
//
// The actual decomposition and execution are handled by the scenario-executor
// component (processor/scenario-executor). This component is deliberately
// minimal — it dispatches, then ACKs.
package scenarioorchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// Component implements the scenario-orchestrator processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	// repoRoot is resolved once at construction from SEMSPEC_REPO_PATH or cwd.
	// It is used to load requirement and scenario data fresh from disk each dispatch cycle.
	repoRoot     string
	tripleWriter *graphutil.TripleWriter

	// completedRequirements caches RequirementExecutionCompleteEvent data keyed
	// by RequirementID. Populated by subscribing to requirement execution
	// completion events so that prerequisite context can be injected into
	// downstream RequirementExecutionRequests.
	completedRequirements sync.Map // map[string]*workflow.RequirementExecutionCompleteEvent

	// Lifecycle
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics
	triggersProcessed     atomic.Int64
	requirementsTriggered atomic.Int64
	triggersFailed        atomic.Int64
	lastActivityMu        sync.RWMutex
	lastActivity          time.Time
}

// NewComponent creates a new scenario-orchestrator processor.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Apply defaults for unset fields.
	defaults := DefaultConfig()
	if config.StreamName == "" {
		config.StreamName = defaults.StreamName
	}
	if config.ConsumerName == "" {
		config.ConsumerName = defaults.ConsumerName
	}
	if config.TriggerSubject == "" {
		config.TriggerSubject = defaults.TriggerSubject
	}
	if config.WorkflowTriggerSubject == "" {
		config.WorkflowTriggerSubject = defaults.WorkflowTriggerSubject
	}
	if config.ExecutionTimeout == "" {
		config.ExecutionTimeout = defaults.ExecutionTimeout
	}
	if config.MaxConcurrent == 0 {
		config.MaxConcurrent = defaults.MaxConcurrent
	}
	if config.Ports == nil {
		config.Ports = defaults.Ports
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return nil, fmt.Errorf("resolve repo root: %w", err)
	}

	return &Component{
		name:       "scenario-orchestrator",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
		repoRoot:   repoRoot,
	}, nil
}

// resolveRepoRoot returns the repository root path, preferring SEMSPEC_REPO_PATH
// and falling back to the current working directory.
func resolveRepoRoot() (string, error) {
	if root := os.Getenv("SEMSPEC_REPO_PATH"); root != "" {
		return root, nil
	}
	root, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	return root, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("initialized scenario-orchestrator",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"trigger_subject", c.config.TriggerSubject,
		"max_concurrent", c.config.MaxConcurrent)
	return nil
}

// Start begins consuming scenario orchestration triggers.
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

	// Initialize TripleWriter for workflow state operations.
	c.tripleWriter = &graphutil.TripleWriter{
		NATSClient:    c.natsClient,
		Logger:        c.logger,
		ComponentName: "scenario-orchestrator",
	}

	// Push-based consumption — messages arrive via callback, no polling delay.
	cfg := natsclient.StreamConsumerConfig{
		StreamName:    c.config.StreamName,
		ConsumerName:  c.config.ConsumerName,
		FilterSubject: c.config.TriggerSubject,
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       c.config.GetExecutionTimeout() + 30*time.Second,
	}
	if err := c.natsClient.ConsumeStreamWithConfig(subCtx, cfg, c.handleTriggerPush); err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("consume orchestration triggers: %w", err)
	}

	// Consumer 2: requirement execution completions — cache results for prereq context.
	completionCfg := natsclient.StreamConsumerConfig{
		StreamName:    "WORKFLOW",
		ConsumerName:  "scenario-orchestrator-completions",
		FilterSubject: workflow.RequirementExecutionComplete.Pattern,
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       30 * time.Second,
	}
	if err := c.natsClient.ConsumeStreamWithConfig(subCtx, completionCfg, c.handleRequirementComplete); err != nil {
		c.logger.Warn("failed to subscribe to requirement completions — prereq context will use fallback",
			"error", err)
		// Non-fatal: orchestrator can still dispatch without cached prereq context.
	}

	c.logger.Info("scenario-orchestrator started",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"subject", c.config.TriggerSubject,
		"max_concurrent", c.config.MaxConcurrent)

	return nil
}

func (c *Component) rollbackStart(cancel context.CancelFunc) {
	c.mu.Lock()
	c.running = false
	c.cancel = nil
	c.mu.Unlock()
	cancel()
}

// handleTriggerPush is the push-based callback for ConsumeStreamWithConfig.
// Messages arrive immediately when published — no polling delay.
func (c *Component) handleTriggerPush(ctx context.Context, msg jetstream.Msg) {
	c.handleTrigger(ctx, msg)
}

// OrchestratorTrigger is the payload received on scenario.orchestrate.<planSlug>.
// It carries the plan slug. Scenarios and requirements are loaded from disk.
// OrchestratorTrigger is an alias for the registered payload type.
// Using the payload type directly ensures all fields survive deserialization.
type OrchestratorTrigger = payloads.ScenarioOrchestrationTrigger

// handleTrigger processes a single orchestration trigger message.
func (c *Component) handleTrigger(ctx context.Context, msg jetstream.Msg) {
	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	var base message.BaseMessage
	if err := json.Unmarshal(msg.Data(), &base); err != nil {
		c.logger.Error("failed to parse orchestration trigger envelope", "error", err)
		if err := msg.Nak(); err != nil {
			c.logger.Warn("failed to NAK message", "error", err)
		}
		return
	}

	trigger, ok := base.Payload().(*OrchestratorTrigger)
	if !ok {
		c.logger.Error("unexpected payload type in orchestration trigger",
			"type", fmt.Sprintf("%T", base.Payload()))
		if err := msg.Nak(); err != nil {
			c.logger.Warn("failed to NAK message", "error", err)
		}
		return
	}

	if trigger.PlanSlug == "" {
		c.logger.Error("orchestration trigger missing plan_slug")
		if err := msg.Nak(); err != nil {
			c.logger.Warn("failed to NAK message", "error", err)
		}
		return
	}

	c.logger.Info("orchestrating requirements",
		"plan_slug", trigger.PlanSlug,
		"trace_id", trigger.TraceID)

	// Apply execution timeout for the dispatch cycle.
	dispatchCtx, cancel := context.WithTimeout(ctx, c.config.GetExecutionTimeout())
	defer cancel()

	if err := c.dispatchRequirements(dispatchCtx, trigger); err != nil {
		c.logger.Error("requirement dispatch failed",
			"plan_slug", trigger.PlanSlug,
			"error", err)
		c.triggersFailed.Add(1)
		if err := msg.Nak(); err != nil {
			c.logger.Warn("failed to NAK message after dispatch error", "error", err)
		}
		return
	}

	if err := msg.Ack(); err != nil {
		c.logger.Warn("failed to ACK message", "error", err)
	}
}

// dispatchRequirements applies requirement-DAG gating and then triggers a
// requirement-execution-loop for each ready requirement using bounded concurrency
// controlled by config.MaxConcurrent.
//
// DAG gating logic:
//  1. Load all requirements and scenarios for the plan from the workflow manager.
//  2. A requirement is "complete" when every one of its scenarios is passing or skipped.
//  3. A requirement is "ready" when all its DependsOn requirements are complete
//     AND it has at least one non-terminal scenario.
func (c *Component) dispatchRequirements(ctx context.Context, trigger *OrchestratorTrigger) error {
	if c.tripleWriter == nil {
		c.logger.Info("no KV store configured, skipping requirement dispatch", "plan_slug", trigger.PlanSlug)
		return nil
	}
	// Use requirements from trigger payload (populated by plan-manager from cache).
	// Fall back to graph read for backward compat with older triggers.
	requirements := trigger.Requirements
	if len(requirements) == 0 {
		var err error
		requirements, err = workflow.LoadRequirements(ctx, c.tripleWriter, trigger.PlanSlug)
		if err != nil {
			return fmt.Errorf("load requirements for %s: %w", trigger.PlanSlug, err)
		}
	}
	if len(requirements) == 0 {
		c.logger.Info("no requirements found for plan", "plan_slug", trigger.PlanSlug)
		return nil
	}

	allScenarios, err := workflow.LoadScenarios(ctx, c.tripleWriter, trigger.PlanSlug)
	if err != nil {
		return fmt.Errorf("load scenarios for %s: %w", trigger.PlanSlug, err)
	}

	// Apply DAG gating — only dispatch requirements whose upstream deps are satisfied.
	toDispatch := filterReadyRequirements(requirements, allScenarios)

	blocked := len(requirements) - len(toDispatch)
	c.logger.Info("requirement DAG gating applied",
		"plan_slug", trigger.PlanSlug,
		"total_requirements", len(requirements),
		"ready_count", len(toDispatch),
		"blocked_count", blocked)

	if len(toDispatch) == 0 {
		c.logger.Info("all requirements blocked by upstream dependencies", "plan_slug", trigger.PlanSlug)
		return nil
	}

	// Group scenarios by requirement ID for dispatch.
	scenariosByReq := make(map[string][]workflow.Scenario, len(requirements))
	for _, s := range allScenarios {
		scenariosByReq[s.RequirementID] = append(scenariosByReq[s.RequirementID], s)
	}

	sem := make(chan struct{}, c.config.MaxConcurrent)
	var wg sync.WaitGroup
	errs := make(chan error, len(toDispatch))

	for _, req := range toDispatch {
		if ctx.Err() != nil {
			break
		}

		scenarios := scenariosByReq[req.ID]
		prereqs := c.buildPrereqContext(req, requirements)

		wg.Add(1)
		go func(r workflow.Requirement, sc []workflow.Scenario, deps []payloads.PrereqContext) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			if err := c.triggerRequirementExecution(ctx, trigger.PlanSlug, trigger.TraceID, r, sc, deps); err != nil {
				c.logger.Error("failed to trigger requirement execution",
					"requirement_id", r.ID,
					"error", err)
				errs <- err
			} else {
				c.requirementsTriggered.Add(1)
			}
		}(req, scenarios, prereqs)
	}

	wg.Wait()
	close(errs)

	var firstErr error
	for err := range errs {
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// buildPrereqContext assembles PrereqContext for a requirement's DependsOn list
// from the cached completion events. Falls back to requirement metadata only
// when completion data is unavailable.
func (c *Component) buildPrereqContext(req workflow.Requirement, allReqs []workflow.Requirement) []payloads.PrereqContext {
	if len(req.DependsOn) == 0 {
		return nil
	}

	// Index all requirements for metadata lookup.
	reqIndex := make(map[string]workflow.Requirement, len(allReqs))
	for _, r := range allReqs {
		reqIndex[r.ID] = r
	}

	var prereqs []payloads.PrereqContext
	for _, depID := range req.DependsOn {
		pc := payloads.PrereqContext{RequirementID: depID}

		// Try cached completion event first (has files + summary).
		if cached, ok := c.completedRequirements.Load(depID); ok {
			evt := cached.(*workflow.RequirementExecutionCompleteEvent)
			pc.Title = evt.Title
			pc.Description = evt.Description
			pc.FilesModified = evt.FilesModified
			pc.Summary = evt.Summary
		} else if dep, ok := reqIndex[depID]; ok {
			// Fallback: requirement metadata only.
			pc.Title = dep.Title
			pc.Description = dep.Description
		}

		prereqs = append(prereqs, pc)
	}
	return prereqs
}

// triggerRequirementExecution publishes a RequirementExecutionRequest as a BaseMessage
// to the requirement-executor component via the configured workflow trigger subject.
func (c *Component) triggerRequirementExecution(
	ctx context.Context,
	planSlug, traceID string,
	req workflow.Requirement,
	scenarios []workflow.Scenario,
	prereqs []payloads.PrereqContext,
) error {
	execReq := &payloads.RequirementExecutionRequest{
		RequirementID: req.ID,
		Slug:          planSlug,
		Title:         req.Title,
		Description:   req.Description,
		Scenarios:     scenarios,
		DependsOn:     prereqs,
		TraceID:       traceID,
	}

	baseMsg := message.NewBaseMessage(execReq.Schema(), execReq, "scenario-orchestrator")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal requirement execution trigger: %w", err)
	}

	js, err := c.natsClient.JetStream()
	if err != nil {
		return fmt.Errorf("get jetstream: %w", err)
	}

	if _, err := js.Publish(ctx, c.config.WorkflowTriggerSubject, data); err != nil {
		return fmt.Errorf("publish to %s: %w", c.config.WorkflowTriggerSubject, err)
	}

	c.logger.Info("Triggered requirement execution",
		"requirement_id", req.ID,
		"plan_slug", planSlug,
		"scenario_count", len(scenarios),
		"prereq_count", len(prereqs),
		"subject", c.config.WorkflowTriggerSubject,
	)
	return nil
}

// handleRequirementComplete caches completion events for prereq context enrichment.
func (c *Component) handleRequirementComplete(_ context.Context, msg jetstream.Msg) {
	var event workflow.RequirementExecutionCompleteEvent
	if err := json.Unmarshal(msg.Data(), &event); err != nil {
		c.logger.Warn("failed to parse requirement completion event", "error", err)
		_ = msg.Ack()
		return
	}

	c.completedRequirements.Store(event.RequirementID, &event)
	c.logger.Debug("cached requirement completion",
		"requirement_id", event.RequirementID,
		"slug", event.Slug,
		"outcome", event.Outcome)

	_ = msg.Ack()
}

// Stop gracefully stops the component.
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	if c.cancel != nil {
		c.cancel()
	}

	c.running = false
	c.logger.Info("scenario-orchestrator stopped",
		"triggers_processed", c.triggersProcessed.Load(),
		"requirements_triggered", c.requirementsTriggered.Load(),
		"triggers_failed", c.triggersFailed.Load())

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "scenario-orchestrator",
		Type:        "processor",
		Description: "Dispatches scenario-execution-loop workflows for pending Scenarios in a plan (ADR-025 Phase 4)",
		Version:     "0.1.0",
	}
}

// InputPorts returns configured input port definitions.
func (c *Component) InputPorts() []component.Port {
	if c.config.Ports == nil {
		return []component.Port{}
	}

	ports := make([]component.Port, len(c.config.Ports.Inputs))
	for i, portDef := range c.config.Ports.Inputs {
		ports[i] = component.Port{
			Name:        portDef.Name,
			Direction:   component.DirectionInput,
			Required:    portDef.Required,
			Description: portDef.Description,
			Config: component.NATSPort{
				Subject: portDef.Subject,
			},
		}
	}
	return ports
}

// OutputPorts returns configured output port definitions.
func (c *Component) OutputPorts() []component.Port {
	if c.config.Ports == nil {
		return []component.Port{}
	}

	ports := make([]component.Port, len(c.config.Ports.Outputs))
	for i, portDef := range c.config.Ports.Outputs {
		ports[i] = component.Port{
			Name:        portDef.Name,
			Direction:   component.DirectionOutput,
			Required:    portDef.Required,
			Description: portDef.Description,
			Config: component.NATSPort{
				Subject: portDef.Subject,
			},
		}
	}
	return ports
}

// ConfigSchema returns the configuration schema.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return orchestratorSchema
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
		ErrorCount: int(c.triggersFailed.Load()),
		Uptime:     time.Since(startTime),
		Status:     status,
	}
}

// DataFlow returns current data flow metrics.
func (c *Component) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{
		MessagesPerSecond: 0,
		BytesPerSecond:    0,
		ErrorRate:         0,
		LastActivity:      c.getLastActivity(),
	}
}

// IsRunning returns whether the component is running.
func (c *Component) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
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
