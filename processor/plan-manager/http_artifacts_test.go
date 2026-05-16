package planmanager

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractSlugArtifactName(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		wantSlug string
		wantName string
	}{
		{
			name:     "collection endpoint",
			path:     "/plan-manager/plans/feature-a/artifacts",
			wantSlug: "feature-a",
			wantName: "",
		},
		{
			name:     "collection endpoint trailing slash",
			path:     "/plan-manager/plans/feature-a/artifacts/",
			wantSlug: "feature-a",
			wantName: "",
		},
		{
			name:     "named artifact",
			path:     "/plan-manager/plans/feature-a/artifacts/architecture",
			wantSlug: "feature-a",
			wantName: "architecture",
		},
		{
			name:     "named artifact with dash",
			path:     "/plan-manager/plans/feature-a/artifacts/qa-summary",
			wantSlug: "feature-a",
			wantName: "qa-summary",
		},
		{
			name:     "not artifacts endpoint",
			path:     "/plan-manager/plans/feature-a/reviews",
			wantSlug: "",
			wantName: "",
		},
		{
			name:     "missing slug",
			path:     "/plan-manager/plans/",
			wantSlug: "",
			wantName: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			slug, name := extractSlugArtifactName(tc.path)
			if slug != tc.wantSlug || name != tc.wantName {
				t.Errorf("extractSlugArtifactName(%q) = (%q, %q), want (%q, %q)",
					tc.path, slug, name, tc.wantSlug, tc.wantName)
			}
		})
	}
}

// newArtifactsTestComponent builds a Component rooted at a temp directory
// with .semspec/plans/{slug}/ pre-populated with the given markdown files.
// Returns the component and the absolute slug directory.
func newArtifactsTestComponent(t *testing.T, slug string, files map[string]string) (*Component, string) {
	t.Helper()
	repoRoot := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", repoRoot)

	slugDir := filepath.Join(repoRoot, ".semspec", "plans", slug)
	if err := os.MkdirAll(slugDir, 0o755); err != nil {
		t.Fatalf("mkdir slug dir: %v", err)
	}
	for filename, content := range files {
		path := filepath.Join(slugDir, filename)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", filename, err)
		}
	}

	return &Component{logger: slog.Default()}, slugDir
}

func TestHandlePlanArtifactsList_ReturnsExisting(t *testing.T) {
	c, _ := newArtifactsTestComponent(t, "feature-a", map[string]string{
		"plan.md":         "# Plan\n",
		"architecture.md": "# Architecture\n",
		"requirements.md": "# Requirements\n",
		// scenarios, qa-summary, run-summary intentionally missing.
	})

	req := httptest.NewRequest(http.MethodGet, "/plan-manager/plans/feature-a/artifacts", nil)
	w := httptest.NewRecorder()

	c.handlePlanArtifactsList(w, req, "feature-a")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp PhaseArtifactsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Slug != "feature-a" {
		t.Errorf("Slug = %q, want %q", resp.Slug, "feature-a")
	}
	if len(resp.Artifacts) != 3 {
		t.Fatalf("Artifacts len = %d, want 3; got %+v", len(resp.Artifacts), resp.Artifacts)
	}

	// Canonical phase order: plan, architecture, requirements
	wantNames := []string{"plan", "architecture", "requirements"}
	for i, want := range wantNames {
		if resp.Artifacts[i].Name != want {
			t.Errorf("Artifacts[%d].Name = %q, want %q", i, resp.Artifacts[i].Name, want)
		}
		if resp.Artifacts[i].Filename != want+".md" {
			t.Errorf("Artifacts[%d].Filename = %q, want %q", i, resp.Artifacts[i].Filename, want+".md")
		}
		if resp.Artifacts[i].Size == 0 {
			t.Errorf("Artifacts[%d].Size = 0, want > 0", i)
		}
		if resp.Artifacts[i].ModifiedAt == "" {
			t.Errorf("Artifacts[%d].ModifiedAt empty", i)
		}
	}
}

func TestHandlePlanArtifactsList_EmptyWhenNoFiles(t *testing.T) {
	c, _ := newArtifactsTestComponent(t, "feature-empty", nil)

	req := httptest.NewRequest(http.MethodGet, "/plan-manager/plans/feature-empty/artifacts", nil)
	w := httptest.NewRecorder()

	c.handlePlanArtifactsList(w, req, "feature-empty")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp PhaseArtifactsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Artifacts) != 0 {
		t.Errorf("Artifacts len = %d, want 0; got %+v", len(resp.Artifacts), resp.Artifacts)
	}
}

func TestHandlePlanArtifactContent_ReturnsMarkdown(t *testing.T) {
	body := "# Architecture\n\nSome content.\n"
	c, _ := newArtifactsTestComponent(t, "feature-a", map[string]string{
		"architecture.md": body,
	})

	req := httptest.NewRequest(http.MethodGet, "/plan-manager/plans/feature-a/artifacts/architecture", nil)
	w := httptest.NewRecorder()

	c.handlePlanArtifactContent(w, req, "feature-a", "architecture")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/markdown") {
		t.Errorf("Content-Type = %q, want text/markdown prefix", got)
	}
	if w.Body.String() != body {
		t.Errorf("body = %q, want %q", w.Body.String(), body)
	}
}

func TestHandlePlanArtifactContent_UnknownName(t *testing.T) {
	c, _ := newArtifactsTestComponent(t, "feature-a", map[string]string{
		"architecture.md": "# Architecture\n",
	})

	req := httptest.NewRequest(http.MethodGet, "/plan-manager/plans/feature-a/artifacts/not-a-thing", nil)
	w := httptest.NewRecorder()

	c.handlePlanArtifactContent(w, req, "feature-a", "not-a-thing")

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandlePlanArtifactContent_NotWritten(t *testing.T) {
	// Plan exists but qa-summary.md hasn't been emitted yet (pre-QA phase).
	c, _ := newArtifactsTestComponent(t, "feature-a", map[string]string{
		"architecture.md": "# Architecture\n",
	})

	req := httptest.NewRequest(http.MethodGet, "/plan-manager/plans/feature-a/artifacts/qa-summary", nil)
	w := httptest.NewRecorder()

	c.handlePlanArtifactContent(w, req, "feature-a", "qa-summary")

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}
