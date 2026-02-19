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
