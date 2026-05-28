// Package executionmanager provides a component that orchestrates the task
// execution pipeline: developer → structural validator → code reviewer.
//
// It replaces the reactive task-execution-loop (18 rules) with a single component
// that manages the 3-stage TDD pipeline using entity triples for state and JSON
// rules for terminal transitions.
//
// Pipeline stages:
//  1. Developer — writes tests FIRST, then implements code to make them pass (full TDD cycle)
//  2. Structural Validator — deterministic checklist validation of modified files
//  3. Code Reviewer — LLM-driven code review with verdict + feedback
//
// On reviewer rejection with remaining budget, the developer is retried with the
// reviewer feedback. On budget exhaustion or "restructure" rejection, the
// execution escalates to the requirement level.
//
// Terminal status transitions (completed, escalated, failed) are owned by the
// JSON rule processor, not this component. This component writes workflow.phase;
// rules react and set workflow.status + publish events.
package executionmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sscache "github.com/c360studio/semstreams/pkg/cache"

	"github.com/c360studio/semspec/internal/trajectory"
	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/prompt"
	promptdomain "github.com/c360studio/semspec/prompt/domain"
	"github.com/c360studio/semspec/tools/sandbox"
	"github.com/c360studio/semspec/tools/terminal"
	workflowtools "github.com/c360studio/semspec/tools/workflow"
	"github.com/c360studio/semspec/vocabulary/observability"
	wf "github.com/c360studio/semspec/vocabulary/workflow"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semspec/workflow/harnesscatalog"
	"github.com/c360studio/semspec/workflow/jsonutil"
	"github.com/c360studio/semspec/workflow/lessons"
	"github.com/c360studio/semspec/workflow/parseincident"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semspec/workflow/phases"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	ssmodel "github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	componentName    = "execution-manager"
	componentVersion = "0.1.0"

	// WorkflowSlugTaskExecution identifies this workflow in agent TaskMessages.
	WorkflowSlugTaskExecution = "semspec-task-execution"

	// Pipeline stage constants used as WorkflowStep in TaskMessages.
	// TDD pipeline: develop → review.
	stageDevelop = "develop" // developer writes tests then implements code (full TDD cycle)
	stageReview  = "review"  // LLM code review with verdict + feedback

	// Phase values written to entity triples.
	phaseDeveloping       = "developing"
	phaseValidating       = "validating"
	phaseReviewing        = "reviewing"
	phaseApproved         = "approved"
	phaseEscalated        = "escalated"
	phaseError            = "error"
	phaseValidationFailed = "validation_failed"
	phaseRejected         = "rejected"

	// Rejection type that escalates to requirement level — approach is wrong.
	rejectionTypeRestructure = "restructure"
)

// worktreeManager defines the sandbox operations used by the orchestrator.
// Satisfied by *sandbox.Client; narrow interface enables mock injection in tests.
type worktreeManager interface {
	CreateWorktree(ctx context.Context, taskID string, opts ...sandbox.WorktreeOption) (*sandbox.WorktreeInfo, error)
	DeleteWorktree(ctx context.Context, taskID string) error
	MergeWorktree(ctx context.Context, taskID string, opts ...sandbox.MergeOption) (*sandbox.MergeResult, error)
	ListWorktreeFiles(ctx context.Context, taskID string) ([]sandbox.FileEntry, error)
	GitStatus(ctx context.Context, taskID string) (string, error)
}

// newWorktreeManager returns a worktreeManager backed by the sandbox client,
// or nil if url is empty. Using a constructor avoids the Go nil-interface gotcha
// where a typed nil (*sandbox.Client)(nil) assigned to an interface appears non-nil.
func newWorktreeManager(url string) worktreeManager {
	if url == "" {
		return nil
	}
	return sandbox.NewClient(url)
}

// Component orchestrates the task execution pipeline.
type Component struct {
	config       Config
	natsClient   *natsclient.Client
	logger       *slog.Logger
	decoder      *message.Decoder
	platform     component.PlatformMeta
	toolRegistry component.ToolRegistryReader
	tripleWriter *graphutil.TripleWriter
	sandbox      worktreeManager        // required — Start() fails if not configured
	indexingGate *workflow.IndexingGate // nil when graph-gateway not configured
	assembler    *prompt.Assembler      // composes system prompts for each pipeline stage

	// Lesson system — writes/reads through graph pipeline (no direct KV dependency).
	lessonWriter    *lessons.Writer
	errorCategories *workflow.ErrorCategoryRegistry
	modelRegistry   ssmodel.RegistryReader

	inputPorts  []component.Port
	outputPorts []component.Port

	// store is the 3-layer execution store (cache + KV + triples).
	store *executionStore

	// planBucket is a best-effort read-only handle to PLAN_STATES so the
	// TaskContext builder can surface plan-level fields (currently just the
	// architect's TestSurface) to the developer prompt. nil when the bucket
	// is unavailable at startup — callers treat that as "no test_surface".
	planBucket jetstream.KeyValue

	// activeExecs is a typed TTL cache mapping entityID → *taskExecution.
	// Holds runtime pipeline state (mutexes, timers) for in-flight executions.
	// Entries are explicitly deleted on completion; TTL is a safety net for leaks.
	activeExecs   sscache.Cache[*taskExecution]
	activeExecsMu sync.Mutex // guards get-or-set for duplicate trigger detection

	// taskRouting is a typed TTL cache mapping agent TaskID → entityID.
	// Provides O(1) completion routing from agent loop events to executions.
	taskRouting sscache.Cache[string]

	// checklist holds the project-specific quality gate checks from .semspec/checklist.json.
	// Loaded once at startup; injected into TaskContext so developer prompts show
	// the actual checks that structural-validator will run.
	checklist []workflow.Check

	// standards holds the full project standards loaded from .semspec/standards.json.
	// Role filtering happens at assembly time via ForRole().
	standards *workflow.Standards

	// replayLoops caches terminal LoopEntity entries by TaskID during AGENT_LOOPS
	// replay so that resumeStuckExecutions can populate Outcome/Result on the
	// reconstructed LoopCompletedEvent. Cleared after resume completes.
	replayLoops map[string]agentic.LoopEntity

	// Lifecycle
	// shutdownCancel is cancelled in Stop() to unblock awaitIndexing goroutines.
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc
	wg             sync.WaitGroup
	running        bool
	mu             sync.RWMutex
	lifecycleMu    sync.Mutex

	// Metrics
	triggersProcessed   atomic.Int64
	executionsCompleted atomic.Int64
	executionsEscalated atomic.Int64
	executionsApproved  atomic.Int64
	errors              atomic.Int64
	lastActivityMu      sync.RWMutex
	lastActivity        time.Time
}

// NewComponent creates a new execution-orchestrator from raw JSON config.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var cfg Config
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal execution-orchestrator config: %w", err)
	}
	cfg = cfg.withDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("component", componentName)

	c := &Component{
		config:        cfg,
		natsClient:    deps.NATSClient,
		logger:        logger,
		decoder:       message.NewDecoder(deps.PayloadRegistry),
		platform:      deps.Platform,
		toolRegistry:  deps.ToolRegistry,
		modelRegistry: deps.ModelRegistry,
		sandbox:       newWorktreeManager(cfg.SandboxURL),
		indexingGate:  workflow.NewIndexingGate(cfg.GraphGatewayURL, logger),
		tripleWriter: &graphutil.TripleWriter{
			NATSClient:    deps.NATSClient,
			Logger:        logger,
			ComponentName: componentName,
		},
	}

	// Initialize prompt assembler with all software domain fragments.
	registry := prompt.NewRegistry()
	registry.RegisterAll(promptdomain.Software()...)
	registry.Register(prompt.ToolGuidanceFragment(prompt.DefaultToolGuidance()))
	registry.Register(prompt.GraphManifestFragment(workflowtools.RegistrySummaryFetchFn()))
	c.assembler = prompt.NewAssembler(registry)

	for _, p := range cfg.Ports.Inputs {
		c.inputPorts = append(c.inputPorts, component.BuildPortFromDefinition(
			component.PortDefinition{Name: p.Name, Subject: p.Subject, Type: p.Type, StreamName: p.StreamName},
			component.DirectionInput,
		))
	}
	for _, p := range cfg.Ports.Outputs {
		c.outputPorts = append(c.outputPorts, component.BuildPortFromDefinition(
			component.PortDefinition{Name: p.Name, Subject: p.Subject, Type: p.Type, StreamName: p.StreamName},
			component.DirectionOutput,
		))
	}

	return c, nil
}

// Initialize prepares the component. No-op.
func (c *Component) Initialize() error { return nil }

// Start begins consuming trigger events and loop-completion events.
func (c *Component) Start(ctx context.Context) error {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()

	c.mu.RLock()
	if c.running {
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()

	if c.sandbox == nil {
		return fmt.Errorf("sandbox is required: SandboxURL must be configured for execution-manager")
	}

	c.initLessonsAndConfig()
	c.logger.Info("Starting execution-orchestrator")

	// Initialize typed caches for in-flight execution routing.
	// TTL is a safety net for leaked entries; normal cleanup is explicit via Delete.
	if ae, err := sscache.NewTTL[*taskExecution](ctx, 4*time.Hour, 30*time.Minute); err == nil {
		c.activeExecs = ae
	} else {
		return fmt.Errorf("create active executions cache: %w", err)
	}
	if tr, err := sscache.NewTTL[string](ctx, 4*time.Hour, 30*time.Minute); err == nil {
		c.taskRouting = tr
	} else {
		return fmt.Errorf("create task routing cache: %w", err)
	}

	// Initialize EXECUTION_STATES bucket and store.
	c.initExecutionStore(ctx)

	// Reconcile: recover in-flight executions from graph state.
	// Also populates the execution store from KV or graph.
	c.reconcileFromGraph(ctx)
	if c.store != nil {
		c.store.reconcile(ctx)
	}

	// Start mutation request/reply handlers (execution.mutation.*).
	if err := c.startExecMutationHandler(ctx); err != nil {
		c.logger.Warn("Failed to start execution mutation handlers", "error", err)
	}

	// shutdownCtx is used by awaitIndexing goroutines to detect component shutdown.
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	c.shutdownCtx = shutdownCtx
	c.shutdownCancel = shutdownCancel

	// KV watchers:
	// - AGENT_LOOPS: TDD pipeline loop completions (from agentic-dispatch)
	// - EXECUTION_STATES task.>: pending task executions (KV self-trigger)
	// - EXECUTION_STATES req.>: requirement termination → cancel orphan children
	c.wg.Add(3)
	go func() {
		defer c.wg.Done()
		c.watchLoopCompletions(ctx)
	}()
	go func() {
		defer c.wg.Done()
		c.watchTaskPending(ctx)
	}()
	go func() {
		defer c.wg.Done()
		c.watchRequirementTermination(ctx)
	}()

	c.mu.Lock()
	c.running = true
	c.mu.Unlock()

	return nil
}

// Stop performs graceful shutdown.
func (c *Component) Stop(timeout time.Duration) error {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()

	c.mu.RLock()
	if !c.running {
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()

	c.logger.Info("Stopping execution-orchestrator",
		"triggers_processed", c.triggersProcessed.Load(),
		"executions_approved", c.executionsApproved.Load(),
		"executions_escalated", c.executionsEscalated.Load(),
	)

	if c.shutdownCancel != nil {
		c.shutdownCancel()
	}

	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	select {
	case <-done:
		c.logger.Debug("All in-flight handlers drained")
	case <-time.After(timeout):
		c.logger.Warn("Timed out waiting for in-flight handlers to drain")
	}

	for _, key := range c.activeExecs.Keys() {
		if exec, ok := c.activeExecs.Get(key); ok {
			exec.mu.Lock()
			if exec.timeoutTimer != nil {
				exec.timeoutTimer.stop()
			}
			// Discard worktrees for any active executions on shutdown.
			c.discardWorktree(exec)
			exec.mu.Unlock()
		}
	}

	c.mu.Lock()
	c.running = false
	c.mu.Unlock()

	return nil
}

// ---------------------------------------------------------------------------
// Agent roster initialization (Phase B)
// ---------------------------------------------------------------------------

// initAgentGraph connects to the ENTITY_STATES KV bucket and loads error
// categories. When the bucket is unavailable, agent selection is disabled
// and the orchestrator falls back to using the model from the trigger payload.
// initExecutionStore creates the EXECUTION_STATES KV bucket and initializes
// the 3-layer execution store. If bucket creation fails, the store operates
// in cache+graph-only mode (graceful degradation).
func (c *Component) initExecutionStore(ctx context.Context) {
	var kvStore *natsclient.KVStore

	bucket, err := c.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:  c.config.ExecutionStateBucket,
		History: 1,
	})
	if err != nil {
		c.logger.Warn("EXECUTION_STATES bucket creation failed — KV layer disabled",
			"bucket", c.config.ExecutionStateBucket, "error", err)
	} else {
		kvStore = c.natsClient.NewKVStore(bucket)
		c.logger.Info("EXECUTION_STATES bucket ready", "bucket", c.config.ExecutionStateBucket)
	}

	store, err := newExecutionStore(ctx, kvStore, c.tripleWriter, c.logger)
	if err != nil {
		c.logger.Error("Failed to create execution store", "error", err)
		return
	}
	c.store = store

	// Best-effort read handle to PLAN_STATES for plan-level lookups during
	// TaskContext population (test_surface injection). Failure is non-fatal.
	if js, err := c.natsClient.JetStream(); err == nil {
		if pb, err := js.KeyValue(ctx, "PLAN_STATES"); err == nil {
			c.planBucket = pb
		} else {
			c.logger.Warn("PLAN_STATES bucket unavailable — test_surface injection disabled",
				"error", err)
		}
	}
}

// readPlanTestSurface returns the plan's declared test_surface, or nil when
// unavailable (bucket missing, plan missing, architecture missing, surface
// unset). Callers treat nil as "no test_surface declared" and proceed.
func (c *Component) readPlanTestSurface(ctx context.Context, slug string) *workflow.TestSurface {
	if c.planBucket == nil || slug == "" {
		return nil
	}
	entry, err := c.planBucket.Get(ctx, slug)
	if err != nil {
		return nil
	}
	var plan workflow.Plan
	if err := json.Unmarshal(entry.Value(), &plan); err != nil {
		return nil
	}
	if plan.Architecture == nil {
		return nil
	}
	return plan.Architecture.TestSurface
}

func (c *Component) readPlanHarnessProfiles(ctx context.Context, slug string) []prompt.ResolvedHarnessProfileContext {
	if c.planBucket == nil || slug == "" {
		return nil
	}
	entry, err := c.planBucket.Get(ctx, slug)
	if err != nil {
		return nil
	}
	var plan workflow.Plan
	if err := json.Unmarshal(entry.Value(), &plan); err != nil {
		return nil
	}
	if plan.Architecture == nil || len(plan.Architecture.HarnessProfiles) == 0 {
		return nil
	}
	catalog, err := harnesscatalog.Load("")
	if err != nil {
		c.logger.Warn("Failed to load harness catalog for task context", "slug", slug, "error", err)
		return nil
	}
	resolved, err := catalog.ResolveSelections(plan.Architecture.HarnessProfiles)
	if err != nil {
		c.logger.Warn("Invalid harness profile selection in plan", "slug", slug, "error", err)
		return nil
	}
	return resolvedHarnessProfilesToPrompt(resolved)
}

func resolvedHarnessProfilesToPrompt(resolved []harnesscatalog.ResolvedSelection) []prompt.ResolvedHarnessProfileContext {
	out := make([]prompt.ResolvedHarnessProfileContext, 0, len(resolved))
	for _, r := range resolved {
		p := r.Profile
		out = append(out, prompt.ResolvedHarnessProfileContext{
			ProfileID:          p.ID,
			Tier:               p.Tier,
			Orchestration:      p.EffectiveOrchestration(),
			UsedBy:             append([]string(nil), r.Selection.UsedBy...),
			Purpose:            r.Selection.Purpose,
			Covers:             append([]string(nil), r.Selection.Covers...),
			Proves:             append([]string(nil), p.Proves...),
			RunnerSupport:      append([]string(nil), p.RunnerSupport...),
			Cost:               p.Cost,
			Constraints:        append([]string(nil), p.Constraints...),
			RequiredAssertions: append([]string(nil), p.RequiredAssertions...),
			EvidenceAnchors:    append([]string(nil), p.EvidenceAnchors...),
			Images:             harnessImagesToPrompt(p.Images),
			Ports:              harnessPortsToPrompt(p.Ports),
			Env:                cloneStringStringMap(p.Env),
			Readiness:          append([]string(nil), p.Readiness...),
			TestGuidance:       append([]string(nil), p.TestGuidance...),
		})
	}
	return out
}

func harnessImagesToPrompt(images []harnesscatalog.ImageRef) []prompt.HarnessImageContext {
	out := make([]prompt.HarnessImageContext, 0, len(images))
	for _, img := range images {
		out = append(out, prompt.HarnessImageContext{Name: img.Name, Purpose: img.Purpose})
	}
	return out
}

func harnessPortsToPrompt(ports []harnesscatalog.PortRef) []prompt.HarnessPortContext {
	out := make([]prompt.HarnessPortContext, 0, len(ports))
	for _, port := range ports {
		out = append(out, prompt.HarnessPortContext{
			Name:          port.Name,
			ContainerPort: port.ContainerPort,
			Protocol:      port.Protocol,
			Purpose:       port.Purpose,
		})
	}
	return out
}

func cloneStringStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// lookupRecoveryHint returns the req.RecoveryHint string for the given
// (slug, requirementID) pair, or "" when unavailable. ADR-037 stage-1
// (a3): plan-decision-handler.applyRecoveryHint writes the recovery
// agent's diagnosis onto req.RecoveryHint when accepting a
// proposed_by="recovery-agent" PlanDecision; this lookup surfaces it
// to the next developer dispatch as supplementary feedback.
//
// Returns "" silently for any failure path (bucket missing, plan
// missing, req missing, no hint set) — recovery hints are advisory,
// not load-bearing. Best-effort.
func (c *Component) lookupRecoveryHint(ctx context.Context, slug, requirementID string) string {
	if c.planBucket == nil || slug == "" || requirementID == "" {
		return ""
	}
	entry, err := c.planBucket.Get(ctx, slug)
	if err != nil {
		return ""
	}
	var plan workflow.Plan
	if err := json.Unmarshal(entry.Value(), &plan); err != nil {
		return ""
	}
	for _, req := range plan.Requirements {
		if req.ID == requirementID {
			return req.RecoveryHint
		}
	}
	return ""
}

func (c *Component) initLessonsAndConfig() {
	// Lesson writer uses TripleWriter (NATS request-reply to graph-ingest).
	// No direct KV bucket access — no startup race.
	c.lessonWriter = &lessons.Writer{TW: c.tripleWriter, Logger: c.logger}

	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		repoRoot, _ = os.Getwd()
	}
	catPath := filepath.Join(repoRoot, "configs", "error_categories.json")
	if reg, err := workflow.LoadErrorCategories(catPath); err != nil {
		c.logger.Debug("Failed to load error categories — signal matching disabled", "error", err)
	} else {
		c.errorCategories = reg
	}

	// Load project checklist so developer prompts show the actual quality gates.
	checklistPath := filepath.Join(repoRoot, ".semspec", "checklist.json")
	if data, err := os.ReadFile(checklistPath); err == nil {
		var cl workflow.Checklist
		if err := json.Unmarshal(data, &cl); err == nil && len(cl.Checks) > 0 {
			c.checklist = cl.Checks
			c.logger.Info("Loaded project checklist for prompt injection", "checks", len(cl.Checks))
		}
	}

	// Load project standards so execution prompts include role-filtered standards.
	if stds := workflow.LoadStandardsFromDisk(repoRoot); stds != nil && len(stds.Items) > 0 {
		c.standards = stds
		c.logger.Info("Loaded project standards for prompt injection", "items", len(stds.Items))
	}

	c.logger.Info("Lesson system initialized")
}

// ---------------------------------------------------------------------------
// Startup reconciliation — recover in-flight executions from graph state
// ---------------------------------------------------------------------------

// terminalPhases are phases that indicate execution is complete — no recovery needed.
var terminalPhases = map[string]bool{
	phaseApproved:  true,
	phaseEscalated: true,
	phaseError:     true,
	phaseRejected:  true,
}

// reconcileFromGraph queries ENTITY_STATES for active (non-terminal) task
// executions and rebuilds the in-memory cache. This allows the component
// to resume in-flight executions after a process restart.
func (c *Component) reconcileFromGraph(ctx context.Context) {
	reconcileCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	entities, err := c.tripleWriter.ReadEntitiesByPrefix(reconcileCtx,
		workflow.EntityPrefix()+".exec.task.run.", 200)
	if err != nil {
		c.logger.Info("No graph state to reconcile (expected on first start)",
			"error", err)
		return
	}

	recovered := 0
	for entityID, triples := range entities {
		phase := triples[wf.Phase]
		if terminalPhases[phase] {
			continue // Already complete — no recovery needed.
		}

		state := &workflow.TaskExecution{
			EntityID:       entityID,
			Slug:           triples[wf.Slug],
			TaskID:         triples[wf.TaskID],
			Title:          triples[wf.Title],
			ProjectID:      triples[wf.ProjectID],
			TraceID:        triples[wf.TraceID],
			Model:          triples[wf.Model],
			Prompt:         triples[wf.Prompt],
			AgentID:        triples[wf.AgentID],
			WorktreePath:   triples[wf.WorktreePath],
			WorktreeBranch: triples[wf.WorktreeBranch],
			Stage:          phase,
		}
		if iter, ok := triples[wf.TDDCycle]; ok {
			fmt.Sscanf(iter, "%d", &state.TDDCycle)
		}
		if maxIter, ok := triples[wf.MaxTDDCycles]; ok {
			fmt.Sscanf(maxIter, "%d", &state.MaxTDDCycles)
		}
		exec := &taskExecution{
			key:           workflow.TaskExecutionKey(state.Slug, state.TaskID),
			TaskExecution: state,
		}

		c.activeExecs.Set(entityID, exec) //nolint:errcheck // cache set is best-effort

		// Also populate the execution store for KV observability.
		c.syncToStore(reconcileCtx, exec)
		recovered++

		c.logger.Info("Recovered execution from graph",
			"entity_id", entityID,
			"slug", exec.Slug,
			"stage", phase,
			"tdd_cycle", exec.TDDCycle,
		)
	}

	if recovered > 0 {
		c.logger.Info("Reconciliation complete",
			"recovered", recovered,
			"total_entities", len(entities))
	}
}

// syncToStore writes the current execution state to the EXECUTION_STATES KV bucket.
// This provides observable state for downstream watchers and restart recovery.
// Caller must hold exec.mu (or ensure exclusive access).
func (c *Component) syncToStore(ctx context.Context, exec *taskExecution) {
	if c.store == nil || exec.TaskExecution == nil {
		return
	}
	if err := c.store.saveTask(ctx, exec.key, exec.TaskExecution); err != nil {
		c.logger.Warn("Failed to sync execution to store",
			"key", exec.key, "stage", exec.Stage, "error", err)
	}
}

// createWorktree creates a sandbox worktree for the given execution.
// Sandbox isolation is mandatory — callers must treat errors as fatal for the
// execution (mark error, do not dispatch).
func (c *Component) createWorktree(ctx context.Context, exec *taskExecution) error {
	var wtOpts []sandbox.WorktreeOption
	if exec.ScenarioBranch != "" {
		wtOpts = append(wtOpts, sandbox.WithBaseBranch(exec.ScenarioBranch))
	}
	wtInfo, err := c.sandbox.CreateWorktree(ctx, exec.TaskID, wtOpts...)
	if err != nil {
		return fmt.Errorf("create worktree for task %s: %w", exec.TaskID, err)
	}
	exec.WorktreePath = wtInfo.Path
	exec.WorktreeBranch = wtInfo.Branch
	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.WorktreePath, wtInfo.Path)
	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.WorktreeBranch, wtInfo.Branch)
	c.logger.Info("Worktree created",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"path", wtInfo.Path,
		"branch", wtInfo.Branch,
	)
	return nil
}

// ---------------------------------------------------------------------------
// Pipeline selection by task type
// ---------------------------------------------------------------------------

// initialPhaseForType returns the starting phase for a given task type.
// All task types start at phaseDeveloping — the developer handles the full
// TDD cycle (tests + implementation) regardless of task type.
func (c *Component) initialPhaseForType(_ workflow.TaskType) string {
	return phaseDeveloping
}

// dispatchFirstStage dispatches the developer as the first (and only pre-validation)
// pipeline stage. Called from initTaskExecution after exec is initialized.
func (c *Component) dispatchFirstStage(ctx context.Context, exec *taskExecution) {
	c.dispatchDeveloperLocked(ctx, exec)
}

// ---------------------------------------------------------------------------
// Loop-completion handler (via AGENT_LOOPS KV — see loop_completions.go)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Stage 1: Developer complete (full TDD cycle — tests + implementation)
// ---------------------------------------------------------------------------

// emitDeveloperParseIncident writes ADR-035 CP-1 telemetry for the
// developer's most recent submit_work parse. Strict outcomes are
// no-ops; rejected or tolerated_quirk outcomes write a parse.incident
// triple set keyed at "<event.LoopID>:parse:response_parse" so retry
// replays of the same loop are idempotent in the SKG.
//
// Best-effort: a graph-write failure is logged but does NOT fail the
// developer flow. CP-1 is observability — not gating.
//
// Phase 2 of the named-quirks list (per ADR-035 audit Phase-1 note +
// the planner first-wire pattern in commit 403a39d). PromptVersion is
// left empty until the prompt-pack revision is surfaced at this
// layer; an empty value is skipped at write time so no sentinel
// triples land in the graph.
func (c *Component) emitDeveloperParseIncident(ctx context.Context, exec *taskExecution, event *agentic.LoopCompletedEvent, quirks []jsonutil.QuirkID, parseErr error) {
	if c.tripleWriter == nil {
		return
	}
	ic := parseincident.IncidentContext{
		CallID: event.LoopID,
		Role:   "developer",
		Model:  exec.Model,
	}
	// TODO(ADR-035 phase 3): align Reason triple with the eventual
	// RETRY HINT injected into the developer's loop on retry — same
	// shape as the planner emit-parse-incident TODO.
	if _, err := parseincident.EmitForResult(
		ctx,
		c.tripleWriter,
		ic,
		observability.CheckpointResponseParse,
		jsonutil.QuirkIDsToStrings(quirks),
		event.Result,
		parseErr,
	); err != nil {
		c.logger.Warn("CP-1 incident emit failed",
			"slug", exec.Slug,
			"task_id", exec.TaskID,
			"loop_id", event.LoopID,
			"error", err,
		)
	}
}

func (c *Component) handleDeveloperCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *taskExecution) {
	c.resetTimeoutLocked(exec)
	c.taskRouting.Delete(exec.DeveloperTaskID)

	// Capture the developer's loop ID for the lesson-decomposer (ADR-033
	// Phase 2b). Set on every developer-complete — overwriting on retries
	// is correct, since only the last developer attempt is the one whose
	// trajectory the decomposer needs to analyse.
	if event.LoopID != "" {
		exec.DeveloperLoopID = event.LoopID
	}

	if event.Outcome != agentic.OutcomeSuccess {
		c.routeFixableRejection(ctx, exec, buildLoopFailureFeedback(event))
		return
	}

	var result payloads.DeveloperResult
	parsed := jsonutil.ParseStrict(event.Result)
	parseErr := json.Unmarshal([]byte(parsed.JSON), &result)

	// CP-1 incident emit (ADR-035 §3, audit B.1): surface per-call
	// quirk fires and parse rejections to the SKG so operators can
	// query (role=developer, model, prompt_version) incident rates.
	// Best-effort — graph-write failures are logged but do NOT fail
	// the developer flow (telemetry is observability, not gating).
	c.emitDeveloperParseIncident(ctx, exec, event, parsed.QuirksFired, parseErr)

	if parseErr != nil {
		c.logger.Warn("Failed to parse developer result", "slug", exec.Slug, "error", parseErr)
	} else {
		exec.FilesModified = result.FilesModified
		exec.DeveloperOutput = result.Output
		exec.DeveloperLLMRequestIDs = result.LLMRequestIDs
	}

	// Small models sometimes stop the loop without calling submit_work, or
	// submit with an empty files_modified list after asking circular questions.
	// That produced a "successful" empty developer output that burned a TDD
	// cycle on nothing. Treat it as a fixable rejection so the retry path
	// re-dispatches the developer with actionable feedback.
	if parseErr != nil || len(exec.FilesModified) == 0 {
		var feedback string
		switch {
		case parseErr != nil:
			feedback = "Your previous attempt ended without calling submit_work. You must call submit_work with a summary and a non-empty files_modified array before stopping. If you asked a question and did not get an answer, make reasonable assumptions from the plan and scenarios and continue — do not stop the loop waiting for an answer."
		default:
			feedback = "Your previous submit_work had an empty files_modified array. You must write at least one file before calling submit_work. Create the implementation and test files called for by the scenarios, then submit again with the list of files you created or modified."
		}
		c.routeFixableRejection(ctx, exec, feedback)
		return
	}

	// Pre-reviewer claim/observation gate: the developer reported FilesModified
	// but `git status` against the worktree is empty. This is the v10
	// hallucination wedge from project_dev_wedge_diagnosis_2026_05_03 — the
	// model ran only `cat main.go` and submitted confident prose claiming a
	// /health endpoint, never writing anything. Without this gate the work
	// would dispatch a validator and then a reviewer, both of which read the
	// unchanged file and reject — burning a full TDD cycle worth of LLM calls
	// for every hallucinated submit. Routing back through routeFixableRejection
	// re-prompts the developer with explicit guidance and consumes the cycle
	// budget, so persistent hallucinators still escalate. Sits BEFORE
	// mergeWorktree's existing guard at ~line 1754, which only fires inside
	// markApprovedLocked (after the reviewer approves) — that path is
	// unreachable for this failure mode because the reviewer correctly rejects.
	if c.config.requireDeveloperDiff() && c.developerWorkClean(ctx, exec) {
		// Before declaring fabrication, check whether the agent wrote
		// the claimed files to /workspace (parent fixture root) instead
		// of its assigned worktree. 2026-05-12 take 16 revealed this
		// distinct failure mode: sonnet ran `cd /workspace && cat > build.gradle`
		// — writes landed in /workspace, not the worktree, and the
		// downstream merge loop wedged because /workspace stayed dirty
		// across cycles. The gate's "worktree clean" verdict is correct
		// in both cases; the inferred root cause differs and so should
		// the feedback. See .semspec/investigation-diff-gate-2026-05-12.md.
		leaked := c.developerLeakedToWorkspace(ctx, exec)
		var feedback string
		if len(leaked) > 0 {
			feedback = fmt.Sprintf("Your `git status` in your worktree is empty, but the parent fixture root (`/workspace`) has uncommitted changes to: %s. You likely ran `cd /workspace && cat > <file>` or similar — those writes land in the parent fixture, NOT your worktree, so they do NOT count as your contribution. Your bash starts with cwd=your worktree. Use relative paths or paths inside your worktree. Do NOT `cd /workspace` to write files.", strings.Join(leaked, ", "))
			c.logger.Warn("Developer wrote to /workspace instead of worktree; routing to retry with path-confusion guidance",
				"slug", exec.Slug,
				"task_id", exec.TaskID,
				"claimed_files", exec.FilesModified,
				"leaked_to_workspace", leaked,
				"tdd_cycle", exec.TDDCycle,
			)
		} else {
			feedback = fmt.Sprintf("Your submit_work claimed to modify %d file(s) (%s) but `git status` against your worktree shows NO changes. Reading a file with `cat` is not modifying it. You must use bash to actually write the files before calling submit_work — for example: `cat > main.go << 'EOF' ... EOF`, or `tee path < input`, or `sed -i 's/old/new/' path`. Verify with `bash('git status')` BEFORE submit_work — if the output is empty, you have not written anything yet. Re-implement the work and submit again.", len(exec.FilesModified), strings.Join(exec.FilesModified, ", "))
			c.logger.Warn("Developer claim/observation mismatch — files_modified non-empty but worktree clean; routing to retry",
				"slug", exec.Slug,
				"task_id", exec.TaskID,
				"claimed_files", exec.FilesModified,
				"tdd_cycle", exec.TDDCycle,
			)
		}
		c.routeFixableRejection(ctx, exec, feedback)
		return
	}

	// Write developer output triples — one triple per modified file.
	for _, f := range exec.FilesModified {
		_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.FilesModified, f)
	}
	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseValidating); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseValidating, "error", err)
	}
	exec.Stage = phaseValidating
	c.syncToStore(ctx, exec)

	// Dispatch structural validator.
	c.dispatchValidatorLocked(ctx, exec)
}

// developerWorkClean returns true when the gate should fire: the developer
// claimed work but the worktree shows no changes. Returns false on any
// uncertainty (sandbox unavailable, query error, non-empty status output)
// so that legitimate work is never blocked by a transient sandbox blip.
func (c *Component) developerWorkClean(ctx context.Context, exec *taskExecution) bool {
	if c.sandbox == nil || exec.TaskID == "" {
		return false
	}
	output, err := c.sandbox.GitStatus(ctx, exec.TaskID)
	if err != nil {
		c.logger.Warn("git status query failed in pre-reviewer gate; allowing dispatch (defense-in-depth)",
			"slug", exec.Slug,
			"task_id", exec.TaskID,
			"error", err,
		)
		return false
	}
	return strings.TrimSpace(output) == ""
}

// developerLeakedToWorkspace returns the subset of exec.FilesModified
// that appear in the fixture root (/workspace) as modified or
// untracked. Used by the pre-reviewer gate to distinguish path
// confusion (sonnet's `cd /workspace && cat > file`) from genuine
// fabrication — both produce a clean worktree but the right feedback
// differs.
//
// Returns nil when the sandbox is unavailable, the GitStatus call
// errors, or no claimed files appear in /workspace. The caller should
// fall through to the fabrication feedback in that case.
//
// Implementation notes:
//   - Queries sandbox.GitStatus(ctx, "main") which resolves to the
//     repo root per worktreeFor() (since 2026-05-12 handleGitStatus fix).
//   - Parses `git status --porcelain` output: lines start with a
//     2-char status code (e.g. " M ", "?? ") followed by the path.
//   - Strict path match — does not handle renames (which would appear
//     as "old -> new"). Renames into /workspace are not in scope for
//     the leakage failure mode this gate catches.
func (c *Component) developerLeakedToWorkspace(ctx context.Context, exec *taskExecution) []string {
	if c.sandbox == nil || len(exec.FilesModified) == 0 {
		return nil
	}
	output, err := c.sandbox.GitStatus(ctx, "main")
	if err != nil {
		c.logger.Debug("git status main query failed in path-confusion check; falling back to fabrication feedback",
			"slug", exec.Slug,
			"task_id", exec.TaskID,
			"error", err,
		)
		return nil
	}
	if strings.TrimSpace(output) == "" {
		return nil
	}

	// Collect dirty paths in /workspace from porcelain output.
	dirty := make(map[string]struct{})
	for _, line := range strings.Split(output, "\n") {
		if len(line) < 4 {
			continue
		}
		// Porcelain format: 2-char status + space + path.
		path := strings.TrimSpace(line[3:])
		// Untracked directories appear as "src/test/java/org/" — keep
		// the prefix so we can match files claimed under that tree.
		if path != "" {
			dirty[path] = struct{}{}
		}
	}

	var leaked []string
	for _, claimed := range exec.FilesModified {
		// Exact match (modified file) OR prefix match (file under an
		// untracked directory listed by porcelain).
		if _, ok := dirty[claimed]; ok {
			leaked = append(leaked, claimed)
			continue
		}
		for dirtyPath := range dirty {
			if strings.HasSuffix(dirtyPath, "/") && strings.HasPrefix(claimed, dirtyPath) {
				leaked = append(leaked, claimed)
				break
			}
		}
	}
	return leaked
}

// ---------------------------------------------------------------------------
// Stage 4: Reviewer complete
// ---------------------------------------------------------------------------

func (c *Component) handleReviewerCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *taskExecution) {
	c.resetTimeoutLocked(exec)
	c.taskRouting.Delete(exec.ReviewerTaskID)

	if event.Outcome != agentic.OutcomeSuccess {
		c.routeFixableRejection(ctx, exec, buildLoopFailureFeedback(event))
		return
	}

	result, ok := c.parseCodeReviewResult(event.Result, exec.Slug)
	if !ok {
		// Parse failure — retry the reviewer if budget allows. Parse failures
		// are transient (model output quality) and should not consume TDD cycles.
		maxRetries := c.config.MaxReviewRetries
		if maxRetries == 0 {
			maxRetries = 3
		}
		if exec.ReviewRetryCount < maxRetries {
			exec.ReviewRetryCount++
			// Capture a short summary of the raw output so the next dispatch
			// can include it in the prompt. Without this the retry sees
			// exactly the same input as the failed attempt and reproduces
			// the malformed output (closes the blind-retry gap for the
			// code-reviewer parse-retry path).
			exec.ReviewerParseError = summarizeReviewerParseFailure(event.Result)
			c.logger.Warn("Retrying code reviewer after parse failure",
				"slug", exec.Slug,
				"task_id", exec.TaskID,
				"attempt", exec.ReviewRetryCount,
				"max", maxRetries,
			)
			c.syncToStore(ctx, exec)
			c.startExecutionTimeout(exec)
			c.dispatchReviewerLocked(ctx, exec)
			return
		}
		c.logger.Error("Code reviewer parse failure after max retries, defaulting to rejected",
			"slug", exec.Slug,
			"task_id", exec.TaskID,
			"attempts", exec.ReviewRetryCount,
		)
	}
	// Successful parse — clear any prior parse-error so non-retry flows do
	// not carry stale context.
	exec.ReviewerParseError = ""

	exec.Verdict = result.Verdict
	exec.RejectionType = result.RejectionType
	exec.Feedback = result.Feedback
	exec.ReviewerLLMRequestIDs = result.LLMRequestIDs

	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Verdict, result.Verdict)
	if result.Feedback != "" {
		_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Feedback, result.Feedback)
	}
	if result.RejectionType != "" {
		_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.RejectionType, result.RejectionType)
	}

	c.logger.Info("Code review verdict",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"verdict", result.Verdict,
		"rejection_type", result.RejectionType,
		"tdd_cycle", exec.TDDCycle,
	)

	// Tally recurring error patterns and warn if any category crosses the
	// threshold. Reads the lessons graph — sees decomposer-written lessons
	// from prior rejections without needing a synchronous lesson write here
	// (ADR-033 Phase 3 moved the lesson-write to the decomposer).
	c.checkRejectionPatterns(ctx, exec, result.Feedback, result.Verdict)

	if result.Verdict == "approved" {
		// ADR-033 Phase 6: signal the decomposer on first-try approval so
		// it can produce a positive "best practice" lesson. Gated behind
		// EnablePositiveLessons (default false) because every first-try
		// success now becomes a decomposer LLM call.
		if c.shouldDispatchPositiveLesson(exec) {
			c.publishLessonDecomposeRequest(ctx, exec, result.Verdict, result.Feedback, event.LoopID)
		}
		c.markApprovedLocked(ctx, exec)
		return
	}

	// ADR-033 Phase 2b: signal the decomposer to produce an evidence-cited
	// Lesson. Best-effort — never blocks the rejection flow.
	c.publishLessonDecomposeRequest(ctx, exec, result.Verdict, result.Feedback, event.LoopID)

	c.handleRejectionLocked(ctx, exec, result)
}

// parseCodeReviewResult unmarshals the reviewer JSON result. Returns (result, true)
// on success. On parse failure, returns a default rejected result and false so the
// caller can decide whether to retry. Strips markdown code fences that some LLMs
// wrap around JSON tool responses.
func (c *Component) parseCodeReviewResult(raw string, slug string) (payloads.TaskCodeReviewResult, bool) {
	var result payloads.TaskCodeReviewResult
	cleaned := jsonutil.ExtractJSON(raw)
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		c.logger.Warn("Failed to parse code review result",
			"slug", slug, "error", err)
		result.Verdict = "rejected"
		result.RejectionType = "fixable"
		result.Feedback = "parse failure — could not read reviewer response"
		return result, false
	}
	// Validate verdict even when JSON parsed OK — small models can return
	// empty or unrecognized verdict strings.
	if err := phases.ValidateVerdict(result.Verdict); err != nil {
		c.logger.Warn("Code review result has invalid verdict",
			"slug", slug, "verdict", result.Verdict, "error", err)
		result.Verdict = "rejected"
		result.RejectionType = "fixable"
		result.Feedback = fmt.Sprintf("invalid verdict from reviewer: %s", err)
		return result, false
	}
	return result, true
}

// handleRejectionLocked processes a rejected code review: writes the phase
// triple, classifies feedback, and routes the retry or escalation.
func (c *Component) handleRejectionLocked(ctx context.Context, exec *taskExecution, result payloads.TaskCodeReviewResult) {
	exec.Stage = phaseRejected
	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseRejected); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseRejected, "error", err)
	}

	// Classify feedback into error categories for guidance enrichment.
	matchedCategories := c.classifyFeedback(result.Feedback)

	// (b1) Capture this cycle's snapshot before routing the retry. Next-
	// cycle dispatchDeveloperLocked reads exec.PriorCycles to compose
	// the "PRIOR ATTEMPTS" prompt block — pattern recognition over the
	// cycle sequence (gemini-pro recovery agent's analytical value made
	// deterministic and inline). Append even on restructure rejections;
	// the escalation path doesn't render history but the snapshot is
	// useful for downstream lesson-decomposer + recovery diagnoses.
	appendCycleSnapshot(exec, result)

	switch result.RejectionType {
	case rejectionTypeRestructure:
		c.markEscalatedLocked(ctx, exec, fmt.Sprintf("restructure rejection: %s", result.RejectionType))
	default:
		enrichedFeedback := c.enrichFeedbackWithGuidance(result.Feedback, matchedCategories)
		c.routeFixableRejection(ctx, exec, enrichedFeedback)
	}
}

// appendCycleSnapshot adds the current cycle's outcome to exec.PriorCycles,
// capping the slice at priorCyclesCap and clipping per-cycle feedback at
// snapshotFeedbackCap. Caller must hold exec.mu.
func appendCycleSnapshot(exec *taskExecution, result payloads.TaskCodeReviewResult) {
	feedback := result.Feedback
	if len(feedback) > snapshotFeedbackCap {
		feedback = feedback[:snapshotFeedbackCap] + "…"
	}
	snap := cycleSnapshot{
		Cycle:         exec.TDDCycle,
		LoopID:        exec.DeveloperLoopID,
		Verdict:       result.Verdict,
		RejectionType: result.RejectionType,
		Feedback:      feedback,
		FilesModified: append([]string(nil), exec.FilesModified...),
	}
	exec.PriorCycles = append(exec.PriorCycles, snap)
	if len(exec.PriorCycles) > priorCyclesCap {
		// Drop oldest, keep the most-recent priorCyclesCap entries.
		exec.PriorCycles = exec.PriorCycles[len(exec.PriorCycles)-priorCyclesCap:]
	}
}

// classifyFeedback runs signal matching against error categories and returns
// matched category IDs. Does not write to the graph — lesson recording happens
// in extractLessons which runs earlier in the review completion flow.
func (c *Component) classifyFeedback(feedback string) []string {
	if c.errorCategories == nil || feedback == "" {
		return nil
	}
	matches := c.errorCategories.MatchSignals(feedback)
	categoryIDs := make([]string, 0, len(matches))
	for _, m := range matches {
		categoryIDs = append(categoryIDs, m.Category.ID)
	}
	return categoryIDs
}

// routeFixableRejection handles a fixable rejection: retries the developer or
// escalates on budget exhaustion. The developer handles the full TDD cycle
// (tests + implementation) so all fixable rejections route back to developer.
func (c *Component) routeFixableRejection(ctx context.Context, exec *taskExecution, feedback string) {
	if exec.TDDCycle+1 < exec.MaxTDDCycles {
		c.startDeveloperRetryLocked(ctx, exec, feedback)
	} else {
		c.markEscalatedLocked(ctx, exec, "fixable rejections exceeded TDD cycle budget")
	}
}

// enrichFeedbackWithGuidance appends a REMEDIATION GUIDANCE section to feedback
// when matched error category IDs are provided. Returns original feedback when
// no categories are matched or when the registry is unavailable.
func (c *Component) enrichFeedbackWithGuidance(feedback string, categoryIDs []string) string {
	if c.errorCategories == nil || len(categoryIDs) == 0 {
		return feedback
	}
	var sb strings.Builder
	sb.WriteString(feedback)
	sb.WriteString("\n\n--- REMEDIATION GUIDANCE ---\n")
	for _, id := range categoryIDs {
		if catDef, ok := c.errorCategories.Get(id); ok {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", catDef.Label, catDef.Guidance))
		}
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// Terminal state handlers
// ---------------------------------------------------------------------------

// markApprovedLocked transitions to the approved terminal state.
// Caller must hold exec.mu.
//
// Approval is gated on a successful worktree merge: if the merge fails
// (e.g. the worktree was deleted by a parent requirement's cleanup during
// a cross-component race), we route to markErrorLocked instead of
// persisting phaseApproved. Previously the merge error was swallowed
// silently and the task was marked approved for changes that never
// landed.
func (c *Component) markApprovedLocked(ctx context.Context, exec *taskExecution) {
	if exec.terminated {
		return
	}

	// Merge BEFORE setting terminated=true, so a merge failure can route
	// through markErrorLocked (which itself checks exec.terminated). Classify
	// the failure so downstream retry/UI can distinguish infrastructure
	// (sandbox wedged) from agent (merge conflict, test failure, etc.) —
	// the INFRASTRUCTURE: prefix is the wire-level signal Phase 5 will key off.
	if err := c.mergeWorktree(exec); err != nil {
		reason := fmt.Sprintf("merge_failed: %v", err)
		if errors.Is(err, sandbox.ErrNeedsReconciliation) {
			reason = "INFRASTRUCTURE: " + reason
		}
		c.markErrorLocked(ctx, exec, reason)
		return
	}

	exec.terminated = true

	exec.Stage = phaseApproved
	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseApproved); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseApproved, "error", err)
	}
	c.syncToStore(ctx, exec)

	c.executionsApproved.Add(1)
	c.executionsCompleted.Add(1)

	c.logger.Info("Task execution approved",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"tdd_cycle", exec.TDDCycle,
	)

	// Notify callers (e.g. scenario-executor) that the TDD pipeline completed.
	// Safe against self-receive: the completion event uses exec.TaskID (external),
	// which is not stored in our taskRouting cache (only internal pipeline task IDs are).

	// Relay to RequirementExecution KV for durable watcher delivery.

	c.publishEntity(context.Background(), NewTaskExecutionEntity(exec).WithPhase(phaseApproved))
	c.cleanupExecutionLocked(exec)
}

// markEscalatedLocked transitions to the escalated terminal state.
// Caller must hold exec.mu.
func (c *Component) markEscalatedLocked(ctx context.Context, exec *taskExecution, reason string) {
	if exec.terminated {
		return
	}
	exec.terminated = true

	// Discard worktree — work was not approved.
	c.discardWorktree(exec)

	exec.Stage = phaseEscalated
	// Mirror the reason on the in-memory struct so syncToStore + the
	// downstream synthetic completion event (req_completions.go) carry it
	// to requirement-executor. Without this, the EscalationReason triple
	// is the only carrier and the requirement-level "Skipping retry — TDD
	// budget exhausted upstream" log fires with escalation_reason="".
	// Caught 2026-05-08 take 7 OpenRouter @easy.
	exec.EscalationReason = reason
	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseEscalated); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseEscalated, "error", err)
	}
	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.EscalationReason, reason)
	c.syncToStore(ctx, exec)

	c.executionsEscalated.Add(1)
	c.executionsCompleted.Add(1)

	c.logger.Info("Task execution escalated",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"tdd_cycle", exec.TDDCycle,
		"reason", reason,
	)
	trajectory.LogSummary(ctx, c.logger, c.natsClient, exec.DeveloperLoopID, "tdd-escalated", 0)

	c.publishRecoveryRequested(ctx, &payloads.RecoveryRequested{
		RecoveryID:          uuid.New().String(),
		Layer:               payloads.RecoveryLayerPhaseLocal,
		Slug:                exec.Slug,
		RequirementID:       exec.RequirementID,
		TaskID:              exec.TaskID,
		LoopID:              exec.DeveloperLoopID,
		EscalationReason:    reason,
		LastFailureFeedback: exec.Feedback,
		TraceID:             exec.TraceID,
	})

	// Notify callers that the TDD pipeline escalated (treated as failure).

	c.publishEntity(context.Background(), NewTaskExecutionEntity(exec).WithPhase(phaseEscalated).WithErrorReason(reason))
	c.cleanupExecutionLocked(exec)
}

// publishRecoveryRequested fires an ADR-037 stage-1 phase-local recovery
// request on recovery.requested.<slug>. Best-effort: failure does not roll
// back the escalation (the task is already phaseEscalated). The recovery-
// agent component consumes these and, on submit_work, emits RecoveryComplete
// on recovery.complete.<slug> for the watcher to reconcile.
func (c *Component) publishRecoveryRequested(ctx context.Context, req *payloads.RecoveryRequested) {
	if c.natsClient == nil {
		return
	}
	if err := req.Validate(); err != nil {
		c.logger.Warn("Recovery request failed local validation; skipping publish",
			"slug", req.Slug, "task_id", req.TaskID, "error", err)
		return
	}
	baseMsg := message.NewBaseMessage(req.Schema(), req, componentName)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Warn("Failed to marshal RecoveryRequested",
			"slug", req.Slug, "task_id", req.TaskID, "error", err)
		return
	}
	subject := payloads.RecoveryRequestedSubjectPrefix + req.Slug
	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		c.logger.Warn("Failed to publish RecoveryRequested",
			"slug", req.Slug, "subject", subject, "error", err)
		return
	}
	c.logger.Info("Recovery requested (phase-local)",
		"slug", req.Slug,
		"task_id", req.TaskID,
		"recovery_id", req.RecoveryID,
		"reason", req.EscalationReason)
}

// markErrorLocked transitions to the error terminal state.
// Caller must hold exec.mu.
func (c *Component) markErrorLocked(ctx context.Context, exec *taskExecution, reason string) {
	if exec.terminated {
		return
	}
	exec.terminated = true

	// Discard worktree — execution errored.
	c.discardWorktree(exec)

	exec.Stage = phaseError
	exec.ErrorReason = reason
	exec.ErrorClass = workflow.ClassifyErrorReason(reason)
	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseError); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseError, "error", err)
	}
	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.ErrorReason, reason)
	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.ErrorClass, exec.ErrorClass)
	c.syncToStore(ctx, exec)

	c.errors.Add(1)
	c.executionsCompleted.Add(1)

	c.logger.Error("Task execution failed",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"reason", reason,
	)

	// Notify callers that the TDD pipeline errored.

	c.publishEntity(context.Background(), NewTaskExecutionEntity(exec).WithPhase(phaseError).WithErrorReason(reason))
	c.cleanupExecutionLocked(exec)
}

// resetTimeoutLocked cancels the existing timeout timer and starts a fresh one.
// Called when a loop completion arrives so each TDD stage gets a full timeout
// budget — prevents the race where the original timer fires during a retry.
// Caller must hold exec.mu.
func (c *Component) resetTimeoutLocked(exec *taskExecution) {
	if exec.timeoutTimer != nil {
		exec.timeoutTimer.stop()
	}
	c.startExecutionTimeout(exec)
}

// cleanupExecutionLocked removes execution from maps and cancels timeout.
// Caller must hold exec.mu.
func (c *Component) cleanupExecutionLocked(exec *taskExecution) {
	if exec.timeoutTimer != nil {
		exec.timeoutTimer.stop()
	}
	c.taskRouting.Delete(exec.DeveloperTaskID)
	c.taskRouting.Delete(exec.ValidatorTaskID)
	c.taskRouting.Delete(exec.ReviewerTaskID)
	c.activeExecs.Delete(exec.EntityID) //nolint:errcheck // cache delete is best-effort
}

// ---------------------------------------------------------------------------
// Retry logic
// ---------------------------------------------------------------------------

// startDeveloperRetryLocked increments iteration and re-dispatches the developer.
// Caller must hold exec.mu.
func (c *Component) startDeveloperRetryLocked(ctx context.Context, exec *taskExecution, feedback string) {
	exec.TDDCycle++
	exec.FilesModified = nil
	exec.DeveloperOutput = nil
	exec.DeveloperLLMRequestIDs = nil
	exec.ValidationPassed = false
	exec.ValidationResults = nil
	exec.Verdict = ""
	exec.RejectionType = ""
	exec.ReviewerLLMRequestIDs = nil
	exec.ReviewRetryCount = 0 // reset reviewer parse-retry budget for new TDD cycle
	// Enrich feedback with worktree file listing so the retrying developer
	// knows what files already exist from prior iterations.
	if c.sandbox != nil && exec.WorktreePath != "" {
		files, err := c.sandbox.ListWorktreeFiles(ctx, exec.TaskID)
		if err != nil {
			c.logger.Warn("Failed to list worktree files for retry prompt",
				"task_id", exec.TaskID, "error", err)
		} else if len(files) > 0 {
			var listing strings.Builder
			listing.WriteString("\n\nFiles in your working directory from previous iterations:\n")
			for _, f := range files {
				if f.IsDir {
					fmt.Fprintf(&listing, "  %s/ (directory)\n", f.Name)
				} else {
					fmt.Fprintf(&listing, "  %s (%d bytes)\n", f.Name, f.Size)
				}
			}
			feedback += listing.String()
		}
	}

	// Keep Feedback — accumulated for next developer prompt.
	exec.Feedback = feedback

	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.TDDCycle, exec.TDDCycle)
	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseDeveloping); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseDeveloping, "error", err)
	}
	exec.Stage = phaseDeveloping
	c.syncToStore(ctx, exec)

	c.logger.Info("Retrying developer",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"new_tdd_cycle", exec.TDDCycle,
	)

	c.dispatchDeveloperLocked(ctx, exec)
}

// ---------------------------------------------------------------------------
// Per-execution timeout
// ---------------------------------------------------------------------------

// startExecutionTimeout starts a timer that marks the execution as errored if
// it does not complete within the configured timeout.
//
// Caller must hold exec.mu.
func (c *Component) startExecutionTimeout(exec *taskExecution) {
	timeout := c.config.GetTimeout()

	timer := time.AfterFunc(timeout, func() {
		c.logger.Warn("Execution timed out",
			"entity_id", exec.EntityID,
			"slug", exec.Slug,
			"task_id", exec.TaskID,
			"timeout", timeout,
		)
		exec.mu.Lock()
		defer exec.mu.Unlock()
		c.markErrorLocked(context.Background(), exec, fmt.Sprintf("execution timed out after %s", timeout))
	})

	exec.timeoutTimer = &timeoutHandle{
		stop: func() { timer.Stop() },
	}
}

// ---------------------------------------------------------------------------
// Prompt assembly helpers
// ---------------------------------------------------------------------------

// resolveProvider maps a model string to a prompt.Provider for formatting.
func resolveProvider(modelStr string) prompt.Provider {
	switch {
	case strings.Contains(modelStr, "claude"):
		return prompt.ProviderAnthropic
	case strings.Contains(modelStr, "gpt"), strings.Contains(modelStr, "o1"), strings.Contains(modelStr, "o3"):
		return prompt.ProviderOpenAI
	default:
		return prompt.ProviderOllama
	}
}

// buildAssemblyContext creates a prompt.AssemblyContext for the given role and execution state.
// modelName is the model the agent will run on; it determines whether the
// dispatch attaches a ResponseFormat (and therefore whether the prompt
// assembler elides schema prose).
// The ctx parameter is used for graph reads (error trends, lessons); context.WithoutCancel
// is applied so these reads survive caller cancellation without inheriting the deadline.
func (c *Component) buildAssemblyContext(ctx context.Context, role prompt.Role, exec *taskExecution, modelName string) *prompt.AssemblyContext {
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
		Role:              role,
		Provider:          resolveProvider(modelName),
		Domain:            "software",
		AvailableTools:    prompt.FilterTools(c.availableToolNames(), role),
		SupportsTools:     true,
		MaxTokens:         maxTokens,
		Persona:           prompt.GlobalPersonas().ForRole(role),
		Vocabulary:        prompt.GlobalPersonas().Vocabulary(),
		HasResponseFormat: terminal.EndpointSupportsResponseFormat(endpoint),
	}

	// Wire role-filtered project standards.
	if c.standards != nil {
		asmCtx.Standards = prompt.NewStandardsContext(c.standards.ForRole(string(role)))
	}

	// Wire task context for execution roles.
	if role == prompt.RoleDeveloper ||
		role == prompt.RoleValidator || role == prompt.RoleReviewer {
		asmCtx.TaskContext = &prompt.TaskContext{
			PlanGoal:        exec.Title,
			IsRetry:         exec.TDDCycle > 0,
			Feedback:        exec.Feedback,
			Iteration:       exec.TDDCycle + 1, // 1-based for display
			MaxIterations:   exec.MaxTDDCycles,
			Checklist:       c.checklist,
			TestSurface:     c.readPlanTestSurface(ctx, exec.Slug),
			HarnessProfiles: c.readPlanHarnessProfiles(ctx, exec.Slug),
			WorktreePath:    exec.WorktreePath,
		}

		// Populate ErrorTrends from role-scoped lesson counts. Use threshold 0
		// so even first-time errors surface in the retry prompt. Graph reads use
		// a detached context so they survive caller cancellation. Role is the
		// caller-supplied prompt role — counts must match the prompt being
		// assembled, not be hardcoded against developer.
		if c.lessonWriter != nil && c.errorCategories != nil {
			graphCtx := context.WithoutCancel(ctx)
			if counts, err := c.lessonWriter.GetRoleLessonCounts(graphCtx, string(role)); err == nil {
				for catID, count := range counts.Counts {
					if catDef, ok := c.errorCategories.Get(string(catID)); ok {
						asmCtx.TaskContext.ErrorTrends = append(asmCtx.TaskContext.ErrorTrends, prompt.ErrorTrend{
							CategoryID: catDef.ID,
							Label:      catDef.Label,
							Guidance:   catDef.Guidance,
							Count:      count,
						})
					}
				}
			}
		}
	}

	// Wire role-scoped lessons learned. Lessons are fetched for the role this
	// prompt is being assembled for — without this, the reviewer prompt would
	// surface developer lessons (Phase 0 bug 0.1 in ADR-033).
	if c.lessonWriter != nil {
		graphCtx := context.WithoutCancel(ctx)
		lessons, err := c.lessonWriter.RotateLessonsForRole(graphCtx, string(role), 10)
		if err == nil && len(lessons) > 0 {
			tk := &prompt.LessonsLearned{}
			for _, les := range lessons {
				lesson := prompt.LessonEntry{
					Category:      les.Source,
					Summary:       les.Summary,
					InjectionForm: les.InjectionForm,
					Positive:      les.Positive,
					Role:          les.Role,
				}
				if len(les.CategoryIDs) > 0 && c.errorCategories != nil {
					if catDef, ok := c.errorCategories.Get(les.CategoryIDs[0]); ok {
						lesson.Guidance = catDef.Guidance
					}
				}
				tk.Lessons = append(tk.Lessons, lesson)
			}
			asmCtx.LessonsLearned = tk
		}
	}

	return asmCtx
}

// availableToolNames returns the full list of tool names registered in the system.
// This is a best-effort list for prompt assembly — actual tool availability is
// controlled by the agentic-tools component at runtime.
func (c *Component) availableToolNames() []string {
	// review_scenario was registered for the legacy scenario-reviewer
	// terminal that was deleted; left it in for cleanup tracking. Dropped
	// 2026-05-08 take-14 follow-up — no executor implements it, so its
	// presence here just bloated the wire palette and confused small
	// models that picked it instead of submit_work.
	// research SHELVED 2026-05-15 — removed from dev's wire palette
	// after take-27 evidence showed dispatch worked but didn't fix
	// the actual wedge shape (RepeatToolFailure on bash 404
	// worktree-not-found). Pivoted to upstream-strengthening. See
	// [[research-shelved-pivot-to-upstream-strengthening-2026-05-15]].
	return []string{
		"bash", "submit_work", "ask_question",
		"write_todos", "scratchpad",
		"graph_search", "graph_query", "graph_summary",
		"web_search", "http_request",
		"decompose_task",
	}
}

// parentRequirementTerminated returns true when a DAG-node task's parent
// requirement has reached a terminal state (completed/failed/error). Called
// before each pipeline-stage dispatch so we stop burning LLM calls on orphan
// work after requirement-executor has given up on the parent. Tasks created
// outside the requirement-executor flow (RequirementID empty) always return
// false — they have no parent to check.
func (c *Component) parentRequirementTerminated(exec *taskExecution) (bool, string) {
	if exec.RequirementID == "" {
		return false, ""
	}
	key := workflow.RequirementExecutionKey(exec.Slug, exec.RequirementID)
	req, ok := c.store.getReq(key)
	if !ok {
		return false, "" // parent state not in cache/KV — don't block dispatch
	}
	if workflow.IsTerminalReqStage(req.Stage) {
		return true, req.Stage
	}
	return false, ""
}

// ---------------------------------------------------------------------------
// Agent dispatch: Developer (Stage 1 — full TDD cycle: tests + implementation)
// ---------------------------------------------------------------------------

func (c *Component) dispatchDeveloperLocked(ctx context.Context, exec *taskExecution) {
	if terminated, stage := c.parentRequirementTerminated(exec); terminated {
		c.logger.Info("Parent requirement terminal — skipping developer dispatch",
			"task_id", exec.TaskID, "requirement_id", exec.RequirementID, "parent_stage", stage)
		c.markErrorLocked(ctx, exec, fmt.Sprintf("parent_requirement_%s", stage))
		return
	}
	if err := c.checkWorktreeExists(ctx, exec); err != nil {
		c.logger.Warn("Worktree lost — marking execution as error",
			"task_id", exec.TaskID,
			"worktree_path", exec.WorktreePath,
			"error", err,
		)
		c.markErrorLocked(ctx, exec, "worktree_lost")
		return
	}

	taskID := fmt.Sprintf("dev-%s-%s", exec.EntityID, uuid.New().String())
	exec.DeveloperTaskID = taskID
	c.taskRouting.Set(taskID, exec.EntityID)

	// Resolve developer model via capability registry. config.Model is the
	// component-level override knob; the canonical path is CapabilityCoding →
	// registry. exec.Model (set upstream by req-executor when it dispatched
	// the node) is intentionally NOT consulted here — the previous code path
	// let req-executor.config.Model silently pin every downstream developer,
	// which made execution-manager.config.Model inert and tripped the take-7
	// MoE-vs-dense bug. Each dispatch site owns its own resolution; upstream
	// state does not leak into role-specific routing.
	devModel := model.ResolveModel(c.modelRegistry, c.config.Model, model.CapabilityCoding)

	// Assemble system prompt via fragment pipeline. Pass devModel so the
	// assembler sees HasResponseFormat for the endpoint the dispatch will
	// hit; ResponseFormat is attached on the TaskMessage below.
	asmCtx := c.buildAssemblyContext(ctx, prompt.RoleDeveloper, exec, devModel)
	assembled := c.assembler.Assemble(asmCtx)

	userPrompt := exec.Prompt
	if exec.TDDCycle > 0 && exec.Feedback != "" {
		userPrompt += "\n\n---\n\nREVISION REQUEST: Your previous implementation was rejected.\n\n" + exec.Feedback
	}
	// (b2/b3): when the dev is on cycle N>0, render prior cycles'
	// outcomes + reviewer feedback as a structured "PRIOR ATTEMPTS"
	// block. Pattern recognition over the cycle sequence — the
	// deterministic equivalent of what gemini-pro recovery agent
	// would write up. Cheap (no LLM call), tier-agnostic (works on
	// flat model registries), Goodhart-safe (information not
	// safety net).
	if history := summarizeCycleHistory(exec.PriorCycles); history != "" {
		userPrompt += "\n\n---\n\n" + history
	}
	// ADR-037 stage-1 (a3): if a recovery PlanDecision was accepted on the
	// req between cycles, plan-decision-handler.applyRecoveryHint wrote the
	// recovery agent's diagnosis onto req.RecoveryHint. Surface it to the
	// dev in the same channel as reviewer feedback so the manager-role
	// recommendation reaches the wedged agent. Cleared on req completion.
	if hint := c.lookupRecoveryHint(ctx, exec.Slug, exec.RequirementID); hint != "" {
		userPrompt += "\n\n---\n\nMANAGER RECOVERY GUIDANCE (from a manager-role analysis of your prior cycle's wedge — apply this BEFORE re-attempting the same approach):\n\n" + hint
	}

	var endpoint *ssmodel.EndpointConfig
	if c.modelRegistry != nil {
		endpoint = c.modelRegistry.GetEndpoint(devModel)
	}

	task := &agentic.TaskMessage{
		TaskID: taskID,
		Role:   agentic.RoleGeneral,
		Model:  devModel,
		// Filter the wire tool palette by RoleDeveloper. Without this, the
		// developer sees every registered tool — including decompose_task
		// (a requirement-executor terminal) and review_scenario (a
		// scenario-reviewer terminal). Take 11 (2026-05-08) had qwen3.6-27b
		// call decompose_task instead of submit_work after exploring with
		// bash, then wedge with finish_reason=stop because the dispatch had
		// no developer-shaped next move. The prompt-side FilterTools above
		// only filters guidance text; the wire palette must be filtered
		// here too.
		Tools:        terminal.ToolsForEndpoint(c.toolRegistry, "developer", endpoint, prompt.FilterTools(c.availableToolNames(), prompt.RoleDeveloper)...),
		WorkflowSlug: WorkflowSlugTaskExecution,
		WorkflowStep: stageDevelop,
		Prompt:       userPrompt,
		ToolChoice:   prompt.ResolveToolChoice(prompt.RoleDeveloper, asmCtx.AvailableTools),
		Context: &agentic.ConstructedContext{
			Content: assembled.SystemMessage,
		},
		Metadata: map[string]any{
			"plan_slug":        exec.Slug,
			"task_id":          exec.TaskID,
			"deliverable_type": "developer",
			// role + model for SKG tool.recovery.incident partitioning.
			"role":  string(prompt.RoleDeveloper),
			"model": devModel,
		},
		ResponseFormat: terminal.ResponseFormatForEndpoint(endpoint, "developer"),
	}
	c.publishTask(ctx, "agent.task.development", task)

	c.logger.Info("Dispatched developer",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"tdd_cycle", exec.TDDCycle,
		"developer_task_id", taskID,
		"fragments", len(assembled.FragmentsUsed),
		"system_chars", assembled.SystemMessageChars,
	)
}

// ---------------------------------------------------------------------------
// Agent dispatch: Structural Validator
// ---------------------------------------------------------------------------

func (c *Component) dispatchValidatorLocked(ctx context.Context, exec *taskExecution) {
	if terminated, stage := c.parentRequirementTerminated(exec); terminated {
		c.logger.Info("Parent requirement terminal — skipping validator dispatch",
			"task_id", exec.TaskID, "requirement_id", exec.RequirementID, "parent_stage", stage)
		c.markErrorLocked(ctx, exec, fmt.Sprintf("parent_requirement_%s", stage))
		return
	}
	if err := c.checkWorktreeExists(ctx, exec); err != nil {
		c.logger.Warn("Worktree lost — marking execution as error",
			"task_id", exec.TaskID,
			"worktree_path", exec.WorktreePath,
			"error", err,
		)
		c.markErrorLocked(ctx, exec, "worktree_lost")
		return
	}

	c.logger.Info("Dispatching structural validation",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"tdd_cycle", exec.TDDCycle,
	)

	// Release lock while waiting for the deterministic validator.
	exec.mu.Unlock()
	result, err := c.runStructuralValidation(ctx, exec)
	exec.mu.Lock()

	if exec.terminated {
		return
	}

	if err != nil {
		c.logger.Error("Structural validation failed",
			"slug", exec.Slug,
			"error", err,
		)
		c.markEscalatedLocked(ctx, exec, fmt.Sprintf("structural validation error: %v", err))
		return
	}

	exec.ValidationPassed = result.Passed
	exec.ValidationResults = result.CheckResults

	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.ValidationPassed, fmt.Sprintf("%t", exec.ValidationPassed))

	if !exec.ValidationPassed {
		if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseValidationFailed); err != nil {
			c.logger.Error("Failed to write phase triple", "phase", phaseValidationFailed, "error", err)
		}
		exec.Stage = phaseValidationFailed
		c.syncToStore(ctx, exec)

		c.extractStructuralLessons(ctx, exec, exec.ValidationResults)

		if exec.TDDCycle+1 < exec.MaxTDDCycles {
			c.startDeveloperRetryLocked(ctx, exec, buildValidationFailureFeedback(exec.ValidationResults))
		} else {
			c.markEscalatedLocked(ctx, exec, "validation failures exceeded TDD cycle budget")
		}
		return
	}

	c.logger.Info("Validation passed, dispatching reviewer",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"tdd_cycle", exec.TDDCycle,
	)

	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseReviewing); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseReviewing, "error", err)
	}
	exec.Stage = phaseReviewing
	c.syncToStore(ctx, exec)
	c.dispatchReviewerLocked(ctx, exec)
}

// runStructuralValidation publishes a ValidationRequest to the structural-validator
// component and waits for the result. Same pattern as ask_question — fire and wait.
func (c *Component) runStructuralValidation(ctx context.Context, exec *taskExecution) (payloads.ValidationResult, error) {
	timeout := 30 * time.Second

	req := &payloads.ValidationRequest{
		ExecutionID:     uuid.New().String(),
		Slug:            exec.Slug,
		FilesModified:   exec.FilesModified,
		WorktreePath:    exec.WorktreePath,
		TaskID:          exec.TaskID,
		DeveloperLoopID: exec.DeveloperLoopID,
		TraceID:         exec.TraceID,
	}

	baseMsg := message.NewBaseMessage(req.Schema(), req, componentName)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return payloads.ValidationResult{}, fmt.Errorf("marshal validation request: %w", err)
	}

	resultSubject := fmt.Sprintf("workflow.result.structural-validator.%s", exec.Slug)
	js, err := c.natsClient.JetStream()
	if err != nil {
		return payloads.ValidationResult{}, fmt.Errorf("get jetstream: %w", err)
	}

	stream, err := js.Stream(ctx, "WORKFLOW")
	if err != nil {
		return payloads.ValidationResult{}, fmt.Errorf("get WORKFLOW stream: %w", err)
	}

	consumerName := fmt.Sprintf("val-%d", time.Now().UnixNano())
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name:          consumerName,
		FilterSubject: resultSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverNewPolicy,
	})
	if err != nil {
		return payloads.ValidationResult{}, fmt.Errorf("create validation consumer: %w", err)
	}
	defer func() {
		_ = stream.DeleteConsumer(context.Background(), consumerName)
	}()

	// Small delay to ensure JetStream consumer is fully registered before
	// publishing the request. Without this, the validator may respond before
	// our consumer catches the result (DeliverNewPolicy race).
	time.Sleep(50 * time.Millisecond)

	// Publish validation request.
	if err := c.natsClient.PublishToStream(ctx, "workflow.async.structural-validator", data); err != nil {
		return payloads.ValidationResult{}, fmt.Errorf("publish validation request: %w", err)
	}

	c.logger.Debug("Published validation request, waiting for result",
		"slug", exec.Slug,
		"subject", resultSubject,
		"timeout", timeout,
	)

	// Wait for result with timeout.
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		msgs, fetchErr := consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
		if fetchErr != nil {
			if waitCtx.Err() != nil {
				return payloads.ValidationResult{}, fmt.Errorf("validation timed out after %s", timeout)
			}
			continue
		}

		for msg := range msgs.Messages() {
			_ = msg.Ack()
			c.logger.Debug("Received validation result message",
				"slug", exec.Slug,
				"subject", msg.Subject(),
				"data_len", len(msg.Data()),
			)

			// Deserialize via registered BaseMessage payload.
			base, err := c.decoder.Decode(msg.Data())
			if err != nil {
				return payloads.ValidationResult{}, fmt.Errorf("unmarshal validation result BaseMessage: %w", err)
			}
			vr, ok := base.Payload().(*payloads.ValidationResult)
			if !ok {
				return payloads.ValidationResult{}, fmt.Errorf("unexpected payload type %T, want *payloads.ValidationResult", base.Payload())
			}
			return *vr, nil
		}

		if waitCtx.Err() != nil {
			return payloads.ValidationResult{}, fmt.Errorf("validation timed out after %s", timeout)
		}
	}
}

// ---------------------------------------------------------------------------
// Agent dispatch: Code Reviewer
// ---------------------------------------------------------------------------

func (c *Component) dispatchReviewerLocked(ctx context.Context, exec *taskExecution) {
	if terminated, stage := c.parentRequirementTerminated(exec); terminated {
		c.logger.Info("Parent requirement terminal — skipping reviewer dispatch",
			"task_id", exec.TaskID, "requirement_id", exec.RequirementID, "parent_stage", stage)
		c.markErrorLocked(ctx, exec, fmt.Sprintf("parent_requirement_%s", stage))
		return
	}
	if err := c.checkWorktreeExists(ctx, exec); err != nil {
		c.logger.Warn("Worktree lost — marking execution as error",
			"task_id", exec.TaskID,
			"worktree_path", exec.WorktreePath,
			"error", err,
		)
		c.markErrorLocked(ctx, exec, "worktree_lost")
		return
	}

	taskID := fmt.Sprintf("rev-%s-%s", exec.EntityID, uuid.New().String())
	exec.ReviewerTaskID = taskID
	c.taskRouting.Set(taskID, exec.EntityID)

	// Resolve the reviewer model up-front so buildAssemblyContext sees the
	// endpoint the dispatch will actually hit (ResponseFormat support is
	// per-endpoint and gates schema-prose elision in the assembled prompt).
	// Capability-first with config.CodeReviewerModel as a hard override —
	// matches the pattern used by the developer dispatch above.
	reviewerModel := model.ResolveModel(c.modelRegistry, c.config.CodeReviewerModel, model.CapabilityReviewing)

	// Assemble system prompt via fragment pipeline.
	asmCtx := c.buildAssemblyContext(ctx, prompt.RoleReviewer, exec, reviewerModel)
	assembled := c.assembler.Assemble(asmCtx)

	// User prompt carries task context (what to review), not implementation
	// instructions. The system message fragments (software.reviewer.*) drive
	// review behavior — the user prompt just identifies the subject.
	var reviewSubject strings.Builder
	if exec.ReviewerParseError != "" {
		// Parse-retry: surface the prior failure so the model knows what
		// shape was rejected and produces a parseable verdict this time.
		reviewSubject.WriteString("## Previous attempt failed\n\nYour previous response could not be parsed:\n\n```\n")
		reviewSubject.WriteString(exec.ReviewerParseError)
		reviewSubject.WriteString("\n```\n\nProduce a valid response this time. Address the parse failure above; the review subject below has not changed.\n\n")
	}
	reviewSubject.WriteString("Task: ")
	reviewSubject.WriteString(exec.Title)
	if len(exec.FilesModified) > 0 {
		reviewSubject.WriteString("\nFiles modified: ")
		reviewSubject.WriteString(strings.Join(exec.FilesModified, ", "))
	}

	var reviewerEndpoint *ssmodel.EndpointConfig
	if c.modelRegistry != nil {
		reviewerEndpoint = c.modelRegistry.GetEndpoint(reviewerModel)
	}

	task := &agentic.TaskMessage{
		TaskID: taskID,
		Role:   agentic.RoleReviewer,
		Model:  reviewerModel,
		// Filter the wire tool palette by RoleReviewer — same rationale as
		// the developer dispatch above. Reviewer's filter excludes
		// decompose_task / review_scenario / web_search / http_request.
		Tools:        terminal.ToolsForEndpoint(c.toolRegistry, "review", reviewerEndpoint, prompt.FilterTools(c.availableToolNames(), prompt.RoleReviewer)...),
		WorkflowSlug: WorkflowSlugTaskExecution,
		WorkflowStep: stageReview,
		Prompt:       reviewSubject.String(),
		ToolChoice:   prompt.ResolveToolChoice(prompt.RoleReviewer, asmCtx.AvailableTools),
		Context: &agentic.ConstructedContext{
			Content: assembled.SystemMessage,
		},
		Metadata: map[string]any{
			"plan_slug":        exec.Slug,
			"task_id":          exec.TaskID,
			"deliverable_type": "review",
			// role + model for SKG tool.recovery.incident partitioning.
			"role":  string(prompt.RoleReviewer),
			"model": reviewerModel,
		},
		ResponseFormat: terminal.ResponseFormatForEndpoint(reviewerEndpoint, "review"),
	}
	c.publishTask(ctx, "agent.task.reviewer", task)

	c.logger.Info("Dispatched code reviewer",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"tdd_cycle", exec.TDDCycle,
		"fragments", len(assembled.FragmentsUsed),
		"system_chars", assembled.SystemMessageChars,
	)
}

// ---------------------------------------------------------------------------
// Worktree lifecycle helpers
// ---------------------------------------------------------------------------

// checkWorktreeExists probes the sandbox to confirm the worktree for this
// execution still exists. It is called before dispatching any TDD stage so
// that a lost worktree (e.g. sandbox restart) is caught early and marked as
// error rather than silently dispatching an agent that will fail at tool time.
//
// Returns nil when:
//   - sandbox is not configured (c.config.SandboxURL == "")
//   - no worktree has been assigned to this execution yet
//   - the sandbox is unreachable (connection error) — fail later at dispatch
//
// Returns an error only on an explicit 404 (worktree gone) or other non-200
// HTTP response from the sandbox.
//
// Caller must hold exec.mu.
func (c *Component) checkWorktreeExists(ctx context.Context, exec *taskExecution) error {
	if c.config.SandboxURL == "" {
		return nil
	}
	if exec.WorktreePath == "" {
		return nil
	}

	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet,
		c.config.SandboxURL+"/worktree/"+exec.TaskID, nil)
	if err != nil {
		// Malformed URL — unexpected but not a worktree-lost condition.
		return fmt.Errorf("checkWorktreeExists: build request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// Network error (connection refused, timeout, etc.) — treat as unknown,
		// not as missing. The dispatch will fail with a clearer error if the
		// sandbox is truly down.
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}
	return fmt.Errorf("worktree check: sandbox returned %d for task %s", resp.StatusCode, exec.TaskID)
}

// mergeWorktree merges the worktree for the given execution back into its
// scenario branch (if set) or the current HEAD branch. Merge metadata
// (commit hash, files changed) is captured for lineage tracking.
//
// Returns the merge error after retries are exhausted so the caller can fail
// the task instead of silently marking it approved. A previous version swallowed
// the error and persisted phaseApproved even when the worktree had been deleted
// by requirement-executor during a cross-component race; the result was "green"
// status for changes that never made it to main. Callers MUST check the return
// value. Returns nil (not an error) for no-op cases: sandbox disabled, empty
// WorktreePath, or component shutting down.
// Caller must hold exec.mu.
func (c *Component) mergeWorktree(exec *taskExecution) error {
	if c.sandbox == nil || exec.WorktreePath == "" {
		return nil
	}

	// Derive a context that outlives request cancellation so the merge can
	// complete even if the reviewer's ctx is cancelled. Fall back to Background
	// when shutdownCtx hasn't been set yet (e.g. in unit tests that skip Start).
	parent := c.shutdownCtx
	if parent == nil {
		parent = context.Background()
	}
	mergeCtx := context.WithoutCancel(parent)

	// Pre-flight: if the worktree no longer exists (e.g. deleted by a parent
	// requirement's cleanup), fail fast with a clean reason rather than
	// producing "chdir: no such file or directory" noise from the merge retry.
	if err := c.checkWorktreeExists(mergeCtx, exec); err != nil {
		return fmt.Errorf("merge worktree: %w", err)
	}

	var opts []sandbox.MergeOption
	if exec.ScenarioBranch != "" {
		opts = append(opts, sandbox.WithTargetBranch(exec.ScenarioBranch))
	}
	opts = append(opts, sandbox.WithCommitMessage(fmt.Sprintf("feat(%s): %s", exec.Slug, exec.TaskID)))
	// Provenance trailers (invariant A1 from docs/audit/task-11-worktree-invariants.md).
	// Every merge commit carries enough identity that `git log` alone can trace
	// the change back to its plan / requirement / loop / task without needing
	// to cross-reference EXECUTION_STATES. Node-ID is deferred — it lives on
	// NodeResult rather than TaskExecution, so plumbing it through requires
	// TaskCreateRequest surgery and is tracked as follow-up.
	opts = append(opts, sandbox.WithTrailer("Task-ID", exec.TaskID))
	opts = append(opts, sandbox.WithTrailer("Plan-Slug", exec.Slug))
	if exec.RequirementID != "" {
		opts = append(opts, sandbox.WithTrailer("Requirement-ID", exec.RequirementID))
	}
	if exec.LoopID != "" {
		opts = append(opts, sandbox.WithTrailer("Loop-ID", exec.LoopID))
	}
	// Keep worktree alive so requirement-level reviewer can access files.
	// requirement-executor calls DeleteWorktree after review completes.
	opts = append(opts, sandbox.WithKeepWorktree())
	if exec.TraceID != "" {
		opts = append(opts, sandbox.WithTrailer("Trace-ID", exec.TraceID))
	}

	// Retry merge up to 3 times — concurrent node merges can conflict when
	// the sandbox repo lock is contended. EXCEPT when the sandbox has flagged
	// itself as needing reconciliation: retrying against a wedged repo just
	// burns tokens and delays the human signal.
	//
	// Layer-1 retry (this loop): transient INFRASTRUCTURE failures —
	// repoMu contention between parallel merges, transient git plumbing
	// errors ("stash failed: exit status 128" from auto-stash on a busy
	// repo, etc.). Same worktree, same hash, retry verbatim.
	// Origin: commit 2ff4b6f (2026-04-16, Gemini @easy E2E surfacing).
	//
	// Layer-2 retry lives in processor/requirement-executor/component.go's
	// node-failure path: when this loop exhausts its 3 attempts and the
	// task moves to stage=error, requirement-executor decides whether to
	// re-dispatch the developer agent for the SAME node with the prior
	// workspace + feedback. That layer fixes AGENT flakes (TDD budget
	// exhaustion, bug-#9 claim/observation mismatch, code that doesn't
	// compile). Different cause, different remedy, do not collapse.
	//
	// Today's repro (2026-04-29 Gemini @easy success) had 1 layer-1 retry
	// loop succeed first try, but layer-2 fire twice on a different node
	// because the developer reported files_modified that produced no
	// commit. The third re-dispatch wrote real code and the merge worked.
	var result *sandbox.MergeResult
	var err error
	for attempt := range 3 {
		result, err = c.sandbox.MergeWorktree(mergeCtx, exec.TaskID, opts...)
		if err == nil {
			break
		}
		if c.shutdownCtx != nil && c.shutdownCtx.Err() != nil {
			return nil // component shutting down — don't treat as a task failure
		}
		if errors.Is(err, sandbox.ErrNeedsReconciliation) {
			// Sandbox is wedged. Do not retry. Fall through to the failure
			// path below, but with an INFRASTRUCTURE-prefixed error so UI/
			// retry logic can distinguish it from an agent-level merge conflict.
			c.logger.Error("Worktree merge blocked — sandbox needs reconciliation; skipping retry",
				"slug", exec.Slug,
				"task_id", exec.TaskID,
				"attempt", attempt+1,
				"error", err,
			)
			break
		}
		c.logger.Warn("Worktree merge failed, retrying",
			"slug", exec.Slug,
			"task_id", exec.TaskID,
			"attempt", attempt+1,
			"error", err,
		)
		time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
	}
	if err != nil {
		c.logger.Warn("Worktree merge failed after retries; failing task",
			"slug", exec.Slug,
			"task_id", exec.TaskID,
			"error", err,
		)
		// Caller (markApprovedLocked) calls markErrorLocked with a reason that
		// classifies infra vs agent failure and writes the authoritative
		// ErrorReason triple — so mergeWorktree only needs to propagate the
		// wrapped error. errors.Is(err, sandbox.ErrNeedsReconciliation) still
		// matches on the returned chain for that classification.
		return fmt.Errorf("merge worktree after retries: %w", err)
	}

	// Claim/observation cross-check: the developer reported FilesModified
	// but the sandbox observed no commit. This is the silent-work-drop
	// pattern from the 2026-04-27 Gemini @t2 run (bug #9). Two failure
	// modes:
	//   - Sandbox set NothingToCommit=true (true no-op), but the developer
	//     claimed work — meaning the tools agent reported files it didn't
	//     actually write to the worktree. Phantom completion territory.
	//   - Sandbox returned an empty Commit without NothingToCommit (a
	//     malformed response that pre-FIX-A could happen on certain retry
	//     paths). Defensive — fail rather than trust a malformed response.
	if c.config.requireMergeObservation() && len(exec.FilesModified) > 0 && (result.NothingToCommit || result.Commit == "") {
		c.logger.Error("Merge claim/observation mismatch — developer reported files_modified but sandbox observed no commit",
			"slug", exec.Slug,
			"task_id", exec.TaskID,
			"claimed_files", exec.FilesModified,
			"nothing_to_commit", result.NothingToCommit,
			"commit", result.Commit,
		)
		return fmt.Errorf("merge claim/observation mismatch: developer reported %d files but sandbox produced no commit (nothing_to_commit=%v)", len(exec.FilesModified), result.NothingToCommit)
	}

	c.logger.Info("Worktree merged successfully",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"commit", result.Commit,
		"files_changed", len(result.FilesChanged),
		"nothing_to_commit", result.NothingToCommit,
	)
	// Update FilesModified with definitive file list from merge.
	if len(result.FilesChanged) > 0 {
		exec.FilesModified = make([]string, len(result.FilesChanged))
		for i, f := range result.FilesChanged {
			exec.FilesModified[i] = f.Path
		}
	}
	// Record the merge commit so requirement-executor's claim/observation
	// gate can read it from EXECUTION_STATES KV. Empty for the legitimate
	// no-op case (NothingToCommit==true with no claimed files), which is
	// indistinguishable from "task never had a worktree" — the downstream
	// gate only fires when FilesModified is non-empty.
	exec.MergeCommit = result.Commit

	// Wait for semsource to index the merge commit so dependent tasks
	// get fresh graph context. Soft gate: proceeds with warning on timeout.
	c.awaitIndexing(result.Commit, exec.TaskID)
	return nil
}

// awaitIndexing waits for semsource to index a merge commit. No-op when the
// indexing gate is not configured. Timeout produces a warning, not an error.
// Uses a context that cancels on component shutdown so the gate doesn't delay
// graceful stop.
func (c *Component) awaitIndexing(commitSHA, taskID string) {
	if c.indexingGate == nil || commitSHA == "" {
		return
	}

	budget := c.config.GetIndexingBudget()
	if budget <= 0 {
		budget = workflow.DefaultIndexingBudget
	}

	// Cancel the gate if the component is shutting down.
	ctx, cancel := context.WithCancel(c.shutdownCtx)
	defer cancel()

	if err := c.indexingGate.AwaitCommitIndexed(ctx, commitSHA, budget); err != nil {
		c.logger.Warn("Indexing gate timed out; dependent task may have stale context",
			"commit", commitSHA,
			"budget", budget,
			"task_id", taskID,
		)
	} else {
		c.logger.Info("Merge commit indexed by semsource",
			"commit", commitSHA,
			"task_id", taskID,
		)
	}
}

// discardWorktree deletes the worktree for the given execution.
// Best-effort: failures are logged but never block terminal transitions.
// Caller must hold exec.mu.
func (c *Component) discardWorktree(exec *taskExecution) {
	if c.sandbox == nil || exec.WorktreePath == "" {
		return
	}
	if err := c.sandbox.DeleteWorktree(context.Background(), exec.TaskID); err != nil {
		c.logger.Warn("Failed to delete worktree",
			"slug", exec.Slug,
			"task_id", exec.TaskID,
			"error", err,
		)
	} else {
		c.logger.Debug("Worktree discarded",
			"slug", exec.Slug,
			"task_id", exec.TaskID,
		)
	}
}

// ---------------------------------------------------------------------------
// Requirement-executor loop completion relay
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Triple and task publishing helpers
// ---------------------------------------------------------------------------

// publishTask wraps a TaskMessage in a BaseMessage and publishes to JetStream.
func (c *Component) publishTask(ctx context.Context, subject string, task *agentic.TaskMessage) {
	baseMsg := message.NewBaseMessage(task.Schema(), task, componentName)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Debug("Failed to marshal task message", "error", err)
		c.errors.Add(1)
		return
	}

	if c.natsClient != nil {
		if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
			c.logger.Debug("Failed to publish task", "subject", subject, "error", err)
			c.errors.Add(1)
		}
	}
}

// ---------------------------------------------------------------------------
// Utility helpers
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// component.Discoverable interface
// ---------------------------------------------------------------------------

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        componentName,
		Type:        "processor",
		Description: "Orchestrates TDD task execution pipeline: developer → validator → reviewer with retry and escalation",
		Version:     componentVersion,
	}
}

// InputPorts returns the component's declared input ports.
func (c *Component) InputPorts() []component.Port { return c.inputPorts }

// OutputPorts returns the component's declared output ports.
func (c *Component) OutputPorts() []component.Port { return c.outputPorts }

// ConfigSchema returns the JSON schema for this component's configuration.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return executionOrchestratorSchema
}

// Health returns the current health status of the component.
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	running := c.running
	c.mu.RUnlock()

	if running {
		return component.HealthStatus{
			Healthy:    true,
			Status:     "healthy",
			LastCheck:  time.Now(),
			ErrorCount: int(c.errors.Load()),
		}
	}
	return component.HealthStatus{Status: "stopped"}
}

// DataFlow returns current flow metrics for the component.
func (c *Component) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{
		LastActivity: c.getLastActivity(),
	}
}
