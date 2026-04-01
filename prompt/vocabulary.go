package prompt

// Vocabulary provides display labels for prompt assembly and UI rendering.
// Loaded from configs/vocabulary.json (or preset files). When nil, fragments
// use hardcoded defaults matching the current prompt text.
type Vocabulary struct {
	Agent     string          `json:"agent"`      // "Developer", "Analyst", "Adventurer"
	Task      string          `json:"task"`       // "Task", "Sprint", "Story"
	Plan      string          `json:"plan"`       // "Plan", "Project Brief", "PRD"
	Review    string          `json:"review"`     // "Code Review", "Quality Gate"
	Team      string          `json:"team"`       // "Team", "Party", "Squad"
	RoleNames map[Role]string `json:"role_names"` // planner→"Strategic Analyst"
}

// DefaultVocabulary returns vocabulary matching the current hardcoded prompt values.
func DefaultVocabulary() *Vocabulary {
	return &Vocabulary{
		Agent:  "Developer",
		Task:   "Task",
		Plan:   "Plan",
		Review: "Code Review",
		Team:   "Team",
		RoleNames: map[Role]string{
			RolePlanner:              "Planner",
			RoleDeveloper:            "Developer",
			RoleValidator:            "Validator",
			RoleReviewer:             "Code Reviewer",
			RolePlanReviewer:         "Plan Reviewer",
			RoleRequirementGenerator: "Requirement Generator",
			RoleScenarioGenerator:    "Scenario Generator",
			RoleArchitect:            "Architect",
			RoleQA:                   "QA Engineer",
		},
	}
}

// RoleName returns the display name for a role, falling back to string(role).
func (v *Vocabulary) RoleName(role Role) string {
	if v != nil {
		if name, ok := v.RoleNames[role]; ok {
			return name
		}
	}
	return string(role)
}

// AgentPersona provides optional persona configuration for prompt injection.
// Configured per-role in semspec.json. When nil, existing domain fragments
// provide the identity (no behavioral change for current users).
type AgentPersona struct {
	DisplayName  string   `json:"display_name"`            // "Mary", "Winston" — UI + logs
	SystemPrompt string   `json:"system_prompt,omitempty"` // injected at CategoryPersona
	Backstory    string   `json:"backstory,omitempty"`     // optional character narrative
	Traits       []string `json:"traits,omitempty"`        // personality attributes
	Style        string   `json:"style,omitempty"`         // communication style
}
