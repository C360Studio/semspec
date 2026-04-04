package executionmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// Mutation subjects — execution-manager is the single writer to EXECUTION_STATES.
// requirement-executor and other components send mutations via request/reply.
const (
	execMutationTaskCreate   = "execution.mutation.task.create"
	execMutationTaskPhase    = "execution.mutation.task.phase"
	execMutationTaskComplete = "execution.mutation.task.complete"
	execMutationReqCreate    = "execution.mutation.req.create"
	execMutationReqPhase     = "execution.mutation.req.phase"
	execMutationReqNode      = "execution.mutation.req.node"
	execMutationReqReset     = "execution.mutation.req.reset"
	execMutationClaim        = "execution.mutation.claim"
)

// ---------------------------------------------------------------------------
// Request types
// ---------------------------------------------------------------------------

// TaskCreateRequest creates a new task execution entry.
type TaskCreateRequest struct {
	Slug           string            `json:"slug"`
	TaskID         string            `json:"task_id"`
	Title          string            `json:"title"`
	Description    string            `json:"description,omitempty"`
	ProjectID      string            `json:"project_id,omitempty"`
	Prompt         string            `json:"prompt,omitempty"`
	Model          string            `json:"model,omitempty"`
	TraceID        string            `json:"trace_id,omitempty"`
	LoopID         string            `json:"loop_id,omitempty"`
	RequestID      string            `json:"request_id,omitempty"`
	TaskType       workflow.TaskType `json:"task_type,omitempty"`
	MaxTDDCycles   int               `json:"max_tdd_cycles,omitempty"`
	AgentID        string            `json:"agent_id,omitempty"`
	BlueTeamID     string            `json:"blue_team_id,omitempty"`
	RedTeamID      string            `json:"red_team_id,omitempty"`
	WorktreePath   string            `json:"worktree_path,omitempty"`
	WorktreeBranch string            `json:"worktree_branch,omitempty"`
	ScenarioBranch string            `json:"scenario_branch,omitempty"`
}

// TaskPhaseRequest transitions a task execution to a new phase.
type TaskPhaseRequest struct {
	Key   string `json:"key"`   // KV key: task.<slug>.<taskID>
	Stage string `json:"stage"` // target phase
	// Optional fields updated alongside the phase transition:
	TDDCycle         *int     `json:"tdd_cycle,omitempty"`
	Verdict          string   `json:"verdict,omitempty"`
	RejectionType    string   `json:"rejection_type,omitempty"`
	Feedback         string   `json:"feedback,omitempty"`
	FilesModified    []string `json:"files_modified,omitempty"`
	TestsPassed      *bool    `json:"tests_passed,omitempty"`
	ValidationPassed *bool    `json:"validation_passed,omitempty"`
	ErrorReason      string   `json:"error_reason,omitempty"`
	EscalationReason string   `json:"escalation_reason,omitempty"`
	// Routing task IDs (set when dispatching to agentic loop)
	DeveloperTaskID string `json:"developer_task_id,omitempty"`
	ValidatorTaskID string `json:"validator_task_id,omitempty"`
	ReviewerTaskID  string `json:"reviewer_task_id,omitempty"`
	RedTeamTaskID   string `json:"red_team_task_id,omitempty"`
}

// TaskCompleteRequest marks a task execution as terminally complete.
type TaskCompleteRequest struct {
	Key              string `json:"key"`
	Stage            string `json:"stage"` // approved, escalated, error
	Verdict          string `json:"verdict,omitempty"`
	Feedback         string `json:"feedback,omitempty"`
	ErrorReason      string `json:"error_reason,omitempty"`
	EscalationReason string `json:"escalation_reason,omitempty"`
}

// ReqCreateRequest creates a new requirement execution entry.
type ReqCreateRequest struct {
	Slug          string              `json:"slug"`
	RequirementID string              `json:"requirement_id"`
	Title         string              `json:"title"`
	Description   string              `json:"description,omitempty"`
	ProjectID     string              `json:"project_id,omitempty"`
	TraceID       string              `json:"trace_id,omitempty"`
	LoopID        string              `json:"loop_id,omitempty"`
	RequestID     string              `json:"request_id,omitempty"`
	Model         string              `json:"model,omitempty"`
	Scenarios     []workflow.Scenario `json:"scenarios,omitempty"`
	BlueTeamID    string              `json:"blue_team_id,omitempty"`
	RedTeamID     string              `json:"red_team_id,omitempty"`
}

// ReqPhaseRequest transitions a requirement execution to a new phase.
type ReqPhaseRequest struct {
	Key            string `json:"key"` // KV key: req.<slug>.<reqID>
	Stage          string `json:"stage"`
	NodeCount      *int   `json:"node_count,omitempty"`
	CurrentNodeIdx *int   `json:"current_node_idx,omitempty"`
	ReviewVerdict  string `json:"review_verdict,omitempty"`
	ReviewFeedback string `json:"review_feedback,omitempty"`
	ErrorReason    string `json:"error_reason,omitempty"`
	// Routing
	DecomposerTaskID  string `json:"decomposer_task_id,omitempty"`
	CurrentNodeTaskID string `json:"current_node_task_id,omitempty"`
	ReviewerTaskID    string `json:"reviewer_task_id,omitempty"`
	RedTeamTaskID     string `json:"red_team_task_id,omitempty"`
	// Branch
	RequirementBranch string `json:"requirement_branch,omitempty"`
	// DAG state — persisted after decomposition for crash recovery
	DAGRaw        json.RawMessage `json:"dag,omitempty"`
	SortedNodeIDs []string        `json:"sorted_node_ids,omitempty"`
}

// ReqResetRequest deletes a requirement execution entry from EXECUTION_STATES.
// Called by plan-manager's retry handler to clear failed/error entries before re-dispatch.
type ReqResetRequest struct {
	Key string `json:"key"` // KV key: req.<slug>.<reqID>
}

// ReqNodeRequest updates DAG node state within a requirement execution.
type ReqNodeRequest struct {
	Key            string               `json:"key"`
	CurrentNodeIdx *int                 `json:"current_node_idx,omitempty"`
	NodeResult     *workflow.NodeResult `json:"node_result,omitempty"`
	// Routing for current node
	CurrentNodeTaskID string `json:"current_node_task_id,omitempty"`
}

// ExecClaimRequest claims an execution for processing (intermediate status).
type ExecClaimRequest struct {
	Key   string `json:"key"`   // KV key
	Stage string `json:"stage"` // target in-progress stage
}

// ExecMutationResponse is the reply to all execution mutation requests.
type ExecMutationResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Key     string `json:"key,omitempty"` // returned on create
}

// ---------------------------------------------------------------------------
// Handler registration
// ---------------------------------------------------------------------------

// startExecMutationHandler subscribes to execution.mutation.* subjects.
// Called from Start().
func (c *Component) startExecMutationHandler(ctx context.Context) error {
	if c.natsClient == nil {
		return nil
	}

	subjects := []struct {
		subject string
		handler func(context.Context, []byte) ExecMutationResponse
	}{
		{execMutationTaskCreate, c.handleTaskCreateMutation},
		{execMutationTaskPhase, c.handleTaskPhaseMutation},
		{execMutationTaskComplete, c.handleTaskCompleteMutation},
		{execMutationReqCreate, c.handleReqCreateMutation},
		{execMutationReqPhase, c.handleReqPhaseMutation},
		{execMutationReqNode, c.handleReqNodeMutation},
		{execMutationReqReset, c.handleReqResetMutation},
		{execMutationClaim, c.handleExecClaimMutation},
	}

	for _, s := range subjects {
		h := s.handler
		if _, err := c.natsClient.SubscribeForRequests(ctx, s.subject, func(reqCtx context.Context, data []byte) ([]byte, error) {
			resp := h(reqCtx, data)
			return json.Marshal(resp)
		}); err != nil {
			return fmt.Errorf("subscribe to %s: %w", s.subject, err)
		}
	}

	c.logger.Info("Execution mutation handlers started", "count", len(subjects))
	return nil
}

// ---------------------------------------------------------------------------
// Task mutation handlers
// ---------------------------------------------------------------------------

func (c *Component) handleTaskCreateMutation(ctx context.Context, data []byte) ExecMutationResponse {
	var req TaskCreateRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return ExecMutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" || req.TaskID == "" {
		return ExecMutationResponse{Success: false, Error: "slug and task_id required"}
	}

	key := workflow.TaskExecutionKey(req.Slug, req.TaskID)

	// Check for duplicate
	if _, ok := c.store.getTask(key); ok {
		return ExecMutationResponse{Success: false, Error: fmt.Sprintf("task execution already exists: %s", key)}
	}

	maxCycles := req.MaxTDDCycles
	if maxCycles == 0 {
		maxCycles = c.config.MaxTDDCycles
	}

	now := time.Now()
	exec := &workflow.TaskExecution{
		EntityID:       workflow.TaskExecutionEntityID(req.Slug, req.TaskID),
		Slug:           req.Slug,
		TaskID:         req.TaskID,
		Stage:          phaseDeveloping, // initial phase
		TDDCycle:       0,
		MaxTDDCycles:   maxCycles,
		Title:          req.Title,
		Description:    req.Description,
		ProjectID:      req.ProjectID,
		Prompt:         req.Prompt,
		Model:          req.Model,
		TraceID:        req.TraceID,
		LoopID:         req.LoopID,
		RequestID:      req.RequestID,
		TaskType:       req.TaskType,
		AgentID:        req.AgentID,
		BlueTeamID:     req.BlueTeamID,
		RedTeamID:      req.RedTeamID,
		WorktreePath:   req.WorktreePath,
		WorktreeBranch: req.WorktreeBranch,
		ScenarioBranch: req.ScenarioBranch,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := c.store.saveTask(ctx, key, exec); err != nil {
		return ExecMutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Info("Task execution created via mutation", "key", key, "slug", req.Slug, "task_id", req.TaskID)
	return ExecMutationResponse{Success: true, Key: key}
}

func (c *Component) handleTaskPhaseMutation(ctx context.Context, data []byte) ExecMutationResponse {
	var req TaskPhaseRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return ExecMutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Key == "" || req.Stage == "" {
		return ExecMutationResponse{Success: false, Error: "key and phase required"}
	}

	exec, ok := c.store.getTask(req.Key)
	if !ok {
		return ExecMutationResponse{Success: false, Error: fmt.Sprintf("task not found: %s", req.Key)}
	}

	exec.Stage = req.Stage
	if req.TDDCycle != nil {
		exec.TDDCycle = *req.TDDCycle
	}
	if req.Verdict != "" {
		exec.Verdict = req.Verdict
	}
	if req.RejectionType != "" {
		exec.RejectionType = req.RejectionType
	}
	if req.Feedback != "" {
		exec.Feedback = req.Feedback
	}
	if len(req.FilesModified) > 0 {
		exec.FilesModified = req.FilesModified
	}
	if req.TestsPassed != nil {
		exec.TestsPassed = *req.TestsPassed
	}
	if req.ValidationPassed != nil {
		exec.ValidationPassed = *req.ValidationPassed
	}
	if req.ErrorReason != "" {
		exec.ErrorReason = req.ErrorReason
	}
	if req.EscalationReason != "" {
		exec.EscalationReason = req.EscalationReason
	}
	// Routing task IDs
	if req.DeveloperTaskID != "" {
		exec.DeveloperTaskID = req.DeveloperTaskID
	}
	if req.ValidatorTaskID != "" {
		exec.ValidatorTaskID = req.ValidatorTaskID
	}
	if req.ReviewerTaskID != "" {
		exec.ReviewerTaskID = req.ReviewerTaskID
	}
	if req.RedTeamTaskID != "" {
		exec.RedTeamTaskID = req.RedTeamTaskID
	}

	if err := c.store.saveTask(ctx, req.Key, exec); err != nil {
		return ExecMutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Debug("Task phase updated via mutation", "key", req.Key, "phase", req.Stage)
	return ExecMutationResponse{Success: true}
}

func (c *Component) handleTaskCompleteMutation(ctx context.Context, data []byte) ExecMutationResponse {
	var req TaskCompleteRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return ExecMutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Key == "" || req.Stage == "" {
		return ExecMutationResponse{Success: false, Error: "key and phase required"}
	}
	if !workflow.IsTerminalTaskStage(req.Stage) {
		return ExecMutationResponse{Success: false, Error: fmt.Sprintf("not a terminal stage: %s", req.Stage)}
	}

	exec, ok := c.store.getTask(req.Key)
	if !ok {
		return ExecMutationResponse{Success: false, Error: fmt.Sprintf("task not found: %s", req.Key)}
	}

	exec.Stage = req.Stage
	if req.Verdict != "" {
		exec.Verdict = req.Verdict
	}
	if req.Feedback != "" {
		exec.Feedback = req.Feedback
	}
	if req.ErrorReason != "" {
		exec.ErrorReason = req.ErrorReason
	}
	if req.EscalationReason != "" {
		exec.EscalationReason = req.EscalationReason
	}

	if err := c.store.saveTask(ctx, req.Key, exec); err != nil {
		return ExecMutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Info("Task execution completed via mutation",
		"key", req.Key, "phase", req.Stage, "verdict", req.Verdict)
	return ExecMutationResponse{Success: true}
}

// ---------------------------------------------------------------------------
// Requirement mutation handlers
// ---------------------------------------------------------------------------

func (c *Component) handleReqCreateMutation(ctx context.Context, data []byte) ExecMutationResponse {
	var req ReqCreateRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return ExecMutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" || req.RequirementID == "" {
		return ExecMutationResponse{Success: false, Error: "slug and requirement_id required"}
	}

	key := workflow.RequirementExecutionKey(req.Slug, req.RequirementID)

	if _, ok := c.store.getReq(key); ok {
		return ExecMutationResponse{Success: false, Error: fmt.Sprintf("req execution already exists: %s", key)}
	}

	now := time.Now()
	exec := &workflow.RequirementExecution{
		EntityID:       workflow.RequirementExecutionEntityID(req.Slug, req.RequirementID),
		Slug:           req.Slug,
		RequirementID:  req.RequirementID,
		Stage:          "decomposing", // initial phase
		Title:          req.Title,
		Description:    req.Description,
		ProjectID:      req.ProjectID,
		TraceID:        req.TraceID,
		LoopID:         req.LoopID,
		RequestID:      req.RequestID,
		Model:          req.Model,
		Scenarios:      req.Scenarios,
		BlueTeamID:     req.BlueTeamID,
		RedTeamID:      req.RedTeamID,
		CurrentNodeIdx: -1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := c.store.saveReq(ctx, key, exec); err != nil {
		return ExecMutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Info("Requirement execution created via mutation",
		"key", key, "slug", req.Slug, "requirement_id", req.RequirementID)
	return ExecMutationResponse{Success: true, Key: key}
}

func (c *Component) handleReqPhaseMutation(ctx context.Context, data []byte) ExecMutationResponse {
	var req ReqPhaseRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return ExecMutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Key == "" || req.Stage == "" {
		return ExecMutationResponse{Success: false, Error: "key and phase required"}
	}

	exec, ok := c.store.getReq(req.Key)
	if !ok {
		return ExecMutationResponse{Success: false, Error: fmt.Sprintf("req not found: %s", req.Key)}
	}

	exec.Stage = req.Stage
	if req.NodeCount != nil {
		exec.NodeCount = *req.NodeCount
	}
	if req.CurrentNodeIdx != nil {
		exec.CurrentNodeIdx = *req.CurrentNodeIdx
	}
	if req.ReviewVerdict != "" {
		exec.ReviewVerdict = req.ReviewVerdict
	}
	if req.ReviewFeedback != "" {
		exec.ReviewFeedback = req.ReviewFeedback
	}
	if req.ErrorReason != "" {
		exec.ErrorReason = req.ErrorReason
	}
	if req.DecomposerTaskID != "" {
		exec.DecomposerTaskID = req.DecomposerTaskID
	}
	if req.CurrentNodeTaskID != "" {
		exec.CurrentNodeTaskID = req.CurrentNodeTaskID
	}
	if req.ReviewerTaskID != "" {
		exec.ReviewerTaskID = req.ReviewerTaskID
	}
	if req.RedTeamTaskID != "" {
		exec.RedTeamTaskID = req.RedTeamTaskID
	}
	if req.RequirementBranch != "" {
		exec.RequirementBranch = req.RequirementBranch
	}
	if len(req.DAGRaw) > 0 {
		exec.DAGRaw = req.DAGRaw
	}
	if len(req.SortedNodeIDs) > 0 {
		exec.SortedNodeIDs = req.SortedNodeIDs
	}

	if err := c.store.saveReq(ctx, req.Key, exec); err != nil {
		return ExecMutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Debug("Req phase updated via mutation", "key", req.Key, "phase", req.Stage)
	return ExecMutationResponse{Success: true}
}

func (c *Component) handleReqNodeMutation(ctx context.Context, data []byte) ExecMutationResponse {
	var req ReqNodeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return ExecMutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Key == "" {
		return ExecMutationResponse{Success: false, Error: "key required"}
	}

	exec, ok := c.store.getReq(req.Key)
	if !ok {
		return ExecMutationResponse{Success: false, Error: fmt.Sprintf("req not found: %s", req.Key)}
	}

	if req.CurrentNodeIdx != nil {
		exec.CurrentNodeIdx = *req.CurrentNodeIdx
	}
	if req.CurrentNodeTaskID != "" {
		exec.CurrentNodeTaskID = req.CurrentNodeTaskID
	}
	if req.NodeResult != nil {
		exec.NodeResults = append(exec.NodeResults, *req.NodeResult)
	}

	if err := c.store.saveReq(ctx, req.Key, exec); err != nil {
		return ExecMutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Debug("Req node updated via mutation", "key", req.Key)
	return ExecMutationResponse{Success: true}
}

func (c *Component) handleReqResetMutation(ctx context.Context, data []byte) ExecMutationResponse {
	var req ReqResetRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return ExecMutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Key == "" {
		return ExecMutationResponse{Success: false, Error: "key required"}
	}

	c.store.deleteReq(ctx, req.Key)

	c.logger.Info("Requirement execution reset via mutation", "key", req.Key)
	return ExecMutationResponse{Success: true}
}

// ---------------------------------------------------------------------------
// Claim handler
// ---------------------------------------------------------------------------

func (c *Component) handleExecClaimMutation(ctx context.Context, data []byte) ExecMutationResponse {
	var req ExecClaimRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return ExecMutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Key == "" || req.Stage == "" {
		return ExecMutationResponse{Success: false, Error: "key and phase required"}
	}

	// Try task first, then req
	if exec, ok := c.store.getTask(req.Key); ok {
		if exec.Stage == req.Stage {
			return ExecMutationResponse{Success: false, Error: fmt.Sprintf("already at stage %s", req.Stage)}
		}
		exec.Stage = req.Stage
		if err := c.store.saveTask(ctx, req.Key, exec); err != nil {
			return ExecMutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
		}
		c.logger.Info("Execution claimed via mutation", "key", req.Key, "phase", req.Stage)
		return ExecMutationResponse{Success: true}
	}

	if exec, ok := c.store.getReq(req.Key); ok {
		if exec.Stage == req.Stage {
			return ExecMutationResponse{Success: false, Error: fmt.Sprintf("already at stage %s", req.Stage)}
		}
		exec.Stage = req.Stage
		if err := c.store.saveReq(ctx, req.Key, exec); err != nil {
			return ExecMutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
		}
		c.logger.Info("Execution claimed via mutation", "key", req.Key, "phase", req.Stage)
		return ExecMutationResponse{Success: true}
	}

	return ExecMutationResponse{Success: false, Error: fmt.Sprintf("execution not found: %s", req.Key)}
}
