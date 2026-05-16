package projectmanager

import (
	"bytes"
	"encoding/json"
	"net/http"
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
		QALevel:       workflow.QALevelIntegration,
		QATestCommand: "./gradlew test",
	})

	// PATCH with the SAME qa_level and qa_test_command — should be a no-op.
	qaLevel := string(workflow.QALevelIntegration)
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

	qaLevel := string(workflow.QALevelIntegration)
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
	if afterFile.QALevel != workflow.QALevelIntegration {
		t.Errorf("QALevel = %q, want integration", afterFile.QALevel)
	}
	if !afterFile.UpdatedAt.After(originalTS) {
		t.Errorf("UpdatedAt = %v, want after %v (real-change PATCH should reseat)",
			afterFile.UpdatedAt, originalTS)
	}
}
