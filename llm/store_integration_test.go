//go:build integration

package llm

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
)

func TestCallStore_StoreAndGet(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewCallStore(ctx, tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	now := time.Now()
	record := &CallRecord{
		RequestID:        "req-store-get-123",
		TraceID:          "trace-store-get-456",
		LoopID:           "loop-store-get-789",
		Capability:       "planning",
		Model:            "test-model",
		Provider:         "test-provider",
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		StartedAt:        now,
	}

	// Store the record
	if err := store.Store(ctx, record); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	// Retrieve by key
	key := "trace-store-get-456.req-store-get-123"
	retrieved, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if retrieved.RequestID != record.RequestID {
		t.Errorf("RequestID = %q, want %q", retrieved.RequestID, record.RequestID)
	}
	if retrieved.TraceID != record.TraceID {
		t.Errorf("TraceID = %q, want %q", retrieved.TraceID, record.TraceID)
	}
	if retrieved.LoopID != record.LoopID {
		t.Errorf("LoopID = %q, want %q", retrieved.LoopID, record.LoopID)
	}
	if retrieved.PromptTokens != record.PromptTokens {
		t.Errorf("PromptTokens = %d, want %d", retrieved.PromptTokens, record.PromptTokens)
	}
	if retrieved.CompletionTokens != record.CompletionTokens {
		t.Errorf("CompletionTokens = %d, want %d", retrieved.CompletionTokens, record.CompletionTokens)
	}
	if retrieved.TotalTokens != record.TotalTokens {
		t.Errorf("TotalTokens = %d, want %d", retrieved.TotalTokens, record.TotalTokens)
	}
}

func TestCallStore_StoreRequiresRequestID(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewCallStore(ctx, tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	record := &CallRecord{
		RequestID: "", // Empty - should fail
		TraceID:   "trace-123",
	}

	err = store.Store(ctx, record)
	if err == nil {
		t.Error("Store() should return error when RequestID is empty")
	}
}

func TestCallStore_GetByTraceID(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewCallStore(ctx, tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	traceID := "trace-getbytraceid-123"
	now := time.Now()

	// Store multiple records with the same trace ID
	records := []*CallRecord{
		{RequestID: "req-1", TraceID: traceID, StartedAt: now},
		{RequestID: "req-2", TraceID: traceID, StartedAt: now.Add(time.Second)},
		{RequestID: "req-3", TraceID: traceID, StartedAt: now.Add(2 * time.Second)},
		{RequestID: "req-other", TraceID: "other-trace", StartedAt: now}, // Different trace
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
		if retrieved[0].RequestID != "req-1" {
			t.Errorf("First record = %q, want %q", retrieved[0].RequestID, "req-1")
		}
		if retrieved[2].RequestID != "req-3" {
			t.Errorf("Last record = %q, want %q", retrieved[2].RequestID, "req-3")
		}
	}
}

func TestCallStore_GetByTraceID_Empty(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewCallStore(ctx, tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	// Empty trace ID should return error
	_, err = store.GetByTraceID(ctx, "")
	if err == nil {
		t.Error("GetByTraceID() should return error for empty trace_id")
	}
}

func TestCallStore_GetByTraceID_NotFound(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewCallStore(ctx, tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	// Non-existent trace ID should return empty slice
	records, err := store.GetByTraceID(ctx, "nonexistent-trace")
	if err != nil {
		t.Fatalf("GetByTraceID() error = %v", err)
	}

	if len(records) != 0 {
		t.Errorf("GetByTraceID() returned %d records, want 0", len(records))
	}
}

func TestCallStore_GetByLoopID(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewCallStore(ctx, tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	loopID := "loop-getbyloopid-123"
	now := time.Now()

	// Store multiple records with different loop IDs
	records := []*CallRecord{
		{RequestID: "req-loop-1", TraceID: "trace-1", LoopID: loopID, StartedAt: now},
		{RequestID: "req-loop-2", TraceID: "trace-2", LoopID: loopID, StartedAt: now.Add(time.Second)},
		{RequestID: "req-other-loop", TraceID: "trace-3", LoopID: "other-loop", StartedAt: now},
	}

	for _, r := range records {
		if err := store.Store(ctx, r); err != nil {
			t.Fatalf("Store() error = %v", err)
		}
	}

	// Retrieve by loop ID
	retrieved, err := store.GetByLoopID(ctx, loopID)
	if err != nil {
		t.Fatalf("GetByLoopID() error = %v", err)
	}

	if len(retrieved) != 2 {
		t.Errorf("GetByLoopID() returned %d records, want 2", len(retrieved))
	}
}

func TestCallStore_GetByLoopID_Empty(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewCallStore(ctx, tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	// Empty loop ID should return error
	_, err = store.GetByLoopID(ctx, "")
	if err == nil {
		t.Error("GetByLoopID() should return error for empty loop_id")
	}
}

func TestCallStore_Delete(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewCallStore(ctx, tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	record := &CallRecord{
		RequestID: "req-delete-123",
		TraceID:   "trace-delete-456",
		StartedAt: time.Now(),
	}

	// Store the record
	if err := store.Store(ctx, record); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	// Verify it exists
	key := "trace-delete-456.req-delete-123"
	_, err = store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() before delete error = %v", err)
	}

	// Delete it
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Verify it's gone
	_, err = store.Get(ctx, key)
	if err == nil {
		t.Error("Get() after delete should return error")
	}
}

func TestCallStore_WithCustomTTL(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	customTTL := 1 * time.Hour
	store, err := NewCallStore(ctx, tc.Client, WithCallsTTL(customTTL))
	if err != nil {
		t.Fatalf("Failed to create store with custom TTL: %v", err)
	}

	if store.ttl != customTTL {
		t.Errorf("store.ttl = %v, want %v", store.ttl, customTTL)
	}
}

func TestInitGlobalCallStore_Success(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())

	// Clear global store reference (but sync.Once remembers previous init)
	clearGlobalStoreForTest(t)

	ctx := context.Background()

	// InitGlobalCallStore may be a no-op if sync.Once already fired in another test.
	// We're testing that calling it doesn't error and that GlobalCallStore works.
	err := InitGlobalCallStore(ctx, tc.Client)
	if err != nil {
		t.Fatalf("InitGlobalCallStore() error = %v", err)
	}

	store := GlobalCallStore()
	// Note: store may be nil if sync.Once fired in another test with different NATS.
	// This test validates no panic and error handling, not "first init" behavior.
	_ = store
}

func TestInitGlobalCallStore_Idempotent(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	// First call - may be no-op if sync.Once already fired
	err1 := InitGlobalCallStore(ctx, tc.Client)
	store1 := GlobalCallStore()

	// Second call should definitely be no-op (sync.Once guarantees this)
	err2 := InitGlobalCallStore(ctx, tc.Client)
	store2 := GlobalCallStore()

	// Both calls should not error
	if err1 != nil {
		t.Errorf("First InitGlobalCallStore() error = %v", err1)
	}
	if err2 != nil {
		t.Errorf("Second InitGlobalCallStore() error = %v", err2)
	}

	// The store reference should be stable between calls
	// (sync.Once ensures the initializer only runs once)
	if store1 != store2 {
		t.Error("InitGlobalCallStore() should be idempotent - same store should be returned")
	}
}

func TestCallStore_ConcurrentAccess(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewCallStore(ctx, tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	traceID := "trace-concurrent"
	now := time.Now()

	const numWriters = 10
	const numReaders = 5

	// Error channel to collect errors from goroutines (buffered to avoid blocking)
	errCh := make(chan error, numWriters+numReaders)

	// Concurrently store records
	var wg sync.WaitGroup
	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			record := &CallRecord{
				RequestID: "req-concurrent-" + string(rune('0'+idx)),
				TraceID:   traceID,
				StartedAt: now.Add(time.Duration(idx) * time.Millisecond),
			}
			if err := store.Store(ctx, record); err != nil {
				errCh <- fmt.Errorf("Store(%d): %w", idx, err)
			}
		}(i)
	}
	wg.Wait()

	// Concurrently read records
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

	// Report all errors from goroutines
	var errors []error
	for err := range errCh {
		errors = append(errors, err)
	}
	if len(errors) > 0 {
		for _, err := range errors {
			t.Errorf("Concurrent access error: %v", err)
		}
	}
}

// clearGlobalStoreForTest clears only the global store reference for testing.
// NOTE: This does NOT reset sync.Once because doing so is a data race.
// Tests using this function should not assume they are the "first" initialization.
// The sync.Once will remember that init was already attempted.
func clearGlobalStoreForTest(t *testing.T) {
	t.Helper()
	globalCallStoreMu.Lock()
	globalCallStore = nil
	globalCallStoreMu.Unlock()
	// DO NOT reset initOnce - it's a data race and sync.Once is not designed to be reset.
	// Tests must be designed to work regardless of whether init has been called before.
}
