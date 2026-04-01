package agentgraph_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

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

func TestHelper_GetChildEntityIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		loopID       string
		setupParent  func(kv *mockKV)
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
			name:   "returns full entity IDs from spawned triples",
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
			wantChildren: []string{
				agentgraph.LoopEntityID("child-a"),
				agentgraph.LoopEntityID("child-b"),
			},
		},
		{
			name:    "propagates get error",
			loopID:  "err-loop",
			getErr:  errors.New("nats timeout"),
			wantErr: true,
		},
		{
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
			wantChildren: []string{agentgraph.LoopEntityID("valid-child")},
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

			children, err := h.GetChildEntityIDs(context.Background(), tc.loopID)

			if (err != nil) != tc.wantErr {
				t.Fatalf("GetChildEntityIDs() error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}

			if len(children) != len(tc.wantChildren) {
				t.Fatalf("GetChildEntityIDs() returned %d children, want %d: %v", len(children), len(tc.wantChildren), children)
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

	child1EID := agentgraph.LoopEntityID("child-1")
	child2EID := agentgraph.LoopEntityID("child-2")
	rootEID := agentgraph.LoopEntityID("root")

	tests := []struct {
		name       string
		rootLoopID string
		maxDepth   int
		setup      func(kv *mockKV)
		wantIDs    []string
		wantErr    bool
	}{
		{
			name:       "returns all traversed entity IDs",
			rootLoopID: "root",
			maxDepth:   5,
			setup: func(kv *mockKV) {
				// Root spawns child-1 and child-2 (stored as entity IDs in triples).
				rootEntity := &gtypes.EntityState{
					ID: rootEID,
					Triples: []message.Triple{
						{Predicate: agentgraph.PredicateSpawned, Object: child1EID},
						{Predicate: agentgraph.PredicateSpawned, Object: child2EID},
					},
				}
				rootData, _ := json.Marshal(rootEntity)
				kv.data[rootEID] = rootData

				// Children stored under their entity IDs (no double-hashing).
				for _, eid := range []string{child1EID, child2EID} {
					childEntity := &gtypes.EntityState{ID: eid}
					childData, _ := json.Marshal(childEntity)
					kv.data[eid] = childData
				}
			},
			wantIDs: []string{rootEID, child1EID, child2EID},
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

