package llm

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
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

// ============================================================================
// Integration Tests (require Docker)
// ============================================================================

func TestToolCallStore_StoreAndGet(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewToolCallStore(ctx, tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	now := time.Now()
	record := &ToolCallRecord{
		CallID:      "call-store-get-123",
		TraceID:     "trace-store-get-456",
		LoopID:      "loop-store-get-789",
		ToolName:    "file_read",
		Parameters:  `{"path": "/src/main.go"}`,
		Result:      "package main",
		Status:      "success",
		StartedAt:   now,
		CompletedAt: now.Add(10 * time.Millisecond),
		DurationMs:  10,
	}

	// Store the record
	if err := store.Store(ctx, record); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	// Retrieve by key
	key := "trace-store-get-456.call-store-get-123"
	retrieved, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if retrieved.CallID != record.CallID {
		t.Errorf("CallID = %q, want %q", retrieved.CallID, record.CallID)
	}
	if retrieved.TraceID != record.TraceID {
		t.Errorf("TraceID = %q, want %q", retrieved.TraceID, record.TraceID)
	}
	if retrieved.LoopID != record.LoopID {
		t.Errorf("LoopID = %q, want %q", retrieved.LoopID, record.LoopID)
	}
	if retrieved.ToolName != record.ToolName {
		t.Errorf("ToolName = %q, want %q", retrieved.ToolName, record.ToolName)
	}
	if retrieved.Status != record.Status {
		t.Errorf("Status = %q, want %q", retrieved.Status, record.Status)
	}
}

func TestToolCallStore_StoreRequiresCallID(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewToolCallStore(ctx, tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	record := &ToolCallRecord{
		CallID:  "", // Empty - should fail
		TraceID: "trace-123",
	}

	err = store.Store(ctx, record)
	if err == nil {
		t.Error("Store() should return error when CallID is empty")
	}
}

func TestToolCallStore_GetByTraceID(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewToolCallStore(ctx, tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	traceID := "trace-toolcall-bytrace-123"
	now := time.Now()

	records := []*ToolCallRecord{
		{CallID: "call-1", TraceID: traceID, ToolName: "file_read", Status: "success", StartedAt: now},
		{CallID: "call-2", TraceID: traceID, ToolName: "git_status", Status: "success", StartedAt: now.Add(time.Second)},
		{CallID: "call-3", TraceID: traceID, ToolName: "file_write", Status: "success", StartedAt: now.Add(2 * time.Second)},
		{CallID: "call-other", TraceID: "other-trace", ToolName: "file_list", Status: "success", StartedAt: now},
	}

	for _, r := range records {
		if err := store.Store(ctx, r); err != nil {
			t.Fatalf("Store() error = %v", err)
		}
	}

	// Retrieve by trace ID
	retrieved, err := store.GetByTraceID(ctx, traceID)
	if err != nil {
		t.Fatalf("GetByTraceID() error = %v", err)
	}

	if len(retrieved) != 3 {
		t.Errorf("GetByTraceID() returned %d records, want 3", len(retrieved))
	}

	// Should be sorted by StartedAt (chronological)
	if len(retrieved) == 3 {
		if retrieved[0].CallID != "call-1" {
			t.Errorf("First record = %q, want %q", retrieved[0].CallID, "call-1")
		}
		if retrieved[2].CallID != "call-3" {
			t.Errorf("Last record = %q, want %q", retrieved[2].CallID, "call-3")
		}
	}
}

func TestToolCallStore_GetByTraceID_Empty(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewToolCallStore(ctx, tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	_, err = store.GetByTraceID(ctx, "")
	if err == nil {
		t.Error("GetByTraceID() should return error for empty trace_id")
	}
}

func TestToolCallStore_GetByTraceID_NotFound(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewToolCallStore(ctx, tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	records, err := store.GetByTraceID(ctx, "nonexistent-trace-tool")
	if err != nil {
		t.Fatalf("GetByTraceID() error = %v", err)
	}

	if len(records) != 0 {
		t.Errorf("GetByTraceID() returned %d records, want 0", len(records))
	}
}

func TestToolCallStore_GetByLoopID(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewToolCallStore(ctx, tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	loopID := "loop-toolcall-byloop-123"
	now := time.Now()

	records := []*ToolCallRecord{
		{CallID: "call-loop-1", TraceID: "trace-1", LoopID: loopID, ToolName: "file_read", Status: "success", StartedAt: now},
		{CallID: "call-loop-2", TraceID: "trace-2", LoopID: loopID, ToolName: "git_status", Status: "success", StartedAt: now.Add(time.Second)},
		{CallID: "call-other-loop", TraceID: "trace-3", LoopID: "other-loop", ToolName: "file_list", Status: "success", StartedAt: now},
	}

	for _, r := range records {
		if err := store.Store(ctx, r); err != nil {
			t.Fatalf("Store() error = %v", err)
		}
	}

	retrieved, err := store.GetByLoopID(ctx, loopID)
	if err != nil {
		t.Fatalf("GetByLoopID() error = %v", err)
	}

	if len(retrieved) != 2 {
		t.Errorf("GetByLoopID() returned %d records, want 2", len(retrieved))
	}
}

func TestToolCallStore_GetByLoopID_Empty(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewToolCallStore(ctx, tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	_, err = store.GetByLoopID(ctx, "")
	if err == nil {
		t.Error("GetByLoopID() should return error for empty loop_id")
	}
}

func TestToolCallStore_Delete(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewToolCallStore(ctx, tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	record := &ToolCallRecord{
		CallID:   "call-delete-123",
		TraceID:  "trace-delete-456",
		ToolName: "file_read",
		Status:   "success",
	}

	if err := store.Store(ctx, record); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	key := "trace-delete-456.call-delete-123"
	_, err = store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() before delete error = %v", err)
	}

	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	_, err = store.Get(ctx, key)
	if err == nil {
		t.Error("Get() after delete should return error")
	}
}

func TestToolCallStore_WithCustomTTL(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	customTTL := 1 * time.Hour
	store, err := NewToolCallStore(ctx, tc.Client, WithToolCallsTTL(customTTL))
	if err != nil {
		t.Fatalf("Failed to create store with custom TTL: %v", err)
	}

	if store.ttl != customTTL {
		t.Errorf("store.ttl = %v, want %v", store.ttl, customTTL)
	}
}

func TestNewToolCallStore_NilClient(t *testing.T) {
	ctx := context.Background()

	_, err := NewToolCallStore(ctx, nil)
	if err == nil {
		t.Error("NewToolCallStore() should return error when client is nil")
	}
}

func TestToolCallStore_ConcurrentAccess(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewToolCallStore(ctx, tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	traceID := "trace-concurrent-tool"
	now := time.Now()

	const numWriters = 10
	const numReaders = 5

	errCh := make(chan error, numWriters+numReaders)

	var wg sync.WaitGroup
	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			record := &ToolCallRecord{
				CallID:    fmt.Sprintf("call-concurrent-%d", idx),
				TraceID:   traceID,
				ToolName:  "file_read",
				Status:    "success",
				StartedAt: now.Add(time.Duration(idx) * time.Millisecond),
			}
			if err := store.Store(ctx, record); err != nil {
				errCh <- fmt.Errorf("Store(%d): %w", idx, err)
			}
		}(i)
	}
	wg.Wait()

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			records, err := store.GetByTraceID(ctx, traceID)
			if err != nil {
				errCh <- fmt.Errorf("GetByTraceID(%d): %w", idx, err)
				return
			}
			if len(records) == 0 {
				errCh <- fmt.Errorf("GetByTraceID(%d): returned empty", idx)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		for _, err := range errs {
			t.Errorf("Concurrent access error: %v", err)
		}
	}
}
