// Package scenarioexecutor provides a component that orchestrates per-scenario
// execution using a serial-first strategy.
//
// It replaces the reactive scenario-execution-loop (7 rules) and dag-execution-loop
// (6 rules) with a single component that decomposes scenarios into a DAG, then
// executes nodes serially in topological order.
//
// Execution flow:
//  1. Receive trigger from scenario-orchestrator
//  2. Dispatch decomposer agent → get validated TaskDAG
//  3. Topological sort → linear execution order
//  4. Execute each node serially as an agent task
//  5. All nodes done → completed; any failure → failed
//
// Terminal status transitions (completed, failed) are owned by the JSON rule
// processor, not this component. This component writes workflow.phase; rules
// react and set workflow.status + publish events.
package scenarioexecutor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	executionorchestrator "github.com/c360studio/semspec/processor/execution-orchestrator"
	"github.com/c360studio/semspec/prompt"
	promptdomain "github.com/c360studio/semspec/prompt/domain"
	"github.com/c360studio/semspec/tools/decompose"
	"github.com/c360studio/semspec/tools/sandbox"
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
	componentName    = "scenario-executor"
	componentVersion = "0.1.0"

	// WorkflowSlugScenarioExecution identifies scenario events in LoopCompletedEvent.
	WorkflowSlugScenarioExecution = "semspec-scenario-execution"

	// Pipeline stage constants used as WorkflowStep in TaskMessages.
	stageDecompose      = "decompose"
	stageScenarioRedTeam = "scenario-red-team"
	stageScenarioReview  = "scenario-review"

	// Phase values written to entity triples.
	phaseDecomposing = "decomposing"
	phaseExecuting   = "executing"
	phaseCompleted   = "completed"
	phaseFailed      = "failed"
	phaseError       = "error"
	phaseRedTeaming  = "red_teaming"
	phaseReviewing   = "reviewing"

	// NATS subjects.
	subjectScenarioTrigger   = "workflow.trigger.scenario-execution-loop"
	subjectLoopCompleted     = "agent.complete.>"
	subjectDecomposer        = "agent.task.development"
	subjectExecutionTrigger  = "workflow.trigger.task-execution-loop"
)

// consumerInfo tracks a JetStream consumer created during Start so it can be
// stopped cleanly via StopConsumer rather than context cancellation.
type consumerInfo struct {
	streamName   string
	consumerName string
}

// Component orchestrates per-scenario execution.
type Component struct {
	config       Config
	natsClient   *natsclient.Client
	logger       *slog.Logger
	platform     component.PlatformMeta
	tripleWriter *graphutil.TripleWriter
	sandbox      *sandbox.Client // nil when sandbox is disabled
	assembler    *prompt.Assembler // composes system prompts for scenario-level review

	inputPorts  []component.Port
	outputPorts []component.Port

	// activeExecutions maps entityID → *scenarioExecution.
	activeExecutions sync.Map

	// taskIDIndex maps TaskID → entityID for O(1) completion routing.
	taskIDIndex sync.Map

	// Lifecycle
	wg            sync.WaitGroup
	consumerInfos []consumerInfo
	running       bool
	mu            sync.RWMutex
	lifecycleMu   sync.Mutex

	// Metrics
	triggersProcessed  atomic.Int64
	scenariosCompleted atomic.Int64
	scenariosFailed    atomic.Int64
	errors             atomic.Int64
	lastActivityMu     sync.RWMutex
	lastActivity       time.Time
}

// NewComponent creates a new scenario-executor from raw JSON config.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var cfg Config
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal scenario-executor config: %w", err)
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

	c.logger.Info("Starting scenario-executor")

	// Consumer 1: scenario execution triggers from scenario-orchestrator.
	triggerCfg := natsclient.StreamConsumerConfig{
		StreamName:    "WORKFLOW",
		ConsumerName:  "scenario-executor-scenario-trigger",
		FilterSubject: subjectScenarioTrigger,
		DeliverPolicy: "new",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       30 * time.Second,
		MaxAckPending: 1,
	}
	if err := c.natsClient.ConsumeStreamWithConfig(ctx, triggerCfg, c.handleTrigger); err != nil {
		return fmt.Errorf("consume scenario triggers: %w", err)
	}
	c.consumerInfos = append(c.consumerInfos, consumerInfo{
		streamName:   triggerCfg.StreamName,
		consumerName: triggerCfg.ConsumerName,
	})

	// Consumer 2: agentic loop completion events.
	completionCfg := natsclient.StreamConsumerConfig{
		StreamName:    "AGENT",
		ConsumerName:  "scenario-executor-loop-completions",
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

	c.logger.Info("Stopping scenario-executor",
		"triggers_processed", c.triggersProcessed.Load(),
		"scenarios_completed", c.scenariosCompleted.Load(),
		"scenarios_failed", c.scenariosFailed.Load(),
	)

	for _, info := range c.consumerInfos {
		c.natsClient.StopConsumer(info.streamName, info.consumerName)
	}
	c.consumerInfos = nil

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

	c.activeExecutions.Range(func(_, value any) bool {
		exec := value.(*scenarioExecution)
		exec.mu.Lock()
		if exec.timeoutTimer != nil {
			exec.timeoutTimer.stop()
		}
		exec.mu.Unlock()
		return true
	})

	c.mu.Lock()
	c.running = false
	c.mu.Unlock()

	return nil
}

// ---------------------------------------------------------------------------
// Trigger handler
// ---------------------------------------------------------------------------

func (c *Component) handleTrigger(ctx context.Context, msg jetstream.Msg) {
	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	trigger, err := payloads.ParseReactivePayload[payloads.ScenarioExecutionRequest](msg.Data())
	if err != nil {
		c.logger.Error("Failed to parse scenario execution trigger", "error", err)
		c.errors.Add(1)
		_ = msg.Nak()
		return
	}

	if trigger.ScenarioID == "" || trigger.Slug == "" {
		c.logger.Error("Trigger missing scenario_id or slug")
		c.errors.Add(1)
		_ = msg.Nak()
		return
	}

	instance := strings.ReplaceAll(trigger.Slug+"-"+trigger.ScenarioID, ".", "-")
	entityID := fmt.Sprintf("local.semspec.workflow.scenario-execution.execution.%s", instance)

	c.logger.Info("Scenario execution trigger received",
		"slug", trigger.Slug,
		"scenario_id", trigger.ScenarioID,
		"entity_id", entityID,
		"trace_id", trigger.TraceID,
	)

	model := trigger.Model
	if model == "" {
		model = c.config.Model
	}

	exec := &scenarioExecution{
		EntityID:       entityID,
		Slug:           trigger.Slug,
		ScenarioID:     trigger.ScenarioID,
		Prompt:         trigger.Prompt,
		Role:           trigger.Role,
		Model:          model,
		ProjectID:      trigger.ProjectID,
		TraceID:        trigger.TraceID,
		LoopID:         trigger.LoopID,
		RequestID:      trigger.RequestID,
		CurrentNodeIdx: -1,
		VisitedNodes:   make(map[string]bool),
	}

	// Assign blue team from roster when teams are enabled.
	if c.teamsEnabled() && len(c.config.Teams.Roster) >= 2 {
		exec.BlueTeamID = c.config.Teams.Roster[0].Name
	}

	if _, loaded := c.activeExecutions.LoadOrStore(entityID, exec); loaded {
		c.logger.Debug("Duplicate trigger for active scenario, skipping", "entity_id", entityID)
		_ = msg.Ack()
		return
	}

	// Acknowledge the trigger — execution is now owned by this component.
	_ = msg.Ack()

	// Create per-scenario branch for worktree isolation.
	if c.sandbox != nil {
		branchName := "semspec/scenario-" + trigger.ScenarioID
		if err := c.sandbox.CreateBranch(ctx, branchName, "HEAD"); err != nil {
			c.logger.Warn("Failed to create scenario branch; worktrees will branch from HEAD",
				"branch", branchName, "error", err)
		} else {
			exec.ScenarioBranch = branchName
			c.logger.Info("Scenario branch created", "branch", branchName)
		}
	}

	// Write initial entity triples.
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.Type, "scenario-execution")
	if err := c.tripleWriter.WriteTriple(ctx, entityID, wf.Phase, phaseDecomposing); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseDecomposing, "error", err)
	}
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.Slug, trigger.Slug)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.ScenarioID, trigger.ScenarioID)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.ProjectID, trigger.ProjectID)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.TraceID, trigger.TraceID)

	// Publish initial entity snapshot for graph observability.
	c.publishEntity(ctx, NewScenarioExecutionEntity(exec).WithPhase(phaseDecomposing))

	// Lock for timeout + dispatch.
	exec.mu.Lock()
	defer exec.mu.Unlock()

	c.startExecutionTimeoutLocked(exec)
	c.dispatchDecomposerLocked(ctx, exec)
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
		// Not a LoopCompletedEvent — not for us; ack and discard.
		_ = msg.Ack()
		return
	}

	// Accept events from our own slug (decomposer completions) and from the
	// execution-orchestrator's slug (TDD pipeline node completions).
	if event.WorkflowSlug != WorkflowSlugScenarioExecution && event.WorkflowSlug != executionorchestrator.WorkflowSlugTaskExecution {
		// Wrong workflow slug — belongs to another component; ack and discard.
		_ = msg.Ack()
		return
	}

	c.updateLastActivity()

	entityIDVal, ok := c.taskIDIndex.Load(event.TaskID)
	if !ok {
		c.logger.Debug("Loop completed for unknown task ID",
			"task_id", event.TaskID,
			"workflow_step", event.WorkflowStep,
		)
		// Unknown task ID — not ours; ack to prevent redelivery.
		_ = msg.Ack()
		return
	}
	entityID := entityIDVal.(string)

	execVal, ok := c.activeExecutions.Load(entityID)
	if !ok {
		c.logger.Debug("No active execution for entity", "entity_id", entityID)
		return
	}
	exec := execVal.(*scenarioExecution)

	exec.mu.Lock()
	defer exec.mu.Unlock()

	// Acknowledge before processing — we own this message regardless of outcome.
	_ = msg.Ack()

	if exec.terminated {
		return
	}

	c.logger.Info("Loop completion received",
		"slug", exec.Slug,
		"scenario_id", exec.ScenarioID,
		"workflow_step", event.WorkflowStep,
	)

	switch event.WorkflowStep {
	case stageDecompose:
		c.handleDecomposerCompleteLocked(ctx, event, exec)
	case stageScenarioRedTeam:
		c.handleScenarioRedTeamCompleteLocked(ctx, event, exec)
	case stageScenarioReview:
		c.handleScenarioReviewerCompleteLocked(ctx, event, exec)
	default:
		// Node completion — WorkflowStep is the nodeID.
		c.handleNodeCompleteLocked(ctx, event, exec)
	}
}

// ---------------------------------------------------------------------------
// Decomposer complete
// ---------------------------------------------------------------------------

func (c *Component) handleDecomposerCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *scenarioExecution) {
	c.taskIDIndex.Delete(exec.DecomposerTaskID)

	if event.Outcome != agentic.OutcomeSuccess {
		c.markFailedLocked(ctx, exec, fmt.Sprintf("decomposer failed: outcome=%s", event.Outcome))
		return
	}

	// Parse the DAG from decomposer result.
	// The decompose_task tool returns {"goal": "...", "dag": {"nodes": [...]}}.
	var dagResponse struct {
		Goal string            `json:"goal"`
		DAG  decompose.TaskDAG `json:"dag"`
	}
	if err := json.Unmarshal([]byte(event.Result), &dagResponse); err != nil {
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

	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseExecuting); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseExecuting, "error", err)
	}
	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.NodeCount, len(sorted))

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

func (c *Component) handleNodeCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *scenarioExecution) {
	c.taskIDIndex.Delete(exec.CurrentNodeTaskID)

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
		// Mark the node itself as failed in the graph before transitioning the
		// scenario execution to failed.
		c.publishDAGNodeStatus(ctx, exec, nodeID, "failed")
		c.markFailedLocked(ctx, exec, fmt.Sprintf("node %q failed: outcome=%s", nodeID, event.Outcome))
		return
	}

	// TODO: no vocabulary constant for per-node status predicates; kept as formatted string.
	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, fmt.Sprintf("workflow.node.%s.status", nodeID), "completed")

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
		if err := json.Unmarshal([]byte(event.Result), &parsed); err == nil {
			nodeResult.FilesModified = append(parsed.FilesModified, parsed.FilesCreated...)
			nodeResult.Summary = parsed.Summary
		}
	}
	exec.NodeResults = append(exec.NodeResults, nodeResult)

	c.logger.Info("Node completed",
		"entity_id", exec.EntityID,
		"node_id", nodeID,
		"completed", len(exec.VisitedNodes),
		"total", len(exec.SortedNodeIDs),
	)

	// Check if all nodes are done.
	if len(exec.VisitedNodes) >= len(exec.SortedNodeIDs) {
		// All nodes complete — proceed to scenario-level review.
		c.beginScenarioReviewLocked(ctx, exec)
		return
	}

	// Dispatch next node.
	c.dispatchNextNodeLocked(ctx, exec)
}

// ---------------------------------------------------------------------------
// Agent dispatch: Decomposer
// ---------------------------------------------------------------------------

func (c *Component) dispatchDecomposerLocked(ctx context.Context, exec *scenarioExecution) {
	taskID := fmt.Sprintf("decompose-%s-%s", exec.EntityID, uuid.New().String())
	exec.DecomposerTaskID = taskID
	c.taskIDIndex.Store(taskID, exec.EntityID)

	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleGeneral,
		Model:        exec.Model,
		WorkflowSlug: WorkflowSlugScenarioExecution,
		WorkflowStep: stageDecompose,
		Prompt:       exec.Prompt,
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

// ---------------------------------------------------------------------------
// Agent dispatch: DAG node (serial)
// ---------------------------------------------------------------------------

func (c *Component) dispatchNextNodeLocked(ctx context.Context, exec *scenarioExecution) {
	exec.CurrentNodeIdx++
	if exec.CurrentNodeIdx >= len(exec.SortedNodeIDs) {
		// All nodes dispatched and completed — proceed to scenario-level review.
		c.beginScenarioReviewLocked(ctx, exec)
		return
	}

	nodeID := exec.SortedNodeIDs[exec.CurrentNodeIdx]
	node, ok := exec.NodeIndex[nodeID]
	if !ok {
		c.markErrorLocked(ctx, exec, fmt.Sprintf("node %q not found in index", nodeID))
		return
	}

	taskID := fmt.Sprintf("node-%s-%s-%s", exec.EntityID, nodeID, uuid.New().String())
	exec.CurrentNodeTaskID = taskID
	c.taskIDIndex.Store(taskID, exec.EntityID)

	// Dispatch to execution-orchestrator for TDD pipeline processing
	// (test → build → validate → review) instead of direct agent dispatch.
	trigger := &workflow.TriggerPayload{
		WorkflowID:     "task-execution-loop",
		Slug:           exec.Slug,
		TaskID:         taskID,
		Title:          node.Prompt,
		Prompt:         node.Prompt,
		Model:          exec.Model,
		ProjectID:      exec.ProjectID,
		TraceID:        exec.TraceID,
		LoopID:         exec.LoopID,
		RequestID:      fmt.Sprintf("node-%s-%s", exec.ScenarioID, nodeID),
		ScenarioBranch: exec.ScenarioBranch,
	}

	// TODO: no vocabulary constant for per-node status predicates; kept as formatted string.
	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, fmt.Sprintf("workflow.node.%s.status", nodeID), "running")

	// Update the DAG node graph entity to reflect that execution has started.
	c.publishDAGNodeStatus(ctx, exec, nodeID, "executing")

	if err := c.publishTrigger(ctx, subjectExecutionTrigger, trigger); err != nil {
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
// Scenario-level review pipeline
// ---------------------------------------------------------------------------

// beginScenarioReviewLocked starts the scenario-level review pipeline.
// If teams are enabled, dispatches red team first. Otherwise, goes straight to reviewer.
// Caller must hold exec.mu.
func (c *Component) beginScenarioReviewLocked(ctx context.Context, exec *scenarioExecution) {
	if c.teamsEnabled() && exec.BlueTeamID != "" {
		c.dispatchScenarioRedTeamLocked(ctx, exec)
	} else {
		c.dispatchScenarioReviewerLocked(ctx, exec)
	}
}

// dispatchScenarioRedTeamLocked dispatches the red team challenge for a scenario.
// Caller must hold exec.mu.
func (c *Component) dispatchScenarioRedTeamLocked(ctx context.Context, exec *scenarioExecution) {
	taskID := fmt.Sprintf("scenario-red-%s-%s", exec.EntityID, uuid.New().String())
	exec.RedTeamTaskID = taskID
	c.taskIDIndex.Store(taskID, exec.EntityID)

	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseRedTeaming); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseRedTeaming, "error", err)
	}

	asmCtx := c.buildScenarioReviewContext(exec)
	asmCtx.RedTeamContext = &prompt.RedTeamContext{
		BlueTeamFiles:   c.aggregateFiles(exec),
		BlueTeamSummary: c.aggregateNodeSummaries(exec),
	}
	assembled := c.assembler.Assemble(asmCtx)

	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleDeveloper,
		Model:        exec.Model,
		WorkflowSlug: WorkflowSlugScenarioExecution,
		WorkflowStep: stageScenarioRedTeam,
		Prompt:       exec.Prompt,
		Context: &agentic.ConstructedContext{
			Content: assembled.SystemMessage,
		},
	}
	if err := c.publishTask(ctx, "agent.task.red-team", task); err != nil {
		c.logger.Error("Failed to dispatch scenario red team, falling back to reviewer", "error", err)
		// Fallback: skip red team, go directly to reviewer.
		c.dispatchScenarioReviewerLocked(ctx, exec)
		return
	}

	c.logger.Info("Dispatched scenario red team",
		"entity_id", exec.EntityID,
		"task_id", taskID,
	)
}

// handleScenarioRedTeamCompleteLocked processes the red team challenge result.
// Caller must hold exec.mu.
func (c *Component) handleScenarioRedTeamCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *scenarioExecution) {
	c.taskIDIndex.Delete(exec.RedTeamTaskID)

	if event.Result != "" {
		var challenge payloads.RedTeamChallengeResult
		if err := json.Unmarshal([]byte(event.Result), &challenge); err != nil {
			c.logger.Warn("Failed to parse scenario red team result, proceeding to reviewer",
				"entity_id", exec.EntityID, "error", err)
		} else {
			exec.RedTeamChallenge = &challenge
		}
	}

	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseReviewing); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseReviewing, "error", err)
	}
	c.dispatchScenarioReviewerLocked(ctx, exec)
}

// dispatchScenarioReviewerLocked dispatches the scenario-level reviewer.
// Caller must hold exec.mu.
func (c *Component) dispatchScenarioReviewerLocked(ctx context.Context, exec *scenarioExecution) {
	taskID := fmt.Sprintf("scenario-rev-%s-%s", exec.EntityID, uuid.New().String())
	exec.ReviewerTaskID = taskID
	c.taskIDIndex.Store(taskID, exec.EntityID)

	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseReviewing); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseReviewing, "error", err)
	}

	asmCtx := c.buildScenarioReviewContext(exec)
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
		WorkflowSlug: WorkflowSlugScenarioExecution,
		WorkflowStep: stageScenarioReview,
		Prompt:       exec.Prompt,
		Context: &agentic.ConstructedContext{
			Content: assembled.SystemMessage,
		},
	}
	if err := c.publishTask(ctx, "agent.task.reviewer", task); err != nil {
		c.logger.Error("Failed to dispatch scenario reviewer", "error", err)
		c.markErrorLocked(ctx, exec, fmt.Sprintf("dispatch scenario reviewer: %v", err))
		return
	}

	c.logger.Info("Dispatched scenario reviewer",
		"entity_id", exec.EntityID,
		"task_id", taskID,
	)
}

// handleScenarioReviewerCompleteLocked processes the scenario reviewer verdict.
// Caller must hold exec.mu.
func (c *Component) handleScenarioReviewerCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *scenarioExecution) {
	c.taskIDIndex.Delete(exec.ReviewerTaskID)

	if event.Outcome != agentic.OutcomeSuccess {
		c.markFailedLocked(ctx, exec, fmt.Sprintf("scenario reviewer failed: outcome=%s", event.Outcome))
		return
	}

	// Parse verdict from reviewer result.
	var result struct {
		Verdict  string `json:"verdict"`
		Feedback string `json:"feedback"`
	}
	if event.Result != "" {
		if err := json.Unmarshal([]byte(event.Result), &result); err != nil {
			c.logger.Warn("Failed to parse scenario reviewer result", "entity_id", exec.EntityID, "error", err)
		}
	}

	exec.ReviewVerdict = result.Verdict
	exec.ReviewFeedback = result.Feedback

	if result.Verdict == "approved" || result.Verdict == "" {
		// Approved (or no verdict parsed — don't block on parse failures).
		c.markCompletedLocked(ctx, exec)
	} else {
		c.markFailedLocked(ctx, exec, fmt.Sprintf("scenario rejected: %s", result.Feedback))
	}
}

// buildScenarioReviewContext assembles the prompt context for scenario-level review.
func (c *Component) buildScenarioReviewContext(exec *scenarioExecution) *prompt.AssemblyContext {
	return &prompt.AssemblyContext{
		Role:           prompt.RoleScenarioReviewer,
		Provider:       resolveProvider(exec.Model),
		Domain:         "software",
		AvailableTools: prompt.FilterTools(availableToolNames(), prompt.RoleScenarioReviewer),
		SupportsTools:  true,
		ScenarioReviewContext: &prompt.ScenarioReviewContext{
			FilesModified: c.aggregateFiles(exec),
			NodeResults:   c.buildNodeSummaries(exec),
		},
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
		"file_read", "file_write", "file_list",
		"git_status", "git_diff", "git_commit",
		"workflow_graph_summary", "workflow_query_graph", "workflow_get_codebase_summary",
		"workflow_get_entity", "workflow_traverse_relationships", "workflow_read_document",
		"web_search", "exec",
		"review_scenario",
	}
}

// aggregateFiles deduplicates files modified across all completed nodes.
func (c *Component) aggregateFiles(exec *scenarioExecution) []string {
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
func (c *Component) aggregateNodeSummaries(exec *scenarioExecution) string {
	var parts []string
	for _, nr := range exec.NodeResults {
		if nr.Summary != "" {
			parts = append(parts, fmt.Sprintf("[%s] %s", nr.NodeID, nr.Summary))
		}
	}
	return strings.Join(parts, "; ")
}

// buildNodeSummaries converts NodeResult slice to prompt.NodeResultSummary slice.
func (c *Component) buildNodeSummaries(exec *scenarioExecution) []prompt.NodeResultSummary {
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
func (c *Component) markCompletedLocked(ctx context.Context, exec *scenarioExecution) {
	if exec.terminated {
		return
	}
	exec.terminated = true

	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseCompleted); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseCompleted, "error", err)
	}

	c.scenariosCompleted.Add(1)

	c.logger.Info("Scenario execution completed",
		"entity_id", exec.EntityID,
		"slug", exec.Slug,
		"scenario_id", exec.ScenarioID,
		"nodes_completed", len(exec.VisitedNodes),
	)

	c.publishScenarioCompleteEvent(ctx, exec, "completed")
	c.publishEntity(context.Background(), NewScenarioExecutionEntity(exec).WithPhase(phaseCompleted))
	c.cleanupExecutionLocked(exec)
}

// markFailedLocked transitions to the failed terminal state.
// Caller must hold exec.mu.
func (c *Component) markFailedLocked(ctx context.Context, exec *scenarioExecution, reason string) {
	if exec.terminated {
		return
	}
	exec.terminated = true

	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseFailed); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseFailed, "error", err)
	}
	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.FailureReason, reason)

	c.scenariosFailed.Add(1)

	c.logger.Error("Scenario execution failed",
		"entity_id", exec.EntityID,
		"slug", exec.Slug,
		"scenario_id", exec.ScenarioID,
		"reason", reason,
	)

	c.publishScenarioCompleteEvent(ctx, exec, "failed")
	c.publishEntity(context.Background(), NewScenarioExecutionEntity(exec).WithPhase(phaseFailed).WithFailureReason(reason))
	c.cleanupExecutionLocked(exec)
}

// publishScenarioCompleteEvent publishes a typed ScenarioExecutionCompleteEvent
// to the WORKFLOW stream for downstream consumption (plan-api).
func (c *Component) publishScenarioCompleteEvent(ctx context.Context, exec *scenarioExecution, outcome string) {
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

	event := workflow.ScenarioExecutionCompleteEvent{
		Slug:          exec.Slug,
		ScenarioID:    exec.ScenarioID,
		ProjectID:     exec.ProjectID,
		TraceID:       exec.TraceID,
		Outcome:       outcome,
		NodeCount:     len(exec.VisitedNodes),
		FilesModified: allFiles,
	}

	if c.natsClient == nil {
		return
	}

	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Warn("Failed to get JetStream for scenario completion event", "error", err)
		return
	}

	data, err := json.Marshal(event)
	if err != nil {
		c.logger.Warn("Failed to marshal scenario completion event", "error", err)
		return
	}

	if _, err := js.Publish(ctx, workflow.ScenarioExecutionComplete.Pattern, data); err != nil {
		c.logger.Warn("Failed to publish scenario completion event",
			"entity_id", exec.EntityID,
			"outcome", outcome,
			"error", err,
		)
	}
}

// markErrorLocked transitions to the error terminal state.
// Caller must hold exec.mu.
func (c *Component) markErrorLocked(ctx context.Context, exec *scenarioExecution, reason string) {
	if exec.terminated {
		return
	}
	exec.terminated = true

	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseError); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseError, "error", err)
	}
	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.ErrorReason, reason)

	c.errors.Add(1)

	c.logger.Error("Scenario execution error",
		"entity_id", exec.EntityID,
		"slug", exec.Slug,
		"scenario_id", exec.ScenarioID,
		"reason", reason,
	)

	c.publishScenarioCompleteEvent(ctx, exec, "error")
	c.publishEntity(context.Background(), NewScenarioExecutionEntity(exec).WithPhase(phaseError).WithErrorReason(reason))
	c.cleanupExecutionLocked(exec)
}

// cleanupExecutionLocked removes execution from maps and cancels timeout.
// Caller must hold exec.mu.
func (c *Component) cleanupExecutionLocked(exec *scenarioExecution) {
	if exec.timeoutTimer != nil {
		exec.timeoutTimer.stop()
	}
	c.taskIDIndex.Delete(exec.DecomposerTaskID)
	c.taskIDIndex.Delete(exec.CurrentNodeTaskID)
	c.taskIDIndex.Delete(exec.RedTeamTaskID)
	c.taskIDIndex.Delete(exec.ReviewerTaskID)
	c.activeExecutions.Delete(exec.EntityID)
}

// ---------------------------------------------------------------------------
// Per-execution timeout
// ---------------------------------------------------------------------------

// startExecutionTimeoutLocked starts a timer that marks the execution as errored
// if it does not complete within the configured timeout.
// Caller must hold exec.mu.
func (c *Component) startExecutionTimeoutLocked(exec *scenarioExecution) {
	timeout := c.config.GetTimeout()

	timer := time.AfterFunc(timeout, func() {
		c.logger.Warn("Scenario execution timed out",
			"entity_id", exec.EntityID,
			"slug", exec.Slug,
			"scenario_id", exec.ScenarioID,
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
func (c *Component) publishDAGNodes(ctx context.Context, exec *scenarioExecution) {
	executionID := fmt.Sprintf("%s-%s", exec.Slug, exec.ScenarioID)
	for i := range exec.DAG.Nodes {
		node := &exec.DAG.Nodes[i]
		entity := newDAGNodeEntity(executionID, node, exec.EntityID)
		c.publishEntity(ctx, entity)
	}
}

// publishDAGNodeStatus updates the DAGNodeStatus triple for a single node by
// re-publishing its full entity payload with the new status.  Publishing is
// best-effort: failures are logged as warnings and do not abort execution.
func (c *Component) publishDAGNodeStatus(ctx context.Context, exec *scenarioExecution, nodeID, status string) {
	node, ok := exec.NodeIndex[nodeID]
	if !ok {
		c.logger.Warn("publishDAGNodeStatus: node not found in index",
			"entity_id", exec.EntityID, "node_id", nodeID)
		return
	}
	executionID := fmt.Sprintf("%s-%s", exec.Slug, exec.ScenarioID)
	entity := newDAGNodeEntity(executionID, node, exec.EntityID).withStatus(status)
	c.publishEntity(ctx, entity)
}

// publishTrigger wraps a TriggerPayload in a BaseMessage and publishes to JetStream.
// Used for dispatching nodes to the execution-orchestrator.
func (c *Component) publishTrigger(ctx context.Context, subject string, trigger *workflow.TriggerPayload) error {
	baseMsg := message.NewBaseMessage(trigger.Schema(), trigger, componentName)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal trigger payload: %w", err)
	}

	if c.natsClient != nil {
		if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
			return fmt.Errorf("publish to %s: %w", subject, err)
		}
	}
	return nil
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

// teamsEnabled returns true when team-based scenario review is configured.
func (c *Component) teamsEnabled() bool {
	return c.config.Teams != nil && c.config.Teams.Enabled && len(c.config.Teams.Roster) >= 2
}

// ---------------------------------------------------------------------------
// component.Discoverable interface
// ---------------------------------------------------------------------------

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        componentName,
		Type:        "processor",
		Description: "Orchestrates per-scenario execution: decompose → serial task execution",
		Version:     componentVersion,
	}
}

// InputPorts returns the component's declared input ports.
func (c *Component) InputPorts() []component.Port { return c.inputPorts }

// OutputPorts returns the component's declared output ports.
func (c *Component) OutputPorts() []component.Port { return c.outputPorts }

// ConfigSchema returns the JSON schema for this component's configuration.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return scenarioExecutorSchema
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
