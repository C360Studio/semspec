package tools

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// mockExecutor is a simple mock for testing the RecordingExecutor wrapper.
type mockExecutor struct {
	executeFunc func(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error)
	tools       []agentic.ToolDefinition
}

func (m *mockExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, call)
	}
	return agentic.ToolResult{CallID: call.ID, Content: "ok"}, nil
}

func (m *mockExecutor) ListTools() []agentic.ToolDefinition {
	return m.tools
}

// Verify RecordingExecutor implements ToolExecutor
var _ agentictools.ToolExecutor = (*RecordingExecutor)(nil)

func TestRecordingExecutor_PassesThrough(t *testing.T) {
	inner := &mockExecutor{
		executeFunc: func(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
			return agentic.ToolResult{
				CallID:  call.ID,
				Content: "result content",
			}, nil
		},
		tools: []agentic.ToolDefinition{
			{Name: "test_tool", Description: "test", Parameters: map[string]any{"type": "object"}},
		},
	}

	recorder := NewRecordingExecutor(inner)

	// Execute should pass through to inner
	call := agentic.ToolCall{
		ID:   "call-123",
		Name: "test_tool",
	}
	result, err := recorder.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.CallID != "call-123" {
		t.Errorf("CallID = %q, want %q", result.CallID, "call-123")
	}
	if result.Content != "result content" {
		t.Errorf("Content = %q, want %q", result.Content, "result content")
	}

	// ListTools should delegate
	tools := recorder.ListTools()
	if len(tools) != 1 {
		t.Errorf("ListTools() returned %d tools, want 1", len(tools))
	}
	if tools[0].Name != "test_tool" {
		t.Errorf("Tool name = %q, want %q", tools[0].Name, "test_tool")
	}
}

func TestRecordingExecutor_ErrorPassesThrough(t *testing.T) {
	inner := &mockExecutor{
		executeFunc: func(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
			return agentic.ToolResult{
				CallID: call.ID,
				Error:  "tool error",
			}, fmt.Errorf("execution failed")
		},
	}

	recorder := NewRecordingExecutor(inner)

	call := agentic.ToolCall{
		ID:   "call-err",
		Name: "failing_tool",
	}
	result, err := recorder.Execute(context.Background(), call)

	// Error should pass through
	if err == nil {
		t.Error("Execute() should return error")
	}
	if result.Error != "tool error" {
		t.Errorf("Result.Error = %q, want %q", result.Error, "tool error")
	}
}

func TestRecordingExecutor_GracefulWithoutStore(t *testing.T) {
	// When GlobalToolCallStore() returns nil, recording should be silently skipped.
	inner := &mockExecutor{
		executeFunc: func(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
			return agentic.ToolResult{
				CallID:  call.ID,
				Content: "ok",
			}, nil
		},
	}

	recorder := NewRecordingExecutor(inner)

	call := agentic.ToolCall{
		ID:        "call-no-store",
		Name:      "test_tool",
		Arguments: map[string]any{"path": "/test"},
	}

	// Should not panic even though GlobalToolCallStore() is nil
	result, err := recorder.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Content != "ok" {
		t.Errorf("Content = %q, want %q", result.Content, "ok")
	}

	// Give goroutine time to complete (it should exit gracefully)
	time.Sleep(50 * time.Millisecond)
}

func TestRecordingExecutor_DoesNotSlowExecution(t *testing.T) {
	callDuration := 10 * time.Millisecond
	inner := &mockExecutor{
		executeFunc: func(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
			time.Sleep(callDuration)
			return agentic.ToolResult{CallID: call.ID, Content: "done"}, nil
		},
	}

	recorder := NewRecordingExecutor(inner)

	call := agentic.ToolCall{ID: "call-timing", Name: "slow_tool"}

	start := time.Now()
	_, err := recorder.Execute(context.Background(), call)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// The recording is async (goroutine), so execution time should be
	// close to the inner execution time, not significantly longer.
	maxExpected := callDuration + 50*time.Millisecond
	if elapsed > maxExpected {
		t.Errorf("Execute() took %v, want < %v (recording should be async)", elapsed, maxExpected)
	}
}

func TestTruncateJSON(t *testing.T) {
	tests := []struct {
		name   string
		input  map[string]any
		maxLen int
		want   string
	}{
		{
			name:   "nil map",
			input:  nil,
			maxLen: 100,
			want:   "{}",
		},
		{
			name:   "small map",
			input:  map[string]any{"key": "value"},
			maxLen: 100,
			want:   `{"key":"value"}`,
		},
		{
			name:   "truncated",
			input:  map[string]any{"key": "a very long value that should be truncated"},
			maxLen: 20,
			want:   `{"key":"a very long ...`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateJSON(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateJSON() = %q, want %q", got, tt.want)
			}
		})
	}
}
