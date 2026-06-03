package requirementexecutor

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestBuildBindingContextBlock_Empty covers the no-op paths: empty
// scenario slice, @unit-only scenarios, scenarios without any
// integration-tier signal. The block must return "" so the dev prompt
// stays unchanged for purely unit-tier work.
func TestBuildBindingContextBlock_Empty(t *testing.T) {
	cases := []struct {
		name      string
		scenarios []workflow.Scenario
	}{
		{name: "empty slice"},
		{name: "nil slice", scenarios: nil},
		{
			name: "@unit only, no harness binding",
			scenarios: []workflow.Scenario{
				{ID: "s1", Tags: []string{"@unit"}},
				{ID: "s2", Tags: []string{"@unit"}},
			},
		},
		{
			name: "untagged scenarios with no harness binding",
			scenarios: []workflow.Scenario{
				{ID: "s1"},
				{ID: "s2"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildBindingContextBlock(tc.scenarios)
			if got != "" {
				t.Errorf("expected empty block, got:\n%s", got)
			}
		})
	}
}

// TestBuildBindingContextBlock_IntegrationScenario covers the smoke-9
// shape: an @integration scenario with harness profile binding, env,
// and required assertions. All four binding facets must surface.
func TestBuildBindingContextBlock_IntegrationScenario(t *testing.T) {
	scenarios := []workflow.Scenario{
		{
			ID:                "scenario.ebe27a10f9e4.1.1.3",
			Tags:              []string{"@integration"},
			HarnessProfileIDs: []string{"mavlink.px4-sitl.mavsdk-smoke"},
			Env:               map[string]string{"PX4_SIM_MODEL": "iris"},
			RequiredAssertions: []string{
				"Observe a MAVLink heartbeat from the SITL target.",
				"Assert MAVSDK reports a connected vehicle before plugin calls run.",
			},
		},
	}

	got := buildBindingContextBlock(scenarios)
	if got == "" {
		t.Fatal("expected non-empty block, got empty")
	}

	mustContain(t, got, "## Integration Test Context")
	mustContain(t, got, "scenario.ebe27a10f9e4.1.1.3")
	mustContain(t, got, "@integration")
	mustContain(t, got, `"integration"`) // JUnit5 tag literal
	mustContain(t, got, "pytest.mark.integration")
	// Go `//go:build integration` example intentionally omitted — it's
	// file-level, not per-test, and would exclude unit tests in the
	// same file (go-reviewer feedback on PR #90).
	if strings.Contains(got, "//go:build") {
		t.Errorf("Go build-tag example should not appear in tier directive:\n%s", got)
	}
	mustContain(t, got, `"mavlink.px4-sitl.mavsdk-smoke"`) // harness ID as string literal
	mustContain(t, got, `"PX4_SIM_MODEL"`)                 // env var key as literal
	mustContain(t, got, "Observe a MAVLink heartbeat from the SITL target.")
	mustContain(t, got, "Assert MAVSDK reports a connected vehicle before plugin calls run.")
}

// TestBuildBindingContextBlock_HarnessOnlyNoTag covers a scenario that
// has harness bindings but somehow lacks a tier tag. The block must
// still render the harness/env/assertions so the dev sees them; the
// tier directive is omitted when no tier tag is present.
func TestBuildBindingContextBlock_HarnessOnlyNoTag(t *testing.T) {
	scenarios := []workflow.Scenario{
		{
			ID:                "s1",
			HarnessProfileIDs: []string{"some.profile"},
			Env:               map[string]string{"VAR": "val"},
		},
	}
	got := buildBindingContextBlock(scenarios)
	mustContain(t, got, "some.profile")
	mustContain(t, got, `"VAR"`)
	if strings.Contains(got, "pytest.mark.") {
		t.Errorf("tier directive should be absent when no tier tag, got:\n%s", got)
	}
}

// TestBuildBindingContextBlock_MultipleScenarios covers a story with
// multiple integration-tier scenarios. Each must surface as its own
// bullet block with its own bindings.
func TestBuildBindingContextBlock_MultipleScenarios(t *testing.T) {
	scenarios := []workflow.Scenario{
		{
			ID:                "s1",
			Tags:              []string{"@integration"},
			HarnessProfileIDs: []string{"profile.a"},
		},
		{
			ID:                "s2",
			Tags:              []string{"@smoke"},
			HarnessProfileIDs: []string{"profile.b"},
		},
		{
			// @unit only — must be filtered out.
			ID:   "s3",
			Tags: []string{"@unit"},
		},
	}
	got := buildBindingContextBlock(scenarios)
	mustContain(t, got, "s1")
	mustContain(t, got, "s2")
	mustContain(t, got, "profile.a")
	mustContain(t, got, "profile.b")
	if strings.Contains(got, "- **s3**") {
		t.Errorf("@unit-only scenario s3 should be filtered out, got:\n%s", got)
	}
}

// TestBuildBindingContextBlock_MultipleEnvKeysSorted covers
// determinism: when a scenario has multiple env keys, they must render
// in sorted order so the prompt is byte-identical across runs.
func TestBuildBindingContextBlock_MultipleEnvKeysSorted(t *testing.T) {
	scenarios := []workflow.Scenario{
		{
			ID:                "s1",
			Tags:              []string{"@integration"},
			HarnessProfileIDs: []string{"p1"},
			Env: map[string]string{
				"ZEBRA":  "z",
				"ALPHA":  "a",
				"MIDDLE": "m",
			},
		},
	}
	got := buildBindingContextBlock(scenarios)
	// Expect "ALPHA", "MIDDLE", "ZEBRA" in that order.
	idxA := strings.Index(got, `"ALPHA"`)
	idxM := strings.Index(got, `"MIDDLE"`)
	idxZ := strings.Index(got, `"ZEBRA"`)
	if idxA < 0 || idxM < 0 || idxZ < 0 {
		t.Fatalf("missing env keys in output:\n%s", got)
	}
	if !(idxA < idxM && idxM < idxZ) {
		t.Errorf("env keys not in sorted order: ALPHA@%d MIDDLE@%d ZEBRA@%d", idxA, idxM, idxZ)
	}
}

func mustContain(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("output missing %q\n--- output ---\n%s", want, got)
	}
}

// TestSynthesizeTaskDAGForStory_BindingBlockAppendedToPrompts pins the
// integration: when a Story has @integration scenarios, every TaskNode
// in the synthesized DAG gets the binding block appended to its Prompt
// (after the original Task.Description, separated by ---).
func TestSynthesizeTaskDAGForStory_BindingBlockAppendedToPrompts(t *testing.T) {
	story := workflow.Story{
		ID:             "story.demo.1.1",
		RequirementIDs: []string{"req.demo.1"}, ComponentName: "placeholder-component",
		Title:      "Lifecycle",
		FilesOwned: []string{"src/x.go"},
		Tasks: []workflow.Task{
			{ID: "task.demo.1.1.1", StoryID: "story.demo.1.1", Description: "Write failing test for lifecycle."},
			{ID: "task.demo.1.1.2", StoryID: "story.demo.1.1", Description: "Implement to pass."},
		},
	}
	plan := &workflow.Plan{
		Stories: []workflow.Story{story},
		Scenarios: []workflow.Scenario{
			{
				ID:                "scen.demo.1.1.1",
				StoryID:           "story.demo.1.1",
				RequirementID:     "req.demo.1",
				Tags:              []string{"@integration"},
				HarnessProfileIDs: []string{"mavlink.px4-sitl.mavsdk-smoke"},
				Env:               map[string]string{"PX4_SIM_MODEL": "iris"},
			},
		},
	}

	dag, err := synthesizeTaskDAGForStory(plan, story)
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	if len(dag.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(dag.Nodes))
	}
	// Build a Task.ID → Description index so the prefix assertion isn't
	// coupled to specific description strings (go-reviewer feedback).
	descByID := make(map[string]string, len(story.Tasks))
	for _, t := range story.Tasks {
		descByID[t.ID] = t.Description
	}
	for _, n := range dag.Nodes {
		if !strings.Contains(n.Prompt, "Integration Test Context") {
			t.Errorf("node %s prompt missing binding block:\n%s", n.ID, n.Prompt)
		}
		if !strings.Contains(n.Prompt, "mavlink.px4-sitl.mavsdk-smoke") {
			t.Errorf("node %s prompt missing harness profile literal:\n%s", n.ID, n.Prompt)
		}
		if want := descByID[n.ID]; !strings.HasPrefix(n.Prompt, want) {
			t.Errorf("node %s prompt should start with original Task.Description %q, got:\n%s", n.ID, want, n.Prompt)
		}
	}
}

// TestBuildBindingContextBlock_AssertionCap pins the safety cap on
// RequiredAssertions: a profile with more than maxAssertionsPerScenario
// entries renders the first N + a truncation marker. Catalog authors
// today stay well below this cap; the test guards against future bloat.
func TestBuildBindingContextBlock_AssertionCap(t *testing.T) {
	many := make([]string, 0, maxAssertionsPerScenario+5)
	for i := 0; i < maxAssertionsPerScenario+5; i++ {
		many = append(many, "Assertion XYZ-"+string(rune('A'+i)))
	}
	scenarios := []workflow.Scenario{
		{
			ID:                 "s1",
			Tags:               []string{"@integration"},
			HarnessProfileIDs:  []string{"p1"},
			RequiredAssertions: many,
		},
	}
	got := buildBindingContextBlock(scenarios)
	// First N must render.
	for i := 0; i < maxAssertionsPerScenario; i++ {
		if !strings.Contains(got, many[i]) {
			t.Errorf("expected assertion %d (%q) in output", i, many[i])
		}
	}
	// (+N more) marker should appear with the correct overflow count.
	if !strings.Contains(got, "(+5 more — see harness catalog entry)") {
		t.Errorf("expected truncation marker '(+5 more — see harness catalog entry)' in output:\n%s", got)
	}
	// Last few assertions must NOT render verbatim.
	for i := maxAssertionsPerScenario; i < len(many); i++ {
		if strings.Contains(got, many[i]+"\n") {
			t.Errorf("assertion %d (%q) should be truncated", i, many[i])
		}
	}
}

// TestSynthesizeTaskDAGForStory_NoBindingForUnitOnlyStory pins that
// @unit-only stories don't get the binding block appended (no behavior
// change for trivial scopes).
func TestSynthesizeTaskDAGForStory_NoBindingForUnitOnlyStory(t *testing.T) {
	story := workflow.Story{
		ID:             "story.demo.1.1",
		RequirementIDs: []string{"req.demo.1"}, ComponentName: "placeholder-component",
		Title:      "Simple",
		FilesOwned: []string{"src/x.go"},
		Tasks: []workflow.Task{
			{ID: "task.demo.1.1.1", StoryID: "story.demo.1.1", Description: "Write a test."},
		},
	}
	plan := &workflow.Plan{
		Stories: []workflow.Story{story},
		Scenarios: []workflow.Scenario{
			{ID: "scen.demo.1.1.1", StoryID: "story.demo.1.1", RequirementID: "req.demo.1", Tags: []string{"@unit"}},
		},
	}

	dag, err := synthesizeTaskDAGForStory(plan, story)
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	if got := dag.Nodes[0].Prompt; got != "Write a test." {
		t.Errorf("expected prompt unchanged for unit-only story, got:\n%s", got)
	}
}
