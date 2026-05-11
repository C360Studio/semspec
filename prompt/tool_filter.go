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

		RoleValidator: {
			AllowExact: []string{"bash", "submit_work"},
		},
		RoleReviewer: {
			AllowExact: []string{"bash", "submit_work", "graph_search", "graph_query", "graph_summary", "scratchpad"},
		},

		// --- Planning roles ---
		//
		// Generator roles get scratchpad (single-shot pre-commit reasoning
		// runway) but NOT write_todos — single-dispatch work has no
		// cross-iteration list to maintain. See
		// docs/structured-output-levels.md role map.

		RolePlanner: {
			AllowExact: []string{"bash", "graph_search", "graph_query", "graph_summary", "web_search", "http_request", "ask_question", "submit_work", "scratchpad"},
		},
		RoleRequirementGenerator: {
			AllowExact: []string{"bash", "graph_search", "graph_query", "submit_work", "scratchpad"},
		},
		RoleScenarioGenerator: {
			AllowExact: []string{"bash", "submit_work", "scratchpad"},
		},
		RoleTaskGenerator: {
			AllowExact: []string{"bash", "graph_search", "graph_query", "scratchpad"},
		},
		RolePlanReviewer: {
			AllowExact: []string{"bash", "submit_work", "graph_search", "graph_query", "scratchpad"},
		},
		RoleTaskReviewer: {
			AllowExact: []string{"bash", "scratchpad"},
		},

		// --- Scenario-level review ---

		RoleScenarioReviewer: {
			AllowExact: []string{"bash", "submit_work", "graph_search", "graph_query", "scratchpad"},
		},

		// --- Plan-level QA reviewer (release-readiness verdict, read-only) ---

		RolePlanQAReviewer: {
			AllowExact: []string{"bash", "submit_work", "graph_search", "graph_query", "scratchpad"},
		},

		// Developer: TDD agent — bash for code, graph + web for discovery, http_request for local API testing.
		// write_todos: per-iteration TDD memory (ADR-036) — natural fit
		// for multi-cycle dispatches where context compaction may evict
		// the plan; persona instructs use across TDD iterations.
		// scratchpad: pre-cycle "think through the implementation"
		// runway, also useful between cycles when the prior reviewer
		// feedback needs digesting.
		RoleDeveloper: {
			AllowExact: []string{"bash", "submit_work", "graph_search", "graph_query", "graph_summary", "web_search", "http_request", "write_todos", "scratchpad"},
		},

		// Architect: technology choices, component boundaries, data flow.
		// Read-only exploration via bash + graph; web/http for tech docs.
		// No decompose_task / review_scenario — those are other-role
		// terminals that confused take 11's developer.
		// write_todos + scratchpad: multi-step technology exploration
		// before commit.
		RoleArchitect: {
			AllowExact: []string{"bash", "submit_work", "graph_search", "graph_query", "graph_summary", "web_search", "http_request", "write_todos", "scratchpad"},
		},

		// Lesson decomposer: reads trajectory + reviewer verdict, emits one
		// audited lesson via submit_work. Bash for cited-evidence file
		// reads, graph_query for trajectory pull. ADR-033 Phase 2b.
		// write_todos + scratchpad: track per-iteration synthesis steps
		// when the trajectory analysis spans multiple cycles.
		RoleLessonDecomposer: {
			AllowExact: []string{"bash", "submit_work", "graph_search", "graph_query", "write_todos", "scratchpad"},
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
