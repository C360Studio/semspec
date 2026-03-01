// Package workflowapi provides HTTP endpoints for workflow-related data.
// It exposes review synthesis results and other workflow data to the UI.
package workflowapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// Component implements the workflow-api component.
// It provides HTTP endpoints for querying workflow data and questions.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	// KV bucket for workflow executions
	execBucket jetstream.KeyValue

	// Question HTTP handler for Q&A endpoints
	questionHandler *workflow.QuestionHTTPHandler

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

// NewComponent creates a new workflow-api component.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Apply defaults
	defaults := DefaultConfig()
	if config.ExecutionBucketName == "" {
		config.ExecutionBucketName = defaults.ExecutionBucketName
	}
	if config.EventStreamName == "" {
		config.EventStreamName = defaults.EventStreamName
	}
	if config.UserStreamName == "" {
		config.UserStreamName = defaults.UserStreamName
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	logger := deps.GetLogger()

	// Create question HTTP handler for Q&A endpoints
	// Must be done here (not in Start) so it's available when RegisterHTTPHandlers is called
	questionHandler, err := workflow.NewQuestionHTTPHandler(deps.NATSClient, logger)
	if err != nil {
		logger.Warn("Failed to create question handler, Q&A endpoints will be unavailable",
			"error", err)
	}

	return &Component{
		name:            "workflow-api",
		config:          config,
		natsClient:      deps.NATSClient,
		logger:          logger,
		questionHandler: questionHandler,
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized workflow-api",
		"exec_bucket", c.config.ExecutionBucketName)
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

	// Get KV bucket for workflow executions
	execBucket, err := js.KeyValue(ctx, c.config.ExecutionBucketName)
	if err != nil {
		// Don't fail startup - bucket might be created later by workflow processor
		c.logger.Warn("Workflow executions bucket not found, will retry on queries",
			"bucket", c.config.ExecutionBucketName,
			"error", err)
	}

	// Create cancellation context
	childCtx, cancel := context.WithCancel(ctx)

	// Update state atomically with lock for complex state
	c.mu.Lock()
	c.execBucket = execBucket
	c.cancel = cancel
	c.startTime = time.Now()
	c.mu.Unlock()

	// Start workflow events subscriber for plan auto-approval (ADR-005).
	// Handles plan_approved events from the plan-review-loop workflow.
	go c.handleWorkflowEvents(childCtx, js)

	// Start user signal subscriber for escalation and error handling.
	// Consumes user.signal.> from the USER stream to handle max-retry
	// escalations and workflow step failures.
	go c.handleUserSignals(childCtx, js)

	// Start question graph publisher (watches QUESTIONS KV bucket).
	// Publishes question entities to the graph on creation, answer, timeout.
	go c.handleQuestionUpdates(childCtx, js)

	// Transition to running
	c.state.Store(stateRunning)

	c.logger.Info("workflow-api started",
		"exec_bucket", c.config.ExecutionBucketName)

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

	c.logger.Info("workflow-api stopped")

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "workflow-api",
		Type:        "processor",
		Description: "HTTP endpoints for workflow data including review synthesis results",
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
	return workflowAPISchema
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

// getExecBucket gets the execution bucket, attempting to reconnect if needed.
// Uses double-checked locking to prevent race conditions.
func (c *Component) getExecBucket(ctx context.Context) (jetstream.KeyValue, error) {
	c.mu.RLock()
	bucket := c.execBucket
	c.mu.RUnlock()

	if bucket != nil {
		return bucket, nil
	}

	// Upgrade to write lock and check again (double-checked locking)
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check again after acquiring write lock
	if c.execBucket != nil {
		return c.execBucket, nil
	}

	// Try to get the bucket (it may have been created since startup)
	js, err := c.natsClient.JetStream()
	if err != nil {
		return nil, fmt.Errorf("get jetstream: %w", err)
	}

	bucket, err = js.KeyValue(ctx, c.config.ExecutionBucketName)
	if err != nil {
		return nil, fmt.Errorf("bucket not found: %w", err)
	}

	// Cache it
	c.execBucket = bucket

	return bucket, nil
}
