//go:build integration

package plancoordinator

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
// Stream subjects required by the plan-coordinator integration tests.
// ---------------------------------------------------------------------------

var coordTestStreamSubjects = []string{
	"workflow.trigger.plan-coordinator",
	"workflow.async.planner",
	"workflow.async.plan-reviewer",
	"workflow.async.requirement-generator",
	"workflow.async.scenario-generator",
	"workflow.events.>",
}

var agentTestStreamSubjects = []string{
	"agent.complete.>",
}

var graphTestStreamSubjects = []string{
	"graph.mutation.triple.add",
}

// ---------------------------------------------------------------------------
// Mock LLM — returns deterministic synthesis/focus results.
// Uses the existing mockLLM from component_test.go (same package).
// ---------------------------------------------------------------------------

func newIntegrationMockLLM() *mockLLM {
	return &mockLLM{
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

func newTestCoordinator(t *testing.T, tc *natsclient.TestClient, autoApprove bool) *Component {
	t.Helper()

	mock := newIntegrationMockLLM()
	cfg := DefaultConfig()
	cfg.AutoApprove = autoApprove
	cfg.MaxReviewIterations = 3

	// Build the same prompt assembler the real component uses.
	registry := prompt.NewRegistry()
	registry.RegisterAll(promptdomain.Software()...)
	registry.Register(prompt.ToolGuidanceFragment(prompt.DefaultToolGuidance()))
	assembler := prompt.NewAssembler(registry)

	c := &Component{
		config:        cfg,
		natsClient:    tc.Client,
		logger:        slog.Default(),
		llmClient:     mock,
		modelRegistry: model.Global(),
		assembler:     assembler,
		tripleWriter: &graphutil.TripleWriter{
			NATSClient:    tc.Client,
			Logger:        slog.Default(),
			ComponentName: componentName,
		},
	}
	return c
}

func setupTestPlan(t *testing.T, ctx context.Context, slug string) {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	if _, err := m.CreatePlan(ctx, slug, "Test Plan"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
}

func publishTrigger(t *testing.T, tc *natsclient.TestClient, ctx context.Context, slug string) {
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

func publishLoopCompleted(t *testing.T, tc *natsclient.TestClient, ctx context.Context, taskID, result string) {
	t.Helper()
	event := &agentic.LoopCompletedEvent{
		LoopID:  fmt.Sprintf("loop-%s", taskID),
		TaskID:  taskID,
		Result:  result,
		Outcome: "success",
	}
	// Must wrap in BaseMessage — handleLoopCompleted unmarshals BaseMessage
	// and extracts the typed payload via base.Payload().
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

func publishGeneratorEvent(t *testing.T, tc *natsclient.TestClient, ctx context.Context, subject string, event any) {
	t.Helper()
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal generator event: %v", err)
	}
	js, err := tc.Client.JetStream()
	if err != nil {
		t.Fatalf("get jetstream: %v", err)
	}
	if _, err := js.Publish(ctx, subject, data); err != nil {
		t.Fatalf("publish to %s: %v", subject, err)
	}
}

// waitForJS fetches one message from a JetStream consumer on the given subject.
func waitForJS(t *testing.T, tc *natsclient.TestClient, ctx context.Context, stream, subject string, timeout time.Duration) jetstream.Msg {
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

// extractTaskID parses a BaseMessage-wrapped payload and returns the TaskID field.
func extractTaskID(t *testing.T, data []byte) string {
	t.Helper()
	// BaseMessage wraps the payload — extract it generically.
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

// TestIntegration_Round1_SynthesisDispatchesReviewer verifies that after
// planners complete and synthesis runs, the coordinator dispatches a
// reviewer (round 1) to workflow.async.plan-reviewer.
func TestIntegration_Round1_SynthesisDispatchesReviewer(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithStreams(
			natsclient.TestStreamConfig{Name: "WORKFLOW", Subjects: coordTestStreamSubjects},
			natsclient.TestStreamConfig{Name: "AGENT", Subjects: agentTestStreamSubjects},
			natsclient.TestStreamConfig{Name: "GRAPH", Subjects: graphTestStreamSubjects},
			natsclient.TestStreamConfig{Name: "ENTITY_INGEST", Subjects: []string{"graph.ingest.entity"}},
		),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	slug := "test-round1-reviewer"
	setupTestPlan(t, ctx, slug)

	comp := newTestCoordinator(t, tc, true)
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = comp.Stop(5 * time.Second) })

	// Trigger coordination.
	publishTrigger(t, tc, ctx, slug)

	// Wait for planner dispatch.
	plannerMsg := waitForJS(t, tc, ctx, "WORKFLOW", "workflow.async.planner", 10*time.Second)
	taskID := extractTaskID(t, plannerMsg.Data())
	if taskID == "" {
		t.Fatal("planner task_id is empty")
	}

	// Simulate planner completion.
	publishLoopCompleted(t, tc, ctx, taskID,
		`{"goal":"Add goodbye endpoint","context":"Hello world","scope":{"include":["api/"]}}`)

	// After synthesis, expect reviewer dispatch (round 1).
	reviewerMsg := waitForJS(t, tc, ctx, "WORKFLOW", "workflow.async.plan-reviewer", 10*time.Second)
	if reviewerMsg == nil {
		t.Fatal("No reviewer dispatch after synthesis")
	}
	t.Log("PASS: Round 1 reviewer dispatched after synthesis")
}

// TestIntegration_Round1_ApprovalTriggersRequirementGen verifies that when
// auto_approve=true and the reviewer approves in round 1, requirement
// generation is dispatched.
func TestIntegration_Round1_ApprovalTriggersRequirementGen(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithStreams(
			natsclient.TestStreamConfig{Name: "WORKFLOW", Subjects: coordTestStreamSubjects},
			natsclient.TestStreamConfig{Name: "AGENT", Subjects: agentTestStreamSubjects},
			natsclient.TestStreamConfig{Name: "GRAPH", Subjects: graphTestStreamSubjects},
			natsclient.TestStreamConfig{Name: "ENTITY_INGEST", Subjects: []string{"graph.ingest.entity"}},
		),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	slug := "test-round1-approval"
	setupTestPlan(t, ctx, slug)

	comp := newTestCoordinator(t, tc, true)
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = comp.Stop(5 * time.Second) })

	// Round 1: trigger → planner → synthesis → reviewer.
	publishTrigger(t, tc, ctx, slug)

	plannerMsg := waitForJS(t, tc, ctx, "WORKFLOW", "workflow.async.planner", 10*time.Second)
	plannerTaskID := extractTaskID(t, plannerMsg.Data())
	publishLoopCompleted(t, tc, ctx, plannerTaskID,
		`{"goal":"Test","context":"Test","scope":{}}`)

	reviewerMsg := waitForJS(t, tc, ctx, "WORKFLOW", "workflow.async.plan-reviewer", 10*time.Second)
	reviewerTaskID := extractTaskID(t, reviewerMsg.Data())

	// Reviewer approves → expect requirement-generator dispatch.
	publishLoopCompleted(t, tc, ctx, reviewerTaskID,
		`{"verdict":"approved","summary":"Plan looks good"}`)

	reqGenMsg := waitForJS(t, tc, ctx, "WORKFLOW", "workflow.async.requirement-generator", 10*time.Second)
	if reqGenMsg == nil {
		t.Fatal("No requirement-generator dispatch after round 1 approval")
	}
	t.Log("PASS: Round 1 approval → requirement generation dispatched")
}

// TestIntegration_NeedsChanges_RetriesPlanning verifies that a
// "needs_changes" verdict in round 1 retries planning, not requirement gen.
func TestIntegration_NeedsChanges_RetriesPlanning(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithStreams(
			natsclient.TestStreamConfig{Name: "WORKFLOW", Subjects: coordTestStreamSubjects},
			natsclient.TestStreamConfig{Name: "AGENT", Subjects: agentTestStreamSubjects},
			natsclient.TestStreamConfig{Name: "GRAPH", Subjects: graphTestStreamSubjects},
			natsclient.TestStreamConfig{Name: "ENTITY_INGEST", Subjects: []string{"graph.ingest.entity"}},
		),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	slug := "test-needs-changes"
	setupTestPlan(t, ctx, slug)

	comp := newTestCoordinator(t, tc, true)
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = comp.Stop(5 * time.Second) })

	// Round 1: trigger → planner → synthesis → reviewer.
	publishTrigger(t, tc, ctx, slug)

	plannerMsg := waitForJS(t, tc, ctx, "WORKFLOW", "workflow.async.planner", 10*time.Second)
	plannerTaskID := extractTaskID(t, plannerMsg.Data())
	publishLoopCompleted(t, tc, ctx, plannerTaskID,
		`{"goal":"Test","context":"Test","scope":{}}`)

	reviewerMsg := waitForJS(t, tc, ctx, "WORKFLOW", "workflow.async.plan-reviewer", 10*time.Second)
	reviewerTaskID := extractTaskID(t, reviewerMsg.Data())

	// Reviewer rejects → expect planner retry (not requirement-generator).
	publishLoopCompleted(t, tc, ctx, reviewerTaskID,
		`{"verdict":"needs_changes","summary":"Missing error handling"}`)

	retryMsg := waitForJS(t, tc, ctx, "WORKFLOW", "workflow.async.planner", 10*time.Second)
	if retryMsg == nil {
		t.Fatal("No planner retry after round 1 rejection")
	}
	t.Log("PASS: Round 1 rejection → planner retry (correct)")
}

// TestIntegration_HumanGate_PausesAtAwaitingHuman verifies that with
// auto_approve=false, the coordinator pauses at phaseAwaitingHuman after
// round 1 reviewer approves.
func TestIntegration_HumanGate_PausesAtAwaitingHuman(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithStreams(
			natsclient.TestStreamConfig{Name: "WORKFLOW", Subjects: coordTestStreamSubjects},
			natsclient.TestStreamConfig{Name: "AGENT", Subjects: agentTestStreamSubjects},
			natsclient.TestStreamConfig{Name: "GRAPH", Subjects: graphTestStreamSubjects},
			natsclient.TestStreamConfig{Name: "ENTITY_INGEST", Subjects: []string{"graph.ingest.entity"}},
		),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	slug := "test-human-gate"
	setupTestPlan(t, ctx, slug)

	comp := newTestCoordinator(t, tc, false) // auto_approve=false
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = comp.Stop(5 * time.Second) })

	// Round 1: trigger → planner → synthesis → reviewer.
	publishTrigger(t, tc, ctx, slug)

	plannerMsg := waitForJS(t, tc, ctx, "WORKFLOW", "workflow.async.planner", 10*time.Second)
	plannerTaskID := extractTaskID(t, plannerMsg.Data())
	publishLoopCompleted(t, tc, ctx, plannerTaskID,
		`{"goal":"Test","context":"Test","scope":{}}`)

	reviewerMsg := waitForJS(t, tc, ctx, "WORKFLOW", "workflow.async.plan-reviewer", 10*time.Second)
	reviewerTaskID := extractTaskID(t, reviewerMsg.Data())

	// Reviewer approves but auto_approve=false → should NOT dispatch requirement-generator.
	publishLoopCompleted(t, tc, ctx, reviewerTaskID,
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
