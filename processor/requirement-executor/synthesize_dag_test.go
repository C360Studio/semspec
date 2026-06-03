package requirementexecutor

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestSynthesizeTaskDAGForStory_LinearTasks(t *testing.T) {
	plan := &workflow.Plan{
		Slug:         "x",
		Requirements: []workflow.Requirement{{ID: "req.x.1"}},
		Scenarios: []workflow.Scenario{
			{ID: "scen.1", RequirementID: "req.x.1", StoryID: "story.x.1.1"},
		},
	}
	story := workflow.Story{
		ID: "story.x.1.1", RequirementIDs: []string{"req.x.1"}, ComponentName: "placeholder-component",
		FilesOwned: []string{"src/x.go"},
		Tasks: []workflow.Task{
			{ID: "task.x.1.1.1", StoryID: "story.x.1.1", Description: "tests"},
			{ID: "task.x.1.1.2", StoryID: "story.x.1.1", Description: "impl", DependsOn: []string{"task.x.1.1.1"}},
			{ID: "task.x.1.1.3", StoryID: "story.x.1.1", Description: "verify", DependsOn: []string{"task.x.1.1.2"}},
		},
	}
	dag, err := synthesizeTaskDAGForStory(plan, story)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if dag == nil {
		t.Fatal("expected non-nil DAG")
	}
	if len(dag.Nodes) != 3 {
		t.Fatalf("want 3 nodes, got %d", len(dag.Nodes))
	}
	if len(dag.Nodes[0].DependsOn) != 0 {
		t.Errorf("entry task should have empty DependsOn, got %v", dag.Nodes[0].DependsOn)
	}
	if len(dag.Nodes[1].DependsOn) != 1 || dag.Nodes[1].DependsOn[0] != "task.x.1.1.1" {
		t.Errorf("node[1].DependsOn = %v, want [task.x.1.1.1]", dag.Nodes[1].DependsOn)
	}
	for i, n := range dag.Nodes {
		if len(n.FileScope) != 1 || n.FileScope[0] != "src/x.go" {
			t.Errorf("node[%d].FileScope = %v, want [src/x.go]", i, n.FileScope)
		}
		if len(n.ScenarioIDs) != 1 || n.ScenarioIDs[0] != "scen.1" {
			t.Errorf("node[%d].ScenarioIDs = %v, want [scen.1]", i, n.ScenarioIDs)
		}
		if n.Role != "developer" {
			t.Errorf("node[%d].Role = %q, want developer", i, n.Role)
		}
	}
}

func TestSynthesizeTaskDAGForStory_PerStoryScopeNoCrossEdges(t *testing.T) {
	// ADR-043 PR 4h: per-Story synthesis carries NO cross-Story DependsOn.
	// Sequencing of Story B after Story A is the caller's job (topo-sort
	// of Story.DependsOn). The DAG for B contains only B's own nodes.
	plan := &workflow.Plan{
		Slug:         "x",
		Requirements: []workflow.Requirement{{ID: "req.x.1"}},
	}
	storyB := workflow.Story{
		ID: "story.x.1.2", RequirementIDs: []string{"req.x.1"}, ComponentName: "placeholder-component-2",
		FilesOwned: []string{"src/b.go"},
		DependsOn:  []string{"story.x.1.1"}, // signals outer ordering — NOT a node edge
		Tasks: []workflow.Task{
			{ID: "task.x.1.2.1", StoryID: "story.x.1.2", Description: "B1"},
		},
	}
	dag, err := synthesizeTaskDAGForStory(plan, storyB)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(dag.Nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(dag.Nodes))
	}
	if len(dag.Nodes[0].DependsOn) != 0 {
		t.Errorf("entry task DependsOn must be empty — cross-Story edges live on Story.DependsOn, not on the synthesized DAG; got %v", dag.Nodes[0].DependsOn)
	}
}

func TestSynthesizeTaskDAGForStory_EmptyTasksReturnsError(t *testing.T) {
	story := workflow.Story{
		ID:         "story.x.1.1",
		FilesOwned: []string{"src/x.go"},
		Tasks:      nil,
	}
	_, err := synthesizeTaskDAGForStory(nil, story)
	if err == nil {
		t.Fatal("expected error for story with no tasks")
	}
	if !strings.Contains(err.Error(), "no tasks") {
		t.Errorf("error %q missing phrase 'no tasks'", err.Error())
	}
}

func TestSynthesizeTaskDAGForStory_EmptyFilesOwnedReturnsError(t *testing.T) {
	story := workflow.Story{
		ID:         "story.x.1.1",
		FilesOwned: nil,
		Tasks: []workflow.Task{
			{ID: "task.x.1.1.1", Description: "tests"},
		},
	}
	_, err := synthesizeTaskDAGForStory(nil, story)
	if err == nil {
		t.Fatal("expected error for story with empty files_owned")
	}
	if !strings.Contains(err.Error(), "empty files_owned") {
		t.Errorf("error %q missing phrase 'empty files_owned'", err.Error())
	}
}

func TestSynthesizeTaskDAGForStory_NilPlanOK(t *testing.T) {
	// When plan is nil we can't look up scenarios — node.ScenarioIDs
	// stays empty but synthesis still succeeds (the dispatcher would
	// have already validated the plan exists).
	story := workflow.Story{
		ID:         "story.x.1.1",
		FilesOwned: []string{"src/x.go"},
		Tasks:      []workflow.Task{{ID: "task.x.1.1.1", Description: "tests"}},
	}
	dag, err := synthesizeTaskDAGForStory(nil, story)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(dag.Nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(dag.Nodes))
	}
	if len(dag.Nodes[0].ScenarioIDs) != 0 {
		t.Errorf("nil plan should produce empty ScenarioIDs, got %v", dag.Nodes[0].ScenarioIDs)
	}
}

func TestTopoSortStoryIDs_Linear(t *testing.T) {
	stories := []workflow.Story{
		{ID: "s2", DependsOn: []string{"s1"}},
		{ID: "s1"},
		{ID: "s3", DependsOn: []string{"s2"}},
	}
	sorted, err := topoSortStoryIDs(stories)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := []string{"s1", "s2", "s3"}
	if len(sorted) != 3 {
		t.Fatalf("len = %d, want 3", len(sorted))
	}
	for i, w := range want {
		if sorted[i] != w {
			t.Errorf("sorted[%d] = %q, want %q", i, sorted[i], w)
		}
	}
}

func TestTopoSortStoryIDs_CycleErrors(t *testing.T) {
	stories := []workflow.Story{
		{ID: "s1", DependsOn: []string{"s2"}},
		{ID: "s2", DependsOn: []string{"s1"}},
	}
	_, err := topoSortStoryIDs(stories)
	if err == nil {
		t.Fatal("expected cycle error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error %q missing 'cycle'", err.Error())
	}
}

// TestTopoSortStoryIDs_CrossRequirementDependsOnSilentlyDropped pins the
// smoke-6 regression: when Sarah authors Story.DependsOn referencing a
// Story on another requirement (e.g. story.x.2.1 depends_on=[story.x.1.1]
// where story.x.1.1 belongs to req 1), the topo-sort over req 2's
// Stories must NOT treat that cross-requirement reference as a fatal
// "unknown story" error. Cross-requirement ordering is enforced upstream
// by Requirement.DependsOn at the scenario-orchestrator; the topo-sort's
// job is intra-requirement ordering only.
//
// Smoke 6 (2026-06-01 mavlink-hard) hit this when Sarah produced
// Story.DependsOn that exactly mirrored the Requirement.DependsOn graph
// — semantically redundant. Pre-fix, this rejected 3 of 5 requirements
// with "topo-sort stories failed: story ... depends on unknown story ...".
func TestTopoSortStoryIDs_CrossRequirementDependsOnSilentlyDropped(t *testing.T) {
	// Simulate req 2's story slice. The lone story has a DependsOn that
	// points at a story belonging to req 1 (not in this slice).
	stories := []workflow.Story{
		{ID: "story.x.2.1", DependsOn: []string{"story.x.1.1"}},
	}
	got, err := topoSortStoryIDs(stories)
	if err != nil {
		t.Fatalf("cross-requirement DependsOn must NOT error; got: %v", err)
	}
	if len(got) != 1 || got[0] != "story.x.2.1" {
		t.Errorf("got %v, want [story.x.2.1]", got)
	}
}

// TestTopoSortStoryIDs_MixedLocalAndCrossReqDepsRetainsLocalOrder pins
// that local DependsOn is still honored when cross-req refs are mixed
// in. The local ordering wins; cross-req refs are dropped silently.
func TestTopoSortStoryIDs_MixedLocalAndCrossReqDepsRetainsLocalOrder(t *testing.T) {
	stories := []workflow.Story{
		{ID: "story.x.2.2", DependsOn: []string{"story.x.2.1", "story.x.1.1"}}, // local + cross-req
		{ID: "story.x.2.1", DependsOn: []string{"story.x.1.1"}},                // cross-req only
	}
	got, err := topoSortStoryIDs(stories)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// Local dep wins: 2.1 must precede 2.2. Cross-req refs ignored.
	want := []string{"story.x.2.1", "story.x.2.2"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestTopoSortStoryIDs_Empty(t *testing.T) {
	sorted, err := topoSortStoryIDs(nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if sorted != nil {
		t.Errorf("expected nil sorted, got %v", sorted)
	}
}

func TestTopoSortStoryIDs_DuplicateIDErrors(t *testing.T) {
	stories := []workflow.Story{
		{ID: "s1"},
		{ID: "s1"},
	}
	_, err := topoSortStoryIDs(stories)
	if err == nil {
		t.Fatal("expected duplicate-ID error")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error %q missing 'duplicate'", err.Error())
	}
}
