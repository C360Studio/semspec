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
			AllowExact: []string{"bash", "submit_work", "scratchpad"},
		},

		// --- Planning roles ---
		//
		// Generator roles get scratchpad (single-shot pre-commit reasoning
		// runway) but NOT write_todos — single-dispatch work has no
		// cross-iteration list to maintain. See
		// docs/structured-output-levels.md role map.
		//
		// Graph tools removed 2026-05-12: across 4+ tracked real-LLM @hard
		// runs (take 10, take 16, hybrid 2026-05-04, hybrid 2026-05-06)
		// agents called graph_* zero or near-zero times. On the rare call,
		// empty results or EOF errors caused agents to bail to bash+curl.
		// ADR-036 captured the operational gap. Keeping the tool surface
		// exposed adds prompt bloat + tool-selection ambiguity for no
		// measurable benefit. Graph stays as harness substrate (context
		// pre-injection via assembly fragments); agents do not query it.
		// Re-add per-role IF a paid run shows graph would have moved a
		// decision forward.

		RolePlanner: {
			AllowExact: []string{"bash", "web_search", "http_request", "ask_question", "submit_work", "scratchpad"},
		},
		RoleRequirementGenerator: {
			AllowExact: []string{"bash", "submit_work", "scratchpad"},
		},
		RoleScenarioGenerator: {
			AllowExact: []string{"bash", "submit_work", "scratchpad"},
		},
		// Task decomposer (req-executor's decompose dispatch) — bounded
		// palette because the decomposer's job is to commit a DAG, not to
		// research. decompose_task is the terminal tool; scratchpad +
		// write_todos give it private reasoning space (broken-out from
		// the strict tool-args). Deliberately NO bash / graph_search /
		// graph_query / submit_work: the user message carries all the
		// requirement + scope + scenario data the decomposer needs.
		RoleTaskGenerator: {
			AllowExact: []string{"decompose_task", "scratchpad", "write_todos"},
		},
		// Recovery agent (manager-role wedge diagnosis) — closed action
		// set is enforced at parse time; the palette here is "the tools
		// you'll actually call." submit_work is the terminal commit;
		// scratchpad is the reasoning channel (auditable in trajectory).
		// Deliberately NO bash / graph / web: the diagnosis is evidence-
		// reading + reasoning, not research.
		RoleRecoveryAgent: {
			AllowExact: []string{"submit_work", "scratchpad"},
		},
		RolePlanReviewer: {
			AllowExact: []string{"bash", "submit_work", "scratchpad"},
		},
		RoleTaskReviewer: {
			AllowExact: []string{"bash", "scratchpad"},
		},

		// --- Scenario-level review ---

		RoleScenarioReviewer: {
			AllowExact: []string{"bash", "submit_work", "scratchpad"},
		},

		// --- Plan-level QA reviewer (release-readiness verdict, read-only) ---

		RolePlanQAReviewer: {
			AllowExact: []string{"bash", "submit_work", "scratchpad"},
		},

		// Developer: TDD agent — bash for code, web for external reference,
		// http_request for local API testing.
		// write_todos: per-iteration TDD memory (ADR-036) — natural fit
		// for multi-cycle dispatches where context compaction may evict
		// the plan; persona instructs use across TDD iterations.
		// scratchpad: pre-cycle "think through the implementation"
		// runway, also useful between cycles when the prior reviewer
		// feedback needs digesting.
		RoleDeveloper: {
			AllowExact: []string{"bash", "submit_work", "web_search", "http_request", "write_todos", "scratchpad"},
		},

		// Architect: technology choices, component boundaries, data flow.
		// Read-only exploration via bash; web/http for tech docs.
		// No decompose_task / review_scenario — those are other-role
		// terminals that confused take 11's developer.
		// write_todos + scratchpad: multi-step technology exploration
		// before commit.
		RoleArchitect: {
			AllowExact: []string{"bash", "submit_work", "web_search", "http_request", "write_todos", "scratchpad"},
		},

		// Lesson decomposer: reads trajectory + reviewer verdict, emits one
		// audited lesson via submit_work. Bash for cited-evidence file
		// reads. Trajectory pull goes through NATS request
		// agentic.query.trajectory via internal/trajectory.LogSummary —
		// not the graph_query tool. ADR-033 Phase 2b.
		// write_todos + scratchpad: track per-iteration synthesis steps
		// when the trajectory analysis spans multiple cycles.
		RoleLessonDecomposer: {
			AllowExact: []string{"bash", "submit_work", "write_todos", "scratchpad"},
		},

		// Researcher: single-shot upstream-API-surface investigation for a
		// developer's research() call. Reads via bash (subject to the
		// existing worktree-leak governance rule), http_request, and
		// web_search; answer_research is the terminal. Deliberately
		// minimal: NO submit_work (researcher delivers via answer_research,
		// not a developer-shaped deliverable), NO research (no recursive
		// delegation — bounds the delegation graph to one-deep), NO
		// write_todos (researcher's task is narrow + bounded by its iter
		// budget, no cross-iteration plan to persist), NO scratchpad in
		// v1 (start tight; if R5 telemetry shows researcher reasoning is
		// weak we add scratchpad in R5+1).
		RoleResearcher: {
			AllowExact: []string{"bash", "http_request", "web_search", "answer_research"},
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
