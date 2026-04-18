package executionmanager

import (
	"context"
	"encoding/json"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
	"github.com/nats-io/nats.go/jetstream"
)

// watchLoopCompletions watches the AGENT_LOOPS KV bucket for agentic loop
// completions. When a loop reaches terminal state and its TaskID matches a
// task in the TDD pipeline, the completion is routed to the appropriate handler.
//
// This replaces the old agent.complete.> JetStream consumer. AGENT_LOOPS is
// written by semstreams' agentic-dispatch and provides durable, replayable
// state — no messages lost on startup races or restarts.
//
// Replay gate: the NATS KV watcher delivers all existing keys before sending a
// nil sentinel. During that initial replay we populate taskRouting (so live
// events can route correctly) but suppress pipeline stage progression — those
// executions may already be further along or may need resumption via
// resumeStuckExecutions once replay is complete.
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

	c.replayLoops = make(map[string]agentic.LoopEntity)

	replayDone := false

	for entry := range watcher.Updates() {
		if entry == nil {
			// Nil sentinel marks the end of the initial KV replay.
			replayDone = true
			c.logger.Info("AGENT_LOOPS replay complete for execution-manager")
			c.resumeStuckExecutions(ctx)
			continue
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}
		if !replayDone {
			c.handleLoopEntityUpdateReplay(ctx, entry)
		} else {
			c.handleLoopEntityUpdate(ctx, entry)
		}
	}
}

// handleLoopEntityUpdateReplay processes an AGENT_LOOPS KV entry received
// during the initial watcher replay (before the nil sentinel). It populates
// taskRouting so that live events arriving after replay can route correctly,
// but deliberately skips stage-handler dispatch. Pipeline advancement for
// completions that arrived during downtime is deferred to resumeStuckExecutions.
//
// reconcileFromGraph populates activeExecs but not taskRouting. This handler
// bridges that gap: it scans activeExecs for any execution whose current-stage
// task ID matches loop.TaskID and records the entityID→taskID mapping.
func (c *Component) handleLoopEntityUpdateReplay(_ context.Context, entry jetstream.KeyValueEntry) {
	var loop agentic.LoopEntity
	if err := json.Unmarshal(entry.Value(), &loop); err != nil {
		return
	}

	if !loop.State.IsTerminal() {
		return
	}

	if loop.WorkflowSlug != WorkflowSlugTaskExecution {
		return
	}

	if loop.TaskID == "" {
		return
	}

	// If taskRouting already has an entry for this task ID (set by a prior
	// reconcile path), there is nothing more to do.
	if _, ok := c.taskRouting.Get(loop.TaskID); ok {
		return
	}

	// Walk activeExecs to find the execution that owns this task ID. This is an
	// O(n) scan, but n is the number of in-flight executions (typically small)
	// and this path runs only once during startup replay.
	for _, entityID := range c.activeExecs.Keys() {
		exec, ok := c.activeExecs.Get(entityID)
		if !ok {
			continue
		}

		exec.mu.Lock()
		taskID := c.currentStageTaskID(exec)
		exec.mu.Unlock()

		if taskID == loop.TaskID {
			c.taskRouting.Set(loop.TaskID, entityID) //nolint:errcheck // best-effort
			c.replayLoops[loop.TaskID] = loop
			return
		}
	}
}

// resumeStuckExecutions is called once after the AGENT_LOOPS replay sentinel.
// It checks every active execution to see whether its current-stage task
// completed during downtime (terminal in AGENT_LOOPS but processed replay-only).
// If yes, it calls the appropriate stage handler to advance the pipeline.
// If no matching loop completion exists, the execution is left to the timeout
// mechanism — we log a warning rather than aggressively re-dispatching.
func (c *Component) resumeStuckExecutions(ctx context.Context) {
	for _, entityID := range c.activeExecs.Keys() {
		exec, ok := c.activeExecs.Get(entityID)
		if !ok {
			continue
		}

		exec.mu.Lock()

		if exec.terminated {
			exec.mu.Unlock()
			continue
		}

		// Determine which task ID is expected for the current pipeline stage.
		currentTaskID := c.currentStageTaskID(exec)
		if currentTaskID == "" {
			// Stage has no outstanding task ID — nothing to resume.
			exec.mu.Unlock()
			continue
		}

		// Check whether this task ID completed during downtime. taskRouting maps
		// agent task IDs to execution entity IDs; it is only populated during
		// replay when handleLoopEntityUpdateReplay finds a terminal loop entry.
		mappedEntityID, routed := c.taskRouting.Get(currentTaskID)
		if !routed || mappedEntityID == "" {
			c.logger.Warn("Execution appears stuck after replay — awaiting timeout escalation",
				"entity_id", entityID,
				"slug", exec.Slug,
				"stage", exec.Stage,
				"current_task_id", currentTaskID,
			)
			exec.mu.Unlock()
			continue
		}

		// We have a terminal loop completion for the current stage that was
		// withheld during replay. Rebuild the minimal event and dispatch.
		c.logger.Info("Resuming stuck execution after replay",
			"entity_id", entityID,
			"slug", exec.Slug,
			"stage", exec.Stage,
			"task_id", currentTaskID,
		)

		event := &agentic.LoopCompletedEvent{
			TaskID:       currentTaskID,
			CompletedAt:  time.Now(),
			WorkflowSlug: WorkflowSlugTaskExecution,
			WorkflowStep: exec.Stage,
		}

		// Populate outcome/result from the cached replay data so the handlers
		// can detect failures (e.g. max_iterations reached).
		if rl, ok := c.replayLoops[currentTaskID]; ok {
			event.Outcome = rl.Outcome
			event.Result = rl.Result
			event.LoopID = rl.ID
		}

		switch exec.Stage {
		case phaseDeveloping,
			"testing",  // backward-compat: old executions with phaseTesting
			"building": // backward-compat: old executions with phaseBuilding
			event.WorkflowStep = stageDevelop
			c.handleDeveloperCompleteLocked(ctx, event, exec)
		case phaseReviewing:
			event.WorkflowStep = stageReview
			c.handleReviewerCompleteLocked(ctx, event, exec)
		default:
			// phaseValidating is handled by the structural-validator (not AGENT_LOOPS).
			// Any other phase is unknown — log and skip.
			c.logger.Warn("Unrecognised stage during stuck-execution resume",
				"entity_id", entityID, "stage", exec.Stage)
			exec.mu.Unlock()
			continue
		}

		// The *Locked handlers do not release exec.mu — the caller owns the lock.
		exec.mu.Unlock()
	}

	// Free replay data — no longer needed after resume.
	c.replayLoops = nil
}

// currentStageTaskID returns the agentic task ID that is expected to complete
// for exec's current pipeline stage. Returns "" when the stage has no outstanding
// agent task (e.g. phaseValidating, which is driven by structural-validator).
//
// The "testing" and "building" cases handle executions loaded from KV that were
// persisted by an older code version before tester/builder were merged into developer.
// Caller must hold exec.mu.
func (c *Component) currentStageTaskID(exec *taskExecution) string {
	switch exec.Stage {
	case phaseDeveloping,
		"testing",  // backward-compat: old executions with phaseTesting
		"building": // backward-compat: old executions with phaseBuilding
		return exec.DeveloperTaskID
	case phaseReviewing:
		return exec.ReviewerTaskID
	default:
		return ""
	}
}

// handleLoopEntityUpdate processes a single AGENT_LOOPS KV update.
// Routes terminal TDD pipeline loops to the appropriate handler.
func (c *Component) handleLoopEntityUpdate(ctx context.Context, entry jetstream.KeyValueEntry) {
	var loop agentic.LoopEntity
	if err := json.Unmarshal(entry.Value(), &loop); err != nil {
		return
	}

	if !loop.State.IsTerminal() {
		return
	}

	// Only handle TDD pipeline events.
	if loop.WorkflowSlug != WorkflowSlugTaskExecution {
		return
	}

	if loop.TaskID == "" {
		return
	}

	c.updateLastActivity()

	entityID, ok := c.taskRouting.Get(loop.TaskID)
	if !ok {
		return // not ours
	}

	exec, ok := c.activeExecs.Get(entityID)
	if !ok {
		return
	}

	exec.mu.Lock()
	defer exec.mu.Unlock()

	if exec.terminated {
		return
	}

	c.logger.Info("Loop completion received via KV",
		"slug", exec.Slug,
		"task_id", exec.TaskID,
		"loop_task_id", loop.TaskID,
		"workflow_step", loop.WorkflowStep,
		"outcome", loop.Outcome,
		"tdd_cycle", exec.TDDCycle,
	)

	// Build LoopCompletedEvent for compatibility with existing handlers.
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

	switch event.WorkflowStep {
	case stageDevelop,
		"test",  // backward-compat: old loop entries with stageTest
		"build": // backward-compat: old loop entries with stageBuild
		c.handleDeveloperCompleteLocked(ctx, event, exec)
	case stageReview:
		c.handleReviewerCompleteLocked(ctx, event, exec)
	}
}
