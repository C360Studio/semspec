package requirementexecutor

import (
	"context"
	"encoding/json"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
	"github.com/nats-io/nats.go/jetstream"
)

// watchLoopCompletions watches the AGENT_LOOPS KV bucket for agentic loop
// completions (decomposer, requirement reviewer, red-team). These are direct
// agentic loops dispatched by requirement-executor — not routed through
// execution-manager's TDD pipeline.
//
// AGENT_LOOPS is written by semstreams' agentic-dispatch and provides durable,
// replayable state — no messages lost on startup races or restarts.
func (c *Component) watchLoopCompletions(ctx context.Context) {
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Warn("Cannot watch AGENT_LOOPS: no JetStream", "error", err)
		return
	}

	bucket, err := workflow.WaitForKVBucket(ctx, js, "AGENT_LOOPS")
	if err != nil {
		c.logger.Warn("AGENT_LOOPS bucket not available — loop completion watcher disabled", "error", err)
		return
	}

	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		c.logger.Warn("Failed to watch AGENT_LOOPS — loop completion watcher disabled", "error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Loop completion watcher started (watching AGENT_LOOPS)")

	for entry := range watcher.Updates() {
		if entry == nil {
			continue // end of initial replay
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}
		c.handleLoopEntityUpdate(ctx, entry)
	}
}

// handleLoopEntityUpdate processes a single AGENT_LOOPS KV update.
// Routes terminal loops to the appropriate handler based on TaskID matching.
func (c *Component) handleLoopEntityUpdate(ctx context.Context, entry jetstream.KeyValueEntry) {
	var loop agentic.LoopEntity
	if err := json.Unmarshal(entry.Value(), &loop); err != nil {
		return
	}

	if !loop.State.IsTerminal() {
		return
	}
	if loop.TaskID == "" {
		return
	}

	exec := c.findExecByTaskID(loop.TaskID)
	if exec == nil {
		return
	}

	exec.mu.Lock()
	defer exec.mu.Unlock()

	if exec.terminated {
		return
	}

	c.updateLastActivity()

	c.logger.Info("Loop completion received via KV",
		"slug", exec.Slug,
		"requirement_id", exec.RequirementID,
		"task_id", loop.TaskID,
		"workflow_step", loop.WorkflowStep,
		"outcome", loop.Outcome,
	)

	event := &agentic.LoopCompletedEvent{
		LoopID:       loop.ID,
		TaskID:       loop.TaskID,
		Outcome:      loop.Outcome,
		Result:       loop.Result,
		WorkflowSlug: loop.WorkflowSlug,
		WorkflowStep: loop.WorkflowStep,
		CompletedAt:  loop.CompletedAt,
	}
	if event.CompletedAt.IsZero() {
		event.CompletedAt = time.Now()
	}

	switch {
	case loop.TaskID == exec.DecomposerTaskID:
		c.handleDecomposerCompleteLocked(ctx, event, exec)
	case loop.TaskID == exec.RedTeamTaskID:
		c.handleRequirementRedTeamCompleteLocked(ctx, event, exec)
	case loop.TaskID == exec.ReviewerTaskID:
		c.handleRequirementReviewerCompleteLocked(ctx, event, exec)
	}
}

// watchTaskCompletions watches the EXECUTION_STATES KV bucket for TDD pipeline
// node completions (task.> keys). execution-manager writes terminal state here
// via syncToStore when TDD nodes finish (approved, escalated, error).
//
// This is separate from AGENT_LOOPS because execution-manager is a pipeline
// orchestrator, not an agentic loop — it doesn't write to AGENT_LOOPS.
func (c *Component) watchTaskCompletions(ctx context.Context) {
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Warn("Cannot watch EXECUTION_STATES: no JetStream", "error", err)
		return
	}

	bucket, err := workflow.WaitForKVBucket(ctx, js, "EXECUTION_STATES")
	if err != nil {
		c.logger.Warn("EXECUTION_STATES bucket not available — task completion watcher disabled", "error", err)
		return
	}

	watcher, err := bucket.Watch(ctx, "task.>")
	if err != nil {
		c.logger.Warn("Failed to watch EXECUTION_STATES task.> — task completion watcher disabled", "error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Task completion watcher started (watching EXECUTION_STATES task.>)")

	for entry := range watcher.Updates() {
		if entry == nil {
			continue
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}
		c.handleTaskStateChange(ctx, entry)
	}
}

// handleTaskStateChange processes a single EXECUTION_STATES task.> KV update.
// Routes terminal TDD pipeline completions to handleNodeCompleteLocked.
func (c *Component) handleTaskStateChange(ctx context.Context, entry jetstream.KeyValueEntry) {
	var taskExec workflow.TaskExecution
	if err := json.Unmarshal(entry.Value(), &taskExec); err != nil {
		return
	}

	if !workflow.IsTerminalTaskStage(taskExec.Stage) {
		return
	}
	if taskExec.TaskID == "" {
		return
	}

	exec := c.findExecByTaskID(taskExec.TaskID)
	if exec == nil {
		return
	}

	exec.mu.Lock()
	defer exec.mu.Unlock()

	if exec.terminated {
		return
	}

	// Only handle node completions via this path.
	if taskExec.TaskID != exec.CurrentNodeTaskID {
		return
	}

	c.updateLastActivity()

	outcome := agentic.OutcomeSuccess
	if taskExec.Stage != "approved" {
		outcome = agentic.OutcomeFailed
	}

	c.logger.Info("Task completion received via KV",
		"slug", exec.Slug,
		"requirement_id", exec.RequirementID,
		"task_id", taskExec.TaskID,
		"stage", taskExec.Stage,
		"outcome", outcome,
	)

	event := &agentic.LoopCompletedEvent{
		TaskID:       taskExec.TaskID,
		Outcome:      outcome,
		WorkflowStep: taskExec.TaskID,
		CompletedAt:  taskExec.UpdatedAt,
	}

	c.handleNodeCompleteLocked(ctx, event, exec)
}

// findExecByTaskID scans active executions for one whose dispatched task IDs
// match the given task ID. Returns nil if not found.
func (c *Component) findExecByTaskID(taskID string) *requirementExecution {
	var found *requirementExecution
	c.activeExecutions.Range(func(_, value any) bool {
		exec := value.(*requirementExecution)
		if exec.DecomposerTaskID == taskID ||
			exec.CurrentNodeTaskID == taskID ||
			exec.ReviewerTaskID == taskID ||
			exec.RedTeamTaskID == taskID {
			found = exec
			return false
		}
		return true
	})
	return found
}
