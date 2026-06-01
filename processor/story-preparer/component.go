// Package storypreparer provides Sarah, the BMAD product-owner processor
// (ADR-043 Move 3). She watches PLAN_STATES for plans reaching
// architecture_generated, claims preparing_stories, dispatches an LLM agent
// loop via agentic-dispatch, validates the resulting Stories +
// Tasks against workflow.ValidateStories, and publishes
// plan.mutation.stories.generated for plan-manager (the single writer) to
// persist + transition to ready_for_execution.
//
// ADR-043 PR 3 ships the component dormant — Config.Enabled defaults to
// false so the existing architecture_generated → scenarios_generated flow
// is unchanged. PR 4 flips Enabled and reworks scenario-generator +
// execution-manager to consume Stories.
package storypreparer

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
	// mutationStoriesGenerated is the subject plan-manager listens on for
	// stories.generated mutation requests. plan-manager persists Plan.Stories
	// and transitions the plan from preparing_stories to ready_for_execution.
	mutationStoriesGenerated = "plan.mutation.stories.generated"

	// workflowSlugPlanning identifies planning workflows in agent TaskMessages.
	// Shared with architecture-generator + other planning-phase generators.
	workflowSlugPlanning = "semspec-planning"

	// stepStoryPreparation is the workflow step for story preparation.
	stepStoryPreparation = "story-preparation"

	// subjectStoryTask is the NATS subject for story-preparer agent tasks.
	subjectStoryTask = "agent.task.story-preparation"
)

// Component implements the story-preparer processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	modelRegistry ssmodel.RegistryReader
	toolRegistry  component.ToolRegistryReader
	assembler     *prompt.Assembler
	lessonWriter  *lessons.Writer

	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	retry *dispatchretry.State

	triggersProcessed atomic.Int64
	generationsFailed atomic.Int64
	lastActivityMu    sync.RWMutex
	lastActivity      time.Time
}

// ---------------------------------------------------------------------------
// Factory
// ---------------------------------------------------------------------------

// NewComponent creates a new story-preparer processor.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

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

	registry := prompt.NewRegistry()
	registry.RegisterAll(promptdomain.Software()...)
	registry.Register(prompt.ToolGuidanceFragment(prompt.DefaultToolGuidance()))
	registry.Register(prompt.GraphManifestFragment(workflowtools.RegistrySummaryFetchFn()))
	assembler := prompt.NewAssembler(registry)

	tw := &graphutil.TripleWriter{
		NATSClient:    deps.NATSClient,
		Logger:        logger,
		ComponentName: "story-preparer",
	}

	return &Component{
		name:          "story-preparer",
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
	c.logger.Debug("Initialized story-preparer",
		"enabled", c.config.Enabled,
		"plan_state_bucket", c.config.PlanStateBucket)
	return nil
}

// Start begins watching for plans reaching architecture_generated. When
// Config.Enabled is false, the watcher is established but the trigger
// path short-circuits — this lets operators flip Enabled via config edit
// without restarting the component.
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

	js, err := c.natsClient.JetStream()
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("cannot get JetStream: %w", err)
	}

	go c.watchPlanStates(subCtx, js)
	go c.watchLoopCompletions(subCtx)

	c.logger.Info("story-preparer started",
		"enabled", c.config.Enabled,
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

	c.logger.Info("story-preparer stopped",
		"triggers_processed", c.triggersProcessed.Load(),
		"generations_failed", c.generationsFailed.Load())

	return nil
}

// ---------------------------------------------------------------------------
// KV watcher
// ---------------------------------------------------------------------------

// watchPlanStates watches PLAN_STATES for plans reaching architecture_generated.
// When Config.Enabled is false the loop runs but the per-entry filter
// short-circuits — keeps the dormant component low-cost without changing the
// startup contract.
func (c *Component) watchPlanStates(ctx context.Context, js jetstream.JetStream) {
	bucket, err := workflow.WaitForKVBucket(ctx, js, c.config.PlanStateBucket)
	if err != nil {
		c.logger.Warn("PLAN_STATES not available — story-preparer watcher disabled",
			"bucket", c.config.PlanStateBucket, "error", err)
		return
	}

	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		c.logger.Warn("Failed to watch PLAN_STATES", "error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Watching PLAN_STATES for architecture_generated",
		"enabled", c.config.Enabled)

	for entry := range watcher.Updates() {
		if entry == nil {
			continue
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}
		if !c.config.Enabled {
			continue
		}

		var plan workflow.Plan
		if json.Unmarshal(entry.Value(), &plan) != nil {
			continue
		}
		if plan.Status != workflow.StatusArchitectureGenerated {
			continue
		}

		// Claim the plan to prevent re-trigger on watcher restarts. If the
		// claim fails another watcher already advanced — usually because
		// PR 3 ships dormant and scenario-generator claims generating_scenarios
		// first.
		if !workflow.ClaimPlanStatus(ctx, c.natsClient, plan.Slug, workflow.StatusPreparingStories, c.logger) {
			continue
		}

		c.triggersProcessed.Add(1)
		c.updateLastActivity()

		go c.processStoryPhase(ctx, &plan)
	}
}

// processStoryPhase handles the story-preparation phase for a single plan.
// Dispatches Sarah via agentic-dispatch with the assembled prompt context.
func (c *Component) processStoryPhase(ctx context.Context, plan *workflow.Plan) {
	c.retry.Track(plan.Slug, plan)
	c.dispatchStoryPreparer(ctx, plan, "", plan.ReviewFormattedFindings)
}

// ---------------------------------------------------------------------------
// Agent dispatch
// ---------------------------------------------------------------------------

// dispatchStoryPreparer dispatches a story-preparer agent loop via
// agentic-dispatch. previousError is non-empty on retry attempts.
// reviewFindings is the formatted plan-reviewer R3 rejection text from the
// prior round (empty on first dispatch).
func (c *Component) dispatchStoryPreparer(ctx context.Context, plan *workflow.Plan, previousError string, reviewFindings ...string) {
	c.updateLastActivity()

	taskID := fmt.Sprintf("storyprep-%s-%s", plan.Slug, uuid.New().String())
	c.retry.SetActiveLoop(plan.Slug, taskID)

	storyCtx := buildPromptContext(plan, previousError)
	if len(reviewFindings) > 0 {
		storyCtx.ReviewFindings = reviewFindings[0]
	}

	capability := c.config.DefaultCapability
	if capability == "" {
		capability = string(model.CapabilityPlanning)
	}
	modelName := c.modelRegistry.Resolve(capability)

	provider := c.resolveProvider()
	var (
		maxTokens int
		endpoint  *ssmodel.EndpointConfig
	)
	if c.modelRegistry != nil {
		if ep := c.modelRegistry.GetEndpoint(modelName); ep != nil {
			maxTokens = ep.MaxTokens
			endpoint = ep
		}
	}

	asmCtx := &prompt.AssemblyContext{
		Role:                prompt.RoleStoryPreparer,
		Provider:            provider,
		HasResponseFormat:   terminal.EndpointSupportsResponseFormatGated(endpoint, c.config.AttachResponseFormat),
		Domain:              "software",
		AvailableTools:      prompt.FilterTools(c.availableToolNames(), prompt.RoleStoryPreparer),
		SupportsTools:       true,
		MaxTokens:           maxTokens,
		Standards:           prompt.LoadStandardsForRoleFromDisk(prompt.RoleStoryPreparer),
		Persona:             prompt.GlobalPersonas().ForRole(prompt.RoleStoryPreparer),
		Vocabulary:          prompt.GlobalPersonas().Vocabulary(),
		StoryPreparerPrompt: storyCtx,
	}

	if c.lessonWriter != nil {
		graphCtx := context.WithoutCancel(ctx)
		if roleLessons, err := c.lessonWriter.RotateLessonsForRole(graphCtx, "story-preparer", 10); err == nil && len(roleLessons) > 0 {
			tk := &prompt.LessonsLearned{}
			for _, les := range roleLessons {
				tk.Lessons = append(tk.Lessons, prompt.LessonEntry{
					Category:      les.Source,
					Summary:       les.Summary,
					InjectionForm: les.InjectionForm,
					Positive:      les.Positive,
					Role:          les.Role,
				})
			}
			asmCtx.LessonsLearned = tk
		}
	}

	assembled := c.assembler.Assemble(asmCtx)
	if assembled.RenderError != nil {
		c.logger.Error("Story-preparer user-prompt render failed", "slug", plan.Slug, "error", assembled.RenderError)
		return
	}

	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleGeneral,
		Model:        modelName,
		Prompt:       assembled.UserMessage,
		Tools:        terminal.ToolsForEndpoint(c.toolRegistry, "stories", endpoint, prompt.FilterTools(c.availableToolNames(), prompt.RoleStoryPreparer)...),
		ToolChoice:   &agentic.ToolChoice{Mode: "required"},
		WorkflowSlug: workflowSlugPlanning,
		WorkflowStep: stepStoryPreparation,
		Context: &agentic.ConstructedContext{
			Content: assembled.SystemMessage,
		},
		Metadata: map[string]any{
			"plan_slug":        plan.Slug,
			"deliverable_type": "stories",
			"task_id":          "main",
			"role":             string(prompt.RoleStoryPreparer),
			"model":            modelName,
		},
		ResponseFormat: terminal.ResponseFormatForEndpointGated(endpoint, "stories", c.config.AttachResponseFormat),
	}

	baseMsg := message.NewBaseMessage(task.Schema(), task, "semspec-story-preparer")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to marshal task message, rejecting plan",
			"slug", plan.Slug, "error", err)
		c.sendGenerationFailed(ctx, plan.Slug, fmt.Sprintf("story-preparer dispatch marshal failed: %v", err))
		return
	}

	if err := c.natsClient.PublishToStream(ctx, subjectStoryTask, data); err != nil {
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to dispatch story-preparer, rejecting plan",
			"slug", plan.Slug, "error", err)
		c.sendGenerationFailed(ctx, plan.Slug, fmt.Sprintf("story-preparer dispatch failed: %v", err))
		return
	}

	c.logger.Info("Dispatched story-preparer agent",
		"slug", plan.Slug,
		"task_id", taskID,
		"model", modelName,
		"fragments", len(assembled.FragmentsUsed),
		"system_chars", assembled.SystemMessageChars)
}

// buildPromptContext projects the plan into the StoryPreparerPromptContext
// shape — analyst capabilities, architecture components (with their files
// + capability mappings), and requirement summaries.
func buildPromptContext(plan *workflow.Plan, previousError string) *prompt.StoryPreparerPromptContext {
	caps := make([]prompt.StoryPreparerCapability, 0)
	if plan.Exploration != nil {
		caps = make([]prompt.StoryPreparerCapability, len(plan.Exploration.Capabilities))
		for i, c := range plan.Exploration.Capabilities {
			caps[i] = prompt.StoryPreparerCapability{Name: c.Name, Description: c.Description}
		}
	}

	var components []prompt.StoryPreparerComponent
	if plan.Architecture != nil {
		components = make([]prompt.StoryPreparerComponent, len(plan.Architecture.ComponentBoundaries))
		for i, comp := range plan.Architecture.ComponentBoundaries {
			components[i] = prompt.StoryPreparerComponent{
				Name:                comp.Name,
				Responsibility:      comp.Responsibility,
				ImplementationFiles: append([]string(nil), comp.ImplementationFiles...),
				Capabilities:        append([]string(nil), comp.Capabilities...),
			}
		}
	}

	reqs := make([]prompt.ExistingRequirementSummary, len(plan.Requirements))
	for i, r := range plan.Requirements {
		reqs[i] = prompt.ExistingRequirementSummary{
			ID:          r.ID,
			Title:       r.Title,
			Description: r.Description,
			DependsOn:   append([]string(nil), r.DependsOn...),
		}
	}

	return &prompt.StoryPreparerPromptContext{
		PlanTitle:              plan.Title,
		PlanGoal:               plan.Goal,
		PlanContext:            plan.Context,
		Capabilities:           caps,
		ArchitectureComponents: components,
		Requirements:           reqs,
		PreviousError:          previousError,
	}
}

// ---------------------------------------------------------------------------
// Loop completion watcher
// ---------------------------------------------------------------------------

// watchLoopCompletions watches AGENT_LOOPS for story-preparer completions.
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

	c.logger.Info("Loop completion watcher started (watching AGENT_LOOPS for story-preparation)")

	replayDone := false
	processedLoops := make(map[string]bool)

	for entry := range watcher.Updates() {
		if entry == nil {
			replayDone = true
			c.logger.Info("AGENT_LOOPS replay complete for story-preparer")
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
		if loop.WorkflowStep != stepStoryPreparation {
			continue
		}

		slug, _ := loop.Metadata["plan_slug"].(string)
		if slug == "" {
			continue
		}

		if !replayDone {
			c.logger.Debug("Replay: skipping completed story-preparation loop",
				"slug", slug, "loop_id", loop.ID)
			continue
		}

		if processedLoops[loop.ID] {
			c.logger.Debug("Skipping already-processed loop",
				"loop_id", loop.ID, "slug", slug)
			continue
		}
		processedLoops[loop.ID] = true

		c.handleLoopCompletion(ctx, &loop, slug)
	}
}

// handleLoopCompletion processes a completed story-preparer agent loop.
// Parses the Stories list, validates via workflow.ValidateStories (Sarah's
// readiness gate as a defensive backstop), and publishes
// plan.mutation.stories.generated for plan-manager to persist + transition.
func (c *Component) handleLoopCompletion(ctx context.Context, loop *agentic.LoopEntity, slug string) {
	c.updateLastActivity()

	if c.retry.IsStaleLoop(slug, loop.TaskID) {
		c.logger.Debug("Dropping stale story-preparation loop completion (task ID mismatch)",
			"slug", slug, "loop_task_id", loop.TaskID, "loop_id", loop.ID)
		return
	}

	if loop.Outcome != agentic.OutcomeSuccess {
		c.generationsFailed.Add(1)
		loopErrorMsg := loop.Error
		if loopErrorMsg == "" {
			loopErrorMsg = fmt.Sprintf("agent loop ended with outcome %q", loop.Outcome)
		}
		c.logger.Error("Story-preparer agent loop failed",
			"slug", slug,
			"loop_id", loop.ID,
			"outcome", loop.Outcome,
			"error", loopErrorMsg)
		c.retryOrFail(ctx, slug, loopErrorMsg)
		return
	}

	stories, err := parseStoriesFromResult(loop.Result)
	if err != nil {
		c.generationsFailed.Add(1)
		parseErrorMsg := fmt.Sprintf("failed to parse stories: %s", err.Error())
		c.logger.Error("Failed to parse stories from agent result",
			"slug", slug,
			"loop_id", loop.ID,
			"error", err)
		c.retryOrFail(ctx, slug, parseErrorMsg)
		return
	}

	// Staleness guard + plan re-load for validators.
	kvPlan, loadErr := c.loadPlanFromKV(ctx, slug)
	if loadErr == nil {
		status := kvPlan.EffectiveStatus()
		if status != workflow.StatusPreparingStories {
			c.logger.Warn("Plan advanced past preparing_stories, discarding stale result",
				"slug", slug,
				"current_status", status,
				"loop_id", loop.ID)
			c.retry.Clear(slug)
			return
		}
	}

	// Sarah's readiness gate runs as workflow.ValidateStories. Cross-entity
	// component-resolution checks live on plan-reviewer R3 (mergeStoryFindings)
	// because they need the plan's architecture, which arrives via plan-manager
	// after the mutation lands.
	if err := workflow.ValidateStories(stories); err != nil {
		c.generationsFailed.Add(1)
		msg := fmt.Sprintf("story validation failed: %s", err.Error())
		c.logger.Warn("Stories rejected by Sarah's readiness gate",
			"slug", slug, "loop_id", loop.ID, "error", err)
		c.retryOrFail(ctx, slug, msg)
		return
	}

	traceID, _ := loop.Metadata["trace_id"].(string)
	if err := c.publishStoriesGenerated(ctx, slug, stories, traceID); err != nil {
		if strings.Contains(err.Error(), "invalid transition") {
			c.logger.Warn("Stories mutation rejected (plan advanced), discarding stale result",
				"slug", slug,
				"error", err,
				"loop_id", loop.ID)
			c.retry.Clear(slug)
			return
		}
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to publish stories mutation, rejecting plan",
			"slug", slug, "loop_id", loop.ID, "error", err)
		c.sendGenerationFailed(ctx, slug, fmt.Sprintf("stories mutation publish failed: %v", err))
		return
	}

	c.retry.Clear(slug)

	c.logger.Info("Stories prepared via agentic-dispatch and mutation accepted",
		"slug", slug,
		"loop_id", loop.ID,
		"stories", len(stories))
}

// retryOrFail attempts to re-dispatch story preparation with the error
// message as feedback. On exhaustion publishes generation.failed.
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
		c.logger.Warn("Story preparation exhausted retries",
			"slug", slug,
			"attempts", entry.Count,
			"max", c.config.MaxGenerationRetries,
			"last_error", errorMsg)
		c.sendGenerationFailed(ctx, slug, errorMsg)
		return
	}

	c.logger.Info("Retrying story preparation",
		"slug", slug,
		"attempt", entry.Count,
		"max", c.config.MaxGenerationRetries,
		"previous_error", errorMsg)

	c.dispatchStoryPreparer(ctx, plan, errorMsg)
}

// sendGenerationFailed publishes plan.mutation.generation.failed to reject the plan.
func (c *Component) sendGenerationFailed(ctx context.Context, slug, feedback string) {
	failReq, _ := json.Marshal(map[string]string{
		"slug":  slug,
		"phase": "story-preparation",
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

// parseStoriesFromResult extracts the Stories list from an agent loop result.
// The deliverable shape is validated by submit_work, so the unmarshal is
// the primary parse — the deeper structural invariants live on
// workflow.ValidateStories.
func parseStoriesFromResult(result string) ([]workflow.Story, error) {
	if result == "" {
		return nil, fmt.Errorf("empty result")
	}

	var wrapper struct {
		Stories []workflow.Story `json:"stories"`
	}
	if err := json.Unmarshal([]byte(result), &wrapper); err != nil {
		return nil, fmt.Errorf("parse stories JSON: %w", err)
	}

	if len(wrapper.Stories) == 0 {
		return nil, fmt.Errorf("stories list is empty — Sarah must emit at least one story per dispatch")
	}

	return wrapper.Stories, nil
}

// ---------------------------------------------------------------------------
// Mutation publishing
// ---------------------------------------------------------------------------

// publishStoriesGenerated sends plan.mutation.stories.generated to plan-manager.
func (c *Component) publishStoriesGenerated(ctx context.Context, slug string, stories []workflow.Story, traceID string) error {
	mutReq := workflow.StoriesGeneratedEvent{
		Slug:       slug,
		Stories:    stories,
		StoryCount: len(stories),
		TraceID:    traceID,
	}

	data, err := json.Marshal(mutReq)
	if err != nil {
		return fmt.Errorf("marshal stories mutation: %w", err)
	}

	resp, err := c.natsClient.RequestWithRetry(ctx, mutationStoriesGenerated, data,
		10*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		return fmt.Errorf("send stories mutation: %w", err)
	}

	var mutResp struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(resp, &mutResp); err != nil {
		return fmt.Errorf("unmarshal stories mutation response: %w", err)
	}
	if !mutResp.Success {
		return fmt.Errorf("plan-manager rejected stories mutation: %s", mutResp.Error)
	}

	c.logger.Info("Stories mutation accepted by plan-manager", "slug", slug)
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (c *Component) resolveProvider() prompt.Provider {
	capability := c.config.DefaultCapability
	if capability == "" {
		capability = string(model.CapabilityPlanning)
	}
	modelName := c.modelRegistry.Resolve(capability)
	if endpoint := c.modelRegistry.GetEndpoint(modelName); endpoint != nil {
		return prompt.Provider(endpoint.Provider)
	}
	return prompt.ProviderOllama
}

func (c *Component) availableToolNames() []string {
	return []string{
		"bash", "submit_work",
		"write_todos", "scratchpad",
	}
}

// ---------------------------------------------------------------------------
// Component.Discoverable implementation
// ---------------------------------------------------------------------------

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "story-preparer",
		Type:        "processor",
		Description: "Sarah (BMAD PO): shards requirements into ready-for-dev Stories with Task checklists (ADR-043 Move 3)",
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
	return storyPreparerSchema
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
