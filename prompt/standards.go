package prompt

import "github.com/c360studio/semspec/workflow"

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
