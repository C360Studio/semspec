//go:build integration

package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow/graphutil"
	sgraph "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
)

// mockGraphIngest provides in-memory graph-ingest NATS responders for testing.
// It handles graph.mutation.triple.add and graph.ingest.query.entity/prefix.
type mockGraphIngest struct {
	mu       sync.Mutex
	entities map[string]*sgraph.EntityState // entityID → state
}

// startMockGraphIngest registers Core NATS request/reply handlers on the
// graph-ingest subjects. The handlers store triples in memory and respond to
// entity and prefix queries without requiring an external graph-ingest service.
func startMockGraphIngest(t *testing.T, nc *natsclient.Client) *mockGraphIngest {
	t.Helper()
	m := &mockGraphIngest{entities: make(map[string]*sgraph.EntityState)}

	// Handle triple writes.
	nc.SubscribeForRequests(context.Background(), "graph.mutation.triple.add", func(_ context.Context, data []byte) ([]byte, error) {
		var req sgraph.AddTripleRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return json.Marshal(map[string]any{"success": false, "error": err.Error()})
		}

		m.mu.Lock()
		defer m.mu.Unlock()

		entity, ok := m.entities[req.Triple.Subject]
		if !ok {
			entity = &sgraph.EntityState{
				ID:        req.Triple.Subject,
				UpdatedAt: time.Now(),
			}
			m.entities[req.Triple.Subject] = entity
		}

		// Append all triples — graph-ingest preserves multi-valued predicates.
		entity.Triples = append(entity.Triples, req.Triple)
		entity.Version++
		entity.UpdatedAt = time.Now()

		return json.Marshal(map[string]any{"success": true, "kv_revision": entity.Version})
	})

	// Handle entity queries.
	nc.SubscribeForRequests(context.Background(), "graph.ingest.query.entity", func(_ context.Context, data []byte) ([]byte, error) {
		var req struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, err
		}

		m.mu.Lock()
		entity, ok := m.entities[req.ID]
		m.mu.Unlock()

		if !ok {
			return nil, fmt.Errorf("not found: %s", req.ID)
		}
		return json.Marshal(entity)
	})

	// Handle prefix queries.
	nc.SubscribeForRequests(context.Background(), "graph.ingest.query.prefix", func(_ context.Context, data []byte) ([]byte, error) {
		var req struct {
			Prefix string `json:"prefix"`
			Limit  int    `json:"limit"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, err
		}

		m.mu.Lock()
		var matches []sgraph.EntityState
		for id, entity := range m.entities {
			if len(id) >= len(req.Prefix) && id[:len(req.Prefix)] == req.Prefix {
				matches = append(matches, *entity)
				if req.Limit > 0 && len(matches) >= req.Limit {
					break
				}
			}
		}
		m.mu.Unlock()

		return json.Marshal(map[string]any{"entities": matches})
	})

	// Flush ensures all subscriptions are registered on the server before any
	// caller fires requests. Without this, there is a race between the async
	// subscribe round-trip and the first WriteTriple call.
	if conn := nc.GetConnection(); conn != nil {
		_ = conn.Flush()
	}

	return m
}

// newTestTripleWriter creates a TripleWriter backed by a real NATS for integration
// tests. It also starts the in-memory graph-ingest mock so that TripleWriter
// calls to graph.mutation.triple.add and the read-back queries all succeed.
func newTestTripleWriter(t *testing.T) *graphutil.TripleWriter {
	t.Helper()
	tc := natsclient.NewTestClient(t,
		natsclient.WithKVBuckets("ENTITY_STATES"),
	)
	startMockGraphIngest(t, tc.Client)
	return &graphutil.TripleWriter{
		NATSClient:    tc.Client,
		Logger:        slog.Default(),
		ComponentName: "test",
	}
}

func TestKV_CreatePlan_DuplicateRejected(t *testing.T) {
	tw := newTestTripleWriter(t)
	ctx := context.Background()

	if _, err := CreatePlan(ctx, tw, "dupe-test", "First"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	_, err := CreatePlan(ctx, tw, "dupe-test", "Second")
	if err == nil {
		t.Fatal("CreatePlan with duplicate slug should fail")
	}
}

func TestKV_InvalidSlug(t *testing.T) {
	tw := newTestTripleWriter(t)
	ctx := context.Background()

	slugs := []string{"", "../escape", "has spaces", "UPPERCASE", "a/b"}
	for _, slug := range slugs {
		if _, err := CreatePlan(ctx, tw, slug, "Bad"); err == nil {
			t.Errorf("CreatePlan(%q) should fail", slug)
		}
	}
}

func TestKV_RequirementDAGValidation(t *testing.T) {
	tw := newTestTripleWriter(t)
	ctx := context.Background()

	if _, err := CreatePlan(ctx, tw, "dag-test", "DAG Test"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	now := time.Now().Truncate(time.Second)

	reqs := []Requirement{
		{ID: "req-self", PlanID: PlanEntityID("dag-test"), Title: "Self", Status: RequirementStatusActive, DependsOn: []string{"req-self"}, CreatedAt: now, UpdatedAt: now},
	}
	if err := SaveRequirements(ctx, tw, reqs, "dag-test"); err == nil {
		t.Error("SaveRequirements with self-reference should fail")
	}

	cycleReqs := []Requirement{
		{ID: "req-a", PlanID: PlanEntityID("dag-test"), Title: "A", Status: RequirementStatusActive, DependsOn: []string{"req-b"}, CreatedAt: now, UpdatedAt: now},
		{ID: "req-b", PlanID: PlanEntityID("dag-test"), Title: "B", Status: RequirementStatusActive, DependsOn: []string{"req-a"}, CreatedAt: now, UpdatedAt: now},
	}
	if err := SaveRequirements(ctx, tw, cycleReqs, "dag-test"); err == nil {
		t.Error("SaveRequirements with cycle should fail")
	}
}
