package prompt

import "strings"

// ToolFilter defines which tools a role can access.
type ToolFilter struct {
	// AllowPrefixes allows tools matching any of these prefixes.
	AllowPrefixes []string

	// AllowExact allows specific tool names.
	AllowExact []string

	// DenyExact blocks specific tool names even if they match a prefix.
	DenyExact []string
}

// DefaultToolFilters returns the default tool filter for each role.
func DefaultToolFilters() map[Role]*ToolFilter {
	return map[Role]*ToolFilter{
		// --- Execution roles ---

		RoleBuilder: {
			AllowExact: []string{"file_read", "file_write", "file_list", "git_status", "git_diff", "workflow_graph_summary"},
		},
		RoleTester: {
			AllowExact: []string{"file_read", "file_write", "file_list", "exec"},
		},
		RoleValidator: {
			AllowExact: []string{"file_read", "file_list", "file_write", "exec"},
		},
		RoleReviewer: {
			AllowExact: []string{"file_read", "file_list", "git_diff", "review_scenario"},
		},

		// --- Planning roles ---

		RolePlanner: {
			AllowExact: []string{"file_read", "file_list", "git_log", "graph_query", "workflow_graph_summary"},
		},
		RoleRequirementGenerator: {
			AllowExact: []string{"file_read", "file_list", "graph_query", "workflow_graph_summary"},
		},
		RoleScenarioGenerator: {
			AllowExact: []string{"file_read", "file_list"},
		},
		RoleTaskGenerator: {
			AllowExact: []string{"file_read", "file_list", "git_log", "graph_query", "workflow_graph_summary"},
		},
		RolePlanReviewer: {
			AllowExact: []string{"file_read", "file_list"},
		},
		RoleTaskReviewer: {
			AllowExact: []string{"file_read", "file_list"},
		},

		// --- Coordination roles ---

		RoleCoordinator: {
			AllowExact: []string{"spawn_agent", "query_agent_tree"},
		},
		RolePlanCoordinator: {
			AllowExact: []string{"spawn_planner", "get_planner_result", "save_plan", "workflow_graph_summary"},
		},

		// --- Deprecated: developer gets builder tools for backward compat ---

		RoleDeveloper: {
			AllowPrefixes: []string{"file_", "git_"},
			AllowExact:    []string{"decompose_task", "spawn_agent", "create_tool", "query_agent_tree", "workflow_graph_summary"},
		},
	}
}

// FilterTools returns the subset of allTools that the given role is allowed to use.
func FilterTools(allTools []string, role Role) []string {
	filters := DefaultToolFilters()
	filter, ok := filters[role]
	if !ok {
		// Unknown roles get all tools
		return allTools
	}

	var allowed []string
	for _, tool := range allTools {
		if isToolAllowed(tool, filter) {
			allowed = append(allowed, tool)
		}
	}
	return allowed
}

// isToolAllowed checks if a tool name passes the filter.
func isToolAllowed(tool string, filter *ToolFilter) bool {
	// Check deny list first
	for _, denied := range filter.DenyExact {
		if tool == denied {
			return false
		}
	}

	// Check exact allow
	for _, exact := range filter.AllowExact {
		if tool == exact {
			return true
		}
	}

	// Check prefix allow
	for _, prefix := range filter.AllowPrefixes {
		if strings.HasPrefix(tool, prefix) {
			return true
		}
	}

	return false
}
