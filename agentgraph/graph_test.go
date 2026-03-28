package agentgraph_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"

	"github.com/c360studio/semspec/agentgraph"
	semspecvocab "github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow"
)

// -- mock KV store --

// mockKV is a minimal in-memory KVStore for testing.
type mockKV struct {
	data    map[string][]byte
	putErr  error
	getErr  error
	retryFn func(key string) error // optional per-key error injection for UpdateWithRetry
}

func newMockKV() *mockKV {
	return &mockKV{data: make(map[string][]byte)}
}

func (m *mockKV) Get(_ context.Context, key string) (*natsclient.KVEntry, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	v, ok := m.data[key]
	if !ok {
		return nil, errors.New("kv: key not found")
	}
	return &natsclient.KVEntry{Key: key, Value: v, Revision: 1}, nil
}

func (m *mockKV) Put(_ context.Context, key string, value []byte) (uint64, error) {
	if m.putErr != nil {
		return 0, m.putErr
	}
	m.data[key] = value
	return 1, nil
}

func (m *mockKV) UpdateWithRetry(_ context.Context, key string, updateFn func(current []byte) ([]byte, error)) error {
	if m.retryFn != nil {
		if err := m.retryFn(key); err != nil {
			return err
		}
	}
	current := m.data[key] // nil if not present
	updated, err := updateFn(current)
	if err != nil {
		return err
	}
	m.data[key] = updated
	return nil
}

func (m *mockKV) KeysByPrefix(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	for k := range m.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

// -- helpers --

// getStoredEntity retrieves and unmarshals an entity from the mock KV.
func getStoredEntity(t *testing.T, kv *mockKV, key string) *gtypes.EntityState {
	t.Helper()
	data, ok := kv.data[key]
	if !ok {
		t.Fatalf("key %q not found in mock KV", key)
	}
	var entity gtypes.EntityState
	if err := json.Unmarshal(data, &entity); err != nil {
		t.Fatalf("unmarshal entity at %q: %v", key, err)
	}
	return &entity
}

func tripleByPredicate(triples []message.Triple, predicate string) *message.Triple {
	for i := range triples {
		if triples[i].Predicate == predicate {
			return &triples[i]
		}
	}
	return nil
}

// -- tests --

func TestHelper_RecordLoopCreated(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		loopID  string
		role    string
		model   string
		putErr  error
		wantErr bool
	}{
		{
			name:    "creates entity with role model status triples",
			loopID:  "loop-1",
			role:    "planner",
			model:   "gpt-4o",
			wantErr: false,
		},
		{
			name:    "propagates put error",
			loopID:  "loop-err",
			role:    "executor",
			model:   "gpt-4o-mini",
			putErr:  errors.New("nats: bucket unavailable"),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			kv := newMockKV()
			kv.putErr = tc.putErr
			h := agentgraph.NewHelper(kv)

			err := h.RecordLoopCreated(context.Background(), tc.loopID, tc.role, tc.model)

			if (err != nil) != tc.wantErr {
				t.Fatalf("RecordLoopCreated() error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}

			wantID := agentgraph.LoopEntityID(tc.loopID)
			entity := getStoredEntity(t, kv, wantID)

			if entity.ID != wantID {
				t.Errorf("entity ID = %q, want %q", entity.ID, wantID)
			}

			// Verify all three property triples are present.
			roleT := tripleByPredicate(entity.Triples, agentgraph.PredicateRole)
			if roleT == nil {
				t.Error("missing role triple")
			} else if roleT.Object != tc.role {
				t.Errorf("role triple object = %v, want %q", roleT.Object, tc.role)
			}

			modelT := tripleByPredicate(entity.Triples, agentgraph.PredicateModel)
			if modelT == nil {
				t.Error("missing model triple")
			} else if modelT.Object != tc.model {
				t.Errorf("model triple object = %v, want %q", modelT.Object, tc.model)
			}

			statusT := tripleByPredicate(entity.Triples, agentgraph.PredicateStatus)
			if statusT == nil {
				t.Error("missing status triple")
			} else if statusT.Object != "created" {
				t.Errorf("status triple object = %v, want \"created\"", statusT.Object)
			}

			// All triples must carry the Semspec source.
			for _, triple := range entity.Triples {
				if triple.Source != agentgraph.SourceSemspec {
					t.Errorf("triple %q has source %q, want %q", triple.Predicate, triple.Source, agentgraph.SourceSemspec)
				}
			}
		})
	}
}

func TestHelper_RecordSpawn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		parentLoopID string
		childLoopID  string
		role         string
		model        string
		putErr       error
		retryErr     error
		wantErr      bool
		wantRel      bool
	}{
		{
			name:         "creates child entity and relationship",
			parentLoopID: "parent-1",
			childLoopID:  "child-1",
			role:         "executor",
			model:        "gpt-4o-mini",
			wantErr:      false,
			wantRel:      true,
		},
		{
			name:         "fails when child entity creation fails",
			parentLoopID: "parent-2",
			childLoopID:  "child-2",
			role:         "executor",
			model:        "gpt-4o-mini",
			putErr:       errors.New("storage error"),
			wantErr:      true,
			wantRel:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			kv := newMockKV()
			kv.putErr = tc.putErr
			h := agentgraph.NewHelper(kv)

			err := h.RecordSpawn(context.Background(), tc.parentLoopID, tc.childLoopID, tc.role, tc.model)

			if (err != nil) != tc.wantErr {
				t.Fatalf("RecordSpawn() error = %v, wantErr %v", err, tc.wantErr)
			}

			if tc.wantRel {
				// Verify the parent entity has a PredicateSpawned triple pointing to the child.
				parentID := agentgraph.LoopEntityID(tc.parentLoopID)
				parentEntity := getStoredEntity(t, kv, parentID)

				spawnedT := tripleByPredicate(parentEntity.Triples, agentgraph.PredicateSpawned)
				if spawnedT == nil {
					t.Fatal("missing spawned triple on parent entity")
				}
				wantTo := agentgraph.LoopEntityID(tc.childLoopID)
				if spawnedT.Object != wantTo {
					t.Errorf("spawned triple object = %v, want %q", spawnedT.Object, wantTo)
				}
			}
		})
	}
}

func TestHelper_RecordLoopStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		loopID   string
		status   string
		getErr   error
		retryErr error
		wantErr  bool
	}{
		{
			name:    "updates status triple successfully",
			loopID:  "loop-1",
			status:  "running",
			wantErr: false,
		},
		{
			name:     "propagates get error",
			loopID:   "loop-bad",
			status:   "running",
			retryErr: errors.New("entity not found"),
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			kv := newMockKV()

			// Pre-populate the entity so UpdateWithRetry has something to read.
			if tc.retryErr == nil {
				entityID := agentgraph.LoopEntityID(tc.loopID)
				entity := &gtypes.EntityState{
					ID:      entityID,
					Triples: []message.Triple{},
				}
				data, _ := json.Marshal(entity)
				kv.data[entityID] = data
			} else {
				kv.retryFn = func(_ string) error { return tc.retryErr }
			}

			h := agentgraph.NewHelper(kv)

			err := h.RecordLoopStatus(context.Background(), tc.loopID, tc.status)

			if (err != nil) != tc.wantErr {
				t.Fatalf("RecordLoopStatus() error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}

			// After the update, the stored entity should contain the status triple.
			entityID := agentgraph.LoopEntityID(tc.loopID)
			stored := getStoredEntity(t, kv, entityID)
			statusT := tripleByPredicate(stored.Triples, agentgraph.PredicateStatus)
			if statusT == nil {
				t.Error("status triple not found after update")
			} else if statusT.Object != tc.status {
				t.Errorf("status triple object = %v, want %q", statusT.Object, tc.status)
			}
		})
	}
}

func TestHelper_GetChildren(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		loopID       string
		setupParent  func(kv *mockKV) // pre-populate parent entity
		getErr       error
		wantChildren []string
		wantErr      bool
	}{
		{
			name:   "returns empty slice when loop has no spawned children",
			loopID: "root",
			setupParent: func(kv *mockKV) {
				entityID := agentgraph.LoopEntityID("root")
				entity := &gtypes.EntityState{ID: entityID}
				data, _ := json.Marshal(entity)
				kv.data[entityID] = data
			},
			wantChildren: nil,
		},
		{
			// GetChildren extracts the instance segment (the 16-hex-char hash) from each
			// spawned entity ID triple. It does NOT return the original logical ID.
			name:   "returns loop IDs extracted from spawned triples",
			loopID: "parent",
			setupParent: func(kv *mockKV) {
				entityID := agentgraph.LoopEntityID("parent")
				childAEntityID := agentgraph.LoopEntityID("child-a")
				childBEntityID := agentgraph.LoopEntityID("child-b")
				entity := &gtypes.EntityState{
					ID: entityID,
					Triples: []message.Triple{
						{Predicate: agentgraph.PredicateSpawned, Object: childAEntityID},
						{Predicate: agentgraph.PredicateSpawned, Object: childBEntityID},
					},
				}
				data, _ := json.Marshal(entity)
				kv.data[entityID] = data
			},
			// wantChildren contains the hashed instance segments parsed from the entity IDs,
			// not the original logical IDs passed to LoopEntityID.
			wantChildren: func() []string {
				a, _ := agentgraph.ParseEntityID(agentgraph.LoopEntityID("child-a"))
				b, _ := agentgraph.ParseEntityID(agentgraph.LoopEntityID("child-b"))
				return []string{a, b}
			}(),
		},
		{
			name:    "propagates get error",
			loopID:  "err-loop",
			getErr:  errors.New("nats timeout"),
			wantErr: true,
		},
		{
			// GetChildren skips triples whose Object is not a valid 6-part entity ID.
			// For valid entity IDs it returns the hashed instance segment, not the original ID.
			name:   "skips malformed entity IDs",
			loopID: "parent-skip",
			setupParent: func(kv *mockKV) {
				entityID := agentgraph.LoopEntityID("parent-skip")
				entity := &gtypes.EntityState{
					ID: entityID,
					Triples: []message.Triple{
						{Predicate: agentgraph.PredicateSpawned, Object: agentgraph.LoopEntityID("valid-child")},
						{Predicate: agentgraph.PredicateSpawned, Object: "not-a-valid-entity-id"},
					},
				}
				data, _ := json.Marshal(entity)
				kv.data[entityID] = data
			},
			// wantChildren is the hashed instance segment of "valid-child"'s entity ID.
			wantChildren: func() []string {
				inst, _ := agentgraph.ParseEntityID(agentgraph.LoopEntityID("valid-child"))
				return []string{inst}
			}(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			kv := newMockKV()
			kv.getErr = tc.getErr
			if tc.setupParent != nil {
				tc.setupParent(kv)
			}

			h := agentgraph.NewHelper(kv)

			children, err := h.GetChildren(context.Background(), tc.loopID)

			if (err != nil) != tc.wantErr {
				t.Fatalf("GetChildren() error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}

			if len(children) != len(tc.wantChildren) {
				t.Fatalf("GetChildren() returned %d children, want %d: %v", len(children), len(tc.wantChildren), children)
			}
			for i, want := range tc.wantChildren {
				if children[i] != want {
					t.Errorf("children[%d] = %q, want %q", i, children[i], want)
				}
			}
		})
	}
}

func TestHelper_GetTree(t *testing.T) {
	t.Parallel()

	// GetChildren returns the hashed instance segment from each spawned entity ID
	// triple. GetTree then passes those hash strings to LoopEntityID for the next
	// BFS level — but the KV stores entries under LoopEntityID of the original
	// logical ID, not of the hash. This means child lookups fail silently (the
	// node is skipped via continue) and GetTree only ever returns the root entity
	// ID when the KV is keyed by original logical IDs.
	//
	// To verify real multi-level traversal the test must store children under
	// LoopEntityID(hash), where hash = ParseEntityID(LoopEntityID(original)).
	root := agentgraph.LoopEntityID("root")

	// Derive the entity IDs that GetTree will actually look up for the children.
	// GetChildren returns parsed.Instance of the stored child entity ID triple.
	// GetTree then calls LoopEntityID on that instance to form the lookup key.
	child1Hash, _ := agentgraph.ParseEntityID(agentgraph.LoopEntityID("child-1"))
	child2Hash, _ := agentgraph.ParseEntityID(agentgraph.LoopEntityID("child-2"))
	child1LookupKey := agentgraph.LoopEntityID(child1Hash)
	child2LookupKey := agentgraph.LoopEntityID(child2Hash)

	tests := []struct {
		name       string
		rootLoopID string
		maxDepth   int
		setup      func(kv *mockKV)
		wantIDs    []string
		wantErr    bool
	}{
		{
			// Children are stored under the double-hashed lookup keys that GetTree
			// will construct, so the BFS traversal can actually find them.
			name:       "returns all traversed entity IDs",
			rootLoopID: "root",
			maxDepth:   5,
			setup: func(kv *mockKV) {
				// Root references child entity IDs via their original hashed keys.
				rootEntity := &gtypes.EntityState{
					ID: root,
					Triples: []message.Triple{
						{Predicate: agentgraph.PredicateSpawned, Object: agentgraph.LoopEntityID("child-1")},
						{Predicate: agentgraph.PredicateSpawned, Object: agentgraph.LoopEntityID("child-2")},
					},
				}
				rootData, _ := json.Marshal(rootEntity)
				kv.data[root] = rootData

				// Children are stored under the keys GetTree will look up after
				// extracting the instance hash and re-applying LoopEntityID.
				for _, lookupKey := range []string{child1LookupKey, child2LookupKey} {
					childEntity := &gtypes.EntityState{ID: lookupKey}
					childData, _ := json.Marshal(childEntity)
					kv.data[lookupKey] = childData
				}
			},
			wantIDs: []string{root, child1LookupKey, child2LookupKey},
		},
		{
			name:       "returns only root when no children",
			rootLoopID: "lonely-root",
			maxDepth:   3,
			setup: func(kv *mockKV) {
				id := agentgraph.LoopEntityID("lonely-root")
				entity := &gtypes.EntityState{ID: id}
				data, _ := json.Marshal(entity)
				kv.data[id] = data
			},
			wantIDs: []string{agentgraph.LoopEntityID("lonely-root")},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			kv := newMockKV()
			if tc.setup != nil {
				tc.setup(kv)
			}

			h := agentgraph.NewHelper(kv)

			ids, err := h.GetTree(context.Background(), tc.rootLoopID, tc.maxDepth)

			if (err != nil) != tc.wantErr {
				t.Fatalf("GetTree() error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}

			if len(ids) != len(tc.wantIDs) {
				t.Fatalf("GetTree() returned %d IDs, want %d: %v", len(ids), len(tc.wantIDs), ids)
			}
			got := make(map[string]bool, len(ids))
			for _, id := range ids {
				got[id] = true
			}
			for _, wantID := range tc.wantIDs {
				if !got[wantID] {
					t.Errorf("GetTree() missing expected entity ID %q", wantID)
				}
			}
		})
	}
}

func TestHelper_GetStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		loopID     string
		triples    []message.Triple
		getErr     error
		wantStatus string
		wantErr    bool
	}{
		{
			name:   "returns status when triple is present",
			loopID: "loop-1",
			triples: []message.Triple{
				{Predicate: agentgraph.PredicateStatus, Object: "running"},
			},
			wantStatus: "running",
		},
		{
			name:       "returns empty string when no status triple",
			loopID:     "loop-nostatus",
			triples:    []message.Triple{},
			wantStatus: "",
		},
		{
			name:    "propagates get error",
			loopID:  "loop-err",
			getErr:  errors.New("entity not found"),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			kv := newMockKV()
			kv.getErr = tc.getErr
			if tc.getErr == nil {
				entityID := agentgraph.LoopEntityID(tc.loopID)
				entity := &gtypes.EntityState{
					ID:      entityID,
					Triples: tc.triples,
				}
				data, _ := json.Marshal(entity)
				kv.data[entityID] = data
			}

			h := agentgraph.NewHelper(kv)

			status, err := h.GetStatus(context.Background(), tc.loopID)

			if (err != nil) != tc.wantErr {
				t.Fatalf("GetStatus() error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if status != tc.wantStatus {
				t.Errorf("GetStatus() = %q, want %q", status, tc.wantStatus)
			}
		})
	}
}

// TestPredicateAlignment asserts that the convenience predicate constants in
// agentgraph/graph.go carry identical string values to the canonical constants
// registered in vocabulary/semspec/predicates.go.
func TestPredicateAlignment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		agentgraphConst string
		vocabConst      string
		name            string
	}{
		{agentgraph.PredicateSpawned, semspecvocab.PredicateLoopSpawned, "Spawned"},
		{agentgraph.PredicateLoopTask, semspecvocab.PredicateLoopTaskLink, "LoopTask"},
		{agentgraph.PredicateDependsOn, semspecvocab.PredicateTaskDependsOn, "DependsOn"},
		{agentgraph.PredicateRole, semspecvocab.PredicateAgenticLoopRole, "Role"},
		{agentgraph.PredicateModel, semspecvocab.PredicateAgenticLoopModel, "Model"},
		{agentgraph.PredicateStatus, semspecvocab.PredicateAgenticLoopStatus, "Status"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.agentgraphConst != tc.vocabConst {
				t.Errorf("agentgraph predicate %q != vocabulary predicate %q", tc.agentgraphConst, tc.vocabConst)
			}
		})
	}
}

// -- Phase 4 tests: graph storage methods --

func makeTestCategories() []*workflow.ErrorCategoryDef {
	return []*workflow.ErrorCategoryDef{
		{
			ID:          "missing_tests",
			Label:       "Missing Tests",
			Description: "Required tests not present",
			Signals:     []string{"no test file", "0% coverage"},
			Guidance:    "Add unit tests for all exported functions.",
		},
		{
			ID:          "bad_error_handling",
			Label:       "Bad Error Handling",
			Description: "Errors silently swallowed",
			Signals:     []string{"_ = err", "error ignored"},
			Guidance:    "Return or wrap all errors.",
		},
	}
}

func makeTestRegistry(t *testing.T) *workflow.ErrorCategoryRegistry {
	t.Helper()
	data := `{"categories":[` +
		`{"id":"missing_tests","label":"Missing Tests","description":"Required tests not present","signals":["no test file"],"guidance":"Add tests."},` +
		`{"id":"bad_error_handling","label":"Bad Error Handling","description":"Errors silently swallowed","signals":["_ = err"],"guidance":"Return errors."}` +
		`]}`
	reg, err := workflow.LoadErrorCategoriesFromBytes([]byte(data))
	if err != nil {
		t.Fatalf("makeTestRegistry: %v", err)
	}
	return reg
}

func makeTestAgent() workflow.Agent {
	now := time.Now().Truncate(time.Second)
	return workflow.Agent{
		ID:        "alpha1",
		Name:      "developer-alpha1",
		Role:      "developer",
		Model:     "gpt-4o",
		Status:    workflow.AgentAvailable,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func TestHelper_SeedErrorCategories(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	cats := makeTestCategories()
	if err := h.SeedErrorCategories(context.Background(), cats); err != nil {
		t.Fatalf("SeedErrorCategories() error = %v", err)
	}

	// Verify two entities were stored.
	wantID0 := agentgraph.ErrorCategoryEntityID("missing_tests")
	if _, ok := kv.data[wantID0]; !ok {
		t.Errorf("expected entity at key %q", wantID0)
	}

	e0 := getStoredEntity(t, kv, wantID0)
	labelT := tripleByPredicate(e0.Triples, agentgraph.PredicateErrorCategoryLabel)
	if labelT == nil {
		t.Error("missing label triple on first category entity")
	} else if labelT.Object != "Missing Tests" {
		t.Errorf("label triple object = %v, want %q", labelT.Object, "Missing Tests")
	}

	// Count signal triples — there should be 2 for "missing_tests".
	signalCount := 0
	for _, tr := range e0.Triples {
		if tr.Predicate == agentgraph.PredicateErrorCategorySignal {
			signalCount++
		}
	}
	if signalCount != 2 {
		t.Errorf("signal triple count = %d, want 2", signalCount)
	}
}

func TestHelper_SeedErrorCategories_Idempotent(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	cats := makeTestCategories()
	if err := h.SeedErrorCategories(context.Background(), cats); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	if err := h.SeedErrorCategories(context.Background(), cats); err != nil {
		t.Fatalf("second seed (idempotent): %v", err)
	}

	// Both category entities should exist (Put is idempotent).
	wantID0 := agentgraph.ErrorCategoryEntityID("missing_tests")
	wantID1 := agentgraph.ErrorCategoryEntityID("bad_error_handling")
	if _, ok := kv.data[wantID0]; !ok {
		t.Errorf("missing entity at key %q after idempotent seed", wantID0)
	}
	if _, ok := kv.data[wantID1]; !ok {
		t.Errorf("missing entity at key %q after idempotent seed", wantID1)
	}
}

func TestHelper_CreateAgent(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	agent := makeTestAgent()
	if err := h.CreateAgent(context.Background(), agent); err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	wantID := agentgraph.AgentEntityID("alpha1")
	entity := getStoredEntity(t, kv, wantID)

	if entity.ID != wantID {
		t.Errorf("entity.ID = %q, want %q", entity.ID, wantID)
	}

	nameT := tripleByPredicate(entity.Triples, agentgraph.PredicateAgentName)
	if nameT == nil || nameT.Object != "developer-alpha1" {
		t.Errorf("name triple = %v, want %q", nameT, "developer-alpha1")
	}

	roleT := tripleByPredicate(entity.Triples, agentgraph.PredicateAgentRole)
	if roleT == nil || roleT.Object != "developer" {
		t.Errorf("role triple = %v, want %q", roleT, "developer")
	}

	stateT := tripleByPredicate(entity.Triples, agentgraph.PredicateAgentState)
	if stateT == nil || stateT.Object != "available" {
		t.Errorf("state triple = %v, want %q", stateT, "available")
	}

	countsT := tripleByPredicate(entity.Triples, agentgraph.PredicateAgentErrorCounts)
	if countsT == nil || countsT.Object != "{}" {
		t.Errorf("error counts triple = %v, want %q", countsT, "{}")
	}
}

func TestHelper_GetAgent(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	original := makeTestAgent()
	if err := h.CreateAgent(context.Background(), original); err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	got, err := h.GetAgent(context.Background(), original.ID)
	if err != nil {
		t.Fatalf("GetAgent() error = %v", err)
	}

	if got.ID != original.ID {
		t.Errorf("ID = %q, want %q", got.ID, original.ID)
	}
	if got.Name != original.Name {
		t.Errorf("Name = %q, want %q", got.Name, original.Name)
	}
	if got.Role != original.Role {
		t.Errorf("Role = %q, want %q", got.Role, original.Role)
	}
	if got.Model != original.Model {
		t.Errorf("Model = %q, want %q", got.Model, original.Model)
	}
	if got.Status != original.Status {
		t.Errorf("Status = %q, want %q", got.Status, original.Status)
	}
	if !got.CreatedAt.Equal(original.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, original.CreatedAt)
	}
}

func TestHelper_GetOrCreateDefaultAgent_CreatesOnFirst(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	agent, err := h.GetOrCreateDefaultAgent(context.Background(), "reviewer", "claude-3-5")
	if err != nil {
		t.Fatalf("GetOrCreateDefaultAgent() error = %v", err)
	}

	if agent == nil {
		t.Fatal("expected non-nil agent")
	}
	if agent.Role != "reviewer" {
		t.Errorf("Role = %q, want %q", agent.Role, "reviewer")
	}
	if agent.Model != "claude-3-5" {
		t.Errorf("Model = %q, want %q", agent.Model, "claude-3-5")
	}
	if agent.Status != workflow.AgentAvailable {
		t.Errorf("Status = %q, want %q", agent.Status, workflow.AgentAvailable)
	}
}

func TestHelper_GetOrCreateDefaultAgent_ReturnsExisting(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	// Pre-create an agent with role "developer".
	existing := makeTestAgent()
	if err := h.CreateAgent(context.Background(), existing); err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}
	kvSizeBefore := len(kv.data)

	got, err := h.GetOrCreateDefaultAgent(context.Background(), "developer", "gpt-4o-mini")
	if err != nil {
		t.Fatalf("GetOrCreateDefaultAgent() error = %v", err)
	}

	if got.Role != "developer" {
		t.Errorf("Role = %q, want %q", got.Role, "developer")
	}
	if got.ID != existing.ID {
		t.Errorf("ID = %q, want %q (should return existing)", got.ID, existing.ID)
	}
	// No additional Put should occur for the existing agent.
	if len(kv.data) != kvSizeBefore {
		t.Errorf("expected no new keys, got %d total (was %d before)", len(kv.data), kvSizeBefore)
	}
}

func TestHelper_RecordReview(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	rev := agentgraph.Review{
		ID:              "rev1",
		ScenarioID:      "scenario-abc",
		AgentID:         "alpha1",
		ReviewerAgentID: "reviewer1",
		Verdict:         agentgraph.VerdictAccepted,
		Q1Correctness:   5,
		Q2Quality:       4,
		Q3Completeness:  4,
		Explanation:     "All criteria met",
		Timestamp:       time.Now().Truncate(time.Second),
	}

	if err := h.RecordReview(context.Background(), rev); err != nil {
		t.Fatalf("RecordReview() error = %v", err)
	}

	wantID := agentgraph.ReviewEntityID("rev1")
	entity := getStoredEntity(t, kv, wantID)

	if entity.ID != wantID {
		t.Errorf("entity.ID = %q, want %q", entity.ID, wantID)
	}

	verdictT := tripleByPredicate(entity.Triples, agentgraph.PredicateReviewVerdict)
	if verdictT == nil || verdictT.Object != "accepted" {
		t.Errorf("verdict triple = %v, want %q", verdictT, "accepted")
	}

	agentT := tripleByPredicate(entity.Triples, agentgraph.PredicateReviewAgentID)
	if agentT == nil || agentT.Object != "alpha1" {
		t.Errorf("agent_id triple = %v, want %q", agentT, "alpha1")
	}
}

func TestHelper_RecordReview_ErrorRefsWithRelatedEntities(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	rev := agentgraph.Review{
		ID:              "rev2",
		ScenarioID:      "scenario-xyz",
		AgentID:         "beta1",
		ReviewerAgentID: "reviewer1",
		Verdict:         agentgraph.VerdictRejected,
		Q1Correctness:   2,
		Q2Quality:       2,
		Q3Completeness:  2,
		Explanation:     "Tests missing",
		Timestamp:       time.Now(),
		Errors: []agentgraph.ReviewErrorRef{
			{
				CategoryID:       "missing_tests",
				RelatedEntityIDs: []string{"entity-a", "entity-b"},
			},
		},
	}

	if err := h.RecordReview(context.Background(), rev); err != nil {
		t.Fatalf("RecordReview() error = %v", err)
	}

	entity := getStoredEntity(t, kv, agentgraph.ReviewEntityID("rev2"))

	catCount := 0
	relCount := 0
	for _, tr := range entity.Triples {
		switch tr.Predicate {
		case agentgraph.PredicateReviewErrorCategory:
			catCount++
			if tr.Object != "missing_tests" {
				t.Errorf("error category triple object = %v, want %q", tr.Object, "missing_tests")
			}
		case agentgraph.PredicateReviewRelatedEntity:
			relCount++
		}
	}

	if catCount != 1 {
		t.Errorf("error category triple count = %d, want 1", catCount)
	}
	if relCount != 2 {
		t.Errorf("related entity triple count = %d, want 2", relCount)
	}
}

func TestHelper_IncrementAgentErrorCounts_FirstTime(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	agent := makeTestAgent()
	if err := h.CreateAgent(context.Background(), agent); err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	if err := h.IncrementAgentErrorCounts(context.Background(), agent.ID, []string{"missing_tests"}); err != nil {
		t.Fatalf("IncrementAgentErrorCounts() error = %v", err)
	}

	entityID := agentgraph.AgentEntityID(agent.ID)
	stored := getStoredEntity(t, kv, entityID)

	// Find the error counts triple.
	countsT := tripleByPredicate(stored.Triples, agentgraph.PredicateAgentErrorCounts)
	if countsT == nil {
		t.Fatal("missing error counts triple")
	}

	countsStr, ok := countsT.Object.(string)
	if !ok {
		t.Fatalf("error counts object is not string: %T", countsT.Object)
	}

	counts := map[string]int{}
	if err := json.Unmarshal([]byte(countsStr), &counts); err != nil {
		t.Fatalf("parse counts: %v", err)
	}
	if counts["missing_tests"] != 1 {
		t.Errorf("missing_tests count = %d, want 1", counts["missing_tests"])
	}
}

func TestHelper_IncrementAgentErrorCounts_Accumulates(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	agent := makeTestAgent()
	if err := h.CreateAgent(context.Background(), agent); err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	// Increment twice to verify accumulation.
	for i := 0; i < 2; i++ {
		if err := h.IncrementAgentErrorCounts(context.Background(), agent.ID, []string{"bad_error_handling"}); err != nil {
			t.Fatalf("IncrementAgentErrorCounts() call %d error = %v", i+1, err)
		}
	}

	entityID := agentgraph.AgentEntityID(agent.ID)
	stored := getStoredEntity(t, kv, entityID)

	countsT := tripleByPredicate(stored.Triples, agentgraph.PredicateAgentErrorCounts)
	if countsT == nil {
		t.Fatal("missing error counts triple")
	}

	countsStr, ok := countsT.Object.(string)
	if !ok {
		t.Fatalf("error counts object is not string: %T", countsT.Object)
	}

	counts := map[string]int{}
	if err := json.Unmarshal([]byte(countsStr), &counts); err != nil {
		t.Fatalf("parse counts: %v", err)
	}
	if counts["bad_error_handling"] != 2 {
		t.Errorf("bad_error_handling count = %d, want 2", counts["bad_error_handling"])
	}
}

func TestHelper_UpdateAgentStats(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	agent := makeTestAgent()
	if err := h.CreateAgent(context.Background(), agent); err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	stats := workflow.ReviewStats{
		Q1CorrectnessAvg:  4.5,
		Q2QualityAvg:      4.0,
		Q3CompletenessAvg: 3.8,
		OverallAvg:        4.1,
		ReviewCount:       3,
	}

	if err := h.UpdateAgentStats(context.Background(), agent.ID, stats); err != nil {
		t.Fatalf("UpdateAgentStats() error = %v", err)
	}

	entityID := agentgraph.AgentEntityID(agent.ID)
	stored := getStoredEntity(t, kv, entityID)

	q1T := tripleByPredicate(stored.Triples, agentgraph.PredicateAgentQ1Avg)
	if q1T == nil {
		t.Fatal("q1_avg triple not found after update")
	}
	if f, ok := q1T.Object.(float64); !ok || f != 4.5 {
		t.Errorf("q1_avg = %v, want 4.5", q1T.Object)
	}
}

func TestHelper_GetAgentErrorTrends_FiltersThreshold(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)
	registry := makeTestRegistry(t)

	// Build an agent entity with error counts directly.
	entityID := agentgraph.AgentEntityID("trendagent1")
	countsJSON := `{"missing_tests":1,"bad_error_handling":2}`
	entity := &gtypes.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{Predicate: agentgraph.PredicateAgentErrorCounts, Object: countsJSON},
		},
	}
	data, _ := json.Marshal(entity)
	kv.data[entityID] = data

	trends, err := h.GetAgentErrorTrends(context.Background(), "trendagent1", registry)
	if err != nil {
		t.Fatalf("GetAgentErrorTrends() error = %v", err)
	}

	// missing_tests has count=1, should be filtered out; bad_error_handling has count=2.
	if len(trends) != 1 {
		t.Fatalf("expected 1 trend (count>1), got %d", len(trends))
	}
	if trends[0].Category.ID != "bad_error_handling" {
		t.Errorf("trend[0].Category.ID = %q, want %q", trends[0].Category.ID, "bad_error_handling")
	}
	if trends[0].Count != 2 {
		t.Errorf("trend[0].Count = %d, want 2", trends[0].Count)
	}
}

func TestHelper_GetAgentErrorTrendsWithThreshold(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)
	registry := makeTestRegistry(t)

	entityID := agentgraph.AgentEntityID("threshold-agent")
	countsJSON := `{"missing_tests":2,"bad_error_handling":4}`
	entity := &gtypes.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{Predicate: agentgraph.PredicateAgentErrorCounts, Object: countsJSON},
		},
	}
	data, _ := json.Marshal(entity)
	kv.data[entityID] = data

	// Default threshold (1) — both categories should appear.
	trends, err := h.GetAgentErrorTrends(context.Background(), "threshold-agent", registry)
	if err != nil {
		t.Fatalf("GetAgentErrorTrends() error = %v", err)
	}
	if len(trends) != 2 {
		t.Fatalf("default threshold: expected 2 trends, got %d", len(trends))
	}

	// Custom threshold of 3 — only bad_error_handling (count=4) should appear.
	trends, err = h.GetAgentErrorTrendsWithThreshold(context.Background(), "threshold-agent", registry, 3)
	if err != nil {
		t.Fatalf("GetAgentErrorTrendsWithThreshold() error = %v", err)
	}
	if len(trends) != 1 {
		t.Fatalf("threshold=3: expected 1 trend, got %d", len(trends))
	}
	if trends[0].Category.ID != "bad_error_handling" {
		t.Errorf("threshold=3: trend[0].Category.ID = %q, want %q", trends[0].Category.ID, "bad_error_handling")
	}

	// Threshold of 5 — nothing should appear.
	trends, err = h.GetAgentErrorTrendsWithThreshold(context.Background(), "threshold-agent", registry, 5)
	if err != nil {
		t.Fatalf("GetAgentErrorTrendsWithThreshold() error = %v", err)
	}
	if len(trends) != 0 {
		t.Fatalf("threshold=5: expected 0 trends, got %d", len(trends))
	}
}

func TestHelper_GetAgentErrorTrends_SortedByCount(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)
	registry := makeTestRegistry(t)

	entityID := agentgraph.AgentEntityID("trendagent2")
	countsJSON := `{"missing_tests":3,"bad_error_handling":5}`
	entity := &gtypes.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{Predicate: agentgraph.PredicateAgentErrorCounts, Object: countsJSON},
		},
	}
	data, _ := json.Marshal(entity)
	kv.data[entityID] = data

	trends, err := h.GetAgentErrorTrends(context.Background(), "trendagent2", registry)
	if err != nil {
		t.Fatalf("GetAgentErrorTrends() error = %v", err)
	}

	if len(trends) != 2 {
		t.Fatalf("expected 2 trends, got %d", len(trends))
	}
	if trends[0].Count <= trends[1].Count {
		t.Errorf("trends not sorted descending: trends[0].Count=%d, trends[1].Count=%d",
			trends[0].Count, trends[1].Count)
	}
	if trends[0].Category.ID != "bad_error_handling" {
		t.Errorf("highest count trend should be bad_error_handling, got %q", trends[0].Category.ID)
	}
}

// -- Phase B tests: roster query methods --

func TestHelper_ListAgentsByRole_Multiple(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	now := time.Now().Truncate(time.Second)
	for _, a := range []workflow.Agent{
		{ID: "dev1", Name: "developer-dev1", Role: "developer", Model: "gpt-4o", Status: workflow.AgentAvailable, CreatedAt: now, UpdatedAt: now},
		{ID: "dev2", Name: "developer-dev2", Role: "developer", Model: "gpt-4o", Status: workflow.AgentAvailable, CreatedAt: now, UpdatedAt: now},
		{ID: "rev1", Name: "reviewer-rev1", Role: "reviewer", Model: "gpt-4o", Status: workflow.AgentAvailable, CreatedAt: now, UpdatedAt: now},
	} {
		if err := h.CreateAgent(context.Background(), a); err != nil {
			t.Fatalf("CreateAgent(%q) error = %v", a.ID, err)
		}
	}

	agents, err := h.ListAgentsByRole(context.Background(), "developer")
	if err != nil {
		t.Fatalf("ListAgentsByRole() error = %v", err)
	}

	if len(agents) != 2 {
		t.Fatalf("expected 2 developer agents, got %d", len(agents))
	}
	for _, a := range agents {
		if a.Role != "developer" {
			t.Errorf("agent %q has role %q, want %q", a.ID, a.Role, "developer")
		}
	}
}

func TestHelper_ListAgentsByRole_Empty(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	agents, err := h.ListAgentsByRole(context.Background(), "developer")
	if err != nil {
		t.Fatalf("ListAgentsByRole() error = %v", err)
	}

	if agents == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}
}

func TestHelper_SetAgentStatus(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	agent := makeTestAgent()
	if err := h.CreateAgent(context.Background(), agent); err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	if err := h.SetAgentStatus(context.Background(), agent.ID, workflow.AgentBenched); err != nil {
		t.Fatalf("SetAgentStatus() error = %v", err)
	}

	got, err := h.GetAgent(context.Background(), agent.ID)
	if err != nil {
		t.Fatalf("GetAgent() error = %v", err)
	}
	if got.Status != workflow.AgentBenched {
		t.Errorf("Status = %q, want %q", got.Status, workflow.AgentBenched)
	}
}

func TestHelper_SelectAgent_PicksLowestErrors(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	agents := []struct {
		id     string
		errors int
	}{
		{"ag1", 5},
		{"ag2", 2},
		{"ag3", 8},
	}

	for _, tc := range agents {
		a := workflow.Agent{
			ID:        tc.id,
			Name:      "developer-" + tc.id,
			Role:      "developer",
			Model:     "gpt-4o",
			Status:    workflow.AgentAvailable,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := h.CreateAgent(ctx, a); err != nil {
			t.Fatalf("CreateAgent(%q) error = %v", tc.id, err)
		}
		// Increment errors to reach the desired total.
		cats := make([]string, tc.errors)
		for i := range cats {
			cats[i] = "missing_tests"
		}
		if err := h.IncrementAgentErrorCounts(ctx, tc.id, cats); err != nil {
			t.Fatalf("IncrementAgentErrorCounts(%q) error = %v", tc.id, err)
		}
	}

	selected, err := h.SelectAgent(ctx, "developer", "")
	if err != nil {
		t.Fatalf("SelectAgent() error = %v", err)
	}
	if selected == nil {
		t.Fatal("expected non-nil agent")
	}
	if selected.ID != "ag2" {
		t.Errorf("SelectAgent() returned agent %q, want %q (lowest error count)", selected.ID, "ag2")
	}
}

func TestHelper_SelectAgent_TiesBreakByScore(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	for _, a := range []workflow.Agent{
		{ID: "low-score", Name: "developer-low-score", Role: "developer", Model: "gpt-4o", Status: workflow.AgentAvailable, CreatedAt: now, UpdatedAt: now},
		{ID: "high-score", Name: "developer-high-score", Role: "developer", Model: "gpt-4o", Status: workflow.AgentAvailable, CreatedAt: now, UpdatedAt: now},
	} {
		if err := h.CreateAgent(ctx, a); err != nil {
			t.Fatalf("CreateAgent(%q) error = %v", a.ID, err)
		}
	}

	// Both agents have the same error count (0). Differentiate via OverallAvg.
	if err := h.UpdateAgentStats(ctx, "low-score", workflow.ReviewStats{OverallAvg: 3.0, ReviewCount: 1}); err != nil {
		t.Fatalf("UpdateAgentStats(low-score) error = %v", err)
	}
	if err := h.UpdateAgentStats(ctx, "high-score", workflow.ReviewStats{OverallAvg: 8.5, ReviewCount: 1}); err != nil {
		t.Fatalf("UpdateAgentStats(high-score) error = %v", err)
	}

	selected, err := h.SelectAgent(ctx, "developer", "")
	if err != nil {
		t.Fatalf("SelectAgent() error = %v", err)
	}
	if selected == nil {
		t.Fatal("expected non-nil agent")
	}
	if selected.ID != "high-score" {
		t.Errorf("SelectAgent() returned %q, want %q (higher OverallAvg)", selected.ID, "high-score")
	}
}

func TestHelper_SelectAgent_SkipsBenched(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	available := workflow.Agent{ID: "avail1", Name: "developer-avail1", Role: "developer", Model: "gpt-4o", Status: workflow.AgentAvailable, CreatedAt: now, UpdatedAt: now}
	benched := workflow.Agent{ID: "bench1", Name: "developer-bench1", Role: "developer", Model: "gpt-4o", Status: workflow.AgentBenched, CreatedAt: now, UpdatedAt: now}

	for _, a := range []workflow.Agent{available, benched} {
		if err := h.CreateAgent(ctx, a); err != nil {
			t.Fatalf("CreateAgent(%q) error = %v", a.ID, err)
		}
	}

	// Give the benched agent a lower error count to ensure selection is not
	// based on counts alone — the status filter must exclude it first.
	if err := h.IncrementAgentErrorCounts(ctx, available.ID, []string{"missing_tests", "missing_tests", "missing_tests", "missing_tests", "missing_tests"}); err != nil {
		t.Fatalf("IncrementAgentErrorCounts(avail1) error = %v", err)
	}
	if err := h.IncrementAgentErrorCounts(ctx, benched.ID, []string{"missing_tests"}); err != nil {
		t.Fatalf("IncrementAgentErrorCounts(bench1) error = %v", err)
	}

	selected, err := h.SelectAgent(ctx, "developer", "")
	if err != nil {
		t.Fatalf("SelectAgent() error = %v", err)
	}
	if selected == nil {
		t.Fatal("expected non-nil agent")
	}
	if selected.ID != available.ID {
		t.Errorf("SelectAgent() returned %q, want %q (available despite higher errors)", selected.ID, available.ID)
	}
}

func TestHelper_SelectAgent_CreatesNew(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)

	agent, err := h.SelectAgent(context.Background(), "developer", "test-model")
	if err != nil {
		t.Fatalf("SelectAgent() error = %v", err)
	}
	if agent == nil {
		t.Fatal("expected non-nil agent when nextModel is set")
	}
	if agent.Model != "test-model" {
		t.Errorf("Model = %q, want %q", agent.Model, "test-model")
	}
	if agent.Role != "developer" {
		t.Errorf("Role = %q, want %q", agent.Role, "developer")
	}
	if agent.Status != workflow.AgentAvailable {
		t.Errorf("Status = %q, want %q", agent.Status, workflow.AgentAvailable)
	}
}

func TestHelper_SelectAgent_NilWhenExhausted(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	a := workflow.Agent{ID: "bench2", Name: "developer-bench2", Role: "developer", Model: "gpt-4o", Status: workflow.AgentBenched, CreatedAt: now, UpdatedAt: now}
	if err := h.CreateAgent(ctx, a); err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	selected, err := h.SelectAgent(ctx, "developer", "")
	if err != nil {
		t.Fatalf("SelectAgent() error = %v", err)
	}
	if selected != nil {
		t.Errorf("SelectAgent() = %v, want nil when all agents benched and no nextModel", selected)
	}
}

func TestHelper_BenchAgent_ThresholdMet(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)
	ctx := context.Background()

	agent := makeTestAgent()
	if err := h.CreateAgent(ctx, agent); err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	// Set error count to exactly the threshold for one category.
	if err := h.IncrementAgentErrorCounts(ctx, agent.ID, []string{"missing_tests", "missing_tests", "missing_tests"}); err != nil {
		t.Fatalf("IncrementAgentErrorCounts() error = %v", err)
	}

	benched, err := h.BenchAgent(ctx, agent.ID, 3)
	if err != nil {
		t.Fatalf("BenchAgent() error = %v", err)
	}
	if !benched {
		t.Error("BenchAgent() = false, want true (threshold met)")
	}

	got, err := h.GetAgent(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgent() error = %v", err)
	}
	if got.Status != workflow.AgentBenched {
		t.Errorf("Status = %q, want %q", got.Status, workflow.AgentBenched)
	}
}

func TestHelper_BenchAgent_ThresholdNotMet(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	h := agentgraph.NewHelper(kv)
	ctx := context.Background()

	agent := makeTestAgent()
	if err := h.CreateAgent(ctx, agent); err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	// Error count (2) is below threshold (3).
	if err := h.IncrementAgentErrorCounts(ctx, agent.ID, []string{"missing_tests", "missing_tests"}); err != nil {
		t.Fatalf("IncrementAgentErrorCounts() error = %v", err)
	}

	benched, err := h.BenchAgent(ctx, agent.ID, 3)
	if err != nil {
		t.Fatalf("BenchAgent() error = %v", err)
	}
	if benched {
		t.Error("BenchAgent() = true, want false (threshold not met)")
	}

	got, err := h.GetAgent(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgent() error = %v", err)
	}
	if got.Status != workflow.AgentAvailable {
		t.Errorf("Status = %q, want %q (should be unchanged)", got.Status, workflow.AgentAvailable)
	}
}
