package sourceingester

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
)

func TestMergeChecks_AddsNew(t *testing.T) {
	existing := []workflow.Check{
		{Name: "pytest", Command: "pytest", Category: "test"},
	}
	proposed := []workflow.Check{
		{Name: "pytest", Command: "pytest --different", Category: "test"},
		{Name: "npm-test", Command: "npm test", Category: "test"},
	}

	merged, changed := mergeChecks(existing, proposed)

	if !changed {
		t.Fatal("Expected changed=true when new checks are added")
	}
	if len(merged) != 2 {
		t.Fatalf("Expected 2 checks, got %d", len(merged))
	}
	// Existing check preserved as-is (not overwritten by proposed)
	if merged[0].Command != "pytest" {
		t.Errorf("Expected existing command preserved, got %q", merged[0].Command)
	}
	// New check appended
	if merged[1].Name != "npm-test" {
		t.Errorf("Expected npm-test appended, got %q", merged[1].Name)
	}
}

func TestMergeChecks_PreservesExisting(t *testing.T) {
	existing := []workflow.Check{
		{Name: "pytest", Command: "pytest -v --tb=short", Timeout: "600s", Category: "test"},
	}
	proposed := []workflow.Check{
		{Name: "pytest", Command: "pytest", Timeout: "300s", Category: "test"},
	}

	merged, changed := mergeChecks(existing, proposed)

	if changed {
		t.Fatal("Expected changed=false when no new checks")
	}
	if len(merged) != 1 {
		t.Fatalf("Expected 1 check, got %d", len(merged))
	}
	// User's customized command must be preserved
	if merged[0].Command != "pytest -v --tb=short" {
		t.Errorf("Expected user command preserved, got %q", merged[0].Command)
	}
	if merged[0].Timeout != "600s" {
		t.Errorf("Expected user timeout preserved, got %q", merged[0].Timeout)
	}
}

func TestMergeChecks_NoChangeNoop(t *testing.T) {
	existing := []workflow.Check{
		{Name: "go-build", Command: "go build ./..."},
		{Name: "go-test", Command: "go test ./..."},
	}
	proposed := []workflow.Check{
		{Name: "go-build", Command: "go build ./..."},
		{Name: "go-test", Command: "go test ./..."},
	}

	merged, changed := mergeChecks(existing, proposed)

	if changed {
		t.Fatal("Expected changed=false when all proposed checks already exist")
	}
	if len(merged) != 2 {
		t.Fatalf("Expected 2 checks, got %d", len(merged))
	}
}

func TestMergeChecks_PreservesOrder(t *testing.T) {
	existing := []workflow.Check{
		{Name: "go-build", Command: "go build ./..."},
		{Name: "go-vet", Command: "go vet ./..."},
		{Name: "go-test", Command: "go test ./..."},
	}
	proposed := []workflow.Check{
		{Name: "go-build", Command: "go build ./..."},
		{Name: "eslint", Command: "npx eslint ."},
		{Name: "npm-test", Command: "npm test"},
	}

	merged, changed := mergeChecks(existing, proposed)

	if !changed {
		t.Fatal("Expected changed=true")
	}
	if len(merged) != 5 {
		t.Fatalf("Expected 5 checks, got %d", len(merged))
	}

	// Existing order preserved
	expectedOrder := []string{"go-build", "go-vet", "go-test", "eslint", "npm-test"}
	for i, name := range expectedOrder {
		if merged[i].Name != name {
			t.Errorf("Position %d: expected %q, got %q", i, name, merged[i].Name)
		}
	}
}

func TestMergeChecks_NormalisesDefaults(t *testing.T) {
	existing := []workflow.Check{
		{Name: "pytest", Command: "pytest"},
	}
	proposed := []workflow.Check{
		{Name: "npm-test", Command: "npm test"},
	}

	merged, changed := mergeChecks(existing, proposed)

	if !changed {
		t.Fatal("Expected changed=true")
	}

	newCheck := merged[1]
	if newCheck.WorkingDir != "." {
		t.Errorf("Expected WorkingDir defaulted to '.', got %q", newCheck.WorkingDir)
	}
	if newCheck.Timeout != "120s" {
		t.Errorf("Expected Timeout defaulted to '120s', got %q", newCheck.Timeout)
	}
	if newCheck.Trigger == nil {
		t.Error("Expected Trigger defaulted to empty slice, got nil")
	}
}

func TestChecklistUpdater_UpdateFromDetection_AddsJSChecks(t *testing.T) {
	dir := t.TempDir()
	semspecDir := filepath.Join(dir, ".semspec")
	if err := os.MkdirAll(semspecDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create initial checklist with just pytest (Python-only project)
	checklist := workflow.Checklist{
		Version:   "1.0.0",
		CreatedAt: time.Now(),
		Checks: []workflow.Check{
			{
				Name:     "pytest",
				Command:  "pytest",
				Trigger:  []string{"*.py"},
				Category: "test",
				Required: true,
				Timeout:  "300s",
			},
		},
	}
	writeTestJSON(t, filepath.Join(semspecDir, "checklist.json"), checklist)

	// Create initial project.json with just Python
	project := workflow.ProjectConfig{
		Name:    "Test Project",
		Version: "1.0.0",
		Languages: []workflow.LanguageInfo{
			{Name: "Python", Primary: true},
		},
	}
	writeTestJSON(t, filepath.Join(semspecDir, "project.json"), project)

	// Create a Python file (so detection finds Python)
	if err := os.MkdirAll(filepath.Join(dir, "api"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Now add a package.json — this should trigger JS detection
	packageJSON := `{"name": "test", "version": "1.0.0", "scripts": {"test": "jest"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(packageJSON), 0644); err != nil {
		t.Fatal(err)
	}

	// Run update
	updater := NewChecklistUpdater(semspecDir, dir)
	result, err := updater.UpdateFromDetection()
	if err != nil {
		t.Fatalf("UpdateFromDetection failed: %v", err)
	}

	// Should have added JS checks
	if result.ChecksAdded == 0 {
		t.Fatal("Expected at least one new check added after package.json appeared")
	}

	// Verify npm-test was added
	found := false
	for _, name := range result.NewCheckNames {
		if name == "npm-test" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected npm-test in new checks, got %v", result.NewCheckNames)
	}

	// Verify JavaScript was added to project.json
	if len(result.LanguagesAdded) == 0 {
		t.Fatal("Expected JavaScript added to project.json")
	}
	foundJS := false
	for _, lang := range result.LanguagesAdded {
		if lang == "JavaScript" {
			foundJS = true
			break
		}
	}
	if !foundJS {
		t.Errorf("Expected JavaScript in languages added, got %v", result.LanguagesAdded)
	}

	// Read back checklist and verify merge
	data, _ := os.ReadFile(filepath.Join(semspecDir, "checklist.json"))
	var updated workflow.Checklist
	json.Unmarshal(data, &updated)

	// pytest should still be first (existing order preserved)
	if updated.Checks[0].Name != "pytest" {
		t.Errorf("Expected pytest first, got %s", updated.Checks[0].Name)
	}
	if len(updated.Checks) <= 1 {
		t.Fatalf("Expected more than 1 check after JS detection, got %d", len(updated.Checks))
	}
}

func TestChecklistUpdater_UpdateFromDetection_SkipsWhenNoChecklist(t *testing.T) {
	dir := t.TempDir()
	semspecDir := filepath.Join(dir, ".semspec")
	// Deliberately do NOT create checklist.json

	updater := NewChecklistUpdater(semspecDir, dir)
	result, err := updater.UpdateFromDetection()
	if err != nil {
		t.Fatalf("Expected no error for uninitialized project, got: %v", err)
	}

	if result.ChecksAdded != 0 {
		t.Errorf("Expected 0 checks added for uninitialized project, got %d", result.ChecksAdded)
	}
}

func TestChecklistUpdater_UpdateProjectConfig_AddsLanguage(t *testing.T) {
	dir := t.TempDir()
	semspecDir := filepath.Join(dir, ".semspec")
	if err := os.MkdirAll(semspecDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create project.json with Python only
	project := workflow.ProjectConfig{
		Name:    "Test",
		Version: "1.0.0",
		Languages: []workflow.LanguageInfo{
			{Name: "Python", Primary: true},
		},
	}
	writeTestJSON(t, filepath.Join(semspecDir, "project.json"), project)

	// Create checklist.json (required for UpdateFromDetection to proceed)
	checklist := workflow.Checklist{
		Version: "1.0.0",
		Checks:  []workflow.Check{{Name: "pytest", Command: "pytest"}},
	}
	writeTestJSON(t, filepath.Join(semspecDir, "checklist.json"), checklist)

	// Add Go marker files
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Add a requirements.txt so Python is still detected
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask\n"), 0644); err != nil {
		t.Fatal(err)
	}

	updater := NewChecklistUpdater(semspecDir, dir)
	result, err := updater.UpdateFromDetection()
	if err != nil {
		t.Fatalf("UpdateFromDetection failed: %v", err)
	}

	// Go should have been added
	foundGo := false
	for _, lang := range result.LanguagesAdded {
		if lang == "Go" {
			foundGo = true
			break
		}
	}
	if !foundGo {
		t.Errorf("Expected Go in languages added, got %v", result.LanguagesAdded)
	}

	// Read back project.json and verify
	data, _ := os.ReadFile(filepath.Join(semspecDir, "project.json"))
	var updated workflow.ProjectConfig
	json.Unmarshal(data, &updated)

	// Python should still be primary
	if !updated.Languages[0].Primary {
		t.Error("Expected Python to remain primary")
	}
	if updated.Languages[0].Name != "Python" {
		t.Errorf("Expected Python first, got %s", updated.Languages[0].Name)
	}

	// Go should be added but not primary
	if len(updated.Languages) < 2 {
		t.Fatalf("Expected at least 2 languages, got %d", len(updated.Languages))
	}
	foundGoInConfig := false
	for _, lang := range updated.Languages {
		if lang.Name == "Go" {
			if lang.Primary {
				t.Error("Newly added Go should NOT be primary")
			}
			foundGoInConfig = true
		}
	}
	if !foundGoInConfig {
		t.Error("Go not found in updated project.json languages")
	}
}

func TestChecklistUpdater_UpdateProjectConfig_PreservesExisting(t *testing.T) {
	dir := t.TempDir()
	semspecDir := filepath.Join(dir, ".semspec")
	if err := os.MkdirAll(semspecDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create project with Python (customized version)
	project := workflow.ProjectConfig{
		Name:    "Test",
		Version: "1.0.0",
		Languages: []workflow.LanguageInfo{
			{Name: "Python", Version: strPtr("3.12"), Primary: true},
		},
	}
	writeTestJSON(t, filepath.Join(semspecDir, "project.json"), project)

	checklist := workflow.Checklist{
		Version: "1.0.0",
		Checks:  []workflow.Check{{Name: "pytest", Command: "pytest"}},
	}
	writeTestJSON(t, filepath.Join(semspecDir, "checklist.json"), checklist)

	// Add requirements.txt so Python is detected (but version won't match user's)
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask\n"), 0644); err != nil {
		t.Fatal(err)
	}

	updater := NewChecklistUpdater(semspecDir, dir)
	result, err := updater.UpdateFromDetection()
	if err != nil {
		t.Fatalf("UpdateFromDetection failed: %v", err)
	}

	// No new languages should be added (Python already exists)
	if len(result.LanguagesAdded) != 0 {
		t.Errorf("Expected no languages added, got %v", result.LanguagesAdded)
	}

	// Verify user's version string is preserved
	data, _ := os.ReadFile(filepath.Join(semspecDir, "project.json"))
	var updated workflow.ProjectConfig
	json.Unmarshal(data, &updated)

	if updated.Languages[0].Version == nil || *updated.Languages[0].Version != "3.12" {
		t.Error("Expected user's Python version '3.12' preserved")
	}
}

func TestChecklistUpdater_Idempotent(t *testing.T) {
	dir := t.TempDir()
	semspecDir := filepath.Join(dir, ".semspec")
	if err := os.MkdirAll(semspecDir, 0755); err != nil {
		t.Fatal(err)
	}

	checklist := workflow.Checklist{
		Version: "1.0.0",
		Checks:  []workflow.Check{{Name: "pytest", Command: "pytest"}},
	}
	writeTestJSON(t, filepath.Join(semspecDir, "checklist.json"), checklist)

	project := workflow.ProjectConfig{
		Name:      "Test",
		Version:   "1.0.0",
		Languages: []workflow.LanguageInfo{{Name: "Python", Primary: true}},
	}
	writeTestJSON(t, filepath.Join(semspecDir, "project.json"), project)

	// Add package.json so JS is detected
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask\n"), 0644); err != nil {
		t.Fatal(err)
	}

	updater := NewChecklistUpdater(semspecDir, dir)

	// First run: should add JS checks
	result1, err := updater.UpdateFromDetection()
	if err != nil {
		t.Fatalf("First run failed: %v", err)
	}
	if result1.ChecksAdded == 0 {
		t.Fatal("Expected checks added on first run")
	}

	// Second run: should be a no-op
	result2, err := updater.UpdateFromDetection()
	if err != nil {
		t.Fatalf("Second run failed: %v", err)
	}
	if result2.ChecksAdded != 0 {
		t.Errorf("Expected 0 checks added on second run, got %d", result2.ChecksAdded)
	}
	if len(result2.LanguagesAdded) != 0 {
		t.Errorf("Expected 0 languages added on second run, got %v", result2.LanguagesAdded)
	}
}

// --- Helpers -----------------------------------------------------------------

func writeTestJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal JSON for %s: %v", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("Failed to create directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("Failed to write %s: %v", path, err)
	}
}

func strPtr(s string) *string {
	return &s
}
