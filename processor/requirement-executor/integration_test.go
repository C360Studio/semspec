//go:build integration

package requirementexecutor

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
)

// TestComponentStartStop verifies that a Component backed by a real NATS
// connection can start and stop cleanly without errors.
func TestComponentStartStop(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithStreams(
			natsclient.TestStreamConfig{
				Name:     "WORKFLOW",
				Subjects: []string{"workflow.trigger.requirement-execution-loop", "workflow.async.>", "workflow.events.>"},
			},
			natsclient.TestStreamConfig{
				Name:     "AGENT",
				Subjects: []string{"agentic.loop_completed.v1", "agent.complete.>", "agent.task.>"},
			},
			natsclient.TestStreamConfig{
				Name:     "GRAPH",
				Subjects: []string{"graph.mutation.triple.add"},
			},
		),
	)

	raw, _ := json.Marshal(DefaultConfig())
	comp, err := NewComponent(raw, component.Dependencies{
		NATSClient: tc.Client,
	})
	if err != nil {
		t.Fatalf("NewComponent() error = %v", err)
	}

	c := comp.(*Component)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	c.mu.RLock()
	running := c.running
	c.mu.RUnlock()

	if !running {
		t.Error("component should be running after Start()")
	}

	h := c.Health()
	if !h.Healthy {
		t.Errorf("Health().Healthy = false after Start, status = %q", h.Status)
	}

	if err := c.Stop(5 * time.Second); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	c.mu.RLock()
	running = c.running
	c.mu.RUnlock()

	if running {
		t.Error("component should not be running after Stop()")
	}

	h = c.Health()
	if h.Healthy {
		t.Errorf("Health().Healthy = true after Stop, want false")
	}
	if h.Status != "stopped" {
		t.Errorf("Health().Status = %q, want stopped", h.Status)
	}
}

// TestComponentStartStop_IdempotentStart verifies that calling Start() twice
// does not produce an error and the component remains running.
func TestComponentStartStop_IdempotentStart(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithStreams(
			natsclient.TestStreamConfig{
				Name:     "WORKFLOW",
				Subjects: []string{"workflow.trigger.requirement-execution-loop", "workflow.async.>", "workflow.events.>"},
			},
			natsclient.TestStreamConfig{
				Name:     "AGENT",
				Subjects: []string{"agentic.loop_completed.v1", "agent.complete.>", "agent.task.>"},
			},
			natsclient.TestStreamConfig{
				Name:     "GRAPH",
				Subjects: []string{"graph.mutation.triple.add"},
			},
		),
	)

	raw, _ := json.Marshal(DefaultConfig())
	comp, err := NewComponent(raw, component.Dependencies{
		NATSClient: tc.Client,
	})
	if err != nil {
		t.Fatalf("NewComponent() error = %v", err)
	}

	c := comp.(*Component)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := c.Start(ctx); err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	t.Cleanup(func() { _ = c.Stop(5 * time.Second) })

	// Second Start() should be a no-op and not return an error.
	if err := c.Start(ctx); err != nil {
		t.Fatalf("second Start() error = %v", err)
	}

	// Component must still report running after the idempotent second Start().
	c.mu.RLock()
	running := c.running
	c.mu.RUnlock()
	if !running {
		t.Error("component should still be running after idempotent second Start()")
	}
}

// TestComponentStartStop_IdempotentStop verifies that calling Stop() twice
// returns nil on the second call.
func TestComponentStartStop_IdempotentStop(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithStreams(
			natsclient.TestStreamConfig{
				Name:     "WORKFLOW",
				Subjects: []string{"workflow.trigger.requirement-execution-loop", "workflow.async.>", "workflow.events.>"},
			},
			natsclient.TestStreamConfig{
				Name:     "AGENT",
				Subjects: []string{"agentic.loop_completed.v1", "agent.complete.>", "agent.task.>"},
			},
			natsclient.TestStreamConfig{
				Name:     "GRAPH",
				Subjects: []string{"graph.mutation.triple.add"},
			},
		),
	)

	raw, _ := json.Marshal(DefaultConfig())
	comp, err := NewComponent(raw, component.Dependencies{
		NATSClient: tc.Client,
	})
	if err != nil {
		t.Fatalf("NewComponent() error = %v", err)
	}

	c := comp.(*Component)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := c.Stop(5 * time.Second); err != nil {
		t.Fatalf("first Stop() error = %v", err)
	}
	// Second Stop() on a non-running component must be a no-op.
	if err := c.Stop(5 * time.Second); err != nil {
		t.Fatalf("second Stop() error = %v, want nil", err)
	}
}

// TestComponentStartStop_KVWatchersStart verifies that the component starts its
// KV watchers cleanly. The KV self-trigger path (EXECUTION_STATES req.> watcher)
// replaced the old JetStream stream consumer. This test confirms Start() succeeds
// with an EXECUTION_STATES KV bucket in place.
func TestComponentStartStop_KVWatchersStart(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithStreams(
			natsclient.TestStreamConfig{
				Name:     "WORKFLOW",
				Subjects: []string{"workflow.async.>", "workflow.events.>"},
			},
			natsclient.TestStreamConfig{
				Name:     "AGENT",
				Subjects: []string{"agentic.loop_completed.v1", "agent.complete.>", "agent.task.>"},
			},
			natsclient.TestStreamConfig{
				Name:     "GRAPH",
				Subjects: []string{"graph.mutation.triple.add"},
			},
		),
		natsclient.WithKVBuckets("EXECUTION_STATES", "AGENT_LOOPS"),
	)

	raw, _ := json.Marshal(DefaultConfig())
	comp, err := NewComponent(raw, component.Dependencies{
		NATSClient: tc.Client,
	})
	if err != nil {
		t.Fatalf("NewComponent() error = %v", err)
	}

	c := comp.(*Component)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = c.Stop(5 * time.Second) })

	c.mu.RLock()
	running := c.running
	c.mu.RUnlock()

	if !running {
		t.Error("component should be running after Start() with KV buckets available")
	}
}
