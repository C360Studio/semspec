// Package researchermanager owns the RESEARCH KV bucket and (in later
// R-phases) routes research requests from the developer agent to a
// researcher sub-agent.
//
// The researcher delegation pattern was motivated by take-23 (2026-05-14)
// where the implement-driver dev cycle wedged at iter=80 with 35 external
// source reads, 0 worktree writes, and ~3.2M cumulative tok_in. A research
// tool lets the developer offload upstream-API-surface investigation to a
// sub-agent whose context window is separate, returning a distilled
// summary (capped at workflow.MaxResearchAnswerBytes) that the developer
// can drop into its working context without flooding it with raw source.
//
// See project_research_tool_plan_2026_05_14 for the full design and R-phase
// plan.
//
// R1 (this file): skeleton + KV bucket creation. The component starts
// cleanly but does not yet dispatch researcher loops or react to
// ResearchRequest/Answer payloads. Those land in R2 (tool executors) and
// R3 (researcher dispatch + persona).
package researchermanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
)

const componentName = "researcher-manager"

// Component owns the RESEARCH KV bucket. Future phases will add the
// request/answer routing logic; R1 is intentionally a stub so the bucket
// exists and downstream components can wire against it.
type Component struct {
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	mu      sync.Mutex
	store   *workflow.ResearchStore
	cancel  context.CancelFunc
	running bool
}

// NewComponent constructs a researcher-manager from raw JSON config + the
// usual component dependencies.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var cfg Config
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &cfg); err != nil {
			return nil, fmt.Errorf("unmarshal researcher-manager config: %w", err)
		}
	}
	if cfg.Bucket == "" {
		cfg.Bucket = workflow.ResearchBucket
	}

	return &Component{
		config:     cfg,
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
	}, nil
}

// Initialize is a no-op for R1. Reserved for future setup that needs to
// happen before Start (e.g. payload-registry sanity checks).
func (c *Component) Initialize() error { return nil }

// Start creates the RESEARCH KV bucket so the research tool executor and
// downstream components can read/write research records. R2/R3 will extend
// Start to launch the request watcher + answer-routing goroutines.
func (c *Component) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return nil
	}
	if c.natsClient == nil {
		return fmt.Errorf("researcher-manager: NATS client is required")
	}

	store, err := workflow.NewResearchStore(c.natsClient)
	if err != nil {
		return fmt.Errorf("create research store: %w", err)
	}
	c.store = store

	// R1: bucket-only. R2/R3 will replace this with the watcher launch.
	watchCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	_ = watchCtx // reserved for the R2 watcher goroutine

	c.running = true
	c.logger.Info("researcher-manager started (R1 skeleton — dispatch wiring pending R2/R3)",
		slog.String("bucket", c.config.Bucket))
	return nil
}

// Stop gracefully halts the component. R1 has no goroutines to drain;
// cancelling the watchCtx is a no-op now but the wiring is kept so R2 can
// add its watcher without touching Stop.
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	c.running = false
	return nil
}

// Meta returns the component's discovery metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        componentName,
		Type:        "processor",
		Description: "Owns RESEARCH KV and routes research requests from the developer to a researcher sub-agent (R-phase skeleton; dispatch wiring pending R2/R3)",
		Version:     "0.1.0",
	}
}

// InputPorts/OutputPorts are empty in R1. R2 will add the
// agent.research.requested input port and the agent.research.answered output
// port when the routing logic ships.
func (c *Component) InputPorts() []component.Port  { return nil }
func (c *Component) OutputPorts() []component.Port { return nil }

// ConfigSchema returns the component's JSON-Schema description. Empty for
// R1 since the only field (Bucket) is optional with a sensible default.
func (c *Component) ConfigSchema() component.ConfigSchema { return component.ConfigSchema{} }

// Health reports whether the component has started.
func (c *Component) Health() component.HealthStatus {
	c.mu.Lock()
	running := c.running
	c.mu.Unlock()
	status := "stopped"
	if running {
		status = "healthy"
	}
	return component.HealthStatus{
		Healthy:   running,
		Status:    status,
		LastCheck: time.Now().UTC(),
	}
}

// DataFlow is empty for R1 — no metrics tracked yet. R2 will populate this
// with request/answer counters and routing latency.
func (c *Component) DataFlow() component.FlowMetrics { return component.FlowMetrics{} }
