// Package planreviewer provides a processor that reviews plans against SOPs
// before approval using LLM analysis.
package planreviewer

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

// llmCompleter is the subset of the LLM client used by the plan-reviewer.
type llmCompleter interface {
	Complete(ctx context.Context, req llm.Request) (*llm.Response, error)
}

// Component implements the plan-reviewer processor.
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
	reviewsProcessed atomic.Int64
	reviewsApproved  atomic.Int64
	reviewsRejected  atomic.Int64
	reviewsFailed    atomic.Int64
	lastActivityMu   sync.RWMutex
	lastActivity     time.Time
}

// NewComponent creates a new plan-reviewer processor.
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
		SourceName:     "plan-reviewer",
	}, logger)

	return &Component{
		name:       "plan-reviewer",
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
	c.logger.Debug("Initialized plan-reviewer",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"trigger_subject", c.config.TriggerSubject)
	return nil
}

// Start begins processing plan review triggers.
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

	c.logger.Info("plan-reviewer started",
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

// PlanReviewTrigger is the trigger payload for plan review.
type PlanReviewTrigger struct {
	workflow.CallbackFields

	RequestID     string          `json:"request_id"`
	Slug          string          `json:"slug"`
	ProjectID     string          `json:"project_id"`
	PlanContent   json.RawMessage `json:"plan_content"`
	ScopePatterns []string        `json:"scope_patterns"`
	SOPContext    string          `json:"sop_context,omitempty"` // Pre-built SOP context
}

// Schema implements message.Payload.
func (t *PlanReviewTrigger) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "plan-review-trigger", Version: "v1"}
}

// Validate implements message.Payload.
func (t *PlanReviewTrigger) Validate() error {
	if t.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}
	if t.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	if len(t.PlanContent) == 0 {
		return fmt.Errorf("plan_content is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (t *PlanReviewTrigger) MarshalJSON() ([]byte, error) {
	type Alias PlanReviewTrigger
	return json.Marshal((*Alias)(t))
}

// UnmarshalJSON implements json.Unmarshaler.
func (t *PlanReviewTrigger) UnmarshalJSON(data []byte) error {
	type Alias PlanReviewTrigger
	return json.Unmarshal(data, (*Alias)(t))
}

// handleMessage processes a single plan review trigger.
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	c.reviewsProcessed.Add(1)
	c.updateLastActivity()

	// Parse the trigger (handles both BaseMessage-wrapped and raw JSON from
	// workflow-processor publish_async)
	trigger, err := workflow.ParseNATSMessage[PlanReviewTrigger](msg.Data())
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

	c.logger.Info("Processing plan review trigger",
		"request_id", trigger.RequestID,
		"slug", trigger.Slug)

	// Perform the review using LLM
	result, err := c.reviewPlan(ctx, trigger)
	if err != nil {
		c.reviewsFailed.Add(1)
		c.logger.Error("Failed to review plan",
			"request_id", trigger.RequestID,
			"slug", trigger.Slug,
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

	// Track metrics
	if result.IsApproved() {
		c.reviewsApproved.Add(1)
	} else {
		c.reviewsRejected.Add(1)
	}

	// Publish result
	if err := c.publishResult(ctx, trigger, result); err != nil {
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

	c.logger.Info("Plan review completed",
		"request_id", trigger.RequestID,
		"slug", trigger.Slug,
		"verdict", result.Verdict,
		"summary", result.Summary,
		"findings_count", len(result.Findings))

	// Log individual findings for observability
	for i, f := range result.Findings {
		c.logger.Info("Plan review finding",
			"slug", trigger.Slug,
			"finding_index", i,
			"sop_id", f.SOPID,
			"severity", f.Severity,
			"status", f.Status,
			"issue", f.Issue,
			"suggestion", f.Suggestion)
	}
}

// reviewPlan calls the LLM to review the plan against SOPs.
// It uses the centralized context-builder to retrieve SOPs, file tree, and related context.
func (c *Component) reviewPlan(ctx context.Context, trigger *PlanReviewTrigger) (*prompts.PlanReviewResult, error) {
	// Check context cancellation before expensive operations
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	// Step 1: Request plan-review context from context-builder (graph-first)
	// This retrieves SOPs, project file tree, plan content, and architecture docs
	var enrichedContext string
	ctxResp := c.contextHelper.BuildContextGraceful(ctx, &contextbuilder.ContextBuildRequest{
		TaskType:      contextbuilder.TaskTypePlanReview,
		PlanSlug:      trigger.Slug,
		PlanContent:   string(trigger.PlanContent),
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
	systemPrompt := prompts.PlanReviewerSystemPrompt()
	userPrompt := prompts.PlanReviewerUserPrompt(trigger.Slug, string(trigger.PlanContent), enrichedContext)

	// If no context at all, auto-approve
	if enrichedContext == "" {
		c.logger.Warn("No SOP context available for plan review",
			"slug", trigger.Slug,
			"context_builder_responded", ctxResp != nil)
		return &prompts.PlanReviewResult{
			Verdict:  "approved",
			Summary:  "No plan-scope SOPs or relevant context found. Plan approved by default.",
			Findings: nil,
		}, nil
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

		c.logger.Debug("Review LLM response received",
			"model", llmResp.Model,
			"tokens_used", llmResp.TokensUsed,
			"attempt", attempt+1)

		result, parseErr := c.parseReviewFromResponse(llmResp.Content)
		if parseErr == nil {
			return result, nil
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
			llm.Message{Role: "user", Content: reviewerFormatCorrectionPrompt(parseErr)},
		)
	}

	return nil, fmt.Errorf("parse review from response: %w", lastErr)
}

// reviewerFormatCorrectionPrompt builds a correction message for the LLM when
// the review response isn't valid JSON.
func reviewerFormatCorrectionPrompt(err error) string {
	return fmt.Sprintf(
		"Your response could not be parsed as JSON. Error: %s\n\n"+
			"Please respond with ONLY a valid JSON object matching this structure:\n"+
			"```json\n"+
			"{\n"+
			"  \"verdict\": \"approved\" or \"needs_changes\",\n"+
			"  \"summary\": \"Brief overall assessment\",\n"+
			"  \"findings\": [\n"+
			"    {\n"+
			"      \"sop_id\": \"source.doc.sops.example\",\n"+
			"      \"sop_title\": \"Example SOP\",\n"+
			"      \"severity\": \"error\" or \"warning\" or \"info\",\n"+
			"      \"status\": \"compliant\" or \"violation\" or \"not_applicable\",\n"+
			"      \"issue\": \"Description\",\n"+
			"      \"suggestion\": \"How to fix\"\n"+
			"    }\n"+
			"  ]\n"+
			"}\n"+
			"```",
		err.Error(),
	)
}

// parseReviewFromResponse extracts the review result from the LLM response.
func (c *Component) parseReviewFromResponse(content string) (*prompts.PlanReviewResult, error) {
	// Extract JSON from the response (may be wrapped in markdown code blocks)
	jsonContent := llm.ExtractJSON(content)
	if jsonContent == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var result prompts.PlanReviewResult
	if err := json.Unmarshal([]byte(jsonContent), &result); err != nil {
		return nil, fmt.Errorf("parse JSON: %w (content: %s)", err, jsonContent[:min(200, len(jsonContent))])
	}

	// Validate verdict
	if result.Verdict != "approved" && result.Verdict != "needs_changes" {
		return nil, fmt.Errorf("invalid verdict: %s (expected approved or needs_changes)", result.Verdict)
	}

	return &result, nil
}

// PlanReviewResult is the result payload for plan review.
type PlanReviewResult struct {
	RequestID string                      `json:"request_id"`
	Slug      string                      `json:"slug"`
	Verdict   string                      `json:"verdict"`
	Summary   string                      `json:"summary"`
	Findings  []prompts.PlanReviewFinding `json:"findings"`
	// FormattedFindings is a human-readable markdown rendering of the
	// findings array. Workflow templates should reference this field
	// (not the raw findings array) when embedding review feedback in
	// LLM prompts, because semstreams interpolation JSON-stringifies
	// arrays — producing unreadable output for local LLMs.
	FormattedFindings string `json:"formatted_findings"`
	Status            string `json:"status"`
}

// Schema implements message.Payload.
func (r *PlanReviewResult) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "review-result", Version: "v1"}
}

// Validate implements message.Payload.
func (r *PlanReviewResult) Validate() error {
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *PlanReviewResult) MarshalJSON() ([]byte, error) {
	type Alias PlanReviewResult
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *PlanReviewResult) UnmarshalJSON(data []byte) error {
	type Alias PlanReviewResult
	return json.Unmarshal(data, (*Alias)(r))
}

// publishResult publishes a result notification for the plan review.
// publishResult publishes a review result notification.
// Uses the workflow-processor's async callback pattern (ADR-005 Phase 6).
func (c *Component) publishResult(ctx context.Context, trigger *PlanReviewTrigger, result *prompts.PlanReviewResult) error {
	payload := &PlanReviewResult{
		RequestID:         trigger.RequestID,
		Slug:              trigger.Slug,
		Verdict:           result.Verdict,
		Summary:           result.Summary,
		Findings:          result.Findings,
		FormattedFindings: result.FormatFindings(),
		Status:            "completed",
	}

	if !trigger.HasCallback() {
		c.logger.Warn("No callback configured for plan-reviewer result",
			"slug", trigger.Slug,
			"request_id", trigger.RequestID)
		return nil
	}

	if err := trigger.PublishCallbackSuccess(ctx, c.natsClient, payload); err != nil {
		return fmt.Errorf("publish callback: %w", err)
	}
	c.logger.Info("Published plan-reviewer callback result",
		"slug", trigger.Slug,
		"verdict", result.Verdict,
		"task_id", trigger.TaskID,
		"callback", trigger.CallbackSubject)
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
	c.logger.Info("plan-reviewer stopped",
		"reviews_processed", c.reviewsProcessed.Load(),
		"reviews_approved", c.reviewsApproved.Load(),
		"reviews_rejected", c.reviewsRejected.Load(),
		"reviews_failed", c.reviewsFailed.Load())

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "plan-reviewer",
		Type:        "processor",
		Description: "Reviews plans against SOPs before approval using LLM analysis",
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
	return planReviewerSchema
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
