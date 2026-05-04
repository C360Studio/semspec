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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/c360studio/semspec/graph"
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
	"github.com/c360studio/semspec/workflow/jsonutil"
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
	// stepDrafting is the workflow step for plan drafting (coordinator + focused planners).
	stepDrafting = "drafting"

	// subjectPlanningTask is the NATS subject for planning agent tasks.
	subjectPlanningTask = "agent.task.planning"
)

// planDispatchContext holds the dispatch parameters needed to retry a planner
// loop. Stored in pendingDispatch on dispatch, read on retry so revision
// context (existing plan, reviewer findings) is preserved across retries.
type planDispatchContext struct {
	Title            string
	IsRevision       bool
	PreviousPlanJSON string
	RevisionPrompt   string
}

// Component implements the planner processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	modelRegistry ssmodel.RegistryReader
	toolRegistry  component.ToolRegistryReader
	assembler     *prompt.Assembler
	lessonWriter  *lessons.Writer
	tripleWriter  *graphutil.TripleWriter // for ADR-035 CP-1 incident emit

	// sandboxClient fetches a project file tree snapshot at dispatch time so
	// the planner user prompt can ground the model in actual workspace
	// layout. Nil when SandboxURL config is empty (greenfield / dev runs).
	sandboxClient *sandbox.Client

	// retry tracks per-plan attempts keyed by slug; payload is *planDispatchContext.
	retry *dispatchretry.State

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
	if config.DefaultCapability == "" {
		config.DefaultCapability = defaults.DefaultCapability
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
		ComponentName: "planner",
	}

	var sandboxClient *sandbox.Client
	if config.SandboxURL != "" {
		sandboxClient = sandbox.NewClient(config.SandboxURL)
	}

	return &Component{
		name:          "planner",
		config:        config,
		natsClient:    deps.NATSClient,
		logger:        logger,
		modelRegistry: deps.ModelRegistry,
		toolRegistry:  deps.ToolRegistry,
		assembler:     assembler,
		lessonWriter:  &lessons.Writer{TW: tw, Logger: logger},
		tripleWriter:  tw,
		sandboxClient: sandboxClient,
		retry: dispatchretry.New(dispatchretry.Config{
			MaxRetries: config.MaxGenerationRetries,
			BackoffMs:  config.RetryBackoffMs,
		}),
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized planner", "capability", c.config.DefaultCapability)
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

	// KV watcher — self-triggers on new plan creation (status == "created", revision == 1).
	go c.watchPlanStates(subCtx)

	// Loop completion watcher — picks up agentic-dispatch results from AGENT_LOOPS KV.
	go c.watchLoopCompletions(subCtx)

	c.logger.Info("planner started", "capability", c.config.DefaultCapability)

	return nil
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
				go c.dispatchPlanner(ctx, plan.Slug, plan.Title, true, string(planJSON), revisionPrompt, "")
			} else {
				go c.dispatchPlanner(ctx, plan.Slug, plan.Title, false, "", "", "")
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

	replayDone := false

	for entry := range watcher.Updates() {
		if entry == nil {
			// Nil sentinel marks end of initial KV replay.
			replayDone = true
			c.logger.Info("AGENT_LOOPS replay complete for planner")
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
		if loop.WorkflowStep != stepDrafting {
			continue
		}

		slug, _ := loop.Metadata["plan_slug"].(string)
		if slug == "" {
			continue
		}

		// During replay, skip — these loops already produced mutations
		// before the restart.
		if !replayDone {
			c.logger.Debug("Replay: skipping completed planning loop",
				"slug", slug, "loop_id", loop.ID)
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

	// Stale loop guard: drop completions from older dispatches that race a retry.
	if c.retry.IsStaleLoop(slug, loop.TaskID) {
		c.logger.Debug("Dropping stale planning loop completion (task ID mismatch)",
			"slug", slug, "loop_task_id", loop.TaskID, "loop_id", loop.ID)
		return
	}

	if loop.Outcome != agentic.OutcomeSuccess {
		errMsg := loop.Error
		if errMsg == "" {
			errMsg = fmt.Sprintf("agent loop ended with outcome %q", loop.Outcome)
		}
		c.retryOrFail(ctx, slug, errMsg)
		return
	}

	// Check if the plan has moved past drafting while we were working.
	// If so, our result is stale — discard it without rejecting the plan.
	if kvPlan, loadErr := c.loadPlanFromKV(ctx, slug); loadErr == nil {
		status := kvPlan.EffectiveStatus()
		if status != workflow.StatusDrafting {
			c.logger.Warn("Plan advanced past drafting while planner was working, discarding stale result",
				"slug", slug,
				"current_status", status,
				"loop_id", loop.ID)
			c.retry.Clear(slug)
			return
		}
	}
	// If KV read fails, proceed with the mutation — plan-manager will validate.

	planContent, quirks, err := parsePlanFromResult(loop.Result)

	// CP-1 incident emit (ADR-035 §3, Phase 2): surface per-call quirk
	// fires and parse rejections to the SKG so operators can query
	// incident rates by (role, model, prompt_version). Best-effort —
	// graph-write failures are logged but do NOT fail the planner flow,
	// because telemetry is observability, not gating.
	c.emitParseIncident(ctx, loop, quirks, err)

	if err != nil {
		c.retryOrFail(ctx, slug, fmt.Sprintf("planner output parse failed: %v", err))
		return
	}

	scope := &workflow.Scope{
		Include:    planContent.Scope.Include,
		Exclude:    planContent.Scope.Exclude,
		DoNotTouch: planContent.Scope.DoNotTouch,
		Create:     planContent.Scope.Create,
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
		c.logger.Error("Failed to marshal drafted mutation, rejecting plan", "slug", slug, "error", err)
		c.sendGenerationFailed(ctx, slug, fmt.Sprintf("drafted mutation marshal failed: %v", err))
		return
	}

	resp, err := c.natsClient.RequestWithRetry(ctx, mutationDraftedSubject, data, 10*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		c.logger.Error("Drafted mutation request failed, rejecting plan", "slug", slug, "error", err)
		c.sendGenerationFailed(ctx, slug, fmt.Sprintf("drafted mutation publish failed: %v", err))
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

		// If the plan advanced past drafting, this is NOT a generation failure —
		// our result is simply stale. Log a warning and clean up without rejecting.
		if strings.Contains(errMsg, "invalid transition") {
			c.logger.Warn("Drafted mutation rejected due to plan state advancement, discarding stale result",
				"slug", slug,
				"error", errMsg,
				"loop_id", loop.ID)
			c.retry.Clear(slug)
			return
		}

		c.logger.Error("Plan-manager rejected drafted mutation, rejecting plan", "slug", slug, "error", errMsg)
		c.sendGenerationFailed(ctx, slug, fmt.Sprintf("drafted mutation rejected: %s", errMsg))
		return
	}

	// Success — clear retry state.
	c.retry.Clear(slug)

	c.plansGenerated.Add(1)
	c.logger.Info("Plan drafted via agentic-dispatch and mutation accepted",
		"slug", slug,
		"loop_id", loop.ID)
}

// dispatchPlanner dispatches a planner agent loop via agentic-dispatch.
// The agent explores the codebase using bash and graph tools, then produces
// a Goal/Context/Scope plan. Running through the agentic loop gives it real
// tool execution, trajectory tracking, and codebase visibility.
// buildPlannerUserPrompt assembles the per-turn user prompt for the planning
// buildPlannerPromptContext maps the dispatcher's args into the typed
// prompt-package context the user-prompt fragment renders against. Replaces
// the legacy buildPlannerUserPrompt; the actual prompt body now lives in
// prompt/domain/software_render.go::renderPlannerPrompt.
func buildPlannerPromptContext(title string, isRevision bool, previousPlanJSON, revisionPrompt, previousError, projectFileTree string) *prompt.PlannerPromptContext {
	return &prompt.PlannerPromptContext{
		Title:            title,
		IsRevision:       isRevision,
		PreviousPlanJSON: previousPlanJSON,
		RevisionPrompt:   revisionPrompt,
		PreviousError:    previousError,
		ProjectFileTree:  projectFileTree,
	}
}

// fetchProjectFileTree returns a ground-truth snapshot of the project's
// tracked files. Runs `git ls-files | head -50` in the sandbox at dispatch
// time so the planner user prompt can ground the model in actual workspace
// layout. Returns "" when the sandbox is unavailable, the workspace is
// empty (greenfield), or the call errors — all valid states the renderer
// silently skips. Bounded at 50 entries so a 10K-file workspace doesn't
// blow up the prompt; the planner still has graph_summary + bash for full
// exploration.
func (c *Component) fetchProjectFileTree(ctx context.Context) string {
	if c.sandboxClient == nil {
		return ""
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// taskID="main" is the sandbox's reserved ID for the repo root workspace
	// (cmd/sandbox/server.go:63). All other IDs route to per-task worktrees
	// that don't exist at planner-dispatch time. Without "main", the fetch
	// silently 404s and the renderer skips injection — caught 2026-05-03 v2
	// regression where the prompt never gained the "## Project Files"
	// section. 5s timeout: this is supposed to be near-instant; if git
	// takes longer than that something else is wrong and we should not
	// block plan dispatch on it.
	// Filter .semspec/ and other meta-paths so the planner doesn't see its
	// own bookkeeping artifacts in the inventory. Without this filter the
	// 2026-05-03 v8 planner panicked under multi-round-rejection pressure
	// and dumped every visible path (including .semspec/checklist.json,
	// .semspec/projects/.../plan.json, etc.) into scope.include — the
	// over-inclusion failure mode the relevance-not-inventory persona
	// fix in 66c58e1 was supposed to prevent. The persona fix prevents
	// well-behaved over-inclusion; this filter prevents catastrophic
	// over-inclusion when the planner is already panicking.
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

func (c *Component) dispatchPlanner(ctx context.Context, slug, title string, isRevision bool, previousPlanJSON, revisionPrompt, previousError string) {
	// Gate on semsource readiness — agents need graph data for codebase context.
	// Block up to readiness budget (SEMSOURCE_READINESS_BUDGET), then proceed anyway.
	c.waitForGraphReady(ctx, slug)

	c.triggersProcessed.Add(1)
	c.updateLastActivity()
	// Track records the dispatch context for retry re-dispatch. No-op when
	// an entry already exists (retry re-entry preserves the running count
	// and the original revision context).
	c.retry.Track(slug, &planDispatchContext{
		Title:            title,
		IsRevision:       isRevision,
		PreviousPlanJSON: previousPlanJSON,
		RevisionPrompt:   revisionPrompt,
	})

	taskID := fmt.Sprintf("plan-%s-%s", slug, uuid.New().String())
	c.retry.SetActiveLoop(slug, taskID)

	// Resolve model for planning capability.
	capability := c.config.DefaultCapability
	if capability == "" {
		capability = string(model.CapabilityPlanning)
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
	projectFileTree := c.fetchProjectFileTree(ctx)
	asmCtx := &prompt.AssemblyContext{
		Role:           prompt.RolePlanner,
		Provider:       provider,
		Domain:         "software",
		AvailableTools: prompt.FilterTools(c.availableToolNames(), prompt.RolePlanner),
		SupportsTools:  true,
		MaxTokens:      maxTokens,
		Persona:        prompt.GlobalPersonas().ForRole(prompt.RolePlanner),
		Vocabulary:     prompt.GlobalPersonas().Vocabulary(),
		PlannerPrompt:  buildPlannerPromptContext(title, isRevision, previousPlanJSON, revisionPrompt, previousError, projectFileTree),
	}

	// Wire role-scoped lessons learned.
	if c.lessonWriter != nil {
		graphCtx := context.WithoutCancel(ctx)
		if roleLessons, err := c.lessonWriter.RotateLessonsForRole(graphCtx, "planner", 10); err == nil && len(roleLessons) > 0 {
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
		c.logger.Error("Planner user-prompt render failed", "slug", slug, "error", assembled.RenderError)
		return
	}

	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleGeneral,
		Model:        modelName,
		Prompt:       assembled.UserMessage,
		Tools:        terminal.ToolsForDeliverable(c.toolRegistry, "plan", c.availableToolNames()...),
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
		c.logger.Error("Failed to marshal task message, rejecting plan", "slug", slug, "error", err)
		c.sendGenerationFailed(ctx, slug, fmt.Sprintf("planner dispatch marshal failed: %v", err))
		return
	}

	if err := c.natsClient.PublishToStream(ctx, subjectPlanningTask, data); err != nil {
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to dispatch plan coordinator, rejecting plan", "slug", slug, "error", err)
		c.sendGenerationFailed(ctx, slug, fmt.Sprintf("planner dispatch failed: %v", err))
		return
	}

	c.logger.Info("Dispatched plan coordinator agent",
		"slug", slug,
		"task_id", taskID,
		"model", modelName,
		"fragments", len(assembled.FragmentsUsed),
		"system_chars", assembled.SystemMessageChars)
}

// waitForGraphReady blocks until the graph source registry has at least one
// ready semsource source, or the readiness budget expires. This gates plan
// dispatch so agents have graph data for codebase context. If no semsource
// is configured or the budget expires, proceeds without graph data.
func (c *Component) waitForGraphReady(ctx context.Context, slug string) {
	reg := graph.GlobalSources()
	if reg == nil || !reg.HasSemsources() {
		return
	}

	budget := reg.ReadinessBudget()
	if budget <= 0 {
		budget = 60 * time.Second // default
	}

	waitCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	// Fast path: already ready (background monitoring detected it).
	if reg.WaitForReady(waitCtx) {
		return
	}

	// Timed out — proceed anyway but warn.
	c.logger.Warn("Semsource not ready within budget, proceeding without graph data",
		"slug", slug, "budget", budget)
}

// availableToolNames returns the full list of tool names for prompt assembly.
// Actual tool availability is controlled by agentic-tools at runtime.
func (c *Component) availableToolNames() []string {
	tools := []string{
		"bash", "submit_work",
		"graph_search", "graph_query", "graph_summary",
		"web_search", "http_request",
	}
	if c.config.InteractiveMode {
		tools = append(tools, "ask_question")
	}
	return tools
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

// PlanContent holds the LLM-generated plan fields.
type PlanContent struct {
	Title   string `json:"title,omitempty"`
	Goal    string `json:"goal"`
	Context string `json:"context"`
	Scope   struct {
		Include    []string `json:"include,omitempty"`
		Exclude    []string `json:"exclude,omitempty"`
		DoNotTouch []string `json:"do_not_touch,omitempty"`
		Create     []string `json:"create,omitempty"`
	} `json:"scope"`
	Status string `json:"status,omitempty"`
}

// emitParseIncident writes ADR-035 CP-1 telemetry for the planner's
// most recent submit_work parse. Strict outcomes are no-ops; rejected
// or tolerated_quirk outcomes write a parse.incident triple set
// keyed at "<loop.ID>:parse:response_parse" so retry replays of the
// same loop are idempotent in the SKG.
//
// Best-effort: a graph-write failure is logged but does NOT fail the
// planner flow. CP-1 is observability — not gating.
//
// Phase 2 of the named-quirks list (per ADR-035 audit Phase-1 note).
// PromptVersion is left empty until the prompt-pack revision is
// surfaced at this layer; an empty value is skipped at write time so
// no sentinel triples land in the graph.
func (c *Component) emitParseIncident(ctx context.Context, loop *agentic.LoopEntity, quirks []jsonutil.QuirkID, parseErr error) {
	if c.tripleWriter == nil {
		return
	}

	ic := parseincident.IncidentContext{
		CallID: loop.ID,
		Role:   "planner",
		Model:  loop.Model,
	}
	ev := parseincident.IncidentEvent{
		Checkpoint:  observability.CheckpointResponseParse,
		RawResponse: loop.Result,
	}

	switch {
	case parseErr != nil:
		ev.Outcome = observability.OutcomeRejected
		// TODO(ADR-035 phase 3): when RETRY HINT injection is wired
		// at this site, the same string should flow into both the
		// loop feedback channel AND this Reason triple, so SKG
		// queries on parse incidents see the exact text the model
		// received. Today parseErr.Error() is the rejection reason
		// (which IS the precursor to a future retry hint), so
		// alignment is forward-compatible.
		ev.Reason = parseErr.Error()
	case len(quirks) > 0:
		ev.Outcome = observability.OutcomeToleratedQuirk
		ev.Quirks = make([]string, 0, len(quirks))
		for _, q := range quirks {
			ev.Quirks = append(ev.Quirks, string(q))
		}
	default:
		// Strict — Emit is a no-op for this case but call it anyway
		// for symmetry; clearer than a conditional skip here.
		ev.Outcome = observability.OutcomeStrict
	}

	if _, err := parseincident.Emit(ctx, c.tripleWriter, ic, ev); err != nil {
		c.logger.Warn("CP-1 incident emit failed",
			"loop_id", loop.ID,
			"outcome", ev.Outcome,
			"error", err,
		)
	}
}

// parsePlanFromResult extracts PlanContent from an agent loop result
// string. The result may be raw JSON or wrapped in markdown code fences,
// contain JS-style line comments, or have trailing commas — all handled
// by jsonutil.ParseStrict's named-quirks list per ADR-035 audit C.3.
//
// Returns (planContent, quirksFired, error). The QuirksFired slice is
// empty in the direct-unmarshal happy path and populated only when
// ParseStrict's fallback path was needed. Callers feed it into
// parseincident.Emit alongside their per-call context to surface
// per-call quirk-fire telemetry to the SKG (CP-1 phase-2 wiring per
// ADR-035 §3).
//
// The previous local extractJSON helper did only brace-walking with no
// fence/comment/comma handling and didn't track string-state during the
// walk (so a JSON value containing `}` inside a string could mis-match).
// jsonutil.ParseStrict handles all of those plus reports per-fire
// counter telemetry so quirk regressions on the planner surface stay
// observable.
func parsePlanFromResult(result string) (*PlanContent, []jsonutil.QuirkID, error) {
	if result == "" {
		return nil, nil, fmt.Errorf("empty result")
	}

	// Try direct JSON parse first. Happy path returns no quirks fired —
	// the input was clean JSON and ParseStrict was never invoked.
	var pc PlanContent
	if err := json.Unmarshal([]byte(result), &pc); err == nil && pc.Goal != "" {
		return &pc, nil, nil
	}

	// Fall through to jsonutil.ParseStrict — first caller migrated from
	// a local extract helper to the package-level API per ADR-035 audit
	// C.3. QuirksFired flows back to the caller for CP-1 incident
	// emission.
	parsed := jsonutil.ParseStrict(result)
	if parsed.JSON == "" {
		return nil, parsed.QuirksFired, fmt.Errorf("no JSON found in result")
	}

	if err := json.Unmarshal([]byte(parsed.JSON), &pc); err != nil {
		return nil, parsed.QuirksFired, fmt.Errorf("parse JSON: %w (content: %s)", err, parsed.JSON[:min(200, len(parsed.JSON))])
	}

	if pc.Goal == "" {
		return nil, parsed.QuirksFired, fmt.Errorf("plan missing 'goal' field")
	}

	return &pc, parsed.QuirksFired, nil
}

// retryOrFail increments the retry counter for this slug and re-dispatches
// with error feedback if under the limit, otherwise signals permanent failure.
//
// On cold start (no in-memory entry), reconstruct dispatch context from
// PLAN_STATES and Track before Tick — the dispatchretry helper is unaware
// of NATS/KV by design.
func (c *Component) retryOrFail(ctx context.Context, slug, errMsg string) {
	if _, ok := c.retry.Snapshot(slug); !ok {
		pdc := c.recoverDispatchContext(ctx, slug, errMsg)
		c.retry.Track(slug, pdc)
	}

	entry, retryOK := c.retry.Tick(ctx, slug)
	if entry == nil {
		if ctx.Err() != nil {
			c.logger.Debug("retryOrFail aborted during backoff", "slug", slug, "error", ctx.Err())
			return
		}
		c.logger.Warn("retryOrFail: lost retry context, failing immediately", "slug", slug)
		c.generationsFailed.Add(1)
		c.sendGenerationFailed(ctx, slug, errMsg)
		return
	}

	pdc, _ := entry.Payload.(*planDispatchContext)
	if pdc == nil {
		pdc = &planDispatchContext{}
	}

	if !retryOK {
		c.generationsFailed.Add(1)
		c.logger.Error("Plan generation failed after max retries",
			"slug", slug,
			"max_retries", c.config.MaxGenerationRetries,
			"error", errMsg)
		c.sendGenerationFailed(ctx, slug, errMsg)
		return
	}

	c.logger.Warn("Retrying plan generation",
		"slug", slug,
		"attempt", entry.Count,
		"max", c.config.MaxGenerationRetries,
		"is_revision", pdc.IsRevision,
		"reason", errMsg)
	go c.dispatchPlanner(ctx, slug, pdc.Title, pdc.IsRevision, pdc.PreviousPlanJSON, pdc.RevisionPrompt, errMsg)
}

// recoverDispatchContext reconstructs the dispatch context from PLAN_STATES.
// Used when a retry fires after a process restart wiped the in-memory state.
// Returns an empty context as a last resort so the caller can still re-dispatch.
func (c *Component) recoverDispatchContext(ctx context.Context, slug, errMsg string) *planDispatchContext {
	plan, err := c.loadPlanFromKV(ctx, slug)
	if err != nil {
		c.logger.Warn("Retry requested but no dispatch context found — dispatching as fresh plan",
			"slug", slug, "kv_error", err, "reason", errMsg)
		return &planDispatchContext{}
	}
	c.logger.Info("Recovered plan context from PLAN_STATES after restart", "slug", slug)
	pdc := &planDispatchContext{Title: plan.Title}
	pdc.IsRevision = plan.Goal != "" && len(plan.ReviewFindings) > 0
	if !pdc.IsRevision {
		return pdc
	}
	planJSON, _ := json.Marshal(plan)
	pdc.PreviousPlanJSON = string(planJSON)
	findings := plan.ReviewFormattedFindings
	if findings == "" {
		findings = plan.ReviewSummary
	}
	pdc.RevisionPrompt = fmt.Sprintf("## REVISION REQUEST (iteration %d)\n\nThe reviewer rejected your previous plan. Address ALL findings below.\n\n%s", plan.ReviewIteration, findings)
	return pdc
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

// loadPlanFromKV reads a plan from the PLAN_STATES KV bucket for restart recovery.
func (c *Component) loadPlanFromKV(ctx context.Context, slug string) (*workflow.Plan, error) {
	js, err := c.natsClient.JetStream()
	if err != nil {
		return nil, fmt.Errorf("no JetStream: %w", err)
	}
	bucket, err := js.KeyValue(ctx, "PLAN_STATES")
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

	// Retry state goes away with the component instance — no explicit clear
	// needed. cancel() above already wakes any in-flight Tick() backoff.

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
	modelName := c.modelRegistry.Resolve(capability)
	if endpoint := c.modelRegistry.GetEndpoint(modelName); endpoint != nil {
		return prompt.Provider(endpoint.Provider)
	}
	return prompt.ProviderOllama
}
