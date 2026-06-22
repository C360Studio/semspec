//go:build integration

package projectmanager

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/c360studio/semspec/test/integration/graphmock"
	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semstreams/natsclient"
)

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestIntegration_SaveWritesTriples verifies that saveConfig writes both
// triples and file.
func TestIntegration_SaveWritesTriples(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	mock := graphmock.Start(t, tc.Client)

	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, ".semspec"), 0755); err != nil {
		t.Fatal(err)
	}

	tw := &graphutil.TripleWriter{
		NATSClient:    tc.Client,
		Logger:        slog.Default(),
		ComponentName: "project-manager",
	}
	store := newProjectStore(tw, repoRoot, slog.Default())

	now := time.Now()
	pc := &workflow.ProjectConfig{
		Name:      "test-project",
		Version:   "1.0.0",
		Org:       "testorg",
		Platform:  "testplatform",
		UpdatedAt: now,
	}

	if err := store.saveConfig(ctx, pc); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	// Verify cache was updated.
	cached := store.getConfig()
	if cached == nil || cached.Name != "test-project" {
		t.Fatal("cache not updated after saveConfig")
	}

	// Verify file was written.
	var fromFile workflow.ProjectConfig
	data, err := os.ReadFile(filepath.Join(repoRoot, ".semspec", "project.json"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if err := json.Unmarshal(data, &fromFile); err != nil {
		t.Fatalf("unmarshal file: %v", err)
	}
	if fromFile.Name != "test-project" {
		t.Fatalf("file name = %q, want test-project", fromFile.Name)
	}

	// Verify triples were written to mock graph.
	entityID := workflow.ProjectConfigEntityID("project")
	entity, ok := mock.Entity(entityID)

	if !ok {
		t.Fatal("no entity written to graph")
	}

	if v := graphmock.TripleValue(entity.Triples, semspec.ProjectConfigName); v != "test-project" {
		t.Errorf("triple name = %q, want test-project", v)
	}
	if v := graphmock.TripleValue(entity.Triples, semspec.ProjectConfigType); v != "project" {
		t.Errorf("triple type = %q, want project", v)
	}
	if v := graphmock.TripleValue(entity.Triples, semspec.ProjectConfigOrg); v != "testorg" {
		t.Errorf("triple org = %q, want testorg", v)
	}

	// Verify JSON blob triple is round-trippable.
	jsonBlob := graphmock.TripleValue(entity.Triples, semspec.ProjectConfigJSON)
	if jsonBlob == "" {
		t.Fatal("no JSON blob triple written")
	}
	var roundTrip workflow.ProjectConfig
	if err := json.Unmarshal([]byte(jsonBlob), &roundTrip); err != nil {
		t.Fatalf("round-trip unmarshal: %v", err)
	}
	if roundTrip.Name != "test-project" {
		t.Errorf("round-trip name = %q, want test-project", roundTrip.Name)
	}
}

// TestIntegration_ReconcileGraphWins verifies that when the graph has a newer
// version than the file, the graph version populates the cache and file.
func TestIntegration_ReconcileGraphWins(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	mock := graphmock.Start(t, tc.Client)

	repoRoot := t.TempDir()
	semspecDir := filepath.Join(repoRoot, ".semspec")
	if err := os.MkdirAll(semspecDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write an old file version (same org/platform — only name and timestamp differ).
	workflow.InitEntityPrefix("testorg", "testplat", "")
	oldTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	oldPC := workflow.ProjectConfig{
		Name:      "old-name",
		Version:   "1.0.0",
		Org:       "testorg",
		Platform:  "testplat",
		UpdatedAt: oldTime,
	}
	if err := writeJSONFile(filepath.Join(semspecDir, workflow.ProjectConfigFile), oldPC); err != nil {
		t.Fatal(err)
	}

	// Pre-populate mock graph with a newer version (same prefix).
	entityID := workflow.ProjectConfigEntityID("project")

	newTime := time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC)
	newPC := workflow.ProjectConfig{
		Name:      "new-name",
		Version:   "1.0.0",
		Org:       "testorg",
		Platform:  "testplat",
		UpdatedAt: newTime,
	}
	jsonBlob, _ := json.Marshal(newPC)

	tw := &graphutil.TripleWriter{
		NATSClient:    tc.Client,
		Logger:        slog.Default(),
		ComponentName: "test-setup",
	}
	_ = tw.WriteTriple(ctx, entityID, semspec.ProjectConfigType, "project")
	_ = tw.WriteTriple(ctx, entityID, semspec.ProjectConfigUpdatedAt, newTime.Format(time.RFC3339))
	_ = tw.WriteTriple(ctx, entityID, semspec.ProjectConfigJSON, string(jsonBlob))
	_ = tw.WriteTriple(ctx, entityID, semspec.ProjectConfigName, "new-name")

	// Verify mock has the entity.
	_, exists := mock.Entity(entityID)
	if !exists {
		t.Fatal("setup: entity not in mock graph")
	}

	// Create store and reconcile.
	store := newProjectStore(tw, repoRoot, slog.Default())
	store.reconcile(ctx)

	// Cache should have the graph version (newer).
	cached := store.getConfig()
	if cached == nil {
		t.Fatal("cache nil after reconcile")
	}
	if cached.Name != "new-name" {
		t.Errorf("cache name = %q, want new-name", cached.Name)
	}

	// File should be synced to graph version.
	var fromFile workflow.ProjectConfig
	data, _ := os.ReadFile(filepath.Join(semspecDir, workflow.ProjectConfigFile))
	_ = json.Unmarshal(data, &fromFile)
	if fromFile.Name != "new-name" {
		t.Errorf("file name = %q, want new-name (synced from graph)", fromFile.Name)
	}
}

// TestIntegration_ReconcileFileWins verifies that when the file has a newer
// version than the graph, the file version stays in the cache.
func TestIntegration_ReconcileFileWins(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	mock := graphmock.Start(t, tc.Client)

	repoRoot := t.TempDir()
	semspecDir := filepath.Join(repoRoot, ".semspec")
	if err := os.MkdirAll(semspecDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write a new file version.
	newTime := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	newPC := workflow.ProjectConfig{
		Name:      "file-wins",
		Version:   "1.0.0",
		Org:       "fileorg",
		Platform:  "fileplat",
		UpdatedAt: newTime,
	}
	if err := writeJSONFile(filepath.Join(semspecDir, workflow.ProjectConfigFile), newPC); err != nil {
		t.Fatal(err)
	}

	// Pre-populate mock graph with an older version.
	workflow.InitEntityPrefix("fileorg", "fileplat", "")
	entityID := workflow.ProjectConfigEntityID("project")

	oldTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	oldPC := workflow.ProjectConfig{
		Name:      "old-graph-name",
		Version:   "1.0.0",
		UpdatedAt: oldTime,
	}
	jsonBlob, _ := json.Marshal(oldPC)

	tw := &graphutil.TripleWriter{
		NATSClient:    tc.Client,
		Logger:        slog.Default(),
		ComponentName: "test-setup",
	}
	_ = tw.WriteTriple(ctx, entityID, semspec.ProjectConfigType, "project")
	_ = tw.WriteTriple(ctx, entityID, semspec.ProjectConfigUpdatedAt, oldTime.Format(time.RFC3339))
	_ = tw.WriteTriple(ctx, entityID, semspec.ProjectConfigJSON, string(jsonBlob))

	_, exists := mock.Entity(entityID)
	if !exists {
		t.Fatal("setup: entity not in mock graph")
	}

	// Reconcile — file should win.
	store := newProjectStore(tw, repoRoot, slog.Default())
	store.reconcile(ctx)

	cached := store.getConfig()
	if cached == nil {
		t.Fatal("cache nil after reconcile")
	}
	if cached.Name != "file-wins" {
		t.Errorf("cache name = %q, want file-wins", cached.Name)
	}
}

// TestIntegration_ReconcileNoGraph verifies that reconcile falls back to
// files when the graph has no entities.
func TestIntegration_ReconcileNoGraph(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	graphmock.Start(t, tc.Client)

	repoRoot := t.TempDir()
	semspecDir := filepath.Join(repoRoot, ".semspec")
	if err := os.MkdirAll(semspecDir, 0755); err != nil {
		t.Fatal(err)
	}

	pc := workflow.ProjectConfig{Name: "from-file", Version: "1.0.0", UpdatedAt: time.Now()}
	if err := writeJSONFile(filepath.Join(semspecDir, workflow.ProjectConfigFile), pc); err != nil {
		t.Fatal(err)
	}

	tw := &graphutil.TripleWriter{
		NATSClient:    tc.Client,
		Logger:        slog.Default(),
		ComponentName: "project-manager",
	}
	store := newProjectStore(tw, repoRoot, slog.Default())
	store.reconcile(ctx)

	cached := store.getConfig()
	if cached == nil || cached.Name != "from-file" {
		t.Fatalf("expected file fallback, got %+v", cached)
	}
}

// TestIntegration_StandardsSaveAndReconcile verifies the full round-trip
// for standards: save → triples → reconcile from graph.
func TestIntegration_StandardsSaveAndReconcile(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	mock := graphmock.Start(t, tc.Client)

	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, ".semspec"), 0755); err != nil {
		t.Fatal(err)
	}

	tw := &graphutil.TripleWriter{
		NATSClient:    tc.Client,
		Logger:        slog.Default(),
		ComponentName: "project-manager",
	}

	// Save standards with rules.
	store := newProjectStore(tw, repoRoot, slog.Default())
	now := time.Now()
	st := &workflow.Standards{
		Version:       "1.0.0",
		GeneratedAt:   now,
		UpdatedAt:     now,
		TokenEstimate: 120,
		Items: []workflow.Standard{
			{ID: "test-coverage", Text: "Minimum 80% test coverage", Severity: "error", Category: "testing"},
			{ID: "no-panics", Text: "Never use panic in library code", Severity: "error", Category: "code-quality"},
		},
	}
	if err := store.saveStandards(ctx, st); err != nil {
		t.Fatalf("saveStandards: %v", err)
	}

	// Verify triples written.
	entityID := workflow.ProjectConfigEntityID("standards")
	jsonBlob := ""
	if entity, ok := mock.Entity(entityID); ok {
		jsonBlob = graphmock.TripleValue(entity.Triples, semspec.ProjectConfigJSON)
	}
	if jsonBlob == "" {
		t.Fatal("no JSON blob triple for standards")
	}

	// Create a fresh store with old file, reconcile — graph should win.
	oldST := workflow.Standards{Version: "1.0.0", UpdatedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}
	_ = writeJSONFile(filepath.Join(repoRoot, ".semspec", workflow.StandardsFile), oldST)

	store2 := newProjectStore(tw, repoRoot, slog.Default())
	store2.reconcile(ctx)

	cached := store2.getStandards()
	if cached == nil {
		t.Fatal("standards nil after reconcile")
	}
	if len(cached.Items) != 2 {
		t.Errorf("expected 2 standards, got %d", len(cached.Items))
	}
	if cached.Items[0].ID != "test-coverage" {
		t.Errorf("expected test-coverage standard, got %q", cached.Items[0].ID)
	}
	if cached.TokenEstimate != 120 {
		t.Errorf("token_estimate = %d, want 120", cached.TokenEstimate)
	}
}

// TestIntegration_SaveApprovedConfig verifies that triple writers handle
// the ApprovedAt branch (non-nil approval timestamp).
func TestIntegration_SaveApprovedConfig(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	mock := graphmock.Start(t, tc.Client)

	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, ".semspec"), 0755); err != nil {
		t.Fatal(err)
	}

	tw := &graphutil.TripleWriter{
		NATSClient:    tc.Client,
		Logger:        slog.Default(),
		ComponentName: "project-manager",
	}
	store := newProjectStore(tw, repoRoot, slog.Default())

	now := time.Now()
	approvedAt := now.Add(-time.Hour)

	// Save approved project config.
	pc := &workflow.ProjectConfig{
		Name: "approved-project", Version: "1.0.0", Org: "org", Platform: "plat",
		ApprovedAt: &approvedAt, UpdatedAt: now,
	}
	if err := store.saveConfig(ctx, pc); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	// Save approved checklist.
	cl := &workflow.Checklist{
		Version: "1.0.0", CreatedAt: now, ApprovedAt: &approvedAt, UpdatedAt: now,
		Checks: []workflow.Check{{Name: "lint", Command: "golangci-lint run", Category: "lint", Required: true}},
	}
	if err := store.saveChecklist(ctx, cl); err != nil {
		t.Fatalf("saveChecklist: %v", err)
	}

	// Save approved standards.
	st := &workflow.Standards{
		Version: "1.0.0", GeneratedAt: now, ApprovedAt: &approvedAt, UpdatedAt: now,
		Items: []workflow.Standard{{ID: "r1", Text: "Standard 1", Severity: "error"}},
	}
	if err := store.saveStandards(ctx, st); err != nil {
		t.Fatalf("saveStandards: %v", err)
	}

	// Verify ApprovedAt triples were written for all three.
	for _, configType := range []string{"project", "checklist", "standards"} {
		entityID := workflow.ProjectConfigEntityID(configType)
		entity, ok := mock.Entity(entityID)
		if !ok {
			t.Errorf("%s: entity not in graph", configType)
			continue
		}
		approvedVal := graphmock.TripleValue(entity.Triples, semspec.ProjectConfigApprovedAt)
		if approvedVal == "" {
			t.Errorf("%s: expected approved_at triple, got empty", configType)
		}
	}
}

// TestIntegration_ChecklistSaveAndReconcile verifies the full round-trip
// for checklist: save → triples → reconcile from graph.
func TestIntegration_ChecklistSaveAndReconcile(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	graphmock.Start(t, tc.Client)

	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, ".semspec"), 0755); err != nil {
		t.Fatal(err)
	}

	tw := &graphutil.TripleWriter{
		NATSClient:    tc.Client,
		Logger:        slog.Default(),
		ComponentName: "project-manager",
	}

	// Save a checklist.
	store := newProjectStore(tw, repoRoot, slog.Default())
	now := time.Now()
	cl := &workflow.Checklist{
		Version:   "1.0.0",
		CreatedAt: now,
		UpdatedAt: now,
		Checks: []workflow.Check{
			{Name: "go-build", Command: "go build ./...", Category: "compile", Required: true},
		},
	}
	if err := store.saveChecklist(ctx, cl); err != nil {
		t.Fatalf("saveChecklist: %v", err)
	}

	// Create a fresh store and reconcile — should recover from graph.
	// Write an old file so graph wins.
	oldCL := workflow.Checklist{Version: "1.0.0", UpdatedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}
	_ = writeJSONFile(filepath.Join(repoRoot, ".semspec", workflow.ChecklistFile), oldCL)

	store2 := newProjectStore(tw, repoRoot, slog.Default())
	store2.reconcile(ctx)

	cached := store2.getChecklist()
	if cached == nil {
		t.Fatal("checklist nil after reconcile")
	}
	if len(cached.Checks) != 1 || cached.Checks[0].Name != "go-build" {
		t.Errorf("expected go-build check, got %+v", cached.Checks)
	}
}
