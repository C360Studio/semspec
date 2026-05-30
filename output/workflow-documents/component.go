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

	// ADR-040 Move 3: per-slug debouncer that coalesces EXECUTION_STATES
	// updates into one tasks.md re-render per second.
	openSpecDebouncer *openSpecDebouncer

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
		name:              "workflow-documents",
		config:            config,
		natsClient:        deps.NATSClient,
		logger:            deps.GetLogger(),
		baseDir:           baseDir,
		planStateBucket:   config.PlanStateBucket,
		openSpecDebouncer: newOpenSpecDebouncer(),
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
	// ADR-040 Move 3: live tasks.md checkbox flipping. Best-effort —
	// failure to start the watcher just means tasks.md doesn't update
	// between PLAN_STATES milestones; static snapshots still land.
	go c.watchExecutionStates(watchCtx, js)

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

// phaseArtifact pairs an output filename with the renderer that produces
// its content. The renderer returns "" when it has nothing to emit at the
// current plan state, which writePlanDocuments treats as "skip this file."
// Adding a new BMAD/OpenSpec-style artifact = appending one entry here.
type phaseArtifact struct {
	filename string
	render   func(*workflow.Plan) string
}

// phaseArtifacts is the full ordered list of markdown deliverables
// workflow-documents writes at each milestone transition. plan.md remains
// the comprehensive view; the rest are per-phase artifacts that mirror
// BMAD/OpenSpec's drill-down structure (architecture / requirements /
// scenarios / qa-summary / run-summary).
//
// Each render function decides whether to emit content based on the
// plan's current state — e.g. RenderArchitecture returns "" when
// plan.Architecture is nil, so architecture.md isn't created on
// pre-architecture-phase plans.
var phaseArtifacts = []phaseArtifact{
	{"plan.md", RenderPlan},
	{"architecture.md", RenderArchitecture},
	{"requirements.md", RenderRequirements},
	{"scenarios.md", RenderScenarios},
	{"qa-summary.md", RenderQASummary},
	{"run-summary.md", RenderRunSummary},
}

// writePlanDocuments writes plan.md, plan.json, and the per-phase
// markdown artifacts (architecture.md, requirements.md, scenarios.md,
// qa-summary.md, run-summary.md) to .semspec/plans/{slug}/. Each
// per-phase file is only written when its renderer returns non-empty
// content, so e.g. scenarios.md doesn't appear until the plan has
// scenarios attached.
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

	// Write phase artifacts. plan.md is non-skippable (always renders);
	// the rest skip themselves when the plan has nothing to show for
	// that phase.
	var writtenFiles []string
	for _, a := range phaseArtifacts {
		content := a.render(plan)
		if content == "" {
			continue
		}
		path := filepath.Join(planDir, a.filename)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			c.logger.Error("Failed to write phase artifact",
				"slug", plan.Slug, "file", a.filename, "error", err)
			c.writeErrors.Add(1)
			// Continue with the other artifacts — one failed write
			// shouldn't abort the rest.
			continue
		}
		writtenFiles = append(writtenFiles, a.filename)
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
		} else {
			writtenFiles = append(writtenFiles, "plan.json")
		}
	}

	// ADR-040 Move 3: emit OpenSpec-shaped artifacts alongside the BMAD
	// ones. Skipped for legacy plans (Plan.Exploration nil) so old plans
	// don't get half-populated openspec/ directories.
	openSpecFiles := c.writeOpenSpecArtifacts(plan, planDir)
	writtenFiles = append(writtenFiles, openSpecFiles...)

	c.documentsWritten.Add(1)
	c.updateLastActivity()

	c.logger.Info("Wrote plan documents",
		"slug", plan.Slug,
		"status", plan.EffectiveStatus(),
		"path", planDir,
		"files", writtenFiles)
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
