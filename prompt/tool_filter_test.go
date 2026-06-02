package prompt

import (
	"slices"
	"testing"
)

// TestFilterTools_DeveloperPalette pins the developer's allowed-tool set
// to its load-bearing entries. The shape of these assertions catches two
// regression classes:
//
//  1. Palette drift between the per-role allowlist (this file's input)
//     and the per-component availableToolNames() that feeds it. After
//     613ca6c we know "tool is registered + tool is in palette" is the
//     adoption seam — losing the developer's research/write_todos/scratchpad
//     entries would silently regress to the take-18 "MUST language is in
//     the prompt but the tool isn't in the wire palette" failure mode.
//  2. Researcher palette accidentally gaining `research` (recursive
//     delegation), `submit_work` (wrong terminal), or `write_todos`
//     (cross-iter persistence in a single-shot role). The guardrail
//     test in processor/researcher-manager covers the same shape;
//     duplicating the assertion here makes the lock visible at the
//     prompt layer where the per-role policy lives.
func TestFilterTools_DeveloperPalette(t *testing.T) {
	// Superset of every tool the developer might be passed by the
	// dispatcher. FilterTools narrows to what's in the role's AllowExact.
	allTools := []string{
		"bash", "submit_work", "ask_question",
		"write_todos", "scratchpad",
		"research",
		"graph_search", "graph_query", "graph_summary",
		"web_search", "http_request",
		"decompose_task",
	}

	allowed := FilterTools(allTools, RoleDeveloper)

	// Load-bearing entries — losing any of these regresses an adoption
	// story we built explicit memory + commits around.
	required := []string{
		"bash",         // primary execution surface
		"submit_work",  // terminal
		"web_search",   // upstream discovery
		"http_request", // canonical fetch
		"write_todos",  // 613ca6c wiring fix
		"scratchpad",   // 613ca6c wiring fix
	}
	for _, name := range required {
		if !slices.Contains(allowed, name) {
			t.Errorf("RoleDeveloper palette missing required tool %q (got %v)", name, allowed)
		}
	}

	// Tools filtered OUT for the developer.
	// - graph_* removed 2026-05-12 (per package header)
	// - research SHELVED 2026-05-15 — take-27 evidence showed dispatch
	//   worked but didn't fix the actual wedge shape; pivoted to upstream-
	//   strengthening. See [[research-shelved-pivot-to-upstream-
	//   strengthening-2026-05-15]]. Pin the exclusion so re-enabling is
	//   a deliberate diff, not an accidental revert.
	for _, name := range []string{"graph_search", "graph_query", "graph_summary", "research"} {
		if slices.Contains(allowed, name) {
			t.Errorf("RoleDeveloper palette unexpectedly includes %q (regressed previously-removed tool)", name)
		}
	}
}

// TestFilterTools_ResearcherPalette pins the researcher's tight palette.
// The forbidden list catches the regressions that would invite recursion,
// wrong-terminal submissions, or scope creep into developer-shaped work.
func TestFilterTools_ResearcherPalette(t *testing.T) {
	allTools := []string{
		"bash", "submit_work", "ask_question",
		"write_todos", "scratchpad",
		"research", "answer_research",
		"graph_search", "graph_query", "graph_summary",
		"web_search", "http_request",
		"decompose_task",
	}

	allowed := FilterTools(allTools, RoleResearcher)

	required := []string{"bash", "http_request", "web_search", "answer_research"}
	for _, name := range required {
		if !slices.Contains(allowed, name) {
			t.Errorf("RoleResearcher palette missing required tool %q (got %v)", name, allowed)
		}
	}

	// Critical guardrails — adding any of these would change the
	// researcher's semantic shape.
	forbidden := []string{
		"research",       // recursion guard
		"submit_work",    // wrong terminal — researcher delivers via answer_research
		"write_todos",    // single-shot focused task, no cross-iter plan
		"scratchpad",     // (intentional v1 absence — add only if R5 telemetry shows weak reasoning)
		"decompose_task", // not a generator role
		"ask_question",   // researcher answers questions, doesn't ask them
	}
	for _, name := range forbidden {
		if slices.Contains(allowed, name) {
			t.Errorf("RoleResearcher palette unexpectedly includes forbidden tool %q (would change role semantics)", name)
		}
	}
}

// TestFilterTools_UnknownRoleReturnsAll documents the fall-through
// behavior so a future role-rename doesn't silently start receiving the
// full palette.
func TestFilterTools_UnknownRoleReturnsAll(t *testing.T) {
	got := FilterTools([]string{"bash", "submit_work"}, Role("not-a-real-role"))
	if !slices.Equal(got, []string{"bash", "submit_work"}) {
		t.Errorf("unknown role should return all tools unchanged; got %v", got)
	}
}

// TestFilterTools_StoryPreparerPalette pins Sarah's tight palette so a
// future tool addition doesn't silently leak into the story-preparer
// dispatch. Added 2026-06-02 alongside the explicit DefaultToolFilters
// entry that closes the unknown-role fall-through gap for RoleStoryPreparer.
func TestFilterTools_StoryPreparerPalette(t *testing.T) {
	allTools := []string{
		"bash", "submit_work", "ask_question", "answer_question",
		"write_todos", "scratchpad", "web_search", "http_request",
		"graph_search", "graph_query", "graph_summary",
		"research", "answer_research", "spawn_agent",
	}
	allowed := FilterTools(allTools, RoleStoryPreparer)
	want := []string{"bash", "submit_work", "write_todos", "scratchpad"}
	if !slices.Equal(allowed, want) {
		t.Errorf("RoleStoryPreparer palette = %v, want %v", allowed, want)
	}
}

// TestFilterTools_AllDispatchRolesHaveExplicitEntry pins the contract
// that every role used by an actual processor dispatcher has an
// explicit entry in DefaultToolFilters — not a fall-through to "all
// tools." Drift on this list = the semteams 51-tools-leak surface.
//
// Roles intentionally excluded (used only for telemetry tagging, NOT
// dispatch):
//   - RoleQA — qa-reviewer tags its loop with this string, but the
//     actual FilterTools call uses RolePlanQAReviewer.
//   - RoleCapabilities / RoleContext — sub-role labels used inside
//     persona fragments, not standalone dispatch roles.
func TestFilterTools_AllDispatchRolesHaveExplicitEntry(t *testing.T) {
	filters := DefaultToolFilters()
	dispatchRoles := []Role{
		RolePlanner,
		RoleArchitect,
		RoleRequirementGenerator,
		RoleScenarioGenerator,
		RoleStoryPreparer,
		RoleScenarioReviewer,
		RolePlanReviewer,
		RoleTaskReviewer,
		RoleReviewer,
		RoleValidator,
		RoleDeveloper,
		RolePlanQAReviewer,
		RoleLessonDecomposer,
		RoleRecoveryAgent,
		RoleResearcher,
	}
	for _, role := range dispatchRoles {
		if _, ok := filters[role]; !ok {
			t.Errorf("dispatch role %q missing from DefaultToolFilters() — falls through to 'all tools' return and risks the 51-tool leak shape", role)
		}
	}
}
