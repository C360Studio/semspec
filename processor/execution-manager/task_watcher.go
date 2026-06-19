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

	// Claim: pending → developing (atomic compare-and-swap via mutation handler).
	if !c.claimTaskExecution(ctx, key, "pending", phaseDeveloping) {
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
			EntityID: entityID,
			Slug:     taskExec.Slug,
			TaskID:   taskExec.TaskID,
			// RequirementID was missing from this struct literal; the watcher's
			// claim-then-rebuild path dropped it, then syncToStore re-persisted
			// the in-memory exec without it, leaving every task in
			// EXECUTION_STATES KV with requirement_id="". Caught 2026-05-11
			// mock e2e reproduction of the gemini @hard take 7 finding: recovery
			// PlanDecision's affected_req_ids was empty because the wedged task's
			// requirement_id had been silently stripped here. Every downstream
			// path that keyed off exec.RequirementID (recovery dispatch capability
			// routing, parent-req termination check, requirement-executor scope
			// queries) was operating on empty.
			RequirementID:  taskExec.RequirementID,
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
			WorktreePath:   taskExec.WorktreePath,
			WorktreeBranch: taskExec.WorktreeBranch,
			ScenarioBranch: taskExec.ScenarioBranch,
			FileScope:      taskExec.FileScope,
			// Scenarios threading (2026-06-03): without this line, the
			// watcher rebuilds TaskExecution from KV without scenarios,
			// then syncToStore at initTaskExecution overwrites the KV
			// with the in-memory copy — silently stripping scenarios
			// from the persisted entity. Mirrors the 2026-05-11
			// RequirementID bug above: ANY new TaskExecution field
			// MUST be added here or syncToStore strips it on first
			// watcher pass. Production paid mavlink-hard 2026-06-03
			// reproduced this: TaskCreateRequest had scenarios=3,
			// pre-save marshal had scenarios=true, then 5ms later a
			// syncToStore save wrote scenarios_on_struct=0 because
			// this struct literal dropped the field on rebuild.
			Scenarios: taskExec.Scenarios,
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
	_ = c.tripleWriter.UpdateTriple(ctx, entityID, wf.Type, "task-execution")
	_ = c.tripleWriter.UpdateTriple(ctx, entityID, wf.Phase, phaseDeveloping)
	_ = c.tripleWriter.UpdateTriple(ctx, entityID, wf.Slug, exec.Slug)
	_ = c.tripleWriter.UpdateTriple(ctx, entityID, wf.TaskID, exec.TaskID)
	_ = c.tripleWriter.UpdateTriple(ctx, entityID, wf.Title, exec.Title)
	_ = c.tripleWriter.UpdateTriple(ctx, entityID, wf.ProjectID, exec.ProjectID)
	_ = c.tripleWriter.UpdateTriple(ctx, entityID, wf.TDDCycle, 0)
	_ = c.tripleWriter.UpdateTriple(ctx, entityID, wf.MaxTDDCycles, exec.MaxTDDCycles)
	_ = c.tripleWriter.UpdateTriple(ctx, entityID, wf.TraceID, exec.TraceID)
	_ = c.tripleWriter.UpdateTriple(ctx, entityID, wf.Model, exec.Model)
	_ = c.tripleWriter.UpdateTriple(ctx, entityID, wf.CurrentStage, phaseDeveloping)
	if exec.Prompt != "" {
		_ = c.tripleWriter.UpdateTriple(ctx, entityID, wf.Prompt, exec.Prompt)
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
// handler. fromStage pins the expected source stage so the claim is a
// compare-and-swap (#157); a concurrent or stale claim from a different stage is
// rejected. Returns true if the claim succeeded.
func (c *Component) claimTaskExecution(ctx context.Context, key, fromStage, toStage string) bool {
	data, err := json.Marshal(ExecClaimRequest{Key: key, Stage: toStage, ExpectedFromStage: fromStage})
	if err != nil {
		return false
	}
	resp := c.handleExecClaimMutation(ctx, data)
	return resp.Success
}
