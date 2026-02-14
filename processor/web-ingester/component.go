// Package webingester provides a component for ingesting web pages
// into the knowledge graph for context assembly.
package webingester

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/source"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// webIngesterSchema defines the configuration schema.
var webIngesterSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// graphIngestSubject is the subject for publishing entities.
const graphIngestSubject = "graph.ingest.entity"

// Component implements the web-ingester processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger
	platform   component.PlatformMeta
	handler    *Handler

	// Lifecycle management
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc
	wg        sync.WaitGroup // Tracks running goroutines for graceful shutdown

	// Metrics
	pagesIngested   atomic.Int64
	chunksPublished atomic.Int64
	errors          atomic.Int64
	lastActivityMu  sync.RWMutex
	lastActivity    time.Time
}

// NewComponent creates a new web-ingester processor component.
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
		name:       "web-ingester",
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

// Start begins processing web source ingestion requests.
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

	// Create fetcher
	fetcher := NewFetcher(
		c.config.GetFetchTimeout(),
		c.config.GetUserAgent(),
		c.config.GetMaxContentSize(),
	)

	// Create handler
	handler, err := NewHandler(fetcher, c.config.ChunkConfig)
	if err != nil {
		c.mu.Lock()
		c.running = false
		c.mu.Unlock()
		return fmt.Errorf("create handler: %w", err)
	}
	c.handler = handler

	// Set up consumer for ingestion requests
	runCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	// Start consumer in background with WaitGroup tracking
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.consumeMessages(runCtx)
	}()

	c.logger.Info("Web ingester started",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName)

	return nil
}

// consumeMessages processes incoming web ingestion requests.
func (c *Component) consumeMessages(ctx context.Context) {
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Error("Failed to get JetStream context", "error", err)
		return
	}

	// Get or create consumer
	consumer, err := js.Consumer(ctx, c.config.StreamName, c.config.ConsumerName)
	if err != nil {
		c.logger.Error("Failed to get consumer", "error", err, "stream", c.config.StreamName, "consumer", c.config.ConsumerName)
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
				// NAK the current message so it can be redelivered
				_ = msg.Nak()
				// Drain remaining messages from this batch
				for remaining := range msgs.Messages() {
					_ = remaining.Nak()
				}
				return
			default:
				c.handleMessage(ctx, msg)
			}
		}
	}
}

// handleMessage processes a single web ingestion request.
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	c.updateLastActivity()

	// Parse request
	var req source.AddWebSourceRequest
	if err := json.Unmarshal(msg.Data(), &req); err != nil {
		c.logger.Warn("Failed to parse web ingestion request", "error", err)
		c.errors.Add(1)
		_ = msg.Nak()
		return
	}

	c.logger.Info("Processing web ingestion request", "url", req.URL, "project", req.Project)

	// Process web source
	result, err := c.handler.IngestWebSource(ctx, req)
	if err != nil {
		c.logger.Error("Failed to ingest web source", "url", req.URL, "error", err)
		c.errors.Add(1)
		_ = msg.Nak()
		return
	}

	// Publish entities to graph
	// Publish chunks first, then parent - this ensures chunks are never orphaned
	if len(result.Entities) > 1 {
		// Chunks are entities[1:]
		for _, chunk := range result.Entities[1:] {
			if err := c.publishEntity(ctx, chunk); err != nil {
				c.logger.Error("Failed to publish chunk", "entity_id", chunk.EntityID_, "error", err)
				c.errors.Add(1)
				_ = msg.Nak()
				return
			}
			c.chunksPublished.Add(1)
		}
	}
	// Publish parent entity last
	if len(result.Entities) > 0 {
		if err := c.publishEntity(ctx, result.Entities[0]); err != nil {
			c.logger.Error("Failed to publish parent entity", "entity_id", result.Entities[0].EntityID_, "error", err)
			c.errors.Add(1)
			_ = msg.Nak()
			return
		}
	}

	c.pagesIngested.Add(1)
	_ = msg.Ack()

	c.logger.Info("Web source ingested successfully",
		"url", req.URL,
		"title", result.Title,
		"chunks", result.ChunkCount)
}

// publishEntity wraps a WebEntityPayload and publishes it to the graph ingestion stream.
func (c *Component) publishEntity(ctx context.Context, entity *WebEntityPayload) error {
	msg := message.NewBaseMessage(WebEntityType, entity, "semspec")
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal entity message: %w", err)
	}
	return c.natsClient.PublishToStream(ctx, graphIngestSubject, data)
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
func (c *Component) Stop(timeout time.Duration) error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil
	}

	if c.cancel != nil {
		c.cancel()
	}
	c.mu.Unlock()

	// Wait for goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	var err error
	select {
	case <-done:
		// Graceful shutdown completed
	case <-time.After(timeout):
		err = fmt.Errorf("stop timed out after %v", timeout)
	}

	c.mu.Lock()
	c.running = false
	c.mu.Unlock()

	c.logger.Info("Web ingester stopped",
		"pages_ingested", c.pagesIngested.Load(),
		"chunks_published", c.chunksPublished.Load(),
		"errors", c.errors.Load())

	return err
}

// Discoverable interface implementation

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "web-ingester",
		Type:        "processor",
		Description: "Web page ingester for knowledge graph population",
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
	return webIngesterSchema
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
