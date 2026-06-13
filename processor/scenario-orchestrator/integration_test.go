//go:build integration

package scenarioorchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	sgraph "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
)

// workflowStreamSubjects are the subjects covered by the WORKFLOW stream used
// in integration tests. They must include both the inbound trigger subject and
// the outbound execution-loop subject so that the component can consume and
// publish within the same stream.
var workflowStreamSubjects = []string{
	"scenario.orchestrate.*",
	"workflow.trigger.requirement-execution-loop",
}

// ---------------------------------------------------------------------------
// Mock graph-ingest — in-memory NATS responder for triple read/write.
// ---------------------------------------------------------------------------

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

		// Upsert triple — replace existing predicate or append.
		found := false
		for i, t := range entity.Triples {
			if t.Predicate == req.Triple.Predicate {
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

// makeReqForPlan builds a Requirement with the correct PlanEntityID for a given slug.
func TestComponentStartStop(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithStreams(natsclient.TestStreamConfig{
			Name:     "WORKFLOW",
			Subjects: workflowStreamSubjects,
		}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Mock graph-ingest so startup reconciliation does not block on unanswered requests.
	startMockGraphIngest(t, tc.Client)

	comp := newIntegrationComponent(t, tc)

	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if !comp.IsRunning() {
		t.Error("IsRunning() = false after Start()")
	}

	health := comp.Health()
	if !health.Healthy {
		t.Errorf("Health().Healthy = false after Start()")
	}
	if health.Status != "running" {
		t.Errorf("Health().Status = %q, want %q", health.Status, "running")
	}

	if err := comp.Stop(5 * time.Second); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if comp.IsRunning() {
		t.Error("IsRunning() = true after Stop()")
	}

	stoppedHealth := comp.Health()
	if stoppedHealth.Healthy {
		t.Error("Health().Healthy = true after Stop(), want false")
	}
}

// TestDispatchScenarios_PublishesMessages verifies that an OrchestratorTrigger
// for a plan with two requirements (each having a pending scenario) results in
// exactly two RequirementExecutionRequest messages being published to
// workflow.trigger.requirement-execution-loop.
func newIntegrationComponent(t *testing.T, tc *natsclient.TestClient) *Component {
	t.Helper()

	rawCfg, err := json.Marshal(DefaultConfig())
	if err != nil {
		t.Fatalf("marshal default config: %v", err)
	}

	deps := component.Dependencies{NATSClient: tc.Client}
	compI, err := NewComponent(rawCfg, deps)
	if err != nil {
		t.Fatalf("NewComponent() error: %v", err)
	}
	return compI.(*Component)
}

// publishTrigger wraps an OrchestratorTrigger in a BaseMessage envelope and
// publishes it to the JetStream stream so the component can consume it.
