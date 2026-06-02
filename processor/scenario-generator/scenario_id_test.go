package scenariogenerator

import (
	"log/slog"
	"strings"
	"testing"
)

// TestScenarioIDFor_LegacyShape pins the pre-ADR-043 scenario ID format
// for back-compat with mock fixtures and pre-Sarah plans: when no story
// sequence is supplied (empty string), the ID is the historical
// `scenario.<slug>.<reqseq>.<i>` shape.
func TestScenarioIDFor_LegacyShape(t *testing.T) {
	got := scenarioIDFor("demo", "1", "", 2)
	want := "scenario.demo.1.2"
	if got != want {
		t.Errorf("scenarioIDFor(\"demo\", \"1\", \"\", 2) = %q, want %q", got, want)
	}
}

// TestScenarioIDFor_PerStoryShape pins the ADR-043 PR 4j scenario ID
// format: when storyseq is non-empty, it's spliced between reqseq and the
// per-batch index. This mirrors Sarah's task ID convention
// (task.<slug>.<reqseq>.<storyseq>.<taskseq>).
func TestScenarioIDFor_PerStoryShape(t *testing.T) {
	got := scenarioIDFor("demo", "1", "2", 3)
	want := "scenario.demo.1.2.3"
	if got != want {
		t.Errorf("scenarioIDFor(\"demo\", \"1\", \"2\", 3) = %q, want %q", got, want)
	}
}

// TestStorySequence_ExtractsTrailingSeq pins the storyseq derivation. Story
// IDs are `story.<slug>.<reqseq>.<storyseq>`; we want just the storyseq
// segment so it composes with reqseq into the scenario ID.
func TestStorySequence_ExtractsTrailingSeq(t *testing.T) {
	cases := map[string]string{
		"story.demo.1.1":   "1",
		"story.demo.1.42":  "42",
		"story.x.2.3":      "3",
		"":                 "",
		"story-without-dots": "story-without-dots",
	}
	for in, want := range cases {
		got := storySequence(in)
		if got != want {
			t.Errorf("storySequence(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestParseScenariosFromResult_PerStoryProducesDistinctIDsAcrossStories is
// the headline regression test for go-reviewer Pass-2 finding C3.
//
// Pre-fix, two Stories under the same Requirement produced colliding
// scenario IDs (`scenario.demo.1.1`, `scenario.demo.1.2` from BOTH
// dispatches). The collision was masked by Pass-2 C2 — the consumer wiped
// one batch on every mutation, so duplicates never landed in plan.Scenarios
// simultaneously. PR #73 fixed C2; running both dispatches now lands both
// batches, and identical IDs would break every downstream lookup. This PR
// closes C3 by including the storyseq segment in per-Story dispatch IDs.
func TestParseScenariosFromResult_PerStoryProducesDistinctIDsAcrossStories(t *testing.T) {
	c := &Component{logger: slog.Default()}

	// The agent result is a minimal scenarios JSON wrapper with a single
	// Given/When/Then so parsing succeeds without triggering tag validation.
	result := `{"scenarios":[
		{"title":"happy","given":"a precondition","when":"an action","then":["an assertion"],"tags":["@unit"]}
	]}`

	storyA, err := c.parseScenariosFromResult(result, "demo", "req.demo.1", "story.demo.1.1")
	if err != nil {
		t.Fatalf("parse storyA: %v", err)
	}
	storyB, err := c.parseScenariosFromResult(result, "demo", "req.demo.1", "story.demo.1.2")
	if err != nil {
		t.Fatalf("parse storyB: %v", err)
	}

	if len(storyA) != 1 || len(storyB) != 1 {
		t.Fatalf("expected one scenario per Story, got A=%d B=%d", len(storyA), len(storyB))
	}
	if storyA[0].ID == storyB[0].ID {
		t.Errorf("Story A and Story B produced the SAME scenario ID (%q) under one Requirement — collision survives across the per-Story merge, which is the Pass-2 C3 bug", storyA[0].ID)
	}
	if !strings.HasPrefix(storyA[0].ID, "scenario.demo.1.1.") {
		t.Errorf("Story A's scenario ID = %q, want prefix scenario.demo.1.1.", storyA[0].ID)
	}
	if !strings.HasPrefix(storyB[0].ID, "scenario.demo.1.2.") {
		t.Errorf("Story B's scenario ID = %q, want prefix scenario.demo.1.2.", storyB[0].ID)
	}
}

// TestParseScenariosFromResult_LegacyDispatchPreservesPreADR043IDs pins
// the back-compat path: when storyID is empty (mock fixtures, pre-Sarah
// plans), scenario IDs keep the historical `scenario.<slug>.<reqseq>.<i>`
// format. Migrating downstream tooling to expect the 5-segment shape would
// break legacy callers — this test catches that drift.
func TestParseScenariosFromResult_LegacyDispatchPreservesPreADR043IDs(t *testing.T) {
	c := &Component{logger: slog.Default()}
	result := `{"scenarios":[
		{"title":"happy","given":"a precondition","when":"an action","then":["an assertion"],"tags":["@unit"]}
	]}`

	scenarios, err := c.parseScenariosFromResult(result, "demo", "req.demo.1", "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(scenarios) != 1 {
		t.Fatalf("expected 1 scenario, got %d", len(scenarios))
	}
	if scenarios[0].ID != "scenario.demo.1.1" {
		t.Errorf("legacy ID = %q, want scenario.demo.1.1", scenarios[0].ID)
	}
}

// TestParseScenariosFromResult_DeterministicAcrossReRuns pins that two
// parses of the same input produce identical IDs — load-bearing for
// idempotent retries (scenario-generator's retry loop) so the second
// dispatch lands the same IDs in plan.Scenarios as the first would have.
func TestParseScenariosFromResult_DeterministicAcrossReRuns(t *testing.T) {
	c := &Component{logger: slog.Default()}
	result := `{"scenarios":[
		{"title":"a","given":"g","when":"w","then":["t"],"tags":["@unit"]},
		{"title":"b","given":"g","when":"w","then":["t"],"tags":["@unit"]}
	]}`

	first, err := c.parseScenariosFromResult(result, "demo", "req.demo.1", "story.demo.1.1")
	if err != nil {
		t.Fatalf("first parse: %v", err)
	}
	second, err := c.parseScenariosFromResult(result, "demo", "req.demo.1", "story.demo.1.1")
	if err != nil {
		t.Fatalf("second parse: %v", err)
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Errorf("non-deterministic ID at index %d: first=%q second=%q", i, first[i].ID, second[i].ID)
		}
	}
}
