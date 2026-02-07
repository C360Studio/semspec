// Package workflowdocuments provides an output component that subscribes to
// workflow document messages and writes them as markdown files.
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

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// Component implements the workflow-documents output processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	transformer *Transformer
	baseDir     string

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

	// Apply defaults if ports not specified
	if config.Ports == nil {
		defaults := DefaultConfig()
		config.Ports = defaults.Ports
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Resolve base directory
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

	// Resolve subjects from port definitions
	inputSubject := "output.workflow.documents"
	inputStream := "WORKFLOW"
	outputSubject := "workflow.documents.written"

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
		name:          "workflow-documents",
		config:        config,
		natsClient:    deps.NATSClient,
		logger:        deps.GetLogger(),
		transformer:   NewTransformer(),
		baseDir:       baseDir,
		inputSubject:  inputSubject,
		inputStream:   inputStream,
		outputSubject: outputSubject,
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	// Ensure base directory exists
	semspecDir := filepath.Join(c.baseDir, ".semspec", "changes")
	if err := os.MkdirAll(semspecDir, 0755); err != nil {
		return fmt.Errorf("create semspec directory: %w", err)
	}

	c.logger.Debug("Initialized workflow-documents component",
		"base_dir", c.baseDir,
		"semspec_dir", semspecDir)

	return nil
}

// Start begins consuming document output messages and writing files.
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
		ConsumerName:  "workflow-documents",
		FilterSubject: c.inputSubject,
		DeliverPolicy: "new",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       30 * time.Second,
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

	c.logger.Info("workflow-documents started",
		"base_dir", c.baseDir,
		"input", c.inputSubject,
		"output", c.outputSubject)

	return nil
}

// handleMessage processes a single document output message.
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	var baseMsg message.BaseMessage
	if err := json.Unmarshal(msg.Data(), &baseMsg); err != nil {
		c.logger.Warn("Failed to unmarshal base message",
			"error", err,
			"subject", msg.Subject())
		_ = msg.Nak()
		return
	}

	// Extract payload - try direct type assertion first
	payload, ok := baseMsg.Payload().(*DocumentOutputPayload)
	if !ok {
		// Fallback: re-marshal and unmarshal the payload
		// This handles cases where the payload was deserialized as a generic type
		payloadBytes, err := json.Marshal(baseMsg.Payload())
		if err != nil {
			c.logger.Warn("Failed to marshal payload for conversion",
				"error", err,
				"subject", msg.Subject())
			_ = msg.Nak()
			return
		}
		var rawPayload DocumentOutputPayload
		if err := json.Unmarshal(payloadBytes, &rawPayload); err != nil {
			c.logger.Warn("Payload is not DocumentOutputPayload",
				"type", baseMsg.Type(),
				"subject", msg.Subject())
			_ = msg.Nak()
			return
		}
		payload = &rawPayload
	}

	if payload.Slug == "" || payload.Document == "" {
		c.logger.Warn("Invalid document output payload",
			"slug", payload.Slug,
			"document", payload.Document)
		_ = msg.Term()
		return
	}

	// Transform and write document
	if err := c.writeDocument(ctx, payload); err != nil {
		c.logger.Error("Failed to write document",
			"slug", payload.Slug,
			"document", payload.Document,
			"error", err)
		c.writeErrors.Add(1)
		_ = msg.Nak()
		return
	}

	_ = msg.Ack()
	c.documentsWritten.Add(1)
	c.updateLastActivity()

	c.logger.Info("Wrote document",
		"slug", payload.Slug,
		"document", payload.Document)
}

// writeDocument transforms content and writes the markdown file.
func (c *Component) writeDocument(ctx context.Context, payload *DocumentOutputPayload) error {
	// Check for context cancellation before starting work
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Create change directory
	changeDir := filepath.Join(c.baseDir, ".semspec", "changes", payload.Slug)
	if err := os.MkdirAll(changeDir, 0755); err != nil {
		return fmt.Errorf("create change directory: %w", err)
	}

	// Check for context cancellation before transformation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Transform content to markdown based on document type
	var markdown string
	switch payload.Document {
	case "proposal":
		markdown = c.transformer.TransformProposal(payload.Content)
	case "design":
		markdown = c.transformer.TransformDesign(payload.Content)
	case "spec":
		markdown = c.transformer.TransformSpec(payload.Content)
	case "tasks":
		markdown = c.transformer.TransformTasks(payload.Content)
	default:
		markdown = c.transformer.Transform(payload.Content)
	}

	// Check for context cancellation before file write
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Write markdown file
	filename := payload.Document + ".md"
	filePath := filepath.Join(changeDir, filename)

	if err := os.WriteFile(filePath, []byte(markdown), 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	// Publish notification
	if c.outputSubject != "" {
		notification := &DocumentWrittenPayload{
			Slug:     payload.Slug,
			Document: payload.Document,
			Path:     filePath,
			EntityID: payload.EntityID,
		}

		notificationMsg := message.NewBaseMessage(DocumentWrittenType, notification, "workflow-documents")
		data, err := json.Marshal(notificationMsg)
		if err != nil {
			c.logger.Warn("Failed to marshal written notification",
				"error", err)
		} else {
			if err := c.natsClient.Publish(ctx, c.outputSubject, data); err != nil {
				c.logger.Warn("Failed to publish written notification",
					"error", err,
					"subject", c.outputSubject)
			}
		}
	}

	return nil
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
		Description: "Transforms workflow JSON content to markdown files",
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
	return workflowDocumentsSchema
}

// Health returns the current health status.
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	running := c.running
	startTime := c.startTime
	c.mu.RUnlock()

	errorCount := int(c.writeErrors.Load())

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
