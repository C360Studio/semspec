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

// stubAncestryServer answers /git/ancestry based on a caller-supplied
// relationship map. The key is "ancestor|descendant"; the value is the
// AncestryResult to return. Unknown pairs produce a 404-shape response so
// tests can verify the unreachable-sandbox branch.
type stubAncestryServer struct {
	server    *httptest.Server
	responses map[string]map[string]bool // keyed by "anc|desc" → {ancestor_exists, descendant_exists, is_ancestor}
	requests  []string
}

func newStubAncestryServer(t *testing.T, responses map[string]map[string]bool) *stubAncestryServer {
	t.Helper()
	s := &stubAncestryServer{responses: responses}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/git/ancestry" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Ancestor   string `json:"ancestor"`
			Descendant string `json:"descendant"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		key := body.Ancestor + "|" + body.Descendant
		s.requests = append(s.requests, key)
		result, ok := s.responses[key]
		if !ok {
			// Treat missing-from-map as "nothing exists" so tests can
			// positively assert healthy=false on unknown refs.
			result = map[string]bool{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{
			"ancestor_exists":   result["ancestor_exists"],
			"descendant_exists": result["descendant_exists"],
			"is_ancestor":       result["is_ancestor"],
		})
	}))
	t.Cleanup(s.server.Close)
	return s
}

func TestBuildGitAuditReport_AllGreen(t *testing.T) {
	stub := newStubAncestryServer(t, map[string]map[string]bool{
		"semspec/requirement-r1|semspec/plan-demo": {"ancestor_exists": true, "descendant_exists": true, "is_ancestor": true},
		"semspec/requirement-r2|semspec/plan-demo": {"ancestor_exists": true, "descendant_exists": true, "is_ancestor": true},
	})
	c := newTestComponentWithSandbox(t, stub.server.URL)
	plan := &workflow.Plan{
		Slug:            "demo",
		AssembledBranch: "semspec/plan-demo",
		Requirements: []workflow.Requirement{
			{ID: "r1"}, {ID: "r2"},
		},
	}

	got := c.buildGitAuditReport(context.Background(), plan)

	if !got.Healthy {
		t.Errorf("Healthy = false on all-green plan; report=%+v", got)
	}
	if len(got.Findings) != 2 {
		t.Fatalf("Findings len = %d, want 2", len(got.Findings))
	}
	for _, f := range got.Findings {
		if !f.ExistsInSandbox || !f.MergedIntoAssembled {
			t.Errorf("finding %q: exists=%v merged=%v, both should be true",
				f.Branch, f.ExistsInSandbox, f.MergedIntoAssembled)
		}
	}
}

// TestBuildGitAuditReport_LyingAboutState is the load-bearing case: plan
// claims complete, AssembledBranch is set, but a requirement's commits
// are NOT actually reachable from the assembled branch. The report must
// flip Healthy=false and annotate the offending requirement so the human
// knows which one to investigate.
func TestBuildGitAuditReport_LyingAboutState(t *testing.T) {
	stub := newStubAncestryServer(t, map[string]map[string]bool{
		"semspec/requirement-r1|semspec/plan-demo": {"ancestor_exists": true, "descendant_exists": true, "is_ancestor": true},
		"semspec/requirement-r2|semspec/plan-demo": {"ancestor_exists": true, "descendant_exists": true, "is_ancestor": false}, // divergence
	})
	c := newTestComponentWithSandbox(t, stub.server.URL)
	plan := &workflow.Plan{
		Slug:            "demo",
		AssembledBranch: "semspec/plan-demo",
		Requirements:    []workflow.Requirement{{ID: "r1"}, {ID: "r2"}},
	}

	got := c.buildGitAuditReport(context.Background(), plan)
	if got.Healthy {
		t.Fatalf("Healthy = true on lying-about-state plan; report=%+v", got)
	}
	// Find r2 specifically and verify its notes explain the drift.
	var r2 *GitAuditFinding
	for i := range got.Findings {
		if got.Findings[i].RequirementID == "r2" {
			r2 = &got.Findings[i]
		}
	}
	if r2 == nil {
		t.Fatalf("r2 missing from findings: %+v", got.Findings)
	}
	if r2.MergedIntoAssembled {
		t.Errorf("r2.MergedIntoAssembled = true, want false")
	}
	if r2.Notes == "" {
		t.Error("r2.Notes should describe the divergence")
	}
}

// TestBuildGitAuditReport_RequirementBranchMissing verifies detection of
// the "branch pruned too aggressively" failure mode — a requirement
// whose branch doesn't exist in git despite the plan still referencing it.
func TestBuildGitAuditReport_RequirementBranchMissing(t *testing.T) {
	stub := newStubAncestryServer(t, map[string]map[string]bool{
		// r1 branch doesn't exist → default empty map (all false)
		"semspec/requirement-r2|semspec/plan-demo": {"ancestor_exists": true, "descendant_exists": true, "is_ancestor": true},
	})
	c := newTestComponentWithSandbox(t, stub.server.URL)
	plan := &workflow.Plan{
		Slug:            "demo",
		AssembledBranch: "semspec/plan-demo",
		Requirements:    []workflow.Requirement{{ID: "r1"}, {ID: "r2"}},
	}

	got := c.buildGitAuditReport(context.Background(), plan)
	if got.Healthy {
		t.Fatalf("Healthy=true on missing-branch plan; report=%+v", got)
	}
	for _, f := range got.Findings {
		if f.RequirementID == "r1" && f.ExistsInSandbox {
			t.Error("r1.ExistsInSandbox = true, want false (branch absent)")
		}
	}
}

// TestBuildGitAuditReport_NoAssembledBranch covers pre-B1 plans or plans
// that haven't yet reached complete — the audit falls back to
// branch-existence-only checks (still useful for catching pruning bugs).
func TestBuildGitAuditReport_NoAssembledBranch(t *testing.T) {
	// Ancestry called with (branch, branch) asks "does this branch exist?"
	// We encode the positive answer as ancestor_exists=true regardless.
	stub := newStubAncestryServer(t, map[string]map[string]bool{
		"semspec/requirement-r1|semspec/requirement-r1": {"ancestor_exists": true, "descendant_exists": true, "is_ancestor": true},
	})
	c := newTestComponentWithSandbox(t, stub.server.URL)
	plan := &workflow.Plan{
		Slug:         "demo",
		Requirements: []workflow.Requirement{{ID: "r1"}},
		// AssembledBranch intentionally empty.
	}

	got := c.buildGitAuditReport(context.Background(), plan)
	if !got.Healthy {
		t.Errorf("Healthy=false on pre-B1 plan with existing branch; report=%+v", got)
	}
	if len(got.Findings) != 1 {
		t.Fatalf("findings len = %d, want 1", len(got.Findings))
	}
	if got.Findings[0].MergedIntoAssembled {
		t.Error("pre-B1 plan should have MergedIntoAssembled=false (no assembly)")
	}
	if !got.Findings[0].ExistsInSandbox {
		t.Error("branch should have been detected via existence probe")
	}
}

// TestBuildGitAuditReport_NoSandbox verifies the graceful-degradation
// contract: an audit request against a plan-manager with no sandbox
// client reports unhealthy with a warning rather than 500-ing.
func TestBuildGitAuditReport_NoSandbox(t *testing.T) {
	c := &Component{name: "plan-manager"}
	c.logger = slog.Default()
	plan := &workflow.Plan{Slug: "demo", Requirements: []workflow.Requirement{{ID: "r1"}}}
	got := c.buildGitAuditReport(context.Background(), plan)
	if got.Healthy {
		t.Error("Healthy = true without a sandbox client, want false")
	}
	if len(got.Warnings) == 0 {
		t.Error("expected at least one warning when sandbox is not configured")
	}
}
