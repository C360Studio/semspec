package planmanager

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// PhaseArtifact describes one of the per-phase markdown deliverables
// written by the workflow-documents output component.
type PhaseArtifact struct {
	// Name is the stable identifier (no extension): "architecture",
	// "requirements", "scenarios", "qa-summary", "run-summary", "plan".
	Name string `json:"name"`
	// Filename is the on-disk file name including extension.
	Filename string `json:"filename"`
	// Size is the file size in bytes.
	Size int64 `json:"size"`
	// ModifiedAt is the file mtime in RFC3339 format.
	ModifiedAt string `json:"modified_at"`
}

// PhaseArtifactsResponse is the body of GET /plans/{slug}/artifacts.
type PhaseArtifactsResponse struct {
	Slug      string          `json:"slug"`
	Artifacts []PhaseArtifact `json:"artifacts"`
}

// phaseArtifactAllowList enumerates the BMAD/OpenSpec markdown files that
// workflow-documents writes under .semspec/plans/{slug}/. The map values
// are the stable identifiers used in the API and as in-page anchor IDs.
// Order is preserved by phaseArtifactOrder so the list endpoint returns
// artifacts in the same sequence the rendering pipeline produces them.
var phaseArtifactAllowList = map[string]string{
	"plan":         "plan.md",
	"architecture": "architecture.md",
	"requirements": "requirements.md",
	"scenarios":    "scenarios.md",
	"qa-summary":   "qa-summary.md",
	"run-summary":  "run-summary.md",
}

// phaseArtifactOrder defines the rendering order — same sequence the
// workflow-documents component writes them.
var phaseArtifactOrder = []string{
	"plan",
	"architecture",
	"requirements",
	"scenarios",
	"qa-summary",
	"run-summary",
}

// extractSlugArtifactName parses paths like
// /plan-manager/plans/{slug}/artifacts and
// /plan-manager/plans/{slug}/artifacts/{name}. Returns (slug, name) with
// name="" when the request targets the collection endpoint.
func extractSlugArtifactName(path string) (slug, name string) {
	idx := strings.Index(path, "/plans/")
	if idx == -1 {
		return "", ""
	}
	remainder := path[idx+len("/plans/"):]
	parts := strings.Split(strings.TrimSuffix(remainder, "/"), "/")
	if len(parts) < 2 || parts[1] != "artifacts" {
		return "", ""
	}
	slug = parts[0]
	if len(parts) >= 3 {
		name = parts[2]
	}
	return slug, name
}

// planArtifactsDir resolves the absolute path to .semspec/plans/{slug}/.
// Slug is assumed to have passed workflow.ValidateSlug at the routing layer.
func (c *Component) planArtifactsDir(slug string) string {
	return filepath.Join(c.resolveRepoRoot(), ".semspec", "plans", slug)
}

// handlePlanArtifactsList handles GET /plan-manager/plans/{slug}/artifacts.
// Returns the list of phase artifacts that currently exist on disk for
// the plan, in canonical phase order. Empty list (200) is the response
// shape when none have been written yet — the caller distinguishes
// "no artifacts yet" from "plan does not exist" via the prior GET on
// the plan itself.
func (c *Component) handlePlanArtifactsList(w http.ResponseWriter, _ *http.Request, slug string) {
	dir := c.planArtifactsDir(slug)

	artifacts := make([]PhaseArtifact, 0, len(phaseArtifactOrder))
	for _, name := range phaseArtifactOrder {
		filename := phaseArtifactAllowList[name]
		path := filepath.Join(dir, filename)
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			c.logger.Warn("Failed to stat phase artifact",
				"slug", slug, "filename", filename, "error", err)
			continue
		}
		if info.IsDir() {
			continue
		}
		artifacts = append(artifacts, PhaseArtifact{
			Name:       name,
			Filename:   filename,
			Size:       info.Size(),
			ModifiedAt: info.ModTime().UTC().Format(time.RFC3339),
		})
	}

	resp := PhaseArtifactsResponse{Slug: slug, Artifacts: artifacts}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		c.logger.Warn("Failed to encode artifacts response", "slug", slug, "error", err)
	}
}

// handlePlanArtifactContent handles GET /plan-manager/plans/{slug}/artifacts/{name}.
// Returns the raw markdown content for the named artifact. Name is
// restricted to the phaseArtifactAllowList — anything else 404s without
// touching the filesystem.
func (c *Component) handlePlanArtifactContent(w http.ResponseWriter, _ *http.Request, slug, name string) {
	filename, ok := phaseArtifactAllowList[name]
	if !ok {
		writeJSONError(w, "Unknown artifact", http.StatusNotFound)
		return
	}

	path := filepath.Join(c.planArtifactsDir(slug), filename)
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			writeJSONError(w, "Artifact not found", http.StatusNotFound)
			return
		}
		c.logger.Error("Failed to read phase artifact",
			"slug", slug, "name", name, "error", err)
		writeJSONError(w, "Failed to read artifact", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if _, err := w.Write(content); err != nil {
		c.logger.Warn("Failed to write artifact body", "slug", slug, "name", name, "error", err)
	}
}

// handleExportSpecs handles POST /plans/{slug}/export-specs.
// Generates per-requirement spec Markdown files in .semspec/specs/.
func (c *Component) handleExportSpecs(w http.ResponseWriter, r *http.Request, slug string) {
	repoRoot := c.getRepoRoot(w)
	if repoRoot == "" {
		return
	}

	plan, err := c.loadPlanCached(r.Context(), slug)
	if err != nil {
		c.logger.Error("Failed to load plan for export", "slug", slug, "error", err)
		writeJSONError(w, "Plan not found: "+err.Error(), http.StatusNotFound)
		return
	}

	files, err := workflow.ExportSpecFiles(r.Context(), plan, repoRoot)
	if err != nil {
		c.logger.Error("Failed to export specs", "slug", slug, "error", err)
		writeJSONError(w, "Failed to export specs: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Files []string `json:"files"`
		Count int      `json:"count"`
	}{
		Files: files,
		Count: len(files),
	})
}

// handleGenerateArchive handles POST /plans/{slug}/archive.
// Generates an archive Markdown document summarising the plan.
func (c *Component) handleGenerateArchive(w http.ResponseWriter, r *http.Request, slug string) {
	repoRoot := c.getRepoRoot(w)
	if repoRoot == "" {
		return
	}

	plan, err := c.loadPlanCached(r.Context(), slug)
	if err != nil {
		c.logger.Error("Failed to load plan for archive", "slug", slug, "error", err)
		writeJSONError(w, "Plan not found: "+err.Error(), http.StatusNotFound)
		return
	}

	filePath, err := workflow.GenerateArchive(r.Context(), plan, repoRoot)
	if err != nil {
		c.logger.Error("Failed to generate archive", "slug", slug, "error", err)
		writeJSONError(w, "Failed to generate archive: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		File string `json:"file"`
	}{
		File: filePath,
	})
}
