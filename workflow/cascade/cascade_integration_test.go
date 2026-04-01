//go:build integration

package cascade

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
	sgraph "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
)

// mockGraphIngest handles graph NATS request/reply in integration tests.
type mockGraphIngest struct {
	mu       sync.Mutex
	entities map[string]*sgraph.EntityState
}

func startMockGraphIngest(t *testing.T, nc *natsclient.Client) *mockGraphIngest {
	t.Helper()
	m := &mockGraphIngest{entities: make(map[string]*sgraph.EntityState)}

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

		found := false
		for i, tr := range entity.Triples {
			if tr.Predicate == req.Triple.Predicate {
				entity.Triples[i] = req.Triple
				found = true
				break
			}
		}
		if !found {
			entity.Triples = append(entity.Triples, req.Triple)
		}
		entity.Version++
		entity.UpdatedAt = time.Now()

		return json.Marshal(map[string]any{"success": true, "kv_revision": entity.Version})
	})

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

// setupCascadeFixture creates a real NATS-backed fixture with a plan, requirements,
// and scenarios seeded for integration tests.
func setupCascadeFixture(t *testing.T) (*graphutil.TripleWriter, string) {
	t.Helper()
	ctx := context.Background()

	tc := natsclient.NewTestClient(t, natsclient.WithKVBuckets("ENTITY_STATES"))
	startMockGraphIngest(t, tc.Client)

	tw := &graphutil.TripleWriter{
		NATSClient:    tc.Client,
		Logger:        slog.Default(),
		ComponentName: "test",
	}
	slug := "cascade-test"

	if _, err := workflow.CreatePlan(ctx, tw, slug, "Cascade Test Plan"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	reqs := []workflow.Requirement{
		{ID: "req-1", PlanID: workflow.PlanEntityID(slug), Title: "Auth", Status: workflow.RequirementStatusActive},
		{ID: "req-2", PlanID: workflow.PlanEntityID(slug), Title: "Logging", Status: workflow.RequirementStatusActive},
	}
	if err := workflow.SaveRequirements(ctx, tw, reqs, slug); err != nil {
		t.Fatalf("SaveRequirements: %v", err)
	}

	scenarios := []workflow.Scenario{
		{ID: "sc-1", RequirementID: "req-1", Given: "a user"},
		{ID: "sc-2", RequirementID: "req-1", Given: "a token"},
		{ID: "sc-3", RequirementID: "req-2", Given: "log files"},
	}
	if err := workflow.SaveScenarios(ctx, tw, scenarios, slug); err != nil {
		t.Fatalf("SaveScenarios: %v", err)
	}

	return tw, slug
}

func TestChangeProposal_AffectsOneRequirement(t *testing.T) {
	tw, slug := setupCascadeFixture(t)

	proposal := &workflow.ChangeProposal{
		ID:             "cp-1",
		AffectedReqIDs: []string{"req-1"}, // affects sc-1, sc-2
	}

	result, err := ChangeProposal(context.Background(), tw, slug, proposal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.AffectedRequirementIDs) != 1 {
		t.Errorf("AffectedRequirementIDs = %d, want 1", len(result.AffectedRequirementIDs))
	}
	if len(result.AffectedScenarioIDs) != 2 {
		t.Errorf("AffectedScenarioIDs = %d, want 2 (sc-1, sc-2)", len(result.AffectedScenarioIDs))
	}
}

func TestChangeProposal_AffectsAllRequirements(t *testing.T) {
	tw, slug := setupCascadeFixture(t)

	proposal := &workflow.ChangeProposal{
		ID:             "cp-1",
		AffectedReqIDs: []string{"req-1", "req-2"},
	}

	result, err := ChangeProposal(context.Background(), tw, slug, proposal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.AffectedScenarioIDs) != 3 {
		t.Errorf("AffectedScenarioIDs = %d, want 3", len(result.AffectedScenarioIDs))
	}
}

func TestChangeProposal_NoMatchingScenarios(t *testing.T) {
	tw, slug := setupCascadeFixture(t)

	proposal := &workflow.ChangeProposal{
		ID:             "cp-1",
		AffectedReqIDs: []string{"req-nonexistent"},
	}

	result, err := ChangeProposal(context.Background(), tw, slug, proposal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.AffectedScenarioIDs) != 0 {
		t.Errorf("AffectedScenarioIDs = %d, want 0", len(result.AffectedScenarioIDs))
	}
}
