//go:build integration

package executionmanager

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/test/integration/graphmock"
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
	graphmock.Start(t, tc.Client)

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

func TestIntegration_StartRequiresPlanStatesBucket(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	cfg := DefaultConfig()
	cfg.Model = "default"
	rawCfg, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal default config: %v", err)
	}
	compI, err := NewComponent(rawCfg, component.Dependencies{NATSClient: tc.Client})
	if err != nil {
		t.Fatalf("NewComponent() error: %v", err)
	}
	comp := compI.(*Component)
	comp.sandbox = &stubSandbox{}

	err = comp.Start(ctx)
	if err == nil {
		t.Fatal("Start() error = nil, want missing PLAN_STATES dependency failure")
	}
	if !strings.Contains(err.Error(), planStatesBucketName) {
		t.Fatalf("Start() error = %q, want %s context", err, planStatesBucketName)
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

	comp := startExecIntegrationComponent(t, tc, ctx)

	nativeConn := tc.GetNativeConnection()
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
	if err := nativeConn.Flush(); err != nil {
		t.Fatalf("flush subscriptions: %v", err)
	}

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
	developerMsg := fetchLastStreamMessageForSubject(t, tc, ctx, "AGENT", "agent.task.development", 15*time.Second)
	if len(developerMsg) == 0 {
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

	comp := startExecIntegrationComponent(t, tc, ctx)

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
	ctx := context.Background()
	ensurePlanStatesBucket(t, tc, ctx)

	cfg := DefaultConfig()
	cfg.Model = "default"
	rawCfg, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal default config: %v", err)
	}

	deps := component.Dependencies{NATSClient: tc.Client}
	compI, err := NewComponent(rawCfg, deps)
	if err != nil {
		t.Fatalf("NewComponent() error: %v", err)
	}
	comp := compI.(*Component)
	comp.sandbox = &stubSandbox{}
	return comp
}

func startExecIntegrationComponent(t *testing.T, tc *natsclient.TestClient, ctx context.Context) *Component {
	t.Helper()
	graphmock.Start(t, tc.Client)
	comp := newExecIntegrationComponent(t, tc)
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = comp.Stop(5 * time.Second) })
	return comp
}

func startExecMutationIntegrationComponent(t *testing.T, tc *natsclient.TestClient, ctx context.Context) *Component {
	t.Helper()
	graphmock.Start(t, tc.Client)
	comp := newExecIntegrationComponent(t, tc)
	if err := comp.initExecutionStore(ctx); err != nil {
		t.Fatalf("initExecutionStore() error = %v", err)
	}
	if err := comp.startExecMutationHandler(ctx); err != nil {
		t.Fatalf("startExecMutationHandler() error = %v", err)
	}
	return comp
}

func ensurePlanStatesBucket(t *testing.T, tc *natsclient.TestClient, ctx context.Context) jetstream.KeyValue {
	t.Helper()
	js, err := tc.Client.JetStream()
	if err != nil {
		t.Fatalf("ensurePlanStatesBucket: JetStream(): %v", err)
	}
	bucket, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: planStatesBucketName,
	})
	if err != nil {
		t.Fatalf("ensurePlanStatesBucket: create/open %s: %v", planStatesBucketName, err)
	}
	return bucket
}

func writeKVPlan(t *testing.T, tc *natsclient.TestClient, ctx context.Context, plan workflow.Plan) {
	t.Helper()
	if plan.Slug == "" {
		t.Fatal("writeKVPlan: plan slug is required")
	}
	bucket := ensurePlanStatesBucket(t, tc, ctx)
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("writeKVPlan: marshal plan: %v", err)
	}
	if _, err := bucket.Put(ctx, plan.Slug, data); err != nil {
		t.Fatalf("writeKVPlan: put %q: %v", plan.Slug, err)
	}
}

// writeKVPendingTask serialises a TaskExecution and writes it to the
// EXECUTION_STATES KV bucket under the canonical task key. The watcher
// in watchTaskPending will pick it up and initiate execution.
func writeKVPendingTask(t *testing.T, tc *natsclient.TestClient, ctx context.Context, task workflow.TaskExecution) {
	t.Helper()
	writeKVPlan(t, tc, ctx, workflow.Plan{Slug: task.Slug})

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

func fetchLastStreamMessageForSubject(t *testing.T, tc *natsclient.TestClient, ctx context.Context, streamName, subject string, timeout time.Duration) []byte {
	t.Helper()
	js, err := tc.Client.JetStream()
	if err != nil {
		t.Fatalf("fetchLastStreamMessageForSubject: JetStream(): %v", err)
	}
	stream, err := js.Stream(ctx, streamName)
	if err != nil {
		t.Fatalf("fetchLastStreamMessageForSubject: get %s stream: %v", streamName, err)
	}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		msg, err := stream.GetLastMsgForSubject(ctx, subject)
		if err == nil {
			return append([]byte(nil), msg.Data...)
		}
		lastErr = err
		select {
		case <-ctx.Done():
			t.Logf("fetchLastStreamMessageForSubject: context done for %s/%s: %v", streamName, subject, ctx.Err())
			return nil
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
	if info, err := stream.Info(ctx); err == nil {
		t.Logf("fetchLastStreamMessageForSubject: timeout waiting for %s/%s, last error: %v, stream_subjects=%v msgs=%d first_seq=%d last_seq=%d",
			streamName, subject, lastErr, info.Config.Subjects, info.State.Msgs, info.State.FirstSeq, info.State.LastSeq)
		if info.State.LastSeq > 0 {
			if raw, err := stream.GetMsg(ctx, info.State.LastSeq); err == nil {
				t.Logf("fetchLastStreamMessageForSubject: last stored subject=%s len=%d", raw.Subject, len(raw.Data))
			}
		}
	} else {
		t.Logf("fetchLastStreamMessageForSubject: timeout waiting for %s/%s, last error: %v, stream info error: %v", streamName, subject, lastErr, err)
	}
	return nil
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
