package planmanager

import (
	"encoding/json"
	"sync"

	"github.com/c360studio/semspec/workflow"
)

// coordinationExecution holds in-memory state for a single coordination pipeline.
// Keyed by entityID (semspec.local.exec.coord.run.<slug>) in the component's
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
	// semspec.local.exec.plan.run.<slug>
	EntityID string

	// CurrentPhase tracks the current pipeline phase in memory.
	// Used as a guard against duplicate/stale events from generators.
	CurrentPhase string

	// Iteration tracks the current review cycle (0-based). Incremented
	// each time the reviewer returns needs_changes and the planner is
	// re-dispatched with feedback.
	Iteration int

	// ReviewFeedback holds the reviewer's summary from the most recent
	// needs_changes verdict. Passed to the planner as PreviousFindings
	// on retry.
	ReviewFeedback string

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

	// --- Review round tracking ---

	// ReviewRound tracks which review gate we're at:
	//   1 = plan review (after synthesis, before requirement generation)
	//   2 = requirements/scenarios review (after scenario generation, before execution)
	ReviewRound int

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
// The planner publishes Result{Content: &PlanContent{Goal, Context, Scope}},
// so the Goal/Context/Scope are nested under "content".
type PlannerResultPayload struct {
	Content *plannerContentPayload `json:"content"`
	// Legacy: top-level fields for backward compatibility with older planner versions.
	Goal    string   `json:"goal"`
	Context string   `json:"context"`
	Scope   scopeRaw `json:"scope"`
}

type plannerContentPayload struct {
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
