package requirementexecutor

import (
	"fmt"

	"github.com/c360studio/semspec/workflow"
)

// BuildDevPromptForTesting synthesizes the DAG for `story` against
// `plan` and returns the dev prompt of the first node. Cross-package
// integration tests (test/plumbing/) use this to assert the dev prompt
// surfaces the binding block end-to-end without duplicating synthesis
// logic. Production code paths continue to use synthesizeTaskDAGForStory
// directly.
//
// Returns "" if synthesis fails or the DAG is empty — callers should
// treat empty as a test-setup error rather than a no-op success.
func BuildDevPromptForTesting(plan *workflow.Plan, story workflow.Story) string {
	dag, err := synthesizeTaskDAGForStory(plan, story)
	if err != nil || dag == nil || len(dag.Nodes) == 0 {
		return ""
	}
	return dag.Nodes[0].Prompt
}

// synthesizeTaskDAGForStory converts a single Sarah-prepared Story into a
// TaskDAG. ADR-043 PR 4h: per-Story dispatch synthesizes one DAG per Story
// (instead of combining every Story on a requirement into one flat DAG).
// Sequencing of Stories within a requirement is handled by the caller via
// Story.DependsOn topo-sort; the synthesized DAG carries only the Story's
// own intra-Task ordering.
//
// Mapping:
//   - Each Task in the Story becomes one TaskNode.
//   - TaskNode.ID = Task.ID (canonical task.<slug>.<reqseq>.<storyseq>.<taskseq>).
//   - TaskNode.Prompt = Task.Description.
//   - TaskNode.Role = "developer".
//   - TaskNode.DependsOn = intra-story Task.DependsOn (canonical IDs).
//   - TaskNode.FileScope = parent Story.FilesOwned.
//   - TaskNode.ScenarioIDs = IDs of scenarios whose StoryID == this story.
//
// Returns (nil, err) on Sarah-readiness gate violations (no tasks / no
// files_owned). Caller surfaces as a planning-phase failure.
func synthesizeTaskDAGForStory(plan *workflow.Plan, story workflow.Story) (*TaskDAG, error) {
	if len(story.Tasks) == 0 {
		return nil, fmt.Errorf("story %q has no tasks; cannot synthesize DAG", story.ID)
	}
	if len(story.FilesOwned) == 0 {
		return nil, fmt.Errorf("story %q has empty files_owned; cannot synthesize DAG", story.ID)
	}

	var scenarioIDs []string
	var storyScenarios []workflow.Scenario
	if plan != nil {
		storyScenarios = plan.ScenariosForStory(story.ID)
		for _, sc := range storyScenarios {
			scenarioIDs = append(scenarioIDs, sc.ID)
		}
	}

	// Issue #90: front-load ADR-041 mechanical binding requirements
	// (tier tag, harness profile string literal, env var consumption,
	// required assertions) into the dev's task prompt. Sarah's authored
	// task description is intentionally short; smoke 9 showed the dev
	// burned multiple TDD cycles re-discovering the bindings via reviewer
	// feedback even though the data is already on the scenarios (after
	// issue #89 denormalized it from the catalog). The binding block
	// returns "" for @unit-only stories so this is purely additive.
	bindingBlock := buildBindingContextBlock(storyScenarios)

	files := append([]string(nil), story.FilesOwned...)
	nodes := make([]TaskNode, 0, len(story.Tasks))
	for _, t := range story.Tasks {
		prompt := t.Description
		if bindingBlock != "" {
			prompt = prompt + "\n\n---\n\n" + bindingBlock
		}
		nodes = append(nodes, TaskNode{
			ID:          t.ID,
			Prompt:      prompt,
			Role:        "developer",
			DependsOn:   append([]string(nil), t.DependsOn...),
			FileScope:   files,
			ScenarioIDs: append([]string(nil), scenarioIDs...),
		})
	}

	dag := &TaskDAG{Nodes: nodes}
	if err := dag.Validate(); err != nil {
		return nil, fmt.Errorf("synthesized DAG fails validation: %w", err)
	}
	return dag, nil
}

// topoSortStoryIDs returns the Story.IDs ordered so that every Story's
// DependsOn entries appear before it. ADR-043 PR 4h: the requirement-
// executor consumes this order to dispatch Stories sequentially.
//
// Scope: this function operates on a single requirement's Stories. Cross-
// requirement Story.DependsOn entries (Sarah-authored references to
// Stories on OTHER requirements) are intentionally ignored — that
// ordering is enforced upstream by Requirement.DependsOn at the
// scenario-orchestrator level, which already serializes requirement
// dispatch on the inter-requirement graph. Smoke 6 (2026-06-01
// mavlink-hard) surfaced this when Sarah produced Story.DependsOn that
// mirrored the Requirement.DependsOn graph — semantically redundant but
// the early implementation treated every unknown ID as a fatal error,
// rejecting 3 of 5 requirements.
//
// Cycles within the local set are still an upstream planning bug
// (plan-reviewer R3 catches them); returning an error here is the
// fail-loudly path that surfaces a regression as a synthesis error
// rather than silently flattening into non-deterministic order.
//
// Typo'd Story.DependsOn references (story IDs that don't exist anywhere
// in the plan) are caught upstream by `workflow.ValidateStoryDAG`, which
// runs against the full plan Stories list before persistence. We can
// therefore trust that any DependsOn id absent from the local slice is
// a legitimate cross-requirement reference, not a typo.
func topoSortStoryIDs(stories []workflow.Story) ([]string, error) {
	if len(stories) == 0 {
		return nil, nil
	}

	idIndex := make(map[string]struct{}, len(stories))
	deps := make(map[string][]string, len(stories))
	for _, s := range stories {
		if _, dup := idIndex[s.ID]; dup {
			return nil, fmt.Errorf("duplicate story ID %q in requirement", s.ID)
		}
		idIndex[s.ID] = struct{}{}
		deps[s.ID] = append([]string(nil), s.DependsOn...)
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(stories))
	var sorted []string

	var visit func(id string) error
	visit = func(id string) error {
		switch color[id] {
		case gray:
			return fmt.Errorf("story dependency cycle involves %q", id)
		case black:
			return nil
		}
		color[id] = gray
		for _, dep := range deps[id] {
			if _, ok := idIndex[dep]; !ok {
				// Cross-requirement reference. Skip — Requirement.DependsOn
				// already serializes inter-requirement dispatch upstream.
				continue
			}
			if err := visit(dep); err != nil {
				return err
			}
		}
		color[id] = black
		sorted = append(sorted, id)
		return nil
	}

	for _, s := range stories {
		if color[s.ID] == white {
			if err := visit(s.ID); err != nil {
				return nil, err
			}
		}
	}
	return sorted, nil
}
