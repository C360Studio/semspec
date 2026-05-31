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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/c360studio/semspec/internal/trajectory"
	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/prompt"
	promptdomain "github.com/c360studio/semspec/prompt/domain"
	"github.com/c360studio/semspec/tools/sandbox"
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
	// stepReviewing is the workflow step for plan review.
	stepReviewing = "reviewing"

	// subjectReviewTask is the NATS subject for review agent tasks.
	subjectReviewTask = "agent.task.reviewer"
)

// reviewRetryPayload carries the per-key context retryOrFail needs to
// re-dispatch a review attempt: the plan JSON to re-prompt with, and the
// round (R1=draft review, R2=scenario review). Stored as the Payload field
// of a dispatchretry.Entry; round comes back via type assertion on retry.
type reviewRetryPayload struct {
	planContent string
	round       reviewRound
}

// Component implements the plan-reviewer processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	modelRegistry   ssmodel.RegistryReader
	toolRegistry    component.ToolRegistryReader
	assembler       *prompt.Assembler
	lessonWriter    *lessons.Writer
	tripleWriter    *graphutil.TripleWriter // ADR-035 CP-1 incident emit
	sandboxClient   *sandbox.Client         // ground-truth project file tree fetch (take 20 fix)
	errorCategories *workflow.ErrorCategoryRegistry

	// retry tracks per-plan attempts keyed by slug; payload is *reviewRetryPayload.
	retry *dispatchretry.State

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

	// Initialize prompt assembler with software domain.
	registry := prompt.NewRegistry()
	registry.RegisterAll(promptdomain.Software()...)
	registry.Register(prompt.ToolGuidanceFragment(prompt.DefaultToolGuidance()))
	registry.Register(prompt.GraphManifestFragment(workflowtools.RegistrySummaryFetchFn()))
	assembler := prompt.NewAssembler(registry)

	tw := &graphutil.TripleWriter{
		NATSClient:    deps.NATSClient,
		Logger:        logger,
		ComponentName: "plan-reviewer",
	}

	// Load error categories for lesson classification.
	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		repoRoot, _ = os.Getwd()
	}
	var errorCats *workflow.ErrorCategoryRegistry
	if reg, loadErr := workflow.LoadErrorCategories(repoRoot + "/configs/error_categories.json"); loadErr == nil {
		errorCats = reg
	}

	var sandboxClient *sandbox.Client
	if config.SandboxURL != "" {
		sandboxClient = sandbox.NewClient(config.SandboxURL)
	}

	return &Component{
		name:            "plan-reviewer",
		config:          config,
		natsClient:      deps.NATSClient,
		logger:          logger,
		modelRegistry:   deps.ModelRegistry,
		toolRegistry:    deps.ToolRegistry,
		assembler:       assembler,
		lessonWriter:    &lessons.Writer{TW: tw, Logger: logger},
		tripleWriter:    tw,
		sandboxClient:   sandboxClient,
		errorCategories: errorCats,
		retry: dispatchretry.New(dispatchretry.Config{
			MaxRetries: config.MaxReviewRetries,
			BackoffMs:  config.RetryBackoffMs,
		}),
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized plan-reviewer",
		"plan_state_bucket", c.config.PlanStateBucket)
	return nil
}

// Start begins watching for plan state transitions that require a review pass.
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
		"plan_state_bucket", c.config.PlanStateBucket)

	return nil
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

	// Stale loop guard: drop completions from older dispatches that race a retry.
	if c.retry.IsStaleLoop(slug, loop.TaskID) {
		c.logger.Debug("Dropping stale review loop completion (task ID mismatch)",
			"slug", slug, "loop_task_id", loop.TaskID, "loop_id", loop.ID, "round", round)
		return
	}

	// Stale completion guard: if the plan has moved past reviewing state,
	// our result is stale — discard without sending mutations.
	if planJSON, loadErr := c.loadPlanContentFromKV(ctx, slug); loadErr == nil {
		var plan workflow.Plan
		if json.Unmarshal([]byte(planJSON), &plan) == nil {
			status := plan.EffectiveStatus()
			if status != workflow.StatusReviewingDraft && status != workflow.StatusReviewingScenarios {
				c.logger.Warn("Plan not in reviewing state, discarding stale review result",
					"slug", slug,
					"current_status", status,
					"loop_id", loop.ID)
				c.retry.Clear(slug)
				return
			}
		}
	}

	if loop.Outcome != agentic.OutcomeSuccess {
		errMsg := loop.Error
		if errMsg == "" {
			errMsg = fmt.Sprintf("review agent loop ended with outcome %q", loop.Outcome)
		}
		c.logger.Error("Review agent loop failed",
			"slug", slug,
			"loop_id", loop.ID,
			"round", round,
			"outcome", loop.Outcome,
			"error", errMsg)
		trajectory.LogSummary(ctx, c.logger, c.natsClient, loop.ID, "plan-review-loop-failed", 0)
		c.retryOrFail(ctx, slug, round, errMsg)
		return
	}

	result, err := parseReviewFromResult(loop.Result)
	if err != nil {
		// CP-1 incident emit (ADR-035) — capture the unparseable response
		// as audited telemetry. Same defensive shape as qa-reviewer; same
		// rationale (take 14, 2026-05-08): without this, a parse failure
		// on a plan-reviewer dispatch would leave only a WARN line and
		// the actual model output would be lost.
		c.emitParseIncident(ctx, "plan-reviewer", loop.ID, loop.Model, loop.Result, err)
		c.logger.Warn("Failed to parse review from agent result, retrying",
			"slug", slug,
			"loop_id", loop.ID,
			"round", round,
			"error", err,
			"result_len", len(loop.Result),
			"result_preview", truncatePreview(loop.Result, 240))
		trajectory.LogSummary(ctx, c.logger, c.natsClient, loop.ID, "plan-review-parse-failed", 0)
		c.retryOrFail(ctx, slug, round, fmt.Sprintf("failed to parse review result: %v", err))
		return
	}

	// ADR-040 Move 2 + ADR-041 Move 4: merge deterministic findings before
	// the verdict is acted on. The LLM reviewer might miss a docs-only
	// capability, a depends_on cycle, an untagged scenario, or a missing
	// @integration for a services-class harness; the structural rules
	// don't. NormalizeVerdict in each merge bumps "approved" to
	// "needs_changes" when any error finding lands.
	if planForRules, loadErr := c.loadPlanForRules(ctx, slug); loadErr == nil {
		mergeCapabilityFindings(planForRules, result)
		mergeScenarioTagFindings(planForRules, result)
	} else {
		c.logger.Warn("Skipping capability + scenario-tag rules — plan load failed",
			"slug", slug, "error", loadErr)
	}

	c.logger.Info("Review agent complete",
		"slug", slug,
		"round", round,
		"verdict", result.Verdict,
		"summary", result.Summary,
		"findings", len(result.Findings))

	// Clear retry state AFTER the mutation is dispatched, not after parse.
	// Mutations on the verdict path don't currently surface validator
	// rejections back here (they go via sendGenerationFailed directly), so
	// the placement is defensive — it keeps the pattern consistent with
	// requirement-generator/scenario-generator/architecture-generator. If
	// a future hardening adds retry-on-mutation-failure, this placement
	// keeps the entry alive for retryOrFail to Tick.
	if result.IsApproved() {
		c.reviewsApproved.Add(1)
		if err := c.sendApprovalMutations(ctx, slug, result.Summary, round); err != nil {
			c.logger.Error("Failed to send approval mutations, rejecting plan",
				"slug", slug, "round", round, "error", err)
			c.retry.Clear(slug)
			c.sendGenerationFailed(ctx, slug, round, fmt.Sprintf("approval mutation failed: %v", err))
			return
		}
	} else {
		c.reviewsRejected.Add(1)
		c.sendRevisionMutation(ctx, slug, round, result)
		c.extractPlanLessons(ctx, slug, result)
	}
	c.retry.Clear(slug)
}

// dispatchReviewer dispatches a plan-reviewer agent loop via agentic-dispatch.
// previousError carries the parse / structural failure from a prior attempt
// when this is a retry; empty on the first dispatch. Threading it through
// closes the blind-retry gap (the reviewer otherwise sees the same prompt as
// the failed attempt and produces the same malformed output).
func (c *Component) dispatchReviewer(ctx context.Context, slug, planContent string, round reviewRound, previousError string) {
	c.updateLastActivity()

	// Seed retry state so retryOrFail can re-dispatch with the same params.
	// Track is no-op when the entry already exists (e.g. on retry re-entry),
	// preserving the running count.
	c.retry.Track(slug, &reviewRetryPayload{planContent: planContent, round: round})

	taskID := fmt.Sprintf("review-%s-r%d-%s", slug, round, uuid.New().String())
	c.retry.SetActiveLoop(slug, taskID)

	// Load role-filtered standards for the fragment pipeline.
	stdCtx := prompt.LoadStandardsForRoleFromDisk(prompt.RolePlanReviewer)
	hasStandards := stdCtx != nil && len(stdCtx.Items) > 0

	// Resolve model.
	capability := c.config.DefaultCapability
	if model.ParseCapability(capability) == "" {
		capability = string(model.CapabilityReviewing)
	}
	modelName := c.modelRegistry.Resolve(capability)

	// Assemble system prompt via fragment pipeline.
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
	// Extract prior-round context from the plan content (already a marshaled
	// workflow.Plan). plan-manager has stored ReviewFormattedFindings + the
	// iteration counter on every revision round; we unmarshal once here to
	// surface them to the reviewer prompt. Stateless across revisions
	// caused take-22's wedge: planner pass-2 addressed the prior finding,
	// reviewer pass-2 (with no memory of pass-1) re-rejected with the same
	// complaint shape and hit max_revisions → escalated.
	priorFindings, priorIteration := extractPriorReviewContext(planContent)

	asmCtx := &prompt.AssemblyContext{
		Role:              prompt.RolePlanReviewer,
		Provider:          provider,
		HasResponseFormat: terminal.EndpointSupportsResponseFormat(endpoint),
		Domain:            "software",
		AvailableTools:    prompt.FilterTools(c.availableToolNames(), prompt.RolePlanReviewer),
		SupportsTools:     true,
		MaxTokens:         maxTokens,
		Standards:         stdCtx,
		Persona:           prompt.GlobalPersonas().ForRole(prompt.RolePlanReviewer),
		Vocabulary:        prompt.GlobalPersonas().Vocabulary(),
		PlanReviewerPrompt: &prompt.PlanReviewerPromptContext{
			Slug:                slug,
			PlanContent:         planContent,
			HasStandards:        hasStandards,
			Round:               int(round),
			PreviousError:       previousError,
			ProjectFileTree:     c.fetchProjectFileTree(ctx),
			PreviousFindings:    priorFindings,
			ReviewIteration:     priorIteration,
			MaxReviewIterations: c.config.MaxReviewRetries,
		},
	}

	// Wire role-scoped lessons learned. plan-reviewer was the lone agentic
	// component without lesson injection (Phase 0 bug 0.2 in ADR-033). No
	// producer creates plan-reviewer-targeted lessons today (deferred to
	// Phase 1+ alongside the decomposer); this wire-up lands now so when the
	// producer ships it is a one-line change to make lessons reach this
	// prompt.
	if c.lessonWriter != nil {
		graphCtx := context.WithoutCancel(ctx)
		if roleLessons, err := c.lessonWriter.RotateLessonsForRole(graphCtx, string(prompt.RolePlanReviewer), 10); err == nil && len(roleLessons) > 0 {
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
		c.logger.Error("Plan-reviewer user-prompt render failed", "slug", slug, "round", round, "error", assembled.RenderError)
		return
	}

	// Wire palette filtered by RolePlanReviewer — see take-11 fix
	// in execution-manager for rationale.
	reviewTools := prompt.FilterTools(c.availableToolNames(), prompt.RolePlanReviewer)

	task := &agentic.TaskMessage{
		TaskID: taskID,
		Role:   agentic.RoleReviewer,
		Model:  modelName,
		Prompt: assembled.UserMessage,
		Tools:  terminal.ToolsForEndpoint(c.toolRegistry, "review", endpoint, reviewTools...),
		// ToolChoice forces a tool call every iteration so the model
		// can't terminate by returning plain content (which would leave
		// loop.Result empty/text and fail the parser). Plan-reviewer
		// shipped without this; take 14 (2026-05-08) it worked by luck
		// because qwen3.6-27b happened to use submit_work, but the
		// sibling qa-reviewer hit the failure mode the same run. Same
		// fix shape applied symmetrically.
		ToolChoice:   prompt.ResolveToolChoice(prompt.RolePlanReviewer, reviewTools),
		WorkflowSlug: workflow.WorkflowSlugPlanning,
		WorkflowStep: stepReviewing,
		Context: &agentic.ConstructedContext{
			Content: assembled.SystemMessage,
		},
		Metadata: map[string]any{
			"plan_slug":        slug,
			"review_round":     int(round),
			"task_id":          "main", // reviewer runs against main workspace, not a worktree
			"deliverable_type": "review",
			// role + model for SKG tool.recovery.incident partitioning
			// (see planner/component.go dispatchPlanner for the full rationale).
			"role":  string(prompt.RolePlanReviewer),
			"model": modelName,
		},
		ResponseFormat: terminal.ResponseFormatForEndpoint(endpoint, "review"),
	}

	baseMsg := message.NewBaseMessage(task.Schema(), task, "semspec-plan-reviewer")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.reviewsFailed.Add(1)
		c.logger.Error("Failed to marshal task message, rejecting plan", "slug", slug, "error", err)
		c.sendGenerationFailed(ctx, slug, round, fmt.Sprintf("reviewer dispatch marshal failed: %v", err))
		return
	}

	if err := c.natsClient.PublishToStream(ctx, subjectReviewTask, data); err != nil {
		c.reviewsFailed.Add(1)
		c.logger.Error("Failed to dispatch reviewer agent, rejecting plan", "slug", slug, "error", err)
		c.sendGenerationFailed(ctx, slug, round, fmt.Sprintf("reviewer dispatch failed: %v", err))
		return
	}

	c.logger.Info("Dispatched reviewer agent",
		"slug", slug,
		"task_id", taskID,
		"round", round,
		"model", modelName,
		"fragments", len(assembled.FragmentsUsed),
		"system_chars", assembled.SystemMessageChars)
}

// fetchProjectFileTree returns a ground-truth snapshot of the project's
// tracked files. Mirrors processor/planner/component.go fetchProjectFileTree
// — same `git ls-files | head -50` against the sandbox's "main" workspace,
// same .semspec/.git filter, same 5s timeout, same silent-skip on any
// failure. Reviewer-side fetch (rather than threading the planner's
// snapshot through plan KV) means each review reads current ground truth,
// which matters during multi-round revisions of greenfield plans where
// scope.create files become real between rounds. Returns "" when the
// sandbox is unavailable; the renderer omits the section silently and the
// path-check criterion weakens to "verify scope.create coverage only".
func (c *Component) fetchProjectFileTree(ctx context.Context) string {
	if c.sandboxClient == nil {
		return ""
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result, err := c.sandboxClient.Exec(fetchCtx, "main",
		"git ls-files | grep -v '^\\.semspec/' | grep -v '^\\.git/' | head -50", 5000)
	if err != nil {
		c.logger.Debug("fetchProjectFileTree: sandbox exec failed, skipping injection",
			"error", err)
		return ""
	}
	if result == nil || result.ExitCode != 0 {
		return ""
	}
	return strings.TrimSpace(result.Stdout)
}

// extractPriorReviewContext pulls ReviewFormattedFindings + ReviewIteration
// from a marshaled workflow.Plan so the reviewer can be made aware of its
// own prior verdict on revision rounds. Returns ("", 0) on the first review
// pass (no prior round) or when the plan content can't be parsed
// (defensive: never block the dispatch on this — degrade to stateless).
func extractPriorReviewContext(planContent string) (findings string, iteration int) {
	if planContent == "" {
		return "", 0
	}
	var plan workflow.Plan
	if err := json.Unmarshal([]byte(planContent), &plan); err != nil {
		return "", 0
	}
	return plan.ReviewFormattedFindings, plan.ReviewIteration
}

// availableToolNames returns the full list of tool names for prompt assembly.
func (c *Component) availableToolNames() []string {
	return []string{
		"bash", "submit_work", "ask_question",
		"write_todos", "scratchpad",
		"graph_search", "graph_query", "graph_summary",
		"web_search", "http_request",
		"decompose_task",
	}
}

// retryOrFail attempts to re-dispatch the review with the error appended to the
// prompt as context. When MaxReviewRetries is exhausted it falls through to
// sendGenerationFailed which rejects the plan.
//
// On cold start (no in-memory entry), reload plan from PLAN_STATES and Track
// before Tick — the dispatchretry helper is unaware of NATS/KV by design.
func (c *Component) retryOrFail(ctx context.Context, slug string, round reviewRound, errorMsg string) {
	if _, ok := c.retry.Snapshot(slug); !ok {
		planContent, err := c.loadPlanContentFromKV(ctx, slug)
		if err != nil {
			c.logger.Warn("retryOrFail: no retry state and PLAN_STATES recovery failed, failing immediately",
				"slug", slug, "error", err)
			c.reviewsFailed.Add(1)
			c.sendGenerationFailed(ctx, slug, round, errorMsg)
			return
		}
		c.logger.Info("Recovered plan from PLAN_STATES after restart", "slug", slug)
		c.retry.Track(slug, &reviewRetryPayload{planContent: planContent, round: round})
	}

	entry, retryOK := c.retry.Tick(ctx, slug)
	if entry == nil {
		if ctx.Err() != nil {
			c.logger.Debug("retryOrFail aborted during backoff", "slug", slug, "error", ctx.Err())
			return
		}
		c.logger.Warn("retryOrFail: lost retry context, failing immediately", "slug", slug)
		c.reviewsFailed.Add(1)
		c.sendGenerationFailed(ctx, slug, round, errorMsg)
		return
	}

	payload, _ := entry.Payload.(*reviewRetryPayload)

	if !retryOK {
		c.logger.Warn("Review exhausted retries",
			"slug", slug,
			"round", round,
			"attempts", entry.Count,
			"max", c.config.MaxReviewRetries,
			"last_error", errorMsg)
		c.reviewsFailed.Add(1)
		c.sendGenerationFailed(ctx, slug, round, errorMsg)
		return
	}

	c.logger.Info("Retrying review",
		"slug", slug,
		"round", round,
		"attempt", entry.Count,
		"max", c.config.MaxReviewRetries,
		"previous_error", errorMsg)

	c.dispatchReviewer(ctx, slug, payload.planContent, payload.round, errorMsg)
}

// loadPlanForRules reads a plan from PLAN_STATES and unmarshals it for the
// ADR-040 capability rules. Returns the parsed Plan or an error if KV/parse
// fails. Caller falls back to "skip rules, log warning" semantics so a
// transient KV blip doesn't break the review flow.
func (c *Component) loadPlanForRules(ctx context.Context, slug string) (*workflow.Plan, error) {
	planJSON, err := c.loadPlanContentFromKV(ctx, slug)
	if err != nil {
		return nil, err
	}
	var plan workflow.Plan
	if err := json.Unmarshal([]byte(planJSON), &plan); err != nil {
		return nil, fmt.Errorf("unmarshal plan: %w", err)
	}
	return &plan, nil
}

// loadPlanContentFromKV reads a plan from PLAN_STATES and returns its JSON
// representation for re-dispatch. Used for crash recovery when retryState is lost.
func (c *Component) loadPlanContentFromKV(ctx context.Context, slug string) (string, error) {
	js, err := c.natsClient.JetStream()
	if err != nil {
		return "", fmt.Errorf("no JetStream: %w", err)
	}
	bucket, err := js.KeyValue(ctx, c.config.PlanStateBucket)
	if err != nil {
		return "", fmt.Errorf("PLAN_STATES bucket: %w", err)
	}
	entry, err := bucket.Get(ctx, slug)
	if err != nil {
		return "", fmt.Errorf("get plan %q: %w", slug, err)
	}
	return string(entry.Value()), nil
}

// emitParseIncident writes ADR-035 CP-1 telemetry when plan-review parse
// fails. Mirrors the qa-reviewer + requirement-executor shapes.
// Best-effort: nil tripleWriter is a no-op (test path).
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
		nil, // no quirks tracked for plan-review parser yet
		rawResponse,
		parseErr,
	); err != nil {
		c.logger.Warn("CP-1 incident emit failed",
			"loop_id", loopID, "role", role, "error", err)
	}
}

// truncatePreview collapses newlines and bounds length so a model
// response can be embedded in a single structured-log field without
// blowing the log shipper. Empty input returns empty.
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

// parseReviewFromResult extracts a PlanReviewResult from the agent's submit_work deliverable.
func parseReviewFromResult(result string) (*workflow.PlanReviewResult, error) {
	if result == "" {
		return nil, fmt.Errorf("empty result")
	}

	var review workflow.PlanReviewResult

	// Try direct JSON parse first.
	if err := json.Unmarshal([]byte(result), &review); err == nil {
		if review.Verdict == "approved" || review.Verdict == "needs_changes" {
			review.NormalizeVerdict()
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

	review.NormalizeVerdict()

	return &review, nil
}

// PlanReviewResult is the result payload for plan review.
type PlanReviewResult struct {
	RequestID string                       `json:"request_id"`
	Slug      string                       `json:"slug"`
	Verdict   string                       `json:"verdict"`
	Summary   string                       `json:"summary"`
	Findings  []workflow.PlanReviewFinding `json:"findings"`
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

// extractPlanLessons creates lessons from error-severity findings in a plan review rejection.
// Each finding is tagged with the role responsible for the phase that produced it.
//
// ADR-033 Phase 3: every emitted lesson now carries an EvidenceFiles citation
// to the plan artifact (`.semspec/plans/<slug>/plan.json`), satisfying the
// writer's evidence requirement. Findings already have structured per-phase
// information so we self-populate evidence rather than routing through the
// decomposer LLM (which would lose the structured findings shape).
func (c *Component) extractPlanLessons(ctx context.Context, slug string, result *workflow.PlanReviewResult) {
	if c.lessonWriter == nil {
		return
	}

	planFileRef := workflow.FileRef{
		Path: ".semspec/plans/" + slug + "/plan.json",
	}

	for _, finding := range result.ErrorFindings() {
		if finding.Issue == "" {
			continue
		}

		role := phaseToRole(finding.Phase)
		lesson := workflow.Lesson{
			Source:        "plan-review",
			ScenarioID:    slug,
			Summary:       finding.Issue,
			Role:          role,
			EvidenceFiles: []workflow.FileRef{planFileRef},
		}

		if c.errorCategories != nil {
			for _, m := range c.errorCategories.MatchSignals(finding.Issue) {
				lesson.CategoryIDs = append(lesson.CategoryIDs, m.Category.ID)
			}
		}

		if err := c.lessonWriter.RecordLesson(ctx, lesson); err != nil {
			c.logger.Warn("Failed to record plan lesson", "slug", slug, "role", role, "error", err)
		}

		if len(lesson.CategoryIDs) > 0 {
			if err := c.lessonWriter.IncrementRoleLessonCounts(ctx, role, lesson.CategoryIDs); err != nil {
				c.logger.Warn("Failed to increment plan lesson counts", "role", role, "error", err)
			}
		}
	}
}

// phaseToRole maps a plan review finding's phase to the pipeline role responsible.
func phaseToRole(phase string) string {
	switch phase {
	case "plan":
		return "planner"
	case "requirements":
		return "requirement-generator"
	case "architecture":
		return "architect"
	case "scenarios":
		return "scenario-generator"
	default:
		slog.Warn("Unknown plan review phase, defaulting to planner", "phase", phase)
		return "planner"
	}
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
	modelName := c.modelRegistry.Resolve(capability)
	if endpoint := c.modelRegistry.GetEndpoint(modelName); endpoint != nil {
		return prompt.Provider(endpoint.Provider)
	}
	return prompt.ProviderOllama
}
