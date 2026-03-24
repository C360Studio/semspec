//go:build integration

package planapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/prompt"
	promptdomain "github.com/c360studio/semspec/prompt/domain"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// ---------------------------------------------------------------------------
// Stream subjects required by the coordinator integration tests.
// ---------------------------------------------------------------------------

var coordIntegTestStreamSubjects = []string{
	"workflow.trigger.plan-coordinator",
	"workflow.async.planner",
	"workflow.async.plan-reviewer",
	"workflow.async.requirement-generator",
	"workflow.async.scenario-generator",
	"workflow.events.>",
}

var agentIntegTestStreamSubjects = []string{
	"agent.complete.>",
}

var graphIntegTestStreamSubjects = []string{
	"graph.mutation.triple.add",
}

// ---------------------------------------------------------------------------
// Mock LLM — returns deterministic synthesis/focus results.
// Uses mockCoordLLM from coordinator_test.go (same package).
// ---------------------------------------------------------------------------

func newIntegrationMockCoordLLM() *mockCoordLLM {
	return &mockCoordLLM{
		responses: []*llm.Response{
			// Focus determination (call 0)
			{Content: `{"focus_areas":[{"area":"general","description":"Full plan"}]}`, Model: "mock"},
			// Synthesis (call 1) — called after all planners complete
			{Content: `{"goal":"Add goodbye endpoint","context":"Hello world project","scope":{"include":["api/"]}}`, Model: "mock"},
			// Synthesis retry if needed (call 2)
			{Content: `{"goal":"Add goodbye endpoint","context":"Hello world project","scope":{"include":["api/"]}}`, Model: "mock"},
		},
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestCoordInteg(t *testing.T, tc *natsclient.TestClient, autoApprove bool) *coordinator {
	t.Helper()

	mock := newIntegrationMockCoordLLM()
	cfg := CoordinatorConfig{
		MaxConcurrentPlanners: 3,
		TimeoutSeconds:        1800,
		MaxReviewIterations:   3,
		AutoApprove:           &autoApprove,
		DefaultCapability:     "planning",
	}

	// Build the same prompt assembler the real coordinator uses.
	registry := prompt.NewRegistry()
	registry.RegisterAll(promptdomain.Software()...)
	registry.Register(prompt.ToolGuidanceFragment(prompt.DefaultToolGuidance()))
	assembler := prompt.NewAssembler(registry)

	co := &coordinator{
		config:        cfg,
		natsClient:    tc.Client,
		logger:        slog.Default(),
		llmClient:     mock,
		modelRegistry: model.Global(),
		assembler:     assembler,
		tripleWriter: &graphutil.TripleWriter{
			NATSClient:    tc.Client,
			Logger:        slog.Default(),
			ComponentName: coordinatorName,
		},
	}
	return co
}

func setupCoordTestPlan(t *testing.T, ctx context.Context, slug string) {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

		if _, err := workflow.CreatePlan(ctx, nil, slug, "Test Plan"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
}

func publishCoordTrigger(t *testing.T, tc *natsclient.TestClient, ctx context.Context, slug string) {
	t.Helper()
	trigger := &payloads.PlanCoordinatorRequest{
		RequestID: "test-req-1",
		Slug:      slug,
		Title:     "Test Plan",
		TraceID:   "test-trace-1",
	}
	baseMsg := message.NewBaseMessage(trigger.Schema(), trigger, "test")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		t.Fatalf("marshal trigger: %v", err)
	}
	js, err := tc.Client.JetStream()
	if err != nil {
		t.Fatalf("get jetstream: %v", err)
	}
	if _, err := js.Publish(ctx, subjectCoordinationTrigger, data); err != nil {
		t.Fatalf("publish trigger: %v", err)
	}
}

func publishCoordLoopCompleted(t *testing.T, tc *natsclient.TestClient, ctx context.Context, taskID, result string) {
	t.Helper()
	event := &agentic.LoopCompletedEvent{
		LoopID:  fmt.Sprintf("loop-%s", taskID),
		TaskID:  taskID,
		Result:  result,
		Outcome: "success",
	}
	baseMsg := message.NewBaseMessage(event.Schema(), event, "test")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		t.Fatalf("marshal loop completed: %v", err)
	}
	js, err := tc.Client.JetStream()
	if err != nil {
		t.Fatalf("get jetstream: %v", err)
	}
	if _, err := js.Publish(ctx, "agent.complete.test", data); err != nil {
		t.Fatalf("publish loop completed: %v", err)
	}
}

// waitForCoordJS fetches one message from a JetStream consumer on the given subject.
func waitForCoordJS(t *testing.T, tc *natsclient.TestClient, ctx context.Context, stream, subject string, timeout time.Duration) jetstream.Msg {
	t.Helper()
	js, err := tc.Client.JetStream()
	if err != nil {
		t.Fatalf("get jetstream: %v", err)
	}
	s, err := js.Stream(ctx, stream)
	if err != nil {
		t.Fatalf("get stream %s: %v", stream, err)
	}
	cons, err := s.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name:          fmt.Sprintf("test-%d", time.Now().UnixNano()),
		FilterSubject: subject,
		AckPolicy:     jetstream.AckNonePolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	if err != nil {
		t.Fatalf("create consumer for %s: %v", subject, err)
	}
	msgs, err := cons.Fetch(1, jetstream.FetchMaxWait(timeout))
	if err != nil {
		t.Fatalf("fetch from %s: %v", subject, err)
	}
	for msg := range msgs.Messages() {
		return msg
	}
	t.Fatalf("no message on %s within %v", subject, timeout)
	return nil
}

// extractCoordTaskID parses a BaseMessage-wrapped payload and returns the TaskID field.
func extractCoordTaskID(t *testing.T, data []byte) string {
	t.Helper()
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	var task struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(envelope.Payload, &task); err != nil {
		t.Fatalf("unmarshal task_id: %v", err)
	}
	return task.TaskID
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestCoordIntegration_Round1_SynthesisDispatchesReviewer verifies that after
// planners complete and synthesis runs, the coordinator dispatches a
// reviewer (round 1) to workflow.async.plan-reviewer.
func TestCoordIntegration_Round1_SynthesisDispatchesReviewer(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithStreams(
			natsclient.TestStreamConfig{Name: "WORKFLOW", Subjects: coordIntegTestStreamSubjects},
			natsclient.TestStreamConfig{Name: "AGENT", Subjects: agentIntegTestStreamSubjects},
			natsclient.TestStreamConfig{Name: "GRAPH", Subjects: graphIntegTestStreamSubjects},
			natsclient.TestStreamConfig{Name: "ENTITY_INGEST", Subjects: []string{"graph.ingest.entity"}},
		),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	slug := "test-round1-reviewer"
	setupCoordTestPlan(t, ctx, slug)

	co := newTestCoordInteg(t, tc, true)
	if err := co.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { co.Stop() })

	// Trigger coordination.
	publishCoordTrigger(t, tc, ctx, slug)

	// Wait for planner dispatch.
	plannerMsg := waitForCoordJS(t, tc, ctx, "WORKFLOW", "workflow.async.planner", 10*time.Second)
	taskID := extractCoordTaskID(t, plannerMsg.Data())
	if taskID == "" {
		t.Fatal("planner task_id is empty")
	}

	// Simulate planner completion.
	publishCoordLoopCompleted(t, tc, ctx, taskID,
		`{"goal":"Add goodbye endpoint","context":"Hello world","scope":{"include":["api/"]}}`)

	// After synthesis, expect reviewer dispatch (round 1).
	reviewerMsg := waitForCoordJS(t, tc, ctx, "WORKFLOW", "workflow.async.plan-reviewer", 10*time.Second)
	if reviewerMsg == nil {
		t.Fatal("No reviewer dispatch after synthesis")
	}
	t.Log("PASS: Round 1 reviewer dispatched after synthesis")
}

// TestCoordIntegration_Round1_ApprovalTriggersRequirementGen verifies that when
// auto_approve=true and the reviewer approves in round 1, requirement
// generation is dispatched.
func TestCoordIntegration_Round1_ApprovalTriggersRequirementGen(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithStreams(
			natsclient.TestStreamConfig{Name: "WORKFLOW", Subjects: coordIntegTestStreamSubjects},
			natsclient.TestStreamConfig{Name: "AGENT", Subjects: agentIntegTestStreamSubjects},
			natsclient.TestStreamConfig{Name: "GRAPH", Subjects: graphIntegTestStreamSubjects},
			natsclient.TestStreamConfig{Name: "ENTITY_INGEST", Subjects: []string{"graph.ingest.entity"}},
		),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	slug := "test-round1-approval"
	setupCoordTestPlan(t, ctx, slug)

	co := newTestCoordInteg(t, tc, true)
	if err := co.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { co.Stop() })

	// Round 1: trigger → planner → synthesis → reviewer.
	publishCoordTrigger(t, tc, ctx, slug)

	plannerMsg := waitForCoordJS(t, tc, ctx, "WORKFLOW", "workflow.async.planner", 10*time.Second)
	plannerTaskID := extractCoordTaskID(t, plannerMsg.Data())
	publishCoordLoopCompleted(t, tc, ctx, plannerTaskID,
		`{"goal":"Test","context":"Test","scope":{}}`)

	reviewerMsg := waitForCoordJS(t, tc, ctx, "WORKFLOW", "workflow.async.plan-reviewer", 10*time.Second)
	reviewerTaskID := extractCoordTaskID(t, reviewerMsg.Data())

	// Reviewer approves → expect requirement-generator dispatch.
	publishCoordLoopCompleted(t, tc, ctx, reviewerTaskID,
		`{"verdict":"approved","summary":"Plan looks good"}`)

	reqGenMsg := waitForCoordJS(t, tc, ctx, "WORKFLOW", "workflow.async.requirement-generator", 10*time.Second)
	if reqGenMsg == nil {
		t.Fatal("No requirement-generator dispatch after round 1 approval")
	}
	t.Log("PASS: Round 1 approval → requirement generation dispatched")
}

// TestCoordIntegration_NeedsChanges_RetriesPlanning verifies that a
// "needs_changes" verdict in round 1 retries planning, not requirement gen.
func TestCoordIntegration_NeedsChanges_RetriesPlanning(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithStreams(
			natsclient.TestStreamConfig{Name: "WORKFLOW", Subjects: coordIntegTestStreamSubjects},
			natsclient.TestStreamConfig{Name: "AGENT", Subjects: agentIntegTestStreamSubjects},
			natsclient.TestStreamConfig{Name: "GRAPH", Subjects: graphIntegTestStreamSubjects},
			natsclient.TestStreamConfig{Name: "ENTITY_INGEST", Subjects: []string{"graph.ingest.entity"}},
		),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	slug := "test-needs-changes"
	setupCoordTestPlan(t, ctx, slug)

	co := newTestCoordInteg(t, tc, true)
	if err := co.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { co.Stop() })

	// Round 1: trigger → planner → synthesis → reviewer.
	publishCoordTrigger(t, tc, ctx, slug)

	plannerMsg := waitForCoordJS(t, tc, ctx, "WORKFLOW", "workflow.async.planner", 10*time.Second)
	plannerTaskID := extractCoordTaskID(t, plannerMsg.Data())
	publishCoordLoopCompleted(t, tc, ctx, plannerTaskID,
		`{"goal":"Test","context":"Test","scope":{}}`)

	reviewerMsg := waitForCoordJS(t, tc, ctx, "WORKFLOW", "workflow.async.plan-reviewer", 10*time.Second)
	reviewerTaskID := extractCoordTaskID(t, reviewerMsg.Data())

	// Reviewer rejects → expect planner retry (not requirement-generator).
	publishCoordLoopCompleted(t, tc, ctx, reviewerTaskID,
		`{"verdict":"needs_changes","summary":"Missing error handling"}`)

	retryMsg := waitForCoordJS(t, tc, ctx, "WORKFLOW", "workflow.async.planner", 10*time.Second)
	if retryMsg == nil {
		t.Fatal("No planner retry after round 1 rejection")
	}
	t.Log("PASS: Round 1 rejection → planner retry (correct)")
}

// TestCoordIntegration_HumanGate_PausesAtAwaitingHuman verifies that with
// auto_approve=false, the coordinator pauses at phaseAwaitingHuman after
// round 1 reviewer approves.
func TestCoordIntegration_HumanGate_PausesAtAwaitingHuman(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithStreams(
			natsclient.TestStreamConfig{Name: "WORKFLOW", Subjects: coordIntegTestStreamSubjects},
			natsclient.TestStreamConfig{Name: "AGENT", Subjects: agentIntegTestStreamSubjects},
			natsclient.TestStreamConfig{Name: "GRAPH", Subjects: graphIntegTestStreamSubjects},
			natsclient.TestStreamConfig{Name: "ENTITY_INGEST", Subjects: []string{"graph.ingest.entity"}},
		),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	slug := "test-human-gate"
	setupCoordTestPlan(t, ctx, slug)

	co := newTestCoordInteg(t, tc, false) // auto_approve=false
	if err := co.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { co.Stop() })

	// Round 1: trigger → planner → synthesis → reviewer.
	publishCoordTrigger(t, tc, ctx, slug)

	plannerMsg := waitForCoordJS(t, tc, ctx, "WORKFLOW", "workflow.async.planner", 10*time.Second)
	plannerTaskID := extractCoordTaskID(t, plannerMsg.Data())
	publishCoordLoopCompleted(t, tc, ctx, plannerTaskID,
		`{"goal":"Test","context":"Test","scope":{}}`)

	reviewerMsg := waitForCoordJS(t, tc, ctx, "WORKFLOW", "workflow.async.plan-reviewer", 10*time.Second)
	reviewerTaskID := extractCoordTaskID(t, reviewerMsg.Data())

	// Reviewer approves but auto_approve=false → should NOT dispatch requirement-generator.
	publishCoordLoopCompleted(t, tc, ctx, reviewerTaskID,
		`{"verdict":"approved","summary":"Plan approved"}`)

	// Give the coordinator time to process.
	time.Sleep(1 * time.Second)

	// Verify no requirement-generator was dispatched.
	js, _ := tc.Client.JetStream()
	s, _ := js.Stream(ctx, "WORKFLOW")
	cons, _ := s.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name:          fmt.Sprintf("test-verify-%d", time.Now().UnixNano()),
		FilterSubject: "workflow.async.requirement-generator",
		AckPolicy:     jetstream.AckNonePolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	msgs, _ := cons.Fetch(1, jetstream.FetchMaxWait(2*time.Second))
	count := 0
	for range msgs.Messages() {
		count++
	}
	if count > 0 {
		t.Fatal("Requirement-generator dispatched despite auto_approve=false")
	}

	t.Log("PASS: Human gate — no requirement-generator dispatch (awaiting human)")
}
