package prompt

// AnalystPromptContext carries everything the analyst sub-phase user-prompt
// fragment needs to render (ADR-040 Move 1). The analyst is Mary's first
// sub-phase: she identifies CAPABILITIES from the user prompt without
// proposing scope, files, or implementation steps.
//
// Distinct from PlannerPromptContext — the planner sub-phase consumes the
// resulting Exploration and produces Goal/Context/Scope. Two contexts so
// the renderer can produce sub-phase-specific instructions without
// branching inside one shared context.
type AnalystPromptContext struct {
	// Title is the plan title (the user's request as a one-line label).
	Title string

	// Description is the user's full request, when available. Provides the
	// raw text Mary classifies into capabilities.
	Description string

	// ProjectFileTree is the workspace snapshot at dispatch time (matches
	// PlannerPromptContext.ProjectFileTree). Helps Mary distinguish
	// "new" capabilities from "modified" ones when an OpenSpec
	// openspec/specs/ directory exists.
	ProjectFileTree string

	// PreviousError surfaces the parser/transport error from a prior
	// analyst attempt, when retrying. Empty on first attempt.
	PreviousError string
}
