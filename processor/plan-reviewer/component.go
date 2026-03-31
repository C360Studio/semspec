// Package planreviewer provides a processor that reviews plans against SOPs
// by dispatching a reviewer agent through agentic-dispatch.
//
// The reviewer agent has real bash and graph access, so it can verify plan
// scope against the actual codebase — not just evaluate the text.
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

	"github.com/google/uuid"

	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/prompt"
	promptdomain "github.com/c360studio/semspec/prompt/domain"
	workflowtools "github.com/c360studio/semspec/tools/workflow"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semspec/workflow/prompts"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// stepReviewing is the workflow step for plan review.
	stepReviewing = "reviewing"

	// subjectReviewTask is the NATS subject for review agent tasks.
	subjectReviewTask = "agent.task.reviewer"
)

// Component implements the plan-reviewer processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

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

	// Initialize prompt assembler with software domain.
	registry := prompt.NewRegistry()
	registry.RegisterAll(promptdomain.Software()...)
	registry.Register(prompt.ToolGuidanceFragment(prompt.DefaultToolGuidance()))
	registry.Register(prompt.GraphManifestFragment(workflowtools.FederatedManifestFetchFn()))
	assembler := prompt.NewAssembler(registry)

	return &Component{
		name:          "plan-reviewer",
		config:        config,
		natsClient:    deps.NATSClient,
		logger:        logger,
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
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Warn("Cannot start PLAN_STATES watcher: no JetStream", "error", err)
	} else {
		go c.watchPlanStates(subCtx, js)
	}

	// Loop completion watcher — picks up agentic-dispatch results from AGENT_LOOPS KV.
	go c.watchLoopCompletions(subCtx)

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
func (c *Component) handleMessagePush(ctx context.Context, msg jetstream.Msg) {
	c.handleMessage(ctx, msg)
}

// handleMessage processes a single plan review trigger from JetStream.
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	c.reviewsProcessed.Add(1)
	c.updateLastActivity()

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
		if err := msg.Ack(); err != nil {
			c.logger.Warn("Failed to ACK message", "error", err)
		}
		return
	}

	c.logger.Info("Processing plan review trigger",
		"request_id", trigger.RequestID,
		"slug", trigger.Slug,
		"trace_id", trigger.TraceID)

	// ACK immediately — the agent loop handles retries internally.
	if err := msg.Ack(); err != nil {
		c.logger.Warn("Failed to ACK message", "error", err)
	}

	// Dispatch reviewer agent with plan content + SOP context.
	c.dispatchReviewer(ctx, trigger.Slug, string(trigger.PlanContent), trigger.SOPContext, roundDraftReview)
}

// watchLoopCompletions watches the AGENT_LOOPS KV bucket for review agent
// completions. Routes terminal events to handleLoopCompletion.
func (c *Component) watchLoopCompletions(ctx context.Context) {
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Warn("Cannot watch AGENT_LOOPS: no JetStream", "error", err)
		return
	}

	bucket, err := workflow.WaitForKVBucket(ctx, js, "AGENT_LOOPS")
	if err != nil {
		c.logger.Warn("AGENT_LOOPS bucket not available — loop completion watcher disabled", "error", err)
		return
	}

	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		c.logger.Warn("Failed to watch AGENT_LOOPS", "error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Loop completion watcher started (watching AGENT_LOOPS for reviews)")

	for entry := range watcher.Updates() {
		if entry == nil {
			continue
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}

		var loop agentic.LoopEntity
		if err := json.Unmarshal(entry.Value(), &loop); err != nil {
			continue
		}
		if !loop.State.IsTerminal() {
			continue
		}
		// Use the planner's workflow slug since review is part of the planning pipeline.
		if loop.WorkflowSlug != workflow.WorkflowSlugPlanning {
			continue
		}
		if loop.WorkflowStep != stepReviewing {
			continue
		}

		slug, _ := loop.Metadata["plan_slug"].(string)
		if slug == "" {
			continue
		}

		c.handleLoopCompletion(ctx, &loop, slug)
	}
}

// handleLoopCompletion processes a completed review agent loop. It parses
// the review verdict and sends appropriate approval/rejection mutations.
func (c *Component) handleLoopCompletion(ctx context.Context, loop *agentic.LoopEntity, slug string) {
	c.updateLastActivity()

	// Extract review round from metadata.
	round := roundDraftReview
	if r, ok := loop.Metadata["review_round"].(float64); ok {
		round = reviewRound(int(r))
	}

	if loop.Outcome != agentic.OutcomeSuccess {
		c.reviewsFailed.Add(1)
		c.logger.Error("Review agent loop failed",
			"slug", slug,
			"loop_id", loop.ID,
			"round", round,
			"outcome", loop.Outcome,
			"error", loop.Error)
		c.sendGenerationFailed(ctx, slug, round, loop.Error)
		return
	}

	result, err := parseReviewFromResult(loop.Result)
	if err != nil {
		c.reviewsFailed.Add(1)
		c.logger.Error("Failed to parse review from agent result",
			"slug", slug,
			"loop_id", loop.ID,
			"round", round,
			"error", err)
		c.sendGenerationFailed(ctx, slug, round, fmt.Sprintf("failed to parse review result: %v", err))
		return
	}

	c.logger.Info("Review agent complete",
		"slug", slug,
		"round", round,
		"verdict", result.Verdict,
		"summary", result.Summary)

	if result.IsApproved() {
		c.reviewsApproved.Add(1)
		if err := c.sendApprovalMutations(ctx, slug, result.Summary, round); err != nil {
			c.logger.Warn("Failed to send approval mutations",
				"slug", slug, "round", round, "error", err)
		}
	} else {
		c.reviewsRejected.Add(1)
		c.sendRevisionMutation(ctx, slug, round, result)
	}
}

// dispatchReviewer dispatches a plan-reviewer agent loop via agentic-dispatch.
func (c *Component) dispatchReviewer(ctx context.Context, slug, planContent, sopContext string, round reviewRound) {
	c.updateLastActivity()

	taskID := fmt.Sprintf("review-%s-r%d-%s", slug, round, uuid.New().String())

	// Build SOP context: prefer trigger's SOPContext, fall back to disk.
	enrichedContext := sopContext
	if enrichedContext == "" {
		enrichedContext = c.loadSOPContextFromDisk(slug)
	}

	// Build user prompt.
	userPrompt := prompts.PlanReviewerUserPrompt(slug, planContent, enrichedContext, int(round))

	// Resolve model.
	capability := c.config.DefaultCapability
	if model.ParseCapability(capability) == "" {
		capability = string(model.CapabilityReviewing)
	}
	modelName := c.modelRegistry.Resolve(model.Capability(capability))

	// Assemble system prompt via fragment pipeline.
	provider := c.resolveProvider()
	assembled := c.assembler.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RolePlanReviewer,
		Provider:       provider,
		Domain:         "software",
		AvailableTools: prompt.FilterTools(c.availableToolNames(), prompt.RolePlanReviewer),
		SupportsTools:  true,
	})

	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleReviewer,
		Model:        modelName,
		Prompt:       userPrompt,
		WorkflowSlug: workflow.WorkflowSlugPlanning,
		WorkflowStep: stepReviewing,
		Context: &agentic.ConstructedContext{
			Content: assembled.SystemMessage,
		},
		Metadata: map[string]any{
			"plan_slug":    slug,
			"review_round": int(round),
			"task_id":      "main", // reviewer runs against main workspace, not a worktree
		},
	}

	baseMsg := message.NewBaseMessage(task.Schema(), task, "semspec-plan-reviewer")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.reviewsFailed.Add(1)
		c.logger.Error("Failed to marshal task message", "slug", slug, "error", err)
		return
	}

	if err := c.natsClient.PublishToStream(ctx, subjectReviewTask, data); err != nil {
		c.reviewsFailed.Add(1)
		c.logger.Error("Failed to dispatch reviewer agent", "slug", slug, "error", err)
		return
	}

	c.logger.Info("Dispatched reviewer agent",
		"slug", slug,
		"task_id", taskID,
		"round", round,
		"model", modelName,
		"fragments", len(assembled.FragmentsUsed))
}

// availableToolNames returns the full list of tool names for prompt assembly.
func (c *Component) availableToolNames() []string {
	return []string{
		"bash", "submit_work", "submit_review", "ask_question",
		"graph_search", "graph_query", "graph_summary",
		"web_search", "http_request",
		"decompose_task", "spawn_agent",
		"review_scenario",
	}
}

// loadSOPContextFromDisk loads standards from .semspec/standards.json as a fallback.
func (c *Component) loadSOPContextFromDisk(slug string) string {
	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		repoRoot, _ = os.Getwd()
	}
	standardsPath := filepath.Join(repoRoot, ".semspec", "standards.json")
	data, err := os.ReadFile(standardsPath)
	if err != nil {
		return ""
	}
	var standards workflow.Standards
	if err := json.Unmarshal(data, &standards); err != nil || len(standards.Rules) == 0 {
		return ""
	}
	var rules []string
	for _, r := range standards.Rules {
		rules = append(rules, fmt.Sprintf("- [%s] %s", r.ID, r.Text))
	}
	c.logger.Info("Loaded standards from disk for plan review",
		"slug", slug, "rule_count", len(standards.Rules))
	return "## Project Standards\n\n" + strings.Join(rules, "\n")
}

// parseReviewFromResult extracts a PlanReviewResult from the agent's submit_review output.
func parseReviewFromResult(result string) (*prompts.PlanReviewResult, error) {
	if result == "" {
		return nil, fmt.Errorf("empty result")
	}

	var review prompts.PlanReviewResult

	// Try direct JSON parse first.
	if err := json.Unmarshal([]byte(result), &review); err == nil {
		if review.Verdict == "approved" || review.Verdict == "needs_changes" {
			// Correct verdict: only error-severity violations should block approval.
			if review.Verdict == "needs_changes" && len(review.ErrorFindings()) == 0 {
				review.Verdict = "approved"
			}
			return &review, nil
		}
	}

	// Try extracting JSON from text.
	start := strings.Index(result, "{")
	if start < 0 {
		return nil, fmt.Errorf("no JSON found in result")
	}
	depth := 0
	end := -1
	for i := start; i < len(result); i++ {
		switch result[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i + 1
			}
		}
		if end > 0 {
			break
		}
	}
	if end <= start {
		return nil, fmt.Errorf("malformed JSON in result")
	}

	if err := json.Unmarshal([]byte(result[start:end]), &review); err != nil {
		return nil, fmt.Errorf("parse review JSON: %w", err)
	}

	if review.Verdict != "approved" && review.Verdict != "needs_changes" {
		return nil, fmt.Errorf("invalid verdict: %s", review.Verdict)
	}

	if review.Verdict == "needs_changes" && len(review.ErrorFindings()) == 0 {
		review.Verdict = "approved"
	}

	return &review, nil
}

// PlanReviewResult is the result payload for plan review.
type PlanReviewResult struct {
	RequestID string                      `json:"request_id"`
	Slug      string                      `json:"slug"`
	Verdict   string                      `json:"verdict"`
	Summary   string                      `json:"summary"`
	Findings  []prompts.PlanReviewFinding `json:"findings"`
	// FormattedFindings is a human-readable markdown rendering of the findings.
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

// Stop gracefully stops the component.
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil
	}

	cancel := c.cancel
	c.running = false
	c.cancel = nil
	c.mu.Unlock()

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
		Description: "Reviews plans against SOPs via agentic-dispatch reviewer",
		Version:     "0.2.0",
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
			Config:      component.NATSPort{Subject: portDef.Subject},
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
			Config:      component.NATSPort{Subject: portDef.Subject},
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
