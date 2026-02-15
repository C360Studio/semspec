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
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/retry"
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
	if config.AgentTaskSubject == "" {
		config.AgentTaskSubject = defaults.AgentTaskSubject
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

	// Parse the trigger
	var baseMsg message.BaseMessage
	if err := json.Unmarshal(msg.Data(), &baseMsg); err != nil {
		c.logger.Error("Failed to parse message", "error", err)
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}

	// Extract batch trigger payload
	var trigger workflow.BatchTriggerPayload
	payloadBytes, err := json.Marshal(baseMsg.Payload())
	if err != nil {
		c.logger.Error("Failed to marshal payload", "error", err)
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}
	if err := json.Unmarshal(payloadBytes, &trigger); err != nil {
		c.logger.Error("Failed to unmarshal trigger", "error", err)
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
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}

	if len(tasks) == 0 {
		c.logger.Warn("No tasks found for plan",
			"slug", trigger.Slug)
		if err := msg.Ack(); err != nil {
			c.logger.Warn("Failed to ACK message", "error", err)
		}
		return
	}

	// Execute tasks with dependency-aware parallelism
	stats, err := c.executeBatch(ctx, &trigger, tasks)
	if err != nil {
		c.logger.Error("Batch execution failed",
			"slug", trigger.Slug,
			"batch_id", trigger.BatchID,
			"error", err)
		// Don't NAK - we may have partially completed
	}

	// Publish batch completion with per-batch stats
	if err := c.publishBatchResult(ctx, &trigger, tasks, stats); err != nil {
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
// Returns per-batch stats for result reporting.
func (c *Component) executeBatch(ctx context.Context, trigger *workflow.BatchTriggerPayload, tasks []workflow.Task) (*batchStats, error) {
	// Apply execution timeout
	execCtx, cancel := context.WithTimeout(ctx, c.config.GetExecutionTimeout())
	defer cancel()

	// Build dependency graph
	graph, err := NewDependencyGraph(tasks)
	if err != nil {
		return nil, fmt.Errorf("build dependency graph: %w", err)
	}

	// Phase 1: Fire ALL context builds in parallel (no concurrency limit)
	taskContexts := c.buildAllContexts(execCtx, tasks, trigger.Slug)

	// Phase 2: Dispatch tasks as dependencies are satisfied
	return c.dispatchWithDependencies(execCtx, trigger, graph, taskContexts)
}

// taskWithContext holds a task and its built context.
type taskWithContext struct {
	task      *workflow.Task
	context   *workflow.ContextPayload
	model     string
	fallbacks []string
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
			cap := model.ParseCapability(capStr)
			if cap == "" {
				cap = model.CapabilityCoding
			}

			modelName := c.modelRegistry.Resolve(cap)
			fallbacks := c.modelRegistry.GetFallbackChain(cap)

			// Build context
			contextPayload := c.buildContext(ctx, t, slug)
			if contextPayload != nil {
				c.contextsBuilt.Add(1)
			}

			mu.Lock()
			results[t.ID] = &taskWithContext{
				task:      t,
				context:   contextPayload,
				model:     modelName,
				fallbacks: fallbacks,
			}
			mu.Unlock()
		}(task)
	}

	wg.Wait()
	return results
}

// buildContext builds context for a single task with retry for transient failures.
func (c *Component) buildContext(ctx context.Context, task *workflow.Task, slug string) *workflow.ContextPayload {
	ctxTimeout, cancel := context.WithTimeout(ctx, c.config.GetContextTimeout())
	defer cancel()

	var result *workflow.ContextPayload

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
func (c *Component) buildContextOnce(ctx context.Context, task *workflow.Task, slug string) (*workflow.ContextPayload, error) {
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

	// Publish context build request - retryable on failure
	if err := c.natsClient.Publish(ctx, subject, reqBytes); err != nil {
		return nil, fmt.Errorf("publish context request: %w", err)
	}

	// Wait for context response from KV bucket
	resp, err := c.waitForContextResponse(ctx, reqID)
	if err != nil {
		return nil, err // Already classified as retryable/non-retryable
	}

	// Convert to ContextPayload
	return &workflow.ContextPayload{
		Documents:  resp.Documents,
		Entities:   convertEntities(resp.Entities),
		SOPs:       resp.SOPIDs,
		TokenCount: resp.TokensUsed,
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

	// Helper to dispatch ready tasks
	dispatchReady := func(readyTasks []*workflow.Task) {
		for _, task := range readyTasks {
			runningMu.Lock()
			if running[task.ID] {
				runningMu.Unlock()
				continue
			}
			running[task.ID] = true
			runningMu.Unlock()

			twc := taskContexts[task.ID]
			if twc == nil {
				// Task has no context - mark as failed and signal completion
				c.logger.Error("No context for task - marking as failed", "task_id", task.ID)
				stats.failed.Add(1)
				c.executionsFailed.Add(1)
				completedCh <- task.ID
				continue
			}

			wg.Add(1)
			go func(t *taskWithContext) {
				defer wg.Done()

				// Check context early
				if ctx.Err() != nil {
					completedCh <- t.task.ID
					return
				}

				// Acquire semaphore slot
				select {
				case c.sem <- struct{}{}:
					defer func() { <-c.sem }()
				case <-ctx.Done():
					completedCh <- t.task.ID
					return
				}

				// Dispatch task
				if err := c.dispatchTask(ctx, trigger, t); err != nil {
					c.logger.Error("Task dispatch failed",
						"task_id", t.task.ID,
						"error", err)
					stats.failed.Add(1)
					c.executionsFailed.Add(1)
				} else {
					stats.dispatched.Add(1)
					c.tasksDispatched.Add(1)
				}

				// Signal completion
				completedCh <- t.task.ID
			}(twc)
		}
	}

	// Start with tasks that have no dependencies
	readyTasks := graph.GetReadyTasks()
	dispatchReady(readyTasks)

	// Process completions and dispatch newly ready tasks
	go func() {
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
	}()

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

// dispatchTask dispatches a single task to an agent.
func (c *Component) dispatchTask(ctx context.Context, trigger *workflow.BatchTriggerPayload, twc *taskWithContext) error {
	payload := workflow.TaskExecutionPayload{
		Task:      *twc.task,
		Slug:      trigger.Slug,
		BatchID:   trigger.BatchID,
		Context:   twc.context,
		Model:     twc.model,
		Fallbacks: twc.fallbacks,
	}

	// Create base message using registered type
	baseMsg := message.NewBaseMessage(
		payload.Schema(),
		&payload,
		"task-dispatcher",
	)

	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	// Publish to agent task subject using JetStream for ordering guarantees.
	// JetStream publish waits for acknowledgment, ensuring the message is
	// written to the stream before we signal task completion. This is critical
	// for dependency ordering - dependent tasks must not be dispatched until
	// their dependencies' dispatch messages are confirmed delivered.
	subject := c.config.AgentTaskSubject
	js, err := c.natsClient.JetStream()
	if err != nil {
		return fmt.Errorf("get jetstream: %w", err)
	}

	if _, err := js.Publish(ctx, subject, data); err != nil {
		return fmt.Errorf("publish task: %w", err)
	}

	contextTokens := 0
	if twc.context != nil {
		contextTokens = twc.context.TokenCount
	}
	c.logger.Debug("Task dispatched",
		"task_id", twc.task.ID,
		"model", twc.model,
		"context_tokens", contextTokens)

	return nil
}

// BatchDispatchResult is the result payload for batch dispatch.
type BatchDispatchResult struct {
	RequestID       string `json:"request_id"`
	Slug            string `json:"slug"`
	BatchID         string `json:"batch_id"`
	TaskCount       int    `json:"task_count"`
	DispatchedCount int    `json:"dispatched_count"`
	FailedCount     int    `json:"failed_count"`
	Status          string `json:"status"`
}

// Schema implements message.Payload.
func (r *BatchDispatchResult) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "result", Version: "v1"}
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

	baseMsg := message.NewBaseMessage(
		message.Type{Domain: "workflow", Category: "result", Version: "v1"},
		result,
		"task-dispatcher",
	)

	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}

	subject := fmt.Sprintf("%s.%s", c.config.OutputSubject, trigger.Slug)
	return c.natsClient.Publish(ctx, subject, data)
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
