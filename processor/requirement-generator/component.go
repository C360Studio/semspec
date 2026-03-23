// Package requirementgenerator provides a processor that decomposes approved plans
// into structured Requirements using LLM. Each Requirement captures a single
// behavioral intent that can later be refined into BDD scenarios.
package requirementgenerator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// maxFormatRetries is the total number of LLM call attempts when the response
// isn't valid JSON. On each retry, the parse error is fed back to the LLM as a
// correction prompt so it can fix the output format.
const maxFormatRetries = 3

// llmCompleter is the subset of the LLM client used by the requirement-generator.
// Extracted as an interface to enable testing with mock responses.
type llmCompleter interface {
	Complete(ctx context.Context, req llm.Request) (*llm.Response, error)
}

// RequirementsGeneratedType is the message type for requirements-generated events.
// This matches the type consumed by plan-api's dispatchCascadeEvent handler.
var RequirementsGeneratedType = message.Type{
	Domain:   "workflow",
	Category: "requirements-generated",
	Version:  "v1",
}

// requirementsGeneratedPayload wraps workflow.RequirementsGeneratedEvent to satisfy
// the message.Payload interface required by message.NewBaseMessage.
// The JSON layout is identical to RequirementsGeneratedEvent so plan-api's
// ParseReactivePayload[workflow.RequirementsGeneratedEvent] can deserialise it.
type requirementsGeneratedPayload struct {
	Slug             string `json:"slug"`
	RequirementCount int    `json:"requirement_count"`
	TraceID          string `json:"trace_id,omitempty"`
}

func (p *requirementsGeneratedPayload) Schema() message.Type {
	return RequirementsGeneratedType
}

func (p *requirementsGeneratedPayload) Validate() error {
	if p.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

func (p *requirementsGeneratedPayload) MarshalJSON() ([]byte, error) {
	type Alias requirementsGeneratedPayload
	return json.Marshal((*Alias)(p))
}

func (p *requirementsGeneratedPayload) UnmarshalJSON(data []byte) error {
	type Alias requirementsGeneratedPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// Result is the result payload for requirement generation.
// Registered in payload_registry.go and implements message.Payload.
type Result struct {
	Slug             string `json:"slug"`
	RequirementCount int    `json:"requirement_count"`
	Status           string `json:"status"`
}

// Schema implements message.Payload.
func (r *Result) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "requirement-generator-result", Version: "v1"}
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

// requirementItem is the LLM-generated JSON shape for a single requirement.
// The LLM is instructed to output an array of these objects.
type requirementItem struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// Component implements the requirement-generator processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	llmClient llmCompleter

	// JetStream consumer
	consumer jetstream.Consumer
	stream   jetstream.Stream

	// Lifecycle
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics
	triggersProcessed     atomic.Int64
	requirementsGenerated atomic.Int64
	generationsFailed     atomic.Int64
	lastActivityMu        sync.RWMutex
	lastActivity          time.Time
}

// NewComponent creates a new requirement-generator processor.
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
	if config.Ports == nil {
		config.Ports = defaults.Ports
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	logger := deps.GetLogger()

	return &Component{
		name:       "requirement-generator",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     logger,
		llmClient: llm.NewClient(model.Global(),
			llm.WithLogger(logger),
		),
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized requirement-generator",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"trigger_subject", c.config.TriggerSubject)
	return nil
}

// Start begins processing requirement-generator triggers.
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

	// Get JetStream context.
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("get jetstream: %w", err)
	}

	// Get stream.
	stream, err := js.Stream(subCtx, c.config.StreamName)
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("get stream %s: %w", c.config.StreamName, err)
	}
	c.stream = stream

	// Create or get consumer.
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

	// Start consuming messages.
	go c.consumeLoop(subCtx)

	c.logger.Info("requirement-generator started",
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

		// Fetch messages with a timeout.
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

// handleMessage processes a single requirement-generator trigger.
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	if ctx.Err() != nil {
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message during shutdown", "error", err)
		}
		return
	}

	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	trigger, ok := c.parseTrigger(msg)
	if !ok {
		return
	}

	c.logger.Info("Processing requirement-generator trigger",
		"slug", trigger.Slug,
		"trace_id", trigger.TraceID)

	// Signal in-progress to prevent redelivery during LLM operations.
	if err := msg.InProgress(); err != nil {
		c.logger.Debug("Failed to signal in-progress", "error", err)
	}

	llmCtx := c.buildLLMContext(ctx, trigger)

	requirements, err := c.generateRequirements(llmCtx, trigger)
	if err != nil {
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to generate requirements",
			"slug", trigger.Slug, "error", err)
		// NAK for retry — requirement-generator has no reactive KV state to update.
		if nakErr := msg.Nak(); nakErr != nil {
			c.logger.Warn("Failed to NAK message", "error", nakErr)
		}
		return
	}

	if err := c.saveAndPublish(ctx, trigger, requirements); err != nil {
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to save requirements or publish event",
			"slug", trigger.Slug, "error", err)
		if nakErr := msg.Nak(); nakErr != nil {
			c.logger.Warn("Failed to NAK message", "error", nakErr)
		}
		return
	}

	c.requirementsGenerated.Add(1)

	if err := msg.Ack(); err != nil {
		c.logger.Warn("Failed to ACK message", "error", err)
	}

	c.logger.Info("Requirements generated successfully",
		"slug", trigger.Slug,
		"requirement_count", len(requirements))
}

// parseTrigger deserialises and validates the NATS message payload. It NAKs or
// ACKs the message on failure and returns false so the caller can return early.
func (c *Component) parseTrigger(msg jetstream.Msg) (*payloads.RequirementGeneratorRequest, bool) {
	trigger, err := payloads.ParseReactivePayload[payloads.RequirementGeneratorRequest](msg.Data())
	if err != nil {
		c.logger.Error("Failed to parse trigger", "error", err)
		if nakErr := msg.Nak(); nakErr != nil {
			c.logger.Warn("Failed to NAK message", "error", nakErr)
		}
		return nil, false
	}

	if err := trigger.Validate(); err != nil {
		c.logger.Error("Invalid trigger payload", "error", err)
		// ACK invalid requests — they will not succeed on retry.
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Warn("Failed to ACK invalid message", "error", ackErr)
		}
		return nil, false
	}

	return trigger, true
}

// buildLLMContext injects trace context into ctx when the trigger carries a trace ID,
// so that LLM calls are properly attributed in the trajectory store.
func (c *Component) buildLLMContext(ctx context.Context, trigger *payloads.RequirementGeneratorRequest) context.Context {
	if trigger.TraceID == "" {
		return ctx
	}
	return llm.WithTraceContext(ctx, llm.TraceContext{
		TraceID: trigger.TraceID,
	})
}

// generateRequirements calls the LLM to produce a slice of Requirement structs for the given plan.
func (c *Component) generateRequirements(ctx context.Context, trigger *payloads.RequirementGeneratorRequest) ([]workflow.Requirement, error) {
	// Load the plan to get Goal/Context/Scope for the prompt.
	repoRoot := repoRootPath()
	manager := workflow.NewManager(repoRoot)

	plan, err := manager.LoadPlan(ctx, trigger.Slug)
	if err != nil {
		return nil, fmt.Errorf("load plan %q: %w", trigger.Slug, err)
	}

	systemPrompt := requirementGeneratorSystemPrompt()
	userPrompt := requirementGeneratorUserPrompt(plan, "")

	// Partial regeneration: prepend context about approved requirements and rejection
	// reasons so the LLM generates only replacements for the rejected ones.
	if len(trigger.ReplaceRequirementIDs) > 0 {
		existing, _ := manager.LoadRequirements(ctx, trigger.Slug)

		var sb strings.Builder
		sb.WriteString("## Existing Approved Requirements (DO NOT regenerate these)\n\n")
		for _, r := range existing {
			if r.Status == workflow.RequirementStatusActive {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", r.ID, r.Title))
			}
		}
		sb.WriteString("\n## Rejected Requirements (regenerate replacements for these only)\n\n")
		for _, id := range trigger.ReplaceRequirementIDs {
			reason := trigger.RejectionReasons[id]
			if reason == "" {
				reason = "no reason provided"
			}
			sb.WriteString(fmt.Sprintf("- %s: rejected because: %s\n", id, reason))
		}
		sb.WriteString("\n")
		userPrompt = sb.String() + userPrompt
	}

	items, err := c.generateFromMessages(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("LLM requirement generation: %w", err)
	}

	// For partial regen, new requirement IDs must not collide with existing ones.
	// Determine the starting sequence offset from the current requirements count.
	seqOffset := 0
	if len(trigger.ReplaceRequirementIDs) > 0 {
		existing, _ := manager.LoadRequirements(ctx, trigger.Slug)
		seqOffset = len(existing)
	}

	// Convert LLM items to workflow.Requirement structs.
	now := time.Now()
	requirements := make([]workflow.Requirement, 0, len(items))
	for i, item := range items {
		requirements = append(requirements, workflow.Requirement{
			ID:          fmt.Sprintf("requirement.%s.%d", trigger.Slug, seqOffset+i+1),
			PlanID:      plan.ID,
			Title:       item.Title,
			Description: item.Description,
			Status:      workflow.RequirementStatusActive,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}

	return requirements, nil
}

// generateFromMessages calls the LLM with format-correction retry.
// The conversation history accumulates across retries so the model can see
// its previous (invalid) output alongside the correction instruction.
func (c *Component) generateFromMessages(ctx context.Context, systemPrompt, userPrompt string) ([]requirementItem, error) {
	temperature := 0.7
	messages := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
	var lastErr error

	for attempt := range maxFormatRetries {
		llmResp, err := c.llmClient.Complete(ctx, llm.Request{
			Capability:  c.config.DefaultCapability,
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

		items, parseErr := parseRequirementsFromResponse(llmResp.Content)
		if parseErr == nil {
			return items, nil
		}

		lastErr = parseErr

		// Don't retry on the last attempt.
		if attempt+1 >= maxFormatRetries {
			break
		}

		c.logger.Warn("LLM format retry",
			"attempt", attempt+1,
			"error", parseErr)

		// Append assistant response + correction to conversation history.
		messages = append(messages,
			llm.Message{Role: "assistant", Content: llmResp.Content},
			llm.Message{Role: "user", Content: formatCorrectionPrompt(parseErr)},
		)
	}

	return nil, fmt.Errorf("parse requirements from response: %w", lastErr)
}

// parseRequirementsFromResponse extracts a JSON array of requirement items
// from the LLM response, tolerating markdown code fences.
func parseRequirementsFromResponse(content string) ([]requirementItem, error) {
	// Try array extraction first (LLM may return [...] directly),
	// then fall back to object extraction (LLM may wrap in {requirements: [...]}).
	jsonContent := llm.ExtractJSONArray(content)
	if jsonContent == "" {
		jsonContent = llm.ExtractJSON(content)
	}
	if jsonContent == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var items []requirementItem
	if err := json.Unmarshal([]byte(jsonContent), &items); err != nil {
		// Try unwrapping from an object with a "requirements" key.
		var wrapper struct {
			Requirements []requirementItem `json:"requirements"`
		}
		if wrapErr := json.Unmarshal([]byte(jsonContent), &wrapper); wrapErr == nil && len(wrapper.Requirements) > 0 {
			items = wrapper.Requirements
		} else {
			return nil, fmt.Errorf("parse JSON array: %w (content: %s)", err, jsonContent[:min(200, len(jsonContent))])
		}
	}

	if len(items) == 0 {
		return nil, fmt.Errorf("requirements array is empty")
	}

	for i, item := range items {
		if item.Title == "" {
			return nil, fmt.Errorf("requirement[%d] missing 'title' field", i)
		}
	}

	return items, nil
}

// formatCorrectionPrompt builds a feedback message telling the LLM its
// previous response wasn't a valid JSON array.
func formatCorrectionPrompt(err error) string {
	return fmt.Sprintf(
		"Your response could not be parsed as a JSON array. Error: %s\n\n"+
			"Please respond with ONLY a valid JSON array matching this structure:\n"+
			"```json\n"+
			"[\n"+
			"  {\n"+
			"    \"title\": \"<concise requirement title>\",\n"+
			"    \"description\": \"<detailed description of the behavioral intent>\"\n"+
			"  }\n"+
			"]\n"+
			"```",
		err.Error(),
	)
}

// saveAndPublish saves requirements to disk, transitions the plan status, and
// publishes a RequirementsGeneratedEvent to JetStream to continue the cascade.
func (c *Component) saveAndPublish(ctx context.Context, trigger *payloads.RequirementGeneratorRequest, requirements []workflow.Requirement) error {
	repoRoot := repoRootPath()
	manager := workflow.NewManager(repoRoot)

	// Partial regeneration: merge new requirements with the existing set rather
	// than replacing it wholesale. Deprecated entries from the prior round are
	// already persisted; we only append the freshly generated replacements.
	if len(trigger.ReplaceRequirementIDs) > 0 {
		existing, err := manager.LoadRequirements(ctx, trigger.Slug)
		if err == nil {
			requirements = append(existing, requirements...)
		} else {
			c.logger.Warn("Failed to load existing requirements for partial regen merge; overwriting",
				"slug", trigger.Slug, "error", err)
		}
	}

	// Persist requirements to .semspec/projects/default/plans/{slug}/requirements.json.
	if err := manager.SaveRequirements(ctx, requirements, trigger.Slug); err != nil {
		return fmt.Errorf("save requirements: %w", err)
	}

	// Transition plan status to requirements_generated.
	plan, err := manager.LoadPlan(ctx, trigger.Slug)
	if err != nil {
		return fmt.Errorf("load plan for status transition: %w", err)
	}

	if err := manager.SetPlanStatus(ctx, plan, workflow.StatusRequirementsGenerated); err != nil {
		// Log and continue — the requirements are saved; status update is best-effort.
		c.logger.Warn("Failed to transition plan status to requirements_generated",
			"slug", trigger.Slug, "error", err)
	}

	// Publish RequirementsGeneratedEvent so plan-api can dispatch scenario generation.
	// Use requirementsGeneratedPayload (implements message.Payload) with the same JSON
	// layout as workflow.RequirementsGeneratedEvent so ParseReactivePayload deserialises it correctly.
	event := &requirementsGeneratedPayload{
		Slug:             trigger.Slug,
		RequirementCount: len(requirements),
		TraceID:          trigger.TraceID,
	}

	baseMsg := message.NewBaseMessage(RequirementsGeneratedType, event, "requirement-generator")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal requirements-generated event: %w", err)
	}

	if err := c.natsClient.PublishToStream(ctx, workflow.RequirementsGenerated.Pattern, data); err != nil {
		return fmt.Errorf("publish requirements-generated event: %w", err)
	}

	c.logger.Info("Published RequirementsGeneratedEvent",
		"slug", trigger.Slug,
		"requirement_count", len(requirements),
		"subject", workflow.RequirementsGenerated.Pattern)

	return nil
}

// requirementGeneratorSystemPrompt returns the system prompt instructing the LLM
// to decompose a plan into structured requirements.
func requirementGeneratorSystemPrompt() string {
	return `You are a requirements analyst. Your task is to decompose a software development plan into a set of clear, testable requirements.

Each requirement must capture a single behavioral intent — what the system should do, not how it should do it.

Output ONLY a valid JSON array of requirement objects. Do not include any prose, explanation, or markdown outside the JSON block.

Each object must have exactly these fields:
- "title": a concise, action-oriented title (e.g. "User can reset password via email link")
- "description": a detailed description of the behavioral intent, including edge cases and constraints

Example output:
` + "```json" + `
[
  {
    "title": "System validates JWT tokens on every protected endpoint",
    "description": "Every HTTP request to a protected API endpoint must include a valid JWT bearer token. The system must reject requests with expired, malformed, or missing tokens with a 401 status. Tokens must be validated against the configured signing key."
  }
]
` + "```"
}

// requirementGeneratorUserPrompt builds the user prompt from plan fields and optional graph context.
func requirementGeneratorUserPrompt(plan *workflow.Plan, graphContext string) string {
	var sb strings.Builder

	sb.WriteString("## Plan to Decompose\n\n")
	sb.WriteString(fmt.Sprintf("**Title**: %s\n\n", plan.Title))

	if plan.Goal != "" {
		sb.WriteString(fmt.Sprintf("**Goal**: %s\n\n", plan.Goal))
	}
	if plan.Context != "" {
		sb.WriteString(fmt.Sprintf("**Context**: %s\n\n", plan.Context))
	}

	if len(plan.Scope.Include) > 0 || len(plan.Scope.Exclude) > 0 || len(plan.Scope.DoNotTouch) > 0 {
		sb.WriteString("**Scope**:\n")
		if len(plan.Scope.Include) > 0 {
			sb.WriteString(fmt.Sprintf("- Include: %s\n", strings.Join(plan.Scope.Include, ", ")))
		}
		if len(plan.Scope.Exclude) > 0 {
			sb.WriteString(fmt.Sprintf("- Exclude: %s\n", strings.Join(plan.Scope.Exclude, ", ")))
		}
		if len(plan.Scope.DoNotTouch) > 0 {
			sb.WriteString(fmt.Sprintf("- Do not touch: %s\n", strings.Join(plan.Scope.DoNotTouch, ", ")))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Decompose the above plan into a JSON array of requirements. Each requirement should represent a distinct behavioral intent that can be independently verified.\n")

	if graphContext != "" {
		sb.WriteString("\n## Codebase Context\n\nThe following context from the knowledge graph provides information about the existing codebase structure:\n\n")
		sb.WriteString(graphContext)
	}

	return sb.String()
}

// repoRootPath resolves the repository root path, preferring the SEMSPEC_REPO_PATH
// environment variable and falling back to the current working directory.
func repoRootPath() string {
	if root := os.Getenv("SEMSPEC_REPO_PATH"); root != "" {
		return root
	}
	root, err := os.Getwd()
	if err != nil {
		return "."
	}
	return root
}

// Stop gracefully stops the component.
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil
	}

	// Copy cancel function and clear state before releasing lock.
	cancel := c.cancel
	c.running = false
	c.cancel = nil
	c.mu.Unlock()

	// Cancel context after releasing lock to avoid potential deadlock.
	if cancel != nil {
		cancel()
	}

	c.logger.Info("requirement-generator stopped",
		"triggers_processed", c.triggersProcessed.Load(),
		"requirements_generated", c.requirementsGenerated.Load(),
		"generations_failed", c.generationsFailed.Load())

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "requirement-generator",
		Type:        "processor",
		Description: "Generates Requirements for approved plans using LLM",
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
	return requirementGeneratorSchema
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
