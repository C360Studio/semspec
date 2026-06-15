package planreviewer

import (
	"fmt"

	"github.com/c360studio/semspec/workflow"
)

// mergeDeterministicFindings appends all machine-checkable plan review
// findings to result. Keep this path shared by preflight and post-LLM review:
// preflight saves paid reviewer calls when hard structural gates already fail,
// while the post-LLM merge remains a race/backstop if plan state changes after
// dispatch or a future caller bypasses preflight.
func mergeDeterministicFindings(plan *workflow.Plan, result *workflow.PlanReviewResult) {
	mergeCapabilityFindings(plan, result)
	mergeArchitectureFindings(plan, result)
	mergeStoryFindings(plan, result)
	mergeScenarioTagFindings(plan, result)
}

// deterministicPreflightReview returns a synthetic review result when hard
// structural rules already prove the plan needs changes. Warning/info findings
// do not short-circuit the LLM reviewer; only error-severity violations do.
func deterministicPreflightReview(plan *workflow.Plan) *workflow.PlanReviewResult {
	if plan == nil {
		return nil
	}

	result := &workflow.PlanReviewResult{
		Verdict: "approved",
		Summary: "Deterministic structural preflight passed.",
	}
	mergeDeterministicFindings(plan, result)

	errorCount := len(result.ErrorFindings())
	if errorCount == 0 {
		return nil
	}

	result.Verdict = "needs_changes"
	result.Summary = fmt.Sprintf(
		"Deterministic structural preflight found %d blocking issue(s); reviewer LLM was not dispatched.",
		errorCount,
	)
	return result
}
