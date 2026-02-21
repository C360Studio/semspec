package projectapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// setupTestComponent creates a Component wired to a temp repo root.
func setupTestComponent(t *testing.T) (*Component, string) {
	t.Helper()
	repoRoot := t.TempDir()
	c := &Component{
		name:     "project-api",
		config:   Config{RepoPath: repoRoot},
		repoPath: repoRoot,
		logger:   slog.Default(),
	}
	return c, repoRoot
}

// registerHandlers wires the component's handlers into a fresh mux and returns a test server.
func registerHandlers(c *Component) *httptest.Server {
	mux := http.NewServeMux()
	c.RegisterHTTPHandlers("api/project", mux)
	return httptest.NewServer(mux)
}

// readJSONFile reads and unmarshals a JSON file into dst.
func readJSONFile(t *testing.T, path string, dst any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Base(path), err)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		t.Fatalf("parse %s: %v", filepath.Base(path), err)
	}
}

// TestHandleStatus_Uninitialized verifies the status response when no config files exist.
func TestHandleStatus_Uninitialized(t *testing.T) {
	c, _ := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/project/status")
	if err != nil {
		t.Fatalf("GET /api/project/status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var status workflow.InitStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if status.Initialized {
		t.Error("initialized should be false when no config files exist")
	}
	if status.HasProjectJSON {
		t.Error("has_project_json should be false")
	}
	if status.HasChecklist {
		t.Error("has_checklist should be false")
	}
	if status.HasStandards {
		t.Error("has_standards should be false")
	}
	if status.SOPCount != 0 {
		t.Errorf("sop_count should be 0, got %d", status.SOPCount)
	}
}

// TestHandleStatus_PartialInit verifies status when only project.json exists.
func TestHandleStatus_PartialInit(t *testing.T) {
	c, repoRoot := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	// Create .semspec directory and project.json only
	semspecDir := filepath.Join(repoRoot, ".semspec")
	if err := os.MkdirAll(semspecDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(semspecDir, "project.json"), []byte(`{}`), 0644); err != nil {
		t.Fatalf("write project.json: %v", err)
	}

	resp, err := http.Get(srv.URL + "/api/project/status")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	var status workflow.InitStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if status.Initialized {
		t.Error("initialized should be false when only project.json exists")
	}
	if !status.HasProjectJSON {
		t.Error("has_project_json should be true")
	}
	if status.HasChecklist {
		t.Error("has_checklist should be false")
	}
}

// TestHandleStatus_FullyInitialized verifies status when all three config files exist.
func TestHandleStatus_FullyInitialized(t *testing.T) {
	c, repoRoot := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	semspecDir := filepath.Join(repoRoot, ".semspec")
	if err := os.MkdirAll(semspecDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, name := range []string{"project.json", "checklist.json", "standards.json"} {
		if err := os.WriteFile(filepath.Join(semspecDir, name), []byte(`{}`), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	resp, err := http.Get(srv.URL + "/api/project/status")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	var status workflow.InitStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !status.Initialized {
		t.Error("initialized should be true when all three config files exist")
	}
	if !status.HasProjectJSON {
		t.Error("has_project_json should be true")
	}
	if !status.HasChecklist {
		t.Error("has_checklist should be true")
	}
	if !status.HasStandards {
		t.Error("has_standards should be true")
	}
}

// TestHandleStatus_SOPCount verifies SOP file counting.
func TestHandleStatus_SOPCount(t *testing.T) {
	c, repoRoot := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	sopDir := filepath.Join(repoRoot, ".semspec", "sources", "docs")
	if err := os.MkdirAll(sopDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, name := range []string{"sop1.md", "sop2.md", "not-an-md.txt"} {
		if err := os.WriteFile(filepath.Join(sopDir, name), []byte("content"), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	resp, err := http.Get(srv.URL + "/api/project/status")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	var status workflow.InitStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if status.SOPCount != 2 {
		t.Errorf("sop_count should be 2 (only .md files), got %d", status.SOPCount)
	}
}

// TestHandleStatus_WorkspacePath verifies the workspace_path field is set.
func TestHandleStatus_WorkspacePath(t *testing.T) {
	c, repoRoot := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/project/status")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	var status workflow.InitStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if status.WorkspacePath != repoRoot {
		t.Errorf("workspace_path should be %q, got %q", repoRoot, status.WorkspacePath)
	}
}

// TestHandleStatus_MethodNotAllowed verifies POST is rejected on the status endpoint.
func TestHandleStatus_MethodNotAllowed(t *testing.T) {
	c, _ := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/project/status", "application/json", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

// TestHandleDetect_GoProject verifies detect returns Go language for a Go project.
func TestHandleDetect_GoProject(t *testing.T) {
	c, repoRoot := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	// Create go.mod to trigger Go detection
	if err := os.WriteFile(filepath.Join(repoRoot, "go.mod"), []byte("module example.com/test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	resp, err := http.Post(srv.URL+"/api/project/detect", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /detect: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result workflow.DetectionResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(result.Languages) == 0 {
		t.Fatal("expected at least one language detected")
	}

	found := false
	for _, lang := range result.Languages {
		if lang.Name == "Go" {
			found = true
			if lang.Marker != "go.mod" {
				t.Errorf("expected marker go.mod, got %q", lang.Marker)
			}
		}
	}
	if !found {
		t.Error("expected Go language to be detected")
	}
}

// TestHandleDetect_EmptyProject verifies detect returns empty slices for an empty project.
func TestHandleDetect_EmptyProject(t *testing.T) {
	c, _ := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/project/detect", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /detect: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result workflow.DetectionResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if result.Languages == nil {
		t.Error("languages should be empty slice, not nil")
	}
	if result.Frameworks == nil {
		t.Error("frameworks should be empty slice, not nil")
	}
	if result.Tooling == nil {
		t.Error("tooling should be empty slice, not nil")
	}
	if result.ExistingDocs == nil {
		t.Error("existing_docs should be empty slice, not nil")
	}
}

// TestHandleDetect_MethodNotAllowed verifies GET is rejected on the detect endpoint.
func TestHandleDetect_MethodNotAllowed(t *testing.T) {
	c, _ := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/project/detect")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

// TestHandleGenerateStandards_Stub verifies the stub endpoint returns empty rules.
func TestHandleGenerateStandards_Stub(t *testing.T) {
	c, _ := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	body := `{"detection":{},"existing_docs_content":{}}`
	resp, err := http.Post(srv.URL+"/api/project/generate-standards", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /generate-standards: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result GenerateStandardsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Stub should return empty rules and zero token estimate
	if result.TokenEstimate != 0 {
		t.Errorf("token_estimate should be 0 for stub, got %d", result.TokenEstimate)
	}
	if result.Rules == nil {
		t.Error("rules should be empty slice, not nil")
	}
	if len(result.Rules) != 0 {
		t.Errorf("rules should be empty for stub, got %d rules", len(result.Rules))
	}
}

// TestHandleGenerateStandards_MethodNotAllowed verifies GET is rejected.
func TestHandleGenerateStandards_MethodNotAllowed(t *testing.T) {
	c, _ := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/project/generate-standards")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

// TestHandleInit_WritesAllFiles verifies init writes project.json, checklist.json, and standards.json.
func TestHandleInit_WritesAllFiles(t *testing.T) {
	c, repoRoot := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	req := InitRequest{
		Project: ProjectInitInput{
			Name:        "test-project",
			Description: "A test project",
			Languages:   []string{"Go"},
			Frameworks:  []string{},
			Repository:  "github.com/example/test",
		},
		Checklist: []workflow.Check{
			{
				Name:        "go-build",
				Command:     "go build ./...",
				Trigger:     []string{"*.go"},
				Category:    workflow.CheckCategoryCompile,
				Required:    true,
				Timeout:     "120s",
				Description: "Compile all Go packages",
			},
		},
		Standards: StandardsInput{
			Version: "1.0.0",
			Rules: []workflow.Rule{
				{
					ID:       "test-coverage",
					Text:     "All new code must include tests.",
					Severity: workflow.RuleSeverityError,
					Category: "testing",
					Origin:   workflow.RuleOriginInit,
				},
			},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	resp, err := http.Post(srv.URL+"/api/project/init", "application/json", strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("POST /init: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var initResp InitResponse
	if err := json.NewDecoder(resp.Body).Decode(&initResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if !initResp.Success {
		t.Error("success should be true")
	}
	if len(initResp.FilesWritten) != 3 {
		t.Errorf("expected 3 files written, got %d: %v", len(initResp.FilesWritten), initResp.FilesWritten)
	}

	// Verify project.json
	var projectConfig workflow.ProjectConfig
	readJSONFile(t, filepath.Join(repoRoot, ".semspec", "project.json"), &projectConfig)
	if projectConfig.Name != "test-project" {
		t.Errorf("project.json name should be test-project, got %q", projectConfig.Name)
	}
	if projectConfig.InitializedAt.IsZero() {
		t.Error("initialized_at should be set")
	}
	if projectConfig.Version == "" {
		t.Error("version should be set")
	}

	// Verify checklist.json
	var checklist workflow.Checklist
	readJSONFile(t, filepath.Join(repoRoot, ".semspec", "checklist.json"), &checklist)
	if len(checklist.Checks) != 1 {
		t.Errorf("expected 1 check, got %d", len(checklist.Checks))
	}
	if checklist.Checks[0].Name != "go-build" {
		t.Errorf("first check should be go-build, got %q", checklist.Checks[0].Name)
	}
	if checklist.CreatedAt.IsZero() {
		t.Error("checklist created_at should be set")
	}

	// Verify standards.json
	var standards workflow.Standards
	readJSONFile(t, filepath.Join(repoRoot, ".semspec", "standards.json"), &standards)
	if len(standards.Rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(standards.Rules))
	}
	if standards.Rules[0].ID != "test-coverage" {
		t.Errorf("first rule should be test-coverage, got %q", standards.Rules[0].ID)
	}
}

// TestHandleInit_CreatesSOPDirectory verifies the sources/docs directory is created.
func TestHandleInit_CreatesSOPDirectory(t *testing.T) {
	c, repoRoot := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	req := InitRequest{
		Project: ProjectInitInput{
			Name:        "test",
			Description: "",
			Languages:   []string{},
			Frameworks:  []string{},
		},
		Checklist: []workflow.Check{},
		Standards: StandardsInput{
			Version: "1.0.0",
			Rules:   []workflow.Rule{},
		},
	}
	data, _ := json.Marshal(req)

	resp, err := http.Post(srv.URL+"/api/project/init", "application/json", strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	sopDir := filepath.Join(repoRoot, ".semspec", "sources", "docs")
	info, err := os.Stat(sopDir)
	if err != nil {
		t.Fatalf("sources/docs directory was not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("sources/docs should be a directory")
	}
}

// TestHandleInit_EmptyBody returns bad request on missing body.
func TestHandleInit_EmptyBody(t *testing.T) {
	c, _ := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/project/init", "application/json", strings.NewReader(""))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for empty body, got %d", resp.StatusCode)
	}
}

// TestHandleInit_MethodNotAllowed verifies GET is rejected.
func TestHandleInit_MethodNotAllowed(t *testing.T) {
	c, _ := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/project/init")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

// TestHandleInit_FilesWrittenPaths verifies the file paths in the response are relative.
func TestHandleInit_FilesWrittenPaths(t *testing.T) {
	c, _ := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	req := InitRequest{
		Project:   ProjectInitInput{Name: "my-project", Languages: []string{}, Frameworks: []string{}},
		Checklist: []workflow.Check{},
		Standards: StandardsInput{Version: "1.0.0", Rules: []workflow.Rule{}},
	}
	data, _ := json.Marshal(req)

	resp, err := http.Post(srv.URL+"/api/project/init", "application/json", strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	var initResp InitResponse
	if err := json.NewDecoder(resp.Body).Decode(&initResp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	expected := map[string]bool{
		".semspec/project.json":   true,
		".semspec/checklist.json": true,
		".semspec/standards.json": true,
	}
	for _, f := range initResp.FilesWritten {
		if !expected[f] {
			t.Errorf("unexpected file in files_written: %q", f)
		}
		delete(expected, f)
	}
	for missing := range expected {
		t.Errorf("missing file in files_written: %q", missing)
	}
}

// TestHandleInit_ProjectLanguages verifies language info is stored correctly.
func TestHandleInit_ProjectLanguages(t *testing.T) {
	c, repoRoot := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	req := InitRequest{
		Project: ProjectInitInput{
			Name:       "multi-lang",
			Languages:  []string{"Go", "TypeScript"},
			Frameworks: []string{"SvelteKit"},
		},
		Checklist: []workflow.Check{},
		Standards: StandardsInput{Version: "1.0.0", Rules: []workflow.Rule{}},
	}
	data, _ := json.Marshal(req)

	resp, err := http.Post(srv.URL+"/api/project/init", "application/json", strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	projectPath := filepath.Join(repoRoot, ".semspec", "project.json")
	projectData, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatalf("read project.json: %v", err)
	}

	var projectConfig workflow.ProjectConfig
	if err := json.Unmarshal(projectData, &projectConfig); err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(projectConfig.Languages) != 2 {
		t.Errorf("expected 2 languages, got %d", len(projectConfig.Languages))
	}
	if projectConfig.Languages[0].Name != "Go" {
		t.Errorf("first language should be Go, got %q", projectConfig.Languages[0].Name)
	}
	if !projectConfig.Languages[0].Primary {
		t.Error("first language should be primary")
	}
	if projectConfig.Languages[1].Primary {
		t.Error("second language should not be primary")
	}

	if len(projectConfig.Frameworks) != 1 {
		t.Errorf("expected 1 framework, got %d", len(projectConfig.Frameworks))
	}
	if projectConfig.Frameworks[0].Name != "SvelteKit" {
		t.Errorf("expected SvelteKit, got %q", projectConfig.Frameworks[0].Name)
	}
}

// TestHandleInit_StandardsGeneratedAt verifies generated_at is set in standards.json.
func TestHandleInit_StandardsGeneratedAt(t *testing.T) {
	c, repoRoot := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	before := time.Now().Add(-time.Second)

	req := InitRequest{
		Project:   ProjectInitInput{Name: "ts", Languages: []string{}, Frameworks: []string{}},
		Checklist: []workflow.Check{},
		Standards: StandardsInput{Version: "1.0.0", Rules: []workflow.Rule{}},
	}
	data, _ := json.Marshal(req)

	resp, err := http.Post(srv.URL+"/api/project/init", "application/json", strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	after := time.Now().Add(time.Second)

	standardsData, err := os.ReadFile(filepath.Join(repoRoot, ".semspec", "standards.json"))
	if err != nil {
		t.Fatalf("read standards.json: %v", err)
	}

	var standards workflow.Standards
	if err := json.Unmarshal(standardsData, &standards); err != nil {
		t.Fatalf("parse: %v", err)
	}

	if standards.GeneratedAt.Before(before) || standards.GeneratedAt.After(after) {
		t.Errorf("generated_at %v is not between %v and %v", standards.GeneratedAt, before, after)
	}
}

// TestHandleInit_ContentTypeJSON verifies response has JSON content-type.
func TestHandleInit_ContentTypeJSON(t *testing.T) {
	c, _ := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	req := InitRequest{
		Project:   ProjectInitInput{Name: "ct", Languages: []string{}, Frameworks: []string{}},
		Checklist: []workflow.Check{},
		Standards: StandardsInput{Version: "1.0.0", Rules: []workflow.Rule{}},
	}
	data, _ := json.Marshal(req)

	resp, err := http.Post(srv.URL+"/api/project/init", "application/json", strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected application/json content-type, got %q", ct)
	}
}

// ============================================================================
// Wizard endpoint tests
// ============================================================================

// TestHandleWizard_ReturnsLanguages verifies the wizard returns supported languages.
func TestHandleWizard_ReturnsLanguages(t *testing.T) {
	c, _ := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/project/wizard")
	if err != nil {
		t.Fatalf("GET /wizard: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var wizard WizardResponse
	if err := json.NewDecoder(resp.Body).Decode(&wizard); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(wizard.Languages) == 0 {
		t.Fatal("expected at least one language")
	}

	// Verify known languages are present.
	langNames := make(map[string]bool)
	for _, l := range wizard.Languages {
		langNames[l.Name] = true
		if !l.HasAST {
			t.Errorf("language %q should have has_ast=true", l.Name)
		}
	}
	for _, expected := range []string{"Go", "Python", "TypeScript", "JavaScript"} {
		if !langNames[expected] {
			t.Errorf("expected language %q in wizard response", expected)
		}
	}

	if len(wizard.Frameworks) == 0 {
		t.Fatal("expected at least one framework")
	}
}

// TestHandleWizard_MethodNotAllowed verifies POST is rejected.
func TestHandleWizard_MethodNotAllowed(t *testing.T) {
	c, _ := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/project/wizard", "application/json", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

// ============================================================================
// Scaffold endpoint tests
// ============================================================================

// TestHandleScaffold_CreatesMarkerFiles verifies scaffold creates expected files.
func TestHandleScaffold_CreatesMarkerFiles(t *testing.T) {
	c, repoRoot := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	req := ScaffoldRequest{
		Languages:  []string{"Python", "JavaScript"},
		Frameworks: []string{"Flask"},
	}
	data, _ := json.Marshal(req)

	resp, err := http.Post(srv.URL+"/api/project/scaffold", "application/json", strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("POST /scaffold: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var scaffoldResp ScaffoldResponse
	if err := json.NewDecoder(resp.Body).Decode(&scaffoldResp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if scaffoldResp.SemspecDir != ".semspec" {
		t.Errorf("semspec_dir should be .semspec, got %q", scaffoldResp.SemspecDir)
	}

	// Verify marker files exist on disk.
	expectedFiles := []string{"requirements.txt", "package.json", "app.py"}
	for _, f := range expectedFiles {
		if _, err := os.Stat(filepath.Join(repoRoot, f)); os.IsNotExist(err) {
			t.Errorf("expected marker file %q to exist", f)
		}
	}

	// Verify .semspec directory was created.
	if _, err := os.Stat(filepath.Join(repoRoot, ".semspec")); os.IsNotExist(err) {
		t.Error("expected .semspec directory to exist")
	}

	// Verify scaffold state was persisted.
	var state workflow.ScaffoldState
	readJSONFile(t, filepath.Join(repoRoot, ".semspec", "scaffold.json"), &state)
	if state.ScaffoldedAt.IsZero() {
		t.Error("scaffolded_at should be set")
	}
	if len(state.Languages) != 2 {
		t.Errorf("expected 2 languages in state, got %d", len(state.Languages))
	}
	if len(state.FilesCreated) == 0 {
		t.Error("files_created should not be empty")
	}
}

// TestHandleScaffold_EmptyLanguages returns error.
func TestHandleScaffold_EmptyLanguages(t *testing.T) {
	c, _ := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	req := ScaffoldRequest{Languages: []string{}, Frameworks: []string{}}
	data, _ := json.Marshal(req)

	resp, err := http.Post(srv.URL+"/api/project/scaffold", "application/json", strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for empty languages, got %d", resp.StatusCode)
	}
}

// TestHandleScaffold_DeduplicatesFiles verifies no duplicate files when languages share markers.
func TestHandleScaffold_DeduplicatesFiles(t *testing.T) {
	c, repoRoot := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	// TypeScript and JavaScript both create package.json — should be deduplicated.
	req := ScaffoldRequest{Languages: []string{"TypeScript", "JavaScript"}, Frameworks: []string{}}
	data, _ := json.Marshal(req)

	resp, err := http.Post(srv.URL+"/api/project/scaffold", "application/json", strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	var scaffoldResp ScaffoldResponse
	if err := json.NewDecoder(resp.Body).Decode(&scaffoldResp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Count how many times package.json appears.
	pkgCount := 0
	for _, f := range scaffoldResp.FilesCreated {
		if f == "package.json" {
			pkgCount++
		}
	}
	if pkgCount != 1 {
		t.Errorf("package.json should appear exactly once, got %d (files: %v)", pkgCount, scaffoldResp.FilesCreated)
	}

	// Verify both TypeScript-specific and shared files exist.
	if _, err := os.Stat(filepath.Join(repoRoot, "tsconfig.json")); os.IsNotExist(err) {
		t.Error("tsconfig.json should exist for TypeScript")
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "package.json")); os.IsNotExist(err) {
		t.Error("package.json should exist")
	}
}

// ============================================================================
// Approve endpoint tests
// ============================================================================

// initProjectForApproval creates all three config files so approve has something to work with.
func initProjectForApproval(t *testing.T, repoRoot string) {
	t.Helper()
	semspecDir := filepath.Join(repoRoot, ".semspec")
	if err := os.MkdirAll(semspecDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	pc := workflow.ProjectConfig{Name: "test", Version: "1.0.0", InitializedAt: time.Now()}
	writeJSONFileForTest(t, filepath.Join(semspecDir, "project.json"), pc)

	cl := workflow.Checklist{Version: "1.0.0", CreatedAt: time.Now(), Checks: []workflow.Check{}}
	writeJSONFileForTest(t, filepath.Join(semspecDir, "checklist.json"), cl)

	st := workflow.Standards{Version: "1.0.0", GeneratedAt: time.Now(), Rules: []workflow.Rule{}}
	writeJSONFileForTest(t, filepath.Join(semspecDir, "standards.json"), st)
}

// writeJSONFileForTest is a test helper that writes JSON to a file.
func writeJSONFileForTest(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write %s: %v", filepath.Base(path), err)
	}
}

// TestHandleApprove_SetsApprovedAt verifies approve sets the timestamp on a config file.
func TestHandleApprove_SetsApprovedAt(t *testing.T) {
	c, repoRoot := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	initProjectForApproval(t, repoRoot)

	before := time.Now().Add(-time.Second)

	req := ApproveRequest{File: "project.json"}
	data, _ := json.Marshal(req)

	resp, err := http.Post(srv.URL+"/api/project/approve", "application/json", strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("POST /approve: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var approveResp ApproveResponse
	if err := json.NewDecoder(resp.Body).Decode(&approveResp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if approveResp.File != "project.json" {
		t.Errorf("expected file=project.json, got %q", approveResp.File)
	}
	if approveResp.ApprovedAt.Before(before) {
		t.Errorf("approved_at %v should be after %v", approveResp.ApprovedAt, before)
	}
	// Only one file approved, so all_approved should be false.
	if approveResp.AllApproved {
		t.Error("all_approved should be false after approving only one file")
	}

	// Verify the file on disk has the timestamp.
	var pc workflow.ProjectConfig
	readJSONFile(t, filepath.Join(repoRoot, ".semspec", "project.json"), &pc)
	if pc.ApprovedAt == nil {
		t.Fatal("approved_at should be set on disk")
	}
}

// TestHandleApprove_AllApproved verifies all_approved is true after all three files are approved.
func TestHandleApprove_AllApproved(t *testing.T) {
	c, repoRoot := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	initProjectForApproval(t, repoRoot)

	// Approve all three files.
	for _, file := range []string{"project.json", "checklist.json", "standards.json"} {
		req := ApproveRequest{File: file}
		data, _ := json.Marshal(req)

		resp, err := http.Post(srv.URL+"/api/project/approve", "application/json", strings.NewReader(string(data)))
		if err != nil {
			t.Fatalf("POST /approve (%s): %v", file, err)
		}

		var approveResp ApproveResponse
		if err := json.NewDecoder(resp.Body).Decode(&approveResp); err != nil {
			t.Fatalf("decode (%s): %v", file, err)
		}
		resp.Body.Close()

		if file == "standards.json" && !approveResp.AllApproved {
			t.Error("all_approved should be true after approving all three files")
		}
	}
}

// TestHandleApprove_NonexistentFile returns 404 when the config file doesn't exist.
func TestHandleApprove_NonexistentFile(t *testing.T) {
	c, _ := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	req := ApproveRequest{File: "project.json"}
	data, _ := json.Marshal(req)

	resp, err := http.Post(srv.URL+"/api/project/approve", "application/json", strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for nonexistent file, got %d", resp.StatusCode)
	}
}

// TestHandleApprove_InvalidFile returns 400 for unrecognized file names.
func TestHandleApprove_InvalidFile(t *testing.T) {
	c, _ := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	req := ApproveRequest{File: "random.json"}
	data, _ := json.Marshal(req)

	resp, err := http.Post(srv.URL+"/api/project/approve", "application/json", strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid file, got %d", resp.StatusCode)
	}
}

// TestHandleStatus_ReflectsApproval verifies the status endpoint shows approval timestamps.
func TestHandleStatus_ReflectsApproval(t *testing.T) {
	c, repoRoot := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	initProjectForApproval(t, repoRoot)

	// Approve project.json only.
	req := ApproveRequest{File: "project.json"}
	data, _ := json.Marshal(req)
	resp, err := http.Post(srv.URL+"/api/project/approve", "application/json", strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("POST /approve: %v", err)
	}
	resp.Body.Close()

	// Check status reflects the approval.
	resp, err = http.Get(srv.URL + "/api/project/status")
	if err != nil {
		t.Fatalf("GET /status: %v", err)
	}
	defer resp.Body.Close()

	var status workflow.InitStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if status.ProjectApprovedAt == nil {
		t.Error("project_approved_at should be set after approval")
	}
	if status.ChecklistApprovedAt != nil {
		t.Error("checklist_approved_at should be nil (not yet approved)")
	}
	if status.AllApproved {
		t.Error("all_approved should be false (only one file approved)")
	}
}

// TestHandleStatus_ReflectsScaffold verifies the status endpoint shows scaffold state.
func TestHandleStatus_ReflectsScaffold(t *testing.T) {
	c, repoRoot := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	// Scaffold a project.
	req := ScaffoldRequest{Languages: []string{"Go"}, Frameworks: []string{}}
	data, _ := json.Marshal(req)
	resp, err := http.Post(srv.URL+"/api/project/scaffold", "application/json", strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("POST /scaffold: %v", err)
	}
	resp.Body.Close()

	// Verify go.mod was created.
	if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); os.IsNotExist(err) {
		t.Fatal("go.mod should exist after scaffold")
	}

	// Check status reflects the scaffold.
	resp, err = http.Get(srv.URL + "/api/project/status")
	if err != nil {
		t.Fatalf("GET /status: %v", err)
	}
	defer resp.Body.Close()

	var status workflow.InitStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !status.Scaffolded {
		t.Error("scaffolded should be true after scaffold")
	}
	if status.ScaffoldedAt == nil {
		t.Error("scaffolded_at should be set")
	}
	if len(status.ScaffoldedLanguages) != 1 || status.ScaffoldedLanguages[0] != "Go" {
		t.Errorf("scaffolded_languages should be [Go], got %v", status.ScaffoldedLanguages)
	}
	if len(status.ScaffoldedFiles) == 0 {
		t.Error("scaffolded_files should not be empty")
	}
}
