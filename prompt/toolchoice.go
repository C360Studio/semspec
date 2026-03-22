package prompt

// ToolChoice represents the tool selection mode for LLM requests.
// This mirrors semstreams' agentic.ToolChoice but is defined here
// to avoid a hard dependency on the semstreams version that adds it.
// When semstreams is upgraded to include agentic.ToolChoice, this
// type can be replaced with a type alias.
type ToolChoice struct {
	// Mode is "auto", "required", "none", or "function".
	Mode string `json:"mode"`

	// FunctionName is required when Mode is "function".
	FunctionName string `json:"function_name,omitempty"`
}

// ResolveToolChoice determines the appropriate ToolChoice for a given role and tool set.
//
// Logic:
//   - No tools → nil (auto, model decides)
//   - Single tool → force that specific function
//   - Developer with tools → required (must use a tool each iteration)
//   - Reviewers → nil (they produce JSON, no tools needed)
//   - Planners with tools → nil (auto — tools optional for context gathering)
func ResolveToolChoice(role Role, toolNames []string) *ToolChoice {
	if len(toolNames) == 0 {
		return nil
	}

	// Check role first: reviewers and planners never force tool use.
	switch role {
	case RoleDeveloper:
		// Developer agents MUST call a tool each iteration (bash, submit_work, etc)
		if len(toolNames) == 1 {
			return &ToolChoice{
				Mode:         "function",
				FunctionName: toolNames[0],
			}
		}
		return &ToolChoice{Mode: "required"}

	case RoleReviewer, RolePlanReviewer, RoleTaskReviewer, RoleScenarioReviewer, RolePlanRollupReviewer:
		// Reviewers produce structured JSON output, no tool calls needed
		return nil

	case RolePlanner, RolePlanCoordinator:
		// Planners may optionally use tools for context gathering
		return nil

	default:
		// Single tool for any other role: force it
		if len(toolNames) == 1 {
			return &ToolChoice{
				Mode:         "function",
				FunctionName: toolNames[0],
			}
		}
		return nil
	}
}
