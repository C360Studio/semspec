package requirementexecutor

// exec_mutations.go — helpers for sending mutations to execution-manager.
// Requirement-executor is NOT a writer to EXECUTION_STATES. All persistent
// state changes go through execution-manager via request/reply mutations.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/natsclient"
)

// Mutation subjects — must match execution-manager/mutations.go constants.
const (
	mutReqPhase            = "execution.mutation.req.phase"
	mutReqNode             = "execution.mutation.req.node"
	mutReqReset            = "execution.mutation.req.reset"
	mutReqResetNodeResults = "execution.mutation.req.reset_node_results"
	mutTaskCreate          = "execution.mutation.task.create"
	// mutPlanDecisionAdd targets plan-manager (different processor, different
	// KV bucket). Used for emitting ExecutionExhausted decisions so the
	// human has a decision record to act on.
	mutPlanDecisionAdd = "plan.mutation.plan_decision.add"
)

// execMutationResponse mirrors ExecMutationResponse from execution-manager.
type execMutationResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Key     string `json:"key,omitempty"`
}

// sendReqPhase sends a phase transition mutation to execution-manager.
func (c *Component) sendReqPhase(ctx context.Context, key, stage string, fields map[string]any) error {
	req := map[string]any{
		"key":   key,
		"stage": stage,
	}
	for k, v := range fields {
		req[k] = v
	}
	_, err := c.sendMutation(ctx, mutReqPhase, req)
	return err
}

// sendReqReplaceNodeResults asks execution-manager to replace the NodeResults
// slice on the existing requirement execution KV entry. An empty slice wipes
// the field; a populated slice preserves clean cross-Story evidence while
// dropping the current Story's stale retry entries. nil natsClient short-circuits
// for unit tests.
func (c *Component) sendReqReplaceNodeResults(ctx context.Context, key string, results []workflow.NodeResult) error {
	if c.natsClient == nil {
		return nil
	}
	req := map[string]any{
		"key": key,
	}
	if len(results) > 0 {
		req["node_results"] = results
	}
	_, err := c.sendMutation(ctx, mutReqResetNodeResults, req)
	return err
}

// sendReqResetNodeResults wipes the NodeResults slice on the existing
// requirement execution KV entry. Routes through the nodeResultsSender seam so
// tests can pin the producer call sites (#82).
func (c *Component) sendReqResetNodeResults(ctx context.Context, key string) error {
	return c.nodeResultsSender(ctx, key, nil)
}

// sendReqReset asks execution-manager to DELETE a requirement execution KV
// entry. Used by the recovery dependent-subtree invalidation to un-complete a
// dependent so the orchestrator re-dispatches it and it re-derives its branch
// from the rebuilt prerequisite. nil natsClient short-circuits (unit tests).
func (c *Component) sendReqReset(ctx context.Context, key string) error {
	if c.natsClient == nil {
		return nil
	}
	_, err := c.sendMutation(ctx, mutReqReset, map[string]any{
		"key": key,
	})
	return err
}

// sendReqNode sends a DAG node update mutation to execution-manager.
func (c *Component) sendReqNode(ctx context.Context, key string, nodeIdx int, nodeTaskID string, result *workflow.NodeResult) error {
	req := map[string]any{
		"key":                  key,
		"current_node_idx":     nodeIdx,
		"current_node_task_id": nodeTaskID,
	}
	if result != nil {
		req["node_result"] = result
	}
	_, err := c.sendMutation(ctx, mutReqNode, req)
	return err
}

// sendTaskCreate sends a task execution creation mutation to execution-manager.
// This replaces the previous JetStream publish to workflow.trigger.task-execution-loop.
// Returns nil when natsClient is nil (unit test / no-NATS mode).
func (c *Component) sendTaskCreate(ctx context.Context, req map[string]any) error {
	if c.natsClient == nil {
		return nil
	}
	_, err := c.sendMutation(ctx, mutTaskCreate, req)
	return err
}

// sendPlanDecisionAdd emits a PlanDecision to plan-manager so the human
// gets a decision record for this requirement. Best-effort: logs and
// returns error without blocking the caller's state transition, since the
// requirement has already been marked failed by the time this fires.
func (c *Component) sendPlanDecisionAdd(ctx context.Context, slug string, decision workflow.PlanDecision) error {
	if c.natsClient == nil {
		return nil
	}
	req := map[string]any{
		"slug":     slug,
		"decision": decision,
	}
	_, err := c.sendMutation(ctx, mutPlanDecisionAdd, req)
	return err
}

// sendMutation sends a mutation request/reply to execution-manager and parses the response.
func (c *Component) sendMutation(ctx context.Context, subject string, payload any) (*execMutationResponse, error) {
	if c.natsClient == nil {
		return nil, fmt.Errorf("%s: nats client not available", subject)
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", subject, err)
	}

	respData, err := c.natsClient.RequestWithRetry(ctx, subject, data, 5*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		return nil, fmt.Errorf("%s request failed: %w", subject, err)
	}

	var resp execMutationResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal %s response: %w", subject, err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("%s rejected: %s", subject, resp.Error)
	}

	return &resp, nil
}
