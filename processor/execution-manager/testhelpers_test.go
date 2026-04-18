package executionmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/c360studio/semspec/tools/sandbox"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/component"
	sscache "github.com/c360studio/semstreams/pkg/cache"
	nats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// ---------------------------------------------------------------------------
// mockMsg implements jetstream.Msg for unit tests.
// ---------------------------------------------------------------------------

type mockMsg struct {
	data    []byte
	subject string
	acked   bool
	naked   bool
}

func (m *mockMsg) Data() []byte                              { return m.data }
func (m *mockMsg) Subject() string                           { return m.subject }
func (m *mockMsg) Reply() string                             { return "" }
func (m *mockMsg) Headers() nats.Header                      { return nil }
func (m *mockMsg) Metadata() (*jetstream.MsgMetadata, error) { return nil, nil }
func (m *mockMsg) Ack() error                                { m.acked = true; return nil }
func (m *mockMsg) DoubleAck(_ context.Context) error         { m.acked = true; return nil }
func (m *mockMsg) Nak() error                                { m.naked = true; return nil }
func (m *mockMsg) NakWithDelay(_ time.Duration) error        { m.naked = true; return nil }
func (m *mockMsg) InProgress() error                         { return nil }
func (m *mockMsg) Term() error                               { return nil }
func (m *mockMsg) TermWithReason(_ string) error             { return nil }

// ---------------------------------------------------------------------------
// stubRegistry satisfies RegistryInterface for Register() tests.
// ---------------------------------------------------------------------------

type stubRegistry struct {
	called bool
	cfg    component.RegistrationConfig
}

func (r *stubRegistry) RegisterWithConfig(cfg component.RegistrationConfig) error {
	r.called = true
	r.cfg = cfg
	return nil
}

// ---------------------------------------------------------------------------
// stubSandbox satisfies worktreeManager for tests that need a non-nil sandbox.
// ---------------------------------------------------------------------------

type stubSandbox struct{}

func (s *stubSandbox) CreateWorktree(_ context.Context, _ string, _ ...sandbox.WorktreeOption) (*sandbox.WorktreeInfo, error) {
	return &sandbox.WorktreeInfo{Status: "created", Path: "/tmp/test-wt", Branch: "agent/test"}, nil
}
func (s *stubSandbox) DeleteWorktree(_ context.Context, _ string) error { return nil }
func (s *stubSandbox) MergeWorktree(_ context.Context, _ string, _ ...sandbox.MergeOption) (*sandbox.MergeResult, error) {
	return &sandbox.MergeResult{}, nil
}
func (s *stubSandbox) ListWorktreeFiles(_ context.Context, _ string) ([]sandbox.FileEntry, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// newTestComponent builds a Component with default config and no NATS client.
// The nil NATSClient means publish/request calls are silently skipped, which
// is exactly what we want for unit tests that focus on state transitions.
// ---------------------------------------------------------------------------

func newTestComponent(t *testing.T) *Component {
	t.Helper()
	rawCfg, _ := json.Marshal(map[string]any{})
	deps := component.Dependencies{NATSClient: nil}
	disc, err := NewComponent(rawCfg, deps)
	if err != nil {
		t.Fatalf("newTestComponent: NewComponent failed: %v", err)
	}
	c := disc.(*Component)

	// Initialize typed caches that are normally created in Start().
	ctx := context.Background()
	ae, err := sscache.NewTTL[*taskExecution](ctx, 4*time.Hour, 30*time.Minute)
	if err != nil {
		t.Fatalf("newTestComponent: create active execs cache: %v", err)
	}
	c.activeExecs = ae
	tr, err := sscache.NewTTL[string](ctx, 4*time.Hour, 30*time.Minute)
	if err != nil {
		t.Fatalf("newTestComponent: create task routing cache: %v", err)
	}
	c.taskRouting = tr
	return c
}

// ---------------------------------------------------------------------------
// newTestExec creates a taskExecution for state-machine tests.
// ---------------------------------------------------------------------------

func newTestExec(slug, taskID string) *taskExecution {
	entityID := fmt.Sprintf("%s.exec.task.run.%s-%s", workflow.EntityPrefix(), slug, taskID)
	return &taskExecution{
		key: workflow.TaskExecutionKey(slug, taskID),
		TaskExecution: &workflow.TaskExecution{
			EntityID:     entityID,
			Slug:         slug,
			TaskID:       taskID,
			TDDCycle:     0,
			MaxTDDCycles: 3,
		},
	}
}

// ---------------------------------------------------------------------------
// testCtx returns a background context that is always valid for unit tests.
// ---------------------------------------------------------------------------

func testCtx(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}

// ---------------------------------------------------------------------------
// mockKVEntry implements jetstream.KeyValueEntry for handleTaskPending tests.
// ---------------------------------------------------------------------------

type mockKVEntry struct {
	key   string
	value []byte
	op    jetstream.KeyValueOp
}

func (e *mockKVEntry) Bucket() string                  { return "EXECUTION_STATES" }
func (e *mockKVEntry) Key() string                     { return e.key }
func (e *mockKVEntry) Value() []byte                   { return e.value }
func (e *mockKVEntry) Revision() uint64                { return 1 }
func (e *mockKVEntry) Created() time.Time              { return time.Time{} }
func (e *mockKVEntry) Delta() uint64                   { return 0 }
func (e *mockKVEntry) Operation() jetstream.KeyValueOp { return e.op }

// makeKVEntry builds a mockKVEntry for handleTaskPending unit tests.
func makeKVEntry(t *testing.T, key string, fields map[string]any) *mockKVEntry {
	t.Helper()
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("makeKVEntry: marshal fields: %v", err)
	}
	return &mockKVEntry{
		key:   key,
		value: data,
		op:    jetstream.KeyValuePut,
	}
}
