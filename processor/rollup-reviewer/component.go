// Package rollupreviewer provides a processor that watches PLAN_STATES for plans
// reaching reviewing_rollup status and transitions them to complete.
//
// Phase 1 always auto-approves (LLM review and git merge are TODO for Phase 2+).
// Plan-manager is the single writer — this component only publishes mutations.
package rollupreviewer

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
	// mutationRollupComplete is the subject for rollup completion mutation requests.
	mutationRollupComplete = "plan.mutation.rollup.complete"
)

// rollupMutationRequest is the payload sent to plan-manager on rollup completion.
type rollupMutationRequest struct {
	Slug    string `json:"slug"`
	Verdict string `json:"verdict"` // "approved" or "needs_attention"
	Summary string `json:"summary"`
}

// Component implements the rollup-reviewer processor.
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
	triggersProcessed atomic.Int64
	reviewsFailed     atomic.Int64
	lastActivityMu    sync.RWMutex
	lastActivity      time.Time
}

// ---------------------------------------------------------------------------
// Factory
// ---------------------------------------------------------------------------

// NewComponent creates a new rollup-reviewer processor.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Apply defaults for any zero-value fields.
	defaults := DefaultConfig()
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
		name:       "rollup-reviewer",
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
	c.logger.Debug("Initialized rollup-reviewer",
		"plan_state_bucket", c.config.PlanStateBucket,
		"skip_review", c.config.SkipReview)
	return nil
}

// Start begins processing rollup review triggers.
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

	// Watch PLAN_STATES for plans reaching "reviewing_rollup" status.
	// The KV write IS the trigger — no separate coordinator needed.
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("cannot get JetStream: %w", err)
	}

	go c.watchPlanStates(subCtx, js)

	c.logger.Info("rollup-reviewer started",
		"plan_state_bucket", c.config.PlanStateBucket)

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

	c.logger.Info("rollup-reviewer stopped",
		"triggers_processed", c.triggersProcessed.Load(),
		"reviews_failed", c.reviewsFailed.Load())

	return nil
}

// ---------------------------------------------------------------------------
// KV watcher
// ---------------------------------------------------------------------------

// watchPlanStates watches PLAN_STATES for plans reaching reviewing_rollup.
// The KV value carries plan inline — no follow-up query needed.
func (c *Component) watchPlanStates(ctx context.Context, js jetstream.JetStream) {
	bucket, err := workflow.WaitForKVBucket(ctx, js, c.config.PlanStateBucket)
	if err != nil {
		c.logger.Warn("PLAN_STATES not available — rollup-reviewer watcher disabled",
			"bucket", c.config.PlanStateBucket, "error", err)
		return
	}

	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		c.logger.Warn("Failed to watch PLAN_STATES", "error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Watching PLAN_STATES for reviewing_rollup")

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
		if plan.Status != workflow.StatusReviewingRollup {
			continue
		}

		// reviewing_rollup is already the definitive state set by plan-manager's
		// convergence handler. No claim step needed — just process the rollup.
		c.triggersProcessed.Add(1)
		c.updateLastActivity()

		go c.processRollup(ctx, &plan)
	}
}

// processRollup handles the rollup review for a single plan.
// Phase 1: always auto-approves regardless of config.SkipReview.
func (c *Component) processRollup(ctx context.Context, plan *workflow.Plan) {
	c.logger.Info("Auto-approving rollup (Phase 1 — LLM review not yet implemented)",
		"slug", plan.Slug)

	c.publishRollupComplete(ctx, plan.Slug, "approved",
		"Auto-approved (rollup review not yet implemented)")
}

// publishRollupComplete sends plan.mutation.rollup.complete to plan-manager.
func (c *Component) publishRollupComplete(ctx context.Context, slug, verdict, summary string) {
	mutReq := rollupMutationRequest{
		Slug:    slug,
		Verdict: verdict,
		Summary: summary,
	}

	data, err := json.Marshal(mutReq)
	if err != nil {
		c.reviewsFailed.Add(1)
		c.logger.Error("Failed to marshal rollup mutation",
			"slug", slug, "error", err)
		return
	}

	resp, err := c.natsClient.RequestWithRetry(ctx, mutationRollupComplete, data,
		10*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		c.reviewsFailed.Add(1)
		c.logger.Error("Failed to send rollup mutation",
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
		c.reviewsFailed.Add(1)
		c.logger.Error("Rollup mutation rejected by plan-manager",
			"slug", slug, "error", errMsg)
		return
	}

	c.logger.Info("Rollup mutation accepted by plan-manager",
		"slug", slug, "verdict", verdict)
}

// ---------------------------------------------------------------------------
// Component.Discoverable implementation
// ---------------------------------------------------------------------------

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "rollup-reviewer",
		Type:        "processor",
		Description: "Reviews completed plan rollups; Phase 1 auto-approves to unblock the pipeline",
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
	return rollupReviewerSchema
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
		ErrorCount: int(c.reviewsFailed.Load()),
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
