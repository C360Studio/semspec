// Package astindexer provides an AST indexer processor component
// that extracts code entities from source files and publishes them
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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/graph"
	"github.com/c360studio/semspec/processor/ast"
	// Import language packages to trigger init() registration of parsers
	_ "github.com/c360studio/semspec/processor/ast/golang"
	_ "github.com/c360studio/semspec/processor/ast/ts"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
)

// astIndexerSchema defines the configuration schema
var astIndexerSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// pathWatcher manages watching and parsing for a single resolved path.
type pathWatcher struct {
	config   WatchPathConfig
	root     string                    // Resolved absolute path
	watcher  *ast.Watcher
	parsers  map[string]ast.FileParser // language → parser instance
	excludes map[string]bool           // set of excluded directory names
}

// Component implements the ast-indexer processor
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger
	platform   component.PlatformMeta

	// Per-path watchers
	watchers []*pathWatcher

	// Lifecycle management
	running   bool
	startTime time.Time
	mu        sync.RWMutex

	// Metrics - aggregated across all watchers
	entitiesIndexed atomic.Int64
	parseFailures   atomic.Int64
	errors          atomic.Int64
	lastActivityMu  sync.RWMutex
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

	c := &Component{
		name:       "ast-indexer",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
		platform:   deps.Platform,
	}

	// Initialize watchers for each configured path
	if err := c.initializeWatchers(); err != nil {
		return nil, fmt.Errorf("initialize watchers: %w", err)
	}

	return c, nil
}

// initializeWatchers sets up pathWatcher instances for each configured watch path.
func (c *Component) initializeWatchers() error {
	watchPaths := c.config.GetWatchPaths()

	// Resolve glob patterns to concrete paths
	resolved, err := ResolveWatchPaths(watchPaths)
	if err != nil {
		return err
	}

	for _, rp := range resolved {
		// Verify path exists
		info, err := os.Stat(rp.AbsPath)
		if err != nil {
			return fmt.Errorf("path does not exist: %s: %w", rp.AbsPath, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("path is not a directory: %s", rp.AbsPath)
		}

		pw := &pathWatcher{
			config:   rp.Config,
			root:     rp.AbsPath,
			parsers:  make(map[string]ast.FileParser),
			excludes: make(map[string]bool),
		}

		// Build exclude set
		for _, exc := range rp.Config.Excludes {
			pw.excludes[exc] = true
		}

		// Initialize language-specific parsers using the registry
		languages := rp.Config.Languages
		if len(languages) == 0 {
			languages = []string{"go"}
		}

		for _, lang := range languages {
			parser, err := ast.DefaultRegistry.CreateParser(lang, rp.Config.Org, rp.Config.Project, rp.AbsPath)
			if err != nil {
				return fmt.Errorf("create parser for %s: %w", lang, err)
			}
			pw.parsers[lang] = parser
		}

		c.watchers = append(c.watchers, pw)
	}

	if len(c.watchers) == 0 {
		return fmt.Errorf("no valid watch paths configured")
	}

	return nil
}

// Initialize prepares the component
func (c *Component) Initialize() error {
	return nil
}

// Start begins the AST indexing
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
	c.mu.Unlock()

	// Log what we're watching
	for _, pw := range c.watchers {
		c.logger.Info("Initializing path watcher",
			"path", pw.root,
			"org", pw.config.Org,
			"project", pw.config.Project,
			"languages", pw.config.Languages)
	}

	// Perform initial index for all paths
	c.logger.Info("Starting initial code index",
		"paths", len(c.watchers))

	totalFiles := 0
	for _, pw := range c.watchers {
		results, err := c.parseDirectory(ctx, pw)
		if err != nil {
			return fmt.Errorf("initial index failed for %s: %w", pw.root, err)
		}

		// Publish initial entities
		for _, result := range results {
			if err := c.publishParseResult(ctx, result); err != nil {
				c.logger.Warn("Failed to publish parse result",
					"path", result.Path,
					"error", err)
			}
		}
		totalFiles += len(results)
	}

	c.logger.Info("Initial index complete",
		"paths", len(c.watchers),
		"files", totalFiles,
		"entities", c.entitiesIndexed.Load(),
		"parse_failures", c.parseFailures.Load())

	// Start file watchers if enabled
	if c.config.WatchEnabled {
		for _, pw := range c.watchers {
			if err := c.startWatcher(ctx, pw); err != nil {
				c.logger.Warn("Failed to start file watcher",
					"path", pw.root,
					"error", err)
			}
		}
	}

	// Start periodic reindex if configured
	if c.config.IndexInterval != "" {
		c.startPeriodicIndex(ctx)
	}

	c.mu.Lock()
	c.running = true
	c.startTime = time.Now()
	c.mu.Unlock()

	return nil
}

// startWatcher starts the file system watcher for a specific path.
func (c *Component) startWatcher(ctx context.Context, pw *pathWatcher) error {
	// Build exclude list (config excludes + hidden dirs)
	excludes := make([]string, 0, len(pw.excludes))
	for exc := range pw.excludes {
		excludes = append(excludes, exc)
	}

	watcherConfig := ast.WatcherConfig{
		RepoRoot:       pw.root,
		Org:            pw.config.Org,
		Project:        pw.config.Project,
		DebounceDelay:  100 * time.Millisecond,
		Logger:         c.logger,
		FileExtensions: pw.config.GetFileExtensions(),
		ExcludeDirs:    excludes,
	}

	// Create a multi-language parser wrapper for the watcher
	watcher, err := ast.NewWatcherWithParser(watcherConfig, &multiParser{c: c, pw: pw})
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}
	pw.watcher = watcher

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

	c.logger.Info("File watcher started",
		"path", pw.root,
		"extensions", pw.config.GetFileExtensions())
	return nil
}

// multiParser implements ast.FileParser for multi-language support within a path.
type multiParser struct {
	c  *Component
	pw *pathWatcher
}

func (p *multiParser) ParseFile(ctx context.Context, filePath string) (*ast.ParseResult, error) {
	return p.c.parseFileWithWatcher(ctx, p.pw, filePath)
}

// handleWatchEvent processes a file watcher event
func (c *Component) handleWatchEvent(ctx context.Context, event ast.WatchEvent) {
	c.updateLastActivity()

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

// performFullIndex performs a full reindex of all watched paths.
func (c *Component) performFullIndex(ctx context.Context) {
	c.logger.Debug("Starting periodic reindex")

	totalFiles := 0
	for _, pw := range c.watchers {
		results, err := c.parseDirectory(ctx, pw)
		if err != nil {
			c.logger.Error("Periodic reindex failed",
				"path", pw.root,
				"error", err)
			c.incrementErrors()
			continue
		}

		for _, result := range results {
			if err := c.publishParseResult(ctx, result); err != nil {
				c.logger.Warn("Failed to publish parse result during reindex",
					"path", result.Path,
					"error", err)
			}
		}
		totalFiles += len(results)
	}

	c.logger.Debug("Periodic reindex complete", "files", totalFiles)
}

// parseDirectory parses all source files in a directory using the path's configured parsers.
func (c *Component) parseDirectory(ctx context.Context, pw *pathWatcher) ([]*ast.ParseResult, error) {
	var results []*ast.ParseResult

	// Build extension → parser mapping
	extToParsers := c.buildExtensionParserMap(pw)

	err := filepath.Walk(pw.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if info.IsDir() {
			base := filepath.Base(path)
			// Skip excluded directories
			if pw.excludes[base] || strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		result, err := c.parseFileWithWatcher(ctx, pw, path)
		if err != nil {
			c.logger.Warn("Failed to parse file",
				"path", path,
				"error", err)
			c.incrementParseFailures()
			return nil
		}

		if result != nil {
			results = append(results, result)
		}
		return nil
	})

	// Silence unused warning
	_ = extToParsers

	if err != nil {
		return nil, fmt.Errorf("walk directory: %w", err)
	}

	return results, nil
}

// buildExtensionParserMap creates a mapping from file extension to parser.
func (c *Component) buildExtensionParserMap(pw *pathWatcher) map[string]ast.FileParser {
	result := make(map[string]ast.FileParser)

	for lang, parser := range pw.parsers {
		for _, ext := range ast.DefaultRegistry.GetExtensionsForParser(lang) {
			result[ext] = parser
		}
	}

	return result
}

// parseFileWithWatcher parses a single file using the appropriate parser from the pathWatcher.
func (c *Component) parseFileWithWatcher(ctx context.Context, pw *pathWatcher, filePath string) (*ast.ParseResult, error) {
	ext := strings.ToLower(filepath.Ext(filePath))

	// Find the parser for this extension
	for lang, parser := range pw.parsers {
		for _, langExt := range ast.DefaultRegistry.GetExtensionsForParser(lang) {
			if langExt == ext {
				return parser.ParseFile(ctx, filePath)
			}
		}
	}

	return nil, nil // Skip unsupported file types
}

// publishParseResult publishes parsed entities to graph ingestion
func (c *Component) publishParseResult(ctx context.Context, result *ast.ParseResult) error {
	for _, entity := range result.Entities {
		entityState := entity.EntityState()
		payload := &graph.EntityPayload{
			EntityID_:  entityState.ID,
			TripleData: entityState.Triples,
			UpdatedAt:  entityState.UpdatedAt,
		}
		if err := graph.PublishEntity(ctx, c.natsClient, payload); err != nil {
			return fmt.Errorf("failed to publish entity: %w", err)
		}
		c.entitiesIndexed.Add(1)
		c.updateLastActivity()
	}
	return nil
}

// updateLastActivity safely updates the last activity timestamp
func (c *Component) updateLastActivity() {
	c.lastActivityMu.Lock()
	c.lastActivity = time.Now()
	c.lastActivityMu.Unlock()
}

// getLastActivity safely retrieves the last activity timestamp
func (c *Component) getLastActivity() time.Time {
	c.lastActivityMu.RLock()
	defer c.lastActivityMu.RUnlock()
	return c.lastActivity
}

// incrementErrors safely increments the error counter
func (c *Component) incrementErrors() {
	c.errors.Add(1)
}

// incrementParseFailures safely increments the parse failure counter
func (c *Component) incrementParseFailures() {
	c.parseFailures.Add(1)
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

	// Stop all file watchers
	for _, pw := range c.watchers {
		if pw.watcher != nil {
			if err := pw.watcher.Stop(); err != nil {
				c.logger.Warn("Error stopping watcher",
					"path", pw.root,
					"error", err)
			}
		}
	}

	c.running = false
	c.logger.Info("AST indexer stopped",
		"paths", len(c.watchers),
		"entities_indexed", c.entitiesIndexed.Load(),
		"parse_failures", c.parseFailures.Load(),
		"errors", c.errors.Load())

	return nil
}

// Discoverable interface implementation

// Meta returns component metadata
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "ast-indexer",
		Type:        "processor",
		Description: "Multi-language AST indexer for code entity extraction and graph storage",
		Version:     "0.2.0",
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
		ports[i] = buildPort(portDef, component.DirectionOutput)
	}
	return ports
}

// buildPort creates a component.Port from a PortDefinition, using JetStreamPort
// for jetstream-type ports and NATSPort for core NATS ports.
func buildPort(portDef component.PortDefinition, direction component.Direction) component.Port {
	port := component.Port{
		Name:        portDef.Name,
		Direction:   direction,
		Required:    portDef.Required,
		Description: portDef.Description,
	}
	if portDef.Type == "jetstream" {
		port.Config = component.JetStreamPort{
			StreamName: portDef.StreamName,
			Subjects:   []string{portDef.Subject},
		}
	} else {
		port.Config = component.NATSPort{
			Subject: portDef.Subject,
		}
	}
	return port
}

// ConfigSchema returns the configuration schema
func (c *Component) ConfigSchema() component.ConfigSchema {
	return astIndexerSchema
}

// Health returns the current health status
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	running := c.running
	startTime := c.startTime
	c.mu.RUnlock()

	return component.HealthStatus{
		Healthy:    running,
		LastCheck:  time.Now(),
		ErrorCount: int(c.errors.Load()),
		Uptime:     time.Since(startTime),
		Status:     c.getStatusString(running),
	}
}

// getStatusString returns a status string based on running state
func (c *Component) getStatusString(running bool) string {
	if running {
		return "running"
	}
	return "stopped"
}

// DataFlow returns current data flow metrics
func (c *Component) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{
		MessagesPerSecond: 0, // Could calculate from entitiesIndexed over time
		BytesPerSecond:    0,
		ErrorRate:         0,
		LastActivity:      c.getLastActivity(),
	}
}
