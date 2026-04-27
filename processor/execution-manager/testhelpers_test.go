package executionmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
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
// Optional mergeErr drives merge-failure scenarios; default behavior returns
// success from every method.
// ---------------------------------------------------------------------------

type stubSandbox struct {
	mergeErr     error
	mergeCallsMu sync.Mutex
	mergeCalls   int
	// lastMergeOpts records the MergeOption list of the most recent call so
	// tests can assert on commit trailers etc. Guarded by mergeCallsMu.
	lastMergeOpts []sandbox.MergeOption
	// mergeResult lets tests inject a specific MergeResult shape (e.g. a
	// commit hash + non-empty FilesChanged for happy-path assertions, or an
	// empty result to simulate the silent-no-op bug). Default zero-value
	// returns &sandbox.MergeResult{} — empty Status/Commit/FilesChanged.
	mergeResult *sandbox.MergeResult
}

func (s *stubSandbox) CreateWorktree(_ context.Context, _ string, _ ...sandbox.WorktreeOption) (*sandbox.WorktreeInfo, error) {
	return &sandbox.WorktreeInfo{Status: "created", Path: "/tmp/test-wt", Branch: "agent/test"}, nil
}
func (s *stubSandbox) DeleteWorktree(_ context.Context, _ string) error { return nil }
func (s *stubSandbox) MergeWorktree(_ context.Context, _ string, opts ...sandbox.MergeOption) (*sandbox.MergeResult, error) {
	s.mergeCallsMu.Lock()
	s.mergeCalls++
	s.lastMergeOpts = opts
	s.mergeCallsMu.Unlock()
	if s.mergeErr != nil {
		return nil, s.mergeErr
	}
	if s.mergeResult != nil {
		return s.mergeResult, nil
	}
	return &sandbox.MergeResult{}, nil
}

func (s *stubSandbox) MergeCallCount() int {
	s.mergeCallsMu.Lock()
	defer s.mergeCallsMu.Unlock()
	return s.mergeCalls
}

// capturedTrailers returns the Trailer key/value pairs applied to the last
// MergeWorktree call by running the MergeOption list against sandbox's
// internal options struct (via the exported TrailersFromOptions test helper).
func (s *stubSandbox) capturedTrailers() map[string]string {
	s.mergeCallsMu.Lock()
	defer s.mergeCallsMu.Unlock()
	return sandbox.TrailersFromOptions(s.lastMergeOpts)
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
	// Initialize execution store (kvStore nil → cache-only mode).
	store, err := newExecutionStore(ctx, nil, c.tripleWriter, c.logger)
	if err != nil {
		t.Fatalf("newTestComponent: create execution store: %v", err)
	}
	c.store = store
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
