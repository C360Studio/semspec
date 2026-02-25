// Package structuralvalidator provides a JetStream processor that executes
// deterministic checklist validation as a workflow step.  It consumes
// ValidationRequest messages, runs the matching checks from
// .semspec/checklist.json, and publishes a ValidationResult.
package structuralvalidator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/workflow/reactive"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// Component implements the structural-validator processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger
	executor   *Executor

	// JetStream consumer state.
	consumer jetstream.Consumer

	// Lifecycle.
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics.
	triggersProcessed atomic.Int64
	checksPassed      atomic.Int64
	checksFailed      atomic.Int64
	errorsCount       atomic.Int64
	lastActivityMu    sync.RWMutex
	lastActivity      time.Time
}

// NewComponent constructs a structural-validator Component from raw JSON config
// and semstreams dependencies.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Apply defaults for any unset fields.
	defaults := DefaultConfig()
	if config.StreamName == "" {
		config.StreamName = defaults.StreamName
	}
	if config.ConsumerName == "" {
		config.ConsumerName = defaults.ConsumerName
	}
	if config.ChecklistPath == "" {
		config.ChecklistPath = defaults.ChecklistPath
	}
	if config.DefaultTimeout == "" {
		config.DefaultTimeout = defaults.DefaultTimeout
	}
	if config.Ports == nil {
		config.Ports = defaults.Ports
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	repoPath := resolveRepoPath(config.RepoPath)

	executor := NewExecutor(repoPath, config.ChecklistPath, config.GetDefaultTimeout())

	return &Component{
		name:       "structural-validator",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
		executor:   executor,
	}, nil
}

// resolveRepoPath determines the effective repository root.
// Priority: explicit config → SEMSPEC_REPO_PATH env var → working directory.
func resolveRepoPath(configured string) string {
	if configured != "" {
		return configured
	}
	if env := os.Getenv("SEMSPEC_REPO_PATH"); env != "" {
		return env
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}

// Initialize prepares the component for startup.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized structural-validator",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"checklist_path", c.config.ChecklistPath)
	return nil
}

// Start begins consuming ValidationRequest messages from JetStream.
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

	triggerSubject := "workflow.async.structural-validator"
	if c.config.Ports != nil && len(c.config.Ports.Inputs) > 0 {
		triggerSubject = c.config.Ports.Inputs[0].Subject
	}

	consumerConfig := jetstream.ConsumerConfig{
		Durable:       c.config.ConsumerName,
		FilterSubject: triggerSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		// Allow generous ack wait since checks may run long-lived commands.
		AckWait:    c.config.GetDefaultTimeout() + 30*time.Second,
		MaxDeliver: 3,
	}

	consumer, err := stream.CreateOrUpdateConsumer(subCtx, consumerConfig)
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("create consumer: %w", err)
	}
	c.consumer = consumer

	go c.consumeLoop(subCtx)

	c.logger.Info("structural-validator started",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"subject", triggerSubject)

	return nil
}

func (c *Component) rollbackStart(cancel context.CancelFunc) {
	c.mu.Lock()
	c.running = false
	c.cancel = nil
	c.mu.Unlock()
	cancel()
}

// consumeLoop fetches messages from the JetStream consumer in a tight loop
// until the context is cancelled.
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
			c.logger.Debug("Fetch timeout or error", "error", err)
			continue
		}

		for msg := range msgs.Messages() {
			c.handleMessage(ctx, msg)
		}

		if msgs.Error() != nil && msgs.Error() != context.DeadlineExceeded {
			c.logger.Warn("Message fetch error", "error", msgs.Error())
		}
	}
}

// handleMessage processes a single ValidationRequest message.
// Dispatched by the reactive workflow engine via PublishAsync. Publishes an
// AsyncStepResult callback on completion and a legacy result for direct consumers.
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	// Parse the trigger using the reactive engine's BaseMessage format.
	trigger, err := reactive.ParseReactivePayload[reactive.ValidationRequest](msg.Data())
	if err != nil {
		c.errorsCount.Add(1)
		c.logger.Error("Failed to parse trigger", "error", err)
		if nakErr := msg.Nak(); nakErr != nil {
			c.logger.Warn("Failed to NAK message", "error", nakErr)
		}
		return
	}

	if err := trigger.Validate(); err != nil {
		c.logger.Error("Invalid trigger", "error", err)
		// ACK invalid messages — they will not succeed on retry.
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Warn("Failed to ACK invalid message", "error", ackErr)
		}
		return
	}

	c.logger.Info("Processing structural validation trigger",
		"slug", trigger.Slug,
		"files_modified", len(trigger.FilesModified),
		"has_callback", trigger.HasCallback())

	result, err := c.executor.Execute(ctx, trigger)
	if err != nil {
		c.errorsCount.Add(1)
		c.logger.Error("Executor error",
			"slug", trigger.Slug,
			"error", err)

		// If workflow-dispatched, publish failure callback so the workflow can handle it.
		if trigger.HasCallback() {
			if cbErr := trigger.PublishCallbackFailure(ctx, c.natsClient, err.Error()); cbErr != nil {
				c.logger.Error("Failed to publish failure callback", "error", cbErr)
			}
		}

		if nakErr := msg.Nak(); nakErr != nil {
			c.logger.Warn("Failed to NAK message", "error", nakErr)
		}
		return
	}

	if result.Passed {
		c.checksPassed.Add(1)
	} else {
		c.checksFailed.Add(1)
	}

	// Publish workflow callback if dispatched via publish_async.
	if trigger.HasCallback() {
		if cbErr := trigger.PublishCallbackSuccess(ctx, c.natsClient, result); cbErr != nil {
			c.logger.Warn("Failed to publish callback result",
				"slug", trigger.Slug,
				"error", cbErr)
		}
	}

	// Also publish to legacy result subject for non-workflow consumers
	// (E2E tests, debugging, direct triggers).
	if err := c.publishResult(ctx, result); err != nil {
		c.logger.Warn("Failed to publish validation result",
			"slug", trigger.Slug,
			"error", err)
	}

	if ackErr := msg.Ack(); ackErr != nil {
		c.logger.Warn("Failed to ACK message", "error", ackErr)
	}

	c.logger.Info("Structural validation completed",
		"slug", trigger.Slug,
		"passed", result.Passed,
		"checks_run", result.ChecksRun,
		"warning", result.Warning)
}

// publishResult publishes a ValidationResult to JetStream.
// Subject: workflow.result.structural-validator.<slug>
func (c *Component) publishResult(ctx context.Context, result *ValidationResult) error {
	baseMsg := message.NewBaseMessage(result.Schema(), result, "structural-validator")

	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}

	js, err := c.natsClient.JetStream()
	if err != nil {
		return fmt.Errorf("get jetstream: %w", err)
	}

	subject := fmt.Sprintf("workflow.result.structural-validator.%s", result.Slug)
	if _, err := js.Publish(ctx, subject, data); err != nil {
		return fmt.Errorf("publish: %w", err)
	}
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
	c.logger.Info("structural-validator stopped",
		"triggers_processed", c.triggersProcessed.Load(),
		"checks_passed", c.checksPassed.Load(),
		"checks_failed", c.checksFailed.Load(),
		"errors", c.errorsCount.Load())

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "structural-validator",
		Type:        "processor",
		Description: "Executes deterministic checklist validation as a workflow step",
		Version:     "0.1.0",
	}
}

// InputPorts returns the configured input port definitions.
func (c *Component) InputPorts() []component.Port {
	if c.config.Ports == nil {
		return []component.Port{}
	}
	ports := make([]component.Port, len(c.config.Ports.Inputs))
	for i, def := range c.config.Ports.Inputs {
		ports[i] = component.Port{
			Name:        def.Name,
			Direction:   component.DirectionInput,
			Required:    def.Required,
			Description: def.Description,
			Config:      component.NATSPort{Subject: def.Subject},
		}
	}
	return ports
}

// OutputPorts returns the configured output port definitions.
func (c *Component) OutputPorts() []component.Port {
	if c.config.Ports == nil {
		return []component.Port{}
	}
	ports := make([]component.Port, len(c.config.Ports.Outputs))
	for i, def := range c.config.Ports.Outputs {
		ports[i] = component.Port{
			Name:        def.Name,
			Direction:   component.DirectionOutput,
			Required:    def.Required,
			Description: def.Description,
			Config:      component.NATSPort{Subject: def.Subject},
		}
	}
	return ports
}

// ConfigSchema returns the configuration schema.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return structuralValidatorSchema
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
		ErrorCount: int(c.errorsCount.Load()),
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
