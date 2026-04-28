package prompt

// PlannerPromptContext carries everything the planner user-prompt fragment
// needs to render. Two distinct paths: fresh plan creation (Title only) and
// revision-after-rejection (IsRevision + PreviousPlanJSON + RevisionPrompt).
// PreviousError applies to both paths as a retry hint.
type PlannerPromptContext struct {
	// Title is the plan title (required for fresh creation; ignored on revision).
	Title string

	// IsRevision flips the prompt into "address reviewer findings" mode.
	IsRevision bool

	// PreviousPlanJSON is the rejected plan output the LLM should update.
	// Only consulted when IsRevision is true.
	PreviousPlanJSON string

	// RevisionPrompt is the reviewer-feedback-driven revision instruction
	// (typically containing the structured findings list). Only consulted
	// when IsRevision is true.
	RevisionPrompt string

	// PreviousError is the parser/transport error from the prior dispatch,
	// when retrying. Empty on first attempt.
	PreviousError string
}
