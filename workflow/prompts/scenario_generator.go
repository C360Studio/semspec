package prompts

import (
	"fmt"
	"strings"
)

// ScenarioGeneratorParams contains the requirement data needed to generate scenarios.
type ScenarioGeneratorParams struct {
	// PlanTitle is the parent plan title (for context)
	PlanTitle string

	// PlanGoal is the parent plan goal (for context)
	PlanGoal string

	// RequirementTitle is the requirement title being expanded into scenarios
	RequirementTitle string

	// RequirementDesc is the full requirement description
	RequirementDesc string

	// ArchitectureContext is a pre-formatted summary of actors and integration points.
	// When non-empty, injected into the prompt so scenarios cover system boundaries.
	ArchitectureContext string

	// PreviousError is the error message from a prior failed generation attempt.
	// When set, the prompt includes a section explaining what went wrong so the
	// LLM can self-correct before producing output.
	PreviousError string

	// ReviewFindings contains formatted findings from a prior review round (ADR-029).
	// When set, the prompt includes a section so the generator addresses completeness gaps.
	ReviewFindings string
}

// ActorInfo is a lightweight view of an actor for prompt injection.
type ActorInfo struct {
	Name     string
	Type     string
	Triggers []string
}

// IntegrationInfo is a lightweight view of an integration point for prompt injection.
type IntegrationInfo struct {
	Name      string
	Direction string
	Protocol  string
}

// ScenarioGeneratorResponse is the expected JSON output from the LLM.
type ScenarioGeneratorResponse struct {
	Scenarios []GeneratedScenario `json:"scenarios"`
}

// GeneratedScenario is a single BDD scenario from the LLM response.
type GeneratedScenario struct {
	Given string   `json:"given"`
	When  string   `json:"when"`
	Then  []string `json:"then"`
}

// ScenarioGeneratorPrompt builds the prompt for scenario generation from a single requirement.
// Each scenario is a Given/When/Then behavioral contract that tasks must satisfy.
func ScenarioGeneratorPrompt(params ScenarioGeneratorParams) string {
	archSection := ""
	if params.ArchitectureContext != "" {
		archSection = "\n" + params.ArchitectureContext + "\n"
	}

	base := fmt.Sprintf(`You are generating BDD scenarios for a specific requirement.

## Plan: %s

**Goal:** %s

## Requirement: %s

%s
%s
## Your Task

Generate 1-5 BDD scenarios that define the observable behavior for this requirement. Each scenario must:
- Describe ONE observable behavior
- Be independently executable — a QA engineer could run it without additional context
- Use specific, measurable outcomes
- Cover the happy path first, then key edge cases

**Scenario Design Guidelines:**
- **Given**: Precondition state — what exists before the action. Be specific: "a registered user with a valid session" not "a user exists"
- **When**: The triggering action — what the user or system does. One action per scenario, use active voice
- **Then**: Expected outcomes as an ARRAY of assertions — multiple things to verify. Use specific values where possible: "the response status is 200" not "the request succeeds"

Do NOT include implementation details — describe WHAT happens, not HOW it is implemented.

**Good scenario:**
- Given: "an unauthenticated user with a registered account"
- When: "they submit the login form with a valid email and correct password"
- Then: ["the response status is 200", "a JWT token is returned in the response body", "the token expires in 24 hours"]

**Bad scenario (too vague):**
- Given: "a user exists"
- When: "they log in"
- Then: ["it works"]

## Output Format

Return ONLY valid JSON matching this exact structure:

`+"```json"+`
{
  "scenarios": [
    {
      "given": "an unauthenticated user with a registered account",
      "when": "they submit the login form with a valid email and correct password",
      "then": [
        "the response status is 200",
        "a JWT token is returned in the response body",
        "the token expires in 24 hours"
      ]
    },
    {
      "given": "an unauthenticated user",
      "when": "they submit the login form with an incorrect password",
      "then": [
        "the response status is 401",
        "the response body contains the message 'Invalid credentials'",
        "no token is returned"
      ]
    }
  ]
}
`+"```"+`

**Important:** Return ONLY the JSON object, no additional text or explanation.
`, params.PlanTitle, params.PlanGoal, params.RequirementTitle, params.RequirementDesc, archSection)

	if params.PreviousError != "" {
		base += fmt.Sprintf(`
## Previous Attempt Failed

Your previous output could not be processed: %s

Please fix the issue and ensure your response is valid JSON matching the required format.
`, params.PreviousError)
	}

	if params.ReviewFindings != "" {
		base += fmt.Sprintf(`
## Previous Review Findings (Address These)

The previous set of scenarios was reviewed and rejected. Address ALL of the following findings:

%s
`, params.ReviewFindings)
	}

	return base
}

// ScenarioInfo is a simplified view of a scenario used for prompt injection into the task generator.
type ScenarioInfo struct {
	ID               string
	RequirementTitle string
	Given            string
	When             string
	Then             []string
}

// FormatScenariosForTaskGenerator formats a list of scenarios for injection into the task generator prompt.
// Used in pipeline mode so the LLM can reference scenario IDs in task output.
func FormatScenariosForTaskGenerator(scenarios []ScenarioInfo) string {
	if len(scenarios) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Behavioral Scenarios\n\n")
	sb.WriteString("The following scenarios define the observable behavior this plan must satisfy.\n")
	sb.WriteString("Each task you generate MUST reference one or more of these scenario IDs in its `scenario_ids` field.\n\n")

	for _, s := range scenarios {
		sb.WriteString(fmt.Sprintf("### Scenario `%s`", s.ID))
		if s.RequirementTitle != "" {
			sb.WriteString(fmt.Sprintf(" (%s)", s.RequirementTitle))
		}
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("- **Given:** %s\n", s.Given))
		sb.WriteString(fmt.Sprintf("- **When:** %s\n", s.When))
		sb.WriteString("- **Then:**\n")
		for _, outcome := range s.Then {
			sb.WriteString(fmt.Sprintf("  - %s\n", outcome))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// FormatArchitectureContext produces a markdown summary of actors and integration
// points for injection into the scenario generator prompt. Returns empty string
// when both slices are empty.
func FormatArchitectureContext(actors []ActorInfo, integrations []IntegrationInfo) string {
	if len(actors) == 0 && len(integrations) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Architecture Context\n\n")
	sb.WriteString("Use this architecture context to write more specific scenarios. Scenarios should reference the actors and integration points below where relevant.\n\n")

	if len(actors) > 0 {
		sb.WriteString("### Actors\n\n")
		for _, a := range actors {
			sb.WriteString(fmt.Sprintf("- **%s** (%s)", a.Name, a.Type))
			if len(a.Triggers) > 0 {
				sb.WriteString(fmt.Sprintf(": %s", strings.Join(a.Triggers, ", ")))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if len(integrations) > 0 {
		sb.WriteString("### Integration Points\n\n")
		for _, ip := range integrations {
			sb.WriteString(fmt.Sprintf("- **%s** (%s, %s)\n", ip.Name, ip.Direction, ip.Protocol))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
