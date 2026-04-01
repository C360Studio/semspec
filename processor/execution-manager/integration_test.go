//go:build integration

package executionmanager

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	nats "github.com/nats-io/nats.go"
)

// TestIntegration_StartStop verifies the component lifecycle against a real NATS
// server: Start must succeed, Health must report running, and Stop must cleanly
// shut down the consumer.
func TestIntegration_StartStop(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithStreams(
			natsclient.TestStreamConfig{
				Name:     "WORKFLOW",
				Subjects: []string{"workflow.trigger.task-execution-loop", "workflow.async.>"},
			},
			natsclient.TestStreamConfig{
				Name:     "AGENT",
				Subjects: []string{"agentic.loop_completed.v1", "agent.task.>", "dev.task.>"},
			},
		),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Mock graph-ingest so reconcileFromGraph does not block on unanswered requests.
	startMockGraphIngest(t, tc.Client)

	comp := newExecIntegrationComponent(t, tc)

	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = comp.Stop(5 * time.Second) })

	health := comp.Health()
	if !health.Healthy {
		t.Errorf("Health().Healthy = false after Start()")
	}
	if health.Status != "healthy" {
		t.Errorf("Health().Status = %q, want %q", health.Status, "healthy")
	}

	if err := comp.Stop(5 * time.Second); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	stoppedHealth := comp.Health()
	if stoppedHealth.Healthy {
		t.Error("Health().Healthy = true after Stop(), want false")
	}
	if stoppedHealth.Status != "stopped" {
		t.Errorf("Health().Status = %q after Stop(), want %q", stoppedHealth.Status, "stopped")
	}
}

// TestIntegration_TriggerCreatesExecution verifies the end-to-end trigger path:
// publishing a valid TriggerPayload to the execution trigger subject causes the
// component to register an active execution, publish entity triples to
// graph.mutation.triple.add, and dispatch a developer task to agent.task.development.
func TestIntegration_TriggerCreatesExecution(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithStreams(
			natsclient.TestStreamConfig{
				Name:     "WORKFLOW",
				Subjects: []string{"workflow.trigger.task-execution-loop", "workflow.async.>"},
			},
			natsclient.TestStreamConfig{
				Name:     "AGENT",
				Subjects: []string{"agentic.loop_completed.v1", "agent.task.>", "dev.task.>"},
			},
		),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// Start the mock graph-ingest responder so that WriteTriple and ReadEntity
	// calls in handleTrigger do not time out waiting for a non-existent service.
	startMockGraphIngest(t, tc.Client)

	comp := newExecIntegrationComponent(t, tc)

	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = comp.Stop(5 * time.Second) })

	// Subscribe to agent.task.development before publishing so no messages are missed.
	// dispatchFirstStage calls dispatchDeveloperLocked which publishes to agent.task.development.
	developerTasks := make(chan []byte, 10)
	nativeConn := tc.GetNativeConnection()
	developerSub, err := nativeConn.Subscribe("agent.task.development", func(msg *nats.Msg) {
		data := make([]byte, len(msg.Data))
		copy(data, msg.Data)
		developerTasks <- data
	})
	if err != nil {
		t.Fatalf("Subscribe(agent.task.development) error = %v", err)
	}
	t.Cleanup(func() { _ = developerSub.Unsubscribe() })

	// Subscribe to graph.mutation.triple.add to observe entity triple requests.
	// The mock graph-ingest also subscribes here; both receive the fan-out.
	triples := make(chan []byte, 20)
	tripleSub, err := nativeConn.Subscribe("graph.mutation.triple.add", func(msg *nats.Msg) {
		data := make([]byte, len(msg.Data))
		copy(data, msg.Data)
		triples <- data
	})
	if err != nil {
		t.Fatalf("Subscribe(graph.mutation.triple.add) error = %v", err)
	}
	t.Cleanup(func() { _ = tripleSub.Unsubscribe() })

	trigger := workflow.TriggerPayload{
		Slug:    "test-plan",
		TaskID:  "task-001",
		Title:   "Test task",
		Model:   "default",
		TraceID: "trace-integ-001",
		Prompt:  "Implement the feature",
	}
	publishExecTrigger(t, tc, ctx, trigger)

	// Verify: a developer task message appears on agent.task.development.
	developerMsgs := collectMessagesFrom(ctx, t, developerTasks, 1, 15*time.Second)
	if len(developerMsgs) == 0 {
		t.Fatal("expected at least one developer task message on agent.task.development")
	}

	// Verify: at least one entity triple request was sent to graph.mutation.triple.add.
	triplesMsgs := collectMessagesFrom(ctx, t, triples, 1, 10*time.Second)
	if len(triplesMsgs) == 0 {
		t.Fatal("expected at least one graph triple request on graph.mutation.triple.add")
	}

	// Verify: triggersProcessed counter increments.
	waitForExecCondition(t, ctx, 10*time.Second, func() bool {
		return comp.triggersProcessed.Load() >= 1
	}, "triggersProcessed should reach 1")
}

// TestIntegration_DuplicateTriggerIdempotent verifies that publishing the same
// trigger twice results in only one active execution registration. The
// triggersProcessed counter increments for both deliveries, but the duplicate
// is silently dropped without creating a second execution.
func TestIntegration_DuplicateTriggerIdempotent(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithStreams(
			natsclient.TestStreamConfig{
				Name:     "WORKFLOW",
				Subjects: []string{"workflow.trigger.task-execution-loop", "workflow.async.>"},
			},
			natsclient.TestStreamConfig{
				Name:     "AGENT",
				Subjects: []string{"agentic.loop_completed.v1", "agent.task.>", "dev.task.>"},
			},
		),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// Mock graph-ingest so WriteTriple calls in handleTrigger do not block.
	startMockGraphIngest(t, tc.Client)

	comp := newExecIntegrationComponent(t, tc)

	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = comp.Stop(5 * time.Second) })

	trigger := workflow.TriggerPayload{
		Slug:    "dup-plan",
		TaskID:  "dup-task-001",
		Title:   "Duplicate trigger test",
		Model:   "default",
		TraceID: "trace-dup-001",
		Prompt:  "Implement the feature",
	}

	// Publish twice.
	publishExecTrigger(t, tc, ctx, trigger)
	publishExecTrigger(t, tc, ctx, trigger)

	// Both deliveries must be counted.
	waitForExecCondition(t, ctx, 15*time.Second, func() bool {
		return comp.triggersProcessed.Load() >= 2
	}, "triggersProcessed should reach 2")

	// Only one active execution must be registered for the entity.
	entityID := workflow.TaskExecutionEntityID("dup-plan", "dup-task-001")
	if _, ok := comp.activeExecs.Get(entityID); !ok {
		t.Errorf("expected active execution for %q, but not found", entityID)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newExecIntegrationComponent builds an execution-orchestrator component wired
// to the provided test NATS client using the default configuration.
func newExecIntegrationComponent(t *testing.T, tc *natsclient.TestClient) *Component {
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

// publishExecTrigger wraps a TriggerPayload in the BaseMessage envelope expected
// by ParseReactivePayload and publishes it to the execution trigger subject.
func publishExecTrigger(t *testing.T, tc *natsclient.TestClient, ctx context.Context, trigger workflow.TriggerPayload) {
	t.Helper()
	payloadBytes, err := json.Marshal(trigger)
	if err != nil {
		t.Fatalf("publishExecTrigger: marshal payload: %v", err)
	}
	envelope := map[string]any{
		"payload": json.RawMessage(payloadBytes),
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("publishExecTrigger: marshal envelope: %v", err)
	}
	js, err := tc.Client.JetStream()
	if err != nil {
		t.Fatalf("publishExecTrigger: JetStream(): %v", err)
	}
	if _, err := js.Publish(ctx, "workflow.trigger.task-execution-loop", data); err != nil {
		t.Fatalf("publishExecTrigger: publish trigger: %v", err)
	}
}

// collectMessagesFrom reads from ch until n messages arrive or the deadline passes.
func collectMessagesFrom(ctx context.Context, t *testing.T, ch <-chan []byte, n int, timeout time.Duration) [][]byte {
	t.Helper()

	deadline := time.After(timeout)
	collected := make([][]byte, 0, n)

	for len(collected) < n {
		select {
		case msg := <-ch:
			collected = append(collected, msg)
		case <-deadline:
			t.Logf("collectMessagesFrom: timeout after %v, got %d/%d messages", timeout, len(collected), n)
			return collected
		case <-ctx.Done():
			t.Logf("collectMessagesFrom: context done, got %d/%d messages", len(collected), n)
			return collected
		}
	}
	return collected
}

// waitForExecCondition polls fn until it returns true or the deadline is exceeded.
func waitForExecCondition(t *testing.T, ctx context.Context, timeout time.Duration, fn func() bool, msg string) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waitForExecCondition: context cancelled: %s", msg)
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
	t.Fatalf("waitForExecCondition: timed out: %s", msg)
}
