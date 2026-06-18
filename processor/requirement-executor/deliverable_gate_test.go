package requirementexecutor

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/c360studio/semspec/tools/sandbox"
	"github.com/c360studio/semspec/workflow"
)

func TestStoryScopeCreateObligations_IntersectsPlanCreateWithStoryFilesOwned(t *testing.T) {
	plan := &workflow.Plan{
		Scope: workflow.Scope{Create: []string{
			"./src/present.go",
			"src/missing.go",
			"src/other.go",
			"src/missing.go",
			"../escape.go",
		}},
	}
	story := workflow.Story{
		ID:         "story.demo.1",
		FilesOwned: []string{"src/present.go", "./src/missing.go", "docs/readme.md"},
	}

	got := storyScopeCreateObligations(plan, story)
	want := []string{"src/present.go", "src/missing.go"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("storyScopeCreateObligations() = %v, want %v", got, want)
	}
}

func TestRequirementScopeCreateObligations_UsesAllRequirementStories(t *testing.T) {
	plan := &workflow.Plan{
		Scope: workflow.Scope{Create: []string{"src/a.go", "src/b.go", "src/c.go"}},
		Stories: []workflow.Story{
			{ID: "story.demo.1", FilesOwned: []string{"src/a.go"}},
			{ID: "story.demo.2", FilesOwned: []string{"src/b.go"}},
			{ID: "story.demo.other", FilesOwned: []string{"src/c.go"}},
		},
	}

	got := requirementScopeCreateObligations(plan, []string{"story.demo.1", "story.demo.2"})
	want := []string{"src/a.go", "src/b.go"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("requirementScopeCreateObligations() = %v, want %v", got, want)
	}
}

func TestMissingDeliverableFiles_NormalizesAndDedupes(t *testing.T) {
	got := missingDeliverableFiles(
		[]string{"./src/a.go", "src/b.go", "src/b.go", "../escape.go"},
		[]string{"src/a.go", "src/c.go"},
	)
	want := []string{"src/b.go"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("missingDeliverableFiles() = %v, want %v", got, want)
	}
}

func TestRequirementDeliverableGate_FailsBeforeTerminalCompleteWhenMissing(t *testing.T) {
	c := newTestComponent(t)
	c.sandbox = &stubSandbox{
		execByTaskID: map[string]*sandbox.ExecResult{
			"reviewer-1": {Stdout: "src/present.go\x00", ExitCode: 0},
		},
	}
	exec := deliverableGateExec("reviewer-1")
	plan := deliverableGatePlan()

	exec.mu.Lock()
	handled := c.handleRequirementDeliverableGapForPlanLocked(context.Background(), exec, plan)
	exec.mu.Unlock()

	if !handled {
		t.Fatal("requirement deliverable gap was not handled")
	}
	if !exec.terminated {
		t.Fatal("exec.terminated = false, want failed terminal state")
	}
	if c.requirementsCompleted.Load() != 0 {
		t.Fatalf("requirementsCompleted = %d, want 0", c.requirementsCompleted.Load())
	}
	if c.requirementsFailed.Load() != 1 {
		t.Fatalf("requirementsFailed = %d, want 1", c.requirementsFailed.Load())
	}
}

func TestApprovedDeliverableGate_RetriesCurrentStoryWhenScopeCreateMissing(t *testing.T) {
	c := newTestComponent(t)
	c.sandbox = &stubSandbox{
		execByTaskID: map[string]*sandbox.ExecResult{
			"reviewer-1": {Stdout: "src/present.go\x00", ExitCode: 0},
		},
	}
	exec := deliverableGateExec("reviewer-1")
	plan := deliverableGatePlan()

	exec.mu.Lock()
	handled := c.handleApprovedDeliverableGapForPlanLocked(context.Background(), exec, plan)
	exec.mu.Unlock()

	if !handled {
		t.Fatal("deliverable gap was not handled")
	}
	if exec.RetryCount != 1 {
		t.Fatalf("RetryCount = %d, want 1", exec.RetryCount)
	}
	if !strings.Contains(exec.LastReviewFeedback, "src/missing.go") {
		t.Fatalf("LastReviewFeedback = %q, want missing path", exec.LastReviewFeedback)
	}
	if len(exec.VisitedNodes) != 0 {
		t.Fatalf("VisitedNodes = %v, want reset for Story retry", exec.VisitedNodes)
	}
	if len(exec.NodeResults) != 0 {
		t.Fatalf("NodeResults = %v, want current Story evidence trimmed before retry", exec.NodeResults)
	}
	if c.requirementsCompleted.Load() != 0 {
		t.Fatalf("requirementsCompleted = %d, want 0", c.requirementsCompleted.Load())
	}
}

func TestApprovedDeliverableGate_AllowsCleanStory(t *testing.T) {
	c := newTestComponent(t)
	c.sandbox = &stubSandbox{
		execByTaskID: map[string]*sandbox.ExecResult{
			"reviewer-1": {Stdout: "src/present.go\x00src/missing.go\x00", ExitCode: 0},
		},
	}
	exec := deliverableGateExec("reviewer-1")
	plan := deliverableGatePlan()

	exec.mu.Lock()
	handled := c.handleApprovedDeliverableGapForPlanLocked(context.Background(), exec, plan)
	exec.mu.Unlock()

	if handled {
		t.Fatal("clean deliverable set should not intercept approval")
	}
	if exec.RetryCount != 0 {
		t.Fatalf("RetryCount = %d, want 0", exec.RetryCount)
	}
}

func TestApprovedDeliverableGate_FailsClosedWhenBranchCannotBeInspected(t *testing.T) {
	c := newTestComponent(t)
	c.sandbox = &stubSandbox{execErr: fmt.Errorf("sandbox unavailable")}
	exec := deliverableGateExec("reviewer-1")
	plan := deliverableGatePlan()

	exec.mu.Lock()
	handled := c.handleApprovedDeliverableGapForPlanLocked(context.Background(), exec, plan)
	exec.mu.Unlock()

	if !handled {
		t.Fatal("inspection failure was not handled")
	}
	if !exec.terminated {
		t.Fatal("exec.terminated = false, want failed terminal state")
	}
	if c.requirementsFailed.Load() != 1 {
		t.Fatalf("requirementsFailed = %d, want 1", c.requirementsFailed.Load())
	}
}

func deliverableGateExec(reviewerTaskID string) *requirementExecution {
	return &requirementExecution{
		EntityID:        "semspec.local.exec.req.run.demo-req-1",
		Slug:            "demo",
		RequirementID:   "req.demo.1",
		ReviewerTaskID:  reviewerTaskID,
		SortedStoryIDs:  []string{"story.demo.1"},
		CurrentStoryIdx: 0,
		SortedNodeIDs:   []string{"task.demo.1"},
		VisitedNodes:    map[string]bool{"task.demo.1": true},
		NodeResults: []NodeResult{
			{NodeID: "task.demo.1", FilesModified: []string{"src/present.go"}, CommitSHA: "abc123"},
		},
		CurrentNodeIdx: 0,
		MaxRetries:     2,
	}
}

func deliverableGatePlan() *workflow.Plan {
	return &workflow.Plan{
		Slug:  "demo",
		Scope: workflow.Scope{Create: []string{"src/present.go", "src/missing.go", "src/other.go"}},
		Stories: []workflow.Story{
			{
				ID:             "story.demo.1",
				RequirementIDs: []string{"req.demo.1"},
				FilesOwned:     []string{"src/present.go", "src/missing.go"},
			},
		},
	}
}
