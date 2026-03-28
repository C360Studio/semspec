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
		"iteration", exec.Iteration,
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
	case stageTest:
		c.handleTesterCompleteLocked(ctx, event, exec)
	case stageBuild:
		c.handleBuilderCompleteLocked(ctx, event, exec)
	case stageRedTeam:
		c.handleRedTeamCompleteLocked(ctx, event, exec)
	case stageReview:
		c.handleReviewerCompleteLocked(ctx, event, exec)
	case stageDevelop:
		c.handleDeveloperCompleteLocked(ctx, event, exec)
	}
}
