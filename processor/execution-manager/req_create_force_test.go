package executionmanager

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestHandleReqCreateMutation_ExistingWithoutForceRejected(t *testing.T) {
	c := newTestComponent(t)
	key := workflow.RequirementExecutionKey("plan", "req-1")
	c.store.reqCache.Set(key, &workflow.RequirementExecution{
		Slug:          "plan",
		RequirementID: "req-1",
		Stage:         "active",
		Title:         "old",
	}) //nolint:errcheck

	data, err := json.Marshal(ReqCreateRequest{
		Slug:          "plan",
		RequirementID: "req-1",
		Title:         "new",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	resp := c.handleReqCreateMutation(context.Background(), data)
	if resp.Success {
		t.Fatalf("handleReqCreateMutation succeeded, want duplicate rejection")
	}
}

func TestHandleReqCreateMutation_ForceReplacesExistingReq(t *testing.T) {
	c := newTestComponent(t)
	key := workflow.RequirementExecutionKey("plan", "req-1")
	c.store.reqCache.Set(key, &workflow.RequirementExecution{
		Slug:          "plan",
		RequirementID: "req-1",
		Stage:         "active",
		Title:         "old",
		TraceID:       "trace-old",
	}) //nolint:errcheck

	data, err := json.Marshal(ReqCreateRequest{
		Slug:          "plan",
		RequirementID: "req-1",
		Title:         "new",
		TraceID:       "trace-new",
		Force:         true,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	resp := c.handleReqCreateMutation(context.Background(), data)
	if !resp.Success {
		t.Fatalf("handleReqCreateMutation failed: %s", resp.Error)
	}

	got, ok := c.store.getReq(key)
	if !ok {
		t.Fatalf("getReq(%q) missing after force create", key)
	}
	if got.Stage != "pending" {
		t.Fatalf("Stage = %q, want pending", got.Stage)
	}
	if got.Title != "new" {
		t.Fatalf("Title = %q, want new", got.Title)
	}
	if got.TraceID != "trace-new" {
		t.Fatalf("TraceID = %q, want trace-new", got.TraceID)
	}
}
