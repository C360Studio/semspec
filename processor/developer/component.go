// Package developer provides a JetStream processor that bridges LLM development
// to reactive workflow state. It consumes DeveloperRequest messages, invokes the
// LLM client, and updates the WORKFLOWS KV bucket to advance the reactive workflow.
package developer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/workflow/phases"
	"github.com/c360studio/semspec/workflow/reactive"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	semstreamsWorkflow "github.com/c360studio/semstreams/pkg/workflow"
	"github.com/nats-io/nats.go/jetstream"
)

// llmCompleter is the subset of the LLM client used by the developer.
// Extracted as an interface to enable testing with mock responses.
type llmCompleter interface {
	Complete(ctx context.Context, req llm.Request) (*llm.Response, error)
}

// Component implements the developer processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger
	llmClient  llmCompleter

	// JetStream consumer state.
	consumer jetstream.Consumer

	// KV bucket for workflow state (reactive engine state).
	stateBucket jetstream.KeyValue

	// Lifecycle.
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics.
	triggersProcessed   atomic.Int64
	developmentsSuccess atomic.Int64
	developmentsFailed  atomic.Int64
	lastActivityMu      sync.RWMutex
	lastActivity        time.Time
}

// ---------------------------------------------------------------------------
// Participant interface
// ---------------------------------------------------------------------------

// Compile-time check that Component implements Participant interface.
var _ semstreamsWorkflow.Participant = (*Component)(nil)

// WorkflowID returns the workflow this component participates in.
func (c *Component) WorkflowID() string {
	return reactive.TaskExecutionLoopWorkflowID
}

// Phase returns the phase name this component represents.
func (c *Component) Phase() string {
	return phases.TaskExecDeveloped
}

// StateManager returns nil - this component updates state directly via KV bucket.
// The reactive engine manages state; we just update it on completion.
func (c *Component) StateManager() *semstreamsWorkflow.StateManager {
	return nil
}

// NewComponent constructs a developer Component from raw JSON config
// and semstreams dependencies.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Apply defaults for any unset fields.
	defaults := DefaultConfig()
	if config.StreamName == "" {
		config.StreamName = defaults.StreamName
	}
	if config.ConsumerName == "" {
		config.ConsumerName = defaults.ConsumerName
	}
	if config.TriggerSubject == "" {
		config.TriggerSubject = defaults.TriggerSubject
	}
	if config.StateBucket == "" {
		config.StateBucket = defaults.StateBucket
	}
	if config.DefaultCapability == "" {
		config.DefaultCapability = defaults.DefaultCapability
	}
	if config.Timeout == "" {
		config.Timeout = defaults.Timeout
	}
	if config.Ports == nil {
		config.Ports = defaults.Ports
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	logger := deps.GetLogger()

	return &Component{
		name:       "developer",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     logger,
		llmClient: llm.NewClient(model.Global(),
			llm.WithLogger(logger),
			llm.WithCallStore(llm.GlobalCallStore()),
		),
	}, nil
}

// Initialize prepares the component for startup.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized developer",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"trigger_subject", c.config.TriggerSubject)
	return nil
}

// Start begins consuming DeveloperRequest messages from JetStream.
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

	subCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.mu.Unlock()

	js, err := c.natsClient.JetStream()
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("get jetstream: %w", err)
	}

	stream, err := js.Stream(subCtx, c.config.StreamName)
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("get stream %s: %w", c.config.StreamName, err)
	}

	// Get or create workflow state bucket
	stateBucket, err := js.KeyValue(subCtx, c.config.StateBucket)
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("get state bucket %s: %w", c.config.StateBucket, err)
	}
	c.stateBucket = stateBucket

	triggerSubject := c.config.TriggerSubject
	if c.config.Ports != nil && len(c.config.Ports.Inputs) > 0 {
		triggerSubject = c.config.Ports.Inputs[0].Subject
	}

	consumerConfig := jetstream.ConsumerConfig{
		Durable:       c.config.ConsumerName,
		FilterSubject: triggerSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		// Allow generous ack wait since LLM calls can be slow.
		AckWait:    c.config.GetTimeout() + 30*time.Second,
		MaxDeliver: 3,
	}

	consumer, err := stream.CreateOrUpdateConsumer(subCtx, consumerConfig)
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("create consumer: %w", err)
	}
	c.consumer = consumer

	go c.consumeLoop(subCtx)

	c.logger.Info("developer started",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"subject", triggerSubject)

	return nil
}

func (c *Component) rollbackStart(cancel context.CancelFunc) {
	c.mu.Lock()
	c.running = false
	c.cancel = nil
	c.mu.Unlock()
	cancel()
}

// consumeLoop fetches messages from the JetStream consumer in a tight loop
// until the context is cancelled.
func (c *Component) consumeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msgs, err := c.consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.logger.Debug("Fetch timeout or error", "error", err)
			continue
		}

		for msg := range msgs.Messages() {
			c.handleMessage(ctx, msg)
		}

		if msgs.Error() != nil && msgs.Error() != context.DeadlineExceeded {
			c.logger.Warn("Message fetch error", "error", msgs.Error())
		}
	}
}

// handleMessage processes a single DeveloperRequest message.
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	// Parse the trigger using the reactive engine's BaseMessage format.
	req, err := reactive.ParseReactivePayload[reactive.DeveloperRequest](msg.Data())
	if err != nil {
		c.developmentsFailed.Add(1)
		c.logger.Error("Failed to parse trigger", "error", err)
		if nakErr := msg.Nak(); nakErr != nil {
			c.logger.Warn("Failed to NAK message", "error", nakErr)
		}
		return
	}

	if err := req.Validate(); err != nil {
		c.logger.Error("Invalid trigger", "error", err)
		// ACK invalid messages — they will not succeed on retry.
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Warn("Failed to ACK invalid message", "error", ackErr)
		}
		return
	}

	c.logger.Info("Processing developer request",
		"slug", req.Slug,
		"task_id", req.DeveloperTaskID,
		"execution_id", req.ExecutionID,
		"revision", req.Revision)

	result, err := c.executeDevelopment(ctx, req)
	if err != nil {
		c.developmentsFailed.Add(1)
		c.logger.Error("Development failed",
			"slug", req.Slug,
			"task_id", req.DeveloperTaskID,
			"error", err)

		// Transition workflow to failure state so the reactive engine can handle it
		if req.ExecutionID != "" {
			if transErr := c.transitionToFailure(ctx, req.ExecutionID, err.Error()); transErr != nil {
				c.logger.Error("Failed to transition to failure state", "error", transErr)
				// State transition failed - NAK to allow retry
				if nakErr := msg.Nak(); nakErr != nil {
					c.logger.Warn("Failed to NAK message", "error", nakErr)
				}
				return
			}
			// Only ACK if state transition succeeded
			if ackErr := msg.Ack(); ackErr != nil {
				c.logger.Warn("Failed to ACK message", "error", ackErr)
			}
			return
		}

		// Legacy path: NAK for retry
		if nakErr := msg.Nak(); nakErr != nil {
			c.logger.Warn("Failed to NAK message", "error", nakErr)
		}
		return
	}

	// Update workflow state with development results
	if err := c.updateWorkflowState(ctx, req, result); err != nil {
		c.logger.Warn("Failed to update workflow state",
			"slug", req.Slug,
			"error", err)
	}

	c.developmentsSuccess.Add(1)

	if ackErr := msg.Ack(); ackErr != nil {
		c.logger.Warn("Failed to ACK message", "error", ackErr)
	}

	c.logger.Info("Development completed",
		"slug", req.Slug,
		"task_id", req.DeveloperTaskID,
		"files_modified", len(result.FilesModified))
}

// developerOutput holds the output from development execution.
// This is an internal type - the workflow uses reactive.developerOutput for callbacks.
type developerOutput struct {
	Output        string   `json:"output"`
	FilesModified []string `json:"files_modified"`
	LLMRequestIDs []string `json:"llm_request_ids,omitempty"`
}

// executeDevelopment invokes the LLM client to perform development.
func (c *Component) executeDevelopment(ctx context.Context, req *reactive.DeveloperRequest) (*developerOutput, error) {
	// Build prompt for the developer.
	// For revisions, the prompt already includes original task + previous response + feedback
	// (assembled by taskExecBuildDeveloperPayload in the reactive workflow).
	prompt := req.Prompt
	if prompt == "" {
		prompt = fmt.Sprintf("Implement the development task: %s", req.DeveloperTaskID)
	}

	// Build LLM context with trace information and timeout
	llmCtx, cancel := context.WithTimeout(ctx, c.config.GetTimeout())
	defer cancel()

	if req.TraceID != "" || req.LoopID != "" {
		llmCtx = llm.WithTraceContext(llmCtx, llm.TraceContext{
			TraceID: req.TraceID,
			LoopID:  req.LoopID,
		})
	}

	// Call LLM
	capability := c.config.DefaultCapability
	if capability == "" {
		capability = "coding"
	}

	temperature := 0.7
	llmResp, err := c.llmClient.Complete(llmCtx, llm.Request{
		Capability:  capability,
		Messages:    []llm.Message{{Role: "user", Content: prompt}},
		Temperature: &temperature,
		MaxTokens:   4096,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM completion: %w", err)
	}

	c.logger.Debug("LLM response received",
		"model", llmResp.Model,
		"tokens_used", llmResp.TokensUsed)

	// Parse the response to extract files_modified
	// In mock mode, the response may contain JSON with files_modified field
	result := &developerOutput{
		Output:        llmResp.Content,
		LLMRequestIDs: []string{llmResp.RequestID},
	}

	// Try to parse files_modified from the response
	// Mock LLM responses include this in a special format
	result.FilesModified = c.extractFilesModified(llmResp.Content)

	return result, nil
}

// extractFilesModified attempts to extract a files_modified list from the LLM response.
// Mock responses include this as JSON, real responses may need different parsing.
func (c *Component) extractFilesModified(content string) []string {
	// Try to extract from JSON in the content
	var parsed struct {
		FilesModified []string `json:"files_modified"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err == nil && len(parsed.FilesModified) > 0 {
		return parsed.FilesModified
	}

	// Try to find JSON block in markdown
	jsonContent := llm.ExtractJSON(content)
	if jsonContent != "" {
		if err := json.Unmarshal([]byte(jsonContent), &parsed); err == nil && len(parsed.FilesModified) > 0 {
			return parsed.FilesModified
		}
	}

	// Default: no files modified (caller may need to determine from tool calls)
	return nil
}

// transitionToFailure transitions the workflow to the developer_failed phase.
func (c *Component) transitionToFailure(ctx context.Context, executionID string, cause string) error {
	entry, err := c.stateBucket.Get(ctx, executionID)
	if err != nil {
		return fmt.Errorf("get workflow state: %w", err)
	}

	var state reactive.TaskExecutionState
	if err := json.Unmarshal(entry.Value(), &state); err != nil {
		return fmt.Errorf("unmarshal workflow state: %w", err)
	}

	state.Phase = phases.TaskExecDeveloperFailed
	state.Error = cause
	state.UpdatedAt = time.Now()

	stateData, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	if _, err := c.stateBucket.Update(ctx, executionID, stateData, entry.Revision()); err != nil {
		return fmt.Errorf("update workflow state: %w", err)
	}

	c.logger.Info("Transitioned workflow to failure state",
		"execution_id", executionID,
		"phase", phases.TaskExecDeveloperFailed,
		"cause", cause)
	return nil
}

// updateWorkflowState updates the workflow state with development results.
// This transitions the workflow to the developed phase, which triggers
// the reactive engine to advance to the next step.
func (c *Component) updateWorkflowState(ctx context.Context, req *reactive.DeveloperRequest, result *developerOutput) error {
	// Check if this is a workflow-dispatched request (has ExecutionID)
	if req.ExecutionID == "" {
		c.logger.Debug("No ExecutionID - skipping workflow state update",
			"slug", req.Slug)
		return nil
	}

	// Get current state from KV
	entry, err := c.stateBucket.Get(ctx, req.ExecutionID)
	if err != nil {
		return fmt.Errorf("get workflow state %s: %w", req.ExecutionID, err)
	}

	// Deserialize the typed state
	var state reactive.TaskExecutionState
	if err := json.Unmarshal(entry.Value(), &state); err != nil {
		return fmt.Errorf("unmarshal workflow state: %w", err)
	}

	// Update state with results
	state.FilesModified = result.FilesModified
	if result.Output != "" {
		outputJSON, err := json.Marshal(result.Output)
		if err != nil {
			return fmt.Errorf("marshal developer output: %w", err)
		}
		state.DeveloperOutput = outputJSON
	}
	state.LLMRequestIDs = append(state.LLMRequestIDs, result.LLMRequestIDs...)
	state.Phase = phases.TaskExecDeveloped
	state.UpdatedAt = time.Now()

	// Write back to KV
	stateData, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal updated state: %w", err)
	}

	if _, err := c.stateBucket.Update(ctx, req.ExecutionID, stateData, entry.Revision()); err != nil {
		return fmt.Errorf("update workflow state: %w", err)
	}

	c.logger.Info("Updated workflow state with development result",
		"slug", req.Slug,
		"execution_id", req.ExecutionID,
		"phase", phases.TaskExecDeveloped,
		"files_modified", len(result.FilesModified))
	return nil
}

// Stop gracefully stops the component.
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil
	}

	// Copy cancel function and clear state before releasing lock
	cancel := c.cancel
	c.running = false
	c.cancel = nil
	c.mu.Unlock()

	// Cancel context after releasing lock to avoid potential deadlock
	if cancel != nil {
		cancel()
	}

	c.logger.Info("developer stopped",
		"triggers_processed", c.triggersProcessed.Load(),
		"developments_success", c.developmentsSuccess.Load(),
		"developments_failed", c.developmentsFailed.Load())

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "developer",
		Type:        "processor",
		Description: "Bridges LLM development to reactive workflow state",
		Version:     "0.1.0",
	}
}

// InputPorts returns the configured input port definitions.
func (c *Component) InputPorts() []component.Port {
	if c.config.Ports == nil {
		return []component.Port{}
	}
	ports := make([]component.Port, len(c.config.Ports.Inputs))
	for i, def := range c.config.Ports.Inputs {
		ports[i] = component.Port{
			Name:        def.Name,
			Direction:   component.DirectionInput,
			Required:    def.Required,
			Description: def.Description,
			Config:      component.NATSPort{Subject: def.Subject},
		}
	}
	return ports
}

// OutputPorts returns the configured output port definitions.
func (c *Component) OutputPorts() []component.Port {
	if c.config.Ports == nil {
		return []component.Port{}
	}
	ports := make([]component.Port, len(c.config.Ports.Outputs))
	for i, def := range c.config.Ports.Outputs {
		ports[i] = component.Port{
			Name:        def.Name,
			Direction:   component.DirectionOutput,
			Required:    def.Required,
			Description: def.Description,
			Config:      component.NATSPort{Subject: def.Subject},
		}
	}
	return ports
}

// ConfigSchema returns the configuration schema.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return developerSchema
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
		ErrorCount: int(c.developmentsFailed.Load()),
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
