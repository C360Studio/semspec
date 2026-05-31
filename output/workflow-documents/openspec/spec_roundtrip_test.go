package openspec

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// TestRenderSpec_TagAndHarnessLineEmitted pins the ADR-041 PR 6 emit
// shape: a tagged scenario produces a `@tier` · harness: line below the
// heading. Without this, the operator-facing spec.md would silently drop
// the tier discipline ADR-041 introduces, and adopter tooling that filters
// by tier would see nothing to filter on.
func TestRenderSpec_TagAndHarnessLineEmitted(t *testing.T) {
	plan := tagAwarePlan()
	got := RenderSpec(plan, "mavsdk-lifecycle")

	required := []string{
		"#### Scenario: HEARTBEAT observed after driver start",
		"`@integration` · harness: `mavlink.px4-sitl.mavsdk-smoke`",
		"**GIVEN** the SITL endpoint at env $PX4_SIM_MODEL",
		"**WHEN** the driver starts",
		"**THEN** a MAVLink HEARTBEAT is received within 10 seconds",
	}
	for _, s := range required {
		if !strings.Contains(got, s) {
			t.Errorf("missing %q in:\n%s", s, got)
		}
	}
}

// TestRenderSpec_TaglessScenarioOmitsTagLine pins the back-compat guard:
// legacy scenarios with no tags MUST NOT render an empty tag line. The
// resulting spec.md should look like standard openspec markdown so
// pre-ADR-041 plans keep emitting cleanly.
func TestRenderSpec_TaglessScenarioOmitsTagLine(t *testing.T) {
	got := RenderSpec(samplePlan(), "mavsdk-bootstrap")
	// The tag-line prefix character (backtick + @) MUST NOT appear in any
	// line of the rendered spec when scenarios are tagless.
	if strings.Contains(got, "`@") {
		t.Errorf("tagless plan should not render a tag line containing '`@', got:\n%s", got)
	}
}

// TestRenderSpec_MultiHarnessBindingRendersCSV pins the formatting choice
// from ADR §"OpenSpec round-trip syntax": multi-binding harness IDs render
// comma-separated on the same prose line.
func TestRenderSpec_MultiHarnessBindingRendersCSV(t *testing.T) {
	plan := tagAwarePlan()
	// Override the scenario with a multi-binding variant.
	plan.Scenarios[0].HarnessProfileIDs = []string{
		"mavlink.px4-sitl.mavsdk-smoke",
		"mavlink.raw-mavlink-direct",
	}
	got := RenderSpec(plan, "mavsdk-lifecycle")
	if !strings.Contains(got, "`mavlink.px4-sitl.mavsdk-smoke`, `mavlink.raw-mavlink-direct`") {
		t.Errorf("multi-binding harness IDs should render comma-separated, got:\n%s", got)
	}
}

// TestRenderSpec_RoundTripPreservesTagsAndHarness is the load-bearing
// PR 6 test: emit a tagged plan to spec.md, parse it back via the test-
// scope markdown parser, assert Tags and HarnessProfileIDs round-trip
// without loss. Adopter tools (`openspec validate`) only parse the
// markdown surface; this test pins that surface as the wire shape.
//
// The production import path is graph-based via semsource (separate
// repo); semsource's tag awareness via the new vocabulary/spec
// ScenarioTag + ScenarioHarnessProfile predicates is a follow-up. Here
// we verify the EMITTER produces a parser-friendly surface so that
// once semsource adopts the predicates the round-trip closes end-to-end.
func TestRenderSpec_RoundTripPreservesTagsAndHarness(t *testing.T) {
	plan := tagAwarePlan()
	got := RenderSpec(plan, "mavsdk-lifecycle")

	scenarios := parseScenariosFromSpec(t, got)
	if len(scenarios) != 1 {
		t.Fatalf("expected 1 scenario parsed back, got %d", len(scenarios))
	}
	got0 := scenarios[0]
	want := plan.Scenarios[0]
	if got0.Title != want.Title {
		t.Errorf("Title mismatch: got %q, want %q", got0.Title, want.Title)
	}
	if !equalStrings(got0.Tags, want.Tags) {
		t.Errorf("Tags mismatch: got %v, want %v", got0.Tags, want.Tags)
	}
	if !equalStrings(got0.HarnessProfileIDs, want.HarnessProfileIDs) {
		t.Errorf("HarnessProfileIDs mismatch: got %v, want %v", got0.HarnessProfileIDs, want.HarnessProfileIDs)
	}
	if got0.Given != want.Given {
		t.Errorf("Given mismatch: got %q, want %q", got0.Given, want.Given)
	}
	if got0.When != want.When {
		t.Errorf("When mismatch: got %q, want %q", got0.When, want.When)
	}
	if !equalStrings(got0.Then, want.Then) {
		t.Errorf("Then mismatch: got %v, want %v", got0.Then, want.Then)
	}
}

// ---------------------------------------------------------------------------
// Test-scope markdown parser. Production parsing is graph-based via
// semsource; this parser exists only so PR 6 can pin the emitter shape
// without touching the production import pipeline.
// ---------------------------------------------------------------------------

var (
	scenarioHeadingRE = regexp.MustCompile(`^####\s+Scenario:\s+(.+)$`)
	tagInBackticksRE  = regexp.MustCompile("`([^`]+)`")
)

// parseScenariosFromSpec walks a rendered spec.md and reconstructs each
// scenario as workflow.Scenario. Lossless for the fields ADR-041 PR 6
// emits (Title, Tags, HarnessProfileIDs, Given, When, Then).
//
// The parser is line-oriented and stateful: it tracks whether the current
// position is inside a scenario, and which field (tag line / given /
// when / then) it last consumed. THEN clauses include subsequent AND
// lines. Robust to blank lines between sections.
func parseScenariosFromSpec(t *testing.T, source string) []workflow.Scenario {
	t.Helper()
	lines := strings.Split(source, "\n")
	var (
		scenarios []workflow.Scenario
		current   *workflow.Scenario
	)
	flush := func() {
		if current != nil {
			scenarios = append(scenarios, *current)
		}
		current = nil
	}
	for _, line := range lines {
		// New scenario heading — flush the previous one and start fresh.
		if m := scenarioHeadingRE.FindStringSubmatch(line); m != nil {
			flush()
			current = &workflow.Scenario{Title: strings.TrimSpace(m[1])}
			continue
		}
		if current == nil {
			continue
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "**GIVEN**"):
			current.Given = strings.TrimSpace(strings.TrimPrefix(trimmed, "**GIVEN**"))
		case strings.HasPrefix(trimmed, "**WHEN**"):
			current.When = strings.TrimSpace(strings.TrimPrefix(trimmed, "**WHEN**"))
		case strings.HasPrefix(trimmed, "**THEN**"):
			current.Then = []string{strings.TrimSpace(strings.TrimPrefix(trimmed, "**THEN**"))}
		case strings.HasPrefix(trimmed, "**AND**"):
			current.Then = append(current.Then, strings.TrimSpace(strings.TrimPrefix(trimmed, "**AND**")))
		case strings.HasPrefix(trimmed, "`@") || (strings.HasPrefix(trimmed, "`") && strings.Contains(trimmed, "harness:")):
			// Tag line: extract every backtick-quoted token. Split by
			// the "· harness:" marker into tags vs harness ids.
			parts := strings.SplitN(trimmed, "harness:", 2)
			current.Tags = backtickTokens(parts[0])
			if len(parts) == 2 {
				current.HarnessProfileIDs = backtickTokens(parts[1])
			}
		}
	}
	flush()
	return scenarios
}

// backtickTokens returns the set of backtick-quoted tokens in s, in order.
func backtickTokens(s string) []string {
	matches := tagInBackticksRE.FindAllStringSubmatch(s, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

// equalStrings is a small helper for slice equality in tests.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// tagAwarePlan returns a plan with one capability + one requirement + one
// scenario carrying tier tag + harness binding. Used by the round-trip
// and tag-emit tests.
func tagAwarePlan() *workflow.Plan {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	return &workflow.Plan{
		Slug: "mavlink-lifecycle",
		Exploration: &workflow.Exploration{
			Capabilities: []workflow.Capability{
				{
					Name:        "mavsdk-lifecycle",
					Lifecycle:   workflow.CapabilityNew,
					Description: "Boot mavsdk_server and observe MAVLink HEARTBEAT.",
					Surfaces:    []workflow.CapabilitySurface{workflow.SurfaceAPI},
				},
			},
		},
		Requirements: []workflow.Requirement{
			{
				ID:             "req.mavlink.1",
				PlanID:         "mavlink-lifecycle",
				Title:          "Driver boots and connects to SITL",
				Description:    "The driver SHALL start mavsdk_server and observe a HEARTBEAT.",
				CapabilityName: "mavsdk-lifecycle",
				FilesOwned:     []string{"src/main/java/Driver.java"},
				CreatedAt:      now,
				UpdatedAt:      now,
			},
		},
		Scenarios: []workflow.Scenario{
			{
				ID:                "scn.mavlink.lifecycle.1",
				RequirementID:     "req.mavlink.1",
				Title:             "HEARTBEAT observed after driver start",
				Given:             "the SITL endpoint at env $PX4_SIM_MODEL",
				When:              "the driver starts",
				Then:              []string{"a MAVLink HEARTBEAT is received within 10 seconds", "the MAVSDK Core connection state transitions to mavsdk_core_connected"},
				Tags:              []string{workflow.TierIntegration},
				HarnessProfileIDs: []string{"mavlink.px4-sitl.mavsdk-smoke"},
				CreatedAt:         now,
				UpdatedAt:         now,
			},
		},
	}
}
