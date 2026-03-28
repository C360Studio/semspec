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

	// Agent identity predicates.
	PredicateAgentID          = "agent.identity.id"
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

	// Team entity predicates.
	PredicateTeamID             = "team.identity.id"
	PredicateTeamName           = "team.identity.name"
	PredicateTeamState          = "team.status.state"
	PredicateTeamMember         = "team.member.agent_id"
	PredicateTeamInsight        = "team.knowledge.insight"
	PredicateTeamQ1Avg          = "team.review.q1_avg"
	PredicateTeamQ2Avg          = "team.review.q2_avg"
	PredicateTeamQ3Avg          = "team.review.q3_avg"
	PredicateTeamOverallAvg     = "team.review.overall_avg"
	PredicateTeamReviewCount    = "team.review.count"
	PredicateTeamRedQ1Avg       = "team.redteam.q1_avg"
	PredicateTeamRedQ2Avg       = "team.redteam.q2_avg"
	PredicateTeamRedQ3Avg       = "team.redteam.q3_avg"
	PredicateTeamRedOverallAvg  = "team.redteam.overall_avg"
	PredicateTeamRedReviewCount = "team.redteam.count"
	PredicateTeamErrorCounts    = "team.error.counts"
	PredicateTeamCreatedAt      = "team.lifecycle.created_at"
	PredicateTeamUpdatedAt      = "team.lifecycle.updated_at"
	PredicateAgentTeamID        = "agent.team.id"
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

// CreateAgent writes a new persistent agent entity to the graph.
// All review stats are initialised to zero and error counts to an empty JSON map.
func (h *Helper) CreateAgent(ctx context.Context, agent workflow.Agent) error {
	entityID := AgentEntityID(agent.ID)
	now := time.Now()

	triples := []message.Triple{
		propertyTriple(entityID, PredicateAgentID, agent.ID, now),
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

	data, err := marshalEntityState(entityID, triples, message.Type{Domain: DomainAgent, Category: TypeAgent, Version: "v1"})
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

	return parseAgentFromTriples(agentID, entity.Triples), nil
}

// parseAgentFromTriples reconstructs a workflow.Agent from entity triples.
func parseAgentFromTriples(fallbackID string, triples []message.Triple) *workflow.Agent {
	agent := &workflow.Agent{ID: fallbackID}

	for _, t := range triples {
		v, _ := t.Object.(string)
		switch t.Predicate {
		case PredicateAgentID:
			if v != "" {
				agent.ID = v
			}
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

	return agent
}

// ListAgentsByRole returns all persistent agents whose role triple matches the
// given role string. Agents that cannot be parsed are silently skipped rather
// than failing the whole scan — a corrupt entry should not block dispatch.
func (h *Helper) ListAgentsByRole(ctx context.Context, role string) ([]*workflow.Agent, error) {
	prefix := AgentTypePrefix()

	// KeysByPrefix appends ">" to form a NATS wildcard pattern. The prefix must
	// end with "." so the pattern becomes "...agent.>" (dot-separated wildcard)
	// rather than "...agent>" (literal suffix that won't match sub-keys).
	keys, err := h.kv.KeysByPrefix(ctx, prefix+".")
	if err != nil {
		return nil, fmt.Errorf("agentgraph: list agents by role: %w", err)
	}

	var agents []*workflow.Agent
	for _, key := range keys {
		entry, err := h.kv.Get(ctx, key)
		if err != nil {
			continue
		}
		entity, err := unmarshalEntityState(entry.Value)
		if err != nil {
			continue
		}
		// Check role triple before full parse to avoid unnecessary work.
		roleMatch := false
		for _, t := range entity.Triples {
			if t.Predicate == PredicateAgentRole {
				if r, ok := t.Object.(string); ok && r == role {
					roleMatch = true
				}
				break
			}
		}
		if !roleMatch {
			continue
		}
		parsed, parseErr := types.ParseEntityID(key)
		if parseErr != nil {
			continue
		}
		agents = append(agents, parseAgentFromTriples(parsed.Instance, entity.Triples))
	}

	if agents == nil {
		agents = []*workflow.Agent{}
	}
	return agents, nil
}

// SetAgentStatus atomically updates the agent state and updated-at triples.
// Uses UpdateWithRetry for CAS semantics.
func (h *Helper) SetAgentStatus(ctx context.Context, agentID string, status workflow.AgentStatus) error {
	entityID := AgentEntityID(agentID)

	err := h.kv.UpdateWithRetry(ctx, entityID, func(current []byte) ([]byte, error) {
		entity, unmarshalErr := unmarshalEntityState(current)
		if unmarshalErr != nil {
			return nil, fmt.Errorf("agentgraph: set agent status — get entity %q: %w", agentID, unmarshalErr)
		}

		now := time.Now()
		entity.Triples = replaceTriple(entity.Triples, PredicateAgentState,
			propertyTriple(entityID, PredicateAgentState, string(status), now))
		entity.Triples = replaceTriple(entity.Triples, PredicateAgentUpdatedAt,
			propertyTriple(entityID, PredicateAgentUpdatedAt, now.Format(time.RFC3339), now))
		entity.UpdatedAt = now
		return json.Marshal(entity)
	})
	if err != nil {
		return fmt.Errorf("agentgraph: set agent status %q -> %q: %w", agentID, status, err)
	}
	return nil
}

// SelectAgent selects the best available agent for the given role using a
// two-criteria sort: lowest TotalErrorCount first, then highest OverallAvg as
// a tie-breaker. Only agents with AgentAvailable status are considered.
//
// When no available agent exists and nextModel is non-empty, a fresh agent is
// created with that model and returned. When no available agent exists and
// nextModel is empty, nil is returned without error — the caller is responsible
// for escalating.
func (h *Helper) SelectAgent(ctx context.Context, role, nextModel string) (*workflow.Agent, error) {
	agents, err := h.ListAgentsByRole(ctx, role)
	if err != nil {
		return nil, fmt.Errorf("agentgraph: select agent: %w", err)
	}

	var available []*workflow.Agent
	for _, a := range agents {
		if a.Status == workflow.AgentAvailable {
			available = append(available, a)
		}
	}

	if len(available) > 0 {
		sort.Slice(available, func(i, j int) bool {
			ti := available[i].TotalErrorCount()
			tj := available[j].TotalErrorCount()
			if ti != tj {
				return ti < tj
			}
			return available[i].ReviewStats.OverallAvg > available[j].ReviewStats.OverallAvg
		})
		return available[0], nil
	}

	// No available agents — create one if a model was provided.
	if nextModel == "" {
		return nil, nil
	}

	id := uuid.New().String()
	shortID := strings.ReplaceAll(id, "-", "")[:8]
	now := time.Now()
	agent := workflow.Agent{
		ID:        shortID,
		Name:      role + "-" + shortID,
		Role:      role,
		Model:     nextModel,
		Status:    workflow.AgentAvailable,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := h.CreateAgent(ctx, agent); err != nil {
		return nil, fmt.Errorf("agentgraph: select agent — create for role %q: %w", role, err)
	}
	return &agent, nil
}

// BenchAgent benches the agent if ShouldBench returns true for the given threshold.
// Returns true when the agent was benched, false when the threshold was not met.
// Does not return an error if the agent is already benched — the status update is
// applied unconditionally when the threshold is reached.
func (h *Helper) BenchAgent(ctx context.Context, agentID string, threshold int) (bool, error) {
	agent, err := h.GetAgent(ctx, agentID)
	if err != nil {
		return false, fmt.Errorf("agentgraph: bench agent — get %q: %w", agentID, err)
	}

	if !agent.ShouldBench(threshold) {
		return false, nil
	}

	if err := h.SetAgentStatus(ctx, agentID, workflow.AgentBenched); err != nil {
		return false, fmt.Errorf("agentgraph: bench agent %q: %w", agentID, err)
	}
	return true, nil
}

// GetOrCreateDefaultAgent delegates to SelectAgent to find the best available
// agent for the given role, creating a new one with model when none exists.
// Returns an error when SelectAgent returns nil (all agents exhausted and no
// model was provided to create a new one).
func (h *Helper) GetOrCreateDefaultAgent(ctx context.Context, role, model string) (*workflow.Agent, error) {
	agent, err := h.SelectAgent(ctx, role, model)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, fmt.Errorf("agentgraph: no agent available for role %q", role)
	}
	return agent, nil
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
func (h *Helper) RecordReview(ctx context.Context, review Review) error {
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

	data, err := marshalEntityState(entityID, triples, message.Type{Domain: DomainAgent, Category: TypeReview, Version: "v1"})
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

// CreateTeam writes a new persistent team entity to the graph.
// All review stats are initialised to zero and error counts to an empty JSON map.
// Sets CreatedAt and UpdatedAt to time.Now() when their zero values are passed in.
func (h *Helper) CreateTeam(ctx context.Context, team *workflow.Team) error {
	entityID := TeamEntityID(team.ID)
	now := time.Now()

	if team.CreatedAt.IsZero() {
		team.CreatedAt = now
	}
	if team.UpdatedAt.IsZero() {
		team.UpdatedAt = now
	}

	triples := []message.Triple{
		propertyTriple(entityID, PredicateTeamID, team.ID, now),
		propertyTriple(entityID, PredicateTeamName, team.Name, now),
		propertyTriple(entityID, PredicateTeamState, string(team.Status), now),
		propertyTriple(entityID, PredicateTeamErrorCounts, "{}", now),
		propertyTriple(entityID, PredicateTeamQ1Avg, float64(0), now),
		propertyTriple(entityID, PredicateTeamQ2Avg, float64(0), now),
		propertyTriple(entityID, PredicateTeamQ3Avg, float64(0), now),
		propertyTriple(entityID, PredicateTeamOverallAvg, float64(0), now),
		propertyTriple(entityID, PredicateTeamReviewCount, 0, now),
		propertyTriple(entityID, PredicateTeamRedQ1Avg, float64(0), now),
		propertyTriple(entityID, PredicateTeamRedQ2Avg, float64(0), now),
		propertyTriple(entityID, PredicateTeamRedQ3Avg, float64(0), now),
		propertyTriple(entityID, PredicateTeamRedOverallAvg, float64(0), now),
		propertyTriple(entityID, PredicateTeamRedReviewCount, 0, now),
		propertyTriple(entityID, PredicateTeamCreatedAt, team.CreatedAt.Format(time.RFC3339), now),
		propertyTriple(entityID, PredicateTeamUpdatedAt, team.UpdatedAt.Format(time.RFC3339), now),
	}

	// Member IDs are multi-valued — one triple per member.
	for _, memberID := range team.MemberIDs {
		triples = append(triples, propertyTriple(entityID, PredicateTeamMember, memberID, now))
	}

	// Shared knowledge is stored as a JSON array.
	insightJSON, err := json.Marshal(team.SharedKnowledge)
	if err != nil {
		return fmt.Errorf("agentgraph: marshal team insights for %q: %w", team.ID, err)
	}
	triples = append(triples, propertyTriple(entityID, PredicateTeamInsight, string(insightJSON), now))

	data, err := marshalEntityState(entityID, triples, message.Type{Domain: DomainAgent, Category: TypeTeam, Version: "v1"})
	if err != nil {
		return fmt.Errorf("agentgraph: marshal team %q: %w", team.ID, err)
	}

	if _, err := h.kv.Put(ctx, entityID, data); err != nil {
		return fmt.Errorf("agentgraph: create team %q: %w", team.ID, err)
	}
	return nil
}

// GetTeam retrieves a persistent team by its ID and reconstructs the
// workflow.Team struct from stored triples.
func (h *Helper) GetTeam(ctx context.Context, teamID string) (*workflow.Team, error) {
	entityID := TeamEntityID(teamID)

	entry, err := h.kv.Get(ctx, entityID)
	if err != nil {
		return nil, fmt.Errorf("agentgraph: get team %q: %w", teamID, err)
	}

	entity, err := unmarshalEntityState(entry.Value)
	if err != nil {
		return nil, fmt.Errorf("agentgraph: get team — unmarshal %q: %w", teamID, err)
	}

	return parseTeamFromTriples(teamID, entity.Triples), nil
}

// parseTeamFromTriples reconstructs a workflow.Team from entity triples.
// PredicateTeamMember is multi-valued; all occurrences are collected into MemberIDs.
func parseTeamFromTriples(fallbackID string, triples []message.Triple) *workflow.Team {
	team := &workflow.Team{ID: fallbackID}

	for _, t := range triples {
		v, _ := t.Object.(string)
		switch t.Predicate {
		case PredicateTeamID:
			if v != "" {
				team.ID = v // prefer ID from triple over fallback
			}
		case PredicateTeamName:
			team.Name = v
		case PredicateTeamState:
			team.Status = workflow.TeamStatus(v)
		case PredicateTeamMember:
			if v != "" {
				team.MemberIDs = append(team.MemberIDs, v)
			}
		case PredicateTeamInsight:
			var insights []workflow.TeamInsight
			if err := json.Unmarshal([]byte(v), &insights); err == nil {
				team.SharedKnowledge = insights
			}
		case PredicateTeamQ1Avg:
			team.TeamStats.Q1CorrectnessAvg = toFloat64(t.Object)
		case PredicateTeamQ2Avg:
			team.TeamStats.Q2QualityAvg = toFloat64(t.Object)
		case PredicateTeamQ3Avg:
			team.TeamStats.Q3CompletenessAvg = toFloat64(t.Object)
		case PredicateTeamOverallAvg:
			team.TeamStats.OverallAvg = toFloat64(t.Object)
		case PredicateTeamReviewCount:
			team.TeamStats.ReviewCount = toInt(t.Object)
		case PredicateTeamRedQ1Avg:
			team.RedTeamStats.Q1CorrectnessAvg = toFloat64(t.Object)
		case PredicateTeamRedQ2Avg:
			team.RedTeamStats.Q2QualityAvg = toFloat64(t.Object)
		case PredicateTeamRedQ3Avg:
			team.RedTeamStats.Q3CompletenessAvg = toFloat64(t.Object)
		case PredicateTeamRedOverallAvg:
			team.RedTeamStats.OverallAvg = toFloat64(t.Object)
		case PredicateTeamRedReviewCount:
			team.RedTeamStats.ReviewCount = toInt(t.Object)
		case PredicateTeamErrorCounts:
			raw := map[string]int{}
			if err := json.Unmarshal([]byte(v), &raw); err == nil {
				team.ErrorCounts = make(map[workflow.ErrorCategory]int, len(raw))
				for k, cnt := range raw {
					team.ErrorCounts[k] = cnt
				}
			}
		case PredicateTeamCreatedAt:
			if ts, err := time.Parse(time.RFC3339, v); err == nil {
				team.CreatedAt = ts
			}
		case PredicateTeamUpdatedAt:
			if ts, err := time.Parse(time.RFC3339, v); err == nil {
				team.UpdatedAt = ts
			}
		}
	}

	return team
}

// ListTeams returns all persistent team entities in the graph.
// Teams that cannot be parsed are silently skipped rather than failing the whole
// scan — a corrupt entry should not block team selection.
// Insight sub-entities (keys containing ".insight.") are excluded.
func (h *Helper) ListTeams(ctx context.Context) ([]*workflow.Team, error) {
	prefix := TeamTypePrefix()

	// KeysByPrefix appends ">" to form a NATS wildcard pattern. The prefix must
	// end with "." so the pattern becomes "...team.>" (dot-separated wildcard)
	// rather than "...team>" (literal suffix that won't match sub-keys).
	keys, err := h.kv.KeysByPrefix(ctx, prefix+".")
	if err != nil {
		return nil, fmt.Errorf("agentgraph: list teams: %w", err)
	}

	var teams []*workflow.Team
	for _, key := range keys {
		// Skip team-insight sub-entities which share the team prefix.
		if strings.Contains(key, "."+TypeInsight+".") {
			continue
		}
		entry, err := h.kv.Get(ctx, key)
		if err != nil {
			continue
		}
		entity, err := unmarshalEntityState(entry.Value)
		if err != nil {
			continue
		}
		parsed, parseErr := types.ParseEntityID(key)
		if parseErr != nil {
			continue
		}
		teams = append(teams, parseTeamFromTriples(parsed.Instance, entity.Triples))
	}

	if teams == nil {
		teams = []*workflow.Team{}
	}
	return teams, nil
}

// SelectBlueTeam selects the best active team to act as the blue team (primary execution team).
// Sorting criteria: lowest TotalErrorCount first, then highest TeamStats.OverallAvg as
// tie-breaker. Returns nil without error when no active team exists.
func (h *Helper) SelectBlueTeam(ctx context.Context) (*workflow.Team, error) {
	teams, err := h.ListTeams(ctx)
	if err != nil {
		return nil, fmt.Errorf("agentgraph: select blue team: %w", err)
	}

	var active []*workflow.Team
	for _, t := range teams {
		if t.Status == workflow.TeamActive {
			active = append(active, t)
		}
	}

	if len(active) == 0 {
		return nil, nil
	}

	sort.Slice(active, func(i, j int) bool {
		ti := active[i].TotalErrorCount()
		tj := active[j].TotalErrorCount()
		if ti != tj {
			return ti < tj
		}
		return active[i].TeamStats.OverallAvg > active[j].TeamStats.OverallAvg
	})
	return active[0], nil
}

// SelectRedTeam selects the best active team to act as the red team (critic team)
// for the given blue team. The blue team is excluded from consideration.
// Sorting criteria: highest RedTeamStats.OverallAvg first (best critics),
// tie-break by lowest TotalErrorCount. Returns nil without error when no
// eligible active team exists.
func (h *Helper) SelectRedTeam(ctx context.Context, blueTeamID string) (*workflow.Team, error) {
	teams, err := h.ListTeams(ctx)
	if err != nil {
		return nil, fmt.Errorf("agentgraph: select red team: %w", err)
	}

	var candidates []*workflow.Team
	for _, t := range teams {
		if t.Status == workflow.TeamActive && t.ID != blueTeamID {
			candidates = append(candidates, t)
		}
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		ri := candidates[i].RedTeamStats.OverallAvg
		rj := candidates[j].RedTeamStats.OverallAvg
		if ri != rj {
			return ri > rj
		}
		return candidates[i].TotalErrorCount() < candidates[j].TotalErrorCount()
	})
	return candidates[0], nil
}

// SetTeamStatus atomically updates the team state and updated-at triples.
// Uses UpdateWithRetry for CAS semantics.
func (h *Helper) SetTeamStatus(ctx context.Context, teamID string, status workflow.TeamStatus) error {
	entityID := TeamEntityID(teamID)

	err := h.kv.UpdateWithRetry(ctx, entityID, func(current []byte) ([]byte, error) {
		entity, unmarshalErr := unmarshalEntityState(current)
		if unmarshalErr != nil {
			return nil, fmt.Errorf("agentgraph: set team status — get entity %q: %w", teamID, unmarshalErr)
		}

		now := time.Now()
		entity.Triples = replaceTriple(entity.Triples, PredicateTeamState,
			propertyTriple(entityID, PredicateTeamState, string(status), now))
		entity.Triples = replaceTriple(entity.Triples, PredicateTeamUpdatedAt,
			propertyTriple(entityID, PredicateTeamUpdatedAt, now.Format(time.RFC3339), now))
		entity.UpdatedAt = now
		return json.Marshal(entity)
	})
	if err != nil {
		return fmt.Errorf("agentgraph: set team status %q -> %q: %w", teamID, status, err)
	}
	return nil
}

// UpdateTeamStats replaces the team.review.* stat triples on the team entity.
// Uses UpdateWithRetry for atomic CAS.
func (h *Helper) UpdateTeamStats(ctx context.Context, teamID string, stats workflow.ReviewStats) error {
	entityID := TeamEntityID(teamID)

	err := h.kv.UpdateWithRetry(ctx, entityID, func(current []byte) ([]byte, error) {
		entity, unmarshalErr := unmarshalEntityState(current)
		if unmarshalErr != nil {
			return nil, fmt.Errorf("agentgraph: update team stats — get entity %q: %w", teamID, unmarshalErr)
		}

		now := time.Now()
		replacePredicates := []string{
			PredicateTeamQ1Avg,
			PredicateTeamQ2Avg,
			PredicateTeamQ3Avg,
			PredicateTeamOverallAvg,
			PredicateTeamReviewCount,
			PredicateTeamUpdatedAt,
		}
		for _, pred := range replacePredicates {
			entity.Triples = removeTriples(entity.Triples, pred)
		}

		entity.Triples = append(entity.Triples,
			propertyTriple(entityID, PredicateTeamQ1Avg, stats.Q1CorrectnessAvg, now),
			propertyTriple(entityID, PredicateTeamQ2Avg, stats.Q2QualityAvg, now),
			propertyTriple(entityID, PredicateTeamQ3Avg, stats.Q3CompletenessAvg, now),
			propertyTriple(entityID, PredicateTeamOverallAvg, stats.OverallAvg, now),
			propertyTriple(entityID, PredicateTeamReviewCount, stats.ReviewCount, now),
			propertyTriple(entityID, PredicateTeamUpdatedAt, now.Format(time.RFC3339), now),
		)
		entity.UpdatedAt = now
		return json.Marshal(entity)
	})
	if err != nil {
		return fmt.Errorf("agentgraph: update team stats %q: %w", teamID, err)
	}
	return nil
}

// UpdateTeamRedTeamStats replaces the team.redteam.* stat triples on the team entity.
// Uses UpdateWithRetry for atomic CAS.
func (h *Helper) UpdateTeamRedTeamStats(ctx context.Context, teamID string, stats workflow.ReviewStats) error {
	entityID := TeamEntityID(teamID)

	err := h.kv.UpdateWithRetry(ctx, entityID, func(current []byte) ([]byte, error) {
		entity, unmarshalErr := unmarshalEntityState(current)
		if unmarshalErr != nil {
			return nil, fmt.Errorf("agentgraph: update team redteam stats — get entity %q: %w", teamID, unmarshalErr)
		}

		now := time.Now()
		replacePredicates := []string{
			PredicateTeamRedQ1Avg,
			PredicateTeamRedQ2Avg,
			PredicateTeamRedQ3Avg,
			PredicateTeamRedOverallAvg,
			PredicateTeamRedReviewCount,
			PredicateTeamUpdatedAt,
		}
		for _, pred := range replacePredicates {
			entity.Triples = removeTriples(entity.Triples, pred)
		}

		entity.Triples = append(entity.Triples,
			propertyTriple(entityID, PredicateTeamRedQ1Avg, stats.Q1CorrectnessAvg, now),
			propertyTriple(entityID, PredicateTeamRedQ2Avg, stats.Q2QualityAvg, now),
			propertyTriple(entityID, PredicateTeamRedQ3Avg, stats.Q3CompletenessAvg, now),
			propertyTriple(entityID, PredicateTeamRedOverallAvg, stats.OverallAvg, now),
			propertyTriple(entityID, PredicateTeamRedReviewCount, stats.ReviewCount, now),
			propertyTriple(entityID, PredicateTeamUpdatedAt, now.Format(time.RFC3339), now),
		)
		entity.UpdatedAt = now
		return json.Marshal(entity)
	})
	if err != nil {
		return fmt.Errorf("agentgraph: update team redteam stats %q: %w", teamID, err)
	}
	return nil
}

// UpdateTeamRedTeamStatsIncremental atomically reads the current red-team
// stats, incorporates a new review (q1=accuracy, q2=thoroughness, q3=fairness),
// and writes the updated stats back — all inside a CAS retry loop. This avoids
// the read-modify-write race that would occur if the caller read stats, computed
// the running average, and then wrote them back in separate operations.
func (h *Helper) UpdateTeamRedTeamStatsIncremental(ctx context.Context, teamID string, q1, q2, q3 int) error {
	entityID := TeamEntityID(teamID)

	err := h.kv.UpdateWithRetry(ctx, entityID, func(current []byte) ([]byte, error) {
		entity, unmarshalErr := unmarshalEntityState(current)
		if unmarshalErr != nil {
			return nil, fmt.Errorf("agentgraph: update team redteam stats incremental — get entity %q: %w", teamID, unmarshalErr)
		}

		// Reconstruct current stats from triples.
		var stats workflow.ReviewStats
		for _, t := range entity.Triples {
			switch t.Predicate {
			case PredicateTeamRedQ1Avg:
				stats.Q1CorrectnessAvg = toFloat64(t.Object)
			case PredicateTeamRedQ2Avg:
				stats.Q2QualityAvg = toFloat64(t.Object)
			case PredicateTeamRedQ3Avg:
				stats.Q3CompletenessAvg = toFloat64(t.Object)
			case PredicateTeamRedOverallAvg:
				stats.OverallAvg = toFloat64(t.Object)
			case PredicateTeamRedReviewCount:
				stats.ReviewCount = toInt(t.Object)
			}
		}

		// Incorporate the new review.
		stats.UpdateStats(q1, q2, q3)

		now := time.Now()
		replacePredicates := []string{
			PredicateTeamRedQ1Avg,
			PredicateTeamRedQ2Avg,
			PredicateTeamRedQ3Avg,
			PredicateTeamRedOverallAvg,
			PredicateTeamRedReviewCount,
			PredicateTeamUpdatedAt,
		}
		for _, pred := range replacePredicates {
			entity.Triples = removeTriples(entity.Triples, pred)
		}

		entity.Triples = append(entity.Triples,
			propertyTriple(entityID, PredicateTeamRedQ1Avg, stats.Q1CorrectnessAvg, now),
			propertyTriple(entityID, PredicateTeamRedQ2Avg, stats.Q2QualityAvg, now),
			propertyTriple(entityID, PredicateTeamRedQ3Avg, stats.Q3CompletenessAvg, now),
			propertyTriple(entityID, PredicateTeamRedOverallAvg, stats.OverallAvg, now),
			propertyTriple(entityID, PredicateTeamRedReviewCount, stats.ReviewCount, now),
			propertyTriple(entityID, PredicateTeamUpdatedAt, now.Format(time.RFC3339), now),
		)
		entity.UpdatedAt = now
		return json.Marshal(entity)
	})
	if err != nil {
		return fmt.Errorf("agentgraph: update team redteam stats incremental %q: %w", teamID, err)
	}
	return nil
}

// maxTeamInsights is the maximum number of insights retained per team.
// Oldest insights are dropped when this cap is exceeded.
const maxTeamInsights = 50

// AddTeamInsight appends a new insight to the team's shared knowledge.
// Insights are capped at maxTeamInsights (50); when the cap is exceeded the
// oldest entries are dropped first. Uses UpdateWithRetry for CAS safety.
func (h *Helper) AddTeamInsight(ctx context.Context, teamID string, insight workflow.TeamInsight) error {
	entityID := TeamEntityID(teamID)

	err := h.kv.UpdateWithRetry(ctx, entityID, func(current []byte) ([]byte, error) {
		entity, unmarshalErr := unmarshalEntityState(current)
		if unmarshalErr != nil {
			return nil, fmt.Errorf("agentgraph: add team insight — get entity %q: %w", teamID, unmarshalErr)
		}

		// Extract current insight JSON, scanning in reverse so the most recently
		// written triple wins if multiple exist.
		var insights []workflow.TeamInsight
		for i := len(entity.Triples) - 1; i >= 0; i-- {
			t := entity.Triples[i]
			if t.Predicate == PredicateTeamInsight {
				if v, ok := t.Object.(string); ok {
					_ = json.Unmarshal([]byte(v), &insights)
				}
				break
			}
		}

		insights = append(insights, insight)

		// Enforce cap: drop oldest (leading) entries when exceeded.
		if len(insights) > maxTeamInsights {
			insights = insights[len(insights)-maxTeamInsights:]
		}

		data, err := json.Marshal(insights)
		if err != nil {
			return nil, fmt.Errorf("agentgraph: marshal insights for team %q: %w", teamID, err)
		}

		now := time.Now()
		entity.Triples = replaceTriple(entity.Triples, PredicateTeamInsight,
			propertyTriple(entityID, PredicateTeamInsight, string(data), now))
		entity.Triples = replaceTriple(entity.Triples, PredicateTeamUpdatedAt,
			propertyTriple(entityID, PredicateTeamUpdatedAt, now.Format(time.RFC3339), now))
		entity.UpdatedAt = now
		return json.Marshal(entity)
	})
	if err != nil {
		return fmt.Errorf("agentgraph: add team insight %q: %w", teamID, err)
	}
	return nil
}

// IncrementTeamErrorCounts increments the accumulated error count for each of
// the given category IDs on the team entity. Uses UpdateWithRetry for atomic CAS.
func (h *Helper) IncrementTeamErrorCounts(ctx context.Context, teamID string, categories []workflow.ErrorCategory) error {
	entityID := TeamEntityID(teamID)

	err := h.kv.UpdateWithRetry(ctx, entityID, func(current []byte) ([]byte, error) {
		entity, unmarshalErr := unmarshalEntityState(current)
		if unmarshalErr != nil {
			return nil, fmt.Errorf("agentgraph: increment team error counts — get entity %q: %w", teamID, unmarshalErr)
		}

		// Scan in reverse so the most recently written triple wins.
		counts := map[string]int{}
		for i := len(entity.Triples) - 1; i >= 0; i-- {
			t := entity.Triples[i]
			if t.Predicate == PredicateTeamErrorCounts {
				if v, ok := t.Object.(string); ok {
					_ = json.Unmarshal([]byte(v), &counts)
				}
				break
			}
		}

		for _, cat := range categories {
			counts[string(cat)]++
		}

		data, err := json.Marshal(counts)
		if err != nil {
			return nil, fmt.Errorf("agentgraph: marshal error counts for team %q: %w", teamID, err)
		}

		now := time.Now()
		entity.Triples = replaceTriple(entity.Triples, PredicateTeamErrorCounts,
			propertyTriple(entityID, PredicateTeamErrorCounts, string(data), now))
		entity.UpdatedAt = now
		return json.Marshal(entity)
	})
	if err != nil {
		return fmt.Errorf("agentgraph: update error counts for team %q: %w", teamID, err)
	}
	return nil
}

// GetTeamForAgent reads the agent entity and returns the team ID stored in
// the agent.team.id predicate. Returns an empty string without error if the
// agent has no team assignment.
func (h *Helper) GetTeamForAgent(ctx context.Context, agentID string) (string, error) {
	entityID := AgentEntityID(agentID)

	entry, err := h.kv.Get(ctx, entityID)
	if err != nil {
		return "", fmt.Errorf("agentgraph: get team for agent %q: %w", agentID, err)
	}

	entity, err := unmarshalEntityState(entry.Value)
	if err != nil {
		return "", fmt.Errorf("agentgraph: get team for agent — unmarshal %q: %w", agentID, err)
	}

	for _, t := range entity.Triples {
		if t.Predicate == PredicateAgentTeamID {
			if v, ok := t.Object.(string); ok {
				return v, nil
			}
		}
	}
	return "", nil
}

// SetAgentTeam sets the agent.team.id predicate on the agent entity via
// UpdateWithRetry for CAS safety.
func (h *Helper) SetAgentTeam(ctx context.Context, agentID, teamID string) error {
	entityID := AgentEntityID(agentID)

	err := h.kv.UpdateWithRetry(ctx, entityID, func(current []byte) ([]byte, error) {
		entity, unmarshalErr := unmarshalEntityState(current)
		if unmarshalErr != nil {
			return nil, fmt.Errorf("agentgraph: set agent team — get entity %q: %w", agentID, unmarshalErr)
		}

		now := time.Now()
		entity.Triples = replaceTriple(entity.Triples, PredicateAgentTeamID,
			propertyTriple(entityID, PredicateAgentTeamID, teamID, now))
		entity.Triples = replaceTriple(entity.Triples, PredicateAgentUpdatedAt,
			propertyTriple(entityID, PredicateAgentUpdatedAt, now.Format(time.RFC3339), now))
		entity.UpdatedAt = now
		return json.Marshal(entity)
	})
	if err != nil {
		return fmt.Errorf("agentgraph: set agent team %q -> %q: %w", agentID, teamID, err)
	}
	return nil
}

// DefaultErrorTrendThreshold is the minimum occurrence count for an error
// category to be considered a trend. Categories with counts at or below this
// value are filtered out. Override per-call via GetAgentErrorTrendsWithThreshold.
const DefaultErrorTrendThreshold = 1

// GetAgentErrorTrends returns error trends using the DefaultErrorTrendThreshold.
// See GetAgentErrorTrendsWithThreshold for a configurable variant.
func (h *Helper) GetAgentErrorTrends(
	ctx context.Context,
	agentID string,
	registry *workflow.ErrorCategoryRegistry,
) ([]ErrorTrend, error) {
	return h.GetAgentErrorTrendsWithThreshold(ctx, agentID, registry, DefaultErrorTrendThreshold)
}

// GetAgentErrorTrendsWithThreshold returns a sorted list of error categories
// that have accumulated more than threshold occurrences for the given agent.
// Categories are resolved via registry; unrecognised category IDs are skipped.
// Results are sorted by count descending so callers can use the top-N entries.
func (h *Helper) GetAgentErrorTrendsWithThreshold(
	ctx context.Context,
	agentID string,
	registry *workflow.ErrorCategoryRegistry,
	threshold int,
) ([]ErrorTrend, error) {
	if threshold < 0 {
		threshold = 0
	}
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

	var trends []ErrorTrend
	for catID, count := range counts {
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
