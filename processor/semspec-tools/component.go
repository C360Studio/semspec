// Package semspectools provides a tool executor processor component
// that wraps file and git operations for agentic workflows.
package semspectools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/c360/semspec/tools/file"
	"github.com/c360/semspec/tools/git"
	"github.com/c360/semstreams/agentic"
	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/pkg/errs"
	agentictools "github.com/c360/semstreams/processor/agentic-tools"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	providerName      = "semspec"
	toolExecutePrefix = "tool.execute."
	toolResultPrefix  = "tool.result."
)

// semspecToolsSchema defines the configuration schema
var semspecToolsSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Component implements the semspec-tools processor
type Component struct {
	name       string
	config     Config
	registry   *agentictools.ExecutorRegistry
	natsClient *natsclient.Client
	logger     *slog.Logger
	platform   component.PlatformMeta

	// Lifecycle management
	running   bool
	startTime time.Time
	mu        sync.RWMutex

	// Metrics
	requestsProcessed int64
	errors            int64
	lastActivity      time.Time

	// Consumer management
	consumers   map[string]jetstream.ConsumeContext // tool name → consume context
	cancelFuncs []context.CancelFunc
}

// NewComponent creates a new semspec-tools processor component
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Use default config if ports not set
	if config.Ports == nil {
		config = DefaultConfig()
		// Re-unmarshal to get user-provided values
		if err := json.Unmarshal(rawConfig, &config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Resolve repo path to absolute
	absRepoPath, err := filepath.Abs(config.RepoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve repo path: %w", err)
	}

	// Verify repo path exists
	info, err := os.Stat(absRepoPath)
	if err != nil {
		return nil, fmt.Errorf("repo path does not exist: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("repo path is not a directory: %s", absRepoPath)
	}
	config.RepoPath = absRepoPath

	return &Component{
		name:       "semspec-tools",
		config:     config,
		registry:   agentictools.NewExecutorRegistry(),
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
		platform:   deps.Platform,
		consumers:  make(map[string]jetstream.ConsumeContext),
	}, nil
}

// Initialize prepares the component
func (c *Component) Initialize() error {
	return nil
}

// Start begins processing tool calls
func (c *Component) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return fmt.Errorf("component already running")
	}

	if c.natsClient == nil {
		return fmt.Errorf("NATS client required")
	}

	// Initialize tool executors
	fileExec := file.NewExecutor(c.config.RepoPath)
	gitExec := git.NewExecutor(c.config.RepoPath)

	// Register executors with local registry
	for _, tool := range fileExec.ListTools() {
		if err := c.registry.RegisterTool(tool.Name, fileExec); err != nil {
			return fmt.Errorf("register file tool %s: %w", tool.Name, err)
		}
	}
	for _, tool := range gitExec.ListTools() {
		if err := c.registry.RegisterTool(tool.Name, gitExec); err != nil {
			return fmt.Errorf("register git tool %s: %w", tool.Name, err)
		}
	}

	// Subscribe to tool execution requests (per-tool consumers)
	if err := c.subscribeToToolCalls(ctx); err != nil {
		return err
	}

	// Advertise available tools
	if err := c.advertiseTools(ctx, fileExec, gitExec); err != nil {
		c.logger.Warn("Failed to advertise tools", "error", err)
		// Not fatal - tools may still work if manually configured
	}

	// Start heartbeat in background
	c.startHeartbeat(ctx)

	c.running = true
	c.startTime = time.Now()

	c.logger.Info("Semspec tools started",
		"repo_path", c.config.RepoPath,
		"tools", len(c.registry.ListTools()))

	return nil
}

// subscribeToToolCalls creates a dedicated consumer per tool
func (c *Component) subscribeToToolCalls(ctx context.Context) error {
	// Wait for stream to be available
	if err := c.waitForStream(ctx); err != nil {
		return fmt.Errorf("wait for stream: %w", err)
	}

	// Get JetStream context
	js, err := c.natsClient.JetStream()
	if err != nil {
		return fmt.Errorf("get JetStream: %w", err)
	}

	tools := c.registry.ListTools()

	for _, tool := range tools {
		consumerName := consumerNameForTool(tool.Name)
		if c.config.ConsumerNameSuffix != "" {
			consumerName = consumerName + "-" + c.config.ConsumerNameSuffix
		}
		subject := toolExecutePrefix + tool.Name

		c.logger.Info("Creating consumer for tool",
			"tool", tool.Name,
			"consumer", consumerName,
			"subject", subject)

		consumerCfg := jetstream.ConsumerConfig{
			Name:          consumerName,
			Durable:       consumerName,
			FilterSubject: subject,
			DeliverPolicy: jetstream.DeliverNewPolicy,
			AckPolicy:     jetstream.AckExplicitPolicy,
			MaxDeliver:    3,
		}

		consumer, err := js.CreateOrUpdateConsumer(ctx, c.config.StreamName, consumerCfg)
		if err != nil {
			return fmt.Errorf("create consumer for %s: %w", tool.Name, err)
		}

		consumeCtx, err := consumer.Consume(func(msg jetstream.Msg) {
			c.handleToolCall(msg)
		})
		if err != nil {
			return fmt.Errorf("start consuming %s: %w", tool.Name, err)
		}

		c.consumers[tool.Name] = consumeCtx
	}

	c.logger.Info("Subscribed to tool calls",
		"stream", c.config.StreamName,
		"tools", len(tools))

	return nil
}

// waitForStream waits for the JetStream stream to be available
func (c *Component) waitForStream(ctx context.Context) error {
	js, err := c.natsClient.JetStream()
	if err != nil {
		return fmt.Errorf("get JetStream: %w", err)
	}

	maxRetries := 30
	retryInterval := 100 * time.Millisecond
	maxInterval := 2 * time.Second

	for i := 0; i < maxRetries; i++ {
		_, err := js.Stream(ctx, c.config.StreamName)
		if err == nil {
			return nil
		}

		c.logger.Debug("Stream not yet available, retrying",
			"stream", c.config.StreamName,
			"attempt", i+1)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryInterval):
			retryInterval = min(retryInterval*2, maxInterval)
		}
	}

	return fmt.Errorf("stream %s not found after %d retries", c.config.StreamName, maxRetries)
}

// consumerNameForTool converts tool name to valid NATS consumer name
func consumerNameForTool(toolName string) string {
	sanitized := strings.ReplaceAll(toolName, ".", "-")
	sanitized = strings.ReplaceAll(sanitized, "_", "-")
	return "semspec-tool-" + sanitized
}

// handleToolCall processes a tool execution request
func (c *Component) handleToolCall(msg jetstream.Msg) {
	// Create per-message context with timeout
	timeout := 30 * time.Second
	if c.config.Timeout != "" {
		if d, err := time.ParseDuration(c.config.Timeout); err == nil {
			timeout = d
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Parse tool call
	var call agentic.ToolCall
	if err := json.Unmarshal(msg.Data(), &call); err != nil {
		c.logger.Error("Failed to unmarshal tool call",
			"error", err,
			"subject", msg.Subject())
		_ = msg.Term() // Malformed data is never retryable
		return
	}

	c.logger.Debug("Processing tool call",
		"tool", call.Name,
		"call_id", call.ID)

	// Execute the tool
	startTime := time.Now()
	result, err := c.registry.Execute(ctx, call)
	duration := time.Since(startTime)

	if err != nil {
		c.logger.Error("Tool execution failed",
			"tool", call.Name,
			"call_id", call.ID,
			"error", err,
			"duration", duration)

		// Classify the execution error
		if errs.IsTransient(err) {
			c.logger.Warn("Transient execution error, will retry", "call_id", call.ID)
			_ = msg.Nak()
		} else {
			c.logger.Error("Non-retryable execution error", "call_id", call.ID)
			_ = msg.Term()
		}
		c.incrementErrors()
		return
	}

	if result.Error != "" {
		c.logger.Debug("Tool returned error",
			"tool", call.Name,
			"call_id", call.ID,
			"error", result.Error,
			"duration", duration)
	} else {
		c.logger.Debug("Tool executed successfully",
			"tool", call.Name,
			"call_id", call.ID,
			"duration", duration)
	}

	// Publish result
	if err := c.publishResult(ctx, result); err != nil {
		if errs.IsTransient(err) {
			c.logger.Warn("Transient error publishing result, will retry",
				"call_id", call.ID,
				"error", err)
			_ = msg.Nak()
		} else {
			c.logger.Error("Fatal error publishing result",
				"call_id", call.ID,
				"error", err)
			_ = msg.Term()
		}
		c.incrementErrors()
		return
	}

	// Acknowledge the message
	if err := msg.Ack(); err != nil {
		c.logger.Error("Failed to ack message", "error", err)
	}

	c.mu.Lock()
	c.requestsProcessed++
	c.lastActivity = time.Now()
	c.mu.Unlock()
}

// publishResult publishes a tool result to JetStream
func (c *Component) publishResult(ctx context.Context, result agentic.ToolResult) error {
	if result.CallID == "" {
		return errs.WrapInvalid(
			fmt.Errorf("empty call ID in result"),
			"semspec-tools", "publishResult", "validate result")
	}

	data, err := json.Marshal(result)
	if err != nil {
		return errs.WrapInvalid(err, "semspec-tools", "publishResult", "marshal result")
	}

	subject := toolResultPrefix + result.CallID

	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		return errs.WrapTransient(err, "semspec-tools", "publishResult", "publish to "+subject)
	}

	return nil
}

// advertiseTools publishes tool registrations
func (c *Component) advertiseTools(ctx context.Context, executors ...agentictools.ToolExecutor) error {
	for _, exec := range executors {
		for _, tool := range exec.ListTools() {
			reg := ExternalToolRegistration{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
				Provider:    providerName,
				Timestamp:   time.Now(),
			}

			data, err := json.Marshal(reg)
			if err != nil {
				return fmt.Errorf("marshal registration: %w", err)
			}

			subject := "tool.register." + tool.Name
			if err := c.natsClient.Publish(ctx, subject, data); err != nil {
				return fmt.Errorf("publish %s: %w", tool.Name, err)
			}

			c.logger.Debug("Registered tool", "name", tool.Name)
		}
	}

	return nil
}

// ExternalToolRegistration wraps tool definition for external registration
type ExternalToolRegistration struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	Provider    string         `json:"provider"`
	Timestamp   time.Time      `json:"timestamp"`
}

// ToolHeartbeat signals tool provider is alive
type ToolHeartbeat struct {
	Provider  string    `json:"provider"`
	Tools     []string  `json:"tools"`
	Timestamp time.Time `json:"timestamp"`
}

// ToolUnregister signals tool removal
type ToolUnregister struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
}

// startHeartbeat runs periodic heartbeat in background
func (c *Component) startHeartbeat(ctx context.Context) {
	interval := 10 * time.Second
	if c.config.HeartbeatInterval != "" {
		if d, err := time.ParseDuration(c.config.HeartbeatInterval); err == nil {
			interval = d
		}
	}

	hbCtx, cancel := context.WithCancel(ctx)
	c.cancelFuncs = append(c.cancelFuncs, cancel)

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Send initial heartbeat immediately
		c.sendHeartbeat(hbCtx)

		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				c.sendHeartbeat(hbCtx)
			}
		}
	}()
}

func (c *Component) sendHeartbeat(ctx context.Context) {
	tools := c.registry.ListTools()
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}

	hb := ToolHeartbeat{
		Provider:  providerName,
		Tools:     names,
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(hb)
	if err != nil {
		c.logger.Error("Failed to marshal heartbeat", "error", err)
		return
	}

	subject := "tool.heartbeat." + providerName
	if err := c.natsClient.Publish(ctx, subject, data); err != nil {
		c.logger.Warn("Failed to send heartbeat", "error", err)
	}
}

// unregisterTools sends unregister messages for all tools
func (c *Component) unregisterTools(ctx context.Context) {
	tools := c.registry.ListTools()
	for _, tool := range tools {
		unreg := ToolUnregister{
			Name:     tool.Name,
			Provider: providerName,
		}

		data, err := json.Marshal(unreg)
		if err != nil {
			continue
		}

		subject := "tool.unregister." + tool.Name
		_ = c.natsClient.Publish(ctx, subject, data)
		c.logger.Debug("Unregistered tool", "name", tool.Name)
	}
}

// incrementErrors safely increments the error counter
func (c *Component) incrementErrors() {
	c.mu.Lock()
	c.errors++
	c.mu.Unlock()
}

// Stop gracefully stops the component
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	// Cancel background goroutines
	for _, cancel := range c.cancelFuncs {
		cancel()
	}
	c.cancelFuncs = nil

	// Graceful unregister before stopping
	c.unregisterTools(context.Background())

	// Stop all consumers
	for name, consumeCtx := range c.consumers {
		consumeCtx.Stop()
		c.logger.Debug("Stopped consumer", "tool", name)
	}
	c.consumers = make(map[string]jetstream.ConsumeContext)

	c.running = false
	c.logger.Info("Semspec tools stopped",
		"requests_processed", c.requestsProcessed,
		"errors", c.errors)

	return nil
}

// Discoverable interface implementation

// Meta returns component metadata
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "semspec-tools",
		Type:        "processor",
		Description: "File and git tool executor for agentic workflows",
		Version:     "0.1.0",
	}
}

// InputPorts returns configured input port definitions
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

// OutputPorts returns configured output port definitions
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

// ConfigSchema returns the configuration schema
func (c *Component) ConfigSchema() component.ConfigSchema {
	return semspecToolsSchema
}

// Health returns the current health status
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return component.HealthStatus{
		Healthy:    c.running,
		LastCheck:  time.Now(),
		ErrorCount: int(c.errors),
		Uptime:     time.Since(c.startTime),
		Status:     c.getStatus(),
	}
}

// getStatus returns a status string
func (c *Component) getStatus() string {
	if c.running {
		return "running"
	}
	return "stopped"
}

// DataFlow returns current data flow metrics
func (c *Component) DataFlow() component.FlowMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var errorRate float64
	total := c.requestsProcessed + c.errors
	if total > 0 {
		errorRate = float64(c.errors) / float64(total)
	}

	return component.FlowMetrics{
		MessagesPerSecond: 0,
		BytesPerSecond:    0,
		ErrorRate:         errorRate,
		LastActivity:      c.lastActivity,
	}
}
