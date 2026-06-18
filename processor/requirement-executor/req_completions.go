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
// completions (decomposer, requirement reviewer). These are direct
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

	if loop.TaskID == exec.ReviewerTaskID {
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
		exec = c.recoverExecForTerminalTaskCompletion(ctx, taskExec)
	}
	if exec == nil {
		// A terminal task completion that matches no active or persisted
		// non-terminal execution's current node/reviewer task. Benign + common
		// for nodes whose execution already completed and was cleaned from
		// activeExecs. Recovery attempts above make the QA-recovery silent-drop
		// shape fail closed instead of wedging the DAG.
		c.logger.Debug("terminal task completion matched no active execution — dropping",
			"task_id", taskExec.TaskID, "stage", taskExec.Stage)
		return
	}

	exec.mu.Lock()
	defer exec.mu.Unlock()

	if exec.terminated {
		c.logger.Debug("terminal task completion for terminated execution — dropping",
			"task_id", taskExec.TaskID, "stage", taskExec.Stage,
			"requirement_id", exec.RequirementID)
		return
	}

	// Only handle node completions via this path. A non-current-node task that
	// still matched findExecByTaskID is the reviewer task (handled elsewhere) —
	// or a stale node task left behind by a re-dispatch whose CurrentNodeTaskID
	// did not get re-aligned (the QA-recovery-wedge suspect). Surface both at
	// DEBUG so the mismatch is diagnosable instead of silent.
	if taskExec.TaskID != exec.CurrentNodeTaskID {
		c.logger.Debug("terminal task completion is not the current node task — dropping",
			"task_id", taskExec.TaskID,
			"current_node_task_id", exec.CurrentNodeTaskID,
			"reviewer_task_id", exec.ReviewerTaskID,
			"requirement_id", exec.RequirementID, "stage", taskExec.Stage)
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
		"merge_commit", taskExec.MergeCommit,
	)

	// Surface FilesModified + MergeCommit to handleNodeCompleteLocked via
	// the synthetic event payload. The Result string is the contract
	// handleNodeCompleteLocked already parses for files_modified and
	// changes_summary; we extend it with merge_commit so the requirement-
	// scope claim/observation gate can verify each node's work landed.
	//
	// task_stage + escalation_reason are surfaced for the failure path so
	// handleNodeCompleteLocked can distinguish phase=escalated (TDD budget
	// exhausted by execution-manager — retry will burn another full budget
	// for the same upstream defect) from phase=error (transient flake worth
	// retrying). Bug-#6 fix from the 2026-05-03 /health cascade — without
	// this, escalation triggered another 6 dev dispatches × ~568K input
	// tokens each on the same broken scope.
	resultPayload, _ := json.Marshal(map[string]any{
		"files_modified":    taskExec.FilesModified,
		"changes_summary":   "",
		"merge_commit":      taskExec.MergeCommit,
		"task_stage":        taskExec.Stage,
		"escalation_reason": taskExec.EscalationReason,
	})

	event := &agentic.LoopCompletedEvent{
		TaskID:       taskExec.TaskID,
		Outcome:      outcome,
		Result:       string(resultPayload),
		WorkflowStep: taskExec.TaskID,
		CompletedAt:  taskExec.UpdatedAt,
	}

	c.handleNodeCompleteLocked(ctx, event, exec)
}

// recoverExecForTerminalTaskCompletion rehydrates an in-flight requirement
// execution from EXECUTION_STATES when task.> completion routing misses the
// activeExecs cache. This closes the QA-recovery wedge where a completed
// requirement is reopened from its deterministic KV row, the first recovered
// node finishes, and the terminal task update arrives before activeExecs has a
// matching owner for the new hashed req.run entity.
func (c *Component) recoverExecForTerminalTaskCompletion(ctx context.Context, taskExec workflow.TaskExecution) *requirementExecution {
	if taskExec.Slug == "" || taskExec.RequirementID == "" {
		return nil
	}

	loader := c.loadNonTerminalReqExecFromKV
	if c.taskCompletionReqExecLoader != nil {
		loader = c.taskCompletionReqExecLoader
	}

	exec, err := loader(ctx, taskExec.Slug, taskExec.RequirementID)
	if err != nil {
		c.logger.Warn("Failed to recover requirement execution for terminal task completion",
			"slug", taskExec.Slug,
			"requirement_id", taskExec.RequirementID,
			"task_id", taskExec.TaskID,
			"error", err)
		return nil
	}
	if exec == nil {
		return nil
	}

	exec.mu.Lock()
	match := !exec.terminated && exec.CurrentNodeTaskID == taskExec.TaskID
	currentTaskID := exec.CurrentNodeTaskID
	exec.mu.Unlock()
	if !match {
		c.logger.Debug("recovered requirement execution does not own terminal task completion — dropping",
			"slug", taskExec.Slug,
			"requirement_id", taskExec.RequirementID,
			"task_id", taskExec.TaskID,
			"current_node_task_id", currentTaskID)
		return nil
	}

	c.replaceActiveExecForRequirement(exec)
	c.logger.Info("Recovered requirement execution owner for terminal task completion",
		"entity_id", exec.EntityID,
		"slug", exec.Slug,
		"requirement_id", exec.RequirementID,
		"task_id", taskExec.TaskID)
	return exec
}

// replaceActiveExecForRequirement installs recovered as the active owner for
// its slug + requirement and removes any stale cache rows for the same
// requirement. The stale row can carry the first-pass entity ID while the
// durable EXECUTION_STATES row carries execution-manager's hashed entity ID.
func (c *Component) replaceActiveExecForRequirement(recovered *requirementExecution) {
	if recovered == nil {
		return
	}

	c.activeExecsMu.Lock()
	defer c.activeExecsMu.Unlock()

	for _, key := range c.activeExecs.Keys() {
		exec, ok := c.activeExecs.Get(key)
		if !ok || exec == nil || exec == recovered {
			continue
		}
		exec.mu.Lock()
		sameRequirement := exec.Slug == recovered.Slug && exec.RequirementID == recovered.RequirementID
		if sameRequirement {
			exec.terminated = true
			if exec.timeoutTimer != nil {
				exec.timeoutTimer.stop()
			}
			if exec.recoveryTimer != nil {
				exec.recoveryTimer.stop()
			}
		}
		exec.mu.Unlock()
		if sameRequirement {
			c.activeExecs.Delete(key) //nolint:errcheck // best-effort stale cache cleanup
		}
	}

	c.activeExecs.Set(recovered.EntityID, recovered) //nolint:errcheck // best-effort recovery routing
}

// findExecByTaskID scans active executions for one whose dispatched task IDs
// match the given task ID. Returns nil if not found.
//
// CurrentNodeTaskID and ReviewerTaskID are mutated under exec.mu by
// dispatchNextNodeLocked and dispatchRequirementReviewerLocked; the read
// here must take the lock to avoid racing with a dispatch flipping the
// task ID. Pre-fix the lock was missing, so a node-complete event arriving
// concurrently with a dispatch could match the old taskID (routing the
// completion to the wrong slot) OR miss the new dispatch entirely
// (dropping the completion → watchdog timeout instead of normal
// advancement). Mirrors the pattern at findAwaitingByRequirement. Closes
// go-reviewer Pass-1 finding H2.
func (c *Component) findExecByTaskID(taskID string) *requirementExecution {
	for _, key := range c.activeExecs.Keys() {
		exec, ok := c.activeExecs.Get(key)
		if !ok || exec == nil {
			continue
		}
		exec.mu.Lock()
		match := exec.CurrentNodeTaskID == taskID || exec.ReviewerTaskID == taskID
		exec.mu.Unlock()
		if match {
			return exec
		}
	}
	return nil
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
		case exec.DAG == nil:
			// No DAG yet — re-run synthesis. ADR-043 PR 4g: synthesis is
			// sync, so there is no in-flight decomposer state to resume.
			c.logger.Info("Resuming: re-running DAG synthesis",
				"entity_id", exec.EntityID, "slug", exec.Slug)
			c.startExecutionTimeoutLocked(exec)
			c.dispatchSynthesizerLocked(ctx, exec)
			resumed++

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
