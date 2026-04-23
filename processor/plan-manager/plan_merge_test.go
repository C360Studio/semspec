package planmanager

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/c360studio/semspec/tools/sandbox"
	"github.com/c360studio/semspec/workflow"
)

// newTestComponentWithSandbox returns a plan-manager Component backed by the
// given stub sandbox URL. Tests avoid going through the full NewComponent
// factory because those paths wire NATS + KV + triple writers we don't
// need here.
func newTestComponentWithSandbox(t *testing.T, sandboxURL string) *Component {
	t.Helper()
	c := &Component{name: "plan-manager"}
	c.logger = slog.Default()
	c.sandbox = sandbox.NewClient(sandboxURL)
	return c
}

// stubSandboxForMergeBranches spins up an httptest.Server that responds to
// POST /git/merge-branches with the caller-supplied status + body and
// records the request body for assertion.
type stubMergeBranchesServer struct {
	server     *httptest.Server
	gotRequest *mergeBranchesRequestCapture
}

type mergeBranchesRequestCapture struct {
	Target   string            `json:"target"`
	Base     string            `json:"base"`
	Branches []string          `json:"branches"`
	Trailers map[string]string `json:"trailers"`
}

func newStubMergeBranchesServer(t *testing.T, status int, body any) *stubMergeBranchesServer {
	t.Helper()
	s := &stubMergeBranchesServer{gotRequest: &mergeBranchesRequestCapture{}}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/git/merge-branches" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(s.gotRequest); err != nil {
			t.Errorf("stub sandbox decode: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(s.server.Close)
	return s
}

func TestAssembleRequirementBranches_Success(t *testing.T) {
	stub := newStubMergeBranchesServer(t, http.StatusOK, map[string]any{
		"status": "merged",
		"target": "semspec/plan-demo",
		"merge_commits": []map[string]string{
			{"branch": "semspec/requirement-r1", "commit": "sha1"},
			{"branch": "semspec/requirement-r2", "commit": "sha2"},
		},
	})
	c := newTestComponentWithSandbox(t, stub.server.URL)

	plan := &workflow.Plan{
		Slug: "demo",
		Requirements: []workflow.Requirement{
			{ID: "r1", Title: "R1"},
			{ID: "r2", Title: "R2"},
		},
	}

	if err := c.assembleRequirementBranches(context.Background(), plan); err != nil {
		t.Fatalf("assembleRequirementBranches: unexpected error: %v", err)
	}

	if plan.PlanBranch != "semspec/plan-demo" {
		t.Errorf("PlanBranch = %q, want %q", plan.PlanBranch, "semspec/plan-demo")
	}
	if plan.PlanMergeCommit != "sha2" {
		t.Errorf("PlanMergeCommit = %q, want %q (last merge commit)", plan.PlanMergeCommit, "sha2")
	}
	// Verify the branches were submitted in plan-requirement order with the
	// correct Plan-Slug trailer — plan-manager is the sole contract owner
	// for how requirement branches map to merge-branches input.
	if stub.gotRequest.Target != "semspec/plan-demo" {
		t.Errorf("request target = %q, want %q", stub.gotRequest.Target, "semspec/plan-demo")
	}
	want := []string{"semspec/requirement-r1", "semspec/requirement-r2"}
	if len(stub.gotRequest.Branches) != len(want) {
		t.Fatalf("request branches = %v, want %v", stub.gotRequest.Branches, want)
	}
	for i, b := range want {
		if stub.gotRequest.Branches[i] != b {
			t.Errorf("request branches[%d] = %q, want %q", i, stub.gotRequest.Branches[i], b)
		}
	}
	if stub.gotRequest.Trailers["Plan-Slug"] != "demo" {
		t.Errorf("Plan-Slug trailer = %q, want %q", stub.gotRequest.Trailers["Plan-Slug"], "demo")
	}
}

func TestAssembleRequirementBranches_Conflict(t *testing.T) {
	stub := newStubMergeBranchesServer(t, http.StatusConflict, map[string]any{
		"status":             "conflict",
		"target":             "semspec/plan-demo",
		"conflicting_branch": "semspec/requirement-r2",
		"merge_commits": []map[string]string{
			{"branch": "semspec/requirement-r1", "commit": "sha1"},
		},
		"error": "merge conflict on shared.txt",
	})
	c := newTestComponentWithSandbox(t, stub.server.URL)

	plan := &workflow.Plan{
		Slug: "demo",
		Requirements: []workflow.Requirement{
			{ID: "r1", Title: "R1"},
			{ID: "r2", Title: "R2"},
		},
	}

	err := c.assembleRequirementBranches(context.Background(), plan)
	if err == nil {
		t.Fatal("assembleRequirementBranches: expected error on conflict, got nil")
	}
	// Error message must name the conflicting branch so the UI can point
	// the human at the exact requirement that needs human resolution.
	if msg := err.Error(); !contains(msg, "semspec/requirement-r2") {
		t.Errorf("error should name conflicting branch; got %q", msg)
	}
	// Plan must NOT have been tagged with a merge commit — the merge didn't
	// complete cleanly. PlanBranch staying empty is the "don't lie about
	// state" invariant: the plan isn't assembled yet.
	if plan.PlanBranch != "" {
		t.Errorf("PlanBranch on conflict = %q, want empty", plan.PlanBranch)
	}
	if plan.PlanMergeCommit != "" {
		t.Errorf("PlanMergeCommit on conflict = %q, want empty", plan.PlanMergeCommit)
	}
}

func TestAssembleRequirementBranches_NoSandbox(t *testing.T) {
	c := &Component{name: "plan-manager"}
	c.logger = slog.Default()
	// c.sandbox intentionally nil — SandboxURL unset in config.

	plan := &workflow.Plan{
		Slug: "demo",
		Requirements: []workflow.Requirement{
			{ID: "r1", Title: "R1"},
		},
	}
	if err := c.assembleRequirementBranches(context.Background(), plan); err != nil {
		t.Fatalf("nil sandbox should be a no-op, got error: %v", err)
	}
	if plan.PlanBranch != "" {
		t.Errorf("PlanBranch with nil sandbox = %q, want empty", plan.PlanBranch)
	}
}

func TestAssembleRequirementBranches_NoRequirements(t *testing.T) {
	stub := newStubMergeBranchesServer(t, http.StatusInternalServerError, map[string]string{
		"error": "should not have been called",
	})
	c := newTestComponentWithSandbox(t, stub.server.URL)

	plan := &workflow.Plan{Slug: "empty", Requirements: nil}
	if err := c.assembleRequirementBranches(context.Background(), plan); err != nil {
		t.Fatalf("empty requirements should be a no-op, got error: %v", err)
	}
	if plan.PlanBranch != "" {
		t.Errorf("PlanBranch on empty plan = %q, want empty", plan.PlanBranch)
	}
	// Verify we didn't actually hit the stub server — if we did, the 500
	// response would have produced an error above.
}

func TestAssembleRequirementBranches_NeedsReconciliation(t *testing.T) {
	stub := newStubMergeBranchesServer(t, http.StatusServiceUnavailable, map[string]string{
		"error":      "sandbox wedged",
		"error_code": "needs_reconciliation",
	})
	c := newTestComponentWithSandbox(t, stub.server.URL)

	plan := &workflow.Plan{
		Slug: "demo",
		Requirements: []workflow.Requirement{
			{ID: "r1", Title: "R1"},
		},
	}

	err := c.assembleRequirementBranches(context.Background(), plan)
	if err == nil {
		t.Fatal("needs_reconciliation should surface as an error, got nil")
	}
	// Typed error must propagate through plan-manager so Phase 5's
	// infra_health plumbing can key off it.
	if !errors.Is(err, sandbox.ErrNeedsReconciliation) {
		t.Errorf("error = %v, want errors.Is(err, sandbox.ErrNeedsReconciliation)", err)
	}
}
