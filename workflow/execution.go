// Package workflow — execution.go defines KV-serializable types for the
// EXECUTION_STATES bucket. These replace the internal sync.Map structs in
// execution-manager and requirement-executor.
//
// execution-manager is the single writer to EXECUTION_STATES.
// requirement-executor sends mutations via request/reply.
package workflow

import (
	"encoding/json"
	"time"
)

// TaskExecution is the KV-serializable state for a task in the TDD pipeline.
// Stored in EXECUTION_STATES under key "task.<slug>.<taskID>".
type TaskExecution struct {
	// Identity
	EntityID string `json:"entity_id"`
	Slug     string `json:"slug"`
	TaskID   string `json:"task_id"`

	// Lifecycle
	Stage        string `json:"stage"` // developing, validating, reviewing, approved, escalated, error
	TDDCycle     int    `json:"tdd_cycle"`
	MaxTDDCycles int    `json:"max_tdd_cycles"`

	// Context (from trigger)
	Title       string   `json:"title"`
	Description string   `json:"description"`
	ProjectID   string   `json:"project_id"`
	Prompt      string   `json:"prompt,omitempty"`
	Model       string   `json:"model"`
	TraceID     string   `json:"trace_id,omitempty"`
	LoopID      string   `json:"loop_id,omitempty"`
	RequestID   string   `json:"request_id,omitempty"`
	TaskType    TaskType `json:"task_type,omitempty"`

	// Agent identity (Phase B)
	AgentID    string `json:"agent_id,omitempty"`
	BlueTeamID string `json:"blue_team_id,omitempty"`
	RedTeamID  string `json:"red_team_id,omitempty"`

	// Sandbox worktree (persists across retries)
	WorktreePath   string   `json:"worktree_path,omitempty"`
	WorktreeBranch string   `json:"worktree_branch,omitempty"`
	ScenarioBranch string   `json:"scenario_branch,omitempty"`
	FileScope      []string `json:"file_scope,omitempty"`

	// Pipeline outputs
	FilesModified    []string `json:"files_modified,omitempty"`
	TestsPassed      bool     `json:"tests_passed,omitempty"`
	ValidationPassed bool     `json:"validation_passed,omitempty"`

	// Routing — task IDs for completion event dispatch.
	// Persisted so restart can re-route in-flight completions.
	DeveloperTaskID string `json:"developer_task_id,omitempty"`
	ValidatorTaskID string `json:"validator_task_id,omitempty"`
	ReviewerTaskID  string `json:"reviewer_task_id,omitempty"`
	RedTeamTaskID   string `json:"red_team_task_id,omitempty"`

	// Review outcome
	Verdict       string `json:"verdict,omitempty"`        // "approved" or "rejected"
	RejectionType string `json:"rejection_type,omitempty"` // "fixable" or "restructure"
	Feedback      string `json:"feedback,omitempty"`

	// Terminal annotations
	ErrorReason      string `json:"error_reason,omitempty"`
	EscalationReason string `json:"escalation_reason,omitempty"`

	// Timestamps
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TaskExecutionKey returns the EXECUTION_STATES KV key for a task execution.
func TaskExecutionKey(slug, taskID string) string {
	return "task." + slug + "." + taskID
}

// TaskExecutionEntityID returns the graph entity ID for a task execution.
// Instance is hashed to guarantee 6-part format and compact length.
func TaskExecutionEntityID(slug, taskID string) string {
	return EntityPrefix() + ".exec.task.run." + HashInstanceID(slug, taskID)
}

// IsTerminalTaskStage returns true if the stage is a terminal state.
func IsTerminalTaskStage(phase string) bool {
	switch phase {
	case "approved", "escalated", "error", "rejected":
		return true
	default:
		return false
	}
}

// NodeResult tracks output from a completed DAG node.
type NodeResult struct {
	NodeID        string   `json:"node_id"`
	FilesModified []string `json:"files_modified,omitempty"`
	Summary       string   `json:"summary,omitempty"`
}

// RequirementExecution is the KV-serializable state for a requirement execution.
// Stored in EXECUTION_STATES under key "req.<slug>.<requirementID>".
type RequirementExecution struct {
	// Identity
	EntityID      string `json:"entity_id"`
	Slug          string `json:"slug"`
	RequirementID string `json:"requirement_id"`

	// Lifecycle
	Stage string `json:"stage"` // decomposing, executing, reviewing, completed, failed, error

	// Context (from trigger)
	Title       string     `json:"title"`
	Description string     `json:"description"`
	ProjectID   string     `json:"project_id"`
	TraceID     string     `json:"trace_id,omitempty"`
	LoopID      string     `json:"loop_id,omitempty"`
	RequestID   string     `json:"request_id,omitempty"`
	Model       string     `json:"model,omitempty"`
	Scenarios   []Scenario `json:"scenarios,omitempty"`

	// Dispatch context (carried from trigger for KV self-trigger)
	DependsOn  []PrereqContext `json:"depends_on,omitempty"`
	Prompt     string          `json:"prompt,omitempty"`
	Role       string          `json:"role,omitempty"`
	PlanBranch string          `json:"plan_branch,omitempty"`

	// Agent
	BlueTeamID string `json:"blue_team_id,omitempty"`
	RedTeamID  string `json:"red_team_id,omitempty"`

	// DAG decomposition
	NodeCount      int             `json:"node_count,omitempty"`
	CurrentNodeIdx int             `json:"current_node_idx"` // -1 before execution starts
	DAGRaw         json.RawMessage `json:"dag,omitempty"`    // serialized TaskDAG
	SortedNodeIDs  []string        `json:"sorted_node_ids,omitempty"`
	NodeResults    []NodeResult    `json:"node_results,omitempty"`

	// Routing
	DecomposerTaskID  string `json:"decomposer_task_id,omitempty"`
	CurrentNodeTaskID string `json:"current_node_task_id,omitempty"`
	ReviewerTaskID    string `json:"reviewer_task_id,omitempty"`
	RedTeamTaskID     string `json:"red_team_task_id,omitempty"`

	// Branch
	RequirementBranch string `json:"requirement_branch,omitempty"`

	// Review
	ReviewVerdict  string `json:"review_verdict,omitempty"`
	ReviewFeedback string `json:"review_feedback,omitempty"`
	ErrorReason    string `json:"error_reason,omitempty"`

	// Retry
	RetryCount int `json:"retry_count,omitempty"`
	MaxRetries int `json:"max_retries,omitempty"`

	// Timestamps
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RequirementExecutionKey returns the EXECUTION_STATES KV key for a requirement execution.
func RequirementExecutionKey(slug, requirementID string) string {
	return "req." + slug + "." + requirementID
}

// RequirementExecutionEntityID returns the graph entity ID for a requirement execution.
// Instance is hashed to guarantee 6-part format and compact length.
func RequirementExecutionEntityID(slug, requirementID string) string {
	return EntityPrefix() + ".exec.req.run." + HashInstanceID(slug, requirementID)
}

// PrereqContext carries the completed work from a prerequisite requirement
// so downstream agents can reference prior decisions, files, and patterns.
type PrereqContext struct {
	RequirementID string   `json:"requirement_id"`
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	FilesModified []string `json:"files_modified,omitempty"`
	Summary       string   `json:"summary,omitempty"`
}

// IsTerminalReqStage returns true if the stage is a terminal state.
func IsTerminalReqStage(phase string) bool {
	switch phase {
	case "completed", "failed", "error":
		return true
	default:
		return false
	}
}
