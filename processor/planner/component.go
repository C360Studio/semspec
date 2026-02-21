// Package planner provides a processor that generates Goal/Context/Scope
// for plans using LLM based on the plan title and codebase analysis.
package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/model"
	contextbuilder "github.com/c360studio/semspec/processor/context-builder"
	"github.com/c360studio/semspec/processor/contexthelper"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/prompts"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// maxFormatRetries is the total number of LLM call attempts when the response
// isn't valid JSON. On each retry, the parse error is fed back to the LLM as a
// correction prompt so it can fix the output format.
const maxFormatRetries = 5

// llmCompleter is the subset of the LLM client used by the planner.
// Extracted as an interface to enable testing with mock responses.
type llmCompleter interface {
	Complete(ctx context.Context, req llm.Request) (*llm.Response, error)
}

// Component implements the planner processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	llmClient llmCompleter

	// Centralized context building via context-builder
	contextHelper *contexthelper.Helper

	// JetStream consumer
	consumer jetstream.Consumer
	stream   jetstream.Stream

	// Lifecycle
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics
	triggersProcessed atomic.Int64
	plansGenerated    atomic.Int64
	generationsFailed atomic.Int64
	lastActivityMu    sync.RWMutex
	lastActivity      time.Time
}

// NewComponent creates a new planner processor.
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
	if config.TriggerSubject == "" {
		config.TriggerSubject = defaults.TriggerSubject
	}
	if config.DefaultCapability == "" {
		config.DefaultCapability = defaults.DefaultCapability
	}
	if config.ContextSubjectPrefix == "" {
		config.ContextSubjectPrefix = defaults.ContextSubjectPrefix
	}
	if config.ContextResponseBucket == "" {
		config.ContextResponseBucket = defaults.ContextResponseBucket
	}
	if config.ContextTimeout == "" {
		config.ContextTimeout = defaults.ContextTimeout
	}
	if config.Ports == nil {
		config.Ports = defaults.Ports
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	logger := deps.GetLogger()

	// Initialize context helper for centralized context building
	ctxHelper := contexthelper.New(deps.NATSClient, contexthelper.Config{
		SubjectPrefix:  config.ContextSubjectPrefix,
		ResponseBucket: config.ContextResponseBucket,
		Timeout:        config.GetContextTimeout(),
		SourceName:     "planner",
	}, logger)

	return &Component{
		name:       "planner",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     logger,
		llmClient: llm.NewClient(model.Global(),
			llm.WithLogger(logger),
			llm.WithCallStore(llm.GlobalCallStore()),
		),
		contextHelper: ctxHelper,
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized planner",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"trigger_subject", c.config.TriggerSubject)
	return nil
}

// Start begins processing planner triggers.
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

	// Get JetStream context
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("get jetstream: %w", err)
	}

	// Get stream
	stream, err := js.Stream(subCtx, c.config.StreamName)
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("get stream %s: %w", c.config.StreamName, err)
	}
	c.stream = stream

	// Create or get consumer
	consumerConfig := jetstream.ConsumerConfig{
		Durable:       c.config.ConsumerName,
		FilterSubject: c.config.TriggerSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       180 * time.Second, // Allow time for LLM
		MaxDeliver:    3,
	}

	consumer, err := stream.CreateOrUpdateConsumer(subCtx, consumerConfig)
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("create consumer: %w", err)
	}
	c.consumer = consumer

	// Start consuming messages
	go c.consumeLoop(subCtx)

	c.logger.Info("planner started",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"subject", c.config.TriggerSubject)

	return nil
}

func (c *Component) rollbackStart(cancel context.CancelFunc) {
	c.mu.Lock()
	c.running = false
	c.cancel = nil
	c.mu.Unlock()
	cancel()
}

// consumeLoop continuously consumes messages from the JetStream consumer.
func (c *Component) consumeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
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
			c.handleMessage(ctx, msg)
		}

		if msgs.Error() != nil && msgs.Error() != context.DeadlineExceeded {
			c.logger.Warn("Message fetch error", "error", msgs.Error())
		}
	}
}

// handleMessage processes a single planner trigger.
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	// Check for context cancellation before expensive operations
	if ctx.Err() != nil {
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message during shutdown", "error", err)
		}
		return
	}

	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	// Parse the trigger (handles both BaseMessage-wrapped and raw JSON from
	// workflow-processor publish_async)
	trigger, err := workflow.ParseNATSMessage[workflow.TriggerPayload](msg.Data())
	if err != nil {
		c.logger.Error("Failed to parse trigger", "error", err)
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}

	c.logger.Info("Processing planner trigger",
		"request_id", trigger.RequestID,
		"slug", trigger.Data.Slug,
		"workflow_id", trigger.WorkflowID,
		"trace_id", trigger.TraceID)

	// Inject trace context for LLM call tracking
	llmCtx := ctx
	if trigger.TraceID != "" || trigger.LoopID != "" {
		llmCtx = llm.WithTraceContext(ctx, llm.TraceContext{
			TraceID: trigger.TraceID,
			LoopID:  trigger.LoopID,
		})
	}

	// Generate plan content using LLM
	planContent, err := c.generatePlan(llmCtx, trigger)
	if err != nil {
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to generate plan",
			"request_id", trigger.RequestID,
			"slug", trigger.Data.Slug,
			"error", err)
		// If workflow-dispatched, publish failure callback so the workflow can handle it
		if trigger.HasCallback() {
			if cbErr := trigger.PublishCallbackFailure(ctx, c.natsClient, err.Error()); cbErr != nil {
				c.logger.Error("Failed to publish failure callback", "error", cbErr)
			}
			if err := msg.Ack(); err != nil {
				c.logger.Warn("Failed to ACK message", "error", err)
			}
			return
		}
		// Legacy: NAK for retry
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}

	// Save plan to file
	if err := c.savePlan(ctx, trigger, planContent); err != nil {
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to save plan",
			"request_id", trigger.RequestID,
			"slug", trigger.Data.Slug,
			"error", err)
		// If workflow-dispatched, publish failure callback
		if trigger.HasCallback() {
			if cbErr := trigger.PublishCallbackFailure(ctx, c.natsClient, err.Error()); cbErr != nil {
				c.logger.Error("Failed to publish failure callback", "error", cbErr)
			}
			if err := msg.Ack(); err != nil {
				c.logger.Warn("Failed to ACK message", "error", err)
			}
			return
		}
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}

	// Publish success notification
	if err := c.publishResult(ctx, trigger, planContent); err != nil {
		c.logger.Warn("Failed to publish result notification",
			"request_id", trigger.RequestID,
			"slug", trigger.Data.Slug,
			"error", err)
		// Don't fail - plan was saved successfully
	}

	c.plansGenerated.Add(1)

	// ACK the message
	if err := msg.Ack(); err != nil {
		c.logger.Warn("Failed to ACK message", "error", err)
	}

	c.logger.Info("Plan generated successfully",
		"request_id", trigger.RequestID,
		"slug", trigger.Data.Slug)
}

// PlanContent holds the LLM-generated plan fields.
type PlanContent struct {
	Goal    string `json:"goal"`
	Context string `json:"context"`
	Scope   struct {
		Include    []string `json:"include,omitempty"`
		Exclude    []string `json:"exclude,omitempty"`
		DoNotTouch []string `json:"do_not_touch,omitempty"`
	} `json:"scope"`
	Status string `json:"status,omitempty"`
}

// generatePlan calls the LLM to generate plan content.
// It follows the graph-first pattern by requesting context from the
// centralized context-builder before making the LLM call.
func (c *Component) generatePlan(ctx context.Context, trigger *workflow.WorkflowTriggerPayload) (*PlanContent, error) {
	// Step 1: Request planning context from centralized context-builder (graph-first)
	var graphContext string
	resp := c.contextHelper.BuildContextGraceful(ctx, &contextbuilder.ContextBuildRequest{
		TaskType: contextbuilder.TaskTypePlanning,
		Topic:    trigger.Data.Title,
	})
	if resp != nil {
		// Build context string from response
		graphContext = contexthelper.FormatContextResponse(resp)
		c.logger.Info("Built planning context via context-builder",
			"title", trigger.Data.Title,
			"entities", len(resp.Entities),
			"documents", len(resp.Documents),
			"tokens_used", resp.TokensUsed)
	} else {
		c.logger.Warn("Context build returned nil, proceeding without graph context",
			"title", trigger.Data.Title)
	}

	// Step 2: Build messages with proper system/user separation.
	// The system prompt (with JSON format) is ALWAYS included — even on
	// revision calls — because local LLMs need the format example every time.
	systemPrompt := prompts.PlannerSystemPrompt()
	var userPrompt string
	if trigger.Prompt != "" {
		// Revision or custom prompt: use it as the user message
		userPrompt = trigger.Prompt
	} else {
		// Initial plan: build from title
		userPrompt = prompts.PlannerPromptWithTitle(trigger.Data.Title)
	}
	if graphContext != "" {
		userPrompt = fmt.Sprintf("%s\n\n## Codebase Context\n\nThe following context from the knowledge graph provides information about the existing codebase structure:\n\n%s", userPrompt, graphContext)
	}

	// Step 3: Call LLM with format correction retry.
	capability := c.config.DefaultCapability
	if capability == "" {
		capability = string(model.CapabilityPlanning)
	}

	return c.generatePlanFromMessages(ctx, capability, systemPrompt, userPrompt)
}

// generatePlanFromMessages calls the LLM with format correction retry.
// If the LLM response isn't valid JSON, the parse error is fed back as a
// correction prompt so the LLM can fix the output (up to maxFormatRetries
// total attempts). The conversation history accumulates across retries.
//
// Uses system/user message separation for better results with local LLMs.
// The system prompt contains the JSON output format so every call (initial
// and revision) has clear format instructions.
func (c *Component) generatePlanFromMessages(ctx context.Context, capability, systemPrompt, userPrompt string) (*PlanContent, error) {
	temperature := 0.7
	messages := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
	var lastErr error

	for attempt := range maxFormatRetries {
		llmResp, err := c.llmClient.Complete(ctx, llm.Request{
			Capability:  capability,
			Messages:    messages,
			Temperature: &temperature,
			MaxTokens:   4096,
		})
		if err != nil {
			return nil, fmt.Errorf("LLM completion: %w", err)
		}

		c.logger.Debug("LLM response received",
			"model", llmResp.Model,
			"tokens_used", llmResp.TokensUsed,
			"attempt", attempt+1)

		planContent, parseErr := c.parsePlanFromResponse(llmResp.Content)
		if parseErr == nil {
			return planContent, nil
		}

		lastErr = parseErr

		// Don't retry on the last attempt
		if attempt+1 >= maxFormatRetries {
			break
		}

		c.logger.Warn("LLM format retry",
			"attempt", attempt+1,
			"error", parseErr)

		// Append assistant response + correction to conversation history
		messages = append(messages,
			llm.Message{Role: "assistant", Content: llmResp.Content},
			llm.Message{Role: "user", Content: formatCorrectionPrompt(parseErr)},
		)
	}

	return nil, fmt.Errorf("parse plan from response: %w", lastErr)
}

// parsePlanFromResponse extracts plan content from the LLM response.
func (c *Component) parsePlanFromResponse(content string) (*PlanContent, error) {
	// Extract JSON from the response (may be wrapped in markdown code blocks)
	jsonContent := llm.ExtractJSON(content)
	if jsonContent == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var planContent PlanContent
	if err := json.Unmarshal([]byte(jsonContent), &planContent); err != nil {
		return nil, fmt.Errorf("parse JSON: %w (content: %s)", err, jsonContent[:min(200, len(jsonContent))])
	}

	// Validate required fields
	if planContent.Goal == "" {
		return nil, fmt.Errorf("plan missing 'goal' field")
	}

	return &planContent, nil
}

// formatCorrectionPrompt builds a feedback message telling the LLM its
// previous response wasn't valid JSON and showing the expected structure.
func formatCorrectionPrompt(err error) string {
	return fmt.Sprintf(
		"Your response could not be parsed as JSON. Error: %s\n\n"+
			"Please respond with ONLY a valid JSON object matching this structure:\n"+
			"```json\n"+
			"{\n"+
			"  \"goal\": \"<what this change accomplishes>\",\n"+
			"  \"context\": \"<relevant background>\",\n"+
			"  \"scope\": {\n"+
			"    \"include\": [\"<file or directory patterns to modify>\"],\n"+
			"    \"exclude\": [\"<patterns to avoid>\"]\n"+
			"  }\n"+
			"}\n"+
			"```",
		err.Error(),
	)
}

// savePlan saves the generated plan content to the plan.json file.
func (c *Component) savePlan(ctx context.Context, trigger *workflow.WorkflowTriggerPayload, planContent *PlanContent) error {
	// Check context cancellation before filesystem operations
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled: %w", err)
	}

	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
	}

	manager := workflow.NewManager(repoRoot)

	// Load existing plan
	plan, err := manager.LoadPlan(ctx, trigger.Data.Slug)
	if err != nil {
		return fmt.Errorf("load plan: %w", err)
	}

	// Update with LLM-generated content
	plan.Goal = planContent.Goal
	plan.Context = planContent.Context
	plan.Scope = workflow.Scope{
		Include:    planContent.Scope.Include,
		Exclude:    planContent.Scope.Exclude,
		DoNotTouch: planContent.Scope.DoNotTouch,
	}

	// Save the updated plan
	return manager.SavePlan(ctx, plan)
}

// PlannerResultType is the message type for planner results.
var PlannerResultType = message.Type{Domain: "workflow", Category: "planner-result", Version: "v1"}

// Result is the result payload for plan generation.
type Result struct {
	workflow.CallbackFields

	RequestID string       `json:"request_id"`
	Slug      string       `json:"slug"`
	Content   *PlanContent `json:"content"`
	Status    string       `json:"status"`
}

// Schema implements message.Payload.
func (r *Result) Schema() message.Type {
	return PlannerResultType
}

// Validate implements message.Payload.
func (r *Result) Validate() error {
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *Result) MarshalJSON() ([]byte, error) {
	type Alias Result
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *Result) UnmarshalJSON(data []byte) error {
	type Alias Result
	return json.Unmarshal(data, (*Alias)(r))
}

// publishResult publishes a success notification for the plan generation.
// publishResult publishes a success notification for the plan generation.
// Uses the workflow-processor's async callback pattern (ADR-005 Phase 6).
func (c *Component) publishResult(ctx context.Context, trigger *workflow.WorkflowTriggerPayload, planContent *PlanContent) error {
	result := &Result{
		RequestID: trigger.RequestID,
		Slug:      trigger.Data.Slug,
		Content:   planContent,
		Status:    "completed",
	}

	if !trigger.HasCallback() {
		c.logger.Warn("No callback configured for planner result",
			"slug", trigger.Data.Slug,
			"request_id", trigger.RequestID)
		return nil
	}

	if err := trigger.PublishCallbackSuccess(ctx, c.natsClient, result); err != nil {
		return fmt.Errorf("publish callback: %w", err)
	}
	c.logger.Info("Published planner callback result",
		"slug", trigger.Data.Slug,
		"task_id", trigger.TaskID,
		"callback", trigger.CallbackSubject)
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

	c.logger.Info("planner stopped",
		"triggers_processed", c.triggersProcessed.Load(),
		"plans_generated", c.plansGenerated.Load(),
		"generations_failed", c.generationsFailed.Load())

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "planner",
		Type:        "processor",
		Description: "Generates Goal/Context/Scope for plans using LLM",
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
	return plannerSchema
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
		ErrorCount: int(c.generationsFailed.Load()),
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
