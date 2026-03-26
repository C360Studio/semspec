// Package planreviewer provides a processor that reviews plans against SOPs
// before approval using LLM analysis.
package planreviewer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/prompt"
	promptdomain "github.com/c360studio/semspec/prompt/domain"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semspec/workflow/phases"
	"github.com/c360studio/semspec/workflow/prompts"
	"github.com/c360studio/semstreams/agentic"
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

	llmClient     llmCompleter
	modelRegistry *model.Registry
	assembler     *prompt.Assembler

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
	if config.PlanStateBucket == "" {
		config.PlanStateBucket = defaults.PlanStateBucket
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
		name:       "plan-reviewer",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     logger,
		llmClient: llm.NewClient(model.Global(),
			llm.WithLogger(logger),
		),
		modelRegistry: model.Global(),
		assembler:     assembler,
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

	// Push-based consumption — messages arrive via callback, no polling delay.
	cfg := natsclient.StreamConsumerConfig{
		StreamName:    c.config.StreamName,
		ConsumerName:  c.config.ConsumerName,
		FilterSubject: c.config.TriggerSubject,
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       180 * time.Second,
	}
	if err := c.natsClient.ConsumeStreamWithConfig(subCtx, cfg, c.handleMessagePush); err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("consume plan-review triggers: %w", err)
	}

	// Watch PLAN_STATES for plans reaching "drafted" or "scenarios_generated".
	// This is the KV twofer — the plan-manager write IS the trigger.
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Warn("Cannot start PLAN_STATES watcher: no JetStream", "error", err)
	} else {
		go c.watchPlanStates(subCtx, js)
	}

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

// handleMessagePush is the push-based callback for ConsumeStreamWithConfig.
// Messages arrive immediately when published — no polling delay.
func (c *Component) handleMessagePush(ctx context.Context, msg jetstream.Msg) {
	c.handleMessage(ctx, msg)
}

// handleMessage processes a single plan review trigger.
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	c.reviewsProcessed.Add(1)
	c.updateLastActivity()

	// Parse the reactive engine's BaseMessage-wrapped payload.
	trigger, err := payloads.ParseReactivePayload[payloads.PlanReviewRequest](msg.Data())
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
		"slug", trigger.Slug,
		"trace_id", trigger.TraceID)

	// Signal in-progress to prevent redelivery during LLM operations.
	if err := msg.InProgress(); err != nil {
		c.logger.Debug("Failed to signal in-progress", "error", err)
	}

	// Inject trace context for LLM call tracking
	llmCtx := ctx
	if trigger.TraceID != "" || trigger.LoopID != "" {
		llmCtx = llm.WithTraceContext(ctx, llm.TraceContext{
			TraceID: trigger.TraceID,
			LoopID:  trigger.LoopID,
		})
	}

	// Perform the review using LLM
	result, llmRequestIDs, err := c.reviewPlan(llmCtx, trigger)
	if err != nil {
		c.reviewsFailed.Add(1)
		c.logger.Error("Failed to review plan",
			"request_id", trigger.RequestID,
			"slug", trigger.Slug,
			"error", err)
		// Transition workflow to failure state so the reactive engine can handle it
		if trigger.ExecutionID != "" {
			if transErr := c.transitionToFailure(ctx, trigger.ExecutionID, err.Error()); transErr != nil {
				c.logger.Error("Failed to transition to failure state", "error", transErr)
			}
		}
		if err := msg.Ack(); err != nil {
			c.logger.Warn("Failed to ACK message", "error", err)
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
func (c *Component) reviewPlan(ctx context.Context, trigger *payloads.PlanReviewRequest) (*prompts.PlanReviewResult, []string, error) {
	// Check context cancellation before expensive operations
	if err := ctx.Err(); err != nil {
		return nil, nil, fmt.Errorf("context cancelled: %w", err)
	}

	// Use the pre-built SOP context from the trigger payload, or load
	// standards from disk as a fallback.
	var enrichedContext string
	if trigger.SOPContext != "" {
		enrichedContext = trigger.SOPContext
		c.logger.Info("Using pre-built SOP context from trigger",
			"slug", trigger.Slug,
			"context_length", len(enrichedContext))
	} else {
		// Load standards from .semspec/standards.json if available.
		repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
		if repoRoot == "" {
			repoRoot, _ = os.Getwd()
		}
		standardsPath := filepath.Join(repoRoot, ".semspec", "standards.json")
		if data, err := os.ReadFile(standardsPath); err == nil {
			var standards workflow.Standards
			if err := json.Unmarshal(data, &standards); err == nil && len(standards.Rules) > 0 {
				var rules []string
				for _, r := range standards.Rules {
					rules = append(rules, fmt.Sprintf("- [%s] %s", r.ID, r.Text))
				}
				enrichedContext = "## Project Standards\n\n" + strings.Join(rules, "\n")
				c.logger.Info("Loaded standards from disk for plan review",
					"slug", trigger.Slug,
					"rule_count", len(standards.Rules))
			}
		}
	}

	// Build prompts with enriched context
	// Use fragment-based assembler for system prompt with provider-aware formatting
	provider := c.resolveProvider()
	assembled := c.assembler.Assemble(&prompt.AssemblyContext{
		Role:     prompt.RolePlanReviewer,
		Provider: provider,
		Domain:   "software",
	})
	systemPrompt := assembled.SystemMessage
	userPrompt := prompts.PlanReviewerUserPrompt(trigger.Slug, string(trigger.PlanContent), enrichedContext)

	c.logger.Debug("Assembled plan-reviewer prompt",
		"provider", provider,
		"fragments_used", assembled.FragmentsUsed)

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
			llm.Message{Role: "user", Content: reviewerFormatCorrectionPrompt(parseErr)},
		)
	}

	return nil, llmRequestIDs, fmt.Errorf("parse review from response: %w", lastErr)
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

	// Correct verdict: only error-severity violations should block approval.
	// LLMs sometimes return needs_changes for warning-only violations despite
	// prompt instructions. Override to approved if no error-severity violations exist.
	if result.Verdict == "needs_changes" && len(result.ErrorFindings()) == 0 {
		result.Verdict = "approved"
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
	FormattedFindings string   `json:"formatted_findings"`
	Status            string   `json:"status"`
	LLMRequestIDs     []string `json:"llm_request_ids,omitempty"`
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

// transitionToFailure transitions the workflow to the reviewer_failed phase.
// TODO(migration): Phase N will replace this — state will be entity triples in ENTITY_STATES.
func (c *Component) transitionToFailure(_ context.Context, executionID string, cause string) error {
	c.logger.Warn("transitionToFailure: state management pending migration",
		"execution_id", executionID,
		"phase", phases.PlanReviewerFailed,
		"cause", cause)
	return nil
}

// publishResult emits a LoopCompletedEvent so the review-orchestrator knows
// the review finished and can proceed with approval/rejection/revision.
func (c *Component) publishResult(ctx context.Context, trigger *payloads.PlanReviewRequest, result *prompts.PlanReviewResult, llmRequestIDs []string) error {
	if trigger.TaskID == "" {
		c.logger.Info("Plan review complete (no review-orchestrator TaskID, skipping LoopCompletedEvent)",
			"slug", trigger.Slug, "execution_id", trigger.ExecutionID, "verdict", result.Verdict)
		return nil
	}

	reviewResult := &PlanReviewResult{
		RequestID:     trigger.RequestID,
		Slug:          trigger.Slug,
		Verdict:       result.Verdict,
		Summary:       result.Summary,
		Findings:      result.Findings,
		Status:        "success",
		LLMRequestIDs: llmRequestIDs,
	}

	// Format findings for the review-orchestrator's revision prompts.
	if len(result.Findings) > 0 {
		var formatted []string
		for _, f := range result.Findings {
			formatted = append(formatted, fmt.Sprintf("- [%s] %s: %s", f.Severity, f.SOPTitle, f.Issue))
		}
		reviewResult.FormattedFindings = fmt.Sprintf("## Review Findings\n\n%s", joinStrings(formatted, "\n"))
	}

	resultBytes, err := json.Marshal(reviewResult)
	if err != nil {
		return fmt.Errorf("marshal review result: %w", err)
	}

	event := &agentic.LoopCompletedEvent{
		LoopID:       trigger.ExecutionID,
		TaskID:       trigger.TaskID,
		Outcome:      agentic.OutcomeSuccess,
		Role:         string(agentic.RoleReviewer),
		Result:       string(resultBytes),
		WorkflowSlug: trigger.WorkflowSlug,
		WorkflowStep: "review",
		CompletedAt:  time.Now(),
		Iterations:   1,
	}

	baseMsg := message.NewBaseMessage(event.Schema(), event, "semspec-plan-reviewer")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal loop completed event: %w", err)
	}

	// Publish to agent.complete.<taskID> — covered by agent.complete.> in AGENT stream.
	subject := fmt.Sprintf("agent.complete.%s", trigger.TaskID)
	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		return fmt.Errorf("publish loop completed: %w", err)
	}

	c.logger.Info("Plan review complete, emitted LoopCompletedEvent",
		"slug", trigger.Slug,
		"execution_id", trigger.ExecutionID,
		"task_id", trigger.TaskID,
		"verdict", result.Verdict)
	return nil
}

func joinStrings(ss []string, sep string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
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

// resolveProvider determines the LLM provider for prompt formatting.
func (c *Component) resolveProvider() prompt.Provider {
	capability := c.config.DefaultCapability
	if model.ParseCapability(capability) == "" {
		capability = string(model.CapabilityReviewing)
	}
	modelName := c.modelRegistry.Resolve(model.Capability(capability))
	if endpoint := c.modelRegistry.GetEndpoint(modelName); endpoint != nil {
		return prompt.Provider(endpoint.Provider)
	}
	return prompt.ProviderOllama
}
