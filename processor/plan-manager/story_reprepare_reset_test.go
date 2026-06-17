package planmanager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/nats-io/nats.go/jetstream"
)

type resetKVStub struct {
	keys   []string
	values map[string][]byte
}

func (s resetKVStub) Get(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
	return s.get(key)
}

func (s resetKVStub) get(key string) (jetstream.KeyValueEntry, error) {
	if s.values == nil {
		return nil, errors.New("key not found")
	}
	value, ok := s.values[key]
	if !ok {
		return nil, errors.New("key not found")
	}
	return resetKVEntry{key: key, value: append([]byte(nil), value...)}, nil
}

type resetKVEntry struct {
	key   string
	value []byte
}

func (e resetKVEntry) Bucket() string                  { return "EXECUTION_STATES" }
func (e resetKVEntry) Key() string                     { return e.key }
func (e resetKVEntry) Value() []byte                   { return append([]byte(nil), e.value...) }
func (e resetKVEntry) Revision() uint64                { return 1 }
func (e resetKVEntry) Created() time.Time              { return time.Time{} }
func (e resetKVEntry) Delta() uint64                   { return 0 }
func (e resetKVEntry) Operation() jetstream.KeyValueOp { return jetstream.KeyValuePut }

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
			{ID: "scen.target", RequirementID: "1", StoryID: "story.demo.1"},
		},
	}
	proposal := &workflow.PlanDecision{
		ID:               "plan-decision.demo.recovery.story",
		Kind:             workflow.PlanDecisionKindStoryReprepare,
		AffectedReqIDs:   []string{"1"},
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

func TestApplyStoryReprepare_UsesDependentClosureForResetAndScenarioRemoval(t *testing.T) {
	c := setupTestComponent(t)
	c.execBucket = resetKVStub{
		keys: []string{
			"req.demo.contract",
			"task.demo.node-contract",
			"req.demo.consumer",
			"task.demo.node-consumer",
			"req.demo.unrelated",
			"task.demo.node-unrelated",
		},
		values: map[string][]byte{
			"task.demo.node-contract":  []byte(`{"requirement_id":"contract"}`),
			"task.demo.node-consumer":  []byte(`{"requirement_id":"consumer"}`),
			"task.demo.node-unrelated": []byte(`{"requirement_id":"unrelated"}`),
		},
	}
	var reset []string
	c.reqResetSender = func(_ context.Context, key string) error {
		reset = append(reset, key)
		return nil
	}

	plan := &workflow.Plan{
		Slug:   "demo",
		Status: workflow.StatusImplementing,
		Requirements: []workflow.Requirement{
			{ID: "contract"},
			{ID: "consumer", DependsOn: []string{"contract"}},
			{ID: "unrelated"},
		},
		Scenarios: []workflow.Scenario{
			{ID: "scen.contract", RequirementID: "contract", StoryID: "story.contract"},
			{ID: "scen.consumer", RequirementID: "consumer", StoryID: "story.consumer"},
			{ID: "scen.unrelated", RequirementID: "unrelated", StoryID: "story.unrelated"},
		},
	}
	proposal := &workflow.PlanDecision{
		ID:             "plan-decision.demo.recovery.story",
		Kind:           workflow.PlanDecisionKindStoryReprepare,
		AffectedReqIDs: []string{"contract"},
		Rationale:      "Repair the contract Story and dependent consumer scenario.",
	}

	if err := c.applyStoryReprepare(context.Background(), plan, proposal, plan.Slug); err != nil {
		t.Fatalf("applyStoryReprepare: %v", err)
	}

	assertResetKeys(t, reset, []string{
		"req.demo.contract",
		"task.demo.node-contract",
		"req.demo.consumer",
		"task.demo.node-consumer",
	})
	assertNoResetKeys(t, reset, []string{
		"req.demo.unrelated",
		"task.demo.node-unrelated",
	})
	if plan.Status != workflow.StatusPreparingStories {
		t.Fatalf("Status = %s, want preparing_stories", plan.Status)
	}
	if len(plan.Scenarios) != 1 || plan.Scenarios[0].ID != "scen.unrelated" {
		t.Fatalf("Scenarios = %+v, want only unrelated scenario preserved", plan.Scenarios)
	}
}
