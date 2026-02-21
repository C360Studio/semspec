package llm

import (
	"context"
	"testing"
	"time"
)

func TestCallRecord_KeyFormat(t *testing.T) {
	record := &CallRecord{
		RequestID:  "req-123",
		TraceID:    "trace-456",
		Capability: "planning",
		Model:      "test-model",
		Provider:   "test-provider",
		StartedAt:  time.Now(),
	}

	// Verify the key format would be trace_id.request_id (dot separator for NATS KV compatibility)
	expectedKey := "trace-456.req-123"

	// Build key manually to verify
	key := record.RequestID
	if record.TraceID != "" {
		key = record.TraceID + "." + record.RequestID
	}

	if key != expectedKey {
		t.Errorf("expected key %q, got %q", expectedKey, key)
	}
}

func TestCallRecord_KeyFormatWithoutTrace(t *testing.T) {
	record := &CallRecord{
		RequestID:  "req-123",
		TraceID:    "", // No trace ID
		Capability: "planning",
		Model:      "test-model",
		Provider:   "test-provider",
		StartedAt:  time.Now(),
	}

	// When no trace ID, key should just be request_id
	key := record.RequestID
	if record.TraceID != "" {
		key = record.TraceID + "." + record.RequestID
	}

	if key != "req-123" {
		t.Errorf("expected key %q, got %q", "req-123", key)
	}
}

func TestSortByStartTime(t *testing.T) {
	now := time.Now()
	records := []*CallRecord{
		{RequestID: "third", StartedAt: now.Add(2 * time.Second)},
		{RequestID: "first", StartedAt: now},
		{RequestID: "second", StartedAt: now.Add(1 * time.Second)},
	}

	SortByStartTime(records)

	if records[0].RequestID != "first" {
		t.Errorf("expected first record to be 'first', got %q", records[0].RequestID)
	}
	if records[1].RequestID != "second" {
		t.Errorf("expected second record to be 'second', got %q", records[1].RequestID)
	}
	if records[2].RequestID != "third" {
		t.Errorf("expected third record to be 'third', got %q", records[2].RequestID)
	}
}

func TestSortByStartTime_Empty(t *testing.T) {
	records := []*CallRecord{}
	SortByStartTime(records)
	if len(records) != 0 {
		t.Errorf("expected empty records after sort, got %d", len(records))
	}
}

func TestSortByStartTime_Single(t *testing.T) {
	records := []*CallRecord{
		{RequestID: "only", StartedAt: time.Now()},
	}
	SortByStartTime(records)

	if records[0].RequestID != "only" {
		t.Errorf("expected 'only', got %q", records[0].RequestID)
	}
}

func TestTraceContext(t *testing.T) {
	tc := TraceContext{
		TraceID: "trace-123",
		LoopID:  "loop-456",
	}

	ctx := WithTraceContext(context.Background(), tc)

	extracted := GetTraceContext(ctx)

	if extracted.TraceID != "trace-123" {
		t.Errorf("expected TraceID 'trace-123', got %q", extracted.TraceID)
	}
	if extracted.LoopID != "loop-456" {
		t.Errorf("expected LoopID 'loop-456', got %q", extracted.LoopID)
	}
}

func TestTraceContext_NotSet(t *testing.T) {
	ctx := context.Background()

	extracted := GetTraceContext(ctx)

	if extracted.TraceID != "" {
		t.Errorf("expected empty TraceID, got %q", extracted.TraceID)
	}
	if extracted.LoopID != "" {
		t.Errorf("expected empty LoopID, got %q", extracted.LoopID)
	}
}

func TestCallRecord_DurationCalculation(t *testing.T) {
	start := time.Now()
	time.Sleep(10 * time.Millisecond)
	end := time.Now()

	record := &CallRecord{
		RequestID:   "req-123",
		Capability:  "planning",
		Model:       "test-model",
		Provider:    "test-provider",
		StartedAt:   start,
		CompletedAt: end,
		DurationMs:  end.Sub(start).Milliseconds(),
	}

	if record.DurationMs < 10 {
		t.Errorf("expected DurationMs >= 10, got %d", record.DurationMs)
	}
}

func TestCallRecord_Fields(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "You are a helpful assistant"},
		{Role: "user", Content: "Hello"},
	}

	now := time.Now()
	record := &CallRecord{
		RequestID:     "req-123",
		TraceID:       "trace-456",
		LoopID:        "loop-789",
		Capability:    "planning",
		Model:         "test-model",
		Provider:      "test-provider",
		Messages:      messages,
		Response:      "Hello! How can I help you?",
		TokensIn:      100,
		TokensOut:     50,
		FinishReason:  "stop",
		StartedAt:     now,
		CompletedAt:   now.Add(500 * time.Millisecond),
		DurationMs:    500,
		Retries:       2,
		FallbacksUsed: []string{"primary-model"},
	}

	// Verify all fields
	if record.RequestID != "req-123" {
		t.Errorf("RequestID mismatch")
	}
	if record.TraceID != "trace-456" {
		t.Errorf("TraceID mismatch")
	}
	if record.LoopID != "loop-789" {
		t.Errorf("LoopID mismatch")
	}
	if record.Capability != "planning" {
		t.Errorf("Capability mismatch")
	}
	if record.Model != "test-model" {
		t.Errorf("Model mismatch")
	}
	if record.Provider != "test-provider" {
		t.Errorf("Provider mismatch")
	}
	if len(record.Messages) != 2 {
		t.Errorf("Messages count mismatch")
	}
	if record.Response != "Hello! How can I help you?" {
		t.Errorf("Response mismatch")
	}
	if record.TokensIn != 100 {
		t.Errorf("TokensIn mismatch")
	}
	if record.TokensOut != 50 {
		t.Errorf("TokensOut mismatch")
	}
	if record.FinishReason != "stop" {
		t.Errorf("FinishReason mismatch")
	}
	if record.DurationMs != 500 {
		t.Errorf("DurationMs mismatch")
	}
	if record.Retries != 2 {
		t.Errorf("Retries mismatch")
	}
	if len(record.FallbacksUsed) != 1 {
		t.Errorf("FallbacksUsed count mismatch")
	}
}

func TestCallRecord_ErrorField(t *testing.T) {
	record := &CallRecord{
		RequestID:  "req-123",
		Capability: "planning",
		Model:      "test-model",
		Provider:   "test-provider",
		StartedAt:  time.Now(),
		Error:      "connection refused",
	}

	if record.Error != "connection refused" {
		t.Errorf("expected error 'connection refused', got %q", record.Error)
	}
}

func TestNewCallStore_NilClient(t *testing.T) {
	ctx := context.Background()

	_, err := NewCallStore(ctx, nil)
	if err == nil {
		t.Error("NewCallStore() should return error when client is nil")
	}
}

func TestGlobalCallStore_NilBeforeInit(t *testing.T) {
	// Reset global state for this test
	globalCallStoreMu.Lock()
	oldStore := globalCallStore
	globalCallStore = nil
	globalCallStoreMu.Unlock()

	defer func() {
		globalCallStoreMu.Lock()
		globalCallStore = oldStore
		globalCallStoreMu.Unlock()
	}()

	store := GlobalCallStore()
	if store != nil {
		t.Error("GlobalCallStore() should return nil before InitGlobalCallStore is called")
	}
}
