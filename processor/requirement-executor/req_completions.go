package requirementexecutor

import (
	"context"
	"encoding/json"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
	"github.com/nats-io/nats.go/jetstream"
)

// watchReqCompletions watches EXECUTION_STATES KV for RequirementExecution updates.
// When execution-manager writes completion data (LastCompletionTaskID, etc.) to a
// req.> entry, this watcher detects it and routes to the appropriate handler.
//
// This replaces the ephemeral agent.complete.> JetStream consumer for ALL completion
// types: decomposer, TDD nodes, red-team, and requirement reviewer. The KV write
// provides durable delivery with replay — no messages lost on startup races or restarts.
//
// Follows the plan-manager/execution_events.go pattern.
func (c *Component) watchReqCompletions(ctx context.Context) {
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Warn("Cannot watch EXECUTION_STATES: no JetStream", "error", err)
		return
	}

	bucket, err := workflow.WaitForKVBucket(ctx, js, "EXECUTION_STATES")
	if err != nil {
		c.logger.Warn("EXECUTION_STATES bucket not available — KV completion watcher disabled", "error", err)
		return
	}

	watcher, err := bucket.Watch(ctx, "req.>")
	if err != nil {
		c.logger.Warn("Failed to watch EXECUTION_STATES req entries — KV completion watcher disabled", "error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Requirement completion watcher started (watching EXECUTION_STATES req.>)")

	for entry := range watcher.Updates() {
		if entry == nil {
			continue // end of initial replay
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}
		c.handleReqKVUpdate(ctx, entry)
	}
}

// handleReqKVUpdate processes a single EXECUTION_STATES KV update for a req.> key.
// It checks if the entry contains a new completion (LastCompletionTaskID changed)
// and routes to the appropriate handler.
func (c *Component) handleReqKVUpdate(ctx context.Context, entry jetstream.KeyValueEntry) {
	var reqExec workflow.RequirementExecution
	if err := json.Unmarshal(entry.Value(), &reqExec); err != nil {
		return
	}

	// Only act on entries with a completion signal.
	if reqExec.LastCompletionTaskID == "" {
		return
	}

	// Find the active execution by entity ID.
	entityID := reqExec.EntityID
	if entityID == "" {
		return
	}

	execVal, ok := c.activeExecutions.Load(entityID)
	if !ok {
		return // not ours (different instance or already cleaned up)
	}
	exec := execVal.(*requirementExecution)

	exec.mu.Lock()
	defer exec.mu.Unlock()

	if exec.terminated {
		return
	}

	// Dedup: only process if this completion hasn't been handled yet.
	// Compare the completion task ID against the dispatched task IDs.
	completionTaskID := reqExec.LastCompletionTaskID
	if !c.isExpectedCompletion(exec, completionTaskID) {
		return
	}

	c.logger.Info("KV completion received",
		"slug", exec.Slug,
		"requirement_id", exec.RequirementID,
		"completion_task_id", completionTaskID,
		"workflow_step", reqExec.LastCompletionStep,
		"outcome", reqExec.LastCompletionOutcome,
	)

	// Build a synthetic LoopCompletedEvent for compatibility with existing handlers.
	event := &agentic.LoopCompletedEvent{
		TaskID:       completionTaskID,
		Outcome:      reqExec.LastCompletionOutcome,
		Result:       reqExec.LastCompletionResult,
		WorkflowStep: reqExec.LastCompletionStep,
	}

	switch {
	case completionTaskID == exec.DecomposerTaskID:
		c.handleDecomposerCompleteLocked(ctx, event, exec)
	case completionTaskID == exec.RedTeamTaskID:
		c.handleRequirementRedTeamCompleteLocked(ctx, event, exec)
	case completionTaskID == exec.ReviewerTaskID:
		c.handleRequirementReviewerCompleteLocked(ctx, event, exec)
	case completionTaskID == exec.CurrentNodeTaskID:
		c.handleNodeCompleteLocked(ctx, event, exec)
	}
}

// isExpectedCompletion returns true if the completion task ID matches one of
// the currently dispatched task IDs for this execution.
func (c *Component) isExpectedCompletion(exec *requirementExecution, taskID string) bool {
	return taskID == exec.DecomposerTaskID ||
		taskID == exec.CurrentNodeTaskID ||
		taskID == exec.ReviewerTaskID ||
		taskID == exec.RedTeamTaskID
}
