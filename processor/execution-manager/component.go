// Package executionmanager provides a component that orchestrates the task
// execution pipeline: tester → builder → structural validator → code reviewer.
//
// It replaces the reactive task-execution-loop (18 rules) with a single component
// that manages the 4-stage TDD pipeline using entity triples for state and JSON
// rules for terminal transitions.
//
// Pipeline stages:
//  1. Tester  — writes failing unit tests from acceptance criteria (TDD red phase)
//  2. Builder — implements code to make the tests pass (TDD green phase)
//  3. Structural Validator — deterministic checklist validation of modified files
//  4. Code Reviewer — LLM-driven code review with verdict + feedback
//
// On reviewer rejection with remaining budget, the component routes to either the
// builder (fixable implementation issues) or the tester (missing/edge-case tests)
// based on error-category signal matching. On budget exhaustion or non-fixable
// rejection types (misscoped, architectural, too_big), the execution escalates.
//
// Terminal status transitions (completed, escalated, failed) are owned by the
// JSON rule processor, not this component. This component writes workflow.phase;
// rules react and set workflow.status + publish events.
package executionmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sscache "github.com/c360studio/semstreams/pkg/cache"

	"github.com/c360studio/semspec/agentgraph"
	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/prompt"
	promptdomain "github.com/c360studio/semspec/prompt/domain"
	"github.com/c360studio/semspec/tools/sandbox"
	workflowtools "github.com/c360studio/semspec/tools/workflow"
	wf "github.com/c360studio/semspec/vocabulary/workflow"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semspec/workflow/payloads"
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

	// workflowSlugRequirementExecution identifies requirement-executor's direct
	// agentic loops (decomposer, red-team, reviewer). Completion events with this
	// slug are routed to EXECUTION_STATES KV so requirement-executor's watcher
	// can pick them up durably instead of relying on ephemeral agent.complete.>.
	workflowSlugRequirementExecution = "semspec-requirement-execution"

	// Pipeline stage constants used as WorkflowStep in TaskMessages.
	// 4-stage TDD pipeline: test → build → validate → review.
	stageTest     = "test"     // tester writes failing unit tests
	stageBuild    = "build"    // builder implements code to make tests pass
	stageValidate = "validate" // structural checklist + integration tests
	stageReview   = "review"   // LLM code review with verdict + feedback

	// stageDevelop is kept for backward compatibility but is not used in the
	// new 4-stage pipeline.
	stageDevelop = "develop"

	// stageRedTeam is the adversarial challenge stage inserted between
	// validation and review when team-based execution is active.
	stageRedTeam = "red-team"

	// Phase values written to entity triples.
	phaseTesting          = "testing"
	phaseBuilding         = "building"
	phaseValidating       = "validating"
	phaseReviewing        = "reviewing"
	phaseRedTeaming       = "red_teaming"
	phaseApproved         = "approved"
	phaseEscalated        = "escalated"
	phaseError            = "error"
	phaseValidationFailed = "validation_failed"
	phaseRejected         = "rejected"

	// phaseDeveloping is kept for backward compatibility.
	phaseDeveloping = "developing"

	// subjectRedTeamTask is the dispatch subject for red team challenge tasks.
	subjectRedTeamTask = "agent.task.red-team"

	// Trigger subject.
	subjectExecutionTrigger = "workflow.trigger.task-execution-loop"

	// subjectLoopCompleted is the JetStream subject for agentic loop completion events.
	// Subscribe with wildcard; publish with specific loop/task ID suffix.
	subjectLoopCompleted = "agent.complete.>"

	// Downstream dispatch subjects.
	subjectTesterTask  = "agent.task.testing"  // NEW: tester writes unit tests
	subjectBuilderTask = "agent.task.building" // builder implements code

	// Error category IDs that indicate the tester (not builder) should be retried.
	errorCategoryMissingTests   = "missing_tests"
	errorCategoryEdgeCaseMissed = "edge_case_missed"

	// Rejection types that are NOT retryable — escalate immediately.
	rejectionTypeMisscoped     = "misscoped"
	rejectionTypeArchitectural = "architectural"
	rejectionTypeTooBig        = "too_big"
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

// consumerInfo tracks a JetStream consumer created during Start so it can be
// stopped cleanly via StopConsumer rather than context cancellation.
type consumerInfo struct {
	streamName   string
	consumerName string
}

// Component orchestrates the task execution pipeline.
type Component struct {
	config       Config
	natsClient   *natsclient.Client
	logger       *slog.Logger
	platform     component.PlatformMeta
	tripleWriter *graphutil.TripleWriter
	sandbox      worktreeManager        // nil when sandbox is disabled
	indexingGate *workflow.IndexingGate // nil when graph-gateway not configured
	assembler    *prompt.Assembler      // composes system prompts for each pipeline stage

	// Agent roster (Phase B). Nil when ENTITY_STATES bucket is unavailable.
	agentHelper     *agentgraph.Helper
	errorCategories *workflow.ErrorCategoryRegistry
	modelRegistry   *model.Registry

	inputPorts  []component.Port
	outputPorts []component.Port

	// store is the 3-layer execution store (cache + KV + triples).
	store *executionStore

	// activeExecs is a typed TTL cache mapping entityID → *taskExecution.
	// Holds runtime pipeline state (mutexes, timers) for in-flight executions.
	// Entries are explicitly deleted on completion; TTL is a safety net for leaks.
	activeExecs   sscache.Cache[*taskExecution]
	activeExecsMu sync.Mutex // guards get-or-set for duplicate trigger detection

	// taskRouting is a typed TTL cache mapping agent TaskID → entityID.
	// Provides O(1) completion routing from agent loop events to executions.
	taskRouting sscache.Cache[string]

	// checklist holds the project-specific quality gate checks from .semspec/checklist.json.
	// Loaded once at startup; injected into TaskContext so builder/tester prompts show
	// the actual checks that structural-validator will run.
	checklist []workflow.Check

	// Lifecycle
	consumerInfos []consumerInfo
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
	registry.Register(prompt.GraphManifestFragment(workflowtools.FederatedManifestFetchFn()))
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

	c.initAgentGraph()
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

	// Consumer 1: task execution triggers from task-dispatcher.
	triggerCfg := natsclient.StreamConsumerConfig{
		StreamName:    "WORKFLOW",
		ConsumerName:  "execution-orchestrator-execution-trigger",
		FilterSubject: subjectExecutionTrigger,
		DeliverPolicy: "new",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       30 * time.Second,
		MaxAckPending: 1,
	}
	if err := c.natsClient.ConsumeStreamWithConfig(ctx, triggerCfg, c.handleTrigger); err != nil {
		shutdownCancel()
		return fmt.Errorf("consume execution triggers: %w", err)
	}
	c.consumerInfos = append(c.consumerInfos, consumerInfo{
		streamName:   triggerCfg.StreamName,
		consumerName: triggerCfg.ConsumerName,
	})

	// Consumer 2: agentic loop completion events.
	completionCfg := natsclient.StreamConsumerConfig{
		StreamName:    "AGENT",
		ConsumerName:  "execution-orchestrator-loop-completions",
		FilterSubject: subjectLoopCompleted,
		DeliverPolicy: "new",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       30 * time.Second,
		MaxAckPending: 10,
	}
	if err := c.natsClient.ConsumeStreamWithConfig(ctx, completionCfg, c.handleLoopCompleted); err != nil {
		c.natsClient.StopConsumer(triggerCfg.StreamName, triggerCfg.ConsumerName)
		c.consumerInfos = nil
		shutdownCancel()
		return fmt.Errorf("consume loop completions: %w", err)
	}
	c.consumerInfos = append(c.consumerInfos, consumerInfo{
		streamName:   completionCfg.StreamName,
		consumerName: completionCfg.ConsumerName,
	})

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

	for _, info := range c.consumerInfos {
		c.natsClient.StopConsumer(info.streamName, info.consumerName)
	}
	c.consumerInfos = nil

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
}

func (c *Component) initAgentGraph() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	bucket, err := c.natsClient.GetKeyValueBucket(ctx, "ENTITY_STATES")
	if err != nil {
		c.logger.Debug("ENTITY_STATES bucket not available — agent selection disabled", "error", err)
		return
	}
	kvStore := c.natsClient.NewKVStore(bucket)
	c.agentHelper = agentgraph.NewHelper(kvStore)

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

	// Load project checklist so builder/tester prompts show the actual quality gates.
	checklistPath := filepath.Join(repoRoot, ".semspec", "checklist.json")
	if data, err := os.ReadFile(checklistPath); err == nil {
		var cl workflow.Checklist
		if err := json.Unmarshal(data, &cl); err == nil && len(cl.Checks) > 0 {
			c.checklist = cl.Checks
			c.logger.Info("Loaded project checklist for prompt injection", "checks", len(cl.Checks))
		}
	}

	c.logger.Info("Agent roster initialized")

	c.seedTeams()
}

// teamsEnabled reports whether team-based execution is active. Both the
// Enabled flag and a minimum of 2 roster entries (blue + red) are required.
func (c *Component) teamsEnabled() bool {
	return c.config.Teams != nil && c.config.Teams.Enabled && len(c.config.Teams.Roster) >= 2
}

// seedTeams creates team and agent entities in the graph for each roster entry.
// It is idempotent: CreateTeam and CreateAgent are no-ops when the entity
// already exists. Runs only when teamsEnabled() is true and agentHelper is
// available; logs and returns on any individual failure so a single bad entry
// does not abort the remaining roster.
func (c *Component) seedTeams() {
	if !c.teamsEnabled() {
		return
	}
	if c.agentHelper == nil {
		c.logger.Warn("Team seeding skipped — agent graph not available")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	for _, entry := range c.config.Teams.Roster {
		// Collect member IDs upfront so the team entity has correct MemberIDs
		// for team benching checks (checkTeamBenching iterates MemberIDs).
		var memberIDs []string
		for _, member := range entry.Members {
			memberIDs = append(memberIDs, entry.Name+"-"+member.Role)
		}

		team := &workflow.Team{
			ID:        entry.Name, // stable ID derived from name for idempotency
			Name:      entry.Name,
			Status:    workflow.TeamActive,
			MemberIDs: memberIDs,
		}
		if err := c.agentHelper.CreateTeam(ctx, team); err != nil {
			c.logger.Warn("seedTeams: failed to create team",
				"team", entry.Name, "error", err)
			continue
		}

		for _, member := range entry.Members {
			agentID := entry.Name + "-" + member.Role
			agent := workflow.Agent{
				ID:    agentID,
				Name:  agentID,
				Role:  member.Role,
				Model: member.Model,
			}
			if err := c.agentHelper.CreateAgent(ctx, agent); err != nil {
				c.logger.Warn("seedTeams: failed to create agent",
					"team", entry.Name, "role", member.Role, "error", err)
				continue
			}
			if err := c.agentHelper.SetAgentTeam(ctx, agentID, team.ID); err != nil {
				c.logger.Warn("seedTeams: failed to link agent to team",
					"agent", agentID, "team", team.ID, "error", err)
			}
		}

		c.logger.Info("seedTeams: seeded team",
			"team", entry.Name, "members", len(entry.Members))
	}
}

// selectReplacementAgent attempts to find a replacement agent after benching.
// First tries existing available agents, then walks the model fallback chain
// to create a new agent on a different model tier.
// Returns nil when all options are exhausted (caller should escalate).
func (c *Component) selectReplacementAgent(ctx context.Context, _ *taskExecution) *workflow.Agent {
	if c.agentHelper == nil {
		return nil
	}

	// Single roster query — derive both available agents and used models.
	roster, err := c.agentHelper.ListAgentsByRole(ctx, "developer")
	if err != nil {
		c.logger.Warn("Replacement agent selection failed", "error", err)
		return nil
	}

	// Try existing available agents (lowest errors first).
	var available []*workflow.Agent
	usedModels := make(map[string]bool, len(roster))
	for _, a := range roster {
		usedModels[a.Model] = true
		if a.Status == workflow.AgentAvailable {
			available = append(available, a)
		}
	}
	if len(available) > 0 {
		sort.Slice(available, func(i, j int) bool {
			ti := available[i].TotalErrorCount()
			tj := available[j].TotalErrorCount()
			if ti != tj {
				return ti < tj
			}
			return available[i].ReviewStats.OverallAvg > available[j].ReviewStats.OverallAvg
		})
		return available[0]
	}

	// No available agents — try creating one with the next model in fallback chain.
	if c.modelRegistry == nil {
		return nil
	}
	chain := c.modelRegistry.GetFallbackChainForRole("developer")
	for _, modelName := range chain {
		if usedModels[modelName] {
			continue
		}
		newAgent, createErr := c.agentHelper.SelectAgent(ctx, "developer", modelName)
		if createErr != nil {
			c.logger.Warn("Failed to create agent with fallback model",
				"model", modelName, "error", createErr)
			continue
		}
		return newAgent
	}

	return nil
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
			BlueTeamID:     triples[wf.BlueTeamID],
			WorktreePath:   triples[wf.WorktreePath],
			WorktreeBranch: triples[wf.WorktreeBranch],
			Stage:          phase,
		}
		if iter, ok := triples[wf.Iteration]; ok {
			fmt.Sscanf(iter, "%d", &state.Iteration)
		}
		if maxIter, ok := triples[wf.MaxIterations]; ok {
			fmt.Sscanf(maxIter, "%d", &state.MaxIterations)
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
			"iteration", exec.Iteration,
		)
	}

	if recovered > 0 {
		c.logger.Info("Reconciliation complete",
			"recovered", recovered,
			"total_entities", len(entities))
	}
}

// ---------------------------------------------------------------------------
// Trigger handler
// ---------------------------------------------------------------------------

func (c *Component) handleTrigger(ctx context.Context, msg jetstream.Msg) {
	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	trigger, err := payloads.ParseReactivePayload[workflow.TriggerPayload](msg.Data())
	if err != nil {
		c.logger.Error("Failed to parse execution trigger", "error", err)
		c.errors.Add(1)
		_ = msg.Nak()
		return
	}

	if trigger.Slug == "" || trigger.TaskID == "" {
		c.logger.Error("Trigger missing slug or task_id")
		c.errors.Add(1)
		_ = msg.Nak()
		return
	}

	exec := c.buildExecution(ctx, trigger)

	c.activeExecsMu.Lock()
	if _, exists := c.activeExecs.Get(exec.EntityID); exists {
		c.activeExecsMu.Unlock()
		c.logger.Debug("Duplicate trigger for active execution, skipping", "entity_id", exec.EntityID)
		_ = msg.Ack()
		return
	}
	c.activeExecs.Set(exec.EntityID, exec) //nolint:errcheck // cache set is best-effort
	c.activeExecsMu.Unlock()

	// Ack the trigger now that execution is registered and will make forward progress.
	_ = msg.Ack()

	c.writeInitialTriples(ctx, exec, trigger)
	c.maybeCreateWorktree(ctx, exec)

	// Select pipeline based on task type.
	initialPhase := c.initialPhaseForType(exec.TaskType)

	// Publish initial entity snapshot for graph observability.
	c.publishEntity(ctx, NewTaskExecutionEntity(exec).WithPhase(initialPhase))

	// Lock before timeout and dispatch to prevent race where timeout fires
	// before we finish initializing the execution.
	exec.mu.Lock()
	defer exec.mu.Unlock()
	c.startExecutionTimeout(exec)
	c.dispatchFirstStage(ctx, exec)
}

// buildExecution constructs a taskExecution from a trigger payload, resolving
// the persistent agent and team assignments.
func (c *Component) buildExecution(ctx context.Context, trigger *workflow.TriggerPayload) *taskExecution {
	entityID := workflow.TaskExecutionEntityID(trigger.Slug, trigger.TaskID)

	c.logger.Info("Task execution trigger received",
		"slug", trigger.Slug,
		"task_id", trigger.TaskID,
		"entity_id", entityID,
		"trace_id", trigger.TraceID,
	)

	exec := &taskExecution{
		key: workflow.TaskExecutionKey(trigger.Slug, trigger.TaskID),
		TaskExecution: &workflow.TaskExecution{
			EntityID:       entityID,
			Slug:           trigger.Slug,
			TaskID:         trigger.TaskID,
			Iteration:      0,
			MaxIterations:  c.config.MaxIterations,
			Title:          trigger.Title,
			Description:    trigger.Description,
			ProjectID:      trigger.ProjectID,
			Prompt:         trigger.Prompt,
			Model:          trigger.Model,
			TraceID:        trigger.TraceID,
			LoopID:         trigger.LoopID,
			RequestID:      trigger.RequestID,
			ScenarioBranch: trigger.ScenarioBranch,
			TaskType:       trigger.TaskType,
		},
		ContextRequestID: trigger.ContextRequestID,
	}

	// Resolve persistent agent for this execution (Phase B).
	if c.agentHelper != nil {
		agent, agentErr := c.agentHelper.SelectAgent(ctx, "developer", exec.Model)
		if agentErr != nil {
			c.logger.Warn("Agent selection failed, using trigger model", "error", agentErr)
		} else if agent != nil {
			exec.AgentID = agent.ID
			exec.Model = agent.Model
		}
	}

	// Team mode: select a blue team for this execution. No-op in solo mode.
	if c.teamsEnabled() && c.agentHelper != nil {
		blueTeam, blueErr := c.agentHelper.SelectBlueTeam(ctx)
		if blueErr != nil {
			c.logger.Warn("Blue team selection failed, proceeding without team assignment",
				"slug", trigger.Slug, "error", blueErr)
		} else if blueTeam != nil {
			exec.BlueTeamID = blueTeam.ID
			c.logger.Info("Blue team assigned",
				"slug", trigger.Slug,
				"task_id", trigger.TaskID,
				"blue_team", blueTeam.Name,
			)
		}
	}

	return exec
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

// writeInitialTriples writes the execution entity triples for durability and
// restart recovery. Called once immediately after the trigger is acked.
func (c *Component) writeInitialTriples(ctx context.Context, exec *taskExecution, trigger *workflow.TriggerPayload) {
	entityID := exec.EntityID
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.Type, "task-execution")
	if err := c.tripleWriter.WriteTriple(ctx, entityID, wf.Phase, phaseTesting); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseTesting, "error", err)
	}
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.Slug, trigger.Slug)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.TaskID, trigger.TaskID)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.Title, trigger.Title)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.ProjectID, trigger.ProjectID)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.Iteration, 0)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.MaxIterations, c.config.MaxIterations)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.TraceID, trigger.TraceID)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.Model, exec.Model)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.CurrentStage, phaseTesting)
	if exec.Prompt != "" {
		_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.Prompt, exec.Prompt)
	}
	if exec.AgentID != "" {
		_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.AgentID, exec.AgentID)
	}
	if exec.BlueTeamID != "" {
		_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.BlueTeamID, exec.BlueTeamID)
	}

	// Also write to EXECUTION_STATES KV for observability.
	exec.Stage = phaseTesting
	c.syncToStore(ctx, exec)
}

// maybeCreateWorktree creates a sandbox worktree when the sandbox is configured.
// Worktree path and branch are stored on exec and written as triples.
func (c *Component) maybeCreateWorktree(ctx context.Context, exec *taskExecution) {
	if c.sandbox == nil {
		return
	}
	var wtOpts []sandbox.WorktreeOption
	if exec.ScenarioBranch != "" {
		wtOpts = append(wtOpts, sandbox.WithBaseBranch(exec.ScenarioBranch))
	}
	wtInfo, wtErr := c.sandbox.CreateWorktree(ctx, exec.TaskID, wtOpts...)
	if wtErr != nil {
		c.logger.Error("Failed to create worktree, proceeding without sandbox isolation",
			"slug", exec.Slug,
			"task_id", exec.TaskID,
			"error", wtErr,
		)
		return
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
}

// ---------------------------------------------------------------------------
// Pipeline selection by task type
// ---------------------------------------------------------------------------

// initialPhaseForType returns the starting phase for a given task type.
func (c *Component) initialPhaseForType(taskType workflow.TaskType) string {
	switch taskType {
	case workflow.TaskTypeRefactor:
		return phaseBuilding // refactor skips tester — no new tests needed
	default:
		return phaseTesting // implement (default), test, document all start with tester
	}
}

// dispatchFirstStage dispatches the appropriate first agent based on task type.
// Called from handleTrigger after exec is initialized.
func (c *Component) dispatchFirstStage(ctx context.Context, exec *taskExecution) {
	switch exec.TaskType {
	case workflow.TaskTypeRefactor:
		// Refactor: skip tester, go straight to builder.
		c.dispatchBuilderLocked(ctx, exec)
	default:
		// implement (default): TDD pipeline starts with tester.
		c.dispatchTesterLocked(ctx, exec)
	}
}

// ---------------------------------------------------------------------------
// Loop-completion handler
// ---------------------------------------------------------------------------

func (c *Component) handleLoopCompleted(ctx context.Context, msg jetstream.Msg) {
	var base message.BaseMessage
	if err := json.Unmarshal(msg.Data(), &base); err != nil {
		c.logger.Debug("Failed to unmarshal loop completed envelope", "error", err)
		c.errors.Add(1)
		_ = msg.Nak()
		return
	}

	event, ok := base.Payload().(*agentic.LoopCompletedEvent)
	if !ok {
		_ = msg.Ack()
		return
	}

	// Route requirement-executor's direct agentic loops (decomposer, red-team,
	// reviewer) to EXECUTION_STATES KV. This replaces the ephemeral agent.complete.>
	// path with a durable KV write that requirement-executor's watcher detects.
	if event.WorkflowSlug == workflowSlugRequirementExecution {
		_ = msg.Ack()
		c.handleReqLoopCompleted(ctx, event)
		return
	}

	if event.WorkflowSlug != WorkflowSlugTaskExecution {
		_ = msg.Ack()
		return
	}

	c.updateLastActivity()

	entityID, ok := c.taskRouting.Get(event.TaskID)
	if !ok {
		c.logger.Debug("Loop completed for unknown task ID",
			"task_id", event.TaskID,
			"workflow_step", event.WorkflowStep,
		)
		_ = msg.Ack()
		return
	}
	_ = msg.Ack()

	exec, ok := c.activeExecs.Get(entityID)
	if !ok {
		c.logger.Debug("No active execution for entity", "entity_id", entityID)
		return
	}
	exec.mu.Lock()
	defer exec.mu.Unlock()

	if exec.terminated {
		return
	}

	c.logger.Info("Loop completion received",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"workflow_step", event.WorkflowStep,
		"iteration", exec.Iteration,
	)

	switch event.WorkflowStep {
	case stageTest:
		c.handleTesterCompleteLocked(ctx, event, exec)
	case stageBuild:
		c.handleBuilderCompleteLocked(ctx, event, exec)
	case stageValidate:
		// Validation is now synchronous (dispatched to structural-validator
		// component, not agentic loop). This case should not be reached.
		c.logger.Warn("Unexpected validate completion via agentic loop", "slug", exec.Slug)
	case stageRedTeam:
		c.handleRedTeamCompleteLocked(ctx, event, exec)
	case stageReview:
		c.handleReviewerCompleteLocked(ctx, event, exec)
	case stageDevelop:
		// Backward-compat: route legacy develop completions to the developer handler.
		c.handleDeveloperCompleteLocked(ctx, event, exec)
	default:
		c.logger.Debug("Unknown workflow step", "step", event.WorkflowStep, "entity_id", entityID)
	}
}

// ---------------------------------------------------------------------------
// Stage 1: Tester complete (TDD red phase — writes failing unit tests)
// ---------------------------------------------------------------------------

func (c *Component) handleTesterCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *taskExecution) {
	c.taskRouting.Delete(exec.TesterTaskID)

	var result payloads.DeveloperResult
	if err := json.Unmarshal([]byte(event.Result), &result); err != nil {
		c.logger.Warn("Failed to parse tester result", "slug", exec.Slug, "error", err)
	} else {
		exec.TesterOutput = result.Output
		exec.TestsPassed = false // tests are expected to fail at this stage (TDD red)
	}

	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseBuilding); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseBuilding, "error", err)
	}
	exec.Stage = phaseBuilding
	c.syncToStore(ctx, exec)

	c.logger.Info("Tester complete, dispatching builder",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"iteration", exec.Iteration,
	)

	c.dispatchBuilderLocked(ctx, exec)
}

// ---------------------------------------------------------------------------
// Stage 2: Builder complete (TDD green phase — implements code to pass tests)
// ---------------------------------------------------------------------------

func (c *Component) handleBuilderCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *taskExecution) {
	c.taskRouting.Delete(exec.BuilderTaskID)

	var result payloads.DeveloperResult
	if err := json.Unmarshal([]byte(event.Result), &result); err != nil {
		c.logger.Warn("Failed to parse builder result", "slug", exec.Slug, "error", err)
	} else {
		exec.FilesModified = result.FilesModified
		exec.BuilderOutput = result.Output
		exec.BuilderLLMRequestIDs = result.LLMRequestIDs
	}

	// Write builder output triples.
	if len(exec.FilesModified) > 0 {
		filesJSON, _ := json.Marshal(exec.FilesModified)
		_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.FilesModified, string(filesJSON))
	}
	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseValidating); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseValidating, "error", err)
	}
	exec.Stage = phaseValidating
	c.syncToStore(ctx, exec)

	c.logger.Info("Builder complete, dispatching validator",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"iteration", exec.Iteration,
	)

	c.dispatchValidatorLocked(ctx, exec)
}

// ---------------------------------------------------------------------------
// Stage 3 (compat): Developer complete — kept for backward compatibility
// ---------------------------------------------------------------------------

func (c *Component) handleDeveloperCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *taskExecution) {
	c.taskRouting.Delete(exec.DeveloperTaskID)

	var result payloads.DeveloperResult
	if err := json.Unmarshal([]byte(event.Result), &result); err != nil {
		c.logger.Warn("Failed to parse developer result", "slug", exec.Slug, "error", err)
	} else {
		exec.FilesModified = result.FilesModified
		exec.DeveloperOutput = result.Output
		exec.DeveloperLLMRequestIDs = result.LLMRequestIDs
	}

	// Write developer output triples.
	if len(exec.FilesModified) > 0 {
		filesJSON, _ := json.Marshal(exec.FilesModified)
		_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.FilesModified, string(filesJSON))
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
	c.taskRouting.Delete(exec.ReviewerTaskID)

	result := c.parseCodeReviewResult(event.Result, exec.Slug)

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
		"iteration", exec.Iteration,
	)

	// Team bookkeeping runs on BOTH approval and rejection so that red team
	// critique quality scores and shared knowledge accumulate regardless of verdict.
	if c.teamsEnabled() {
		c.runTeamBookkeeping(ctx, exec, result)
	}

	if result.Verdict == "approved" {
		c.markApprovedLocked(ctx, exec)
		return
	}

	c.handleRejectionLocked(ctx, exec, result)
}

// parseCodeReviewResult unmarshals the reviewer JSON result, defaulting to a
// safe rejected state on parse failure. Strips markdown code fences that some
// LLMs (notably Gemini) wrap around JSON tool responses.
func (c *Component) parseCodeReviewResult(raw string, slug string) payloads.TaskCodeReviewResult {
	var result payloads.TaskCodeReviewResult
	cleaned := stripMarkdownFences(raw)
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		c.logger.Warn("Failed to parse code review result, defaulting to rejected for safety",
			"slug", slug, "error", err)
		result.Verdict = "rejected"
		result.RejectionType = "fixable"
		result.Feedback = "parse failure — could not read reviewer response"
	}
	return result
}

// stripMarkdownFences removes markdown code fences (```json ... ``` or ``` ... ```)
// from LLM output that wraps JSON in formatting.
func stripMarkdownFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Remove opening fence (```json or ```)
	if idx := strings.Index(s, "\n"); idx != -1 {
		s = s[idx+1:]
	} else {
		return s
	}
	// Remove closing fence
	if idx := strings.LastIndex(s, "```"); idx != -1 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}

// runTeamBookkeeping updates red team accuracy stats and blue team error counts
// after a review verdict. Runs on both approval and rejection.
func (c *Component) runTeamBookkeeping(ctx context.Context, exec *taskExecution, result payloads.TaskCodeReviewResult) {
	c.extractTeamInsights(ctx, exec, result.Feedback)

	// Update red team stats atomically to avoid read-modify-write races.
	if exec.RedTeamID != "" && result.RedAccuracy > 0 {
		if err := c.agentHelper.UpdateTeamRedTeamStatsIncremental(ctx, exec.RedTeamID,
			result.RedAccuracy, result.RedThoroughness, result.RedFairness); err != nil {
			c.logger.Warn("Failed to update red team stats",
				"team_id", exec.RedTeamID, "error", err)
		}
	}
}

// handleRejectionLocked processes a rejected code review: writes the phase
// triple, runs benching checks, and routes the retry or escalation.
func (c *Component) handleRejectionLocked(ctx context.Context, exec *taskExecution, result payloads.TaskCodeReviewResult) {
	exec.Stage = phaseRejected
	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseRejected); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseRejected, "error", err)
	}

	agentBenched := c.checkAgentBenching(ctx, exec, result.Feedback)

	// Increment team error counts alongside agent error counts.
	if c.teamsEnabled() && exec.BlueTeamID != "" {
		c.maybeIncrementTeamErrors(ctx, exec.BlueTeamID, result.Feedback)
		c.checkTeamBenching(ctx, exec.BlueTeamID)
	}

	switch result.RejectionType {
	case rejectionTypeMisscoped, rejectionTypeArchitectural, rejectionTypeTooBig:
		c.markEscalatedLocked(ctx, exec, fmt.Sprintf("non-fixable rejection: %s", result.RejectionType))
	default:
		c.routeFixableRejection(ctx, exec, result.Feedback, agentBenched)
	}
}

// maybeIncrementTeamErrors classifies feedback and increments team error counts
// when signal matches are found.
func (c *Component) maybeIncrementTeamErrors(ctx context.Context, blueTeamID, feedback string) {
	if c.errorCategories == nil || feedback == "" {
		return
	}
	matches := c.errorCategories.MatchSignals(feedback)
	var cats []workflow.ErrorCategory
	for _, m := range matches {
		cats = append(cats, m.Category.ID)
	}
	if len(cats) > 0 {
		if err := c.agentHelper.IncrementTeamErrorCounts(ctx, blueTeamID, cats); err != nil {
			c.logger.Warn("Failed to increment team error counts",
				"team_id", blueTeamID, "error", err)
		}
	}
}

// routeFixableRejection handles a fixable rejection: swaps in a replacement
// agent if the current one was benched, then retries or escalates on budget.
func (c *Component) routeFixableRejection(ctx context.Context, exec *taskExecution, feedback string, agentBenched bool) {
	if agentBenched {
		replacement := c.selectReplacementAgent(ctx, exec)
		if replacement == nil {
			c.markEscalatedLocked(ctx, exec, "all agents benched, no fallback models available")
			return
		}
		exec.AgentID = replacement.ID
		exec.Model = replacement.Model
		c.logger.Info("Replacement agent selected after benching",
			"new_agent_id", replacement.ID,
			"new_model", replacement.Model,
			"slug", exec.Slug,
		)
	}
	if exec.Iteration+1 < exec.MaxIterations {
		if c.feedbackNeedsTestRetry(feedback) {
			c.startTesterRetryLocked(ctx, exec, feedback)
		} else {
			c.startBuilderRetryLocked(ctx, exec, feedback)
		}
	} else {
		c.markEscalatedLocked(ctx, exec, "fixable rejections exceeded iteration budget")
	}
}

// checkAgentBenching classifies the rejection feedback into error categories,
// increments the agent's error counts, and benches the agent if the threshold
// is reached. Returns true if the agent was benched by this call.
func (c *Component) checkAgentBenching(ctx context.Context, exec *taskExecution, feedback string) bool {
	if c.agentHelper == nil || exec.AgentID == "" {
		return false
	}

	// Auto-classify feedback into error categories via signal matching.
	if c.errorCategories != nil && feedback != "" {
		matches := c.errorCategories.MatchSignals(feedback)
		var categoryIDs []string
		for _, m := range matches {
			categoryIDs = append(categoryIDs, m.Category.ID)
		}
		if len(categoryIDs) > 0 {
			if err := c.agentHelper.IncrementAgentErrorCounts(ctx, exec.AgentID, categoryIDs); err != nil {
				c.logger.Warn("Failed to increment agent error counts",
					"agent_id", exec.AgentID, "error", err)
			}
		}
	}

	// Check if the agent should be benched.
	benched, err := c.agentHelper.BenchAgent(ctx, exec.AgentID, c.config.BenchingThreshold)
	if err != nil {
		c.logger.Warn("Benching check failed", "agent_id", exec.AgentID, "error", err)
		return false
	}
	if benched {
		c.logger.Info("Agent benched due to error threshold",
			"agent_id", exec.AgentID,
			"threshold", c.config.BenchingThreshold,
			"slug", exec.Slug,
		)
	}
	return benched
}

// ---------------------------------------------------------------------------
// Terminal state handlers
// ---------------------------------------------------------------------------

// markApprovedLocked transitions to the approved terminal state.
// Caller must hold exec.mu.
func (c *Component) markApprovedLocked(ctx context.Context, exec *taskExecution) {
	if exec.terminated {
		return
	}
	exec.terminated = true

	// Merge worktree back to main branch before marking approved.
	c.mergeWorktree(exec)

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
		"iteration", exec.Iteration,
	)

	// Notify callers (e.g. scenario-executor) that the TDD pipeline completed.
	// Safe against self-receive: the completion event uses exec.TaskID (external),
	// which is not stored in our taskRouting cache (only internal pipeline task IDs are).
	c.publishCompletionEvent(ctx, exec, agentic.OutcomeSuccess, "")

	// Relay to RequirementExecution KV for durable watcher delivery.
	c.relayNodeCompletionToReq(ctx, exec, agentic.OutcomeSuccess, "")

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
		"iteration", exec.Iteration,
		"reason", reason,
	)

	// Notify callers that the TDD pipeline escalated (treated as failure).
	c.publishCompletionEvent(ctx, exec, agentic.OutcomeFailed, reason)
	c.relayNodeCompletionToReq(ctx, exec, agentic.OutcomeFailed, reason)

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
	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseError); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseError, "error", err)
	}
	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.ErrorReason, reason)
	c.syncToStore(ctx, exec)

	c.errors.Add(1)
	c.executionsCompleted.Add(1)

	c.logger.Error("Task execution failed",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"reason", reason,
	)

	// Notify callers that the TDD pipeline errored.
	c.publishCompletionEvent(ctx, exec, agentic.OutcomeFailed, reason)
	c.relayNodeCompletionToReq(ctx, exec, agentic.OutcomeFailed, reason)

	c.publishEntity(context.Background(), NewTaskExecutionEntity(exec).WithPhase(phaseError).WithErrorReason(reason))
	c.cleanupExecutionLocked(exec)
}

// cleanupExecutionLocked removes execution from maps and cancels timeout.
// Caller must hold exec.mu.
func (c *Component) cleanupExecutionLocked(exec *taskExecution) {
	if exec.timeoutTimer != nil {
		exec.timeoutTimer.stop()
	}
	c.taskRouting.Delete(exec.TesterTaskID)
	c.taskRouting.Delete(exec.BuilderTaskID)
	c.taskRouting.Delete(exec.DeveloperTaskID)
	c.taskRouting.Delete(exec.ValidatorTaskID)
	c.taskRouting.Delete(exec.ReviewerTaskID)
	c.activeExecs.Delete(exec.EntityID) //nolint:errcheck // cache delete is best-effort
}

// ---------------------------------------------------------------------------
// Retry logic
// ---------------------------------------------------------------------------

// feedbackNeedsTestRetry returns true when the reviewer feedback signals that
// the problem is missing or insufficient tests rather than an implementation bug.
// When true, the tester (not builder) is retried so new tests are written first.
func (c *Component) feedbackNeedsTestRetry(feedback string) bool {
	if c.errorCategories == nil || feedback == "" {
		return false
	}
	matches := c.errorCategories.MatchSignals(feedback)
	for _, m := range matches {
		if m.Category.ID == errorCategoryMissingTests || m.Category.ID == errorCategoryEdgeCaseMissed {
			return true
		}
	}
	return false
}

// startTesterRetryLocked resets tester and all downstream fields, then
// re-dispatches the tester so the TDD cycle restarts from the test phase.
// Caller must hold exec.mu.
func (c *Component) startTesterRetryLocked(ctx context.Context, exec *taskExecution, feedback string) {
	exec.Iteration++
	// Clear tester state.
	exec.TesterOutput = nil
	exec.TestsPassed = false
	// Clear builder state.
	exec.BuilderOutput = nil
	exec.BuilderLLMRequestIDs = nil
	exec.FilesModified = nil
	// Clear developer compat state.
	exec.DeveloperOutput = nil
	exec.DeveloperLLMRequestIDs = nil
	// Clear downstream state.
	exec.ValidationPassed = false
	exec.ValidationResults = nil
	exec.Verdict = ""
	exec.RejectionType = ""
	exec.ReviewerLLMRequestIDs = nil
	exec.Feedback = feedback

	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Iteration, exec.Iteration)
	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseTesting); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseTesting, "error", err)
	}
	exec.Stage = phaseTesting
	c.syncToStore(ctx, exec)

	c.logger.Info("Retrying tester (test coverage feedback)",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"new_iteration", exec.Iteration,
	)

	c.dispatchTesterLocked(ctx, exec)
}

// startBuilderRetryLocked increments iteration and re-dispatches the builder.
// Tests written in a prior tester pass are preserved (TesterOutput is NOT reset)
// so the builder retry has the same test suite to work against.
// Caller must hold exec.mu.
func (c *Component) startBuilderRetryLocked(ctx context.Context, exec *taskExecution, feedback string) {
	exec.Iteration++
	// Clear builder state — tests persist.
	exec.BuilderOutput = nil
	exec.BuilderLLMRequestIDs = nil
	exec.FilesModified = nil
	// Clear developer compat state.
	exec.DeveloperOutput = nil
	exec.DeveloperLLMRequestIDs = nil
	// Clear downstream state.
	exec.ValidationPassed = false
	exec.ValidationResults = nil
	exec.Verdict = ""
	exec.RejectionType = ""
	exec.ReviewerLLMRequestIDs = nil
	// Enrich feedback with worktree file listing so the retrying builder
	// knows what files already exist from prior iterations.
	if c.sandbox != nil && exec.WorktreePath != "" {
		files, err := c.sandbox.ListWorktreeFiles(ctx, exec.TaskID)
		if err != nil {
			c.logger.Warn("Failed to list worktree files for builder retry prompt",
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

	exec.Feedback = feedback

	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Iteration, exec.Iteration)
	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseBuilding); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseBuilding, "error", err)
	}
	exec.Stage = phaseBuilding
	c.syncToStore(ctx, exec)

	c.logger.Info("Retrying builder",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"new_iteration", exec.Iteration,
	)

	c.dispatchBuilderLocked(ctx, exec)
}

// startDeveloperRetryLocked increments iteration and re-dispatches the developer.
// Caller must hold exec.mu.
func (c *Component) startDeveloperRetryLocked(ctx context.Context, exec *taskExecution, feedback string) {
	exec.Iteration++
	exec.FilesModified = nil
	exec.DeveloperOutput = nil
	exec.DeveloperLLMRequestIDs = nil
	exec.ValidationPassed = false
	exec.ValidationResults = nil
	exec.Verdict = ""
	exec.RejectionType = ""
	exec.ReviewerLLMRequestIDs = nil
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

	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Iteration, exec.Iteration)
	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseDeveloping); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseDeveloping, "error", err)
	}
	exec.Stage = phaseDeveloping
	c.syncToStore(ctx, exec)

	c.logger.Info("Retrying developer",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"new_iteration", exec.Iteration,
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
func (c *Component) buildAssemblyContext(role prompt.Role, exec *taskExecution) *prompt.AssemblyContext {
	asmCtx := &prompt.AssemblyContext{
		Role:           role,
		Provider:       resolveProvider(exec.Model),
		Domain:         "software",
		AvailableTools: prompt.FilterTools(c.availableToolNames(), role),
		SupportsTools:  true,
	}

	// Wire task context for execution roles.
	if role == prompt.RoleBuilder || role == prompt.RoleTester || role == prompt.RoleDeveloper ||
		role == prompt.RoleValidator || role == prompt.RoleReviewer {
		asmCtx.TaskContext = &prompt.TaskContext{
			PlanGoal:      exec.Title,
			IsRetry:       exec.Iteration > 0,
			Feedback:      exec.Feedback,
			Iteration:     exec.Iteration + 1, // 1-based for display
			MaxIterations: exec.MaxIterations,
			Checklist:     c.checklist,
		}
	}

	// Wire team knowledge when teams are enabled.
	if c.teamsEnabled() && exec.BlueTeamID != "" && c.agentHelper != nil {
		teamID := exec.BlueTeamID
		team, err := c.agentHelper.GetTeam(context.Background(), teamID)
		if err == nil && team != nil {
			// Map role to skill + category filters matching the old dispatch logic.
			skill := string(role)
			insights := team.FilterInsights(skill, nil, 10)
			if len(insights) > 0 {
				tk := &prompt.TeamKnowledge{TeamID: teamID}
				for _, ins := range insights {
					tk.Lessons = append(tk.Lessons, prompt.TeamLesson{
						Category: ins.Source,
						Summary:  ins.Summary,
						Role:     skill,
					})
				}
				asmCtx.TeamKnowledge = tk
			}
		}
	}

	return asmCtx
}

// availableToolNames returns the full list of tool names registered in the system.
// This is a best-effort list for prompt assembly — actual tool availability is
// controlled by the agentic-tools component at runtime.
func (c *Component) availableToolNames() []string {
	return []string{
		"bash", "submit_work", "submit_review", "ask_question",
		"graph_search", "graph_query", "graph_summary",
		"web_search", "http_request",
		"decompose_task", "spawn_agent",
		"review_scenario",
	}
}

// ---------------------------------------------------------------------------
// Agent dispatch: Tester (Stage 1 — writes failing unit tests)
// ---------------------------------------------------------------------------

func (c *Component) dispatchTesterLocked(ctx context.Context, exec *taskExecution) {
	taskID := fmt.Sprintf("test-%s-%s", exec.EntityID, uuid.New().String())
	exec.TesterTaskID = taskID
	c.taskRouting.Set(taskID, exec.EntityID)

	// Assemble system prompt via fragment pipeline.
	asmCtx := c.buildAssemblyContext(prompt.RoleTester, exec)
	assembled := c.assembler.Assemble(asmCtx)

	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleGeneral,
		Model:        exec.Model,
		Prompt:       exec.Prompt,
		WorkflowSlug: WorkflowSlugTaskExecution,
		WorkflowStep: stageTest,
		ToolChoice:   prompt.ResolveToolChoice(prompt.RoleTester, asmCtx.AvailableTools),
		Context: &agentic.ConstructedContext{
			Content: assembled.SystemMessage,
		},
		Metadata: map[string]any{
			"plan_slug": exec.Slug,
			"task_id":   exec.TaskID,
		},
	}
	c.publishTask(ctx, subjectTesterTask, task)

	c.logger.Info("Dispatched tester",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"iteration", exec.Iteration,
		"tester_task_id", taskID,
		"fragments", len(assembled.FragmentsUsed),
	)
}

// ---------------------------------------------------------------------------
// Agent dispatch: Builder (Stage 2 — implements code to pass tests)
// ---------------------------------------------------------------------------

func (c *Component) dispatchBuilderLocked(ctx context.Context, exec *taskExecution) {
	taskID := fmt.Sprintf("build-%s-%s", exec.EntityID, uuid.New().String())
	exec.BuilderTaskID = taskID
	c.taskRouting.Set(taskID, exec.EntityID)

	// Assemble system prompt via fragment pipeline.
	asmCtx := c.buildAssemblyContext(prompt.RoleBuilder, exec)
	assembled := c.assembler.Assemble(asmCtx)

	// Builder user prompt: original task context + instruction to make tests pass.
	const builderSuffix = "\n\n---\n\nFailing tests have been written. Your job is to implement code that makes them pass."
	userPrompt := exec.Prompt + builderSuffix
	if exec.Iteration > 0 && exec.Feedback != "" {
		userPrompt += "\n\n---\n\nREVISION REQUEST: Your previous implementation was rejected.\n\n" + exec.Feedback
	}

	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleGeneral,
		Model:        exec.Model,
		WorkflowSlug: WorkflowSlugTaskExecution,
		WorkflowStep: stageBuild,
		Prompt:       userPrompt,
		ToolChoice:   prompt.ResolveToolChoice(prompt.RoleBuilder, asmCtx.AvailableTools),
		Context: &agentic.ConstructedContext{
			Content: assembled.SystemMessage,
		},
		Metadata: map[string]any{
			"plan_slug": exec.Slug,
			"task_id":   exec.TaskID,
		},
	}
	c.publishTask(ctx, subjectBuilderTask, task)

	c.logger.Info("Dispatched builder",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"iteration", exec.Iteration,
		"builder_task_id", taskID,
		"fragments", len(assembled.FragmentsUsed),
	)
}

// ---------------------------------------------------------------------------
// Agent dispatch: Developer (kept for backward compatibility)
// ---------------------------------------------------------------------------

func (c *Component) dispatchDeveloperLocked(ctx context.Context, exec *taskExecution) {
	taskID := fmt.Sprintf("dev-%s-%s", exec.EntityID, uuid.New().String())
	exec.DeveloperTaskID = taskID
	c.taskRouting.Set(taskID, exec.EntityID)

	// Assemble system prompt via fragment pipeline.
	asmCtx := c.buildAssemblyContext(prompt.RoleDeveloper, exec)
	assembled := c.assembler.Assemble(asmCtx)

	userPrompt := exec.Prompt
	if exec.Iteration > 0 && exec.Feedback != "" {
		userPrompt += "\n\n---\n\nREVISION REQUEST: Your previous implementation was rejected.\n\n" + exec.Feedback
	}

	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleGeneral,
		Model:        exec.Model,
		WorkflowSlug: WorkflowSlugTaskExecution,
		WorkflowStep: stageDevelop,
		Prompt:       userPrompt,
		ToolChoice:   prompt.ResolveToolChoice(prompt.RoleDeveloper, asmCtx.AvailableTools),
		Context: &agentic.ConstructedContext{
			Content: assembled.SystemMessage,
		},
		Metadata: map[string]any{
			"plan_slug": exec.Slug,
			"task_id":   exec.TaskID,
		},
	}
	c.publishTask(ctx, "agent.task.development", task)

	c.logger.Info("Dispatched developer",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"iteration", exec.Iteration,
		"developer_task_id", taskID,
		"fragments", len(assembled.FragmentsUsed),
	)
}

// ---------------------------------------------------------------------------
// Agent dispatch: Structural Validator
// ---------------------------------------------------------------------------

func (c *Component) dispatchValidatorLocked(ctx context.Context, exec *taskExecution) {
	c.logger.Info("Dispatching structural validation",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"iteration", exec.Iteration,
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

		if exec.Iteration+1 < exec.MaxIterations {
			feedback, _ := json.Marshal(exec.ValidationResults)
			c.startBuilderRetryLocked(ctx, exec, "Structural validation failed. Fix the following issues:\n"+string(feedback))
		} else {
			c.markEscalatedLocked(ctx, exec, "validation failures exceeded iteration budget")
		}
		return
	}

	c.logger.Info("Validation passed, dispatching reviewer",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"iteration", exec.Iteration,
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
// Agent dispatch: Red Team (team-mode only — adversarial challenge before review)
// ---------------------------------------------------------------------------

// dispatchRedTeamLocked selects the red team and publishes a challenge task.
// If no red team is available the function falls back to dispatchReviewerLocked
// so the pipeline always makes forward progress.
// Caller must hold exec.mu.
func (c *Component) dispatchRedTeamLocked(ctx context.Context, exec *taskExecution) {
	redTeam, err := c.agentHelper.SelectRedTeam(ctx, exec.BlueTeamID)
	if err != nil || redTeam == nil {
		c.logger.Warn("No red team available, falling back to direct reviewer",
			"slug", exec.Slug,
			"task_id", exec.TaskID,
			"error", err,
		)
		// Graceful fallback: skip red team, go straight to reviewer.
		if wtErr := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseReviewing); wtErr != nil {
			c.logger.Error("Failed to write phase triple", "phase", phaseReviewing, "error", wtErr)
		}
		exec.Stage = phaseReviewing
		c.syncToStore(ctx, exec)
		c.dispatchReviewerLocked(ctx, exec)
		return
	}

	exec.RedTeamID = redTeam.ID

	// Pre-build the red team knowledge block and store on exec for lineage.
	if kb := c.buildTeamKnowledgeBlock(ctx, redTeam.ID, "red-team", nil); kb != "" {
		exec.RedTeamKnowledge = kb
	}

	taskID := fmt.Sprintf("red-%s-%s", exec.EntityID, uuid.New().String())
	exec.RedTeamTaskID = taskID
	c.taskRouting.Set(taskID, exec.EntityID)

	// Assemble system prompt via fragment pipeline.
	asmCtx := c.buildAssemblyContext(prompt.RoleReviewer, exec)
	asmCtx.RedTeamContext = &prompt.RedTeamContext{
		BlueTeamFiles:   exec.FilesModified,
		BlueTeamSummary: string(exec.BuilderOutput),
	}
	assembled := c.assembler.Assemble(asmCtx)

	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleDeveloper,
		Model:        exec.Model,
		WorkflowSlug: WorkflowSlugTaskExecution,
		WorkflowStep: stageRedTeam,
		Prompt:       exec.Prompt,
		ToolChoice:   prompt.ResolveToolChoice(prompt.RoleReviewer, asmCtx.AvailableTools),
		Context: &agentic.ConstructedContext{
			Content: assembled.SystemMessage,
		},
		Metadata: map[string]any{
			"plan_slug": exec.Slug,
			"task_id":   exec.TaskID,
		},
	}
	c.publishTask(ctx, subjectRedTeamTask, task)

	c.logger.Info("Dispatched red team challenge",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"iteration", exec.Iteration,
		"red_team", redTeam.Name,
		"red_team_task_id", taskID,
		"fragments", len(assembled.FragmentsUsed),
	)
}

// ---------------------------------------------------------------------------
// Stage: Red Team complete
// ---------------------------------------------------------------------------

// handleRedTeamCompleteLocked processes the red team challenge result and
// transitions to the reviewer stage. Parse failures are tolerated — the
// reviewer still runs, just without the red-team input.
// Caller must hold exec.mu.
func (c *Component) handleRedTeamCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *taskExecution) {
	c.taskRouting.Delete(exec.RedTeamTaskID)

	var challenge payloads.RedTeamChallengeResult
	if err := json.Unmarshal([]byte(event.Result), &challenge); err != nil {
		c.logger.Warn("Failed to parse red team challenge result, proceeding to reviewer",
			"slug", exec.Slug,
			"task_id", exec.TaskID,
			"error", err,
		)
		// Continue without red-team input rather than blocking the pipeline.
	} else {
		exec.RedTeamChallenge = &challenge
	}

	issueCount := 0
	testCount := 0
	if exec.RedTeamChallenge != nil {
		issueCount = len(exec.RedTeamChallenge.Issues)
		testCount = len(exec.RedTeamChallenge.TestFiles)
	}

	c.logger.Info("Red team challenge complete, dispatching reviewer",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"iteration", exec.Iteration,
		"issues_found", issueCount,
		"tests_written", testCount,
	)

	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseReviewing); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseReviewing, "error", err)
	}
	exec.Stage = phaseReviewing
	c.syncToStore(ctx, exec)
	c.dispatchReviewerLocked(ctx, exec)
}

// ---------------------------------------------------------------------------
// Agent dispatch: Code Reviewer
// ---------------------------------------------------------------------------

func (c *Component) dispatchReviewerLocked(ctx context.Context, exec *taskExecution) {
	taskID := fmt.Sprintf("rev-%s-%s", exec.EntityID, uuid.New().String())
	exec.ReviewerTaskID = taskID
	c.taskRouting.Set(taskID, exec.EntityID)

	// Assemble system prompt via fragment pipeline.
	asmCtx := c.buildAssemblyContext(prompt.RoleReviewer, exec)

	// Wire red team context if available.
	if exec.RedTeamChallenge != nil {
		asmCtx.RedTeamContext = &prompt.RedTeamContext{
			BlueTeamFiles:   exec.FilesModified,
			BlueTeamSummary: string(exec.BuilderOutput),
		}
	}

	assembled := c.assembler.Assemble(asmCtx)

	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleReviewer,
		Model:        exec.Model,
		WorkflowSlug: WorkflowSlugTaskExecution,
		WorkflowStep: stageReview,
		Prompt:       exec.Prompt,
		ToolChoice:   prompt.ResolveToolChoice(prompt.RoleReviewer, asmCtx.AvailableTools),
		Context: &agentic.ConstructedContext{
			Content: assembled.SystemMessage,
		},
		Metadata: map[string]any{
			"plan_slug": exec.Slug,
			"task_id":   exec.TaskID,
		},
	}
	c.publishTask(ctx, "agent.task.reviewer", task)

	c.logger.Info("Dispatched code reviewer",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"iteration", exec.Iteration,
		"fragments", len(assembled.FragmentsUsed),
	)
}

// ---------------------------------------------------------------------------
// Worktree lifecycle helpers
// ---------------------------------------------------------------------------

// mergeWorktree merges the worktree for the given execution back into its
// scenario branch (if set) or the current HEAD branch. Merge metadata
// (commit hash, files changed) is captured for lineage tracking.
// Best-effort: failures are logged and recorded as a triple but never block
// the terminal state transition. Caller must hold exec.mu.
func (c *Component) mergeWorktree(exec *taskExecution) {
	if c.sandbox == nil || exec.WorktreePath == "" {
		return
	}

	var opts []sandbox.MergeOption
	if exec.ScenarioBranch != "" {
		opts = append(opts, sandbox.WithTargetBranch(exec.ScenarioBranch))
	}
	opts = append(opts, sandbox.WithCommitMessage(fmt.Sprintf("feat(%s): %s", exec.Slug, exec.TaskID)))
	opts = append(opts, sandbox.WithTrailer("Task-ID", exec.TaskID))
	opts = append(opts, sandbox.WithTrailer("Plan-Slug", exec.Slug))
	if exec.TraceID != "" {
		opts = append(opts, sandbox.WithTrailer("Trace-ID", exec.TraceID))
	}

	result, err := c.sandbox.MergeWorktree(context.Background(), exec.TaskID, opts...)
	if err != nil {
		c.logger.Warn("Failed to merge worktree; changes may need manual merge",
			"slug", exec.Slug,
			"task_id", exec.TaskID,
			"error", err,
		)
		_ = c.tripleWriter.WriteTriple(context.Background(), exec.EntityID, wf.ErrorReason,
			fmt.Sprintf("worktree merge failed: %v", err))
	} else {
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
	}
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

// handleReqLoopCompleted routes a requirement-executor agentic loop completion
// (decomposer, red-team, reviewer) into the EXECUTION_STATES KV bucket. This
// replaces the ephemeral agent.complete.> path: the KV write triggers
// requirement-executor's watcher, providing durable delivery with replay.
func (c *Component) handleReqLoopCompleted(ctx context.Context, event *agentic.LoopCompletedEvent) {
	c.updateLastActivity()

	if c.store == nil {
		return
	}
	key, exec, ok := c.store.reqKeyByTaskID(event.TaskID)
	if !ok {
		c.logger.Debug("Req loop completed for unknown task ID",
			"task_id", event.TaskID,
			"workflow_step", event.WorkflowStep,
		)
		return
	}

	// Write completion data to the RequirementExecution KV entry.
	// The KV write IS the event — requirement-executor's watcher picks it up.
	exec.LastCompletionTaskID = event.TaskID
	exec.LastCompletionStep = event.WorkflowStep
	exec.LastCompletionResult = event.Result
	exec.LastCompletionOutcome = event.Outcome

	if err := c.store.saveReq(ctx, key, exec); err != nil {
		c.logger.Warn("Failed to save req loop completion to KV",
			"key", key,
			"task_id", event.TaskID,
			"error", err,
		)
		return
	}

	c.logger.Info("Req loop completion relayed to KV",
		"key", key,
		"task_id", event.TaskID,
		"workflow_step", event.WorkflowStep,
		"outcome", event.Outcome,
	)
}

// relayNodeCompletionToReq writes TDD node completion data to the parent
// RequirementExecution KV entry. exec.TaskID is the node task ID assigned by
// requirement-executor; reqKeyByTaskID matches it against CurrentNodeTaskID.
func (c *Component) relayNodeCompletionToReq(ctx context.Context, exec *taskExecution, outcome, result string) {
	if c.store == nil {
		return
	}
	key, reqExec, ok := c.store.reqKeyByTaskID(exec.TaskID)
	if !ok {
		return // no matching requirement — this task wasn't dispatched by requirement-executor
	}

	reqExec.LastCompletionTaskID = exec.TaskID
	reqExec.LastCompletionStep = exec.TaskID // node completions use taskID as step
	reqExec.LastCompletionResult = result
	reqExec.LastCompletionOutcome = outcome

	if err := c.store.saveReq(ctx, key, reqExec); err != nil {
		c.logger.Warn("Failed to relay node completion to req KV",
			"key", key, "task_id", exec.TaskID, "error", err)
	}
}

// ---------------------------------------------------------------------------
// Triple and task publishing helpers
// ---------------------------------------------------------------------------

// publishCompletionEvent emits a LoopCompletedEvent when the TDD pipeline
// reaches a terminal state. This notifies callers (e.g. scenario-executor)
// that the dispatched task execution is finished.
// Caller must hold exec.mu.
func (c *Component) publishCompletionEvent(ctx context.Context, exec *taskExecution, outcome, result string) {
	loopID := exec.LoopID
	if loopID == "" {
		loopID = exec.TaskID // Use TaskID as LoopID when not set by trigger.
	}

	event := &agentic.LoopCompletedEvent{
		LoopID:       loopID,
		TaskID:       exec.TaskID,
		Outcome:      outcome,
		Role:         string(agentic.RoleDeveloper),
		Result:       result,
		WorkflowSlug: WorkflowSlugTaskExecution,
		WorkflowStep: exec.TaskID,
		CompletedAt:  time.Now(),
		Iterations:   exec.Iteration + 1,
	}

	baseMsg := message.NewBaseMessage(event.Schema(), event, componentName)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Warn("Failed to marshal completion event", "error", err)
		return
	}

	if c.natsClient != nil {
		publishSubject := fmt.Sprintf("agent.complete.%s", loopID)
		if err := c.natsClient.PublishToStream(ctx, publishSubject, data); err != nil {
			c.logger.Warn("Failed to publish completion event",
				"subject", publishSubject,
				"task_id", exec.TaskID,
				"error", err,
			)
		}
	}

	c.logger.Info("Published TDD pipeline completion event",
		"task_id", exec.TaskID,
		"outcome", outcome,
		"slug", exec.Slug,
	)
}

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
		Description: "Orchestrates TDD task execution pipeline: tester → builder → validator → reviewer with retry and escalation",
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
