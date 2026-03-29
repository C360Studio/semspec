// Package requirementgenerator provides a processor that decomposes approved plans
// into structured Requirements by dispatching a requirement-generator agent through
// agentic-dispatch.
//
// The agent explores the codebase via graph and bash tools, reads the plan, and
// produces a JSON array of requirements. This replaces the previous inline
// llmClient.Complete() path which had zero codebase visibility.
package requirementgenerator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// stepRequirementGeneration is the workflow step for requirement generation.
	stepRequirementGeneration = "requirement-generation"

	// subjectRequirementGenerationTask is the NATS subject for requirement-generation agent tasks.
	subjectRequirementGenerationTask = "agent.task.requirement-generation"
)

// RequirementsGeneratedType is the message type for requirements-generated events.
// This matches the type consumed by plan-api's dispatchCascadeEvent handler.
var RequirementsGeneratedType = message.Type{
	Domain:   "workflow",
	Category: "requirements-generated",
	Version:  "v1",
}

// requirementsGeneratedPayload wraps workflow.RequirementsGeneratedEvent to satisfy
// the message.Payload interface required by message.NewBaseMessage.
// The JSON layout is identical to RequirementsGeneratedEvent so plan-manager's
// ParseReactivePayload[workflow.RequirementsGeneratedEvent] can deserialise it.
type requirementsGeneratedPayload struct {
	Slug             string                 `json:"slug"`
	Requirements     []workflow.Requirement `json:"requirements"`
	RequirementCount int                    `json:"requirement_count"`
	TraceID          string                 `json:"trace_id,omitempty"`
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

// requirementItem is the agent-generated JSON shape for a single requirement.
// The agent is instructed to output an array of these objects.
type requirementItem struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// pendingDispatch records metadata for an in-flight requirement-generation dispatch.
// Used to reconstruct the publishResults call when the loop completes.
type pendingDispatch struct {
	trigger *payloads.RequirementGeneratorRequest
}

// Component implements the requirement-generator processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	modelRegistry *model.Registry
	assembler     *prompt.Assembler

	// pending maps taskID → original trigger for in-flight dispatches.
	pendingMu sync.RWMutex
	pending   map[string]*pendingDispatch

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
		name:          "requirement-generator",
		config:        config,
		natsClient:    deps.NATSClient,
		logger:        logger,
		modelRegistry: model.Global(),
		assembler:     assembler,
		pending:       make(map[string]*pendingDispatch),
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
		return fmt.Errorf("consume requirement triggers: %w", err)
	}

	// Watch PLAN_STATES for plans reaching "approved" status.
	// This is the KV twofer — the write IS the trigger.
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Warn("Cannot start PLAN_STATES watcher: no JetStream", "error", err)
	} else {
		go c.watchPlanStates(subCtx, js)
	}

	// Loop completion watcher — picks up agentic-dispatch results from AGENT_LOOPS KV.
	go c.watchLoopCompletions(subCtx)

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

// handleMessagePush is the push-based callback for ConsumeStreamWithConfig.
// Messages arrive immediately when published — no polling delay.
func (c *Component) handleMessagePush(ctx context.Context, msg jetstream.Msg) {
	c.handleMessage(ctx, msg)
}

// handleMessage processes a single requirement-generator trigger.
// ACKs immediately and dispatches an agent loop — retries are handled by the loop.
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

	c.logger.Info("Processing requirement-generator trigger",
		"slug", trigger.Slug,
		"trace_id", trigger.TraceID)

	// ACK immediately — the agent loop handles retries internally.
	if err := msg.Ack(); err != nil {
		c.logger.Warn("Failed to ACK message", "error", err)
	}

	c.dispatchRequirementGenerator(ctx, trigger)
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

// dispatchRequirementGenerator dispatches a requirement-generator agent loop via
// agentic-dispatch. The agent reads the plan, explores the codebase, and outputs
// a JSON array of requirements.
func (c *Component) dispatchRequirementGenerator(ctx context.Context, trigger *payloads.RequirementGeneratorRequest) {
	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	taskID := fmt.Sprintf("reqgen-%s-%s", trigger.Slug, uuid.New().String())

	userPrompt := c.buildUserPrompt(trigger)

	// Resolve model for planning capability.
	capability := c.config.DefaultCapability
	if capability == "" {
		capability = string(model.CapabilityPlanning)
	}
	modelName := c.modelRegistry.Resolve(model.Capability(capability))

	// Assemble system prompt via fragment pipeline.
	provider := c.resolveProvider()
	assembled := c.assembler.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleRequirementGenerator,
		Provider:       provider,
		Domain:         "software",
		AvailableTools: prompt.FilterTools(c.availableToolNames(), prompt.RoleRequirementGenerator),
		SupportsTools:  true,
	})

	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleGeneral,
		Model:        modelName,
		Prompt:       userPrompt,
		WorkflowSlug: workflow.WorkflowSlugPlanning,
		WorkflowStep: stepRequirementGeneration,
		Context: &agentic.ConstructedContext{
			Content: assembled.SystemMessage,
		},
		Metadata: map[string]any{
			"plan_slug": trigger.Slug,
		},
	}

	// Record the pending dispatch before publishing so the completion watcher
	// can look up the original trigger when the loop finishes.
	c.pendingMu.Lock()
	c.pending[taskID] = &pendingDispatch{trigger: trigger}
	c.pendingMu.Unlock()

	baseMsg := message.NewBaseMessage(task.Schema(), task, "semspec-requirement-generator")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.pendingMu.Lock()
		delete(c.pending, taskID)
		c.pendingMu.Unlock()
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to marshal task message", "slug", trigger.Slug, "error", err)
		return
	}

	if err := c.natsClient.PublishToStream(ctx, subjectRequirementGenerationTask, data); err != nil {
		c.pendingMu.Lock()
		delete(c.pending, taskID)
		c.pendingMu.Unlock()
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to dispatch requirement generator", "slug", trigger.Slug, "error", err)
		return
	}

	c.logger.Info("Dispatched requirement generator agent",
		"slug", trigger.Slug,
		"task_id", taskID,
		"model", modelName,
		"fragments", len(assembled.FragmentsUsed))
}

// buildUserPrompt constructs the user prompt for the requirement-generator agent.
// For partial regeneration (ReplaceRequirementIDs set), it includes the approved
// requirements and rejection reasons so the agent only regenerates replacements.
func (c *Component) buildUserPrompt(trigger *payloads.RequirementGeneratorRequest) string {
	var sb strings.Builder

	sb.WriteString("## Plan to Decompose\n\n")
	if trigger.Title != "" {
		sb.WriteString(fmt.Sprintf("**Title**: %s\n\n", trigger.Title))
	}
	if trigger.Goal != "" {
		sb.WriteString(fmt.Sprintf("**Goal**: %s\n\n", trigger.Goal))
	}
	if trigger.Context != "" {
		sb.WriteString(fmt.Sprintf("**Context**: %s\n\n", trigger.Context))
	}
	if trigger.Scope != nil {
		if len(trigger.Scope.Include) > 0 {
			sb.WriteString(fmt.Sprintf("**Scope Include**: %s\n\n", strings.Join(trigger.Scope.Include, ", ")))
		}
		if len(trigger.Scope.Exclude) > 0 {
			sb.WriteString(fmt.Sprintf("**Scope Exclude**: %s\n\n", strings.Join(trigger.Scope.Exclude, ", ")))
		}
		if len(trigger.Scope.DoNotTouch) > 0 {
			sb.WriteString(fmt.Sprintf("**Do Not Touch**: %s\n\n", strings.Join(trigger.Scope.DoNotTouch, ", ")))
		}
	}

	if len(trigger.ReplaceRequirementIDs) > 0 {
		sb.WriteString("## Existing Approved Requirements (DO NOT regenerate these)\n\n")
		for _, r := range trigger.ExistingRequirements {
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
		sb.WriteString("\nGenerate ONLY replacement requirements for the rejected IDs above.\n")
	} else {
		sb.WriteString("Decompose the above plan into a JSON array of requirements. Each requirement should represent a distinct behavioral intent that can be independently verified.\n")
	}

	return sb.String()
}

// watchLoopCompletions watches the AGENT_LOOPS KV bucket for requirement-generation
// agent completions. When a loop reaches terminal state with WorkflowStep matching
// our step, the result is parsed and the requirements mutation is sent.
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

	c.logger.Info("Loop completion watcher started (watching AGENT_LOOPS for requirement-generation)")

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
		if loop.WorkflowStep != stepRequirementGeneration {
			continue
		}

		slug, _ := loop.Metadata["plan_slug"].(string)
		if slug == "" {
			continue
		}

		// Look up the original trigger from the pending map.
		c.pendingMu.RLock()
		dp, ok := c.pending[loop.TaskID]
		c.pendingMu.RUnlock()

		if !ok {
			// This can happen on restart — we lost the in-memory pending map.
			// Log and skip; a future retry or re-trigger is needed.
			c.logger.Warn("No pending dispatch found for completed loop",
				"task_id", loop.TaskID,
				"slug", slug)
			continue
		}

		c.pendingMu.Lock()
		delete(c.pending, loop.TaskID)
		c.pendingMu.Unlock()

		c.handleLoopCompletion(ctx, &loop, slug, dp.trigger)
	}
}

// handleLoopCompletion processes a completed requirement-generation agent loop.
// It parses the requirements from the loop result and calls publishResults.
func (c *Component) handleLoopCompletion(ctx context.Context, loop *agentic.LoopEntity, slug string, trigger *payloads.RequirementGeneratorRequest) {
	c.updateLastActivity()

	if loop.Outcome != agentic.OutcomeSuccess {
		c.generationsFailed.Add(1)
		c.logger.Error("Requirement generation agent loop failed",
			"slug", slug,
			"loop_id", loop.ID,
			"outcome", loop.Outcome,
			"error", loop.Error)
		return
	}

	items, err := parseRequirementsFromResult(loop.Result)
	if err != nil {
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to parse requirements from agent result",
			"slug", slug,
			"loop_id", loop.ID,
			"error", err)
		return
	}

	// For partial regen, new requirement IDs must not collide with existing ones.
	// Determine the starting sequence offset from the current requirements count.
	seqOffset := 0
	if len(trigger.ReplaceRequirementIDs) > 0 {
		seqOffset = len(trigger.ExistingRequirements)
	}

	// Convert agent items to workflow.Requirement structs.
	planID := workflow.PlanEntityID(slug)
	now := time.Now()
	requirements := make([]workflow.Requirement, 0, len(items))
	for i, item := range items {
		requirements = append(requirements, workflow.Requirement{
			ID:          fmt.Sprintf("requirement.%s.%d", slug, seqOffset+i+1),
			PlanID:      planID,
			Title:       item.Title,
			Description: item.Description,
			Status:      workflow.RequirementStatusActive,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}

	if err := c.publishResults(ctx, trigger, requirements); err != nil {
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to publish requirements from loop completion",
			"slug", slug,
			"loop_id", loop.ID,
			"error", err)
		return
	}

	c.requirementsGenerated.Add(1)
	c.logger.Info("Requirements generated via agentic-dispatch and mutation accepted",
		"slug", slug,
		"loop_id", loop.ID,
		"requirement_count", len(requirements))
}

// parseRequirementsFromResult extracts a slice of requirementItem from an agent
// loop result string. The result may be a JSON array, a wrapped object, or
// markdown-fenced JSON.
func parseRequirementsFromResult(result string) ([]requirementItem, error) {
	if result == "" {
		return nil, fmt.Errorf("empty result")
	}

	// Try direct array parse first.
	var items []requirementItem
	if err := json.Unmarshal([]byte(result), &items); err == nil && len(items) > 0 {
		return validateRequirementItems(items)
	}

	// Try object with "requirements" key.
	var wrapper struct {
		Requirements []requirementItem `json:"requirements"`
	}
	if err := json.Unmarshal([]byte(result), &wrapper); err == nil && len(wrapper.Requirements) > 0 {
		return validateRequirementItems(wrapper.Requirements)
	}

	// Try extracting JSON from markdown fences.
	jsonContent := extractJSONArray(result)
	if jsonContent == "" {
		jsonContent = extractJSONObject(result)
	}
	if jsonContent == "" {
		return nil, fmt.Errorf("no JSON found in result")
	}

	if err := json.Unmarshal([]byte(jsonContent), &items); err == nil && len(items) > 0 {
		return validateRequirementItems(items)
	}

	if err := json.Unmarshal([]byte(jsonContent), &wrapper); err == nil && len(wrapper.Requirements) > 0 {
		return validateRequirementItems(wrapper.Requirements)
	}

	return nil, fmt.Errorf("could not parse requirements from result (length %d)", len(result))
}

// validateRequirementItems checks that each item has a non-empty title.
func validateRequirementItems(items []requirementItem) ([]requirementItem, error) {
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

// extractJSONArray finds and returns the first JSON array in s, tolerating
// markdown code fences.
func extractJSONArray(s string) string {
	start := strings.Index(s, "[")
	if start < 0 {
		return ""
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// extractJSONObject finds and returns the first JSON object in s, tolerating
// markdown code fences.
func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
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

// publishResults merges (for partial regen) and sends requirements to plan-manager
// via request/reply (KV twofer — manager writes, watchers react).
func (c *Component) publishResults(ctx context.Context, trigger *payloads.RequirementGeneratorRequest, requirements []workflow.Requirement) error {
	// Partial regeneration: merge new requirements with existing approved ones
	// carried in the trigger payload (no graph reads needed).
	if len(trigger.ReplaceRequirementIDs) > 0 && len(trigger.ExistingRequirements) > 0 {
		replaceSet := make(map[string]bool, len(trigger.ReplaceRequirementIDs))
		for _, id := range trigger.ReplaceRequirementIDs {
			replaceSet[id] = true
		}
		// Keep existing requirements that aren't being replaced, append new ones.
		var merged []workflow.Requirement
		for _, existing := range trigger.ExistingRequirements {
			if !replaceSet[existing.ID] {
				merged = append(merged, existing)
			}
		}
		requirements = append(merged, requirements...)
	}

	// Send results to plan-manager via request/reply.
	mutationReq := struct {
		Slug         string                 `json:"slug"`
		Requirements []workflow.Requirement `json:"requirements"`
		TraceID      string                 `json:"trace_id,omitempty"`
	}{
		Slug:         trigger.Slug,
		Requirements: requirements,
		TraceID:      trigger.TraceID,
	}

	data, err := json.Marshal(mutationReq)
	if err != nil {
		return fmt.Errorf("marshal requirements mutation: %w", err)
	}

	resp, err := c.natsClient.RequestWithRetry(ctx, "plan.mutation.requirements.generated", data, 10*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		return fmt.Errorf("requirements mutation request: %w", err)
	}

	var mutResp struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(resp, &mutResp); err != nil || !mutResp.Success {
		errMsg := "unknown error"
		if err != nil {
			errMsg = err.Error()
		} else if mutResp.Error != "" {
			errMsg = mutResp.Error
		}
		return fmt.Errorf("requirements mutation failed: %s", errMsg)
	}

	c.logger.Info("Requirements sent to plan-manager via mutation",
		"slug", trigger.Slug,
		"requirement_count", len(requirements))

	return nil
}

// availableToolNames returns the full list of tool names for prompt assembly.
// Actual tool availability is controlled by agentic-tools at runtime.
func (c *Component) availableToolNames() []string {
	return []string{
		"bash", "submit_work", "ask_question",
		"graph_search", "graph_query",
	}
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
		Description: "Generates Requirements for approved plans via agentic-dispatch",
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
