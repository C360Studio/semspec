package projectmanager

import (
	"context"
	"encoding/json"
	"errors"
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
//	GET  <prefix>/wizard
//	POST <prefix>/scaffold
//	POST <prefix>/detect
//	POST <prefix>/generate-standards
//	POST <prefix>/init
//	POST <prefix>/approve
func (c *Component) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	// Normalise: ensure leading slash and trailing slash.
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix = prefix + "/"
	}

	mux.HandleFunc(prefix+"status", c.handleStatus)
	mux.HandleFunc(prefix+"wizard", c.handleWizard)
	mux.HandleFunc(prefix+"scaffold", c.handleScaffold)
	mux.HandleFunc(prefix+"detect", c.handleDetect)
	mux.HandleFunc(prefix+"generate-standards", c.handleGenerateStandards)
	mux.HandleFunc(prefix+"init", c.handleInit)
	mux.HandleFunc(prefix+"approve", c.handleApprove)
	mux.HandleFunc(prefix+"config", c.handleConfig)
	mux.HandleFunc(prefix+"checklist", c.handleChecklist)
	mux.HandleFunc(prefix+"standards", c.handleStandards)
}

// ----------------------------------------------------------------------------
// GET /api/project/status
// ----------------------------------------------------------------------------

// handleStatus returns the project initialization state.
// Reads from the in-memory cache (populated on Start via reconcile).
func (c *Component) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s := c.getStore()

	semspecDir := filepath.Join(c.repoPath, ".semspec")

	// Read from cache when available, fall back to files if store not yet initialized.
	var pc *workflow.ProjectConfig
	var cl *workflow.Checklist
	var st *workflow.Standards
	if s != nil {
		pc = s.getConfig()
		cl = s.getChecklist()
		st = s.getStandards()
	} else {
		if v, err := loadJSONFile[workflow.ProjectConfig](filepath.Join(semspecDir, workflow.ProjectConfigFile)); err == nil {
			pc = &v
		}
		if v, err := loadJSONFile[workflow.Checklist](filepath.Join(semspecDir, workflow.ChecklistFile)); err == nil {
			cl = &v
		}
		if v, err := loadJSONFile[workflow.Standards](filepath.Join(semspecDir, workflow.StandardsFile)); err == nil {
			st = &v
		}
	}

	sopCount := countMDFiles(filepath.Join(semspecDir, "sources", "docs"))

	status := workflow.InitStatus{
		Initialized:    pc != nil && cl != nil && st != nil,
		HasProjectJSON: pc != nil,
		HasChecklist:   cl != nil,
		HasStandards:   st != nil,
		SOPCount:       sopCount,
		WorkspacePath:  c.repoPath,
	}

	// Read scaffold state if present (not cached — rare read).
	if scaffoldState, err := loadJSONFile[workflow.ScaffoldState](filepath.Join(semspecDir, workflow.ScaffoldFile)); err == nil {
		status.Scaffolded = true
		status.ScaffoldedAt = &scaffoldState.ScaffoldedAt
		status.ScaffoldedLanguages = scaffoldState.Languages
		status.ScaffoldedFiles = scaffoldState.FilesCreated
	}

	if pc != nil {
		status.ProjectApprovedAt = pc.ApprovedAt
		status.ProjectName = pc.Name
		status.ProjectDescription = pc.Description
		status.ProjectOrg = pc.Org
		status.ProjectPlatform = pc.Platform
	}
	if cl != nil {
		status.ChecklistApprovedAt = cl.ApprovedAt
	}
	if st != nil {
		status.StandardsApprovedAt = st.ApprovedAt
	}

	status.AllApproved = status.ProjectApprovedAt != nil &&
		status.ChecklistApprovedAt != nil &&
		status.StandardsApprovedAt != nil

	status.EntityPrefix = workflow.EntityPrefix()

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

	// Org is the organization segment for entity IDs (default: "semspec").
	Org string `json:"org,omitempty"`

	// Platform is the project identifier for entity IDs.
	// Auto-derived from Name if not set. Should be unique within your org
	// to avoid collisions when federating across semspec instances.
	Platform string `json:"platform,omitempty"`

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

// handleInit writes all confirmed configuration to disk and cache.
// After this call, components can immediately read the config from cache.
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
	if err := c.ensureInitDirs(semspecDir); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	now := time.Now()
	projectConfig := buildProjectConfig(req.Project, now)
	projectConfig.UpdatedAt = now
	checklist := workflow.Checklist{
		Version:   "1.0.0",
		CreatedAt: now,
		UpdatedAt: now,
		Checks:    normaliseChecks(req.Checklist),
	}
	standards := workflow.Standards{
		Version:       req.Standards.Version,
		GeneratedAt:   now,
		UpdatedAt:     now,
		TokenEstimate: estimateTokens(req.Standards.Rules),
		Rules:         normaliseRules(req.Standards.Rules),
	}

	written, err := c.persistInitConfigs(r, w, semspecDir, projectConfig, checklist, standards)
	if err != nil {
		// persistInitConfigs already wrote the HTTP error response.
		return
	}

	c.logger.Info("Project initialized", "name", req.Project.Name, "files", written)
	writeJSON(w, http.StatusOK, InitResponse{Success: true, FilesWritten: written})
}

// ensureInitDirs creates the .semspec and sources/docs directories.
// Returns a user-facing error message on failure (already logged).
func (c *Component) ensureInitDirs(semspecDir string) error {
	if err := os.MkdirAll(semspecDir, 0755); err != nil {
		c.logger.Error("Failed to create .semspec directory", "error", err)
		return errors.New("Failed to create .semspec directory")
	}
	sopDir := filepath.Join(semspecDir, "sources", "docs")
	if err := os.MkdirAll(sopDir, 0755); err != nil {
		c.logger.Error("Failed to create sources/docs directory", "error", err)
		return errors.New("Failed to create SOP directory")
	}
	return nil
}

// persistInitConfigs writes the three config files through the store (when
// available) or directly to disk. It writes the HTTP error and returns a
// non-nil error when any write fails so handleInit can bail early.
func (c *Component) persistInitConfigs(
	r *http.Request, w http.ResponseWriter,
	semspecDir string,
	projectConfig workflow.ProjectConfig,
	checklist workflow.Checklist,
	standards workflow.Standards,
) ([]string, error) {
	var written []string
	s := c.getStore()
	if s != nil {
		return c.persistViaStore(r, w, s, projectConfig, checklist, standards)
	}
	// Fallback: direct file write (pre-Start).
	if err := writeJSONFile(filepath.Join(semspecDir, workflow.ProjectConfigFile), projectConfig); err != nil {
		c.logger.Error("Failed to write project.json", "error", err)
		http.Error(w, "Failed to write project.json", http.StatusInternalServerError)
		return nil, err
	}
	written = append(written, ".semspec/"+workflow.ProjectConfigFile)

	if err := writeJSONFile(filepath.Join(semspecDir, workflow.ChecklistFile), checklist); err != nil {
		c.logger.Error("Failed to write checklist.json", "error", err)
		http.Error(w, "Failed to write checklist.json", http.StatusInternalServerError)
		return nil, err
	}
	written = append(written, ".semspec/"+workflow.ChecklistFile)

	if err := writeJSONFile(filepath.Join(semspecDir, workflow.StandardsFile), standards); err != nil {
		c.logger.Error("Failed to write standards.json", "error", err)
		http.Error(w, "Failed to write standards.json", http.StatusInternalServerError)
		return nil, err
	}
	written = append(written, ".semspec/"+workflow.StandardsFile)
	return written, nil
}

// persistViaStore writes configs through the component store (triples + cache + file).
func (c *Component) persistViaStore(
	r *http.Request, w http.ResponseWriter,
	s *projectStore,
	projectConfig workflow.ProjectConfig,
	checklist workflow.Checklist,
	standards workflow.Standards,
) ([]string, error) {
	var written []string
	if err := s.saveConfig(r.Context(), &projectConfig); err != nil {
		c.logger.Error("Failed to save project config", "error", err)
		http.Error(w, "Failed to write project.json", http.StatusInternalServerError)
		return nil, err
	}
	written = append(written, ".semspec/"+workflow.ProjectConfigFile)

	if err := s.saveChecklist(r.Context(), &checklist); err != nil {
		c.logger.Error("Failed to save checklist", "error", err)
		http.Error(w, "Failed to write checklist.json", http.StatusInternalServerError)
		return nil, err
	}
	written = append(written, ".semspec/"+workflow.ChecklistFile)

	if err := s.saveStandards(r.Context(), &standards); err != nil {
		c.logger.Error("Failed to save standards", "error", err)
		http.Error(w, "Failed to write standards.json", http.StatusInternalServerError)
		return nil, err
	}
	written = append(written, ".semspec/"+workflow.StandardsFile)
	return written, nil
}

// ----------------------------------------------------------------------------
// GET /api/project/wizard
// ----------------------------------------------------------------------------

// WizardLanguage describes a supported language for the setup wizard.
type WizardLanguage struct {
	Name   string `json:"name"`
	Marker string `json:"marker"`
	HasAST bool   `json:"has_ast"`
}

// WizardFramework describes a supported framework for the setup wizard.
type WizardFramework struct {
	Name     string `json:"name"`
	Language string `json:"language"`
}

// WizardResponse is the response from GET /api/project/wizard.
type WizardResponse struct {
	Languages  []WizardLanguage  `json:"languages"`
	Frameworks []WizardFramework `json:"frameworks"`
}

// supportedLanguages defines the languages we can fully support (AST + checklist).
// Order matters — it determines display order in the UI wizard.
var supportedLanguages = []WizardLanguage{
	{Name: "Go", Marker: "go.mod", HasAST: true},
	{Name: "Python", Marker: "requirements.txt", HasAST: true},
	{Name: "TypeScript", Marker: "tsconfig.json", HasAST: true},
	{Name: "JavaScript", Marker: "package.json", HasAST: true},
	{Name: "Java", Marker: "pom.xml", HasAST: true},
	{Name: "Svelte", Marker: "svelte.config.js", HasAST: true},
}

// supportedFrameworks defines the frameworks available in the wizard.
var supportedFrameworks = []WizardFramework{
	{Name: "Flask", Language: "Python"},
	{Name: "SvelteKit", Language: "Svelte"},
	{Name: "Express", Language: "JavaScript"},
}

// handleWizard returns the supported languages and frameworks for the setup wizard.
func (c *Component) handleWizard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, http.StatusOK, WizardResponse{
		Languages:  supportedLanguages,
		Frameworks: supportedFrameworks,
	})
}

// ----------------------------------------------------------------------------
// POST /api/project/scaffold
// ----------------------------------------------------------------------------

// ScaffoldRequest is the request body for POST /api/project/scaffold.
type ScaffoldRequest struct {
	Languages  []string `json:"languages"`
	Frameworks []string `json:"frameworks"`
}

// ScaffoldResponse is the response from POST /api/project/scaffold.
type ScaffoldResponse struct {
	FilesCreated []string `json:"files_created"`
	SemspecDir   string   `json:"semspec_dir"`
}

// markerFile is a file path and its minimal content for scaffold creation.
type markerFile struct {
	Path    string
	Content string
}

// languageMarkerFiles maps a language name to the marker files to create.
// Each file has a name and minimal content.
var languageMarkerFiles = map[string][]markerFile{
	"Go": {
		{Path: "go.mod", Content: "module project\n\ngo 1.22\n"},
	},
	"Python": {
		{Path: "requirements.txt", Content: ""},
	},
	"TypeScript": {
		{Path: "tsconfig.json", Content: `{"compilerOptions":{"strict":true,"target":"ES2022","module":"ESNext","moduleResolution":"bundler"}}` + "\n"},
		{Path: "package.json", Content: `{"name":"project","version":"0.1.0","private":true}` + "\n"},
	},
	"JavaScript": {
		{Path: "package.json", Content: `{"name":"project","version":"0.1.0","private":true}` + "\n"},
	},
	"Java": {
		{Path: "pom.xml", Content: "<project>\n  <modelVersion>4.0.0</modelVersion>\n  <groupId>com.example</groupId>\n  <artifactId>project</artifactId>\n  <version>0.1.0</version>\n</project>\n"},
	},
	"Svelte": {
		{Path: "svelte.config.js", Content: "import adapter from '@sveltejs/adapter-auto';\nexport default { kit: { adapter: adapter() } };\n"},
		{Path: "package.json", Content: `{"name":"project","version":"0.1.0","private":true}` + "\n"},
	},
}

// frameworkMarkerFiles maps a framework to additional marker files.
var frameworkMarkerFiles = map[string][]markerFile{
	"Flask": {
		{Path: "app.py", Content: "# Flask application entry point\n"},
	},
	"SvelteKit": {
		{Path: "src/routes/+page.svelte", Content: "<!-- SvelteKit home page -->\n"},
	},
	"Express": {
		{Path: "index.js", Content: "// Express application entry point\n"},
	},
}

// handleScaffold creates marker files from wizard selections.
// No LLM calls — purely deterministic file creation.
func (c *Component) handleScaffold(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var req ScaffoldRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.Languages) == 0 {
		http.Error(w, "at least one language is required", http.StatusBadRequest)
		return
	}

	// Create .semspec directory.
	semspecDir := filepath.Join(c.repoPath, ".semspec")
	if err := os.MkdirAll(semspecDir, 0755); err != nil {
		c.logger.Error("Failed to create .semspec directory", "error", err)
		http.Error(w, "Failed to create .semspec directory", http.StatusInternalServerError)
		return
	}

	filesCreated := c.writeMarkerFiles(req.Languages, req.Frameworks)

	// Persist scaffold state.
	now := time.Now()
	scaffoldState := workflow.ScaffoldState{
		ScaffoldedAt: now,
		Languages:    req.Languages,
		Frameworks:   req.Frameworks,
		FilesCreated: filesCreated,
	}
	if err := writeJSONFile(filepath.Join(semspecDir, workflow.ScaffoldFile), scaffoldState); err != nil {
		c.logger.Error("Failed to write scaffold state", "error", err)
		// Non-fatal — the files were still created.
	}

	c.logger.Info("Project scaffolded",
		"languages", req.Languages,
		"frameworks", req.Frameworks,
		"files", filesCreated)

	writeJSON(w, http.StatusOK, ScaffoldResponse{
		FilesCreated: filesCreated,
		SemspecDir:   ".semspec",
	})
}

// writeMarkerFiles creates marker files for the given languages and frameworks,
// deduplicating by path. Returns the list of files created.
func (c *Component) writeMarkerFiles(languages, frameworks []string) []string {
	var filesCreated []string
	created := make(map[string]bool)

	writeMarkers := func(markers []markerFile) {
		for _, m := range markers {
			if created[m.Path] {
				continue
			}
			filePath := filepath.Join(c.repoPath, m.Path)
			if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
				c.logger.Error("Failed to create directory", "path", filepath.Dir(filePath), "error", err)
				continue
			}
			if err := os.WriteFile(filePath, []byte(m.Content), 0644); err != nil {
				c.logger.Error("Failed to write marker file", "path", m.Path, "error", err)
				continue
			}
			filesCreated = append(filesCreated, m.Path)
			created[m.Path] = true
		}
	}

	for _, lang := range languages {
		if markers, ok := languageMarkerFiles[lang]; ok {
			writeMarkers(markers)
		} else {
			c.logger.Warn("Unknown language in scaffold request", "language", lang)
		}
	}
	for _, fw := range frameworks {
		if markers, ok := frameworkMarkerFiles[fw]; ok {
			writeMarkers(markers)
		}
	}

	return filesCreated
}

// ----------------------------------------------------------------------------
// POST /api/project/approve
// ----------------------------------------------------------------------------

// handleApprove sets the approved_at timestamp on a config file and writes
// through to cache, triples, and file.
func (c *Component) handleApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var req ApproveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate file name is one of the three config files.
	switch req.File {
	case workflow.ProjectConfigFile, workflow.ChecklistFile, workflow.StandardsFile:
		// valid
	default:
		http.Error(w, "file must be one of: project.json, checklist.json, standards.json", http.StatusBadRequest)
		return
	}

	s := c.getStore()
	now := time.Now()

	switch req.File {
	case workflow.ProjectConfigFile:
		pc := c.loadConfig(s)
		if pc == nil {
			http.Error(w, "config file not found: "+req.File, http.StatusNotFound)
			return
		}
		updated := *pc
		updated.ApprovedAt = &now
		updated.UpdatedAt = now
		if err := c.saveConfigThrough(r.Context(), s, &updated); err != nil {
			http.Error(w, "failed to write "+req.File, http.StatusInternalServerError)
			return
		}

	case workflow.ChecklistFile:
		cl := c.loadChecklist(s)
		if cl == nil {
			http.Error(w, "config file not found: "+req.File, http.StatusNotFound)
			return
		}
		updated := *cl
		updated.ApprovedAt = &now
		updated.UpdatedAt = now
		if err := c.saveChecklistThrough(r.Context(), s, &updated); err != nil {
			http.Error(w, "failed to write "+req.File, http.StatusInternalServerError)
			return
		}

	case workflow.StandardsFile:
		st := c.loadStandards(s)
		if st == nil {
			http.Error(w, "config file not found: "+req.File, http.StatusNotFound)
			return
		}
		updated := *st
		updated.ApprovedAt = &now
		updated.UpdatedAt = now
		if err := c.saveStandardsThrough(r.Context(), s, &updated); err != nil {
			http.Error(w, "failed to write "+req.File, http.StatusInternalServerError)
			return
		}
	}

	c.logger.Info("Config file approved", "file", req.File, "approved_at", now)

	// Check if all three are now approved.
	allApproved := c.checkAllApproved(s)

	writeJSON(w, http.StatusOK, ApproveResponse{
		File:        req.File,
		ApprovedAt:  now,
		AllApproved: allApproved,
	})
}

// checkAllApproved checks whether all three config files have been approved.
func (c *Component) checkAllApproved(s *projectStore) bool {
	pc := c.loadConfig(s)
	if pc == nil || pc.ApprovedAt == nil {
		return false
	}
	cl := c.loadChecklist(s)
	if cl == nil || cl.ApprovedAt == nil {
		return false
	}
	st := c.loadStandards(s)
	if st == nil || st.ApprovedAt == nil {
		return false
	}
	return true
}

// ----------------------------------------------------------------------------
// PATCH /api/project/config
// ----------------------------------------------------------------------------

// handleConfig handles PATCH /api/project/config.
// Updates project.json fields. Org and platform changes are only allowed
// before the first plan is created (no entities in graph = safe to rename).
func (c *Component) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s := c.getStore()
	pc := c.loadConfig(s)
	if pc == nil {
		http.Error(w, "project.json not found — run init first", http.StatusNotFound)
		return
	}

	var req ConfigUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Check if org/platform change is requested and whether it's safe.
	prefixChanging := (req.Org != nil && *req.Org != pc.Org) ||
		(req.Platform != nil && *req.Platform != pc.Platform)

	if prefixChanging {
		// Only allow prefix changes before first plan exists.
		semspecDir := filepath.Join(c.repoPath, ".semspec")
		defaultProjectDir := filepath.Join(semspecDir, "projects", "default", "plans")
		entries, _ := os.ReadDir(defaultProjectDir)
		if len(entries) > 0 {
			http.Error(w, "Cannot change org/platform after plans exist — entity IDs would diverge", http.StatusConflict)
			return
		}
	}

	// Apply updates to a copy.
	updated := *pc
	if req.Name != nil {
		updated.Name = *req.Name
	}
	if req.Description != nil {
		updated.Description = *req.Description
	}
	if req.Org != nil {
		updated.Org = *req.Org
	}
	if req.Platform != nil {
		updated.Platform = *req.Platform
	}
	updated.UpdatedAt = time.Now()

	if err := c.saveConfigThrough(r.Context(), s, &updated); err != nil {
		c.logger.Error("Failed to save project config", "error", err)
		http.Error(w, "Failed to save config", http.StatusInternalServerError)
		return
	}

	// Re-initialize the entity prefix with updated values.
	workflow.InitEntityPrefix(updated.Org, updated.Platform, updated.Name)

	writeJSON(w, http.StatusOK, updated)
}

// ----------------------------------------------------------------------------
// GET/PATCH /api/project/checklist
// ----------------------------------------------------------------------------

// handleChecklist handles GET and PATCH for .semspec/checklist.json.
func (c *Component) handleChecklist(w http.ResponseWriter, r *http.Request) {
	s := c.getStore()

	switch r.Method {
	case http.MethodGet:
		cl := c.loadChecklist(s)
		if cl == nil {
			http.Error(w, "checklist.json not found", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, cl)

	case http.MethodPatch:
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
		var req ChecklistUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		cl := c.loadChecklist(s)
		if cl == nil {
			http.Error(w, "checklist.json not found — run init first", http.StatusNotFound)
			return
		}

		updated := *cl
		updated.Checks = normaliseChecks(req.Checks)
		updated.UpdatedAt = time.Now()
		if err := c.saveChecklistThrough(r.Context(), s, &updated); err != nil {
			http.Error(w, "Failed to save checklist", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, updated)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// ----------------------------------------------------------------------------
// GET/PATCH /api/project/standards
// ----------------------------------------------------------------------------

// handleStandards handles GET and PATCH for .semspec/standards.json.
func (c *Component) handleStandards(w http.ResponseWriter, r *http.Request) {
	s := c.getStore()

	switch r.Method {
	case http.MethodGet:
		st := c.loadStandards(s)
		if st == nil {
			http.Error(w, "standards.json not found", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, st)

	case http.MethodPatch:
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
		var req StandardsUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		st := c.loadStandards(s)
		if st == nil {
			http.Error(w, "standards.json not found — run init first", http.StatusNotFound)
			return
		}

		updated := *st
		updated.Rules = req.Rules
		// Recalculate token estimate (~4 chars per token, rough).
		total := 0
		for _, rule := range updated.Rules {
			total += len(rule.Text)
		}
		updated.TokenEstimate = total / 4
		updated.UpdatedAt = time.Now()

		if err := c.saveStandardsThrough(r.Context(), s, &updated); err != nil {
			http.Error(w, "Failed to save standards", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, updated)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// ----------------------------------------------------------------------------
// Store access helpers — cache-first with file fallback
// ----------------------------------------------------------------------------

// loadConfig reads project config from cache, or falls back to file if store is nil.
func (c *Component) loadConfig(s *projectStore) *workflow.ProjectConfig {
	if s != nil {
		return s.getConfig()
	}
	pc, err := loadJSONFile[workflow.ProjectConfig](filepath.Join(c.repoPath, ".semspec", workflow.ProjectConfigFile))
	if err != nil {
		return nil
	}
	return &pc
}

// loadChecklist reads checklist from cache, or falls back to file if store is nil.
func (c *Component) loadChecklist(s *projectStore) *workflow.Checklist {
	if s != nil {
		return s.getChecklist()
	}
	cl, err := loadJSONFile[workflow.Checklist](filepath.Join(c.repoPath, ".semspec", workflow.ChecklistFile))
	if err != nil {
		return nil
	}
	return &cl
}

// loadStandards reads standards from cache, or falls back to file if store is nil.
func (c *Component) loadStandards(s *projectStore) *workflow.Standards {
	if s != nil {
		return s.getStandards()
	}
	st, err := loadJSONFile[workflow.Standards](filepath.Join(c.repoPath, ".semspec", workflow.StandardsFile))
	if err != nil {
		return nil
	}
	return &st
}

// saveConfigThrough writes project config through store (triples + cache + file),
// or falls back to direct file write if store is nil.
func (c *Component) saveConfigThrough(ctx context.Context, s *projectStore, pc *workflow.ProjectConfig) error {
	if s != nil {
		return s.saveConfig(ctx, pc)
	}
	return writeJSONFile(filepath.Join(c.repoPath, ".semspec", workflow.ProjectConfigFile), pc)
}

// saveChecklistThrough writes checklist through store, or falls back to file.
func (c *Component) saveChecklistThrough(ctx context.Context, s *projectStore, cl *workflow.Checklist) error {
	if s != nil {
		return s.saveChecklist(ctx, cl)
	}
	return writeJSONFile(filepath.Join(c.repoPath, ".semspec", workflow.ChecklistFile), cl)
}

// saveStandardsThrough writes standards through store, or falls back to file.
func (c *Component) saveStandardsThrough(ctx context.Context, s *projectStore, st *workflow.Standards) error {
	if s != nil {
		return s.saveStandards(ctx, st)
	}
	return writeJSONFile(filepath.Join(c.repoPath, ".semspec", workflow.StandardsFile), st)
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
		Org:           input.Org,
		Platform:      input.Platform,
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

// readJSONFile reads and unmarshals a JSON file into the given type.
func loadJSONFile[T any](path string) (T, error) {
	var zero T
	data, err := os.ReadFile(path)
	if err != nil {
		return zero, err
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return zero, err
	}
	return v, nil
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
