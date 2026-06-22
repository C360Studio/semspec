package executionmanager

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/prompt"
	promptdomain "github.com/c360studio/semspec/prompt/domain"
	"github.com/c360studio/semspec/workflow"
	"github.com/nats-io/nats.go/jetstream"
)

func TestWaitForPlanBucket_RetriesUntilAvailable(t *testing.T) {
	component := &Component{}
	bucket := fakePlanBucket{values: map[string][]byte{"plan-mavlink": []byte(`{"slug":"plan-mavlink"}`)}}
	openCalls := 0
	component.planBucketOpener = func(context.Context) (planKeyValue, error) {
		openCalls++
		if openCalls == 1 {
			return nil, errors.New("nats: bucket not found")
		}
		return bucket, nil
	}

	got, err := component.waitForPlanBucket(context.Background())
	if err != nil {
		t.Fatalf("waitForPlanBucket() error = %v", err)
	}
	if got == nil {
		t.Fatalf("waitForPlanBucket() returned nil bucket")
	}
	if openCalls != 2 {
		t.Fatalf("plan bucket opener calls = %d, want 2", openCalls)
	}
}

func TestBuildAssemblyContext_ReviewerRequiresPlan(t *testing.T) {
	component := newPlanContextTestComponent()
	exec := &taskExecution{
		TaskExecution: &workflow.TaskExecution{
			Slug:         "plan-mavlink",
			Title:        "Review MAVSDK driver implementation",
			WorktreePath: "/work/plan-mavlink",
		},
	}

	_, err := component.buildAssemblyContext(context.Background(), prompt.RoleReviewer, exec, "gemini-pro")
	if err == nil {
		t.Fatalf("buildAssemblyContext() error = nil, want PLAN_STATES load failure")
	}
	if !strings.Contains(err.Error(), "PLAN_STATES") {
		t.Fatalf("buildAssemblyContext() error = %q, want PLAN_STATES context", err)
	}
}

func TestBuildAssemblyContext_ReviewerReceivesPlanConstraints(t *testing.T) {
	const constraint = "Do not hand-roll MAVLink framing, do not stub MAVSDK/OSH classes"

	plan := workflow.Plan{
		Slug:        "plan-mavlink",
		Constraints: []string{constraint},
		Contract: &workflow.ContractPacket{
			ID:          workflow.PlanContractID("contract-mavlink"),
			Constraints: []string{constraint},
		},
	}
	planBytes, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}

	component := newPlanContextTestComponent()
	bucket := fakePlanBucket{values: map[string][]byte{plan.Slug: planBytes}}
	component.cachePlanBucket(bucket)

	exec := &taskExecution{
		TaskExecution: &workflow.TaskExecution{
			Slug:         plan.Slug,
			Title:        "Review MAVSDK driver implementation",
			WorktreePath: "/work/plan-mavlink",
		},
	}
	asmCtx, err := component.buildAssemblyContext(context.Background(), prompt.RoleReviewer, exec, "gemini-pro")
	if err != nil {
		t.Fatalf("buildAssemblyContext() error = %v", err)
	}
	if asmCtx.TaskContext == nil {
		t.Fatalf("TaskContext missing")
	}
	if !containsString(asmCtx.TaskContext.PlanConstraints, constraint) {
		t.Fatalf("reviewer TaskContext missing plan constraint: %#v", asmCtx.TaskContext.PlanConstraints)
	}
	if asmCtx.ContractProjection == nil || !containsString(asmCtx.ContractProjection.Constraints, constraint) {
		t.Fatalf("reviewer ContractProjection missing constraints: %#v", asmCtx.ContractProjection)
	}

	out := component.assembler.Assemble(asmCtx)
	if out.RenderError != nil {
		t.Fatalf("assemble reviewer prompt: %v", out.RenderError)
	}
	combined := out.SystemMessage + "\n" + out.UserMessage
	if !strings.Contains(combined, "AUTHORITATIVE CONTRACT PACKET") {
		t.Fatalf("reviewer prompt missing authoritative contract packet\n--- prompt ---\n%s", combined)
	}
	if !strings.Contains(combined, "do not stub MAVSDK/OSH classes") {
		t.Fatalf("reviewer prompt missing no-stubs constraint\n--- prompt ---\n%s", combined)
	}
}

func newPlanContextTestComponent() *Component {
	registry := prompt.NewRegistry()
	registry.RegisterAll(promptdomain.Software()...)
	return &Component{
		assembler: prompt.NewAssembler(registry),
	}
}

type fakePlanBucket struct {
	values map[string][]byte
}

func (b fakePlanBucket) Get(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
	value, ok := b.values[key]
	if !ok {
		return nil, errors.New("key not found")
	}
	return fakePlanEntry{key: key, value: value}, nil
}

type fakePlanEntry struct {
	key   string
	value []byte
}

func (e fakePlanEntry) Bucket() string {
	return planStatesBucketName
}

func (e fakePlanEntry) Key() string {
	return e.key
}

func (e fakePlanEntry) Value() []byte {
	return append([]byte(nil), e.value...)
}

func (e fakePlanEntry) Revision() uint64 {
	return 1
}

func (e fakePlanEntry) Created() time.Time {
	return time.Time{}
}

func (e fakePlanEntry) Delta() uint64 {
	return 0
}

func (e fakePlanEntry) Operation() jetstream.KeyValueOp {
	return jetstream.KeyValuePut
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
