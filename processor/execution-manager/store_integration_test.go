//go:build integration

package executionmanager

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/natsclient"
)

// TestIntegration_ExecutionStoreCreated verifies that Start() creates the
// EXECUTION_STATES KV bucket and initializes the store.
func TestIntegration_ExecutionStoreCreated(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithStreams(
			natsclient.TestStreamConfig{
				Name:     "WORKFLOW",
				Subjects: []string{"workflow.trigger.task-execution-loop", "workflow.async.>"},
			},
			natsclient.TestStreamConfig{
				Name:     "AGENT",
				Subjects: []string{"agentic.loop_completed.v1", "agent.task.>", "agent.complete.>"},
			},
			natsclient.TestStreamConfig{
				Name:     "GRAPH",
				Subjects: []string{"graph.mutation.triple.add", "graph.ingest.>"},
			},
		),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	comp := newExecIntegrationComponent(t, tc)
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = comp.Stop(5 * time.Second) })

	if comp.store == nil {
		t.Fatal("store is nil after Start()")
	}
	if comp.store.kvStore == nil {
		t.Fatal("store.kvStore is nil — EXECUTION_STATES bucket not created")
	}
}

// TestIntegration_TaskCreateMutation verifies the execution.mutation.task.create
// request/reply path: send a create request, verify success, and check the
// store contains the task execution with correct fields.
func TestIntegration_TaskCreateMutation(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithStreams(
			natsclient.TestStreamConfig{
				Name:     "WORKFLOW",
				Subjects: []string{"workflow.trigger.task-execution-loop", "workflow.async.>"},
			},
			natsclient.TestStreamConfig{
				Name:     "AGENT",
				Subjects: []string{"agentic.loop_completed.v1", "agent.task.>", "agent.complete.>"},
			},
			natsclient.TestStreamConfig{
				Name:     "GRAPH",
				Subjects: []string{"graph.mutation.triple.add", "graph.ingest.>"},
			},
		),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	comp := newExecIntegrationComponent(t, tc)
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = comp.Stop(5 * time.Second) })

	// Send task.create mutation via request/reply.
	createReq := TaskCreateRequest{
		Slug:         "test-plan",
		TaskID:       "task-001",
		Title:        "Build the widget",
		ProjectID:    "proj-1",
		Model:        "default",
		TraceID:      "trace-test-001",
		MaxTDDCycles: 3,
	}
	reqData, _ := json.Marshal(createReq)
	respData, err := tc.Client.RequestWithRetry(ctx, execMutationTaskCreate, reqData,
		5*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		t.Fatalf("task.create request failed: %v", err)
	}

	var resp ExecMutationResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("task.create failed: %s", resp.Error)
	}

	expectedKey := workflow.TaskExecutionKey("test-plan", "task-001")
	if resp.Key != expectedKey {
		t.Errorf("key = %q, want %q", resp.Key, expectedKey)
	}

	// Verify store has the task execution.
	exec, ok := comp.store.getTask(expectedKey)
	if !ok {
		t.Fatalf("store.getTask(%q) = not found", expectedKey)
	}
	if exec.Slug != "test-plan" {
		t.Errorf("Slug = %q, want %q", exec.Slug, "test-plan")
	}
	if exec.TaskID != "task-001" {
		t.Errorf("TaskID = %q, want %q", exec.TaskID, "task-001")
	}
	if exec.Stage != "developing" {
		t.Errorf("Stage = %q, want %q", exec.Stage, "developing")
	}
	if exec.MaxTDDCycles != 3 {
		t.Errorf("MaxTDDCycles = %d, want %d", exec.MaxTDDCycles, 3)
	}
}

// TestIntegration_TaskPhaseMutation verifies the execution.mutation.task.phase
// path: create a task, then transition it to building, verify the store reflects
// the new stage.
func TestIntegration_TaskPhaseMutation(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithStreams(
			natsclient.TestStreamConfig{
				Name:     "WORKFLOW",
				Subjects: []string{"workflow.trigger.task-execution-loop", "workflow.async.>"},
			},
			natsclient.TestStreamConfig{
				Name:     "AGENT",
				Subjects: []string{"agentic.loop_completed.v1", "agent.task.>", "agent.complete.>"},
			},
			natsclient.TestStreamConfig{
				Name:     "GRAPH",
				Subjects: []string{"graph.mutation.triple.add", "graph.ingest.>"},
			},
		),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	comp := newExecIntegrationComponent(t, tc)
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = comp.Stop(5 * time.Second) })

	// Create task.
	createReq, _ := json.Marshal(TaskCreateRequest{
		Slug: "plan-a", TaskID: "task-x", Title: "Test", Model: "default",
	})
	respData, err := tc.Client.RequestWithRetry(ctx, execMutationTaskCreate, createReq,
		5*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		t.Fatalf("task.create: %v", err)
	}
	var createResp ExecMutationResponse
	json.Unmarshal(respData, &createResp)
	if !createResp.Success {
		t.Fatalf("task.create failed: %s", createResp.Error)
	}

	key := createResp.Key

	// Transition to building.
	phaseReq, _ := json.Marshal(TaskPhaseRequest{
		Key: key, Stage: "building",
	})
	respData, err = tc.Client.RequestWithRetry(ctx, execMutationTaskPhase, phaseReq,
		5*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		t.Fatalf("task.phase: %v", err)
	}
	var phaseResp ExecMutationResponse
	json.Unmarshal(respData, &phaseResp)
	if !phaseResp.Success {
		t.Fatalf("task.phase failed: %s", phaseResp.Error)
	}

	// Verify store.
	exec, ok := comp.store.getTask(key)
	if !ok {
		t.Fatalf("store.getTask(%q) not found", key)
	}
	if exec.Stage != "building" {
		t.Errorf("Stage = %q, want %q", exec.Stage, "building")
	}
}

// TestIntegration_ReqCreateMutation verifies the execution.mutation.req.create
// path for requirement executions.
func TestIntegration_ReqCreateMutation(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithStreams(
			natsclient.TestStreamConfig{
				Name:     "WORKFLOW",
				Subjects: []string{"workflow.trigger.task-execution-loop", "workflow.async.>"},
			},
			natsclient.TestStreamConfig{
				Name:     "AGENT",
				Subjects: []string{"agentic.loop_completed.v1", "agent.task.>", "agent.complete.>"},
			},
			natsclient.TestStreamConfig{
				Name:     "GRAPH",
				Subjects: []string{"graph.mutation.triple.add", "graph.ingest.>"},
			},
		),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	comp := newExecIntegrationComponent(t, tc)
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = comp.Stop(5 * time.Second) })

	createReq, _ := json.Marshal(ReqCreateRequest{
		Slug:          "plan-b",
		RequirementID: "req-001",
		Title:         "Auth flow",
		ProjectID:     "proj-1",
		TraceID:       "trace-req-001",
	})
	respData, err := tc.Client.RequestWithRetry(ctx, execMutationReqCreate, createReq,
		5*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		t.Fatalf("req.create: %v", err)
	}

	var resp ExecMutationResponse
	json.Unmarshal(respData, &resp)
	if !resp.Success {
		t.Fatalf("req.create failed: %s", resp.Error)
	}

	expectedKey := workflow.RequirementExecutionKey("plan-b", "req-001")
	if resp.Key != expectedKey {
		t.Errorf("key = %q, want %q", resp.Key, expectedKey)
	}

	exec, ok := comp.store.getReq(expectedKey)
	if !ok {
		t.Fatalf("store.getReq(%q) not found", expectedKey)
	}
	if exec.Stage != "decomposing" {
		t.Errorf("Stage = %q, want %q", exec.Stage, "decomposing")
	}
	if exec.RequirementID != "req-001" {
		t.Errorf("RequirementID = %q, want %q", exec.RequirementID, "req-001")
	}
}

// TestIntegration_TaskCompleteTerminal verifies the execution.mutation.task.complete
// path: create a task, complete it with "approved", verify it's terminal.
func TestIntegration_TaskCompleteTerminal(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithStreams(
			natsclient.TestStreamConfig{
				Name:     "WORKFLOW",
				Subjects: []string{"workflow.trigger.task-execution-loop", "workflow.async.>"},
			},
			natsclient.TestStreamConfig{
				Name:     "AGENT",
				Subjects: []string{"agentic.loop_completed.v1", "agent.task.>", "agent.complete.>"},
			},
			natsclient.TestStreamConfig{
				Name:     "GRAPH",
				Subjects: []string{"graph.mutation.triple.add", "graph.ingest.>"},
			},
		),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	comp := newExecIntegrationComponent(t, tc)
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = comp.Stop(5 * time.Second) })

	// Create.
	createReq, _ := json.Marshal(TaskCreateRequest{
		Slug: "plan-c", TaskID: "task-final", Title: "Final", Model: "default",
	})
	respData, err := tc.Client.RequestWithRetry(ctx, execMutationTaskCreate, createReq,
		5*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		t.Fatalf("task.create: %v", err)
	}
	var createResp ExecMutationResponse
	json.Unmarshal(respData, &createResp)
	if !createResp.Success {
		t.Fatalf("task.create failed: %s", createResp.Error)
	}
	key := createResp.Key

	// Complete.
	completeReq, _ := json.Marshal(TaskCompleteRequest{
		Key: key, Stage: "approved", Verdict: "approved",
	})
	respData, err = tc.Client.RequestWithRetry(ctx, execMutationTaskComplete, completeReq,
		5*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		t.Fatalf("task.complete: %v", err)
	}
	var completeResp ExecMutationResponse
	json.Unmarshal(respData, &completeResp)
	if !completeResp.Success {
		t.Fatalf("task.complete failed: %s", completeResp.Error)
	}

	exec, ok := comp.store.getTask(key)
	if !ok {
		t.Fatalf("store.getTask(%q) not found after complete", key)
	}
	if exec.Stage != "approved" {
		t.Errorf("Stage = %q, want %q", exec.Stage, "approved")
	}
	if exec.Verdict != "approved" {
		t.Errorf("Verdict = %q, want %q", exec.Verdict, "approved")
	}
}
