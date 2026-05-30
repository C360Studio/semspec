package planmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupFromSpecTestComponent returns a Component with a temp repo dir
// and the from-spec handler ready to invoke. Graph access is intentionally
// nil so the handler short-circuits at the resolveGraphQuerier step —
// covers the structural-reject and missing-graph paths without spinning
// up a fake graph fixture (translator-level happy path is covered in
// workflow/specimport/translator_test.go).
func setupFromSpecTestComponent(t *testing.T) (*Component, string) {
	t.Helper()
	c := setupTestComponent(t)
	repoDir := t.TempDir()
	c.config.RepoPath = repoDir
	return c, repoDir
}

func writeRepoFile(t *testing.T, repoDir, rel, content string) {
	t.Helper()
	full := filepath.Join(repoDir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestHandleCreatePlanFromSpec_RejectsMissingChangeName(t *testing.T) {
	c, _ := setupFromSpecTestComponent(t)
	body, _ := json.Marshal(CreatePlanFromSpecRequest{})
	req := httptest.NewRequest(http.MethodPost, "/plan-manager/plans/from-spec", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	c.handleCreatePlanFromSpec(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "change_name is required") {
		t.Errorf("expected 'change_name is required', got %s", rec.Body.String())
	}
}

func TestHandleCreatePlanFromSpec_RejectsWhenMethodNotPost(t *testing.T) {
	c, _ := setupFromSpecTestComponent(t)
	req := httptest.NewRequest(http.MethodGet, "/plan-manager/plans/from-spec", nil)
	rec := httptest.NewRecorder()
	c.handleCreatePlanFromSpec(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", rec.Code)
	}
}

func TestHandleCreatePlanFromSpec_RejectsMissingChangeDir(t *testing.T) {
	c, _ := setupFromSpecTestComponent(t)
	body, _ := json.Marshal(CreatePlanFromSpecRequest{ChangeName: "nonexistent"})
	req := httptest.NewRequest(http.MethodPost, "/plan-manager/plans/from-spec", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	c.handleCreatePlanFromSpec(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400 for missing change dir, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleCreatePlanFromSpec_RejectsStructuralFailure(t *testing.T) {
	c, repoDir := setupFromSpecTestComponent(t)
	// Create a change dir but without proposal.md — should fail structural check.
	if err := os.MkdirAll(filepath.Join(repoDir, "openspec", "changes", "broken-change"), 0o755); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(CreatePlanFromSpecRequest{ChangeName: "broken-change"})
	req := httptest.NewRequest(http.MethodPost, "/plan-manager/plans/from-spec", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	c.handleCreatePlanFromSpec(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400 for structural failure, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp FromSpecErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.StructuralResult == nil {
		t.Error("expected StructuralResult in response body")
	}
	hasMissingProposal := false
	if resp.StructuralResult != nil {
		for _, f := range resp.StructuralResult.Findings {
			if f.Code == "missing_proposal" {
				hasMissingProposal = true
			}
		}
	}
	if !hasMissingProposal {
		t.Errorf("expected missing_proposal finding, got: %+v", resp.StructuralResult)
	}
}

func TestHandleCreatePlanFromSpec_RejectsWhenGraphUnavailable(t *testing.T) {
	c, repoDir := setupFromSpecTestComponent(t)
	// Build a well-formed change so structural check passes — but
	// graph.GlobalSources() is nil in tests, so the handler should fall
	// through to the graph-unavailable branch.
	changeDir := "openspec/changes/healthy-change"
	writeRepoFile(t, repoDir, filepath.Join(changeDir, "proposal.md"), `# Proposal: Healthy
## What Changes
- `+"`user-auth`"+` — Authenticate users.
`)
	writeRepoFile(t, repoDir, filepath.Join(changeDir, "specs/user-auth/spec.md"), "# Spec: user-auth\n### Requirement: x\nThe system SHALL.\n")

	body, _ := json.Marshal(CreatePlanFromSpecRequest{ChangeName: "healthy-change"})
	req := httptest.NewRequest(http.MethodPost, "/plan-manager/plans/from-spec", bytes.NewReader(body)).WithContext(context.Background())
	rec := httptest.NewRecorder()
	c.handleCreatePlanFromSpec(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503 when graph unavailable, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "graph querier unavailable") {
		t.Errorf("expected graph-unavailable message, got %s", rec.Body.String())
	}
}

func TestRepoPathForFromSpec_PrefersConfig(t *testing.T) {
	c := setupTestComponent(t)
	c.config.RepoPath = "/tmp/test-config-path"
	if got := c.repoPathForFromSpec(); got != "/tmp/test-config-path" {
		t.Errorf("expected config path, got %q", got)
	}
}

func TestRepoPathForFromSpec_FallsBackToEnv(t *testing.T) {
	c := setupTestComponent(t)
	c.config.RepoPath = ""
	t.Setenv("SEMSPEC_REPO_PATH", "/tmp/env-path")
	if got := c.repoPathForFromSpec(); got != "/tmp/env-path" {
		t.Errorf("expected env path, got %q", got)
	}
}

func TestHandleCreatePlanFromSpec_RejectsPathTraversal(t *testing.T) {
	// Per go-reviewer PR 4 audit blocker #1: change_name must not contain
	// `..` or path separators. This test pins the fix so a future
	// regression in input validation surfaces immediately.
	dangerousNames := []string{
		"../../../etc",
		"..",
		"../adjacent",
		"path/to/change",
		"path\\to\\change",
		".hidden",
	}
	for _, name := range dangerousNames {
		t.Run(name, func(t *testing.T) {
			c, _ := setupFromSpecTestComponent(t)
			body, _ := json.Marshal(CreatePlanFromSpecRequest{ChangeName: name})
			req := httptest.NewRequest(http.MethodPost, "/plan-manager/plans/from-spec", bytes.NewReader(body))
			rec := httptest.NewRecorder()
			c.handleCreatePlanFromSpec(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for change_name=%q, got %d: %s", name, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "single path segment") {
				t.Errorf("expected path-traversal rejection message, got: %s", rec.Body.String())
			}
		})
	}
}

func TestIsSafeChangeName(t *testing.T) {
	cases := map[string]bool{
		"add-mavlink-driver": true,
		"v2-feature":         true,
		"":                   false,
		".":                  false,
		"..":                 false,
		"../escape":          false,
		"escape/..":          false,
		"path/to":            false,
		"path\\to":           false,
		".hidden":            false,
		"normal.name":        true, // dot allowed in middle (e.g. "v1.2")
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			if got := isSafeChangeName(name); got != want {
				t.Errorf("isSafeChangeName(%q) = %v, want %v", name, got, want)
			}
		})
	}
}

func TestWriteExternalSpecTriple_NoOpsOnEmptyExternalID(t *testing.T) {
	// Ensure best-effort behavior: empty external ID → silent no-op,
	// no panic, no triple write attempt.
	writeExternalSpecTriple(context.Background(), nil, "capability", "x", "slug", "")
	writeExternalSpecTriple(context.Background(), nil, "capability", "x", "slug", "ext")
	// Nil triple writer should also be safe.
}
