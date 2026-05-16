package workflow

// Workflow ID constants for known workflow definitions.
// Note: Workflow IDs are defined here for reference by commands.
// The actual workflow definitions are in configs/workflows/*.json.

const (
	// WorkflowSlugPlanning identifies all planning-stage agent TaskMessages
	// (planner, requirement-generator, scenario-generator, plan-reviewer).
	// Used as WorkflowSlug in agentic.TaskMessage dispatches and to filter
	// AGENT_LOOPS KV completions.
	WorkflowSlugPlanning = "semspec-planning"

	// WorkflowSlugLessonDecomposition identifies lesson-decomposer agent
	// TaskMessages (ADR-033 Phase 2b). Set on the dispatched TaskMessage
	// and matched in the AGENT_LOOPS watcher so the decomposer only acts
	// on its own loop completions.
	WorkflowSlugLessonDecomposition = "semspec-lesson-decomposition"

	// WorkflowSlugWedgeRecovery identifies recovery-agent agent TaskMessages
	// (ADR-037 stage 1). Set on the dispatched TaskMessage and matched in
	// the AGENT_LOOPS watcher so the recovery-agent only acts on its own
	// loop completions.
	WorkflowSlugWedgeRecovery = "semspec-wedge-recovery"

	// WorkflowSlugResearch identifies researcher sub-agent TaskMessages
	// dispatched by researcher-manager in response to the developer's
	// research() tool call. Set on the dispatched TaskMessage so the
	// researcher's loop is attributable in AGENT_LOOPS and trajectory
	// snapshots. See project_research_tool_plan_2026_05_14.
	WorkflowSlugResearch = "semspec-research"
)
