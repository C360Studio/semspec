// Package trajectoryapi provides HTTP endpoints for querying agent loop trajectories.
// It aggregates data from step entities in the knowledge graph (written by semstreams
// alpha.53 on loop completion) and the AGENT_LOOPS KV bucket to provide unified
// trajectory views for debugging and observability.
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

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// Component implements the trajectory-api component.
// It provides HTTP endpoints for querying agent loop trajectories.
// Step data is sourced from the knowledge graph (semstreams alpha.53 step entities).
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	// KV buckets
	loopsBucket jetstream.KeyValue

	// NATS ObjectStore for step content (tool results, model responses)
	contentStore jetstream.ObjectStore

	// Graph querier for trajectory step entities
	stepQuerier *StepQuerier

	// KV store for workflow state operations
	kvStore *natsclient.KVStore

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
	if config.LoopsBucket == "" {
		config.LoopsBucket = defaults.LoopsBucket
	}
	if config.ContentBucket == "" {
		config.ContentBucket = defaults.ContentBucket
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
		"loops_bucket", c.config.LoopsBucket,
		"content_bucket", c.config.ContentBucket,
		"graph_gateway_url", c.config.GraphGatewayURL)
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

	// Get AGENT_LOOPS KV bucket — may not exist yet, retry on queries.
	loopsBucket, err := js.KeyValue(ctx, c.config.LoopsBucket)
	if err != nil {
		c.logger.Warn("Loops bucket not found, will retry on queries",
			"bucket", c.config.LoopsBucket,
			"error", err)
	}

	// Get AGENT_CONTENT ObjectStore — optional, used for detailed content fetches.
	var contentStore jetstream.ObjectStore
	if c.config.ContentBucket != "" {
		contentStore, err = js.ObjectStore(ctx, c.config.ContentBucket)
		if err != nil {
			c.logger.Warn("Content object store not found, detailed content unavailable",
				"bucket", c.config.ContentBucket,
				"error", err)
		}
	}

	// Initialize workflow manager for plan access.
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
	// Get ENTITY_STATES KV store for workflow state operations.
	var kvStore *natsclient.KVStore
	if entityBucket, kvErr := c.natsClient.GetKeyValueBucket(ctx, "ENTITY_STATES"); kvErr != nil {
		c.logger.Warn("ENTITY_STATES bucket not available — workflow state operations will use disk fallback",
			"error", kvErr)
	} else {
		kvStore = c.natsClient.NewKVStore(entityBucket)
	}

	// Initialize graph querier for step entity queries.
	var stepQuerier *StepQuerier
	if c.config.GraphGatewayURL != "" {
		stepQuerier = NewStepQuerier(c.config.GraphGatewayURL)
		c.logger.Debug("Initialized step graph querier", "url", c.config.GraphGatewayURL)
	}

	// Create cancellation context.
	_, cancel := context.WithCancel(ctx)

	// Update state atomically with lock for complex state.
	c.mu.Lock()
	c.loopsBucket = loopsBucket
	c.contentStore = contentStore
	c.stepQuerier = stepQuerier
	c.kvStore = kvStore
	c.cancel = cancel
	c.startTime = time.Now()
	c.mu.Unlock()

	// Transition to running.
	c.state.Store(stateRunning)

	c.logger.Info("trajectory-api started",
		"loops_bucket", c.config.LoopsBucket,
		"content_bucket", c.config.ContentBucket,
		"org", c.config.Org,
		"platform", c.config.Platform)

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
		Description: "HTTP endpoints for querying agent loop trajectories from graph step entities",
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

// getLoopsBucket gets the loops bucket, attempting to reconnect if needed.
func (c *Component) getLoopsBucket(ctx context.Context) (jetstream.KeyValue, error) {
	c.mu.RLock()
	bucket := c.loopsBucket
	c.mu.RUnlock()

	if bucket != nil {
		return bucket, nil
	}

	// Upgrade to write lock and check again (double-checked locking).
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.loopsBucket != nil {
		return c.loopsBucket, nil
	}

	js, err := c.natsClient.JetStream()
	if err != nil {
		return nil, fmt.Errorf("get jetstream: %w", err)
	}

	bucket, err = js.KeyValue(ctx, c.config.LoopsBucket)
	if err != nil {
		return nil, fmt.Errorf("bucket not found: %w", err)
	}

	c.loopsBucket = bucket
	return bucket, nil
}

// getContentStore gets the ObjectStore for step content, attempting to reconnect if needed.
func (c *Component) getContentStore(ctx context.Context) (jetstream.ObjectStore, error) {
	c.mu.RLock()
	store := c.contentStore
	c.mu.RUnlock()

	if store != nil {
		return store, nil
	}

	if c.config.ContentBucket == "" {
		return nil, fmt.Errorf("content bucket not configured")
	}

	// Upgrade to write lock and check again.
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.contentStore != nil {
		return c.contentStore, nil
	}

	js, err := c.natsClient.JetStream()
	if err != nil {
		return nil, fmt.Errorf("get jetstream: %w", err)
	}

	store, err = js.ObjectStore(ctx, c.config.ContentBucket)
	if err != nil {
		return nil, fmt.Errorf("content store not found: %w", err)
	}

	c.contentStore = store
	return store, nil
}

// loopEntityID constructs the full graph entity ID for a loop given the short loop ID.
// Returns an empty string if org or platform are not configured (graceful degradation).
func (c *Component) loopEntityID(loopID string) string {
	if c.config.Org == "" || c.config.Platform == "" {
		// When org/platform are not configured, the caller receives an empty entity ID
		// and will fall back to returning an empty step list gracefully.
		return ""
	}
	return fmt.Sprintf("%s.%s.agent.agentic-loop.execution.%s",
		c.config.Org, c.config.Platform, loopID)
}
