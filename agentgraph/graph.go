package agentgraph

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
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

	// Agent identity predicates.
	PredicateAgentName        = "agent.identity.name"
	PredicateAgentRole        = "agent.identity.role"
	PredicateAgentModel       = "agent.config.model"
	PredicateAgentState       = "agent.status.state"
	PredicateAgentErrorCounts = "agent.error.counts"
	PredicateAgentQ1Avg       = "agent.review.q1_avg"
	PredicateAgentQ2Avg       = "agent.review.q2_avg"
	PredicateAgentQ3Avg       = "agent.review.q3_avg"
	PredicateAgentOverallAvg  = "agent.review.overall_avg"
	PredicateAgentReviewCount = "agent.review.count"
	PredicateAgentCreatedAt   = "agent.lifecycle.created_at"
	PredicateAgentUpdatedAt   = "agent.lifecycle.updated_at"

	// Review predicates.
	PredicateReviewScenarioID    = "review.scenario.id"
	PredicateReviewVerdict       = "review.verdict"
	PredicateReviewCorrectness   = "review.rating.correctness"
	PredicateReviewQuality       = "review.rating.quality"
	PredicateReviewCompleteness  = "review.rating.completeness"
	PredicateReviewExplanation   = "review.explanation"
	PredicateReviewAgentID       = "review.agent.id"
	PredicateReviewReviewerID    = "review.reviewer.id"
	PredicateReviewErrorCategory = "review.error.category"
	PredicateReviewRelatedEntity = "review.error.related_entity"
	PredicateReviewTimestamp     = "review.timestamp"

	// Error category predicates.
	PredicateErrorCategoryID          = "error.category.id"
	PredicateErrorCategoryLabel       = "error.category.label"
	PredicateErrorCategoryDescription = "error.category.description"
	PredicateErrorCategorySignal      = "error.category.signal"
	PredicateErrorCategoryGuidance    = "error.category.guidance"
)

// KVEntityStore provides entity storage via NATS KV.
// Any component with a natsclient.Client can create one via NewKVStore.
// *natsclient.KVStore satisfies this interface directly.
type KVEntityStore interface {
	Get(ctx context.Context, key string) (KVEntry, error)
	Put(ctx context.Context, key string, value []byte) (uint64, error)
	UpdateWithRetry(ctx context.Context, key string, updateFn func(current []byte) ([]byte, error)) error
	KeysByPrefix(ctx context.Context, prefix string) ([]string, error)
}

// KVEntry mirrors natsclient.KVEntry to avoid a direct import dependency
// in consuming packages that only need the interface.
type KVEntry struct {
	Key      string
	Value    []byte
	Revision uint64
}

// Helper provides graph operations for agent hierarchy tracking.
// It is a thin façade over KVEntityStore that speaks in agent-domain terms
// (loop IDs, task IDs) rather than raw entity keys.
//
// All methods are safe for concurrent use — they delegate directly to the
// underlying KV store without holding additional state.
type Helper struct {
	kv KVEntityStore
}

// NewHelper constructs a Helper backed by a KVEntityStore.
// The argument is required; passing nil will cause panics at call time.
func NewHelper(kv KVEntityStore) *Helper {
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

	data, err := marshalEntityState(entityID, triples, message.Type{Domain: DomainAgentic, Category: TypeLoop, Version: "v1"})
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
				MessageType: message.Type{Domain: DomainAgentic, Category: TypeLoop, Version: "v1"},
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

// GetChildren returns the loop IDs of all direct children of the given loop.
// It reads the parent entity and scans triples for PredicateSpawned.
func (h *Helper) GetChildren(ctx context.Context, loopID string) ([]string, error) {
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
				parsed, parseErr := types.ParseEntityID(childEntityID)
				if parseErr != nil {
					continue
				}
				children = append(children, parsed.Instance)
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

	// BFS traversal
	visited := map[string]bool{rootEntityID: true}
	result := []string{rootEntityID}
	currentLevel := []string{rootLoopID}

	for depth := 0; depth < maxDepth && len(currentLevel) > 0; depth++ {
		var nextLevel []string
		for _, lid := range currentLevel {
			children, err := h.GetChildren(ctx, lid)
			if err != nil {
				// Skip nodes we can't read rather than failing the whole tree
				continue
			}
			for _, childID := range children {
				childEntityID := LoopEntityID(childID)
				if !visited[childEntityID] {
					visited[childEntityID] = true
					result = append(result, childEntityID)
					nextLevel = append(nextLevel, childID)
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

		data, err := marshalEntityState(entityID, triples, message.Type{Domain: DomainAgentic, Category: TypeErrorCategory, Version: "v1"})
		if err != nil {
			return fmt.Errorf("agentgraph: marshal error category %q: %w", cat.ID, err)
		}

		if _, err := h.kv.Put(ctx, entityID, data); err != nil {
			return fmt.Errorf("agentgraph: seed error category %q: %w", cat.ID, err)
		}
	}
	return nil
}

// CreateAgent writes a new persistent agent entity to the graph.
// All review stats are initialised to zero and error counts to an empty JSON map.
func (h *Helper) CreateAgent(ctx context.Context, agent workflow.Agent) error {
	entityID := AgentEntityID(agent.ID)
	now := time.Now()

	triples := []message.Triple{
		propertyTriple(entityID, PredicateAgentName, agent.Name, now),
		propertyTriple(entityID, PredicateAgentRole, agent.Role, now),
		propertyTriple(entityID, PredicateAgentModel, agent.Model, now),
		propertyTriple(entityID, PredicateAgentState, string(agent.Status), now),
		propertyTriple(entityID, PredicateAgentErrorCounts, "{}", now),
		propertyTriple(entityID, PredicateAgentQ1Avg, float64(0), now),
		propertyTriple(entityID, PredicateAgentQ2Avg, float64(0), now),
		propertyTriple(entityID, PredicateAgentQ3Avg, float64(0), now),
		propertyTriple(entityID, PredicateAgentOverallAvg, float64(0), now),
		propertyTriple(entityID, PredicateAgentReviewCount, 0, now),
		propertyTriple(entityID, PredicateAgentCreatedAt, agent.CreatedAt.Format(time.RFC3339), now),
		propertyTriple(entityID, PredicateAgentUpdatedAt, agent.UpdatedAt.Format(time.RFC3339), now),
	}

	data, err := marshalEntityState(entityID, triples, message.Type{Domain: DomainAgentic, Category: TypeAgent, Version: "v1"})
	if err != nil {
		return fmt.Errorf("agentgraph: marshal agent %q: %w", agent.ID, err)
	}

	if _, err := h.kv.Put(ctx, entityID, data); err != nil {
		return fmt.Errorf("agentgraph: create agent %q: %w", agent.ID, err)
	}
	return nil
}

// GetAgent retrieves a persistent agent by its ID and reconstructs the
// workflow.Agent struct from stored triples.
func (h *Helper) GetAgent(ctx context.Context, agentID string) (*workflow.Agent, error) {
	entityID := AgentEntityID(agentID)

	entry, err := h.kv.Get(ctx, entityID)
	if err != nil {
		return nil, fmt.Errorf("agentgraph: get agent %q: %w", agentID, err)
	}

	entity, err := unmarshalEntityState(entry.Value)
	if err != nil {
		return nil, fmt.Errorf("agentgraph: get agent — unmarshal %q: %w", agentID, err)
	}

	agent := &workflow.Agent{ID: agentID}

	for _, t := range entity.Triples {
		v, _ := t.Object.(string)
		switch t.Predicate {
		case PredicateAgentName:
			agent.Name = v
		case PredicateAgentRole:
			agent.Role = v
		case PredicateAgentModel:
			agent.Model = v
		case PredicateAgentState:
			agent.Status = workflow.AgentStatus(v)
		case PredicateAgentErrorCounts:
			raw := map[string]int{}
			if err := json.Unmarshal([]byte(v), &raw); err == nil {
				agent.ErrorCounts = make(map[workflow.ErrorCategory]int, len(raw))
				for k, cnt := range raw {
					agent.ErrorCounts[k] = cnt
				}
			}
		case PredicateAgentQ1Avg:
			agent.ReviewStats.Q1CorrectnessAvg = toFloat64(t.Object)
		case PredicateAgentQ2Avg:
			agent.ReviewStats.Q2QualityAvg = toFloat64(t.Object)
		case PredicateAgentQ3Avg:
			agent.ReviewStats.Q3CompletenessAvg = toFloat64(t.Object)
		case PredicateAgentOverallAvg:
			agent.ReviewStats.OverallAvg = toFloat64(t.Object)
		case PredicateAgentReviewCount:
			agent.ReviewStats.ReviewCount = toInt(t.Object)
		case PredicateAgentCreatedAt:
			if ts, err := time.Parse(time.RFC3339, v); err == nil {
				agent.CreatedAt = ts
			}
		case PredicateAgentUpdatedAt:
			if ts, err := time.Parse(time.RFC3339, v); err == nil {
				agent.UpdatedAt = ts
			}
		}
	}

	return agent, nil
}

// GetOrCreateDefaultAgent returns an existing agent for the given role, or creates
// a new one when no agent with that role is found in the graph.
// If multiple agents share the same role, the first one found is returned.
func (h *Helper) GetOrCreateDefaultAgent(ctx context.Context, role, model string) (*workflow.Agent, error) {
	prefix := AgentTypePrefix()

	keys, err := h.kv.KeysByPrefix(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("agentgraph: list agents: %w", err)
	}

	for _, key := range keys {
		entry, err := h.kv.Get(ctx, key)
		if err != nil {
			continue
		}
		entity, err := unmarshalEntityState(entry.Value)
		if err != nil {
			continue
		}
		for _, t := range entity.Triples {
			if t.Predicate == PredicateAgentRole {
				if r, ok := t.Object.(string); ok && r == role {
					parsed, parseErr := types.ParseEntityID(key)
					if parseErr != nil {
						continue
					}
					return h.GetAgent(ctx, parsed.Instance)
				}
			}
		}
	}

	// No existing agent found — create a new one.
	id := uuid.New().String()
	shortID := strings.ReplaceAll(id, "-", "")[:8]
	now := time.Now()
	agent := workflow.Agent{
		ID:        shortID,
		Name:      role + "-" + shortID,
		Role:      role,
		Model:     model,
		Status:    workflow.AgentAvailable,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := h.CreateAgent(ctx, agent); err != nil {
		return nil, fmt.Errorf("agentgraph: create default agent for role %q: %w", role, err)
	}
	return &agent, nil
}

// UpdateAgentStats replaces the review stat triples on the agent entity with
// the values provided in stats. Uses UpdateWithRetry for atomic CAS.
func (h *Helper) UpdateAgentStats(ctx context.Context, agentID string, stats workflow.ReviewStats) error {
	entityID := AgentEntityID(agentID)

	err := h.kv.UpdateWithRetry(ctx, entityID, func(current []byte) ([]byte, error) {
		entity, unmarshalErr := unmarshalEntityState(current)
		if unmarshalErr != nil {
			return nil, fmt.Errorf("agentgraph: update agent stats — get entity %q: %w", agentID, unmarshalErr)
		}

		now := time.Now()
		replacePredicates := []string{
			PredicateAgentQ1Avg,
			PredicateAgentQ2Avg,
			PredicateAgentQ3Avg,
			PredicateAgentOverallAvg,
			PredicateAgentReviewCount,
			PredicateAgentUpdatedAt,
		}
		for _, pred := range replacePredicates {
			entity.Triples = removeTriples(entity.Triples, pred)
		}

		entity.Triples = append(entity.Triples,
			propertyTriple(entityID, PredicateAgentQ1Avg, stats.Q1CorrectnessAvg, now),
			propertyTriple(entityID, PredicateAgentQ2Avg, stats.Q2QualityAvg, now),
			propertyTriple(entityID, PredicateAgentQ3Avg, stats.Q3CompletenessAvg, now),
			propertyTriple(entityID, PredicateAgentOverallAvg, stats.OverallAvg, now),
			propertyTriple(entityID, PredicateAgentReviewCount, stats.ReviewCount, now),
			propertyTriple(entityID, PredicateAgentUpdatedAt, now.Format(time.RFC3339), now),
		)
		entity.UpdatedAt = now
		return json.Marshal(entity)
	})
	if err != nil {
		return fmt.Errorf("agentgraph: update agent stats %q: %w", agentID, err)
	}
	return nil
}

// RecordReview writes a peer review as a graph entity.
// Each ReviewErrorRef in review.Errors produces one PredicateReviewErrorCategory
// triple (the category ID) plus one PredicateReviewRelatedEntity triple per
// related entity ID.
func (h *Helper) RecordReview(ctx context.Context, review workflow.Review) error {
	entityID := ReviewEntityID(review.ID)
	now := time.Now()

	triples := []message.Triple{
		propertyTriple(entityID, PredicateReviewScenarioID, review.ScenarioID, now),
		propertyTriple(entityID, PredicateReviewAgentID, review.AgentID, now),
		propertyTriple(entityID, PredicateReviewReviewerID, review.ReviewerAgentID, now),
		propertyTriple(entityID, PredicateReviewVerdict, string(review.Verdict), now),
		propertyTriple(entityID, PredicateReviewCorrectness, review.Q1Correctness, now),
		propertyTriple(entityID, PredicateReviewQuality, review.Q2Quality, now),
		propertyTriple(entityID, PredicateReviewCompleteness, review.Q3Completeness, now),
		propertyTriple(entityID, PredicateReviewExplanation, review.Explanation, now),
		propertyTriple(entityID, PredicateReviewTimestamp, review.Timestamp.Format(time.RFC3339), now),
	}

	for _, errRef := range review.Errors {
		triples = append(triples, propertyTriple(entityID, PredicateReviewErrorCategory, errRef.CategoryID, now))
		for _, relID := range errRef.RelatedEntityIDs {
			triples = append(triples, propertyTriple(entityID, PredicateReviewRelatedEntity, relID, now))
		}
	}

	data, err := marshalEntityState(entityID, triples, message.Type{Domain: DomainAgentic, Category: TypeReview, Version: "v1"})
	if err != nil {
		return fmt.Errorf("agentgraph: marshal review %q: %w", review.ID, err)
	}

	if _, err := h.kv.Put(ctx, entityID, data); err != nil {
		return fmt.Errorf("agentgraph: record review %q: %w", review.ID, err)
	}
	return nil
}

// IncrementAgentErrorCounts increments the accumulated error count for each
// of the given category IDs on the agent entity. Uses UpdateWithRetry for
// atomic CAS with exponential backoff.
func (h *Helper) IncrementAgentErrorCounts(ctx context.Context, agentID string, categoryIDs []string) error {
	entityID := AgentEntityID(agentID)

	err := h.kv.UpdateWithRetry(ctx, entityID, func(current []byte) ([]byte, error) {
		entity, unmarshalErr := unmarshalEntityState(current)
		if unmarshalErr != nil {
			return nil, fmt.Errorf("agentgraph: increment error counts — get entity %q: %w", agentID, unmarshalErr)
		}

		// Scan in reverse so that the most recently written triple wins.
		counts := map[string]int{}
		for i := len(entity.Triples) - 1; i >= 0; i-- {
			t := entity.Triples[i]
			if t.Predicate == PredicateAgentErrorCounts {
				if v, ok := t.Object.(string); ok {
					_ = json.Unmarshal([]byte(v), &counts)
				}
				break
			}
		}

		for _, id := range categoryIDs {
			counts[id]++
		}

		data, err := json.Marshal(counts)
		if err != nil {
			return nil, fmt.Errorf("agentgraph: marshal error counts for agent %q: %w", agentID, err)
		}

		now := time.Now()
		entity.Triples = replaceTriple(entity.Triples, PredicateAgentErrorCounts,
			propertyTriple(entityID, PredicateAgentErrorCounts, string(data), now))
		entity.UpdatedAt = now
		return json.Marshal(entity)
	})
	if err != nil {
		return fmt.Errorf("agentgraph: update error counts for agent %q: %w", agentID, err)
	}
	return nil
}

// GetAgentErrorTrends returns a sorted list of error categories that have
// accumulated more than one occurrence for the given agent.
// Categories are resolved via registry; unrecognised category IDs are skipped.
// Results are sorted by count descending so callers can use the top-N entries.
func (h *Helper) GetAgentErrorTrends(
	ctx context.Context,
	agentID string,
	registry *workflow.ErrorCategoryRegistry,
) ([]workflow.ErrorTrend, error) {
	entityID := AgentEntityID(agentID)

	entry, err := h.kv.Get(ctx, entityID)
	if err != nil {
		return nil, fmt.Errorf("agentgraph: get error trends — get entity %q: %w", agentID, err)
	}

	entity, err := unmarshalEntityState(entry.Value)
	if err != nil {
		return nil, fmt.Errorf("agentgraph: get error trends — unmarshal %q: %w", agentID, err)
	}

	// Scan in reverse so that the most recently written triple wins.
	counts := map[string]int{}
	for i := len(entity.Triples) - 1; i >= 0; i-- {
		t := entity.Triples[i]
		if t.Predicate == PredicateAgentErrorCounts {
			if v, ok := t.Object.(string); ok {
				_ = json.Unmarshal([]byte(v), &counts)
			}
			break
		}
	}

	var trends []workflow.ErrorTrend
	for catID, count := range counts {
		if count <= 1 {
			continue
		}
		cat, ok := registry.Get(catID)
		if !ok {
			continue
		}
		trends = append(trends, workflow.ErrorTrend{Category: cat, Count: count})
	}

	sort.Slice(trends, func(i, j int) bool {
		return trends[i].Count > trends[j].Count
	})

	return trends, nil
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

// removeTriples removes all triples with the given predicate.
func removeTriples(triples []message.Triple, predicate string) []message.Triple {
	result := triples[:0]
	for _, t := range triples {
		if t.Predicate != predicate {
			result = append(result, t)
		}
	}
	return result
}

// toFloat64 coerces numeric graph triple objects to float64.
// Graph storage may round-trip numbers as float64 or int depending on JSON encoding.
func toFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}

// toInt coerces numeric graph triple objects to int.
func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case float32:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
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
