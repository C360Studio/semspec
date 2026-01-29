// Package astindexer provides an AST indexer processor component
// that extracts code entities from Go source files and publishes them
// to the graph ingestion pipeline.
package astindexer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/c360/semspec/processor/ast"
	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/message"
	"github.com/c360/semstreams/natsclient"
)

// astIndexerSchema defines the configuration schema
var astIndexerSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Component implements the ast-indexer processor
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger
	platform   component.PlatformMeta

	// AST parsing
	parser  *ast.Parser
	watcher *ast.Watcher

	// Lifecycle management
	running   bool
	startTime time.Time
	mu        sync.RWMutex

	// Metrics
	entitiesIndexed int64
	errors          int64
	lastActivity    time.Time

	// Cancel functions for background goroutines
	cancelFuncs []context.CancelFunc
}

// NewComponent creates a new ast-indexer processor component
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Use default config if ports not set
	if config.Ports == nil {
		config = DefaultConfig()
		// Re-unmarshal to get user-provided values
		if err := json.Unmarshal(rawConfig, &config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Resolve repo path to absolute
	absRepoPath, err := filepath.Abs(config.RepoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve repo path: %w", err)
	}

	// Verify repo path exists
	info, err := os.Stat(absRepoPath)
	if err != nil {
		return nil, fmt.Errorf("repo path does not exist: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("repo path is not a directory: %s", absRepoPath)
	}
	config.RepoPath = absRepoPath

	return &Component{
		name:       "ast-indexer",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
		platform:   deps.Platform,
		parser:     ast.NewParser(config.Org, config.Project, absRepoPath),
	}, nil
}

// Initialize prepares the component
func (c *Component) Initialize() error {
	return nil
}

// Start begins the AST indexing
func (c *Component) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return fmt.Errorf("component already running")
	}

	if c.natsClient == nil {
		return fmt.Errorf("NATS client required")
	}

	// Perform initial index
	c.logger.Info("Starting initial code index",
		"repo_path", c.config.RepoPath,
		"org", c.config.Org,
		"project", c.config.Project)

	results, err := c.parser.ParseDirectory(ctx, c.config.RepoPath)
	if err != nil {
		return fmt.Errorf("initial index failed: %w", err)
	}

	// Publish initial entities
	for _, result := range results {
		if err := c.publishParseResult(ctx, result); err != nil {
			c.logger.Warn("Failed to publish parse result",
				"path", result.Path,
				"error", err)
		}
	}

	c.logger.Info("Initial index complete",
		"files", len(results),
		"entities", c.entitiesIndexed)

	// Start file watcher if enabled
	if c.config.WatchEnabled {
		if err := c.startWatcher(ctx); err != nil {
			c.logger.Warn("Failed to start file watcher", "error", err)
		}
	}

	// Start periodic reindex if configured
	if c.config.IndexInterval != "" {
		c.startPeriodicIndex(ctx)
	}

	c.running = true
	c.startTime = time.Now()

	return nil
}

// startWatcher starts the file system watcher
func (c *Component) startWatcher(ctx context.Context) error {
	watcherConfig := ast.WatcherConfig{
		RepoRoot:      c.config.RepoPath,
		Org:           c.config.Org,
		Project:       c.config.Project,
		DebounceDelay: 100 * time.Millisecond,
		Logger:        c.logger,
	}

	watcher, err := ast.NewWatcher(watcherConfig)
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}
	c.watcher = watcher

	if err := watcher.Start(ctx); err != nil {
		return fmt.Errorf("failed to start watcher: %w", err)
	}

	// Process watch events in background
	watchCtx, cancel := context.WithCancel(ctx)
	c.cancelFuncs = append(c.cancelFuncs, cancel)

	go func() {
		for {
			select {
			case <-watchCtx.Done():
				return
			case event, ok := <-watcher.Events():
				if !ok {
					return
				}
				c.handleWatchEvent(watchCtx, event)
			}
		}
	}()

	c.logger.Info("File watcher started", "repo_path", c.config.RepoPath)
	return nil
}

// handleWatchEvent processes a file watcher event
func (c *Component) handleWatchEvent(ctx context.Context, event ast.WatchEvent) {
	c.mu.Lock()
	c.lastActivity = time.Now()
	c.mu.Unlock()

	switch event.Operation {
	case ast.OpCreate, ast.OpModify:
		if event.Result != nil {
			if err := c.publishParseResult(ctx, event.Result); err != nil {
				c.logger.Warn("Failed to publish parse result",
					"path", event.Path,
					"error", err)
				c.incrementErrors()
			}
		}
	case ast.OpDelete:
		// For delete, we could publish a tombstone or deletion message
		// For now, just log it
		c.logger.Debug("File deleted", "path", event.Path)
	}

	if event.Error != nil {
		c.logger.Warn("Watch event error",
			"path", event.Path,
			"error", event.Error)
		c.incrementErrors()
	}
}

// startPeriodicIndex starts periodic full reindex
func (c *Component) startPeriodicIndex(ctx context.Context) {
	interval, err := time.ParseDuration(c.config.IndexInterval)
	if err != nil {
		c.logger.Warn("Invalid index interval, skipping periodic index", "error", err)
		return
	}

	indexCtx, cancel := context.WithCancel(ctx)
	c.cancelFuncs = append(c.cancelFuncs, cancel)

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-indexCtx.Done():
				return
			case <-ticker.C:
				c.performFullIndex(indexCtx)
			}
		}
	}()

	c.logger.Info("Periodic index started", "interval", interval)
}

// performFullIndex performs a full reindex of the repository
func (c *Component) performFullIndex(ctx context.Context) {
	c.logger.Debug("Starting periodic reindex")

	results, err := c.parser.ParseDirectory(ctx, c.config.RepoPath)
	if err != nil {
		c.logger.Error("Periodic reindex failed", "error", err)
		c.incrementErrors()
		return
	}

	for _, result := range results {
		if err := c.publishParseResult(ctx, result); err != nil {
			c.logger.Warn("Failed to publish parse result during reindex",
				"path", result.Path,
				"error", err)
		}
	}

	c.logger.Debug("Periodic reindex complete", "files", len(results))
}

// publishParseResult publishes parsed entities to graph ingestion
func (c *Component) publishParseResult(ctx context.Context, result *ast.ParseResult) error {
	// Publish each entity state
	for _, entity := range result.Entities {
		entityState := entity.EntityState()

		// Convert to graph ingest message format
		msg := EntityIngestMessage{
			ID:        entityState.ID,
			Triples:   entityState.Triples,
			UpdatedAt: entityState.UpdatedAt,
		}

		data, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("failed to marshal entity: %w", err)
		}

		// Publish to graph.ingest.entity
		if err := c.natsClient.PublishToStream(ctx, "graph.ingest.entity", data); err != nil {
			return fmt.Errorf("failed to publish entity: %w", err)
		}

		c.mu.Lock()
		c.entitiesIndexed++
		c.lastActivity = time.Now()
		c.mu.Unlock()
	}

	return nil
}

// EntityIngestMessage is the message format for graph ingestion
type EntityIngestMessage struct {
	ID        string           `json:"id"`
	Triples   []message.Triple `json:"triples"`
	UpdatedAt time.Time        `json:"updated_at"`
}

// incrementErrors safely increments the error counter
func (c *Component) incrementErrors() {
	c.mu.Lock()
	c.errors++
	c.mu.Unlock()
}

// Stop gracefully stops the component within the given timeout
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	// Cancel all background goroutines
	for _, cancel := range c.cancelFuncs {
		cancel()
	}
	c.cancelFuncs = nil

	// Stop file watcher
	if c.watcher != nil {
		if err := c.watcher.Stop(); err != nil {
			c.logger.Warn("Error stopping watcher", "error", err)
		}
	}

	c.running = false
	c.logger.Info("AST indexer stopped",
		"entities_indexed", c.entitiesIndexed,
		"errors", c.errors)

	return nil
}

// Discoverable interface implementation

// Meta returns component metadata
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "ast-indexer",
		Type:        "processor",
		Description: "Go AST indexer for code entity extraction and graph storage",
		Version:     "0.1.0",
	}
}

// InputPorts returns configured input port definitions
func (c *Component) InputPorts() []component.Port {
	// AST indexer has no input ports - it generates data from file system
	return []component.Port{}
}

// OutputPorts returns configured output port definitions
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

// ConfigSchema returns the configuration schema
func (c *Component) ConfigSchema() component.ConfigSchema {
	return astIndexerSchema
}

// Health returns the current health status
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return component.HealthStatus{
		Healthy:    c.running,
		LastCheck:  time.Now(),
		ErrorCount: int(c.errors),
		Uptime:     time.Since(c.startTime),
		Status:     c.getStatus(),
	}
}

// getStatus returns a status string
func (c *Component) getStatus() string {
	if c.running {
		return "running"
	}
	return "stopped"
}

// DataFlow returns current data flow metrics
func (c *Component) DataFlow() component.FlowMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return component.FlowMetrics{
		MessagesPerSecond: 0, // Could calculate from entitiesIndexed over time
		BytesPerSecond:    0,
		ErrorRate:         0,
		LastActivity:      c.lastActivity,
	}
}
