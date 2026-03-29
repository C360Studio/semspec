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
)
