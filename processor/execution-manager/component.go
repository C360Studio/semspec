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

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/prompt"
	promptdomain "github.com/c360studio/semspec/prompt/domain"
	"github.com/c360studio/semspec/tools/sandbox"
	"github.com/c360studio/semspec/tools/terminal"
	workflowtools "github.com/c360studio/semspec/tools/workflow"
	wf "github.com/c360studio/semspec/vocabulary/workflow"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semspec/workflow/lessons"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semspec/workflow/phases"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
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
	platform     component.PlatformMeta
	tripleWriter *graphutil.TripleWriter
	sandbox      worktreeManager        // required — Start() fails if not configured
	indexingGate *workflow.IndexingGate // nil when graph-gateway not configured
	assembler    *prompt.Assembler      // composes system prompts for each pipeline stage

	// Lesson system — writes/reads through graph pipeline (no direct KV dependency).
	lessonWriter    *lessons.Writer
	errorCategories *workflow.ErrorCategoryRegistry
	modelRegistry   *model.Registry

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
		config:       cfg,
		natsClient:   deps.NATSClient,
		logger:       logger,
		platform:     deps.Platform,
		sandbox:      newWorktreeManager(cfg.SandboxURL),
		indexingGate: workflow.NewIndexingGate(cfg.GraphGatewayURL, logger),
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

	c.modelRegistry = model.NewDefaultRegistry()

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

func (c *Component) handleDeveloperCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *taskExecution) {
	c.resetTimeoutLocked(exec)
	c.taskRouting.Delete(exec.DeveloperTaskID)

	if event.Outcome != agentic.OutcomeSuccess {
		reason := fmt.Sprintf("developer loop failed: outcome=%s", event.Outcome)
		c.routeFixableRejection(ctx, exec, reason)
		return
	}

	var result payloads.DeveloperResult
	parseErr := json.Unmarshal([]byte(llm.ExtractJSON(event.Result)), &result)
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

// ---------------------------------------------------------------------------
// Stage 4: Reviewer complete
// ---------------------------------------------------------------------------

func (c *Component) handleReviewerCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *taskExecution) {
	c.resetTimeoutLocked(exec)
	c.taskRouting.Delete(exec.ReviewerTaskID)

	if event.Outcome != agentic.OutcomeSuccess {
		reason := fmt.Sprintf("reviewer loop failed: outcome=%s", event.Outcome)
		c.routeFixableRejection(ctx, exec, reason)
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

	// Extract lessons from reviewer feedback (both approval and rejection).
	c.extractLessons(ctx, exec, result.Feedback, result.Verdict)

	if result.Verdict == "approved" {
		c.markApprovedLocked(ctx, exec)
		return
	}

	c.handleRejectionLocked(ctx, exec, result)
}

// parseCodeReviewResult unmarshals the reviewer JSON result. Returns (result, true)
// on success. On parse failure, returns a default rejected result and false so the
// caller can decide whether to retry. Strips markdown code fences that some LLMs
// wrap around JSON tool responses.
func (c *Component) parseCodeReviewResult(raw string, slug string) (payloads.TaskCodeReviewResult, bool) {
	var result payloads.TaskCodeReviewResult
	cleaned := llm.ExtractJSON(raw)
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

	switch result.RejectionType {
	case rejectionTypeRestructure:
		c.markEscalatedLocked(ctx, exec, fmt.Sprintf("restructure rejection: %s", result.RejectionType))
	default:
		enrichedFeedback := c.enrichFeedbackWithGuidance(result.Feedback, matchedCategories)
		c.routeFixableRejection(ctx, exec, enrichedFeedback)
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

	// Notify callers that the TDD pipeline escalated (treated as failure).

	c.publishEntity(context.Background(), NewTaskExecutionEntity(exec).WithPhase(phaseEscalated).WithErrorReason(reason))
	c.cleanupExecutionLocked(exec)
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
// The ctx parameter is used for graph reads (error trends, lessons); context.WithoutCancel
// is applied so these reads survive caller cancellation without inheriting the deadline.
func (c *Component) buildAssemblyContext(ctx context.Context, role prompt.Role, exec *taskExecution) *prompt.AssemblyContext {
	var maxTokens int
	if c.modelRegistry != nil {
		if ep := c.modelRegistry.GetEndpoint(exec.Model); ep != nil {
			maxTokens = ep.MaxTokens
		}
	}
	asmCtx := &prompt.AssemblyContext{
		Role:           role,
		Provider:       resolveProvider(exec.Model),
		Domain:         "software",
		AvailableTools: prompt.FilterTools(c.availableToolNames(), role),
		SupportsTools:  true,
		MaxTokens:      maxTokens,
		Persona:        prompt.GlobalPersonas().ForRole(role),
		Vocabulary:     prompt.GlobalPersonas().Vocabulary(),
	}

	// Wire role-filtered project standards.
	if c.standards != nil {
		asmCtx.Standards = prompt.NewStandardsContext(c.standards.ForRole(string(role)))
	}

	// Wire task context for execution roles.
	if role == prompt.RoleDeveloper ||
		role == prompt.RoleValidator || role == prompt.RoleReviewer {
		asmCtx.TaskContext = &prompt.TaskContext{
			PlanGoal:      exec.Title,
			IsRetry:       exec.TDDCycle > 0,
			Feedback:      exec.Feedback,
			Iteration:     exec.TDDCycle + 1, // 1-based for display
			MaxIterations: exec.MaxTDDCycles,
			Checklist:     c.checklist,
			TestSurface:   c.readPlanTestSurface(ctx, exec.Slug),
		}

		// Populate ErrorTrends from role-scoped lesson counts. Use threshold 0
		// so even first-time errors surface in the retry prompt. Graph reads use
		// a detached context so they survive caller cancellation.
		if c.lessonWriter != nil && c.errorCategories != nil {
			graphCtx := context.WithoutCancel(ctx)
			if counts, err := c.lessonWriter.GetRoleLessonCounts(graphCtx, "developer"); err == nil {
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

	// Wire role-scoped lessons learned.
	if c.lessonWriter != nil {
		graphCtx := context.WithoutCancel(ctx)
		lessons, err := c.lessonWriter.ListLessonsForRole(graphCtx, "developer", 10)
		if err == nil && len(lessons) > 0 {
			tk := &prompt.LessonsLearned{}
			for _, les := range lessons {
				lesson := prompt.LessonEntry{
					Category: les.Source,
					Summary:  les.Summary,
					Role:     les.Role,
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
	return []string{
		"bash", "submit_work", "ask_question",
		"graph_search", "graph_query", "graph_summary",
		"web_search", "http_request",
		"decompose_task",
		"review_scenario",
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

	// Assemble system prompt via fragment pipeline.
	asmCtx := c.buildAssemblyContext(ctx, prompt.RoleDeveloper, exec)
	assembled := c.assembler.Assemble(asmCtx)

	userPrompt := exec.Prompt
	if exec.TDDCycle > 0 && exec.Feedback != "" {
		userPrompt += "\n\n---\n\nREVISION REQUEST: Your previous implementation was rejected.\n\n" + exec.Feedback
	}

	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleGeneral,
		Model:        exec.Model,
		Tools:        terminal.ToolsForDeliverable("developer", c.availableToolNames()...),
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
		},
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

		if exec.TDDCycle+1 < exec.MaxTDDCycles {
			feedback, _ := json.Marshal(exec.ValidationResults)
			msg := "Structural validation failed. Fix the following issues:\n" + string(feedback)
			c.startDeveloperRetryLocked(ctx, exec, msg)
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
		ExecutionID:   uuid.New().String(),
		Slug:          exec.Slug,
		FilesModified: exec.FilesModified,
		WorktreePath:  exec.WorktreePath,
		TaskID:        exec.TaskID,
		TraceID:       exec.TraceID,
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
			var base message.BaseMessage
			if err := json.Unmarshal(msg.Data(), &base); err != nil {
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

	// Assemble system prompt via fragment pipeline.
	asmCtx := c.buildAssemblyContext(ctx, prompt.RoleReviewer, exec)
	assembled := c.assembler.Assemble(asmCtx)

	// User prompt carries task context (what to review), not implementation
	// instructions. The system message fragments (software.reviewer.*) drive
	// review behavior — the user prompt just identifies the subject.
	var reviewSubject strings.Builder
	reviewSubject.WriteString("Task: ")
	reviewSubject.WriteString(exec.Title)
	if len(exec.FilesModified) > 0 {
		reviewSubject.WriteString("\nFiles modified: ")
		reviewSubject.WriteString(strings.Join(exec.FilesModified, ", "))
	}

	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleReviewer,
		Model:        exec.Model,
		Tools:        terminal.ToolsForDeliverable("review", c.availableToolNames()...),
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
		},
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

	c.logger.Info("Worktree merged successfully",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"commit", result.Commit,
		"files_changed", len(result.FilesChanged),
	)
	// Update FilesModified with definitive file list from merge.
	if len(result.FilesChanged) > 0 {
		exec.FilesModified = make([]string, len(result.FilesChanged))
		for i, f := range result.FilesChanged {
			exec.FilesModified[i] = f.Path
		}
	}

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
