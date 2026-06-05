package projectmanager

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// seedProjectConfig writes a project.json into the component's repo root and
// loads it into the store cache. Returns the cached config for assertions.
func seedProjectConfig(t *testing.T, c *Component, pc *workflow.ProjectConfig) {
	t.Helper()
	semspecDir := filepath.Join(c.repoPath, ".semspec")
	if err := c.ensureInitDirs(semspecDir); err != nil {
		t.Fatalf("ensureInitDirs: %v", err)
	}
	if err := writeJSONFile(filepath.Join(semspecDir, workflow.ProjectConfigFile), pc); err != nil {
		t.Fatalf("write project.json: %v", err)
	}
	c.store.populateFromFiles()
}

// TestHandleConfig_NoopPatchPreservesTimestamp verifies that a PATCH whose
// payload matches the current config does not reseat UpdatedAt or rewrite
// the file. Without this guard, every qa-cycle e2e run dirties the
// committed e2e workspace project.json with a fresh timestamp.
func TestHandleConfig_NoopPatchPreservesTimestamp(t *testing.T) {
	c, repoRoot := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	originalTS := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	seedProjectConfig(t, c, &workflow.ProjectConfig{
		Name:          "test",
		Version:       "1.0.0",
		InitializedAt: originalTS,
		UpdatedAt:     originalTS,
		QALevel:       workflow.QALevelUnit,
		QATestCommand: "./gradlew test",
	})

	// PATCH with the SAME qa_level and qa_test_command — should be a no-op.
	qaLevel := string(workflow.QALevelUnit)
	qaCmd := "./gradlew test"
	body, err := json.Marshal(map[string]*string{
		"qa_level":        &qaLevel,
		"qa_test_command": &qaCmd,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req, err := http.NewRequest(http.MethodPatch, srv.URL+"/api/project/config", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var afterFile workflow.ProjectConfig
	readJSONFile(t, filepath.Join(repoRoot, ".semspec", workflow.ProjectConfigFile), &afterFile)
	if !afterFile.UpdatedAt.Equal(originalTS) {
		t.Errorf("UpdatedAt = %v, want unchanged %v (no-op PATCH reseated timestamp)",
			afterFile.UpdatedAt, originalTS)
	}
}

// TestHandleConfig_NoopOrgPatchWithExistingPlansSucceeds verifies the
// prefixChanging guard does not falsely trigger when req.Org matches the
// current value, even when plans already exist. Both guards (prefixChanging
// at the top of handleConfig and the per-field change detector below it)
// use the same "*req.X != pc.X" comparison; this test pins that they stay
// in sync. Without the per-field guard, this PATCH would 200 + reseat
// UpdatedAt; without the prefixChanging guard's same-value tolerance, it
// would 409 even though nothing actually changes.
func TestHandleConfig_NoopOrgPatchWithExistingPlansSucceeds(t *testing.T) {
	c, repoRoot := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	originalTS := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	seedProjectConfig(t, c, &workflow.ProjectConfig{
		Name:          "test",
		Org:           "acme",
		Platform:      "prod",
		Version:       "1.0.0",
		InitializedAt: originalTS,
		UpdatedAt:     originalTS,
	})

	// Simulate an existing plan: prefixChanging guard reads
	// .semspec/projects/default/plans/ to decide whether org/platform
	// changes are allowed. A non-empty dir triggers 409 on real changes.
	plansDir := filepath.Join(repoRoot, ".semspec", "projects", "default", "plans")
	if err := os.MkdirAll(plansDir, 0o755); err != nil {
		t.Fatalf("mkdir plans: %v", err)
	}
	if err := os.WriteFile(filepath.Join(plansDir, "plan-1.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatalf("seed plan file: %v", err)
	}

	// PATCH with the SAME org — must NOT trigger the prefixChanging 409.
	org := "acme"
	body, err := json.Marshal(map[string]*string{"org": &org})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPatch, srv.URL+"/api/project/config", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (no-op PATCH on existing-plan project should succeed)", resp.StatusCode)
	}

	// Timestamp must also be preserved — no-op detection runs after the
	// prefixChanging guard, both keyed off the same comparison shape.
	var afterFile workflow.ProjectConfig
	readJSONFile(t, filepath.Join(repoRoot, ".semspec", workflow.ProjectConfigFile), &afterFile)
	if !afterFile.UpdatedAt.Equal(originalTS) {
		t.Errorf("UpdatedAt = %v, want unchanged %v", afterFile.UpdatedAt, originalTS)
	}
}

// TestHandleConfig_RealOrgPatchWithExistingPlansRejects pins the
// prefixChanging guard's intended behavior so a future refactor doesn't
// accidentally relax it. Pair with the no-op test above.
func TestHandleConfig_RealOrgPatchWithExistingPlansRejects(t *testing.T) {
	c, repoRoot := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	seedProjectConfig(t, c, &workflow.ProjectConfig{
		Name:     "test",
		Org:      "acme",
		Platform: "prod",
		Version:  "1.0.0",
	})

	plansDir := filepath.Join(repoRoot, ".semspec", "projects", "default", "plans")
	if err := os.MkdirAll(plansDir, 0o755); err != nil {
		t.Fatalf("mkdir plans: %v", err)
	}
	if err := os.WriteFile(filepath.Join(plansDir, "plan-1.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatalf("seed plan file: %v", err)
	}

	newOrg := "newcorp"
	body, err := json.Marshal(map[string]*string{"org": &newOrg})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPatch, srv.URL+"/api/project/config", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (real org change with existing plans must reject)", resp.StatusCode)
	}
}

// TestHandleConfig_RealChangePatchUpdatesTimestamp verifies the guard
// doesn't suppress timestamp updates for actual mutations.
func TestHandleConfig_RealChangePatchUpdatesTimestamp(t *testing.T) {
	c, repoRoot := setupTestComponent(t)
	srv := registerHandlers(c)
	defer srv.Close()

	originalTS := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	seedProjectConfig(t, c, &workflow.ProjectConfig{
		Name:          "test",
		Version:       "1.0.0",
		InitializedAt: originalTS,
		UpdatedAt:     originalTS,
		QALevel:       workflow.QALevelSynthesis,
	})

	qaLevel := string(workflow.QALevelUnit)
	body, err := json.Marshal(map[string]*string{"qa_level": &qaLevel})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req, err := http.NewRequest(http.MethodPatch, srv.URL+"/api/project/config", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var afterFile workflow.ProjectConfig
	readJSONFile(t, filepath.Join(repoRoot, ".semspec", workflow.ProjectConfigFile), &afterFile)
	if afterFile.QALevel != workflow.QALevelUnit {
		t.Errorf("QALevel = %q, want integration", afterFile.QALevel)
	}
	if !afterFile.UpdatedAt.After(originalTS) {
		t.Errorf("UpdatedAt = %v, want after %v (real-change PATCH should reseat)",
			afterFile.UpdatedAt, originalTS)
	}
}
