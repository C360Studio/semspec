package prompt

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// ToolGuidance provides one-line guidance for a specific tool.
type ToolGuidance struct {
	// Name is the tool name (e.g., "file_read").
	Name string

	// Guidance is a one-line description of when/how to use this tool.
	Guidance string

	// Roles limits this guidance to specific roles. Empty means all roles.
	Roles []Role

	// Order controls display order in the tool guidance section. Lower values appear first.
	Order int
}

// DefaultToolGuidance returns guidance entries for all semspec tools.
func DefaultToolGuidance() []ToolGuidance {
	return []ToolGuidance{
		// Core tools
		{Name: "bash", Order: 0, Guidance: "Run any shell command — file ops (cat, tee, ls), git, builds, tests, installs. Use this for everything."},
		{Name: "submit_work", Order: 1, Guidance: "Submit completed work. MUST be called when done — task fails without it. Include a summary of what you accomplished."},
		{Name: "ask_question", Order: 2, Guidance: "Ask when blocked and cannot proceed. Default to reasonable assumptions — only ask when truly ambiguous."},

		// Graph tools — summary first so agents know what to query
		{Name: "graph_summary", Order: 10, Guidance: "Knowledge graph overview. Call ONCE first to see what entity types and domains are indexed before deciding to search."},
		{Name: "graph_search", Order: 11, Guidance: "Search the knowledge graph using natural language queries (e.g. \"health endpoint handler\"). Try FIRST for project lookups — the graph has indexed code, docs, and config. If empty or irrelevant, FALL BACK to web_search — do NOT retry the same query rephrased."},
		{Name: "graph_query", Order: 12, Guidance: "GraphQL for specific lookups: semanticSearch(query, limit) for NLQ, entitiesByPredicate(predicate, value) for exact match, entitiesByPrefix(prefix) for ID patterns, entity(id) for single entity."},

		// Web tools
		{Name: "web_search", Order: 20, Guidance: "Search the web for reference materials, external APIs, or libraries. Use AFTER graph_search if graph doesn't have what you need. Always use this BEFORE http_request to find the right URL — never guess URLs."},
		{Name: "http_request", Order: 21, Guidance: "Fetch a URL or test a local API endpoint. For web research: use web_search FIRST to find URLs — NEVER guess or fabricate URLs. For local API testing: use with localhost/sandbox URLs you built yourself."},

		// Agentic tools
		{Name: "decompose_task", Order: 30, Guidance: "Break a task into a DAG of subtasks for parallel execution.", Roles: []Role{RoleDeveloper}},
		{Name: "spawn_agent", Order: 31, Guidance: "Spawn a child agent for independent subtask execution.", Roles: []Role{RoleDeveloper}},
		{Name: "review_scenario", Order: 32, Guidance: "Submit scenario review verdict with structured findings.", Roles: []Role{RoleScenarioReviewer}},
	}
}

// ToolGuidanceFragment returns a Fragment at CategoryToolGuidance that dynamically
// builds tool guidance from the context's AvailableTools list.
func ToolGuidanceFragment(guidance []ToolGuidance) *Fragment {
	return &Fragment{
		ID:       "core.tool-guidance",
		Category: CategoryToolGuidance,
		Priority: 0,
		Condition: func(ctx *AssemblyContext) bool {
			return len(ctx.AvailableTools) > 1
		},
		ContentFunc: func(ctx *AssemblyContext) string {
			return buildToolGuidanceContent(ctx, guidance)
		},
	}
}

// buildToolGuidanceContent generates the tool guidance section.
func buildToolGuidanceContent(ctx *AssemblyContext, guidance []ToolGuidance) string {
	var sb strings.Builder
	sb.WriteString("Available tools and when to use them:\n\n")

	// Filter to tools available for this role.
	filtered := make([]ToolGuidance, 0, len(guidance))
	for _, g := range guidance {
		if !ctx.HasTool(g.Name) {
			continue
		}
		if len(g.Roles) > 0 && !slices.Contains(g.Roles, ctx.Role) {
			continue
		}
		filtered = append(filtered, g)
	}

	// Sort by Order for consistent, intentional display order.
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Order < filtered[j].Order
	})

	for _, g := range filtered {
		sb.WriteString(fmt.Sprintf("- **%s**: %s\n", g.Name, g.Guidance))
	}

	return sb.String()
}
