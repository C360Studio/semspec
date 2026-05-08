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

	// PreviousFindings is the formatted findings text from the prior review
	// iteration on THIS plan, when ReviewIteration > 0. Empty on the first
	// review pass. Without this the reviewer is stateless across revision
	// rounds — it re-evaluates the planner's revised plan from scratch and
	// on a non-deterministic model can re-fire the same complaint even when
	// the planner addressed it. Caught 2026-05-08 take 22: planner pass-2
	// added the implementation specifics the reviewer asked for; reviewer
	// pass-2 (with no memory of pass-1) re-rejected with the same complaint
	// shape and hit max_revisions → escalated. plan-manager already stores
	// this in plan.ReviewFormattedFindings — this wiring just surfaces it
	// to the prompt.
	PreviousFindings string

	// ReviewIteration is the count of completed review iterations on this
	// plan (matches plan.ReviewIteration). 0 on the first review pass; ≥1
	// on revision rounds. Renderer uses this to gate whether to inject the
	// previous-findings section (only fires when > 0) and to label the
	// "iteration N of MaxIterations" budget context for the reviewer.
	ReviewIteration int

	// MaxReviewIterations is the configured ceiling (matches
	// plan-manager.config.MaxReviewIterations). Surfaced to the reviewer
	// so it knows when re-rejection on the same complaint will trigger
	// escalation rather than another revision. Without this context the
	// reviewer can wedge the planner on a stochastic complaint right up
	// to the cap.
	MaxReviewIterations int

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
