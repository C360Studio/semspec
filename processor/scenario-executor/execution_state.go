package scenarioexecutor

import (
	"sync"

	"github.com/c360studio/semspec/tools/decompose"
)

// scenarioExecution holds in-memory state for a single scenario execution.
// Keyed by entityID (local.semspec.workflow.scenario-execution.execution.<slug>-<scenarioID>)
// in the component's activeExecutions sync.Map.
//
// All field access must be guarded by mu. The sync.Map protects map operations,
// but the struct itself is shared across goroutines.
type scenarioExecution struct {
	mu sync.Mutex

	// terminated is set to true when the execution reaches a terminal state
	// (completed, failed, or error). Guards against double-terminal-writes
	// when timeout and completion events race.
	terminated bool

	// EntityID is the canonical graph entity ID:
	// local.semspec.workflow.scenario-execution.execution.<slug>-<scenarioID>
	EntityID string

	// Slug is the plan slug.
	Slug string

	// ScenarioID is the scenario identifier.
	ScenarioID string

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

	// --- Timeout ---

	timeoutTimer *timeoutHandle
}

// timeoutHandle wraps a timer reference so it can be stopped on completion.
type timeoutHandle struct {
	stop func()
}
