package workflowdocuments

import "github.com/c360studio/semspec/workflow"

// maxDisplayTitleChars caps the H1 title length across every rendered
// document. The original plan.Title can be the full user prompt (the
// HTTP API falls back title=description when only description is
// provided — see plan-manager/http.go:633-643). That makes the H1 a
// multi-paragraph blob in every .md artifact. Smoke 6 (2026-06-02)
// produced ~600-char H1s on plan.md / architecture.md / requirements.md /
// scenarios.md / proposal.md / design.md / tasks.md.
//
// 100 chars is the conservative compromise: a real human-authored
// title fits; a prompt-blob does not. The slug is used as fallback
// because it's deterministic, unique, and already in the path.
const maxDisplayTitleChars = 100

// displayTitle returns the human-presentable plan title for use in
// document H1 headings. When plan.Title is empty or longer than
// maxDisplayTitleChars (typically because the API filled it from a
// verbose description), the slug is used instead. plan.Goal stays
// intact downstream — only the H1 changes.
func displayTitle(plan *workflow.Plan) string {
	if plan == nil {
		return ""
	}
	t := plan.Title
	if t == "" || len([]rune(t)) > maxDisplayTitleChars {
		return plan.Slug
	}
	return t
}
