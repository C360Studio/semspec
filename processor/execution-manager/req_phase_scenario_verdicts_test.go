package executionmanager

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestHandleReqPhaseMutation_PersistsScenarioVerdicts(t *testing.T) {
	c := newTestComponent(t)
	ctx := context.Background()
	key := "req.demo.req.demo.1"

	exec := &workflow.RequirementExecution{
		EntityID:      "entity-1",
		Slug:          "demo",
		RequirementID: "req.demo.1",
		Stage:         "reviewing",
	}
	if err := c.store.saveReq(ctx, key, exec); err != nil {
		t.Fatalf("seed saveReq: %v", err)
	}

	reqBytes, _ := json.Marshal(ReqPhaseRequest{
		Key:   key,
		Stage: "completed",
		ScenarioVerdicts: []workflow.ScenarioVerdict{
			{ScenarioID: "scen.telemetry.1", Passed: true},
		},
	})
	resp := c.handleReqPhaseMutation(ctx, reqBytes)
	if !resp.Success {
		t.Fatalf("expected success, got %+v", resp)
	}

	after, ok := c.store.getReq(key)
	if !ok {
		t.Fatal("entry disappeared after req.phase")
	}
	if len(after.ScenarioVerdicts) != 1 {
		t.Fatalf("ScenarioVerdicts = %v, want one persisted verdict", after.ScenarioVerdicts)
	}
	if got := after.ScenarioVerdicts[0].ScenarioID; got != "scen.telemetry.1" {
		t.Fatalf("ScenarioVerdicts[0].ScenarioID = %q, want scen.telemetry.1", got)
	}
	if !after.ScenarioVerdicts[0].Passed {
		t.Fatal("ScenarioVerdicts[0].Passed = false, want true")
	}
}
