package contextbuilder

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// Component implements the context builder processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	modelRegistry *model.Registry
	builder       *Builder

	// JetStream consumer
	consumer jetstream.Consumer
	stream   jetstream.Stream

	// KV bucket for storing context responses (for HTTP queries)
	responseBucket jetstream.KeyValue

	// Lifecycle state machine
	// States: 0=stopped, 1=starting, 2=running, 3=stopping
	state     atomic.Int32
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics
	requestsProcessed atomic.Int64
	requestsFailed    atomic.Int64
	lastActivityMu    sync.RWMutex
	lastActivity      time.Time
}

const (
	stateStopped  = 0
	stateStarting = 1
	stateRunning  = 2
	stateStopping = 3
)

// NewComponent creates a new context builder processor.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Apply defaults
	defaults := DefaultConfig()
	if config.StreamName == "" {
		config.StreamName = defaults.StreamName
	}
	if config.ConsumerName == "" {
		config.ConsumerName = defaults.ConsumerName
	}
	if config.InputSubjectPattern == "" {
		config.InputSubjectPattern = defaults.InputSubjectPattern
	}
	if config.OutputSubjectPrefix == "" {
		config.OutputSubjectPrefix = defaults.OutputSubjectPrefix
	}
	if config.DefaultTokenBudget == 0 {
		config.DefaultTokenBudget = defaults.DefaultTokenBudget
	}
	if config.HeadroomTokens == 0 {
		config.HeadroomTokens = defaults.HeadroomTokens
	}
	if config.GraphGatewayURL == "" {
		config.GraphGatewayURL = defaults.GraphGatewayURL
	}
	if config.DefaultCapability == "" {
		config.DefaultCapability = defaults.DefaultCapability
	}
	if config.SOPEntityPrefix == "" {
		config.SOPEntityPrefix = defaults.SOPEntityPrefix
	}
	if config.Ports == nil {
		config.Ports = defaults.Ports
	}
	if config.ResponseBucketName == "" {
		config.ResponseBucketName = defaults.ResponseBucketName
	}
	if config.ResponseTTLHours == 0 {
		config.ResponseTTLHours = defaults.ResponseTTLHours
	}

	// Get repo path from config or environment
	if config.RepoPath == "" {
		config.RepoPath = os.Getenv("SEMSPEC_REPO_PATH")
	}
	if config.RepoPath == "" {
		var err error
		config.RepoPath, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get working directory: %w", err)
		}
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	logger := deps.GetLogger()
	modelRegistry := model.Global()
	builder := NewBuilder(config, modelRegistry, logger)

	// Set up Q&A integration if NATS client is available
	if deps.NATSClient != nil {
		if err := builder.SetQAIntegration(deps.NATSClient, config); err != nil {
			logger.Warn("Failed to set up Q&A integration", "error", err)
			// Continue without Q&A - graceful degradation
		}
	}

	return &Component{
		name:          "context-builder",
		config:        config,
		natsClient:    deps.NATSClient,
		logger:        logger,
		modelRegistry: modelRegistry,
		builder:       builder,
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized context-builder",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"input_pattern", c.config.InputSubjectPattern)
	return nil
}

// Start begins processing context build requests.
// Uses a state machine to prevent race conditions between Start and Stop.
func (c *Component) Start(ctx context.Context) error {
	// Atomically transition from stopped to starting
	if !c.state.CompareAndSwap(stateStopped, stateStarting) {
		currentState := c.state.Load()
		if currentState == stateRunning || currentState == stateStarting {
			return fmt.Errorf("component already running or starting")
		}
		return fmt.Errorf("component in invalid state: %d", currentState)
	}

	// Ensure we transition to stopped if setup fails
	defer func() {
		if c.state.Load() == stateStarting {
			c.state.Store(stateStopped)
		}
	}()

	if c.natsClient == nil {
		return fmt.Errorf("NATS client required")
	}

	// Get JetStream context
	js, err := c.natsClient.JetStream()
	if err != nil {
		return fmt.Errorf("get jetstream: %w", err)
	}

	// Get stream
	stream, err := js.Stream(ctx, c.config.StreamName)
	if err != nil {
		return fmt.Errorf("get stream %s: %w", c.config.StreamName, err)
	}

	// Create or get consumer
	consumerConfig := jetstream.ConsumerConfig{
		Durable:       c.config.ConsumerName,
		FilterSubject: c.config.InputSubjectPattern,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       60 * time.Second,
		MaxDeliver:    3,
	}

	consumer, err := stream.CreateOrUpdateConsumer(ctx, consumerConfig)
	if err != nil {
		return fmt.Errorf("create consumer: %w", err)
	}

	// Create or get KV bucket for context responses
	responseBucket, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      c.config.ResponseBucketName,
		Description: "Context build responses for UI queries",
		TTL:         time.Duration(c.config.ResponseTTLHours) * time.Hour,
	})
	if err != nil {
		return fmt.Errorf("create response bucket: %w", err)
	}

	// Create cancellation context
	subCtx, cancel := context.WithCancel(ctx)

	// Update state atomically with lock for complex state
	c.mu.Lock()
	c.stream = stream
	c.consumer = consumer
	c.responseBucket = responseBucket
	c.cancel = cancel
	c.startTime = time.Now()
	c.mu.Unlock()

	// Transition to running
	c.state.Store(stateRunning)

	// Start consuming messages
	go c.consumeLoop(subCtx)

	c.logger.Info("context-builder started",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"subject", c.config.InputSubjectPattern)

	return nil
}

// consumeLoop continuously consumes messages from the JetStream consumer.
func (c *Component) consumeLoop(ctx context.Context) {
	for {
		// Check context first
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Verify we're still running
		if c.state.Load() != stateRunning {
			return
		}

		// Fetch messages with a timeout
		msgs, err := c.consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.logger.Debug("Fetch timeout or error", "error", err)
			continue
		}

		for msg := range msgs.Messages() {
			// Check context before processing each message to prevent goroutine leak
			select {
			case <-ctx.Done():
				// NAK the message so it can be redelivered
				if err := msg.Nak(); err != nil {
					c.logger.Warn("Failed to NAK message during shutdown", "error", err)
				}
				return
			default:
				c.handleMessage(ctx, msg)
			}
		}

		if msgs.Error() != nil && msgs.Error() != context.DeadlineExceeded {
			c.logger.Warn("Message fetch error", "error", msgs.Error())
		}
	}
}

// handleMessage processes a single context build request.
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	// Check for context cancellation before expensive operations
	if ctx.Err() != nil {
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message during shutdown", "error", err)
		}
		return
	}

	c.requestsProcessed.Add(1)
	c.updateLastActivity()

	// Parse the request
	var baseMsg message.BaseMessage
	if err := json.Unmarshal(msg.Data(), &baseMsg); err != nil {
		c.logger.Error("Failed to parse message", "error", err)
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}

	// Extract request payload
	var req ContextBuildRequest
	payloadBytes, err := json.Marshal(baseMsg.Payload())
	if err != nil {
		c.logger.Error("Failed to marshal payload", "error", err)
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}
	if err := json.Unmarshal(payloadBytes, &req); err != nil {
		c.logger.Error("Failed to unmarshal request", "error", err)
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}

	c.logger.Info("Processing context build request",
		"request_id", req.RequestID,
		"task_type", req.TaskType)

	// Validate request
	if err := ValidateRequest(&req); err != nil {
		c.logger.Error("Invalid request", "error", err)
		c.requestsFailed.Add(1)
		c.publishErrorResponse(ctx, &req, fmt.Sprintf("invalid request: %v", err))
		// ACK invalid requests - they won't succeed on retry
		if err := msg.Ack(); err != nil {
			c.logger.Warn("Failed to ACK message", "error", err)
		}
		return
	}

	// Build context
	response, err := c.builder.Build(ctx, &req)
	if err != nil {
		c.requestsFailed.Add(1)
		c.logger.Error("Failed to build context",
			"request_id", req.RequestID,
			"error", err)
		// NAK transient errors for retry
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}

	// Check for build error in response (non-transient, e.g., SOPs don't fit budget)
	if response.Error != "" {
		c.requestsFailed.Add(1)
		c.logger.Warn("Context build returned error",
			"request_id", req.RequestID,
			"error", response.Error)
		// ACK non-transient errors - retrying won't help
	}

	// Publish response
	if err := c.publishResponse(ctx, response); err != nil {
		c.logger.Error("Failed to publish response",
			"request_id", req.RequestID,
			"error", err)
		// NAK if we couldn't publish - should retry
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}

	// ACK the message
	if err := msg.Ack(); err != nil {
		c.logger.Warn("Failed to ACK message", "error", err)
	}

	c.logger.Info("Context built and published",
		"request_id", req.RequestID,
		"tokens_used", response.TokensUsed,
		"tokens_budget", response.TokensBudget)
}

// publishResponse publishes a context build response and stores it in KV for queries.
func (c *Component) publishResponse(ctx context.Context, response *ContextBuildResponse) error {
	baseMsg := message.NewBaseMessage(
		message.Type{Domain: "context", Category: "response", Version: "v1"},
		response,
		"context-builder",
	)

	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}

	subject := fmt.Sprintf("%s.%s", c.config.OutputSubjectPrefix, response.RequestID)
	if err := c.natsClient.Publish(ctx, subject, data); err != nil {
		return fmt.Errorf("publish response: %w", err)
	}

	// Store response in KV bucket for HTTP queries
	if err := c.storeContextResponse(ctx, response); err != nil {
		// Log but don't fail the request - KV storage is secondary
		c.logger.Warn("Failed to store context response in KV",
			"request_id", response.RequestID,
			"error", err)
	}

	return nil
}

// storeContextResponse persists a context response to the KV bucket for HTTP queries.
func (c *Component) storeContextResponse(ctx context.Context, response *ContextBuildResponse) error {
	c.mu.RLock()
	bucket := c.responseBucket
	c.mu.RUnlock()

	if bucket == nil {
		return fmt.Errorf("response bucket not initialized")
	}

	data, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}

	_, err = bucket.Put(ctx, response.RequestID, data)
	return err
}

// publishErrorResponse publishes an error response.
func (c *Component) publishErrorResponse(ctx context.Context, req *ContextBuildRequest, errMsg string) {
	response := &ContextBuildResponse{
		RequestID: req.RequestID,
		TaskType:  req.TaskType,
		Error:     errMsg,
	}

	if err := c.publishResponse(ctx, response); err != nil {
		c.logger.Error("Failed to publish error response",
			"request_id", req.RequestID,
			"error", err)
	}
}

// Stop gracefully stops the component.
func (c *Component) Stop(_ time.Duration) error {
	// Atomically transition from running to stopping
	if !c.state.CompareAndSwap(stateRunning, stateStopping) {
		currentState := c.state.Load()
		if currentState == stateStopped {
			return nil // Already stopped
		}
		if currentState == stateStopping {
			return nil // Already stopping
		}
		return fmt.Errorf("component in unexpected state: %d", currentState)
	}

	// Get and clear cancel function
	c.mu.Lock()
	cancel := c.cancel
	c.cancel = nil
	c.mu.Unlock()

	// Cancel context to stop consume loop
	if cancel != nil {
		cancel()
	}

	// Transition to stopped
	c.state.Store(stateStopped)

	c.logger.Info("context-builder stopped",
		"requests_processed", c.requestsProcessed.Load(),
		"requests_failed", c.requestsFailed.Load())

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "context-builder",
		Type:        "processor",
		Description: "Builds relevant context for workflow tasks based on task type and token budget",
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
		ports[i] = component.Port{
			Name:        portDef.Name,
			Direction:   component.DirectionInput,
			Required:    portDef.Required,
			Description: portDef.Description,
			Config: component.NATSPort{
				Subject: portDef.Subject,
			},
		}
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

// ConfigSchema returns the configuration schema.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return contextBuilderSchema
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
		Healthy:    running,
		LastCheck:  time.Now(),
		ErrorCount: int(c.requestsFailed.Load()),
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
