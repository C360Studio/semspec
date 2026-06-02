package requirementexecutor

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestGroupScenariosByTier_PartitionsCorrectly(t *testing.T) {
	scenarios := []workflow.Scenario{
		{ID: "u1", Tags: []string{workflow.TierUnit}},
		{ID: "i1", Tags: []string{workflow.TierIntegration}},
		{ID: "u2", Tags: []string{workflow.TierUnit, "@flaky"}},
		{ID: "s1", Tags: []string{workflow.TierSmoke}},
		{ID: "e1", Tags: []string{workflow.TierE2E}},
		{ID: "legacy", Tags: nil},
		{ID: "facets-only", Tags: []string{"@flaky"}},
	}
	g := groupScenariosByTier(scenarios)
	if len(g.unit) != 2 || g.unit[0].ID != "u1" || g.unit[1].ID != "u2" {
		t.Errorf("unit grouping wrong: %+v", g.unit)
	}
	if len(g.integration) != 1 || g.integration[0].ID != "i1" {
		t.Errorf("integration grouping wrong: %+v", g.integration)
	}
	if len(g.smoke) != 1 || g.smoke[0].ID != "s1" {
		t.Errorf("smoke grouping wrong: %+v", g.smoke)
	}
	if len(g.e2e) != 1 || g.e2e[0].ID != "e1" {
		t.Errorf("e2e grouping wrong: %+v", g.e2e)
	}
	// Scenarios with no tier tag (including facet-only) land in untagged.
	if len(g.untagged) != 2 {
		t.Errorf("expected 2 untagged (legacy + facets-only), got %d: %+v", len(g.untagged), g.untagged)
	}
}

func TestFirstTierTag_HonorsFirstMatch(t *testing.T) {
	cases := []struct {
		tags []string
		want string
	}{
		{nil, ""},
		{[]string{"@flaky"}, ""},
		{[]string{workflow.TierUnit}, workflow.TierUnit},
		{[]string{"@flaky", workflow.TierUnit}, workflow.TierUnit},
		// First-tier-tag-wins for double-tier scenarios that slipped past
		// upstream validation. ADR-041 PR 1 + PR 3 normally reject these.
		{[]string{workflow.TierIntegration, workflow.TierUnit}, workflow.TierIntegration},
	}
	for _, c := range cases {
		if got := firstTierTag(c.tags); got != c.want {
			t.Errorf("firstTierTag(%v) = %q, want %q", c.tags, got, c.want)
		}
	}
}

// TestBuildReviewPrompt_TierAwareContract is the load-bearing PR 5 test.
// The req-reviewer prompt MUST communicate three contracts to the LLM:
//   - @unit scenarios need running tests
//   - @integration scenarios need correctly-AUTHORED tests but NOT passing
//     ones (the harness isn't running in dev sandbox)
//   - @smoke / @e2e MUST NOT block dev approval
//
// Without these contracts the legacy "verify all scenarios" prompt
// produces the issue-#37 infinite-reject loop: req-reviewer demands
// integration-tier behavior the dev sandbox can't provide.
func TestBuildReviewPrompt_TierAwareContract(t *testing.T) {
	c := &Component{}
	exec := &requirementExecution{
		Title:       "MAVSDK lifecycle",
		Description: "Boot mavsdk_server and observe HEARTBEAT",
		Scenarios: []workflow.Scenario{
			{
				ID:    "scn.unit.1",
				Given: "a Config with defaults",
				When:  "the builder runs",
				Then:  []string{"the connection string is the env fallback"},
				Tags:  []string{workflow.TierUnit},
			},
			{
				ID:                "scn.integration.1",
				Given:             "the SITL endpoint at env $PX4_SIM_MODEL",
				When:              "the driver starts",
				Then:              []string{"a HEARTBEAT is received"},
				Tags:              []string{workflow.TierIntegration},
				HarnessProfileIDs: []string{"mavlink.px4-sitl.mavsdk-smoke"},
			},
		},
	}
	prompt := c.buildReviewPrompt(exec, exec.Scenarios)

	// Load-bearing contract phrases the persona prompt + this scaffold must
	// carry to the LLM. If a future edit drops these, the issue-#37 fix is
	// silently undone.
	mustContain := []string{
		"@unit scenarios",
		"@integration scenarios",
		// The crucial sentence: @integration tests don't need to PASS at
		// dev-completion. This is the structural fix for #37.
		"does NOT need to PASS",
		// qa-runner downstream is named so the LLM knows where the
		// integration-passing gate actually lives.
		"qa-runner gates",
		// String-literal contract for the structural-validator's PR 4 check.
		"STRING LITERAL",
		// Env-var contract.
		"environment variables",
		// Required-assertion contract.
		"required_assertion",
		// Harness binding hint appears inline next to the @integration
		// scenario so the reviewer can verify without cross-referencing.
		"(harness: mavlink.px4-sitl.mavsdk-smoke)",
	}
	for _, want := range mustContain {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing load-bearing phrase %q\n---\n%s\n---", want, prompt)
		}
	}
}

// TestBuildReviewPrompt_SmokeAndE2EDoNotBlock pins that @smoke and @e2e
// scenarios surface the "do NOT block dev approval" instruction. Without
// this, the reviewer would reject dev work for missing future-tier behavior.
func TestBuildReviewPrompt_SmokeAndE2EDoNotBlock(t *testing.T) {
	c := &Component{}
	exec := &requirementExecution{
		Title: "release flows",
		Scenarios: []workflow.Scenario{
			{ID: "scn.smoke.1", Tags: []string{workflow.TierSmoke}, Given: "g", When: "w", Then: []string{"t"}},
			{ID: "scn.e2e.1", Tags: []string{workflow.TierE2E}, Given: "g", When: "w", Then: []string{"t"}},
		},
	}
	prompt := c.buildReviewPrompt(exec, exec.Scenarios)

	// Both @smoke and @e2e sections must include the do-not-block clause.
	occurrences := strings.Count(prompt, "Do NOT block dev approval")
	if occurrences < 2 {
		t.Errorf("expected 'Do NOT block dev approval' in BOTH @smoke and @e2e sections (got %d occurrences)\nprompt:\n%s", occurrences, prompt)
	}
}

// TestBuildReviewPrompt_LegacyUntaggedScenariosKeepWorking pins that
// pre-ADR-041 plans without scenario tags continue to produce a sensible
// prompt rather than crashing or rendering empty sections.
func TestBuildReviewPrompt_LegacyUntaggedScenariosKeepWorking(t *testing.T) {
	c := &Component{}
	exec := &requirementExecution{
		Title: "legacy plan",
		Scenarios: []workflow.Scenario{
			{ID: "scn.1", Given: "g", When: "w", Then: []string{"t"}},
		},
	}
	prompt := c.buildReviewPrompt(exec, exec.Scenarios)

	if !strings.Contains(prompt, "Untagged scenarios (legacy / pre-ADR-041)") {
		t.Errorf("expected legacy section header, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "[scn.1]") {
		t.Errorf("expected scenario id in legacy section, got:\n%s", prompt)
	}
	// Tier sections should NOT render when there are no scenarios at that
	// tier — otherwise the LLM sees empty "### @unit scenarios" headers
	// and may invent obligations.
	if strings.Contains(prompt, "### @unit scenarios") {
		t.Errorf("untagged-only plan should not render an empty @unit section, got:\n%s", prompt)
	}
}

// TestBuildReviewPrompt_FullStackProducesAllTiers exercises the full
// tier-grouped output for a requirement spanning all four pyramid tiers.
// Pins the ordering @unit → @integration → @smoke → @e2e (deterministic
// reviewer-facing diff stability).
func TestBuildReviewPrompt_FullStackProducesAllTiers(t *testing.T) {
	c := &Component{}
	exec := &requirementExecution{
		Title: "full stack",
		Scenarios: []workflow.Scenario{
			{ID: "u", Tags: []string{workflow.TierUnit}, Given: "g", When: "w", Then: []string{"t"}},
			{ID: "i", Tags: []string{workflow.TierIntegration}, HarnessProfileIDs: []string{"p1"}, Given: "g", When: "w", Then: []string{"t"}},
			{ID: "s", Tags: []string{workflow.TierSmoke}, Given: "g", When: "w", Then: []string{"t"}},
			{ID: "e", Tags: []string{workflow.TierE2E}, Given: "g", When: "w", Then: []string{"t"}},
		},
	}
	prompt := c.buildReviewPrompt(exec, exec.Scenarios)

	// Find each section header's index; they must appear in pyramid order.
	headers := []string{
		"### @unit scenarios",
		"### @integration scenarios",
		"### @smoke scenarios",
		"### @e2e scenarios",
	}
	prev := -1
	for _, h := range headers {
		idx := strings.Index(prompt, h)
		if idx < 0 {
			t.Errorf("missing section %q", h)
			continue
		}
		if idx < prev {
			t.Errorf("section %q (idx %d) appears before previous section (idx %d) — pyramid order broken\nprompt:\n%s", h, idx, prev, prompt)
		}
		prev = idx
	}
}
