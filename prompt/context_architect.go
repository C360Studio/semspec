package prompt

// ArchitectPromptContext carries data for the architect user-prompt fragment.
type ArchitectPromptContext struct {
	Title           string
	Goal            string
	PlanContext     string
	ScopeInclude    []string
	ScopeExclude    []string
	ScopeProtected  []string
	Requirements    []ExistingRequirementSummary
	PreviousError   string
	ReviewFindings  string
	SOPRequirements []string
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
