package requirementexecutor

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestSynthesizeTaskDAGFromStories_NoStoriesReturnsNil(t *testing.T) {
	plan := &workflow.Plan{Slug: "x", Requirements: []workflow.Requirement{{ID: "req.x.1"}}}
	dag, err := synthesizeTaskDAGFromStories(plan, "req.x.1")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if dag != nil {
		t.Errorf("expected nil DAG (fallthrough signal), got %+v", dag)
	}
}

func TestSynthesizeTaskDAGFromStories_NilPlanReturnsNil(t *testing.T) {
	dag, err := synthesizeTaskDAGFromStories(nil, "req.x.1")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if dag != nil {
		t.Errorf("expected nil DAG, got %+v", dag)
	}
}

func TestSynthesizeTaskDAGFromStories_SingleStoryLinearTasks(t *testing.T) {
	plan := &workflow.Plan{
		Slug:         "x",
		Requirements: []workflow.Requirement{{ID: "req.x.1"}},
		Stories: []workflow.Story{
			{
				ID: "story.x.1.1", RequirementID: "req.x.1",
				FilesOwned: []string{"src/x.go"},
				Tasks: []workflow.Task{
					{ID: "task.x.1.1.1", StoryID: "story.x.1.1", Description: "tests"},
					{ID: "task.x.1.1.2", StoryID: "story.x.1.1", Description: "impl", DependsOn: []string{"task.x.1.1.1"}},
					{ID: "task.x.1.1.3", StoryID: "story.x.1.1", Description: "verify", DependsOn: []string{"task.x.1.1.2"}},
				},
			},
		},
		Scenarios: []workflow.Scenario{
			{ID: "scen.1", RequirementID: "req.x.1", StoryID: "story.x.1.1"},
		},
	}
	dag, err := synthesizeTaskDAGFromStories(plan, "req.x.1")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if dag == nil {
		t.Fatal("expected non-nil DAG")
	}
	if len(dag.Nodes) != 3 {
		t.Fatalf("want 3 nodes, got %d", len(dag.Nodes))
	}
	// Node 0 is entry (no DependsOn).
	if len(dag.Nodes[0].DependsOn) != 0 {
		t.Errorf("entry task should have empty DependsOn, got %v", dag.Nodes[0].DependsOn)
	}
	// Node 1 depends on node 0.
	if len(dag.Nodes[1].DependsOn) != 1 || dag.Nodes[1].DependsOn[0] != "task.x.1.1.1" {
		t.Errorf("node[1].DependsOn = %v, want [task.x.1.1.1]", dag.Nodes[1].DependsOn)
	}
	// FileScope = Story.FilesOwned.
	for i, n := range dag.Nodes {
		if len(n.FileScope) != 1 || n.FileScope[0] != "src/x.go" {
			t.Errorf("node[%d].FileScope = %v, want [src/x.go]", i, n.FileScope)
		}
	}
	// ScenarioIDs from plan.ScenariosForStory.
	for i, n := range dag.Nodes {
		if len(n.ScenarioIDs) != 1 || n.ScenarioIDs[0] != "scen.1" {
			t.Errorf("node[%d].ScenarioIDs = %v, want [scen.1]", i, n.ScenarioIDs)
		}
	}
	// Role assigned to "developer".
	if dag.Nodes[0].Role != "developer" {
		t.Errorf("Role = %q, want developer", dag.Nodes[0].Role)
	}
}

func TestSynthesizeTaskDAGFromStories_CrossStoryDependsOnExpansion(t *testing.T) {
	// Story B depends on story A. The entry task(s) of B should inherit
	// the exit task(s) of A as DependsOn.
	plan := &workflow.Plan{
		Slug:         "x",
		Requirements: []workflow.Requirement{{ID: "req.x.1"}},
		Stories: []workflow.Story{
			{
				ID: "story.x.1.1", RequirementID: "req.x.1",
				FilesOwned: []string{"src/a.go"},
				Tasks: []workflow.Task{
					{ID: "task.x.1.1.1", StoryID: "story.x.1.1", Description: "A1"},
					{ID: "task.x.1.1.2", StoryID: "story.x.1.1", Description: "A2", DependsOn: []string{"task.x.1.1.1"}},
				},
			},
			{
				ID: "story.x.1.2", RequirementID: "req.x.1",
				FilesOwned: []string{"src/b.go"},
				DependsOn:  []string{"story.x.1.1"},
				Tasks: []workflow.Task{
					{ID: "task.x.1.2.1", StoryID: "story.x.1.2", Description: "B1"},
				},
			},
		},
	}
	dag, err := synthesizeTaskDAGFromStories(plan, "req.x.1")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// Find B1 node.
	var b1 *struct {
		ID, Prompt string
		DependsOn  []string
	}
	for _, n := range dag.Nodes {
		if n.ID == "task.x.1.2.1" {
			b1 = &struct {
				ID, Prompt string
				DependsOn  []string
			}{n.ID, n.Prompt, n.DependsOn}
		}
	}
	if b1 == nil {
		t.Fatal("did not find task.x.1.2.1")
	}
	// B1 had no intra-story DependsOn; cross-story expansion adds A's exit (task.x.1.1.2, the last task in A's chain).
	if len(b1.DependsOn) != 1 || b1.DependsOn[0] != "task.x.1.1.2" {
		t.Errorf("B1.DependsOn = %v, want [task.x.1.1.2] (A's exit task)", b1.DependsOn)
	}
}

func TestSynthesizeTaskDAGFromStories_EmptyTasksReturnsError(t *testing.T) {
	plan := &workflow.Plan{
		Slug:         "x",
		Requirements: []workflow.Requirement{{ID: "req.x.1"}},
		Stories: []workflow.Story{
			{
				ID: "story.x.1.1", RequirementID: "req.x.1",
				FilesOwned: []string{"src/x.go"},
				Tasks:      nil,
			},
		},
	}
	_, err := synthesizeTaskDAGFromStories(plan, "req.x.1")
	if err == nil {
		t.Fatal("expected error for story with no tasks")
	}
	if !strings.Contains(err.Error(), "no tasks") {
		t.Errorf("error %q missing phrase 'no tasks'", err.Error())
	}
}

func TestSynthesizeTaskDAGFromStories_EmptyFilesOwnedReturnsError(t *testing.T) {
	plan := &workflow.Plan{
		Slug:         "x",
		Requirements: []workflow.Requirement{{ID: "req.x.1"}},
		Stories: []workflow.Story{
			{
				ID: "story.x.1.1", RequirementID: "req.x.1",
				FilesOwned: nil,
				Tasks: []workflow.Task{
					{ID: "task.x.1.1.1", StoryID: "story.x.1.1", Description: "tests"},
				},
			},
		},
	}
	_, err := synthesizeTaskDAGFromStories(plan, "req.x.1")
	if err == nil {
		t.Fatal("expected error for story with empty files_owned")
	}
	if !strings.Contains(err.Error(), "empty files_owned") {
		t.Errorf("error %q missing phrase 'empty files_owned'", err.Error())
	}
}

func TestSynthesizeTaskDAGFromStories_OtherRequirementSkipped(t *testing.T) {
	plan := &workflow.Plan{
		Slug:         "x",
		Requirements: []workflow.Requirement{{ID: "req.x.1"}, {ID: "req.x.2"}},
		Stories: []workflow.Story{
			{
				ID: "story.x.2.1", RequirementID: "req.x.2", // belongs to req 2
				FilesOwned: []string{"src/y.go"},
				Tasks:      []workflow.Task{{ID: "task.x.2.1.1", StoryID: "story.x.2.1", Description: "y"}},
			},
		},
	}
	dag, err := synthesizeTaskDAGFromStories(plan, "req.x.1")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if dag != nil {
		t.Errorf("expected nil DAG (no stories for req.x.1), got %+v", dag)
	}
}

func TestStoryExitTasks(t *testing.T) {
	stories := []workflow.Story{
		{
			ID: "s1",
			Tasks: []workflow.Task{
				{ID: "t1"},
				{ID: "t2", DependsOn: []string{"t1"}},
				{ID: "t3", DependsOn: []string{"t2"}},
				// t3 is the exit (nothing depends on it).
			},
		},
		{
			ID: "s2",
			Tasks: []workflow.Task{
				{ID: "u1"},
				// u1 has no dependents → exit.
			},
		},
		{
			ID:    "s3-empty",
			Tasks: nil, // skipped entirely
		},
	}
	got := storyExitTasks(stories)
	if exits, ok := got["s1"]; !ok || len(exits) != 1 || exits[0] != "t3" {
		t.Errorf("s1 exits = %v, want [t3]", exits)
	}
	if exits, ok := got["s2"]; !ok || len(exits) != 1 || exits[0] != "u1" {
		t.Errorf("s2 exits = %v, want [u1]", exits)
	}
	if _, ok := got["s3-empty"]; ok {
		t.Errorf("empty-tasks story s3-empty should not appear in exit map")
	}
}
