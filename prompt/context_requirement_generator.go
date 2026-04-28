package prompt

// RequirementGeneratorContext carries everything the requirement-generator
// user-prompt fragment needs to render. The component constructs one of these
// from its trigger payload before calling Assembler.Assemble; keeping the
// shape on prompt/ (instead of importing workflow/payloads) avoids the
// payloads → prompt → payloads dependency cycle.
type RequirementGeneratorContext struct {
	// Title, Goal, Context are the plan fields the agent decomposes into requirements.
	Title   string
	Goal    string
	Context string

	// ScopeInclude lists files/directories the plan is allowed to modify.
	ScopeInclude []string
	// ScopeExclude lists files/directories explicitly out of scope.
	ScopeExclude []string
	// ScopeDoNotTouch lists files/directories that must NEVER be modified.
	ScopeDoNotTouch []string

	// ExistingRequirements is populated for partial-regen flows so the LLM
	// sees what's already approved (and shouldn't re-emit). Empty slice for
	// fresh generation.
	ExistingRequirements []ExistingRequirementSummary

	// ReplaceRequirementIDs lists the IDs of rejected requirements to be
	// replaced. Non-empty implies partial regen.
	ReplaceRequirementIDs []string

	// RejectionReasons maps requirement ID → human-readable reason for
	// rejection. Used to give the LLM specific failure context per ID.
	RejectionReasons map[string]string

	// PreviousError is the parser/validation error from the prior generation
	// attempt, when retrying. Empty on first attempt.
	PreviousError string

	// ReviewFindings is the prior review-round's findings text (ADR-029),
	// injected so the generator can address completeness gaps. Empty when no
	// prior review applies.
	ReviewFindings string
}
