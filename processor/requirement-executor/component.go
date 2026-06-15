// Package requirementexecutor provides a component that orchestrates per-requirement
// execution using a serial-first strategy.
//
// It replaces the reactive scenario-execution-loop (7 rules) and dag-execution-loop
// (6 rules) with a single component that decomposes a requirement into a DAG, then
// executes nodes serially in topological order.
//
// Execution flow:
//  1. Receive trigger from scenario-orchestrator (RequirementExecutionRequest)
//  2. Dispatch decomposer agent → get validated TaskDAG
//  3. Topological sort → linear execution order
//  4. Execute each node serially as an agent task
//  5. All nodes done → review all scenarios → completed; any failure → failed
//
// Terminal status transitions (completed, failed) are owned by the JSON rule
// processor, not this component. This component writes workflow.phase; rules
// react and set workflow.status + publish events.
package requirementexecutor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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
	sscache "github.com/c360studio/semstreams/pkg/cache"
	"github.com/google/uuid"
)

const (
	componentName    = "requirement-executor"
	componentVersion = "0.1.0"

	// WorkflowSlugRequirementExecution identifies requirement events in LoopCompletedEvent.
	WorkflowSlugRequirementExecution = "semspec-requirement-execution"

	// Pipeline stage constants used as WorkflowStep in TaskMessages.
	stageDecompose         = "decompose"
	stageRequirementReview = "requirement-review"

	// Phase values written to entity triples.
	phaseDecomposing      = "decomposing"
	phaseExecuting        = "executing"
	phaseCompleted        = "completed"
	phaseFailed           = "failed"
	phaseError            = "error"
	phaseReviewing        = "reviewing"
	phaseAwaitingRecovery = "awaiting-recovery" // ADR-037 race-closure interim state — not terminal

	// NATS subjects.
	subjectDecomposer = "agent.task.development"
)

// sandboxClient is the narrow interface requirement-executor uses over the
// sandbox API. Declared as an interface so tests can inject a stub and verify
// which worktree/branch lifecycle calls the orchestrator makes. Satisfied by
// *sandbox.Client.
type sandboxClient interface {
	CreateWorktree(ctx context.Context, taskID string, opts ...sandbox.WorktreeOption) (*sandbox.WorktreeInfo, error)
	DeleteWorktree(ctx context.Context, taskID string) error
	CreateBranch(ctx context.Context, branch, baseRef string) error
	DeleteBranch(ctx context.Context, branch string) error
}

type requirementReviewResult struct {
	Verdict          string                          `json:"verdict"`
	RejectionType    string                          `json:"rejection_type"` // "fixable" or "restructure"
	Feedback         string                          `json:"feedback"`
	ScenarioVerdicts []requirementReviewScenarioItem `json:"scenario_verdicts"`
}

type requirementReviewScenarioItem struct {
	ScenarioID string `json:"scenario_id"`
	Passed     *bool  `json:"passed"`
	Feedback   string `json:"feedback,omitempty"`
}

func (r requirementReviewResult) toScenarioVerdicts() []ScenarioVerdict {
	out := make([]ScenarioVerdict, 0, len(r.ScenarioVerdicts))
	for _, sv := range r.ScenarioVerdicts {
		passed := false
		if sv.Passed != nil {
			passed = *sv.Passed
		}
		out = append(out, ScenarioVerdict{
			ScenarioID: sv.ScenarioID,
			Passed:     passed,
			Feedback:   sv.Feedback,
		})
	}
	return out
}

// newSandboxClient returns a sandboxClient backed by the real sandbox HTTP
// client, or an untyped nil interface when url is empty. Using a constructor
// avoids the Go nil-interface gotcha where a typed nil (*sandbox.Client)(nil)
// assigned to the interface field appears non-nil.
func newSandboxClient(url string) sandboxClient {
	c := sandbox.NewClient(url)
	if c == nil {
		return nil
	}
	return c
}

// storyStatusClaimerFunc is the function signature for atomically transitioning
// a Story's status via plan-manager. The default implementation calls
// workflow.ClaimStoryStatus; tests inject a fake that enforces CanTransitionTo
// so the Executing→Failed→Pending→Ready walk can be asserted without a live
// NATS substrate. See reopenOwnedStoriesForRecoveryLocked.
type storyStatusClaimerFunc func(ctx context.Context, slug, storyID string, target workflow.StoryStatus) bool

// Component orchestrates per-requirement execution.
type Component struct {
	config        Config
	natsClient    *natsclient.Client
	logger        *slog.Logger
	platform      component.PlatformMeta
	toolRegistry  component.ToolRegistryReader
	modelRegistry ssmodel.RegistryReader
	tripleWriter  *graphutil.TripleWriter
	sandbox       sandboxClient     // nil when sandbox is disabled
	assembler     *prompt.Assembler // composes system prompts for requirement-level review

	// storyStatusClaimer is the seam for story-status transitions used by
	// reopenOwnedStoriesForRecoveryLocked. Defaults to a wrapper around
	// workflow.ClaimStoryStatus; tests inject a fake that enforces
	// CanTransitionTo so the full walk chain is exercisable without NATS.
	storyStatusClaimer storyStatusClaimerFunc

	// Story-gate (Murat) review knowledge — symmetric with the per-task
	// reviewer in execution-manager so the requirement-level gate sees the
	// same project standards + team lessons rather than judging in isolation.
	standards       *workflow.Standards
	lessonWriter    *lessons.Writer
	errorCategories *workflow.ErrorCategoryRegistry

	inputPorts  []component.Port
	outputPorts []component.Port

	// activeExecs is a typed TTL cache mapping entityID → *requirementExecution.
	// Holds runtime state for in-flight executions.
	// Entries are explicitly deleted on completion; TTL is a safety net for leaks.
	activeExecs   sscache.Cache[*requirementExecution]
	activeExecsMu sync.Mutex // guards get-or-set for duplicate trigger detection

	// Lifecycle
	wg          sync.WaitGroup
	replayGate  sync.WaitGroup // tracks watcher replay completion; resumption waits on this
	running     bool
	mu          sync.RWMutex
	lifecycleMu sync.Mutex

	// Metrics
	triggersProcessed     atomic.Int64
	requirementsCompleted atomic.Int64
	requirementsFailed    atomic.Int64
	// retriesExhausted bumps when a requirement hits its max_retries ceiling
	// and is marked failed without further attempts. Passive signal for
	// answering "should we raise the default retry budget?" after we have
	// enough runs to mine — paired with requirementsCompleted (which carries
	// the retry_count on each execution) via trajectory data.
	retriesExhausted atomic.Int64
	errors           atomic.Int64
	lastActivityMu   sync.RWMutex
	lastActivity     time.Time
}

// NewComponent creates a new requirement-executor from raw JSON config.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var cfg Config
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal requirement-executor config: %w", err)
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

	registry := prompt.NewRegistry()
	registry.RegisterAll(promptdomain.Software()...)
	registry.Register(prompt.ToolGuidanceFragment(prompt.DefaultToolGuidance()))
	registry.Register(prompt.GraphManifestFragment(workflowtools.RegistrySummaryFetchFn()))

	nc := deps.NATSClient
	c := &Component{
		config:        cfg,
		natsClient:    nc,
		logger:        logger,
		platform:      deps.Platform,
		toolRegistry:  deps.ToolRegistry,
		modelRegistry: deps.ModelRegistry,
		sandbox:       newSandboxClient(cfg.SandboxURL),
		assembler:     prompt.NewAssembler(registry),
		tripleWriter: &graphutil.TripleWriter{
			NATSClient:    nc,
			Logger:        logger,
			ComponentName: componentName,
		},
		// Default story-status claimer wraps the real NATS round-trip.
		// Tests may replace this with a fake that enforces CanTransitionTo.
		storyStatusClaimer: func(ctx context.Context, slug, storyID string, target workflow.StoryStatus) bool {
			return workflow.ClaimStoryStatus(ctx, nc, slug, storyID, target, logger)
		},
	}
	c.initReviewKnowledge()

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

	c.logger.Info("Starting requirement-executor")

	// Initialize typed cache for in-flight execution routing.
	// TTL is a safety net for leaked entries; normal cleanup is explicit via Delete.
	ae, err := sscache.NewTTL[*requirementExecution](ctx, 4*time.Hour, 30*time.Minute)
	if err != nil {
		return fmt.Errorf("init active executions cache: %w", err)
	}
	c.activeExecs = ae

	// Reconcile: recover in-flight executions from graph state.
	c.reconcileFromGraph(ctx)

	// KV watchers for durable completion delivery and self-triggering:
	// - AGENT_LOOPS: decomposer, reviewer (direct agentic loops)
	// - EXECUTION_STATES task.>: TDD node completions (execution-manager pipeline)
	// - EXECUTION_STATES req.>: pending requirement executions (KV self-trigger)
	//
	// All watchers replay historical entries on startup. The replayGate
	// tracks when all watchers have finished replay (nil sentinel received).
	// Resumption of interrupted executions is deferred until after all replay
	// is complete to avoid re-dispatching work that completed during downtime.
	c.replayGate.Add(3)
	c.wg.Add(4) // 3 watchers + 1 resumption goroutine
	go func() {
		defer c.wg.Done()
		c.watchLoopCompletions(ctx)
	}()
	go func() {
		defer c.wg.Done()
		c.watchTaskCompletions(ctx)
	}()
	go func() {
		defer c.wg.Done()
		c.watchReqPending(ctx)
	}()
	go func() {
		defer c.wg.Done()
		c.replayGate.Wait()
		c.resumeInterruptedExecutions(ctx)
	}()

	// ADR-037 race closure: subscribe to plan-decision-accepted events so
	// awaiting-recovery execs can be resumed when their recovery
	// PlanDecision lands accepted. Best-effort: a startup failure here
	// disables the resume path but does not stop the component — the
	// recovery timer remains as the terminal-fail fallback.
	if c.recoveryDeferEnabled() {
		if err := c.startPlanDecisionAcceptedConsumer(ctx); err != nil {
			c.logger.Warn("Failed to start plan-decision-accepted consumer; recovery resume disabled",
				"error", err)
		}
	}

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

	c.logger.Info("Stopping requirement-executor",
		"triggers_processed", c.triggersProcessed.Load(),
		"requirements_completed", c.requirementsCompleted.Load(),
		"requirements_failed", c.requirementsFailed.Load(),
		"retries_exhausted", c.retriesExhausted.Load(),
	)

	// Drain in-flight timeout goroutines.
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
		c.logger.Debug("All in-flight timeout goroutines drained")
	case <-time.After(timeout):
		c.logger.Warn("Timed out waiting for in-flight timeout goroutines to drain")
	}

	for _, key := range c.activeExecs.Keys() {
		exec, ok := c.activeExecs.Get(key)
		if !ok {
			continue
		}
		exec.mu.Lock()
		if exec.timeoutTimer != nil {
			exec.timeoutTimer.stop()
		}
		if exec.recoveryTimer != nil {
			exec.recoveryTimer.stop()
		}
		exec.mu.Unlock()
	}

	c.mu.Lock()
	c.running = false
	c.mu.Unlock()

	return nil
}

// ---------------------------------------------------------------------------
// Reconciliation
// ---------------------------------------------------------------------------

// reconcileFromGraph tries KV-first recovery of in-flight requirement executions,
// falling back to graph-based recovery when KV is unavailable. KV holds the full
// execution struct including DAG, node results, and routing task IDs — graph only
// carries identity triples, so KV produces a much richer restored state.
func (c *Component) reconcileFromGraph(ctx context.Context) {
	if c.reconcileFromKV(ctx) {
		return // KV had active executions — skip graph fallback.
	}

	// Graph fallback: restores minimal identity fields only. Used on first
	// startup before any KV state exists, or when EXECUTION_STATES is unavailable.
	reconcileCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	entities, err := c.tripleWriter.ReadEntitiesByPrefix(reconcileCtx,
		workflow.EntityPrefix()+".exec.req.run.", 200)
	if err != nil {
		c.logger.Info("No graph state to reconcile (expected on first start)", "error", err)
		return
	}

	recovered := 0
	for entityID, triples := range entities {
		phase := triples[wf.Phase]
		// Skip terminal phases — no recovery needed.
		// phaseAwaitingRecovery is NON-terminal; we leave it alone here. The
		// recovery accept/reject/timeout path either resumes the exec (next
		// dispatch happens through resumeFromRecoveryLocked) or terminal-fails
		// it. Reconcile-on-restart would not have the recoveryTimer in flight,
		// so an awaiting-recovery exec recovered from graph state is effectively
		// orphaned; surfacing it as failed is the safer reconcile shape.
		if phase == phaseCompleted || phase == phaseFailed || phase == phaseError {
			continue
		}
		if phase == phaseAwaitingRecovery {
			// Recovered from graph with no timer — treat as failed reconcile,
			// since we don't have the original failure reason or the deadline.
			// The recovery PlanDecision is durable; if it lands, the cascade
			// will dirty-mark and a fresh req execution will spawn. Better to
			// release than to hold an exec with no completion path.
			continue
		}

		exec := &requirementExecution{
			EntityID:       entityID,
			Slug:           triples[wf.Slug],
			TraceID:        triples[wf.TraceID],
			CurrentNodeIdx: -1,
			VisitedNodes:   make(map[string]bool),
		}

		c.activeExecs.Set(entityID, exec) //nolint:errcheck // best-effort reconciliation
		recovered++
		c.logger.Info("Recovered requirement execution from graph",
			"entity_id", entityID,
			"slug", exec.Slug,
			"phase", phase,
		)
	}

	if recovered > 0 {
		c.logger.Info("Requirement execution reconciliation complete",
			"recovered", recovered,
			"total_entities", len(entities),
		)
	}
}

// reconcileFromKV reads EXECUTION_STATES and rebuilds full in-memory state for
// any non-terminal requirement execution. Restores DAG, node results, routing task
// IDs, retry state, and branch — everything needed to resume execution after a
// process restart without replaying the trigger.
//
// Returns true if at least one execution was recovered (signals caller to skip the
// graph fallback).
func (c *Component) reconcileFromKV(ctx context.Context) bool {
	if c.natsClient == nil {
		return false
	}

	reconcileCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	bucket, err := c.natsClient.GetKeyValueBucket(reconcileCtx, "EXECUTION_STATES")
	if err != nil {
		c.logger.Debug("EXECUTION_STATES not available for KV reconciliation", "error", err)
		return false
	}
	kvStore := c.natsClient.NewKVStore(bucket)

	keys, err := kvStore.Keys(reconcileCtx)
	if err != nil || len(keys) == 0 {
		c.logger.Debug("No execution states found in KV", "error", err)
		return false
	}

	recovered := 0
	for _, key := range keys {
		if !strings.HasPrefix(key, "req.") {
			continue
		}

		entry, err := kvStore.Get(reconcileCtx, key)
		if err != nil {
			c.logger.Debug("Failed to read KV entry during reconciliation",
				"key", key, "error", err)
			continue
		}

		var reqExec workflow.RequirementExecution
		if err := json.Unmarshal(entry.Value, &reqExec); err != nil {
			c.logger.Warn("Failed to unmarshal KV entry during reconciliation",
				"key", key, "error", err)
			continue
		}

		// Skip terminal stages — nothing to resume.
		if workflow.IsTerminalReqStage(reqExec.Stage) {
			continue
		}

		exec := c.rebuildExecFromKV(key, &reqExec)
		c.activeExecs.Set(exec.EntityID, exec) //nolint:errcheck // best-effort reconciliation
		recovered++

		nodeCount := 0
		if exec.DAG != nil {
			nodeCount = len(exec.DAG.Nodes)
		}
		c.logger.Info("Recovered requirement execution from KV",
			"entity_id", exec.EntityID,
			"slug", exec.Slug,
			"stage", reqExec.Stage,
			"current_node_idx", exec.CurrentNodeIdx,
			"node_count", nodeCount,
			"has_dag", exec.DAG != nil,
			"store_key", key,
		)
	}

	if recovered > 0 {
		c.logger.Info("KV requirement execution reconciliation complete",
			"recovered", recovered,
		)
	}

	return recovered > 0
}

// rebuildExecFromKV reconstructs a requirementExecution from a KV-serialized
// RequirementExecution record. Restores DAG, node index, visited nodes, and
// retry state so that the execution can be resumed.
func (c *Component) rebuildExecFromKV(key string, reqExec *workflow.RequirementExecution) *requirementExecution {
	exec := &requirementExecution{
		EntityID:          reqExec.EntityID,
		Slug:              reqExec.Slug,
		RequirementID:     reqExec.RequirementID,
		Title:             reqExec.Title,
		Description:       reqExec.Description,
		TraceID:           reqExec.TraceID,
		LoopID:            reqExec.LoopID,
		RequestID:         reqExec.RequestID,
		Model:             reqExec.Model,
		ProjectID:         reqExec.ProjectID,
		Scenarios:         reqExec.Scenarios,
		CurrentNodeTaskID: reqExec.CurrentNodeTaskID,
		ReviewerTaskID:    reqExec.ReviewerTaskID,
		RequirementBranch: reqExec.RequirementBranch,
		BaseBranch:        reqExec.BaseBranch,
		CurrentNodeIdx:    reqExec.CurrentNodeIdx,
		SortedNodeIDs:     reqExec.SortedNodeIDs,
		SortedStoryIDs:    reqExec.SortedStoryIDs,
		CurrentStoryIdx:   reqExec.CurrentStoryIdx,
		RetryCount:        reqExec.RetryCount,
		MaxRetries:        reqExec.MaxRetries,
		storeKey:          key,
	}

	if exec.MaxRetries == 0 {
		exec.MaxRetries = c.config.MaxRequirementRetries
	}

	// Rebuild DAG and NodeIndex from the serialized DAGRaw blob.
	if len(reqExec.DAGRaw) > 0 {
		var dag TaskDAG
		if err := json.Unmarshal(reqExec.DAGRaw, &dag); err == nil {
			exec.DAG = &dag
			exec.NodeIndex = make(map[string]*TaskNode, len(dag.Nodes))
			for i := range dag.Nodes {
				exec.NodeIndex[dag.Nodes[i].ID] = &dag.Nodes[i]
			}
		}
	}

	// Rebuild VisitedNodes from NodeResults, scoped to the current Story's
	// node set. NodeResults accumulates across Stories (the requirement-level
	// claim/observation gate at handleApprovedClaimMismatchLocked iterates
	// all of them); but VisitedNodes tracks which nodes have finished in the
	// CURRENT Story only. The reviewer-fires-when-done check at
	// recordNodeSuccessLocked compares len(VisitedNodes) against
	// len(SortedNodeIDs) — which only carries the current Story's nodes —
	// so if VisitedNodes were populated from the full cross-Story
	// accumulator, the comparison would trivially fire after the first
	// resumed node completes and skip the rest of the Story. Closes
	// go-reviewer Pass-1 finding C3.
	//
	// CommitSHA is round-tripped on the NodeResults so the claim/observation
	// gate sees the merge commit for nodes that completed before the
	// restart (Pass-1 C4 — closed in the prior commit of this stack).
	currentStoryNodes := make(map[string]bool, len(reqExec.SortedNodeIDs))
	for _, id := range reqExec.SortedNodeIDs {
		currentStoryNodes[id] = true
	}
	exec.VisitedNodes = make(map[string]bool, len(reqExec.NodeResults))
	exec.NodeResults = make([]NodeResult, 0, len(reqExec.NodeResults))
	for _, nr := range reqExec.NodeResults {
		// Only mark visited if this node belongs to the current Story's DAG.
		// When SortedNodeIDs is empty (legacy / pre-DAG state), fall back to
		// the prior behavior of treating every NodeResult as visited so
		// non-Story-aware plans keep working.
		if len(currentStoryNodes) == 0 || currentStoryNodes[nr.NodeID] {
			exec.VisitedNodes[nr.NodeID] = true
		}
		exec.NodeResults = append(exec.NodeResults, NodeResult{
			NodeID:        nr.NodeID,
			FilesModified: nr.FilesModified,
			Summary:       nr.Summary,
			CommitSHA:     nr.CommitSHA,
		})
	}

	return exec
}

// applyParsedDAGLocked topo-sorts the DAG, populates exec state for the
// per-node dispatch loop, persists to EXECUTION_STATES, publishes graph
// entities, and kicks off the first node dispatch.
//
// source identifies the DAG origin for telemetry ("stories-synthesis").
// All callers must hold exec.mu.
//
// ADR-043 PR 4g — the decomposer LLM path was retired; synthesis from
// Sarah-prepared Stories is now the only DAG source. The function kept
// its name to localize the rename diff.
func (c *Component) applyParsedDAGLocked(ctx context.Context, exec *requirementExecution, dag *TaskDAG, source string) {
	sorted, err := topoSort(dag)
	if err != nil {
		c.markFailedLocked(ctx, exec, fmt.Sprintf("topological sort failed: %v", err))
		return
	}

	exec.DAG = dag
	exec.SortedNodeIDs = sorted
	exec.NodeIndex = make(map[string]*TaskNode, len(dag.Nodes))
	for i := range dag.Nodes {
		exec.NodeIndex[dag.Nodes[i].ID] = &dag.Nodes[i]
	}

	// Persist DAG state to EXECUTION_STATES for crash recovery.
	// The DAGRaw + SortedNodeIDs + SortedStoryIDs + CurrentStoryIdx fields let
	// reconcileFromKV rebuild the full execution state without re-running the
	// decomposer AND without losing the per-Story cursor (Pass-1 C1/C2).
	dagRaw, _ := json.Marshal(*dag)

	nodeCount := len(sorted)
	fields := map[string]any{
		"node_count":      nodeCount,
		"dag":             json.RawMessage(dagRaw),
		"sorted_node_ids": sorted,
	}
	// Include the Story cursor when populated. dispatchSynthesizerLocked seeds
	// these the first time the requirement enters per-Story dispatch, and
	// advanceToNextStoryLocked re-enters here after incrementing the cursor —
	// so this single sendReqPhase site is the chokepoint for keeping KV in
	// sync with the in-memory cursor.
	if len(exec.SortedStoryIDs) > 0 {
		fields["sorted_story_ids"] = exec.SortedStoryIDs
		// Send as pointer to satisfy the *int wire shape — value 0 still
		// distinguishes "set to 0" from "leave unchanged" on the consumer.
		idx := exec.CurrentStoryIdx
		fields["current_story_idx"] = &idx
	}
	if err := c.sendReqPhase(ctx, exec.storeKey, phaseExecuting, fields); err != nil {
		c.logger.Warn("Failed to send req.phase mutation", "stage", phaseExecuting, "error", err)
	}

	c.logger.Info("Decomposition complete, starting serial execution",
		"entity_id", exec.EntityID,
		"node_count", len(sorted),
		"source", source,
	)

	// Publish each DAG node as a graph entity so the knowledge graph captures
	// the full execution hierarchy. Best-effort: failure does not abort execution.
	// TODO(#180-followup): move publish off-lock — all call sites of
	// applyParsedDAGLocked hold exec.mu (via dispatchCurrentStoryLocked /
	// advanceToNextStoryLocked), making a clean unlock/relock here tangled.
	// The 2 s / zero-retry bound on UpsertEntity limits the worst-case hold
	// to 2 s × N nodes; acceptable until a refactor extracts graph publish
	// as a post-lock pass.
	c.publishDAGNodes(ctx, exec)

	// Dispatch the first node.
	c.dispatchNextNodeLocked(ctx, exec)
}

// ---------------------------------------------------------------------------
// Node complete
// ---------------------------------------------------------------------------

// nodeResultPayload mirrors the synthesizer in req_completions.go's
// handleTaskStateChange (the only production producer of node-complete
// event.Result today). Note that producer hardcodes changes_summary="";
// the field is retained on the parser struct for forward compatibility
// in case a different producer wires real summaries through.
type nodeResultPayload struct {
	FilesModified []string `json:"files_modified"`
	FilesCreated  []string `json:"files_created"`
	Summary       string   `json:"changes_summary"`
	MergeCommit   string   `json:"merge_commit"`
}

// emitParseIncident writes ADR-035 CP-1 telemetry for a parse-checkpoint
// outcome. Strict outcomes are no-ops; rejected or tolerated_quirk
// outcomes write a parse.incident triple set keyed at
// "<event.LoopID>:parse:response_parse" so retry replays of the same
// loop are idempotent in the SKG.
//
// Used by both the decomposer-completion handler (audit B.4) and the
// node-completion handler (audit B.5) — same shape, different role.
// extraLogFields lets the caller add site-specific log context (e.g.
// node_id) to the emit-failure Warn line.
//
// Best-effort: graph-write failures are logged but do NOT fail the
// flow — telemetry is observability, not gating. Phase-2 of the
// named-quirks list per ADR-035 §3 + the planner first-wire pattern
// in commit 403a39d.
func (c *Component) emitParseIncident(ctx context.Context, role string, event *agentic.LoopCompletedEvent, exec *requirementExecution, quirks []jsonutil.QuirkID, parseErr error, extraLogFields ...any) {
	if c.tripleWriter == nil {
		return
	}
	ic := parseincident.IncidentContext{
		CallID: event.LoopID,
		Role:   role,
		Model:  exec.Model,
	}
	// TODO(ADR-035 phase 3): align Reason triple with future RETRY HINT
	// injection at this site, mirroring the planner + exec-mgr TODOs.
	if _, err := parseincident.EmitForResult(
		ctx,
		c.tripleWriter,
		ic,
		observability.CheckpointResponseParse,
		jsonutil.QuirkIDsToStrings(quirks),
		event.Result,
		parseErr,
	); err != nil {
		fields := []any{
			"entity_id", exec.EntityID,
			"loop_id", event.LoopID,
			"role", role,
			"error", err,
		}
		fields = append(fields, extraLogFields...)
		c.logger.Warn("CP-1 incident emit failed", fields...)
	}
}

// parseNodeResultPayload extracts the structured node-completion fields
// from a LoopCompletedEvent.Result string. Per ADR-035 audit site B.5,
// the caller routes parse failures through the requirement-level retry
// path rather than silently producing a zero-value NodeResult.
//
// Returns (payload, quirksFired, error). QuirksFired surfaces ParseStrict's
// per-fire attribution so the caller can flow it into parseincident.Emit
// for CP-1 SKG telemetry (ADR-035 audit B.5 + phase-2 wire). The slice
// is empty when ParseStrict didn't apply any named-quirk transform.
//
// Two failure classes:
//   - shape: payload is empty after JSON extraction or json.Unmarshal fails
//   - content: well-formed JSON has none of files_modified, files_created,
//     changes_summary, or merge_commit populated — node produced no
//     recordable output despite outcome=success
//
// Both classes return errors; the caller maps them to the same retry path.
func parseNodeResultPayload(raw string) (*nodeResultPayload, []jsonutil.QuirkID, error) {
	parsed := jsonutil.ParseStrict(raw)
	if parsed.JSON == "" {
		return nil, parsed.QuirksFired, fmt.Errorf("no JSON object found in event result")
	}
	var result nodeResultPayload
	if err := json.Unmarshal([]byte(parsed.JSON), &result); err != nil {
		return nil, parsed.QuirksFired, fmt.Errorf("unmarshal: %w", err)
	}
	if len(result.FilesModified) == 0 && len(result.FilesCreated) == 0 &&
		result.Summary == "" && result.MergeCommit == "" {
		return nil, parsed.QuirksFired, fmt.Errorf("payload has no files_modified, files_created, changes_summary, or merge_commit — node produced no recordable output")
	}
	return &result, parsed.QuirksFired, nil
}

func (c *Component) handleNodeCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *requirementExecution) {
	// Get nodeID from current execution state. Execution is serial, so
	// CurrentNodeIdx always identifies the active node. This works for both
	// direct agentic-loop completions (WorkflowStep=nodeID) and
	// execution-orchestrator completions (WorkflowStep=taskID).
	if exec.CurrentNodeIdx < 0 || exec.CurrentNodeIdx >= len(exec.SortedNodeIDs) {
		c.markErrorLocked(ctx, exec, "node completion received but no active node")
		return
	}
	nodeID := exec.SortedNodeIDs[exec.CurrentNodeIdx]
	exec.VisitedNodes[nodeID] = true

	if event.Outcome != agentic.OutcomeSuccess {
		c.handleNodeFailureLocked(ctx, event, exec, nodeID)
		return
	}

	// Parse the result payload BEFORE publishing completion or appending to
	// NodeResults. ADR-035 audit site B.5: a populated-but-malformed payload
	// (or one with no recordable output fields) routes through the
	// requirement-level retry path rather than silently producing a
	// zero-value NodeResult and letting the downstream reviewer judge work
	// the executor never recorded. Empty Result remains legitimate (test
	// fixtures and the no-payload success case fall through unchanged).
	parsed, ok := c.parseNodeCompletionPayloadLocked(ctx, event, exec, nodeID)
	if !ok {
		return
	}

	c.recordNodeSuccessLocked(ctx, exec, nodeID, parsed)
}

// handleNodeFailureLocked deals with a non-success node outcome. It splits
// the TDD-exhaustion (deterministic upstream defect — markFailed) and
// retry-the-node (transient flake — re-dispatch with feedback) paths.
// Caller must hold exec.mu.
//
// Layer-2 retry: AGENT failures worth a fresh generation — the bug-#9
// claim/observation guard fired because the developer reported
// files_modified that produced no commit, code didn't compile, merge
// raced, etc. Re-dispatch the developer for the SAME node with prior
// workspace + feedback so a NEW generation can fix what the prior one
// missed.
//
// Layer-1 retry lives in processor/execution-manager/component.go's
// mergeWorktree (see that comment for cross-reference). Layer-1 fixes
// INFRASTRUCTURE flakes (repoMu contention, transient git plumbing) by
// retrying the merge of the same hash. Different cause, different
// remedy, do not collapse.
//
// Skip retry on phase=escalated. TDD-budget exhaustion at the
// execution-manager level means the developer already burned its full
// max_tdd_cycles budget on the same task with no progress. Retrying
// spawns a NEW task with cycle=0 against the same scope, same prompt,
// same upstream defect — and burns another full budget. Caught
// 2026-05-03 on openrouter @easy /health where the retry chain produced
// ~570K input tokens per dev dispatch × (max_tdd_cycles 3) ×
// (max_requirement_retries 2 + 1 first try) = 9 dev dispatches all
// hallucinating against a broken scope. PlanDecision
// (Kind=ExecutionExhausted) is the right escalation path;
// markFailedLocked surfaces it.
func (c *Component) handleNodeFailureLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *requirementExecution, nodeID string) {
	// TODO(#180-followup): publish off-lock; bounded at 2 s by UpsertEntity config.
	c.publishDAGNodeStatus(ctx, exec, nodeID, "failed")

	// Parse the failure stage out of the synthetic event payload so we
	// can distinguish phase=escalated from phase=error. Stages are
	// surfaced by req_completions.handleTaskStateChange.
	var failurePayload struct {
		TaskStage        string `json:"task_stage"`
		EscalationReason string `json:"escalation_reason"`
	}
	if event.Result != "" {
		_ = json.Unmarshal([]byte(event.Result), &failurePayload)
	}

	tddExhausted := failurePayload.TaskStage == "escalated"

	if !tddExhausted && exec.RetryCount < exec.MaxRetries && exec.MaxRetries > 0 {
		feedback := fmt.Sprintf("Node %q failed (outcome=%s). Retry the implementation.", nodeID, event.Outcome)
		c.retryNodeAtRequirementLevelLocked(ctx, exec, nodeID, feedback, "Retrying failed node at requirement level")
		return
	}

	failureReason := fmt.Sprintf("node %q failed: outcome=%s", nodeID, event.Outcome)
	if tddExhausted {
		failureReason = fmt.Sprintf("node %q TDD budget exhausted at task level: %s",
			nodeID, failurePayload.EscalationReason)
		c.logger.Info("Skipping requirement-level retry — TDD budget exhausted upstream",
			"entity_id", exec.EntityID,
			"node_id", nodeID,
			"escalation_reason", failurePayload.EscalationReason)
		// ADR-037 race closure: execution-manager.publishRecoveryRequested
		// fires synchronously alongside the task-level escalation that drives
		// us here. Defer terminal-failure so the in-flight recovery's
		// accepted PlanDecision can revive this exec instead of dirty-marking
		// a graveyard.
		if c.deferToAwaitingRecoveryLocked(ctx, exec, failureReason) {
			return
		}
	}
	c.markFailedLocked(ctx, exec, failureReason)
}

// parseNodeCompletionPayloadLocked parses event.Result for a successful
// node completion. Returns the parsed payload (nil when event.Result is
// empty — legitimate for fixtures and no-payload successes) and ok=true
// to continue, or ok=false when parse failure routed through the retry
// path and the caller must abort. Caller must hold exec.mu.
func (c *Component) parseNodeCompletionPayloadLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *requirementExecution, nodeID string) (*nodeResultPayload, bool) {
	if event.Result == "" {
		return nil, true
	}
	parsed, quirks, parseErr := parseNodeResultPayload(event.Result)

	// CP-1 incident emit (ADR-035 audit B.5) — DAG nodes dispatch as
	// developer tasks, so role=developer aggregates with B.1 developer
	// fires under per-(role, model) operator queries. B.1 dispatches one
	// developer call per task; B.5 dispatches N per requirement (one per
	// DAG node). Keeping the same role label means the partition stays
	// queryable end-to-end without fragmenting on dispatch path. If
	// dispatch-path attribution becomes load-bearing, add a separate
	// predicate to vocabulary/observability rather than fragment the role
	// axis.
	c.emitParseIncident(ctx, "developer", event, exec, quirks, parseErr, "node_id", nodeID)

	if parseErr == nil {
		return parsed, true
	}

	// TODO(#180-followup): publish off-lock; bounded at 2 s by UpsertEntity config.
	c.publishDAGNodeStatus(ctx, exec, nodeID, "failed")
	feedback := fmt.Sprintf(
		"Node %q completed but its result payload was unrecoverable: %s. "+
			"Re-run the same scope; ensure submit_work returns valid JSON containing "+
			"files_modified (and/or files_created), changes_summary, and merge_commit fields.",
		nodeID, parseErr,
	)
	if exec.RetryCount < exec.MaxRetries && exec.MaxRetries > 0 {
		c.retryNodeAtRequirementLevelLocked(ctx, exec, nodeID, feedback, "Node result parse failed — retrying at requirement level", "parse_error", parseErr)
		return nil, false
	}
	c.markFailedLocked(ctx, exec, fmt.Sprintf("node %q result parse failed and retries exhausted: %v", nodeID, parseErr))
	return nil, false
}

// retryNodeAtRequirementLevelLocked rewinds the DAG cursor and
// re-dispatches the just-failed node with feedback. Shared by the
// outcome-failure and parse-failure paths so retry bookkeeping (counter,
// dirty-nodes, KV mutation, log line) stays consistent. Caller must hold
// exec.mu.
func (c *Component) retryNodeAtRequirementLevelLocked(ctx context.Context, exec *requirementExecution, nodeID, feedback, logMsg string, extraLogFields ...any) {
	exec.RetryCount++
	exec.LastReviewFeedback = feedback
	exec.terminated = false
	exec.DirtyNodeIDs = []string{nodeID}
	delete(exec.VisitedNodes, nodeID)

	if err := c.sendReqPhase(ctx, exec.storeKey, phaseExecuting, map[string]any{
		"retry_count": exec.RetryCount,
		"dirty_nodes": exec.DirtyNodeIDs,
	}); err != nil {
		c.logger.Warn("Failed to send req.phase mutation for node retry", "error", err)
	}

	fields := []any{
		"entity_id", exec.EntityID,
		"node_id", nodeID,
		"retry_count", exec.RetryCount,
	}
	fields = append(fields, extraLogFields...)
	c.logger.Info(logMsg, fields...)

	exec.CurrentNodeIdx--
	c.dispatchNextNodeLocked(ctx, exec)
}

// recordNodeSuccessLocked appends the node's result, updates KV, and
// either advances to the next node or kicks off the requirement
// reviewer when the DAG is complete. Caller must hold exec.mu.
func (c *Component) recordNodeSuccessLocked(ctx context.Context, exec *requirementExecution, nodeID string, parsed *nodeResultPayload) {
	// TODO(#180-followup): publish off-lock; bounded at 2 s by UpsertEntity config.
	c.publishDAGNodeStatus(ctx, exec, nodeID, "completed")

	nodeResult := NodeResult{NodeID: nodeID}
	if parsed != nil {
		nodeResult.FilesModified = append(parsed.FilesModified, parsed.FilesCreated...)
		nodeResult.Summary = parsed.Summary
		nodeResult.CommitSHA = parsed.MergeCommit
	}
	exec.NodeResults = append(exec.NodeResults, nodeResult)

	wfResult := &workflow.NodeResult{
		NodeID:        nodeResult.NodeID,
		FilesModified: nodeResult.FilesModified,
		Summary:       nodeResult.Summary,
		CommitSHA:     nodeResult.CommitSHA,
	}
	if err := c.sendReqNode(ctx, exec.storeKey, exec.CurrentNodeIdx, "", wfResult); err != nil {
		c.logger.Warn("Failed to send req.node mutation", "node_id", nodeID, "error", err)
	}

	c.logger.Info("Node completed",
		"entity_id", exec.EntityID,
		"node_id", nodeID,
		"completed", len(exec.VisitedNodes),
		"total", len(exec.SortedNodeIDs),
	)

	if len(exec.VisitedNodes) >= len(exec.SortedNodeIDs) {
		c.beginRequirementReviewLocked(ctx, exec)
		return
	}
	c.dispatchNextNodeLocked(ctx, exec)
}

// ---------------------------------------------------------------------------
// DAG synthesis from Sarah-prepared Stories
// ---------------------------------------------------------------------------

// dispatchSynthesizerLocked synthesizes the TaskDAG from Sarah-prepared
// Stories on the plan and hands off to applyParsedDAGLocked. (The LLM
// decomposer path was retired in ADR-043 PR 4g — synthesis from Sarah's
// Stories is the only DAG source.)
//
// Synthesis failure (no plan, no Stories for this requirement, invalid
// DAG) marks the requirement failed — Sarah is always-on post-PR 4l, so
// a missing Story is a planning-phase bug, not a runtime fallback case.
func (c *Component) dispatchSynthesizerLocked(ctx context.Context, exec *requirementExecution) {
	plan, err := c.loadPlanFromKV(ctx, exec.Slug)
	if err != nil {
		c.markFailedLocked(ctx, exec, fmt.Sprintf("load plan from KV failed: %v", err))
		return
	}
	if plan == nil {
		c.markFailedLocked(ctx, exec, "plan not found in PLAN_STATES — cannot synthesize DAG without Stories")
		return
	}

	// Populate SortedStoryIDs the first time the requirement enters this
	// path. Subsequent calls (recovery resume, etc.) re-enter with the
	// list already populated; CurrentStoryIdx is the cursor.
	if len(exec.SortedStoryIDs) == 0 {
		stories := plan.StoriesForRequirement(exec.RequirementID)
		if len(stories) == 0 {
			c.markFailedLocked(ctx, exec, fmt.Sprintf("no Stories on plan for requirement %s — story-preparer must run before execution", exec.RequirementID))
			return
		}
		sortedIDs, err := topoSortStoryIDs(stories)
		if err != nil {
			c.markFailedLocked(ctx, exec, fmt.Sprintf("topo-sort stories failed: %v", err))
			return
		}
		exec.SortedStoryIDs = sortedIDs
		exec.CurrentStoryIdx = 0
	}

	c.dispatchCurrentStoryLocked(ctx, exec, plan)
}

// dispatchCurrentStoryLocked synthesizes the DAG for the Story at
// SortedStoryIDs[CurrentStoryIdx] and hands off to applyParsedDAGLocked.
//
// ADR-044 M:N dedup + reservation: a Story may cover multiple Requirements
// (Story.RequirementIDs plural), so the SAME Story appears in
// StoriesForRequirement() for every requirement it covers. Without a
// reservation, N parallel requirement-executors all dispatch the dev loop
// on the same Story, burning tokens N times.
//
// Two-tier guard:
//
//  1. Post-completion skip (cheap, in-process): if the freshly-loaded plan
//     KV shows Status==Complete, another executor already shipped — copy
//     the deterministic owner's node evidence, advance cursor, and re-enter.
//     Catches the late-arriving case without producing zero-node completions.
//
//  2. Compare-and-swap reservation (load-bearing, NATS round-trip):
//     attempt ready→executing via workflow.ClaimStoryStatus. Plan-manager
//     enforces Story.Status.CanTransitionTo. First executor wins; others
//     observe the rejection and treat it as "another executor owns this
//     Story" only if completed-owner execution evidence exists. Closes
//     the race-load window where N executors all see Status as not-yet-set
//     and would otherwise all dispatch, while still failing closed when no
//     owner proof exists.
//
// Failed / Pending stories fall through to dispatch so requirement-level
// recovery still works. When the claim NATS call has no client wired
// (test components), the reservation is silently skipped — back-compat
// with unit tests that drive dispatchCurrentStoryLocked directly without
// a NATS substrate. Failed claim against a real client requires completed
// owner evidence before the executor skips dispatch.
//
// Caller must hold exec.mu.
func (c *Component) dispatchCurrentStoryLocked(ctx context.Context, exec *requirementExecution, plan *workflow.Plan) {
	if exec.CurrentStoryIdx < 0 || exec.CurrentStoryIdx >= len(exec.SortedStoryIDs) {
		c.markFailedLocked(ctx, exec, fmt.Sprintf("CurrentStoryIdx %d out of range [0, %d)", exec.CurrentStoryIdx, len(exec.SortedStoryIDs)))
		return
	}

	currentStoryID := exec.SortedStoryIDs[exec.CurrentStoryIdx]
	story, ok := findStoryByID(plan, currentStoryID)
	if !ok {
		c.markFailedLocked(ctx, exec, fmt.Sprintf("story %q not found on plan (planning regression — Sarah output drifted from SortedStoryIDs)", currentStoryID))
		return
	}

	if story.Status == workflow.StoryStatusComplete {
		c.logger.Info("Story already complete (M:N coverage by prior requirement); skipping",
			"entity_id", exec.EntityID,
			"requirement_id", exec.RequirementID,
			"story_id", currentStoryID,
			"covered_requirements", story.RequirementIDs)
		if !c.applyCompletedStoryEvidenceLocked(ctx, exec, story) {
			c.markFailedLocked(ctx, exec, fmt.Sprintf("completed Story %s has no completed owner execution evidence for requirement %s", currentStoryID, exec.RequirementID))
			return
		}
		c.advancePastSkippedStoryLocked(ctx, exec, plan)
		return
	}

	// Tier 2: claim the executing reservation. Skipped silently when the
	// NATS client is nil (unit-test components without dispatch substrate).
	if c.natsClient != nil {
		if !workflow.ClaimStoryStatus(ctx, c.natsClient, plan.Slug, currentStoryID, workflow.StoryStatusExecuting, c.logger) {
			c.logger.Info("Story executing-claim rejected (M:N reservation held by another executor); skipping",
				"entity_id", exec.EntityID,
				"requirement_id", exec.RequirementID,
				"story_id", currentStoryID)
			if !c.applyCompletedStoryEvidenceLocked(ctx, exec, story) {
				c.markFailedLocked(ctx, exec, fmt.Sprintf("Story %s reservation was unavailable and no completed owner execution evidence exists for requirement %s", currentStoryID, exec.RequirementID))
				return
			}
			c.advancePastSkippedStoryLocked(ctx, exec, plan)
			return
		}
	}

	dag, synthErr := synthesizeTaskDAGForStory(plan, story)
	if synthErr != nil {
		c.markFailedLocked(ctx, exec, fmt.Sprintf("synthesize DAG for story %s failed: %v", currentStoryID, synthErr))
		return
	}

	c.logger.Info("Dispatching story DAG",
		"entity_id", exec.EntityID,
		"requirement_id", exec.RequirementID,
		"story_id", currentStoryID,
		"story_idx", exec.CurrentStoryIdx+1,
		"story_total", len(exec.SortedStoryIDs),
		"node_count", len(dag.Nodes))

	c.applyParsedDAGLocked(ctx, exec, dag, "stories-synthesis")
}

// advancePastSkippedStoryLocked handles the common case where a Story was
// skipped (either Status==Complete or the executing-claim was rejected by
// the reservation pattern). Increments CurrentStoryIdx and re-enters
// dispatchCurrentStoryLocked for the next Story; if the cursor would
// overrun, marks the requirement complete.
//
// Caller must hold exec.mu.
func (c *Component) advancePastSkippedStoryLocked(ctx context.Context, exec *requirementExecution, plan *workflow.Plan) {
	if exec.CurrentStoryIdx+1 < len(exec.SortedStoryIDs) {
		exec.CurrentStoryIdx++
		resetPerStoryExecutionState(exec)
		c.dispatchCurrentStoryLocked(ctx, exec, plan)
		return
	}
	c.markCompletedLocked(ctx, exec)
}

// applyCompletedStoryEvidenceLocked attaches the owner requirement's real
// node results before a non-owner requirement advances past a completed
// shared Story. That keeps ADR-044's M:N dedup from producing zero-node
// terminal completions: the non-owner still does not re-run the dev loop,
// but its completion record carries the owner Story's execution evidence.
//
// Unit-test components without a NATS substrate keep the historical skip
// behavior; production paths require KV evidence from the deterministic
// Story owner and fail closed when it is missing.
//
// Caller must hold exec.mu.
func (c *Component) applyCompletedStoryEvidenceLocked(ctx context.Context, exec *requirementExecution, story workflow.Story) bool {
	if c.natsClient == nil {
		return true
	}

	ownerID := workflow.DeterministicStoryOwner(story)
	if ownerID == "" {
		c.logger.Warn("Completed Story has no deterministic owner; refusing evidence-free skip",
			"slug", exec.Slug,
			"requirement_id", exec.RequirementID,
			"story_id", story.ID)
		return false
	}

	if ownerID == exec.RequirementID {
		if executionHasStoryEvidence(exec, story) {
			return true
		}
		c.logger.Warn("Owned Story is already complete but current requirement has no node evidence",
			"slug", exec.Slug,
			"requirement_id", exec.RequirementID,
			"story_id", story.ID)
		return false
	}

	ownerExec, err := c.loadCompletedReqExecFromKV(ctx, exec.Slug, ownerID)
	if err != nil {
		c.logger.Warn("Failed to load completed Story owner execution evidence",
			"slug", exec.Slug,
			"requirement_id", exec.RequirementID,
			"owner_requirement_id", ownerID,
			"story_id", story.ID,
			"error", err)
		return false
	}
	if ownerExec == nil {
		c.logger.Warn("Completed Story owner execution evidence missing",
			"slug", exec.Slug,
			"requirement_id", exec.RequirementID,
			"owner_requirement_id", ownerID,
			"story_id", story.ID)
		return false
	}
	if !copyStoryEvidence(exec, story, ownerExec) {
		c.logger.Warn("Completed Story owner has no matching node evidence",
			"slug", exec.Slug,
			"requirement_id", exec.RequirementID,
			"owner_requirement_id", ownerID,
			"story_id", story.ID)
		return false
	}

	c.logger.Info("Copied completed Story owner evidence for M:N non-owner requirement",
		"slug", exec.Slug,
		"requirement_id", exec.RequirementID,
		"owner_requirement_id", ownerID,
		"story_id", story.ID,
		"node_results", len(exec.NodeResults))
	return true
}

func executionHasStoryEvidence(exec *requirementExecution, story workflow.Story) bool {
	taskIDs := storyTaskIDSet(story)
	if len(taskIDs) == 0 {
		return len(exec.NodeResults) > 0
	}
	for _, nr := range exec.NodeResults {
		if taskIDs[nr.NodeID] {
			return true
		}
	}
	return false
}

func copyStoryEvidence(exec *requirementExecution, story workflow.Story, owner *requirementExecution) bool {
	if owner == nil || len(owner.NodeResults) == 0 {
		return false
	}

	taskIDs := storyTaskIDSet(story)
	seen := make(map[string]bool, len(exec.NodeResults))
	for _, nr := range exec.NodeResults {
		seen[nodeEvidenceKey(nr)] = true
	}
	if exec.VisitedNodes == nil {
		exec.VisitedNodes = make(map[string]bool)
	}

	copied := 0
	for _, nr := range owner.NodeResults {
		if len(taskIDs) > 0 && !taskIDs[nr.NodeID] {
			continue
		}
		key := nodeEvidenceKey(nr)
		if seen[key] {
			continue
		}
		seen[key] = true
		exec.NodeResults = append(exec.NodeResults, NodeResult{
			NodeID:        nr.NodeID,
			FilesModified: append([]string(nil), nr.FilesModified...),
			Summary:       nr.Summary,
			CommitSHA:     nr.CommitSHA,
		})
		exec.VisitedNodes[nr.NodeID] = true
		copied++
	}
	return copied > 0 || executionHasStoryEvidence(exec, story)
}

func storyTaskIDSet(story workflow.Story) map[string]bool {
	out := make(map[string]bool, len(story.Tasks))
	for _, task := range story.Tasks {
		if task.ID != "" {
			out[task.ID] = true
		}
	}
	return out
}

func nodeEvidenceKey(nr NodeResult) string {
	return nr.NodeID + "\x00" + nr.CommitSHA
}

// findStoryByID returns the Story with the given ID from plan.Stories.
// Returns the zero Story and false when not found.
func findStoryByID(plan *workflow.Plan, storyID string) (workflow.Story, bool) {
	if plan == nil {
		return workflow.Story{}, false
	}
	for _, s := range plan.Stories {
		if s.ID == storyID {
			return s, true
		}
	}
	return workflow.Story{}, false
}

// loadPlanScope reads the plan from PLAN_STATES KV and returns its scope.
// Returns nil on any error (best-effort).
func (c *Component) loadPlanScope(ctx context.Context, slug string) *workflow.Scope {
	if c.natsClient == nil {
		return nil
	}
	js, err := c.natsClient.JetStream()
	if err != nil {
		return nil
	}
	bucket, err := js.KeyValue(ctx, "PLAN_STATES")
	if err != nil {
		return nil
	}
	entry, err := bucket.Get(ctx, slug)
	if err != nil {
		return nil
	}
	var plan struct {
		Scope *workflow.Scope `json:"scope"`
	}
	if err := json.Unmarshal(entry.Value(), &plan); err != nil {
		return nil
	}
	return plan.Scope
}

// loadPlanFromKV reads the full plan from PLAN_STATES KV. Returns nil
// (with error nil) when the bucket / entry is missing — the decomposer
// bypass path treats that as "no Stories available, fall through to
// legacy LLM decomposition". A non-nil error signals a parse failure;
// the caller logs and falls through too. Used by ADR-043 PR 4f.
func (c *Component) loadPlanFromKV(ctx context.Context, slug string) (*workflow.Plan, error) {
	if c.natsClient == nil {
		return nil, nil
	}
	js, err := c.natsClient.JetStream()
	if err != nil {
		return nil, nil
	}
	bucket, err := js.KeyValue(ctx, "PLAN_STATES")
	if err != nil {
		return nil, nil
	}
	entry, err := bucket.Get(ctx, slug)
	if err != nil {
		return nil, nil
	}
	var plan workflow.Plan
	if err := json.Unmarshal(entry.Value(), &plan); err != nil {
		return nil, fmt.Errorf("unmarshal plan %q: %w", slug, err)
	}
	return &plan, nil
}

// buildReviewPrompt constructs the prompt for requirement-level review.
// Includes requirement context, scenarios grouped by tier with tier-aware
// verification instructions (ADR-041 Move 6 — the load-bearing fix for
// issue #37), and files modified by completed nodes.
//
// Scenarios are partitioned by tier tag (@unit / @integration / @smoke /
// @e2e). Each tier group carries instructions that tell the reviewer
// WHAT to look for at that tier — crucially, @integration scenarios do
// NOT need to pass at dev-completion (the harness may not be running in the
// dev sandbox); they need to be authored correctly. Runtime proof is enforced
// by qa_level=integration when the project selects that level. Full/e2e
// orchestration remains an operator CI concern for MVP.
//
// Legacy scenarios without tier tags (PR 1's tags field absent) fall into
// an "untagged" group with the legacy "verify all aspects" instruction —
// existing pre-ADR-041 plans keep working.
//
// scenarios is the per-Story-scoped subset to put before the reviewer (per
// ADR-043 PR 4h). Callers must pass scopeScenariosToCurrentStory(exec) so
// the Prompt and the Context.Content agree on the verdict surface — pre-
// fix the Prompt used exec.Scenarios unfiltered while Context used the
// scoped subset, so the Story-1 reviewer was asked to verify Stories 2-3
// scenarios that the developer never authored. Closes go-reviewer Pass-1
// finding C5.
//
//revive:disable-next-line:function-length // sequential tier-grouped prompt builder; splitting would obscure the load-bearing tier-aware contract.
func (c *Component) buildReviewPrompt(exec *requirementExecution, scenarios []workflow.Scenario) string {
	var sb strings.Builder

	sb.WriteString("Requirement: ")
	sb.WriteString(exec.Title)
	sb.WriteString("\n")

	if exec.Description != "" {
		sb.WriteString("Description: ")
		sb.WriteString(exec.Description)
		sb.WriteString("\n")
	}

	if len(scenarios) > 0 {
		groups := groupScenariosByTier(scenarios)

		sb.WriteString("\n## Scenarios to verify (grouped by test pyramid tier)\n")
		sb.WriteString("\nApply tier-appropriate verification per ADR-041. The harness is not guaranteed to run in the dev sandbox — @integration scenarios are verified for AUTHORING CORRECTNESS here, not runtime behavior. Runtime proof is enforced later by qa_level=integration when the project selects that level; full/e2e orchestration remains operator CI for MVP.\n")

		if len(groups.unit) > 0 {
			sb.WriteString("\n### @unit scenarios\n")
			sb.WriteString("Verify each has a unit test method exercising the behavior with fakes / in-process state. The unit test MUST run and pass at dev-completion (no external dependencies). Reject if missing or if the test references real services / SITL / databases / peer processes (category error — flag for planner to retire the scenario).\n\n")
			writeScenarioList(&sb, groups.unit)
		}

		if len(groups.integration) > 0 {
			sb.WriteString("\n### @integration scenarios\n")
			sb.WriteString("Verify each has an integration test or documented integration-test plan that (a) declares the tier using the project's test framework idiom, (b) binds to the scenario's harness_profile_ids where project tooling consumes such bindings, (c) reads harness endpoints from environment variables declared by the bound catalog profile (NOT a hardcoded host/port), and (d) asserts on each required_assertion of every bound profile. The test does NOT need to PASS here — the harness may not be up in the dev sandbox. Approve when AUTHORING is correct; qa_level=integration is the runtime evidence gate when the project selects it, while full/e2e orchestration remains operator CI for MVP.\n\n")
			writeScenarioList(&sb, groups.integration)
		}

		if len(groups.smoke) > 0 {
			sb.WriteString("\n### @smoke scenarios\n")
			sb.WriteString("Verify each has at least a stub test file OR a documented release-gating plan. Do NOT block dev approval — smoke is gated at scheduled tiers downstream.\n\n")
			writeScenarioList(&sb, groups.smoke)
		}

		if len(groups.e2e) > 0 {
			sb.WriteString("\n### @e2e scenarios\n")
			sb.WriteString("Verify each has at least a stub test file OR a documented release-gating plan. Do NOT block dev approval — e2e is observed in full-system deployments downstream.\n\n")
			writeScenarioList(&sb, groups.e2e)
		}

		if len(groups.untagged) > 0 {
			sb.WriteString("\n### Untagged scenarios (legacy / pre-ADR-041)\n")
			sb.WriteString("These scenarios lack tier tags (plan was drafted before ADR-041 lands). Apply the legacy contract: verify each scenario's Given/When/Then is exercised by the implementation. If you find yourself wanting to demand integration-tier behavior from these, surface that as planner feedback rather than rejecting the dev.\n\n")
			writeScenarioList(&sb, groups.untagged)
		}
	}

	if len(exec.NodeResults) > 0 {
		sb.WriteString("\n## Completed Implementation Nodes\n")
		for _, nr := range exec.NodeResults {
			sb.WriteString(fmt.Sprintf("- %s", nr.NodeID))
			if len(nr.FilesModified) > 0 {
				sb.WriteString(fmt.Sprintf(" (files: %s)", strings.Join(nr.FilesModified, ", ")))
			}
			if nr.Summary != "" {
				sb.WriteString(fmt.Sprintf(": %s", nr.Summary))
			}
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// scenarioTierGroups partitions a requirement's scenarios by tier tag.
// Scenarios without a tier tag fall into `untagged` so legacy plans
// without ADR-041's Tags field keep producing a sensible review prompt.
type scenarioTierGroups struct {
	unit        []workflow.Scenario
	integration []workflow.Scenario
	smoke       []workflow.Scenario
	e2e         []workflow.Scenario
	untagged    []workflow.Scenario
}

// groupScenariosByTier walks scenarios once, dispatching each to the group
// matching its first recognized tier tag. A scenario with multiple tier
// tags should have been rejected upstream (PR 1's ValidateScenarioTags
// rejects, plan-reviewer's scenario.missing_tier_tag rule rejects); if one
// somehow slips through, the FIRST tier tag wins so the reviewer at least
// gets a consistent interpretation.
func groupScenariosByTier(scenarios []workflow.Scenario) scenarioTierGroups {
	var g scenarioTierGroups
	for _, s := range scenarios {
		switch firstTierTag(s.Tags) {
		case workflow.TierUnit:
			g.unit = append(g.unit, s)
		case workflow.TierIntegration:
			g.integration = append(g.integration, s)
		case workflow.TierSmoke:
			g.smoke = append(g.smoke, s)
		case workflow.TierE2E:
			g.e2e = append(g.e2e, s)
		default:
			g.untagged = append(g.untagged, s)
		}
	}
	return g
}

// firstTierTag returns the first recognized tier tag in the slice, or "".
func firstTierTag(tags []string) string {
	for _, t := range tags {
		if workflow.IsTierTag(t) {
			return t
		}
	}
	return ""
}

// writeScenarioList writes scenarios in the legacy [id] Given/When/Then
// format, plus an inline harness binding hint for @integration scenarios
// so the reviewer doesn't have to cross-reference to figure out which
// profile_ids the test should bind to.
func writeScenarioList(sb *strings.Builder, scenarios []workflow.Scenario) {
	for i, sc := range scenarios {
		thenParts := strings.Join(sc.Then, ", ")
		sb.WriteString(fmt.Sprintf("%d. [%s] Given %s, When %s, Then %s",
			i+1, sc.ID, sc.Given, sc.When, thenParts))
		if len(sc.HarnessProfileIDs) > 0 {
			sb.WriteString(fmt.Sprintf("  (harness: %s)", strings.Join(sc.HarnessProfileIDs, ", ")))
		}
		sb.WriteString("\n")
	}
}

// ---------------------------------------------------------------------------
// Agent dispatch: DAG node (serial)
// ---------------------------------------------------------------------------

// checkRequirementBranch verifies that the per-requirement branch still exists
// before dispatching a non-first DAG node. A missing branch means the sandbox
// was restarted (or its state wiped) between nodes and further dispatch would
// fail at git-checkout time with a confusing error.
//
// Returns nil when:
//   - no branch has been created yet (exec.RequirementBranch == "")
//   - sandbox is not configured (c.sandbox == nil)
//   - this is the first node (exec.CurrentNodeIdx <= 0) — branch is freshly
//     created by the trigger handler and guaranteed to exist
//
// TODO(branch-check): sandbox.Client does not yet expose a BranchExists method.
// When added, call it here and return an error if the branch is gone so that
// the requirement-level retry mechanism can re-decompose with a fresh branch.
//
// Caller must hold exec.mu.
func (c *Component) checkRequirementBranch(_ context.Context, exec *requirementExecution) error {
	if exec.RequirementBranch == "" {
		return nil
	}
	if c.sandbox == nil {
		return nil
	}
	if exec.CurrentNodeIdx <= 0 {
		// First node — branch was just created by the trigger handler.
		return nil
	}
	// TODO: call c.sandbox.BranchExists(ctx, exec.RequirementBranch) once the
	// sandbox client exposes that method, and return an error if it returns false.
	return nil
}

func (c *Component) dispatchNextNodeLocked(ctx context.Context, exec *requirementExecution) {
	// Advance to the next node, skipping clean nodes on retry.
	for {
		exec.CurrentNodeIdx++
		if exec.CurrentNodeIdx >= len(exec.SortedNodeIDs) {
			// All nodes dispatched and completed — proceed to requirement-level review.
			c.beginRequirementReviewLocked(ctx, exec)
			return
		}
		nodeID := exec.SortedNodeIDs[exec.CurrentNodeIdx]
		if c.isNodeDirty(exec, nodeID) {
			break // dispatch this node
		}
		// Clean node on retry — skip it.
		c.logger.Debug("Skipping clean node on retry",
			"entity_id", exec.EntityID,
			"node_id", nodeID,
			"retry_count", exec.RetryCount)
	}

	nodeID := exec.SortedNodeIDs[exec.CurrentNodeIdx]
	node, ok := exec.NodeIndex[nodeID]
	if !ok {
		c.markErrorLocked(ctx, exec, fmt.Sprintf("node %q not found in index", nodeID))
		return
	}

	if err := c.checkRequirementBranch(ctx, exec); err != nil {
		c.logger.Warn("Requirement branch lost — marking execution as error",
			"entity_id", exec.EntityID,
			"branch", exec.RequirementBranch,
			"error", err,
		)
		c.markErrorLocked(ctx, exec, "branch_lost")
		return
	}

	taskID := fmt.Sprintf("node-%s-%s", workflow.HashInstanceID(exec.EntityID, nodeID), uuid.New().String())
	exec.CurrentNodeTaskID = taskID
	exec.NodeTaskIDs = append(exec.NodeTaskIDs, taskID)

	// Build node prompt — on retry, append reviewer feedback for failed scenarios.
	nodePrompt := node.Prompt
	if exec.RetryCount > 0 && exec.LastReviewFeedback != "" {
		nodePrompt += "\n\n---\n\nREVISION REQUEST: The requirement reviewer rejected the previous attempt.\n\n" + exec.LastReviewFeedback
		nodePrompt += "\n\nYour workspace contains files from the previous attempt. Review what exists before making changes."
	}

	// Send node dispatch mutation to execution-manager.
	if err := c.sendReqNode(ctx, exec.storeKey, exec.CurrentNodeIdx, taskID, nil); err != nil {
		c.logger.Warn("Failed to send req.node mutation", "node_id", nodeID, "error", err)
	}

	// Update the DAG node graph entity to reflect that execution has started.
	// TODO(#180-followup): publish off-lock; bounded at 2 s by UpsertEntity config.
	c.publishDAGNodeStatus(ctx, exec, nodeID, "executing")

	// Dispatch to execution-manager for TDD pipeline processing via mutation.
	// execution-manager's KV watcher picks up the pending task entry.
	//
	// We intentionally do NOT carry exec.Model on the wire. execution-manager
	// resolves the developer model via its own capability+config.Model knob;
	// propagating exec.Model here would silently override that resolution and
	// recreate the take-7 bug (req-executor.config.Model="moe" pinning every
	// downstream developer regardless of execution-manager.config.Model).
	taskReq := map[string]any{
		"slug":            exec.Slug,
		"task_id":         taskID,
		"requirement_id":  exec.RequirementID,
		"title":           node.Prompt,
		"prompt":          nodePrompt,
		"project_id":      exec.ProjectID,
		"trace_id":        exec.TraceID,
		"loop_id":         exec.LoopID,
		"request_id":      fmt.Sprintf("node-%s-%s", exec.RequirementID, nodeID),
		"scenario_branch": exec.RequirementBranch,
		"file_scope":      node.FileScope,
	}
	if nodeScenarios := filterScenariosByIDs(exec.Scenarios, node.ScenarioIDs); len(nodeScenarios) > 0 {
		taskReq["scenarios"] = nodeScenarios
	}
	if err := c.sendTaskCreate(ctx, taskReq); err != nil {
		c.markErrorLocked(ctx, exec, fmt.Sprintf("dispatch node %q failed: %v", nodeID, err))
		return
	}

	c.logger.Info("Dispatched node",
		"requirement_id", exec.RequirementID,
		"entity_id", exec.EntityID,
		"node_id", nodeID,
		"node_index", exec.CurrentNodeIdx,
		"total_nodes", len(exec.SortedNodeIDs),
		"task_id", taskID,
	)
}

// ---------------------------------------------------------------------------
// Requirement-level review pipeline
// ---------------------------------------------------------------------------

// beginRequirementReviewLocked starts the requirement-level review pipeline.
// Caller must hold exec.mu.
func (c *Component) beginRequirementReviewLocked(ctx context.Context, exec *requirementExecution) {
	c.dispatchRequirementReviewerLocked(ctx, exec)
}

// dispatchRequirementReviewerLocked dispatches the requirement-level reviewer.
// Caller must hold exec.mu.
func (c *Component) dispatchRequirementReviewerLocked(ctx context.Context, exec *requirementExecution) {
	taskID := fmt.Sprintf("requirement-rev-%s-%s", exec.EntityID, uuid.New().String())
	exec.ReviewerTaskID = taskID

	// Create a worktree for the reviewer so it can access merged files.
	// Based on the requirement branch which has all approved node merges.
	if c.sandbox != nil && exec.RequirementBranch != "" {
		if _, err := c.sandbox.CreateWorktree(ctx, taskID, sandbox.WithBaseBranch(exec.RequirementBranch)); err != nil {
			c.logger.Warn("Failed to create reviewer worktree (reviewer will have limited file access)",
				"task_id", taskID, "branch", exec.RequirementBranch, "error", err)
		}
	}

	if err := c.sendReqPhase(ctx, exec.storeKey, phaseReviewing, map[string]any{
		"reviewer_task_id": taskID,
	}); err != nil {
		c.logger.Warn("Failed to send req.phase mutation", "stage", phaseReviewing, "error", err)
	}

	// Resolve requirement-reviewer model via capability registry.
	// config.ReviewerModel is honored as a hard override; the canonical path
	// is CapabilityReviewing → registry. Matches the pattern at
	// dispatchDeveloperLocked / dispatchCodeReviewer.
	reviewerModel := model.ResolveModel(c.modelRegistry, c.config.ReviewerModel, model.CapabilityReviewing)

	// Resolve the per-Story scoped scenarios ONCE so the user prompt
	// (buildReviewPrompt) and the system message (assembled.SystemMessage
	// via buildRequirementReviewContext) agree on the verdict surface.
	// Pre-fix the two sides could diverge, asking the reviewer to verify
	// scenarios that weren't in its prompt context.
	scopedScenarios := scopeScenariosToCurrentStory(exec)

	asmCtx := c.buildRequirementReviewContext(ctx, exec, reviewerModel, scopedScenarios)
	assembled := c.assembler.Assemble(asmCtx)

	var reviewerEndpoint *ssmodel.EndpointConfig
	if c.modelRegistry != nil {
		reviewerEndpoint = c.modelRegistry.GetEndpoint(reviewerModel)
	}

	task := &agentic.TaskMessage{
		TaskID: taskID,
		Role:   agentic.RoleReviewer,
		Model:  reviewerModel,
		// Filter the wire tool palette by RoleScenarioReviewer (req-level
		// reviewer is scenario-shaped — see buildRequirementReviewContext
		// at the symmetric prompt-side filter). Without this, the reviewer
		// sees review_scenario / web_search / http_request — terminals
		// from other roles that confuse small models. Same fix shape as
		// execution-manager applied take 11.
		Tools:        terminal.ToolsForEndpoint(c.toolRegistry, "review", reviewerEndpoint, prompt.FilterTools(availableToolNames(), prompt.RoleScenarioReviewer)...),
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: stageRequirementReview,
		Prompt:       c.buildReviewPrompt(exec, scopedScenarios),
		ToolChoice:   prompt.ResolveToolChoice(prompt.RoleScenarioReviewer, asmCtx.AvailableTools),
		Context: &agentic.ConstructedContext{
			Content: assembled.SystemMessage,
		},
		Metadata: map[string]any{
			"requirement_id":   exec.RequirementID,
			"plan_slug":        exec.Slug,
			"task_id":          taskID,
			"deliverable_type": "review",
			// role + model for SKG tool.recovery.incident partitioning.
			"role":  string(prompt.RoleScenarioReviewer),
			"model": reviewerModel,
		},
		ResponseFormat: terminal.ResponseFormatForEndpoint(reviewerEndpoint, "review"),
	}
	if err := c.publishTask(ctx, "agent.task.reviewer", task); err != nil {
		c.logger.Error("Failed to dispatch requirement reviewer", "error", err)
		c.markErrorLocked(ctx, exec, fmt.Sprintf("dispatch requirement reviewer: %v", err))
		return
	}

	c.logger.Info("Dispatched requirement reviewer",
		"entity_id", exec.EntityID,
		"task_id", taskID,
	)
}

// handleApprovedClaimMismatchLocked enforces the requirement-scope claim/
// observation cross-check. Defense-in-depth against the bug #9 pattern:
// even when execution-manager's mergeWorktree guard is in place, the
// requirement reviewer should not be the last word on completion if any
// node claimed files but produced no commit observation. Gated by
// config.RequireCommitObservation (default true; the upstream wiring is
// in place — execution-manager records MergeCommit on the task execution,
// req-executor surfaces it via the synthetic completion event Result, and
// handleNodeCompleteLocked populates NodeResult.CommitSHA).
//
// Returns true when the gate fired and the requirement was marked failed —
// caller must NOT proceed to markCompletedLocked.
//
// Caller must hold exec.mu.
func (c *Component) handleApprovedClaimMismatchLocked(ctx context.Context, exec *requirementExecution) bool {
	if !c.config.requireCommitObservation() {
		return false
	}
	var unobserved []string
	for _, nr := range exec.NodeResults {
		if len(nr.FilesModified) > 0 && nr.CommitSHA == "" {
			unobserved = append(unobserved, nr.NodeID)
		}
	}
	if len(unobserved) == 0 {
		return false
	}
	c.logger.Error("Requirement claim/observation mismatch — nodes claimed files but no commit observed",
		"entity_id", exec.EntityID,
		"unobserved_nodes", unobserved,
	)
	c.markFailedLocked(ctx, exec, fmt.Sprintf("requirement claim/observation mismatch: nodes %v claimed files_modified but produced no commit observation", unobserved))
	return true
}

// handleRequirementReviewerCompleteLocked processes the requirement reviewer verdict.
// The reviewer receives all scenarios as a checklist and returns per-scenario verdicts.
//
// On rejection:
//   - "fixable" + retry budget → map failed scenarios to dirty nodes, re-run only those
//   - "restructure" + retry budget → delete branch, re-decompose from scratch
//   - budget exhausted → terminal failure
//
// Caller must hold exec.mu.
func (c *Component) handleRequirementReviewerCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *requirementExecution) {

	if event.Outcome != agentic.OutcomeSuccess {
		c.markFailedLocked(ctx, exec, fmt.Sprintf("requirement reviewer failed: outcome=%s", event.Outcome))
		return
	}

	result, err := c.parseRequirementReviewResultLocked(exec, event.Result)
	if err != nil {
		c.handleInvalidRequirementReviewResultLocked(ctx, exec, err.Error())
		return
	}

	exec.ReviewVerdict = result.Verdict
	exec.ReviewFeedback = result.Feedback
	// Per-scenario verdicts are no longer used for node targeting (the
	// Story-gate retry re-runs the whole Story). Retained on the exec so Phase 3
	// can record which scenarios were deferred-and-noted (un-runnable tier)
	// vs genuinely failed.
	exec.ScenarioVerdicts = result.toScenarioVerdicts()

	if result.Verdict == "approved" {
		c.handleApprovedVerdictLocked(ctx, exec)
		return
	}

	// Rejected — check retry budget.
	if exec.RetryCount >= exec.MaxRetries || exec.MaxRetries == 0 {
		c.retriesExhausted.Add(1)
		// Emit a PlanDecision so the human has a record to act on. Best-effort:
		// if the publish fails we still mark the requirement failed so the plan
		// surfaces it via the attention banner + retry picker. The decision is
		// an additional signal, not the primary resolution path.
		c.emitExhaustionDecision(ctx, exec, result.Verdict, result.Feedback)
		failureReason := fmt.Sprintf("requirement rejected (retries exhausted): %s", result.Feedback)
		// ADR-037 race closure: emitExhaustionDecision publishes
		// RecoveryRequested. Defer terminal-failure so an accepted recovery
		// PlanDecision can revive this exec instead of arriving after the req
		// has already terminal-failed.
		if c.deferToAwaitingRecoveryLocked(ctx, exec, failureReason) {
			return
		}
		c.markFailedLocked(ctx, exec, failureReason)
		return
	}

	switch result.RejectionType {
	case "restructure":
		// Restructure discards the branch and re-synthesizes the DAG from
		// scratch — used when the Story's whole approach is wrong.
		c.startRestructureRetryLocked(ctx, exec, result.Feedback)
	default:
		// "fixable" — the Story's approach is sound but the implementation
		// doesn't yet satisfy the acceptance scenarios. Re-run the Story on the
		// existing branch with feedback (no scenario→node targeting; see
		// startFixableRetryLocked).
		c.startFixableRetryLocked(ctx, exec, result.Feedback)
	}
}

func (c *Component) parseRequirementReviewResultLocked(exec *requirementExecution, raw string) (requirementReviewResult, error) {
	var result requirementReviewResult
	if raw == "" {
		return result, fmt.Errorf("empty review result")
	}
	if err := json.Unmarshal([]byte(jsonutil.ExtractJSON(raw)), &result); err != nil {
		c.logger.Warn("Failed to parse requirement reviewer result", "entity_id", exec.EntityID, "error", err)
		return result, fmt.Errorf("parse review JSON: %w", err)
	}
	if err := phases.ValidateVerdict(result.Verdict); err != nil {
		c.logger.Warn("Invalid requirement reviewer verdict",
			"entity_id", exec.EntityID,
			"verdict", result.Verdict,
			"error", err,
		)
		return result, fmt.Errorf("invalid verdict: %s", result.Verdict)
	}
	if result.Verdict == "approved" {
		return result, nil
	}
	if result.RejectionType == "" {
		result.RejectionType = "fixable"
	}
	switch result.RejectionType {
	case "fixable", "restructure":
		// Both are valid. Neither needs scenario-level node targeting: fixable
		// re-runs the whole Story on the existing branch; restructure discards
		// the branch and re-synthesizes. (The old per-scenario targeting was
		// removed — every node carried the full Story scenario set, so it could
		// never localize a failure to a node.)
	default:
		return result, fmt.Errorf("invalid rejection_type: %s", result.RejectionType)
	}
	return result, nil
}

func (c *Component) handleInvalidRequirementReviewResultLocked(ctx context.Context, exec *requirementExecution, reason string) {
	maxRetries := c.config.MaxReviewRetries
	if maxRetries == 0 {
		maxRetries = 3
	}
	if exec.ReviewRetryCount < maxRetries {
		exec.ReviewRetryCount++
		c.logger.Info("Retrying requirement reviewer after invalid result",
			"entity_id", exec.EntityID,
			"attempt", exec.ReviewRetryCount,
			"max", maxRetries,
			"reason", reason,
		)
		c.dispatchRequirementReviewerLocked(ctx, exec)
		return
	}
	c.logger.Error("Requirement reviewer failed after max retries",
		"entity_id", exec.EntityID,
		"attempts", exec.ReviewRetryCount,
		"reason", reason,
	)
	c.markFailedLocked(ctx, exec, fmt.Sprintf("requirement reviewer returned invalid result after max retries: %s", reason))
}

// startFixableRetryLocked handles a fixable Story-gate rejection. Murat's
// verdict is at Story/scenario altitude; a failed scenario cannot be localized
// to a single DAG node because every node carries the full Story scenario set
// (synthesize_dag assigns ScenariosForStory to every node — there is no
// per-task scenario partition). So a fixable rejection re-runs the WHOLE Story
// DAG on the EXISTING branch — commits are preserved (unlike restructure, which
// deletes the branch and re-synthesizes) and the reviewer feedback is threaded
// into each node prompt so the dev fixes forward against the existing code.
//
// This is the coarse BMAD quality-gate behaviour: a Story PASSES or RETURNS for
// rework. Subset targeting would require Sarah-authored per-task scenario
// partitions (deliberately not added). A single node that ERRORS mid-DAG is a
// different case — that IS localizable and uses retryNodeAtRequirementLevelLocked.
// Caller must hold exec.mu.
func (c *Component) startFixableRetryLocked(ctx context.Context, exec *requirementExecution, feedback string) {
	exec.RetryCount++
	exec.LastReviewFeedback = feedback
	exec.ReviewRetryCount = 0 // reset reviewer parse-retry budget for new attempt
	exec.terminated = false   // allow new terminal write

	// Re-run every node (no scenario→node targeting). Preserve the branch.
	exec.DirtyNodeIDs = nil
	exec.VisitedNodes = make(map[string]bool)
	exec.NodeResults = nil
	exec.CurrentNodeIdx = -1

	// Mirror the NodeResults reset to KV — it is append-only via
	// handleReqNodeMutation, so without this stale entries reappear on the
	// next restart via rebuildExecFromKV. Best-effort.
	if err := c.sendReqResetNodeResults(ctx, exec.storeKey); err != nil {
		c.logger.Warn("Failed to reset KV NodeResults on fixable retry",
			"entity_id", exec.EntityID, "error", err)
	}

	if err := c.sendReqPhase(ctx, exec.storeKey, phaseExecuting, map[string]any{
		"retry_count": exec.RetryCount,
	}); err != nil {
		c.logger.Warn("Failed to send req.phase mutation for retry", "error", err)
	}

	c.logger.Info("Starting fixable retry — re-running Story on existing branch",
		"entity_id", exec.EntityID,
		"retry_count", exec.RetryCount,
		"nodes", len(exec.SortedNodeIDs),
		"feedback", feedback,
	)

	c.dispatchNextNodeLocked(ctx, exec)
}

// startRestructureRetryLocked handles a "restructure" rejection by deleting
// the requirement branch and re-decomposing from scratch. All prior work is
// discarded — only the reviewer's feedback carries forward.
// Caller must hold exec.mu.
func (c *Component) startRestructureRetryLocked(ctx context.Context, exec *requirementExecution, feedback string) {
	exec.RetryCount++
	exec.LastReviewFeedback = feedback
	exec.terminated = false

	// Delete the old branch to avoid polluted context.
	if c.sandbox != nil && exec.RequirementBranch != "" {
		if err := c.sandbox.DeleteBranch(ctx, exec.RequirementBranch); err != nil {
			c.logger.Warn("Failed to delete old requirement branch",
				"branch", exec.RequirementBranch, "error", err)
		}
		// Create a fresh branch.
		if err := c.sandbox.CreateBranch(ctx, exec.RequirementBranch, "HEAD"); err != nil {
			c.logger.Warn("Failed to recreate requirement branch",
				"branch", exec.RequirementBranch, "error", err)
		}
	}

	// Reset all DAG state.
	exec.DAG = nil
	exec.SortedNodeIDs = nil
	exec.NodeIndex = nil
	exec.CurrentNodeIdx = -1
	exec.CurrentNodeTaskID = ""
	exec.VisitedNodes = make(map[string]bool)
	exec.NodeResults = nil
	// KV NodeResults is append-only; wipe it to mirror the in-memory reset
	// or stale entries from the prior cycle reappear on the next restart.
	// Closes Pass-1 H4 for the restructure-retry path.
	if err := c.sendReqResetNodeResults(ctx, exec.storeKey); err != nil {
		c.logger.Warn("Failed to wipe KV NodeResults on restructure retry",
			"entity_id", exec.EntityID, "error", err)
	}
	exec.DirtyNodeIDs = nil
	exec.ReviewVerdict = ""
	exec.ReviewFeedback = ""
	exec.ReviewRetryCount = 0
	exec.ScenarioVerdicts = nil

	if err := c.sendReqPhase(ctx, exec.storeKey, phaseDecomposing, map[string]any{
		"retry_count": exec.RetryCount,
		"restructure": true,
	}); err != nil {
		c.logger.Warn("Failed to send req.phase mutation for restructure", "error", err)
	}

	c.logger.Info("Starting restructure retry — re-decomposing from scratch",
		"entity_id", exec.EntityID,
		"retry_count", exec.RetryCount,
		"feedback", feedback,
	)

	c.dispatchSynthesizerLocked(ctx, exec)
}

// isNodeDirty returns true if a node should be re-executed on retry.
// With an empty DirtyNodeIDs (first attempt, Story-gate fixable retry, or
// restructure) every node re-runs. DirtyNodeIDs is set only by the single-node
// mid-DAG error path (retryNodeAtRequirementLevelLocked), which CAN localize
// the failure; the Story-gate fixable retry cannot, so it re-runs all nodes.
func (c *Component) isNodeDirty(exec *requirementExecution, nodeID string) bool {
	if exec.RetryCount == 0 || len(exec.DirtyNodeIDs) == 0 {
		return true // first attempt or no dirty list — all nodes are "dirty"
	}
	for _, dirty := range exec.DirtyNodeIDs {
		if dirty == nodeID {
			return true
		}
	}
	return false
}

// buildRequirementReviewContext assembles the prompt context for requirement-level review.
// reviewerModel is the model the reviewer dispatch will hit; it determines
// HasResponseFormat so the assembler can elide schema prose when the
// endpoint honors response_format. scoped is the per-Story scenario subset
// the caller resolved via scopeScenariosToCurrentStory — passing it through
// (rather than recomputing inside) guarantees buildReviewPrompt and this
// function see the SAME verdict surface, which is the load-bearing contract
// behind go-reviewer Pass-1 C5.
func (c *Component) buildRequirementReviewContext(ctx context.Context, exec *requirementExecution, reviewerModel string, scoped []workflow.Scenario) *prompt.AssemblyContext {
	var endpoint *ssmodel.EndpointConfig
	if c.modelRegistry != nil {
		endpoint = c.modelRegistry.GetEndpoint(reviewerModel)
	}
	rc := &prompt.ScenarioReviewContext{
		FilesModified: c.aggregateFiles(exec),
		NodeResults:   c.buildNodeSummaries(exec),
	}

	// ADR-043 PR 4h: per-Story reviewer scope. Filter the requirement's
	// scenarios down to the current Story's scenarios — that is the verdict
	// surface for THIS reviewer dispatch. Other Stories' scenarios are out
	// of scope (they'll get their own reviewer pass).
	if len(scoped) > 0 {
		specs := make([]prompt.ScenarioSpec, len(scoped))
		for i, s := range scoped {
			specs[i] = prompt.ScenarioSpec{
				ID:    s.ID,
				Given: s.Given,
				When:  s.When,
				Then:  s.Then,
			}
		}
		rc.Scenarios = specs
	}

	// On retry, include prior rejection feedback so the reviewer can note improvements.
	if exec.RetryCount > 0 && exec.LastReviewFeedback != "" {
		rc.RetryFeedback = exec.LastReviewFeedback
	}

	asmCtx := &prompt.AssemblyContext{
		Role:                  prompt.RoleScenarioReviewer,
		Provider:              resolveProvider(reviewerModel),
		Domain:                "software",
		AvailableTools:        prompt.FilterTools(availableToolNames(), prompt.RoleScenarioReviewer),
		SupportsTools:         true,
		ScenarioReviewContext: rc,
		Persona:               prompt.GlobalPersonas().ForRole(prompt.RoleScenarioReviewer),
		Vocabulary:            prompt.GlobalPersonas().Vocabulary(),
		HasResponseFormat:     terminal.EndpointSupportsResponseFormat(endpoint),
	}

	// Symmetric context with the per-task reviewer: project standards + rotated
	// team lessons (the Story gate used to judge in isolation — go-reviewer #3).
	if c.standards != nil {
		asmCtx.Standards = prompt.NewStandardsContext(c.standards.ForRole(string(prompt.RoleScenarioReviewer)))
	}
	asmCtx.LessonsLearned = c.rotateReviewLessons(ctx)

	// Plan/requirement framing + architecture so Murat judges scenarios in
	// context, not in isolation. Best-effort: a missing plan just leaves the
	// fields empty.
	if plan, err := c.loadPlanFromKV(ctx, exec.Slug); err == nil && plan != nil {
		rc.PlanTitle = plan.Title
		rc.PlanGoal = plan.Goal
		rc.ArchitectureContext = prompt.FormatArchitectureContext(prompt.ProjectArchitecture(plan.Architecture))
		for _, r := range plan.Requirements {
			if r.ID == exec.RequirementID {
				rc.RequirementTitle = r.Title
				break
			}
		}
	}

	return asmCtx
}

// rotateReviewLessons returns the role-scoped team lessons for the Story gate,
// or nil when the lesson writer is unset or there are none. Mirrors the
// per-task reviewer's lesson injection in execution-manager.
func (c *Component) rotateReviewLessons(ctx context.Context) *prompt.LessonsLearned {
	if c.lessonWriter == nil {
		return nil
	}
	graphCtx := context.WithoutCancel(ctx)
	entries, err := c.lessonWriter.RotateLessonsForRole(graphCtx, string(prompt.RoleScenarioReviewer), 10)
	if err != nil || len(entries) == 0 {
		return nil
	}
	tk := &prompt.LessonsLearned{}
	for _, les := range entries {
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
	return tk
}

// initReviewKnowledge loads the project standards + error categories and wires
// the lesson writer so the Story-gate (Murat) review has the same knowledge
// surface as the per-task reviewer. Best-effort: missing files just leave the
// gate without that context.
//
// Called from the factory (not Start) deliberately: it only reads disk and
// constructs a lazy lessons.Writer over the already-set tripleWriter — no NATS
// I/O or KV access happens here, so there is no Start-ordering dependency. Do
// not "align" this to execution-manager's Start-time init without that in mind.
func (c *Component) initReviewKnowledge() {
	c.lessonWriter = &lessons.Writer{TW: c.tripleWriter, Logger: c.logger}

	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		repoRoot, _ = os.Getwd()
	}
	catPath := filepath.Join(repoRoot, "configs", "error_categories.json")
	if reg, err := workflow.LoadErrorCategories(catPath); err != nil {
		c.logger.Debug("Failed to load error categories — lesson guidance disabled", "error", err)
	} else {
		c.errorCategories = reg
	}
	if stds := workflow.LoadStandardsFromDisk(repoRoot); stds != nil && len(stds.Items) > 0 {
		c.standards = stds
	}
}

// handleApprovedVerdictLocked routes an approved reviewer verdict. ADR-043
// PR 4h split this off handleRequirementReviewerCompleteLocked to keep the
// outer function under the function-length lint cap and to localize the
// per-Story advancement decision.
//
// Caller must hold exec.mu.
func (c *Component) handleApprovedVerdictLocked(ctx context.Context, exec *requirementExecution) {
	if c.handleApprovedClaimMismatchLocked(ctx, exec) {
		return
	}
	// ADR-044: record the per-Story terminal Status. Best-effort — claim
	// rejection here is non-fatal because the requirement-level advancement
	// is what gates downstream processing. Skipped when natsClient is nil
	// (unit-test components without dispatch substrate).
	c.publishStoryCompleteLocked(ctx, exec)

	// Per-Story reviewer: if more Stories remain, advance the cursor and
	// dispatch the next Story's DAG; otherwise mark the whole requirement
	// complete.
	if exec.CurrentStoryIdx+1 < len(exec.SortedStoryIDs) {
		c.advanceToNextStoryLocked(ctx, exec)
		return
	}
	c.markCompletedLocked(ctx, exec)
}

// publishStoryCompleteLocked tries to transition the current Story's
// Status to complete via the plan-manager mutation. Best-effort: a
// rejection is logged at Debug and does not block requirement-level
// advancement. Used by both approved-verdict path (reservation cleanup
// + state surface for QA-reviewer's capability evidence rollup) and the
// recovery path when a Story-level reservation is being released.
//
// Caller must hold exec.mu.
func (c *Component) publishStoryCompleteLocked(ctx context.Context, exec *requirementExecution) {
	if c.natsClient == nil || exec.CurrentStoryIdx >= len(exec.SortedStoryIDs) {
		return
	}
	storyID := exec.SortedStoryIDs[exec.CurrentStoryIdx]
	workflow.ClaimStoryStatus(ctx, c.natsClient, exec.Slug, storyID, workflow.StoryStatusComplete, c.logger)
}

// advanceToNextStoryLocked increments CurrentStoryIdx, clears per-Story
// state (DAG / node bookkeeping / reviewer verdict / retry counters), and
// dispatches the next Story's DAG. NodeResults accumulate across Stories
// so the requirement-final aggregateFiles call still sees every node's
// output for the merge-commit cross-check.
//
// Caller must hold exec.mu.
func (c *Component) advanceToNextStoryLocked(ctx context.Context, exec *requirementExecution) {
	exec.CurrentStoryIdx++

	// Per-Story state reset. NodeResults intentionally NOT reset — they
	// feed the requirement-level claim/observation cross-check at
	// markCompletedLocked.
	resetPerStoryExecutionState(exec)

	nextStoryID := exec.SortedStoryIDs[exec.CurrentStoryIdx]
	c.logger.Info("Advancing to next Story",
		"entity_id", exec.EntityID,
		"requirement_id", exec.RequirementID,
		"next_story_id", nextStoryID,
		"story_idx", exec.CurrentStoryIdx+1,
		"story_total", len(exec.SortedStoryIDs))

	plan, err := c.loadPlanFromKV(ctx, exec.Slug)
	if err != nil {
		c.markFailedLocked(ctx, exec, fmt.Sprintf("load plan from KV failed mid-requirement: %v", err))
		return
	}
	if plan == nil {
		c.markFailedLocked(ctx, exec, "plan disappeared from PLAN_STATES mid-requirement")
		return
	}

	c.dispatchCurrentStoryLocked(ctx, exec, plan)
}

func resetPerStoryExecutionState(exec *requirementExecution) {
	exec.DAG = nil
	exec.SortedNodeIDs = nil
	exec.NodeIndex = nil
	exec.CurrentNodeIdx = -1
	exec.CurrentNodeTaskID = ""
	exec.VisitedNodes = make(map[string]bool)
	exec.ReviewVerdict = ""
	exec.ReviewFeedback = ""
	exec.ReviewRetryCount = 0
	exec.RetryCount = 0
	exec.DirtyNodeIDs = nil
	exec.ScenarioVerdicts = nil
}

// filterScenariosByIDs returns the subset of scenarios whose IDs match the
// provided ID list. Preserves the order of the input scenarios slice (not
// the ID list). Returns nil when ids is empty so the caller can decide to
// omit the field from the dispatch payload entirely. Used by
// dispatchNextNodeLocked to scope a DAG node's scenarios from the parent
// requirement's full set before sending the TaskCreateRequest — the
// per-task developer + code-reviewer prompts then ground in just the
// scenarios this node is responsible for.
func filterScenariosByIDs(scenarios []workflow.Scenario, ids []string) []workflow.Scenario {
	if len(ids) == 0 || len(scenarios) == 0 {
		return nil
	}
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	out := make([]workflow.Scenario, 0, len(ids))
	for _, s := range scenarios {
		if _, ok := wanted[s.ID]; ok {
			out = append(out, s)
		}
	}
	return out
}

// scopeScenariosToCurrentStory returns the subset of exec.Scenarios whose
// StoryID matches the Story currently being executed (per CurrentStoryIdx).
// When no Stories are tracked or the current Story ID can't be resolved,
// falls back to all exec.Scenarios — preserves pre-ADR-043 behavior for
// legacy plans that reach the reviewer without SortedStoryIDs populated.
func scopeScenariosToCurrentStory(exec *requirementExecution) []workflow.Scenario {
	if exec == nil {
		return nil
	}
	if exec.CurrentStoryIdx < 0 || exec.CurrentStoryIdx >= len(exec.SortedStoryIDs) {
		return exec.Scenarios
	}
	currentStoryID := exec.SortedStoryIDs[exec.CurrentStoryIdx]
	if currentStoryID == "" {
		return exec.Scenarios
	}
	out := make([]workflow.Scenario, 0, len(exec.Scenarios))
	for _, s := range exec.Scenarios {
		if s.StoryID == currentStoryID {
			out = append(out, s)
		}
	}
	return out
}

// resolveProvider maps a model string to a prompt.Provider.
func resolveProvider(modelStr string) prompt.Provider {
	switch {
	case strings.Contains(modelStr, "claude"):
		return prompt.ProviderAnthropic
	case strings.Contains(modelStr, "gpt"),
		strings.Contains(modelStr, "o1"),
		strings.Contains(modelStr, "o3"):
		return prompt.ProviderOpenAI
	default:
		return prompt.ProviderOllama
	}
}

// availableToolNames returns the full tool list the component knows about.
func availableToolNames() []string {
	// review_scenario was registered for the legacy scenario-reviewer
	// terminal that was deleted; dropped 2026-05-08 take-14 follow-up.
	// decompose_task retired with ADR-043 PR 4g (synthesis from Sarah-
	// prepared Stories now drives DAG construction; no LLM tool call).
	return []string{
		"bash", "submit_work", "ask_question",
		"graph_search", "graph_query", "graph_summary",
		"web_search", "http_request",
		"scratchpad", "write_todos",
	}
}

// aggregateFiles deduplicates files modified across all completed nodes.
func (c *Component) aggregateFiles(exec *requirementExecution) []string {
	seen := make(map[string]bool)
	var files []string
	for _, nr := range exec.NodeResults {
		for _, f := range nr.FilesModified {
			if !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
	}
	return files
}

// aggregateNodeSummaries concatenates per-node summaries into a single string.
func (c *Component) aggregateNodeSummaries(exec *requirementExecution) string {
	var parts []string
	for _, nr := range exec.NodeResults {
		if nr.Summary != "" {
			parts = append(parts, fmt.Sprintf("[%s] %s", nr.NodeID, nr.Summary))
		}
	}
	return strings.Join(parts, "; ")
}

// buildNodeSummaries converts NodeResult slice to prompt.NodeResultSummary slice.
func (c *Component) buildNodeSummaries(exec *requirementExecution) []prompt.NodeResultSummary {
	summaries := make([]prompt.NodeResultSummary, len(exec.NodeResults))
	for i, nr := range exec.NodeResults {
		summaries[i] = prompt.NodeResultSummary{
			NodeID:  nr.NodeID,
			Summary: nr.Summary,
			Files:   nr.FilesModified,
		}
	}
	return summaries
}

func workflowNodeResults(results []NodeResult) []workflow.NodeResult {
	if len(results) == 0 {
		return nil
	}
	out := make([]workflow.NodeResult, 0, len(results))
	for _, nr := range results {
		out = append(out, workflow.NodeResult{
			NodeID:        nr.NodeID,
			FilesModified: append([]string(nil), nr.FilesModified...),
			Summary:       nr.Summary,
			CommitSHA:     nr.CommitSHA,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Terminal state handlers
// ---------------------------------------------------------------------------

// markCompletedLocked transitions to the completed terminal state.
// Caller must hold exec.mu.
func (c *Component) markCompletedLocked(ctx context.Context, exec *requirementExecution) {
	if exec.terminated {
		return
	}
	exec.terminated = true

	nodeCount := len(exec.NodeResults)
	if nodeCount == 0 {
		nodeCount = len(exec.VisitedNodes)
	}
	fields := map[string]any{
		"node_count": nodeCount,
	}
	if nodeResults := workflowNodeResults(exec.NodeResults); len(nodeResults) > 0 {
		fields["node_results"] = nodeResults
	}

	if err := c.sendReqPhase(ctx, exec.storeKey, phaseCompleted, fields); err != nil {
		c.logger.Warn("Failed to send req.phase mutation", "stage", phaseCompleted, "error", err)
	}

	c.requirementsCompleted.Add(1)

	c.logger.Info("Requirement execution completed",
		"entity_id", exec.EntityID,
		"slug", exec.Slug,
		"requirement_id", exec.RequirementID,
		"nodes_completed", nodeCount,
	)

	c.publishRequirementCompleteEvent(ctx, exec, "completed")
	c.publishEntity(context.Background(), NewRequirementExecutionEntity(exec).WithPhase(phaseCompleted))
	c.cleanupExecutionLocked(exec, true)
}

// emitExhaustionDecision sends a PlanDecision to plan-manager recording
// that this requirement exhausted its retry budget. The decision is a
// human-attention record; the remedy (retry with different model,
// force-complete, reject plan) is taken via existing endpoints. Fire-and-
// forget: a publish failure logs but does not block the failed-state
// transition that follows. Caller must still hold exec.mu.
func (c *Component) emitExhaustionDecision(ctx context.Context, exec *requirementExecution, verdict, feedback string) {
	if c.natsClient == nil {
		return
	}
	now := time.Now()
	shortID := exec.RequirementID
	if len(shortID) > 16 {
		shortID = shortID[len(shortID)-16:]
	}
	decisionID := fmt.Sprintf("plan-decision.%s.exhaust.%s.%d",
		exec.Slug, shortID, now.UnixNano())

	title := fmt.Sprintf("Requirement %s exhausted retries", exec.RequirementID)
	rationale := fmt.Sprintf(
		"retries=%d/%d last_verdict=%q last_feedback=%s",
		exec.RetryCount, exec.MaxRetries, verdict, feedback,
	)

	// Capture trajectory pointers so the UI can deep-link the human reviewer
	// to the actual LLM calls that exhausted the budget.
	var artifacts []workflow.ArtifactRef
	if exec.LoopID != "" {
		artifacts = append(artifacts, workflow.ArtifactRef{
			Path:    fmt.Sprintf("/trajectories/%s", exec.LoopID),
			Type:    "trajectory",
			Purpose: fmt.Sprintf("Final loop on requirement %s", exec.RequirementID),
		})
	}

	decision := workflow.PlanDecision{
		ID:                 decisionID,
		PlanID:             workflow.PlanEntityID(exec.Slug),
		Kind:               workflow.PlanDecisionKindExecutionExhausted,
		Title:              title,
		Rationale:          rationale,
		Status:             workflow.PlanDecisionStatusProposed,
		ProposedBy:         "requirement-executor",
		AffectedReqIDs:     []string{exec.RequirementID},
		ArtifactReferences: artifacts,
		CreatedAt:          now,
	}

	if err := c.sendPlanDecisionAdd(ctx, exec.Slug, decision); err != nil {
		c.logger.Warn("Failed to emit exhaustion PlanDecision",
			"entity_id", exec.EntityID,
			"requirement_id", exec.RequirementID,
			"error", err,
		)
		return
	}
	c.logger.Info("Emitted exhaustion PlanDecision",
		"entity_id", exec.EntityID,
		"requirement_id", exec.RequirementID,
		"decision_id", decisionID,
	)

	// ADR-037 stage 1: fire phase-local recovery alongside the PlanDecision
	// emit. Caught 2026-05-10 take 4 — requirement-level retry-exhaustion
	// is the third escalation route (alongside plan-manager.escalateRevision
	// and execution-manager.markEscalatedLocked). The req-reviewer produces
	// the richest diagnoses across the codebase (3 rounds × full sub-task
	// trajectories) but Stage 1 was missing the publish here, so all that
	// signal landed in the proposed PlanDecision and stopped. Wiring this
	// publish gives the recovery agent the same chance to dispatch a
	// manager-role diagnosis here as on the other two routes.
	// Forward the exec's Story cursor so the recovery-agent can reach back
	// to Sarah's layer when its diagnosis points at story-shaping (ADR-043
	// PR 4i + Train C). Empty in legacy / single-Story plans, which is
	// fine — recovery-agent won't propose story_reprepare without Stories
	// in scope.
	var affectedStories []string
	if len(exec.SortedStoryIDs) > 0 {
		affectedStories = append(affectedStories, exec.SortedStoryIDs...)
	}

	c.publishRecoveryRequested(ctx, &payloads.RecoveryRequested{
		RecoveryID:          uuid.New().String(),
		Layer:               payloads.RecoveryLayerPhaseLocal,
		Slug:                exec.Slug,
		RequirementID:       exec.RequirementID,
		AffectedStoryIDs:    affectedStories,
		LoopID:              exec.LoopID,
		EscalationReason:    fmt.Sprintf("requirement retries exhausted (%d/%d); last verdict=%q", exec.RetryCount, exec.MaxRetries, verdict),
		LastFailureFeedback: feedback,
	})
}

// publishRecoveryRequested fires an ADR-037 stage-1 phase-local recovery
// request on recovery.requested.<slug>. Best-effort: failure does not roll
// back the exhaustion (the PlanDecision is already emitted and the req
// will move to failed regardless). The recovery-agent component consumes
// these and, on submit_work, emits RecoveryComplete on
// recovery.complete.<slug> for the watcher to reconcile.
func (c *Component) publishRecoveryRequested(ctx context.Context, req *payloads.RecoveryRequested) {
	if c.natsClient == nil {
		return
	}
	if err := req.Validate(); err != nil {
		c.logger.Warn("Recovery request failed local validation; skipping publish",
			"slug", req.Slug, "requirement_id", req.RequirementID, "error", err)
		return
	}
	baseMsg := message.NewBaseMessage(req.Schema(), req, "requirement-executor")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Warn("Failed to marshal RecoveryRequested",
			"slug", req.Slug, "requirement_id", req.RequirementID, "error", err)
		return
	}
	subject := payloads.RecoveryRequestedSubjectPrefix + req.Slug
	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		c.logger.Warn("Failed to publish RecoveryRequested",
			"slug", req.Slug, "subject", subject, "error", err)
		return
	}
	c.logger.Info("Recovery requested (phase-local, req-level exhaustion)",
		"slug", req.Slug,
		"requirement_id", req.RequirementID,
		"recovery_id", req.RecoveryID,
		"reason", req.EscalationReason)
}

// markFailedLocked transitions to the failed terminal state.
// Caller must hold exec.mu.
func (c *Component) markFailedLocked(ctx context.Context, exec *requirementExecution, reason string) {
	if exec.terminated {
		return
	}
	exec.terminated = true

	if err := c.sendReqPhase(ctx, exec.storeKey, phaseFailed, map[string]any{
		"error_reason": reason,
	}); err != nil {
		c.logger.Warn("Failed to send req.phase mutation", "stage", phaseFailed, "error", err)
	}

	// ADR-044: release the current Story's reservation by writing Failed.
	// Best-effort; non-fatal if the claim is rejected (e.g., transition
	// from Executing → Failed is valid but from Pending → Failed too;
	// CanTransitionTo permits both per workflow/story_task.go state machine).
	if c.natsClient != nil && exec.CurrentStoryIdx >= 0 && exec.CurrentStoryIdx < len(exec.SortedStoryIDs) {
		storyID := exec.SortedStoryIDs[exec.CurrentStoryIdx]
		workflow.ClaimStoryStatus(ctx, c.natsClient, exec.Slug, storyID, workflow.StoryStatusFailed, c.logger)
	}

	c.requirementsFailed.Add(1)

	c.logger.Error("Requirement execution failed",
		"entity_id", exec.EntityID,
		"slug", exec.Slug,
		"requirement_id", exec.RequirementID,
		"reason", reason,
	)

	c.publishRequirementCompleteEvent(ctx, exec, "failed")
	c.publishEntity(context.Background(), NewRequirementExecutionEntity(exec).WithPhase(phaseFailed).WithFailureReason(reason))
	c.cleanupExecutionLocked(exec, false)
}

// publishRequirementCompleteEvent publishes a typed RequirementExecutionCompleteEvent
// to the WORKFLOW stream for downstream consumption (plan-api).
func (c *Component) publishRequirementCompleteEvent(ctx context.Context, exec *requirementExecution, outcome string) {
	// Aggregate files modified across all nodes, deduplicating.
	var allFiles []string
	seen := make(map[string]bool)
	for _, nr := range exec.NodeResults {
		for _, f := range nr.FilesModified {
			if !seen[f] {
				seen[f] = true
				allFiles = append(allFiles, f)
			}
		}
	}

	// Count scenarios passed (those with a "passing" verdict, or all if no explicit verdicts).
	scenariosPassed := len(exec.Scenarios) // default: assume all passed when approved
	if outcome != "completed" {
		scenariosPassed = 0
	}

	// Build summary from aggregate node summaries.
	summary := c.aggregateNodeSummaries(exec)
	nodeCount := len(exec.NodeResults)
	if nodeCount == 0 {
		nodeCount = len(exec.VisitedNodes)
	}

	event := workflow.RequirementExecutionCompleteEvent{
		Slug:            exec.Slug,
		RequirementID:   exec.RequirementID,
		Title:           exec.Title,
		Description:     exec.Description,
		ProjectID:       exec.ProjectID,
		TraceID:         exec.TraceID,
		Outcome:         outcome,
		NodeCount:       nodeCount,
		FilesModified:   allFiles,
		Summary:         summary,
		ScenariosTotal:  len(exec.Scenarios),
		ScenariosPassed: scenariosPassed,
	}

	if c.natsClient == nil {
		return
	}

	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Warn("Failed to get JetStream for requirement completion event", "error", err)
		return
	}

	data, err := json.Marshal(event)
	if err != nil {
		c.logger.Warn("Failed to marshal requirement completion event", "error", err)
		return
	}

	if _, err := js.Publish(ctx, workflow.RequirementExecutionComplete.Pattern, data); err != nil {
		c.logger.Warn("Failed to publish requirement completion event",
			"entity_id", exec.EntityID,
			"outcome", outcome,
			"error", err,
		)
	}
}

// markErrorLocked transitions to the error terminal state.
// Caller must hold exec.mu.
func (c *Component) markErrorLocked(ctx context.Context, exec *requirementExecution, reason string) {
	if exec.terminated {
		return
	}
	exec.terminated = true

	errClass := workflow.ClassifyErrorReason(reason)
	if err := c.sendReqPhase(ctx, exec.storeKey, phaseError, map[string]any{
		"error_reason": reason,
		"error_class":  errClass,
	}); err != nil {
		c.logger.Warn("Failed to send req.phase mutation", "stage", phaseError, "error", err)
	}

	c.errors.Add(1)

	c.logger.Error("Requirement execution error",
		"entity_id", exec.EntityID,
		"slug", exec.Slug,
		"requirement_id", exec.RequirementID,
		"reason", reason,
	)

	c.publishRequirementCompleteEvent(ctx, exec, "error")
	c.publishEntity(context.Background(), NewRequirementExecutionEntity(exec).WithPhase(phaseError).WithErrorReason(reason))
	c.cleanupExecutionLocked(exec, false)
}

// cleanupExecutionLocked removes execution from maps and cancels timeout.
// Caller must hold exec.mu.
//
// success=true is the happy path (all nodes merged), where we delete every
// node worktree plus our reviewer worktree. success=false is the failure/
// error path: in-flight execution-manager loops may still be running against
// node worktrees, so deleting them races with live work and produces silent
// merge failures. Leave node worktrees behind on failure and let sandbox's
// stale-cleanup loop reclaim them (cmd/sandbox/cleanup.go, cleanup-age=24h).
// Reviewer worktree is owned solely by this component and is always safe to
// remove.
func (c *Component) cleanupExecutionLocked(exec *requirementExecution, success bool) {
	if exec.timeoutTimer != nil {
		exec.timeoutTimer.stop()
	}
	if exec.recoveryTimer != nil {
		exec.recoveryTimer.stop()
		exec.recoveryTimer = nil
	}

	if c.sandbox != nil {
		var worktreeIDs []string
		if success {
			worktreeIDs = append(worktreeIDs, exec.NodeTaskIDs...)
		}
		if exec.ReviewerTaskID != "" {
			worktreeIDs = append(worktreeIDs, exec.ReviewerTaskID)
		}
		for _, id := range worktreeIDs {
			if err := c.sandbox.DeleteWorktree(context.Background(), id); err != nil {
				c.logger.Debug("Worktree cleanup failed (may already be deleted)",
					"task_id", id, "error", err)
			}
		}
		if !success && len(exec.NodeTaskIDs) > 0 {
			c.logger.Info("Leaving node worktrees for sandbox GC (failure path)",
				"entity_id", exec.EntityID,
				"requirement_id", exec.RequirementID,
				"node_count", len(exec.NodeTaskIDs))
		}
	}

	c.activeExecs.Delete(exec.EntityID) //nolint:errcheck // best-effort cache cleanup
}

// ---------------------------------------------------------------------------
// Per-execution timeout
// ---------------------------------------------------------------------------

// startExecutionTimeoutLocked starts a timer that marks the execution as errored
// if it does not complete within the configured timeout.
// Caller must hold exec.mu.
func (c *Component) startExecutionTimeoutLocked(exec *requirementExecution) {
	timeout := c.config.GetTimeout()

	timer := time.AfterFunc(timeout, func() {
		c.logger.Warn("Requirement execution timed out",
			"entity_id", exec.EntityID,
			"slug", exec.Slug,
			"requirement_id", exec.RequirementID,
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
// Triple and task publishing helpers
// ---------------------------------------------------------------------------

// publishDAGNodes publishes all nodes in the DAG as graph entities with
// status="pending".  Publishing is best-effort: failures are logged as
// warnings and do not abort execution.
func (c *Component) publishDAGNodes(ctx context.Context, exec *requirementExecution) {
	executionID := fmt.Sprintf("%s-%s", exec.Slug, exec.RequirementID)
	for i := range exec.DAG.Nodes {
		node := &exec.DAG.Nodes[i]
		entity := newDAGNodeEntity(executionID, node, exec.EntityID)
		c.publishEntity(ctx, entity)
	}
}

// publishDAGNodeStatus updates the DAGNodeStatus triple for a single node by
// re-publishing its full entity payload with the new status.  Publishing is
// best-effort: failures are logged as warnings and do not abort execution.
func (c *Component) publishDAGNodeStatus(ctx context.Context, exec *requirementExecution, nodeID, status string) {
	node, ok := exec.NodeIndex[nodeID]
	if !ok {
		c.logger.Warn("publishDAGNodeStatus: node not found in index",
			"entity_id", exec.EntityID, "node_id", nodeID)
		return
	}
	executionID := fmt.Sprintf("%s-%s", exec.Slug, exec.RequirementID)
	entity := newDAGNodeEntity(executionID, node, exec.EntityID).withStatus(status)
	c.publishEntity(ctx, entity)
}

// publishTask wraps a TaskMessage in a BaseMessage and publishes to JetStream.
// Returns an error for fail-fast dispatch.
func (c *Component) publishTask(ctx context.Context, subject string, task *agentic.TaskMessage) error {
	baseMsg := message.NewBaseMessage(task.Schema(), task, componentName)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal task message: %w", err)
	}

	if c.natsClient != nil {
		if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
			return fmt.Errorf("publish to %s: %w", subject, err)
		}
	}
	return nil
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
		Description: "Orchestrates per-requirement execution: decompose → serial task execution",
		Version:     componentVersion,
	}
}

// InputPorts returns the component's declared input ports.
func (c *Component) InputPorts() []component.Port { return c.inputPorts }

// OutputPorts returns the component's declared output ports.
func (c *Component) OutputPorts() []component.Port { return c.outputPorts }

// ConfigSchema returns the JSON schema for this component's configuration.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return requirementExecutorSchema
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
