//go:build integration

package scenarioorchestrator

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semspec/test/integration/graphmock"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
)

// workflowStreamSubjects are the subjects covered by the WORKFLOW stream used
// in integration tests. They must include both the inbound trigger subject and
// the outbound execution-loop subject so that the component can consume and
// publish within the same stream.
var workflowStreamSubjects = []string{
	"scenario.orchestrate.*",
	"workflow.trigger.requirement-execution-loop",
}

// makeReqForPlan builds a Requirement with the correct PlanEntityID for a given slug.
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

	// Mock graph-ingest so startup reconciliation does not block on unanswered requests.
	graphmock.Start(t, tc.Client)

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
