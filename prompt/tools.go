package prompt

import (
	"fmt"
	"slices"
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
}

// DefaultToolGuidance returns guidance entries for all semspec tools.
func DefaultToolGuidance() []ToolGuidance {
	return []ToolGuidance{
		// File tools
		{Name: "file_read", Guidance: "Read file contents before modifying. Use to understand existing code patterns and verify current state."},
		{Name: "file_write", Guidance: "Create or modify files. REQUIRED for any code changes — never describe code without writing it.", Roles: []Role{RoleDeveloper}},
		{Name: "file_list", Guidance: "List directory contents to discover project structure and find relevant files."},

		// Git tools
		{Name: "git_status", Guidance: "Check repository status to understand current working tree state."},
		{Name: "git_diff", Guidance: "View changes after modifications to verify correctness before committing."},
		{Name: "git_commit", Guidance: "Commit changes after implementation is complete and verified.", Roles: []Role{RoleDeveloper}},

		// Workflow tools — guide agents to use manifest
		{Name: "workflow_query_graph", Guidance: "Query the knowledge graph using GraphQL. Check the Knowledge Graph Contents section first to see available predicates. Use entitiesByPredicate(predicate: \"...\") for targeted lookups, entity(id: \"...\") for specific entities. Results capped at 100KB."},
		{Name: "workflow_read_document", Guidance: "Read plan or specification documents from the workflow."},
		{Name: "workflow_get_codebase_summary", Guidance: "Get indexed code entity counts and samples. Use as your FIRST graph query to orient before diving into specifics."},
		{Name: "workflow_traverse_relationships", Guidance: "Follow relationships from a known entity (calls, implements, imports). Max depth 3. Start from a specific entity ID, not a broad search."},

		// Web search
		{Name: "web_search", Guidance: "Search the web for external documentation, API references, or library usage. Max 10 results. Use specific queries, not broad topics."},

		// Advanced tools (reactive execution)
		{Name: "decompose_task", Guidance: "Decompose a task into a DAG of subtasks for parallel execution.", Roles: []Role{RoleDeveloper}},
		{Name: "spawn_agent", Guidance: "Spawn a child agent loop for independent subtask execution.", Roles: []Role{RoleDeveloper}},
		{Name: "create_tool", Guidance: "Create a dynamic tool from a FlowSpec definition.", Roles: []Role{RoleDeveloper}},
		{Name: "query_agent_tree", Guidance: "Inspect the agent hierarchy to understand spawned child agents.", Roles: []Role{RoleDeveloper}},
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

	for _, g := range guidance {
		if !ctx.HasTool(g.Name) {
			continue
		}
		// Role filtering
		if len(g.Roles) > 0 && !slices.Contains(g.Roles, ctx.Role) {
			continue
		}
		sb.WriteString(fmt.Sprintf("- **%s**: %s\n", g.Name, g.Guidance))
	}

	return sb.String()
}
