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
			AllowExact: []string{"bash", "submit_work"},
		},
		RoleTester: {
			AllowExact: []string{"bash", "submit_work"},
		},
		RoleValidator: {
			AllowExact: []string{"bash", "submit_work"},
		},
		RoleReviewer: {
			AllowExact: []string{"bash", "submit_work", "graph_search", "graph_query"},
		},

		// --- Planning roles ---

		RolePlanner: {
			AllowExact: []string{"bash", "graph_search", "graph_query", "graph_summary"},
		},
		RoleRequirementGenerator: {
			AllowExact: []string{"bash", "graph_search", "graph_query"},
		},
		RoleScenarioGenerator: {
			AllowExact: []string{"bash"},
		},
		RoleTaskGenerator: {
			AllowExact: []string{"bash", "graph_search", "graph_query"},
		},
		RolePlanReviewer: {
			AllowExact: []string{"bash"},
		},
		RoleTaskReviewer: {
			AllowExact: []string{"bash"},
		},

		// --- Coordination roles ---

		RoleCoordinator: {
			AllowExact: []string{"spawn_agent"},
		},
		RolePlanCoordinator: {
			AllowExact: []string{"bash", "graph_search", "graph_query", "graph_summary", "spawn_planner", "get_planner_result", "save_plan"},
		},

		// --- Scenario-level review ---

		RoleScenarioReviewer: {
			AllowExact: []string{"bash", "submit_work", "graph_search", "graph_query"},
		},

		// --- Plan-level rollup reviewer (read-only) ---

		RolePlanRollupReviewer: {
			AllowExact: []string{"bash", "submit_work", "graph_search", "graph_query"},
		},

		// --- Deprecated: developer gets bash + agentic tools for backward compat ---

		RoleDeveloper: {
			AllowExact: []string{"bash", "submit_work", "decompose_task", "spawn_agent"},
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
