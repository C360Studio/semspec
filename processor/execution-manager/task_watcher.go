package executionmanager

// task_watcher.go — KV self-trigger for task execution.
//
// Watches EXECUTION_STATES for task.> entries with stage="pending" and claims
// them to "developing". This replaces the JetStream stream consumer that
// previously received TriggerPayload triggers from requirement-executor.
//
// The pattern matches the planning components (planner, requirement-generator,
// etc.) which all self-trigger from KV state changes.

import (
	"context"
	"encoding/json"

	wf "github.com/c360studio/semspec/vocabulary/workflow"
	"github.com/c360studio/semspec/workflow"
	"github.com/nats-io/nats.go/jetstream"
)

// watchTaskPending watches EXECUTION_STATES for task.> entries with stage=pending
// and claims them for development. Runs until ctx is cancelled.
func (c *Component) watchTaskPending(ctx context.Context) {
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Warn("Cannot watch EXECUTION_STATES for pending tasks: no JetStream", "error", err)
		return
	}

	bucket, err := workflow.WaitForKVBucket(ctx, js, "EXECUTION_STATES")
	if err != nil {
		c.logger.Warn("EXECUTION_STATES bucket not available — task pending watcher disabled", "error", err)
		return
	}

	watcher, err := bucket.Watch(ctx, "task.>")
	if err != nil {
		c.logger.Warn("Failed to watch EXECUTION_STATES task.> — task pending watcher disabled", "error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Task pending watcher started (watching EXECUTION_STATES task.>)")

	for entry := range watcher.Updates() {
		if entry == nil {
			c.logger.Info("EXECUTION_STATES task.> replay complete for execution-manager pending watcher")
			continue
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}
		c.handleTaskPending(ctx, entry)
	}
}

// handleTaskPending processes a single EXECUTION_STATES task.> KV update.
// Claims pending entries and dispatches the TDD pipeline.
func (c *Component) handleTaskPending(ctx context.Context, entry jetstream.KeyValueEntry) {
	var taskExec workflow.TaskExecution
	if err := json.Unmarshal(entry.Value(), &taskExec); err != nil {
		return
	}

	if taskExec.Stage != "pending" {
		return
	}

	key := entry.Key()

	// Claim: pending → developing (atomic via mutation handler).
	if !c.claimTaskExecution(ctx, key, phaseDeveloping) {
		return
	}

	c.logger.Info("Claimed pending task execution from KV",
		"key", key,
		"slug", taskExec.Slug,
		"task_id", taskExec.TaskID,
	)

	// Build in-memory execution from KV entry.
	entityID := workflow.TaskExecutionEntityID(taskExec.Slug, taskExec.TaskID)

	exec := &taskExecution{
		key: key,
		TaskExecution: &workflow.TaskExecution{
			EntityID:       entityID,
			Slug:           taskExec.Slug,
			TaskID:         taskExec.TaskID,
			Stage:          phaseDeveloping,
			TDDCycle:       0,
			MaxTDDCycles:   taskExec.MaxTDDCycles,
			Title:          taskExec.Title,
			Description:    taskExec.Description,
			ProjectID:      taskExec.ProjectID,
			Prompt:         taskExec.Prompt,
			Model:          taskExec.Model,
			TraceID:        taskExec.TraceID,
			LoopID:         taskExec.LoopID,
			RequestID:      taskExec.RequestID,
			TaskType:       taskExec.TaskType,
			AgentID:        taskExec.AgentID,
			BlueTeamID:     taskExec.BlueTeamID,
			RedTeamID:      taskExec.RedTeamID,
			WorktreePath:   taskExec.WorktreePath,
			WorktreeBranch: taskExec.WorktreeBranch,
			ScenarioBranch: taskExec.ScenarioBranch,
			FileScope:      taskExec.FileScope,
		},
	}

	if exec.MaxTDDCycles == 0 {
		exec.MaxTDDCycles = c.config.MaxTDDCycles
	}

	c.activeExecsMu.Lock()
	if _, exists := c.activeExecs.Get(entityID); exists {
		c.activeExecsMu.Unlock()
		c.logger.Debug("Duplicate pending task for active execution, skipping", "entity_id", entityID)
		return
	}
	c.activeExecs.Set(entityID, exec) //nolint:errcheck // cache set is best-effort
	c.activeExecsMu.Unlock()

	// Dispatch initialization in a goroutine so the watcher loop is never blocked.
	go c.initTaskExecution(ctx, exec)
}

// initTaskExecution performs the post-claim initialization sequence for a
// task execution: store sync, triples, worktree, entity publish, timeout,
// and first stage dispatch. Mirrors the logic from handleTrigger.
func (c *Component) initTaskExecution(ctx context.Context, exec *taskExecution) {
	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	// Sync to store (stage is now developing after claim).
	c.syncToStore(ctx, exec)

	// Write initial triples from exec fields (no trigger needed).
	entityID := exec.EntityID
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.Type, "task-execution")
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.Phase, phaseDeveloping)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.Slug, exec.Slug)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.TaskID, exec.TaskID)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.Title, exec.Title)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.ProjectID, exec.ProjectID)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.TDDCycle, 0)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.MaxTDDCycles, exec.MaxTDDCycles)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.TraceID, exec.TraceID)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.Model, exec.Model)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.CurrentStage, phaseDeveloping)
	if exec.Prompt != "" {
		_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.Prompt, exec.Prompt)
	}

	if err := c.createWorktree(ctx, exec); err != nil {
		c.logger.Error("Worktree creation failed — execution cannot proceed without sandbox isolation",
			"slug", exec.Slug,
			"task_id", exec.TaskID,
			"error", err,
		)
		exec.mu.Lock()
		defer exec.mu.Unlock()
		c.markErrorLocked(ctx, exec, "worktree_creation_failed: "+err.Error())
		return
	}

	// Select pipeline based on task type.
	initialPhase := c.initialPhaseForType(exec.TaskType)

	// Publish initial entity snapshot for graph observability.
	c.publishEntity(ctx, NewTaskExecutionEntity(exec).WithPhase(initialPhase))

	exec.mu.Lock()
	defer exec.mu.Unlock()
	c.startExecutionTimeout(exec)
	c.dispatchFirstStage(ctx, exec)
}

// claimTaskExecution sends a claim mutation to this component's own mutation
// handler. Returns true if the claim succeeded.
func (c *Component) claimTaskExecution(ctx context.Context, key, stage string) bool {
	data, err := json.Marshal(ExecClaimRequest{Key: key, Stage: stage})
	if err != nil {
		return false
	}
	resp := c.handleExecClaimMutation(ctx, data)
	return resp.Success
}
