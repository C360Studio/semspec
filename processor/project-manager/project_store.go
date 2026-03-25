package projectmanager

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
)

// projectStore owns the lifecycle of project configuration entities
// (project.json, checklist.json, standards.json).
// It follows the same 3-layer pattern as planStore:
//
//  1. atomic.Pointer — hot cache, all runtime reads go here
//  2. WriteTriple — durable write-through to ENTITY_STATES via graph-ingest
//  3. reconcile — startup-only recovery, comparing file vs graph timestamps
//
// Runtime reads NEVER hit the graph or filesystem.
type projectStore struct {
	config    atomic.Pointer[workflow.ProjectConfig]
	checklist atomic.Pointer[workflow.Checklist]
	standards atomic.Pointer[workflow.Standards]

	tripleWriter *graphutil.TripleWriter
	repoPath     string
	logger       *slog.Logger
}

// newProjectStore creates a project store. Pass nil tripleWriter for file-only mode.
func newProjectStore(tw *graphutil.TripleWriter, repoPath string, logger *slog.Logger) *projectStore {
	return &projectStore{
		tripleWriter: tw,
		repoPath:     repoPath,
		logger:       logger,
	}
}

// semspecDir returns the .semspec directory path.
func (s *projectStore) semspecDir() string {
	return filepath.Join(s.repoPath, ".semspec")
}

// ----------------------------------------------------------------------------
// Reconciliation
// ----------------------------------------------------------------------------

// reconcile populates the cache from files and graph on startup.
// Strategy: load files first (to bootstrap EntityPrefix), then query graph,
// compare UpdatedAt timestamps, take the later version.
func (s *projectStore) reconcile(ctx context.Context) {
	// Step 1: Always load files first — this bootstraps EntityPrefix from org/platform.
	s.populateFromFiles()

	// Step 2: Bootstrap entity prefix from loaded project config.
	if pc := s.config.Load(); pc != nil && (pc.Org != "" || pc.Platform != "" || pc.Name != "") {
		workflow.InitEntityPrefix(pc.Org, pc.Platform, pc.Name)
	}

	// Step 3: If we have a tripleWriter, query graph and reconcile.
	if s.tripleWriter == nil {
		return
	}

	reconcileCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	prefix := workflow.EntityPrefix() + ".wf.project.config."
	entities, err := s.tripleWriter.ReadEntitiesByPrefix(reconcileCtx, prefix, 10)
	if err != nil {
		s.logger.Warn("Project config graph reconciliation failed, using file versions",
			"error", err)
		return
	}

	for _, triples := range entities {
		configType := triples[semspec.ProjectConfigType]
		graphUpdatedAt := parseRFC3339(triples[semspec.ProjectConfigUpdatedAt])
		jsonBlob := triples[semspec.ProjectConfigJSON]

		switch configType {
		case "project":
			s.reconcileConfig(jsonBlob, graphUpdatedAt)
		case "checklist":
			s.reconcileChecklist(jsonBlob, graphUpdatedAt)
		case "standards":
			s.reconcileStandards(jsonBlob, graphUpdatedAt)
		}
	}
}

// reconcileConfig compares graph version against cached file version for project.json.
func (s *projectStore) reconcileConfig(jsonBlob string, graphUpdatedAt time.Time) {
	if jsonBlob == "" {
		return
	}
	fileVersion := s.config.Load()
	if fileVersion != nil && !graphUpdatedAt.After(fileVersion.UpdatedAt) {
		return // file is same or newer
	}
	var pc workflow.ProjectConfig
	if err := json.Unmarshal([]byte(jsonBlob), &pc); err != nil {
		s.logger.Warn("Failed to unmarshal project config from graph", "error", err)
		return
	}
	s.config.Store(&pc)
	// Write-back to file so they stay in sync.
	_ = writeJSONFile(filepath.Join(s.semspecDir(), workflow.ProjectConfigFile), pc)
	s.logger.Info("Project config: graph version is newer, synced to file",
		"graph_updated_at", graphUpdatedAt,
		"file_updated_at", fileVersion.UpdatedAt)
}

// reconcileChecklist compares graph version against cached file version for checklist.json.
func (s *projectStore) reconcileChecklist(jsonBlob string, graphUpdatedAt time.Time) {
	if jsonBlob == "" {
		return
	}
	fileVersion := s.checklist.Load()
	if fileVersion != nil && !graphUpdatedAt.After(fileVersion.UpdatedAt) {
		return
	}
	var cl workflow.Checklist
	if err := json.Unmarshal([]byte(jsonBlob), &cl); err != nil {
		s.logger.Warn("Failed to unmarshal checklist from graph", "error", err)
		return
	}
	s.checklist.Store(&cl)
	_ = writeJSONFile(filepath.Join(s.semspecDir(), workflow.ChecklistFile), cl)
	s.logger.Info("Checklist: graph version is newer, synced to file",
		"graph_updated_at", graphUpdatedAt)
}

// reconcileStandards compares graph version against cached file version for standards.json.
func (s *projectStore) reconcileStandards(jsonBlob string, graphUpdatedAt time.Time) {
	if jsonBlob == "" {
		return
	}
	fileVersion := s.standards.Load()
	if fileVersion != nil && !graphUpdatedAt.After(fileVersion.UpdatedAt) {
		return
	}
	var st workflow.Standards
	if err := json.Unmarshal([]byte(jsonBlob), &st); err != nil {
		s.logger.Warn("Failed to unmarshal standards from graph", "error", err)
		return
	}
	s.standards.Store(&st)
	_ = writeJSONFile(filepath.Join(s.semspecDir(), workflow.StandardsFile), st)
	s.logger.Info("Standards: graph version is newer, synced to file",
		"graph_updated_at", graphUpdatedAt)
}

// populateFromFiles loads all three config files into the cache.
func (s *projectStore) populateFromFiles() {
	dir := s.semspecDir()
	if pc, err := loadJSONFile[workflow.ProjectConfig](filepath.Join(dir, workflow.ProjectConfigFile)); err == nil {
		s.config.Store(&pc)
	}
	if cl, err := loadJSONFile[workflow.Checklist](filepath.Join(dir, workflow.ChecklistFile)); err == nil {
		s.checklist.Store(&cl)
	}
	if st, err := loadJSONFile[workflow.Standards](filepath.Join(dir, workflow.StandardsFile)); err == nil {
		s.standards.Store(&st)
	}
}

// ----------------------------------------------------------------------------
// Cache reads (hot path)
// ----------------------------------------------------------------------------

// getConfig returns the cached project config, or nil if not loaded.
func (s *projectStore) getConfig() *workflow.ProjectConfig { return s.config.Load() }

// getChecklist returns the cached checklist, or nil if not loaded.
func (s *projectStore) getChecklist() *workflow.Checklist { return s.checklist.Load() }

// getStandards returns the cached standards, or nil if not loaded.
func (s *projectStore) getStandards() *workflow.Standards { return s.standards.Load() }

// ----------------------------------------------------------------------------
// Write-through (cache + triples + file)
// ----------------------------------------------------------------------------

// saveConfig writes a project config through to triples, cache, and file.
func (s *projectStore) saveConfig(ctx context.Context, pc *workflow.ProjectConfig) error {
	if err := s.writeConfigTriples(ctx, pc); err != nil {
		s.logger.Warn("Failed to write project config triples (file still updated)", "error", err)
	}
	s.config.Store(pc)
	return writeJSONFile(filepath.Join(s.semspecDir(), workflow.ProjectConfigFile), pc)
}

// saveChecklist writes a checklist through to triples, cache, and file.
func (s *projectStore) saveChecklist(ctx context.Context, cl *workflow.Checklist) error {
	if err := s.writeChecklistTriples(ctx, cl); err != nil {
		s.logger.Warn("Failed to write checklist triples (file still updated)", "error", err)
	}
	s.checklist.Store(cl)
	return writeJSONFile(filepath.Join(s.semspecDir(), workflow.ChecklistFile), cl)
}

// saveStandards writes standards through to triples, cache, and file.
func (s *projectStore) saveStandards(ctx context.Context, st *workflow.Standards) error {
	if err := s.writeStandardsTriples(ctx, st); err != nil {
		s.logger.Warn("Failed to write standards triples (file still updated)", "error", err)
	}
	s.standards.Store(st)
	return writeJSONFile(filepath.Join(s.semspecDir(), workflow.StandardsFile), st)
}

// ----------------------------------------------------------------------------
// Triple writers
// ----------------------------------------------------------------------------

// writeConfigTriples writes individual predicates + full JSON blob for project config.
func (s *projectStore) writeConfigTriples(ctx context.Context, pc *workflow.ProjectConfig) error {
	tw := s.tripleWriter
	if tw == nil {
		return nil
	}
	entityID := workflow.ProjectConfigEntityID("project")

	_ = tw.WriteTriple(ctx, entityID, semspec.ProjectConfigType, "project")
	_ = tw.WriteTriple(ctx, entityID, semspec.ProjectConfigFile, workflow.ProjectConfigFile)
	_ = tw.WriteTriple(ctx, entityID, semspec.ProjectConfigName, pc.Name)
	_ = tw.WriteTriple(ctx, entityID, semspec.ProjectConfigOrg, pc.Org)
	_ = tw.WriteTriple(ctx, entityID, semspec.ProjectConfigPlatform, pc.Platform)
	_ = tw.WriteTriple(ctx, entityID, semspec.ProjectConfigUpdatedAt, pc.UpdatedAt.Format(time.RFC3339))
	_ = tw.WriteTriple(ctx, entityID, semspec.ProjectConfigApproved, pc.ApprovedAt != nil)
	if pc.ApprovedAt != nil {
		_ = tw.WriteTriple(ctx, entityID, semspec.ProjectConfigApprovedAt, pc.ApprovedAt.Format(time.RFC3339))
	}

	jsonBlob, err := json.Marshal(pc)
	if err != nil {
		return err
	}
	return tw.WriteTriple(ctx, entityID, semspec.ProjectConfigJSON, string(jsonBlob))
}

// writeChecklistTriples writes triples for checklist config.
func (s *projectStore) writeChecklistTriples(ctx context.Context, cl *workflow.Checklist) error {
	tw := s.tripleWriter
	if tw == nil {
		return nil
	}
	entityID := workflow.ProjectConfigEntityID("checklist")

	_ = tw.WriteTriple(ctx, entityID, semspec.ProjectConfigType, "checklist")
	_ = tw.WriteTriple(ctx, entityID, semspec.ProjectConfigFile, workflow.ChecklistFile)
	_ = tw.WriteTriple(ctx, entityID, semspec.ProjectConfigUpdatedAt, cl.UpdatedAt.Format(time.RFC3339))
	_ = tw.WriteTriple(ctx, entityID, semspec.ProjectConfigApproved, cl.ApprovedAt != nil)
	if cl.ApprovedAt != nil {
		_ = tw.WriteTriple(ctx, entityID, semspec.ProjectConfigApprovedAt, cl.ApprovedAt.Format(time.RFC3339))
	}

	jsonBlob, err := json.Marshal(cl)
	if err != nil {
		return err
	}
	return tw.WriteTriple(ctx, entityID, semspec.ProjectConfigJSON, string(jsonBlob))
}

// writeStandardsTriples writes triples for standards config.
func (s *projectStore) writeStandardsTriples(ctx context.Context, st *workflow.Standards) error {
	tw := s.tripleWriter
	if tw == nil {
		return nil
	}
	entityID := workflow.ProjectConfigEntityID("standards")

	_ = tw.WriteTriple(ctx, entityID, semspec.ProjectConfigType, "standards")
	_ = tw.WriteTriple(ctx, entityID, semspec.ProjectConfigFile, workflow.StandardsFile)
	_ = tw.WriteTriple(ctx, entityID, semspec.ProjectConfigUpdatedAt, st.UpdatedAt.Format(time.RFC3339))
	_ = tw.WriteTriple(ctx, entityID, semspec.ProjectConfigApproved, st.ApprovedAt != nil)
	if st.ApprovedAt != nil {
		_ = tw.WriteTriple(ctx, entityID, semspec.ProjectConfigApprovedAt, st.ApprovedAt.Format(time.RFC3339))
	}

	jsonBlob, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return tw.WriteTriple(ctx, entityID, semspec.ProjectConfigJSON, string(jsonBlob))
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

// parseRFC3339 parses an RFC3339 timestamp, returning zero time on failure.
func parseRFC3339(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}
