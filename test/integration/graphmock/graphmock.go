//go:build integration

package graphmock

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	sgraph "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
)

// Ingest is an in-memory graph-ingest responder for integration tests that
// exercise components over real NATS without starting the full graph-ingest
// processor.
type Ingest struct {
	mu       sync.Mutex
	entities map[string]*sgraph.EntityState
}

// Start registers graph-ingest request/reply handlers on the supplied NATS
// client. The fake mirrors the mutation subjects used by semstreams
// graph-ingest: triple.add/triple.add_batch append facts, while
// update_with_triples replaces by predicate group.
func Start(t testing.TB, nc *natsclient.Client) *Ingest {
	t.Helper()
	m := &Ingest{entities: make(map[string]*sgraph.EntityState)}

	m.subscribe(t, nc, "graph.mutation.entity.update_with_triples", m.handleUpdateWithTriples)
	m.subscribe(t, nc, "graph.mutation.entity.create_with_triples", m.handleCreateWithTriples)
	m.subscribe(t, nc, "graph.mutation.triple.add", m.handleTripleAdd)
	m.subscribe(t, nc, "graph.mutation.triple.add_batch", m.handleTripleAddBatch)
	m.subscribe(t, nc, "graph.mutation.triple.remove", m.handleTripleRemove)
	m.subscribe(t, nc, "graph.ingest.query.entity", m.handleQueryEntity)
	m.subscribe(t, nc, "graph.ingest.query.prefix", m.handleQueryPrefix)

	if conn := nc.GetConnection(); conn != nil {
		if err := conn.Flush(); err != nil {
			t.Fatalf("flush graph mock subscriptions: %v", err)
		}
	}

	return m
}

// Entity returns a cloned entity so callers can inspect triples without racing
// concurrent NATS handlers.
func (m *Ingest) Entity(id string) (*sgraph.EntityState, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entity, ok := m.entities[id]
	if !ok {
		return nil, false
	}
	return cloneEntity(entity), true
}

// MustEntity returns a cloned entity or fails the test.
func (m *Ingest) MustEntity(t testing.TB, id string) *sgraph.EntityState {
	t.Helper()
	entity, ok := m.Entity(id)
	if !ok {
		t.Fatalf("entity %q not in graph mock", id)
	}
	return entity
}

// TripleValue converts the first object for predicate to a string, matching
// the convenience helpers that used to be duplicated by integration suites.
func TripleValue(triples []message.Triple, predicate string) string {
	for _, triple := range triples {
		if triple.Predicate != predicate {
			continue
		}
		if s, ok := triple.Object.(string); ok {
			return s
		}
		data, _ := json.Marshal(triple.Object)
		return string(data)
	}
	return ""
}

func (m *Ingest) subscribe(t testing.TB, nc *natsclient.Client, subject string, handler func(context.Context, []byte) ([]byte, error)) {
	t.Helper()
	if _, err := nc.SubscribeForRequests(context.Background(), subject, handler); err != nil {
		t.Fatalf("subscribe graph mock %s: %v", subject, err)
	}
}

func (m *Ingest) handleUpdateWithTriples(_ context.Context, data []byte) ([]byte, error) {
	var req sgraph.UpdateEntityWithTriplesRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return json.Marshal(updateWithTriplesResponse(false, err.Error(), sgraph.ErrorCodeInvalidRequest, nil, 0, 0))
	}
	if req.Entity == nil || req.Entity.ID == "" {
		return json.Marshal(updateWithTriplesResponse(false, "entity id required", sgraph.ErrorCodeInvalidRequest, nil, 0, 0))
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	entity, ok := m.entities[req.Entity.ID]
	if !ok {
		return json.Marshal(updateWithTriplesResponse(false, "entity not found", sgraph.ErrorCodeEntityNotFound, nil, 0, 0))
	}

	removed := removePredicatesFromEntity(entity, req.RemoveTriples)
	mergeTriplesIntoEntity(entity, req.AddTriples)
	entity.Version++
	entity.UpdatedAt = time.Now()

	return json.Marshal(updateWithTriplesResponse(true, "", "", entity, len(req.AddTriples), removed))
}

func (m *Ingest) handleCreateWithTriples(_ context.Context, data []byte) ([]byte, error) {
	var req sgraph.CreateEntityWithTriplesRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return json.Marshal(createWithTriplesResponse(false, err.Error(), sgraph.ErrorCodeInvalidRequest, nil, 0))
	}
	if req.Entity == nil || req.Entity.ID == "" {
		return json.Marshal(createWithTriplesResponse(false, "entity id required", sgraph.ErrorCodeInvalidRequest, nil, 0))
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.entities[req.Entity.ID]; exists {
		return json.Marshal(createWithTriplesResponse(false, "entity already exists", sgraph.ErrorCodeEntityExists, nil, 0))
	}

	entity := &sgraph.EntityState{
		ID:          req.Entity.ID,
		MessageType: req.Entity.MessageType,
		Triples:     append([]message.Triple(nil), req.Triples...),
		Version:     1,
		UpdatedAt:   time.Now(),
	}
	m.entities[req.Entity.ID] = entity

	return json.Marshal(createWithTriplesResponse(true, "", "", entity, len(req.Triples)))
}

func (m *Ingest) handleTripleAdd(_ context.Context, data []byte) ([]byte, error) {
	var req sgraph.AddTripleRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return json.Marshal(sgraph.AddTripleResponse{
			MutationResponse: mutationResponse(false, err.Error(), sgraph.ErrorCodeInvalidRequest, 0),
		})
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	entity := m.entityForWrite(req.Triple.Subject)
	entity.Triples = append(entity.Triples, req.Triple)
	entity.Version++
	entity.UpdatedAt = time.Now()

	return json.Marshal(sgraph.AddTripleResponse{
		MutationResponse: mutationResponse(true, "", "", uint64(entity.Version)),
		Triple:           &req.Triple,
	})
}

func (m *Ingest) handleTripleAddBatch(_ context.Context, data []byte) ([]byte, error) {
	var req sgraph.AddTriplesBatchRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return json.Marshal(sgraph.AddTriplesBatchResponse{
			MutationResponse: mutationResponse(false, err.Error(), sgraph.ErrorCodeInvalidRequest, 0),
		})
	}
	if len(req.Triples) == 0 {
		return json.Marshal(sgraph.AddTriplesBatchResponse{
			MutationResponse: mutationResponse(true, "", "", 0),
			WrittenCount:     0,
		})
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, triple := range req.Triples {
		entity := m.entityForWrite(triple.Subject)
		entity.Triples = append(entity.Triples, triple)
		entity.Version++
		entity.UpdatedAt = time.Now()
	}

	return json.Marshal(sgraph.AddTriplesBatchResponse{
		MutationResponse: mutationResponse(true, "", "", 0),
		WrittenCount:     len(req.Triples),
	})
}

func (m *Ingest) handleTripleRemove(_ context.Context, data []byte) ([]byte, error) {
	var req sgraph.RemoveTripleRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return json.Marshal(sgraph.RemoveTripleResponse{
			MutationResponse: mutationResponse(false, err.Error(), sgraph.ErrorCodeInvalidRequest, 0),
		})
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	entity, ok := m.entities[req.Subject]
	if !ok {
		return json.Marshal(sgraph.RemoveTripleResponse{
			MutationResponse: mutationResponse(true, "", "", 0),
			Removed:          false,
		})
	}
	removed := removePredicatesFromEntity(entity, []string{req.Predicate})
	if removed > 0 {
		entity.Version++
		entity.UpdatedAt = time.Now()
	}

	return json.Marshal(sgraph.RemoveTripleResponse{
		MutationResponse: mutationResponse(true, "", "", uint64(entity.Version)),
		Removed:          removed > 0,
	})
}

func (m *Ingest) handleQueryEntity(_ context.Context, data []byte) ([]byte, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, err
	}

	entity, ok := m.Entity(req.ID)
	if !ok {
		return nil, fmt.Errorf("not found: %s", req.ID)
	}
	return json.Marshal(entity)
}

func (m *Ingest) handleQueryPrefix(_ context.Context, data []byte) ([]byte, error) {
	var req struct {
		Prefix string `json:"prefix"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	var matches []sgraph.EntityState
	for id, entity := range m.entities {
		if !strings.HasPrefix(id, req.Prefix) {
			continue
		}
		matches = append(matches, *cloneEntity(entity))
		if req.Limit > 0 && len(matches) >= req.Limit {
			break
		}
	}

	return json.Marshal(map[string]any{"entities": matches})
}

func (m *Ingest) entityForWrite(id string) *sgraph.EntityState {
	entity, ok := m.entities[id]
	if !ok {
		entity = &sgraph.EntityState{
			ID:        id,
			UpdatedAt: time.Now(),
		}
		m.entities[id] = entity
	}
	return entity
}

func createWithTriplesResponse(success bool, errText, code string, entity *sgraph.EntityState, triplesAdded int) sgraph.CreateEntityWithTriplesResponse {
	return sgraph.CreateEntityWithTriplesResponse{
		MutationResponse: mutationResponse(success, errText, code, revision(entity)),
		Entity:           cloneEntity(entity),
		TriplesAdded:     triplesAdded,
	}
}

func updateWithTriplesResponse(success bool, errText, code string, entity *sgraph.EntityState, triplesAdded, triplesRemoved int) sgraph.UpdateEntityWithTriplesResponse {
	return sgraph.UpdateEntityWithTriplesResponse{
		MutationResponse: mutationResponse(success, errText, code, revision(entity)),
		Entity:           cloneEntity(entity),
		TriplesAdded:     triplesAdded,
		TriplesRemoved:   triplesRemoved,
		Version:          int64(revision(entity)),
	}
}

func mutationResponse(success bool, errText, code string, kvRevision uint64) sgraph.MutationResponse {
	return sgraph.MutationResponse{
		Success:    success,
		Error:      errText,
		ErrorCode:  code,
		Timestamp:  time.Now().UnixNano(),
		KVRevision: kvRevision,
	}
}

func revision(entity *sgraph.EntityState) uint64 {
	if entity == nil {
		return 0
	}
	return uint64(entity.Version)
}

func removePredicatesFromEntity(entity *sgraph.EntityState, predicates []string) int {
	if entity == nil || len(predicates) == 0 || len(entity.Triples) == 0 {
		return 0
	}
	remove := make(map[string]struct{}, len(predicates))
	for _, predicate := range predicates {
		remove[predicate] = struct{}{}
	}
	kept := entity.Triples[:0]
	removed := 0
	for _, triple := range entity.Triples {
		if _, ok := remove[triple.Predicate]; ok {
			removed++
			continue
		}
		kept = append(kept, triple)
	}
	entity.Triples = kept
	return removed
}

func mergeTriplesIntoEntity(entity *sgraph.EntityState, triples []message.Triple) {
	if entity == nil || len(triples) == 0 {
		return
	}
	byPredicate := make(map[string][]message.Triple)
	for _, triple := range triples {
		byPredicate[triple.Predicate] = append(byPredicate[triple.Predicate], triple)
	}
	kept := entity.Triples[:0]
	for _, triple := range entity.Triples {
		if _, replacing := byPredicate[triple.Predicate]; replacing {
			continue
		}
		kept = append(kept, triple)
	}
	entity.Triples = kept
	for _, group := range byPredicate {
		entity.Triples = append(entity.Triples, group...)
	}
}

func cloneEntity(entity *sgraph.EntityState) *sgraph.EntityState {
	if entity == nil {
		return nil
	}
	clone := *entity
	clone.Triples = append([]message.Triple(nil), entity.Triples...)
	return &clone
}
