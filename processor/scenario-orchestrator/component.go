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
	"sync"
	"sync/atomic"
	"time"

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

	// JetStream consumer
	consumer jetstream.Consumer
	stream   jetstream.Stream

	// Lifecycle
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics
	triggersProcessed  atomic.Int64
	scenariosTriggered atomic.Int64
	triggersFailed     atomic.Int64
	lastActivityMu     sync.RWMutex
	lastActivity       time.Time
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

	return &Component{
		name:       "scenario-orchestrator",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
	}, nil
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

	js, err := c.natsClient.JetStream()
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("get jetstream: %w", err)
	}

	stream, err := js.Stream(subCtx, c.config.StreamName)
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("get stream %s: %w", c.config.StreamName, err)
	}
	c.stream = stream

	consumerConfig := jetstream.ConsumerConfig{
		Durable:       c.config.ConsumerName,
		FilterSubject: c.config.TriggerSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       c.config.GetExecutionTimeout() + 30*time.Second,
		MaxDeliver:    3,
	}

	consumer, err := stream.CreateOrUpdateConsumer(subCtx, consumerConfig)
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("create consumer: %w", err)
	}
	c.consumer = consumer

	go c.consumeLoop(subCtx)

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

// consumeLoop continuously fetches orchestration triggers.
func (c *Component) consumeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msgs, err := c.consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.logger.Debug("fetch timeout or error", "error", err)
			continue
		}

		for msg := range msgs.Messages() {
			c.handleTrigger(ctx, msg)
		}

		if msgs.Error() != nil && msgs.Error() != context.DeadlineExceeded {
			c.logger.Warn("message fetch error", "error", msgs.Error())
		}
	}
}

// OrchestratorTrigger is the payload received on scenario.orchestrate.<planSlug>.
// It carries the plan slug and the list of Scenarios that require execution.
// This is a lightweight trigger — the heavy graph query for scenarios can be
// done here or pre-computed by the caller.
type OrchestratorTrigger struct {
	PlanSlug  string        `json:"plan_slug"`
	Scenarios []ScenarioRef `json:"scenarios"`
	TraceID   string        `json:"trace_id,omitempty"`
}

// ScenarioRef is a lightweight reference to a Scenario with the data needed
// to trigger the scenario-execution-loop.
type ScenarioRef struct {
	ScenarioID string `json:"scenario_id"`
	Prompt     string `json:"prompt"`
	Role       string `json:"role,omitempty"`
	Model      string `json:"model,omitempty"`
}

// handleTrigger processes a single orchestration trigger message.
func (c *Component) handleTrigger(ctx context.Context, msg jetstream.Msg) {
	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	var trigger OrchestratorTrigger
	if err := json.Unmarshal(msg.Data(), &trigger); err != nil {
		c.logger.Error("failed to parse orchestration trigger", "error", err)
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

	c.logger.Info("orchestrating scenarios",
		"plan_slug", trigger.PlanSlug,
		"scenario_count", len(trigger.Scenarios),
		"trace_id", trigger.TraceID)

	// Apply execution timeout for the dispatch cycle.
	dispatchCtx, cancel := context.WithTimeout(ctx, c.config.GetExecutionTimeout())
	defer cancel()

	if err := c.dispatchScenarios(dispatchCtx, trigger); err != nil {
		c.logger.Error("scenario dispatch failed",
			"plan_slug", trigger.PlanSlug,
			"error", err)
		c.triggersFailed.Add(1)
		// NAK to allow retry — the message will be redelivered.
		if err := msg.Nak(); err != nil {
			c.logger.Warn("failed to NAK message after dispatch error", "error", err)
		}
		return
	}

	if err := msg.Ack(); err != nil {
		c.logger.Warn("failed to ACK message", "error", err)
	}

	c.logger.Info("scenario orchestration complete",
		"plan_slug", trigger.PlanSlug,
		"scenario_count", len(trigger.Scenarios))
}

// dispatchScenarios triggers a scenario-execution-loop for each Scenario reference
// using bounded concurrency controlled by config.MaxConcurrent.
func (c *Component) dispatchScenarios(ctx context.Context, trigger OrchestratorTrigger) error {
	if len(trigger.Scenarios) == 0 {
		c.logger.Info("no scenarios to dispatch", "plan_slug", trigger.PlanSlug)
		return nil
	}

	sem := make(chan struct{}, c.config.MaxConcurrent)
	var wg sync.WaitGroup
	errs := make(chan error, len(trigger.Scenarios))

	for _, ref := range trigger.Scenarios {
		if ctx.Err() != nil {
			break
		}

		wg.Add(1)
		go func(r ScenarioRef) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			if err := c.triggerScenarioExecution(ctx, trigger.PlanSlug, trigger.TraceID, r); err != nil {
				c.logger.Error("failed to trigger scenario execution",
					"scenario_id", r.ScenarioID,
					"error", err)
				errs <- err
			} else {
				c.scenariosTriggered.Add(1)
			}
		}(ref)
	}

	wg.Wait()
	close(errs)

	// Collect any errors from dispatch goroutines.
	var firstErr error
	for err := range errs {
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// triggerScenarioExecution publishes a ScenarioExecutionRequest as a BaseMessage
// to the scenario-executor component via the configured workflow trigger subject.
func (c *Component) triggerScenarioExecution(ctx context.Context, planSlug, traceID string, ref ScenarioRef) error {
	req := &payloads.ScenarioExecutionRequest{
		ScenarioID: ref.ScenarioID,
		Slug:       planSlug,
		Prompt:     ref.Prompt,
		Role:       ref.Role,
		Model:      ref.Model,
		TraceID:    traceID,
	}

	baseMsg := message.NewBaseMessage(req.Schema(), req, "scenario-orchestrator")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal scenario execution trigger: %w", err)
	}

	js, err := c.natsClient.JetStream()
	if err != nil {
		return fmt.Errorf("get jetstream: %w", err)
	}

	if _, err := js.Publish(ctx, c.config.WorkflowTriggerSubject, data); err != nil {
		return fmt.Errorf("publish to %s: %w", c.config.WorkflowTriggerSubject, err)
	}

	c.logger.Info("Triggered scenario execution",
		"scenario_id", ref.ScenarioID,
		"plan_slug", planSlug,
		"subject", c.config.WorkflowTriggerSubject,
	)
	return nil
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
		"scenarios_triggered", c.scenariosTriggered.Load(),
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
