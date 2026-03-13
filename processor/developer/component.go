// Package developer provides a JetStream processor that bridges LLM development
// to reactive workflow state. It consumes DeveloperRequest messages, invokes the
// LLM client with tool support, and updates the WORKFLOWS KV bucket to advance
// the reactive workflow.
package developer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/prompt"
	promptdomain "github.com/c360studio/semspec/prompt/domain"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semspec/workflow/phases"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/google/uuid"
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
	llmClient     llmCompleter
	modelRegistry *model.Registry
	assembler     *prompt.Assembler

	// JetStream context and consumer state.
	js       jetstream.JetStream
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
	toolCallsExecuted   atomic.Int64
	lastActivityMu      sync.RWMutex
	lastActivity        time.Time
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
	if config.MaxToolIterations == 0 {
		config.MaxToolIterations = defaults.MaxToolIterations
	}
	if config.Ports == nil {
		config.Ports = defaults.Ports
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	logger := deps.GetLogger()

	// Initialize prompt assembler with software domain
	registry := prompt.NewRegistry()
	registry.RegisterAll(promptdomain.Software()...)
	registry.Register(prompt.ToolGuidanceFragment(prompt.DefaultToolGuidance()))
	assembler := prompt.NewAssembler(registry)

	return &Component{
		name:          "developer",
		config:        config,
		natsClient:    deps.NATSClient,
		logger:        logger,
		modelRegistry: model.Global(),
		assembler:     assembler,
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
		"trigger_subject", c.config.TriggerSubject,
		"max_tool_iterations", c.config.MaxToolIterations)
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
	c.js = js

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
		// Allow generous ack wait since LLM calls with tools can be slow.
		AckWait:    c.config.GetTimeout() + 60*time.Second,
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
	req, err := payloads.ParseReactivePayload[payloads.DeveloperRequest](msg.Data())
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

	// Signal in-progress to prevent redelivery during long LLM operations.
	// This resets the AckWait timer and prevents JetStream from redelivering
	// the message while we're still processing (which causes race conditions
	// when multiple instances process the same message concurrently).
	progressFn := func() {
		if err := msg.InProgress(); err != nil {
			c.logger.Debug("Failed to signal in-progress", "error", err)
		}
	}

	result, err := c.executeDevelopment(ctx, req, progressFn)
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
		"files_modified", len(result.FilesModified),
		"tool_calls", result.ToolCallCount)
}

// developerOutput holds the output from development execution.
// This is an internal type for development execution output.
type developerOutput struct {
	Output        string   `json:"output"`
	FilesModified []string `json:"files_modified"`
	LLMRequestIDs []string `json:"llm_request_ids,omitempty"`
	ToolCallCount int      `json:"tool_call_count,omitempty"`
}

// executeDevelopment invokes the LLM client to perform development.
// If the model supports tools, it will execute a tool loop until completion
// or max iterations is reached.
//
// The progressFn callback is called at the start of each tool loop iteration
// to signal that processing is still in progress, preventing JetStream from
// redelivering the message during long-running LLM operations.
func (c *Component) executeDevelopment(ctx context.Context, req *payloads.DeveloperRequest, progressFn func()) (*developerOutput, error) {
	// Build prompt for the developer.
	// For revisions, the prompt already includes original task + previous response + feedback
	// (assembled by taskExecBuildDeveloperPayload in the reactive workflow).
	userPrompt := req.Prompt
	if userPrompt == "" {
		userPrompt = fmt.Sprintf("Implement the development task: %s", req.DeveloperTaskID)
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

	capability := c.config.DefaultCapability
	if capability == "" {
		capability = "coding"
	}

	// Check if we have tool-capable endpoints for this capability
	toolCapable := c.modelRegistry.GetToolCapableEndpoints(model.ParseCapability(capability))
	hasToolSupport := len(toolCapable) > 0

	// Get tool definitions if we have tool-capable endpoints
	var tools []llm.ToolDefinition
	if hasToolSupport {
		tools = c.getToolDefinitions()
	}

	// Assemble system prompt using fragment-based assembler with provider-aware formatting.
	provider := c.resolveProvider()
	assembled := c.assembler.Assemble(&prompt.AssemblyContext{
		Role:          prompt.RoleDeveloper,
		Provider:      provider,
		Domain:        "software",
		SupportsTools: hasToolSupport,
	})
	systemPrompt := assembled.SystemMessage

	c.logger.Debug("Assembled developer prompt",
		"provider", provider,
		"fragments_used", assembled.FragmentsUsed)

	// Build initial message history with system prompt followed by user task.
	messages := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
	var allFilesModified []string
	var allLLMRequestIDs []string
	totalToolCalls := 0

	// Tool execution loop
	for iteration := 0; iteration < c.config.MaxToolIterations; iteration++ {
		// Signal progress to prevent message redelivery during long iterations.
		if progressFn != nil {
			progressFn()
		}

		temperature := 0.7
		llmReq := llm.Request{
			Capability:  capability,
			Messages:    messages,
			Temperature: &temperature,
			MaxTokens:   4096,
		}

		// Only add tools if we have tool support
		if hasToolSupport && len(tools) > 0 {
			llmReq.Tools = tools
			llmReq.ToolChoice = "auto"
		}

		llmResp, err := c.llmClient.Complete(llmCtx, llmReq)
		if err != nil {
			return nil, fmt.Errorf("LLM completion (iteration %d): %w", iteration, err)
		}

		allLLMRequestIDs = append(allLLMRequestIDs, llmResp.RequestID)

		c.logger.Debug("LLM response received",
			"iteration", iteration,
			"model", llmResp.Model,
			"tokens_used", llmResp.TokensUsed,
			"tool_calls", len(llmResp.ToolCalls),
			"finish_reason", llmResp.FinishReason)

		// No tool calls - we're done
		if len(llmResp.ToolCalls) == 0 {
			result := &developerOutput{
				Output:        llmResp.Content,
				LLMRequestIDs: allLLMRequestIDs,
				FilesModified: allFilesModified,
				ToolCallCount: totalToolCalls,
			}

			// Try to parse files_modified from the response if no tools were used
			if len(allFilesModified) == 0 {
				result.FilesModified = c.extractFilesModified(llmResp.Content)
			}

			return result, nil
		}

		// Execute tool calls
		c.logger.Info("Executing tool calls",
			"iteration", iteration,
			"count", len(llmResp.ToolCalls))

		toolResults, filesModified := c.executeToolCalls(llmCtx, req.ExecutionID, llmResp.ToolCalls)
		allFilesModified = append(allFilesModified, filesModified...)
		totalToolCalls += len(llmResp.ToolCalls)

		// Add assistant message with tool calls to history
		messages = append(messages, llm.Message{
			Role:      "assistant",
			Content:   llmResp.Content,
			ToolCalls: llmResp.ToolCalls,
		})

		// Add tool result messages to history
		for callID, result := range toolResults {
			messages = append(messages, llm.Message{
				Role:       "tool",
				ToolCallID: callID,
				Content:    result,
			})
		}
	}

	return nil, fmt.Errorf("max tool iterations (%d) exceeded", c.config.MaxToolIterations)
}

// getToolDefinitions builds LLM tool definitions from registered agentic-tools.
// Filters to only include implementation tools (file_*, git_*) and excludes
// planning tools (workflow_*) to avoid confusing the model.
func (c *Component) getToolDefinitions() []llm.ToolDefinition {
	// Get all globally registered tool definitions
	agenticDefs := agentictools.ListRegisteredTools()

	// Deduplicate by tool name and filter to implementation tools only
	seen := make(map[string]bool)
	var tools []llm.ToolDefinition

	for _, def := range agenticDefs {
		if seen[def.Name] {
			continue
		}

		// Only include implementation tools (file_*, git_*)
		// Exclude workflow_* tools which are for planning, not implementation
		if !strings.HasPrefix(def.Name, "file_") &&
			!strings.HasPrefix(def.Name, "git_") {
			continue
		}

		seen[def.Name] = true
		tools = append(tools, llm.ToolDefinition{
			Name:        def.Name,
			Description: def.Description,
			Parameters:  def.Parameters,
		})
	}

	return tools
}

// executeToolCalls executes tool calls via the agentic-tools pub/sub flow.
// Returns a map of call_id -> result content, and a list of files modified.
func (c *Component) executeToolCalls(ctx context.Context, _ string, calls []llm.ToolCall) (map[string]string, []string) {
	results := make(map[string]string)
	var filesModified []string

	for _, tc := range calls {
		c.toolCallsExecuted.Add(1)

		result, err := c.executeToolCall(ctx, tc)
		if err != nil {
			c.logger.Warn("Tool call failed",
				"tool", tc.Name,
				"call_id", tc.ID,
				"error", err)

			// Provide instructive feedback for tool-not-found errors
			if strings.Contains(err.Error(), "not found") {
				results[tc.ID] = c.buildToolNotFoundError(tc.Name)
			} else {
				results[tc.ID] = fmt.Sprintf("Error: %s", err.Error())
			}
			continue
		}

		results[tc.ID] = result

		// Track file modifications
		if tc.Name == "file_write" {
			if path, ok := tc.Arguments["path"].(string); ok && path != "" {
				filesModified = append(filesModified, path)
			}
		}
	}

	return results, filesModified
}

// buildToolNotFoundError creates an instructive error message when a tool doesn't exist.
// This helps the LLM understand it should not retry the same invalid tool and shows
// what tools are actually available.
func (c *Component) buildToolNotFoundError(toolName string) string {
	// Get the list of available tools
	availableTools := c.getToolDefinitions()
	var toolNames []string
	for _, t := range availableTools {
		toolNames = append(toolNames, t.Name)
	}

	return fmt.Sprintf(
		"Error: tool %q does not exist. Do not attempt to call this tool again. "+
			"Available tools are: %s",
		toolName,
		strings.Join(toolNames, ", "),
	)
}

// executeToolCall publishes a tool execution request and waits for the result.
func (c *Component) executeToolCall(ctx context.Context, tc llm.ToolCall) (string, error) {
	// Extract trace context for correlation
	traceCtx := llm.GetTraceContext(ctx)

	// Convert llm.ToolCall to agentic.ToolCall with trace info
	agenticCall := &agentic.ToolCall{
		ID:        tc.ID,
		Name:      tc.Name,
		Arguments: tc.Arguments,
		LoopID:    traceCtx.LoopID,
		TraceID:   traceCtx.TraceID,
	}

	// Ensure call has an ID
	if agenticCall.ID == "" {
		agenticCall.ID = uuid.New().String()
	}

	// Create BaseMessage wrapper
	baseMsg := message.NewBaseMessage(agenticCall.Schema(), agenticCall, "developer")

	// Marshal message
	msgData, err := json.Marshal(baseMsg)
	if err != nil {
		return "", fmt.Errorf("marshal tool call: %w", err)
	}

	// Create a unique inbox subject for this call
	resultSubject := fmt.Sprintf("tool.result.%s", agenticCall.ID)

	// Subscribe to result before publishing to avoid race
	sub, err := c.js.CreateConsumer(ctx, c.config.StreamName, jetstream.ConsumerConfig{
		FilterSubject: resultSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverNewPolicy,
	})
	if err != nil {
		return "", fmt.Errorf("create result consumer: %w", err)
	}

	// Publish tool execution request
	toolSubject := fmt.Sprintf("tool.execute.%s", tc.Name)
	if _, err := c.js.Publish(ctx, toolSubject, msgData); err != nil {
		return "", fmt.Errorf("publish tool call: %w", err)
	}

	c.logger.Debug("Published tool call",
		"tool", tc.Name,
		"call_id", agenticCall.ID,
		"subject", toolSubject,
		"trace_id", agenticCall.TraceID,
		"loop_id", agenticCall.LoopID)

	// Wait for result with timeout
	resultCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	msgs, err := sub.Fetch(1, jetstream.FetchMaxWait(30*time.Second))
	if err != nil {
		return "", fmt.Errorf("fetch tool result: %w", err)
	}

	for msg := range msgs.Messages() {
		// Parse result - BaseMessage.UnmarshalJSON uses the payload registry
		// to create a typed *agentic.ToolResult from the registered factory
		var baseResult message.BaseMessage
		if err := json.Unmarshal(msg.Data(), &baseResult); err != nil {
			if ackErr := msg.Ack(); ackErr != nil {
				c.logger.Warn("Failed to ACK result message", "error", ackErr)
			}
			return "", fmt.Errorf("unmarshal result: %w", err)
		}

		// Type-assert the payload - registry already created the typed instance
		toolResult, ok := baseResult.Payload().(*agentic.ToolResult)
		if !ok {
			if ackErr := msg.Ack(); ackErr != nil {
				c.logger.Warn("Failed to ACK result message", "error", ackErr)
			}
			return "", fmt.Errorf("expected *agentic.ToolResult, got %T", baseResult.Payload())
		}

		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Warn("Failed to ACK result message", "error", ackErr)
		}

		c.logger.Debug("Received tool result",
			"tool", tc.Name,
			"call_id", agenticCall.ID,
			"has_error", toolResult.Error != "")

		if toolResult.Error != "" {
			return "", fmt.Errorf("tool error: %s", toolResult.Error)
		}

		return toolResult.Content, nil
	}

	if resultCtx.Err() != nil {
		return "", fmt.Errorf("tool call timeout")
	}

	return "", fmt.Errorf("no result received")
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
// TODO(migration): Phase N will replace this — state will be entity triples in ENTITY_STATES.
func (c *Component) transitionToFailure(_ context.Context, executionID string, cause string) error {
	c.logger.Warn("transitionToFailure: state management pending migration",
		"execution_id", executionID,
		"phase", phases.TaskExecDeveloperFailed,
		"cause", cause)
	return nil
}

// updateWorkflowState logs development completion for observability.
// TODO(migration): Phase N will replace this — state will be entity triples in ENTITY_STATES.
func (c *Component) updateWorkflowState(_ context.Context, req *payloads.DeveloperRequest, result *developerOutput) error {
	if req.ExecutionID == "" {
		c.logger.Debug("No ExecutionID - skipping workflow state update",
			"slug", req.Slug)
		return nil
	}
	c.logger.Info("Development complete; state update pending migration",
		"slug", req.Slug,
		"execution_id", req.ExecutionID,
		"phase", phases.TaskExecDeveloped,
		"files_modified", len(result.FilesModified),
		"tool_calls", result.ToolCallCount)
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
		"developments_failed", c.developmentsFailed.Load(),
		"tool_calls_executed", c.toolCallsExecuted.Load())

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "developer",
		Type:        "processor",
		Description: "Bridges LLM development with tool support to reactive workflow state",
		Version:     "0.2.0",
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

// resolveProvider determines the LLM provider for prompt formatting.
func (c *Component) resolveProvider() prompt.Provider {
	capability := c.config.DefaultCapability
	if capability == "" {
		capability = string(model.CapabilityCoding)
	}
	modelName := c.modelRegistry.Resolve(model.Capability(capability))
	if endpoint := c.modelRegistry.GetEndpoint(modelName); endpoint != nil {
		return prompt.Provider(endpoint.Provider)
	}
	return prompt.ProviderOllama
}
