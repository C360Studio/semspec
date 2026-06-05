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

	// claimedSlugs records slugs whose preparing_stories status this
	// watcher's own ClaimPlanStatus call just produced. The watcher will
	// observe the corresponding KV-put event next (because plan-manager
	// echoes the status change back), and without this dedup it would
	// treat the echo as a back-transition (Train C step 5) and double-
	// dispatch. Entries are removed once the echo is consumed.
	claimedSlugsMu sync.Mutex
	claimedSlugs   map[string]bool

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
		claimedSlugs:  make(map[string]bool),
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
		"plan_state_bucket", c.config.PlanStateBucket)
	return nil
}

// Start begins watching for plans reaching architecture_generated.
// ADR-043 PR 4l: story-preparer is always-on when registered. The watcher
// claims preparing_stories from architecture_generated; scenario-generator
// in turn watches stories_generated (PR 4l also removed Bob's
// architecture_generated watch), making the flow strictly sequential and
// race-free.
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

	c.logger.Info("Watching PLAN_STATES for architecture_generated / preparing_stories")

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
		// Two claim points for Sarah's dispatch:
		//   (A) architecture_generated → preparing_stories: forward flow
		//       on a freshly architected plan. ClaimPlanStatus does an
		//       atomic CAS via plan-manager so only one replica wins.
		//   (B) preparing_stories already set: back-transition driven by
		//       plan-manager when a story_reprepare PlanDecision is
		//       accepted (Train C step 4). plan-manager has already
		//       cleared the affected Stories + written Story.RecoveryHint;
		//       Sarah's re-prep reads those hints via the prompt context.
		//       Already-in-target-state means ClaimPlanStatus rejects (a
		//       → a is not a valid transition), so we don't claim — the
		//       transition is already done; we just need to dispatch and
		//       dedup against the echo from path (A) below.
		switch plan.Status {
		case workflow.StatusArchitectureGenerated:
			if !workflow.ClaimPlanStatus(ctx, c.natsClient, plan.Slug, workflow.StatusPreparingStories, c.logger) {
				continue
			}
			// Track the claim so the corresponding preparing_stories echo
			// the watcher sees next does NOT trigger a second dispatch.
			c.markClaimedSlug(plan.Slug)
			plan.Status = workflow.StatusPreparingStories
		case workflow.StatusPreparingStories:
			// Was THIS watcher's claim the cause of this echo? If yes, the
			// dispatch already happened in path (A) — drop the echo and
			// clear the flag. If no, plan-manager drove the back-
			// transition and we dispatch now.
			if c.consumeClaimedSlug(plan.Slug) {
				continue
			}
		default:
			continue
		}

		c.triggersProcessed.Add(1)
		c.updateLastActivity()

		go c.processStoryPhase(ctx, &plan)
	}
}

// markClaimedSlug records that THIS watcher just produced a
// preparing_stories status for the named slug via ClaimPlanStatus. The
// next preparing_stories KV-put echo this watcher observes for the slug
// will be the corresponding self-echo (the status mutation plan-manager
// performed in response to our claim) and should be skipped, not
// treated as a back-transition.
func (c *Component) markClaimedSlug(slug string) {
	c.claimedSlugsMu.Lock()
	defer c.claimedSlugsMu.Unlock()
	c.claimedSlugs[slug] = true
}

// consumeClaimedSlug reports whether THIS watcher had a pending claim
// for the slug and clears the flag. Returns true when the caller should
// SKIP processing the current event (it's the self-echo); false when
// the event came from elsewhere (e.g., plan-manager back-transition)
// and the caller should dispatch.
func (c *Component) consumeClaimedSlug(slug string) bool {
	c.claimedSlugsMu.Lock()
	defer c.claimedSlugsMu.Unlock()
	if c.claimedSlugs[slug] {
		delete(c.claimedSlugs, slug)
		return true
	}
	return false
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
	var integrations []prompt.IntegrationInfo
	var upstreams []prompt.UpstreamResolutionInfo
	if plan.Architecture != nil {
		components = make([]prompt.StoryPreparerComponent, len(plan.Architecture.ComponentBoundaries))
		for i, comp := range plan.Architecture.ComponentBoundaries {
			components[i] = prompt.StoryPreparerComponent{
				Name:                comp.Name,
				Responsibility:      comp.Responsibility,
				ImplementationFiles: append([]string(nil), comp.ImplementationFiles...),
				Capabilities:        append([]string(nil), comp.Capabilities...),
				UpstreamRefs:        append([]string(nil), comp.UpstreamRefs...),
			}
		}
		proj := prompt.ProjectArchitecture(plan.Architecture)
		integrations = proj.Integrations
		upstreams = proj.Upstreams
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

	// Train C step 5: surface Story.RecoveryHint values onto the prompt
	// context. plan-manager wrote these onto the affected Stories at
	// PlanDecision-accept time (Train C step 4 applyRecoveryHint); the
	// Stories themselves stay in plan.Stories so Sarah's emission
	// replaces the whole set per the existing handleStoriesMutation
	// wipe-and-replace contract. Empty on the forward flow
	// (architecture_generated → preparing_stories); populated on the
	// back-transition flow where Sarah is being asked to re-prep.
	var recoveryHints []prompt.StoryRecoveryHint
	for _, s := range plan.Stories {
		if s.RecoveryHint == "" {
			continue
		}
		recoveryHints = append(recoveryHints, prompt.StoryRecoveryHint{
			StoryID: s.ID,
			Hint:    s.RecoveryHint,
		})
	}

	return &prompt.StoryPreparerPromptContext{
		PlanTitle:              plan.Title,
		PlanGoal:               plan.Goal,
		PlanContext:            plan.Context,
		Capabilities:           caps,
		ArchitectureComponents: components,
		Integrations:           integrations,
		Upstreams:              upstreams,
		Requirements:           reqs,
		PreviousError:          previousError,
		StoryRecoveryHints:     recoveryHints,
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

	// ADR-043 PR 4e — pre-load the plan because Sarah's positional output
	// (requirement_index, story labels) needs the plan's Requirements list +
	// slug to resolve into canonical IDs. The load also doubles as the
	// staleness check that historically lived after parse.
	kvPlan, loadErr := c.loadPlanFromKV(ctx, slug)
	if loadErr != nil {
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to load plan for story label resolution",
			"slug", slug, "loop_id", loop.ID, "error", loadErr)
		c.retryOrFail(ctx, slug, fmt.Sprintf("plan load failed pre-parse: %v", loadErr))
		return
	}
	if status := kvPlan.EffectiveStatus(); status != workflow.StatusPreparingStories {
		c.logger.Warn("Plan advanced past preparing_stories, discarding stale result",
			"slug", slug,
			"current_status", status,
			"loop_id", loop.ID)
		c.retry.Clear(slug)
		return
	}

	stories, err := parseStoriesFromResult(loop.Result, kvPlan, slug)
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
// Result parsing — ADR-043 PR 4e positional/labeled wire shape
// ---------------------------------------------------------------------------

// positionalStoryInput is the LLM-facing shape under ADR-044 M:N coverage:
// Sarah picks ONE component_name (1:1 anchor), N requirement_indices
// (M:N coverage join, 0-based into plan.Requirements), and N
// capability_indices (M:N coverage join, 0-based into the rendered
// Capabilities list). She does NOT author files_owned (system derives
// from component.ImplementationFiles) nor depends_on_labels (system
// derives via DeriveStoryScheduling).
type positionalStoryInput struct {
	Label              string                `json:"label"`
	ComponentName      string                `json:"component_name"`
	RequirementIndices []int                 `json:"requirement_indices"`
	CapabilityIndices  []int                 `json:"capability_indices"`
	Title              string                `json:"title"`
	Intent             string                `json:"intent,omitempty"`
	Tasks              []positionalTaskInput `json:"tasks,omitempty"`
}

// positionalTaskInput is the per-task LLM shape — task-local label plus
// labels of other tasks WITHIN the same story (cross-story task ordering
// lives on the story's depends_on_labels).
type positionalTaskInput struct {
	Label           string   `json:"label"`
	Description     string   `json:"description"`
	DependsOnLabels []string `json:"depends_on_labels,omitempty"`
}

// parseStoriesFromResult extracts Sarah's positional output from the loop
// result and resolves it into canonical workflow.Story shape. The
// transformation: (a) resolve requirement_index to Requirement.ID;
// (b) assign canonical Story.ID = story.<slug>.<reqseq>.<storyseq>;
// (c) build a label → Story.ID map and rewrite depends_on_labels →
// canonical Story.ID strings; (d) for each task: assign canonical Task.ID
// = task.<slug>.<reqseq>.<storyseq>.<taskseq>, set StoryID, resolve
// intra-story DependsOn labels.
//
// Label-resolution errors (unknown story label, unknown task label,
// requirement_index out of range) surface as parse errors → retryOrFail
// → Sarah gets another cycle. Sarah's readiness gate runs after this in
// ValidateStories at the workflow layer.
func parseStoriesFromResult(result string, plan *workflow.Plan, slug string) ([]workflow.Story, error) {
	if result == "" {
		return nil, fmt.Errorf("empty result")
	}

	var wrapper struct {
		Stories []positionalStoryInput `json:"stories"`
	}
	if err := json.Unmarshal([]byte(result), &wrapper); err != nil {
		return nil, fmt.Errorf("parse stories JSON: %w", err)
	}

	if len(wrapper.Stories) == 0 {
		return nil, fmt.Errorf("stories list is empty — Sarah must emit at least one story per dispatch")
	}

	return resolveStoryLabels(wrapper.Stories, plan, slug)
}

// resolveStoryLabels transforms Sarah's positional output into canonical
// workflow.Story shape under the ADR-044 M:N contract. Extracted as a
// separate function for testability — see component_test.go for the
// input → output conversion contract.
//
// The function (a) validates each Story's component_name resolves to an
// architecture component, (b) validates every requirement_index +
// capability_index is in range, (c) derives FilesOwned from the
// component's ImplementationFiles (no union, no Sarah authorship), (d)
// assigns canonical Story.ID = story.<slug>.<reqseq>.<storyseq> using
// the FIRST RequirementIndex as the "primary" for seq purposes, (e)
// runs DeriveStoryScheduling to populate Story.DependsOn from semantic
// + resource edges, (f) enforces coverage closure — every Requirement
// and every Capability appears in at least one Story's join.
func resolveStoryLabels(input []positionalStoryInput, plan *workflow.Plan, slug string) ([]workflow.Story, error) {
	if plan == nil {
		return nil, fmt.Errorf("plan required for label resolution")
	}
	if slug == "" {
		return nil, fmt.Errorf("slug required for canonical ID assignment")
	}

	// Build architecture component lookup: ComponentDef.Name → ImplementationFiles.
	componentFiles := make(map[string][]string)
	if plan.Architecture != nil {
		for _, c := range plan.Architecture.ComponentBoundaries {
			if c.Name == "" {
				continue
			}
			componentFiles[c.Name] = append([]string(nil), c.ImplementationFiles...)
		}
	}

	// Build capability index → Name lookup (positional, matching prompt order).
	var capabilityNames []string
	if plan.Exploration != nil {
		capabilityNames = make([]string, 0, len(plan.Exploration.Capabilities))
		for _, c := range plan.Exploration.Capabilities {
			capabilityNames = append(capabilityNames, c.Name)
		}
	}

	// Pass 1: assign canonical story IDs (using FIRST RequirementIndex as
	// primary), validate component_name resolves, validate index ranges.
	storiesByReqseq := make(map[string]int)
	labelToStoryID := make(map[string]string, len(input))
	canonicalIDs := make([]string, len(input))
	resolvedReqIDs := make([][]string, len(input))
	resolvedCapNames := make([][]string, len(input))

	for i, s := range input {
		if _, exists := labelToStoryID[s.Label]; s.Label != "" && exists {
			return nil, fmt.Errorf("story label %q appears more than once", s.Label)
		}
		reqIDs, capNames, err := validateStoryIndices(s, plan, componentFiles, capabilityNames)
		if err != nil {
			return nil, err
		}
		resolvedReqIDs[i] = reqIDs
		resolvedCapNames[i] = capNames

		// Primary reqseq for ID generation = first listed requirement.
		primaryReq := plan.Requirements[s.RequirementIndices[0]]
		reqseq := requirementSeq(primaryReq.ID)
		storiesByReqseq[reqseq]++
		canonicalID := fmt.Sprintf("story.%s.%s.%d", slug, reqseq, storiesByReqseq[reqseq])
		canonicalIDs[i] = canonicalID
		labelToStoryID[s.Label] = canonicalID
	}

	// Pass 2: construct workflow.Story values with derived FilesOwned.
	// Story.DependsOn left empty here; DeriveStoryScheduling populates it.
	out := make([]workflow.Story, len(input))
	for i, s := range input {
		tasks, err := resolveTasks(s.Tasks, canonicalIDs[i], canonicalIDs[i], s.Label)
		if err != nil {
			return nil, err
		}
		out[i] = workflow.Story{
			ID:              canonicalIDs[i],
			ComponentName:   s.ComponentName,
			RequirementIDs:  resolvedReqIDs[i],
			CapabilityNames: resolvedCapNames[i],
			Title:           s.Title,
			Intent:          s.Intent,
			FilesOwned:      append([]string(nil), componentFiles[s.ComponentName]...),
			Tasks:           tasks,
		}
	}

	// Coverage closure: every Requirement appears in at least one Story's
	// RequirementIDs; every Capability appears in at least one Story's
	// CapabilityNames. Under ADR-044 the analyst's capabilities and John's
	// requirements together define the work Sarah must cover; gaps surface
	// as parse errors so Sarah retries.
	if err := assertFullCoverage(out, plan, capabilityNames); err != nil {
		return nil, err
	}

	// Derive Story.DependsOn from (1) semantic prereq closure and (2)
	// file-ownership conflicts. ADR-044 makes Sarah the constraint-author
	// and the system the scheduler-graph author — see workflow/derive_story_scheduling.go.
	if err := workflow.DeriveStoryScheduling(out, plan.Requirements); err != nil {
		return nil, fmt.Errorf("story scheduling derivation: %w", err)
	}

	return out, nil
}

// validateStoryIndices does the per-Story shape + index-range checks
// for resolveStoryLabels Pass 1. Returns the resolved Requirement IDs
// and Capability names on success.
func validateStoryIndices(s positionalStoryInput, plan *workflow.Plan, componentFiles map[string][]string, capabilityNames []string) (reqIDs []string, capNames []string, err error) {
	if s.Label == "" {
		return nil, nil, fmt.Errorf("story at label position: missing label")
	}
	if s.ComponentName == "" {
		return nil, nil, fmt.Errorf("story %q: missing component_name (ADR-044 requires one architectural component anchor)", s.Label)
	}
	if _, ok := componentFiles[s.ComponentName]; !ok {
		return nil, nil, fmt.Errorf("story %q: component_name %q does not resolve to any declared architecture component", s.Label, s.ComponentName)
	}
	if len(s.RequirementIndices) == 0 {
		return nil, nil, fmt.Errorf("story %q: requirement_indices is empty (Story must cover at least one Requirement)", s.Label)
	}
	reqIDs = make([]string, 0, len(s.RequirementIndices))
	seenReq := make(map[int]struct{}, len(s.RequirementIndices))
	for _, idx := range s.RequirementIndices {
		if idx < 0 || idx >= len(plan.Requirements) {
			return nil, nil, fmt.Errorf("story %q: requirement_index %d out of range [0, %d)",
				s.Label, idx, len(plan.Requirements))
		}
		if _, dup := seenReq[idx]; dup {
			return nil, nil, fmt.Errorf("story %q: requirement_index %d listed more than once", s.Label, idx)
		}
		seenReq[idx] = struct{}{}
		reqIDs = append(reqIDs, plan.Requirements[idx].ID)
	}
	capNames = make([]string, 0, len(s.CapabilityIndices))
	seenCap := make(map[int]struct{}, len(s.CapabilityIndices))
	for _, idx := range s.CapabilityIndices {
		if idx < 0 || idx >= len(capabilityNames) {
			return nil, nil, fmt.Errorf("story %q: capability_index %d out of range [0, %d)",
				s.Label, idx, len(capabilityNames))
		}
		if _, dup := seenCap[idx]; dup {
			return nil, nil, fmt.Errorf("story %q: capability_index %d listed more than once", s.Label, idx)
		}
		seenCap[idx] = struct{}{}
		capNames = append(capNames, capabilityNames[idx])
	}
	return reqIDs, capNames, nil
}

// assertFullCoverage checks that every Requirement appears in some
// Story.RequirementIDs and every Capability appears in some
// Story.CapabilityNames. Reported as parse errors so retry-feedback
// surfaces them in Sarah's next prompt.
func assertFullCoverage(stories []workflow.Story, plan *workflow.Plan, capabilityNames []string) error {
	coveredReq := make(map[string]struct{})
	coveredCap := make(map[string]struct{})
	for _, s := range stories {
		for _, rid := range s.RequirementIDs {
			coveredReq[rid] = struct{}{}
		}
		for _, cn := range s.CapabilityNames {
			coveredCap[cn] = struct{}{}
		}
	}
	var missReq []string
	for _, req := range plan.Requirements {
		if _, ok := coveredReq[req.ID]; !ok {
			missReq = append(missReq, req.ID)
		}
	}
	if len(missReq) > 0 {
		return fmt.Errorf("Sarah's stories do not cover every requirement; missing: %v — add the requirement_index to one of your stories (M:N coverage allows a single Story to cover multiple)", missReq)
	}
	var missCap []string
	for _, cap := range capabilityNames {
		if _, ok := coveredCap[cap]; !ok {
			missCap = append(missCap, cap)
		}
	}
	if len(missCap) > 0 {
		return fmt.Errorf("Sarah's stories do not cover every capability; missing: %v — add the capability_index to one of your stories", missCap)
	}
	return nil
}

// resolveTasks assigns canonical Task.IDs and resolves intra-story task
// DependsOn labels. The canonical Task.ID is derived from the story's
// canonical ID by replacing the story.* prefix with task.* and appending
// a 1-indexed task sequence number.
func resolveTasks(inputs []positionalTaskInput, storyID, storyCanonicalID, storyLabel string) ([]workflow.Task, error) {
	// task IDs derive from the story's canonical ID by replacing the
	// story.<slug>.<reqseq>.<storyseq> prefix with task.<slug>.<reqseq>.<storyseq>.<taskseq>.
	taskIDPrefix := "task." + strings.TrimPrefix(storyCanonicalID, "story.")

	labelToTaskID := make(map[string]string, len(inputs))
	canonicalTaskIDs := make([]string, len(inputs))
	for i, t := range inputs {
		if t.Label == "" {
			return nil, fmt.Errorf("story %q: task at index %d missing label", storyLabel, i)
		}
		if _, exists := labelToTaskID[t.Label]; exists {
			return nil, fmt.Errorf("story %q: task label %q appears more than once", storyLabel, t.Label)
		}
		canonicalTaskIDs[i] = fmt.Sprintf("%s.%d", taskIDPrefix, i+1)
		labelToTaskID[t.Label] = canonicalTaskIDs[i]
	}

	out := make([]workflow.Task, len(inputs))
	for i, t := range inputs {
		depends, err := resolveLabels(t.DependsOnLabels, labelToTaskID,
			fmt.Sprintf("task depends_on (story %q)", storyLabel), t.Label)
		if err != nil {
			return nil, err
		}
		out[i] = workflow.Task{
			ID:          canonicalTaskIDs[i],
			StoryID:     storyID,
			Description: t.Description,
			DependsOn:   depends,
		}
	}
	return out, nil
}

// resolveLabels maps a slice of labels to canonical IDs via the provided
// label-to-ID map. Returns an error naming the unresolved label so the
// regen prompt can pinpoint Sarah's mistake.
func resolveLabels(labels []string, m map[string]string, fieldName, sourceLabel string) ([]string, error) {
	if len(labels) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(labels))
	for _, lbl := range labels {
		id, ok := m[lbl]
		if !ok {
			return nil, fmt.Errorf("%s on %q references unknown label %q", fieldName, sourceLabel, lbl)
		}
		out = append(out, id)
	}
	return out, nil
}

// requirementSeq extracts the trailing sequence suffix from a requirement
// ID. Given "requirement.my-plan.3", returns "3". Falls back to the full
// ID if no dotted suffix can be extracted cleanly. Mirrors the
// scenario-generator helper but kept local to story-preparer to avoid
// cross-package dependency on a 3-line utility.
func requirementSeq(requirementID string) string {
	idx := strings.LastIndex(requirementID, ".")
	if idx < 0 || idx == len(requirementID)-1 {
		return requirementID
	}
	return requirementID[idx+1:]
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
