package prompt

import "github.com/c360studio/semspec/workflow"

// QAReviewerPromptContext carries data for the QA reviewer user prompt.
// The QA agent reviews the entire plan + execution artifacts for release
// readiness; the field set deliberately stays small because the legacy
// builder pulls everything off the *workflow.Plan directly.
type QAReviewerPromptContext struct {
	Plan *workflow.Plan
	// PreviousError carries the parse / structural failure from a prior
	// dispatch when this is a retry. Empty on the first attempt; non-empty
	// on parse-error retries so the reviewer is not asked to re-emit a bad
	// shape blindly.
	PreviousError string
}
