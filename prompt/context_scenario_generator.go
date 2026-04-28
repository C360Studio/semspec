package prompt

// ScenarioGeneratorPromptContext carries data for the scenario-generator
// user-prompt fragment. Maps from the legacy prompts.ScenarioGeneratorParams
// shape to a registry-owned typed slot.
type ScenarioGeneratorPromptContext struct {
	// PlanTitle, RequirementTitle, RequirementDescription identify the
	// requirement the LLM should generate scenarios for.
	PlanTitle              string
	RequirementID          string
	RequirementTitle       string
	RequirementDescription string

	// PlanGoal and PlanContext provide the surrounding plan details for
	// scenario authoring.
	PlanGoal    string
	PlanContext string

	// PreviousError carries the prior parse/validation failure when retrying.
	PreviousError string

	// ArchitectureContext is a pre-rendered markdown summary of declared
	// actors and integration points.
	ArchitectureContext string

	// ReviewFindings is the prior round's review-findings text injected so
	// the generator can address completeness gaps (ADR-029).
	ReviewFindings string
}
