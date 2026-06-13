package scenarioorchestrator

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/c360studio/semspec/tools/sandbox"
	"github.com/c360studio/semspec/workflow"
)

// stubMergeSandbox records MergeBranches calls and returns a canned result so
// resolveRequirementBase's >1-prereq path is exercised without a real sandbox.
type stubMergeSandbox struct {
	calls  []sandbox.MergeBranchesRequest
	result *sandbox.MergeBranchesResult
	err    error
}

func (s *stubMergeSandbox) MergeBranches(_ context.Context, req sandbox.MergeBranchesRequest) (*sandbox.MergeBranchesResult, error) {
	s.calls = append(s.calls, req)
	if s.err != nil {
		return nil, s.err
	}
	if s.result != nil {
		return s.result, nil
	}
	return &sandbox.MergeBranchesResult{Status: "merged", Target: req.Target}, nil
}

// TestResolveRequirementBase_RootForksFromPlanBase: a requirement with no
// branch prerequisites derives from the plan base (empty => executor HEAD),
// and never touches the sandbox.
func TestResolveRequirementBase_RootForksFromPlanBase(t *testing.T) {
	stub := &stubMergeSandbox{}
	c := &Component{sandbox: stub}

	got, err := c.resolveRequirementBase(context.Background(),
		workflow.Requirement{ID: "a1"}, nil, "semspec/plan-demo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "semspec/plan-demo" {
		t.Errorf("base = %q, want plan base semspec/plan-demo", got)
	}
	if len(stub.calls) != 0 {
		t.Errorf("root requirement must not merge; got %d MergeBranches calls", len(stub.calls))
	}
}

// TestResolveRequirementBase_SingleStoryEdgeForksFromOwner is design §9 test D:
// story-B depends on story-A (a Pass-2 shared-file edge that never reaches
// Requirement.DependsOn). req b1's branch must derive from story-A's OWNER
// branch (a1) via a pure fork — no reqbase merge.
func TestResolveRequirementBase_SingleStoryEdgeForksFromOwner(t *testing.T) {
	stub := &stubMergeSandbox{}
	c := &Component{sandbox: stub}

	stories := []workflow.Story{
		{ID: "story.A", RequirementIDs: []string{"a1", "a2"}},
		{ID: "story.B", RequirementIDs: []string{"b1"}, DependsOn: []string{"story.A"}},
	}
	got, err := c.resolveRequirementBase(context.Background(),
		workflow.Requirement{ID: "b1"}, stories, "semspec/plan-demo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "semspec/requirement-a1" {
		t.Errorf("base = %q, want owner branch semspec/requirement-a1", got)
	}
	if len(stub.calls) != 0 {
		t.Errorf("single prereq must fork directly, not merge; got %d calls", len(stub.calls))
	}
}

// TestResolveRequirementBase_MultiplePrereqsMergeToReqbase is design §9 test E:
// >1 prerequisite owner branches merge deterministically (sorted) into
// semspec/reqbase-<id>, and that becomes the base.
func TestResolveRequirementBase_MultiplePrereqsMergeToReqbase(t *testing.T) {
	stub := &stubMergeSandbox{
		result: &sandbox.MergeBranchesResult{Status: "merged", Target: "semspec/reqbase-d1"},
	}
	c := &Component{sandbox: stub}

	// d1 depends on c1 and a1 (both their own owners). Self ID excluded.
	stories := []workflow.Story{
		{ID: "story.A", RequirementIDs: []string{"a1"}},
		{ID: "story.C", RequirementIDs: []string{"c1"}},
		{ID: "story.D", RequirementIDs: []string{"d1"}},
	}
	req := workflow.Requirement{ID: "d1", DependsOn: []string{"c1", "a1"}}

	got, err := c.resolveRequirementBase(context.Background(), req, stories, "semspec/plan-demo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "semspec/reqbase-d1" {
		t.Errorf("base = %q, want semspec/reqbase-d1", got)
	}
	if len(stub.calls) != 1 {
		t.Fatalf("want exactly 1 MergeBranches call, got %d", len(stub.calls))
	}
	call := stub.calls[0]
	if call.Target != "semspec/reqbase-d1" {
		t.Errorf("merge target = %q, want semspec/reqbase-d1", call.Target)
	}
	if call.Base != "semspec/plan-demo" {
		t.Errorf("merge base = %q, want plan base semspec/plan-demo", call.Base)
	}
	wantBranches := []string{"semspec/requirement-a1", "semspec/requirement-c1"}
	if !reflect.DeepEqual(call.Branches, wantBranches) {
		t.Errorf("merge branches = %v, want sorted %v", call.Branches, wantBranches)
	}
}

// TestResolveRequirementBase_MultiplePrereqsNoSandboxFailsLoud: a >1-prereq
// requirement with no sandbox is a misconfiguration — fail loud rather than
// silently fork from the plan base and re-introduce the assembly conflict.
func TestResolveRequirementBase_MultiplePrereqsNoSandboxFailsLoud(t *testing.T) {
	c := &Component{sandbox: nil}
	stories := []workflow.Story{
		{ID: "story.A", RequirementIDs: []string{"a1"}},
		{ID: "story.C", RequirementIDs: []string{"c1"}},
		{ID: "story.D", RequirementIDs: []string{"d1"}},
	}
	req := workflow.Requirement{ID: "d1", DependsOn: []string{"c1", "a1"}}

	_, err := c.resolveRequirementBase(context.Background(), req, stories, "semspec/plan-demo")
	if err == nil {
		t.Fatal("expected error when merging >1 prereq without a sandbox, got nil")
	}
}

// TestResolveRequirementBase_MergeErrorPropagates: a sandbox merge conflict /
// failure surfaces as an error so dispatch does not proceed against a bad base.
func TestResolveRequirementBase_MergeErrorPropagates(t *testing.T) {
	stub := &stubMergeSandbox{err: errors.New("boom")}
	c := &Component{sandbox: stub}
	stories := []workflow.Story{
		{ID: "story.A", RequirementIDs: []string{"a1"}},
		{ID: "story.C", RequirementIDs: []string{"c1"}},
		{ID: "story.D", RequirementIDs: []string{"d1"}},
	}
	req := workflow.Requirement{ID: "d1", DependsOn: []string{"c1", "a1"}}

	if _, err := c.resolveRequirementBase(context.Background(), req, stories, ""); err == nil {
		t.Fatal("expected merge error to propagate, got nil")
	}
}
