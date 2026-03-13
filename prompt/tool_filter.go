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
		RoleDeveloper: {
			AllowPrefixes: []string{"file_", "git_", "workflow_"},
			AllowExact:    []string{"decompose_task", "spawn_agent", "create_tool", "query_agent_tree"},
		},
		RoleReviewer: {
			AllowPrefixes: []string{"workflow_"},
			AllowExact:    []string{"file_read", "file_list", "git_diff"},
		},
		RolePlanner: {
			AllowPrefixes: []string{"workflow_"},
			AllowExact:    []string{"file_read", "file_list", "git_status"},
		},
		RolePlanReviewer: {
			AllowPrefixes: []string{"workflow_"},
			AllowExact:    []string{"file_read", "file_list"},
		},
		RoleTaskReviewer: {
			AllowPrefixes: []string{"workflow_"},
			AllowExact:    []string{"file_read", "file_list"},
		},
		RolePlanCoordinator: {
			AllowPrefixes: []string{"workflow_", "file_"},
			AllowExact:    []string{"spawn_planner", "get_planner_result", "save_plan"},
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
