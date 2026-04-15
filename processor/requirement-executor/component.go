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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/prompt"
	promptdomain "github.com/c360studio/semspec/prompt/domain"
	"github.com/c360studio/semspec/tools/decompose"
	"github.com/c360studio/semspec/tools/sandbox"
	"github.com/c360studio/semspec/tools/terminal"
	workflowtools "github.com/c360studio/semspec/tools/workflow"
	wf "github.com/c360studio/semspec/vocabulary/workflow"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
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
	stageDecompose          = "decompose"
	stageRequirementRedTeam = "requirement-red-team"
	stageRequirementReview  = "requirement-review"

	// Phase values written to entity triples.
	phaseDecomposing = "decomposing"
	phaseExecuting   = "executing"
	phaseCompleted   = "completed"
	phaseFailed      = "failed"
	phaseError       = "error"
	phaseRedTeaming  = "red_teaming"
	phaseReviewing   = "reviewing"

	// NATS subjects.
	subjectDecomposer = "agent.task.development"
)

// Component orchestrates per-requirement execution.
type Component struct {
	config       Config
	natsClient   *natsclient.Client
	logger       *slog.Logger
	platform     component.PlatformMeta
	tripleWriter *graphutil.TripleWriter
	sandbox      *sandbox.Client   // nil when sandbox is disabled
	assembler    *prompt.Assembler // composes system prompts for requirement-level review

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
	errors                atomic.Int64
	lastActivityMu        sync.RWMutex
	lastActivity          time.Time
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

	c := &Component{
		config:     cfg,
		natsClient: deps.NATSClient,
		logger:     logger,
		platform:   deps.Platform,
		sandbox:    sandbox.NewClient(cfg.SandboxURL),
		assembler:  prompt.NewAssembler(registry),
		tripleWriter: &graphutil.TripleWriter{
			NATSClient:    deps.NATSClient,
			Logger:        logger,
			ComponentName: componentName,
		},
	}

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
	// - AGENT_LOOPS: decomposer, reviewer, red-team (direct agentic loops)
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
		if phase == phaseCompleted || phase == phaseFailed || phase == phaseError {
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
		BlueTeamID:        reqExec.BlueTeamID,
		RedTeamID:         reqExec.RedTeamID,
		DecomposerTaskID:  reqExec.DecomposerTaskID,
		CurrentNodeTaskID: reqExec.CurrentNodeTaskID,
		ReviewerTaskID:    reqExec.ReviewerTaskID,
		RedTeamTaskID:     reqExec.RedTeamTaskID,
		RequirementBranch: reqExec.RequirementBranch,
		CurrentNodeIdx:    reqExec.CurrentNodeIdx,
		SortedNodeIDs:     reqExec.SortedNodeIDs,
		RetryCount:        reqExec.RetryCount,
		MaxRetries:        reqExec.MaxRetries,
		storeKey:          key,
	}

	if exec.MaxRetries == 0 {
		exec.MaxRetries = c.config.MaxRequirementRetries
	}

	// Rebuild DAG and NodeIndex from the serialized DAGRaw blob.
	if len(reqExec.DAGRaw) > 0 {
		var dag decompose.TaskDAG
		if err := json.Unmarshal(reqExec.DAGRaw, &dag); err == nil {
			exec.DAG = &dag
			exec.NodeIndex = make(map[string]*decompose.TaskNode, len(dag.Nodes))
			for i := range dag.Nodes {
				exec.NodeIndex[dag.Nodes[i].ID] = &dag.Nodes[i]
			}
		}
	}

	// Rebuild VisitedNodes from NodeResults.
	exec.VisitedNodes = make(map[string]bool, len(reqExec.NodeResults))
	exec.NodeResults = make([]NodeResult, 0, len(reqExec.NodeResults))
	for _, nr := range reqExec.NodeResults {
		exec.VisitedNodes[nr.NodeID] = true
		exec.NodeResults = append(exec.NodeResults, NodeResult{
			NodeID:        nr.NodeID,
			FilesModified: nr.FilesModified,
			Summary:       nr.Summary,
		})
	}

	return exec
}

// ---------------------------------------------------------------------------
// Decomposer complete
// ---------------------------------------------------------------------------

func (c *Component) handleDecomposerCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *requirementExecution) {

	if event.Outcome != agentic.OutcomeSuccess {
		c.markFailedLocked(ctx, exec, fmt.Sprintf("decomposer failed: outcome=%s", event.Outcome))
		return
	}

	// Parse the DAG from decomposer result.
	// The decompose_task tool returns {"goal": "...", "dag": {"nodes": [...]}}.
	// Small models (qwen3, etc.) may wrap output in markdown code fences.
	dagJSON := llm.ExtractJSON(event.Result)
	if dagJSON == "" {
		c.markFailedLocked(ctx, exec, "failed to parse decomposer result: no JSON found in result")
		return
	}
	var dagResponse struct {
		Goal string            `json:"goal"`
		DAG  decompose.TaskDAG `json:"dag"`
	}
	if err := json.Unmarshal([]byte(dagJSON), &dagResponse); err != nil {
		c.markFailedLocked(ctx, exec, fmt.Sprintf("failed to parse decomposer result: %v", err))
		return
	}

	if err := dagResponse.DAG.Validate(); err != nil {
		c.markFailedLocked(ctx, exec, fmt.Sprintf("invalid DAG from decomposer: %v", err))
		return
	}

	// Topological sort for serial execution order.
	sorted, err := topoSort(&dagResponse.DAG)
	if err != nil {
		c.markFailedLocked(ctx, exec, fmt.Sprintf("topological sort failed: %v", err))
		return
	}

	exec.DAG = &dagResponse.DAG
	exec.SortedNodeIDs = sorted
	exec.NodeIndex = make(map[string]*decompose.TaskNode, len(dagResponse.DAG.Nodes))
	for i := range dagResponse.DAG.Nodes {
		exec.NodeIndex[dagResponse.DAG.Nodes[i].ID] = &dagResponse.DAG.Nodes[i]
	}

	// Persist DAG state to EXECUTION_STATES for crash recovery.
	// The DAGRaw + SortedNodeIDs fields let reconcileFromKV rebuild the full
	// execution state without re-running the decomposer.
	dagRaw, _ := json.Marshal(dagResponse.DAG)

	nodeCount := len(sorted)
	if err := c.sendReqPhase(ctx, exec.storeKey, phaseExecuting, map[string]any{
		"node_count":      nodeCount,
		"dag":             json.RawMessage(dagRaw),
		"sorted_node_ids": sorted,
	}); err != nil {
		c.logger.Warn("Failed to send req.phase mutation", "stage", phaseExecuting, "error", err)
	}

	c.logger.Info("Decomposition complete, starting serial execution",
		"entity_id", exec.EntityID,
		"node_count", len(sorted),
	)

	// Publish each DAG node as a graph entity so the knowledge graph captures
	// the full execution hierarchy.  Best-effort: failure does not abort execution.
	c.publishDAGNodes(ctx, exec)

	// Dispatch the first node.
	c.dispatchNextNodeLocked(ctx, exec)
}

// ---------------------------------------------------------------------------
// Node complete
// ---------------------------------------------------------------------------

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
		c.publishDAGNodeStatus(ctx, exec, nodeID, "failed")

		// On node failure, check if we can retry at the requirement level.
		// This catches escalated tasks (TDD budget exhausted) and gives them
		// one more chance with the prior workspace intact.
		if exec.RetryCount < exec.MaxRetries && exec.MaxRetries > 0 {
			exec.RetryCount++
			exec.LastReviewFeedback = fmt.Sprintf("Node %q failed (outcome=%s). Retry the implementation.", nodeID, event.Outcome)
			exec.terminated = false
			exec.DirtyNodeIDs = []string{nodeID}
			delete(exec.VisitedNodes, nodeID)

			if err := c.sendReqPhase(ctx, exec.storeKey, phaseExecuting, map[string]any{
				"retry_count": exec.RetryCount,
				"dirty_nodes": exec.DirtyNodeIDs,
			}); err != nil {
				c.logger.Warn("Failed to send req.phase mutation for node retry", "error", err)
			}

			c.logger.Info("Retrying failed node at requirement level",
				"entity_id", exec.EntityID,
				"node_id", nodeID,
				"retry_count", exec.RetryCount,
			)

			// Reset to just before the failed node and re-dispatch.
			exec.CurrentNodeIdx--
			c.dispatchNextNodeLocked(ctx, exec)
			return
		}

		c.markFailedLocked(ctx, exec, fmt.Sprintf("node %q failed: outcome=%s", nodeID, event.Outcome))
		return
	}

	// Update the DAG node graph entity to reflect successful completion.
	c.publishDAGNodeStatus(ctx, exec, nodeID, "completed")

	// Track node result for aggregate reporting.
	var nodeResult NodeResult
	nodeResult.NodeID = nodeID
	if event.Result != "" {
		var parsed struct {
			FilesModified []string `json:"files_modified"`
			FilesCreated  []string `json:"files_created"`
			Summary       string   `json:"changes_summary"`
		}
		if err := json.Unmarshal([]byte(llm.ExtractJSON(event.Result)), &parsed); err == nil {
			nodeResult.FilesModified = append(parsed.FilesModified, parsed.FilesCreated...)
			nodeResult.Summary = parsed.Summary
		}
	}
	exec.NodeResults = append(exec.NodeResults, nodeResult)

	// Send node completion mutation to execution-manager.
	wfResult := &workflow.NodeResult{
		NodeID:        nodeResult.NodeID,
		FilesModified: nodeResult.FilesModified,
		Summary:       nodeResult.Summary,
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

	// Check if all nodes are done.
	if len(exec.VisitedNodes) >= len(exec.SortedNodeIDs) {
		// All nodes complete — proceed to requirement-level review.
		c.beginRequirementReviewLocked(ctx, exec)
		return
	}

	// Dispatch next node.
	c.dispatchNextNodeLocked(ctx, exec)
}

// ---------------------------------------------------------------------------
// Agent dispatch: Decomposer
// ---------------------------------------------------------------------------

func (c *Component) dispatchDecomposerLocked(ctx context.Context, exec *requirementExecution) {
	taskID := fmt.Sprintf("decompose-%s-%s", exec.EntityID, uuid.New().String())
	exec.DecomposerTaskID = taskID

	// Persist decomposer task ID to EXECUTION_STATES so execution-manager can
	// route the completion back via KV (reqKeyByTaskID matches on this field).
	if err := c.sendReqPhase(ctx, exec.storeKey, phaseDecomposing, map[string]any{
		"decomposer_task_id": taskID,
	}); err != nil {
		c.logger.Warn("Failed to send req.phase mutation for decomposer dispatch", "error", err)
	}

	// Use separate decomposer model if configured, otherwise fall back to exec model.
	decomposerModel := c.config.DecomposerModel
	if decomposerModel == "" {
		decomposerModel = exec.Model
	}

	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleDeveloper,
		Model:        decomposerModel,
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: stageDecompose,
		Prompt:       c.buildDecomposerPrompt(exec),
		ToolChoice:   &agentic.ToolChoice{Mode: "function", FunctionName: "decompose_task"},
		Metadata: map[string]any{
			"requirement_id": exec.RequirementID,
			"plan_slug":      exec.Slug,
		},
	}

	if err := c.publishTask(ctx, subjectDecomposer, task); err != nil {
		c.markErrorLocked(ctx, exec, fmt.Sprintf("dispatch decomposer failed: %v", err))
		return
	}

	c.logger.Info("Dispatched decomposer",
		"entity_id", exec.EntityID,
		"task_id", taskID,
	)
}

// buildDecomposerPrompt constructs the decomposer prompt from the requirement context.
// It includes the requirement title, description, prerequisite context, and scenarios
// as acceptance criteria.
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

// buildReviewPrompt constructs the prompt for requirement-level review (reviewer
// and red-team). Includes requirement context, scenarios as acceptance criteria,
// files modified by completed nodes, and node summaries.
func (c *Component) buildReviewPrompt(exec *requirementExecution) string {
	var sb strings.Builder

	sb.WriteString("Requirement: ")
	sb.WriteString(exec.Title)
	sb.WriteString("\n")

	if exec.Description != "" {
		sb.WriteString("Description: ")
		sb.WriteString(exec.Description)
		sb.WriteString("\n")
	}

	if len(exec.Scenarios) > 0 {
		sb.WriteString("\nAcceptance Criteria (scenarios to verify):\n")
		for i, sc := range exec.Scenarios {
			thenParts := strings.Join(sc.Then, ", ")
			sb.WriteString(fmt.Sprintf("%d. [%s] Given %s, When %s, Then %s\n",
				i+1, sc.ID, sc.Given, sc.When, thenParts))
		}
	}

	if len(exec.NodeResults) > 0 {
		sb.WriteString("\nCompleted Implementation Nodes:\n")
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

func (c *Component) buildDecomposerPrompt(exec *requirementExecution) string {
	// Use the explicit prompt if provided (e.g. from legacy trigger).
	if exec.Prompt != "" {
		return exec.Prompt
	}

	var sb strings.Builder

	sb.WriteString("Requirement: ")
	sb.WriteString(exec.Title)
	sb.WriteString("\n")

	if exec.Description != "" {
		sb.WriteString("Description: ")
		sb.WriteString(exec.Description)
		sb.WriteString("\n")
	}

	if len(exec.DependsOn) > 0 {
		sb.WriteString("\nPrerequisite Requirements (already completed — reference their work):\n")
		for i, prereq := range exec.DependsOn {
			sb.WriteString(fmt.Sprintf("%d. %q — %s\n", i+1, prereq.Title, prereq.Description))
			if len(prereq.FilesModified) > 0 {
				sb.WriteString(fmt.Sprintf("   Files modified: %s\n", strings.Join(prereq.FilesModified, ", ")))
			}
			if prereq.Summary != "" {
				sb.WriteString(fmt.Sprintf("   Summary: %s\n", prereq.Summary))
			}
		}
	}

	if exec.Scope != nil {
		sb.WriteString("\nProject File Scope:\n")
		if len(exec.Scope.Include) > 0 {
			sb.WriteString("  Include: " + strings.Join(exec.Scope.Include, ", ") + "\n")
		}
		if len(exec.Scope.Exclude) > 0 {
			sb.WriteString("  Exclude: " + strings.Join(exec.Scope.Exclude, ", ") + "\n")
		}
		if len(exec.Scope.DoNotTouch) > 0 {
			sb.WriteString("  Do not touch: " + strings.Join(exec.Scope.DoNotTouch, ", ") + "\n")
		}
	}

	if len(exec.Scenarios) > 0 {
		sb.WriteString("\nAcceptance Criteria (scenarios to satisfy):\n")
		for i, sc := range exec.Scenarios {
			thenParts := strings.Join(sc.Then, ", ")
			sb.WriteString(fmt.Sprintf("%d. Given %s, When %s, Then %s\n",
				i+1, sc.Given, sc.When, thenParts))
		}
	}

	return sb.String()
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
	c.publishDAGNodeStatus(ctx, exec, nodeID, "executing")

	// Dispatch to execution-manager for TDD pipeline processing via mutation.
	// execution-manager's KV watcher picks up the pending task entry.
	taskReq := map[string]any{
		"slug":            exec.Slug,
		"task_id":         taskID,
		"title":           node.Prompt,
		"prompt":          nodePrompt,
		"model":           exec.Model,
		"project_id":      exec.ProjectID,
		"trace_id":        exec.TraceID,
		"loop_id":         exec.LoopID,
		"request_id":      fmt.Sprintf("node-%s-%s", exec.RequirementID, nodeID),
		"scenario_branch": exec.RequirementBranch,
		"file_scope":      node.FileScope,
	}
	if err := c.sendTaskCreate(ctx, taskReq); err != nil {
		c.markErrorLocked(ctx, exec, fmt.Sprintf("dispatch node %q failed: %v", nodeID, err))
		return
	}

	c.logger.Info("Dispatched node",
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
// If teams are enabled, dispatches red team first. Otherwise, goes straight to reviewer.
// Caller must hold exec.mu.
func (c *Component) beginRequirementReviewLocked(ctx context.Context, exec *requirementExecution) {
	if c.teamsEnabled() && exec.BlueTeamID != "" {
		c.dispatchRequirementRedTeamLocked(ctx, exec)
	} else {
		c.dispatchRequirementReviewerLocked(ctx, exec)
	}
}

// dispatchRequirementRedTeamLocked dispatches the red team challenge for a requirement.
// Caller must hold exec.mu.
func (c *Component) dispatchRequirementRedTeamLocked(ctx context.Context, exec *requirementExecution) {
	taskID := fmt.Sprintf("requirement-red-%s-%s", exec.EntityID, uuid.New().String())
	exec.RedTeamTaskID = taskID

	// Create a worktree for the red team so it can access merged files.
	if c.sandbox != nil && exec.RequirementBranch != "" {
		if _, err := c.sandbox.CreateWorktree(ctx, taskID, sandbox.WithBaseBranch(exec.RequirementBranch)); err != nil {
			c.logger.Warn("Failed to create red-team worktree",
				"task_id", taskID, "branch", exec.RequirementBranch, "error", err)
		}
	}

	if err := c.sendReqPhase(ctx, exec.storeKey, phaseRedTeaming, map[string]any{
		"red_team_task_id": taskID,
	}); err != nil {
		c.logger.Warn("Failed to send req.phase mutation", "stage", phaseRedTeaming, "error", err)
	}

	asmCtx := c.buildRequirementReviewContext(exec)
	asmCtx.RedTeamContext = &prompt.RedTeamContext{
		BlueTeamFiles:   c.aggregateFiles(exec),
		BlueTeamSummary: c.aggregateNodeSummaries(exec),
	}
	assembled := c.assembler.Assemble(asmCtx)

	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleDeveloper,
		Model:        exec.Model,
		Tools:        terminal.ToolsForDeliverable("review", availableToolNames()...),
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: stageRequirementRedTeam,
		Prompt:       c.buildReviewPrompt(exec),
		ToolChoice:   prompt.ResolveToolChoice(prompt.RoleScenarioReviewer, asmCtx.AvailableTools),
		Context: &agentic.ConstructedContext{
			Content: assembled.SystemMessage,
		},
		Metadata: map[string]any{
			"requirement_id": exec.RequirementID,
			"plan_slug":      exec.Slug,
			"task_id":        taskID,
		},
	}
	if err := c.publishTask(ctx, "agent.task.red-team", task); err != nil {
		c.logger.Error("Failed to dispatch requirement red team, falling back to reviewer", "error", err)
		// Fallback: skip red team, go directly to reviewer.
		c.dispatchRequirementReviewerLocked(ctx, exec)
		return
	}

	c.logger.Info("Dispatched requirement red team",
		"entity_id", exec.EntityID,
		"task_id", taskID,
	)
}

// handleRequirementRedTeamCompleteLocked processes the red team challenge result.
// Caller must hold exec.mu.
func (c *Component) handleRequirementRedTeamCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *requirementExecution) {

	if event.Outcome != agentic.OutcomeSuccess {
		c.logger.Warn("Requirement red team loop failed, proceeding to reviewer without red-team input",
			"entity_id", exec.EntityID, "outcome", event.Outcome)
		// Skip result parsing — red team is optional, proceed directly to reviewer.
		if err := c.sendReqPhase(ctx, exec.storeKey, phaseReviewing, nil); err != nil {
			c.logger.Warn("Failed to send req.phase mutation", "stage", phaseReviewing, "error", err)
		}
		c.dispatchRequirementReviewerLocked(ctx, exec)
		return
	}

	if event.Result != "" {
		var challenge payloads.RedTeamChallengeResult
		if err := json.Unmarshal([]byte(llm.ExtractJSON(event.Result)), &challenge); err != nil {
			c.logger.Warn("Failed to parse requirement red team result, proceeding to reviewer",
				"entity_id", exec.EntityID, "error", err)
		} else {
			exec.RedTeamChallenge = &challenge
		}
	}

	if err := c.sendReqPhase(ctx, exec.storeKey, phaseReviewing, nil); err != nil {
		c.logger.Warn("Failed to send req.phase mutation", "stage", phaseReviewing, "error", err)
	}
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

	asmCtx := c.buildRequirementReviewContext(exec)
	// Wire red team findings if available.
	if exec.RedTeamChallenge != nil && asmCtx.ScenarioReviewContext != nil {
		asmCtx.ScenarioReviewContext.RedTeamFindings = &prompt.RedTeamContext{
			BlueTeamFiles:   c.aggregateFiles(exec),
			BlueTeamSummary: c.aggregateNodeSummaries(exec),
		}
	}
	assembled := c.assembler.Assemble(asmCtx)

	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleReviewer,
		Model:        exec.Model,
		Tools:        terminal.ToolsForDeliverable("review", availableToolNames()...),
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: stageRequirementReview,
		Prompt:       c.buildReviewPrompt(exec),
		ToolChoice:   prompt.ResolveToolChoice(prompt.RoleScenarioReviewer, asmCtx.AvailableTools),
		Context: &agentic.ConstructedContext{
			Content: assembled.SystemMessage,
		},
		Metadata: map[string]any{
			"requirement_id":   exec.RequirementID,
			"plan_slug":        exec.Slug,
			"task_id":          taskID,
			"deliverable_type": "review",
		},
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

	// Parse verdict from reviewer result.
	var result struct {
		Verdict          string            `json:"verdict"`
		RejectionType    string            `json:"rejection_type"` // "fixable" or "restructure"
		Feedback         string            `json:"feedback"`
		ScenarioVerdicts []ScenarioVerdict `json:"scenario_verdicts"`
	}
	if event.Result != "" {
		if err := json.Unmarshal([]byte(llm.ExtractJSON(event.Result)), &result); err != nil {
			c.logger.Warn("Failed to parse requirement reviewer result", "entity_id", exec.EntityID, "error", err)
		}
	}

	exec.ReviewVerdict = result.Verdict
	exec.ReviewFeedback = result.Feedback
	exec.ScenarioVerdicts = result.ScenarioVerdicts

	if result.Verdict == "approved" || result.Verdict == "" {
		c.markCompletedLocked(ctx, exec)
		return
	}

	// Rejected — check retry budget.
	if exec.RetryCount >= exec.MaxRetries || exec.MaxRetries == 0 {
		c.markFailedLocked(ctx, exec, fmt.Sprintf("requirement rejected (retries exhausted): %s", result.Feedback))
		return
	}

	switch result.RejectionType {
	case "restructure":
		c.startRestructureRetryLocked(ctx, exec, result.Feedback)
	default:
		// "fixable" or unrecognized → dirty-node retry.
		c.startFixableRetryLocked(ctx, exec, result.Feedback, result.ScenarioVerdicts)
	}
}

// startFixableRetryLocked handles a fixable rejection by mapping failed scenarios
// to dirty DAG nodes and re-running only those nodes through the TDD pipeline.
// Clean nodes are preserved — their worktree commits stay on the RequirementBranch.
// Caller must hold exec.mu.
func (c *Component) startFixableRetryLocked(ctx context.Context, exec *requirementExecution, feedback string, verdicts []ScenarioVerdict) {
	exec.RetryCount++
	exec.LastReviewFeedback = feedback
	exec.terminated = false // allow new terminal write

	// Collect failed scenario IDs.
	failedScenarios := make(map[string]bool)
	for _, sv := range verdicts {
		if !sv.Passed {
			failedScenarios[sv.ScenarioID] = true
		}
	}

	// Map failed scenarios → dirty nodes via ScenarioIDs on TaskNode.
	var dirtyNodes []string
	for _, nodeID := range exec.SortedNodeIDs {
		node, ok := exec.NodeIndex[nodeID]
		if !ok {
			continue
		}
		for _, sid := range node.ScenarioIDs {
			if failedScenarios[sid] {
				dirtyNodes = append(dirtyNodes, nodeID)
				break
			}
		}
	}

	// If no mapping found (decomposer didn't set ScenarioIDs), fall back to
	// marking ALL nodes dirty. This is the safe default — less targeted but
	// still preserves workspace files for the prior-work-directive.
	if len(dirtyNodes) == 0 && len(failedScenarios) > 0 {
		dirtyNodes = make([]string, len(exec.SortedNodeIDs))
		copy(dirtyNodes, exec.SortedNodeIDs)
		c.logger.Warn("No scenario-to-node mapping — marking all nodes dirty",
			"entity_id", exec.EntityID,
			"failed_scenarios", len(failedScenarios))
	}

	exec.DirtyNodeIDs = dirtyNodes

	// Reset execution tracking for dirty nodes only.
	for _, nodeID := range dirtyNodes {
		delete(exec.VisitedNodes, nodeID)
	}
	// Remove node results for dirty nodes.
	var cleanResults []NodeResult
	dirtySet := make(map[string]bool, len(dirtyNodes))
	for _, id := range dirtyNodes {
		dirtySet[id] = true
	}
	for _, nr := range exec.NodeResults {
		if !dirtySet[nr.NodeID] {
			cleanResults = append(cleanResults, nr)
		}
	}
	exec.NodeResults = cleanResults

	// Reset node index to re-dispatch from the beginning.
	// dispatchNextNodeLocked skips clean nodes automatically.
	exec.CurrentNodeIdx = -1

	// Update KV state.
	if err := c.sendReqPhase(ctx, exec.storeKey, phaseExecuting, map[string]any{
		"retry_count": exec.RetryCount,
		"dirty_nodes": dirtyNodes,
	}); err != nil {
		c.logger.Warn("Failed to send req.phase mutation for retry", "error", err)
	}

	c.logger.Info("Starting fixable retry — re-running dirty nodes",
		"entity_id", exec.EntityID,
		"retry_count", exec.RetryCount,
		"dirty_nodes", len(dirtyNodes),
		"clean_nodes", len(exec.SortedNodeIDs)-len(dirtyNodes),
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
	exec.DirtyNodeIDs = nil
	exec.ReviewVerdict = ""
	exec.ReviewFeedback = ""
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

	c.dispatchDecomposerLocked(ctx, exec)
}

// isNodeDirty returns true if a node should be re-executed on retry.
// On first attempt (RetryCount==0) or when DirtyNodeIDs is empty, all nodes run.
// On retry with dirty nodes, only nodes in DirtyNodeIDs run.
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
func (c *Component) buildRequirementReviewContext(exec *requirementExecution) *prompt.AssemblyContext {
	rc := &prompt.ScenarioReviewContext{
		FilesModified: c.aggregateFiles(exec),
		NodeResults:   c.buildNodeSummaries(exec),
	}

	// Populate per-scenario specs for multi-scenario verdict tracking.
	if len(exec.Scenarios) > 0 {
		specs := make([]prompt.ScenarioSpec, len(exec.Scenarios))
		for i, s := range exec.Scenarios {
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

	return &prompt.AssemblyContext{
		Role:                  prompt.RoleScenarioReviewer,
		Provider:              resolveProvider(exec.Model),
		Domain:                "software",
		AvailableTools:        prompt.FilterTools(availableToolNames(), prompt.RoleScenarioReviewer),
		SupportsTools:         true,
		ScenarioReviewContext: rc,
		Persona:               prompt.GlobalPersonas().ForRole(prompt.RoleScenarioReviewer),
		Vocabulary:            prompt.GlobalPersonas().Vocabulary(),
	}
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
	return []string{
		"bash", "submit_work", "ask_question",
		"graph_search", "graph_query", "graph_summary",
		"web_search", "http_request",
		"review_scenario",
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

	if err := c.sendReqPhase(ctx, exec.storeKey, phaseCompleted, nil); err != nil {
		c.logger.Warn("Failed to send req.phase mutation", "stage", phaseCompleted, "error", err)
	}

	c.requirementsCompleted.Add(1)

	c.logger.Info("Requirement execution completed",
		"entity_id", exec.EntityID,
		"slug", exec.Slug,
		"requirement_id", exec.RequirementID,
		"nodes_completed", len(exec.VisitedNodes),
	)

	c.publishRequirementCompleteEvent(ctx, exec, "completed")
	c.publishEntity(context.Background(), NewRequirementExecutionEntity(exec).WithPhase(phaseCompleted))
	c.cleanupExecutionLocked(exec)
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

	c.requirementsFailed.Add(1)

	c.logger.Error("Requirement execution failed",
		"entity_id", exec.EntityID,
		"slug", exec.Slug,
		"requirement_id", exec.RequirementID,
		"reason", reason,
	)

	c.publishRequirementCompleteEvent(ctx, exec, "failed")
	c.publishEntity(context.Background(), NewRequirementExecutionEntity(exec).WithPhase(phaseFailed).WithFailureReason(reason))
	c.cleanupExecutionLocked(exec)
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

	event := workflow.RequirementExecutionCompleteEvent{
		Slug:            exec.Slug,
		RequirementID:   exec.RequirementID,
		Title:           exec.Title,
		Description:     exec.Description,
		ProjectID:       exec.ProjectID,
		TraceID:         exec.TraceID,
		Outcome:         outcome,
		NodeCount:       len(exec.VisitedNodes),
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

	if err := c.sendReqPhase(ctx, exec.storeKey, phaseError, map[string]any{
		"error_reason": reason,
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
	c.cleanupExecutionLocked(exec)
}

// cleanupExecutionLocked removes execution from maps and cancels timeout.
// Caller must hold exec.mu.
func (c *Component) cleanupExecutionLocked(exec *requirementExecution) {
	if exec.timeoutTimer != nil {
		exec.timeoutTimer.stop()
	}

	// Clean up worktrees: node worktrees kept alive by execution-manager
	// (WithKeepWorktree) and reviewer/red-team worktrees we created.
	if c.sandbox != nil {
		var worktreeIDs []string
		worktreeIDs = append(worktreeIDs, exec.NodeTaskIDs...)
		if exec.ReviewerTaskID != "" {
			worktreeIDs = append(worktreeIDs, exec.ReviewerTaskID)
		}
		if exec.RedTeamTaskID != "" {
			worktreeIDs = append(worktreeIDs, exec.RedTeamTaskID)
		}
		for _, id := range worktreeIDs {
			if err := c.sandbox.DeleteWorktree(context.Background(), id); err != nil {
				c.logger.Debug("Worktree cleanup failed (may already be deleted)",
					"task_id", id, "error", err)
			}
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

// teamsEnabled returns true when team-based requirement review is active.
// Teams are always on unless the explicit kill switch (Teams.Enabled = false)
// is set. The actual BlueTeamID guard at usage sites prevents nil-deref when
// no team is assigned.
func (c *Component) teamsEnabled() bool {
	if c.config.Teams != nil && c.config.Teams.Enabled != nil && !*c.config.Teams.Enabled {
		return false // explicit kill switch
	}
	return true
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
