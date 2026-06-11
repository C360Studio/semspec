package projectmanager

// Tests for the pure triple-builder helpers extracted in issue #154 slice #3.
//
// These are unit tests that exercise buildConfigTriples, buildChecklistTriples,
// and buildStandardsTriples without a live NATS connection. They pin:
//   - the projectConfigEntityType Domain/Category/Version values
//   - every required scalar predicate is present with the correct value
//   - ProjectConfigApprovedAt is emitted when ApprovedAt is set, absent when nil
//   - ProjectConfigJSON blob triple is always present
//   - all Subject fields match the supplied entity ID
//
// The seam pattern is copied from workflow/lessons/writer_test.go (slice #1).

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"

	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow"
)

// indexByPred builds predicate → []string from a triple slice. It verifies
// every triple's Subject equals wantEID and every Object is a string.
func indexByPred(t *testing.T, wantEID string, triples []message.Triple) map[string][]string {
	t.Helper()
	byPred := make(map[string][]string)
	for _, tr := range triples {
		if tr.Subject != wantEID {
			t.Errorf("triple subject %q != entity ID %q", tr.Subject, wantEID)
		}
		val, ok := tr.Object.(string)
		if !ok {
			t.Errorf("predicate %q: Object is %T, want string", tr.Predicate, tr.Object)
			continue
		}
		byPred[tr.Predicate] = append(byPred[tr.Predicate], val)
	}
	return byPred
}

// require asserts that predicate pred has exactly one triple with value want.
func requirePred(t *testing.T, byPred map[string][]string, pred, want string) {
	t.Helper()
	vals := byPred[pred]
	if len(vals) == 0 {
		t.Errorf("predicate %q absent from triples", pred)
		return
	}
	if vals[0] != want {
		t.Errorf("predicate %q = %q, want %q", pred, vals[0], want)
	}
}

// absentPred asserts that predicate pred has no triples.
func absentPred(t *testing.T, byPred map[string][]string, pred string) {
	t.Helper()
	if len(byPred[pred]) > 0 {
		t.Errorf("predicate %q should be absent, got %v", pred, byPred[pred])
	}
}

// ---------------------------------------------------------------------------
// projectConfigEntityType: Domain / Category / Version
// ---------------------------------------------------------------------------

// TestProjectConfigEntityType_Fields pins the exact message.Type values chosen
// for the project-config entity. These must match the entity ID namespace
// {prefix}.wf.project.config.{hash}.
func TestProjectConfigEntityType_Fields(t *testing.T) {
	if projectConfigEntityType.Domain != "project-config" {
		t.Errorf("Domain = %q, want %q", projectConfigEntityType.Domain, "project-config")
	}
	if projectConfigEntityType.Category != "entity" {
		t.Errorf("Category = %q, want %q", projectConfigEntityType.Category, "entity")
	}
	if projectConfigEntityType.Version != "v1" {
		t.Errorf("Version = %q, want %q", projectConfigEntityType.Version, "v1")
	}
}

// ---------------------------------------------------------------------------
// buildConfigTriples
// ---------------------------------------------------------------------------

func TestBuildConfigTriples_RequiredScalars(t *testing.T) {
	eid := "semspec.local.wf.project.config.abc"
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	pc := &workflow.ProjectConfig{
		Name:      "myproject",
		Org:       "myorg",
		Platform:  "myplat",
		UpdatedAt: now,
	}

	byPred := indexByPred(t, eid, buildConfigTriples(eid, pc))

	requirePred(t, byPred, semspec.ProjectConfigType, "project")
	requirePred(t, byPred, semspec.ProjectConfigFile, workflow.ProjectConfigFile)
	requirePred(t, byPred, semspec.ProjectConfigName, "myproject")
	requirePred(t, byPred, semspec.ProjectConfigOrg, "myorg")
	requirePred(t, byPred, semspec.ProjectConfigPlatform, "myplat")
	requirePred(t, byPred, semspec.ProjectConfigUpdatedAt, now.Format(time.RFC3339))
	requirePred(t, byPred, semspec.ProjectConfigApproved, "false")
}

func TestBuildConfigTriples_JSONBlobPresent(t *testing.T) {
	eid := "semspec.local.wf.project.config.abc"
	pc := &workflow.ProjectConfig{Name: "blob-test", Org: "o", Platform: "p", UpdatedAt: time.Now()}

	byPred := indexByPred(t, eid, buildConfigTriples(eid, pc))

	blob := byPred[semspec.ProjectConfigJSON]
	if len(blob) == 0 || blob[0] == "" {
		t.Fatal("ProjectConfigJSON blob predicate absent or empty")
	}
	// Verify the blob round-trips.
	var rt workflow.ProjectConfig
	if err := json.Unmarshal([]byte(blob[0]), &rt); err != nil {
		t.Fatalf("JSON blob round-trip unmarshal failed: %v", err)
	}
	if rt.Name != "blob-test" {
		t.Errorf("round-trip Name = %q, want blob-test", rt.Name)
	}
}

func TestBuildConfigTriples_ApprovedAtAbsentWhenNil(t *testing.T) {
	eid := "semspec.local.wf.project.config.abc"
	pc := &workflow.ProjectConfig{Name: "p", Org: "o", Platform: "pl", UpdatedAt: time.Now()}
	// ApprovedAt is nil — predicate must be absent.

	byPred := indexByPred(t, eid, buildConfigTriples(eid, pc))
	absentPred(t, byPred, semspec.ProjectConfigApprovedAt)
}

func TestBuildConfigTriples_ApprovedAtPresentWhenSet(t *testing.T) {
	eid := "semspec.local.wf.project.config.abc"
	approvedAt := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	pc := &workflow.ProjectConfig{
		Name:       "approved",
		Org:        "o",
		Platform:   "pl",
		UpdatedAt:  time.Now(),
		ApprovedAt: &approvedAt,
	}

	byPred := indexByPred(t, eid, buildConfigTriples(eid, pc))
	requirePred(t, byPred, semspec.ProjectConfigApprovedAt, approvedAt.Format(time.RFC3339))
	requirePred(t, byPred, semspec.ProjectConfigApproved, "true")
}

// ---------------------------------------------------------------------------
// buildChecklistTriples
// ---------------------------------------------------------------------------

func TestBuildChecklistTriples_RequiredScalars(t *testing.T) {
	eid := "semspec.local.wf.project.config.def"
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	cl := &workflow.Checklist{
		Version:   "1.0.0",
		UpdatedAt: now,
	}

	byPred := indexByPred(t, eid, buildChecklistTriples(eid, cl))

	requirePred(t, byPred, semspec.ProjectConfigType, "checklist")
	requirePred(t, byPred, semspec.ProjectConfigFile, workflow.ChecklistFile)
	requirePred(t, byPred, semspec.ProjectConfigUpdatedAt, now.Format(time.RFC3339))
	requirePred(t, byPred, semspec.ProjectConfigApproved, "false")
}

func TestBuildChecklistTriples_JSONBlobPresent(t *testing.T) {
	eid := "semspec.local.wf.project.config.def"
	cl := &workflow.Checklist{Version: "1.0.0", UpdatedAt: time.Now()}

	byPred := indexByPred(t, eid, buildChecklistTriples(eid, cl))

	blob := byPred[semspec.ProjectConfigJSON]
	if len(blob) == 0 || blob[0] == "" {
		t.Fatal("ProjectConfigJSON blob predicate absent or empty for checklist")
	}
}

func TestBuildChecklistTriples_ApprovedAtAbsentWhenNil(t *testing.T) {
	eid := "semspec.local.wf.project.config.def"
	cl := &workflow.Checklist{Version: "1.0.0", UpdatedAt: time.Now()}

	byPred := indexByPred(t, eid, buildChecklistTriples(eid, cl))
	absentPred(t, byPred, semspec.ProjectConfigApprovedAt)
}

func TestBuildChecklistTriples_ApprovedAtPresentWhenSet(t *testing.T) {
	eid := "semspec.local.wf.project.config.def"
	approvedAt := time.Date(2026, 4, 15, 8, 0, 0, 0, time.UTC)
	cl := &workflow.Checklist{
		Version:    "1.0.0",
		UpdatedAt:  time.Now(),
		ApprovedAt: &approvedAt,
	}

	byPred := indexByPred(t, eid, buildChecklistTriples(eid, cl))
	requirePred(t, byPred, semspec.ProjectConfigApprovedAt, approvedAt.Format(time.RFC3339))
	requirePred(t, byPred, semspec.ProjectConfigApproved, "true")
}

// ---------------------------------------------------------------------------
// buildStandardsTriples
// ---------------------------------------------------------------------------

func TestBuildStandardsTriples_RequiredScalars(t *testing.T) {
	eid := "semspec.local.wf.project.config.ghi"
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	st := &workflow.Standards{
		Version:   "1.0.0",
		UpdatedAt: now,
	}

	byPred := indexByPred(t, eid, buildStandardsTriples(eid, st))

	requirePred(t, byPred, semspec.ProjectConfigType, "standards")
	requirePred(t, byPred, semspec.ProjectConfigFile, workflow.StandardsFile)
	requirePred(t, byPred, semspec.ProjectConfigUpdatedAt, now.Format(time.RFC3339))
	requirePred(t, byPred, semspec.ProjectConfigApproved, "false")
}

func TestBuildStandardsTriples_JSONBlobPresent(t *testing.T) {
	eid := "semspec.local.wf.project.config.ghi"
	st := &workflow.Standards{Version: "1.0.0", UpdatedAt: time.Now()}

	byPred := indexByPred(t, eid, buildStandardsTriples(eid, st))

	blob := byPred[semspec.ProjectConfigJSON]
	if len(blob) == 0 || blob[0] == "" {
		t.Fatal("ProjectConfigJSON blob predicate absent or empty for standards")
	}
}

func TestBuildStandardsTriples_ApprovedAtAbsentWhenNil(t *testing.T) {
	eid := "semspec.local.wf.project.config.ghi"
	st := &workflow.Standards{Version: "1.0.0", UpdatedAt: time.Now()}

	byPred := indexByPred(t, eid, buildStandardsTriples(eid, st))
	absentPred(t, byPred, semspec.ProjectConfigApprovedAt)
}

func TestBuildStandardsTriples_ApprovedAtPresentWhenSet(t *testing.T) {
	eid := "semspec.local.wf.project.config.ghi"
	approvedAt := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	st := &workflow.Standards{
		Version:    "1.0.0",
		UpdatedAt:  time.Now(),
		ApprovedAt: &approvedAt,
	}

	byPred := indexByPred(t, eid, buildStandardsTriples(eid, st))
	requirePred(t, byPred, semspec.ProjectConfigApprovedAt, approvedAt.Format(time.RFC3339))
	requirePred(t, byPred, semspec.ProjectConfigApproved, "true")
}
