// Package architecturegenerator provides a processor that either generates an
// architecture document for a plan via agentic-dispatch, or immediately
// passes through to architecture_generated when plan.SkipArchitecture is true.
//
// The component watches PLAN_STATES for plans reaching requirements_generated,
// claims the plan by transitioning to generating_architecture, and then either:
//   - (SkipArchitecture == true) publishes plan.mutation.architecture.generated immediately
//   - (SkipArchitecture == false) TODO: dispatches architect agent via agentic-dispatch
//
// Plan-manager is the single writer — this component only publishes mutations.
package architecturegenerator

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

const (
	// mutationArchitectureGenerated is the subject for architecture mutation requests.
	mutationArchitectureGenerated = "plan.mutation.architecture.generated"
)

// Component implements the architecture-generator processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	// Lifecycle
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics
	triggersProcessed  atomic.Int64
	generationsSkipped atomic.Int64
	generationsFailed  atomic.Int64
	lastActivityMu     sync.RWMutex
	lastActivity       time.Time
}

// ---------------------------------------------------------------------------
// Factory
// ---------------------------------------------------------------------------

// NewComponent creates a new architecture-generator processor.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Apply defaults for any zero-value fields.
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
	if config.DefaultCapability == "" {
		config.DefaultCapability = defaults.DefaultCapability
	}
	if config.PlanStateBucket == "" {
		config.PlanStateBucket = defaults.PlanStateBucket
	}
	if config.Ports == nil {
		config.Ports = defaults.Ports
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Component{
		name:       "architecture-generator",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
	}, nil
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized architecture-generator",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"trigger_subject", c.config.TriggerSubject)
	return nil
}

// Start begins processing architecture generation triggers.
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

	// Watch PLAN_STATES for plans reaching "requirements_generated" status.
	// This is the KV twofer — the write IS the trigger.
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("cannot get JetStream: %w", err)
	}

	go c.watchPlanStates(subCtx, js)

	c.logger.Info("architecture-generator started",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName)

	return nil
}

func (c *Component) rollbackStart(cancel context.CancelFunc) {
	c.mu.Lock()
	c.running = false
	c.cancel = nil
	c.mu.Unlock()
	cancel()
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

	c.logger.Info("architecture-generator stopped",
		"triggers_processed", c.triggersProcessed.Load(),
		"generations_skipped", c.generationsSkipped.Load(),
		"generations_failed", c.generationsFailed.Load())

	return nil
}

// ---------------------------------------------------------------------------
// KV watcher
// ---------------------------------------------------------------------------

// watchPlanStates watches PLAN_STATES for plans reaching requirements_generated.
// The KV value carries plan inline — no follow-up query needed.
func (c *Component) watchPlanStates(ctx context.Context, js jetstream.JetStream) {
	bucket, err := workflow.WaitForKVBucket(ctx, js, c.config.PlanStateBucket)
	if err != nil {
		c.logger.Warn("PLAN_STATES not available — architecture-generator watcher disabled",
			"bucket", c.config.PlanStateBucket, "error", err)
		return
	}

	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		c.logger.Warn("Failed to watch PLAN_STATES", "error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Watching PLAN_STATES for requirements_generated")

	for entry := range watcher.Updates() {
		if entry == nil {
			continue
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}

		var plan workflow.Plan
		if json.Unmarshal(entry.Value(), &plan) != nil {
			continue
		}
		if plan.Status != workflow.StatusRequirementsGenerated {
			continue
		}

		// Claim the plan to prevent re-trigger on watcher restarts.
		if !workflow.ClaimPlanStatus(ctx, c.natsClient, plan.Slug, workflow.StatusGeneratingArchitecture, c.logger) {
			continue
		}

		c.triggersProcessed.Add(1)
		c.updateLastActivity()

		go c.processArchitecturePhase(ctx, &plan)
	}
}

// processArchitecturePhase handles the architecture phase for a single plan.
// For plans with SkipArchitecture=true, it publishes the mutation immediately.
// For all others, it dispatches the architect agent (TODO: full agentic dispatch).
func (c *Component) processArchitecturePhase(ctx context.Context, plan *workflow.Plan) {
	if plan.SkipArchitecture {
		c.logger.Info("Skipping architecture phase (simple plan)",
			"slug", plan.Slug)
		c.generationsSkipped.Add(1)
		c.publishArchitectureGenerated(ctx, plan.Slug, nil)
		return
	}

	// TODO: dispatch architect agent via agentic-dispatch for full LLM run.
	// For now, pass through with a nil architecture document so the pipeline
	// continues. The full agent dispatch (watching AGENT_LOOPS, parsing result,
	// sending mutation) follows the same pattern as scenario-generator's
	// dispatchScenarioGenerator + watchLoopCompletions.
	c.logger.Info("Architecture phase: full LLM dispatch not yet implemented, passing through",
		"slug", plan.Slug)
	c.publishArchitectureGenerated(ctx, plan.Slug, nil)
}

// publishArchitectureGenerated sends plan.mutation.architecture.generated to plan-manager.
// architecture is nil for the skip path or when the stub pass-through is used.
func (c *Component) publishArchitectureGenerated(ctx context.Context, slug string, architecture interface{}) {
	mutReq := struct {
		Slug         string      `json:"slug"`
		Architecture interface{} `json:"architecture,omitempty"`
	}{
		Slug:         slug,
		Architecture: architecture,
	}

	data, err := json.Marshal(mutReq)
	if err != nil {
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to marshal architecture mutation",
			"slug", slug, "error", err)
		return
	}

	resp, err := c.natsClient.RequestWithRetry(ctx, mutationArchitectureGenerated, data,
		10*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to send architecture mutation",
			"slug", slug, "error", err)
		return
	}

	var mutResp struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(resp, &mutResp); err != nil || !mutResp.Success {
		errMsg := "unknown error"
		if err != nil {
			errMsg = err.Error()
		} else if mutResp.Error != "" {
			errMsg = mutResp.Error
		}
		c.generationsFailed.Add(1)
		c.logger.Error("Architecture mutation rejected by plan-manager",
			"slug", slug, "error", errMsg)
		return
	}

	c.logger.Info("Architecture phase mutation accepted by plan-manager", "slug", slug)
}

// ---------------------------------------------------------------------------
// Component.Discoverable implementation
// ---------------------------------------------------------------------------

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "architecture-generator",
		Type:        "processor",
		Description: "Generates architecture documents or passes through for simple plans",
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
	return architectureGeneratorSchema
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
		ErrorCount: int(c.generationsFailed.Load()),
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
