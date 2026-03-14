// Package executionorchestrator provides a component that orchestrates the task
// execution pipeline: developer → structural validator → code reviewer.
//
// It replaces the reactive task-execution-loop (18 rules) with a single component
// that manages the 3-stage pipeline using entity triples for state and JSON rules
// for terminal transitions.
//
// Pipeline stages:
//  1. Developer — LLM-driven code generation with tool access
//  2. Structural Validator — deterministic checklist validation of modified files
//  3. Code Reviewer — LLM-driven code review with verdict + feedback
//
// On reviewer rejection with remaining budget, the developer is retried with
// accumulated feedback. On budget exhaustion or non-fixable rejection types
// (misscoped, architectural, too_big), the execution escalates.
//
// Terminal status transitions (completed, escalated, failed) are owned by the
// JSON rule processor, not this component. This component writes workflow.phase;
// rules react and set workflow.status + publish events.
package executionorchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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
	"github.com/nats-io/nats.go"
)

const (
	componentName    = "execution-orchestrator"
	componentVersion = "0.1.0"

	// WorkflowSlugTaskExecution identifies this workflow in agent TaskMessages.
	WorkflowSlugTaskExecution = "semspec-task-execution"

	// Pipeline stage constants used as WorkflowStep in TaskMessages.
	stageDevelop  = "develop"
	stageValidate = "validate"
	stageReview   = "review"

	// Phase values written to entity triples.
	phaseDeveloping       = "developing"
	phaseValidating       = "validating"
	phaseReviewing        = "reviewing"
	phaseApproved         = "approved"
	phaseEscalated        = "escalated"
	phaseError            = "error"
	phaseValidationFailed = "validation_failed"
	phaseRejected         = "rejected"

	// Trigger subject.
	subjectExecutionTrigger = "workflow.trigger.task-execution-loop"

	// subjectLoopCompleted is the JetStream subject for agentic loop completion events.
	subjectLoopCompleted = "agentic.loop_completed.v1"

	// Downstream dispatch subjects.
	subjectDeveloperTask     = "dev.task.development"
	subjectValidatorAsync    = "workflow.async.structural-validator"
	subjectCodeReviewerAsync = "workflow.async.task-code-reviewer"

	// Rejection types that are NOT retryable — escalate immediately.
	rejectionTypeMisscoped     = "misscoped"
	rejectionTypeArchitectural = "architectural"
	rejectionTypeTooBig        = "too_big"
)

// worktreeManager defines the sandbox operations used by the orchestrator.
// Satisfied by *sandbox.Client; narrow interface enables mock injection in tests.
type worktreeManager interface {
	CreateWorktree(ctx context.Context, taskID string) (*sandbox.WorktreeInfo, error)
	DeleteWorktree(ctx context.Context, taskID string) error
	MergeWorktree(ctx context.Context, taskID string) error
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
	sandbox      worktreeManager // nil when sandbox is disabled

	inputPorts  []component.Port
	outputPorts []component.Port

	// activeExecutions maps entityID → *taskExecution.
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
		config:     cfg,
		natsClient: deps.NATSClient,
		logger:     logger,
		platform:   deps.Platform,
		sandbox:    newWorktreeManager(cfg.SandboxURL),
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

	c.logger.Info("Starting execution-orchestrator")

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
		case subjectExecutionTrigger:
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

	c.logger.Info("Stopping execution-orchestrator",
		"triggers_processed", c.triggersProcessed.Load(),
		"executions_approved", c.executionsApproved.Load(),
		"executions_escalated", c.executionsEscalated.Load(),
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
		exec := value.(*taskExecution)
		exec.mu.Lock()
		if exec.timeoutTimer != nil {
			exec.timeoutTimer.stop()
		}
		// Discard worktrees for any active executions on shutdown.
		c.discardWorktree(exec)
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

	trigger, err := payloads.ParseReactivePayload[workflow.TriggerPayload](msg.Data)
	if err != nil {
		c.logger.Error("Failed to parse execution trigger", "error", err)
		c.errors.Add(1)
		return
	}

	if trigger.Slug == "" || trigger.TaskID == "" {
		c.logger.Error("Trigger missing slug or task_id")
		c.errors.Add(1)
		return
	}

	entityID := fmt.Sprintf("local.semspec.workflow.task-execution.execution.%s-%s", trigger.Slug, trigger.TaskID)

	c.logger.Info("Task execution trigger received",
		"slug", trigger.Slug,
		"task_id", trigger.TaskID,
		"entity_id", entityID,
		"trace_id", trigger.TraceID,
	)

	exec := &taskExecution{
		EntityID:         entityID,
		Slug:             trigger.Slug,
		TaskID:           trigger.TaskID,
		Iteration:        0,
		MaxIterations:    c.config.MaxIterations,
		Title:            trigger.Title,
		Description:      trigger.Description,
		ProjectID:        trigger.ProjectID,
		Prompt:           trigger.Prompt,
		Model:            trigger.Model,
		ContextRequestID: trigger.ContextRequestID,
		TraceID:          trigger.TraceID,
		LoopID:           trigger.LoopID,
		RequestID:        trigger.RequestID,
	}

	if _, loaded := c.activeExecutions.LoadOrStore(entityID, exec); loaded {
		c.logger.Debug("Duplicate trigger for active execution, skipping", "entity_id", entityID)
		return
	}

	// Write initial entity triples.
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.Type, "task-execution")
	if err := c.tripleWriter.WriteTriple(ctx, entityID, wf.Phase, phaseDeveloping); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseDeveloping, "error", err)
	}
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.Slug, trigger.Slug)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.TaskID, trigger.TaskID)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.Title, trigger.Title)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.ProjectID, trigger.ProjectID)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.Iteration, 0)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.MaxIterations, c.config.MaxIterations)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.TraceID, trigger.TraceID)

	// Create sandbox worktree if sandbox is enabled.
	if c.sandbox != nil {
		wtInfo, wtErr := c.sandbox.CreateWorktree(ctx, exec.TaskID)
		if wtErr != nil {
			c.logger.Error("Failed to create worktree, proceeding without sandbox isolation",
				"slug", exec.Slug,
				"task_id", exec.TaskID,
				"error", wtErr,
			)
		} else {
			exec.WorktreePath = wtInfo.Path
			exec.WorktreeBranch = wtInfo.Branch
			_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.WorktreePath, wtInfo.Path)
			_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.WorktreeBranch, wtInfo.Branch)
			c.logger.Info("Worktree created",
				"slug", exec.Slug,
				"task_id", exec.TaskID,
				"path", wtInfo.Path,
				"branch", wtInfo.Branch,
			)
		}
	}

	// Publish initial entity snapshot for graph observability.
	c.publishEntity(ctx, NewTaskExecutionEntity(exec).WithPhase(phaseDeveloping))

	// Lock before timeout and dispatch to prevent race where timeout fires
	// before we finish initializing the execution.
	exec.mu.Lock()
	defer exec.mu.Unlock()
	c.startExecutionTimeout(exec)
	c.dispatchDeveloperLocked(ctx, exec)
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

	if event.WorkflowSlug != WorkflowSlugTaskExecution {
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
	exec := execVal.(*taskExecution)

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
	case stageDevelop:
		c.handleDeveloperCompleteLocked(ctx, event, exec)
	case stageValidate:
		c.handleValidatorCompleteLocked(ctx, event, exec)
	case stageReview:
		c.handleReviewerCompleteLocked(ctx, event, exec)
	default:
		c.logger.Debug("Unknown workflow step", "step", event.WorkflowStep, "entity_id", entityID)
	}
}

// ---------------------------------------------------------------------------
// Stage 1: Developer complete
// ---------------------------------------------------------------------------

func (c *Component) handleDeveloperCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *taskExecution) {
	c.taskIDIndex.Delete(exec.DeveloperTaskID)

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

	// Dispatch structural validator.
	c.dispatchValidatorLocked(ctx, exec)
}

// ---------------------------------------------------------------------------
// Stage 2: Validator complete
// ---------------------------------------------------------------------------

func (c *Component) handleValidatorCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *taskExecution) {
	c.taskIDIndex.Delete(exec.ValidatorTaskID)

	var result payloads.ValidationResult
	if err := json.Unmarshal([]byte(event.Result), &result); err != nil {
		c.logger.Warn("Failed to parse validation result", "slug", exec.Slug, "error", err)
		// Default to passed on parse failure — let reviewer catch issues.
		exec.ValidationPassed = true
	} else {
		exec.ValidationPassed = result.Passed
		exec.ValidationResults = result.CheckResults
	}

	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.ValidationPassed, fmt.Sprintf("%t", exec.ValidationPassed))

	if !exec.ValidationPassed {
		// Validation failed — retry developer with validation feedback or escalate.
		if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseValidationFailed); err != nil {
			c.logger.Error("Failed to write phase triple", "phase", phaseValidationFailed, "error", err)
		}

		if exec.Iteration+1 < exec.MaxIterations {
			c.startDeveloperRetryLocked(ctx, exec, "Structural validation failed. Fix the following issues:\n"+string(exec.ValidationResults))
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
	c.dispatchReviewerLocked(ctx, exec)
}

// ---------------------------------------------------------------------------
// Stage 3: Reviewer complete
// ---------------------------------------------------------------------------

func (c *Component) handleReviewerCompleteLocked(ctx context.Context, event *agentic.LoopCompletedEvent, exec *taskExecution) {
	c.taskIDIndex.Delete(exec.ReviewerTaskID)

	var result payloads.TaskCodeReviewResult
	if err := json.Unmarshal([]byte(event.Result), &result); err != nil {
		c.logger.Warn("Failed to parse code review result, defaulting to rejected for safety",
			"slug", exec.Slug, "error", err)
		result.Verdict = "rejected"
		result.RejectionType = "fixable"
		result.Feedback = "parse failure — could not read reviewer response"
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
		"iteration", exec.Iteration,
	)

	if result.Verdict == "approved" {
		c.markApprovedLocked(ctx, exec)
		return
	}

	// Rejected — check rejection type and budget.
	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseRejected); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseRejected, "error", err)
	}

	switch result.RejectionType {
	case rejectionTypeMisscoped, rejectionTypeArchitectural, rejectionTypeTooBig:
		// Non-fixable rejection — escalate immediately regardless of budget.
		c.markEscalatedLocked(ctx, exec, fmt.Sprintf("non-fixable rejection: %s", result.RejectionType))
	default:
		// Fixable rejection — retry if budget remains.
		if exec.Iteration+1 < exec.MaxIterations {
			c.startDeveloperRetryLocked(ctx, exec, result.Feedback)
		} else {
			c.markEscalatedLocked(ctx, exec, "fixable rejections exceeded iteration budget")
		}
	}
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

	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseApproved); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseApproved, "error", err)
	}

	c.executionsApproved.Add(1)
	c.executionsCompleted.Add(1)

	c.logger.Info("Task execution approved",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"iteration", exec.Iteration,
	)

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

	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseEscalated); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseEscalated, "error", err)
	}
	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.EscalationReason, reason)

	c.executionsEscalated.Add(1)
	c.executionsCompleted.Add(1)

	c.logger.Info("Task execution escalated",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"iteration", exec.Iteration,
		"reason", reason,
	)

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

	if err := c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.Phase, phaseError); err != nil {
		c.logger.Error("Failed to write phase triple", "phase", phaseError, "error", err)
	}
	_ = c.tripleWriter.WriteTriple(ctx, exec.EntityID, wf.ErrorReason, reason)

	c.errors.Add(1)
	c.executionsCompleted.Add(1)

	c.logger.Error("Task execution failed",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"reason", reason,
	)

	c.publishEntity(context.Background(), NewTaskExecutionEntity(exec).WithPhase(phaseError).WithErrorReason(reason))
	c.cleanupExecutionLocked(exec)
}

// cleanupExecutionLocked removes execution from maps and cancels timeout.
// Caller must hold exec.mu.
func (c *Component) cleanupExecutionLocked(exec *taskExecution) {
	if exec.timeoutTimer != nil {
		exec.timeoutTimer.stop()
	}
	c.taskIDIndex.Delete(exec.DeveloperTaskID)
	c.taskIDIndex.Delete(exec.ValidatorTaskID)
	c.taskIDIndex.Delete(exec.ReviewerTaskID)
	c.activeExecutions.Delete(exec.EntityID)
}

// ---------------------------------------------------------------------------
// Retry logic
// ---------------------------------------------------------------------------

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
// Agent dispatch: Developer
// ---------------------------------------------------------------------------

func (c *Component) dispatchDeveloperLocked(ctx context.Context, exec *taskExecution) {
	taskID := fmt.Sprintf("dev-%s-%s", exec.EntityID, uuid.New().String())
	exec.DeveloperTaskID = taskID
	c.taskIDIndex.Store(taskID, exec.EntityID)

	// Build the developer request payload.
	req := &payloads.DeveloperRequest{
		ExecutionID:      exec.EntityID,
		RequestID:        exec.RequestID,
		Slug:             exec.Slug,
		DeveloperTaskID:  exec.TaskID,
		Model:            exec.Model,
		ContextRequestID: exec.ContextRequestID,
		TraceID:          exec.TraceID,
		LoopID:           exec.LoopID,
	}

	// Thread sandbox worktree reference so developer operates in isolation.
	if c.sandbox != nil && exec.WorktreePath != "" {
		req.SandboxTaskID = exec.TaskID
	}

	if exec.Iteration > 0 && exec.Feedback != "" {
		// Revision: prepend original prompt with feedback.
		req.Revision = true
		req.Feedback = exec.Feedback
		req.Prompt = exec.Prompt + "\n\n---\n\nREVISION REQUEST: Your previous implementation was rejected.\n\n" + exec.Feedback
	} else {
		req.Prompt = exec.Prompt
	}

	// Publish typed request to developer's trigger subject.
	if err := c.publishBaseMessage(ctx, subjectDeveloperTask, req); err != nil {
		c.logger.Error("Failed to publish developer request",
			"slug", exec.Slug, "task_id", exec.TaskID, "error", err)
		c.markErrorLocked(ctx, exec, fmt.Sprintf("dispatch developer failed: %v", err))
		return
	}

	// Publish TaskMessage for agentic-loop tracking.
	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleGeneral,
		Model:        exec.Model,
		WorkflowSlug: WorkflowSlugTaskExecution,
		WorkflowStep: stageDevelop,
	}
	c.publishTask(ctx, "agent.task.development", task)

	c.logger.Info("Dispatched developer",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"iteration", exec.Iteration,
		"developer_task_id", taskID,
	)
}

// ---------------------------------------------------------------------------
// Agent dispatch: Structural Validator
// ---------------------------------------------------------------------------

func (c *Component) dispatchValidatorLocked(ctx context.Context, exec *taskExecution) {
	taskID := fmt.Sprintf("val-%s-%s", exec.EntityID, uuid.New().String())
	exec.ValidatorTaskID = taskID
	c.taskIDIndex.Store(taskID, exec.EntityID)

	req := &payloads.ValidationRequest{
		ExecutionID:   exec.EntityID,
		Slug:          exec.Slug,
		FilesModified: exec.FilesModified,
		TraceID:       exec.TraceID,
	}

	if err := c.publishBaseMessage(ctx, subjectValidatorAsync, req); err != nil {
		c.logger.Error("Failed to publish validation request",
			"slug", exec.Slug, "task_id", exec.TaskID, "error", err)
		c.markErrorLocked(ctx, exec, fmt.Sprintf("dispatch validator failed: %v", err))
		return
	}

	// Publish TaskMessage for agentic-loop tracking.
	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleGeneral,
		Model:        exec.Model,
		WorkflowSlug: WorkflowSlugTaskExecution,
		WorkflowStep: stageValidate,
	}
	c.publishTask(ctx, "agent.task.validation", task)

	c.logger.Info("Dispatched validator",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"iteration", exec.Iteration,
		"files_modified", len(exec.FilesModified),
	)
}

// ---------------------------------------------------------------------------
// Agent dispatch: Code Reviewer
// ---------------------------------------------------------------------------

func (c *Component) dispatchReviewerLocked(ctx context.Context, exec *taskExecution) {
	taskID := fmt.Sprintf("rev-%s-%s", exec.EntityID, uuid.New().String())
	exec.ReviewerTaskID = taskID
	c.taskIDIndex.Store(taskID, exec.EntityID)

	req := &payloads.TaskCodeReviewRequest{
		ExecutionID:   exec.EntityID,
		RequestID:     exec.RequestID,
		Slug:          exec.Slug,
		DeveloperTask: exec.TaskID,
		Output:        exec.DeveloperOutput,
		TraceID:       exec.TraceID,
		LoopID:        exec.LoopID,
	}

	if err := c.publishBaseMessage(ctx, subjectCodeReviewerAsync, req); err != nil {
		c.logger.Error("Failed to publish code review request",
			"slug", exec.Slug, "task_id", exec.TaskID, "error", err)
		c.markErrorLocked(ctx, exec, fmt.Sprintf("dispatch reviewer failed: %v", err))
		return
	}

	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleReviewer,
		Model:        exec.Model,
		WorkflowSlug: WorkflowSlugTaskExecution,
		WorkflowStep: stageReview,
	}
	c.publishTask(ctx, "agent.task.reviewer", task)

	c.logger.Info("Dispatched code reviewer",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"iteration", exec.Iteration,
	)
}

// ---------------------------------------------------------------------------
// Worktree lifecycle helpers
// ---------------------------------------------------------------------------

// mergeWorktree merges the worktree for the given execution back into main.
// Best-effort: failures are logged and recorded as a triple but never block
// the terminal state transition. Caller must hold exec.mu.
func (c *Component) mergeWorktree(exec *taskExecution) {
	if c.sandbox == nil || exec.WorktreePath == "" {
		return
	}
	if err := c.sandbox.MergeWorktree(context.Background(), exec.TaskID); err != nil {
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
// Triple and task publishing helpers
// ---------------------------------------------------------------------------

// publishBaseMessage wraps a payload in a BaseMessage and publishes to JetStream.
func (c *Component) publishBaseMessage(ctx context.Context, subject string, payload message.Payload) error {
	baseMsg := message.NewBaseMessage(payload.Schema(), payload, componentName)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal base message: %w", err)
	}

	if c.natsClient != nil {
		if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
			return fmt.Errorf("publish to %s: %w", subject, err)
		}
	}
	return nil
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
		Description: "Orchestrates task execution pipeline: developer → validator → reviewer with retry and escalation",
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
