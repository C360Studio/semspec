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

func TestKV_CreateAndLoadPlan(t *testing.T) {
	tw := newTestTripleWriter(t)
	ctx := context.Background()

	plan, err := CreatePlan(ctx, tw, "test-plan", "Test Plan")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	if plan.Slug != "test-plan" {
		t.Errorf("Slug = %q, want %q", plan.Slug, "test-plan")
	}
	if plan.Title != "Test Plan" {
		t.Errorf("Title = %q, want %q", plan.Title, "Test Plan")
	}

	loaded, err := LoadPlan(ctx, tw, "test-plan")
	if err != nil {
		t.Fatalf("LoadPlan: %v", err)
	}

	if loaded.Slug != plan.Slug {
		t.Errorf("loaded Slug = %q, want %q", loaded.Slug, plan.Slug)
	}
	if loaded.Title != plan.Title {
		t.Errorf("loaded Title = %q, want %q", loaded.Title, plan.Title)
	}
}

func TestKV_SaveAndLoadRequirements(t *testing.T) {
	tw := newTestTripleWriter(t)
	ctx := context.Background()

	if _, err := CreatePlan(ctx, tw, "req-test", "Req Test"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	reqs := []Requirement{
		{
			ID:          "req-001",
			PlanID:      PlanEntityID("req-test"),
			Title:       "First Requirement",
			Description: "Do the first thing",
			Status:      RequirementStatusActive,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          "req-002",
			PlanID:      PlanEntityID("req-test"),
			Title:       "Second Requirement",
			Description: "Do the second thing",
			Status:      RequirementStatusActive,
			DependsOn:   []string{"req-001"},
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}

	if err := SaveRequirements(ctx, tw, reqs, "req-test"); err != nil {
		t.Fatalf("SaveRequirements: %v", err)
	}

	loaded, err := LoadRequirements(ctx, tw, "req-test")
	if err != nil {
		t.Fatalf("LoadRequirements: %v", err)
	}

	if len(loaded) != 2 {
		t.Fatalf("LoadRequirements returned %d, want 2", len(loaded))
	}
}

func TestKV_SaveAndLoadScenarios(t *testing.T) {
	tw := newTestTripleWriter(t)
	ctx := context.Background()

	if _, err := CreatePlan(ctx, tw, "scen-test", "Scenario Test"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	reqs := []Requirement{
		{ID: "req-001", PlanID: PlanEntityID("scen-test"), Title: "Req", Status: RequirementStatusActive, CreatedAt: now, UpdatedAt: now},
	}
	if err := SaveRequirements(ctx, tw, reqs, "scen-test"); err != nil {
		t.Fatalf("SaveRequirements: %v", err)
	}

	scenarios := []Scenario{
		{
			ID:            "scen-001",
			RequirementID: "req-001",
			Given:         "A system",
			When:          "Something happens",
			Then:          []string{"Result A", "Result B"},
			Status:        ScenarioStatusPending,
			CreatedAt:     now,
		},
	}

	if err := SaveScenarios(ctx, tw, scenarios, "scen-test"); err != nil {
		t.Fatalf("SaveScenarios: %v", err)
	}

	loaded, err := LoadScenarios(ctx, tw, "scen-test")
	if err != nil {
		t.Fatalf("LoadScenarios: %v", err)
	}

	if len(loaded) != 1 {
		t.Fatalf("LoadScenarios returned %d, want 1", len(loaded))
	}

	if len(loaded[0].Then) != 2 {
		t.Errorf("Then has %d items, want 2", len(loaded[0].Then))
	}
}

func TestKV_SaveAndLoadChangeProposals(t *testing.T) {
	tw := newTestTripleWriter(t)
	ctx := context.Background()

	if _, err := CreatePlan(ctx, tw, "cp-test", "CP Test"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	proposals := []ChangeProposal{
		{
			ID:             "cp-001",
			PlanID:         PlanEntityID("cp-test"),
			Title:          "Change Auth",
			Rationale:      "Need SAML",
			Status:         ChangeProposalStatusProposed,
			ProposedBy:     "reviewer",
			AffectedReqIDs: []string{"req-001", "req-002"},
			CreatedAt:      now,
		},
	}

	if err := SaveChangeProposals(ctx, tw, proposals, "cp-test"); err != nil {
		t.Fatalf("SaveChangeProposals: %v", err)
	}

	loaded, err := LoadChangeProposals(ctx, tw, "cp-test")
	if err != nil {
		t.Fatalf("LoadChangeProposals: %v", err)
	}

	if len(loaded) != 1 {
		t.Fatalf("LoadChangeProposals returned %d, want 1", len(loaded))
	}

	if len(loaded[0].AffectedReqIDs) != 2 {
		t.Errorf("AffectedReqIDs has %d items, want 2", len(loaded[0].AffectedReqIDs))
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
		if _, err := LoadPlan(ctx, tw, slug); err == nil {
			t.Errorf("LoadPlan(%q) should fail", slug)
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

func TestKV_CrossPlanIsolation_Scenarios(t *testing.T) {
	tw := newTestTripleWriter(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	if _, err := CreatePlan(ctx, tw, "plan-x", "Plan X"); err != nil {
		t.Fatalf("CreatePlan X: %v", err)
	}
	if _, err := CreatePlan(ctx, tw, "plan-y", "Plan Y"); err != nil {
		t.Fatalf("CreatePlan Y: %v", err)
	}

	reqsX := []Requirement{{ID: "req-x1", PlanID: PlanEntityID("plan-x"), Title: "X Req", Status: RequirementStatusActive, CreatedAt: now, UpdatedAt: now}}
	reqsY := []Requirement{{ID: "req-y1", PlanID: PlanEntityID("plan-y"), Title: "Y Req", Status: RequirementStatusActive, CreatedAt: now, UpdatedAt: now}}

	if err := SaveRequirements(ctx, tw, reqsX, "plan-x"); err != nil {
		t.Fatalf("SaveRequirements X: %v", err)
	}
	if err := SaveRequirements(ctx, tw, reqsY, "plan-y"); err != nil {
		t.Fatalf("SaveRequirements Y: %v", err)
	}

	scenX := []Scenario{{ID: "sc-x1", RequirementID: "req-x1", Given: "X", When: "X happens", Then: []string{"X result"}, Status: ScenarioStatusPending, CreatedAt: now}}
	scenY := []Scenario{{ID: "sc-y1", RequirementID: "req-y1", Given: "Y", When: "Y happens", Then: []string{"Y result"}, Status: ScenarioStatusPending, CreatedAt: now}}

	if err := SaveScenarios(ctx, tw, scenX, "plan-x"); err != nil {
		t.Fatalf("SaveScenarios X: %v", err)
	}
	if err := SaveScenarios(ctx, tw, scenY, "plan-y"); err != nil {
		t.Fatalf("SaveScenarios Y: %v", err)
	}

	loadedX, err := LoadScenarios(ctx, tw, "plan-x")
	if err != nil {
		t.Fatalf("LoadScenarios X: %v", err)
	}
	// Entity IDs (including scenario and requirement IDs) are hashed and
	// not recoverable as original strings. Verify cross-plan isolation by
	// checking that exactly one scenario is returned per plan.
	if len(loadedX) != 1 {
		t.Errorf("plan-x scenarios: got %d, want 1", len(loadedX))
	}
	if len(loadedX) == 1 && loadedX[0].Given != "X" {
		t.Errorf("plan-x scenario has Given %q, want %q", loadedX[0].Given, "X")
	}

	loadedY, err := LoadScenarios(ctx, tw, "plan-y")
	if err != nil {
		t.Fatalf("LoadScenarios Y: %v", err)
	}
	if len(loadedY) != 1 {
		t.Errorf("plan-y scenarios: got %d, want 1", len(loadedY))
	}
	if len(loadedY) == 1 && loadedY[0].Given != "Y" {
		t.Errorf("plan-y scenario has Given %q, want %q", loadedY[0].Given, "Y")
	}
}

func TestKV_CrossPlanIsolation_ChangeProposals(t *testing.T) {
	tw := newTestTripleWriter(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	if _, err := CreatePlan(ctx, tw, "iso-a", "Iso A"); err != nil {
		t.Fatalf("CreatePlan A: %v", err)
	}
	if _, err := CreatePlan(ctx, tw, "iso-b", "Iso B"); err != nil {
		t.Fatalf("CreatePlan B: %v", err)
	}

	propA := []ChangeProposal{{ID: "cp-a1", PlanID: PlanEntityID("iso-a"), Title: "A prop", Status: ChangeProposalStatusProposed, CreatedAt: now}}
	propB := []ChangeProposal{{ID: "cp-b1", PlanID: PlanEntityID("iso-b"), Title: "B prop", Status: ChangeProposalStatusProposed, CreatedAt: now}}

	if err := SaveChangeProposals(ctx, tw, propA, "iso-a"); err != nil {
		t.Fatalf("SaveChangeProposals A: %v", err)
	}
	if err := SaveChangeProposals(ctx, tw, propB, "iso-b"); err != nil {
		t.Fatalf("SaveChangeProposals B: %v", err)
	}

	loadedA, err := LoadChangeProposals(ctx, tw, "iso-a")
	if err != nil {
		t.Fatalf("LoadChangeProposals A: %v", err)
	}
	// Proposal IDs are stored as hashed entity IDs; check count only to verify isolation.
	if len(loadedA) != 1 {
		t.Errorf("plan iso-a proposals: got %d, want 1", len(loadedA))
	}
	if len(loadedA) == 1 && loadedA[0].Title != "A prop" {
		t.Errorf("plan iso-a proposal has Title %q, want %q", loadedA[0].Title, "A prop")
	}

	loadedB, err := LoadChangeProposals(ctx, tw, "iso-b")
	if err != nil {
		t.Fatalf("LoadChangeProposals B: %v", err)
	}
	if len(loadedB) != 1 {
		t.Errorf("plan iso-b proposals: got %d, want 1", len(loadedB))
	}
	if len(loadedB) == 1 && loadedB[0].Title != "B prop" {
		t.Errorf("plan iso-b proposal has Title %q, want %q", loadedB[0].Title, "B prop")
	}
}
