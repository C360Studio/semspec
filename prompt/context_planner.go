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

	// ProjectFileTree is a ground-truth snapshot of the project's tracked
	// files (typically `git ls-files | head -50`) captured at dispatch time.
	// Empty for greenfield projects or when sandbox is unavailable. Rendered
	// at the top of the user prompt so the planner can't hallucinate paths
	// like "cmd/server/main.go" when the actual root has main.go directly —
	// the failure mode caught 2026-05-03 on openrouter @easy /health where
	// the planner persisted with a hallucinated cmd/server/ structure
	// across three revision rounds despite explicit reviewer suggestions.
	ProjectFileTree string
}
