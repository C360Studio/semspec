package prompt

import (
	"os"

	"github.com/c360studio/semspec/workflow"
)

// StandardsContext carries role-filtered project standards for prompt injection.
type StandardsContext struct {
	// Items are the standards applicable to the current role.
	Items []StandardsItem
}

// StandardsItem is a single standard for prompt rendering.
type StandardsItem struct {
	// ID is the unique identifier (e.g., "test-coverage").
	ID string
	// Text is the standard statement.
	Text string
	// Severity is "error", "warning", or "info".
	Severity string
	// Category groups related standards (e.g., "testing").
	Category string
}

// NewStandardsContext converts role-filtered workflow standards into a prompt
// StandardsContext. Returns nil when items is empty so the fragment condition
// gates rendering automatically.
func NewStandardsContext(items []workflow.Standard) *StandardsContext {
	if len(items) == 0 {
		return nil
	}
	sc := &StandardsContext{Items: make([]StandardsItem, len(items))}
	for i, s := range items {
		sc.Items[i] = StandardsItem{
			ID:       s.ID,
			Text:     s.Text,
			Severity: string(s.Severity),
			Category: s.Category,
		}
	}
	return sc
}

// LoadStandardsForRoleFromDisk reads .semspec/standards.json from
// SEMSPEC_REPO_PATH (or the current working directory as fallback) and
// returns a role-filtered StandardsContext. Returns nil when the file is
// missing OR contains no rules tagged for the requested role — both cases
// the shared.standards fragment treats as "no standards section to render."
//
// Centralizing the load + filter pattern lets every component dispatch site
// stay one line ("Standards: prompt.LoadStandardsForRoleFromDisk(role)"),
// preventing the per-component duplication that pushed dispatch functions
// past the function-length lint threshold.
func LoadStandardsForRoleFromDisk(role Role) *StandardsContext {
	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		repoRoot, _ = os.Getwd()
	}
	stds := workflow.LoadStandardsFromDisk(repoRoot)
	if stds == nil {
		return nil
	}
	return NewStandardsContext(stds.ForRole(string(role)))
}
