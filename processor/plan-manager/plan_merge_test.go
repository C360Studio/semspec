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

	if plan.AssembledBranch != "semspec/plan-demo" {
		t.Errorf("PlanBranch = %q, want %q", plan.AssembledBranch, "semspec/plan-demo")
	}
	if plan.AssembledMergeCommit != "sha2" {
		t.Errorf("PlanMergeCommit = %q, want %q (last merge commit)", plan.AssembledMergeCommit, "sha2")
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
	// Pin current contract: plan-manager sends Base="", which the sandbox
	// maps to HEAD at merge time. If this ever changes (e.g. fixing to
	// main/master), this assertion catches the shift so both layers move
	// together.
	if stub.gotRequest.Base != "" {
		t.Errorf("request Base = %q, want empty (sandbox defaults to HEAD)", stub.gotRequest.Base)
	}
}

// TestAssembleRequirementBranches_TopoSortByDependsOn verifies M2 from the
// Phase 4 go-reviewer findings: requirements are submitted to the sandbox
// in an order where prerequisites appear before their dependents, so merge
// conflicts between logically-independent reqs don't depend on plan-manager's
// KV write order.
func TestAssembleRequirementBranches_TopoSortByDependsOn(t *testing.T) {
	stub := newStubMergeBranchesServer(t, http.StatusOK, map[string]any{
		"status":        "merged",
		"target":        "semspec/plan-dag",
		"merge_commits": []map[string]string{},
	})
	c := newTestComponentWithSandbox(t, stub.server.URL)

	// Input order is intentionally "wrong" — r3 depends on r1 which depends
	// on r2, yet they're stored r1, r2, r3. Expected output: r2, r1, r3.
	plan := &workflow.Plan{
		Slug: "dag",
		Requirements: []workflow.Requirement{
			{ID: "r1", DependsOn: []string{"r2"}},
			{ID: "r2"},
			{ID: "r3", DependsOn: []string{"r1"}},
		},
	}
	if err := c.assembleRequirementBranches(context.Background(), plan); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"semspec/requirement-r2", "semspec/requirement-r1", "semspec/requirement-r3"}
	if len(stub.gotRequest.Branches) != len(want) {
		t.Fatalf("branches = %v, want %v", stub.gotRequest.Branches, want)
	}
	for i, b := range want {
		if stub.gotRequest.Branches[i] != b {
			t.Errorf("branches[%d] = %q, want %q (full: %v)",
				i, stub.gotRequest.Branches[i], b, stub.gotRequest.Branches)
		}
	}
}

// TestAssembleRequirementBranches_ConflictErrorIsWrapped pins the M1 fix:
// the conflict error bubbled up to plan-manager must be matchable via
// errors.Is(err, sandbox.ErrMergeBranchesConflict) so Phase 5's UX code can
// route conflict vs infrastructure failures without string-matching.
func TestAssembleRequirementBranches_ConflictErrorIsWrapped(t *testing.T) {
	stub := newStubMergeBranchesServer(t, http.StatusConflict, map[string]any{
		"status":             "conflict",
		"target":             "semspec/plan-demo",
		"conflicting_branch": "semspec/requirement-r2",
	})
	c := newTestComponentWithSandbox(t, stub.server.URL)
	plan := &workflow.Plan{
		Slug: "demo",
		Requirements: []workflow.Requirement{
			{ID: "r1"},
			{ID: "r2"},
		},
	}
	err := c.assembleRequirementBranches(context.Background(), plan)
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
	if !errors.Is(err, sandbox.ErrMergeBranchesConflict) {
		t.Errorf("error must errors.Is ErrMergeBranchesConflict; got %v", err)
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
	if plan.AssembledBranch != "" {
		t.Errorf("PlanBranch on conflict = %q, want empty", plan.AssembledBranch)
	}
	if plan.AssembledMergeCommit != "" {
		t.Errorf("PlanMergeCommit on conflict = %q, want empty", plan.AssembledMergeCommit)
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
	if plan.AssembledBranch != "" {
		t.Errorf("PlanBranch with nil sandbox = %q, want empty", plan.AssembledBranch)
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
	if plan.AssembledBranch != "" {
		t.Errorf("PlanBranch on empty plan = %q, want empty", plan.AssembledBranch)
	}
	// Verify we didn't actually hit the stub server — if we did, the 500
	// response would have produced an error above.
}

// TestPruneRequirementBranches_DeletesPerRequirement verifies invariant D3:
// after a plan archives, every semspec/requirement-<id> branch is deleted
// so branch lists stay tidy across plan cycles. The AssembledBranch is not
// in the plan.Requirements slice so is implicitly preserved.
func TestPruneRequirementBranches_DeletesPerRequirement(t *testing.T) {
	var deleted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("unexpected method %s", r.Method)
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		// Branch name is in the path: /branch/<name>
		name := r.URL.Path[len("/branch/"):]
		deleted = append(deleted, name)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestComponentWithSandbox(t, srv.URL)
	plan := &workflow.Plan{
		Slug: "archive-me",
		Requirements: []workflow.Requirement{
			{ID: "r1"},
			{ID: "r2"},
			{ID: "r3"},
		},
	}

	c.pruneRequirementBranches(context.Background(), plan)

	want := []string{"semspec/requirement-r1", "semspec/requirement-r2", "semspec/requirement-r3"}
	if len(deleted) != len(want) {
		t.Fatalf("deleted = %v, want %v", deleted, want)
	}
	for i, w := range want {
		if deleted[i] != w {
			t.Errorf("deleted[%d] = %q, want %q", i, deleted[i], w)
		}
	}
}

// TestPruneRequirementBranches_SandboxErrorsAreSwallowed confirms the
// best-effort contract: archiving must not fail because a branch delete
// fails. The audit rationale is that archive is a terminal operator
// action — failing it because of transient sandbox trouble would strand
// the plan in a non-archivable state, which is worse than leaking branches.
func TestPruneRequirementBranches_SandboxErrorsAreSwallowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error": "sandbox exploded"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newTestComponentWithSandbox(t, srv.URL)
	plan := &workflow.Plan{
		Slug: "archive-failing",
		Requirements: []workflow.Requirement{
			{ID: "r1"},
		},
	}
	// Should not panic, should not return anything the caller has to handle.
	c.pruneRequirementBranches(context.Background(), plan)
}

// TestPruneRequirementBranches_NoSandboxIsNoOp verifies the helper is safe
// to call when plan-manager has no sandbox client — matches pre-B1 no-op
// posture and lets tests exercise archive paths without mocking the sandbox.
func TestPruneRequirementBranches_NoSandboxIsNoOp(_ *testing.T) {
	c := &Component{name: "plan-manager"}
	c.logger = slog.Default()
	plan := &workflow.Plan{Slug: "x", Requirements: []workflow.Requirement{{ID: "r1"}}}
	c.pruneRequirementBranches(context.Background(), plan)
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
