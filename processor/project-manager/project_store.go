package projectmanager

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/message"

	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
)

// projectConfigEntityType is the message.Type for project-config entities.
// Domain mirrors the entity ID namespace {prefix}.wf.project.config.{hash}.
// All three config entities (project, checklist, standards) share this type —
// they are distinguished by the semspec.ProjectConfigType predicate value
// ("project" / "checklist" / "standards").
//
// Kept local to this package per issue #154 slice #3; DO NOT add to workflow/entity.go
// (another slice edits nearby).
var projectConfigEntityType = message.Type{
	Domain:   "project-config",
	Category: "entity",
	Version:  "v1",
}

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

// writeConfigTriples persists the project config entity via a single
// metadata-bearing UpsertEntity call (update_with_triples + create_with_triples
// fallback). This ensures the entity carries a MessageType from its very first
// write and survives the semstreams triple.add must-exist change (issue #154
// slice #3).
func (s *projectStore) writeConfigTriples(ctx context.Context, pc *workflow.ProjectConfig) error {
	if s.tripleWriter == nil {
		return nil
	}
	entityID := workflow.ProjectConfigEntityID("project")
	return s.tripleWriter.UpsertEntity(ctx, projectConfigEntityType, entityID, buildConfigTriples(entityID, pc))
}

// writeChecklistTriples persists the checklist config entity via a single
// metadata-bearing UpsertEntity call (issue #154 slice #3).
func (s *projectStore) writeChecklistTriples(ctx context.Context, cl *workflow.Checklist) error {
	if s.tripleWriter == nil {
		return nil
	}
	entityID := workflow.ProjectConfigEntityID("checklist")
	return s.tripleWriter.UpsertEntity(ctx, projectConfigEntityType, entityID, buildChecklistTriples(entityID, cl))
}

// writeStandardsTriples persists the standards config entity via a single
// metadata-bearing UpsertEntity call (issue #154 slice #3).
func (s *projectStore) writeStandardsTriples(ctx context.Context, st *workflow.Standards) error {
	if s.tripleWriter == nil {
		return nil
	}
	entityID := workflow.ProjectConfigEntityID("standards")
	return s.tripleWriter.UpsertEntity(ctx, projectConfigEntityType, entityID, buildStandardsTriples(entityID, st))
}

// ----------------------------------------------------------------------------
// Pure triple builders (testable without NATS)
// ----------------------------------------------------------------------------

// buildConfigTriples constructs the full []message.Triple for a project config entity.
// It is a pure function so it can be unit-tested without a NATS connection.
//
// Every predicate emitted by the old per-predicate UpdateTriple fan-out is preserved
// with identical gating. ProjectConfigApproved is always present ("true"/"false").
// ProjectConfigApprovedAt is conditional on ApprovedAt != nil, matching the original
// behavior. ProjectConfigJSON is always present so the reconcile path can round-trip.
//
// TODO(#154): ProjectConfigJSON is a JSON-blob-in-triple; evaluate dropping once
// reconcile no longer needs it (requires verifying reconcileConfig/reconcileChecklist/
// reconcileStandards no longer read it).
func buildConfigTriples(eid string, pc *workflow.ProjectConfig) []message.Triple {
	approved := "false"
	if pc.ApprovedAt != nil {
		approved = "true"
	}

	// JSON blob for round-trip reconciliation. The error is intentionally
	// ignored: ProjectConfig is a plain JSON-marshalable struct (no chan/func/
	// cyclic fields), so Marshal cannot fail here. The graph write is a
	// best-effort mirror anyway — the .semspec/*.json file is the durable copy.
	jsonBlob, _ := json.Marshal(pc)

	triples := []message.Triple{
		{Subject: eid, Predicate: semspec.ProjectConfigType, Object: "project"},
		{Subject: eid, Predicate: semspec.ProjectConfigFile, Object: workflow.ProjectConfigFile},
		{Subject: eid, Predicate: semspec.ProjectConfigName, Object: pc.Name},
		{Subject: eid, Predicate: semspec.ProjectConfigOrg, Object: pc.Org},
		{Subject: eid, Predicate: semspec.ProjectConfigPlatform, Object: pc.Platform},
		{Subject: eid, Predicate: semspec.ProjectConfigUpdatedAt, Object: pc.UpdatedAt.Format(time.RFC3339)},
		{Subject: eid, Predicate: semspec.ProjectConfigApproved, Object: approved},
		{Subject: eid, Predicate: semspec.ProjectConfigJSON, Object: string(jsonBlob)},
	}
	if pc.ApprovedAt != nil {
		triples = append(triples, message.Triple{
			Subject:   eid,
			Predicate: semspec.ProjectConfigApprovedAt,
			Object:    pc.ApprovedAt.Format(time.RFC3339),
		})
	}
	return triples
}

// buildChecklistTriples constructs the full []message.Triple for a checklist entity.
// Same invariants as buildConfigTriples.
//
// TODO(#154): ProjectConfigJSON is a JSON-blob-in-triple; evaluate dropping once
// reconcile no longer needs it.
func buildChecklistTriples(eid string, cl *workflow.Checklist) []message.Triple {
	approved := "false"
	if cl.ApprovedAt != nil {
		approved = "true"
	}
	jsonBlob, _ := json.Marshal(cl)

	triples := []message.Triple{
		{Subject: eid, Predicate: semspec.ProjectConfigType, Object: "checklist"},
		{Subject: eid, Predicate: semspec.ProjectConfigFile, Object: workflow.ChecklistFile},
		{Subject: eid, Predicate: semspec.ProjectConfigUpdatedAt, Object: cl.UpdatedAt.Format(time.RFC3339)},
		{Subject: eid, Predicate: semspec.ProjectConfigApproved, Object: approved},
		{Subject: eid, Predicate: semspec.ProjectConfigJSON, Object: string(jsonBlob)},
	}
	if cl.ApprovedAt != nil {
		triples = append(triples, message.Triple{
			Subject:   eid,
			Predicate: semspec.ProjectConfigApprovedAt,
			Object:    cl.ApprovedAt.Format(time.RFC3339),
		})
	}
	return triples
}

// buildStandardsTriples constructs the full []message.Triple for a standards entity.
// Same invariants as buildConfigTriples.
//
// TODO(#154): ProjectConfigJSON is a JSON-blob-in-triple; evaluate dropping once
// reconcile no longer needs it.
func buildStandardsTriples(eid string, st *workflow.Standards) []message.Triple {
	approved := "false"
	if st.ApprovedAt != nil {
		approved = "true"
	}
	jsonBlob, _ := json.Marshal(st)

	triples := []message.Triple{
		{Subject: eid, Predicate: semspec.ProjectConfigType, Object: "standards"},
		{Subject: eid, Predicate: semspec.ProjectConfigFile, Object: workflow.StandardsFile},
		{Subject: eid, Predicate: semspec.ProjectConfigUpdatedAt, Object: st.UpdatedAt.Format(time.RFC3339)},
		{Subject: eid, Predicate: semspec.ProjectConfigApproved, Object: approved},
		{Subject: eid, Predicate: semspec.ProjectConfigJSON, Object: string(jsonBlob)},
	}
	if st.ApprovedAt != nil {
		triples = append(triples, message.Triple{
			Subject:   eid,
			Predicate: semspec.ProjectConfigApprovedAt,
			Object:    st.ApprovedAt.Format(time.RFC3339),
		})
	}
	return triples
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

// parseRFC3339 parses an RFC3339 timestamp, returning zero time on failure.
func parseRFC3339(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}
