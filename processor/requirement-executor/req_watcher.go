package requirementexecutor

// req_watcher.go — KV self-trigger for requirement execution.
//
// Watches EXECUTION_STATES for req.> entries with stage="pending" and claims
// them to "decomposing". This replaces the JetStream stream consumer that
// previously received RequirementExecutionRequest triggers from
// scenario-orchestrator.
//
// The pattern matches the planning components (planner, requirement-generator,
// etc.) which all self-trigger from KV state changes.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// mutExecClaim is the subject for claiming an execution entry (atomic
	// transition). Must match execution-manager/mutations.go execMutationClaim.
	mutExecClaim = "execution.mutation.claim"
)

// watchReqPending watches EXECUTION_STATES for req.> entries with stage=pending
// and claims them for decomposition. Runs until ctx is cancelled.
func (c *Component) watchReqPending(ctx context.Context) {
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Warn("Cannot watch EXECUTION_STATES for pending reqs: no JetStream", "error", err)
		c.replayGate.Done()
		return
	}

	bucket, err := workflow.WaitForKVBucket(ctx, js, "EXECUTION_STATES")
	if err != nil {
		c.logger.Warn("EXECUTION_STATES bucket not available — req pending watcher disabled", "error", err)
		c.replayGate.Done()
		return
	}

	watcher, err := bucket.Watch(ctx, "req.>")
	if err != nil {
		c.logger.Warn("Failed to watch EXECUTION_STATES req.> — req pending watcher disabled", "error", err)
		c.replayGate.Done()
		return
	}
	defer watcher.Stop()

	c.logger.Info("Req pending watcher started (watching EXECUTION_STATES req.>)")

	for entry := range watcher.Updates() {
		if entry == nil {
			c.logger.Info("EXECUTION_STATES req.> replay complete for requirement-executor pending watcher")
			c.replayGate.Done()
			continue
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}
		c.handleReqPending(ctx, entry)
	}
}

// handleReqPending processes a single EXECUTION_STATES req.> KV update.
// Claims pending entries and dispatches the decomposer.
func (c *Component) handleReqPending(ctx context.Context, entry jetstream.KeyValueEntry) {
	var reqExec workflow.RequirementExecution
	if err := json.Unmarshal(entry.Value(), &reqExec); err != nil {
		return
	}

	if reqExec.Stage != "pending" {
		return
	}

	key := entry.Key()

	// Claim: pending → decomposing (atomic via execution-manager mutation).
	if !c.claimExecution(ctx, key, phaseDecomposing) {
		return
	}

	c.logger.Info("Claimed pending requirement execution from KV",
		"key", key,
		"slug", reqExec.Slug,
		"requirement_id", reqExec.RequirementID,
	)

	// Build in-memory execution from KV entry.
	instance := strings.ReplaceAll(reqExec.Slug+"-"+reqExec.RequirementID, ".", "-")
	entityID := fmt.Sprintf("%s.exec.req.run.%s", workflow.EntityPrefix(), instance)

	model := reqExec.Model
	if model == "" {
		model = c.config.Model
	}

	exec := &requirementExecution{
		EntityID:       entityID,
		Slug:           reqExec.Slug,
		RequirementID:  reqExec.RequirementID,
		Title:          reqExec.Title,
		Description:    reqExec.Description,
		Scenarios:      reqExec.Scenarios,
		DependsOn:      reqExec.DependsOn,
		Prompt:         reqExec.Prompt,
		Role:           reqExec.Role,
		Model:          model,
		ProjectID:      reqExec.ProjectID,
		TraceID:        reqExec.TraceID,
		LoopID:         reqExec.LoopID,
		RequestID:      reqExec.RequestID,
		CurrentNodeIdx: -1,
		VisitedNodes:   make(map[string]bool),
		MaxRetries:     c.config.MaxRequirementRetries,
		storeKey:       key,
	}

	c.activeExecsMu.Lock()
	if _, exists := c.activeExecs.Get(entityID); exists {
		c.activeExecsMu.Unlock()
		c.logger.Debug("Duplicate pending req for active execution, skipping", "entity_id", entityID)
		return
	}
	c.activeExecs.Set(entityID, exec) //nolint:errcheck // cache set is best-effort
	c.activeExecsMu.Unlock()

	// Dispatch initialization in a goroutine so the watcher loop is never blocked.
	go c.initReqExecution(ctx, exec, reqExec.PlanBranch)
}

// initReqExecution performs the post-claim initialization sequence for a
// requirement execution: branch creation, scope loading, entity publish,
// timeout, and decomposer dispatch.
func (c *Component) initReqExecution(ctx context.Context, exec *requirementExecution, planBranch string) {
	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	// Create per-requirement branch for worktree isolation.
	if c.sandbox != nil {
		branchName := "semspec/requirement-" + exec.RequirementID
		baseBranch := "HEAD"
		if planBranch != "" {
			baseBranch = planBranch
		}
		if err := c.sandbox.CreateBranch(ctx, branchName, baseBranch); err != nil {
			c.logger.Warn("Failed to create requirement branch; worktrees will branch from HEAD",
				"branch", branchName, "base", baseBranch, "error", err)
		} else {
			exec.RequirementBranch = branchName
			c.logger.Info("Requirement branch created", "branch", branchName, "base", baseBranch)
		}
	}

	// Load plan scope from PLAN_STATES.
	if scope := c.loadPlanScope(ctx, exec.Slug); scope != nil {
		exec.Scope = scope
	}

	// Publish initial entity snapshot for graph observability.
	c.publishEntity(ctx, NewRequirementExecutionEntity(exec).WithPhase(phaseDecomposing))

	exec.mu.Lock()
	defer exec.mu.Unlock()

	c.startExecutionTimeoutLocked(exec)
	c.dispatchDecomposerLocked(ctx, exec)
}

// claimExecution sends a claim mutation to execution-manager. Returns true if
// the claim succeeded (this instance owns the entry).
func (c *Component) claimExecution(ctx context.Context, key, stage string) bool {
	resp, err := c.sendMutation(ctx, mutExecClaim, map[string]any{
		"key":   key,
		"stage": stage,
	})
	if err != nil {
		c.logger.Debug("Claim execution failed", "key", key, "stage", stage, "error", err)
		return false
	}
	return resp.Success
}
