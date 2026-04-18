package prompt

import "github.com/c360studio/semstreams/agentic"

// ResolveToolChoice determines the appropriate agentic.ToolChoice for a given
// role and tool set. The result is set directly on agentic.TaskMessage.ToolChoice.
//
// Logic:
//   - No tools → nil (auto, model decides)
//   - Execution roles (builder, tester, developer, validator) → required (must use a tool)
//   - Single tool for execution roles → force that specific function
//   - Reviewers → nil (they produce JSON, no tools needed)
//   - Planners with tools → nil (auto — tools optional for context gathering)
func ResolveToolChoice(role Role, toolNames []string) *agentic.ToolChoice {
	if len(toolNames) == 0 {
		return nil
	}

	// Check role first.
	switch role {
	case RoleDeveloper, RoleValidator:
		// Execution agents MUST call a tool each iteration (bash, submit_work, etc)
		if len(toolNames) == 1 {
			return &agentic.ToolChoice{
				Mode:         "function",
				FunctionName: toolNames[0],
			}
		}
		return &agentic.ToolChoice{Mode: "required"}

	case RoleReviewer, RolePlanReviewer, RoleTaskReviewer, RoleScenarioReviewer, RolePlanQAReviewer:
		// Reviewers submit verdict via submit_work deliverable — must call tools
		return &agentic.ToolChoice{Mode: "required"}

	case RolePlanner, RolePlanCoordinator, RoleRequirementGenerator, RoleScenarioGenerator, RoleArchitect:
		// Generators must call tools — use bash/graph for context, submit_work for deliverable
		return &agentic.ToolChoice{Mode: "required"}

	default:
		// Single tool for any other role: force it
		if len(toolNames) == 1 {
			return &agentic.ToolChoice{
				Mode:         "function",
				FunctionName: toolNames[0],
			}
		}
		return nil
	}
}
