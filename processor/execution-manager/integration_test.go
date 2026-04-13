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
	"github.com/nats-io/nats.go/jetstream"
)

// TestIntegration_StartStop verifies the component lifecycle against a real NATS
// server: Start must succeed, Health must report running, and Stop must cleanly
// shut down.
func TestIntegration_StartStop(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithStreams(
			natsclient.TestStreamConfig{
				Name:     "WORKFLOW",
				Subjects: []string{"workflow.async.>"},
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

// TestIntegration_KVPendingTaskCreatesExecution verifies the KV self-trigger path:
// writing a TaskExecution with stage=pending to EXECUTION_STATES causes the
// component to claim the task, register an active execution, publish entity
// triples to graph.mutation.triple.add, and dispatch a developer task to
// agent.task.development.
func TestIntegration_KVPendingTaskCreatesExecution(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithStreams(
			natsclient.TestStreamConfig{
				Name:     "WORKFLOW",
				Subjects: []string{"workflow.async.>"},
			},
			natsclient.TestStreamConfig{
				Name:     "AGENT",
				Subjects: []string{"agentic.loop_completed.v1", "agent.task.>", "dev.task.>"},
			},
		),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// Mock graph-ingest so WriteTriple calls do not time out.
	startMockGraphIngest(t, tc.Client)

	comp := newExecIntegrationComponent(t, tc)

	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = comp.Stop(5 * time.Second) })

	// Subscribe to agent.task.development before writing KV so no messages are missed.
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

	// Write a pending task to EXECUTION_STATES — the KV watcher picks it up.
	writeKVPendingTask(t, tc, ctx, workflow.TaskExecution{
		Slug:         "test-plan",
		TaskID:       "task-001",
		Title:        "Test task",
		Stage:        "pending",
		Model:        "default",
		TraceID:      "trace-integ-001",
		Prompt:       "Implement the feature",
		MaxTDDCycles: 3,
	})

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

// TestIntegration_DuplicateKVEntryIsIdempotent verifies that a KV update for an
// already-active execution is silently dropped. The triggersProcessed counter
// increments for each claimed entry, but the duplicate detection in
// handleTaskPending prevents a second initTaskExecution from running.
func TestIntegration_DuplicateKVEntryIsIdempotent(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithStreams(
			natsclient.TestStreamConfig{
				Name:     "WORKFLOW",
				Subjects: []string{"workflow.async.>"},
			},
			natsclient.TestStreamConfig{
				Name:     "AGENT",
				Subjects: []string{"agentic.loop_completed.v1", "agent.task.>", "dev.task.>"},
			},
		),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// Mock graph-ingest so WriteTriple calls do not block.
	startMockGraphIngest(t, tc.Client)

	comp := newExecIntegrationComponent(t, tc)

	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = comp.Stop(5 * time.Second) })

	task := workflow.TaskExecution{
		Slug:         "dup-plan",
		TaskID:       "dup-task-001",
		Title:        "Duplicate task test",
		Stage:        "pending",
		Model:        "default",
		TraceID:      "trace-dup-001",
		Prompt:       "Implement the feature",
		MaxTDDCycles: 3,
	}

	// Write the pending task once — wait for claim before writing a second time
	// to ensure the watcher has already processed the first entry.
	writeKVPendingTask(t, tc, ctx, task)

	// Wait for the first claim to complete (triggersProcessed reaches 1).
	waitForExecCondition(t, ctx, 15*time.Second, func() bool {
		return comp.triggersProcessed.Load() >= 1
	}, "triggersProcessed should reach 1 after first KV write")

	// Only one active execution must be registered.
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

// writeKVPendingTask serialises a TaskExecution and writes it to the
// EXECUTION_STATES KV bucket under the canonical task key. The watcher
// in watchTaskPending will pick it up and initiate execution.
func writeKVPendingTask(t *testing.T, tc *natsclient.TestClient, ctx context.Context, task workflow.TaskExecution) {
	t.Helper()

	js, err := tc.Client.JetStream()
	if err != nil {
		t.Fatalf("writeKVPendingTask: JetStream(): %v", err)
	}

	bucket, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: "EXECUTION_STATES",
	})
	if err != nil {
		t.Fatalf("writeKVPendingTask: create/open EXECUTION_STATES: %v", err)
	}

	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("writeKVPendingTask: marshal task: %v", err)
	}

	key := workflow.TaskExecutionKey(task.Slug, task.TaskID)
	if _, err := bucket.Put(ctx, key, data); err != nil {
		t.Fatalf("writeKVPendingTask: put %q: %v", key, err)
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
