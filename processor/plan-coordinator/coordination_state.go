package plancoordinator

import (
	"encoding/json"
	"sync"

	"github.com/c360studio/semspec/workflow"
)

// coordinationExecution holds in-memory state for a single coordination pipeline.
// Keyed by entityID (local.semspec.workflow.coordination.execution.<slug>) in the component's
// activeCoordinations sync.Map.
//
// All field access must be guarded by mu. The sync.Map protects map operations,
// but the struct itself is shared across goroutines.
type coordinationExecution struct {
	mu sync.Mutex

	// terminated is set to true when the execution reaches a terminal state
	// (completed or error). Guards against double-terminal-writes when timeout
	// and completion events race.
	terminated bool

	// EntityID is the canonical graph entity ID:
	// local.semspec.workflow.plan.execution.<slug>
	EntityID string

	// Slug is the plan slug.
	Slug string

	// --- Fields from the original trigger ---

	Title       string
	Description string
	ProjectID   string
	TraceID     string
	LoopID      string
	RequestID   string

	// --- Focus determination ---

	FocusAreas []*FocusArea

	// --- Planner tracking ---

	// ExpectedPlanners is the total number of planners dispatched.
	ExpectedPlanners int

	// CompletedResults maps plannerTaskID → parsed planner result.
	// When len(CompletedResults) == ExpectedPlanners, synthesis begins.
	CompletedResults map[string]*workflow.PlannerResult

	// PlannerTaskIDs holds all dispatched planner task IDs for cleanup.
	PlannerTaskIDs []string

	// LLMRequestIDs accumulated from all planners (for trajectory tracking).
	LLMRequestIDs []string

	// --- Synthesis output ---

	SynthesizedPlan *SynthesizedPlan
	SynthesisLLMID  string // LLM request ID from synthesis call

	// --- Timeout ---

	timeoutTimer *timeoutHandle
}

// allPlannersComplete returns true when all expected planners have reported results.
func (e *coordinationExecution) allPlannersComplete() bool {
	return len(e.CompletedResults) >= e.ExpectedPlanners
}

// collectResults returns a slice of all completed planner results in order.
func (e *coordinationExecution) collectResults() []workflow.PlannerResult {
	results := make([]workflow.PlannerResult, 0, len(e.CompletedResults))
	for _, r := range e.CompletedResults {
		results = append(results, *r)
	}
	return results
}

// FocusArea represents a planning focus area determined by the coordinator.
type FocusArea struct {
	Area        string
	Description string
	Hints       []string
}

// SynthesizedPlan is the final merged plan from multiple planners.
type SynthesizedPlan struct {
	Goal    string
	Context string
	Scope   workflow.Scope
}

// timeoutHandle wraps a timer reference so it can be stopped on completion.
type timeoutHandle struct {
	stop func()
}

// PlannerResultPayload is the parsed result from a planner agent's loop completion.
type PlannerResultPayload struct {
	Goal    string   `json:"goal"`
	Context string   `json:"context"`
	Scope   scopeRaw `json:"scope"`
}

type scopeRaw struct {
	Include    []string `json:"include,omitempty"`
	Exclude    []string `json:"exclude,omitempty"`
	DoNotTouch []string `json:"do_not_touch,omitempty"`
}

// focusAreasJSON marshals focus area names to a JSON string for triple storage.
func focusAreasJSON(areas []*FocusArea) string {
	names := make([]string, len(areas))
	for i, a := range areas {
		names[i] = a.Area
	}
	data, _ := json.Marshal(names)
	return string(data)
}
