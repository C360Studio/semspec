package planmanager

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// newBranchesTestComponent builds a Component with a planStore but no NATS.
// The branches handler must tolerate a nil natsClient (falls back to empty
// execution map) so we can test the handler's own logic in isolation.
func newBranchesTestComponent(t *testing.T) *Component {
	t.Helper()
	ps, err := newPlanStore(context.Background(), nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("newPlanStore: %v", err)
	}
	return &Component{
		logger: slog.Default(),
		plans:  ps,
	}
}

func TestHandlePlanBranches_NoSandbox(t *testing.T) {
	c := newBranchesTestComponent(t)
	plan := &workflow.Plan{ID: workflow.PlanEntityID("x"), Slug: "x"}
	_ = c.plans.save(context.Background(), plan)

	req := httptest.NewRequest(http.MethodGet, "/plan-api/plans/x/branches", nil)
	w := httptest.NewRecorder()
	c.handlePlanBranches(w, req, "x")

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestHandlePlanBranches_PlanNotFound(t *testing.T) {
	c := newBranchesTestComponent(t)
	c.workspace = newWorkspaceProxy("http://sandbox.invalid")

	req := httptest.NewRequest(http.MethodGet, "/plan-api/plans/missing/branches", nil)
	w := httptest.NewRecorder()
	c.handlePlanBranches(w, req, "missing")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandlePlanBranches_HappyPath(t *testing.T) {
	// Stub sandbox: every /git/branch-diff returns a canned summary keyed by
	// branch name so the test can verify per-requirement routing.
	sandbox := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/git/branch-diff" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var req struct {
			Branch string `json:"branch"`
			Base   string `json:"base"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(BranchDiffSummary{
			Base:   req.Base,
			Branch: req.Branch,
			Files: []BranchDiffFile{
				{Path: "src/" + req.Branch + ".go", Status: "added", Insertions: 10, Deletions: 0},
			},
			TotalInsertions: 10,
			TotalDeletions:  0,
		})
	}))
	defer sandbox.Close()

	c := newBranchesTestComponent(t)
	c.workspace = newWorkspaceProxy(sandbox.URL)

	// Plan has 3 requirements; only 2 have a branch recorded (simulating the
	// real-world case where some requirements haven't started yet).
	plan := &workflow.Plan{
		ID:    workflow.PlanEntityID("demo"),
		Slug:  "demo",
		Title: "demo",
		Requirements: []workflow.Requirement{
			{ID: "R1", Title: "Parse input"},
			{ID: "R2", Title: "Compute total"},
			{ID: "R3", Title: "Render output"},
		},
	}
	_ = c.plans.save(context.Background(), plan)

	req := httptest.NewRequest(http.MethodGet, "/plan-api/plans/demo/branches", nil)
	w := httptest.NewRecorder()
	c.handlePlanBranches(w, req, "demo")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var got []PlanRequirementBranch
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("len = %d, want 3: %+v", len(got), got)
	}
	if got[0].Title != "Parse input" || got[0].RequirementID != "R1" {
		t.Errorf("got[0] = %+v, want R1/Parse input", got[0])
	}
	// natsClient is nil → no execution entries → no branches resolved → no files
	// queried. The test confirms the handler gracefully produces one row per
	// requirement even without executions.
	for i, b := range got {
		if b.Branch != "" {
			t.Errorf("got[%d].Branch = %q, want empty (no executions)", i, b.Branch)
		}
		if len(b.Files) != 0 {
			t.Errorf("got[%d].Files = %d, want 0", i, len(b.Files))
		}
	}
}

func TestHandleRequirementFileDiff_MissingPath(t *testing.T) {
	c := newBranchesTestComponent(t)
	c.workspace = newWorkspaceProxy("http://sandbox.invalid")
	plan := &workflow.Plan{ID: workflow.PlanEntityID("p"), Slug: "p"}
	_ = c.plans.save(context.Background(), plan)

	req := httptest.NewRequest(http.MethodGet, "/plan-api/plans/p/requirements/R1/file-diff", nil)
	w := httptest.NewRecorder()
	c.handleRequirementFileDiff(w, req, "p", "R1")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestHandleRequirementFileDiff_HappyPath(t *testing.T) {
	var sawBranch, sawPath, sawBase string
	sandbox := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/git/branch-file-diff" {
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
			return
		}
		var req struct {
			Branch string `json:"branch"`
			Base   string `json:"base"`
			Path   string `json:"path"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		sawBranch, sawPath, sawBase = req.Branch, req.Path, req.Base
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"patch": "--- a/foo\n+++ b/foo\n"})
	}))
	defer sandbox.Close()

	c := newBranchesTestComponent(t)
	c.workspace = newWorkspaceProxy(sandbox.URL)
	plan := &workflow.Plan{ID: workflow.PlanEntityID("p"), Slug: "p"}
	_ = c.plans.save(context.Background(), plan)

	req := httptest.NewRequest(http.MethodGet, "/plan-api/plans/p/requirements/R7/file-diff?path=src/foo.go&base=develop", nil)
	w := httptest.NewRecorder()
	c.handleRequirementFileDiff(w, req, "p", "R7")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if sawBranch != "semspec/requirement-R7" {
		t.Errorf("sandbox saw branch = %q, want semspec/requirement-R7", sawBranch)
	}
	if sawPath != "src/foo.go" {
		t.Errorf("sandbox saw path = %q, want src/foo.go", sawPath)
	}
	if sawBase != "develop" {
		t.Errorf("sandbox saw base = %q, want develop (from ?base= query)", sawBase)
	}

	var got struct {
		Patch string `json:"patch"`
	}
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Patch == "" {
		t.Errorf("patch is empty")
	}
}

func TestResolvePlanBase(t *testing.T) {
	plan := &workflow.Plan{}
	r1 := httptest.NewRequest(http.MethodGet, "/?base=feature", nil)
	if got := resolvePlanBase(r1, plan); got != "feature" {
		t.Errorf("query param: got %q, want feature", got)
	}

	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	plan.GitHub = &workflow.GitHubMetadata{PlanBranch: "plans/x"}
	if got := resolvePlanBase(r2, plan); got != "plans/x" {
		t.Errorf("plan branch: got %q, want plans/x", got)
	}

	r3 := httptest.NewRequest(http.MethodGet, "/", nil)
	plan.GitHub = nil
	if got := resolvePlanBase(r3, plan); got != "main" {
		t.Errorf("default: got %q, want main", got)
	}
}
