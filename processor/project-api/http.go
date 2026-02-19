package projectapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// maxRequestBodySize limits POST body sizes to prevent DoS.
const maxRequestBodySize = 1 << 20 // 1 MB

// RegisterHTTPHandlers registers all project-api HTTP handlers under the given prefix.
// The prefix should be the path segment without a trailing slash (e.g. "api/project").
// Handlers are registered as:
//
//	GET  <prefix>/status
//	POST <prefix>/detect
//	POST <prefix>/generate-standards
//	POST <prefix>/init
func (c *Component) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	// Normalise: ensure leading slash and trailing slash.
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix = prefix + "/"
	}

	mux.HandleFunc(prefix+"status", c.handleStatus)
	mux.HandleFunc(prefix+"detect", c.handleDetect)
	mux.HandleFunc(prefix+"generate-standards", c.handleGenerateStandards)
	mux.HandleFunc(prefix+"init", c.handleInit)
}

// ----------------------------------------------------------------------------
// GET /api/project/status
// ----------------------------------------------------------------------------

// handleStatus returns the project initialization state.
// It reads the filesystem directly — no caching — so the response is always fresh.
func (c *Component) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	semspecDir := filepath.Join(c.repoPath, ".semspec")

	hasProjectJSON := fileExists(filepath.Join(semspecDir, workflow.ProjectConfigFile))
	hasChecklist := fileExists(filepath.Join(semspecDir, workflow.ChecklistFile))
	hasStandards := fileExists(filepath.Join(semspecDir, workflow.StandardsFile))

	sopCount := countMDFiles(filepath.Join(semspecDir, "sources", "docs"))

	status := workflow.InitStatus{
		Initialized:    hasProjectJSON && hasChecklist && hasStandards,
		HasProjectJSON: hasProjectJSON,
		HasChecklist:   hasChecklist,
		HasStandards:   hasStandards,
		SOPCount:       sopCount,
		WorkspacePath:  c.repoPath,
	}

	writeJSON(w, http.StatusOK, status)
}

// ----------------------------------------------------------------------------
// POST /api/project/detect
// ----------------------------------------------------------------------------

// handleDetect runs the stack detector and returns the result.
// No LLM calls are made — detection is purely filesystem-based.
func (c *Component) handleDetect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	detector := workflow.NewFileSystemDetector()
	result, err := detector.Detect(c.repoPath)
	if err != nil {
		c.logger.Error("Detection failed", "repo_path", c.repoPath, "error", err)
		http.Error(w, "Detection failed", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// ----------------------------------------------------------------------------
// POST /api/project/generate-standards
// ----------------------------------------------------------------------------

// GenerateStandardsRequest is the request body for POST /api/project/generate-standards.
type GenerateStandardsRequest struct {
	// Detection is the full DetectionResult from /detect.
	Detection workflow.DetectionResult `json:"detection"`

	// ExistingDocsContent maps relative file path to file content.
	// The UI reads these files and sends them so the LLM has project context.
	ExistingDocsContent map[string]string `json:"existing_docs_content"`
}

// GenerateStandardsResponse is the response body for POST /api/project/generate-standards.
type GenerateStandardsResponse struct {
	// Rules is the generated set of project standards.
	// Empty in the stub implementation — LLM integration is Phase 3.
	Rules []workflow.Rule `json:"rules"`

	// TokenEstimate is the approximate token count for all rules.
	TokenEstimate int `json:"token_estimate"`
}

// handleGenerateStandards is a stub endpoint that returns empty rules.
// LLM integration will be added in Phase 3.
func (c *Component) handleGenerateStandards(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse and discard the request — the stub ignores it.
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	var req GenerateStandardsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Allow empty or missing body — stub works without it.
		c.logger.Debug("generate-standards: could not parse request body (ignored in stub)", "error", err)
	}

	resp := GenerateStandardsResponse{
		Rules:         []workflow.Rule{},
		TokenEstimate: 0,
	}

	writeJSON(w, http.StatusOK, resp)
}

// ----------------------------------------------------------------------------
// POST /api/project/init
// ----------------------------------------------------------------------------

// ProjectInitInput is the project metadata section of the init request.
type ProjectInitInput struct {
	// Name is the human-readable project name.
	Name string `json:"name"`

	// Description is a brief description of the project.
	Description string `json:"description,omitempty"`

	// Languages lists detected/confirmed language names (e.g. ["Go", "TypeScript"]).
	Languages []string `json:"languages"`

	// Frameworks lists detected/confirmed framework names (e.g. ["SvelteKit"]).
	Frameworks []string `json:"frameworks"`

	// Repository is the VCS remote URL.
	Repository string `json:"repository,omitempty"`
}

// StandardsInput is the standards section of the init request.
type StandardsInput struct {
	// Version is the standards schema version (e.g. "1.0.0").
	Version string `json:"version"`

	// Rules is the confirmed set of project standards.
	Rules []workflow.Rule `json:"rules"`
}

// InitRequest is the request body for POST /api/project/init.
type InitRequest struct {
	// Project contains the confirmed project metadata.
	Project ProjectInitInput `json:"project"`

	// Checklist contains the confirmed quality gate checks.
	Checklist []workflow.Check `json:"checklist"`

	// Standards contains the confirmed project standards.
	Standards StandardsInput `json:"standards"`
}

// InitResponse is the response body for POST /api/project/init.
type InitResponse struct {
	// Success is true when all files were written without error.
	Success bool `json:"success"`

	// FilesWritten lists the relative paths of files written (relative to repo root).
	FilesWritten []string `json:"files_written"`
}

// handleInit writes all confirmed configuration to disk.
// After this call, components can immediately read the written files.
func (c *Component) handleInit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var req InitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Project.Name == "" {
		http.Error(w, "project.name is required", http.StatusBadRequest)
		return
	}

	semspecDir := filepath.Join(c.repoPath, ".semspec")
	if err := os.MkdirAll(semspecDir, 0755); err != nil {
		c.logger.Error("Failed to create .semspec directory", "error", err)
		http.Error(w, "Failed to create .semspec directory", http.StatusInternalServerError)
		return
	}

	// Create sources/docs directory for future SOPs.
	sopDir := filepath.Join(semspecDir, "sources", "docs")
	if err := os.MkdirAll(sopDir, 0755); err != nil {
		c.logger.Error("Failed to create sources/docs directory", "error", err)
		http.Error(w, "Failed to create SOP directory", http.StatusInternalServerError)
		return
	}

	now := time.Now()
	var written []string

	// Write project.json
	projectConfig := buildProjectConfig(req.Project, now)
	if err := writeJSONFile(filepath.Join(semspecDir, workflow.ProjectConfigFile), projectConfig); err != nil {
		c.logger.Error("Failed to write project.json", "error", err)
		http.Error(w, "Failed to write project.json", http.StatusInternalServerError)
		return
	}
	written = append(written, ".semspec/"+workflow.ProjectConfigFile)

	// Write checklist.json
	checklist := workflow.Checklist{
		Version:   "1.0.0",
		CreatedAt: now,
		Checks:    normaliseChecks(req.Checklist),
	}
	if err := writeJSONFile(filepath.Join(semspecDir, workflow.ChecklistFile), checklist); err != nil {
		c.logger.Error("Failed to write checklist.json", "error", err)
		http.Error(w, "Failed to write checklist.json", http.StatusInternalServerError)
		return
	}
	written = append(written, ".semspec/"+workflow.ChecklistFile)

	// Write standards.json
	standards := workflow.Standards{
		Version:       req.Standards.Version,
		GeneratedAt:   now,
		TokenEstimate: estimateTokens(req.Standards.Rules),
		Rules:         normaliseRules(req.Standards.Rules),
	}
	if err := writeJSONFile(filepath.Join(semspecDir, workflow.StandardsFile), standards); err != nil {
		c.logger.Error("Failed to write standards.json", "error", err)
		http.Error(w, "Failed to write standards.json", http.StatusInternalServerError)
		return
	}
	written = append(written, ".semspec/"+workflow.StandardsFile)

	c.logger.Info("Project initialized",
		"name", req.Project.Name,
		"files", written)

	writeJSON(w, http.StatusOK, InitResponse{
		Success:      true,
		FilesWritten: written,
	})
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

// buildProjectConfig converts the wizard's ProjectInitInput into a ProjectConfig
// suitable for writing to disk.
func buildProjectConfig(input ProjectInitInput, now time.Time) workflow.ProjectConfig {
	languages := make([]workflow.LanguageInfo, 0, len(input.Languages))
	for i, lang := range input.Languages {
		languages = append(languages, workflow.LanguageInfo{
			Name:    lang,
			Version: nil,
			Primary: i == 0,
		})
	}

	frameworks := make([]workflow.FrameworkInfo, 0, len(input.Frameworks))
	for _, fw := range input.Frameworks {
		frameworks = append(frameworks, workflow.FrameworkInfo{
			Name: fw,
		})
	}

	return workflow.ProjectConfig{
		Name:          input.Name,
		Description:   input.Description,
		Version:       "1.0.0",
		InitializedAt: now,
		Languages:     languages,
		Frameworks:    frameworks,
		Tooling:       workflow.ProjectTooling{},
		Repository: workflow.RepositoryInfo{
			URL: input.Repository,
		},
	}
}

// normaliseChecks ensures the check slice is never nil and fills in default
// values for optional fields (WorkingDir defaults to ".").
func normaliseChecks(checks []workflow.Check) []workflow.Check {
	if checks == nil {
		return []workflow.Check{}
	}
	out := make([]workflow.Check, len(checks))
	for i, ch := range checks {
		if ch.WorkingDir == "" {
			ch.WorkingDir = "."
		}
		if ch.Timeout == "" {
			ch.Timeout = "120s"
		}
		if ch.Trigger == nil {
			ch.Trigger = []string{}
		}
		out[i] = ch
	}
	return out
}

// normaliseRules ensures the rules slice is never nil.
func normaliseRules(rules []workflow.Rule) []workflow.Rule {
	if rules == nil {
		return []workflow.Rule{}
	}
	return rules
}

// estimateTokens provides a rough token estimate for a set of rules.
// Each rule is approximated at 40 tokens (text + metadata overhead).
func estimateTokens(rules []workflow.Rule) int {
	return len(rules) * 40
}

// fileExists reports whether the given path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// countMDFiles counts .md files directly in the given directory.
// Returns 0 gracefully when the directory does not exist.
func countMDFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			count++
		}
	}
	return count
}

// writeJSON marshals v as JSON and writes it to w with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Response is already partially written; log only.
		// slog is used in callers; avoid importing here unnecessarily.
		_ = err
	}
}

// writeJSONFile marshals v as indented JSON and writes it to path,
// creating parent directories as needed.
func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
