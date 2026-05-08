package prompt

// PlanReviewerPromptContext carries data for the plan-reviewer user prompt.
// Round controls which completeness criteria are appended (R1: plan only,
// R2: plan + requirements + scenarios + architecture).
type PlanReviewerPromptContext struct {
	Slug          string
	PlanContent   string
	HasStandards  bool
	Round         int
	PreviousError string

	// ProjectFileTree is a ground-truth snapshot of the project's tracked
	// files (typically `git ls-files | head -50`). The R1 criterion #3
	// (Scope validity) and the role-context's "Compare scope.include against
	// the project file tree" rule both ASK the reviewer to check paths
	// against ground truth — without this field the check is impossible and
	// weak reviewer models default to "flag it" on real files. Caught
	// 2026-05-08 take 20: llama-3.3-70b false-positived "Hallucinated paths
	// in scope.include" on main.go (a real file) two rounds running because
	// the reviewer prompt asked it to verify a tree it never received.
	// Empty for greenfield or when sandbox is unavailable; the renderer
	// silently omits the section and weakens the path-check criterion.
	ProjectFileTree string
}
