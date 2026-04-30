// Package architecturegenerator provides a processor that either generates an
// architecture document for a plan via agentic-dispatch, or immediately
// passes through to architecture_generated when plan.SkipArchitecture is true.
//
// The component watches PLAN_STATES for plans reaching requirements_generated,
// claims the plan by transitioning to generating_architecture, and then either:
//   - (SkipArchitecture == true) publishes plan.mutation.architecture.generated immediately
//   - (SkipArchitecture == false) dispatches architect agent via agentic-dispatch
//
// Plan-manager is the single writer — this component only publishes mutations.
package architecturegenerator

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
	"github.com/c360studio/semspec/tools/terminal"
	workflowtools "github.com/c360studio/semspec/tools/workflow"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/dispatchretry"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semspec/workflow/lessons"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	ssmodel "github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// mutationArchitectureGenerated is the subject for architecture mutation requests.
	mutationArchitectureGenerated = "plan.mutation.architecture.generated"

	// workflowSlugPlanning identifies planning workflows in agent TaskMessages.
	workflowSlugPlanning = "semspec-planning"

	// stepArchitectureGeneration is the workflow step for architecture generation.
	stepArchitectureGeneration = "architecture-generation"

	// subjectArchitectureTask is the NATS subject for architecture-generator agent tasks.
	subjectArchitectureTask = "agent.task.architecture-generation"
)

// Component implements the architecture-generator processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	modelRegistry ssmodel.RegistryReader
	toolRegistry  component.ToolRegistryReader
	assembler     *prompt.Assembler
	lessonWriter  *lessons.Writer

	// Lifecycle
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// retry tracks per-plan attempts keyed by slug; payload is *workflow.Plan.
	retry *dispatchretry.State

	// Metrics
	triggersProcessed  atomic.Int64
	generationsSkipped atomic.Int64
	generationsFailed  atomic.Int64
	lastActivityMu     sync.RWMutex
	lastActivity       time.Time
}

// ---------------------------------------------------------------------------
// Factory
// ---------------------------------------------------------------------------

// NewComponent creates a new architecture-generator processor.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Apply defaults for any zero-value fields.
	defaults := DefaultConfig()
	if config.DefaultCapability == "" {
		config.DefaultCapability = defaults.DefaultCapability
	}
	if config.PlanStateBucket == "" {
		config.PlanStateBucket = defaults.PlanStateBucket
	}
	if config.MaxGenerationRetries == 0 {
		config.MaxGenerationRetries = defaults.MaxGenerationRetries
	}
	if config.RetryBackoffMs <= 0 {
		config.RetryBackoffMs = defaults.RetryBackoffMs
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
	registry.Register(prompt.GraphManifestFragment(workflowtools.RegistrySummaryFetchFn()))
	assembler := prompt.NewAssembler(registry)

	tw := &graphutil.TripleWriter{
		NATSClient:    deps.NATSClient,
		Logger:        logger,
		ComponentName: "architecture-generator",
	}

	return &Component{
		name:          "architecture-generator",
		config:        config,
		natsClient:    deps.NATSClient,
		logger:        logger,
		modelRegistry: deps.ModelRegistry,
		toolRegistry:  deps.ToolRegistry,
		assembler:     assembler,
		lessonWriter:  &lessons.Writer{TW: tw, Logger: logger},
		retry: dispatchretry.New(dispatchretry.Config{
			MaxRetries: config.MaxGenerationRetries,
			BackoffMs:  config.RetryBackoffMs,
		}),
	}, nil
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized architecture-generator",
		"plan_state_bucket", c.config.PlanStateBucket)
	return nil
}

// Start begins processing architecture generation triggers.
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

	// Watch PLAN_STATES for plans reaching "requirements_generated" status.
	// This is the KV twofer — the write IS the trigger.
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("cannot get JetStream: %w", err)
	}

	go c.watchPlanStates(subCtx, js)

	// Loop completion watcher — picks up agentic-dispatch results from AGENT_LOOPS KV.
	go c.watchLoopCompletions(subCtx)

	c.logger.Info("architecture-generator started",
		"plan_state_bucket", c.config.PlanStateBucket)

	return nil
}

func (c *Component) rollbackStart(cancel context.CancelFunc) {
	c.mu.Lock()
	c.running = false
	c.cancel = nil
	c.mu.Unlock()
	cancel()
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

	c.logger.Info("architecture-generator stopped",
		"triggers_processed", c.triggersProcessed.Load(),
		"generations_skipped", c.generationsSkipped.Load(),
		"generations_failed", c.generationsFailed.Load())

	return nil
}

// ---------------------------------------------------------------------------
// KV watcher
// ---------------------------------------------------------------------------

// watchPlanStates watches PLAN_STATES for plans reaching requirements_generated.
// The KV value carries plan inline — no follow-up query needed.
func (c *Component) watchPlanStates(ctx context.Context, js jetstream.JetStream) {
	bucket, err := workflow.WaitForKVBucket(ctx, js, c.config.PlanStateBucket)
	if err != nil {
		c.logger.Warn("PLAN_STATES not available — architecture-generator watcher disabled",
			"bucket", c.config.PlanStateBucket, "error", err)
		return
	}

	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		c.logger.Warn("Failed to watch PLAN_STATES", "error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Watching PLAN_STATES for requirements_generated")

	for entry := range watcher.Updates() {
		if entry == nil {
			continue
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}

		var plan workflow.Plan
		if json.Unmarshal(entry.Value(), &plan) != nil {
			continue
		}
		if plan.Status != workflow.StatusRequirementsGenerated {
			continue
		}

		// Claim the plan to prevent re-trigger on watcher restarts.
		if !workflow.ClaimPlanStatus(ctx, c.natsClient, plan.Slug, workflow.StatusGeneratingArchitecture, c.logger) {
			continue
		}

		c.triggersProcessed.Add(1)
		c.updateLastActivity()

		go c.processArchitecturePhase(ctx, &plan)
	}
}

// processArchitecturePhase handles the architecture phase for a single plan.
// For plans with SkipArchitecture=true, it publishes the mutation immediately.
// For all others, it dispatches the architect agent via agentic-dispatch.
func (c *Component) processArchitecturePhase(ctx context.Context, plan *workflow.Plan) {
	if plan.SkipArchitecture {
		c.logger.Info("Skipping architecture phase (simple plan)",
			"slug", plan.Slug)
		c.generationsSkipped.Add(1)
		if err := c.publishArchitectureGenerated(ctx, plan.Slug, nil); err != nil {
			c.generationsFailed.Add(1)
			c.logger.Error("Failed to skip architecture phase, rejecting plan",
				"slug", plan.Slug, "error", err)
			c.sendGenerationFailed(ctx, plan.Slug, fmt.Sprintf("architecture skip mutation failed: %v", err))
		}
		return
	}

	c.retry.Track(plan.Slug, plan)
	c.dispatchArchitectureGenerator(ctx, plan, "")
}

// ---------------------------------------------------------------------------
// Agent dispatch
// ---------------------------------------------------------------------------

// dispatchArchitectureGenerator dispatches an architecture-generator agent loop
// via agentic-dispatch. previousError is non-empty on retry attempts.
func (c *Component) dispatchArchitectureGenerator(ctx context.Context, plan *workflow.Plan, previousError string) {
	c.updateLastActivity()

	taskID := fmt.Sprintf("archgen-%s-%s", plan.Slug, uuid.New().String())
	c.retry.SetActiveLoop(plan.Slug, taskID)

	// Build requirement summaries for the prompt.
	reqSummaries := make([]prompt.ExistingRequirementSummary, len(plan.Requirements))
	for i, r := range plan.Requirements {
		reqSummaries[i] = prompt.ExistingRequirementSummary{
			ID:          r.ID,
			Title:       r.Title,
			Description: r.Description,
		}
	}

	archCtx := &prompt.ArchitectPromptContext{
		Goal:           plan.Goal,
		PlanContext:    plan.Context,
		ScopeInclude:   plan.Scope.Include,
		ScopeExclude:   plan.Scope.Exclude,
		ScopeProtected: plan.Scope.DoNotTouch,
		Requirements:   reqSummaries,
		PreviousError:  previousError,
	}

	// Resolve model for architecture capability.
	capability := c.config.DefaultCapability
	if capability == "" {
		capability = string(model.CapabilityArchitecture)
	}
	modelName := c.modelRegistry.Resolve(capability)

	// Assemble system prompt via fragment pipeline.
	provider := c.resolveProvider()
	var maxTokens int
	if c.modelRegistry != nil {
		if ep := c.modelRegistry.GetEndpoint(modelName); ep != nil {
			maxTokens = ep.MaxTokens
		}
	}
	asmCtx := &prompt.AssemblyContext{
		Role:            prompt.RoleArchitect,
		Provider:        provider,
		Domain:          "software",
		AvailableTools:  prompt.FilterTools(c.availableToolNames(), prompt.RoleArchitect),
		SupportsTools:   true,
		MaxTokens:       maxTokens,
		Persona:         prompt.GlobalPersonas().ForRole(prompt.RoleArchitect),
		Vocabulary:      prompt.GlobalPersonas().Vocabulary(),
		ArchitectPrompt: archCtx,
	}

	// Wire role-scoped lessons learned.
	if c.lessonWriter != nil {
		graphCtx := context.WithoutCancel(ctx)
		if roleLessons, err := c.lessonWriter.RotateLessonsForRole(graphCtx, "architect", 10); err == nil && len(roleLessons) > 0 {
			tk := &prompt.LessonsLearned{}
			for _, les := range roleLessons {
				tk.Lessons = append(tk.Lessons, prompt.LessonEntry{
					Category:      les.Source,
					Summary:       les.Summary,
					InjectionForm: les.InjectionForm,
					Role:          les.Role,
				})
			}
			asmCtx.LessonsLearned = tk
		}
	}

	assembled := c.assembler.Assemble(asmCtx)
	if assembled.RenderError != nil {
		c.logger.Error("Architect user-prompt render failed", "slug", plan.Slug, "error", assembled.RenderError)
		return
	}

	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleGeneral,
		Model:        modelName,
		Prompt:       assembled.UserMessage,
		Tools:        terminal.ToolsForDeliverable(c.toolRegistry, "architecture", c.availableToolNames()...),
		ToolChoice:   &agentic.ToolChoice{Mode: "required"},
		WorkflowSlug: workflowSlugPlanning,
		WorkflowStep: stepArchitectureGeneration,
		Context: &agentic.ConstructedContext{
			Content: assembled.SystemMessage,
		},
		Metadata: map[string]any{
			"plan_slug":        plan.Slug,
			"deliverable_type": "architecture",
			"task_id":          "main", // architect reads repo root, no isolated worktree
		},
	}

	baseMsg := message.NewBaseMessage(task.Schema(), task, "semspec-architecture-generator")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to marshal task message, rejecting plan",
			"slug", plan.Slug, "error", err)
		c.sendGenerationFailed(ctx, plan.Slug, fmt.Sprintf("architecture dispatch marshal failed: %v", err))
		return
	}

	if err := c.natsClient.PublishToStream(ctx, subjectArchitectureTask, data); err != nil {
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to dispatch architecture generator, rejecting plan",
			"slug", plan.Slug, "error", err)
		c.sendGenerationFailed(ctx, plan.Slug, fmt.Sprintf("architecture dispatch failed: %v", err))
		return
	}

	c.logger.Info("Dispatched architecture generator agent",
		"slug", plan.Slug,
		"task_id", taskID,
		"model", modelName,
		"fragments", len(assembled.FragmentsUsed),
		"system_chars", assembled.SystemMessageChars)
}

// ---------------------------------------------------------------------------
// Loop completion watcher
// ---------------------------------------------------------------------------

// watchLoopCompletions watches the AGENT_LOOPS KV bucket for architecture-generator
// agent completions. When a loop reaches terminal state with WorkflowSlug
// matching the planning workflow and WorkflowStep matching architecture-generation,
// the result is parsed and a mutation is sent to plan-manager.
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

	c.logger.Info("Loop completion watcher started (watching AGENT_LOOPS for architecture-generation)")

	replayDone := false
	processedLoops := make(map[string]bool)

	for entry := range watcher.Updates() {
		if entry == nil {
			// End of initial KV replay. Subsequent entries are live updates.
			replayDone = true
			c.logger.Info("AGENT_LOOPS replay complete for architecture-generator")
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
		if loop.WorkflowSlug != workflowSlugPlanning {
			continue
		}
		if loop.WorkflowStep != stepArchitectureGeneration {
			continue
		}

		slug, _ := loop.Metadata["plan_slug"].(string)
		if slug == "" {
			continue
		}

		// During replay, skip mutation publishing — these loops already
		// produced mutations before the restart.
		if !replayDone {
			c.logger.Debug("Replay: skipping completed architecture-generation loop",
				"slug", slug, "loop_id", loop.ID)
			continue
		}

		// Dedup: skip loops we have already processed in this session.
		if processedLoops[loop.ID] {
			c.logger.Debug("Skipping already-processed loop",
				"loop_id", loop.ID, "slug", slug)
			continue
		}
		processedLoops[loop.ID] = true

		c.handleLoopCompletion(ctx, &loop, slug)
	}
}

// handleLoopCompletion processes a completed architecture-generator agent loop.
// It parses the ArchitectureDocument from the loop result and sends a mutation
// to plan-manager via plan.mutation.architecture.generated.
func (c *Component) handleLoopCompletion(ctx context.Context, loop *agentic.LoopEntity, slug string) {
	c.updateLastActivity()

	// Stale loop guard: drop completions from older dispatches that race a retry.
	if c.retry.IsStaleLoop(slug, loop.TaskID) {
		c.logger.Debug("Dropping stale architecture loop completion (task ID mismatch)",
			"slug", slug, "loop_task_id", loop.TaskID, "loop_id", loop.ID)
		return
	}

	if loop.Outcome != agentic.OutcomeSuccess {
		c.generationsFailed.Add(1)
		loopErrorMsg := loop.Error
		if loopErrorMsg == "" {
			loopErrorMsg = fmt.Sprintf("agent loop ended with outcome %q", loop.Outcome)
		}
		c.logger.Error("Architecture generator agent loop failed",
			"slug", slug,
			"loop_id", loop.ID,
			"outcome", loop.Outcome,
			"error", loopErrorMsg)
		c.retryOrFail(ctx, slug, loopErrorMsg)
		return
	}

	architecture, err := parseArchitectureFromResult(loop.Result)
	if err != nil {
		c.generationsFailed.Add(1)
		parseErrorMsg := fmt.Sprintf("failed to parse architecture: %s", err.Error())
		c.logger.Error("Failed to parse architecture from agent result",
			"slug", slug,
			"loop_id", loop.ID,
			"error", err)
		c.retryOrFail(ctx, slug, parseErrorMsg)
		return
	}

	// Check if the plan has moved past generating_architecture while we were working.
	// If so, our result is stale — discard it without rejecting the plan.
	if kvPlan, loadErr := c.loadPlanFromKV(ctx, slug); loadErr == nil {
		status := kvPlan.EffectiveStatus()
		if status != workflow.StatusGeneratingArchitecture {
			c.logger.Warn("Plan advanced past generating_architecture, discarding stale result",
				"slug", slug,
				"current_status", status,
				"loop_id", loop.ID)
			c.retry.Clear(slug)
			return
		}
	}

	if err := c.publishArchitectureGenerated(ctx, slug, architecture); err != nil {
		// If the plan advanced past generating_architecture, this is NOT a generation
		// failure — our result is simply stale. Discard without rejecting.
		if strings.Contains(err.Error(), "invalid transition") {
			c.logger.Warn("Architecture mutation rejected (plan advanced), discarding stale result",
				"slug", slug,
				"error", err,
				"loop_id", loop.ID)
			c.retry.Clear(slug)
			return
		}
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to publish architecture mutation, rejecting plan",
			"slug", slug, "loop_id", loop.ID, "error", err)
		c.sendGenerationFailed(ctx, slug, fmt.Sprintf("architecture mutation publish failed: %v", err))
		return
	}

	// Clean up retry state on success.
	c.retry.Clear(slug)

	c.logger.Info("Architecture generated via agentic-dispatch and mutation accepted",
		"slug", slug,
		"loop_id", loop.ID,
		"tech_choices", len(architecture.TechnologyChoices),
		"components", len(architecture.ComponentBoundaries),
		"decisions", len(architecture.Decisions))
}

// retryOrFail attempts to re-dispatch architecture generation with the error
// message appended to the prompt. When the maximum retry count is exceeded it
// sends a generation.failed mutation to reject the plan.
//
// On cold start (no in-memory retry entry), reconstruct from PLAN_STATES and
// Track before Tick — the helper is unaware of NATS/KV by design.
func (c *Component) retryOrFail(ctx context.Context, slug, errorMsg string) {
	if _, ok := c.retry.Snapshot(slug); !ok {
		plan, err := c.loadPlanFromKV(ctx, slug)
		if err != nil {
			c.logger.Warn("retryOrFail: no retry state and PLAN_STATES recovery failed, failing immediately",
				"slug", slug, "error", err)
			c.sendGenerationFailed(ctx, slug, errorMsg)
			return
		}
		c.logger.Info("Recovered plan from PLAN_STATES after restart", "slug", slug)
		c.retry.Track(slug, plan)
	}

	entry, retryOK := c.retry.Tick(ctx, slug)
	if entry == nil {
		if ctx.Err() != nil {
			c.logger.Debug("retryOrFail aborted during backoff", "slug", slug, "error", ctx.Err())
			return
		}
		c.logger.Warn("retryOrFail: lost retry context, failing immediately", "slug", slug)
		c.sendGenerationFailed(ctx, slug, errorMsg)
		return
	}

	plan, _ := entry.Payload.(*workflow.Plan)

	if !retryOK {
		c.logger.Warn("Architecture generation exhausted retries",
			"slug", slug,
			"attempts", entry.Count,
			"max", c.config.MaxGenerationRetries,
			"last_error", errorMsg)
		c.sendGenerationFailed(ctx, slug, errorMsg)
		return
	}

	c.logger.Info("Retrying architecture generation",
		"slug", slug,
		"attempt", entry.Count,
		"max", c.config.MaxGenerationRetries,
		"previous_error", errorMsg)

	c.dispatchArchitectureGenerator(ctx, plan, errorMsg)
}

// sendGenerationFailed publishes plan.mutation.generation.failed to reject the plan.
func (c *Component) sendGenerationFailed(ctx context.Context, slug, feedback string) {
	failReq, _ := json.Marshal(map[string]string{
		"slug":  slug,
		"phase": "architecture-generation",
		"error": feedback,
	})
	if _, err := c.natsClient.RequestWithRetry(ctx, "plan.mutation.generation.failed", failReq,
		10*time.Second, natsclient.DefaultRetryConfig()); err != nil {
		c.logger.Warn("Failed to publish generation.failed mutation",
			"slug", slug, "error", err)
	}
}

// loadPlanFromKV reads a plan from the PLAN_STATES KV bucket for restart recovery.
func (c *Component) loadPlanFromKV(ctx context.Context, slug string) (*workflow.Plan, error) {
	js, err := c.natsClient.JetStream()
	if err != nil {
		return nil, fmt.Errorf("no JetStream: %w", err)
	}
	bucket, err := js.KeyValue(ctx, c.config.PlanStateBucket)
	if err != nil {
		return nil, fmt.Errorf("PLAN_STATES bucket: %w", err)
	}
	entry, err := bucket.Get(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("get plan %q: %w", slug, err)
	}
	var plan workflow.Plan
	if err := json.Unmarshal(entry.Value(), &plan); err != nil {
		return nil, fmt.Errorf("unmarshal plan %q: %w", slug, err)
	}
	return &plan, nil
}

// ---------------------------------------------------------------------------
// Result parsing
// ---------------------------------------------------------------------------

// parseArchitectureFromResult extracts an ArchitectureDocument from an agent
// loop result string. The deliverable is already validated by submit_work,
// so direct unmarshal should succeed.
func parseArchitectureFromResult(result string) (*workflow.ArchitectureDocument, error) {
	if result == "" {
		return nil, fmt.Errorf("empty result")
	}

	var doc workflow.ArchitectureDocument
	if err := json.Unmarshal([]byte(result), &doc); err != nil {
		return nil, fmt.Errorf("parse architecture JSON: %w", err)
	}

	if len(doc.TechnologyChoices) == 0 && len(doc.ComponentBoundaries) == 0 && len(doc.Decisions) == 0 {
		return nil, fmt.Errorf("architecture document is empty — no technology choices, components, or decisions")
	}

	return &doc, nil
}

// ---------------------------------------------------------------------------
// Mutation publishing
// ---------------------------------------------------------------------------

// publishArchitectureGenerated sends plan.mutation.architecture.generated to plan-manager.
// architecture is nil for the skip path.
func (c *Component) publishArchitectureGenerated(ctx context.Context, slug string, architecture *workflow.ArchitectureDocument) error {
	mutReq := struct {
		Slug         string                         `json:"slug"`
		Architecture *workflow.ArchitectureDocument `json:"architecture,omitempty"`
	}{
		Slug:         slug,
		Architecture: architecture,
	}

	data, err := json.Marshal(mutReq)
	if err != nil {
		return fmt.Errorf("marshal architecture mutation: %w", err)
	}

	resp, err := c.natsClient.RequestWithRetry(ctx, mutationArchitectureGenerated, data,
		10*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		return fmt.Errorf("send architecture mutation: %w", err)
	}

	var mutResp struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(resp, &mutResp); err != nil {
		return fmt.Errorf("unmarshal architecture mutation response: %w", err)
	}
	if !mutResp.Success {
		return fmt.Errorf("plan-manager rejected architecture mutation: %s", mutResp.Error)
	}

	c.logger.Info("Architecture phase mutation accepted by plan-manager", "slug", slug)
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// resolveProvider determines the LLM provider from the model registry.
func (c *Component) resolveProvider() prompt.Provider {
	capability := c.config.DefaultCapability
	if capability == "" {
		capability = string(model.CapabilityArchitecture)
	}
	modelName := c.modelRegistry.Resolve(capability)
	if endpoint := c.modelRegistry.GetEndpoint(modelName); endpoint != nil {
		return prompt.Provider(endpoint.Provider)
	}
	return prompt.ProviderOllama
}

// availableToolNames returns the full list of tool names for prompt assembly.
// Actual tool availability is controlled by agentic-tools at runtime.
func (c *Component) availableToolNames() []string {
	return []string{
		"bash", "submit_work",
	}
}

// ---------------------------------------------------------------------------
// Component.Discoverable implementation
// ---------------------------------------------------------------------------

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "architecture-generator",
		Type:        "processor",
		Description: "Generates architecture documents or passes through for simple plans",
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
	return architectureGeneratorSchema
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
