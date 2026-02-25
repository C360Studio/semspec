// Package taskdispatcher provides parallel task execution with context building.
// It orchestrates task execution by:
// 1. Building context for all tasks in parallel
// 2. Dispatching tasks respecting dependencies and max_concurrent limits
// 3. Selecting models based on task type using the model registry
package taskdispatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/model"
	contextbuilder "github.com/c360studio/semspec/processor/context-builder"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/phases"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/retry"
	semstreamsWorkflow "github.com/c360studio/semstreams/pkg/workflow"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

// Component implements the task-dispatcher processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	modelRegistry *model.Registry

	// JetStream consumer
	consumer jetstream.Consumer
	stream   jetstream.Stream

	// Execution semaphore for max_concurrent
	sem chan struct{}

	// Lifecycle
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics
	batchesProcessed atomic.Int64
	tasksDispatched  atomic.Int64
	contextsBuilt    atomic.Int64
	executionsFailed atomic.Int64
	lastActivityMu   sync.RWMutex
	lastActivity     time.Time
}

// ---------------------------------------------------------------------------
// Participant interface (task-dispatcher is an entry point, not inside a workflow)
// ---------------------------------------------------------------------------

// Compile-time check that Component implements Participant interface.
var _ semstreamsWorkflow.Participant = (*Component)(nil)

// WorkflowID returns the workflow this component participates in.
// Task-dispatcher is an entry point, so this is for interface completeness.
func (c *Component) WorkflowID() string {
	return "task-dispatch"
}

// Phase returns the phase name this component represents.
func (c *Component) Phase() string {
	return phases.DispatchDispatched
}

// StateManager returns nil - task-dispatcher is an entry point, not inside a reactive workflow.
func (c *Component) StateManager() *semstreamsWorkflow.StateManager {
	return nil
}

// NewComponent creates a new task-dispatcher processor.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Apply defaults
	defaults := DefaultConfig()
	if config.StreamName == "" {
		config.StreamName = defaults.StreamName
	}
	if config.ConsumerName == "" {
		config.ConsumerName = defaults.ConsumerName
	}
	if config.TriggerSubject == "" {
		config.TriggerSubject = defaults.TriggerSubject
	}
	if config.OutputSubject == "" {
		config.OutputSubject = defaults.OutputSubject
	}
	if config.MaxConcurrent == 0 {
		config.MaxConcurrent = defaults.MaxConcurrent
	}
	if config.ContextTimeout == "" {
		config.ContextTimeout = defaults.ContextTimeout
	}
	if config.ExecutionTimeout == "" {
		config.ExecutionTimeout = defaults.ExecutionTimeout
	}
	if config.ContextSubjectPrefix == "" {
		config.ContextSubjectPrefix = defaults.ContextSubjectPrefix
	}
	if config.ContextResponseBucket == "" {
		config.ContextResponseBucket = defaults.ContextResponseBucket
	}
	if config.WorkflowTriggerSubject == "" {
		config.WorkflowTriggerSubject = defaults.WorkflowTriggerSubject
	}
	if config.Ports == nil {
		config.Ports = defaults.Ports
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Component{
		name:          "task-dispatcher",
		config:        config,
		natsClient:    deps.NATSClient,
		logger:        deps.GetLogger(),
		modelRegistry: model.Global(),
		sem:           make(chan struct{}, config.MaxConcurrent),
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized task-dispatcher",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"trigger_subject", c.config.TriggerSubject,
		"max_concurrent", c.config.MaxConcurrent)
	return nil
}

// Start begins processing batch dispatch triggers.
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

	// Get JetStream context
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("get jetstream: %w", err)
	}

	// Get stream
	stream, err := js.Stream(subCtx, c.config.StreamName)
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("get stream %s: %w", c.config.StreamName, err)
	}
	c.stream = stream

	// Create or get consumer
	consumerConfig := jetstream.ConsumerConfig{
		Durable:       c.config.ConsumerName,
		FilterSubject: c.config.TriggerSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       c.config.GetExecutionTimeout() + time.Minute, // Allow time for batch execution
		MaxDeliver:    3,
	}

	consumer, err := stream.CreateOrUpdateConsumer(subCtx, consumerConfig)
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("create consumer: %w", err)
	}
	c.consumer = consumer

	// Start consuming messages
	go c.consumeLoop(subCtx)

	c.logger.Info("task-dispatcher started",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"subject", c.config.TriggerSubject,
		"max_concurrent", c.config.MaxConcurrent)

	return nil
}

func (c *Component) rollbackStart(cancel context.CancelFunc) {
	c.mu.Lock()
	c.running = false
	c.cancel = nil
	c.mu.Unlock()
	cancel()
}

// consumeLoop continuously consumes messages from the JetStream consumer.
func (c *Component) consumeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Fetch messages with a timeout
		msgs, err := c.consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.logger.Debug("Fetch timeout or error", "error", err)
			continue
		}

		for msg := range msgs.Messages() {
			c.handleBatchTrigger(ctx, msg)
		}

		if msgs.Error() != nil && msgs.Error() != context.DeadlineExceeded {
			c.logger.Warn("Message fetch error", "error", msgs.Error())
		}
	}
}

// handleBatchTrigger processes a batch task dispatch trigger.
func (c *Component) handleBatchTrigger(ctx context.Context, msg jetstream.Msg) {
	c.batchesProcessed.Add(1)
	c.updateLastActivity()

	// Parse the trigger (handles both BaseMessage-wrapped and raw JSON from
	// workflow-processor publish_async). Use ParseNATSMessage to preserve TraceID
	// which gets lost when going through the semstreams message registry.
	trigger, err := workflow.ParseNATSMessage[workflow.BatchTriggerPayload](msg.Data())
	if err != nil {
		c.logger.Error("Failed to parse trigger", "error", err)
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}

	c.logger.Info("Processing batch task dispatch",
		"request_id", trigger.RequestID,
		"slug", trigger.Slug,
		"batch_id", trigger.BatchID)

	// Load tasks for this plan
	tasks, err := c.loadTasks(ctx, trigger.Slug)
	if err != nil {
		c.logger.Error("Failed to load tasks",
			"slug", trigger.Slug,
			"error", err)
		// Publish failure result for observability
		c.publishFailureResult(ctx, trigger, "load_tasks_failed", err.Error())
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}

	if len(tasks) == 0 {
		c.logger.Warn("No tasks found for plan",
			"slug", trigger.Slug)
		// Publish failure result for observability
		c.publishFailureResult(ctx, trigger, "no_tasks", "no tasks found for plan")
		if err := msg.Ack(); err != nil {
			c.logger.Warn("Failed to ACK message", "error", err)
		}
		return
	}

	// Execute tasks with dependency-aware parallelism
	stats, err := c.executeBatch(ctx, trigger, tasks)
	if err != nil {
		c.logger.Error("Batch execution failed",
			"slug", trigger.Slug,
			"batch_id", trigger.BatchID,
			"error", err)
		// Don't NAK - we may have partially completed
	}

	// Publish batch completion with per-batch stats
	if err := c.publishBatchResult(ctx, trigger, tasks, stats); err != nil {
		c.logger.Warn("Failed to publish batch result",
			"slug", trigger.Slug,
			"error", err)
	}

	// ACK the message
	if err := msg.Ack(); err != nil {
		c.logger.Warn("Failed to ACK message", "error", err)
	}

	c.logger.Info("Batch dispatch completed",
		"slug", trigger.Slug,
		"batch_id", trigger.BatchID,
		"task_count", len(tasks))
}

// loadTasks loads tasks from the plan's tasks.json file.
func (c *Component) loadTasks(ctx context.Context, slug string) ([]workflow.Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get working directory: %w", err)
		}
	}

	manager := workflow.NewManager(repoRoot)
	return manager.LoadTasks(ctx, slug)
}

// executeBatch executes all tasks with dependency-aware parallelism.
// When phases exist, tasks are dispatched phase-by-phase respecting phase ordering.
// Returns per-batch stats for result reporting.
func (c *Component) executeBatch(ctx context.Context, trigger *workflow.BatchTriggerPayload, tasks []workflow.Task) (*batchStats, error) {
	// Apply execution timeout
	execCtx, cancel := context.WithTimeout(ctx, c.config.GetExecutionTimeout())
	defer cancel()

	// Load phases to determine if we need phase-aware dispatch
	phases := c.loadPhases(execCtx, trigger.Slug)
	if len(phases) > 0 {
		return c.executeBatchWithPhases(execCtx, trigger, tasks, phases)
	}

	// No phases: use flat task dispatch
	graph, err := NewDependencyGraph(tasks)
	if err != nil {
		return nil, fmt.Errorf("build dependency graph: %w", err)
	}

	taskContexts := c.buildAllContexts(execCtx, tasks, trigger.Slug)
	return c.dispatchWithDependencies(execCtx, trigger, graph, taskContexts)
}

// executeBatchWithPhases implements two-level dispatch: phases then tasks within each phase.
func (c *Component) executeBatchWithPhases(ctx context.Context, trigger *workflow.BatchTriggerPayload, tasks []workflow.Task, phases []workflow.Phase) (*batchStats, error) {
	// Build phase dependency graph
	phaseGraph, err := NewPhaseDependencyGraph(phases)
	if err != nil {
		return nil, fmt.Errorf("build phase dependency graph: %w", err)
	}

	// Build task-to-phase index
	tasksByPhase := make(map[string][]workflow.Task)
	for _, t := range tasks {
		tasksByPhase[t.PhaseID] = append(tasksByPhase[t.PhaseID], t)
	}

	c.logger.Info("Starting phase-aware batch dispatch",
		"slug", trigger.Slug,
		"phase_count", len(phases),
		"task_count", len(tasks))

	aggregateStats := &batchStats{}

	// Process phases in dependency order
	for !phaseGraph.IsEmpty() {
		if ctx.Err() != nil {
			return aggregateStats, ctx.Err()
		}

		readyPhases := phaseGraph.GetReadyPhases()
		if len(readyPhases) == 0 {
			return aggregateStats, fmt.Errorf("phase graph deadlock: %d phases remaining but none ready", phaseGraph.RemainingCount())
		}

		// Execute all ready phases concurrently
		var phaseWg sync.WaitGroup
		var phaseMu sync.Mutex
		var phaseErr error

		for _, phase := range readyPhases {
			phaseTasks := tasksByPhase[phase.ID]
			if len(phaseTasks) == 0 {
				c.logger.Info("Phase has no tasks, marking complete",
					"phase_id", phase.ID,
					"phase_name", phase.Name)
				phaseGraph.MarkCompleted(phase.ID)
				continue
			}

			phaseWg.Add(1)
			go func(p *workflow.Phase, pt []workflow.Task) {
				defer phaseWg.Done()

				c.logger.Info("Dispatching phase",
					"phase_id", p.ID,
					"phase_name", p.Name,
					"task_count", len(pt))

				// Build task dependency graph for this phase's tasks
				taskGraph, err := NewDependencyGraph(pt)
				if err != nil {
					phaseMu.Lock()
					if phaseErr == nil {
						phaseErr = fmt.Errorf("phase %s: build task graph: %w", p.ID, err)
					}
					phaseMu.Unlock()
					return
				}

				// Build contexts for this phase's tasks
				taskContexts := c.buildAllContexts(ctx, pt, trigger.Slug)

				// Dispatch tasks within this phase
				phaseStats, err := c.dispatchWithDependencies(ctx, trigger, taskGraph, taskContexts)
				if err != nil {
					c.logger.Error("Phase dispatch failed",
						"phase_id", p.ID,
						"error", err)
				}

				if phaseStats != nil {
					aggregateStats.dispatched.Add(phaseStats.dispatched.Load())
					aggregateStats.failed.Add(phaseStats.failed.Load())
				}

				c.logger.Info("Phase dispatch completed",
					"phase_id", p.ID,
					"phase_name", p.Name)

				// Mark phase as completed to unblock dependents
				phaseGraph.MarkCompleted(p.ID)
			}(phase, phaseTasks)
		}

		phaseWg.Wait()

		if phaseErr != nil {
			return aggregateStats, phaseErr
		}
	}

	return aggregateStats, nil
}

// loadPhases loads phases from the plan directory. Returns nil if no phases exist.
func (c *Component) loadPhases(ctx context.Context, slug string) []workflow.Phase {
	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			c.logger.Warn("Failed to get working directory for phase loading", "error", err)
			return nil
		}
	}

	manager := workflow.NewManager(repoRoot)
	phases, err := manager.LoadPhases(ctx, slug)
	if err != nil {
		c.logger.Debug("No phases found for plan", "slug", slug, "error", err)
		return nil
	}
	return phases
}

// taskWithContext holds a task and its built context.
type taskWithContext struct {
	task             *workflow.Task
	context          *workflow.ContextPayload
	contextRequestID string // from context build request, for agentic-loop correlation
	model            string
	fallbacks        []string
}

// contextBuildResult carries both the context payload and the originating request ID.
type contextBuildResult struct {
	context   *workflow.ContextPayload
	requestID string
}

// buildAllContexts builds context for all tasks in parallel.
func (c *Component) buildAllContexts(ctx context.Context, tasks []workflow.Task, slug string) map[string]*taskWithContext {
	results := make(map[string]*taskWithContext)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := range tasks {
		task := &tasks[i]
		wg.Add(1)

		go func(t *workflow.Task) {
			defer wg.Done()

			// Early exit if context already cancelled
			if ctx.Err() != nil {
				return
			}

			// Select model based on task type
			capStr := workflow.TaskTypeCapabilities[t.Type]
			if capStr == "" {
				capStr = "coding" // Default to coding
			}
			capability := model.ParseCapability(capStr)
			if capability == "" {
				capability = model.CapabilityCoding
			}

			modelName := c.modelRegistry.Resolve(capability)
			fallbacks := c.modelRegistry.GetFallbackChain(capability)

			// Build context
			buildResult := c.buildContext(ctx, t, slug)
			if buildResult != nil {
				c.contextsBuilt.Add(1)
			}

			twc := &taskWithContext{
				task:      t,
				model:     modelName,
				fallbacks: fallbacks,
			}
			if buildResult != nil {
				twc.context = buildResult.context
				twc.contextRequestID = buildResult.requestID
			}

			mu.Lock()
			results[t.ID] = twc
			mu.Unlock()
		}(task)
	}

	wg.Wait()
	return results
}

// buildContext builds context for a single task with retry for transient failures.
// Returns a contextBuildResult carrying both the context payload and the request ID,
// or nil if context building fails after retries.
func (c *Component) buildContext(ctx context.Context, task *workflow.Task, slug string) *contextBuildResult {
	ctxTimeout, cancel := context.WithTimeout(ctx, c.config.GetContextTimeout())
	defer cancel()

	var result *contextBuildResult

	// Use retry for transient failures (network issues, temporary KV unavailability)
	retryConfig := retry.DefaultConfig()
	err := retry.Do(ctxTimeout, retryConfig, func() error {
		resp, err := c.buildContextOnce(ctxTimeout, task, slug)
		if err != nil {
			return err // retry.NonRetryable errors won't be retried
		}
		result = resp
		return nil
	})

	if err != nil {
		c.logger.Warn("Failed to build context after retries",
			"task_id", task.ID,
			"error", err,
			"retryable", !retry.IsNonRetryable(err))
		return nil
	}

	return result
}

// buildContextOnce performs a single context build attempt.
// Returns a contextBuildResult containing both the assembled context and the request ID
// that was used, so callers can pass the ID through to agentic-loop for correlation.
func (c *Component) buildContextOnce(ctx context.Context, task *workflow.Task, slug string) (*contextBuildResult, error) {
	// Build context request
	reqID := uuid.New().String()
	req := contextbuilder.ContextBuildRequest{
		RequestID:  reqID,
		TaskType:   contextbuilder.TaskTypeImplementation,
		WorkflowID: slug, // Use slug for correlation
		Files:      task.Files,
		Capability: "coding",
	}

	// Wrap request in BaseMessage (required by context-builder)
	subject := fmt.Sprintf("%s.implementation", c.config.ContextSubjectPrefix)
	baseMsg := message.NewBaseMessage(req.Schema(), &req, "task-dispatcher")
	reqBytes, err := json.Marshal(baseMsg)
	if err != nil {
		return nil, retry.NonRetryable(fmt.Errorf("marshal context request: %w", err))
	}

	// Use JetStream publish for context build request (ADR-005)
	js, err := c.natsClient.JetStream()
	if err != nil {
		return nil, fmt.Errorf("get jetstream: %w", err)
	}
	if _, err := js.Publish(ctx, subject, reqBytes); err != nil {
		return nil, fmt.Errorf("publish context request: %w", err)
	}

	// Wait for context response from KV bucket
	resp, err := c.waitForContextResponse(ctx, reqID)
	if err != nil {
		return nil, err // Already classified as retryable/non-retryable
	}

	// Return both the context payload and the request ID for downstream correlation
	return &contextBuildResult{
		context: &workflow.ContextPayload{
			Documents:  resp.Documents,
			Entities:   convertEntities(resp.Entities),
			SOPs:       resp.SOPIDs,
			TokenCount: resp.TokensUsed,
		},
		requestID: reqID,
	}, nil
}

// convertEntities converts context-builder entities to workflow entities.
func convertEntities(cbEntities []contextbuilder.EntityRef) []workflow.EntityRef {
	entities := make([]workflow.EntityRef, len(cbEntities))
	for i, e := range cbEntities {
		entities[i] = workflow.EntityRef{
			ID:      e.ID,
			Type:    e.Type,
			Content: e.Content,
		}
	}
	return entities
}

// waitForContextResponse waits for a context build response in the KV bucket using a watcher.
func (c *Component) waitForContextResponse(ctx context.Context, reqID string) (*contextbuilder.ContextBuildResponse, error) {
	js, err := c.natsClient.JetStream()
	if err != nil {
		return nil, fmt.Errorf("get jetstream: %w", err)
	}

	// Get KV bucket
	kv, err := js.KeyValue(ctx, c.config.ContextResponseBucket)
	if err != nil {
		return nil, fmt.Errorf("get kv bucket %s: %w", c.config.ContextResponseBucket, err)
	}

	// First, check if the response already exists
	entry, err := kv.Get(ctx, reqID)
	if err == nil {
		return c.parseContextResponse(entry.Value())
	}
	if err != jetstream.ErrKeyNotFound {
		return nil, fmt.Errorf("get response: %w", err)
	}

	// Create watcher for the specific key
	watcher, err := kv.Watch(ctx, reqID)
	if err != nil {
		return nil, fmt.Errorf("create kv watcher: %w", err)
	}
	defer watcher.Stop()

	// Wait for updates via reactive channel
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case entry := <-watcher.Updates():
			if entry == nil {
				// Initial nil signals watcher is ready, continue waiting
				continue
			}
			if entry.Operation() == jetstream.KeyValueDelete {
				// Key was deleted, treat as error
				return nil, fmt.Errorf("context response deleted before read")
			}
			return c.parseContextResponse(entry.Value())
		}
	}
}

// parseContextResponse unmarshals and validates a context build response.
func (c *Component) parseContextResponse(data []byte) (*contextbuilder.ContextBuildResponse, error) {
	var resp contextbuilder.ContextBuildResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, retry.NonRetryable(fmt.Errorf("unmarshal response: %w", err))
	}

	if resp.Error != "" {
		return nil, retry.NonRetryable(fmt.Errorf("context build error: %s", resp.Error))
	}

	return &resp, nil
}

// batchStats tracks per-batch execution metrics.
type batchStats struct {
	dispatched atomic.Int64
	failed     atomic.Int64
}

// dispatchWithDependencies dispatches tasks as their dependencies complete.
func (c *Component) dispatchWithDependencies(
	ctx context.Context,
	trigger *workflow.BatchTriggerPayload,
	graph *DependencyGraph,
	taskContexts map[string]*taskWithContext,
) (*batchStats, error) {
	stats := &batchStats{}
	var wg sync.WaitGroup
	completedCh := make(chan string, len(taskContexts))
	done := make(chan struct{})

	// Track running tasks
	var runningMu sync.Mutex
	running := make(map[string]bool)

	// dispatchReady starts goroutines for each ready task that has not already been launched.
	dispatchReady := func(readyTasks []*workflow.Task) {
		c.enqueueReadyTasks(ctx, trigger, readyTasks, taskContexts, stats, &wg, &runningMu, running, completedCh)
	}

	// Start with tasks that have no dependencies
	dispatchReady(graph.GetReadyTasks())

	// drainCompletions processes task-done signals and dispatches newly unblocked tasks.
	go c.drainCompletions(ctx, graph, done, completedCh, dispatchReady)

	// Wait for completion goroutine to finish
	select {
	case <-ctx.Done():
		// Wait for in-flight tasks to complete
		wg.Wait()
		return stats, ctx.Err()
	case <-done:
		wg.Wait()
		return stats, nil
	}
}

// enqueueReadyTasks iterates ready tasks, skipping unapproved or already-running
// ones, and launches a goroutine for each eligible task.
func (c *Component) enqueueReadyTasks(
	ctx context.Context,
	trigger *workflow.BatchTriggerPayload,
	readyTasks []*workflow.Task,
	taskContexts map[string]*taskWithContext,
	stats *batchStats,
	wg *sync.WaitGroup,
	runningMu *sync.Mutex,
	running map[string]bool,
	completedCh chan<- string,
) {
	for _, task := range readyTasks {
		if task.Status != workflow.TaskStatusApproved {
			c.logger.Debug("Skipping unapproved task", "task_id", task.ID, "status", task.Status)
			completedCh <- task.ID
			continue
		}

		runningMu.Lock()
		if running[task.ID] {
			runningMu.Unlock()
			continue
		}
		running[task.ID] = true
		runningMu.Unlock()

		twc := taskContexts[task.ID]
		if twc == nil {
			c.logger.Error("No context for task - marking as failed", "task_id", task.ID)
			stats.failed.Add(1)
			c.executionsFailed.Add(1)
			completedCh <- task.ID
			continue
		}

		wg.Add(1)
		go c.runTaskAsync(ctx, trigger, twc, stats, wg, completedCh)
	}
}

// runTaskAsync acquires a semaphore slot, dispatches one task, records metrics,
// and signals the completed channel when done.
func (c *Component) runTaskAsync(
	ctx context.Context,
	trigger *workflow.BatchTriggerPayload,
	twc *taskWithContext,
	stats *batchStats,
	wg *sync.WaitGroup,
	completedCh chan<- string,
) {
	defer wg.Done()

	if ctx.Err() != nil {
		completedCh <- twc.task.ID
		return
	}

	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		completedCh <- twc.task.ID
		return
	}

	if err := c.dispatchTask(ctx, trigger, twc); err != nil {
		c.logger.Error("Task dispatch failed", "task_id", twc.task.ID, "error", err)
		stats.failed.Add(1)
		c.executionsFailed.Add(1)
	} else {
		stats.dispatched.Add(1)
		c.tasksDispatched.Add(1)
	}

	completedCh <- twc.task.ID
}

// drainCompletions reads from completedCh and dispatches any newly unblocked tasks
// until the graph is exhausted or the context is cancelled.
func (c *Component) drainCompletions(
	ctx context.Context,
	graph *DependencyGraph,
	done chan struct{},
	completedCh <-chan string,
	dispatchReady func([]*workflow.Task),
) {
	defer close(done)
	for {
		select {
		case <-ctx.Done():
			return
		case taskID, ok := <-completedCh:
			if !ok {
				return
			}
			newlyReady := graph.MarkCompleted(taskID)
			if graph.IsEmpty() {
				return
			}
			dispatchReady(newlyReady)
		}
	}
}

// dispatchTask triggers the task-execution-loop workflow for a single task.
// The workflow handles the execute → validate → review OODA loop (ADR-003, ADR-005).
// Previously this dispatched directly to agentic-loop, bypassing validation and review.
func (c *Component) dispatchTask(ctx context.Context, trigger *workflow.BatchTriggerPayload, twc *taskWithContext) error {
	// Set StartedAt timestamp before dispatching
	now := time.Now()
	twc.task.StartedAt = &now

	// Build workflow trigger payload.
	// The task-execution-loop workflow expects these fields in trigger.payload:
	//   - task_id: task identifier
	//   - slug: plan slug for context
	//   - prompt: task description for the developer agent
	//   - model: LLM model to use
	//   - context_request_id: for agentic-loop context correlation
	// Generate a trace ID for this task execution if not available
	traceID := uuid.New().String()

	triggerPayload := workflow.NewSemstreamsTrigger(
		"task-execution-loop",               // workflowID
		"developer",                         // role
		twc.task.Description,                // prompt
		trigger.RequestID,                   // requestID
		trigger.Slug,                        // slug
		fmt.Sprintf("Task %s", twc.task.ID), // title
		twc.task.Description,                // description
		traceID,                             // traceID
		"",                                  // projectID
		nil,                                 // scopePatterns
		false,                               // auto
	)

	// Add task-specific fields to the Data blob
	taskData := map[string]any{
		"task_id":            twc.task.ID,
		"slug":               trigger.Slug,
		"prompt":             twc.task.Description,
		"model":              twc.model,
		"context_request_id": twc.contextRequestID,
		"request_id":         trigger.RequestID,
		"trace_id":           traceID,
	}
	taskDataBytes, err := json.Marshal(taskData)
	if err != nil {
		return fmt.Errorf("marshal task data: %w", err)
	}
	triggerPayload.Data = taskDataBytes

	// Wrap in BaseMessage
	baseMsg := message.NewBaseMessage(triggerPayload.Schema(), triggerPayload, "task-dispatcher")

	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal workflow trigger: %w", err)
	}

	// Publish to workflow trigger subject using JetStream for ordering guarantees.
	// JetStream publish waits for acknowledgment, ensuring the message is
	// written to the stream before we signal task completion. This is critical
	// for dependency ordering - dependent tasks must not be dispatched until
	// their dependencies' dispatch messages are confirmed delivered.
	subject := c.config.WorkflowTriggerSubject
	js, err := c.natsClient.JetStream()
	if err != nil {
		return fmt.Errorf("get jetstream: %w", err)
	}

	if _, err := js.Publish(ctx, subject, data); err != nil {
		return fmt.Errorf("publish workflow trigger: %w", err)
	}

	contextTokens := 0
	if twc.context != nil {
		contextTokens = twc.context.TokenCount
	}
	c.logger.Debug("Task execution workflow triggered",
		"task_id", twc.task.ID,
		"model", twc.model,
		"context_tokens", contextTokens,
		"context_request_id", twc.contextRequestID,
		"workflow_subject", subject,
		"started_at", now.Format(time.RFC3339))

	return nil
}

// DispatchResultType is the message type for dispatch results.
var DispatchResultType = message.Type{Domain: "workflow", Category: "dispatch-result", Version: "v1"}

// BatchDispatchResult is the result payload for batch dispatch.
type BatchDispatchResult struct {
	RequestID       string `json:"request_id"`
	Slug            string `json:"slug"`
	BatchID         string `json:"batch_id"`
	TaskCount       int    `json:"task_count"`
	DispatchedCount int    `json:"dispatched_count"`
	FailedCount     int    `json:"failed_count"`
	Status          string `json:"status"`
	Error           string `json:"error,omitempty"`
}

// Schema implements message.Payload.
func (r *BatchDispatchResult) Schema() message.Type {
	return DispatchResultType
}

// Validate implements message.Payload.
func (r *BatchDispatchResult) Validate() error {
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *BatchDispatchResult) MarshalJSON() ([]byte, error) {
	type Alias BatchDispatchResult
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *BatchDispatchResult) UnmarshalJSON(data []byte) error {
	type Alias BatchDispatchResult
	return json.Unmarshal(data, (*Alias)(r))
}

// publishBatchResult publishes a batch completion notification.
// Result is published to workflow.result.task-dispatcher.<slug> for observability.
func (c *Component) publishBatchResult(ctx context.Context, trigger *workflow.BatchTriggerPayload, tasks []workflow.Task, stats *batchStats) error {
	dispatched := 0
	failed := 0
	if stats != nil {
		dispatched = int(stats.dispatched.Load())
		failed = int(stats.failed.Load())
	}

	result := &BatchDispatchResult{
		RequestID:       trigger.RequestID,
		Slug:            trigger.Slug,
		BatchID:         trigger.BatchID,
		TaskCount:       len(tasks),
		DispatchedCount: dispatched,
		FailedCount:     failed,
		Status:          "completed",
	}

	// Wrap in BaseMessage and publish to well-known subject for observability
	baseMsg := message.NewBaseMessage(result.Schema(), result, c.name)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}

	resultSubject := fmt.Sprintf("workflow.result.task-dispatcher.%s", trigger.Slug)
	if err := c.natsClient.Publish(ctx, resultSubject, data); err != nil {
		return fmt.Errorf("publish result: %w", err)
	}
	c.logger.Info("Published task-dispatcher result",
		"slug", trigger.Slug,
		"request_id", trigger.RequestID,
		"subject", resultSubject,
		"dispatched", dispatched,
		"failed", failed)
	return nil
}

// publishFailureResult publishes a failure notification for observability.
func (c *Component) publishFailureResult(ctx context.Context, trigger *workflow.BatchTriggerPayload, status, errorMsg string) {
	result := &BatchDispatchResult{
		RequestID: trigger.RequestID,
		Slug:      trigger.Slug,
		BatchID:   trigger.BatchID,
		Status:    status,
		Error:     errorMsg,
	}

	baseMsg := message.NewBaseMessage(result.Schema(), result, c.name)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Error("Failed to marshal failure result", "error", err)
		return
	}

	resultSubject := fmt.Sprintf("workflow.result.task-dispatcher.%s", trigger.Slug)
	if err := c.natsClient.Publish(ctx, resultSubject, data); err != nil {
		c.logger.Error("Failed to publish failure result",
			"error", err,
			"slug", trigger.Slug,
			"status", status)
		return
	}
	c.logger.Warn("Published task-dispatcher failure result",
		"slug", trigger.Slug,
		"request_id", trigger.RequestID,
		"status", status,
		"error", errorMsg)
}

// Stop gracefully stops the component.
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	if c.cancel != nil {
		c.cancel()
	}

	c.running = false
	c.logger.Info("task-dispatcher stopped",
		"batches_processed", c.batchesProcessed.Load(),
		"tasks_dispatched", c.tasksDispatched.Load(),
		"contexts_built", c.contextsBuilt.Load(),
		"executions_failed", c.executionsFailed.Load())

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "task-dispatcher",
		Type:        "processor",
		Description: "Dispatches tasks with parallel context building and dependency-aware execution",
		Version:     "0.1.0",
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
	return taskDispatcherSchema
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
		ErrorCount: int(c.executionsFailed.Load()),
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

// IsRunning returns whether the component is running.
func (c *Component) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
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
