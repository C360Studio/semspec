//go:build integration

package scenarioorchestrator

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	nats "github.com/nats-io/nats.go"
)

// workflowStreamSubjects are the subjects covered by the WORKFLOW stream used
// in integration tests. They must include both the inbound trigger subject and
// the outbound execution-loop subject so that the component can consume and
// publish within the same stream.
var workflowStreamSubjects = []string{
	"scenario.orchestrate.*",
	"workflow.trigger.scenario-execution-loop",
}

// TestComponentStartStop verifies the component lifecycle against a real NATS
// server: Start must succeed and Stop must cleanly shut down the consumer.
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
// with two scenarios results in exactly two ScenarioExecutionRequest messages
// being published to workflow.trigger.scenario-execution-loop.
func TestDispatchScenarios_PublishesMessages(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithStreams(natsclient.TestStreamConfig{
			Name:     "WORKFLOW",
			Subjects: workflowStreamSubjects,
		}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	comp := newIntegrationComponent(t, tc)

	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = comp.Stop(5 * time.Second) })

	// Subscribe to the execution-loop subject before publishing the trigger so
	// no messages are missed.
	received := make(chan []byte, 10)
	nativeConn := tc.GetNativeConnection()
	sub, err := nativeConn.Subscribe("workflow.trigger.scenario-execution-loop", func(msg *nats.Msg) {
		data := make([]byte, len(msg.Data))
		copy(data, msg.Data)
		received <- data
	})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	// Publish the orchestration trigger with two distinct scenarios.
	trigger := OrchestratorTrigger{
		PlanSlug: "test-plan",
		TraceID:  "trace-integration-001",
		Scenarios: []ScenarioRef{
			{ScenarioID: "sc-alpha", Prompt: "Scenario alpha", Role: "developer", Model: "gpt-4"},
			{ScenarioID: "sc-beta", Prompt: "Scenario beta"},
		},
	}
	publishTrigger(t, tc, ctx, "scenario.orchestrate.test-plan", trigger)

	// Wait for both execution requests to arrive.
	collectedMsgs := collectMessages(ctx, t, received, 2, 20*time.Second)

	if len(collectedMsgs) != 2 {
		t.Fatalf("received %d execution requests, want 2", len(collectedMsgs))
	}

	// Parse and verify each received ScenarioExecutionRequest.
	seenIDs := make(map[string]bool)
	for _, raw := range collectedMsgs {
		req, err := payloads.ParseReactivePayload[payloads.ScenarioExecutionRequest](raw)
		if err != nil {
			t.Fatalf("ParseReactivePayload() error = %v", err)
		}

		if req.Slug != "test-plan" {
			t.Errorf("req.Slug = %q, want %q", req.Slug, "test-plan")
		}
		if req.TraceID != "trace-integration-001" {
			t.Errorf("req.TraceID = %q, want %q", req.TraceID, "trace-integration-001")
		}
		if req.ScenarioID == "" {
			t.Error("req.ScenarioID should not be empty")
		}
		seenIDs[req.ScenarioID] = true
	}

	if !seenIDs["sc-alpha"] {
		t.Error("sc-alpha was not dispatched")
	}
	if !seenIDs["sc-beta"] {
		t.Error("sc-beta was not dispatched")
	}

	// Verify metrics were updated.
	waitForCondition(t, ctx, 5*time.Second, func() bool {
		return comp.scenariosTriggered.Load() == 2
	}, "scenariosTriggered should reach 2")

	waitForCondition(t, ctx, 5*time.Second, func() bool {
		return comp.triggersProcessed.Load() >= 1
	}, "triggersProcessed should reach 1")
}

// TestDispatchScenarios_BoundedConcurrency verifies that a trigger with more
// scenarios than MaxConcurrent eventually dispatches all of them, respecting
// the concurrency limit without deadlocking.
func TestDispatchScenarios_BoundedConcurrency(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithStreams(natsclient.TestStreamConfig{
			Name:     "WORKFLOW",
			Subjects: workflowStreamSubjects,
		}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Use MaxConcurrent=2 with 5 scenarios to verify bounded dispatch works.
	cfg := DefaultConfig()
	cfg.MaxConcurrent = 2
	rawCfg, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	deps := component.Dependencies{NATSClient: tc.Client}
	compI, err := NewComponent(rawCfg, deps)
	if err != nil {
		t.Fatalf("NewComponent() error = %v", err)
	}
	comp := compI.(*Component)

	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = comp.Stop(5 * time.Second) })

	const scenarioCount = 5
	scenarios := make([]ScenarioRef, scenarioCount)
	for i := range scenarios {
		scenarios[i] = ScenarioRef{
			ScenarioID: scenarioID(i),
			Prompt:     promptFor(i),
		}
	}

	received := make(chan []byte, scenarioCount+2)
	nativeConn := tc.GetNativeConnection()
	sub, err := nativeConn.Subscribe("workflow.trigger.scenario-execution-loop", func(msg *nats.Msg) {
		data := make([]byte, len(msg.Data))
		copy(data, msg.Data)
		received <- data
	})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	trigger := OrchestratorTrigger{
		PlanSlug:  "bounded-plan",
		Scenarios: scenarios,
	}
	publishTrigger(t, tc, ctx, "scenario.orchestrate.bounded-plan", trigger)

	collectedMsgs := collectMessages(ctx, t, received, scenarioCount, 30*time.Second)

	if len(collectedMsgs) != scenarioCount {
		t.Fatalf("received %d execution requests, want %d", len(collectedMsgs), scenarioCount)
	}

	// Verify all scenario IDs were dispatched (no duplicates, no misses).
	seenIDs := make(map[string]bool, scenarioCount)
	for _, raw := range collectedMsgs {
		req, err := payloads.ParseReactivePayload[payloads.ScenarioExecutionRequest](raw)
		if err != nil {
			t.Fatalf("ParseReactivePayload() error = %v", err)
		}
		if seenIDs[req.ScenarioID] {
			t.Errorf("scenario %q dispatched more than once", req.ScenarioID)
		}
		seenIDs[req.ScenarioID] = true
	}

	for i := 0; i < scenarioCount; i++ {
		id := scenarioID(i)
		if !seenIDs[id] {
			t.Errorf("scenario %q was never dispatched", id)
		}
	}
}

// TestDispatchScenarios_EmptyList_Integration verifies that an
// OrchestratorTrigger with an empty scenarios list is ACK'd immediately
// without publishing any execution requests.
func TestDispatchScenarios_EmptyList_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithStreams(natsclient.TestStreamConfig{
			Name:     "WORKFLOW",
			Subjects: workflowStreamSubjects,
		}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	comp := newIntegrationComponent(t, tc)
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = comp.Stop(5 * time.Second) })

	received := make(chan []byte, 5)
	nativeConn := tc.GetNativeConnection()
	sub, err := nativeConn.Subscribe("workflow.trigger.scenario-execution-loop", func(msg *nats.Msg) {
		data := make([]byte, len(msg.Data))
		copy(data, msg.Data)
		received <- data
	})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	trigger := OrchestratorTrigger{
		PlanSlug:  "empty-plan",
		Scenarios: []ScenarioRef{},
	}
	publishTrigger(t, tc, ctx, "scenario.orchestrate.empty-plan", trigger)

	// Wait briefly to confirm nothing is published.
	shortCtx, shortCancel := context.WithTimeout(ctx, 3*time.Second)
	defer shortCancel()

	select {
	case <-received:
		t.Error("received unexpected execution request for empty scenarios list")
	case <-shortCtx.Done():
		// Correct: no messages published for empty scenarios.
	}

	// The trigger counter should still increment even for an empty list.
	waitForCondition(t, ctx, 5*time.Second, func() bool {
		return comp.triggersProcessed.Load() >= 1
	}, "triggersProcessed should reach 1")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newIntegrationComponent builds a component wired to the test NATS client.
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

// publishTrigger marshals an OrchestratorTrigger and publishes it to the
// JetStream stream so the component can consume it.
func publishTrigger(t *testing.T, tc *natsclient.TestClient, ctx context.Context, subject string, trigger OrchestratorTrigger) {
	t.Helper()

	data, err := json.Marshal(trigger)
	if err != nil {
		t.Fatalf("marshal trigger: %v", err)
	}

	js, err := tc.Client.JetStream()
	if err != nil {
		t.Fatalf("JetStream(): %v", err)
	}
	if _, err := js.Publish(ctx, subject, data); err != nil {
		t.Fatalf("publish trigger to %s: %v", subject, err)
	}
}

// collectMessages reads from ch until n messages arrive or the deadline passes.
func collectMessages(ctx context.Context, t *testing.T, ch <-chan []byte, n int, timeout time.Duration) [][]byte {
	t.Helper()

	deadline := time.After(timeout)
	collected := make([][]byte, 0, n)

	for len(collected) < n {
		select {
		case msg := <-ch:
			collected = append(collected, msg)
		case <-deadline:
			t.Logf("collectMessages: timeout after %v, got %d/%d messages", timeout, len(collected), n)
			return collected
		case <-ctx.Done():
			t.Logf("collectMessages: context done, got %d/%d messages", len(collected), n)
			return collected
		}
	}
	return collected
}

// waitForCondition polls fn until it returns true or deadline is exceeded.
func waitForCondition(t *testing.T, ctx context.Context, timeout time.Duration, fn func() bool, msg string) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waitForCondition: context cancelled: %s", msg)
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
	t.Fatalf("waitForCondition: timed out: %s", msg)
}

// scenarioID generates a deterministic scenario ID from an index.
func scenarioID(i int) string {
	ids := []string{"sc-0", "sc-1", "sc-2", "sc-3", "sc-4", "sc-5", "sc-6", "sc-7", "sc-8", "sc-9"}
	if i < len(ids) {
		return ids[i]
	}
	return "sc-unknown"
}

// promptFor returns a test prompt for the given scenario index.
func promptFor(i int) string {
	prompts := []string{
		"Test scenario 0", "Test scenario 1", "Test scenario 2",
		"Test scenario 3", "Test scenario 4", "Test scenario 5",
		"Test scenario 6", "Test scenario 7", "Test scenario 8",
		"Test scenario 9",
	}
	if i < len(prompts) {
		return prompts[i]
	}
	return "Test scenario unknown"
}
