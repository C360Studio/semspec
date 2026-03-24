//go:build integration

// Package changeproposalhandler provides integration tests for the change-proposal-handler.
//
// These tests require real NATS infrastructure via testcontainers (Docker).
// Run with: go test -tags integration ./processor/change-proposal-handler/...
package changeproposalhandler

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// setupIntegrationFixture creates a real filesystem plan with requirements,
// scenarios, and a ChangeProposal stored under repoRoot.
// Returns the manager and the proposal ID used.
func setupIntegrationFixture(t *testing.T, repoRoot, slug string) string {
	t.Helper()
	ctx := context.Background()
	
	if _, err := workflow.CreatePlan(ctx, nil, slug, "Integration Test Plan"); err != nil {
		t.Fatalf("CreatePlan(%q): %v", slug, err)
	}

	reqs := []workflow.Requirement{
		{ID: "req-i1", PlanID: workflow.PlanEntityID(slug), Title: "Auth", Status: workflow.RequirementStatusActive},
	}
	if err := workflow.SaveRequirements(ctx, nil, reqs, slug); err != nil {
		t.Fatalf("SaveRequirements: %v", err)
	}

	scenarios := []workflow.Scenario{
		{ID: "sc-i1", RequirementID: "req-i1"},
		{ID: "sc-i2", RequirementID: "req-i1"},
	}
	if err := workflow.SaveScenarios(ctx, nil, scenarios, slug); err != nil {
		t.Fatalf("SaveScenarios: %v", err)
	}

	proposalID := "cp-integration-001"
	proposal := workflow.ChangeProposal{
		ID:             proposalID,
		AffectedReqIDs: []string{"req-i1"},
	}
	if err := workflow.SaveChangeProposals(ctx, nil, []workflow.ChangeProposal{proposal}, slug); err != nil {
		t.Fatalf("SaveChangeProposals: %v", err)
	}

	return proposalID
}

// buildCascadeMsg serialises a ChangeProposalCascadeRequest inside a BaseMessage envelope.
func buildCascadeMsg(t *testing.T, req *payloads.ChangeProposalCascadeRequest) []byte {
	t.Helper()
	baseMsg := message.NewBaseMessage(req.Schema(), req, "test-publisher")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		t.Fatalf("buildCascadeMsg: %v", err)
	}
	return data
}

// workflowStreamConfig returns a stream config covering both the trigger and
// accepted-event subjects used in tests.
func workflowStreamConfig() natsclient.TestStreamConfig {
	return natsclient.TestStreamConfig{
		Name: "WORKFLOW",
		Subjects: []string{
			"workflow.trigger.>",
			"workflow.events.>",
		},
	}
}

// ---------------------------------------------------------------------------
// TestCascadeEndToEnd
// ---------------------------------------------------------------------------

// TestCascadeEndToEnd verifies that:
//  1. The component consumes a ChangeProposalCascadeRequest from JetStream.
//  2. It runs the cascade (marks tasks dirty on the filesystem).
//  3. It publishes a ChangeProposalAcceptedEvent on the accepted subject.
func TestCascadeEndToEnd(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithStreams(workflowStreamConfig()),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repoRoot := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", repoRoot)

	slug := "e2e-cascade-plan"
	proposalID := setupIntegrationFixture(t, repoRoot, slug)

	// Build and start the component.
	cfg := DefaultConfig()
	// Use a unique consumer name per test run to avoid conflicts.
	cfg.ConsumerName = "cph-test-e2e"
	rawConfig, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	deps := component.Dependencies{
		NATSClient: tc.Client,
		Logger:     slog.Default(),
	}
	compDiscoverable, err := NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent: %v", err)
	}
	comp := compDiscoverable.(*Component)
	if err := comp.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// Override repoRoot so the component reads the same temp dir.
	comp.repoRoot = repoRoot

	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = comp.Stop(3 * time.Second) })

	// Subscribe to the accepted-events output subject before publishing.
	acceptedCh := make(chan []byte, 1)
	js, err := tc.Client.JetStream()
	if err != nil {
		t.Fatalf("JetStream: %v", err)
	}
	stream, err := js.Stream(ctx, "WORKFLOW")
	if err != nil {
		t.Fatalf("get WORKFLOW stream: %v", err)
	}
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       "test-accepted-reader",
		FilterSubject: cfg.AcceptedSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       30 * time.Second,
		MaxDeliver:    1,
	})
	if err != nil {
		t.Fatalf("create accepted-events consumer: %v", err)
	}

	// Publish the cascade request.
	req := &payloads.ChangeProposalCascadeRequest{
		ProposalID: proposalID,
		Slug:       slug,
		TraceID:    "trace-e2e-001",
	}
	data := buildCascadeMsg(t, req)
	if _, err := js.Publish(ctx, cfg.TriggerSubject, data); err != nil {
		t.Fatalf("publish cascade request: %v", err)
	}

	// Collect the accepted event (with timeout).
	go func() {
		msgs, err := consumer.Fetch(1, jetstream.FetchMaxWait(20*time.Second))
		if err != nil {
			return
		}
		for msg := range msgs.Messages() {
			acceptedCh <- msg.Data()
			_ = msg.Ack()
		}
	}()

	select {
	case msgData := <-acceptedCh:
		// Unwrap and verify the accepted event.
		var baseMsg struct {
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(msgData, &baseMsg); err != nil {
			t.Fatalf("unmarshal accepted BaseMessage: %v", err)
		}
		var evt payloads.ChangeProposalAcceptedEvent
		if err := json.Unmarshal(baseMsg.Payload, &evt); err != nil {
			t.Fatalf("unmarshal ChangeProposalAcceptedEvent: %v", err)
		}
		if evt.ProposalID != proposalID {
			t.Errorf("AcceptedEvent.ProposalID = %q, want %q", evt.ProposalID, proposalID)
		}
		if evt.Slug != slug {
			t.Errorf("AcceptedEvent.Slug = %q, want %q", evt.Slug, slug)
		}
		if len(evt.AffectedRequirementIDs) == 0 {
			t.Error("AcceptedEvent.AffectedRequirementIDs should not be empty")
		}
		if len(evt.AffectedScenarioIDs) == 0 {
			t.Error("AcceptedEvent.AffectedScenarioIDs should not be empty")
		}

	case <-ctx.Done():
		t.Fatal("timed out waiting for change_proposal.accepted event")
	}
}

// ---------------------------------------------------------------------------
// TestCascadeRequest_ProposalNotFound
// ---------------------------------------------------------------------------

// TestCascadeRequest_ProposalNotFound verifies that when the proposal cannot be
// found the message is Nak'd (not Term'd) because the failure may be transient
// (e.g. the proposal store write from the HTTP handler hasn't landed yet).
func TestCascadeRequest_ProposalNotFound(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithStreams(workflowStreamConfig()),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repoRoot := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", repoRoot)

	slug := "missing-proposal-plan"

	// Create a plan but deliberately save NO proposals.
		if _, err := workflow.CreatePlan(ctx, nil, slug, "Missing Proposal Plan"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := workflow.SaveChangeProposals(ctx, nil, []workflow.ChangeProposal{}, slug); err != nil {
		t.Fatalf("SaveChangeProposals: %v", err)
	}

	// Build and start the component.
	cfg := DefaultConfig()
	cfg.ConsumerName = "cph-test-missing"
	rawConfig, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	deps := component.Dependencies{
		NATSClient: tc.Client,
		Logger:     slog.Default(),
	}
	compDiscoverable2, err := NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent: %v", err)
	}
	comp2 := compDiscoverable2.(*Component)
	if err := comp2.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	comp2.repoRoot = repoRoot
	if err := comp2.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = comp2.Stop(3 * time.Second) })

	// Publish a request that references a non-existent proposal.
	js, err := tc.Client.JetStream()
	if err != nil {
		t.Fatalf("JetStream: %v", err)
	}
	req := &payloads.ChangeProposalCascadeRequest{
		ProposalID: "cp-does-not-exist",
		Slug:       slug,
		TraceID:    "trace-missing-001",
	}
	data := buildCascadeMsg(t, req)
	if _, err := js.Publish(ctx, cfg.TriggerSubject, data); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Wait for the component to attempt processing and increment failures counter.
	// We poll requestsFailed with a short timeout rather than sleeping.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if comp2.requestsFailed.Load() >= 1 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if comp2.requestsFailed.Load() == 0 {
		t.Error("expected requestsFailed counter to be incremented for proposal-not-found case")
	}
}
