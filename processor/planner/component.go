// Package planner provides a processor that generates Goal/Context/Scope
// for plans using LLM based on the plan title and codebase analysis.
package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/model"
	contextbuilder "github.com/c360studio/semspec/processor/context-builder"
	"github.com/c360studio/semspec/processor/contexthelper"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/phases"
	"github.com/c360studio/semspec/workflow/prompts"
	"github.com/c360studio/semspec/workflow/reactive"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	semstreamsWorkflow "github.com/c360studio/semstreams/pkg/workflow"
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

	// Workflow state management (reactive engine KV bucket)
	stateBucket jetstream.KeyValue

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

// Participant interface implementation for workflow observability.
var _ semstreamsWorkflow.Participant = (*Component)(nil)

// WorkflowID returns the workflow this component participates in.
func (c *Component) WorkflowID() string {
	return "plan-review"
}

// Phase returns the phase name this component represents.
func (c *Component) Phase() string {
	return phases.PlanPlanned
}

// StateManager returns nil - this component updates state directly via KV bucket.
// The reactive engine manages state; we just update it on completion.
func (c *Component) StateManager() *semstreamsWorkflow.StateManager {
	return nil
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
	if config.StateBucket == "" {
		config.StateBucket = defaults.StateBucket
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

	// Get or create state bucket for workflow state
	stateBucket, err := js.KeyValue(subCtx, c.config.StateBucket)
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("get state bucket %s: %w", c.config.StateBucket, err)
	}
	c.stateBucket = stateBucket

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

	c.logger.Info("Processing planner trigger",
		"request_id", trigger.RequestID,
		"slug", trigger.Slug,
		"trace_id", trigger.TraceID)

	llmCtx := c.buildLLMContext(ctx, trigger)

	planContent, llmRequestIDs, err := c.generatePlan(llmCtx, trigger)
	if err != nil {
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to generate plan",
			"request_id", trigger.RequestID, "slug", trigger.Slug, "error", err)
		c.handlePlanFailure(ctx, msg, trigger, err)
		return
	}

	if err := c.savePlan(ctx, trigger, planContent); err != nil {
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to save plan",
			"request_id", trigger.RequestID, "slug", trigger.Slug, "error", err)
		c.handlePlanFailure(ctx, msg, trigger, err)
		return
	}

	if err := c.publishResult(ctx, trigger, planContent, llmRequestIDs); err != nil {
		c.logger.Warn("Failed to publish result notification",
			"request_id", trigger.RequestID, "slug", trigger.Slug, "error", err)
		// Don't fail — plan was saved successfully.
	}

	c.plansGenerated.Add(1)

	if err := msg.Ack(); err != nil {
		c.logger.Warn("Failed to ACK message", "error", err)
	}

	c.logger.Info("Plan generated successfully",
		"request_id", trigger.RequestID,
		"slug", trigger.Slug)
}

// parseTrigger deserialises and validates the NATS message payload. It NAKs or
// ACKs the message on failure and returns false so the caller can return early.
func (c *Component) parseTrigger(msg jetstream.Msg) (*reactive.PlannerRequest, bool) {
	trigger, err := reactive.ParseReactivePayload[reactive.PlannerRequest](msg.Data())
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

// buildLLMContext injects trace context into ctx when the trigger carries a trace
// or loop ID, so that LLM calls are properly attributed in the trajectory store.
func (c *Component) buildLLMContext(ctx context.Context, trigger *reactive.PlannerRequest) context.Context {
	if trigger.TraceID == "" && trigger.LoopID == "" {
		return ctx
	}
	return llm.WithTraceContext(ctx, llm.TraceContext{
		TraceID: trigger.TraceID,
		LoopID:  trigger.LoopID,
	})
}

// handlePlanFailure updates the workflow state with the error and transitions
// to the generator_failed phase. For non-workflow requests, NAKs the message.
func (c *Component) handlePlanFailure(ctx context.Context, msg jetstream.Msg, trigger *reactive.PlannerRequest, cause error) {
	// Check if this is a workflow-dispatched request
	if trigger.ExecutionID != "" {
		if err := c.transitionToFailure(ctx, trigger.ExecutionID, cause.Error()); err != nil {
			c.logger.Error("Failed to transition to failure state", "error", err)
		}
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Warn("Failed to ACK message", "error", ackErr)
		}
		return
	}

	// Legacy path: NAK for retry
	if nakErr := msg.Nak(); nakErr != nil {
		c.logger.Warn("Failed to NAK message", "error", nakErr)
	}
}

// transitionToFailure updates the workflow state to the generator_failed phase.
func (c *Component) transitionToFailure(ctx context.Context, executionID, errMsg string) error {
	entry, err := c.stateBucket.Get(ctx, executionID)
	if err != nil {
		return fmt.Errorf("get workflow state %s: %w", executionID, err)
	}

	var state reactive.PlanReviewState
	if err := json.Unmarshal(entry.Value(), &state); err != nil {
		return fmt.Errorf("unmarshal workflow state: %w", err)
	}

	state.Phase = phases.PlanGeneratorFailed
	state.Error = errMsg
	state.UpdatedAt = time.Now()

	stateData, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal updated state: %w", err)
	}

	if _, err := c.stateBucket.Update(ctx, executionID, stateData, entry.Revision()); err != nil {
		return fmt.Errorf("update workflow state: %w", err)
	}

	c.logger.Info("Transitioned workflow to failure state",
		"execution_id", executionID,
		"phase", phases.PlanGeneratorFailed,
		"error", errMsg)
	return nil
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
func (c *Component) generatePlan(ctx context.Context, trigger *reactive.PlannerRequest) (*PlanContent, []string, error) {
	isRevision := trigger.Prompt != "" && strings.HasPrefix(trigger.Prompt, "REVISION REQUEST:")

	// Step 1: Request planning context from centralized context-builder (graph-first).
	// Pass the capability so context-builder can calculate the correct token budget
	// based on the model that will actually be used for LLM calls.
	contextReq := &contextbuilder.ContextBuildRequest{
		TaskType:   contextbuilder.TaskTypePlanning,
		Topic:      trigger.Title,
		Capability: c.config.DefaultCapability,
	}

	// On revision, load the current plan directly for the user prompt.
	// Previously this went through context-builder where it was buried under
	// a generic "Codebase Context" header — the LLM couldn't tell it was
	// looking at its own previous output that needed fixing.
	var currentPlanJSON string
	if isRevision && trigger.Slug != "" {
		if planJSON, err := c.loadCurrentPlanJSON(trigger.Slug); err != nil {
			c.logger.Warn("Could not load current plan for revision context",
				"slug", trigger.Slug, "error", err)
		} else {
			currentPlanJSON = planJSON
			c.logger.Info("Loaded current plan for revision prompt",
				"slug", trigger.Slug, "plan_json_length", len(planJSON))
		}
	}

	var graphContext string
	resp := c.contextHelper.BuildContextGraceful(ctx, contextReq)
	if resp != nil {
		// Build context string from response
		graphContext = contexthelper.FormatContextResponse(resp)
		c.logger.Info("Built planning context via context-builder",
			"title", trigger.Title,
			"entities", len(resp.Entities),
			"documents", len(resp.Documents),
			"tokens_used", resp.TokensUsed)
	} else {
		c.logger.Warn("Context build returned nil, proceeding without graph context",
			"title", trigger.Title)
	}

	// Step 2: Build messages with proper system/user separation.
	// The system prompt (with JSON format) is ALWAYS included — even on
	// revision calls — because local LLMs need the format example every time.
	systemPrompt := prompts.PlannerSystemPrompt()
	var userPrompt string
	if isRevision {
		// Revision prompt: put the current plan FIRST so the LLM sees what it
		// needs to fix, then the reviewer findings, then codebase context.
		var sb strings.Builder
		if currentPlanJSON != "" {
			sb.WriteString("## Your Previous Plan Output\n\n")
			sb.WriteString("This is the plan you produced that was rejected. Update it to address ALL findings below.\n\n")
			sb.WriteString("```json\n")
			sb.WriteString(currentPlanJSON)
			sb.WriteString("\n```\n\n")
		}
		sb.WriteString(trigger.Prompt)
		userPrompt = sb.String()

		c.logger.Debug("Planner received revision prompt",
			"has_current_plan", currentPlanJSON != "",
			"prompt_length", len(userPrompt),
			"slug", trigger.Slug)
	} else if trigger.Prompt != "" {
		// Custom prompt (non-revision): use as-is
		userPrompt = trigger.Prompt
	} else {
		// Initial plan: build from title
		userPrompt = prompts.PlannerPromptWithTitle(trigger.Title)
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
func (c *Component) generatePlanFromMessages(ctx context.Context, capability, systemPrompt, userPrompt string) (*PlanContent, []string, error) {
	temperature := 0.7
	messages := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
	var lastErr error
	var llmRequestIDs []string

	for attempt := range maxFormatRetries {
		llmResp, err := c.llmClient.Complete(ctx, llm.Request{
			Capability:  capability,
			Messages:    messages,
			Temperature: &temperature,
			MaxTokens:   4096,
		})
		if err != nil {
			return nil, llmRequestIDs, fmt.Errorf("LLM completion: %w", err)
		}

		llmRequestIDs = append(llmRequestIDs, llmResp.RequestID)

		c.logger.Debug("LLM response received",
			"model", llmResp.Model,
			"tokens_used", llmResp.TokensUsed,
			"attempt", attempt+1)

		planContent, parseErr := c.parsePlanFromResponse(llmResp.Content)
		if parseErr == nil {
			return planContent, llmRequestIDs, nil
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

	return nil, llmRequestIDs, fmt.Errorf("parse plan from response: %w", lastErr)
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
func (c *Component) savePlan(ctx context.Context, trigger *reactive.PlannerRequest, planContent *PlanContent) error {
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
	plan, err := manager.LoadPlan(ctx, trigger.Slug)
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

	// Record trace ID for trajectory tracking
	if trigger.TraceID != "" && !slices.Contains(plan.ExecutionTraceIDs, trigger.TraceID) {
		plan.ExecutionTraceIDs = append(plan.ExecutionTraceIDs, trigger.TraceID)
	}

	// Save the updated plan
	return manager.SavePlan(ctx, plan)
}

// loadCurrentPlanJSON reads the current plan.json from disk and returns its
// Goal/Context/Scope fields as a JSON string. Used during revision to provide
// the LLM with the plan it needs to fix, routed through the context-builder
// for token budget management.
func (c *Component) loadCurrentPlanJSON(slug string) (string, error) {
	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get working directory: %w", err)
		}
	}

	manager := workflow.NewManager(repoRoot)
	plan, err := manager.LoadPlan(context.Background(), slug)
	if err != nil {
		return "", fmt.Errorf("load plan: %w", err)
	}

	// Extract only the LLM-relevant fields to keep the payload focused
	planSnapshot := struct {
		Goal    string         `json:"goal"`
		Context string         `json:"context"`
		Scope   workflow.Scope `json:"scope"`
	}{
		Goal:    plan.Goal,
		Context: plan.Context,
		Scope:   plan.Scope,
	}

	data, err := json.MarshalIndent(planSnapshot, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal plan: %w", err)
	}
	return string(data), nil
}

// PlannerResultType is the message type for planner results.
var PlannerResultType = message.Type{Domain: "workflow", Category: "planner-result", Version: "v1"}

// Result is the result payload for plan generation.
type Result struct {
	workflow.CallbackFields

	RequestID     string       `json:"request_id"`
	Slug          string       `json:"slug"`
	Content       *PlanContent `json:"content"`
	Status        string       `json:"status"`
	LLMRequestIDs []string     `json:"llm_request_ids,omitempty"`
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

// publishResult updates the workflow state with the plan content and transitions
// to the "planned" phase. The reactive engine watches KV and fires the next rule.
func (c *Component) publishResult(ctx context.Context, trigger *reactive.PlannerRequest, planContent *PlanContent, llmRequestIDs []string) error {
	// Check if this is a workflow-dispatched request (has ExecutionID)
	if trigger.ExecutionID == "" {
		c.logger.Warn("No ExecutionID - cannot update workflow state",
			"slug", trigger.Slug,
			"request_id", trigger.RequestID)
		return nil
	}

	// Get current state from KV
	entry, err := c.stateBucket.Get(ctx, trigger.ExecutionID)
	if err != nil {
		return fmt.Errorf("get workflow state %s: %w", trigger.ExecutionID, err)
	}

	// Deserialize the typed state
	var state reactive.PlanReviewState
	if err := json.Unmarshal(entry.Value(), &state); err != nil {
		return fmt.Errorf("unmarshal workflow state: %w", err)
	}

	// Marshal plan content to JSON for storage
	planContentJSON, err := json.Marshal(planContent)
	if err != nil {
		return fmt.Errorf("marshal plan content: %w", err)
	}

	// Update state with results
	state.PlanContent = planContentJSON
	state.LLMRequestIDs = llmRequestIDs
	state.Phase = phases.PlanPlanned
	state.UpdatedAt = time.Now()

	// Write back to KV
	stateData, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal updated state: %w", err)
	}

	if _, err := c.stateBucket.Update(ctx, trigger.ExecutionID, stateData, entry.Revision()); err != nil {
		return fmt.Errorf("update workflow state: %w", err)
	}

	c.logger.Info("Updated workflow state with plan result",
		"slug", trigger.Slug,
		"execution_id", trigger.ExecutionID,
		"phase", phases.PlanPlanned)
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
