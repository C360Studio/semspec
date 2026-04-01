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

	replayDone := false
	for entry := range watcher.Updates() {
		if entry == nil {
			replayDone = true
			c.logger.Info("AGENT_LOOPS replay complete for requirement-executor")
			c.replayGate.Done()
			continue
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}
		// During replay, still process entries so that completed loops from
		// before the crash are routed to their executions (advancing state
		// in-memory). The resumption goroutine handles anything left over.
		_ = replayDone // used for documentation; all entries are processed
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

	replayDone := false
	for entry := range watcher.Updates() {
		if entry == nil {
			replayDone = true
			c.logger.Info("EXECUTION_STATES task.> replay complete for requirement-executor")
			c.replayGate.Done()
			continue
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}
		_ = replayDone
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
	for _, key := range c.activeExecs.Keys() {
		exec, ok := c.activeExecs.Get(key)
		if !ok {
			continue
		}
		if exec.DecomposerTaskID == taskID ||
			exec.CurrentNodeTaskID == taskID ||
			exec.ReviewerTaskID == taskID ||
			exec.RedTeamTaskID == taskID {
			found = exec
			break
		}
	}
	return found
}

// resumeInterruptedExecutions runs after all watcher replays are complete.
// It scans recovered non-terminal executions and re-dispatches work for any
// that didn't receive a completion event during replay (i.e., the agent was
// mid-processing when the crash occurred).
func (c *Component) resumeInterruptedExecutions(ctx context.Context) {
	keys := c.activeExecs.Keys()
	if len(keys) == 0 {
		return
	}

	c.logger.Info("Checking for interrupted executions to resume", "candidates", len(keys))

	resumed := 0
	for _, key := range keys {
		exec, ok := c.activeExecs.Get(key)
		if !ok {
			continue
		}

		exec.mu.Lock()
		if exec.terminated {
			exec.mu.Unlock()
			continue
		}

		switch {
		case exec.DAG == nil && exec.DecomposerTaskID == "":
			// No DAG and no decomposer dispatched — re-dispatch decomposer.
			c.logger.Info("Resuming: re-dispatching decomposer",
				"entity_id", exec.EntityID, "slug", exec.Slug)
			c.startExecutionTimeoutLocked(exec)
			c.dispatchDecomposerLocked(ctx, exec)
			resumed++

		case exec.DAG == nil && exec.DecomposerTaskID != "":
			// Decomposer was dispatched but hasn't completed. The replay
			// may have already delivered the completion. If not, the timeout
			// mechanism will handle it.
			c.logger.Debug("Decomposer in-flight, waiting for completion or timeout",
				"entity_id", exec.EntityID, "decomposer_task_id", exec.DecomposerTaskID)

		case exec.DAG != nil && exec.CurrentNodeIdx < len(exec.SortedNodeIDs):
			// DAG exists, mid-execution. Check if the current node already
			// completed during replay (it would have been marked visited).
			if exec.CurrentNodeIdx >= 0 {
				nodeID := exec.SortedNodeIDs[exec.CurrentNodeIdx]
				if exec.VisitedNodes[nodeID] {
					// Current node completed during replay — advance to next.
					c.logger.Info("Resuming: advancing past completed node",
						"entity_id", exec.EntityID, "node_id", nodeID)
					c.dispatchNextNodeLocked(ctx, exec)
					resumed++
				} else if exec.CurrentNodeTaskID != "" {
					// Node in-flight — wait for completion or timeout.
					c.logger.Debug("Node in-flight, waiting for completion or timeout",
						"entity_id", exec.EntityID, "node_id", nodeID,
						"task_id", exec.CurrentNodeTaskID)
				} else {
					// Node not started — dispatch it.
					c.logger.Info("Resuming: dispatching node",
						"entity_id", exec.EntityID, "node_id", nodeID)
					c.startExecutionTimeoutLocked(exec)
					c.dispatchNextNodeLocked(ctx, exec)
					resumed++
				}
			} else {
				// CurrentNodeIdx is -1 (before first node) — start execution.
				c.logger.Info("Resuming: starting node execution from beginning",
					"entity_id", exec.EntityID)
				c.startExecutionTimeoutLocked(exec)
				c.dispatchNextNodeLocked(ctx, exec)
				resumed++
			}

		case exec.DAG != nil && exec.CurrentNodeIdx >= len(exec.SortedNodeIDs):
			// All nodes visited — dispatch requirement reviewer if not already done.
			if exec.ReviewerTaskID == "" {
				c.logger.Info("Resuming: all nodes done, dispatching reviewer",
					"entity_id", exec.EntityID)
				c.startExecutionTimeoutLocked(exec)
				c.dispatchRequirementReviewerLocked(ctx, exec)
				resumed++
			} else {
				c.logger.Debug("Reviewer in-flight, waiting for completion or timeout",
					"entity_id", exec.EntityID, "reviewer_task_id", exec.ReviewerTaskID)
			}

		default:
			c.logger.Debug("Recovered execution in unknown state, relying on timeout",
				"entity_id", exec.EntityID,
				"has_dag", exec.DAG != nil,
				"current_node_idx", exec.CurrentNodeIdx)
		}

		exec.mu.Unlock()
	}

	if resumed > 0 {
		c.logger.Info("Interrupted executions resumed", "count", resumed)
	}
}
