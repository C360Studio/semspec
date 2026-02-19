// Package projectapi provides HTTP endpoints for project initialization.
// It exposes status, detection, standards generation, and init endpoints
// that drive the setup wizard UI.
package projectapi

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
)

// Component implements the project-api component.
// It provides HTTP endpoints for project initialization state management.
type Component struct {
	name     string
	config   Config
	repoPath string
	logger   *slog.Logger

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

// NewComponent constructs a project-api Component from raw JSON config and deps.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	repoPath := resolveRepoPath(config.RepoPath)

	return &Component{
		name:     "project-api",
		config:   config,
		repoPath: repoPath,
		logger:   deps.GetLogger(),
	}, nil
}

// resolveRepoPath determines the effective repository root.
// Priority: explicit config value → SEMSPEC_REPO_PATH env var → working directory.
func resolveRepoPath(configured string) string {
	if configured != "" {
		return configured
	}
	if env := os.Getenv("SEMSPEC_REPO_PATH"); env != "" {
		return env
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}

// Initialize prepares the component for startup.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized project-api", "repo_path", c.repoPath)
	return nil
}

// Start begins serving the component.
func (c *Component) Start(ctx context.Context) error {
	if !c.state.CompareAndSwap(stateStopped, stateStarting) {
		current := c.state.Load()
		if current == stateRunning || current == stateStarting {
			return fmt.Errorf("component already running or starting")
		}
		return fmt.Errorf("component in invalid state: %d", current)
	}

	defer func() {
		if c.state.Load() == stateStarting {
			c.state.Store(stateStopped)
		}
	}()

	_, cancel := context.WithCancel(ctx)

	c.mu.Lock()
	c.cancel = cancel
	c.startTime = time.Now()
	c.mu.Unlock()

	c.state.Store(stateRunning)
	c.logger.Info("project-api started", "repo_path", c.repoPath)
	return nil
}

// Stop gracefully stops the component.
func (c *Component) Stop(_ time.Duration) error {
	if !c.state.CompareAndSwap(stateRunning, stateStopping) {
		current := c.state.Load()
		if current == stateStopped || current == stateStopping {
			return nil
		}
		return fmt.Errorf("component in unexpected state: %d", current)
	}

	c.mu.Lock()
	cancel := c.cancel
	c.cancel = nil
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	c.state.Store(stateStopped)
	c.logger.Info("project-api stopped")
	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "project-api",
		Type:        "processor",
		Description: "HTTP endpoints for project initialization and status",
		Version:     "0.1.0",
	}
}

// InputPorts returns an empty port list — this component has no NATS inputs.
func (c *Component) InputPorts() []component.Port {
	return []component.Port{}
}

// OutputPorts returns an empty port list — this component has no NATS outputs.
func (c *Component) OutputPorts() []component.Port {
	return []component.Port{}
}

// ConfigSchema returns the configuration schema.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return projectAPISchema
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
