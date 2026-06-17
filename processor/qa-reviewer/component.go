// Package qareviewer provides the processor that renders the release-readiness
// verdict for a completed plan. It watches PLAN_STATES for plans reaching the
// reviewing_qa (or legacy reviewing_rollup) status, dispatches the Murat QA
// Test Architect persona via agentic-dispatch, and publishes a QAVerdictEvent
// mutation to plan-manager.
//
// Phase 6 wires real LLM review and populates verdict dimensions scoped by the
// plan's qa.level (synthesis/unit/integration). Projects that want to
// bypass qa-reviewer entirely set qa.level=none on their project config —
// plan-manager then routes plans straight to complete without entering
// reviewing_qa at all. Plan-manager is the single writer — this component
// only publishes mutations.
package qareviewer

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

	"github.com/c360studio/semspec/internal/trajectory"
	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/prompt"
	promptdomain "github.com/c360studio/semspec/prompt/domain"
	"github.com/c360studio/semspec/tools/terminal"
	workflowtools "github.com/c360studio/semspec/tools/workflow"
	"github.com/c360studio/semspec/vocabulary/observability"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/dispatchretry"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semspec/workflow/lessons"
	"github.com/c360studio/semspec/workflow/parseincident"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	ssmodel "github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// mutationQAStart is the subject qa-reviewer uses to claim a plan for
	// review, transitioning ready_for_qa → reviewing_qa and (optionally)
	// attaching the executor run payload.
	mutationQAStart = "plan.mutation.qa.start"

	// mutationQAVerdict is the subject qa-reviewer uses to deliver its
	// release-readiness verdict to plan-manager.
	mutationQAVerdict = "plan.mutation.qa.verdict"

	// subjectQAReviewTask is the NATS subject for QA reviewer agent tasks.
	subjectQAReviewTask = "agent.task.reviewer"

	// stepQAReviewing is the workflow step tag used in AGENT_LOOPS metadata
	// so the loop completion watcher can route events to this component.
	stepQAReviewing = "qa-reviewing"
)

// Component implements the qa-reviewer processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	modelRegistry ssmodel.RegistryReader
	toolRegistry  component.ToolRegistryReader
	assembler     *prompt.Assembler
	lessonWriter  *lessons.Writer
	tripleWriter  *graphutil.TripleWriter // ADR-035 CP-1 incident emit

	// retry tracks in-flight reviews: slug → retry state. The payload stored
	// per slug is *workflow.Plan — fetched on retry to rebuild the dispatch.
	retry *dispatchretry.State

	// Lifecycle
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics
	triggersProcessed atomic.Int64
	reviewsFailed     atomic.Int64
	lastActivityMu    sync.RWMutex
	lastActivity      time.Time
}

// ---------------------------------------------------------------------------
// Factory
// ---------------------------------------------------------------------------

// NewComponent creates a new qa-reviewer processor.
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
	if config.MaxReviewRetries == 0 {
		config.MaxReviewRetries = defaults.MaxReviewRetries
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

	// Initialize prompt assembler with software domain fragments.
	registry := prompt.NewRegistry()
	registry.RegisterAll(promptdomain.Software()...)
	registry.Register(prompt.ToolGuidanceFragment(prompt.DefaultToolGuidance()))
	registry.Register(prompt.GraphManifestFragment(workflowtools.RegistrySummaryFetchFn()))
	assembler := prompt.NewAssembler(registry)

	tw := &graphutil.TripleWriter{
		NATSClient:    deps.NATSClient,
		Logger:        logger,
		ComponentName: "qa-reviewer",
	}

	return &Component{
		name:          "qa-reviewer",
		config:        config,
		natsClient:    deps.NATSClient,
		logger:        logger,
		modelRegistry: deps.ModelRegistry,
		toolRegistry:  deps.ToolRegistry,
		assembler:     assembler,
		lessonWriter:  &lessons.Writer{TW: tw, Logger: logger},
		tripleWriter:  tw,
		retry: dispatchretry.New(dispatchretry.Config{
			MaxRetries: config.MaxReviewRetries,
			BackoffMs:  config.RetryBackoffMs,
		}),
	}, nil
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized qa-reviewer",
		"plan_state_bucket", c.config.PlanStateBucket)
	return nil
}

// Start begins processing QA review triggers.
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
	if err := c.startQACompletedConsumer(subCtx); err != nil {
		c.logger.Warn("Failed to start QACompleted consumer — unit/integration plans will stall",
			"error", err)
	}

	c.logger.Info("qa-reviewer started",
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

	c.logger.Info("qa-reviewer stopped",
		"triggers_processed", c.triggersProcessed.Load(),
		"reviews_failed", c.reviewsFailed.Load())

	return nil
}

// ---------------------------------------------------------------------------
// KV watcher — plan trigger
// ---------------------------------------------------------------------------

// watchPlanStates watches PLAN_STATES for plans reaching ready_for_qa with
// level=synthesis. Non-synthesis levels require an executor run first — those
// are driven by the QACompleted JetStream consumer, not this watcher.
func (c *Component) watchPlanStates(ctx context.Context, js jetstream.JetStream) {
	bucket, err := workflow.WaitForKVBucket(ctx, js, c.config.PlanStateBucket)
	if err != nil {
		c.logger.Warn("PLAN_STATES not available — qa-reviewer watcher disabled",
			"bucket", c.config.PlanStateBucket, "error", err)
		return
	}

	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		c.logger.Warn("Failed to watch PLAN_STATES", "error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Watching PLAN_STATES for ready_for_qa (synthesis-level)")

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
		if plan.Status != workflow.StatusReadyForQA {
			continue
		}
		// Non-synthesis plans are claimed by the QACompleted consumer after the
		// executor runs. Ignore them here.
		if plan.EffectiveQALevel() != workflow.QALevelSynthesis {
			continue
		}

		c.triggersProcessed.Add(1)
		c.updateLastActivity()

		go c.processReview(ctx, &plan, nil)
	}
}

// ---------------------------------------------------------------------------
// KV watcher — loop completion
// ---------------------------------------------------------------------------

// watchLoopCompletions watches AGENT_LOOPS for completed QA reviewer loops.
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

	c.logger.Info("Loop completion watcher started (watching AGENT_LOOPS for qa-reviewer)")

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
		if loop.WorkflowSlug != workflow.WorkflowSlugPlanning {
			continue
		}
		if loop.WorkflowStep != stepQAReviewing {
			continue
		}

		slug, _ := loop.Metadata["plan_slug"].(string)
		if slug == "" {
			continue
		}

		c.handleLoopCompletion(ctx, &loop, slug)
	}
}

// ---------------------------------------------------------------------------
// Core review pipeline
// ---------------------------------------------------------------------------

// processReview claims a plan for review and dispatches the LLM. Claims the
// ready_for_qa → reviewing_qa transition via plan.mutation.qa.start, mirroring
// the mutation-driven shape plan-reviewer uses. Synthesis-level plans arrive
// with qaRun == nil; executor-backed levels pass the run payload through so
// plan-manager persists it atomically with the status change.
//
// Idempotency: PLAN_STATES KV PUTs fire on every plan revision bump — status
// changes, ExecutionSummary updates, LastError annotations, SSE notifications.
// Without dedup we would respawn a review (and a full LLM dispatch) on every
// unrelated plan write while the slug sits in ready_for_qa. The dispatchretry
// State doubles as the in-flight marker: presence means "review started; skip
// duplicate trigger." Dispatch-path lifecycle clears it on terminal verdict or
// retry exhaustion.
func (c *Component) processReview(ctx context.Context, plan *workflow.Plan, qaRun *workflow.QARun) {
	if _, fresh := c.retry.Track(plan.Slug, plan); !fresh {
		c.logger.Debug("QA review already in flight — dropping duplicate trigger",
			"slug", plan.Slug)
		return
	}

	// Claim the plan via mutation. Plan-manager persists QARun + status atomically.
	if err := c.publishQAStart(ctx, plan, qaRun); err != nil {
		c.logger.Error("Failed to claim plan for QA review",
			"slug", plan.Slug, "error", err)
		c.retry.Clear(plan.Slug)
		c.reviewsFailed.Add(1)
		c.publishFailClosedVerdict(ctx, plan, fmt.Sprintf("qa.start mutation failed: %v", err))
		return
	}

	// Reload the plan so the dispatcher sees the authoritative QARun + status.
	refreshed, err := c.loadPlanFromKV(ctx, plan.Slug)
	if err != nil {
		c.logger.Warn("QA start succeeded but reload failed — dispatching with cached plan",
			"slug", plan.Slug, "error", err)
		refreshed = plan
		if qaRun != nil {
			refreshed.QARun = qaRun
		}
	}

	c.dispatchReviewer(ctx, refreshed, "")
}

// publishQAStart sends plan.mutation.qa.start to plan-manager, asking it to
// transition ready_for_qa → reviewing_qa and attach the executor result.
func (c *Component) publishQAStart(ctx context.Context, plan *workflow.Plan, qaRun *workflow.QARun) error {
	req := struct {
		Slug   string          `json:"slug"`
		PlanID string          `json:"plan_id,omitempty"`
		QARun  *workflow.QARun `json:"qa_run,omitempty"`
	}{
		Slug:   plan.Slug,
		PlanID: plan.ID,
		QARun:  qaRun,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal qa.start request: %w", err)
	}

	resp, err := c.natsClient.RequestWithRetry(ctx, mutationQAStart, data,
		10*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		return fmt.Errorf("qa.start request: %w", err)
	}

	var mutResp struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(resp, &mutResp); err != nil {
		return fmt.Errorf("qa.start response parse: %w", err)
	}
	if !mutResp.Success {
		return fmt.Errorf("qa.start rejected: %s", mutResp.Error)
	}
	return nil
}

// dispatchReviewer dispatches the QA reviewer LLM agent via agentic-dispatch.
// Test data, if any, lives on plan.QARun (populated by plan-manager before the
// reviewing_qa transition). Synthesis-level plans have QARun == nil.
//
// previousError carries the parse / structural failure from a prior dispatch
// when this is a retry; empty on the first attempt. Threading it into the
// user prompt closes the blind-retry gap — the QA agent otherwise re-emits
// the same malformed verdict shape across the entire retry budget.
//
// Caller contract: c.retry must already track plan.Slug (placed by
// processReview's Track on initial dispatch; preserved through retryOrFail's
// Tick on retries). dispatchReviewer records the new loop's task ID so
// handleLoopCompletion can drop stale completions from older loops.
func (c *Component) dispatchReviewer(ctx context.Context, plan *workflow.Plan, previousError string) {
	c.updateLastActivity()

	taskID := fmt.Sprintf("qa-review-%s-%s", plan.Slug, uuid.New().String())
	c.retry.SetActiveLoop(plan.Slug, taskID)

	// Build QAReviewContext.
	qrc := buildQAReviewContext(plan)

	// Load role-filtered standards.
	stdCtx := prompt.LoadStandardsForRoleFromDisk(prompt.RolePlanQAReviewer)

	// Resolve model.
	capability := c.config.DefaultCapability
	if model.ParseCapability(capability) == "" {
		capability = string(model.CapabilityReviewing)
	}
	modelName := c.modelRegistry.Resolve(capability)

	// Resolve provider.
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

	// Build assembly context.
	asmCtx := &prompt.AssemblyContext{
		Role:               prompt.RolePlanQAReviewer,
		Provider:           provider,
		HasResponseFormat:  terminal.EndpointSupportsResponseFormat(endpoint),
		Domain:             "software",
		AvailableTools:     prompt.FilterTools(c.availableToolNames(), prompt.RolePlanQAReviewer),
		SupportsTools:      true,
		MaxTokens:          maxTokens,
		Standards:          stdCtx,
		QAReviewContext:    qrc,
		QAReviewerPrompt:   &prompt.QAReviewerPromptContext{Plan: plan, PreviousError: previousError, QAReviewContext: qrc},
		Persona:            prompt.GlobalPersonas().ForRole(prompt.RolePlanQAReviewer),
		Vocabulary:         prompt.GlobalPersonas().Vocabulary(),
		ContractProjection: prompt.QAContractProjection(plan),
	}

	// Load role-scoped lessons.
	if c.lessonWriter != nil {
		graphCtx := context.WithoutCancel(ctx)
		if roleLessons, err := c.lessonWriter.RotateLessonsForRole(graphCtx, string(prompt.RolePlanQAReviewer), 10); err == nil && len(roleLessons) > 0 {
			ll := &prompt.LessonsLearned{}
			for _, les := range roleLessons {
				ll.Lessons = append(ll.Lessons, prompt.LessonEntry{
					Category:      les.Source,
					Summary:       les.Summary,
					InjectionForm: les.InjectionForm,
					Positive:      les.Positive,
					Role:          les.Role,
				})
			}
			asmCtx.LessonsLearned = ll
		}
	}

	assembled := c.assembler.Assemble(asmCtx)
	if assembled.RenderError != nil {
		c.logger.Error("QA-reviewer user-prompt render failed", "slug", plan.Slug, "error", assembled.RenderError)
		c.publishFailClosedVerdict(ctx, plan, fmt.Sprintf("qa-reviewer prompt render failed: %v", assembled.RenderError))
		return
	}

	// Wire palette filtered by RolePlanQAReviewer — see take-11 fix
	// in execution-manager for rationale.
	qaTools := prompt.FilterTools(c.availableToolNames(), prompt.RolePlanQAReviewer)

	// Point Murat's bash inspection at the per-plan QA worktree (a checkout of
	// the assembled plan branch holding the merged per-requirement
	// implementation), staged by plan-manager at convergence and ensured at
	// review start. Fall back to the repo root ("main") only when there is no
	// assembled branch — a plan that reached review before the assemble-before-QA
	// path existed. Gating on AssembledBranch avoids a 404 storm: the sandbox's
	// exec endpoint hard-fails every command when a named worktree is absent,
	// unlike the unit runner which degrades gracefully.
	qaTaskID := "main"
	if plan.AssembledBranch != "" {
		qaTaskID = workflow.QAWorktreeID(plan.Slug)
	}

	task := &agentic.TaskMessage{
		TaskID: taskID,
		Role:   agentic.RoleReviewer,
		Model:  modelName,
		Prompt: assembled.UserMessage,
		Tools:  terminal.ToolsForEndpoint(c.toolRegistry, "qa-review", endpoint, qaTools...),
		// ToolChoice forces the model to call a tool every iteration —
		// without it, the model can return plain content and the loop
		// terminates with empty/unparseable loop.Result. Caught take 14
		// (2026-05-08): qa-reviewer dispatched twice, both came back with
		// "no JSON found in result" within 3 seconds (no real LLM work);
		// model returned text without calling submit_work. ResolveToolChoice
		// for RolePlanQAReviewer returns Mode:"required" (must use a tool;
		// model picks from palette). submit_work is the only StopLoop tool,
		// so the loop will eventually terminate via submit_work whose args
		// JSON becomes loop.Result for the parser.
		ToolChoice:   prompt.ResolveToolChoice(prompt.RolePlanQAReviewer, qaTools),
		WorkflowSlug: workflow.WorkflowSlugPlanning,
		WorkflowStep: stepQAReviewing,
		Context: &agentic.ConstructedContext{
			Content: assembled.SystemMessage,
		},
		Metadata: map[string]any{
			"plan_slug":        plan.Slug,
			"qa_level":         string(plan.EffectiveQALevel()),
			"task_id":          qaTaskID,
			"deliverable_type": "qa-review",
			// role + model for SKG tool.recovery.incident partitioning.
			"role":  string(prompt.RoleQA),
			"model": modelName,
		},
		ResponseFormat: terminal.ResponseFormatForEndpoint(endpoint, "qa-review"),
	}

	baseMsg := message.NewBaseMessage(task.Schema(), task, "semspec-qa-reviewer")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.reviewsFailed.Add(1)
		c.logger.Error("Failed to marshal QA review task message, rejecting plan",
			"slug", plan.Slug, "error", err)
		c.publishFailClosedVerdict(ctx, plan, fmt.Sprintf("qa-reviewer dispatch marshal failed: %v", err))
		return
	}

	if err := c.natsClient.PublishToStream(ctx, subjectQAReviewTask, data); err != nil {
		c.reviewsFailed.Add(1)
		c.logger.Error("Failed to dispatch QA reviewer agent, rejecting plan",
			"slug", plan.Slug, "error", err)
		c.publishFailClosedVerdict(ctx, plan, fmt.Sprintf("qa-reviewer dispatch failed: %v", err))
		return
	}

	c.logger.Info("Dispatched QA reviewer agent",
		"slug", plan.Slug,
		"task_id", taskID,
		"model", modelName,
		"qa_level", plan.EffectiveQALevel(),
		"fragments", len(assembled.FragmentsUsed),
		"system_chars", assembled.SystemMessageChars)
}

// handleLoopCompletion processes a completed QA reviewer agent loop.
func (c *Component) handleLoopCompletion(ctx context.Context, loop *agentic.LoopEntity, slug string) {
	c.updateLastActivity()

	// Stale loop guard: drop completion events from older dispatches. retryOrFail
	// re-dispatches with a fresh task ID; if a slow stale loop completes after
	// the new one starts, processing it would double-fire retryOrFail and the
	// retry storm reappears.
	if c.retry.IsStaleLoop(slug, loop.TaskID) {
		c.logger.Debug("Dropping stale QA loop completion (task ID mismatch)",
			"slug", slug, "loop_task_id", loop.TaskID, "loop_id", loop.ID)
		return
	}

	// Stale completion guard: if the plan has moved past reviewing state, discard.
	var currentPlan *workflow.Plan
	if planJSON, loadErr := c.loadPlanFromKV(ctx, slug); loadErr == nil {
		status := planJSON.EffectiveStatus()
		if status != workflow.StatusReviewingRollup && status != workflow.StatusReviewingQA {
			c.logger.Warn("Plan not in reviewing state, discarding stale QA review result",
				"slug", slug, "current_status", status, "loop_id", loop.ID)
			c.retry.Clear(slug)
			return
		}
		currentPlan = planJSON
	}

	if loop.Outcome != agentic.OutcomeSuccess {
		errMsg := loop.Error
		if errMsg == "" {
			errMsg = fmt.Sprintf("qa-review agent loop ended with outcome %q", loop.Outcome)
		}
		c.logger.Error("QA reviewer agent loop failed",
			"slug", slug, "loop_id", loop.ID, "outcome", loop.Outcome, "error", errMsg)
		trajectory.LogSummary(ctx, c.logger, c.natsClient, loop.ID, "qa-loop-failed", 0)
		c.retryOrFail(ctx, slug, errMsg)
		return
	}

	result, err := parseQAReviewResult(loop.Result)
	if err != nil {
		// Emit ADR-035 CP-1 incident so the unparseable response is
		// captured as audited telemetry before it scrolls off. Without
		// this the WARN log alone has no body — the model's actual
		// output is lost. Caught take 14 (2026-05-08): two consecutive
		// QA dispatches both failed parse, the snapshot interval missed
		// the trajectories, and we had nothing to debug from. The
		// preview log is defensive on top of the SKG emit.
		c.emitParseIncident(ctx, "qa", loop.ID, loop.Model, loop.Result, err)
		c.logger.Warn("Failed to parse QA review result, retrying",
			"slug", slug, "loop_id", loop.ID, "error", err,
			"result_len", len(loop.Result),
			"result_preview", truncatePreview(loop.Result, 240))
		trajectory.LogSummary(ctx, c.logger, c.natsClient, loop.ID, "qa-parse-failed", 0)
		c.retryOrFail(ctx, slug, fmt.Sprintf("failed to parse qa-review result: %v", err))
		return
	}

	// Recover plan from retry payload when KV load above failed.
	plan := currentPlan
	if plan == nil {
		if snap, ok := c.retry.Snapshot(slug); ok {
			if p, ok2 := snap.Payload.(*workflow.Plan); ok2 {
				plan = p
			}
		}
	}

	hadTestData := plan != nil && plan.QARun != nil
	c.logger.Info("QA reviewer agent complete",
		"slug", slug, "verdict", result.Verdict, "summary", result.Summary,
		"had_test_data", hadTestData)

	verdict := buildQAVerdictEvent(slug, plan, result)
	// Record the lesson on the PUBLISHED verdict, not the raw agent output: the
	// skip-guard can coerce approved→needs_changes, and we want the reviewer to
	// learn from exactly that failure mode (it approved a skipped run).
	c.recordQARejectionLesson(ctx, slug, verdict.Verdict, verdict.Summary, result)
	c.publishQAVerdict(ctx, verdict)
	// Clear retry state AFTER the verdict mutation is dispatched. publishQAVerdict
	// doesn't currently surface mutation errors back here (logged + swallowed),
	// so the placement is defensive — keeps the pattern consistent with the
	// other generator/reviewer components and lets future retry-on-publish
	// wiring use retryOrFail without losing context.
	c.retry.Clear(slug)
}

// buildQAVerdictEvent assembles the QAVerdictEvent published to plan-manager.
// Pure function — no NATS, no I/O.
func buildQAVerdictEvent(slug string, plan *workflow.Plan, result *qaReviewOutput) *workflow.QAVerdictEvent {
	level := workflow.QALevelSynthesis
	if plan != nil {
		level = plan.EffectiveQALevel()
	}
	if forced := forcedExecutableQAVerdict(slug, plan, level); forced != nil {
		return forced
	}

	// Deterministic safety net over the LLM's skip judgment: the agent is told to
	// never return `approved` when tests were skipped (it must use
	// conditionally_approved for legitimate deferrals or needs_changes for
	// gaming). reconcileSkipVerdict enforces that invariant in code so a
	// misbehaving review can't produce a false all-green.
	var qaRun *workflow.QARun
	if plan != nil {
		qaRun = plan.QARun
	}
	finalVerdict, skipNote := reconcileSkipVerdict(result.Verdict, qaRun)

	verdict := &workflow.QAVerdictEvent{
		Slug:    slug,
		Level:   level,
		Verdict: finalVerdict,
		Summary: appendVerdictNote(result.Summary, skipNote),
		Dimensions: workflow.QAVerdictDimensions{
			RequirementFulfillment: result.Dimensions.RequirementFulfillment,
			CapabilityEvidence:     result.Dimensions.CapabilityEvidence,
			Coverage:               result.Dimensions.Coverage,
			AssertionQuality:       result.Dimensions.AssertionQuality,
			RegressionSurface:      result.Dimensions.RegressionSurface,
			FlakeJudgment:          result.Dimensions.FlakeJudgment,
		},
	}
	if plan != nil {
		verdict.PlanID = plan.ID
	}
	if finalVerdict != workflow.QAVerdictNeedsChanges || len(result.PlanDecisions) == 0 {
		return verdict
	}
	now := time.Now()
	for _, cp := range result.PlanDecisions {
		proposal := workflow.PlanDecision{
			ID:             fmt.Sprintf("plan-decision.%s.qa.%s", slug, uuid.New().String()[:8]),
			PlanID:         workflow.PlanEntityID(slug),
			Title:          cp.Title,
			Rationale:      cp.Rationale,
			Status:         workflow.PlanDecisionStatusProposed,
			ProposedBy:     "qa-reviewer",
			AffectedReqIDs: cp.AffectedReqIDs,
			CreatedAt:      now,
		}
		for _, ar := range cp.ArtifactRefs {
			proposal.ArtifactReferences = append(proposal.ArtifactReferences, workflow.ArtifactRef{
				Path:    ar.Path,
				Type:    ar.Type,
				Purpose: ar.Purpose,
			})
		}
		verdict.PlanDecisions = append(verdict.PlanDecisions, proposal)
		verdict.PlanDecisionIDs = append(verdict.PlanDecisionIDs, proposal.ID)
	}
	return verdict
}

// reconcileSkipVerdict is the deterministic safety net over the LLM's skip
// judgment. By the time it runs, the executor reported Passed (build + every
// executed test succeeded — real failures already short-circuited to red in
// forcedExecutableQAVerdict). It enforces, in code, the invariant that a run
// with skipped tests is never reported as a full all-green `approved`:
//
//   - approved + skips present          → needs_changes (fail closed: the agent
//     approved without classifying the skips; bias to red so a retry/human
//     resolves it — a skipped test must never silently pass as green)
//   - conditionally_approved + no skips → approved (nothing was deferred)
//   - everything else passes through unchanged
//
// Returns the reconciled verdict and an optional note appended to the summary.
func reconcileSkipVerdict(raw string, qaRun *workflow.QARun) (workflow.QAVerdict, string) {
	v := workflow.QAVerdict(raw)
	hasSkips := qaRun != nil && len(qaRun.SkippedTests) > 0
	switch {
	case hasSkips && v == workflow.QAVerdictApproved:
		return workflow.QAVerdictNeedsChanges, fmt.Sprintf(
			"[skip-guard] coerced approved→needs_changes: %d test(s) were skipped but the review approved without classifying them; a run with skipped tests cannot be all-green. Classify each skip (legitimate sandbox limitation → conditionally_approved; gaming → needs_changes).",
			len(qaRun.SkippedTests))
	case !hasSkips && v == workflow.QAVerdictConditionallyApproved:
		// Nothing was actually deferred — a conditional approval with no skips is
		// just an approval.
		return workflow.QAVerdictApproved, ""
	default:
		return v, ""
	}
}

// appendVerdictNote joins a coercion note onto the reviewer's summary.
func appendVerdictNote(summary, note string) string {
	switch {
	case note == "":
		return summary
	case summary == "":
		return note
	default:
		return summary + "\n\n" + note
	}
}

func forcedExecutableQAVerdict(slug string, plan *workflow.Plan, level workflow.QALevel) *workflow.QAVerdictEvent {
	if plan == nil || !level.UsesSandboxTests() {
		return nil
	}
	base := &workflow.QAVerdictEvent{
		Slug:   slug,
		PlanID: plan.ID,
		Level:  level,
		Dimensions: workflow.QAVerdictDimensions{
			Coverage:          "not satisfied",
			AssertionQuality:  "not satisfied",
			RegressionSurface: "not satisfied",
		},
	}
	if plan.QARun == nil {
		base.Verdict = workflow.QAVerdictRejected
		base.Summary = fmt.Sprintf("QA level %s requires an executor result, but no QARun was attached. Refusing to approve without executed test evidence.", level)
		base.ReviewerError = "missing qa executor result"
		return base
	}
	if plan.QARun.RunnerError != "" {
		base.Verdict = workflow.QAVerdictRejected
		base.Summary = fmt.Sprintf("QA executor failed at level %s: %s", level, plan.QARun.RunnerError)
		base.ReviewerError = plan.QARun.RunnerError
		return base
	}
	if !plan.QARun.Passed {
		plan.QARun.Failures = classifyQAFailures(plan.QARun.Failures)
		base.Verdict = workflow.QAVerdictNeedsChanges
		base.Summary = summarizeQAFailures(level, plan.QARun)
		return base
	}
	return nil
}

func summarizeQAFailures(level workflow.QALevel, run *workflow.QARun) string {
	if run == nil {
		return fmt.Sprintf("QA level %s did not produce a run result.", level)
	}
	if len(run.Failures) == 0 {
		return fmt.Sprintf("QA level %s failed: executor reported passed=false with no structured failures.", level)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "QA level %s failed with %d failure(s).", level, len(run.Failures))
	for i, failure := range run.Failures {
		if i >= 3 {
			fmt.Fprintf(&b, " %d additional failure(s) omitted.", len(run.Failures)-i)
			break
		}
		fmt.Fprintf(&b, " [%s]", failure.JobName)
		if failure.TestName != "" {
			fmt.Fprintf(&b, " %s", failure.TestName)
		}
		if failure.Category != "" {
			fmt.Fprintf(&b, " category=%s", failure.Category)
		}
		if failure.Message != "" {
			fmt.Fprintf(&b, ": %s", failure.Message)
		}
		excerpt := strings.TrimSpace(failure.LogExcerpt)
		if excerpt != "" {
			if len(excerpt) > 240 {
				excerpt = excerpt[:240] + "..."
			}
			fmt.Fprintf(&b, " Excerpt: %s", excerpt)
		}
	}
	return b.String()
}

// recordQARejectionLesson persists a role-scoped lesson when the verdict
// is needs_changes, so future qa-reviewer prompts learn from the rejection.
// No-op when lesson writing is disabled or the verdict is approval.
//
// ADR-033 Phase 3: each lesson now carries EvidenceFiles citations. Prefer
// the agent's plan_decisions[].artifact_refs[].path entries when present
// (those are the actual logs/screenshots/traces the agent surfaced as
// evidence); otherwise fall back to a stable citation pointing at the plan
// artifact (`.semspec/plans/<slug>/plan.json`) so the writer's evidence
// requirement is satisfied.
func (c *Component) recordQARejectionLesson(ctx context.Context, slug string, finalVerdict workflow.QAVerdict, finalSummary string, result *qaReviewOutput) {
	if finalVerdict != workflow.QAVerdictNeedsChanges || c.lessonWriter == nil {
		return
	}
	evidenceFiles := qaEvidenceFiles(slug, result)
	lesson := workflow.Lesson{
		Source:        "qa-review",
		ScenarioID:    slug,
		Summary:       fmt.Sprintf("QA rejection: %s", finalSummary),
		Role:          string(prompt.RolePlanQAReviewer),
		EvidenceFiles: evidenceFiles,
	}
	if err := c.lessonWriter.RecordLesson(context.WithoutCancel(ctx), lesson); err != nil {
		c.logger.Warn("Failed to record qa-reviewer lesson", "slug", slug, "error", err)
	}
}

// qaEvidenceFiles selects FileRef citations for a QA-rejection lesson.
// Pulls workspace-relative paths from plan_decisions.artifact_refs first
// (deduped), then falls back to the plan artifact when none are present —
// the latter is a stable citation regardless of artifact availability.
func qaEvidenceFiles(slug string, result *qaReviewOutput) []workflow.FileRef {
	planRef := workflow.FileRef{Path: ".semspec/plans/" + slug + "/plan.json"}
	if result == nil {
		return []workflow.FileRef{planRef}
	}
	seen := map[string]bool{}
	var out []workflow.FileRef
	for _, pd := range result.PlanDecisions {
		for _, ar := range pd.ArtifactRefs {
			if ar.Path == "" || seen[ar.Path] {
				continue
			}
			seen[ar.Path] = true
			out = append(out, workflow.FileRef{Path: ar.Path})
		}
	}
	if len(out) == 0 {
		return []workflow.FileRef{planRef}
	}
	return out
}

// retryOrFail attempts to re-dispatch the QA review. When MaxReviewRetries is
// exhausted it publishes a fail-closed rejected verdict.
func (c *Component) retryOrFail(ctx context.Context, slug, errorMsg string) {
	entry, retryOK := c.retry.Tick(ctx, slug)
	if entry == nil {
		// Either no retry state (fail-closed without payload) or ctx canceled
		// during backoff (graceful shutdown — don't fire a verdict).
		if ctx.Err() != nil {
			c.logger.Debug("retryOrFail aborted during backoff", "slug", slug, "error", ctx.Err())
			return
		}
		c.logger.Warn("retryOrFail: no retry state for slug, failing immediately", "slug", slug)
		c.reviewsFailed.Add(1)
		c.publishQAVerdict(ctx, &workflow.QAVerdictEvent{
			Slug:          slug,
			Level:         workflow.QALevelSynthesis,
			Verdict:       workflow.QAVerdictRejected,
			Summary:       "QA review failed: no retry context available",
			ReviewerError: errorMsg,
		})
		return
	}

	plan, _ := entry.Payload.(*workflow.Plan)

	if !retryOK {
		c.logger.Warn("QA review exhausted retries",
			"slug", slug, "attempts", entry.Count, "max", c.config.MaxReviewRetries,
			"last_error", errorMsg)
		c.reviewsFailed.Add(1)
		c.publishFailClosedVerdict(ctx, plan, errorMsg)
		return
	}

	c.logger.Info("Retrying QA review",
		"slug", slug, "attempt", entry.Count, "max", c.config.MaxReviewRetries,
		"previous_error", errorMsg)

	c.dispatchReviewer(ctx, plan, errorMsg)
}

// publishFailClosedVerdict publishes a rejected verdict when the LLM agent fails.
// Fail closed: never silently approve on error.
func (c *Component) publishFailClosedVerdict(ctx context.Context, plan *workflow.Plan, errMsg string) {
	evt := &workflow.QAVerdictEvent{
		Verdict:       workflow.QAVerdictRejected,
		Summary:       fmt.Sprintf("QA review agent failed — escalating to human: %s", errMsg),
		ReviewerError: errMsg,
	}
	if plan != nil {
		evt.Slug = plan.Slug
		evt.PlanID = plan.ID
		evt.Level = plan.EffectiveQALevel()
	}
	c.publishQAVerdict(ctx, evt)
}

// loadPlanFromKV reads a plan from PLAN_STATES by slug.
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
		return nil, fmt.Errorf("unmarshal plan: %w", err)
	}
	return &plan, nil
}

// ---------------------------------------------------------------------------
// Verdict publishing
// ---------------------------------------------------------------------------

// publishQAVerdict sends plan.mutation.qa.verdict to plan-manager.
func (c *Component) publishQAVerdict(ctx context.Context, verdict *workflow.QAVerdictEvent) {
	data, err := json.Marshal(verdict)
	if err != nil {
		c.reviewsFailed.Add(1)
		c.logger.Error("Failed to marshal QA verdict",
			"slug", verdict.Slug, "error", err)
		return
	}

	resp, err := c.natsClient.RequestWithRetry(ctx, mutationQAVerdict, data,
		10*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		c.reviewsFailed.Add(1)
		c.logger.Error("Failed to send QA verdict mutation",
			"slug", verdict.Slug, "error", err)
		return
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
		c.reviewsFailed.Add(1)
		c.logger.Error("QA verdict mutation rejected by plan-manager",
			"slug", verdict.Slug, "error", errMsg)
		return
	}

	c.logger.Info("QA verdict accepted by plan-manager",
		"slug", verdict.Slug, "verdict", verdict.Verdict, "level", verdict.Level)
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// buildQAReviewContext constructs the prompt context from plan and QA run data.
// Test results live on plan.QARun (populated by plan-manager when it consumed
// QACompletedEvent). Synthesis-level plans have QARun == nil.
func buildQAReviewContext(plan *workflow.Plan) *prompt.QAReviewContext {
	qrc := &prompt.QAReviewContext{
		PlanTitle:       plan.Title,
		PlanGoal:        plan.Goal,
		PlanConstraints: append([]string(nil), plan.Constraints...),
		QALevel:         plan.EffectiveQALevel(),
	}

	// Populate requirements summary.
	for _, r := range plan.Requirements {
		if r.Status == workflow.RequirementStatusDeprecated {
			continue
		}
		qrc.Requirements = append(qrc.Requirements, prompt.RequirementSummary{
			Title:  r.Title,
			Status: string(r.Status),
		})
	}

	// ADR-044 M:N evidence rollup: per-Story summary + per-Capability
	// covering-Story index. QA-reviewer's release-readiness verdict shifts
	// from "all requirements complete" to "every capability has evidence
	// from at least one shipped Story."
	qrc.Stories = buildQAStorySummaries(plan)
	qrc.Capabilities = buildQACapabilityEvidence(plan)

	// Pull test surface from architecture if available.
	if plan.Architecture != nil {
		qrc.TestSurface = plan.Architecture.TestSurface
	}

	// Populate from the persisted QA run when available.
	if plan.QARun != nil {
		qrc.Passed = plan.QARun.Passed
		qrc.Failures = plan.QARun.Failures
		qrc.SkippedTests = plan.QARun.SkippedTests
		qrc.Artifacts = plan.QARun.Artifacts
		qrc.RunnerError = plan.QARun.RunnerError
	}

	return qrc
}

// buildQAStorySummaries projects plan.Stories into the per-Story rollup
// the QA-reviewer prompt surfaces under ADR-044.
func buildQAStorySummaries(plan *workflow.Plan) []prompt.QAStorySummary {
	if plan == nil || len(plan.Stories) == 0 {
		return nil
	}
	out := make([]prompt.QAStorySummary, 0, len(plan.Stories))
	for _, s := range plan.Stories {
		out = append(out, prompt.QAStorySummary{
			ID:              s.ID,
			Title:           s.Title,
			ComponentName:   s.ComponentName,
			RequirementIDs:  append([]string(nil), s.RequirementIDs...),
			CapabilityNames: append([]string(nil), s.CapabilityNames...),
			Status:          string(s.Status),
		})
	}
	return out
}

// buildQACapabilityEvidence walks plan.Exploration.Capabilities and joins
// against plan.Stories[*].CapabilityNames to surface, per Capability, the
// list of Stories that claim coverage + how many of those Stories actually
// reached terminal-complete. Empty CoveringStoryIDs is a coverage gap —
// the capability is declared but no Story claims it. Zero ShippedCount
// is an evidence gap — Stories claim coverage but none has shipped.
func buildQACapabilityEvidence(plan *workflow.Plan) []prompt.QACapabilityEvidence {
	if plan == nil || plan.Exploration == nil || len(plan.Exploration.Capabilities) == 0 {
		return nil
	}
	out := make([]prompt.QACapabilityEvidence, 0, len(plan.Exploration.Capabilities))
	for _, cap := range plan.Exploration.Capabilities {
		entry := prompt.QACapabilityEvidence{
			Name:        cap.Name,
			Description: cap.Description,
		}
		for _, s := range plan.Stories {
			for _, cn := range s.CapabilityNames {
				if cn != cap.Name {
					continue
				}
				entry.CoveringStoryIDs = append(entry.CoveringStoryIDs, s.ID)
				if s.Status == workflow.StoryStatusComplete {
					entry.ShippedCount++
				}
				break
			}
		}
		out = append(out, entry)
	}
	return out
}

// buildUserPrompt is a thin wrapper around the registry path so the existing
// component tests can stay focused on prompt content without going through
// the full assembler. The actual prompt body lives in
// prompt/domain/software_render.go::renderQAReviewerPrompt; this helper
// exists only to keep component_test.go callable. Production dispatch uses
// assembled.UserMessage from the assembler.
func buildUserPrompt(plan *workflow.Plan) string {
	r := prompt.NewRegistry()
	r.RegisterAll(promptdomain.Software()...)
	a := prompt.NewAssembler(r)
	qrc := buildQAReviewContext(plan)
	out := a.Assemble(&prompt.AssemblyContext{
		Role:             prompt.RolePlanQAReviewer,
		Provider:         prompt.ProviderOpenAI,
		QAReviewContext:  qrc,
		QAReviewerPrompt: &prompt.QAReviewerPromptContext{Plan: plan, QAReviewContext: qrc},
	})
	return out.UserMessage
}

// qaReviewOutput is the parsed submit_work output from the QA reviewer agent.
type qaReviewOutput struct {
	Verdict string `json:"verdict"`
	Summary string `json:"summary"`

	Dimensions struct {
		RequirementFulfillment string `json:"requirement_fulfillment"`
		CapabilityEvidence     string `json:"capability_evidence"`
		Coverage               string `json:"coverage"`
		AssertionQuality       string `json:"assertion_quality"`
		RegressionSurface      string `json:"regression_surface"`
		FlakeJudgment          string `json:"flake_judgment"`
	} `json:"dimensions"`

	PlanDecisions []qaPlanDecision `json:"plan_decisions"`
}

// emitParseIncident writes ADR-035 CP-1 telemetry when QA review parse
// fails. Mirrors the requirement-executor.emitParseIncident shape:
// best-effort SKG triple emit so the unparseable response is captured
// for post-mortem analysis. nil tripleWriter is a no-op (test-only
// path with no graph wiring).
//
// Without this, a parse failure leaves only a WARN log line — the
// model's actual response text is gone and operators have no way to
// classify the failure (empty content vs malformed JSON vs non-JSON
// text). Take 14 (2026-05-08) hit exactly that gap.
func (c *Component) emitParseIncident(ctx context.Context, role, loopID, model, rawResponse string, parseErr error) {
	if c.tripleWriter == nil {
		return
	}
	ic := parseincident.IncidentContext{
		CallID: loopID,
		Role:   role,
		Model:  model,
	}
	if _, err := parseincident.EmitForResult(
		ctx,
		c.tripleWriter,
		ic,
		observability.CheckpointResponseParse,
		nil, // no quirks tracked for QA parser yet
		rawResponse,
		parseErr,
	); err != nil {
		c.logger.Warn("CP-1 incident emit failed",
			"loop_id", loopID, "role", role, "error", err)
	}
}

// truncatePreview returns a single-line, length-capped view of s
// suitable for embedding in a structured log field. Newlines collapse
// to spaces so the WARN line stays parseable; max bounds the bytes so
// 50KB model dumps don't blow the log shipper.
func truncatePreview(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) == 0 {
		return ""
	}
	out := strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\r", " ")
	if len(out) > maxBytes {
		out = out[:maxBytes] + "…"
	}
	return out
}

// qaPlanDecision is a single change proposal from the agent's submit_work output.
type qaPlanDecision struct {
	Title          string   `json:"title"`
	Rationale      string   `json:"rationale"`
	AffectedReqIDs []string `json:"affected_requirement_ids"`
	RejectionType  string   `json:"rejection_type"`
	ArtifactRefs   []struct {
		Path    string `json:"path"`
		Type    string `json:"type"`
		Purpose string `json:"purpose"`
	} `json:"artifact_refs"`
}

// parseQAReviewResult extracts a qaReviewOutput from the agent's loop.Result.
// Tries direct JSON parse first; falls back to extracting the first JSON object.
func parseQAReviewResult(result string) (*qaReviewOutput, error) {
	if result == "" {
		return nil, fmt.Errorf("empty result")
	}

	var output qaReviewOutput

	// Direct parse.
	if err := json.Unmarshal([]byte(result), &output); err == nil {
		if isValidQAVerdict(output.Verdict) {
			return &output, nil
		}
	}

	// Extract JSON from text.
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

	if err := json.Unmarshal([]byte(result[start:end]), &output); err != nil {
		return nil, fmt.Errorf("parse qa-review JSON: %w", err)
	}

	if !isValidQAVerdict(output.Verdict) {
		return nil, fmt.Errorf("invalid qa verdict %q: must be approved|conditionally_approved|needs_changes|rejected", output.Verdict)
	}

	return &output, nil
}

// isValidQAVerdict returns true for the accepted verdict strings.
func isValidQAVerdict(v string) bool {
	return v == string(workflow.QAVerdictApproved) ||
		v == string(workflow.QAVerdictConditionallyApproved) ||
		v == string(workflow.QAVerdictNeedsChanges) ||
		v == string(workflow.QAVerdictRejected)
}

// availableToolNames returns the full list of tool names for prompt assembly.
func (c *Component) availableToolNames() []string {
	return []string{
		"bash", "submit_work", "ask_question",
		"graph_search", "graph_query", "graph_summary",
		"web_search", "http_request",
	}
}

// resolveProvider determines the LLM provider for prompt formatting.
func (c *Component) resolveProvider() prompt.Provider {
	capability := c.config.DefaultCapability
	if model.ParseCapability(capability) == "" {
		capability = string(model.CapabilityReviewing)
	}
	modelName := c.modelRegistry.Resolve(capability)
	if endpoint := c.modelRegistry.GetEndpoint(modelName); endpoint != nil {
		return prompt.Provider(endpoint.Provider)
	}
	return prompt.ProviderOllama
}

// ---------------------------------------------------------------------------
// Component.Discoverable implementation
// ---------------------------------------------------------------------------

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "qa-reviewer",
		Type:        "processor",
		Description: "Renders release-readiness verdict via Murat QA Test Architect persona; scoped by project qa.level",
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
	return qaReviewerSchema
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
