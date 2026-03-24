//go:build integration

package scenarioorchestrator

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	nats "github.com/nats-io/nats.go"
)

// workflowStreamSubjects are the subjects covered by the WORKFLOW stream used
// in integration tests. They must include both the inbound trigger subject and
// the outbound execution-loop subject so that the component can consume and
// publish within the same stream.
var workflowStreamSubjects = []string{
	"scenario.orchestrate.*",
	"workflow.trigger.requirement-execution-loop",
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
// for a plan with two requirements (each having a pending scenario) results in
// exactly two RequirementExecutionRequest messages being published to
// workflow.trigger.requirement-execution-loop.
func TestDispatchScenarios_PublishesMessages(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", repoRoot)

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

	// Write plan data to disk: two requirements, one pending scenario each.
	const planSlug = "test-plan"
		if _, err := workflow.CreatePlan(ctx, nil, planSlug, "Test Plan"); err != nil {
		t.Fatalf("CreatePlan() error: %v", err)
	}

	requirements := []workflow.Requirement{
		makeReq("req-alpha"),
		makeReq("req-beta"),
	}
	if err := workflow.SaveRequirements(ctx, nil, requirements, planSlug); err != nil {
		t.Fatalf("SaveRequirements() error: %v", err)
	}

	scenarios := []workflow.Scenario{
		makeScenario("sc-alpha-1", "req-alpha", workflow.ScenarioStatusPending),
		makeScenario("sc-beta-1", "req-beta", workflow.ScenarioStatusPending),
	}
	if err := workflow.SaveScenarios(ctx, nil, scenarios, planSlug); err != nil {
		t.Fatalf("SaveScenarios() error: %v", err)
	}

	// Subscribe to the execution-loop subject before publishing the trigger so
	// no messages are missed.
	received := make(chan []byte, 10)
	sub := subscribeExecLoop(t, tc, received)
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	trigger := OrchestratorTrigger{
		PlanSlug: planSlug,
		TraceID:  "trace-integration-001",
	}
	publishTrigger(t, tc, ctx, "scenario.orchestrate."+planSlug, trigger)

	// Wait for both execution requests to arrive.
	collectedMsgs := collectMessages(ctx, t, received, 2, 20*time.Second)

	if len(collectedMsgs) != 2 {
		t.Fatalf("received %d execution requests, want 2", len(collectedMsgs))
	}

	// Parse and verify each received RequirementExecutionRequest.
	seenIDs := make(map[string]bool)
	for _, raw := range collectedMsgs {
		req, err := payloads.ParseReactivePayload[payloads.RequirementExecutionRequest](raw)
		if err != nil {
			t.Fatalf("ParseReactivePayload() error = %v", err)
		}

		if req.Slug != planSlug {
			t.Errorf("req.Slug = %q, want %q", req.Slug, planSlug)
		}
		if req.TraceID != "trace-integration-001" {
			t.Errorf("req.TraceID = %q, want %q", req.TraceID, "trace-integration-001")
		}
		if req.RequirementID == "" {
			t.Error("req.RequirementID should not be empty")
		}
		seenIDs[req.RequirementID] = true
	}

	if !seenIDs["req-alpha"] {
		t.Error("req-alpha was not dispatched")
	}
	if !seenIDs["req-beta"] {
		t.Error("req-beta was not dispatched")
	}

	// Verify metrics were updated.
	waitForCondition(t, ctx, 5*time.Second, func() bool {
		return comp.requirementsTriggered.Load() == 2
	}, "requirementsTriggered should reach 2")

	waitForCondition(t, ctx, 5*time.Second, func() bool {
		return comp.triggersProcessed.Load() >= 1
	}, "triggersProcessed should reach 1")
}

// TestDispatchScenarios_BoundedConcurrency verifies that a trigger for a plan
// with more requirements than MaxConcurrent eventually dispatches all of them,
// respecting the concurrency limit without deadlocking.
func TestDispatchScenarios_BoundedConcurrency(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", repoRoot)

	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithStreams(natsclient.TestStreamConfig{
			Name:     "WORKFLOW",
			Subjects: workflowStreamSubjects,
		}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Use MaxConcurrent=2 with 5 requirements to verify bounded dispatch works.
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

	const requirementCount = 5
	const planSlug = "bounded-plan"

	// Write plan data: N independent requirements, one pending scenario each.
		if _, err := workflow.CreatePlan(ctx, nil, planSlug, "Bounded Plan"); err != nil {
		t.Fatalf("CreatePlan() error: %v", err)
	}

	requirements := make([]workflow.Requirement, requirementCount)
	scenarios := make([]workflow.Scenario, requirementCount)
	for i := range requirements {
		reqID := requirementID(i)
		requirements[i] = makeReq(reqID)
		scenarios[i] = makeScenario(scenarioID(i), reqID, workflow.ScenarioStatusPending)
	}

	if err := workflow.SaveRequirements(ctx, nil, requirements, planSlug); err != nil {
		t.Fatalf("SaveRequirements() error: %v", err)
	}
	if err := workflow.SaveScenarios(ctx, nil, scenarios, planSlug); err != nil {
		t.Fatalf("SaveScenarios() error: %v", err)
	}

	received := make(chan []byte, requirementCount+2)
	sub := subscribeExecLoop(t, tc, received)
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	trigger := OrchestratorTrigger{
		PlanSlug: planSlug,
	}
	publishTrigger(t, tc, ctx, "scenario.orchestrate."+planSlug, trigger)

	collectedMsgs := collectMessages(ctx, t, received, requirementCount, 30*time.Second)

	if len(collectedMsgs) != requirementCount {
		t.Fatalf("received %d execution requests, want %d", len(collectedMsgs), requirementCount)
	}

	// Verify all requirement IDs were dispatched (no duplicates, no misses).
	seenIDs := make(map[string]bool, requirementCount)
	for _, raw := range collectedMsgs {
		req, err := payloads.ParseReactivePayload[payloads.RequirementExecutionRequest](raw)
		if err != nil {
			t.Fatalf("ParseReactivePayload() error = %v", err)
		}
		if seenIDs[req.RequirementID] {
			t.Errorf("requirement %q dispatched more than once", req.RequirementID)
		}
		seenIDs[req.RequirementID] = true
	}

	for i := 0; i < requirementCount; i++ {
		id := requirementID(i)
		if !seenIDs[id] {
			t.Errorf("requirement %q was never dispatched", id)
		}
	}
}

// TestDispatchScenarios_EmptyList_Integration verifies that an
// OrchestratorTrigger for a plan with no requirements is ACK'd immediately
// without publishing any execution requests.
func TestDispatchScenarios_EmptyList_Integration(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", repoRoot)

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

	// Plan directory exists on disk but has no requirements written.
		if _, err := workflow.CreatePlan(ctx, nil, "empty-plan", "Empty Plan"); err != nil {
		t.Fatalf("CreatePlan() error: %v", err)
	}

	received := make(chan []byte, 5)
	sub := subscribeExecLoop(t, tc, received)
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	trigger := OrchestratorTrigger{
		PlanSlug: "empty-plan",
	}
	publishTrigger(t, tc, ctx, "scenario.orchestrate.empty-plan", trigger)

	// Wait briefly to confirm nothing is published.
	shortCtx, shortCancel := context.WithTimeout(ctx, 3*time.Second)
	defer shortCancel()

	select {
	case <-received:
		t.Error("received unexpected execution request for plan with no requirements")
	case <-shortCtx.Done():
		// Correct: no messages published for empty requirements.
	}

	// The trigger counter should still increment even for an empty list.
	waitForCondition(t, ctx, 5*time.Second, func() bool {
		return comp.triggersProcessed.Load() >= 1
	}, "triggersProcessed should reach 1")
}

// ---------------------------------------------------------------------------
// DAG-gating integration tests
// ---------------------------------------------------------------------------

// TestDAGGating_RootRequirementsDispatchImmediately verifies that two independent
// requirements (no DependsOn) both have their pending scenarios dispatched in a
// single orchestration cycle. Root requirements are never blocked by upstream.
func TestDAGGating_RootRequirementsDispatchImmediately(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", repoRoot)

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

	// Persist plan data: two independent requirements, one scenario each.
	const planSlug = "dag-root-test"
		if _, err := workflow.CreatePlan(ctx, nil, planSlug, "DAG Root Test"); err != nil {
		t.Fatalf("CreatePlan() error: %v", err)
	}

	requirements := []workflow.Requirement{
		makeReq("req-alpha"),
		makeReq("req-beta"),
	}
	if err := workflow.SaveRequirements(ctx, nil, requirements, planSlug); err != nil {
		t.Fatalf("SaveRequirements() error: %v", err)
	}

	scenarios := []workflow.Scenario{
		makeScenario("sc-alpha-1", "req-alpha", workflow.ScenarioStatusPending),
		makeScenario("sc-beta-1", "req-beta", workflow.ScenarioStatusPending),
	}
	if err := workflow.SaveScenarios(ctx, nil, scenarios, planSlug); err != nil {
		t.Fatalf("SaveScenarios() error: %v", err)
	}

	received := make(chan []byte, 10)
	sub := subscribeExecLoop(t, tc, received)
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	trigger := OrchestratorTrigger{
		PlanSlug: planSlug,
		TraceID:  "trace-dag-root-001",
	}
	publishTrigger(t, tc, ctx, "scenario.orchestrate."+planSlug, trigger)

	// Both root requirements must be dispatched.
	msgs := collectMessages(ctx, t, received, 2, 20*time.Second)
	if len(msgs) != 2 {
		t.Fatalf("got %d dispatched requirements, want 2", len(msgs))
	}

	seenIDs := parsedRequirementIDs(t, msgs)
	if !seenIDs["req-alpha"] {
		t.Error("req-alpha was not dispatched")
	}
	if !seenIDs["req-beta"] {
		t.Error("req-beta was not dispatched")
	}
}

// TestDAGGating_DependentRequirementBlocked verifies that a requirement with an
// unsatisfied upstream dependency blocks its scenario from dispatch until the
// upstream requirement is complete (all its scenarios passing).
func TestDAGGating_DependentRequirementBlocked(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", repoRoot)

	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithStreams(natsclient.TestStreamConfig{
			Name:     "WORKFLOW",
			Subjects: workflowStreamSubjects,
		}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	comp := newIntegrationComponent(t, tc)
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = comp.Stop(5 * time.Second) })

	// Req-A (root) → Req-B (depends on A).
	const planSlug = "dag-blocked-test"
		if _, err := workflow.CreatePlan(ctx, nil, planSlug, "DAG Blocked Test"); err != nil {
		t.Fatalf("CreatePlan() error: %v", err)
	}

	requirements := []workflow.Requirement{
		makeReq("req-a"),
		makeReq("req-b", "req-a"),
	}
	if err := workflow.SaveRequirements(ctx, nil, requirements, planSlug); err != nil {
		t.Fatalf("SaveRequirements() error: %v", err)
	}

	// Phase 1: both scenarios pending — req-b must be blocked.
	scenarios := []workflow.Scenario{
		makeScenario("sc-a-1", "req-a", workflow.ScenarioStatusPending),
		makeScenario("sc-b-1", "req-b", workflow.ScenarioStatusPending),
	}
	if err := workflow.SaveScenarios(ctx, nil, scenarios, planSlug); err != nil {
		t.Fatalf("SaveScenarios() error: %v", err)
	}

	received := make(chan []byte, 10)
	sub := subscribeExecLoop(t, tc, received)
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	trigger := OrchestratorTrigger{
		PlanSlug: planSlug,
		TraceID:  "trace-dag-blocked-001",
	}
	publishTrigger(t, tc, ctx, "scenario.orchestrate."+planSlug, trigger)

	// Only req-a should be dispatched.
	msgs := collectMessages(ctx, t, received, 1, 10*time.Second)
	if len(msgs) != 1 {
		t.Fatalf("phase 1: got %d dispatched requirements, want 1", len(msgs))
	}
	seenIDs := parsedRequirementIDs(t, msgs)
	if !seenIDs["req-a"] {
		t.Errorf("phase 1: expected req-a dispatched, got %v", seenIDs)
	}
	if seenIDs["req-b"] {
		t.Error("phase 1: req-b should be blocked but was dispatched")
	}

	// Drain the channel before the second trigger.
	drainChannel(received)

	// Phase 2: mark req-a's scenario as passing so req-b becomes unblocked.
	scenarios[0].Status = workflow.ScenarioStatusPassing
	if err := workflow.SaveScenarios(ctx, nil, scenarios, planSlug); err != nil {
		t.Fatalf("SaveScenarios() phase 2 error: %v", err)
	}

	trigger2 := OrchestratorTrigger{
		PlanSlug: planSlug,
		TraceID:  "trace-dag-blocked-002",
	}
	publishTrigger(t, tc, ctx, "scenario.orchestrate."+planSlug, trigger2)

	// Now req-b should be dispatched.
	msgs2 := collectMessages(ctx, t, received, 1, 20*time.Second)
	if len(msgs2) != 1 {
		t.Fatalf("phase 2: got %d dispatched requirements, want 1", len(msgs2))
	}
	seenIDs2 := parsedRequirementIDs(t, msgs2)
	if !seenIDs2["req-b"] {
		t.Errorf("phase 2: expected req-b dispatched after req-a complete, got %v", seenIDs2)
	}
}

// TestDAGGating_FailingScenarioBlocksDownstream verifies that a requirement whose
// scenario is failing is treated as incomplete, blocking any dependent requirement.
func TestDAGGating_FailingScenarioBlocksDownstream(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", repoRoot)

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

	const planSlug = "dag-fail-block-test"
		if _, err := workflow.CreatePlan(ctx, nil, planSlug, "DAG Fail Block Test"); err != nil {
		t.Fatalf("CreatePlan() error: %v", err)
	}

	requirements := []workflow.Requirement{
		makeReq("req-a"),
		makeReq("req-b", "req-a"),
	}
	if err := workflow.SaveRequirements(ctx, nil, requirements, planSlug); err != nil {
		t.Fatalf("SaveRequirements() error: %v", err)
	}

	// Req-A's scenario is failing — req-a is incomplete, req-b is blocked.
	scenarios := []workflow.Scenario{
		makeScenario("sc-a-1", "req-a", workflow.ScenarioStatusFailing),
		makeScenario("sc-b-1", "req-b", workflow.ScenarioStatusPending),
	}
	if err := workflow.SaveScenarios(ctx, nil, scenarios, planSlug); err != nil {
		t.Fatalf("SaveScenarios() error: %v", err)
	}

	received := make(chan []byte, 5)
	sub := subscribeExecLoop(t, tc, received)
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	trigger := OrchestratorTrigger{
		PlanSlug: planSlug,
		TraceID:  "trace-dag-fail-001",
	}
	publishTrigger(t, tc, ctx, "scenario.orchestrate."+planSlug, trigger)

	// req-a should be dispatched (it has a failing scenario — needs retry).
	// req-b should NOT be dispatched (blocked by incomplete req-a).
	msgs := collectMessages(ctx, t, received, 1, 10*time.Second)
	if len(msgs) != 1 {
		t.Fatalf("got %d dispatched requirements, want 1 (only req-a)", len(msgs))
	}
	seenIDs := parsedRequirementIDs(t, msgs)
	if !seenIDs["req-a"] {
		t.Errorf("expected req-a dispatched for retry, got %v", seenIDs)
	}
	if seenIDs["req-b"] {
		t.Error("req-b should be blocked by failing req-a")
	}

	// The trigger should still be counted as processed.
	waitForCondition(t, ctx, 5*time.Second, func() bool {
		return comp.triggersProcessed.Load() >= 1
	}, "triggersProcessed should reach 1")
}

// TestDAGGating_DiamondDependency exercises the four-node diamond pattern:
//
//	A → B → D
//	A → C → D
//
// Only A can run first; B and C unblock once A passes; D unblocks only when
// both B and C pass.
func TestDAGGating_DiamondDependency(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", repoRoot)

	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithStreams(natsclient.TestStreamConfig{
			Name:     "WORKFLOW",
			Subjects: workflowStreamSubjects,
		}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	comp := newIntegrationComponent(t, tc)
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = comp.Stop(5 * time.Second) })

	const planSlug = "dag-diamond-test"
		if _, err := workflow.CreatePlan(ctx, nil, planSlug, "DAG Diamond Test"); err != nil {
		t.Fatalf("CreatePlan() error: %v", err)
	}

	requirements := []workflow.Requirement{
		makeReq("req-a"),
		makeReq("req-b", "req-a"),
		makeReq("req-c", "req-a"),
		makeReq("req-d", "req-b", "req-c"),
	}
	if err := workflow.SaveRequirements(ctx, nil, requirements, planSlug); err != nil {
		t.Fatalf("SaveRequirements() error: %v", err)
	}

	// All scenarios start pending.
	scenarios := []workflow.Scenario{
		makeScenario("sc-a-1", "req-a", workflow.ScenarioStatusPending),
		makeScenario("sc-b-1", "req-b", workflow.ScenarioStatusPending),
		makeScenario("sc-c-1", "req-c", workflow.ScenarioStatusPending),
		makeScenario("sc-d-1", "req-d", workflow.ScenarioStatusPending),
	}
	saveScenariosHelper(t, ctx, m, scenarios, planSlug)

	received := make(chan []byte, 20)
	sub := subscribeExecLoop(t, tc, received)
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	// --- Step 1: all pending, only A should dispatch. ---
	publishTrigger(t, tc, ctx, "scenario.orchestrate."+planSlug, OrchestratorTrigger{
		PlanSlug: planSlug,
		TraceID:  "trace-diamond-step1",
	})
	msgs := collectMessages(ctx, t, received, 1, 15*time.Second)
	if len(msgs) != 1 {
		t.Fatalf("step 1: got %d dispatched, want 1 (only A)", len(msgs))
	}
	if ids := parsedRequirementIDs(t, msgs); !ids["req-a"] {
		t.Errorf("step 1: expected req-a, got %v", ids)
	}
	drainChannel(received)

	// --- Step 2: mark A passing — B and C should both dispatch. ---
	scenarios[0].Status = workflow.ScenarioStatusPassing
	saveScenariosHelper(t, ctx, m, scenarios, planSlug)

	publishTrigger(t, tc, ctx, "scenario.orchestrate."+planSlug, OrchestratorTrigger{
		PlanSlug: planSlug,
		TraceID:  "trace-diamond-step2",
	})
	msgs = collectMessages(ctx, t, received, 2, 20*time.Second)
	if len(msgs) != 2 {
		t.Fatalf("step 2: got %d dispatched, want 2 (B and C)", len(msgs))
	}
	ids := parsedRequirementIDs(t, msgs)
	if !ids["req-b"] {
		t.Errorf("step 2: req-b not dispatched after A passed, got %v", ids)
	}
	if !ids["req-c"] {
		t.Errorf("step 2: req-c not dispatched after A passed, got %v", ids)
	}
	if ids["req-d"] {
		t.Error("step 2: req-d should still be blocked (req-c not yet passing)")
	}
	drainChannel(received)

	// --- Step 3: mark B passing only — D still blocked because C is pending.
	// C should be re-dispatched (still pending, deps satisfied).
	scenarios[1].Status = workflow.ScenarioStatusPassing
	saveScenariosHelper(t, ctx, m, scenarios, planSlug)

	publishTrigger(t, tc, ctx, "scenario.orchestrate."+planSlug, OrchestratorTrigger{
		PlanSlug: planSlug,
		TraceID:  "trace-diamond-step3",
	})
	msgs = collectMessages(ctx, t, received, 1, 10*time.Second)
	if len(msgs) != 1 {
		t.Fatalf("step 3: got %d dispatched, want 1 (only C re-dispatched)", len(msgs))
	}
	ids = parsedRequirementIDs(t, msgs)
	if !ids["req-c"] {
		t.Errorf("step 3: expected req-c re-dispatched, got %v", ids)
	}
	if ids["req-d"] {
		t.Error("step 3: req-d should remain blocked until req-c also passes")
	}
	drainChannel(received)

	// --- Step 4: mark C passing — D should now dispatch. ---
	scenarios[2].Status = workflow.ScenarioStatusPassing
	saveScenariosHelper(t, ctx, m, scenarios, planSlug)

	publishTrigger(t, tc, ctx, "scenario.orchestrate."+planSlug, OrchestratorTrigger{
		PlanSlug: planSlug,
		TraceID:  "trace-diamond-step4",
	})
	msgs = collectMessages(ctx, t, received, 1, 20*time.Second)
	if len(msgs) != 1 {
		t.Fatalf("step 4: got %d dispatched, want 1 (only D)", len(msgs))
	}
	if ids := parsedRequirementIDs(t, msgs); !ids["req-d"] {
		t.Errorf("step 4: expected req-d dispatched after B and C passed, got %v", ids)
	}
}

// ---------------------------------------------------------------------------
// DAG-gating test helpers
// ---------------------------------------------------------------------------

// newIntegrationComponentWithRoot builds a component wired to the test NATS
// client. The caller is responsible for setting SEMSPEC_REPO_PATH before
// calling NewComponent (done via t.Setenv in each test).
func newIntegrationComponentWithRoot(t *testing.T, tc *natsclient.TestClient) *Component {
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

// subscribeExecLoop registers a Core NATS subscription on the requirement
// execution-loop subject, forwarding raw message bytes into ch.
func subscribeExecLoop(t *testing.T, tc *natsclient.TestClient, ch chan<- []byte) *nats.Subscription {
	t.Helper()
	sub, err := tc.GetNativeConnection().Subscribe(
		"workflow.trigger.requirement-execution-loop",
		func(msg *nats.Msg) {
			data := make([]byte, len(msg.Data))
			copy(data, msg.Data)
			ch <- data
		},
	)
	if err != nil {
		t.Fatalf("Subscribe() error: %v", err)
	}
	return sub
}

// parsedRequirementIDs parses a slice of raw BaseMessage bytes and returns the
// set of RequirementIDs contained in the RequirementExecutionRequest payloads.
func parsedRequirementIDs(t *testing.T, msgs [][]byte) map[string]bool {
	t.Helper()
	ids := make(map[string]bool, len(msgs))
	for _, raw := range msgs {
		req, err := payloads.ParseReactivePayload[payloads.RequirementExecutionRequest](raw)
		if err != nil {
			t.Fatalf("ParseReactivePayload() error: %v", err)
		}
		ids[req.RequirementID] = true
	}
	return ids
}

// assertNoMessages waits for duration and fails the test if any message
// arrives on ch within that window.
func assertNoMessages(t *testing.T, ctx context.Context, ch <-chan []byte, duration time.Duration, msg string) {
	t.Helper()
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ch:
		t.Errorf("assertNoMessages: unexpected message received — %s", msg)
	case <-timer.C:
		// Correct: no messages arrived within the window.
	case <-ctx.Done():
		t.Fatalf("assertNoMessages: context cancelled — %s", msg)
	}
}

// drainChannel discards all messages currently buffered in ch without blocking.
func drainChannel(ch <-chan []byte) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// saveScenariosHelper calls SaveScenarios and fails the test on error.
func saveScenariosHelper(t *testing.T, ctx context.Context, kv *natsclient.KVStore, scenarios []workflow.Scenario, slug string) {
	t.Helper()
	if err := workflow.SaveScenarios(ctx, nil, scenarios, slug); err != nil {
		t.Fatalf("SaveScenarios() error: %v", err)
	}
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

// publishTrigger wraps an OrchestratorTrigger in a BaseMessage envelope and
// publishes it to the JetStream stream so the component can consume it.
func publishTrigger(t *testing.T, tc *natsclient.TestClient, ctx context.Context, subject string, trigger OrchestratorTrigger) {
	t.Helper()

	typed := &payloads.ScenarioOrchestrationTrigger{
		PlanSlug: trigger.PlanSlug,
		TraceID:  trigger.TraceID,
	}

	baseMsg := message.NewBaseMessage(typed.Schema(), typed, "test")
	data, err := json.Marshal(baseMsg)
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

// requirementID generates a deterministic requirement ID from an index.
func requirementID(i int) string {
	ids := []string{"req-0", "req-1", "req-2", "req-3", "req-4", "req-5", "req-6", "req-7", "req-8", "req-9"}
	if i < len(ids) {
		return ids[i]
	}
	return "req-unknown"
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
