package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semspec/llm"
	_ "github.com/c360studio/semspec/llm/providers" // Register providers
	"github.com/c360studio/semspec/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_Complete_Success(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		// Return success response (OpenAI format)
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
						"content": "Hello! How can I help you?",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     10,
				"completion_tokens": 8,
				"total_tokens":      18,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

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
				Provider: "ollama",
				URL:      server.URL,
				Model:    "test-model",
			},
		},
	)

	client := llm.NewClient(registry)

	resp, err := client.Complete(context.Background(), llm.Request{
		Capability: "fast",
		Messages: []llm.Message{
			{Role: "user", Content: "Hello"},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "Hello! How can I help you?", resp.Content)
	assert.Equal(t, "test-model", resp.Model)
	assert.Equal(t, 18, resp.TokensUsed)
	assert.Equal(t, "stop", resp.FinishReason)
}

func TestClient_Complete_RetryOnTransientError(t *testing.T) {
	var attempts atomic.Int32

	// Server that fails first 2 times, then succeeds
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attempts.Add(1)

		if attempt < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("Service temporarily unavailable"))
			return
		}

		// Success on third attempt
		resp := map[string]any{
			"model": "test-model",
			"choices": []map[string]any{
				{
					"message": map[string]string{
						"role":    "assistant",
						"content": "Success after retries",
					},
					"finish_reason": "stop",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

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

	// Use shorter backoff for testing
	client := llm.NewClient(registry, llm.WithRetryConfig(llm.RetryConfig{
		MaxAttempts:       3,
		BackoffBase:       10 * time.Millisecond,
		BackoffMultiplier: 1.5,
		MaxBackoff:        100 * time.Millisecond,
	}))

	resp, err := client.Complete(context.Background(), llm.Request{
		Capability: "fast",
		Messages: []llm.Message{
			{Role: "user", Content: "Test"},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "Success after retries", resp.Content)
	assert.Equal(t, int32(3), attempts.Load())
}

func TestClient_Complete_NoRetryOnFatalError(t *testing.T) {
	var attempts atomic.Int32

	// Server that returns 401 Unauthorized (fatal)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Invalid API key"))
	}))
	defer server.Close()

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

	client := llm.NewClient(registry)

	_, err := client.Complete(context.Background(), llm.Request{
		Capability: "fast",
		Messages: []llm.Message{
			{Role: "user", Content: "Test"},
		},
	})

	require.Error(t, err)
	assert.True(t, llm.IsFatal(err))
	assert.Equal(t, int32(1), attempts.Load()) // Only one attempt
}

func TestClient_Complete_Fallback(t *testing.T) {
	var primaryAttempts, fallbackAttempts atomic.Int32

	// Primary server always fails
	primaryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryAttempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("Primary down"))
	}))
	defer primaryServer.Close()

	// Fallback server succeeds
	fallbackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackAttempts.Add(1)
		resp := map[string]any{
			"model": "fallback-model",
			"choices": []map[string]any{
				{
					"message": map[string]string{
						"role":    "assistant",
						"content": "From fallback",
					},
					"finish_reason": "stop",
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer fallbackServer.Close()

	registry := model.NewRegistry(
		map[model.Capability]*model.CapabilityConfig{
			model.CapabilityFast: {
				Preferred: []string{"primary"},
				Fallback:  []string{"fallback"},
			},
		},
		map[string]*model.EndpointConfig{
			"primary": {
				Provider: "ollama",
				URL:      primaryServer.URL,
				Model:    "primary-model",
			},
			"fallback": {
				Provider: "ollama",
				URL:      fallbackServer.URL,
				Model:    "fallback-model",
			},
		},
	)

	// Use short retry for faster test
	client := llm.NewClient(registry, llm.WithRetryConfig(llm.RetryConfig{
		MaxAttempts:       2,
		BackoffBase:       1 * time.Millisecond,
		BackoffMultiplier: 1.0,
		MaxBackoff:        10 * time.Millisecond,
	}))

	resp, err := client.Complete(context.Background(), llm.Request{
		Capability: "fast",
		Messages: []llm.Message{
			{Role: "user", Content: "Test"},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "From fallback", resp.Content)
	assert.Equal(t, int32(2), primaryAttempts.Load())  // Tried twice (max attempts)
	assert.Equal(t, int32(1), fallbackAttempts.Load()) // Succeeded first try
}

func TestClient_Complete_RateLimitRetry(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attempts.Add(1)

		if attempt == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte("Rate limited"))
			return
		}

		resp := map[string]any{
			"model": "test-model",
			"choices": []map[string]any{
				{
					"message": map[string]string{
						"role":    "assistant",
						"content": "Success",
					},
					"finish_reason": "stop",
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

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

	client := llm.NewClient(registry, llm.WithRetryConfig(llm.RetryConfig{
		MaxAttempts:       3,
		BackoffBase:       1 * time.Millisecond,
		BackoffMultiplier: 1.0,
		MaxBackoff:        10 * time.Millisecond,
	}))

	resp, err := client.Complete(context.Background(), llm.Request{
		Capability: "fast",
		Messages: []llm.Message{
			{Role: "user", Content: "Test"},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "Success", resp.Content)
	assert.Equal(t, int32(2), attempts.Load())
}

func TestClient_Complete_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

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

	client := llm.NewClient(registry)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Complete(ctx, llm.Request{
		Capability: "fast",
		Messages: []llm.Message{
			{Role: "user", Content: "Test"},
		},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "context")
}

func TestClient_Complete_ValidationErrors(t *testing.T) {
	registry := model.NewDefaultRegistry()
	client := llm.NewClient(registry)

	tests := []struct {
		name    string
		req     llm.Request
		wantErr string
	}{
		{
			name:    "empty capability",
			req:     llm.Request{Messages: []llm.Message{{Role: "user", Content: "hi"}}},
			wantErr: "capability is required",
		},
		{
			name:    "no messages",
			req:     llm.Request{Capability: "fast"},
			wantErr: "at least one message is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Complete(context.Background(), tt.req)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
