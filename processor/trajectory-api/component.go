// Package trajectoryapi provides HTTP endpoints for querying LLM call trajectories.
// It aggregates data from LLM_CALLS and AGENT_LOOPS KV buckets to provide
// unified trajectory views for debugging and observability.
package trajectoryapi

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
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// Component implements the trajectory-api component.
// It provides HTTP endpoints for querying LLM call trajectories and tool call history.
// NOTE: LLM calls are now stored in the knowledge graph, not KV.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	// KV buckets
	// NOTE: llmCallsBucket removed - LLM calls are now graph entities
	toolCallsBucket jetstream.KeyValue
	loopsBucket     jetstream.KeyValue

	// Graph querier for LLM call entities
	llmCallQuerier *LLMCallQuerier

	// Workflow manager for accessing plan data
	workflowManager *workflow.Manager

	// Lifecycle state machine
	// States: 0=stopped, 1=starting, 2=running, 3=stopping
	state     atomic.Int32
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc
}

const (
	stateStopped  = 0
	stateStarting = 1
	stateRunning  = 2
	stateStopping = 3
)

// NewComponent creates a new trajectory-api component.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Apply defaults
	defaults := DefaultConfig()
	if config.LLMCallsBucket == "" {
		config.LLMCallsBucket = defaults.LLMCallsBucket
	}
	if config.ToolCallsBucket == "" {
		config.ToolCallsBucket = defaults.ToolCallsBucket
	}
	if config.LoopsBucket == "" {
		config.LoopsBucket = defaults.LoopsBucket
	}
	if config.GraphGatewayURL == "" {
		config.GraphGatewayURL = defaults.GraphGatewayURL
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	logger := deps.GetLogger()

	return &Component{
		name:       "trajectory-api",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     logger,
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized trajectory-api",
		"llm_calls_bucket", c.config.LLMCallsBucket,
		"tool_calls_bucket", c.config.ToolCallsBucket,
		"loops_bucket", c.config.LoopsBucket)
	return nil
}

// Start begins the component.
func (c *Component) Start(ctx context.Context) error {
	// Atomically transition from stopped to starting
	if !c.state.CompareAndSwap(stateStopped, stateStarting) {
		currentState := c.state.Load()
		if currentState == stateRunning || currentState == stateStarting {
			return fmt.Errorf("component already running or starting")
		}
		return fmt.Errorf("component in invalid state: %d", currentState)
	}

	// Ensure we transition to stopped if setup fails
	defer func() {
		if c.state.Load() == stateStarting {
			c.state.Store(stateStopped)
		}
	}()

	if c.natsClient == nil {
		return fmt.Errorf("NATS client required")
	}

	// Get JetStream context
	js, err := c.natsClient.JetStream()
	if err != nil {
		return fmt.Errorf("get jetstream: %w", err)
	}

	// Get KV buckets - these may not exist yet, so we'll try lazily
	// NOTE: LLM calls are now stored in the knowledge graph, not KV.
	// The llmCallsBucket is no longer used.

	toolCallsBucket, err := js.KeyValue(ctx, c.config.ToolCallsBucket)
	if err != nil {
		c.logger.Warn("Tool calls bucket not found, will retry on queries",
			"bucket", c.config.ToolCallsBucket,
			"error", err)
	}

	loopsBucket, err := js.KeyValue(ctx, c.config.LoopsBucket)
	if err != nil {
		c.logger.Warn("Loops bucket not found, will retry on queries",
			"bucket", c.config.LoopsBucket,
			"error", err)
	}

	// Initialize workflow manager for plan access
	repoRoot := c.config.RepoRoot
	if repoRoot == "" {
		repoRoot = os.Getenv("SEMSPEC_REPO_PATH")
	}
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			c.logger.Warn("Failed to get working directory, using '.'", "error", err)
			repoRoot = "."
		}
	}
	workflowManager := workflow.NewManager(repoRoot)

	// Initialize graph querier for LLM calls
	var llmCallQuerier *LLMCallQuerier
	if c.config.GraphGatewayURL != "" {
		llmCallQuerier = NewLLMCallQuerier(c.config.GraphGatewayURL)
		c.logger.Debug("Initialized LLM call graph querier", "url", c.config.GraphGatewayURL)
	}

	// Create cancellation context
	_, cancel := context.WithCancel(ctx)

	// Update state atomically with lock for complex state
	c.mu.Lock()
	c.toolCallsBucket = toolCallsBucket
	c.loopsBucket = loopsBucket
	c.llmCallQuerier = llmCallQuerier
	c.workflowManager = workflowManager
	c.cancel = cancel
	c.startTime = time.Now()
	c.mu.Unlock()

	// Transition to running
	c.state.Store(stateRunning)

	c.logger.Info("trajectory-api started",
		"tool_calls_bucket", c.config.ToolCallsBucket,
		"loops_bucket", c.config.LoopsBucket,
		"note", "LLM calls are now stored in the knowledge graph")

	return nil
}

// Stop gracefully stops the component.
func (c *Component) Stop(_ time.Duration) error {
	// Atomically transition from running to stopping
	if !c.state.CompareAndSwap(stateRunning, stateStopping) {
		currentState := c.state.Load()
		if currentState == stateStopped {
			return nil // Already stopped
		}
		if currentState == stateStopping {
			return nil // Already stopping
		}
		return fmt.Errorf("component in unexpected state: %d", currentState)
	}

	// Get and clear cancel function
	c.mu.Lock()
	cancel := c.cancel
	c.cancel = nil
	c.mu.Unlock()

	// Cancel context
	if cancel != nil {
		cancel()
	}

	// Transition to stopped
	c.state.Store(stateStopped)

	c.logger.Info("trajectory-api stopped")

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "trajectory-api",
		Type:        "processor",
		Description: "HTTP endpoints for querying LLM call trajectories and agent loop history",
		Version:     "0.1.0",
	}
}

// InputPorts returns configured input port definitions.
func (c *Component) InputPorts() []component.Port {
	return []component.Port{}
}

// OutputPorts returns configured output port definitions.
func (c *Component) OutputPorts() []component.Port {
	return []component.Port{}
}

// ConfigSchema returns the configuration schema.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return trajectoryAPISchema
}

// Health returns the current health status.
func (c *Component) Health() component.HealthStatus {
	state := c.state.Load()
	running := state == stateRunning

	c.mu.RLock()
	startTime := c.startTime
	c.mu.RUnlock()

	status := "stopped"
	switch state {
	case stateStarting:
		status = "starting"
	case stateRunning:
		status = "running"
	case stateStopping:
		status = "stopping"
	}

	return component.HealthStatus{
		Healthy:   running,
		LastCheck: time.Now(),
		Uptime:    time.Since(startTime),
		Status:    status,
	}
}

// DataFlow returns current data flow metrics.
func (c *Component) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{}
}

// NOTE: getLLMCallsBucket removed - LLM calls are now stored in the knowledge graph.
// Use graph queries with llm.call.* predicates to access LLM call data.

// getToolCallsBucket gets the tool calls bucket, attempting to reconnect if needed.
func (c *Component) getToolCallsBucket(ctx context.Context) (jetstream.KeyValue, error) {
	c.mu.RLock()
	bucket := c.toolCallsBucket
	c.mu.RUnlock()

	if bucket != nil {
		return bucket, nil
	}

	// Upgrade to write lock and check again (double-checked locking)
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check again after acquiring write lock
	if c.toolCallsBucket != nil {
		return c.toolCallsBucket, nil
	}

	// Try to get the bucket (it may have been created since startup)
	js, err := c.natsClient.JetStream()
	if err != nil {
		return nil, fmt.Errorf("get jetstream: %w", err)
	}

	bucket, err = js.KeyValue(ctx, c.config.ToolCallsBucket)
	if err != nil {
		return nil, fmt.Errorf("bucket not found: %w", err)
	}

	// Cache it
	c.toolCallsBucket = bucket

	return bucket, nil
}

// getLoopsBucket gets the loops bucket, attempting to reconnect if needed.
func (c *Component) getLoopsBucket(ctx context.Context) (jetstream.KeyValue, error) {
	c.mu.RLock()
	bucket := c.loopsBucket
	c.mu.RUnlock()

	if bucket != nil {
		return bucket, nil
	}

	// Upgrade to write lock and check again (double-checked locking)
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check again after acquiring write lock
	if c.loopsBucket != nil {
		return c.loopsBucket, nil
	}

	// Try to get the bucket (it may have been created since startup)
	js, err := c.natsClient.JetStream()
	if err != nil {
		return nil, fmt.Errorf("get jetstream: %w", err)
	}

	bucket, err = js.KeyValue(ctx, c.config.LoopsBucket)
	if err != nil {
		return nil, fmt.Errorf("bucket not found: %w", err)
	}

	// Cache it
	c.loopsBucket = bucket

	return bucket, nil
}
