// Package taskreviewer provides a processor that reviews generated tasks against SOPs
// before approval using LLM analysis.
package taskreviewer

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
	contextbuilder "github.com/c360studio/semspec/processor/context-builder"
	"github.com/c360studio/semspec/processor/contexthelper"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/phases"
	"github.com/c360studio/semspec/workflow/reactive"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	semstreamsWorkflow "github.com/c360studio/semstreams/pkg/workflow"
	"github.com/nats-io/nats.go/jetstream"
)

// maxFormatRetries is the total number of LLM call attempts when the response
// isn't valid JSON. On each retry, the parse error is fed back to the LLM as a
// correction prompt so it can fix the output format.
const maxFormatRetries = 5

// llmCompleter is the subset of the LLM client used by the task-reviewer.
type llmCompleter interface {
	Complete(ctx context.Context, req llm.Request) (*llm.Response, error)
}

// Component implements the task-reviewer processor.
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

	// KV bucket for workflow state (reactive engine state)
	stateBucket jetstream.KeyValue

	// Lifecycle
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics
	reviewsProcessed atomic.Int64
	reviewsApproved  atomic.Int64
	reviewsRejected  atomic.Int64
	reviewsFailed    atomic.Int64
	lastActivityMu   sync.RWMutex
	lastActivity     time.Time
}

// ---------------------------------------------------------------------------
// Participant interface
// ---------------------------------------------------------------------------

// Compile-time check that Component implements Participant interface.
var _ semstreamsWorkflow.Participant = (*Component)(nil)

// WorkflowID returns the workflow this component participates in.
func (c *Component) WorkflowID() string {
	return "task-review-loop"
}

// Phase returns the phase name this component represents.
func (c *Component) Phase() string {
	return phases.TasksReviewed
}

// StateManager returns nil - this component updates state directly via KV bucket.
// The reactive engine manages state; we just update it on completion.
func (c *Component) StateManager() *semstreamsWorkflow.StateManager {
	return nil
}

// NewComponent creates a new task-reviewer processor.
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
	if config.ResultSubjectPrefix == "" {
		config.ResultSubjectPrefix = defaults.ResultSubjectPrefix
	}
	if config.LLMTimeout == "" {
		config.LLMTimeout = defaults.LLMTimeout
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
		SourceName:     "task-reviewer",
	}, logger)

	return &Component{
		name:       "task-reviewer",
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
	c.logger.Debug("Initialized task-reviewer",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"trigger_subject", c.config.TriggerSubject)
	return nil
}

// Start begins processing task review triggers.
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

	// Get or create workflow state bucket
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

	c.logger.Info("task-reviewer started",
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

// handleMessage processes a single task review trigger.
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	c.reviewsProcessed.Add(1)
	c.updateLastActivity()

	// Parse the trigger dispatched by the reactive engine (BaseMessage-wrapped).
	trigger, err := reactive.ParseReactivePayload[reactive.TaskReviewRequest](msg.Data())
	if err != nil {
		c.reviewsFailed.Add(1)
		c.logger.Error("Failed to parse trigger", "error", err)
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}

	if err := trigger.Validate(); err != nil {
		c.logger.Error("Invalid trigger", "error", err)
		// ACK invalid requests - they won't succeed on retry
		if err := msg.Ack(); err != nil {
			c.logger.Warn("Failed to ACK message", "error", err)
		}
		return
	}

	c.logger.Info("Processing task review trigger",
		"request_id", trigger.RequestID,
		"slug", trigger.Slug,
		"task_count", len(trigger.Tasks),
		"trace_id", trigger.TraceID)

	// Inject trace context for LLM call tracking
	llmCtx := ctx
	if trigger.TraceID != "" || trigger.LoopID != "" {
		llmCtx = llm.WithTraceContext(ctx, llm.TraceContext{
			TraceID: trigger.TraceID,
			LoopID:  trigger.LoopID,
		})
	}

	// Perform the review using LLM
	result, llmRequestIDs, err := c.reviewTasks(llmCtx, trigger)
	if err != nil {
		c.reviewsFailed.Add(1)
		c.logger.Error("Failed to review tasks",
			"request_id", trigger.RequestID,
			"slug", trigger.Slug,
			"error", err)
		// Transition workflow to failure state so the reactive engine can handle it
		if trigger.ExecutionID != "" {
			if transErr := c.transitionToFailure(ctx, trigger.ExecutionID, err.Error()); transErr != nil {
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
		c.logger.Debug("No ExecutionID - NAKing for retry",
			"request_id", trigger.RequestID,
			"slug", trigger.Slug)
		if nakErr := msg.Nak(); nakErr != nil {
			c.logger.Warn("Failed to NAK message", "error", nakErr)
		}
		return
	}

	// Track metrics
	if result.IsApproved() {
		c.reviewsApproved.Add(1)
	} else {
		c.reviewsRejected.Add(1)
	}

	// Publish result
	if err := c.publishResult(ctx, trigger, result, llmRequestIDs); err != nil {
		c.logger.Warn("Failed to publish result notification",
			"request_id", trigger.RequestID,
			"slug", trigger.Slug,
			"error", err)
		// Don't fail - review was successful
	}

	// ACK the message
	if err := msg.Ack(); err != nil {
		c.logger.Warn("Failed to ACK message", "error", err)
	}

	c.logger.Info("Task review completed",
		"request_id", trigger.RequestID,
		"slug", trigger.Slug,
		"verdict", result.Verdict,
		"summary", result.Summary,
		"findings_count", len(result.Findings))

	// Log individual findings for observability
	for i, f := range result.Findings {
		c.logger.Info("Task review finding",
			"slug", trigger.Slug,
			"finding_index", i,
			"sop_id", f.SOPID,
			"severity", f.Severity,
			"status", f.Status,
			"issue", f.Issue,
			"suggestion", f.Suggestion,
			"task_id", f.TaskID)
	}
}

// transitionToFailure transitions the workflow to the reviewer-failed phase.
func (c *Component) transitionToFailure(ctx context.Context, executionID string, cause string) error {
	entry, err := c.stateBucket.Get(ctx, executionID)
	if err != nil {
		return fmt.Errorf("get workflow state: %w", err)
	}

	var state reactive.TaskReviewState
	if err := json.Unmarshal(entry.Value(), &state); err != nil {
		return fmt.Errorf("unmarshal workflow state: %w", err)
	}

	state.Phase = phases.TaskReviewerFailed
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
		"phase", phases.TaskReviewerFailed,
		"cause", cause)
	return nil
}

// reviewTasks calls the LLM to review the tasks against SOPs.
// It uses the centralized context-builder to retrieve SOPs, file tree, and related context.
func (c *Component) reviewTasks(ctx context.Context, trigger *reactive.TaskReviewRequest) (*LLMTaskReviewResult, []string, error) {
	// Check context cancellation before expensive operations
	if err := ctx.Err(); err != nil {
		return nil, nil, fmt.Errorf("context cancelled: %w", err)
	}

	// Step 1: Request task-review context from context-builder (graph-first)
	// This retrieves SOPs, project file tree, and architecture docs
	// We reuse TaskTypePlanReview since task review has similar requirements
	// (both need SOPs, file tree, architecture docs to validate against)
	var enrichedContext string
	ctxResp := c.contextHelper.BuildContextGraceful(ctx, &contextbuilder.ContextBuildRequest{
		TaskType:      contextbuilder.TaskTypePlanReview,
		PlanSlug:      trigger.Slug,
		ScopePatterns: trigger.ScopePatterns,
	})
	if ctxResp != nil {
		enrichedContext = contexthelper.FormatContextResponse(ctxResp)
		c.logger.Info("Built review context via context-builder",
			"slug", trigger.Slug,
			"entities", len(ctxResp.Entities),
			"documents", len(ctxResp.Documents),
			"sop_ids", ctxResp.SOPIDs,
			"tokens_used", ctxResp.TokensUsed)
	}

	// Merge trigger's pre-built SOPContext when context-builder didn't return SOPs.
	// This handles the case where graph is down but context-builder still returns
	// partial context (file tree, plan docs) — we still need SOPs for review.
	if trigger.SOPContext != "" {
		if enrichedContext == "" {
			enrichedContext = trigger.SOPContext
			c.logger.Info("Using pre-built SOP context from trigger (no context-builder response)",
				"slug", trigger.Slug,
				"context_length", len(enrichedContext))
		} else if ctxResp != nil && len(ctxResp.SOPIDs) == 0 {
			enrichedContext = enrichedContext + "\n\n## SOP Standards\n\n" + trigger.SOPContext
			c.logger.Info("Merged trigger SOP context (context-builder returned no SOPs)",
				"slug", trigger.Slug,
				"sop_context_length", len(trigger.SOPContext))
		}
	}

	// Build prompts with enriched context
	systemPrompt := SystemPrompt()
	userPrompt := UserPrompt(trigger.Slug, trigger.Tasks, enrichedContext)

	// If no context at all, auto-approve with basic validation
	if enrichedContext == "" {
		c.logger.Warn("No SOP context available for task review",
			"slug", trigger.Slug,
			"context_builder_responded", ctxResp != nil)
		// Still validate basic task structure
		result, err := c.validateBasicTaskStructure(trigger.Tasks)
		return result, nil, err
	}

	// Resolve capability for model selection
	capability := c.config.DefaultCapability
	if model.ParseCapability(capability) == "" {
		capability = string(model.CapabilityReviewing)
	}

	temperature := 0.3 // Lower temperature for more consistent reviews
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

		c.logger.Debug("Review LLM response received",
			"model", llmResp.Model,
			"tokens_used", llmResp.TokensUsed,
			"attempt", attempt+1)

		result, parseErr := c.parseReviewFromResponse(llmResp.Content)
		if parseErr == nil {
			return result, llmRequestIDs, nil
		}

		lastErr = parseErr

		if attempt+1 >= maxFormatRetries {
			break
		}

		c.logger.Warn("Reviewer LLM format retry",
			"attempt", attempt+1,
			"error", parseErr)

		messages = append(messages,
			llm.Message{Role: "assistant", Content: llmResp.Content},
			llm.Message{Role: "user", Content: formatCorrectionPrompt(parseErr)},
		)
	}

	return nil, llmRequestIDs, fmt.Errorf("parse review from response: %w", lastErr)
}

// validateBasicTaskStructure performs basic structural validation when no SOPs are present.
// It checks that tasks have acceptance criteria and returns approved if valid.
func (c *Component) validateBasicTaskStructure(tasks []workflow.Task) (*LLMTaskReviewResult, error) {
	var findings []TaskReviewFinding
	hasError := false

	for _, task := range tasks {
		if len(task.AcceptanceCriteria) == 0 {
			findings = append(findings, TaskReviewFinding{
				SOPID:      "builtin.acceptance-criteria",
				SOPTitle:   "Acceptance Criteria Requirement",
				Severity:   "error",
				Status:     "violation",
				Issue:      "Task has no acceptance criteria",
				Suggestion: "Add at least one Given/When/Then acceptance criterion",
				TaskID:     task.ID,
			})
			hasError = true
		}
	}

	verdict := "approved"
	summary := "Tasks pass basic structural validation. No SOPs were found to validate against."
	if hasError {
		verdict = "needs_changes"
		summary = "Tasks are missing acceptance criteria. All tasks must have at least one Given/When/Then criterion."
	}

	return &LLMTaskReviewResult{
		Verdict:  verdict,
		Summary:  summary,
		Findings: findings,
	}, nil
}

// parseReviewFromResponse extracts the review result from the LLM response.
func (c *Component) parseReviewFromResponse(content string) (*LLMTaskReviewResult, error) {
	// Extract JSON from the response (may be wrapped in markdown code blocks)
	jsonContent := llm.ExtractJSON(content)
	if jsonContent == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var result LLMTaskReviewResult
	if err := json.Unmarshal([]byte(jsonContent), &result); err != nil {
		return nil, fmt.Errorf("parse JSON: %w (content: %s)", err, jsonContent[:min(200, len(jsonContent))])
	}

	// Validate verdict
	if result.Verdict != "approved" && result.Verdict != "needs_changes" {
		return nil, fmt.Errorf("invalid verdict: %s (expected approved or needs_changes)", result.Verdict)
	}

	return &result, nil
}

// publishResult updates the workflow state with the review results.
// This transitions the workflow to the tasks-reviewed phase, which triggers
// the reactive engine to advance to the next step.
func (c *Component) publishResult(ctx context.Context, trigger *reactive.TaskReviewRequest, result *LLMTaskReviewResult, llmRequestIDs []string) error {
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
	var state reactive.TaskReviewState
	if err := json.Unmarshal(entry.Value(), &state); err != nil {
		return fmt.Errorf("unmarshal workflow state: %w", err)
	}

	// Marshal findings to JSON for storage
	findingsJSON, err := json.Marshal(result.Findings)
	if err != nil {
		return fmt.Errorf("marshal findings: %w", err)
	}

	// Update state with results
	state.Verdict = result.Verdict
	state.Summary = result.Summary
	state.Findings = findingsJSON
	state.FormattedFindings = result.FormatFindings()
	state.ReviewerLLMRequestIDs = llmRequestIDs
	state.Phase = phases.TasksReviewed
	state.UpdatedAt = time.Now()

	// Write back to KV
	stateData, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal updated state: %w", err)
	}

	if _, err := c.stateBucket.Update(ctx, trigger.ExecutionID, stateData, entry.Revision()); err != nil {
		return fmt.Errorf("update workflow state: %w", err)
	}

	c.logger.Info("Updated workflow state with review result",
		"slug", trigger.Slug,
		"execution_id", trigger.ExecutionID,
		"phase", phases.TasksReviewed,
		"verdict", result.Verdict)
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

	c.logger.Info("task-reviewer stopped",
		"reviews_processed", c.reviewsProcessed.Load(),
		"reviews_approved", c.reviewsApproved.Load(),
		"reviews_rejected", c.reviewsRejected.Load(),
		"reviews_failed", c.reviewsFailed.Load())

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "task-reviewer",
		Type:        "processor",
		Description: "Reviews generated tasks against SOPs before approval using LLM analysis",
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
	return taskReviewerSchema
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
		ErrorCount: int(c.reviewsFailed.Load()),
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
