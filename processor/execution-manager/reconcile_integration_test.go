//go:build integration

package executionmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	wf "github.com/c360studio/semspec/vocabulary/workflow"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
	sgraph "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
)

// TestIntegration_ReconcileFromGraph verifies the startup reconciliation:
// 1. Write execution triples directly to graph (simulating a prior run)
// 2. Start the component
// 3. Verify the execution was recovered into activeExecutions
func TestIntegration_ReconcileFromGraph(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithStreams(
			natsclient.TestStreamConfig{
				Name:     "WORKFLOW",
				Subjects: []string{"workflow.trigger.task-execution-loop", "workflow.async.>"},
			},
			natsclient.TestStreamConfig{
				Name:     "AGENT",
				Subjects: []string{"agentic.loop_completed.v1", "agent.task.>", "dev.task.>", "agent.complete.>"},
			},
			// No GRAPH stream — triple read/write uses Core NATS request/reply
			// which conflicts with JetStream stream capture.
		),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start mock graph-ingest to handle triple writes and queries.
	startMockGraphIngest(t, tc.Client)

	tw := &graphutil.TripleWriter{
		NATSClient:    tc.Client,
		Logger:        testLogger(),
		ComponentName: "test",
	}

	// Write execution entity triples directly — simulates state from a prior run.
	entityID := workflow.EntityPrefix() + ".exec.task.run.test-reconcile-abc123"
	slug := "test-reconcile"

	_ = tw.WriteTriple(ctx, entityID, wf.Type, "task-execution")
	_ = tw.WriteTriple(ctx, entityID, wf.Phase, phaseBuilding) // Mid-pipeline — not terminal.
	_ = tw.WriteTriple(ctx, entityID, wf.Slug, slug)
	_ = tw.WriteTriple(ctx, entityID, wf.TaskID, "task-reconcile-1")
	_ = tw.WriteTriple(ctx, entityID, wf.Title, "Reconcile Test")
	_ = tw.WriteTriple(ctx, entityID, wf.TraceID, "trace-reconcile-1")
	_ = tw.WriteTriple(ctx, entityID, wf.Iteration, 1)
	_ = tw.WriteTriple(ctx, entityID, wf.MaxIterations, 3)
	_ = tw.WriteTriple(ctx, entityID, "workflow.execution.model", "mock-coder")
	_ = tw.WriteTriple(ctx, entityID, "workflow.execution.agent_id", "agent-alpha-builder")

	// Verify the triples were written by reading them back.
	triples, err := tw.ReadEntity(ctx, entityID)
	if err != nil {
		t.Fatalf("ReadEntity after write: %v", err)
	}

	if triples[wf.Slug] != slug {
		t.Fatalf("ReadEntity slug = %q, want %q", triples[wf.Slug], slug)
	}
	if triples[wf.Phase] != phaseBuilding {
		t.Fatalf("ReadEntity phase = %q, want %q", triples[wf.Phase], phaseBuilding)
	}
	if triples["workflow.execution.model"] != "mock-coder" {
		t.Fatalf("ReadEntity model = %q, want %q", triples["workflow.execution.model"], "mock-coder")
	}

	t.Log("PASS: Triples written and read back successfully")

	// Now start the component — it should reconcile and recover this execution.
	comp := newExecIntegrationComponent(t, tc)
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = comp.Stop(5 * time.Second) })

	// Verify the execution was recovered into activeExecutions.
	execVal, ok := comp.activeExecutions.Load(entityID)
	if !ok {
		t.Fatal("Execution not recovered into activeExecutions after Start()")
	}

	exec := execVal.(*taskExecution)
	if exec.Slug != slug {
		t.Errorf("Recovered exec.Slug = %q, want %q", exec.Slug, slug)
	}
	if exec.Model != "mock-coder" {
		t.Errorf("Recovered exec.Model = %q, want %q", exec.Model, "mock-coder")
	}
	if exec.Iteration != 1 {
		t.Errorf("Recovered exec.Iteration = %d, want 1", exec.Iteration)
	}
	if exec.AgentID != "agent-alpha-builder" {
		t.Errorf("Recovered exec.AgentID = %q, want %q", exec.AgentID, "agent-alpha-builder")
	}

	t.Log("PASS: Execution recovered from graph after restart")
}

// TestIntegration_ReconcileSkipsTerminal verifies that terminal-phase entities
// are NOT recovered during reconciliation.
func TestIntegration_ReconcileSkipsTerminal(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithStreams(
			natsclient.TestStreamConfig{
				Name:     "WORKFLOW",
				Subjects: []string{"workflow.trigger.task-execution-loop", "workflow.async.>"},
			},
			natsclient.TestStreamConfig{
				Name:     "AGENT",
				Subjects: []string{"agentic.loop_completed.v1", "agent.task.>", "dev.task.>", "agent.complete.>"},
			},
			// No GRAPH stream — triple read/write uses Core NATS request/reply
			// which conflicts with JetStream stream capture.
		),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	startMockGraphIngest(t, tc.Client)

	tw := &graphutil.TripleWriter{
		NATSClient:    tc.Client,
		Logger:        testLogger(),
		ComponentName: "test",
	}

	// Write a terminal (approved) execution.
	terminalID := workflow.EntityPrefix() + ".exec.task.run.test-terminal-xyz"
	_ = tw.WriteTriple(ctx, terminalID, wf.Type, "task-execution")
	_ = tw.WriteTriple(ctx, terminalID, wf.Phase, phaseApproved)
	_ = tw.WriteTriple(ctx, terminalID, wf.Slug, "test-terminal")

	// Write an active (building) execution.
	activeID := workflow.EntityPrefix() + ".exec.task.run.test-active-xyz"
	_ = tw.WriteTriple(ctx, activeID, wf.Type, "task-execution")
	_ = tw.WriteTriple(ctx, activeID, wf.Phase, phaseBuilding)
	_ = tw.WriteTriple(ctx, activeID, wf.Slug, "test-active")

	comp := newExecIntegrationComponent(t, tc)
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = comp.Stop(5 * time.Second) })

	// Terminal should NOT be recovered.
	if _, ok := comp.activeExecutions.Load(terminalID); ok {
		t.Error("Terminal execution should not be recovered")
	}

	// Active should be recovered.
	if _, ok := comp.activeExecutions.Load(activeID); !ok {
		t.Error("Active execution should be recovered")
	}

	t.Log("PASS: Terminal skipped, active recovered")
}

// TestIntegration_TripleRoundTrip verifies that WriteTriple + ReadEntity
// produces correct data for all value types (string, int, bool).
func TestIntegration_TripleRoundTrip(t *testing.T) {
	// No GRAPH stream — triple read/write uses Core NATS request/reply,
	// which conflicts with JetStream stream capture.
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	startMockGraphIngest(t, tc.Client)

	tw := &graphutil.TripleWriter{
		NATSClient:    tc.Client,
		Logger:        testLogger(),
		ComponentName: "test",
	}

	entityID := fmt.Sprintf("test.triple.roundtrip.%d", time.Now().UnixNano())

	// Write different value types.
	_ = tw.WriteTriple(ctx, entityID, "test.string", "hello")
	_ = tw.WriteTriple(ctx, entityID, "test.int", 42)
	_ = tw.WriteTriple(ctx, entityID, "test.bool", true)

	// Read back.
	triples, err := tw.ReadEntity(ctx, entityID)
	if err != nil {
		t.Fatalf("ReadEntity: %v", err)
	}

	if triples["test.string"] != "hello" {
		t.Errorf("string = %q, want %q", triples["test.string"], "hello")
	}
	if triples["test.int"] != "42" {
		t.Errorf("int = %q, want %q", triples["test.int"], "42")
	}
	if triples["test.bool"] != "true" {
		t.Errorf("bool = %q, want %q", triples["test.bool"], "true")
	}

	t.Log("PASS: Triple round-trip for string, int, bool")
}

func testLogger() *slog.Logger {
	return slog.Default()
}

// mockGraphIngest provides in-memory graph-ingest NATS responders for testing.
// It handles graph.mutation.triple.add and graph.ingest.query.entity/prefix.
type mockGraphIngest struct {
	mu       sync.Mutex
	entities map[string]*sgraph.EntityState // entityID → state
}

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

	return m
}

// tripleValue is a convenience to convert message.Triple objects to string.
func tripleValue(triples []message.Triple, predicate string) string {
	for _, t := range triples {
		if t.Predicate == predicate {
			if s, ok := t.Object.(string); ok {
				return s
			}
			data, _ := json.Marshal(t.Object)
			return string(data)
		}
	}
	return ""
}
