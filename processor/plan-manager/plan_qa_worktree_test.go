package planmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/c360studio/semspec/tools/sandbox"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
)

// qaSandboxStub is a recording httptest sandbox covering the endpoints the QA
// worktree helpers touch: merge-branches, worktree create/delete/exists.
type qaSandboxStub struct {
	server *httptest.Server

	mu          sync.Mutex
	mergeStatus int
	mergeBody   any
	exists      bool // GET /worktree/{id} answer

	mergeCalls  int
	createCalls []createCall
	deleteCalls []string
	existsCalls []string
}

type createCall struct {
	TaskID     string
	BaseBranch string
}

func newQASandboxStub(t *testing.T) *qaSandboxStub {
	t.Helper()
	s := &qaSandboxStub{
		mergeStatus: http.StatusOK,
		mergeBody: map[string]any{
			"status":        "merged",
			"target":        "semspec/plan-demo",
			"merge_commits": []map[string]string{{"branch": "semspec/requirement-r1", "commit": "sha1"}},
		},
	}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/git/merge-branches":
			s.mergeCalls++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(s.mergeStatus)
			_ = json.NewEncoder(w).Encode(s.mergeBody)
		case r.Method == http.MethodPost && r.URL.Path == "/worktree":
			var body struct {
				TaskID     string `json:"task_id"`
				BaseBranch string `json:"base_branch"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.createCalls = append(s.createCalls, createCall{body.TaskID, body.BaseBranch})
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status": "created", "path": "/wt/" + body.TaskID, "branch": "agent/" + body.TaskID,
			})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/worktree/"):
			s.deleteCalls = append(s.deleteCalls, strings.TrimPrefix(r.URL.Path, "/worktree/"))
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/worktree/"):
			s.existsCalls = append(s.existsCalls, strings.TrimPrefix(r.URL.Path, "/worktree/"))
			if s.exists {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.server.Close)
	return s
}

func qaTestComponent(t *testing.T, stub *qaSandboxStub) *Component {
	t.Helper()
	c := &Component{name: "plan-manager"}
	c.logger = slog.Default()
	c.sandbox = sandbox.NewClient(stub.server.URL)
	return c
}

func demoPlan() *workflow.Plan {
	return &workflow.Plan{
		Slug:         "demo",
		Requirements: []workflow.Requirement{{ID: "r1", Title: "R1"}},
	}
}

func TestAssembleAndStageQAWorktree_Success(t *testing.T) {
	stub := newQASandboxStub(t)
	c := qaTestComponent(t, stub)
	plan := demoPlan()

	if err := c.assembleAndStageQAWorktree(context.Background(), plan); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if plan.AssembledBranch != "semspec/plan-demo" {
		t.Errorf("AssembledBranch = %q, want semspec/plan-demo", plan.AssembledBranch)
	}
	if stub.mergeCalls != 1 {
		t.Errorf("merge calls = %d, want 1", stub.mergeCalls)
	}
	// Idempotent staging: delete-then-create on the deterministic QA worktree id.
	if len(stub.deleteCalls) != 1 || stub.deleteCalls[0] != workflow.QAWorktreeID("demo") {
		t.Errorf("delete calls = %v, want [%s]", stub.deleteCalls, workflow.QAWorktreeID("demo"))
	}
	if len(stub.createCalls) != 1 {
		t.Fatalf("create calls = %v, want 1", stub.createCalls)
	}
	if got := stub.createCalls[0]; got.TaskID != workflow.QAWorktreeID("demo") || got.BaseBranch != "semspec/plan-demo" {
		t.Errorf("create call = %+v, want {qa-demo, semspec/plan-demo}", got)
	}
}

func TestAssembleAndStageQAWorktree_ConflictPropagatesAndDoesNotStage(t *testing.T) {
	stub := newQASandboxStub(t)
	stub.mergeStatus = http.StatusConflict
	stub.mergeBody = map[string]any{
		"status": "conflict", "target": "semspec/plan-demo",
		"conflicting_branch": "semspec/requirement-r1",
	}
	c := qaTestComponent(t, stub)
	plan := demoPlan()

	err := c.assembleAndStageQAWorktree(context.Background(), plan)
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
	if !errors.Is(err, sandbox.ErrMergeBranchesConflict) {
		t.Errorf("error must errors.Is ErrMergeBranchesConflict; got %v", err)
	}
	// A failed merge must NOT create a QA worktree — there's nothing assembled
	// to check out, and QA must not run against a half-merged or empty tree.
	if len(stub.createCalls) != 0 {
		t.Errorf("worktree created on conflict: %v, want none", stub.createCalls)
	}
}

func TestAssembleAndStageQAWorktree_NoSandboxIsNoop(t *testing.T) {
	c := &Component{name: "plan-manager", logger: slog.Default()} // c.sandbox nil
	if err := c.assembleAndStageQAWorktree(context.Background(), demoPlan()); err != nil {
		t.Fatalf("nil sandbox should be a no-op, got: %v", err)
	}
}

func TestEnsureQAWorktree_CreatesWhenMissing(t *testing.T) {
	stub := newQASandboxStub(t)
	stub.exists = false
	c := qaTestComponent(t, stub)
	plan := demoPlan()
	plan.AssembledBranch = "semspec/plan-demo"

	c.ensureQAWorktree(context.Background(), plan)

	if len(stub.existsCalls) != 1 {
		t.Errorf("exists checks = %d, want 1", len(stub.existsCalls))
	}
	if len(stub.createCalls) != 1 || stub.createCalls[0].BaseBranch != "semspec/plan-demo" {
		t.Errorf("create calls = %v, want one on semspec/plan-demo", stub.createCalls)
	}
}

func TestEnsureQAWorktree_NoopWhenPresent(t *testing.T) {
	stub := newQASandboxStub(t)
	stub.exists = true
	c := qaTestComponent(t, stub)
	plan := demoPlan()
	plan.AssembledBranch = "semspec/plan-demo"

	c.ensureQAWorktree(context.Background(), plan)

	if len(stub.createCalls) != 0 {
		t.Errorf("create calls = %v, want none (worktree already present)", stub.createCalls)
	}
}

func TestEnsureQAWorktree_NoAssembledBranchIsNoop(t *testing.T) {
	stub := newQASandboxStub(t)
	c := qaTestComponent(t, stub)
	plan := demoPlan() // AssembledBranch empty

	c.ensureQAWorktree(context.Background(), plan)

	if len(stub.existsCalls)+len(stub.createCalls) != 0 {
		t.Errorf("made sandbox calls with no assembled branch: exists=%v create=%v",
			stub.existsCalls, stub.createCalls)
	}
}

func TestDeleteQAWorktree_CallsDelete(t *testing.T) {
	stub := newQASandboxStub(t)
	c := qaTestComponent(t, stub)

	c.deleteQAWorktree(context.Background(), "demo")

	if len(stub.deleteCalls) != 1 || stub.deleteCalls[0] != workflow.QAWorktreeID("demo") {
		t.Errorf("delete calls = %v, want [qa-demo]", stub.deleteCalls)
	}
}

// routeAssemblyConflict: a merge conflict fires phase-local recovery and keeps
// the plan in implementing; an infra error stalls without firing recovery.
func TestRouteAssemblyConflict_ConflictEmitsRecovery(t *testing.T) {
	c := setupTestComponent(t)
	var captured *payloads.RecoveryRequested
	c.recoveryPublisher = func(_ context.Context, req *payloads.RecoveryRequested) { captured = req }

	plan := setupTestPlan(t, c, "conflict-plan")
	plan.Status = workflow.StatusImplementing
	plan.Requirements = []workflow.Requirement{{ID: "r1"}, {ID: "r2"}}
	_ = c.plans.save(context.Background(), plan)

	conflictErr := fmt.Errorf("branch x conflicts: %w", sandbox.ErrMergeBranchesConflict)
	c.routeAssemblyConflict(context.Background(), plan, conflictErr)

	if captured == nil {
		t.Fatal("expected RecoveryRequested to be emitted on merge conflict")
	}
	if len(captured.AffectedRequirementIDs) != 2 {
		t.Errorf("AffectedRequirementIDs = %v, want both reqs", captured.AffectedRequirementIDs)
	}
	if captured.Layer != payloads.RecoveryLayerPhaseLocal {
		t.Errorf("Layer = %v, want phase-local", captured.Layer)
	}
	stored, _ := c.plans.get("conflict-plan")
	if stored.Status != workflow.StatusImplementing {
		t.Errorf("plan status = %q, want implementing (must not advance to QA)", stored.Status)
	}
	if stored.LastError == "" {
		t.Error("LastError should be set so the UI can surface the stall")
	}
}

// handleConvergenceAllSucceeded is the load-bearing early-return: on a pre-QA
// merge conflict the plan must NOT advance to QA and must NOT stage a worktree
// (else QA would inspect an unmerged/empty tree — the exact bug being fixed).
func TestHandleConvergenceAllSucceeded_ConflictDoesNotAdvanceToQA(t *testing.T) {
	stub := newQASandboxStub(t)
	stub.mergeStatus = http.StatusConflict
	stub.mergeBody = map[string]any{
		"status": "conflict", "target": "semspec/plan-demo",
		"conflicting_branch": "semspec/requirement-r2",
	}
	c := setupTestComponent(t)
	c.sandbox = sandbox.NewClient(stub.server.URL)
	var recovery *payloads.RecoveryRequested
	c.recoveryPublisher = func(_ context.Context, r *payloads.RecoveryRequested) { recovery = r }

	plan := setupTestPlan(t, c, "demo")
	plan.Status = workflow.StatusImplementing
	plan.QALevel = workflow.QALevelUnit
	plan.Requirements = []workflow.Requirement{{ID: "r1"}, {ID: "r2"}}
	_ = c.plans.save(context.Background(), plan)

	c.handleConvergenceAllSucceeded(context.Background(), plan, "demo", 2)

	stored, _ := c.plans.get("demo")
	if stored.Status != workflow.StatusImplementing {
		t.Errorf("status = %q, want implementing (must not advance to QA on conflict)", stored.Status)
	}
	if recovery == nil {
		t.Error("expected RecoveryRequested on pre-QA merge conflict")
	}
	if len(stub.createCalls) != 0 {
		t.Errorf("QA worktree staged despite conflict: %v", stub.createCalls)
	}
}

func TestHandleConvergenceAllSucceeded_SuccessStagesWorktreeAndAdvances(t *testing.T) {
	stub := newQASandboxStub(t) // merge OK by default, target semspec/plan-demo
	c := setupTestComponent(t)
	c.sandbox = sandbox.NewClient(stub.server.URL)
	// c.natsClient nil → publishQARequestIfNeeded no-ops; we assert state + staging.

	plan := setupTestPlan(t, c, "demo")
	plan.Status = workflow.StatusImplementing
	plan.QALevel = workflow.QALevelUnit
	plan.Requirements = []workflow.Requirement{{ID: "r1"}}
	_ = c.plans.save(context.Background(), plan)

	c.handleConvergenceAllSucceeded(context.Background(), plan, "demo", 1)

	stored, _ := c.plans.get("demo")
	if stored.Status != workflow.StatusReadyForQA {
		t.Errorf("status = %q, want ready_for_qa", stored.Status)
	}
	if stored.AssembledBranch != "semspec/plan-demo" {
		t.Errorf("AssembledBranch = %q, want semspec/plan-demo", stored.AssembledBranch)
	}
	if len(stub.createCalls) != 1 || stub.createCalls[0].TaskID != workflow.QAWorktreeID("demo") {
		t.Errorf("QA worktree not staged on success: %v", stub.createCalls)
	}
}

func TestRouteAssemblyConflict_InfraErrorDoesNotEmitRecovery(t *testing.T) {
	c := setupTestComponent(t)
	recoveryFired := false
	c.recoveryPublisher = func(_ context.Context, _ *payloads.RecoveryRequested) { recoveryFired = true }

	plan := setupTestPlan(t, c, "infra-plan")
	plan.Status = workflow.StatusImplementing
	plan.Requirements = []workflow.Requirement{{ID: "r1"}}
	_ = c.plans.save(context.Background(), plan)

	c.routeAssemblyConflict(context.Background(), plan, errors.New("sandbox unreachable"))

	if recoveryFired {
		t.Error("infra error must NOT fire plan-scope recovery (it's a transient stall)")
	}
	stored, _ := c.plans.get("infra-plan")
	if stored.Status != workflow.StatusImplementing {
		t.Errorf("plan status = %q, want implementing", stored.Status)
	}
	if stored.LastError == "" {
		t.Error("LastError should be set on infra stall")
	}
}
