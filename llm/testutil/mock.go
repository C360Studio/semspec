// Package testutil provides test utilities for the llm package.
// It includes mock implementations for testing LLM client interactions.
package testutil

import (
	"context"
	"sync"

	"github.com/c360studio/semspec/llm"
)

// MockLLMClient is a thread-safe mock LLM client for testing.
// It captures the context passed to Complete() and returns configured responses.
//
// Usage:
//
//	// Single response mock
//	mock := &MockLLMClient{
//	    Responses: []*llm.Response{
//	        {Content: `{"result": "success"}`, Model: "test-model"},
//	    },
//	}
//
//	// Multiple responses (for retry testing)
//	mock := &MockLLMClient{
//	    Responses: []*llm.Response{
//	        {Content: "invalid json", Model: "test-model"},
//	        {Content: `{"result": "success"}`, Model: "test-model"},
//	    },
//	}
//
//	// Error response
//	mock := &MockLLMClient{
//	    Err: errors.New("connection failed"),
//	}
type MockLLMClient struct {
	mu              sync.Mutex
	capturedContext context.Context
	Responses       []*llm.Response // Responses to return in sequence
	Err             error           // Error to return (takes precedence over Responses)
	callCount       int
	responseIndex   int
}

// Complete implements llm.Client interface.
// Returns the next response from Responses slice, or Err if set.
// Captures the context for verification in tests.
func (m *MockLLMClient) Complete(ctx context.Context, _ llm.Request) (*llm.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.capturedContext = ctx
	m.callCount++

	if m.Err != nil {
		return nil, m.Err
	}

	if m.responseIndex < len(m.Responses) {
		resp := m.Responses[m.responseIndex]
		m.responseIndex++
		return resp, nil
	}

	// Default response if no responses configured
	return &llm.Response{Content: "", Model: "test-model"}, nil
}

// GetCapturedContext returns the last context passed to Complete().
func (m *MockLLMClient) GetCapturedContext() context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.capturedContext
}

// GetCallCount returns the number of times Complete() was called.
func (m *MockLLMClient) GetCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

// Reset resets the mock's state (call count and response index).
// Useful for reusing the same mock instance across multiple test cases.
func (m *MockLLMClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount = 0
	m.responseIndex = 0
	m.capturedContext = nil
}
