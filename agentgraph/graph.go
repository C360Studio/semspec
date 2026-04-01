package agentgraph

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/types"
	"github.com/google/uuid"

	"github.com/c360studio/semspec/workflow"
)

// Agentic relationship and property predicates for graph triples.
//
// These predicate strings must match the constants in vocabulary/semspec/predicates.go,
// which registers them with the vocabulary system via init().
const (
	// PredicateSpawned records a parent loop spawning a child loop.
	// Direction: parent loop entity -> child loop entity.
	PredicateSpawned = "agentic.loop.spawned"

	// PredicateLoopTask records the association between a loop and a task it owns.
	// Direction: loop entity -> task entity.
	PredicateLoopTask = "agentic.loop.task"

	// PredicateDependsOn records a task-to-task dependency (DAG edge).
	// Direction: dependent task entity -> prerequisite task entity.
	PredicateDependsOn = "agentic.task.depends_on"

	// PredicateRole records the functional role of a loop (e.g., "planner", "executor").
	PredicateRole = "agentic.loop.role"

	// PredicateModel records the LLM model identifier used by a loop.
	PredicateModel = "agentic.loop.model"

	// PredicateStatus records the current lifecycle status of a loop.
	PredicateStatus = "agentic.loop.status"

	// Error category predicates.
	PredicateErrorCategoryID          = "error.category.id"
	PredicateErrorCategoryLabel       = "error.category.label"
	PredicateErrorCategoryDescription = "error.category.description"
	PredicateErrorCategorySignal      = "error.category.signal"
	PredicateErrorCategoryGuidance    = "error.category.guidance"

	// Lesson entity predicates.
	PredicateLessonID         = "lesson.id"
	PredicateLessonSource     = "lesson.source"
	PredicateLessonScenarioID = "lesson.scenario_id"
	PredicateLessonSummary    = "lesson.summary"
	PredicateLessonCategories = "lesson.categories"
	PredicateLessonRole       = "lesson.role"
	PredicateLessonCreatedAt  = "lesson.created_at"
	PredicateLessonCounts     = "lesson.counts"

)

// KVStore defines the KV operations used by the agent graph helper.
// *natsclient.KVStore satisfies this interface directly — no adapter needed.
type KVStore interface {
	Get(ctx context.Context, key string) (*natsclient.KVEntry, error)
	Put(ctx context.Context, key string, value []byte) (uint64, error)
	UpdateWithRetry(ctx context.Context, key string, updateFn func(current []byte) ([]byte, error)) error
	KeysByPrefix(ctx context.Context, prefix string) ([]string, error)
}

// Helper provides graph operations for agent hierarchy tracking.
// It is a thin façade over KVStore that speaks in agent-domain terms
// (loop IDs, task IDs) rather than raw entity keys.
//
// All methods are safe for concurrent use — they delegate directly to the
// underlying KV store without holding additional state.
type Helper struct {
	kv KVStore
}

// NewHelper constructs a Helper backed by a KVStore.
// The argument is required; passing nil will cause panics at call time.
func NewHelper(kv KVStore) *Helper {
	return &Helper{kv: kv}
}

// RecordLoopCreated creates a graph entity for a newly-started loop and attaches
// property triples for role, model, and initial status.
// It is idempotent: if the entity already exists it will be overwritten via Put.
func (h *Helper) RecordLoopCreated(ctx context.Context, loopID, role, model string) error {
	entityID := LoopEntityID(loopID)
	now := time.Now()

	triples := []message.Triple{
		propertyTriple(entityID, PredicateRole, role, now),
		propertyTriple(entityID, PredicateModel, model, now),
		propertyTriple(entityID, PredicateStatus, "created", now),
	}

	data, err := marshalEntityState(entityID, triples, message.Type{Domain: DomainAgent, Category: TypeLoop, Version: "v1"})
	if err != nil {
		return fmt.Errorf("agentgraph: marshal loop %q: %w", loopID, err)
	}

	if _, err := h.kv.Put(ctx, entityID, data); err != nil {
		return fmt.Errorf("agentgraph: record loop created %q: %w", loopID, err)
	}
	return nil
}

// RecordSpawn creates the child loop entity (with role and model) and then
// adds a PredicateSpawned triple to the parent entity pointing to the child.
// Both operations must succeed; a failure in either step returns an error.
func (h *Helper) RecordSpawn(ctx context.Context, parentLoopID, childLoopID, role, model string) error {
	if err := h.RecordLoopCreated(ctx, childLoopID, role, model); err != nil {
		return fmt.Errorf("agentgraph: record spawn — child entity: %w", err)
	}

	parentEntityID := LoopEntityID(parentLoopID)
	childEntityID := LoopEntityID(childLoopID)

	// Add a PredicateSpawned triple to the parent entity atomically.
	err := h.kv.UpdateWithRetry(ctx, parentEntityID, func(current []byte) ([]byte, error) {
		var entity *gtypes.EntityState
		if len(current) == 0 {
			// Parent doesn't exist yet — create a minimal entity.
			entity = &gtypes.EntityState{
				ID:          parentEntityID,
				MessageType: message.Type{Domain: DomainAgent, Category: TypeLoop, Version: "v1"},
				UpdatedAt:   time.Now(),
			}
		} else {
			var unmarshalErr error
			entity, unmarshalErr = unmarshalEntityState(current)
			if unmarshalErr != nil {
				return nil, fmt.Errorf("agentgraph: record spawn — corrupt parent entity %q: %w",
					parentLoopID, unmarshalErr)
			}
		}

		entity.Triples = append(entity.Triples,
			propertyTriple(parentEntityID, PredicateSpawned, childEntityID, time.Now()),
		)
		entity.UpdatedAt = time.Now()
		return json.Marshal(entity)
	})
	if err != nil {
		return fmt.Errorf("agentgraph: record spawn — relationship %q -> %q: %w",
			parentLoopID, childLoopID, err)
	}
	return nil
}

// RecordLoopStatus updates the status property triple on an existing loop entity.
// It uses UpdateWithRetry for atomic CAS.
func (h *Helper) RecordLoopStatus(ctx context.Context, loopID, status string) error {
	entityID := LoopEntityID(loopID)

	err := h.kv.UpdateWithRetry(ctx, entityID, func(current []byte) ([]byte, error) {
		entity, unmarshalErr := unmarshalEntityState(current)
		if unmarshalErr != nil {
			return nil, fmt.Errorf("agentgraph: record loop status — get entity %q: %w", loopID, unmarshalErr)
		}

		now := time.Now()
		entity.Triples = replaceTriple(entity.Triples, PredicateStatus,
			propertyTriple(entityID, PredicateStatus, status, now))
		entity.UpdatedAt = now
		return json.Marshal(entity)
	})
	if err != nil {
		return fmt.Errorf("agentgraph: record loop status %q -> %q: %w", loopID, status, err)
	}
	return nil
}

// GetChildEntityIDs returns the entity IDs of all direct children of the given loop.
// It reads the parent entity and scans triples for PredicateSpawned.
// Returns full entity IDs (not parsed instances) to avoid double-hashing.
func (h *Helper) GetChildEntityIDs(ctx context.Context, loopID string) ([]string, error) {
	entityID := LoopEntityID(loopID)

	entry, err := h.kv.Get(ctx, entityID)
	if err != nil {
		return nil, fmt.Errorf("agentgraph: get children of %q: %w", loopID, err)
	}

	entity, err := unmarshalEntityState(entry.Value)
	if err != nil {
		return nil, fmt.Errorf("agentgraph: get children — unmarshal %q: %w", loopID, err)
	}

	var children []string
	for _, t := range entity.Triples {
		if t.Predicate == PredicateSpawned {
			if childEntityID, ok := t.Object.(string); ok {
				if _, parseErr := types.ParseEntityID(childEntityID); parseErr != nil {
					continue // skip malformed
				}
				children = append(children, childEntityID)
			}
		}
	}
	return children, nil
}

// GetTree returns the entity IDs of all loop entities reachable from rootLoopID
// by following PredicateSpawned edges up to maxDepth hops via BFS.
// The root entity itself is included in the result.
func (h *Helper) GetTree(ctx context.Context, rootLoopID string, maxDepth int) ([]string, error) {
	rootEntityID := LoopEntityID(rootLoopID)

	// BFS traversal using entity IDs directly (no double-hashing).
	visited := map[string]bool{rootEntityID: true}
	result := []string{rootEntityID}
	currentLevel := []string{rootEntityID}

	for depth := 0; depth < maxDepth && len(currentLevel) > 0; depth++ {
		var nextLevel []string
		for _, eid := range currentLevel {
			// Read entity directly by entity ID (already hashed).
			entry, err := h.kv.Get(ctx, eid)
			if err != nil {
				continue
			}
			entity, err := unmarshalEntityState(entry.Value)
			if err != nil {
				continue
			}
			for _, t := range entity.Triples {
				if t.Predicate == PredicateSpawned {
					if childEntityID, ok := t.Object.(string); ok {
						if _, parseErr := types.ParseEntityID(childEntityID); parseErr != nil {
							continue
						}
						if !visited[childEntityID] {
							visited[childEntityID] = true
							result = append(result, childEntityID)
							nextLevel = append(nextLevel, childEntityID)
						}
					}
				}
			}
		}
		currentLevel = nextLevel
	}

	return result, nil
}

// GetStatus returns the current status value stored on a loop entity's
// PredicateStatus triple. If the entity exists but carries no status triple,
// an empty string is returned without error.
func (h *Helper) GetStatus(ctx context.Context, loopID string) (string, error) {
	entityID := LoopEntityID(loopID)

	entry, err := h.kv.Get(ctx, entityID)
	if err != nil {
		return "", fmt.Errorf("agentgraph: get status — get entity %q: %w", loopID, err)
	}

	entity, err := unmarshalEntityState(entry.Value)
	if err != nil {
		return "", fmt.Errorf("agentgraph: get status — unmarshal %q: %w", loopID, err)
	}

	for _, t := range entity.Triples {
		if t.Predicate == PredicateStatus {
			if s, ok := t.Object.(string); ok {
				return s, nil
			}
		}
	}
	return "", nil
}

// SeedErrorCategories writes each error category definition as a graph entity.
// The operation is idempotent: re-seeding the same category IDs will update
// the existing entities via Put rather than creating duplicates.
func (h *Helper) SeedErrorCategories(ctx context.Context, categories []*workflow.ErrorCategoryDef) error {
	now := time.Now()

	for _, cat := range categories {
		entityID := ErrorCategoryEntityID(cat.ID)

		triples := []message.Triple{
			propertyTriple(entityID, PredicateErrorCategoryID, cat.ID, now),
			propertyTriple(entityID, PredicateErrorCategoryLabel, cat.Label, now),
			propertyTriple(entityID, PredicateErrorCategoryDescription, cat.Description, now),
			propertyTriple(entityID, PredicateErrorCategoryGuidance, cat.Guidance, now),
		}
		for _, signal := range cat.Signals {
			triples = append(triples, propertyTriple(entityID, PredicateErrorCategorySignal, signal, now))
		}

		data, err := marshalEntityState(entityID, triples, message.Type{Domain: DomainAgent, Category: TypeErrorCategory, Version: "v1"})
		if err != nil {
			return fmt.Errorf("agentgraph: marshal error category %q: %w", cat.ID, err)
		}

		if _, err := h.kv.Put(ctx, entityID, data); err != nil {
			return fmt.Errorf("agentgraph: seed error category %q: %w", cat.ID, err)
		}
	}
	return nil
}

// marshalEntityState builds a graph.EntityState and marshals it to JSON.
func marshalEntityState(id string, triples []message.Triple, msgType message.Type) ([]byte, error) {
	entity := &gtypes.EntityState{
		ID:          id,
		Triples:     triples,
		MessageType: msgType,
		UpdatedAt:   time.Now(),
	}
	return json.Marshal(entity)
}

// unmarshalEntityState deserializes JSON into a graph.EntityState.
// Returns an error if data is nil or empty.
func unmarshalEntityState(data []byte) (*gtypes.EntityState, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("unmarshal entity: empty data")
	}
	var entity gtypes.EntityState
	if err := json.Unmarshal(data, &entity); err != nil {
		return nil, fmt.Errorf("unmarshal entity: %w", err)
	}
	return &entity, nil
}

// replaceTriple replaces the first triple matching predicate, or appends if not found.
func replaceTriple(triples []message.Triple, predicate string, replacement message.Triple) []message.Triple {
	for i, t := range triples {
		if t.Predicate == predicate {
			triples[i] = replacement
			return triples
		}
	}
	return append(triples, replacement)
}

// propertyTriple constructs a property triple for a loop entity.
// Confidence is set to 1.0 because the values come directly from authoritative
// Semspec internal state rather than inferred or sensor data.
func propertyTriple(subject, predicate string, value any, ts time.Time) message.Triple {
	return message.Triple{
		Subject:    subject,
		Predicate:  predicate,
		Object:     value,
		Source:     SourceSemspec,
		Timestamp:  ts,
		Confidence: 1.0,
	}
}

// ---------------------------------------------------------------------------
// Lesson CRUD — role-scoped lessons learned
// ---------------------------------------------------------------------------

// RecordLesson writes a lesson entity to the graph. Each lesson is stored as
// its own entity so they can be enumerated independently.
func (h *Helper) RecordLesson(ctx context.Context, lesson workflow.Lesson) error {
	if lesson.ID == "" {
		lesson.ID = uuid.New().String()
	}
	if lesson.CreatedAt.IsZero() {
		lesson.CreatedAt = time.Now()
	}

	eid := LessonEntityID(lesson.ID)
	now := lesson.CreatedAt

	categoriesJSON, err := json.Marshal(lesson.CategoryIDs)
	if err != nil {
		return fmt.Errorf("agentgraph: marshal lesson categories: %w", err)
	}

	triples := []message.Triple{
		propertyTriple(eid, PredicateLessonID, lesson.ID, now),
		propertyTriple(eid, PredicateLessonSource, lesson.Source, now),
		propertyTriple(eid, PredicateLessonScenarioID, lesson.ScenarioID, now),
		propertyTriple(eid, PredicateLessonSummary, lesson.Summary, now),
		propertyTriple(eid, PredicateLessonCategories, string(categoriesJSON), now),
		propertyTriple(eid, PredicateLessonRole, lesson.Role, now),
		propertyTriple(eid, PredicateLessonCreatedAt, now.Format(time.RFC3339), now),
	}

	data, err := marshalEntityState(eid, triples, message.Type{Domain: DomainAgent, Category: TypeLesson, Version: "v1"})
	if err != nil {
		return fmt.Errorf("agentgraph: marshal lesson %q: %w", lesson.ID, err)
	}

	if _, err := h.kv.Put(ctx, eid, data); err != nil {
		return fmt.Errorf("agentgraph: record lesson %q: %w", lesson.ID, err)
	}
	return nil
}

// ListLessonsForRole returns all lessons matching the given role, up to limit.
// If role is empty, all lessons are returned. Results are sorted most-recent-first.
func (h *Helper) ListLessonsForRole(ctx context.Context, role string, limit int) ([]workflow.Lesson, error) {
	keys, err := h.kv.KeysByPrefix(ctx, LessonTypePrefix())
	if err != nil {
		return nil, fmt.Errorf("agentgraph: list lessons: %w", err)
	}

	var lessons []workflow.Lesson
	for _, key := range keys {
		entry, err := h.kv.Get(ctx, key)
		if err != nil {
			continue
		}
		lesson, err := parseLessonFromEntity(entry.Value)
		if err != nil {
			continue
		}
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

// parseLessonFromEntity extracts a Lesson from a serialised entity state.
func parseLessonFromEntity(data []byte) (workflow.Lesson, error) {
	entity, err := unmarshalEntityState(data)
	if err != nil {
		return workflow.Lesson{}, err
	}

	var lesson workflow.Lesson
	for _, t := range entity.Triples {
		v, _ := t.Object.(string)
		switch t.Predicate {
		case PredicateLessonID:
			lesson.ID = v
		case PredicateLessonSource:
			lesson.Source = v
		case PredicateLessonScenarioID:
			lesson.ScenarioID = v
		case PredicateLessonSummary:
			lesson.Summary = v
		case PredicateLessonCategories:
			_ = json.Unmarshal([]byte(v), &lesson.CategoryIDs)
		case PredicateLessonRole:
			lesson.Role = v
		case PredicateLessonCreatedAt:
			lesson.CreatedAt, _ = time.Parse(time.RFC3339, v)
		}
	}
	return lesson, nil
}

// IncrementRoleLessonCounts atomically increments the per-category occurrence
// counts for the given role. Creates the counts entity if it doesn't exist.
func (h *Helper) IncrementRoleLessonCounts(ctx context.Context, role string, categoryIDs []string) error {
	if len(categoryIDs) == 0 {
		return nil
	}
	eid := RoleLessonCountsEntityID(role)

	err := h.kv.UpdateWithRetry(ctx, eid, func(current []byte) ([]byte, error) {
		var counts workflow.RoleLessonCounts
		if len(current) > 0 {
			entity, unmarshalErr := unmarshalEntityState(current)
			if unmarshalErr != nil {
				return nil, fmt.Errorf("agentgraph: increment lesson counts — unmarshal %q: %w", role, unmarshalErr)
			}
			// Extract existing counts from the counts predicate.
			for i := len(entity.Triples) - 1; i >= 0; i-- {
				t := entity.Triples[i]
				if t.Predicate == PredicateLessonCounts {
					if v, ok := t.Object.(string); ok {
						_ = json.Unmarshal([]byte(v), &counts)
					}
					break
				}
			}
		}

		counts.Increment(categoryIDs)

		data, err := json.Marshal(counts)
		if err != nil {
			return nil, fmt.Errorf("agentgraph: marshal lesson counts for %q: %w", role, err)
		}

		now := time.Now()
		triples := []message.Triple{
			propertyTriple(eid, PredicateLessonCounts, string(data), now),
			propertyTriple(eid, PredicateLessonRole, role, now),
		}

		return marshalEntityState(eid, triples, message.Type{Domain: DomainAgent, Category: TypeLessonCounts, Version: "v1"})
	})
	if err != nil {
		return fmt.Errorf("agentgraph: increment lesson counts for role %q: %w", role, err)
	}
	return nil
}

// GetRoleLessonCounts reads the per-category occurrence counts for the given role.
// Returns an empty RoleLessonCounts (not an error) if no counts entity exists yet.
func (h *Helper) GetRoleLessonCounts(ctx context.Context, role string) (workflow.RoleLessonCounts, error) {
	eid := RoleLessonCountsEntityID(role)

	entry, err := h.kv.Get(ctx, eid)
	if err != nil {
		// Key not found is not an error — no counts yet.
		return workflow.RoleLessonCounts{}, nil
	}

	entity, err := unmarshalEntityState(entry.Value)
	if err != nil {
		return workflow.RoleLessonCounts{}, fmt.Errorf("agentgraph: get lesson counts — unmarshal %q: %w", role, err)
	}

	var counts workflow.RoleLessonCounts
	for i := len(entity.Triples) - 1; i >= 0; i-- {
		t := entity.Triples[i]
		if t.Predicate == PredicateLessonCounts {
			if v, ok := t.Object.(string); ok {
				_ = json.Unmarshal([]byte(v), &counts)
			}
			break
		}
	}
	return counts, nil
}

// GetRoleLessonTrends returns error categories that have accumulated more than
// threshold occurrences for the given role. Categories are resolved via registry;
// unrecognised category IDs are skipped. Results are sorted by count descending.
func (h *Helper) GetRoleLessonTrends(
	ctx context.Context,
	role string,
	registry *workflow.ErrorCategoryRegistry,
	threshold int,
) ([]ErrorTrend, error) {
	if threshold < 0 {
		threshold = 0
	}

	counts, err := h.GetRoleLessonCounts(ctx, role)
	if err != nil {
		return nil, err
	}

	var trends []ErrorTrend
	for catID, count := range counts.Counts {
		if count <= threshold {
			continue
		}
		cat, ok := registry.Get(catID)
		if !ok {
			continue
		}
		trends = append(trends, ErrorTrend{Category: cat, Count: count})
	}

	sort.Slice(trends, func(i, j int) bool {
		return trends[i].Count > trends[j].Count
	})

	return trends, nil
}
