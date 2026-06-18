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

func TestReqResetThenSequentialRedispatchAllowsDependentWithoutForce(t *testing.T) {
	c := newTestComponent(t)
	ctx := context.Background()
	req1Key := workflow.RequirementExecutionKey("plan", "req-1")
	req2Key := workflow.RequirementExecutionKey("plan", "req-2")
	for key, id := range map[string]string{req1Key: "req-1", req2Key: "req-2"} {
		c.store.reqCache.Set(key, &workflow.RequirementExecution{
			Slug:          "plan",
			RequirementID: id,
			Stage:         "active",
			Title:         "old " + id,
		}) //nolint:errcheck
	}

	for _, key := range []string{req1Key, req2Key} {
		data, err := json.Marshal(ReqResetRequest{Key: key})
		if err != nil {
			t.Fatalf("marshal reset: %v", err)
		}
		if resp := c.handleReqResetMutation(ctx, data); !resp.Success {
			t.Fatalf("reset %s failed: %s", key, resp.Error)
		}
	}

	req1Create := marshalReqCreate(t, ReqCreateRequest{
		Slug:          "plan",
		RequirementID: "req-1",
		Title:         "new req-1",
		Force:         true,
	})
	if resp := c.handleReqCreateMutation(ctx, req1Create); !resp.Success {
		t.Fatalf("force create req-1 failed: %s", resp.Error)
	}

	req2Create := marshalReqCreate(t, ReqCreateRequest{
		Slug:          "plan",
		RequirementID: "req-2",
		Title:         "new req-2",
	})
	if resp := c.handleReqCreateMutation(ctx, req2Create); !resp.Success {
		t.Fatalf("normal dependent create req-2 failed after upfront reset: %s", resp.Error)
	}

	got, ok := c.store.getReq(req2Key)
	if !ok {
		t.Fatalf("getReq(%q) missing after dependent create", req2Key)
	}
	if got.Stage != "pending" || got.Title != "new req-2" {
		t.Fatalf("dependent req after create = stage %q title %q, want pending/new req-2", got.Stage, got.Title)
	}
}

func marshalReqCreate(t *testing.T, req ReqCreateRequest) []byte {
	t.Helper()
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal req.create: %v", err)
	}
	return data
}
