package prompt

// ArchitectPromptContext carries data for the architect user-prompt fragment.
type ArchitectPromptContext struct {
	Goal            string
	PlanContext     string
	ScopeInclude    []string
	ScopeExclude    []string
	ScopeProtected  []string
	Requirements    []ExistingRequirementSummary
	HarnessProfiles []HarnessProfileCard
	PreviousError   string
	ReviewFindings  string
}

// HarnessProfileCard is the compact catalog projection shown to the architect.
// Full details are resolved later for decomposer/developer prompts.
type HarnessProfileCard struct {
	ID                 string
	Tier               string
	Proves             []string
	Covers             map[string][]string
	RunnerSupport      []string
	Cost               string
	Constraints        []string
	RequiredAssertions []string
}

// ActorInfo is a lightweight view of an actor for prompt injection. Used by
// architecture context rendering.
type ActorInfo struct {
	Name     string
	Type     string
	Triggers []string
}

// IntegrationInfo is a lightweight view of an integration point for prompt
// injection.
type IntegrationInfo struct {
	Name      string
	Direction string
	Protocol  string
}
