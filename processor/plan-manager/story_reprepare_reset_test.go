package planmanager

import (
	"context"
	"errors"
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/nats-io/nats.go/jetstream"
)

type resetKVStub struct {
	keys []string
}

func (s resetKVStub) Get(context.Context, string) (jetstream.KeyValueEntry, error) {
	return nil, errors.New("not implemented")
}

func (s resetKVStub) GetRevision(context.Context, string, uint64) (jetstream.KeyValueEntry, error) {
	return nil, errors.New("not implemented")
}

func (s resetKVStub) Put(context.Context, string, []byte) (uint64, error) {
	return 0, errors.New("not implemented")
}

func (s resetKVStub) PutString(context.Context, string, string) (uint64, error) {
	return 0, errors.New("not implemented")
}

func (s resetKVStub) Create(context.Context, string, []byte, ...jetstream.KVCreateOpt) (uint64, error) {
	return 0, errors.New("not implemented")
}

func (s resetKVStub) Update(context.Context, string, []byte, uint64) (uint64, error) {
	return 0, errors.New("not implemented")
}

func (s resetKVStub) Delete(context.Context, string, ...jetstream.KVDeleteOpt) error {
	return errors.New("not implemented")
}

func (s resetKVStub) Purge(context.Context, string, ...jetstream.KVDeleteOpt) error {
	return errors.New("not implemented")
}

func (s resetKVStub) Watch(context.Context, string, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	return nil, errors.New("not implemented")
}

func (s resetKVStub) WatchAll(context.Context, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	return nil, errors.New("not implemented")
}

func (s resetKVStub) WatchFiltered(context.Context, []string, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	return nil, errors.New("not implemented")
}

func (s resetKVStub) Keys(context.Context, ...jetstream.WatchOpt) ([]string, error) {
	return append([]string(nil), s.keys...), nil
}

func (s resetKVStub) ListKeys(context.Context, ...jetstream.WatchOpt) (jetstream.KeyLister, error) {
	return nil, errors.New("not implemented")
}

func (s resetKVStub) ListKeysFiltered(context.Context, ...string) (jetstream.KeyLister, error) {
	return nil, errors.New("not implemented")
}

func (s resetKVStub) History(context.Context, string, ...jetstream.WatchOpt) ([]jetstream.KeyValueEntry, error) {
	return nil, errors.New("not implemented")
}

func (s resetKVStub) Bucket() string { return "EXECUTION_STATES" }

func (s resetKVStub) PurgeDeletes(context.Context, ...jetstream.KVPurgeOpt) error {
	return errors.New("not implemented")
}

func (s resetKVStub) Status(context.Context) (jetstream.KeyValueStatus, error) {
	return nil, errors.New("not implemented")
}

func TestApplyStoryReprepare_ResetFailureLeavesPlanUntouched(t *testing.T) {
	c := setupTestComponent(t)
	c.execBucket = resetKVStub{keys: []string{"req.demo.1"}}
	c.reqResetSender = func(context.Context, string) error {
		return errors.New("reset unavailable")
	}

	plan := &workflow.Plan{
		Slug:   "demo",
		Status: workflow.StatusImplementing,
		Scenarios: []workflow.Scenario{
			{ID: "scen.target", RequirementID: "req.demo.1", StoryID: "story.demo.1"},
		},
	}
	proposal := &workflow.PlanDecision{
		ID:               "plan-decision.demo.recovery.story",
		Kind:             workflow.PlanDecisionKindStoryReprepare,
		AffectedReqIDs:   []string{"req.demo.1"},
		AffectedStoryIDs: []string{"story.demo.1"},
	}

	if err := c.applyStoryReprepare(context.Background(), plan, proposal, plan.Slug); err == nil {
		t.Fatal("applyStoryReprepare returned nil; reset failure must abort the accept")
	}
	if plan.Status != workflow.StatusImplementing {
		t.Fatalf("plan.Status = %s, want implementing after reset failure", plan.Status)
	}
	if len(plan.Scenarios) != 1 || plan.Scenarios[0].ID != "scen.target" {
		t.Fatalf("plan.Scenarios = %+v, want original scenario retained after reset failure", plan.Scenarios)
	}
}
