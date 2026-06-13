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
	execMutationTaskCreate          = "execution.mutation.task.create"
	execMutationTaskPhase           = "execution.mutation.task.phase"
	execMutationTaskComplete        = "execution.mutation.task.complete"
	execMutationReqCreate           = "execution.mutation.req.create"
	execMutationReqPhase            = "execution.mutation.req.phase"
	execMutationReqNode             = "execution.mutation.req.node"
	execMutationReqReset            = "execution.mutation.req.reset"
	execMutationReqResetNodeResults = "execution.mutation.req.reset_node_results"
	execMutationClaim               = "execution.mutation.claim"
)

// ---------------------------------------------------------------------------
// Request types
// ---------------------------------------------------------------------------

// TaskCreateRequest creates a new task execution entry.
type TaskCreateRequest struct {
	Slug           string            `json:"slug"`
	TaskID         string            `json:"task_id"`
	RequirementID  string            `json:"requirement_id,omitempty"` // parent requirement for DAG-node tasks
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
	WorktreePath   string            `json:"worktree_path,omitempty"`
	WorktreeBranch string            `json:"worktree_branch,omitempty"`
	ScenarioBranch string            `json:"scenario_branch,omitempty"`
	FileScope      []string          `json:"file_scope,omitempty"`

	// Scenarios are the BDD scenarios this DAG node is responsible for
	// satisfying. Filtered from the parent requirement's full scenario set
	// by node.ScenarioIDs at requirement-executor dispatch time. Persisted
	// to TaskExecution.Scenarios so developer + reviewer + validator prompts
	// can ground their work in the actual given/when/then contract — closes
	// the Cline-blind-to-contract disconnect surfaced 2026-06-03.
	Scenarios []workflow.Scenario `json:"scenarios,omitempty"`
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
	ErrorClass       string   `json:"error_class,omitempty"`
	EscalationReason string   `json:"escalation_reason,omitempty"`
	// Routing task IDs (set when dispatching to agentic loop)
	DeveloperTaskID string `json:"developer_task_id,omitempty"`
	ValidatorTaskID string `json:"validator_task_id,omitempty"`
	ReviewerTaskID  string `json:"reviewer_task_id,omitempty"`
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
	Slug          string                   `json:"slug"`
	RequirementID string                   `json:"requirement_id"`
	Title         string                   `json:"title"`
	Description   string                   `json:"description,omitempty"`
	ProjectID     string                   `json:"project_id,omitempty"`
	TraceID       string                   `json:"trace_id,omitempty"`
	LoopID        string                   `json:"loop_id,omitempty"`
	RequestID     string                   `json:"request_id,omitempty"`
	Model         string                   `json:"model,omitempty"`
	Scenarios     []workflow.Scenario      `json:"scenarios,omitempty"`
	DependsOn     []workflow.PrereqContext `json:"depends_on,omitempty"`
	Prompt        string                   `json:"prompt,omitempty"`
	Role          string                   `json:"role,omitempty"`
	PlanBranch    string                   `json:"plan_branch,omitempty"`
	// BaseBranch is the orchestrator-resolved branch-derivation base (see
	// workflow.RequirementExecution.BaseBranch). Carried verbatim into the
	// requirement execution so the executor forks the requirement branch from
	// its prerequisites' work rather than the plan base.
	BaseBranch string `json:"base_branch,omitempty"`
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
	ErrorClass     string `json:"error_class,omitempty"`
	// Routing
	CurrentNodeTaskID string `json:"current_node_task_id,omitempty"`
	ReviewerTaskID    string `json:"reviewer_task_id,omitempty"`
	// Branch
	RequirementBranch string `json:"requirement_branch,omitempty"`
	// DAG state — persisted after decomposition for crash recovery
	DAGRaw        json.RawMessage `json:"dag,omitempty"`
	SortedNodeIDs []string        `json:"sorted_node_ids,omitempty"`
	// Story sequencing (ADR-043 PR 4h)
	SortedStoryIDs  []string `json:"sorted_story_ids,omitempty"`
	CurrentStoryIdx *int     `json:"current_story_idx,omitempty"`
}

// ReqResetRequest deletes a requirement execution entry from EXECUTION_STATES.
// Called by plan-manager's retry handler to clear failed/error entries before re-dispatch.
type ReqResetRequest struct {
	Key string `json:"key"` // KV key: req.<slug>.<reqID>
}

// ReqResetNodeResultsRequest replaces the NodeResults slice on an
// existing requirement execution KV entry without disturbing any other
// field. When NodeResults is nil/empty the slice is wiped (recovery
// resume + restructure retry paths). When NodeResults is populated the
// slice is REPLACED with the supplied list (fixable-retry path, which
// trims dirty-node entries but keeps clean entries — closes go-reviewer
// Pass-1 H4b).
//
// Closes go-reviewer Pass-1 findings H4 and H4b. The KV-side NodeResults
// is otherwise append-only via handleReqNodeMutation; without this
// mutation, in-memory trims/wipes would diverge from KV and rebuilt
// state would carry stale entries on the next restart.
type ReqResetNodeResultsRequest struct {
	Key         string                `json:"key"`                    // KV key: req.<slug>.<reqID>
	NodeResults []workflow.NodeResult `json:"node_results,omitempty"` // empty/nil = wipe; populated = replace
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
		{execMutationReqResetNodeResults, c.handleReqResetNodeResultsMutation},
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
		RequirementID:  req.RequirementID,
		Stage:          "pending", // KV self-trigger: watcher claims → developing
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
		WorktreePath:   req.WorktreePath,
		WorktreeBranch: req.WorktreeBranch,
		ScenarioBranch: req.ScenarioBranch,
		FileScope:      req.FileScope,
		Scenarios:      req.Scenarios,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := c.store.saveTask(ctx, key, exec); err != nil {
		return ExecMutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Info("Task execution created via mutation",
		"key", key,
		"slug", req.Slug,
		"task_id", req.TaskID,
		"requirement_id", req.RequirementID,
	)
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
	if req.ErrorClass != "" {
		exec.ErrorClass = req.ErrorClass
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
		Stage:          "pending", // KV self-trigger: watcher claims → decomposing
		Title:          req.Title,
		Description:    req.Description,
		ProjectID:      req.ProjectID,
		TraceID:        req.TraceID,
		LoopID:         req.LoopID,
		RequestID:      req.RequestID,
		Model:          req.Model,
		Scenarios:      req.Scenarios,
		DependsOn:      req.DependsOn,
		Prompt:         req.Prompt,
		Role:           req.Role,
		PlanBranch:     req.PlanBranch,
		BaseBranch:     req.BaseBranch,
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
	if req.ErrorClass != "" {
		exec.ErrorClass = req.ErrorClass
	}
	if req.CurrentNodeTaskID != "" {
		exec.CurrentNodeTaskID = req.CurrentNodeTaskID
	}
	if req.ReviewerTaskID != "" {
		exec.ReviewerTaskID = req.ReviewerTaskID
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
	if len(req.SortedStoryIDs) > 0 {
		exec.SortedStoryIDs = req.SortedStoryIDs
	}
	if req.CurrentStoryIdx != nil {
		exec.CurrentStoryIdx = *req.CurrentStoryIdx
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

// handleReqResetNodeResultsMutation replaces the NodeResults slice on an
// existing requirement execution entry while leaving every other field
// intact. Called from requirement-executor when in-memory NodeResults
// state diverges from KV — without this mutation the KV-side slice
// (which handleReqNodeMutation only APPENDS to) would accumulate stale
// entries and reappear on the next restart via rebuildExecFromKV.
//
// When req.NodeResults is empty/nil the KV slice is wiped (recovery
// resume + restructure retry — closes Pass-1 H4). When req.NodeResults
// is populated the KV slice is REPLACED with the supplied list
// (fixable-retry, which keeps clean-node entries and drops dirty-node
// entries — closes Pass-1 H4b).
//
// Returns Success=true even when the key doesn't exist — the reset
// semantic is idempotent and treating "no entry to reset" as success
// keeps the producer's call site simple. A missing key here usually
// means the exec was never persisted (unit-test mode or pre-create
// failure); both are fine to no-op.
func (c *Component) handleReqResetNodeResultsMutation(ctx context.Context, data []byte) ExecMutationResponse {
	var req ReqResetNodeResultsRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return ExecMutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Key == "" {
		return ExecMutationResponse{Success: false, Error: "key required"}
	}

	exec, ok := c.store.getReq(req.Key)
	if !ok {
		c.logger.Debug("reset_node_results: no req entry, no-op", "key", req.Key)
		return ExecMutationResponse{Success: true}
	}

	// Empty/nil → wipe; populated → replace. The append-vs-replace split
	// is the load-bearing contract; handleReqNodeMutation continues to
	// own the append path for normal node completion.
	exec.NodeResults = req.NodeResults

	if err := c.store.saveReq(ctx, req.Key, exec); err != nil {
		return ExecMutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	if len(req.NodeResults) == 0 {
		c.logger.Info("Requirement NodeResults wiped via mutation", "key", req.Key)
	} else {
		c.logger.Info("Requirement NodeResults replaced via mutation",
			"key", req.Key, "kept", len(req.NodeResults))
	}
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
