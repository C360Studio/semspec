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

	// tripleWriter is used for workflow state operations (read/write graph triples).
	tripleWriter *graphutil.TripleWriter

	// plans is the component-owned cache following the execution-manager pattern.
	// Requirements and Scenarios are carried inline on Plan — no sibling stores.
	// All HTTP reads go through cache. Writes update cache + WriteTriple + KV.
	plans *planStore

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

// loadPlanCached loads a plan from the store (cache → KV → graph fallback).
func (c *Component) loadPlanCached(ctx context.Context, slug string) (*workflow.Plan, error) {
	c.mu.RLock()
	ps := c.plans
	tw := c.tripleWriter
	c.mu.RUnlock()

	if plan, ok := ps.get(slug); ok {
		return plan, nil
	}

	// Cache + KV miss — fall back to graph (startup race or external mutation).
	if tw == nil {
		return nil, fmt.Errorf("%w: %s", workflow.ErrPlanNotFound, slug)
	}
	prefix := workflow.EntityPrefix() + ".wf.plan.plan."
	entities, err := tw.ReadEntitiesByPrefix(ctx, prefix, 500)
	if err != nil {
		return nil, fmt.Errorf("graph fallback failed: %w", err)
	}
	for entityID, triples := range entities {
		plan := workflow.PlanFromTripleMap(entityID, triples)
		if plan.Slug != slug {
			continue
		}
		// Backfill all three layers. Graph write is idempotent since we just read from it.
		if saveErr := ps.save(ctx, plan); saveErr != nil {
			c.logger.Warn("Failed to backfill plan from graph", "slug", slug, "error", saveErr)
		}
		return plan, nil
	}
	return nil, fmt.Errorf("%w: %s", workflow.ErrPlanNotFound, slug)
}

// savePlanCached saves a plan through all three layers (cache → KV → graph).
func (c *Component) savePlanCached(ctx context.Context, plan *workflow.Plan) error {
	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	return ps.save(ctx, plan)
}

// setPlanStatusCached transitions plan status and persists through all three layers.
// The caller's plan pointer is updated in-place so it remains consistent after the call.
func (c *Component) setPlanStatusCached(ctx context.Context, plan *workflow.Plan, target workflow.Status) error {
	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(target) {
		return fmt.Errorf("%w: %s → %s", workflow.ErrInvalidTransition, current, target)
	}

	plan.Status = target

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	return ps.save(ctx, plan)
}

// approvePlanCached approves a plan and persists through all three layers.
// ps.approve mutates the plan pointer in-place before saving, so the caller's
// pointer is updated as a side effect.
func (c *Component) approvePlanCached(ctx context.Context, plan *workflow.Plan) error {
	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	// ps.approve mutates plan directly (sets Approved, ApprovedAt, Status) then saves.
	return ps.approve(ctx, plan)
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
	if config.PlanStateBucket == "" {
		config.PlanStateBucket = defaults.PlanStateBucket
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

	return &Component{
		name:       "plan-manager",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     logger,
		workspace:  newWorkspaceProxy(config.SandboxURL),
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

	execBucket, planBucket, tw := c.acquireInfrastructure(ctx, js)

	// Create cancellation context before initializing stores (needed for TTL cache background goroutine).
	childCtx, cancel := context.WithCancel(ctx)

	// Initialize plan store and reconcile from startup sources.
	// Requirements and Scenarios are carried inline on the Plan struct.
	ps, err := newPlanStore(childCtx, planBucket, tw, c.logger)
	if err != nil {
		cancel()
		return fmt.Errorf("create plan store: %w", err)
	}
	ps.reconcile(ctx)

	// Update state atomically with lock for complex state
	c.mu.Lock()
	c.execBucket = execBucket
	c.tripleWriter = tw
	c.plans = ps
	c.cancel = cancel
	c.startTime = time.Now()
	c.mu.Unlock()

	// Start question graph publisher (watches QUESTIONS KV bucket).
	go c.handleQuestionUpdates(childCtx, js)

	// Start mutation request handlers (plan.mutation.* — generators return results here).
	if err := c.startMutationHandler(childCtx); err != nil {
		cancel()
		c.state.Store(stateStopped)
		return fmt.Errorf("start mutation handler: %w", err)
	}

	// Watch EXECUTION_STATES for requirement completions → plan completion transition.
	go c.watchExecutionCompletions(childCtx)

	// Transition to running
	c.state.Store(stateRunning)

	c.logger.Info("plan-manager started",
		"exec_bucket", c.config.ExecutionBucketName)

	return nil
}

// Stop gracefully stops the component.
// acquireInfrastructure sets up KV buckets and TripleWriter for Start().
// Failures are non-fatal — the component degrades gracefully.
func (c *Component) acquireInfrastructure(ctx context.Context, js jetstream.JetStream) (jetstream.KeyValue, jetstream.KeyValue, *graphutil.TripleWriter) {
	// Workflow executions bucket (read-only, may not exist yet).
	execBucket, err := js.KeyValue(ctx, c.config.ExecutionBucketName)
	if err != nil {
		c.logger.Warn("Workflow executions bucket not found, will retry on queries",
			"bucket", c.config.ExecutionBucketName, "error", err)
	}

	// PLAN_STATES KV bucket (observable, the KV twofer).
	planBucket, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:  c.config.PlanStateBucket,
		History: 1,
	})
	if err != nil {
		c.logger.Warn("PLAN_STATES bucket unavailable, KV layer disabled",
			"bucket", c.config.PlanStateBucket, "error", err)
	}

	tw := &graphutil.TripleWriter{
		NATSClient:    c.natsClient,
		Logger:        c.logger,
		ComponentName: "plan-manager",
	}

	return execBucket, planBucket, tw
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

// shouldGateReview returns true if the plan should hold at awaiting_review
// instead of transitioning directly to complete. GitHub-originated plans
// always gate; otherwise controlled by auto_approve_review config.
func (c *Component) shouldGateReview(plan *workflow.Plan) bool {
	if plan.GitHub != nil {
		return true
	}
	return !c.config.IsAutoApproveReview()
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
