// binding_context.go implements issue #90: surface ADR-041 mechanical
// binding requirements (tier tag, harness profile string, env var names,
// required assertions) in the dev's task prompt so the dev can satisfy
// them on the first cycle rather than discovering them via reviewer
// feedback across multiple cycles.
//
// Smoke 9 (2026-06-02 hybrid-gpt5 mavlink-hard) showed the dev was
// substantively responsive to LLM reviewer feedback but burned cycles
// re-discovering the same mechanical bindings each pass. Front-loading
// the data Sarah already has access to (via scenarios denormalized in
// issue #89) closes the gap.
//
// The binding context block is appended to TaskNode.Prompt at DAG
// synthesis time. When scenarios carry no integration-tier binding
// data the helper returns "" and the prompt stays unchanged — the
// addition is purely additive for @unit / @e2e-only stories.

package requirementexecutor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/c360studio/semspec/workflow"
)

// maxAssertionsPerScenario caps how many RequiredAssertions render in a
// single scenario's bullet block. Catalogs today carry ~3-5 assertions per
// profile so the cap is generous; a future profile bloating past this
// gets truncated with an explicit marker rather than blowing prompt token
// budget. Per go-reviewer feedback on PR #90.
const maxAssertionsPerScenario = 10

// buildBindingContextBlock returns a formatted Markdown block listing
// per-scenario integration-tier authoring requirements (tier tag,
// harness profile string, env var names, required assertions). Returns
// "" when no scenario in the slice carries binding-relevant data.
//
// Scenarios without HarnessProfileIDs and without a non-@unit tier tag
// are skipped — they're @unit work where the dev doesn't need this
// guidance. Only @integration / @smoke / @e2e scenarios are surfaced,
// since those are the ones where structural-validator + the reviewer
// enforce the mechanical bindings.
func buildBindingContextBlock(scenarios []workflow.Scenario) string {
	relevant := filterIntegrationTierScenarios(scenarios)
	if len(relevant) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Integration Test Context\n\n")
	b.WriteString("The scenarios below target integration-tier evidence beyond pure unit scope. ")
	b.WriteString("For MVP, this block is authoring and review context: write tests so they can ")
	b.WriteString("participate in the project's own integration-test protocol, usually by ")
	b.WriteString("declaring the tier, reading environment-provided endpoints, and asserting ")
	b.WriteString("the catalog's required behavior. Murat records whether runtime proof is ")
	b.WriteString("present, missing, or deferred; full semspec-managed harness routing is post-MVP.\n\n")

	for _, sc := range relevant {
		b.WriteString(formatScenarioBinding(sc))
	}

	return b.String()
}

// filterIntegrationTierScenarios returns scenarios that carry at least
// one non-@unit tier tag OR at least one harness profile binding.
// Stable input order is preserved so the prompt is deterministic given
// the same scenario set.
func filterIntegrationTierScenarios(scenarios []workflow.Scenario) []workflow.Scenario {
	out := make([]workflow.Scenario, 0, len(scenarios))
	for _, sc := range scenarios {
		if hasIntegrationTierSignal(sc) {
			out = append(out, sc)
		}
	}
	return out
}

// hasIntegrationTierSignal returns true when a scenario has either an
// @integration / @smoke / @e2e tier tag OR a non-empty HarnessProfileIDs.
// @unit-only scenarios with no harness binding return false — the dev
// doesn't need the binding context for those.
func hasIntegrationTierSignal(sc workflow.Scenario) bool {
	if len(sc.HarnessProfileIDs) > 0 {
		return true
	}
	for _, tag := range sc.Tags {
		if tag == "@integration" || tag == "@smoke" || tag == "@e2e" {
			return true
		}
	}
	return false
}

// formatScenarioBinding renders one scenario's binding requirements as
// a Markdown bullet block.
func formatScenarioBinding(sc workflow.Scenario) string {
	var b strings.Builder

	// Scenario header with tags.
	tagPart := ""
	if len(sc.Tags) > 0 {
		tagPart = " " + strings.Join(sc.Tags, " ")
	}
	fmt.Fprintf(&b, "- **%s**%s\n", sc.ID, tagPart)

	// Tier-tag directive — frame language-agnostic per the
	// feedback_no_colons_or_dots_in_bdd_tags convention. JUnit5 uses
	// @Tag("integration"); pytest uses pytest.mark.integration. Go is
	// omitted on purpose: //go:build integration is a file-level build
	// constraint, not a per-test tag, and would exclude every test in
	// the file. Go devs apply per-test conventions (subtest naming,
	// test helper gating) which they'll pick up from the JUnit5/pytest
	// patterns. Per go-reviewer feedback on PR #90.
	if tag := pickTierTag(sc.Tags); tag != "" {
		bare := strings.TrimPrefix(tag, "@")
		fmt.Fprintf(&b, "  - Tag tests for the %s tier (e.g. JUnit5 `@Tag(%q)`, pytest `pytest.mark.%s`).\n",
			tag, bare, bare)
	}

	// Harness profile identifiers. In MVP they are traceability metadata for
	// authoring/review and for project-owned integration test configuration.
	// A future harness runner may consume them as routing keys; until then the
	// literal need only appear where the project framework actually consumes it
	// — a dead reference does not bind anything.
	// Reframed post-#113 (2026-06-03) — structural-validator no longer
	// greps for these literals.
	if len(sc.HarnessProfileIDs) > 0 {
		fmt.Fprintf(&b, "  - Harness profile ID(s) for integration evidence traceability: %s.\n",
			quotedJoined(sc.HarnessProfileIDs))
	}

	// Env vars injected by project-owned integration setup now, and by a
	// semspec-managed harness runner post-MVP. The endpoint/config bindings
	// come from these env vars at test runtime; hardcoding values would
	// bypass the injection and ignore harness-specific configuration.
	// The dev's test code reads them via the language's idiomatic env
	// accessor (System.getenv / os.environ / std::env::var / etc.).
	if len(sc.Env) > 0 {
		keys := make([]string, 0, len(sc.Env))
		for k := range sc.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintf(&b, "  - Env vars provided by the integration test environment (read at test runtime, do not hardcode): %s.\n",
			quotedJoined(keys))
	}

	// Required assertions — copy verbatim from catalog. These describe
	// the BEHAVIOR the test must demonstrate, not text the test must
	// contain. The reviewer judges semantic satisfaction. Capped at
	// maxAssertionsPerScenario to guard prompt token budget.
	if len(sc.RequiredAssertions) > 0 {
		b.WriteString("  - Behavior the test must demonstrate (reviewer judges actual coverage, not text match):\n")
		limit := len(sc.RequiredAssertions)
		if limit > maxAssertionsPerScenario {
			limit = maxAssertionsPerScenario
		}
		for i := 0; i < limit; i++ {
			fmt.Fprintf(&b, "    * %s\n", sc.RequiredAssertions[i])
		}
		if dropped := len(sc.RequiredAssertions) - limit; dropped > 0 {
			fmt.Fprintf(&b, "    * (+%d more — see harness catalog entry)\n", dropped)
		}
	}

	b.WriteString("\n")
	return b.String()
}

// pickTierTag returns the first tier tag found in the tags slice
// (@integration / @smoke / @e2e), or "" when only @unit / facet tags
// are present. ADR-041 guarantees exactly one tier tag per scenario via
// ValidateScenarioTags, so the first match is the canonical one.
func pickTierTag(tags []string) string {
	for _, t := range tags {
		if t == "@integration" || t == "@smoke" || t == "@e2e" {
			return t
		}
	}
	return ""
}

// quotedJoined renders a slice as `"a", "b", "c"`. Used for prompt
// literals so the dev sees explicit string-literal form.
func quotedJoined(items []string) string {
	parts := make([]string, len(items))
	for i, s := range items {
		parts[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(parts, ", ")
}
