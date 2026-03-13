package executionorchestrator

import (
	"encoding/json"
	"sync"
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

	// --- Developer output (populated after developer completes) ---

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

	// timeoutTimer holds the per-execution timeout.
	timeoutTimer *timeoutHandle
}

// timeoutHandle wraps a timer reference so it can be stopped on completion.
type timeoutHandle struct {
	stop func()
}
