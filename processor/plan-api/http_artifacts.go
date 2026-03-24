package planapi

import (
	"encoding/json"
	"net/http"

	"github.com/c360studio/semspec/workflow"
)

// handleExportSpecs handles POST /plans/{slug}/export-specs.
// Generates per-requirement spec Markdown files in .semspec/specs/.
func (c *Component) handleExportSpecs(w http.ResponseWriter, r *http.Request, slug string) {
	repoRoot := c.getRepoRoot(w)
	if repoRoot == "" {
		return
	}

	c.mu.RLock()
	kv := c.kvStore
	c.mu.RUnlock()

	files, err := workflow.ExportSpecFiles(r.Context(), kv, repoRoot, slug)
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

	c.mu.RLock()
	kv := c.kvStore
	c.mu.RUnlock()

	filePath, err := workflow.GenerateArchive(r.Context(), kv, repoRoot, slug)
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
