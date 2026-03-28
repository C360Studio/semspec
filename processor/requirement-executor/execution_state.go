package requirementexecutor

import (
	"sync"

	"github.com/c360studio/semspec/tools/decompose"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
)

// NodeResult tracks output from a completed DAG node for aggregate reporting.
type NodeResult struct {
	NodeID        string   `json:"node_id"`
	FilesModified []string `json:"files_modified,omitempty"`
	Summary       string   `json:"summary,omitempty"`
}

// requirementExecution holds in-memory state for a single requirement execution.
// Keyed by entityID (semspec.local.exec.req.run.<slug>-<requirementID>)
// in the component's activeExecutions sync.Map.
//
// All field access must be guarded by mu. The sync.Map protects map operations,
// but the struct itself is shared across goroutines.
type requirementExecution struct {
	mu sync.Mutex

	// terminated is set to true when the execution reaches a terminal state
	// (completed, failed, or error). Guards against double-terminal-writes
	// when timeout and completion events race.
	terminated bool

	// storeKey is the EXECUTION_STATES KV key (req.<slug>.<reqID>).
	// Set after successful creation mutation to execution-manager.
	storeKey string

	// EntityID is the canonical graph entity ID:
	// semspec.local.exec.req.run.<slug>-<requirementID>
	EntityID string

	// Slug is the plan slug.
	Slug string

	// RequirementID is the requirement identifier.
	RequirementID string

	// Title is the requirement title.
	Title string

	// Description is the requirement description.
	Description string

	// Scenarios are the acceptance criteria for this requirement.
	Scenarios []workflow.Scenario

	// DependsOn carries completed work from prerequisite requirements.
	DependsOn []payloads.PrereqContext

	// --- Fields from the original trigger ---

	Prompt    string
	Role      string
	Model     string
	ProjectID string
	TraceID   string
	LoopID    string
	RequestID string

	// --- Decomposition output ---

	// DAG is the validated task DAG from the decomposer agent.
	DAG *decompose.TaskDAG

	// SortedNodeIDs is the topologically sorted list of node IDs.
	// Execution proceeds serially through this list.
	SortedNodeIDs []string

	// NodeIndex maps nodeID → TaskNode for quick lookup.
	NodeIndex map[string]*decompose.TaskNode

	// DecomposerTaskID is the agentic task ID of the decomposer agent.
	DecomposerTaskID string

	// --- Serial execution tracking ---

	// CurrentNodeIdx is the index into SortedNodeIDs of the node currently
	// being executed. -1 before execution starts.
	CurrentNodeIdx int

	// CurrentNodeTaskID is the agentic task ID of the currently executing node.
	CurrentNodeTaskID string

	// VisitedNodes tracks which nodes have finished successfully.
	VisitedNodes map[string]bool

	// NodeResults tracks aggregate output from completed nodes.
	NodeResults []NodeResult

	// --- Branch strategy ---

	// RequirementBranch is the branch created for this requirement execution
	// (e.g. "semspec/requirement-auth-refresh"). Task worktrees branch from
	// and merge back into this branch.
	RequirementBranch string

	// --- Requirement-level review ---

	// RedTeamTaskID is the agentic task ID for the red team challenge.
	RedTeamTaskID string

	// RedTeamChallenge holds the parsed red team result.
	RedTeamChallenge *payloads.RedTeamChallengeResult

	// ReviewerTaskID is the agentic task ID for the scenario reviewer.
	ReviewerTaskID string

	// ReviewVerdict is the reviewer's verdict ("approved" or "rejected").
	ReviewVerdict string

	// ReviewFeedback is the reviewer's feedback.
	ReviewFeedback string

	// BlueTeamID is the team that did the implementation (set from trigger).
	BlueTeamID string

	// RedTeamID is the adversarial review team.
	RedTeamID string

	// --- Requirement-level retry ---

	// RetryCount is the number of requirement-level retries performed so far.
	// Incremented on each fixable rejection or restructure. Gated by config.MaxRequirementRetries.
	RetryCount int

	// MaxRetries is the retry budget from config (copied at creation time).
	MaxRetries int

	// DirtyNodeIDs lists node IDs that need re-execution on a fixable retry.
	// Empty means all nodes are clean (first attempt or restructure).
	DirtyNodeIDs []string

	// LastReviewFeedback carries the reviewer's feedback from the last rejection.
	// Appended to dirty node prompts on retry.
	LastReviewFeedback string

	// ScenarioVerdicts carries per-scenario pass/fail from the last review.
	ScenarioVerdicts []ScenarioVerdict

	// --- Timeout ---

	timeoutTimer *timeoutHandle
}

// ScenarioVerdict carries a per-scenario pass/fail from the requirement reviewer.
type ScenarioVerdict struct {
	ScenarioID string `json:"scenario_id"`
	Passed     bool   `json:"passed"`
	Feedback   string `json:"feedback,omitempty"`
}

// timeoutHandle wraps a timer reference so it can be stopped on completion.
type timeoutHandle struct {
	stop func()
}
