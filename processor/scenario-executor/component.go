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
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/tools/decompose"
	wf "github.com/c360studio/semspec/vocabulary/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

const (
	componentName    = "scenario-executor"
	componentVersion = "0.1.0"

	// WorkflowSlugScenarioExecution identifies scenario events in LoopCompletedEvent.
	WorkflowSlugScenarioExecution = "semspec-scenario-execution"

	// Pipeline stage constants used as WorkflowStep in TaskMessages.
	stageDecompose = "decompose"

	// Phase values written to entity triples.
	phaseDecomposing = "decomposing"
	phaseExecuting   = "executing"
	phaseCompleted   = "completed"
	phaseFailed      = "failed"
	phaseError       = "error"

	// NATS subjects.
	subjectScenarioTrigger = "workflow.trigger.scenario-execution-loop"
	subjectLoopCompleted   = "agentic.loop_completed.v1"
	subjectDecomposer      = "workflow.async.scenario-decomposer"
)

// Component orchestrates per-scenario execution.
type Component struct {
	config       Config
	natsClient   *natsclient.Client
	logger       *slog.Logger
	platform     component.PlatformMeta
	tripleWriter *graphutil.TripleWriter

	inputPorts  []component.Port
	outputPorts []component.Port

	// activeExecutions maps entityID → *scenarioExecution.
	activeExecutions sync.Map

	// taskIDIndex maps TaskID → entityID for O(1) completion routing.
	taskIDIndex sync.Map

	// Lifecycle
	shutdown      chan struct{}
	wg            sync.WaitGroup
	running       bool
	mu            sync.RWMutex
	lifecycleMu   sync.Mutex
	subscriptions []*natsclient.Subscription

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

	c := &Component{
		config:     cfg,
		natsClient: deps.NATSClient,
		logger:     logger,
		platform:   deps.Platform,
		shutdown:   make(chan struct{}),
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

	triggerHandler := func(ctx context.Context, msg *nats.Msg) {
		c.wg.Add(1)
		defer c.wg.Done()
		select {
		case <-c.shutdown:
			return
		default:
		}
		c.handleTrigger(ctx, msg)
	}

	completionHandler := func(ctx context.Context, msg *nats.Msg) {
		c.wg.Add(1)
		defer c.wg.Done()
		select {
		case <-c.shutdown:
			return
		default:
		}
		c.handleLoopCompleted(ctx, msg)
	}

	for _, port := range c.inputPorts {
		subject := graphutil.PortSubject(port)
		if subject == "" {
			continue
		}

		var handler func(context.Context, *nats.Msg)
		switch subject {
		case subjectScenarioTrigger:
			handler = triggerHandler
		case subjectLoopCompleted:
			handler = completionHandler
		default:
			c.logger.Debug("Skipping unrecognized input port", "subject", subject)
			continue
		}

		sub, err := c.natsClient.Subscribe(ctx, subject, handler)
		if err != nil {
			return fmt.Errorf("subscribe to %s: %w", subject, err)
		}
		c.subscriptions = append(c.subscriptions, sub)
		c.logger.Debug("Subscribed", "subject", subject)
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

	c.logger.Info("Stopping scenario-executor",
		"triggers_processed", c.triggersProcessed.Load(),
		"scenarios_completed", c.scenariosCompleted.Load(),
		"scenarios_failed", c.scenariosFailed.Load(),
	)

	close(c.shutdown)

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

	c.activeExecutions.Range(func(_, value any) bool {
		exec := value.(*scenarioExecution)
		exec.mu.Lock()
		if exec.timeoutTimer != nil {
			exec.timeoutTimer.stop()
		}
		exec.mu.Unlock()
		return true
	})

	for _, sub := range c.subscriptions {
		if err := sub.Unsubscribe(); err != nil {
			c.logger.Debug("Unsubscribe error", "error", err)
		}
	}
	c.subscriptions = nil

	c.mu.Lock()
	c.running = false
	c.mu.Unlock()

	return nil
}

// ---------------------------------------------------------------------------
// Trigger handler
// ---------------------------------------------------------------------------

func (c *Component) handleTrigger(ctx context.Context, msg *nats.Msg) {
	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	trigger, err := payloads.ParseReactivePayload[payloads.ScenarioExecutionRequest](msg.Data)
	if err != nil {
		c.logger.Error("Failed to parse scenario execution trigger", "error", err)
		c.errors.Add(1)
		return
	}

	if trigger.ScenarioID == "" || trigger.Slug == "" {
		c.logger.Error("Trigger missing scenario_id or slug")
		c.errors.Add(1)
		return
	}

	entityID := fmt.Sprintf("local.semspec.workflow.scenario-execution.execution.%s-%s", trigger.Slug, trigger.ScenarioID)

	c.logger.Info("Scenario execution trigger received",
		"slug", trigger.Slug,
		"scenario_id", trigger.ScenarioID,
		"entity_id", entityID,
		"trace_id", trigger.TraceID,
	)

	exec := &scenarioExecution{
		EntityID:       entityID,
		Slug:           trigger.Slug,
		ScenarioID:     trigger.ScenarioID,
		Prompt:         trigger.Prompt,
		Role:           trigger.Role,
		Model:          trigger.Model,
		ProjectID:      trigger.ProjectID,
		TraceID:        trigger.TraceID,
		LoopID:         trigger.LoopID,
		RequestID:      trigger.RequestID,
		CurrentNodeIdx: -1,
		VisitedNodes:   make(map[string]bool),
	}

	if _, loaded := c.activeExecutions.LoadOrStore(entityID, exec); loaded {
		c.logger.Debug("Duplicate trigger for active scenario, skipping", "entity_id", entityID)
		return
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

func (c *Component) handleLoopCompleted(ctx context.Context, msg *nats.Msg) {
	var base message.BaseMessage
	if err := json.Unmarshal(msg.Data, &base); err != nil {
		c.logger.Debug("Failed to unmarshal loop completed envelope", "error", err)
		c.errors.Add(1)
		return
	}

	event, ok := base.Payload().(*agentic.LoopCompletedEvent)
	if !ok {
		return
	}

	if event.WorkflowSlug != WorkflowSlugScenarioExecution {
		return
	}

	c.updateLastActivity()

	entityIDVal, ok := c.taskIDIndex.Load(event.TaskID)
	if !ok {
		c.logger.Debug("Loop completed for unknown task ID",
			"task_id", event.TaskID,
			"workflow_step", event.WorkflowStep,
		)
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

	// Dispatch the first node.
	c.dispatchNextNodeLocked(ctx, exec)
}

// ---------------------------------------------------------------------------
// Node complete
// ---------------------------------------------------------------------------

func (c *Component) handleNodeCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *scenarioExecution) {
	c.taskIDIndex.Delete(exec.CurrentNodeTaskID)

	nodeID := event.WorkflowStep
	exec.VisitedNodes[nodeID] = true

	if event.Outcome != agentic.OutcomeSuccess {
		c.markFailedLocked(ctx, exec, fmt.Sprintf("node %q failed: outcome=%s", nodeID, event.Outcome))
		return
	}

	// TODO: no vocabulary constant for per-node status predicates; kept as formatted string.
	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, fmt.Sprintf("workflow.node.%s.status", nodeID), "completed")

	c.logger.Info("Node completed",
		"entity_id", exec.EntityID,
		"node_id", nodeID,
		"completed", len(exec.VisitedNodes),
		"total", len(exec.SortedNodeIDs),
	)

	// Check if all nodes are done.
	if len(exec.VisitedNodes) >= len(exec.SortedNodeIDs) {
		c.markCompletedLocked(ctx, exec)
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
		// All nodes dispatched and completed.
		c.markCompletedLocked(ctx, exec)
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

	role, subject := normalizeRole(node.Role)

	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         role,
		Model:        exec.Model,
		WorkflowSlug: WorkflowSlugScenarioExecution,
		WorkflowStep: nodeID,
		Prompt:       node.Prompt,
	}

	// TODO: no vocabulary constant for per-node status predicates; kept as formatted string.
	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, fmt.Sprintf("workflow.node.%s.status", nodeID), "running")

	if err := c.publishTask(ctx, subject, task); err != nil {
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

	c.publishEntity(context.Background(), NewScenarioExecutionEntity(exec).WithPhase(phaseFailed).WithFailureReason(reason))
	c.cleanupExecutionLocked(exec)
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

// normalizeRole maps an LLM-provided role string to a known agentic role constant
// and a NATS dispatch subject. Unknown roles default to general.
func normalizeRole(raw string) (string, string) {
	switch raw {
	case "developer", "development", "dev":
		return agentic.RoleDeveloper, "agent.task.development"
	case "reviewer", "review":
		return agentic.RoleReviewer, "agent.task.reviewer"
	default:
		return agentic.RoleGeneral, "agent.task.general"
	}
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
