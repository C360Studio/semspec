// Package planner provides a processor that generates Goal/Context/Scope
// for plans by dispatching a planner agent through agentic-dispatch.
//
// The agent explores the codebase via bash and graph tools, then produces
// a structured plan. Running through the agentic loop gives it real tool
// execution, trajectory tracking, and codebase visibility — replacing the
// previous inline llmClient.Complete() path which had zero codebase context.
package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
	// stepDrafting is the workflow step for plan drafting (coordinator + focused planners).
	stepDrafting = "drafting"

	// subjectPlanningTask is the NATS subject for planning agent tasks.
	subjectPlanningTask = "agent.task.planning"
)

// Component implements the planner processor.
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
		name:          "planner",
		config:        config,
		natsClient:    deps.NATSClient,
		logger:        logger,
		modelRegistry: model.Global(),
		assembler:     assembler,
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
		return fmt.Errorf("consume planner triggers: %w", err)
	}

	// KV watcher — self-triggers on new plan creation (status == "created", revision == 1).
	go c.watchPlanStates(subCtx)

	// Loop completion watcher — picks up agentic-dispatch results from AGENT_LOOPS KV.
	go c.watchLoopCompletions(subCtx)

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

// watchPlanStates watches the PLAN_STATES KV bucket and self-triggers the planner
// whenever a new plan is created (revision == 1). This replaces the deleted coordinator
// as the dispatch path for initial plan drafting.
func (c *Component) watchPlanStates(ctx context.Context) {
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Warn("Cannot watch PLAN_STATES: no JetStream", "error", err)
		return
	}

	bucket, err := workflow.WaitForKVBucket(ctx, js, "PLAN_STATES")
	if err != nil {
		c.logger.Warn("PLAN_STATES bucket not available — KV self-trigger disabled", "error", err)
		return
	}

	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		c.logger.Warn("Failed to start PLAN_STATES watcher", "error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Watching PLAN_STATES for new plan creations")

	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-watcher.Updates():
			if !ok {
				c.logger.Info("PLAN_STATES watcher closed")
				return
			}
			if entry == nil {
				continue
			}
			if entry.Operation() != jetstream.KeyValuePut {
				continue
			}
			var plan workflow.Plan
			if err := json.Unmarshal(entry.Value(), &plan); err != nil {
				continue
			}

			// Only trigger on new plans (status empty or "created").
			if plan.Status != "" && plan.Status != workflow.StatusCreated {
				continue
			}
			if plan.Slug == "" {
				continue
			}

			// Claim the plan to prevent re-trigger on KV replay or concurrent watchers.
			if !workflow.ClaimPlanStatus(ctx, c.natsClient, plan.Slug, workflow.StatusDrafting, c.logger) {
				continue
			}

			// Detect revision mode: if the plan already has a Goal and ReviewFindings,
			// this is an R1 retry (ADR-029). The planner should refine the existing
			// draft rather than starting from scratch.
			if plan.Goal != "" && len(plan.ReviewFindings) > 0 {
				planJSON, _ := json.Marshal(plan)
				findings := plan.ReviewFormattedFindings
				if findings == "" {
					findings = plan.ReviewSummary
				}
				revisionPrompt := fmt.Sprintf("## REVISION REQUEST (iteration %d)\n\nThe reviewer rejected your previous plan. Address ALL findings below.\n\n%s", plan.ReviewIteration, findings)
				c.logger.Info("Detected revision plan — dispatching in refinement mode",
					"slug", plan.Slug,
					"review_iteration", plan.ReviewIteration)
				go c.dispatchPlanner(ctx, plan.Slug, plan.Title, true, string(planJSON), revisionPrompt)
			} else {
				go c.dispatchPlanner(ctx, plan.Slug, plan.Title, false, "", "")
			}
		}
	}
}

// watchLoopCompletions watches the AGENT_LOOPS KV bucket for planning agent
// completions. When a loop reaches terminal state with WorkflowSlug matching
// our planning workflow, the result is parsed and the plan mutation is sent.
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

	c.logger.Info("Loop completion watcher started (watching AGENT_LOOPS for planning)")

	for entry := range watcher.Updates() {
		if entry == nil {
			continue // end of initial replay
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
		if loop.WorkflowSlug != workflow.WorkflowSlugPlanning {
			continue
		}
		if loop.WorkflowStep != stepDrafting {
			continue
		}

		slug, _ := loop.Metadata["plan_slug"].(string)
		if slug == "" {
			continue
		}

		c.handleLoopCompletion(ctx, &loop, slug)
	}
}

// handleLoopCompletion processes a completed planning agent loop. It parses
// the plan content from the loop result and sends a plan.mutation.drafted
// request to plan-manager.
func (c *Component) handleLoopCompletion(ctx context.Context, loop *agentic.LoopEntity, slug string) {
	c.updateLastActivity()

	if loop.Outcome != agentic.OutcomeSuccess {
		c.generationsFailed.Add(1)
		c.logger.Error("Planning agent loop failed",
			"slug", slug,
			"loop_id", loop.ID,
			"outcome", loop.Outcome,
			"error", loop.Error)
		c.sendGenerationFailed(ctx, slug, fmt.Sprintf("planner loop failed: %s", loop.Error))
		return
	}

	planContent, err := parsePlanFromResult(loop.Result)
	if err != nil {
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to parse plan from agent result",
			"slug", slug,
			"loop_id", loop.ID,
			"error", err)
		c.sendGenerationFailed(ctx, slug, fmt.Sprintf("planner output parse failed: %v", err))
		return
	}

	scope := &workflow.Scope{
		Include:    planContent.Scope.Include,
		Exclude:    planContent.Scope.Exclude,
		DoNotTouch: planContent.Scope.DoNotTouch,
	}

	mutReq := draftedMutationRequest{
		Slug:    slug,
		Title:   planContent.Title,
		Goal:    planContent.Goal,
		Context: planContent.Context,
		Scope:   scope,
	}
	data, err := json.Marshal(mutReq)
	if err != nil {
		c.logger.Error("Failed to marshal drafted mutation", "slug", slug, "error", err)
		return
	}

	resp, err := c.natsClient.RequestWithRetry(ctx, mutationDraftedSubject, data, 10*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		c.logger.Error("Drafted mutation request failed", "slug", slug, "error", err)
		return
	}

	var mutResp struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(resp, &mutResp); err != nil || !mutResp.Success {
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		} else {
			errMsg = mutResp.Error
		}
		c.logger.Error("Plan-manager rejected drafted mutation", "slug", slug, "error", errMsg)
		return
	}

	c.plansGenerated.Add(1)
	c.logger.Info("Plan drafted via agentic-dispatch and mutation accepted",
		"slug", slug,
		"loop_id", loop.ID)
}

// dispatchPlanner dispatches a planner agent loop via agentic-dispatch.
// The agent explores the codebase using bash and graph tools, then produces
// a Goal/Context/Scope plan. Running through the agentic loop gives it real
// tool execution, trajectory tracking, and codebase visibility.
func (c *Component) dispatchPlanner(ctx context.Context, slug, title string, isRevision bool, previousPlanJSON, revisionPrompt string) {
	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	taskID := fmt.Sprintf("plan-%s-%s", slug, uuid.New().String())

	// Build user prompt.
	var userPrompt string
	if isRevision && revisionPrompt != "" {
		var sb []byte
		if previousPlanJSON != "" {
			sb = append(sb, "## Your Previous Plan Output\n\nThis is the plan you produced that was rejected. Update it to address ALL findings below.\n\n```json\n"...)
			sb = append(sb, previousPlanJSON...)
			sb = append(sb, "\n```\n\n"...)
		}
		sb = append(sb, revisionPrompt...)
		userPrompt = string(sb)
	} else if title != "" {
		userPrompt = prompts.PlannerPromptWithTitle(title)
	}

	// Resolve model for planning capability.
	capability := c.config.DefaultCapability
	if capability == "" {
		capability = string(model.CapabilityPlanning)
	}
	modelName := c.modelRegistry.Resolve(model.Capability(capability))

	// Assemble system prompt via fragment pipeline.
	provider := c.resolveProvider()
	assembled := c.assembler.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RolePlanner,
		Provider:       provider,
		Domain:         "software",
		AvailableTools: prompt.FilterTools(c.availableToolNames(), prompt.RolePlanner),
		SupportsTools:  true,
	})

	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleGeneral,
		Model:        modelName,
		Prompt:       userPrompt,
		ToolChoice:   &agentic.ToolChoice{Mode: "required"},
		WorkflowSlug: workflow.WorkflowSlugPlanning,
		WorkflowStep: stepDrafting,
		Context: &agentic.ConstructedContext{
			Content: assembled.SystemMessage,
		},
		Metadata: map[string]any{
			"plan_slug":        slug,
			"task_id":          "main", // planner runs against main workspace, not a worktree
			"deliverable_type": "plan",
		},
	}

	baseMsg := message.NewBaseMessage(task.Schema(), task, "semspec-planner")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to marshal task message", "slug", slug, "error", err)
		return
	}

	if err := c.natsClient.PublishToStream(ctx, subjectPlanningTask, data); err != nil {
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to dispatch plan coordinator", "slug", slug, "error", err)
		return
	}

	c.logger.Info("Dispatched plan coordinator agent",
		"slug", slug,
		"task_id", taskID,
		"model", modelName,
		"fragments", len(assembled.FragmentsUsed))
}

// availableToolNames returns the full list of tool names for prompt assembly.
// Actual tool availability is controlled by agentic-tools at runtime.
func (c *Component) availableToolNames() []string {
	return []string{
		"bash", "submit_work", "submit_review", "ask_question",
		"graph_search", "graph_query", "graph_summary",
		"web_search", "http_request",
		"decompose_task", "spawn_agent",
		"review_scenario",
	}
}

// mutationDraftedSubject is the request/reply subject for plan.mutation.drafted.
const mutationDraftedSubject = "plan.mutation.drafted"

// draftedMutationRequest is sent to plan-manager after drafting a plan.
type draftedMutationRequest struct {
	Slug    string          `json:"slug"`
	Title   string          `json:"title,omitempty"`
	Goal    string          `json:"goal"`
	Context string          `json:"context"`
	Scope   *workflow.Scope `json:"scope,omitempty"`
	TraceID string          `json:"trace_id,omitempty"`
}

// handleMessagePush is the push-based callback for ConsumeStreamWithConfig.
func (c *Component) handleMessagePush(ctx context.Context, msg jetstream.Msg) {
	c.handleMessage(ctx, msg)
}

// handleMessage processes a single planner trigger from the JetStream consumer.
// This is the backward-compatible trigger path (alongside the KV self-trigger).
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	if ctx.Err() != nil {
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message during shutdown", "error", err)
		}
		return
	}

	trigger, ok := c.parseTrigger(msg)
	if !ok {
		return
	}

	c.logger.Info("Processing planner trigger",
		"request_id", trigger.RequestID,
		"slug", trigger.Slug,
		"trace_id", trigger.TraceID)

	// ACK immediately — the agent loop handles retries internally.
	if err := msg.Ack(); err != nil {
		c.logger.Warn("Failed to ACK message", "error", err)
	}

	// Dispatch coordinator agent.
	c.dispatchPlanner(ctx, trigger.Slug, trigger.Title,
		trigger.Revision, trigger.PreviousPlanJSON, trigger.Prompt)
}

// parseTrigger deserialises and validates the NATS message payload.
func (c *Component) parseTrigger(msg jetstream.Msg) (*payloads.PlannerRequest, bool) {
	trigger, err := payloads.ParseReactivePayload[payloads.PlannerRequest](msg.Data())
	if err != nil {
		c.logger.Error("Failed to parse trigger", "error", err)
		if nakErr := msg.Nak(); nakErr != nil {
			c.logger.Warn("Failed to NAK message", "error", nakErr)
		}
		return nil, false
	}

	if err := trigger.Validate(); err != nil {
		c.logger.Error("Invalid trigger payload", "error", err)
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Warn("Failed to ACK invalid message", "error", ackErr)
		}
		return nil, false
	}

	return trigger, true
}

// PlanContent holds the LLM-generated plan fields.
type PlanContent struct {
	Title   string `json:"title,omitempty"`
	Goal    string `json:"goal"`
	Context string `json:"context"`
	Scope   struct {
		Include    []string `json:"include,omitempty"`
		Exclude    []string `json:"exclude,omitempty"`
		DoNotTouch []string `json:"do_not_touch,omitempty"`
	} `json:"scope"`
	Status string `json:"status,omitempty"`
}

// parsePlanFromResult extracts PlanContent from an agent loop result string.
// The result may be raw JSON or wrapped in markdown code fences.
func parsePlanFromResult(result string) (*PlanContent, error) {
	if result == "" {
		return nil, fmt.Errorf("empty result")
	}

	// Try direct JSON parse first.
	var pc PlanContent
	if err := json.Unmarshal([]byte(result), &pc); err == nil && pc.Goal != "" {
		return &pc, nil
	}

	// Try extracting from markdown code fences.
	jsonContent := extractJSON(result)
	if jsonContent == "" {
		return nil, fmt.Errorf("no JSON found in result")
	}

	if err := json.Unmarshal([]byte(jsonContent), &pc); err != nil {
		return nil, fmt.Errorf("parse JSON: %w (content: %s)", err, jsonContent[:min(200, len(jsonContent))])
	}

	if pc.Goal == "" {
		return nil, fmt.Errorf("plan missing 'goal' field")
	}

	return &pc, nil
}

// extractJSON pulls a JSON object from text that may contain markdown fences.
func extractJSON(s string) string {
	// Look for ```json ... ``` wrapper.
	start := -1
	for i := 0; i < len(s)-3; i++ {
		if s[i] == '{' {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	// Find matching closing brace.
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// sendGenerationFailed publishes a plan.mutation.generation.failed mutation to
// inform plan-manager that plan generation has failed for the given slug.
func (c *Component) sendGenerationFailed(ctx context.Context, slug, feedback string) {
	failReq, _ := json.Marshal(map[string]string{
		"slug":  slug,
		"phase": "plan-generation",
		"error": feedback,
	})
	if _, err := c.natsClient.RequestWithRetry(ctx, "plan.mutation.generation.failed", failReq,
		10*time.Second, natsclient.DefaultRetryConfig()); err != nil {
		c.logger.Warn("Failed to publish generation.failed mutation",
			"slug", slug, "error", err)
	}
}

// PlannerResultType is the message type for planner results.
var PlannerResultType = message.Type{Domain: "workflow", Category: "planner-result", Version: "v1"}

// Result is the result payload for plan generation.
type Result struct {
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
		Description: "Generates Goal/Context/Scope for plans via agentic-dispatch coordinator",
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

// resolveProvider determines the LLM provider for prompt formatting.
func (c *Component) resolveProvider() prompt.Provider {
	capability := c.config.DefaultCapability
	if capability == "" {
		capability = string(model.CapabilityPlanning)
	}
	modelName := c.modelRegistry.Resolve(model.Capability(capability))
	if endpoint := c.modelRegistry.GetEndpoint(modelName); endpoint != nil {
		return prompt.Provider(endpoint.Provider)
	}
	return prompt.ProviderOllama
}
