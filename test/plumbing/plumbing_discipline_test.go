// Package plumbing exercises the post-LLM-emission plumbing chain
// (scenario-generator denormalizer → workflow validators → requirement-
// executor synthesizer) end-to-end using production code paths and a
// canned in-memory plan. Catches plumbing-discipline regressions
// deterministically in under one second, without docker, mock-LLM, or
// real-LLM tokens.
//
// Issue #92 (the meta rule from the smoke-9 post-mortem 2026-06-02):
// every architectural finding from a paid run was observable from
// deterministic data-shape inspection alone. This package is the
// runtime check that the inspection lives in CI rather than waiting
// for a paid run to re-discover it.
//
// Why integration-tier rather than unit-tier: each individual contract
// is unit-tested in its own package (`processor/scenario-generator/
// denormalize_test.go`, `workflow/plan_story_test.go`, `processor/
// requirement-executor/binding_context_test.go`). This file proves the
// contracts COMPOSE — Bob's output is consumable by Sarah, Sarah's
// output is consumable by the synthesizer, the synthesizer's prompt
// surfaces the data Bob captured. Cross-package wire correctness is
// the value-add no unit test can provide on its own.
package plumbing_test

import (
	"strings"
	"testing"

	requirementexecutor "github.com/c360studio/semspec/processor/requirement-executor"
	scenariogenerator "github.com/c360studio/semspec/processor/scenario-generator"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/harnesscatalog"
)

// plumbingCatalog is the in-memory harness profile catalog the test
// scenarios reference. Mirrors the shape of
// workflow/harnesscatalog/catalog/*.yaml (Env + RequiredAssertions only —
// fields the denormalizer cares about). Self-contained so the test does
// not depend on the on-disk catalog evolving.
func plumbingCatalog() *harnesscatalog.Catalog {
	return &harnesscatalog.Catalog{
		Profiles: map[string]harnesscatalog.Profile{
			"plumbing.test-profile": {
				ID:                 "plumbing.test-profile",
				Env:                map[string]string{"PLUMBING_TEST_ENDPOINT": "tcp://localhost:9999"},
				RequiredAssertions: []string{"Observe the plumbing-test heartbeat."},
			},
		},
	}
}

// TestPlumbingDiscipline_FullChain pins the four post-smoke-9 contracts
// the unit tests prove in isolation:
//
//  1. scenario.Env and scenario.RequiredAssertions are populated from
//     the catalog when scenario.HarnessProfileIDs is non-empty (#89)
//  2. ValidateStories rejects sibling Stories that share files_owned
//     without a DependsOn edge (#88)
//  3. The dev's task prompt surfaces the @Tag/binding/env/assertion
//     context from the scenario (#90)
//
// Sequenced through production code paths in the order a real plan
// flows: Bob's denormalizer → Sarah's validator → synthesizer.
func TestPlumbingDiscipline_FullChain(t *testing.T) {
	catalog := plumbingCatalog()

	// Stage 1: a scenario with a harness binding goes through Bob's
	// denormalizer. Before denormalization Env/RequiredAssertions are
	// empty even though the catalog has the data.
	sc := workflow.Scenario{
		ID:                "scen.demo.1.1.1",
		RequirementID:     "req.demo.1",
		StoryID:           "story.demo.1.1",
		Tags:              []string{"@integration"},
		HarnessProfileIDs: []string{"plumbing.test-profile"},
	}
	if len(sc.Env) != 0 || len(sc.RequiredAssertions) != 0 {
		t.Fatalf("pre-denormalization scenario should have empty env/assertions, got env=%v assertions=%v", sc.Env, sc.RequiredAssertions)
	}

	if err := scenariogenerator.DenormalizeHarnessProfileData(&sc, catalog); err != nil {
		t.Fatalf("denormalize: %v", err)
	}
	if sc.Env["PLUMBING_TEST_ENDPOINT"] != "tcp://localhost:9999" {
		t.Errorf("expected env to be populated from catalog, got %v", sc.Env)
	}
	if len(sc.RequiredAssertions) != 1 || sc.RequiredAssertions[0] != "Observe the plumbing-test heartbeat." {
		t.Errorf("expected assertions to be populated from catalog, got %v", sc.RequiredAssertions)
	}

	// Stage 2: ValidateStories accepts a well-formed plan and rejects
	// the smoke-9 shape (overlapping files_owned without DependsOn).
	goodStories := []workflow.Story{
		{
			ID: "story.demo.1.1", RequirementID: "req.demo.1", Title: "Lifecycle",
			Status:     workflow.StoryStatusReady,
			FilesOwned: []string{"src/lifecycle.go"},
			Tasks: []workflow.Task{
				{ID: "task.demo.1.1.1", StoryID: "story.demo.1.1", Description: "Write failing lifecycle test."},
			},
		},
	}
	if err := workflow.ValidateStories(goodStories); err != nil {
		t.Errorf("well-formed stories should validate: %v", err)
	}

	smoke9Stories := []workflow.Story{
		{
			ID: "story.demo.1.1", RequirementID: "req.demo.1", Title: "Lifecycle",
			Status:     workflow.StoryStatusReady,
			FilesOwned: []string{"src/shared.go", "src/lifecycle.go"},
			Tasks:      []workflow.Task{{ID: "task.demo.1.1.1", StoryID: "story.demo.1.1", Description: "x"}},
		},
		{
			ID: "story.demo.2.1", RequirementID: "req.demo.2", Title: "Telemetry",
			Status:     workflow.StoryStatusReady,
			FilesOwned: []string{"src/shared.go", "src/telemetry.go"},
			Tasks:      []workflow.Task{{ID: "task.demo.2.1.1", StoryID: "story.demo.2.1", Description: "y"}},
			// Missing DependsOn — would race-write src/shared.go.
		},
	}
	if err := workflow.ValidateStories(smoke9Stories); err == nil {
		t.Error("smoke-9 file-overlap shape should fail validation")
	}

	// Stage 3: the synthesizer surfaces the binding block in the dev
	// prompt. Builds a minimal plan with the denormalized scenario.
	story := workflow.Story{
		ID: "story.demo.1.1", RequirementID: "req.demo.1", Title: "Lifecycle",
		Status:     workflow.StoryStatusReady,
		FilesOwned: []string{"src/lifecycle.go"},
		Tasks: []workflow.Task{
			{ID: "task.demo.1.1.1", StoryID: "story.demo.1.1", Description: "Write failing lifecycle test."},
		},
	}
	plan := &workflow.Plan{
		Stories:   []workflow.Story{story},
		Scenarios: []workflow.Scenario{sc},
	}

	// Use the exported test helper: the synthesizer lives in
	// requirement-executor and isn't exported, so we exercise its
	// behavior through a public surface. Since it is package-private,
	// the test exercises it via a thin pass-through in the package.
	// In practice the synthesizer is called by the executor on
	// requirement dispatch.
	prompt := requirementexecutor.BuildDevPromptForTesting(plan, story)
	if !strings.Contains(prompt, "## Integration Test Context") {
		t.Errorf("dev prompt should include the binding block:\n%s", prompt)
	}
	if !strings.Contains(prompt, "plumbing.test-profile") {
		t.Errorf("dev prompt should include the harness profile string literal:\n%s", prompt)
	}
	if !strings.Contains(prompt, "PLUMBING_TEST_ENDPOINT") {
		t.Errorf("dev prompt should include the env var name from catalog:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Observe the plumbing-test heartbeat.") {
		t.Errorf("dev prompt should include the required assertion from catalog:\n%s", prompt)
	}
}

// TestPlumbingDiscipline_UnitOnlyChain pins the no-op path: a story
// whose scenarios are all @unit produces no binding block in the dev
// prompt and the denormalizer is a no-op. Proves the additive nature
// of the chain — simple stories pay no overhead.
func TestPlumbingDiscipline_UnitOnlyChain(t *testing.T) {
	catalog := plumbingCatalog()

	sc := workflow.Scenario{
		ID:            "scen.demo.1.1.1",
		RequirementID: "req.demo.1",
		StoryID:       "story.demo.1.1",
		Tags:          []string{"@unit"},
	}
	if err := scenariogenerator.DenormalizeHarnessProfileData(&sc, catalog); err != nil {
		t.Fatalf("denormalize: %v", err)
	}
	if len(sc.Env) != 0 || len(sc.RequiredAssertions) != 0 {
		t.Errorf("unit-only scenario should not pick up catalog data, got env=%v assertions=%v", sc.Env, sc.RequiredAssertions)
	}

	story := workflow.Story{
		ID: "story.demo.1.1", RequirementID: "req.demo.1", Title: "Simple",
		Status:     workflow.StoryStatusReady,
		FilesOwned: []string{"src/x.go"},
		Tasks: []workflow.Task{
			{ID: "task.demo.1.1.1", StoryID: "story.demo.1.1", Description: "Write a unit test."},
		},
	}
	plan := &workflow.Plan{
		Stories:   []workflow.Story{story},
		Scenarios: []workflow.Scenario{sc},
	}

	prompt := requirementexecutor.BuildDevPromptForTesting(plan, story)
	if strings.Contains(prompt, "Integration Test Context") {
		t.Errorf("unit-only story prompt should NOT include binding block:\n%s", prompt)
	}
	if prompt != "Write a unit test." {
		t.Errorf("unit-only prompt should equal Task.Description verbatim, got:\n%s", prompt)
	}
}
