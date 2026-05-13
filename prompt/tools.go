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
		{Name: "submit_work", Order: 1, Guidance: "Submit completed work. Call ONLY after finishing your task — not on your first turn. See output format for required fields."},
		{Name: "ask_question", Order: 2, Guidance: "Ask when blocked and cannot proceed. Default to reasonable assumptions — only ask when truly ambiguous."},

		// Internal reasoning tools — these are YOUR private memory. They
		// write to the trajectory and are visible to your next iteration,
		// to the recovery agent if you wedge, and to a human reviewing
		// your work later.
		//
		// Guidance uses Claude Code's TodoWrite scenario-list shape (event
		// triggers, plus explicit "When NOT") instead of conditional
		// "REQUIRED for X" language. Take-17 + take-18 (2026-05-12/13) ran
		// sonnet developers under increasingly prescriptive MUST language
		// — adoption stayed at zero across 18 trajectories on take-18.
		// Conditionals like "for any task with more than one step" or
		// "when the task involves decomposition" let the model self-classify
		// out: it decides the task isn't "really multi-step", and the rule
		// doesn't apply. Event triggers (BEFORE first bash, AFTER reviewer
		// rejection) are state transitions the model can't argue past.

		// write_todos: working task list ACROSS iterations. Context
		// compaction can evict your plan; write_todos survives it.
		{Name: "write_todos", Order: 3, Guidance: `Use write_todos proactively in these scenarios:
- BEFORE your first bash command on a new task — capture the plan you read out of the task brief
- AFTER prereq context tells you a previous attempt failed — capture what to do differently this time
- AFTER a reviewer rejection — turn each finding in the feedback into a todo
- WHEN you finish a step — mark it completed in the SAME call you do the work, never batch at the end

When NOT to use write_todos:
- A single deterministic bash command with no preconditions and no follow-up
- Re-running the exact command from the last iteration after a transient failure

Submit the entire current list each call (full replacement). The list is your private memory between iterations; without it, context compaction can drop your plan and you repeat work or lose track of what is left.`},

		// scratchpad: free-form reasoning channel for a SINGLE dispatch.
		// Strict tool-args on submit_work are easier to produce correctly
		// AFTER you have laid out your thinking here.
		{Name: "scratchpad", Order: 4, Guidance: `Use scratchpad proactively in these scenarios:
- BEFORE submit_work when the work involved multi-file changes or weighing approaches — write the prose you used to decide
- BEFORE writing a non-trivial new file — lay out the structure as plain prose first
- AFTER reading reviewer feedback — write your understanding of what they want before changing code

When NOT to use scratchpad:
- A submit_work call reporting a single deterministic fix
- Purely informational tasks with no decision points

Text is unconstrained — plain prose explaining your approach, things you considered, edge cases. Strict commits go more cleanly after you have laid out your thinking; submit_work calls produced without this step routinely have missing files, wrong scope, or hallucinated paths.`},

		// Graph tools removed from agent palettes 2026-05-12 — see
		// prompt/tool_filter.go header comment. Tools remain registered
		// in tools/workflow/register.go but no role surfaces them, so
		// guidance entries are not needed. Re-add per-role + per-tool
		// guidance IF a future role demonstrably needs graph access.

		// Web tools
		{Name: "web_search", Order: 20, Guidance: "Search the web for reference materials, external APIs, or libraries. Always use this BEFORE http_request to find the right URL — never guess URLs."},
		{Name: "http_request", Order: 21, Guidance: "Fetch a URL or test a local API endpoint. For web research: use web_search FIRST to find URLs — NEVER guess or fabricate URLs. For local API testing: use with localhost/sandbox URLs you built yourself."},

		// Agentic tools
		// decompose_task is registered with RoleTaskGenerator semantically
		// (the requirement-executor decomposer is what calls it). The
		// previous RoleDeveloper tag was a take-11 footgun that put it
		// in the developer's prompt-side guidance — model picked it
		// instead of submit_work. Keep the role tag aligned with the
		// dispatcher.
		{Name: "decompose_task", Order: 30, Guidance: "Break a task into a DAG of subtasks for parallel execution.", Roles: []Role{RoleTaskGenerator}},
		// review_scenario was the terminal for the legacy scenario-
		// reviewer dispatch that was deleted; the tool itself was never
		// re-registered. Listing it in tool guidance pollutes the
		// reviewer's prompt with a non-existent tool name. Removed
		// 2026-05-08 take-14 follow-up.
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
// For small models (MaxTokens < SmallModelTokenThreshold), only tool names are
// listed — the full descriptions are already in the OpenAI tools array.
func buildToolGuidanceContent(ctx *AssemblyContext, guidance []ToolGuidance) string {
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

	var sb strings.Builder

	// Small models: compact list — full descriptions are in the tools array.
	if ctx.MaxTokens > 0 && ctx.MaxTokens < SmallModelTokenThreshold {
		sb.WriteString("Tools: ")
		names := make([]string, 0, len(filtered))
		hasSubmitWork := false
		for _, g := range filtered {
			names = append(names, g.Name)
			if g.Name == "submit_work" {
				hasSubmitWork = true
			}
		}
		sb.WriteString(strings.Join(names, ", "))
		sb.WriteString("\n")
		if hasSubmitWork {
			sb.WriteString("\nIMPORTANT: Call the submit_work function to submit your work. Pass your output as the function arguments. Do NOT write JSON as text.\n")
		}
		return sb.String()
	}

	// Large models: full guidance.
	sb.WriteString("Available tools and when to use them:\n\n")
	for _, g := range filtered {
		sb.WriteString(fmt.Sprintf("- **%s**: %s\n", g.Name, g.Guidance))
	}

	return sb.String()
}
