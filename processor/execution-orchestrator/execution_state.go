package executionorchestrator

import (
	"encoding/json"
	"sync"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
)

// taskExecution holds in-memory state for a single task execution pipeline.
// Keyed by entityID (local.semspec.workflow.task-execution.execution.<slug>-<taskID>) in the
// component's activeExecutions sync.Map.
//
// All field access must be guarded by mu. The sync.Map protects map operations,
// but the struct itself is shared across goroutines.
type taskExecution struct {
	mu sync.Mutex

	// terminated is set to true when the execution reaches a terminal state
	// (approved, escalated, or error). Guards against double-terminal-writes
	// when timeout and completion events race.
	terminated bool

	// EntityID is the canonical graph entity ID:
	// local.semspec.workflow.task-execution.execution.<slug>-<taskID>
	EntityID string

	// Slug is the plan slug.
	Slug string

	// TaskID is the task identifier from the plan.
	TaskID string

	// Iteration tracks how many developer→validate→review cycles have completed
	// (0-based). Compared against MaxIterations to decide retry or escalate.
	Iteration int

	// MaxIterations is the per-execution budget from Config.MaxIterations.
	MaxIterations int

	// --- Fields from the original trigger ---

	Title            string
	Description      string
	ProjectID        string
	Prompt           string // Complete developer prompt with inline context
	Model            string
	ContextRequestID string
	TraceID          string
	LoopID           string
	RequestID        string

	// --- Tester output (populated after tester completes) ---

	TesterTaskID string
	TesterOutput json.RawMessage
	TestsPassed  bool

	// --- Builder output (populated after builder completes) ---

	BuilderTaskID        string
	BuilderOutput        json.RawMessage
	BuilderLLMRequestIDs []string

	// --- Developer output (populated after developer completes; kept for backward compat) ---

	FilesModified          []string
	DeveloperOutput        json.RawMessage
	DeveloperLLMRequestIDs []string

	// --- Validator output (populated after validator completes) ---

	ValidationPassed  bool
	ValidationResults json.RawMessage

	// --- Reviewer output (populated after reviewer completes) ---

	Verdict               string // "approved" or "rejected"
	RejectionType         string // "fixable", "misscoped", "architectural", "too_big"
	Feedback              string
	ReviewerLLMRequestIDs []string

	// --- Task IDs for routing loop-completion events ---

	DeveloperTaskID string
	ValidatorTaskID string
	ReviewerTaskID  string

	// --- Task type (pipeline selection) ---

	// TaskType determines which execution pipeline to use.
	// Default (empty or "implement"): Tester → Builder → Validator → Reviewer
	TaskType workflow.TaskType

	// --- Persistent agent identity (Phase B) ---

	// AgentID is the persistent agent ID assigned to this execution.
	// Set during initial trigger handling via SelectAgent; may change on
	// benching if a replacement agent is selected.
	AgentID string

	// --- Sandbox worktree (persists across retries) ---

	// WorktreePath is the filesystem path of the worktree on the sandbox server.
	// Set once during initial dispatch; never cleared on retry.
	WorktreePath string

	// WorktreeBranch is the git branch name for this worktree (e.g. "agent/<taskID>").
	// Set once during initial dispatch; never cleared on retry.
	WorktreeBranch string

	// ScenarioBranch is the scenario branch this task merges into
	// (e.g. "semspec/scenario-auth-refresh"). Set from the trigger payload.
	ScenarioBranch string

	// --- Team-mode fields (populated when teams are enabled) ---

	// BlueTeamID is the team that did the implementation.
	BlueTeamID string

	// RedTeamID is the team that challenges the implementation.
	RedTeamID string

	// RedTeamAgentID is the specific agent from the red team doing the critique.
	RedTeamAgentID string

	// RedTeamTaskID is used for routing red-team loop completion events.
	RedTeamTaskID string

	// RedTeamChallenge holds the parsed red team challenge result.
	RedTeamChallenge *payloads.RedTeamChallengeResult

	// RedTeamKnowledge holds the team knowledge block pre-built for red team
	// dispatch. Stored here because agentic.TaskMessage has no prompt field;
	// future wiring will pass this via a dedicated RedTeamRequest payload.
	// TODO: introduce RedTeamRequest payload and inject RedTeamKnowledge there.
	RedTeamKnowledge string

	// timeoutTimer holds the per-execution timeout.
	timeoutTimer *timeoutHandle
}

// timeoutHandle wraps a timer reference so it can be stopped on completion.
type timeoutHandle struct {
	stop func()
}
