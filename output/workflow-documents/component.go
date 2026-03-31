// Package workflowdocuments provides an output component that watches
// PLAN_STATES KV and writes plan artifacts (.semspec/plans/{slug}/plan.md
// and plan.json) at key milestones for human review and git audit trails.
package workflowdocuments

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// Component implements the workflow-documents output processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	baseDir         string
	planStateBucket string

	// Lifecycle
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics
	documentsWritten atomic.Int64
	writeErrors      atomic.Int64
	lastActivityMu   sync.RWMutex
	lastActivity     time.Time
}

// NewComponent creates a new workflow-documents output component.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if config.PlanStateBucket == "" {
		config.PlanStateBucket = DefaultConfig().PlanStateBucket
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Resolve base directory.
	baseDir := config.BaseDir
	if baseDir == "" {
		baseDir = os.Getenv("SEMSPEC_REPO_PATH")
	}
	if baseDir == "" {
		var err error
		baseDir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get working directory: %w", err)
		}
	}

	return &Component{
		name:            "workflow-documents",
		config:          config,
		natsClient:      deps.NATSClient,
		logger:          deps.GetLogger(),
		baseDir:         baseDir,
		planStateBucket: config.PlanStateBucket,
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	semspecDir := filepath.Join(c.baseDir, ".semspec", "plans")
	if err := os.MkdirAll(semspecDir, 0755); err != nil {
		return fmt.Errorf("create semspec directory: %w", err)
	}
	c.logger.Debug("Initialized workflow-documents component",
		"base_dir", c.baseDir,
		"semspec_dir", semspecDir)
	return nil
}

// Start begins watching PLAN_STATES KV for milestone transitions and writing plan artifacts.
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

	watchCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.mu.Unlock()

	js, err := c.natsClient.JetStream()
	if err != nil {
		c.mu.Lock()
		c.running = false
		c.cancel = nil
		c.mu.Unlock()
		cancel()
		return fmt.Errorf("get JetStream: %w", err)
	}

	go c.watchPlanStates(watchCtx, js)

	c.logger.Info("workflow-documents started",
		"base_dir", c.baseDir,
		"plan_state_bucket", c.planStateBucket)

	return nil
}

// watchPlanStates watches the PLAN_STATES KV bucket for milestone transitions
// and writes plan.md + plan.json on each milestone.
func (c *Component) watchPlanStates(ctx context.Context, js jetstream.JetStream) {
	bucket, err := workflow.WaitForKVBucket(ctx, js, c.planStateBucket)
	if err != nil {
		c.logger.Warn("PLAN_STATES not available — document generation disabled",
			"bucket", c.planStateBucket, "error", err)
		return
	}

	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		c.logger.Warn("Failed to watch PLAN_STATES", "error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Watching PLAN_STATES for plan document generation",
		"bucket", c.planStateBucket)

	for entry := range watcher.Updates() {
		if entry == nil {
			continue
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}

		var plan workflow.Plan
		if err := json.Unmarshal(entry.Value(), &plan); err != nil {
			c.logger.Debug("Skipping unparseable PLAN_STATES entry",
				"key", entry.Key(), "error", err)
			continue
		}

		if !isMilestoneStatus(plan.EffectiveStatus()) {
			continue
		}

		c.writePlanDocuments(ctx, &plan)
	}
}

// writePlanDocuments writes plan.md and plan.json to .semspec/plans/{slug}/.
func (c *Component) writePlanDocuments(ctx context.Context, plan *workflow.Plan) {
	select {
	case <-ctx.Done():
		return
	default:
	}

	if err := workflow.ValidateSlug(plan.Slug); err != nil {
		c.logger.Warn("Skipping plan with invalid slug",
			"slug", plan.Slug, "error", err)
		return
	}

	planDir := filepath.Join(c.baseDir, ".semspec", "plans", plan.Slug)
	if err := os.MkdirAll(planDir, 0755); err != nil {
		c.logger.Error("Failed to create plan directory",
			"slug", plan.Slug, "error", err)
		c.writeErrors.Add(1)
		return
	}

	// Write plan.md
	markdown := RenderPlan(plan)
	mdPath := filepath.Join(planDir, "plan.md")
	if err := os.WriteFile(mdPath, []byte(markdown), 0644); err != nil {
		c.logger.Error("Failed to write plan.md",
			"slug", plan.Slug, "error", err)
		c.writeErrors.Add(1)
		return
	}

	// Write plan.json (pretty-printed)
	prettyJSON, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		c.logger.Warn("Failed to marshal plan JSON",
			"slug", plan.Slug, "error", err)
	} else {
		jsonPath := filepath.Join(planDir, "plan.json")
		if err := os.WriteFile(jsonPath, prettyJSON, 0644); err != nil {
			c.logger.Warn("Failed to write plan.json",
				"slug", plan.Slug, "error", err)
		}
	}

	c.documentsWritten.Add(1)
	c.updateLastActivity()

	c.logger.Info("Wrote plan documents",
		"slug", plan.Slug,
		"status", plan.EffectiveStatus(),
		"path", mdPath)
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
	c.logger.Info("workflow-documents stopped",
		"documents_written", c.documentsWritten.Load(),
		"write_errors", c.writeErrors.Load())

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "workflow-documents",
		Type:        "output",
		Description: "Watches PLAN_STATES and writes plan.md + plan.json artifacts",
		Version:     "2.0.0",
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
	return workflowDocumentsSchema
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
		ErrorCount: int(c.writeErrors.Load()),
		Uptime:     time.Since(startTime),
		Status:     status,
	}
}

// DataFlow returns current data flow metrics.
func (c *Component) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{
		LastActivity: c.getLastActivity(),
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
