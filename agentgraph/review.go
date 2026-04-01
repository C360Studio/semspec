package agentgraph

import "github.com/c360studio/semspec/workflow"

// ErrorTrend carries a resolved error category with its occurrence count.
// Used by the lesson-based prompt injection system to surface recurring error patterns.
type ErrorTrend struct {
	// Category is the resolved category definition.
	Category *workflow.ErrorCategoryDef

	// Count is the number of times this category has been observed for the role.
	Count int
}
