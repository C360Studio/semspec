// Package lessons provides a shared lesson writer that uses TripleWriter
// to persist lessons through the graph pipeline (NATS request-reply to
// graph-ingest). No direct KV bucket access needed — graph-ingest handles
// ENTITY_STATES, eliminating startup race conditions.
package lessons

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
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

// RecordLesson writes a lesson entity to the graph as triples.
//
// ADR-033 Phase 1: writer accepts lessons with or without evidence pointers.
// When evidence is missing, a Debug log is emitted (warn-only-but-quiet
// during the migration window — most legacy lessons lack evidence and would
// flood the warn channel). Phase 3 will flip this to a hard reject.
func (w *Writer) RecordLesson(ctx context.Context, lesson workflow.Lesson) error {
	if lesson.ID == "" {
		lesson.ID = uuid.New().String()
	}
	if lesson.CreatedAt.IsZero() {
		lesson.CreatedAt = time.Now()
	}

	eid := agentgraph.LessonEntityID(lesson.ID)

	categoriesJSON, err := json.Marshal(lesson.CategoryIDs)
	if err != nil {
		return fmt.Errorf("lessons: marshal categories: %w", err)
	}

	// Write each field as a triple. TripleWriter handles NATS request-reply
	// to graph-ingest which does the CAS write to ENTITY_STATES.
	triples := []struct {
		predicate string
		value     any
	}{
		{agentgraph.PredicateLessonID, lesson.ID},
		{agentgraph.PredicateLessonSource, lesson.Source},
		{agentgraph.PredicateLessonScenarioID, lesson.ScenarioID},
		{agentgraph.PredicateLessonSummary, lesson.Summary},
		{agentgraph.PredicateLessonCategories, string(categoriesJSON)},
		{agentgraph.PredicateLessonRole, lesson.Role},
		{agentgraph.PredicateLessonCreatedAt, lesson.CreatedAt.Format(time.RFC3339)},
	}

	// ADR-033 Phase 1+ optional fields — only written when set, to keep
	// triple count small for legacy lessons.
	if lesson.Detail != "" {
		triples = append(triples, struct {
			predicate string
			value     any
		}{agentgraph.PredicateLessonDetail, lesson.Detail})
	}
	if lesson.InjectionForm != "" {
		triples = append(triples, struct {
			predicate string
			value     any
		}{agentgraph.PredicateLessonInjectionForm, lesson.InjectionForm})
	}
	if len(lesson.EvidenceSteps) > 0 {
		stepsJSON, err := json.Marshal(lesson.EvidenceSteps)
		if err != nil {
			return fmt.Errorf("lessons: marshal evidence_steps: %w", err)
		}
		triples = append(triples, struct {
			predicate string
			value     any
		}{agentgraph.PredicateLessonEvidenceSteps, string(stepsJSON)})
	}
	if len(lesson.EvidenceFiles) > 0 {
		filesJSON, err := json.Marshal(lesson.EvidenceFiles)
		if err != nil {
			return fmt.Errorf("lessons: marshal evidence_files: %w", err)
		}
		triples = append(triples, struct {
			predicate string
			value     any
		}{agentgraph.PredicateLessonEvidenceFiles, string(filesJSON)})
	}
	if lesson.RootCauseRole != "" {
		triples = append(triples, struct {
			predicate string
			value     any
		}{agentgraph.PredicateLessonRootCauseRole, lesson.RootCauseRole})
	}
	if lesson.Positive {
		triples = append(triples, struct {
			predicate string
			value     any
		}{agentgraph.PredicateLessonPositive, "true"})
	}
	if lesson.RetiredAt != nil {
		triples = append(triples, struct {
			predicate string
			value     any
		}{agentgraph.PredicateLessonRetiredAt, lesson.RetiredAt.Format(time.RFC3339)})
	}
	if lesson.LastInjectedAt != nil {
		triples = append(triples, struct {
			predicate string
			value     any
		}{agentgraph.PredicateLessonLastInjectedAt, lesson.LastInjectedAt.Format(time.RFC3339)})
	}

	if len(lesson.EvidenceSteps) == 0 && len(lesson.EvidenceFiles) == 0 && w.Logger != nil {
		w.Logger.Debug("Lesson recorded without evidence pointers (Phase 1 warn-only)",
			"lesson_id", lesson.ID, "source", lesson.Source, "role", lesson.Role)
	}

	for _, t := range triples {
		if err := w.TW.WriteTriple(ctx, eid, t.predicate, t.value); err != nil {
			return fmt.Errorf("lessons: write triple %s: %w", t.predicate, err)
		}
	}

	return nil
}

// ListLessonsForRole reads all lesson entities from the graph via prefix scan
// and filters by role. Results are sorted most-recent-first.
func (w *Writer) ListLessonsForRole(ctx context.Context, role string, limit int) ([]workflow.Lesson, error) {
	prefix := agentgraph.LessonTypePrefix()
	entities, err := w.TW.ReadEntitiesByPrefix(ctx, prefix, 200)
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

// parseLessonFromTriples reconstructs a Lesson from a predicate→value map.
// ADR-033 Phase 1+ fields are optional — missing predicates leave the
// corresponding Lesson fields at their zero value.
func parseLessonFromTriples(triples map[string]string) workflow.Lesson {
	var lesson workflow.Lesson
	lesson.ID = triples[agentgraph.PredicateLessonID]
	lesson.Source = triples[agentgraph.PredicateLessonSource]
	lesson.ScenarioID = triples[agentgraph.PredicateLessonScenarioID]
	lesson.Summary = triples[agentgraph.PredicateLessonSummary]
	lesson.Role = triples[agentgraph.PredicateLessonRole]
	lesson.Detail = triples[agentgraph.PredicateLessonDetail]
	lesson.InjectionForm = triples[agentgraph.PredicateLessonInjectionForm]
	lesson.RootCauseRole = triples[agentgraph.PredicateLessonRootCauseRole]

	if raw, ok := triples[agentgraph.PredicateLessonCategories]; ok {
		_ = json.Unmarshal([]byte(raw), &lesson.CategoryIDs)
	}
	if raw, ok := triples[agentgraph.PredicateLessonCreatedAt]; ok {
		lesson.CreatedAt, _ = time.Parse(time.RFC3339, raw)
	}
	if raw, ok := triples[agentgraph.PredicateLessonEvidenceSteps]; ok {
		_ = json.Unmarshal([]byte(raw), &lesson.EvidenceSteps)
	}
	if raw, ok := triples[agentgraph.PredicateLessonEvidenceFiles]; ok {
		_ = json.Unmarshal([]byte(raw), &lesson.EvidenceFiles)
	}
	if raw, ok := triples[agentgraph.PredicateLessonPositive]; ok {
		lesson.Positive = raw == "true"
	}
	if raw, ok := triples[agentgraph.PredicateLessonRetiredAt]; ok && raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			lesson.RetiredAt = &t
		}
	}
	if raw, ok := triples[agentgraph.PredicateLessonLastInjectedAt]; ok && raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			lesson.LastInjectedAt = &t
		}
	}

	return lesson
}
