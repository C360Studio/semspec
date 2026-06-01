package requirementexecutor

import (
	"fmt"

	"github.com/c360studio/semspec/workflow"
)

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
	if plan != nil {
		for _, sc := range plan.ScenariosForStory(story.ID) {
			scenarioIDs = append(scenarioIDs, sc.ID)
		}
	}

	files := append([]string(nil), story.FilesOwned...)
	nodes := make([]TaskNode, 0, len(story.Tasks))
	for _, t := range story.Tasks {
		nodes = append(nodes, TaskNode{
			ID:          t.ID,
			Prompt:      t.Description,
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
// Cycles are an upstream planning bug (plan-reviewer R3
// `story.depends_on_cycle` catches them); returning an error here is the
// fail-loudly path that surfaces a planning regression as a synthesis
// error rather than silently flattening into a non-deterministic order.
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
				return fmt.Errorf("story %q depends on unknown story %q", id, dep)
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
