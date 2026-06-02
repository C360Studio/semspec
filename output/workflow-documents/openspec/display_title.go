package openspec

import "github.com/c360studio/semspec/workflow"

// maxDisplayPlanTitleChars caps the H1 title length across every
// OpenSpec document. Mirrors workflowdocuments.maxDisplayTitleChars
// in the parent package — see that constant for the rationale (smoke 6
// produced ~600-char H1s when plan.Title was filled from a verbose
// API description).
const maxDisplayPlanTitleChars = 100

// displayPlanTitle returns the human-presentable plan title for use in
// OpenSpec document H1 headings. When plan.Title is empty or longer
// than maxDisplayPlanTitleChars, the slug is used as fallback. plan.Goal
// stays intact in the body — only the H1 changes.
//
// Kept package-local rather than imported from workflow-documents
// (openspec is a sibling subpackage; the helper is small and
// well-contained, so duplication is cheaper than the import path).
func displayPlanTitle(plan *workflow.Plan) string {
	if plan == nil {
		return ""
	}
	t := plan.Title
	if t == "" || len([]rune(t)) > maxDisplayPlanTitleChars {
		return plan.Slug
	}
	return t
}
