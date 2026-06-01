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

	// RequiredTiers tells the scenario-generator which tier tags must appear
	// across its emitted scenarios for this requirement (ADR-041 Move 3).
	// Computed by the scenario-generator's classifier from the requirement's
	// capability surfaces + the architecture's selected harness profiles.
	// Each entry names a tier tag (e.g. "@unit", "@integration") and the
	// harness profile IDs scenarios at that tier must bind to (empty for
	// non-@integration tiers). The user-prompt fragment renders this as a
	// bullet list so the LLM emits ≥1 scenario per required tier with the
	// correct tag + binding.
	RequiredTiers []RequiredTier

	// Story fields — populated when the dispatcher is operating in per-Story
	// mode (ADR-043 PR 4j). When StoryID is non-empty, the user-prompt
	// renderer scopes the authoring task to this Sarah-prepared Story
	// (a slice of the parent Requirement) rather than the whole Requirement.
	// StoryID empty means legacy per-Requirement mode — pre-Sarah plans or
	// mock fixtures without Stories.
	StoryID         string
	StoryTitle      string
	StoryIntent     string
	StoryFilesOwned []string
	StoryComponents []string
}

// RequiredTier names one tier tag the scenario-generator MUST cover for the
// current requirement, plus any catalog harness profile IDs scenarios at
// that tier must bind to. ADR-041 Move 3.
type RequiredTier struct {
	// Tag is the tier tag (e.g. workflow.TierUnit "@unit" /
	// workflow.TierIntegration "@integration" / TierE2E "@e2e"). Always
	// non-empty.
	Tag string

	// HarnessProfileIDs lists the catalog profile IDs scenarios at this
	// tier must bind to. Populated only for "@integration"; empty for the
	// other tiers.
	HarnessProfileIDs []string
}
