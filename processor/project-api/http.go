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

	// Read scaffold state if present.
	if scaffoldState, err := loadJSONFile[workflow.ScaffoldState](filepath.Join(semspecDir, workflow.ScaffoldFile)); err == nil {
		status.Scaffolded = true
		status.ScaffoldedAt = &scaffoldState.ScaffoldedAt
		status.ScaffoldedLanguages = scaffoldState.Languages
		status.ScaffoldedFiles = scaffoldState.FilesCreated
	}

	// Read per-file approval timestamps and project info from the actual config files.
	if hasProjectJSON {
		if pc, err := loadJSONFile[workflow.ProjectConfig](filepath.Join(semspecDir, workflow.ProjectConfigFile)); err == nil {
			status.ProjectApprovedAt = pc.ApprovedAt
			status.ProjectName = pc.Name
			status.ProjectDescription = pc.Description
		}
	}
	if hasChecklist {
		if cl, err := loadJSONFile[workflow.Checklist](filepath.Join(semspecDir, workflow.ChecklistFile)); err == nil {
			status.ChecklistApprovedAt = cl.ApprovedAt
		}
	}
	if hasStandards {
		if st, err := loadJSONFile[workflow.Standards](filepath.Join(semspecDir, workflow.StandardsFile)); err == nil {
			status.StandardsApprovedAt = st.ApprovedAt
		}
	}

	status.AllApproved = status.ProjectApprovedAt != nil &&
		status.ChecklistApprovedAt != nil &&
		status.StandardsApprovedAt != nil

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

// ApproveRequest is the request body for POST /api/project/approve.
type ApproveRequest struct {
	File string `json:"file"`
}

// ApproveResponse is the response from POST /api/project/approve.
type ApproveResponse struct {
	File        string    `json:"file"`
	ApprovedAt  time.Time `json:"approved_at"`
	AllApproved bool      `json:"all_approved"`
}

// handleApprove sets the approved_at timestamp on a config file and publishes
// a graph entity for the approval event.
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

	semspecDir := filepath.Join(c.repoPath, ".semspec")
	filePath := filepath.Join(semspecDir, req.File)

	if !fileExists(filePath) {
		http.Error(w, "config file not found: "+req.File, http.StatusNotFound)
		return
	}

	now := time.Now()

	// Read, set approved_at, write back — type depends on which file.
	switch req.File {
	case workflow.ProjectConfigFile:
		pc, err := loadJSONFile[workflow.ProjectConfig](filePath)
		if err != nil {
			http.Error(w, "failed to read "+req.File, http.StatusInternalServerError)
			return
		}
		pc.ApprovedAt = &now
		if err := writeJSONFile(filePath, pc); err != nil {
			http.Error(w, "failed to write "+req.File, http.StatusInternalServerError)
			return
		}

	case workflow.ChecklistFile:
		cl, err := loadJSONFile[workflow.Checklist](filePath)
		if err != nil {
			http.Error(w, "failed to read "+req.File, http.StatusInternalServerError)
			return
		}
		cl.ApprovedAt = &now
		if err := writeJSONFile(filePath, cl); err != nil {
			http.Error(w, "failed to write "+req.File, http.StatusInternalServerError)
			return
		}

	case workflow.StandardsFile:
		st, err := loadJSONFile[workflow.Standards](filePath)
		if err != nil {
			http.Error(w, "failed to read "+req.File, http.StatusInternalServerError)
			return
		}
		st.ApprovedAt = &now
		if err := writeJSONFile(filePath, st); err != nil {
			http.Error(w, "failed to write "+req.File, http.StatusInternalServerError)
			return
		}
	}

	c.logger.Info("Config file approved", "file", req.File, "approved_at", now)

	// Check if all three are now approved.
	allApproved := c.checkAllApproved(semspecDir)

	writeJSON(w, http.StatusOK, ApproveResponse{
		File:        req.File,
		ApprovedAt:  now,
		AllApproved: allApproved,
	})
}

// checkAllApproved reads all three config files and returns true if all have approved_at set.
func (c *Component) checkAllApproved(semspecDir string) bool {
	pc, err := loadJSONFile[workflow.ProjectConfig](filepath.Join(semspecDir, workflow.ProjectConfigFile))
	if err != nil || pc.ApprovedAt == nil {
		return false
	}
	cl, err := loadJSONFile[workflow.Checklist](filepath.Join(semspecDir, workflow.ChecklistFile))
	if err != nil || cl.ApprovedAt == nil {
		return false
	}
	st, err := loadJSONFile[workflow.Standards](filepath.Join(semspecDir, workflow.StandardsFile))
	if err != nil || st.ApprovedAt == nil {
		return false
	}
	return true
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
