// Package lessons provides a shared lesson writer that uses TripleWriter
// to persist lessons through the graph pipeline (NATS request-reply to
// graph-ingest). No direct KV bucket access needed — graph-ingest handles
// ENTITY_STATES, eliminating startup race conditions.
package lessons

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/c360studio/semspec/agentgraph"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/google/uuid"
)

// Writer persists and reads lessons via TripleWriter (NATS → graph-ingest).
type Writer struct {
	TW     *graphutil.TripleWriter
	Logger *slog.Logger
}

// ErrLessonWithoutEvidence is returned by RecordLesson when the supplied
// Lesson has neither EvidenceSteps nor EvidenceFiles populated. ADR-033
// Phase 3 makes evidence mandatory so the retirement sweep (Phase 5) has
// something to verify against; producers that cannot supply evidence must
// publish a `lesson.decompose.requested` event and let the decomposer
// produce the lesson with its evidence-citing prompt.
var ErrLessonWithoutEvidence = errors.New("lessons: lesson must cite at least one EvidenceSteps or EvidenceFiles entry (ADR-033 Phase 3)")

// firstRunes returns the first n runes of s, used for prompt-safe log
// snippets when the full summary may exceed slog field limits.
func firstRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// stepRefDelim separates fields inside a StepRef triple value.
// StepRef.LoopID is a UUID and never contains "|".
const stepRefDelim = "|"

// fileRefDelim separates fields inside a FileRef triple value.
// Layout puts Path last so SplitN(_, 4) keeps a "|"-containing path intact.
const fileRefDelim = "|"

// RecordLesson writes a lesson entity to the graph as triples.
//
// ADR-033 Phase 3: every lesson MUST cite at least one trajectory step
// (EvidenceSteps) or file region (EvidenceFiles). The writer rejects
// evidence-less lessons with ErrLessonWithoutEvidence so producers cannot
// silently ship lessons that the retirement sweep (Phase 5) cannot
// validate. All in-tree producers (execution-manager structural,
// plan-reviewer, qa-reviewer, lesson-decomposer) populate evidence; only
// callers that bypass these paths trip this guard.
//
// Multi-valued fields (CategoryIDs, EvidenceSteps, EvidenceFiles) are
// written as one triple per element to keep triples atomic per
// feedback_no_json_in_triples — never json.Marshal into a triple object.
func (w *Writer) RecordLesson(ctx context.Context, lesson workflow.Lesson) error {
	if len(lesson.EvidenceSteps) == 0 && len(lesson.EvidenceFiles) == 0 {
		if w.Logger != nil {
			w.Logger.Warn("Rejected lesson with no evidence (ADR-033 Phase 3)",
				"source", lesson.Source, "role", lesson.Role, "summary_prefix", firstRunes(lesson.Summary, 80))
		}
		return ErrLessonWithoutEvidence
	}
	if lesson.ID == "" {
		lesson.ID = uuid.New().String()
	}
	if lesson.CreatedAt.IsZero() {
		lesson.CreatedAt = time.Now()
	}

	eid := agentgraph.LessonEntityID(lesson.ID)

	type triple struct {
		predicate string
		value     any
	}
	triples := []triple{
		{agentgraph.PredicateLessonID, lesson.ID},
		{agentgraph.PredicateLessonSource, lesson.Source},
		{agentgraph.PredicateLessonScenarioID, lesson.ScenarioID},
		{agentgraph.PredicateLessonSummary, lesson.Summary},
		{agentgraph.PredicateLessonRole, lesson.Role},
		{agentgraph.PredicateLessonCreatedAt, lesson.CreatedAt.Format(time.RFC3339)},
	}

	// One triple per category ID — atomic, no JSON encoding.
	for _, cat := range lesson.CategoryIDs {
		triples = append(triples, triple{agentgraph.PredicateLessonCategories, cat})
	}

	// ADR-033 Phase 1+ optional fields — only written when set, to keep
	// triple count small for legacy lessons.
	if lesson.Detail != "" {
		triples = append(triples, triple{agentgraph.PredicateLessonDetail, lesson.Detail})
	}
	if lesson.InjectionForm != "" {
		triples = append(triples, triple{agentgraph.PredicateLessonInjectionForm, lesson.InjectionForm})
	}
	for _, step := range lesson.EvidenceSteps {
		triples = append(triples, triple{agentgraph.PredicateLessonEvidenceSteps, encodeStepRef(step)})
	}
	for _, file := range lesson.EvidenceFiles {
		triples = append(triples, triple{agentgraph.PredicateLessonEvidenceFiles, encodeFileRef(file)})
	}
	if lesson.RootCauseRole != "" {
		triples = append(triples, triple{agentgraph.PredicateLessonRootCauseRole, lesson.RootCauseRole})
	}
	if lesson.Positive {
		triples = append(triples, triple{agentgraph.PredicateLessonPositive, "true"})
	}
	if lesson.RetiredAt != nil {
		triples = append(triples, triple{agentgraph.PredicateLessonRetiredAt, lesson.RetiredAt.Format(time.RFC3339)})
	}
	if lesson.LastInjectedAt != nil {
		triples = append(triples, triple{agentgraph.PredicateLessonLastInjectedAt, lesson.LastInjectedAt.Format(time.RFC3339)})
	}

	for _, t := range triples {
		if err := w.TW.WriteTriple(ctx, eid, t.predicate, t.value); err != nil {
			return fmt.Errorf("lessons: write triple %s: %w", t.predicate, err)
		}
	}

	return nil
}

// encodeStepRef serialises a StepRef as `<loop_id>|<step_index>`.
func encodeStepRef(s workflow.StepRef) string {
	return s.LoopID + stepRefDelim + strconv.Itoa(s.StepIndex)
}

// decodeStepRef parses `<loop_id>|<step_index>`. Returns (zero, false) on
// malformed input.
func decodeStepRef(raw string) (workflow.StepRef, bool) {
	parts := strings.SplitN(raw, stepRefDelim, 2)
	if len(parts) != 2 || parts[0] == "" {
		return workflow.StepRef{}, false
	}
	idx, err := strconv.Atoi(parts[1])
	if err != nil {
		return workflow.StepRef{}, false
	}
	return workflow.StepRef{LoopID: parts[0], StepIndex: idx}, true
}

// encodeFileRef serialises a FileRef as
// `<line_start>|<line_end>|<commit_sha>|<path>`. Path is last so a path
// containing the delimiter survives SplitN(_, 4).
func encodeFileRef(f workflow.FileRef) string {
	return strconv.Itoa(f.LineStart) + fileRefDelim +
		strconv.Itoa(f.LineEnd) + fileRefDelim +
		f.CommitSHA + fileRefDelim +
		f.Path
}

// decodeFileRef parses `<line_start>|<line_end>|<commit_sha>|<path>`.
// Returns (zero, false) on malformed input.
func decodeFileRef(raw string) (workflow.FileRef, bool) {
	parts := strings.SplitN(raw, fileRefDelim, 4)
	if len(parts) != 4 || parts[3] == "" {
		return workflow.FileRef{}, false
	}
	lineStart, err := strconv.Atoi(parts[0])
	if err != nil {
		return workflow.FileRef{}, false
	}
	lineEnd, err := strconv.Atoi(parts[1])
	if err != nil {
		return workflow.FileRef{}, false
	}
	return workflow.FileRef{
		LineStart: lineStart,
		LineEnd:   lineEnd,
		CommitSHA: parts[2],
		Path:      parts[3],
	}, true
}

// ListLessonsForRole reads all lesson entities from the graph via prefix scan
// and filters by role. Results are sorted most-recent-first. This is the
// read-only path — for prompt-injection selection that ages lessons via
// LastInjectedAt, callers should use RotateLessonsForRole instead
// (ADR-033 Phase 4b).
func (w *Writer) ListLessonsForRole(ctx context.Context, role string, limit int) ([]workflow.Lesson, error) {
	prefix := agentgraph.LessonTypePrefix()
	entities, err := w.TW.ReadEntitiesByPrefixMulti(ctx, prefix, 200)
	if err != nil {
		return nil, fmt.Errorf("lessons: list by prefix: %w", err)
	}

	var lessons []workflow.Lesson
	for _, triples := range entities {
		lesson := parseLessonFromTriples(triples)
		if role != "" && lesson.Role != role {
			continue
		}
		lessons = append(lessons, lesson)
	}

	sort.Slice(lessons, func(i, j int) bool {
		return lessons[i].CreatedAt.After(lessons[j].CreatedAt)
	})

	if limit > 0 && len(lessons) > limit {
		return lessons[:limit], nil
	}
	return lessons, nil
}

// RotateLessonsForRole selects the next batch of lessons to inject into a
// prompt, rotating which ones surface via LastInjectedAt (ADR-033 Phase 4b).
// Sort order:
//
//  1. Lessons that have never been injected (nil LastInjectedAt) first,
//     newest-CreatedAt-first within that group.
//  2. Then lessons whose LastInjectedAt is oldest, with CreatedAt DESC as
//     the tie-break.
//
// After selection, LastInjectedAt is bumped to time.Now() for each selected
// lesson via a best-effort triple write — if the bump fails the rotation
// degrades to the existing recency behavior (same lessons resurface), but
// the caller still receives a valid selection.
//
// Retired lessons (RetiredAt non-nil) are skipped entirely — Phase 5's
// retirement sweep marks lessons as retired when their evidence becomes
// stale, and we must not inject them even if they'd otherwise sort first.
func (w *Writer) RotateLessonsForRole(ctx context.Context, role string, limit int) ([]workflow.Lesson, error) {
	prefix := agentgraph.LessonTypePrefix()
	entities, err := w.TW.ReadEntitiesByPrefixMulti(ctx, prefix, 200)
	if err != nil {
		return nil, fmt.Errorf("lessons: rotate by prefix: %w", err)
	}

	var pool []workflow.Lesson
	for _, triples := range entities {
		lesson := parseLessonFromTriples(triples)
		if role != "" && lesson.Role != role {
			continue
		}
		if lesson.RetiredAt != nil {
			continue
		}
		pool = append(pool, lesson)
	}

	sortLessonsForRotation(pool)

	selected := pool
	if limit > 0 && len(pool) > limit {
		selected = pool[:limit]
	}

	now := time.Now()
	for i := range selected {
		eid := agentgraph.LessonEntityID(selected[i].ID)
		if err := w.TW.WriteTriple(ctx, eid, agentgraph.PredicateLessonLastInjectedAt, now.Format(time.RFC3339)); err != nil {
			if w.Logger != nil {
				w.Logger.Debug("Failed to bump LastInjectedAt — rotation degrades to recency",
					"lesson_id", selected[i].ID, "error", err)
			}
			continue
		}
		// Mirror the bump locally so the returned slice reflects what the
		// graph now holds. Avoids the next caller seeing stale values when
		// they read back from the graph milliseconds later.
		t := now
		selected[i].LastInjectedAt = &t
	}

	return selected, nil
}

// RetireLesson marks a lesson as no longer eligible for injection by
// writing the lesson.retired_at predicate. ADR-033 Phase 5: the
// lesson-curator component calls this when a lesson's evidence has
// gone stale (cited file deleted, code substantially rewritten, or the
// lesson hasn't been injected in N weeks).
//
// Retired lessons remain in the graph for audit; RotateLessonsForRole
// skips them. Reason is included in logs for traceability and is
// optional — pass "" when no specific reason is meaningful.
func (w *Writer) RetireLesson(ctx context.Context, lessonID, reason string) error {
	if lessonID == "" {
		return fmt.Errorf("lessons: retire requires lesson ID")
	}
	if w.TW == nil {
		return fmt.Errorf("lessons: retire requires TripleWriter")
	}
	eid := agentgraph.LessonEntityID(lessonID)
	now := time.Now().Format(time.RFC3339)
	if err := w.TW.WriteTriple(ctx, eid, agentgraph.PredicateLessonRetiredAt, now); err != nil {
		return fmt.Errorf("lessons: write retired_at: %w", err)
	}
	if w.Logger != nil {
		w.Logger.Info("Lesson retired", "lesson_id", lessonID, "reason", reason)
	}
	return nil
}

// IncrementRoleLessonCounts is a no-op retained for API compatibility.
// Counts are now computed from the lesson list at read time via
// GetRoleLessonCounts, eliminating the read-then-write race condition.
func (w *Writer) IncrementRoleLessonCounts(_ context.Context, _ string, _ []string) error {
	return nil
}

// GetRoleLessonCounts computes per-category occurrence counts by scanning
// all lessons for the given role. No separate counter entity needed —
// this eliminates the read-then-write race that existed with stored counters.
func (w *Writer) GetRoleLessonCounts(ctx context.Context, role string) (workflow.RoleLessonCounts, error) {
	lessons, err := w.ListLessonsForRole(ctx, role, 0)
	if err != nil {
		return workflow.RoleLessonCounts{}, err
	}

	var counts workflow.RoleLessonCounts
	for _, lesson := range lessons {
		if len(lesson.CategoryIDs) > 0 {
			counts.Increment(lesson.CategoryIDs)
		}
	}
	return counts, nil
}

// parseLessonFromTriples reconstructs a Lesson from a predicate→[]values map.
// Single-valued predicates take the first observed value; multi-valued
// predicates (CategoryIDs, EvidenceSteps, EvidenceFiles) take all values.
//
// Backwards compat: lessons recorded before the atomic-triples switch stored
// CategoryIDs as a single JSON-array string. If we see exactly one value
// prefixed with `[`, fall back to JSON decoding.
func parseLessonFromTriples(triples map[string][]string) workflow.Lesson {
	var lesson workflow.Lesson
	lesson.ID = first(triples[agentgraph.PredicateLessonID])
	lesson.Source = first(triples[agentgraph.PredicateLessonSource])
	lesson.ScenarioID = first(triples[agentgraph.PredicateLessonScenarioID])
	lesson.Summary = first(triples[agentgraph.PredicateLessonSummary])
	lesson.Role = first(triples[agentgraph.PredicateLessonRole])
	lesson.Detail = first(triples[agentgraph.PredicateLessonDetail])
	lesson.InjectionForm = first(triples[agentgraph.PredicateLessonInjectionForm])
	lesson.RootCauseRole = first(triples[agentgraph.PredicateLessonRootCauseRole])

	lesson.CategoryIDs = decodeCategoryIDs(triples[agentgraph.PredicateLessonCategories])

	if raw := first(triples[agentgraph.PredicateLessonCreatedAt]); raw != "" {
		lesson.CreatedAt, _ = time.Parse(time.RFC3339, raw)
	}

	for _, raw := range triples[agentgraph.PredicateLessonEvidenceSteps] {
		// Backwards compat: legacy Phase 1 wrote a single JSON-array string.
		if strings.HasPrefix(raw, "[") {
			var legacy []workflow.StepRef
			if err := json.Unmarshal([]byte(raw), &legacy); err == nil {
				lesson.EvidenceSteps = append(lesson.EvidenceSteps, legacy...)
				continue
			}
		}
		if step, ok := decodeStepRef(raw); ok {
			lesson.EvidenceSteps = append(lesson.EvidenceSteps, step)
		}
	}

	for _, raw := range triples[agentgraph.PredicateLessonEvidenceFiles] {
		if strings.HasPrefix(raw, "[") {
			var legacy []workflow.FileRef
			if err := json.Unmarshal([]byte(raw), &legacy); err == nil {
				lesson.EvidenceFiles = append(lesson.EvidenceFiles, legacy...)
				continue
			}
		}
		if file, ok := decodeFileRef(raw); ok {
			lesson.EvidenceFiles = append(lesson.EvidenceFiles, file)
		}
	}

	if raw := first(triples[agentgraph.PredicateLessonPositive]); raw != "" {
		lesson.Positive = raw == "true"
	}
	if raw := first(triples[agentgraph.PredicateLessonRetiredAt]); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			lesson.RetiredAt = &t
		}
	}
	if raw := first(triples[agentgraph.PredicateLessonLastInjectedAt]); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			lesson.LastInjectedAt = &t
		}
	}

	return lesson
}

// decodeCategoryIDs prefers atomic-triple values (one ID per triple). For
// legacy lessons that stored a single JSON-array string, falls back to JSON
// decode.
func decodeCategoryIDs(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	if len(values) == 1 && strings.HasPrefix(values[0], "[") {
		var legacy []string
		if err := json.Unmarshal([]byte(values[0]), &legacy); err == nil {
			return legacy
		}
		return nil
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	return out
}

// sortLessonsForRotation orders a lesson slice for ADR-033 Phase 4b
// rotation. Order:
//
//  1. Lessons that have never been injected (nil LastInjectedAt) come
//     first, newest-CreatedAt-first within that group.
//  2. Then lessons whose LastInjectedAt is oldest, with CreatedAt DESC
//     as the tie-break.
//
// Extracted so the rotation order is testable without a NATS round-trip.
func sortLessonsForRotation(lessons []workflow.Lesson) {
	sort.Slice(lessons, func(i, j int) bool {
		ai, aj := lessons[i].LastInjectedAt, lessons[j].LastInjectedAt
		switch {
		case ai == nil && aj != nil:
			return true
		case ai != nil && aj == nil:
			return false
		case ai == nil && aj == nil:
			return lessons[i].CreatedAt.After(lessons[j].CreatedAt)
		default:
			if ai.Equal(*aj) {
				return lessons[i].CreatedAt.After(lessons[j].CreatedAt)
			}
			return ai.Before(*aj)
		}
	})
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
