package spawn_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semspec/tools/spawn"
)

// -- mock implementations --

// mockNATSClient records publish calls.
type mockNATSClient struct {
	mu         sync.Mutex
	published  []publishedMsg
	publishErr error
}

type publishedMsg struct {
	subject string
	data    []byte
}

func newMockNATSClient() *mockNATSClient {
	return &mockNATSClient{}
}

func (m *mockNATSClient) PublishToStream(_ context.Context, subject string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.publishErr != nil {
		return m.publishErr
	}
	m.published = append(m.published, publishedMsg{subject: subject, data: data})
	return nil
}

// mockGraphHelper records RecordSpawn calls.
type mockGraphHelper struct {
	mu       sync.Mutex
	spawns   []spawnRecord
	spawnErr error
}

type spawnRecord struct {
	parentLoopID string
	childLoopID  string
	role         string
	model        string
}

func (g *mockGraphHelper) RecordSpawn(_ context.Context, parentLoopID, childLoopID, role, model string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.spawnErr != nil {
		return g.spawnErr
	}
	g.spawns = append(g.spawns, spawnRecord{
		parentLoopID: parentLoopID,
		childLoopID:  childLoopID,
		role:         role,
		model:        model,
	})
	return nil
}

// mockKeyWatcher implements jetstream.KeyWatcher for test control.
type mockKeyWatcher struct {
	ch     chan jetstream.KeyValueEntry
	closed bool
	mu     sync.Mutex
}

func newMockKeyWatcher() *mockKeyWatcher {
	return &mockKeyWatcher{ch: make(chan jetstream.KeyValueEntry, 10)}
}

func (w *mockKeyWatcher) Updates() <-chan jetstream.KeyValueEntry {
	return w.ch
}

func (w *mockKeyWatcher) Stop() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.closed {
		w.closed = true
		close(w.ch)
	}
	return nil
}

// sendEntry sends a KV entry to the watcher. Non-blocking with buffer.
func (w *mockKeyWatcher) sendEntry(entry jetstream.KeyValueEntry) {
	w.ch <- entry
}

// sendNil sends the end-of-replay signal.
func (w *mockKeyWatcher) sendNil() {
	w.ch <- nil
}

// mockLoopWatcher implements spawn.LoopWatcher for test control.
type mockLoopWatcher struct {
	mu       sync.Mutex
	watchers map[string]*mockKeyWatcher
	watchErr error
}

func newMockLoopWatcher() *mockLoopWatcher {
	return &mockLoopWatcher{watchers: make(map[string]*mockKeyWatcher)}
}

func (m *mockLoopWatcher) Watch(_ context.Context, key string, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.watchErr != nil {
		return nil, m.watchErr
	}
	w := newMockKeyWatcher()
	m.watchers[key] = w
	// Send nil to signal end of initial replay (no existing entries).
	go func() { w.sendNil() }()
	return w, nil
}

// watcherFor returns the watcher created for the given key, polling briefly.
func (m *mockLoopWatcher) watcherFor(t *testing.T, key string) *mockKeyWatcher {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		w := m.watchers[key]
		m.mu.Unlock()
		if w != nil {
			return w
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for watcher on key %q", key)
	return nil
}

// mockKVEntry implements jetstream.KeyValueEntry for test use.
type mockKVEntry struct {
	key   string
	value []byte
	op    jetstream.KeyValueOp
}

func (e *mockKVEntry) Bucket() string                  { return "AGENT_LOOPS" }
func (e *mockKVEntry) Key() string                     { return e.key }
func (e *mockKVEntry) Value() []byte                   { return e.value }
func (e *mockKVEntry) Revision() uint64                { return 1 }
func (e *mockKVEntry) Created() time.Time              { return time.Now() }
func (e *mockKVEntry) Delta() uint64                   { return 0 }
func (e *mockKVEntry) Operation() jetstream.KeyValueOp { return e.op }
func (e *mockKVEntry) Headers() jetstream.MsgMetadata  { return jetstream.MsgMetadata{} }

// -- helpers --

// buildLoopEntity creates a serialized LoopEntity for KV entry value.
func buildLoopEntity(t *testing.T, loopID string, state agentic.LoopState, outcome, result, errMsg string) []byte {
	t.Helper()
	entity := agentic.LoopEntity{
		ID:      loopID,
		State:   state,
		Outcome: outcome,
		Result:  result,
		Error:   errMsg,
	}
	data, err := json.Marshal(entity)
	if err != nil {
		t.Fatalf("marshal LoopEntity: %v", err)
	}
	return data
}

// putEntry creates a KV put entry for the given loop entity.
func putEntry(key string, value []byte) jetstream.KeyValueEntry {
	return &mockKVEntry{key: key, value: value, op: jetstream.KeyValuePut}
}

// baseCall returns a minimal ToolCall for the spawn_agent tool.
func baseCall(prompt, role string) agentic.ToolCall {
	return agentic.ToolCall{
		ID:     "call-1",
		Name:   "spawn_agent",
		LoopID: "parent-loop",
		Arguments: map[string]any{
			"prompt":  prompt,
			"role":    role,
			"timeout": "100ms",
		},
	}
}

// extractChildLoopID waits for a publish to agent.task.* and extracts the
// child loop ID from the TaskMessage payload.
func extractChildLoopID(t *testing.T, m *mockNATSClient) string {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		pubs := m.published
		m.mu.Unlock()
		for _, p := range pubs {
			if strings.HasPrefix(p.subject, "agent.task.") {
				var env struct {
					Payload agentic.TaskMessage `json:"payload"`
				}
				if err := json.Unmarshal(p.data, &env); err == nil && env.Payload.LoopID != "" {
					return env.Payload.LoopID
				}
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("timed out waiting for published TaskMessage to extract child loop ID")
	return ""
}

// newTestExecutor creates an executor with standard test dependencies.
func newTestExecutor(nats *mockNATSClient, graph *mockGraphHelper, loops *mockLoopWatcher, opts ...spawn.Option) *spawn.Executor {
	allOpts := []spawn.Option{
		spawn.WithDefaultModel("claude-3-5-sonnet"),
		spawn.WithMaxDepth(5),
		spawn.WithLoopsBucket(loops),
	}
	allOpts = append(allOpts, opts...)
	return spawn.NewExecutor(nats, graph, allOpts...)
}

// -- tests --

func TestExecutor_ListTools(t *testing.T) {
	t.Parallel()

	e := spawn.NewExecutor(newMockNATSClient(), &mockGraphHelper{})
	tools := e.ListTools()

	if len(tools) != 1 {
		t.Fatalf("ListTools() returned %d tools, want 1", len(tools))
	}
	tool := tools[0]
	if tool.Name != "spawn_agent" {
		t.Errorf("tool name = %q, want %q", tool.Name, "spawn_agent")
	}
	if tool.Description == "" {
		t.Error("tool description must not be empty")
	}
	params, ok := tool.Parameters["required"].([]string)
	if !ok {
		t.Fatal("tool parameters missing 'required' slice")
	}
	requiredSet := make(map[string]bool, len(params))
	for _, p := range params {
		requiredSet[p] = true
	}
	if !requiredSet["prompt"] {
		t.Error("'prompt' must be in the required list")
	}
	if !requiredSet["role"] {
		t.Error("'role' must be in the required list")
	}
}

func TestExecutor_ListTools_IncludesNewParameters(t *testing.T) {
	t.Parallel()

	e := spawn.NewExecutor(newMockNATSClient(), &mockGraphHelper{})
	tools := e.ListTools()
	props, ok := tools[0].Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("tool parameters missing 'properties'")
	}

	for _, name := range []string{"system_context", "workflow_slug", "workflow_step", "metadata"} {
		if _, exists := props[name]; !exists {
			t.Errorf("missing parameter %q in tool definition", name)
		}
	}
}

func TestExecutor_SuccessfulSpawn(t *testing.T) {
	t.Parallel()

	mockNATS := newMockNATSClient()
	mockGraph := &mockGraphHelper{}
	mockLoops := newMockLoopWatcher()

	e := newTestExecutor(mockNATS, mockGraph, mockLoops)

	resultCh := make(chan agentic.ToolResult, 1)
	go func() {
		r, _ := e.Execute(context.Background(), baseCall("write hello world", "developer"))
		resultCh <- r
	}()

	childLoopID := extractChildLoopID(t, mockNATS)
	w := mockLoops.watcherFor(t, childLoopID)
	w.sendEntry(putEntry(childLoopID, buildLoopEntity(t, childLoopID, agentic.LoopStateComplete, agentic.OutcomeSuccess, "Hello, World!", "")))

	result := <-resultCh
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Content != "Hello, World!" {
		t.Errorf("Content = %q, want %q", result.Content, "Hello, World!")
	}
	if result.CallID != "call-1" {
		t.Errorf("CallID = %q, want %q", result.CallID, "call-1")
	}
}

func TestExecutor_ChildFailure(t *testing.T) {
	t.Parallel()

	mockNATS := newMockNATSClient()
	mockLoops := newMockLoopWatcher()
	e := newTestExecutor(mockNATS, &mockGraphHelper{}, mockLoops)

	resultCh := make(chan agentic.ToolResult, 1)
	go func() {
		r, _ := e.Execute(context.Background(), baseCall("failing task", "executor"))
		resultCh <- r
	}()

	childLoopID := extractChildLoopID(t, mockNATS)
	w := mockLoops.watcherFor(t, childLoopID)
	w.sendEntry(putEntry(childLoopID, buildLoopEntity(t, childLoopID, agentic.LoopStateFailed, agentic.OutcomeFailed, "", "max iterations reached")))

	result := <-resultCh
	if !strings.Contains(result.Error, "max iterations reached") {
		t.Errorf("Error = %q, want it to contain 'max iterations reached'", result.Error)
	}
}

func TestExecutor_ChildCancelled(t *testing.T) {
	t.Parallel()

	mockNATS := newMockNATSClient()
	mockLoops := newMockLoopWatcher()
	e := newTestExecutor(mockNATS, &mockGraphHelper{}, mockLoops)

	resultCh := make(chan agentic.ToolResult, 1)
	go func() {
		r, _ := e.Execute(context.Background(), baseCall("cancelled task", "executor"))
		resultCh <- r
	}()

	childLoopID := extractChildLoopID(t, mockNATS)
	w := mockLoops.watcherFor(t, childLoopID)
	w.sendEntry(putEntry(childLoopID, buildLoopEntity(t, childLoopID, agentic.LoopStateCancelled, agentic.OutcomeCancelled, "", "")))

	result := <-resultCh
	if result.Error == "" {
		t.Fatal("expected error for cancelled child loop")
	}
	if !strings.Contains(result.Error, "terminal state") {
		t.Errorf("Error = %q, want it to mention 'terminal state'", result.Error)
	}
}

func TestExecutor_SkipsNonTerminalUpdates(t *testing.T) {
	t.Parallel()

	mockNATS := newMockNATSClient()
	mockLoops := newMockLoopWatcher()
	e := newTestExecutor(mockNATS, &mockGraphHelper{}, mockLoops)

	resultCh := make(chan agentic.ToolResult, 1)
	go func() {
		r, _ := e.Execute(context.Background(), baseCall("progressing task", "developer"))
		resultCh <- r
	}()

	childLoopID := extractChildLoopID(t, mockNATS)
	w := mockLoops.watcherFor(t, childLoopID)

	// Send non-terminal updates first — should be skipped.
	w.sendEntry(putEntry(childLoopID, buildLoopEntity(t, childLoopID, agentic.LoopStateExploring, "", "", "")))
	w.sendEntry(putEntry(childLoopID, buildLoopEntity(t, childLoopID, agentic.LoopStateExecuting, "", "", "")))
	// Then terminal.
	w.sendEntry(putEntry(childLoopID, buildLoopEntity(t, childLoopID, agentic.LoopStateComplete, agentic.OutcomeSuccess, "done", "")))

	result := <-resultCh
	if result.Content != "done" {
		t.Errorf("Content = %q, want %q", result.Content, "done")
	}
}

func TestExecutor_Timeout(t *testing.T) {
	t.Parallel()

	mockLoops := newMockLoopWatcher()
	e := newTestExecutor(newMockNATSClient(), &mockGraphHelper{}, mockLoops)

	result, err := e.Execute(context.Background(), baseCall("slow task", "executor"))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !strings.Contains(result.Error, "timed out") {
		t.Errorf("Error = %q, want it to contain 'timed out'", result.Error)
	}
}

func TestExecutor_ContextCancellation(t *testing.T) {
	t.Parallel()

	mockNATS := newMockNATSClient()
	mockLoops := newMockLoopWatcher()
	e := newTestExecutor(mockNATS, &mockGraphHelper{}, mockLoops)

	ctx, cancel := context.WithCancel(context.Background())

	resultCh := make(chan agentic.ToolResult, 1)
	go func() {
		r, _ := e.Execute(ctx, agentic.ToolCall{
			ID:     "c",
			Name:   "spawn_agent",
			LoopID: "parent-loop",
			Arguments: map[string]any{
				"prompt":  "cancelled task",
				"role":    "executor",
				"timeout": "30s",
			},
		})
		resultCh <- r
	}()

	// Wait for publish, then cancel.
	extractChildLoopID(t, mockNATS)
	cancel()

	result := <-resultCh
	if !strings.Contains(result.Error, "context cancelled") {
		t.Errorf("Error = %q, want it to contain 'context cancelled'", result.Error)
	}
}

func TestExecutor_DepthLimitExceeded(t *testing.T) {
	t.Parallel()

	mockLoops := newMockLoopWatcher()
	e := newTestExecutor(newMockNATSClient(), &mockGraphHelper{}, mockLoops,
		spawn.WithMaxDepth(3),
	)

	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c",
		Name:   "spawn_agent",
		LoopID: "p",
		Arguments: map[string]any{
			"prompt": "deep task",
			"role":   "executor",
			"depth":  float64(2), // 2+1=3 == maxDepth → reject
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !strings.Contains(result.Error, "depth limit reached") {
		t.Errorf("Error = %q, want 'depth limit reached'", result.Error)
	}
}

func TestExecutor_MissingArguments(t *testing.T) {
	t.Parallel()

	mockLoops := newMockLoopWatcher()
	e := newTestExecutor(newMockNATSClient(), &mockGraphHelper{}, mockLoops)

	t.Run("missing prompt", func(t *testing.T) {
		result, _ := e.Execute(context.Background(), agentic.ToolCall{
			ID: "c", Name: "spawn_agent", LoopID: "p",
			Arguments: map[string]any{"role": "executor"},
		})
		if !strings.Contains(result.Error, "'prompt' is required") {
			t.Errorf("Error = %q", result.Error)
		}
	})

	t.Run("missing role", func(t *testing.T) {
		result, _ := e.Execute(context.Background(), agentic.ToolCall{
			ID: "c", Name: "spawn_agent", LoopID: "p",
			Arguments: map[string]any{"prompt": "task"},
		})
		if !strings.Contains(result.Error, "'role' is required") {
			t.Errorf("Error = %q", result.Error)
		}
	})
}

func TestExecutor_NoLoopsBucket(t *testing.T) {
	t.Parallel()

	// No WithLoopsBucket — loopsBucket is nil.
	e := spawn.NewExecutor(newMockNATSClient(), &mockGraphHelper{},
		spawn.WithDefaultModel("m"),
	)

	result, err := e.Execute(context.Background(), baseCall("task", "developer"))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !strings.Contains(result.Error, "AGENT_LOOPS KV bucket not configured") {
		t.Errorf("Error = %q, want mention of bucket not configured", result.Error)
	}
}

func TestExecutor_NoModelReturnsError(t *testing.T) {
	t.Parallel()

	mockLoops := newMockLoopWatcher()
	e := spawn.NewExecutor(newMockNATSClient(), &mockGraphHelper{},
		spawn.WithLoopsBucket(mockLoops),
		// no WithDefaultModel
	)

	result, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID: "c", Name: "spawn_agent", LoopID: "p",
		Arguments: map[string]any{
			"prompt":  "task",
			"role":    "developer",
			"timeout": "100ms",
		},
	})
	if !strings.Contains(result.Error, "no model specified") {
		t.Errorf("Error = %q, want 'no model specified'", result.Error)
	}
}

func TestExecutor_PublishFailure(t *testing.T) {
	t.Parallel()

	mockNATS := newMockNATSClient()
	mockNATS.publishErr = errors.New("NATS: stream not found")
	mockLoops := newMockLoopWatcher()
	e := newTestExecutor(mockNATS, &mockGraphHelper{}, mockLoops)

	_, err := e.Execute(context.Background(), baseCall("task", "executor"))
	if err == nil {
		t.Fatal("expected Go error from publish failure")
	}
	if !strings.Contains(err.Error(), "publish task") {
		t.Errorf("error = %q, want mention of 'publish task'", err.Error())
	}
}

func TestExecutor_GraphErrorNonFatal(t *testing.T) {
	t.Parallel()

	mockNATS := newMockNATSClient()
	mockLoops := newMockLoopWatcher()
	e := newTestExecutor(mockNATS, &mockGraphHelper{spawnErr: errors.New("graph down")}, mockLoops)

	resultCh := make(chan agentic.ToolResult, 1)
	go func() {
		r, _ := e.Execute(context.Background(), baseCall("task", "developer"))
		resultCh <- r
	}()

	childLoopID := extractChildLoopID(t, mockNATS)
	w := mockLoops.watcherFor(t, childLoopID)
	w.sendEntry(putEntry(childLoopID, buildLoopEntity(t, childLoopID, agentic.LoopStateComplete, agentic.OutcomeSuccess, "result", "")))

	result := <-resultCh
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Content != "result" {
		t.Errorf("Content = %q, want %q", result.Content, "result")
	}
	warn, _ := result.Metadata["warning"].(string)
	if !strings.Contains(warn, "graph recording failed") {
		t.Errorf("Metadata[warning] = %q, want 'graph recording failed'", warn)
	}
}

func TestExecutor_ContextPassthrough(t *testing.T) {
	t.Parallel()

	mockNATS := newMockNATSClient()
	mockLoops := newMockLoopWatcher()
	e := newTestExecutor(mockNATS, &mockGraphHelper{}, mockLoops)

	resultCh := make(chan agentic.ToolResult, 1)
	go func() {
		r, _ := e.Execute(context.Background(), agentic.ToolCall{
			ID:     "c",
			Name:   "spawn_agent",
			LoopID: "parent-loop",
			Arguments: map[string]any{
				"prompt":         "focused planning task",
				"role":           "general",
				"timeout":        "500ms",
				"system_context": "You are a focused planner.",
				"workflow_slug":  "planning",
				"workflow_step":  "drafting",
				"metadata": map[string]any{
					"plan_slug":  "add-auth",
					"focus_area": "security",
				},
			},
		})
		resultCh <- r
	}()

	childLoopID := extractChildLoopID(t, mockNATS)

	// Verify TaskMessage has context fields.
	mockNATS.mu.Lock()
	pubs := mockNATS.published
	mockNATS.mu.Unlock()

	var taskMsg agentic.TaskMessage
	for _, p := range pubs {
		if strings.HasPrefix(p.subject, "agent.task.") {
			var env struct {
				Payload agentic.TaskMessage `json:"payload"`
			}
			if err := json.Unmarshal(p.data, &env); err == nil {
				taskMsg = env.Payload
			}
		}
	}

	if taskMsg.WorkflowSlug != "planning" {
		t.Errorf("TaskMessage.WorkflowSlug = %q, want %q", taskMsg.WorkflowSlug, "planning")
	}
	if taskMsg.WorkflowStep != "drafting" {
		t.Errorf("TaskMessage.WorkflowStep = %q, want %q", taskMsg.WorkflowStep, "drafting")
	}
	if taskMsg.Context == nil {
		t.Fatal("TaskMessage.Context is nil, want system_context")
	}
	if taskMsg.Context.Content != "You are a focused planner." {
		t.Errorf("TaskMessage.Context.Content = %q, want system prompt", taskMsg.Context.Content)
	}
	if slug, _ := taskMsg.Metadata["plan_slug"].(string); slug != "add-auth" {
		t.Errorf("TaskMessage.Metadata[plan_slug] = %q, want %q", slug, "add-auth")
	}
	if focus, _ := taskMsg.Metadata["focus_area"].(string); focus != "security" {
		t.Errorf("TaskMessage.Metadata[focus_area] = %q, want %q", focus, "security")
	}

	// Complete the loop so Execute returns.
	w := mockLoops.watcherFor(t, childLoopID)
	w.sendEntry(putEntry(childLoopID, buildLoopEntity(t, childLoopID, agentic.LoopStateComplete, agentic.OutcomeSuccess, "done", "")))
	<-resultCh
}

func TestExecutor_BackwardCompat_NoContextFields(t *testing.T) {
	t.Parallel()

	mockNATS := newMockNATSClient()
	mockLoops := newMockLoopWatcher()
	e := newTestExecutor(mockNATS, &mockGraphHelper{}, mockLoops)

	resultCh := make(chan agentic.ToolResult, 1)
	go func() {
		r, _ := e.Execute(context.Background(), baseCall("simple task", "developer"))
		resultCh <- r
	}()

	childLoopID := extractChildLoopID(t, mockNATS)

	// Verify TaskMessage has no context fields when not provided.
	mockNATS.mu.Lock()
	pubs := mockNATS.published
	mockNATS.mu.Unlock()

	for _, p := range pubs {
		if strings.HasPrefix(p.subject, "agent.task.") {
			var env struct {
				Payload agentic.TaskMessage `json:"payload"`
			}
			if err := json.Unmarshal(p.data, &env); err == nil {
				if env.Payload.Context != nil {
					t.Error("TaskMessage.Context should be nil when system_context not provided")
				}
				if env.Payload.WorkflowSlug != "" {
					t.Errorf("TaskMessage.WorkflowSlug = %q, want empty", env.Payload.WorkflowSlug)
				}
			}
		}
	}

	w := mockLoops.watcherFor(t, childLoopID)
	w.sendEntry(putEntry(childLoopID, buildLoopEntity(t, childLoopID, agentic.LoopStateComplete, agentic.OutcomeSuccess, "ok", "")))
	<-resultCh
}

func TestExecutor_ChildMetadataInResult(t *testing.T) {
	t.Parallel()

	mockNATS := newMockNATSClient()
	mockLoops := newMockLoopWatcher()
	e := newTestExecutor(mockNATS, &mockGraphHelper{}, mockLoops)

	resultCh := make(chan agentic.ToolResult, 1)
	go func() {
		r, _ := e.Execute(context.Background(), baseCall("check metadata", "developer"))
		resultCh <- r
	}()

	childLoopID := extractChildLoopID(t, mockNATS)
	w := mockLoops.watcherFor(t, childLoopID)
	w.sendEntry(putEntry(childLoopID, buildLoopEntity(t, childLoopID, agentic.LoopStateComplete, agentic.OutcomeSuccess, "result", "")))

	result := <-resultCh
	if result.Metadata == nil {
		t.Fatal("Metadata is nil")
	}
	if _, ok := result.Metadata["child_loop_id"]; !ok {
		t.Error("missing 'child_loop_id' in Metadata")
	}
	if _, ok := result.Metadata["task_id"]; !ok {
		t.Error("missing 'task_id' in Metadata")
	}
}

func TestExecutor_PublishSubjectFormat(t *testing.T) {
	t.Parallel()

	mockNATS := newMockNATSClient()
	mockLoops := newMockLoopWatcher()
	e := newTestExecutor(mockNATS, &mockGraphHelper{}, mockLoops)

	go func() {
		e.Execute(context.Background(), baseCall("check subject", "executor"))
	}()

	// Wait for publish.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		mockNATS.mu.Lock()
		n := len(mockNATS.published)
		mockNATS.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	mockNATS.mu.Lock()
	pubs := mockNATS.published
	mockNATS.mu.Unlock()

	if len(pubs) == 0 {
		t.Fatal("no messages published")
	}
	if !strings.HasPrefix(pubs[0].subject, "agent.task.") {
		t.Errorf("subject = %q, want agent.task.* prefix", pubs[0].subject)
	}
}

func TestExecutor_DepthCoercion_IntDepth(t *testing.T) {
	t.Parallel()

	mockLoops := newMockLoopWatcher()
	e := newTestExecutor(newMockNATSClient(), &mockGraphHelper{}, mockLoops,
		spawn.WithMaxDepth(5),
	)

	result, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID: "c", Name: "spawn_agent", LoopID: "p",
		Arguments: map[string]any{
			"prompt": "deep",
			"role":   "executor",
			"depth":  4, // int, not float64; 4+1=5==maxDepth → reject
		},
	})
	if !strings.Contains(result.Error, "depth limit reached") {
		t.Errorf("Error = %q, want 'depth limit reached'", result.Error)
	}
}

func TestExecutor_InvalidTimeout(t *testing.T) {
	t.Parallel()

	mockLoops := newMockLoopWatcher()
	e := newTestExecutor(newMockNATSClient(), &mockGraphHelper{}, mockLoops)

	result, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID: "c", Name: "spawn_agent", LoopID: "p",
		Arguments: map[string]any{
			"prompt":  "task",
			"role":    "executor",
			"timeout": "not-a-duration",
		},
	})
	if !strings.Contains(result.Error, "invalid timeout") {
		t.Errorf("Error = %q, want 'invalid timeout'", result.Error)
	}
}

func TestExecutor_DefaultModelUsed(t *testing.T) {
	t.Parallel()

	mockNATS := newMockNATSClient()
	mockLoops := newMockLoopWatcher()
	e := newTestExecutor(mockNATS, &mockGraphHelper{}, mockLoops)

	resultCh := make(chan agentic.ToolResult, 1)
	go func() {
		r, _ := e.Execute(context.Background(), agentic.ToolCall{
			ID: "c", Name: "spawn_agent", LoopID: "p",
			Arguments: map[string]any{
				"prompt":  "task",
				"role":    "developer",
				"timeout": "500ms",
				// no "model"
			},
		})
		resultCh <- r
	}()

	childLoopID := extractChildLoopID(t, mockNATS)
	w := mockLoops.watcherFor(t, childLoopID)
	w.sendEntry(putEntry(childLoopID, buildLoopEntity(t, childLoopID, agentic.LoopStateComplete, agentic.OutcomeSuccess, "ok", "")))
	<-resultCh

	mockNATS.mu.Lock()
	pubs := mockNATS.published
	mockNATS.mu.Unlock()

	var env struct {
		Payload agentic.TaskMessage `json:"payload"`
	}
	json.Unmarshal(pubs[0].data, &env)
	if env.Payload.Model != "claude-3-5-sonnet" {
		t.Errorf("Model = %q, want default %q", env.Payload.Model, "claude-3-5-sonnet")
	}
}
