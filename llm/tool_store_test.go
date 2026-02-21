package llm

import (
	"context"
	"testing"
	"time"
)

func TestToolCallRecord_KeyFormat(t *testing.T) {
	record := &ToolCallRecord{
		CallID:   "call-123",
		TraceID:  "trace-456",
		ToolName: "file_read",
	}

	// Verify the key format would be trace_id.call_id (dot separator for NATS KV compatibility)
	expectedKey := "trace-456.call-123"

	key := record.CallID
	if record.TraceID != "" {
		key = record.TraceID + "." + record.CallID
	}

	if key != expectedKey {
		t.Errorf("expected key %q, got %q", expectedKey, key)
	}
}

func TestToolCallRecord_KeyFormatWithoutTrace(t *testing.T) {
	record := &ToolCallRecord{
		CallID:   "call-123",
		TraceID:  "", // No trace ID
		ToolName: "file_read",
	}

	key := record.CallID
	if record.TraceID != "" {
		key = record.TraceID + "." + record.CallID
	}

	if key != "call-123" {
		t.Errorf("expected key %q, got %q", "call-123", key)
	}
}

func TestSortToolCallsByStartTime(t *testing.T) {
	now := time.Now()
	records := []*ToolCallRecord{
		{CallID: "third", StartedAt: now.Add(2 * time.Second)},
		{CallID: "first", StartedAt: now},
		{CallID: "second", StartedAt: now.Add(1 * time.Second)},
	}

	SortToolCallsByStartTime(records)

	if records[0].CallID != "first" {
		t.Errorf("expected first record to be 'first', got %q", records[0].CallID)
	}
	if records[1].CallID != "second" {
		t.Errorf("expected second record to be 'second', got %q", records[1].CallID)
	}
	if records[2].CallID != "third" {
		t.Errorf("expected third record to be 'third', got %q", records[2].CallID)
	}
}

func TestSortToolCallsByStartTime_Empty(t *testing.T) {
	records := []*ToolCallRecord{}
	SortToolCallsByStartTime(records)
	if len(records) != 0 {
		t.Errorf("expected empty records after sort, got %d", len(records))
	}
}

func TestSortToolCallsByStartTime_Single(t *testing.T) {
	records := []*ToolCallRecord{
		{CallID: "only", StartedAt: time.Now()},
	}
	SortToolCallsByStartTime(records)

	if records[0].CallID != "only" {
		t.Errorf("expected 'only', got %q", records[0].CallID)
	}
}

func TestToolCallRecord_Fields(t *testing.T) {
	now := time.Now()
	record := &ToolCallRecord{
		CallID:      "call-123",
		TraceID:     "trace-456",
		LoopID:      "loop-789",
		ToolName:    "file_read",
		Parameters:  `{"path": "/src/main.go"}`,
		Result:      "package main\n...",
		Status:      "success",
		StartedAt:   now,
		CompletedAt: now.Add(50 * time.Millisecond),
		DurationMs:  50,
	}

	if record.CallID != "call-123" {
		t.Errorf("CallID mismatch")
	}
	if record.TraceID != "trace-456" {
		t.Errorf("TraceID mismatch")
	}
	if record.LoopID != "loop-789" {
		t.Errorf("LoopID mismatch")
	}
	if record.ToolName != "file_read" {
		t.Errorf("ToolName mismatch")
	}
	if record.Parameters != `{"path": "/src/main.go"}` {
		t.Errorf("Parameters mismatch")
	}
	if record.Result != "package main\n..." {
		t.Errorf("Result mismatch")
	}
	if record.Status != "success" {
		t.Errorf("Status mismatch")
	}
	if record.DurationMs != 50 {
		t.Errorf("DurationMs mismatch")
	}
}

func TestToolCallRecord_ErrorField(t *testing.T) {
	record := &ToolCallRecord{
		CallID:   "call-123",
		ToolName: "git_status",
		Status:   "error",
		Error:    "not a git repository",
	}

	if record.Error != "not a git repository" {
		t.Errorf("expected error 'not a git repository', got %q", record.Error)
	}
}

func TestToolCallRecord_DurationCalculation(t *testing.T) {
	start := time.Now()
	time.Sleep(10 * time.Millisecond)
	end := time.Now()

	record := &ToolCallRecord{
		CallID:      "call-123",
		ToolName:    "file_read",
		Status:      "success",
		StartedAt:   start,
		CompletedAt: end,
		DurationMs:  end.Sub(start).Milliseconds(),
	}

	if record.DurationMs < 10 {
		t.Errorf("expected DurationMs >= 10, got %d", record.DurationMs)
	}
}

func TestNewToolCallStore_NilClient(t *testing.T) {
	ctx := context.Background()

	_, err := NewToolCallStore(ctx, nil)
	if err == nil {
		t.Error("NewToolCallStore() should return error when client is nil")
	}
}
