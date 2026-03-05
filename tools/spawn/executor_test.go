package spawn_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"

	"github.com/c360studio/semspec/tools/spawn"
)

// -- mock implementations --

// mockSubscription implements spawn.Subscription.
type mockSubscription struct {
	unsubscribed bool
}

func (s *mockSubscription) Unsubscribe() error {
	s.unsubscribed = true
	return nil
}

// mockNATSClient records publish calls and allows tests to inject handler
// references so they can drive message delivery synchronously.
type mockNATSClient struct {
	mu            sync.Mutex
	published     []publishedMsg
	publishErr    error
	subscribeErr  error
	subscriptions []*capturedSubscription
}

type publishedMsg struct {
	subject string
	data    []byte
}

// capturedSubscription records a subscription and retains the handler so
// tests can fire messages directly.
type capturedSubscription struct {
	subject string
	handler func([]byte)
}

func (s *capturedSubscription) fire(data []byte) {
	if s.handler != nil {
		s.handler(data)
	}
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

func (m *mockNATSClient) Subscribe(
	_ context.Context,
	subject string,
	handler func([]byte),
) (spawn.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.subscribeErr != nil {
		return nil, m.subscribeErr
	}
	cs := &capturedSubscription{subject: subject, handler: handler}
	m.subscriptions = append(m.subscriptions, cs)
	return &mockSubscription{}, nil
}

// subscriptionForSubject returns the capturedSubscription whose subject
// matches the given pattern, or nil if not found. Thread-safe.
func (m *mockNATSClient) subscriptionForSubject(subject string) *capturedSubscription {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.subscriptions {
		if s.subject == subject {
			return s
		}
	}
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

// -- helpers --

// buildCompletedPayload constructs the JSON that the executor expects on
// agent.complete.<loopID>: a BaseMessage envelope with a LoopCompletedEvent
// payload.
func buildCompletedPayload(t *testing.T, loopID, taskID, result string) []byte {
	t.Helper()
	event := agentic.LoopCompletedEvent{
		LoopID:  loopID,
		TaskID:  taskID,
		Outcome: agentic.OutcomeSuccess,
		Result:  result,
	}
	return wrapPayload(t, event)
}

// buildFailedPayload constructs the JSON for a LoopFailedEvent envelope.
func buildFailedPayload(t *testing.T, loopID, taskID, reason, errMsg string) []byte {
	t.Helper()
	event := agentic.LoopFailedEvent{
		LoopID:  loopID,
		TaskID:  taskID,
		Outcome: agentic.OutcomeFailed,
		Reason:  reason,
		Error:   errMsg,
	}
	return wrapPayload(t, event)
}

// wrapPayload encodes a payload as a minimal BaseMessage JSON envelope that
// unmarshalPayload (the private helper inside executor.go) can decode.
func wrapPayload(t *testing.T, payload any) []byte {
	t.Helper()
	inner, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("wrapPayload: marshal inner: %v", err)
	}
	envelope := map[string]json.RawMessage{
		"payload": json.RawMessage(inner),
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("wrapPayload: marshal envelope: %v", err)
	}
	return data
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
			"timeout": "100ms", // short timeout for tests
		},
	}
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

func TestExecutor_Execute(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		call           agentic.ToolCall
		publishErr     error
		subscribeErr   error
		graphErr       error
		noDefaultModel bool
		cancelCtx      bool // cancel context instead of driving a completion event
		drive          func(t *testing.T, m *mockNATSClient, childLoopID string)
		wantContent    string
		wantErrMsg     string // non-empty portion in ToolResult.Error
		wantGoErr      bool   // true when Execute itself returns a non-nil error
	}{
		{
			name: "successful spawn returns child result",
			call: baseCall("write a hello world program", "developer"),
			drive: func(t *testing.T, m *mockNATSClient, childLoopID string) {
				t.Helper()
				sub := waitForSubscription(t, m, fmt.Sprintf("agent.complete.%s", childLoopID))
				sub.fire(buildCompletedPayload(t, childLoopID, "task-x", "Hello, World!"))
			},
			wantContent: "Hello, World!",
		},
		{
			name: "child failure returns error tool result",
			call: baseCall("do something", "executor"),
			drive: func(t *testing.T, m *mockNATSClient, childLoopID string) {
				t.Helper()
				sub := waitForSubscription(t, m, fmt.Sprintf("agent.failed.%s", childLoopID))
				sub.fire(buildFailedPayload(t, childLoopID, "task-y", "iteration limit", "max iterations reached"))
			},
			wantErrMsg: "iteration limit",
		},
		{
			name:       "timeout returns error tool result",
			call:       baseCall("slow task", "executor"),
			wantErrMsg: "timed out",
		},
		{
			name: "depth limit exceeded returns error immediately",
			call: agentic.ToolCall{
				ID:     "call-depth",
				Name:   "spawn_agent",
				LoopID: "parent-loop",
				// depth is passed as a tool argument (ToolCall has no Metadata field
				// in this semstreams version). With maxDepth=5, depth 4+1=5 hits the limit.
				Arguments: map[string]any{
					"prompt": "nested task",
					"role":   "executor",
					"depth":  float64(4),
				},
			},
			wantErrMsg: "depth limit reached",
		},
		{
			name:       "context cancellation returns error tool result",
			call:       baseCall("cancelled task", "executor"),
			cancelCtx:  true,
			wantErrMsg: "context cancelled",
		},
		{
			name: "missing prompt argument returns error tool result",
			call: agentic.ToolCall{
				ID:     "call-no-prompt",
				Name:   "spawn_agent",
				LoopID: "parent-loop",
				Arguments: map[string]any{
					"role": "executor",
				},
			},
			wantErrMsg: "'prompt' is required",
		},
		{
			name: "missing role argument returns error tool result",
			call: agentic.ToolCall{
				ID:     "call-no-role",
				Name:   "spawn_agent",
				LoopID: "parent-loop",
				Arguments: map[string]any{
					"prompt": "do something",
				},
			},
			wantErrMsg: "'role' is required",
		},
		{
			name:       "publish failure returns Go error",
			call:       baseCall("publish fails", "executor"),
			publishErr: errors.New("NATS: stream not found"),
			wantGoErr:  true,
		},
		{
			name:         "subscribe failure returns Go error",
			call:         baseCall("subscribe fails", "executor"),
			subscribeErr: errors.New("NATS: connection closed"),
			wantGoErr:    true,
		},
		{
			name:     "graph error is non-fatal, child result still returned",
			call:     baseCall("graph fails", "executor"),
			graphErr: errors.New("bucket unavailable"),
			drive: func(t *testing.T, m *mockNATSClient, childLoopID string) {
				t.Helper()
				sub := waitForSubscription(t, m, fmt.Sprintf("agent.complete.%s", childLoopID))
				sub.fire(buildCompletedPayload(t, childLoopID, "task-g", "graph-fail-result"))
			},
			wantContent: "graph-fail-result",
		},
		{
			name:           "no model and no default returns error",
			noDefaultModel: true,
			call: agentic.ToolCall{
				ID:     "call-no-model",
				Name:   "spawn_agent",
				LoopID: "parent-loop",
				Arguments: map[string]any{
					"prompt":  "some task",
					"role":    "developer",
					"timeout": "100ms",
				},
			},
			wantErrMsg: "no model specified",
		},
		{
			name: "default model used when model arg omitted",
			call: agentic.ToolCall{
				ID:     "call-default-model",
				Name:   "spawn_agent",
				LoopID: "parent-loop",
				Arguments: map[string]any{
					"prompt":  "some task",
					"role":    "developer",
					"timeout": "100ms",
				},
			},
			drive: func(t *testing.T, m *mockNATSClient, childLoopID string) {
				t.Helper()
				sub := waitForSubscription(t, m, fmt.Sprintf("agent.complete.%s", childLoopID))
				sub.fire(buildCompletedPayload(t, childLoopID, "task-z", "done"))
			},
			wantContent: "done",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mockNATS := newMockNATSClient()
			mockNATS.publishErr = tc.publishErr
			mockNATS.subscribeErr = tc.subscribeErr

			mockGraph := &mockGraphHelper{spawnErr: tc.graphErr}

			opts := []spawn.Option{spawn.WithMaxDepth(5)}
			if !tc.noDefaultModel {
				opts = append(opts, spawn.WithDefaultModel("claude-3-5-sonnet"))
			}
			e := spawn.NewExecutor(mockNATS, mockGraph, opts...)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			var (
				result agentic.ToolResult
				goErr  error
				wg     sync.WaitGroup
			)

			wg.Add(1)
			go func() {
				defer wg.Done()
				result, goErr = e.Execute(ctx, tc.call)
			}()

			if tc.cancelCtx {
				go func() {
					// Wait until both subscriptions are registered, then cancel.
					deadline := time.Now().Add(500 * time.Millisecond)
					for time.Now().Before(deadline) {
						mockNATS.mu.Lock()
						n := len(mockNATS.subscriptions)
						mockNATS.mu.Unlock()
						if n >= 2 {
							break
						}
						time.Sleep(1 * time.Millisecond)
					}
					cancel()
				}()
			} else if tc.drive != nil {
				go func() {
					childLoopID := extractChildLoopID(t, mockNATS)
					tc.drive(t, mockNATS, childLoopID)
				}()
			}

			wg.Wait()

			if tc.wantGoErr {
				if goErr == nil {
					t.Fatalf("expected Execute to return a Go error, got nil")
				}
				return
			}
			if goErr != nil {
				t.Fatalf("Execute returned unexpected Go error: %v", goErr)
			}

			if tc.wantContent != "" {
				if result.Content != tc.wantContent {
					t.Errorf("ToolResult.Content = %q, want %q", result.Content, tc.wantContent)
				}
				if result.Error != "" {
					t.Errorf("ToolResult.Error should be empty, got %q", result.Error)
				}
			}

			if tc.wantErrMsg != "" {
				if result.Error == "" {
					t.Fatalf("expected ToolResult.Error containing %q, got empty string", tc.wantErrMsg)
				}
				if !strings.Contains(result.Error, tc.wantErrMsg) {
					t.Errorf("ToolResult.Error = %q, want it to contain %q", result.Error, tc.wantErrMsg)
				}
			}

			if tc.call.ID != "" && !tc.wantGoErr {
				if result.CallID != tc.call.ID {
					t.Errorf("ToolResult.CallID = %q, want %q", result.CallID, tc.call.ID)
				}
			}
		})
	}
}

// waitForSubscription polls until the mock records a subscription on the
// given subject.
func waitForSubscription(t *testing.T, m *mockNATSClient, subject string) *capturedSubscription {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if sub := m.subscriptionForSubject(subject); sub != nil {
			return sub
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for subscription on %q", subject)
	return nil
}

// extractChildLoopID waits for at least one subscription to appear on the
// mock client and extracts the child loop ID from the subject
// "agent.complete.<childLoopID>".
func extractChildLoopID(t *testing.T, m *mockNATSClient) string {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		subs := m.subscriptions
		m.mu.Unlock()
		for _, s := range subs {
			var childLoopID string
			if n, _ := fmt.Sscanf(s.subject, "agent.complete.%s", &childLoopID); n == 1 {
				return childLoopID
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("timed out waiting for agent.complete subscription to extract child loop ID")
	return ""
}

// TestExecutor_WithDefaultModel_UsedWhenNoModelArg verifies that
// WithDefaultModel supplies the model when the caller omits "model".
func TestExecutor_WithDefaultModel_UsedWhenNoModelArg(t *testing.T) {
	t.Parallel()

	mockNATS := newMockNATSClient()
	e := spawn.NewExecutor(mockNATS, &mockGraphHelper{},
		spawn.WithDefaultModel("my-default-model"),
	)

	resultCh := make(chan agentic.ToolResult, 1)
	go func() {
		result, _ := e.Execute(context.Background(), agentic.ToolCall{
			ID:     "call-dm",
			Name:   "spawn_agent",
			LoopID: "parent-loop",
			Arguments: map[string]any{
				"prompt":  "task without model arg",
				"role":    "developer",
				"timeout": "500ms",
				// no "model" key
			},
		})
		resultCh <- result
	}()

	childLoopID := extractChildLoopID(t, mockNATS)
	sub := waitForSubscription(t, mockNATS, fmt.Sprintf("agent.complete.%s", childLoopID))
	sub.fire(buildCompletedPayload(t, childLoopID, "dm-task", "ok"))
	<-resultCh

	mockNATS.mu.Lock()
	published := mockNATS.published
	mockNATS.mu.Unlock()

	if len(published) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(published))
	}

	// Decode the BaseMessage envelope to inspect the TaskMessage payload.
	var env struct {
		Payload agentic.TaskMessage `json:"payload"`
	}
	if err := json.Unmarshal(published[0].data, &env); err != nil {
		t.Fatalf("unmarshal published message: %v", err)
	}
	if env.Payload.Model != "my-default-model" {
		t.Errorf("TaskMessage.Model = %q, want %q", env.Payload.Model, "my-default-model")
	}
}

// TestExecutor_WithMaxDepth_LimitEnforced verifies that WithMaxDepth(n) is
// respected: currentDepth+1 >= n is rejected.
func TestExecutor_WithMaxDepth_LimitEnforced(t *testing.T) {
	t.Parallel()

	// Set maxDepth=3. A call with currentDepth=2 should be rejected (2+1 == 3).
	e := spawn.NewExecutor(newMockNATSClient(), &mockGraphHelper{},
		spawn.WithDefaultModel("m"),
		spawn.WithMaxDepth(3),
	)

	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "call-d",
		Name:   "spawn_agent",
		LoopID: "parent-loop",
		Arguments: map[string]any{
			"prompt": "deep task",
			"role":   "executor",
			"depth":  float64(2), // 2+1 = 3 == maxDepth
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !strings.Contains(result.Error, "depth limit reached") {
		t.Errorf("ToolResult.Error = %q, want it to contain 'depth limit reached'", result.Error)
	}
	if !strings.Contains(result.Error, "max depth 3") {
		t.Errorf("ToolResult.Error = %q, want it to mention 'max depth 3'", result.Error)
	}
}

// TestExecutor_ParseArguments_TimeoutParsing verifies that a valid Go duration
// string ("30s") is accepted and an invalid one returns a ToolResult error.
func TestExecutor_ParseArguments_TimeoutParsing(t *testing.T) {
	t.Parallel()

	t.Run("valid timeout accepted", func(t *testing.T) {
		t.Parallel()

		mockNATS := newMockNATSClient()
		e := spawn.NewExecutor(mockNATS, &mockGraphHelper{},
			spawn.WithDefaultModel("m"),
		)

		resultCh := make(chan agentic.ToolResult, 1)
		go func() {
			r, _ := e.Execute(context.Background(), agentic.ToolCall{
				ID:     "c",
				Name:   "spawn_agent",
				LoopID: "p",
				Arguments: map[string]any{
					"prompt":  "task",
					"role":    "executor",
					"timeout": "45s",
				},
			})
			resultCh <- r
		}()

		childLoopID := extractChildLoopID(t, mockNATS)
		waitForSubscription(t, mockNATS, fmt.Sprintf("agent.complete.%s", childLoopID)).
			fire(buildCompletedPayload(t, childLoopID, "t", "done"))

		result := <-resultCh
		if result.Error != "" {
			t.Errorf("unexpected ToolResult.Error = %q for valid timeout", result.Error)
		}
	})

	t.Run("invalid timeout returns ToolResult error", func(t *testing.T) {
		t.Parallel()

		e := spawn.NewExecutor(newMockNATSClient(), &mockGraphHelper{},
			spawn.WithDefaultModel("m"),
		)
		result, err := e.Execute(context.Background(), agentic.ToolCall{
			ID:     "c",
			Name:   "spawn_agent",
			LoopID: "p",
			Arguments: map[string]any{
				"prompt":  "task",
				"role":    "executor",
				"timeout": "not-a-duration",
			},
		})
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		if !strings.Contains(result.Error, "invalid timeout") {
			t.Errorf("ToolResult.Error = %q, want it to mention 'invalid timeout'", result.Error)
		}
	})
}

// TestExecutor_ParseArguments_ExtraArgsIgnored verifies that unknown or
// unsupported arguments (like "tools") in the call do not cause errors.
// In this semstreams version, TaskMessage has no Tools field so the argument
// is accepted but silently dropped.
func TestExecutor_ParseArguments_ExtraArgsIgnored(t *testing.T) {
	t.Parallel()

	mockNATS := newMockNATSClient()
	e := spawn.NewExecutor(mockNATS, &mockGraphHelper{},
		spawn.WithDefaultModel("m"),
	)

	toolsDef := []any{
		map[string]any{
			"name":        "file_read",
			"description": "read a file",
			"parameters":  map[string]any{"type": "object"},
		},
	}

	resultCh := make(chan agentic.ToolResult, 1)
	go func() {
		r, _ := e.Execute(context.Background(), agentic.ToolCall{
			ID:     "c",
			Name:   "spawn_agent",
			LoopID: "p",
			Arguments: map[string]any{
				"prompt":  "use tools",
				"role":    "executor",
				"timeout": "500ms",
				"tools":   toolsDef, // accepted but not forwarded to TaskMessage
			},
		})
		resultCh <- r
	}()

	childLoopID := extractChildLoopID(t, mockNATS)
	waitForSubscription(t, mockNATS, fmt.Sprintf("agent.complete.%s", childLoopID)).
		fire(buildCompletedPayload(t, childLoopID, "t", "ok"))

	result := <-resultCh
	if result.Error != "" {
		t.Errorf("unexpected ToolResult.Error = %q; extra args must not cause failure", result.Error)
	}
	if result.Content != "ok" {
		t.Errorf("Content = %q, want %q", result.Content, "ok")
	}
}

// TestExecutor_DepthCoercion_IntDepth verifies that a Go int (not float64)
// stored in Arguments["depth"] is handled correctly. JSON unmarshals numbers to
// float64, but direct map construction can produce int.
func TestExecutor_DepthCoercion_IntDepth(t *testing.T) {
	t.Parallel()

	e := spawn.NewExecutor(newMockNATSClient(), &mockGraphHelper{},
		spawn.WithDefaultModel("m"),
		spawn.WithMaxDepth(5),
	)

	// currentDepth=4 as int (not float64); 4+1==5 == maxDepth → reject.
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c",
		Name:   "spawn_agent",
		LoopID: "p",
		Arguments: map[string]any{
			"prompt": "deep",
			"role":   "executor",
			"depth":  4, // int, not float64
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !strings.Contains(result.Error, "depth limit reached") {
		t.Errorf("ToolResult.Error = %q, want 'depth limit reached'", result.Error)
	}
}

// TestExecutor_SecondSubscribeFailure verifies that a failure on the second
// Subscribe call (agent.failed.*) also returns a Go error.
func TestExecutor_SecondSubscribeFailure_ReturnsGoError(t *testing.T) {
	t.Parallel()

	// Inject a failure only on the second Subscribe call.
	callCount := 0
	var mu sync.Mutex

	// We need a custom mock that errors only on call #2.
	type perCallMock struct {
		mockNATSClient
	}
	m := &perCallMock{}
	m.mockNATSClient = *newMockNATSClient()

	// Shadow the Subscribe method to fail on the second call.
	// Since Go doesn't support method overriding on struct embedding for
	// interface satisfaction, we implement a thin wrapper that satisfies
	// spawn.NATSClient directly.
	client := &secondSubFailClient{
		base:    newMockNATSClient(),
		mu:      &mu,
		count:   &callCount,
		failOn:  2,
		failErr: errors.New("nats: no route"),
	}
	_ = m // not used

	e := spawn.NewExecutor(client, &mockGraphHelper{},
		spawn.WithDefaultModel("m"),
	)
	_, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c",
		Name:   "spawn_agent",
		LoopID: "p",
		Arguments: map[string]any{
			"prompt": "task",
			"role":   "executor",
		},
	})
	if err == nil {
		t.Fatal("expected a Go error from Execute, got nil")
	}
	if !strings.Contains(err.Error(), "subscribe to failure subject") {
		t.Errorf("error = %q, want it to mention 'subscribe to failure subject'", err.Error())
	}
}

// secondSubFailClient is a NATSClient that fails only on the Nth Subscribe.
type secondSubFailClient struct {
	base    *mockNATSClient
	mu      *sync.Mutex
	count   *int
	failOn  int
	failErr error
}

func (c *secondSubFailClient) PublishToStream(ctx context.Context, subject string, data []byte) error {
	return c.base.PublishToStream(ctx, subject, data)
}

func (c *secondSubFailClient) Subscribe(
	ctx context.Context,
	subject string,
	handler func([]byte),
) (spawn.Subscription, error) {
	c.mu.Lock()
	*c.count++
	n := *c.count
	c.mu.Unlock()
	if n == c.failOn {
		return nil, c.failErr
	}
	return c.base.Subscribe(ctx, subject, handler)
}

// TestExecutor_MalformedEvent_ReturnsError verifies that a malformed
// completion event is propagated as a ToolResult error. The executor sends
// a diagnostic error rather than waiting for the timeout, so the result
// arrives quickly with an error mentioning the parse failure.
func TestExecutor_MalformedEvent_ReturnsError(t *testing.T) {
	t.Parallel()

	mockNATS := newMockNATSClient()
	e := spawn.NewExecutor(mockNATS, &mockGraphHelper{},
		spawn.WithDefaultModel("m"),
	)

	resultCh := make(chan agentic.ToolResult, 1)
	go func() {
		r, _ := e.Execute(context.Background(), agentic.ToolCall{
			ID:     "c",
			Name:   "spawn_agent",
			LoopID: "p",
			Arguments: map[string]any{
				"prompt":  "task",
				"role":    "executor",
				"timeout": "5s", // long timeout — result must arrive before it fires
			},
		})
		resultCh <- r
	}()

	childLoopID := extractChildLoopID(t, mockNATS)
	completeSub := waitForSubscription(t, mockNATS, fmt.Sprintf("agent.complete.%s", childLoopID))

	// Send garbage bytes — the handler propagates a diagnostic error.
	completeSub.fire([]byte(`not valid json at all`))

	result := <-resultCh
	if !strings.Contains(result.Error, "malformed completion event") {
		t.Errorf("ToolResult.Error = %q, want it to contain 'malformed completion event'", result.Error)
	}
}

// TestExecutor_DuplicateCompletionEvent_FirstWins verifies that when two
// completion events arrive, the first is delivered and the second is silently
// discarded by the non-blocking send in the handler.
func TestExecutor_DuplicateCompletionEvent_FirstWins(t *testing.T) {
	t.Parallel()

	mockNATS := newMockNATSClient()
	e := spawn.NewExecutor(mockNATS, &mockGraphHelper{},
		spawn.WithDefaultModel("m"),
	)

	resultCh := make(chan agentic.ToolResult, 1)
	go func() {
		r, _ := e.Execute(context.Background(), agentic.ToolCall{
			ID:     "c",
			Name:   "spawn_agent",
			LoopID: "p",
			Arguments: map[string]any{
				"prompt":  "task",
				"role":    "executor",
				"timeout": "500ms",
			},
		})
		resultCh <- r
	}()

	childLoopID := extractChildLoopID(t, mockNATS)
	completeSub := waitForSubscription(t, mockNATS, fmt.Sprintf("agent.complete.%s", childLoopID))

	completeSub.fire(buildCompletedPayload(t, childLoopID, "t", "first"))
	completeSub.fire(buildCompletedPayload(t, childLoopID, "t", "second")) // must be dropped

	result := <-resultCh
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Content != "first" {
		t.Errorf("Content = %q, want %q (first event must win)", result.Content, "first")
	}
}

// TestExecutor_GraphWarningInMetadata verifies that a graph failure injects a
// non-empty "warning" key into the returned Metadata.
func TestExecutor_GraphWarningInMetadata(t *testing.T) {
	t.Parallel()

	mockNATS := newMockNATSClient()
	e := spawn.NewExecutor(mockNATS, &mockGraphHelper{spawnErr: errors.New("graph down")},
		spawn.WithDefaultModel("m"),
	)

	resultCh := make(chan agentic.ToolResult, 1)
	go func() {
		r, _ := e.Execute(context.Background(), agentic.ToolCall{
			ID:     "c",
			Name:   "spawn_agent",
			LoopID: "p",
			Arguments: map[string]any{
				"prompt":  "task",
				"role":    "executor",
				"timeout": "500ms",
			},
		})
		resultCh <- r
	}()

	childLoopID := extractChildLoopID(t, mockNATS)
	waitForSubscription(t, mockNATS, fmt.Sprintf("agent.complete.%s", childLoopID)).
		fire(buildCompletedPayload(t, childLoopID, "t", "result"))

	result := <-resultCh
	if result.Error != "" {
		t.Fatalf("ToolResult.Error = %q, want empty (graph error is non-fatal)", result.Error)
	}
	if result.Metadata == nil {
		t.Fatal("ToolResult.Metadata is nil")
	}
	warn, ok := result.Metadata["warning"]
	if !ok {
		t.Error("Metadata missing 'warning' key")
	}
	if warnStr, _ := warn.(string); !strings.Contains(warnStr, "graph recording failed") {
		t.Errorf("Metadata[warning] = %q, want it to contain 'graph recording failed'", warnStr)
	}
}

// TestExecutor_PublishSubjectFormat verifies that the TaskMessage is published
// to the correct subject prefix.
func TestExecutor_PublishSubjectFormat(t *testing.T) {
	t.Parallel()

	mockNATS := newMockNATSClient()
	mockGraph := &mockGraphHelper{}

	e := spawn.NewExecutor(mockNATS, mockGraph,
		spawn.WithDefaultModel("gpt-4o"),
	)

	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = e.Execute(ctx, agentic.ToolCall{
			ID:     "call-pub",
			Name:   "spawn_agent",
			LoopID: "parent-loop",
			Arguments: map[string]any{
				"prompt":  "check subject",
				"role":    "executor",
				"timeout": "50ms",
			},
		})
	}()

	wg.Wait()

	mockNATS.mu.Lock()
	published := mockNATS.published
	mockNATS.mu.Unlock()

	if len(published) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(published))
	}
	subject := published[0].subject
	var taskID string
	if n, _ := fmt.Sscanf(subject, "agent.task.%s", &taskID); n != 1 || taskID == "" {
		t.Errorf("published subject %q does not match agent.task.<taskID>", subject)
	}
}

// TestExecutor_ChildMetadataInResult verifies that the successful ToolResult
// includes the child_loop_id and task_id in its metadata.
func TestExecutor_ChildMetadataInResult(t *testing.T) {
	t.Parallel()

	mockNATS := newMockNATSClient()
	mockGraph := &mockGraphHelper{}
	e := spawn.NewExecutor(mockNATS, mockGraph,
		spawn.WithDefaultModel("gpt-4o"),
	)

	ctx := context.Background()

	resultCh := make(chan agentic.ToolResult, 1)
	go func() {
		result, _ := e.Execute(ctx, agentic.ToolCall{
			ID:     "call-meta",
			Name:   "spawn_agent",
			LoopID: "parent-loop",
			Arguments: map[string]any{
				"prompt":  "check metadata",
				"role":    "developer",
				"timeout": "500ms",
			},
		})
		resultCh <- result
	}()

	childLoopID := extractChildLoopID(t, mockNATS)
	sub := waitForSubscription(t, mockNATS, fmt.Sprintf("agent.complete.%s", childLoopID))
	sub.fire(buildCompletedPayload(t, childLoopID, "meta-task", "meta result"))

	result := <-resultCh
	if result.Error != "" {
		t.Fatalf("unexpected error in result: %s", result.Error)
	}

	if result.Metadata == nil {
		t.Fatal("ToolResult.Metadata is nil, expected child_loop_id and task_id")
	}
	if _, ok := result.Metadata["child_loop_id"]; !ok {
		t.Error("ToolResult.Metadata missing 'child_loop_id'")
	}
	if _, ok := result.Metadata["task_id"]; !ok {
		t.Error("ToolResult.Metadata missing 'task_id'")
	}
	if result.Metadata["child_loop_id"] != childLoopID {
		t.Errorf("child_loop_id = %v, want %q", result.Metadata["child_loop_id"], childLoopID)
	}
}
