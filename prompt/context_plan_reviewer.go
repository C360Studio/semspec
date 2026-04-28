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
}
