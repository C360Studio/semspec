//go:build integration

package planmanager

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// cascadeStreamSubjects covers the subjects the plan-api cascade handlers
// need for integration tests.
var cascadeStreamSubjects = []string{
	"workflow.events.>",
	"workflow.async.>",
}

func setupCascadeComponent(t *testing.T, tc *natsclient.TestClient) *Component {
	t.Helper()
	return &Component{
		natsClient: tc.Client,
		logger:     slog.Default(),
	}
}

func setupPlanWithRequirements(t *testing.T, ctx context.Context, slug string, reqCount int) string {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	plan, err := workflow.CreatePlan(ctx, nil, slug, "Test Plan")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	// Advance plan to approved + requirements_generated.
	if err := workflow.ApprovePlan(ctx, nil, plan); err != nil {
		t.Fatalf("ApprovePlan: %v", err)
	}

	// Create requirements on disk.
	reqs := make([]workflow.Requirement, reqCount)
	for i := range reqs {
		reqs[i] = workflow.Requirement{
			ID:     fmt.Sprintf("requirement.%s.%d", slug, i+1),
			Title:  fmt.Sprintf("Requirement %d", i+1),
			Status: "active",
		}
	}
	if err := workflow.SaveRequirements(ctx, nil, reqs, slug); err != nil {
		t.Fatalf("SaveRequirements: %v", err)
	}

	if err := workflow.SetPlanStatus(ctx, nil, plan, workflow.StatusRequirementsGenerated); err != nil {
		t.Fatalf("SetPlanStatus: %v", err)
	}

	return tmpDir
}

func waitForJSMessage(t *testing.T, tc *natsclient.TestClient, ctx context.Context, stream, subject string, timeout time.Duration) jetstream.Msg {
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

func countJSMessages(t *testing.T, tc *natsclient.TestClient, ctx context.Context, stream, subject string, timeout time.Duration) int {
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
		Name:          fmt.Sprintf("test-count-%d", time.Now().UnixNano()),
		FilterSubject: subject,
		AckPolicy:     jetstream.AckNonePolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	if err != nil {
		t.Fatalf("create consumer for %s: %v", subject, err)
	}
	msgs, err := cons.Fetch(10, jetstream.FetchMaxWait(timeout))
	if err != nil {
		return 0
	}
	count := 0
	for range msgs.Messages() {
		count++
	}
	return count
}

// TestIntegration_RequirementsGenerated_DispatchesScenarioGenerators verifies
// that when a RequirementsGeneratedEvent is handled by plan-api, it dispatches
// one ScenarioGeneratorRequest per requirement. This is the manual approval
// path where the plan-coordinator has terminated.
func TestIntegration_RequirementsGenerated_DispatchesScenarioGenerators(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithStreams(
			natsclient.TestStreamConfig{Name: "WORKFLOW", Subjects: cascadeStreamSubjects},
		),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	slug := "test-cascade-req-gen"
	setupPlanWithRequirements(t, ctx, slug, 3)

	comp := setupCascadeComponent(t, tc)

	// Simulate RequirementsGeneratedEvent.
	event := &workflow.RequirementsGeneratedEvent{
		Slug:    slug,
		TraceID: "test-trace-1",
	}
	comp.handleRequirementsGeneratedEvent(ctx, event)

	// Verify 3 scenario-generator requests were dispatched.
	count := countJSMessages(t, tc, ctx, "WORKFLOW", "workflow.async.scenario-generator", 5*time.Second)
	if count != 3 {
		t.Fatalf("Expected 3 scenario-generator dispatches, got %d", count)
	}

	t.Log("PASS: RequirementsGeneratedEvent → 3 scenario-generator requests dispatched")
}

// TestIntegration_ScenariosGenerated_UpdatesStatus verifies that when a
// ScenariosGeneratedEvent is handled, the plan status transitions to
// scenarios_generated.
func TestIntegration_ScenariosGenerated_UpdatesStatus(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithStreams(
			natsclient.TestStreamConfig{Name: "WORKFLOW", Subjects: cascadeStreamSubjects},
		),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	slug := "test-cascade-scen-gen"
	setupPlanWithRequirements(t, ctx, slug, 3)

	comp := setupCascadeComponent(t, tc)

	// Simulate ScenariosGeneratedEvent.
	event := &workflow.ScenariosGeneratedEvent{
		Slug:          slug,
		ScenarioCount: 6,
		TraceID:       "test-trace-1",
	}
	comp.handleScenariosGeneratedEvent(ctx, event)

	// Verify plan status is now scenarios_generated.
	plan, err := workflow.LoadPlan(ctx, nil, slug)
	if err != nil {
		t.Fatalf("LoadPlan: %v", err)
	}

	if plan.Status != workflow.StatusScenariosGenerated {
		t.Fatalf("Plan status = %q, want %q", plan.Status, workflow.StatusScenariosGenerated)
	}

	t.Log("PASS: ScenariosGeneratedEvent → plan status = scenarios_generated")
}

// TestIntegration_PromoteRound2_SetsReadyForExecution verifies that when a
// human promotes a plan that already has requirements (round 2), the plan
// transitions to ready_for_execution.
func TestIntegration_PromoteRound2_SetsReadyForExecution(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithStreams(
			natsclient.TestStreamConfig{Name: "WORKFLOW", Subjects: cascadeStreamSubjects},
		),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	slug := "test-promote-round2"
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	plan, err := workflow.CreatePlan(ctx, nil, slug, "Test Plan")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	// Set up plan at scenarios_generated state with requirements.
	if err := workflow.ApprovePlan(ctx, nil, plan); err != nil {
		t.Fatalf("ApprovePlan: %v", err)
	}
	reqs := []workflow.Requirement{
		{ID: "requirement." + slug + ".1", Title: "Req 1", Status: "active"},
	}
	if err := workflow.SaveRequirements(ctx, nil, reqs, slug); err != nil {
		t.Fatalf("SaveRequirements: %v", err)
	}
	scenarios := []workflow.Scenario{
		{ID: "scenario." + slug + ".1.1", Given: "g", When: "w", Then: []string{"t"}},
	}
	if err := workflow.SaveScenarios(ctx, nil, scenarios, slug); err != nil {
		t.Fatalf("SaveScenarios: %v", err)
	}
	// Need to advance status through the chain
	if err := workflow.SetPlanStatus(ctx, nil, plan, workflow.StatusRequirementsGenerated); err != nil {
		t.Fatalf("SetPlanStatus to requirements_generated: %v", err)
	}
	if err := workflow.SetPlanStatus(ctx, nil, plan, workflow.StatusScenariosGenerated); err != nil {
		t.Fatalf("SetPlanStatus to scenarios_generated: %v", err)
	}
	// Reset approved flag to simulate round 2 awaiting human
	plan.Approved = false
	if err := workflow.SavePlan(ctx, nil, plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	comp := setupCascadeComponent(t, tc)

	// Simulate human promote (round 2).
	req := newPromoteRequest(t, slug)
	w := newRecorder()
	comp.handlePromotePlan(w, req, slug)

	if w.Code != 200 {
		t.Fatalf("Promote returned %d: %s", w.Code, w.Body.String())
	}

	// Reload plan and check status.
	plan, err = workflow.LoadPlan(ctx, nil, slug)
	if err != nil {
		t.Fatalf("LoadPlan after promote: %v", err)
	}

	if plan.Status != workflow.StatusReadyForExecution {
		t.Fatalf("Plan status = %q, want %q", plan.Status, workflow.StatusReadyForExecution)
	}

	t.Log("PASS: Round 2 promote → plan status = ready_for_execution")
}

// helpers for HTTP test requests
func newPromoteRequest(t *testing.T, slug string) *http.Request {
	t.Helper()
	return httptest.NewRequest("POST", "/plan-api/plans/"+slug+"/promote", nil)
}

func newRecorder() *httptest.ResponseRecorder {
	return httptest.NewRecorder()
}
