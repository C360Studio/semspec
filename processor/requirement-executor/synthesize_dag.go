package requirementexecutor

import (
	"fmt"

	"github.com/c360studio/semspec/workflow"
)

// synthesizeTaskDAGFromStories converts Sarah's Stories (ADR-043 Move 3 +
// PR 4e positional shape, persisted by plan-manager from
// StoriesGeneratedEvent) into a TaskDAG that the
// requirement-executor's existing downstream consumes unchanged. This is
// the PR 4f decomposer-bypass: when the plan carries Sarah-authored
// Stories for the requirement, the LLM decomposer call is skipped — the
// DAG comes directly from the structured Story.Tasks.
//
// Mapping:
//   - Each Task in each Story for the requirement becomes one TaskNode.
//   - TaskNode.ID = Task.ID (canonical task.<slug>.<reqseq>.<storyseq>.<taskseq>).
//   - TaskNode.Prompt = Task.Description (Sarah's 1-line intent).
//   - TaskNode.Role = "developer" (matches the decomposer's default role
//     for code-producing tasks; future ADR-043 work may differentiate
//     review / qa nodes here).
//   - TaskNode.DependsOn = intra-story Task.DependsOn (canonical IDs),
//     extended on entry tasks (Tasks with empty DependsOn) with the exit
//     tasks of cross-story prereqs from Story.DependsOn.
//   - TaskNode.FileScope = parent Story.FilesOwned.
//   - TaskNode.ScenarioIDs = IDs of scenarios whose StoryID == this story
//     (populated by Bob's attachStoryIDs in PR 4b; empty for legacy plans).
//
// Returns (nil, nil) when no Stories own the requirement — signals the
// caller to fall back to the legacy decomposer-LLM dispatch path.
// Returns (nil, err) when the synthesis produces a structurally invalid
// DAG (caller surfaces as a parse-style failure).
func synthesizeTaskDAGFromStories(plan *workflow.Plan, requirementID string) (*TaskDAG, error) {
	if plan == nil || requirementID == "" {
		return nil, nil
	}
	stories := plan.StoriesForRequirement(requirementID)
	if len(stories) == 0 {
		return nil, nil
	}

	// Pre-compute per-story exit tasks (tasks no other task in the same
	// story depends on) so cross-story DependsOn can expand to concrete
	// node-level prereqs.
	exitTasksByStoryID := storyExitTasks(stories)

	nodes := make([]TaskNode, 0)
	for _, s := range stories {
		if len(s.Tasks) == 0 {
			// Empty Tasks on a story is a Sarah readiness-gate violation —
			// caught upstream by workflow.ValidateStories. Surface as a
			// synthesis error here so the caller knows the bypass can't
			// proceed.
			return nil, fmt.Errorf("story %q has no tasks; cannot synthesize DAG", s.ID)
		}
		if len(s.FilesOwned) == 0 {
			return nil, fmt.Errorf("story %q has empty files_owned; cannot synthesize DAG (Sarah's gate should have caught this)", s.ID)
		}

		// Collect cross-story prereqs: exit tasks of every story this story
		// depends on. Entry tasks (Task.DependsOn empty) inherit them.
		var crossPrereqs []string
		for _, depStoryID := range s.DependsOn {
			crossPrereqs = append(crossPrereqs, exitTasksByStoryID[depStoryID]...)
		}

		// ScenarioIDs come from the plan's scenarios whose StoryID matches.
		var scenarioIDs []string
		for _, sc := range plan.ScenariosForStory(s.ID) {
			scenarioIDs = append(scenarioIDs, sc.ID)
		}

		// FileScope: clone Story.FilesOwned (each TaskNode gets the
		// full story scope; per-task file partitioning is a refinement
		// PR 4g+ may introduce).
		files := append([]string(nil), s.FilesOwned...)

		for _, t := range s.Tasks {
			deps := append([]string(nil), t.DependsOn...)
			if len(t.DependsOn) == 0 && len(crossPrereqs) > 0 {
				// Entry task — inherit cross-story prereqs.
				deps = append(deps, crossPrereqs...)
			}
			nodes = append(nodes, TaskNode{
				ID:          t.ID,
				Prompt:      t.Description,
				Role:        "developer",
				DependsOn:   deps,
				FileScope:   files,
				ScenarioIDs: append([]string(nil), scenarioIDs...),
			})
		}
	}

	if len(nodes) == 0 {
		// Should be unreachable given the per-story empty-Tasks guard above,
		// but defensive — the caller fallback path is well-tested.
		return nil, nil
	}

	dag := &TaskDAG{Nodes: nodes}
	if err := dag.Validate(); err != nil {
		return nil, fmt.Errorf("synthesized DAG fails validation: %w", err)
	}
	return dag, nil
}

// storyExitTasks returns a map from Story.ID to the list of Task.IDs that
// no other task in the same story depends on (the "exit" tasks). A
// cross-story DependsOn edge expands to "the exit tasks of the prereq
// story" so the consuming story waits on the actual terminal work, not
// intermediate steps.
func storyExitTasks(stories []workflow.Story) map[string][]string {
	out := make(map[string][]string, len(stories))
	for _, s := range stories {
		if len(s.Tasks) == 0 {
			continue
		}
		// Build a set of task IDs that ARE depended on by some other task
		// in the same story.
		dependedOn := make(map[string]struct{}, len(s.Tasks))
		for _, t := range s.Tasks {
			for _, dep := range t.DependsOn {
				dependedOn[dep] = struct{}{}
			}
		}
		var exits []string
		for _, t := range s.Tasks {
			if _, ok := dependedOn[t.ID]; !ok {
				exits = append(exits, t.ID)
			}
		}
		out[s.ID] = exits
	}
	return out
}
