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

	// ProjectFileTree is a ground-truth snapshot of the project's tracked
	// files (typically `git ls-files | head -50`). The persona repeatedly
	// instructs the model to set files_owned from the plan's scope.include
	// and warns against inventing fake file splits ("Inventing fake file
	// splits to make the partition look clean produces broken work at
	// execution time"). Without ground truth, weak models still hallucinate
	// path shapes that look idiomatic (api/handlers/*.go on a project that
	// has no api/ directory). Same fix shape as plan-reviewer's take-20
	// fix. Empty for greenfield or when sandbox is unavailable; the
	// renderer silently omits the section.
	ProjectFileTree string

	// Capabilities is the ADR-040 Move 2 input: the analyst sub-phase's
	// classified capability list. When non-empty, the renderer instructs
	// John to produce ONE Requirement per capability with capability_name
	// set, and the parser will populate Requirement.CapabilityName.
	// Empty for plans that ran the legacy single-pass planner (no
	// analyst sub-phase) — back-compat preserved.
	Capabilities []CapabilityCard
}

// CapabilityCard is the minimal capability projection the
// requirement-generator user prompt renders. Mirrors the workflow.Capability
// fields the LLM needs to produce one requirement per capability.
type CapabilityCard struct {
	Name        string   `json:"name"`
	Lifecycle   string   `json:"lifecycle"`
	Description string   `json:"description"`
	DependsOn   []string `json:"depends_on,omitempty"`
}
