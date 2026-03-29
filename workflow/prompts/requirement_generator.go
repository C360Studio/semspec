package prompts

import (
	"fmt"
)

// RequirementGeneratorParams contains the plan data needed to generate requirements.
type RequirementGeneratorParams struct {
	// Title is the plan title
	Title string

	// Goal describes what we're building or fixing
	Goal string

	// Context describes the current state and why this matters
	Context string

	// ScopeInclude lists files/directories in scope
	ScopeInclude []string

	// ScopeExclude lists files/directories explicitly out of scope
	ScopeExclude []string

	// ScopeProtected lists files/directories that must not be modified
	ScopeProtected []string

	// SOPRequirements lists SOP requirements that must be reflected in generated requirements.
	SOPRequirements []string

	// PreviousError holds the error message from a prior failed generation attempt.
	// When set, the prompt includes a section instructing the model to fix the issue.
	PreviousError string
}

// RequirementGeneratorResponse is the expected JSON output from the LLM.
type RequirementGeneratorResponse struct {
	Requirements []GeneratedRequirement `json:"requirements"`
}

// GeneratedRequirement is a single requirement from the LLM response.
type GeneratedRequirement struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// RequirementGeneratorPrompt builds the prompt for requirement generation from an approved plan.
// Each requirement represents a distinct behavioral intent that must be satisfied.
func RequirementGeneratorPrompt(params RequirementGeneratorParams) string {
	scopeInclude := formatScopeList(params.ScopeInclude, "all files")
	scopeExclude := formatScopeList(params.ScopeExclude, "none")
	scopeProtected := formatScopeList(params.ScopeProtected, "none")

	previousErrorSection := ""
	if params.PreviousError != "" {
		previousErrorSection = fmt.Sprintf(`

## Previous Attempt Failed

Your previous output could not be processed: %s

Please fix the issue and ensure your response is valid JSON matching the required format.`, params.PreviousError)
	}

	return fmt.Sprintf(`You are extracting high-level requirements from a development plan.

## Plan: %s

**Goal:** %s

**Context:** %s

**Scope:**
- Include: %s
- Exclude: %s
- Protected (do not touch): %s

## CRITICAL: Stay On Goal

Every requirement you extract MUST directly reflect the goal stated above.
- Do NOT invent requirements for features or behavior not mentioned in the goal.
- Use the exact feature names, paths, and terms from the goal.
- Requirements describe outcomes, not implementation steps.

## Your Task

Extract 3-10 high-level requirements from this plan. Each requirement must:
- Describe a distinct piece of intent — what the system should do or be
- Be independently testable — someone could write a test for it in isolation
- Use active voice: "The system must...", "Users must be able to..."
- Describe outcomes, not implementation (no function names, class names, or data structures)
- Cover both functional requirements (what it does) and quality requirements (how well it does it) when relevant

**Good requirements:**
- "The API must reject requests with missing required fields and return a descriptive error message"
- "Users must be able to authenticate using their existing credentials without re-registration"

**Bad requirements (too implementation-specific):**
- "The validateRequest() function must check the JSON body for missing fields"
- "The JWT middleware must call the UserService.Authenticate method"

## Output Format

Return ONLY valid JSON matching this exact structure:

`+"```json"+`
{
  "requirements": [
    {
      "title": "Input Validation",
      "description": "The API must validate all request parameters and return a 400 error with a descriptive message for any missing or malformed input."
    },
    {
      "title": "Authentication",
      "description": "Users must be able to authenticate using their email and password, receiving a JWT token valid for 24 hours upon success."
    }
  ]
}
`+"```"+`

**Important:** Return ONLY the JSON object, no additional text or explanation.
%s%s`, params.Title, params.Goal, params.Context, scopeInclude, scopeExclude, scopeProtected,
		FormatSOPRequirements(params.SOPRequirements), previousErrorSection)
}
