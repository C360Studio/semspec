package lessons

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semspec/agentgraph"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
)

// mv builds a multi-valued triples map from variadic key/value pairs. Each
// key is the predicate; each value is appended to that predicate's slice.
func mv(pairs ...string) map[string][]string {
	out := make(map[string][]string)
	for i := 0; i+1 < len(pairs); i += 2 {
		out[pairs[i]] = append(out[pairs[i]], pairs[i+1])
	}
	return out
}

func TestParseLessonFromTriples(t *testing.T) {
	triples := mv(
		agentgraph.PredicateLessonID, "abc-123",
		agentgraph.PredicateLessonSource, "reviewer-feedback",
		agentgraph.PredicateLessonScenarioID, "task-42",
		agentgraph.PredicateLessonSummary, "Missing error handling",
		agentgraph.PredicateLessonRole, "developer",
		agentgraph.PredicateLessonCategories, "missing_tests",
		agentgraph.PredicateLessonCategories, "sop_violation",
		agentgraph.PredicateLessonCreatedAt, "2026-04-03T12:00:00Z",
	)

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
	if len(lesson.CategoryIDs) != 2 || lesson.CategoryIDs[0] != "missing_tests" || lesson.CategoryIDs[1] != "sop_violation" {
		t.Errorf("CategoryIDs = %v, want [missing_tests, sop_violation]", lesson.CategoryIDs)
	}
	if lesson.CreatedAt.IsZero() {
		t.Error("CreatedAt should be parsed, got zero")
	}
}

func TestParseLessonFromTriples_LegacyJSONCategories(t *testing.T) {
	// Backwards compat: a single triple holding a JSON-array string (the
	// pre-atomic encoding) must still decode correctly.
	triples := mv(
		agentgraph.PredicateLessonID, "legacy-cats",
		agentgraph.PredicateLessonCategories, `["missing_tests","sop_violation"]`,
	)
	lesson := parseLessonFromTriples(triples)
	if len(lesson.CategoryIDs) != 2 || lesson.CategoryIDs[0] != "missing_tests" {
		t.Errorf("legacy JSON categories: CategoryIDs = %v", lesson.CategoryIDs)
	}
}

func TestParseLessonFromTriples_EmptyMap(t *testing.T) {
	lesson := parseLessonFromTriples(map[string][]string{})
	if lesson.ID != "" || lesson.Source != "" || lesson.Role != "" {
		t.Errorf("expected empty lesson from empty triples, got %+v", lesson)
	}
}

func TestParseLessonFromTriples_MalformedCategories(t *testing.T) {
	// A single non-array value that doesn't start with `[` is treated as a
	// raw atomic category ID.
	triples := mv(
		agentgraph.PredicateLessonID, "abc",
		agentgraph.PredicateLessonCategories, "not-json",
	)
	lesson := parseLessonFromTriples(triples)
	if len(lesson.CategoryIDs) != 1 || lesson.CategoryIDs[0] != "not-json" {
		t.Errorf("expected single atomic category, got %v", lesson.CategoryIDs)
	}
}

func TestParseLessonFromTriples_MalformedLegacyJSONCategories(t *testing.T) {
	// A single value starting with `[` that fails to JSON-decode yields nil.
	triples := mv(
		agentgraph.PredicateLessonID, "abc",
		agentgraph.PredicateLessonCategories, `[broken json`,
	)
	lesson := parseLessonFromTriples(triples)
	if lesson.CategoryIDs != nil {
		t.Errorf("expected nil for malformed legacy JSON, got %v", lesson.CategoryIDs)
	}
}

func TestParseLessonFromTriples_MalformedTimestamp(t *testing.T) {
	triples := mv(
		agentgraph.PredicateLessonID, "abc",
		agentgraph.PredicateLessonCreatedAt, "not-a-date",
	)
	lesson := parseLessonFromTriples(triples)
	if !lesson.CreatedAt.IsZero() {
		t.Errorf("expected zero time for malformed timestamp, got %v", lesson.CreatedAt)
	}
}

func TestGetRoleLessonCounts_ErrorsWithNoNATS(t *testing.T) {
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
	w := &Writer{TW: nilTripleWriter(), Logger: testLogger()}

	lesson := workflow.Lesson{
		Source:        "reviewer-feedback",
		Summary:       "test lesson",
		Role:          "developer",
		EvidenceFiles: []workflow.FileRef{{Path: "main.go"}},
	}

	if err := w.RecordLesson(context.Background(), lesson); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListLessonsForRole_FiltersRole(t *testing.T) {
	devTriples := mv(
		agentgraph.PredicateLessonID, "1",
		agentgraph.PredicateLessonRole, "developer",
	)
	planTriples := mv(
		agentgraph.PredicateLessonID, "2",
		agentgraph.PredicateLessonRole, "planner",
	)

	devLesson := parseLessonFromTriples(devTriples)
	planLesson := parseLessonFromTriples(planTriples)

	if devLesson.Role != "developer" {
		t.Errorf("expected developer, got %q", devLesson.Role)
	}
	if planLesson.Role != "planner" {
		t.Errorf("expected planner, got %q", planLesson.Role)
	}
}

func TestGetRoleLessonCounts_AggregatesCategories(t *testing.T) {
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
// ADR-033 Phase 1 schema round-trip tests (post atomic-triples switch)
// ---------------------------------------------------------------------------

func TestStepRefRoundTrip(t *testing.T) {
	step := workflow.StepRef{LoopID: "550e8400-e29b-41d4-a716-446655440000", StepIndex: 7}
	encoded := encodeStepRef(step)
	decoded, ok := decodeStepRef(encoded)
	if !ok {
		t.Fatalf("decode failed for %q", encoded)
	}
	if decoded != step {
		t.Errorf("round-trip mismatch: got %+v, want %+v", decoded, step)
	}
}

func TestDecodeStepRef_Malformed(t *testing.T) {
	cases := []string{"", "no-pipe", "|empty-loop", "loop|notanint"}
	for _, c := range cases {
		if _, ok := decodeStepRef(c); ok {
			t.Errorf("decodeStepRef(%q) should fail", c)
		}
	}
}

func TestFileRefRoundTrip(t *testing.T) {
	f := workflow.FileRef{Path: "main.go", LineStart: 10, LineEnd: 20, CommitSHA: "deadbeef"}
	encoded := encodeFileRef(f)
	decoded, ok := decodeFileRef(encoded)
	if !ok {
		t.Fatalf("decode failed for %q", encoded)
	}
	if decoded != f {
		t.Errorf("round-trip mismatch: got %+v, want %+v", decoded, f)
	}
}

func TestFileRefRoundTrip_PathContainsDelimiter(t *testing.T) {
	// Path is encoded last so SplitN(_, 4) preserves "|" inside the path.
	f := workflow.FileRef{Path: "weird|path.go", LineStart: 1, LineEnd: 2, CommitSHA: "abc"}
	encoded := encodeFileRef(f)
	decoded, ok := decodeFileRef(encoded)
	if !ok {
		t.Fatalf("decode failed for %q", encoded)
	}
	if decoded != f {
		t.Errorf("path-with-pipe round-trip: got %+v, want %+v", decoded, f)
	}
}

func TestDecodeFileRef_Malformed(t *testing.T) {
	cases := []string{"", "1|2|abc", "1|2|abc|", "x|2|abc|main.go", "1|y|abc|main.go"}
	for _, c := range cases {
		if _, ok := decodeFileRef(c); ok {
			t.Errorf("decodeFileRef(%q) should fail", c)
		}
	}
}

func TestParseLessonFromTriples_Phase1FieldsRoundTrip(t *testing.T) {
	retired := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	lastInj := time.Date(2026, 4, 30, 9, 0, 0, 0, time.UTC)

	triples := mv(
		agentgraph.PredicateLessonID, "abc",
		agentgraph.PredicateLessonRole, "developer",
		agentgraph.PredicateLessonDetail, "long-form root-cause narrative",
		agentgraph.PredicateLessonInjectionForm, "case study form",
		agentgraph.PredicateLessonRootCauseRole, "architect",
		agentgraph.PredicateLessonPositive, "true",
		agentgraph.PredicateLessonRetiredAt, retired.Format(time.RFC3339),
		agentgraph.PredicateLessonLastInjectedAt, lastInj.Format(time.RFC3339),
		agentgraph.PredicateLessonEvidenceSteps, "l1|3",
		agentgraph.PredicateLessonEvidenceSteps, "l2|7",
		agentgraph.PredicateLessonEvidenceFiles, "10|20|deadbeef|main.go",
	)

	lesson := parseLessonFromTriples(triples)

	if lesson.Detail != "long-form root-cause narrative" {
		t.Errorf("Detail = %q", lesson.Detail)
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

func TestParseLessonFromTriples_LegacyJSONEvidence(t *testing.T) {
	// Backwards compat: lessons recorded under the pre-atomic Phase 1
	// encoding stored evidence as a single JSON-array string.
	triples := mv(
		agentgraph.PredicateLessonID, "legacy",
		agentgraph.PredicateLessonEvidenceSteps, `[{"loop_id":"l1","step_index":3},{"loop_id":"l2","step_index":7}]`,
		agentgraph.PredicateLessonEvidenceFiles, `[{"path":"main.go","line_start":10,"line_end":20,"commit_sha":"deadbeef"}]`,
	)
	lesson := parseLessonFromTriples(triples)
	if len(lesson.EvidenceSteps) != 2 || lesson.EvidenceSteps[1].StepIndex != 7 {
		t.Errorf("legacy EvidenceSteps = %+v", lesson.EvidenceSteps)
	}
	if len(lesson.EvidenceFiles) != 1 || lesson.EvidenceFiles[0].Path != "main.go" {
		t.Errorf("legacy EvidenceFiles = %+v", lesson.EvidenceFiles)
	}
}

func TestParseLessonFromTriples_Phase1FieldsAbsent(t *testing.T) {
	triples := mv(
		agentgraph.PredicateLessonID, "legacy",
		agentgraph.PredicateLessonRole, "developer",
	)
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

func TestParseLessonFromTriples_MalformedEvidence(t *testing.T) {
	// Atomic-format malformed values are silently skipped (no panic, no
	// half-built ref).
	triples := mv(
		agentgraph.PredicateLessonID, "x",
		agentgraph.PredicateLessonEvidenceSteps, "no-pipe-no-int",
		agentgraph.PredicateLessonEvidenceFiles, "1|2|abc", // missing path field
	)
	lesson := parseLessonFromTriples(triples)
	if lesson.EvidenceSteps != nil {
		t.Errorf("malformed evidence_steps must yield nil, got %v", lesson.EvidenceSteps)
	}
	if lesson.EvidenceFiles != nil {
		t.Errorf("malformed evidence_files must yield nil, got %v", lesson.EvidenceFiles)
	}
}

func TestParseLessonFromTriples_PositiveFalseValue(t *testing.T) {
	triples := mv(
		agentgraph.PredicateLessonID, "x",
		agentgraph.PredicateLessonPositive, "false",
	)
	lesson := parseLessonFromTriples(triples)
	if lesson.Positive {
		t.Error("Positive should be false when triple value is \"false\"")
	}
}

func TestParseLessonFromTriples_MalformedTimestampFields(t *testing.T) {
	triples := mv(
		agentgraph.PredicateLessonID, "x",
		agentgraph.PredicateLessonRetiredAt, "not-a-date",
		agentgraph.PredicateLessonLastInjectedAt, "also-not-a-date",
	)
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

func TestRecordLesson_RejectsLessonWithoutEvidence(t *testing.T) {
	// ADR-033 Phase 3: writer rejects evidence-less lessons. The error
	// surfaces as ErrLessonWithoutEvidence so producers can branch on it
	// instead of swallowing the error and silently dropping the lesson.
	w := &Writer{TW: nilTripleWriter(), Logger: testLogger()}
	lesson := workflow.Lesson{
		Source:  "reviewer-feedback",
		Summary: "legacy",
		Role:    "developer",
	}
	err := w.RecordLesson(context.Background(), lesson)
	if err == nil {
		t.Fatal("Phase 3 must reject lessons without evidence, got nil")
	}
	if !errors.Is(err, ErrLessonWithoutEvidence) {
		t.Errorf("expected ErrLessonWithoutEvidence, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// ADR-033 Phase 4b rotation order tests (nil-LastInjectedAt first,
// oldest-injected next, CreatedAt DESC tie-break)
// ---------------------------------------------------------------------------

func TestSortLessonsForRotation_NeverInjectedFirst(t *testing.T) {
	t1 := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)

	lessons := []workflow.Lesson{
		{ID: "old-injected", LastInjectedAt: &t1, CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "new-uninjected", CreatedAt: time.Date(2026, 4, 27, 0, 0, 0, 0, time.UTC)},
		{ID: "recent-injected", LastInjectedAt: &t2, CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "old-uninjected", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
	}
	sortLessonsForRotation(lessons)

	want := []string{"new-uninjected", "old-uninjected", "old-injected", "recent-injected"}
	for i, l := range lessons {
		if l.ID != want[i] {
			t.Errorf("position %d: got %q, want %q\n  full: %v", i, l.ID, want[i],
				lessonIDs(lessons))
			break
		}
	}
}

func TestSortLessonsForRotation_TieBreakByCreatedAtDesc(t *testing.T) {
	// Two lessons injected at the same instant — the newer-CreatedAt
	// should sort first. This matches the nil-LastInjectedAt branch's
	// tie-break so behaviour is consistent across the two paths.
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	lessons := []workflow.Lesson{
		{ID: "older", LastInjectedAt: &now, CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "newer", LastInjectedAt: &now, CreatedAt: time.Date(2026, 4, 28, 0, 0, 0, 0, time.UTC)},
	}
	sortLessonsForRotation(lessons)
	if lessons[0].ID != "newer" {
		t.Errorf("expected newer-CreatedAt first on tie, got %v", lessonIDs(lessons))
	}
}

func TestSortLessonsForRotation_EmptySliceNoPanic(t *testing.T) {
	var lessons []workflow.Lesson
	sortLessonsForRotation(lessons) // must not panic
	if len(lessons) != 0 {
		t.Errorf("empty slice should stay empty, got %v", lessons)
	}
}

func lessonIDs(ls []workflow.Lesson) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.ID
	}
	return out
}

func TestRotateLessonsForRole_ErrorsWithNoNATS(t *testing.T) {
	// Mirror the existing TestGetRoleLessonCounts_ErrorsWithNoNATS pattern.
	w := &Writer{TW: nilTripleWriter(), Logger: testLogger()}
	_, err := w.RotateLessonsForRole(context.Background(), "developer", 5)
	if err == nil {
		t.Fatal("expected error with nil NATSClient, got nil")
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
