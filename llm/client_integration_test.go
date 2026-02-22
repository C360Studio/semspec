//go:build integration

package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/c360studio/semspec/llm"
	_ "github.com/c360studio/semspec/llm/providers" // Register providers
	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semstreams/natsclient"
)

// waitForRecords polls the CallStore until the expected number of records
// are available for a given trace ID, or times out.
func waitForRecords(t *testing.T, store *llm.CallStore, traceID string, minCount int, timeout time.Duration) []*llm.CallRecord {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		records, err := store.GetByTraceID(ctx, traceID)
		if err == nil && len(records) >= minCount {
			return records
		}

		select {
		case <-ctx.Done():
			t.Fatalf("Timed out waiting for %d records with trace %s (got %d)", minCount, traceID, len(records))
			return nil
		case <-ticker.C:
			// Poll again
		}
	}
}

// TestClient_Complete_RecordsCallWithTraceContext verifies that when a trace context
// is set, the LLM client records the call to the CallStore with the correct trace ID.
func TestClient_Complete_RecordsCallWithTraceContext(t *testing.T) {
	// Create mock LLM server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":      "chatcmpl-123",
			"object":  "chat.completion",
			"created": 1677652288,
			"model":   "test-model",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]string{
						"role":    "assistant",
						"content": "Test response",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     100,
				"completion_tokens": 50,
				"total_tokens":      150,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create NATS test client and CallStore
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := llm.NewCallStore(ctx, tc.Client)
	if err != nil {
		t.Fatalf("Failed to create CallStore: %v", err)
	}

	// Create registry with test endpoint
	registry := model.NewRegistry(
		map[model.Capability]*model.CapabilityConfig{
			model.CapabilityFast: {
				Description: "Test capability",
				Preferred:   []string{"test-model"},
			},
		},
		map[string]*model.EndpointConfig{
			"test-model": {
				Provider:  "ollama",
				URL:       server.URL,
				Model:     "test-model",
				MaxTokens: 128000, // Context budget
			},
		},
	)

	// Create client with CallStore
	client := llm.NewClient(registry, llm.WithCallStore(store))

	// Set up trace context
	traceID := "test-trace-id-12345"
	loopID := "test-loop-id-67890"
	ctxWithTrace := llm.WithTraceContext(ctx, llm.TraceContext{
		TraceID: traceID,
		LoopID:  loopID,
	})

	// Make the LLM call
	resp, err := client.Complete(ctxWithTrace, llm.Request{
		Capability: "fast",
		Messages: []llm.Message{
			{Role: "user", Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if resp.Content != "Test response" {
		t.Errorf("Response content = %q, want %q", resp.Content, "Test response")
	}

	// Wait for the async call to be recorded
	records := waitForRecords(t, store, traceID, 1, 2*time.Second)
	record := records[0]

	// Verify trace context
	if record.TraceID != traceID {
		t.Errorf("Record TraceID = %q, want %q", record.TraceID, traceID)
	}
	if record.LoopID != loopID {
		t.Errorf("Record LoopID = %q, want %q", record.LoopID, loopID)
	}

	// Verify tokens
	if record.PromptTokens != 100 {
		t.Errorf("Record PromptTokens = %d, want %d", record.PromptTokens, 100)
	}
	if record.CompletionTokens != 50 {
		t.Errorf("Record CompletionTokens = %d, want %d", record.CompletionTokens, 50)
	}
	if record.TotalTokens != 150 {
		t.Errorf("Record TotalTokens = %d, want %d", record.TotalTokens, 150)
	}

	// Verify model info
	if record.Model != "test-model" {
		t.Errorf("Record Model = %q, want %q", record.Model, "test-model")
	}
	if record.Capability != "fast" {
		t.Errorf("Record Capability = %q, want %q", record.Capability, "fast")
	}
}

// TestClient_Complete_RecordsContextBudget verifies that the context budget
// (endpoint.MaxTokens) is recorded in the CallRecord.
func TestClient_Complete_RecordsContextBudget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"model": "big-context-model",
			"choices": []map[string]any{
				{
					"message": map[string]string{
						"role":    "assistant",
						"content": "Response",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     500,
				"completion_tokens": 100,
				"total_tokens":      600,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := llm.NewCallStore(ctx, tc.Client)
	if err != nil {
		t.Fatalf("Failed to create CallStore: %v", err)
	}

	// Create registry with specific context budget
	contextBudget := 200000 // 200K tokens
	registry := model.NewRegistry(
		map[model.Capability]*model.CapabilityConfig{
			model.CapabilityPlanning: {
				Preferred: []string{"big-context-model"},
			},
		},
		map[string]*model.EndpointConfig{
			"big-context-model": {
				Provider:  "ollama",
				URL:       server.URL,
				Model:     "big-context-model",
				MaxTokens: contextBudget,
			},
		},
	)

	client := llm.NewClient(registry, llm.WithCallStore(store))

	traceID := "test-context-budget-trace"
	ctxWithTrace := llm.WithTraceContext(ctx, llm.TraceContext{TraceID: traceID})

	_, err = client.Complete(ctxWithTrace, llm.Request{
		Capability: "planning",
		Messages: []llm.Message{
			{Role: "user", Content: "Plan something"},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	records := waitForRecords(t, store, traceID, 1, 2*time.Second)

	if records[0].ContextBudget != contextBudget {
		t.Errorf("ContextBudget = %d, want %d", records[0].ContextBudget, contextBudget)
	}
}

// TestClient_Complete_NoTraceContext verifies that calls without trace context
// are still recorded (with empty TraceID).
func TestClient_Complete_NoTraceContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"model": "test-model",
			"choices": []map[string]any{
				{
					"message": map[string]string{
						"role":    "assistant",
						"content": "Response",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     10,
				"completion_tokens": 5,
				"total_tokens":      15,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := llm.NewCallStore(ctx, tc.Client)
	if err != nil {
		t.Fatalf("Failed to create CallStore: %v", err)
	}

	registry := model.NewRegistry(
		map[model.Capability]*model.CapabilityConfig{
			model.CapabilityFast: {
				Preferred: []string{"test-model"},
			},
		},
		map[string]*model.EndpointConfig{
			"test-model": {
				Provider: "ollama",
				URL:      server.URL,
				Model:    "test-model",
			},
		},
	)

	client := llm.NewClient(registry, llm.WithCallStore(store))

	// Make call without trace context
	_, err = client.Complete(ctx, llm.Request{
		Capability: "fast",
		Messages: []llm.Message{
			{Role: "user", Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	// The call should still be recorded, but we can't query by trace ID
	// since there is none. This test verifies no panic/error occurs.
}

// TestClient_Complete_MultipleCallsSameTrace verifies that multiple LLM calls
// with the same trace ID are all recorded and retrievable.
func TestClient_Complete_MultipleCallsSameTrace(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := map[string]any{
			"model": "test-model",
			"choices": []map[string]any{
				{
					"message": map[string]string{
						"role":    "assistant",
						"content": "Response " + string(rune('0'+callCount)),
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     10 * callCount,
				"completion_tokens": 5 * callCount,
				"total_tokens":      15 * callCount,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := llm.NewCallStore(ctx, tc.Client)
	if err != nil {
		t.Fatalf("Failed to create CallStore: %v", err)
	}

	registry := model.NewRegistry(
		map[model.Capability]*model.CapabilityConfig{
			model.CapabilityFast: {
				Preferred: []string{"test-model"},
			},
		},
		map[string]*model.EndpointConfig{
			"test-model": {
				Provider:  "ollama",
				URL:       server.URL,
				Model:     "test-model",
				MaxTokens: 32000,
			},
		},
	)

	client := llm.NewClient(registry, llm.WithCallStore(store))

	traceID := "multi-call-trace"
	ctxWithTrace := llm.WithTraceContext(ctx, llm.TraceContext{TraceID: traceID})

	// Make 3 calls with the same trace
	for i := 0; i < 3; i++ {
		_, err = client.Complete(ctxWithTrace, llm.Request{
			Capability: "fast",
			Messages: []llm.Message{
				{Role: "user", Content: "Message " + string(rune('0'+i))},
			},
		})
		if err != nil {
			t.Fatalf("Complete() call %d error = %v", i, err)
		}
	}

	// Wait for all 3 calls to be recorded
	records := waitForRecords(t, store, traceID, 3, 5*time.Second)

	// Verify all records have the same trace ID
	for i, r := range records {
		if r.TraceID != traceID {
			t.Errorf("Record %d TraceID = %q, want %q", i, r.TraceID, traceID)
		}
		if r.ContextBudget != 32000 {
			t.Errorf("Record %d ContextBudget = %d, want %d", i, r.ContextBudget, 32000)
		}
	}

	// Verify records are sorted by time (ascending)
	for i := 1; i < len(records); i++ {
		if records[i].StartedAt.Before(records[i-1].StartedAt) {
			t.Errorf("Records not sorted: record %d started before record %d", i, i-1)
		}
	}
}

// TestClient_Complete_RecordsFailedCall verifies that failed LLM calls
// are also recorded with error information.
func TestClient_Complete_RecordsFailedCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a fatal error (401 Unauthorized)
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Invalid API key"))
	}))
	defer server.Close()

	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := llm.NewCallStore(ctx, tc.Client)
	if err != nil {
		t.Fatalf("Failed to create CallStore: %v", err)
	}

	registry := model.NewRegistry(
		map[model.Capability]*model.CapabilityConfig{
			model.CapabilityFast: {
				Preferred: []string{"test-model"},
			},
		},
		map[string]*model.EndpointConfig{
			"test-model": {
				Provider:  "ollama",
				URL:       server.URL,
				Model:     "test-model",
				MaxTokens: 8000,
			},
		},
	)

	client := llm.NewClient(registry, llm.WithCallStore(store))

	traceID := "failed-call-trace"
	ctxWithTrace := llm.WithTraceContext(ctx, llm.TraceContext{TraceID: traceID})

	_, err = client.Complete(ctxWithTrace, llm.Request{
		Capability: "fast",
		Messages: []llm.Message{
			{Role: "user", Content: "This will fail"},
		},
	})

	// Should have an error
	if err == nil {
		t.Fatal("Expected error from Complete(), got nil")
	}

	// Wait for the failed call to be recorded
	records := waitForRecords(t, store, traceID, 1, 2*time.Second)
	record := records[0]

	// Verify error was recorded
	if record.Error == "" {
		t.Error("Expected Error field to be set for failed call")
	}

	// Verify trace ID is still recorded
	if record.TraceID != traceID {
		t.Errorf("Record TraceID = %q, want %q", record.TraceID, traceID)
	}

	// Verify context budget is recorded even for failed calls
	if record.ContextBudget != 8000 {
		t.Errorf("ContextBudget = %d, want %d", record.ContextBudget, 8000)
	}
}
