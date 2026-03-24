//go:build integration

package requirementexecutor

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
)

// TestComponentStartStop verifies that a Component backed by a real NATS
// connection can start and stop cleanly without errors.
func TestComponentStartStop(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())

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
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())

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

	// Second Start() should be a no-op — subscriptions should not be doubled.
	if err := c.Start(ctx); err != nil {
		t.Fatalf("second Start() error = %v", err)
	}

	// Should still have the original consumer count, not double.
	if len(c.consumerInfos) > 2 {
		t.Errorf("consumerInfos len = %d after double Start(), want ≤ 2 (idempotent)", len(c.consumerInfos))
	}
}

// TestComponentStartStop_IdempotentStop verifies that calling Stop() twice
// returns nil on the second call.
func TestComponentStartStop_IdempotentStop(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())

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

// TestTriggerReceived verifies that publishing a RequirementExecutionRequest to
// the trigger subject causes the component to consume the message and record
// it in triggersProcessed. Because there is no real agent running, the
// component will attempt to dispatch to the decomposer subject (which has no
// consumer) and the publish will silently be queued. We only assert the
// trigger counter advances.
func TestTriggerReceived(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())

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

	// Build and publish a valid RequirementExecutionRequest directly to NATS.
	req := payloads.RequirementExecutionRequest{
		RequirementID: "integ-req-001",
		Slug:          "integ-plan",
		Title:         "Integration test requirement",
		Model:         "default",
	}
	reqBytes, _ := json.Marshal(req)
	envelope := map[string]json.RawMessage{
		"payload": reqBytes,
	}
	data, _ := json.Marshal(envelope)

	if err := tc.Client.Publish(ctx, subjectRequirementTrigger, data); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	// Wait for the component to process the trigger (up to 5 seconds).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if c.triggersProcessed.Load() >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if c.triggersProcessed.Load() < 1 {
		t.Errorf("triggersProcessed = %d, want ≥ 1 after publishing trigger", c.triggersProcessed.Load())
	}
}
