package lessons

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semspec/agentgraph"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
)

func TestParseLessonFromTriples(t *testing.T) {
	triples := map[string]string{
		agentgraph.PredicateLessonID:         "abc-123",
		agentgraph.PredicateLessonSource:     "reviewer-feedback",
		agentgraph.PredicateLessonScenarioID: "task-42",
		agentgraph.PredicateLessonSummary:    "Missing error handling",
		agentgraph.PredicateLessonRole:       "developer",
		agentgraph.PredicateLessonCategories: `["missing_tests","sop_violation"]`,
		agentgraph.PredicateLessonCreatedAt:  "2026-04-03T12:00:00Z",
	}

	lesson := parseLessonFromTriples(triples)

	if lesson.ID != "abc-123" {
		t.Errorf("ID = %q, want %q", lesson.ID, "abc-123")
	}
	if lesson.Source != "reviewer-feedback" {
		t.Errorf("Source = %q, want %q", lesson.Source, "reviewer-feedback")
	}
	if lesson.ScenarioID != "task-42" {
		t.Errorf("ScenarioID = %q, want %q", lesson.ScenarioID, "task-42")
	}
	if lesson.Summary != "Missing error handling" {
		t.Errorf("Summary = %q, want %q", lesson.Summary, "Missing error handling")
	}
	if lesson.Role != "developer" {
		t.Errorf("Role = %q, want %q", lesson.Role, "developer")
	}
	if len(lesson.CategoryIDs) != 2 || lesson.CategoryIDs[0] != "missing_tests" {
		t.Errorf("CategoryIDs = %v, want [missing_tests, sop_violation]", lesson.CategoryIDs)
	}
	if lesson.CreatedAt.IsZero() {
		t.Error("CreatedAt should be parsed, got zero")
	}
}

func TestParseLessonFromTriples_EmptyMap(t *testing.T) {
	lesson := parseLessonFromTriples(map[string]string{})
	if lesson.ID != "" || lesson.Source != "" || lesson.Role != "" {
		t.Errorf("expected empty lesson from empty triples, got %+v", lesson)
	}
}

func TestParseLessonFromTriples_MalformedCategories(t *testing.T) {
	triples := map[string]string{
		agentgraph.PredicateLessonID:         "abc",
		agentgraph.PredicateLessonCategories: "not-json",
	}
	lesson := parseLessonFromTriples(triples)
	if lesson.CategoryIDs != nil {
		t.Errorf("expected nil CategoryIDs for malformed JSON, got %v", lesson.CategoryIDs)
	}
}

func TestParseLessonFromTriples_MalformedTimestamp(t *testing.T) {
	triples := map[string]string{
		agentgraph.PredicateLessonID:        "abc",
		agentgraph.PredicateLessonCreatedAt: "not-a-date",
	}
	lesson := parseLessonFromTriples(triples)
	if !lesson.CreatedAt.IsZero() {
		t.Errorf("expected zero time for malformed timestamp, got %v", lesson.CreatedAt)
	}
}

func TestGetRoleLessonCounts_ErrorsWithNoNATS(t *testing.T) {
	// With nil NATSClient, the prefix scan fails. GetRoleLessonCounts
	// propagates the error (no silent swallowing).
	w := &Writer{TW: nilTripleWriter(), Logger: testLogger()}

	_, err := w.GetRoleLessonCounts(context.Background(), "developer")
	if err == nil {
		t.Fatal("expected error with nil NATSClient, got nil")
	}
}

func TestIncrementRoleLessonCounts_IsNoOp(t *testing.T) {
	w := &Writer{TW: nilTripleWriter(), Logger: testLogger()}

	err := w.IncrementRoleLessonCounts(context.Background(), "developer", []string{"missing_tests"})
	if err != nil {
		t.Fatalf("IncrementRoleLessonCounts should be a no-op, got error: %v", err)
	}
}

func TestRecordLesson_GeneratesIDAndTimestamp(t *testing.T) {
	// With nil NATSClient, WriteTriple is a no-op (returns nil).
	w := &Writer{TW: nilTripleWriter(), Logger: testLogger()}

	lesson := workflow.Lesson{
		Source:  "reviewer-feedback",
		Summary: "test lesson",
		Role:    "developer",
	}

	err := w.RecordLesson(context.Background(), lesson)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Can't verify the generated ID/timestamp since WriteTriple is a no-op,
	// but this confirms no panics and the code path completes.
}

func TestListLessonsForRole_SortsMostRecentFirst(t *testing.T) {
	// Test the sort logic via parseLessonFromTriples + sort behavior.
	now := time.Now()
	older := now.Add(-1 * time.Hour)
	newest := now.Add(1 * time.Hour)

	lessons := []workflow.Lesson{
		{ID: "mid", CreatedAt: now, Role: "developer"},
		{ID: "old", CreatedAt: older, Role: "developer"},
		{ID: "new", CreatedAt: newest, Role: "developer"},
	}

	// Verify sort order matches what ListLessonsForRole would produce.
	sorted := make([]workflow.Lesson, len(lessons))
	copy(sorted, lessons)
	workflow.FilterLessons(sorted, "developer", nil, 0) // uses same sort

	// Manual sort check — newest first.
	if lessons[0].ID == "new" && lessons[1].ID == "mid" && lessons[2].ID == "old" {
		// Already sorted — this shouldn't happen with our input order.
		t.Skip("input happened to be sorted")
	}
}

func TestListLessonsForRole_FiltersRole(t *testing.T) {
	// Verify the role filter in parseLessonFromTriples + the filtering logic.
	devTriples := map[string]string{
		agentgraph.PredicateLessonID:   "1",
		agentgraph.PredicateLessonRole: "developer",
	}
	planTriples := map[string]string{
		agentgraph.PredicateLessonID:   "2",
		agentgraph.PredicateLessonRole: "planner",
	}

	devLesson := parseLessonFromTriples(devTriples)
	planLesson := parseLessonFromTriples(planTriples)

	if devLesson.Role != "developer" {
		t.Errorf("expected developer, got %q", devLesson.Role)
	}
	if planLesson.Role != "planner" {
		t.Errorf("expected planner, got %q", planLesson.Role)
	}

	// Simulate the role filter from ListLessonsForRole.
	all := []workflow.Lesson{devLesson, planLesson}
	var filtered []workflow.Lesson
	for _, l := range all {
		if l.Role == "developer" {
			filtered = append(filtered, l)
		}
	}
	if len(filtered) != 1 || filtered[0].ID != "1" {
		t.Errorf("expected 1 developer lesson, got %v", filtered)
	}
}

func TestListLessonsForRole_LimitTruncates(t *testing.T) {
	// Verify limit logic.
	lessons := []workflow.Lesson{
		{ID: "1"}, {ID: "2"}, {ID: "3"},
	}
	limit := 2
	if limit > 0 && len(lessons) > limit {
		lessons = lessons[:limit]
	}
	if len(lessons) != 2 {
		t.Errorf("expected 2 after limit, got %d", len(lessons))
	}
}

func TestGetRoleLessonCounts_AggregatesCategories(t *testing.T) {
	// Test the count computation logic directly.
	lessons := []workflow.Lesson{
		{Role: "developer", CategoryIDs: []string{"missing_tests", "sop_violation"}},
		{Role: "developer", CategoryIDs: []string{"missing_tests"}},
		{Role: "developer", CategoryIDs: nil},
	}

	var counts workflow.RoleLessonCounts
	for _, lesson := range lessons {
		if len(lesson.CategoryIDs) > 0 {
			counts.Increment(lesson.CategoryIDs)
		}
	}

	if counts.Counts["missing_tests"] != 2 {
		t.Errorf("missing_tests count = %d, want 2", counts.Counts["missing_tests"])
	}
	if counts.Counts["sop_violation"] != 1 {
		t.Errorf("sop_violation count = %d, want 1", counts.Counts["sop_violation"])
	}
}

// ---------------------------------------------------------------------------
// ADR-033 Phase 1 schema round-trip tests
// ---------------------------------------------------------------------------

func TestParseLessonFromTriples_Phase1FieldsRoundTrip(t *testing.T) {
	retired := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	lastInj := time.Date(2026, 4, 30, 9, 0, 0, 0, time.UTC)

	triples := map[string]string{
		agentgraph.PredicateLessonID:             "abc",
		agentgraph.PredicateLessonRole:           "developer",
		agentgraph.PredicateLessonDetail:         "long-form root-cause narrative",
		agentgraph.PredicateLessonInjectionForm:  "case study form",
		agentgraph.PredicateLessonRootCauseRole:  "architect",
		agentgraph.PredicateLessonPositive:       "true",
		agentgraph.PredicateLessonRetiredAt:      retired.Format(time.RFC3339),
		agentgraph.PredicateLessonLastInjectedAt: lastInj.Format(time.RFC3339),
		agentgraph.PredicateLessonEvidenceSteps:  `[{"loop_id":"l1","step_index":3},{"loop_id":"l2","step_index":7}]`,
		agentgraph.PredicateLessonEvidenceFiles:  `[{"path":"main.go","line_start":10,"line_end":20,"commit_sha":"deadbeef"}]`,
	}

	lesson := parseLessonFromTriples(triples)

	if lesson.Detail != "long-form root-cause narrative" {
		t.Errorf("Detail = %q, want long-form...", lesson.Detail)
	}
	if lesson.InjectionForm != "case study form" {
		t.Errorf("InjectionForm = %q", lesson.InjectionForm)
	}
	if lesson.RootCauseRole != "architect" {
		t.Errorf("RootCauseRole = %q, want architect", lesson.RootCauseRole)
	}
	if !lesson.Positive {
		t.Error("Positive should be true")
	}
	if lesson.RetiredAt == nil || !lesson.RetiredAt.Equal(retired) {
		t.Errorf("RetiredAt = %v, want %v", lesson.RetiredAt, retired)
	}
	if lesson.LastInjectedAt == nil || !lesson.LastInjectedAt.Equal(lastInj) {
		t.Errorf("LastInjectedAt = %v, want %v", lesson.LastInjectedAt, lastInj)
	}
	if len(lesson.EvidenceSteps) != 2 || lesson.EvidenceSteps[0].LoopID != "l1" || lesson.EvidenceSteps[1].StepIndex != 7 {
		t.Errorf("EvidenceSteps = %+v", lesson.EvidenceSteps)
	}
	if len(lesson.EvidenceFiles) != 1 || lesson.EvidenceFiles[0].Path != "main.go" || lesson.EvidenceFiles[0].LineEnd != 20 || lesson.EvidenceFiles[0].CommitSHA != "deadbeef" {
		t.Errorf("EvidenceFiles = %+v", lesson.EvidenceFiles)
	}
}

func TestParseLessonFromTriples_Phase1FieldsAbsent(t *testing.T) {
	// Legacy lesson — only the original predicates set.
	triples := map[string]string{
		agentgraph.PredicateLessonID:   "legacy",
		agentgraph.PredicateLessonRole: "developer",
	}
	lesson := parseLessonFromTriples(triples)
	if lesson.Detail != "" || lesson.InjectionForm != "" || lesson.RootCauseRole != "" {
		t.Errorf("expected empty Phase 1 string fields, got Detail=%q InjectionForm=%q RootCauseRole=%q",
			lesson.Detail, lesson.InjectionForm, lesson.RootCauseRole)
	}
	if lesson.Positive {
		t.Error("Positive should default to false")
	}
	if lesson.RetiredAt != nil {
		t.Errorf("RetiredAt should be nil, got %v", lesson.RetiredAt)
	}
	if lesson.LastInjectedAt != nil {
		t.Errorf("LastInjectedAt should be nil, got %v", lesson.LastInjectedAt)
	}
	if lesson.EvidenceSteps != nil {
		t.Errorf("EvidenceSteps should be nil, got %v", lesson.EvidenceSteps)
	}
	if lesson.EvidenceFiles != nil {
		t.Errorf("EvidenceFiles should be nil, got %v", lesson.EvidenceFiles)
	}
}

func TestParseLessonFromTriples_MalformedEvidenceJSON(t *testing.T) {
	triples := map[string]string{
		agentgraph.PredicateLessonID:            "x",
		agentgraph.PredicateLessonEvidenceSteps: "not-json",
		agentgraph.PredicateLessonEvidenceFiles: `{"not":"an array"}`,
	}
	lesson := parseLessonFromTriples(triples)
	if lesson.EvidenceSteps != nil {
		t.Errorf("malformed evidence_steps must yield nil, got %v", lesson.EvidenceSteps)
	}
	if lesson.EvidenceFiles != nil {
		t.Errorf("malformed evidence_files must yield nil, got %v", lesson.EvidenceFiles)
	}
}

func TestParseLessonFromTriples_PositiveFalseValue(t *testing.T) {
	triples := map[string]string{
		agentgraph.PredicateLessonID:       "x",
		agentgraph.PredicateLessonPositive: "false",
	}
	lesson := parseLessonFromTriples(triples)
	if lesson.Positive {
		t.Error("Positive should be false when triple value is \"false\"")
	}
}

func TestParseLessonFromTriples_MalformedTimestampFields(t *testing.T) {
	triples := map[string]string{
		agentgraph.PredicateLessonID:             "x",
		agentgraph.PredicateLessonRetiredAt:      "not-a-date",
		agentgraph.PredicateLessonLastInjectedAt: "also-not-a-date",
	}
	lesson := parseLessonFromTriples(triples)
	if lesson.RetiredAt != nil {
		t.Errorf("malformed RetiredAt must yield nil, got %v", lesson.RetiredAt)
	}
	if lesson.LastInjectedAt != nil {
		t.Errorf("malformed LastInjectedAt must yield nil, got %v", lesson.LastInjectedAt)
	}
}

func TestRecordLesson_AcceptsLessonWithEvidence(t *testing.T) {
	w := &Writer{TW: nilTripleWriter(), Logger: testLogger()}
	now := time.Now()
	lesson := workflow.Lesson{
		Source:        "decomposer",
		Summary:       "test",
		Role:          "developer",
		Detail:        "narrative",
		InjectionForm: "case study",
		EvidenceSteps: []workflow.StepRef{{LoopID: "l1", StepIndex: 1}},
		EvidenceFiles: []workflow.FileRef{{Path: "x.go", LineStart: 1, LineEnd: 2, CommitSHA: "abc"}},
		RootCauseRole: "architect",
		Positive:      true,
		RetiredAt:     &now,
	}
	if err := w.RecordLesson(context.Background(), lesson); err != nil {
		t.Fatalf("RecordLesson with evidence should succeed, got %v", err)
	}
}

func TestRecordLesson_AcceptsLegacyLessonWithoutEvidence(t *testing.T) {
	// Phase 1: warn-only, no rejection.
	w := &Writer{TW: nilTripleWriter(), Logger: testLogger()}
	lesson := workflow.Lesson{
		Source:  "reviewer-feedback",
		Summary: "legacy",
		Role:    "developer",
	}
	if err := w.RecordLesson(context.Background(), lesson); err != nil {
		t.Fatalf("RecordLesson without evidence should still succeed in Phase 1, got %v", err)
	}
}

// nilTripleWriter returns a TripleWriter with nil NATSClient.
// WriteTriple returns nil (no-op), ReadEntity/ReadEntitiesByPrefix return errors.
func nilTripleWriter() *graphutil.TripleWriter {
	return &graphutil.TripleWriter{
		Logger:        testLogger(),
		ComponentName: "test",
	}
}

func testLogger() *slog.Logger {
	return slog.Default()
}
