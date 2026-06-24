package requirementexecutor

import (
	"sync"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
)

// NodeResult tracks output from a completed DAG node for aggregate reporting.
//
// CommitSHA is the merge commit produced when this node's worktree was
// merged into main. Empty when the node is still in flight, or when the
// merge silently dropped the work (the bug-#9 pattern).
// Today only populated when execution-manager wires it through; absence
// is interpreted by the require_commit_observation gate.
type NodeResult struct {
	NodeID        string   `json:"node_id"`
	FilesModified []string `json:"files_modified,omitempty"`
	Summary       string   `json:"summary,omitempty"`
	CommitSHA     string   `json:"commit_sha,omitempty"`
}

// requirementExecution holds in-memory state for a single requirement execution.
// Keyed by entityID (semspec.local.exec.req.run.<slug>-<requirementID>)
// in the component's activeExecs TTL cache.
//
// All field access must be guarded by mu. The cache protects map operations,
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

	// Scope is the plan's file scope (include/exclude/do_not_touch).
	// Populated from PLAN_STATES at execution start so decomposer and
	// downstream agents know which files are in play.
	Scope *workflow.Scope

	// --- Fields from the original trigger ---

	Prompt    string
	Role      string
	Model     string
	ProjectID string
	TraceID   string
	LoopID    string
	RequestID string

	// --- Story sequencing (ADR-043 PR 4h) ---

	// SortedStoryIDs is the topologically sorted list of Story.IDs for
	// this requirement. The executor dispatches one Story at a time in
	// this order. Empty before initialization; populated from
	// plan.StoriesForRequirement via topoSortStoryIDs.
	SortedStoryIDs []string

	// CurrentStoryIdx is the index into SortedStoryIDs of the Story
	// currently being executed. -1 before the first Story is dispatched.
	CurrentStoryIdx int

	// --- Per-Story DAG (re-populated on each Story transition) ---

	// DAG is the synthesized TaskDAG for the CURRENT Story. Reset between
	// Stories — at any moment the DAG carries only the nodes Sarah authored
	// for SortedStoryIDs[CurrentStoryIdx].
	DAG *TaskDAG

	// SortedNodeIDs is the topologically sorted list of node IDs for the
	// CURRENT Story's DAG.
	SortedNodeIDs []string

	// NodeIndex maps nodeID → TaskNode for the CURRENT Story's DAG.
	NodeIndex map[string]*TaskNode

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

	// NodeTaskIDs tracks all dispatched node task IDs for worktree cleanup.
	// execution-manager keeps worktrees alive after merge (WithKeepWorktree)
	// so the requirement-level reviewer can access files. Cleaned up in
	// cleanupExecutionLocked.
	NodeTaskIDs []string

	// --- Branch strategy ---

	// RequirementBranch is the branch created for this requirement execution
	// (e.g. "semspec/requirement-auth-refresh"). Task worktrees branch from
	// and merge back into this branch.
	RequirementBranch string

	// BaseBranch is the orchestrator-resolved DependsOn-derivation base this
	// requirement's branch forks FROM (see workflow.RequirementExecution.BaseBranch).
	// Carried so the recovery-resume branch recreate forks from the same derived
	// base as the initial create, not "HEAD" — otherwise a reopened mid-chain
	// requirement loses the prerequisite edits it was derived from.
	BaseBranch string

	// --- Requirement-level review ---

	// ReviewerTaskID is the agentic task ID for the scenario reviewer.
	ReviewerTaskID string

	// ReviewVerdict is the reviewer's verdict ("approved" or "rejected").
	ReviewVerdict string

	// ReviewFeedback is the reviewer's feedback.
	ReviewFeedback string

	// ReviewRetryCount tracks reviewer re-dispatches on parse failure or
	// invalid verdict. Independent of the requirement-level RetryCount.
	ReviewRetryCount int

	// --- Requirement-level retry ---

	// RetryCount is the number of requirement-level retries performed so far.
	// Incremented on each fixable rejection or restructure. Gated by config.MaxRequirementRetries.
	RetryCount int

	// MaxRetries is the retry budget from config (copied at creation time).
	MaxRetries int

	// DirtyNodeIDs lists node IDs that need re-execution on a targeted fixable retry.
	// Empty means no targeted retry is active; first attempts and restructure retries
	// run by phase/DAG state.
	DirtyNodeIDs []string

	// LastReviewFeedback carries the reviewer's feedback from the last rejection.
	// Appended to dirty node prompts on retry.
	LastReviewFeedback string

	// ScenarioVerdicts carries accumulated passing/failing per-scenario proof
	// from approved Story reviews. It is persisted at requirement completion
	// so M:N borrowers can verify owner evidence for their scoped scenarios.
	ScenarioVerdicts []ScenarioVerdict

	// --- Timeout ---

	timeoutTimer *timeoutHandle

	// --- ADR-037 race-closure (phaseAwaitingRecovery) ---

	// awaitingRecovery is true while this execution has been deferred from
	// terminal-failure pending a recovery PlanDecision accept (or timeout).
	// When true, terminated MUST be false — the exec is paused, not closed.
	awaitingRecovery bool

	// recoveryTimer fires after Config.RecoveryTimeoutSeconds with no accept;
	// it terminal-fails the exec via markFailedLocked using recoveryReason.
	// Separate from timeoutTimer (per-execution wall clock) so the two
	// timeouts can be reasoned about independently.
	recoveryTimer *timeoutHandle

	// recoveryReason is the original failure reason captured at defer time.
	// Used as the markFailedLocked argument when recoveryTimer expires.
	recoveryReason string

	// recoveryRestarts is the count of resumeFromRecoveryLocked invocations.
	// Bounded by Config.MaxRecoveryRestarts so a recovery loop can't burn
	// budget indefinitely. Separate counter from RetryCount because recovery
	// restarts are out-of-band — they fire AFTER the normal retry budget is
	// already exhausted.
	recoveryRestarts int

	// recoveryInfraRetries counts CONSECUTIVE awaiting-recovery deadline
	// expiries where the pending-PlanDecision check could not reach NATS/KV
	// (infrastructure unreachable — e.g. a network blip). The deadline timer
	// extends instead of terminal-failing in that case, but is bounded by
	// maxRecoveryInfraRetries so a PERMANENT outage still terminates. Reset to
	// 0 on any successful read so only a sustained outage trips the cap.
	recoveryInfraRetries int
}

// ScenarioVerdict carries a per-scenario pass/fail from the requirement reviewer.
type ScenarioVerdict = workflow.ScenarioVerdict

// timeoutHandle wraps a timer reference so it can be stopped on completion.
type timeoutHandle struct {
	stop func()
}
