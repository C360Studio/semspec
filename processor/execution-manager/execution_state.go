package executionmanager

import (
	"encoding/json"
	"sync"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
)

// taskExecution holds the live runtime state for a single task execution.
// Embeds *workflow.TaskExecution for all persistent fields — the store
// owns the embedded struct. Runtime-only fields (mu, terminated, timeout,
// raw LLM outputs) live here and are not persisted to KV.
//
// All field access must be guarded by mu.
type taskExecution struct {
	mu         sync.Mutex
	terminated bool
	key        string // EXECUTION_STATES KV key: task.<slug>.<taskID>

	// Persistent state — owned by the execution store.
	// Field access via embedding: exec.Slug, exec.Stage, exec.TDDCycle, etc.
	*workflow.TaskExecution

	// --- Runtime-only fields (not persisted to KV) ---

	// Context request ID from the original trigger.
	ContextRequestID string

	// Raw LLM outputs (large blobs, not worth persisting to KV).
	DeveloperOutput        json.RawMessage
	DeveloperLLMRequestIDs []string
	ValidationResults      []payloads.CheckResult
	ReviewerLLMRequestIDs  []string

	// Red team runtime state.
	RedTeamAgentID   string
	RedTeamChallenge *payloads.RedTeamChallengeResult
	RedTeamKnowledge string

	// Timeout management.
	timeoutTimer *timeoutHandle
}

// timeoutHandle wraps a timer reference so it can be stopped on completion.
type timeoutHandle struct {
	stop func()
}
