// Package rdfexport provides a streaming output component that subscribes
// to graph entity ingestion messages and serializes them to RDF formats.
package rdfexport

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/export"
	// Import processor packages to trigger init() registration of payloads
	_ "github.com/c360studio/semspec/processor/ast-indexer"
	_ "github.com/c360studio/semspec/processor/constitution"
	_ "github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/component"
	ssgraph "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	ssexport "github.com/c360studio/semstreams/vocabulary/export"
	"github.com/nats-io/nats.go/jetstream"
)

// Component implements the rdf-export output processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	format  ssexport.Format
	profile export.Profile
	baseIRI string

	// Resolved subjects from port config
	inputSubject  string
	inputStream   string
	outputSubject string

	// Lifecycle
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics
	messagesProcessed atomic.Int64
	serializeErrors   atomic.Int64
	publishErrors     atomic.Int64
	lastActivityMu    sync.RWMutex
	lastActivity      time.Time
}

// NewComponent creates a new rdf-export output component.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if config.Ports == nil {
		config = DefaultConfig()
		if err := json.Unmarshal(rawConfig, &config); err != nil {
			return nil, fmt.Errorf("unmarshal config with defaults: %w", err)
		}
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Resolve subjects from port definitions
	inputSubject := "graph.ingest.entity"
	inputStream := "GRAPH"
	outputSubject := "graph.export.rdf"

	if config.Ports != nil {
		if len(config.Ports.Inputs) > 0 {
			inputSubject = config.Ports.Inputs[0].Subject
			inputStream = config.Ports.Inputs[0].StreamName
		}
		if len(config.Ports.Outputs) > 0 {
			outputSubject = config.Ports.Outputs[0].Subject
		}
	}

	return &Component{
		name:          "rdf-export",
		config:        config,
		natsClient:    deps.NATSClient,
		logger:        deps.GetLogger(),
		format:        config.GetFormat(),
		profile:       config.GetProfile(),
		baseIRI:       config.GetBaseIRI(),
		inputSubject:  inputSubject,
		inputStream:   inputStream,
		outputSubject: outputSubject,
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	return nil
}

// Start begins consuming entity ingest messages and producing RDF output.
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

	// Set running state while holding lock to prevent race condition
	c.running = true
	c.startTime = time.Now()

	consumeCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.mu.Unlock()

	consumerCfg := natsclient.StreamConsumerConfig{
		StreamName:    c.inputStream,
		ConsumerName:  "rdf-export",
		FilterSubject: c.inputSubject,
		DeliverPolicy: "new",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       10 * time.Second,
	}

	err := c.natsClient.ConsumeStreamWithConfig(consumeCtx, consumerCfg, c.handleMessage)
	if err != nil {
		// Rollback running state on failure
		c.mu.Lock()
		c.running = false
		c.cancel = nil
		c.mu.Unlock()
		cancel()
		return fmt.Errorf("start consumer: %w", err)
	}

	c.logger.Info("rdf-export started",
		"format", c.config.Format,
		"profile", c.config.Profile,
		"input", c.inputSubject,
		"output", c.outputSubject)

	return nil
}

// handleMessage processes a single entity ingest message.
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	var baseMsg message.BaseMessage
	if err := json.Unmarshal(msg.Data(), &baseMsg); err != nil {
		c.logger.Warn("Failed to unmarshal base message",
			"error", err,
			"subject", msg.Subject())
		_ = msg.Nak()
		return
	}

	graphable, ok := baseMsg.Payload().(ssgraph.Graphable)
	if !ok {
		c.logger.Warn("Payload does not implement Graphable",
			"type", baseMsg.Type(),
			"subject", msg.Subject())
		_ = msg.Nak()
		return
	}

	entityID := graphable.EntityID()
	triples := graphable.Triples()

	// Infer entity type and generate rdf:type triples
	entityType := export.InferEntityType(entityID)
	typeTriples := export.TypeTriples(entityID, entityType, c.profile)

	// Prepend type triples to entity triples
	allTriples := append(typeTriples, triples...)

	// Serialize using semstreams vocabulary/export
	output, err := ssexport.SerializeToString(allTriples, c.format,
		ssexport.WithBaseIRI(c.baseIRI))
	if err != nil {
		c.logger.Warn("Failed to serialize RDF",
			"entity_id", entityID,
			"format", c.config.Format,
			"error", err)
		c.serializeErrors.Add(1)
		_ = msg.Nak()
		return
	}

	// Use JetStream publish for durable RDF output (ADR-005)
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Warn("Failed to get JetStream for RDF output",
			"entity_id", entityID,
			"error", err)
		c.publishErrors.Add(1)
		_ = msg.Nak()
		return
	}
	if _, err := js.Publish(ctx, c.outputSubject, []byte(output)); err != nil {
		c.logger.Warn("Failed to publish RDF output",
			"entity_id", entityID,
			"subject", c.outputSubject,
			"error", err)
		c.publishErrors.Add(1)
		_ = msg.Nak()
		return
	}

	_ = msg.Ack()
	c.messagesProcessed.Add(1)
	c.updateLastActivity()

	c.logger.Debug("Exported entity to RDF",
		"entity_id", entityID,
		"entity_type", entityType,
		"format", c.config.Format,
		"output_bytes", len(output))
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
	c.logger.Info("rdf-export stopped",
		"messages_processed", c.messagesProcessed.Load(),
		"serialize_errors", c.serializeErrors.Load(),
		"publish_errors", c.publishErrors.Load())

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "rdf-export",
		Type:        "output",
		Description: "Serializes graph entities to RDF formats (Turtle, N-Triples, JSON-LD)",
		Version:     "1.0.0",
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

// ConfigSchema returns the configuration schema.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return rdfExportSchema
}

// Health returns the current health status.
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	running := c.running
	startTime := c.startTime
	c.mu.RUnlock()

	errorCount := int(c.serializeErrors.Load() + c.publishErrors.Load())

	status := "stopped"
	if running {
		status = "running"
	}

	return component.HealthStatus{
		Healthy:    running,
		LastCheck:  time.Now(),
		ErrorCount: errorCount,
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
