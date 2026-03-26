// Package planmanager provides HTTP endpoints for workflow-related data.
// It exposes review synthesis results and other workflow data to the UI.
package planmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// Component implements the plan-api component.
// It provides HTTP endpoints for querying workflow data and questions.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	// KV bucket for workflow executions
	execBucket jetstream.KeyValue

	// coordinator is the embedded plan-coordinator pipeline.
	coordinator *coordinator

	// tripleWriter is used for workflow state operations (read/write graph triples).
	tripleWriter *graphutil.TripleWriter

	// Entity stores — component-owned caches following the execution-manager pattern.
	// All HTTP reads go through cache. Writes update cache + WriteTriple.
	plans        *planStore
	requirements *requirementStore
	scenarios    *scenarioStore

	// Question HTTP handler for Q&A endpoints
	questionHandler *workflow.QuestionHTTPHandler

	// workspace proxies read-only workspace requests to the sandbox server.
	workspace *workspaceProxy

	// rollupTaskIndex maps rollup taskID → plan slug for routing agent.complete
	// events back to the correct plan when rollup review completes.
	rollupTaskIndex sync.Map

	// Lifecycle state machine
	// States: 0=stopped, 1=starting, 2=running, 3=stopping
	state     atomic.Int32
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc
}

// loadPlanCached loads a plan from the cache, falling back to graph if not cached.
// On cache miss + graph hit, the plan is added to the cache.
func (c *Component) loadPlanCached(ctx context.Context, slug string) (*workflow.Plan, error) {
	c.mu.RLock()
	ps := c.plans
	tw := c.tripleWriter
	c.mu.RUnlock()

	if plan, ok := ps.get(slug); ok {
		return plan, nil
	}

	// Cache miss — fall back to graph (startup race or external mutation).
	plan, err := workflow.LoadPlan(ctx, tw, slug)
	if err != nil {
		return nil, err
	}
	ps.put(plan)
	return plan, nil
}

// savePlanCached saves a plan via triples and updates the cache.
func (c *Component) savePlanCached(ctx context.Context, plan *workflow.Plan) error {
	c.mu.RLock()
	ps := c.plans
	tw := c.tripleWriter
	c.mu.RUnlock()

	if err := workflow.SavePlan(ctx, tw, plan); err != nil {
		return err
	}
	ps.put(plan)
	return nil
}

// setPlanStatusCached transitions plan status and updates the cache.
func (c *Component) setPlanStatusCached(ctx context.Context, plan *workflow.Plan, target workflow.Status) error {
	c.mu.RLock()
	tw := c.tripleWriter
	ps := c.plans
	c.mu.RUnlock()

	if err := workflow.SetPlanStatus(ctx, tw, plan, target); err != nil {
		return err
	}
	ps.put(plan)
	return nil
}

// approvePlanCached approves a plan and updates the cache.
func (c *Component) approvePlanCached(ctx context.Context, plan *workflow.Plan) error {
	c.mu.RLock()
	tw := c.tripleWriter
	ps := c.plans
	c.mu.RUnlock()

	if err := workflow.ApprovePlan(ctx, tw, plan); err != nil {
		return err
	}
	ps.put(plan)
	return nil
}

const (
	stateStopped  = 0
	stateStarting = 1
	stateRunning  = 2
	stateStopping = 3
)

// NewComponent creates a new plan-api component.
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

	// Create coordinator with plan-api config fields.
	co := newCoordinator(config.coordinatorConfig(), deps.NATSClient, logger)

	return &Component{
		name:            "plan-manager",
		config:          config,
		natsClient:      deps.NATSClient,
		logger:          logger,
		coordinator:     co,
		questionHandler: questionHandler,
		workspace:       newWorkspaceProxy(config.SandboxURL),
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized plan-api",
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

	// Initialize TripleWriter for workflow state operations.
	tw := &graphutil.TripleWriter{
		NATSClient:    c.natsClient,
		Logger:        c.logger,
		ComponentName: "plan-manager",
	}

	// Initialize entity stores and reconcile from graph.
	ps := newPlanStore(tw, c.logger)
	rs := newRequirementStore(tw, c.logger)
	ss := newScenarioStore(tw, c.logger)
	ps.reconcile(ctx)
	rs.reconcile(ctx)
	ss.reconcile(ctx)

	// Create cancellation context
	childCtx, cancel := context.WithCancel(ctx)

	// Wire stores into coordinator so it can read/write plan state.
	c.coordinator.plans = ps
	c.coordinator.requirements = rs

	// Update state atomically with lock for complex state
	c.mu.Lock()
	c.execBucket = execBucket
	c.tripleWriter = tw
	c.plans = ps
	c.requirements = rs
	c.scenarios = ss
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

	// Start rollup review completion subscriber.
	// Consumes agent.complete.> to route rollup review results back to the plan.
	go c.handleRollupCompletions(childCtx, js)

	// Start the embedded coordinator (subscribes to NATS triggers + loop completions).
	if err := c.coordinator.Start(childCtx); err != nil {
		cancel()
		c.state.Store(stateStopped)
		return fmt.Errorf("start coordinator: %w", err)
	}

	// Transition to running
	c.state.Store(stateRunning)

	c.logger.Info("plan-api started",
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

	// Stop the embedded coordinator first (drains in-flight handlers).
	c.coordinator.Stop()

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

	c.logger.Info("plan-api stopped")

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "plan-manager",
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
