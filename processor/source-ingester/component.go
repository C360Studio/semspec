// Package sourceingester provides a component for ingesting documents
// and SOPs into the knowledge graph for context assembly.
package sourceingester

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// sourceIngesterSchema defines the configuration schema.
var sourceIngesterSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// graphIngestSubject is the subject for publishing entities.
const graphIngestSubject = "graph.ingest.entity"

// Component implements the source-ingester processor.
type Component struct {
	name          string
	config        Config
	natsClient    *natsclient.Client
	logger        *slog.Logger
	platform      component.PlatformMeta
	handler       *Handler
	openSpecHandler *OpenSpecHandler
	watcher       *DocWatcher

	// Lifecycle management
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics
	documentsIngested atomic.Int64
	chunksPublished   atomic.Int64
	errors            atomic.Int64
	lastActivityMu    sync.RWMutex
	lastActivity      time.Time
}

// NewComponent creates a new source-ingester processor component.
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
		name:       "source-ingester",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
		platform:   deps.Platform,
	}

	return c, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	return nil
}

// Start begins processing document ingestion requests.
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
	// Mark as starting immediately to prevent concurrent starts
	c.running = true
	c.startTime = time.Now()
	c.mu.Unlock()

	// Create LLM client for document analysis
	// For now, create a minimal registry - in production this would come from config
	registry := c.createModelRegistry()
	llmClient := llm.NewClient(registry, llm.WithCallStore(llm.GlobalCallStore()))

	// Create document handler
	handler, err := NewHandler(
		llmClient,
		c.config.SourcesDir,
		c.config.ChunkConfig,
		c.config.GetAnalysisTimeout(),
	)
	if err != nil {
		c.mu.Lock()
		c.running = false
		c.mu.Unlock()
		return fmt.Errorf("create handler: %w", err)
	}
	c.handler = handler

	// Create OpenSpec handler
	c.openSpecHandler = NewOpenSpecHandler(c.config.SourcesDir)

	// Set up consumer for ingestion requests
	runCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	// Start consumer in background
	go c.consumeMessages(runCtx)

	// Start file watcher if enabled
	if c.config.WatchConfig.Enabled {
		watcher, err := NewDocWatcher(c.config.WatchConfig, c.config.SourcesDir, c.logger)
		if err != nil {
			c.logger.Error("Failed to create document watcher", "error", err)
		} else {
			c.watcher = watcher
			if err := watcher.Start(runCtx); err != nil {
				c.logger.Error("Failed to start document watcher", "error", err)
			} else {
				// Process watcher events in background
				go c.processWatchEvents(runCtx)
			}
		}
	}

	c.logger.Info("Source ingester started",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"sources_dir", c.config.SourcesDir,
		"watching", c.config.WatchConfig.Enabled)

	return nil
}

// createModelRegistry returns the global model registry.
// This ensures consistent health tracking and configuration with other components.
func (c *Component) createModelRegistry() *model.Registry {
	return model.Global()
}

// consumeMessages processes incoming ingestion requests.
func (c *Component) consumeMessages(ctx context.Context) {
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Error("Failed to get JetStream context", "error", err)
		return
	}

	// Get stream
	stream, err := js.Stream(ctx, c.config.StreamName)
	if err != nil {
		c.logger.Error("Failed to get stream", "error", err, "stream", c.config.StreamName)
		return
	}

	// Create or update durable consumer
	consumerConfig := jetstream.ConsumerConfig{
		Durable:       c.config.ConsumerName,
		FilterSubject: "source.ingest.>",
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       60 * time.Second,
		MaxDeliver:    3,
	}
	consumer, err := stream.CreateOrUpdateConsumer(ctx, consumerConfig)
	if err != nil {
		c.logger.Error("Failed to create consumer", "error", err, "stream", c.config.StreamName, "consumer", c.config.ConsumerName)
		return
	}

	c.logger.Info("Consumer connected", "stream", c.config.StreamName, "consumer", c.config.ConsumerName)

	// Consume messages
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Fetch next message with timeout
		msgs, err := consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue // Timeout, try again
		}

		for msg := range msgs.Messages() {
			select {
			case <-ctx.Done():
				// NAK the message so it can be redelivered
				_ = msg.Nak()
				return
			default:
				c.handleMessage(ctx, msg)
			}
		}
	}
}

// handleMessage processes a single ingestion request.
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	c.updateLastActivity()

	// Parse request
	var req IngestRequest
	if err := json.Unmarshal(msg.Data(), &req); err != nil {
		c.logger.Warn("Failed to parse ingestion request", "error", err)
		c.errors.Add(1)
		_ = msg.Nak()
		return
	}

	c.logger.Info("Processing ingestion request", "path", req.Path, "project_id", req.ProjectID)

	// Process document - use OpenSpec handler for OpenSpec files
	var entities []*SourceEntityPayload
	var err error
	if IsOpenSpecFile(req.Path) {
		c.logger.Debug("Detected OpenSpec file", "path", req.Path)
		entities, err = c.openSpecHandler.IngestSpec(ctx, req)
	} else {
		entities, err = c.handler.IngestDocument(ctx, req)
	}
	if err != nil {
		c.logger.Error("Failed to ingest document", "path", req.Path, "error", err)
		c.errors.Add(1)
		_ = msg.Nak()
		return
	}

	// Publish entities to graph
	if err := c.publishEntities(ctx, entities); err != nil {
		c.logger.Error("Failed to publish entities", "path", req.Path, "error", err)
		c.errors.Add(1)
		_ = msg.Nak()
		return
	}

	c.documentsIngested.Add(1)
	_ = msg.Ack()

	c.logger.Info("Document ingested successfully",
		"path", req.Path,
		"entities", len(entities))
}

// publishEntity wraps a SourceEntityPayload and publishes it to the graph ingestion stream.
func (c *Component) publishEntity(ctx context.Context, entity *SourceEntityPayload) error {
	msg := message.NewBaseMessage(SourceEntityType, entity, "semspec")
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal entity message: %w", err)
	}
	return c.natsClient.PublishToStream(ctx, graphIngestSubject, data)
}

// publishEntities publishes all entities to the graph in the correct order.
// Chunks are published first, then the parent entity to ensure chunks are never orphaned.
func (c *Component) publishEntities(ctx context.Context, entities []*SourceEntityPayload) error {
	// Publish chunks first (entities[1:])
	if len(entities) > 1 {
		for _, chunk := range entities[1:] {
			if err := c.publishEntity(ctx, chunk); err != nil {
				return fmt.Errorf("publish chunk %s: %w", chunk.EntityID_, err)
			}
			c.chunksPublished.Add(1)
		}
	}
	// Publish parent entity last (entities[0])
	if len(entities) > 0 {
		if err := c.publishEntity(ctx, entities[0]); err != nil {
			return fmt.Errorf("publish parent %s: %w", entities[0].EntityID_, err)
		}
	}
	return nil
}

// processWatchEvents handles file watch events and triggers ingestion.
func (c *Component) processWatchEvents(ctx context.Context) {
	if c.watcher == nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-c.watcher.Events():
			if !ok {
				return
			}
			c.handleWatchEvent(ctx, event)
		}
	}
}

// handleWatchEvent processes a single file watch event.
func (c *Component) handleWatchEvent(ctx context.Context, event WatchEvent) {
	c.updateLastActivity()

	switch event.Operation {
	case WatchOpCreate, WatchOpModify:
		// Trigger ingestion for new or modified documents
		c.logger.Info("Document file changed, triggering ingestion",
			"path", event.Path,
			"operation", event.Operation)

		// Create ingestion request
		req := IngestRequest{
			Path: event.Path,
		}

		// Process document - use OpenSpec handler for OpenSpec files
		var entities []*SourceEntityPayload
		var err error
		if IsOpenSpecFile(req.Path) {
			c.logger.Debug("Detected OpenSpec file via watcher", "path", req.Path)
			entities, err = c.openSpecHandler.IngestSpec(ctx, req)
		} else {
			entities, err = c.handler.IngestDocument(ctx, req)
		}
		if err != nil {
			c.logger.Error("Failed to ingest watched document",
				"path", event.Path,
				"error", err)
			c.errors.Add(1)
			return
		}

		// Publish entities to graph
		if err := c.publishEntities(ctx, entities); err != nil {
			c.logger.Error("Failed to publish entities", "path", event.Path, "error", err)
			c.errors.Add(1)
			return
		}

		c.documentsIngested.Add(1)
		c.logger.Info("Watched document ingested successfully",
			"path", event.Path,
			"entities", len(entities))

	case WatchOpDelete:
		// Log deletion - graph cleanup would be handled separately
		c.logger.Info("Document file deleted",
			"path", event.Path)
		// TODO: Publish deletion event to graph for entity removal
	}
}

// updateLastActivity safely updates the last activity timestamp.
func (c *Component) updateLastActivity() {
	c.lastActivityMu.Lock()
	c.lastActivity = time.Now()
	c.lastActivityMu.Unlock()
}

// getLastActivity safely retrieves the last activity timestamp.
func (c *Component) getLastActivity() time.Time {
	c.lastActivityMu.RLock()
	defer c.lastActivityMu.RUnlock()
	return c.lastActivity
}

// Stop gracefully stops the component within the given timeout.
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	if c.cancel != nil {
		c.cancel()
	}

	// Stop watcher if running
	if c.watcher != nil {
		if err := c.watcher.Stop(); err != nil {
			c.logger.Error("Failed to stop document watcher", "error", err)
		}
	}

	c.running = false
	c.logger.Info("Source ingester stopped",
		"documents_ingested", c.documentsIngested.Load(),
		"chunks_published", c.chunksPublished.Load(),
		"errors", c.errors.Load())

	return nil
}

// Discoverable interface implementation

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "source-ingester",
		Type:        "processor",
		Description: "Document and SOP ingester for knowledge graph population",
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
		ports[i] = buildPort(portDef, component.DirectionInput)
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
		ports[i] = buildPort(portDef, component.DirectionOutput)
	}
	return ports
}

// buildPort creates a component.Port from a PortDefinition.
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

// ConfigSchema returns the configuration schema.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return sourceIngesterSchema
}

// Health returns the current health status.
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

// getStatusString returns a status string based on running state.
func (c *Component) getStatusString(running bool) string {
	if running {
		return "running"
	}
	return "stopped"
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
