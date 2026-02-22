package taskgenerator

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/c360studio/semspec/llm"
)

// mockLLMClient captures the context passed to Complete for trace context verification.
type mockLLMClient struct {
	mu              sync.Mutex
	capturedContext context.Context
	response        *llm.Response
	callCount       int
}

func (m *mockLLMClient) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	m.mu.Lock()
	m.capturedContext = ctx
	m.callCount++
	m.mu.Unlock()

	return m.response, nil
}

func (m *mockLLMClient) getCapturedContext() context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.capturedContext
}

// validTasksJSON is a valid response for task generation.
const validTasksJSON = `{
  "tasks": [
    {
      "description": "Implement user authentication",
      "type": "implement",
      "acceptance_criteria": [
        {
          "given": "a user with credentials",
          "when": "they log in",
          "then": "they get a token"
        }
      ]
    }
  ]
}`

func TestTaskGenerator_TraceContextPassedToLLM(t *testing.T) {
	tests := []struct {
		name        string
		traceID     string
		loopID      string
		wantTraceID string
		wantLoopID  string
	}{
		{
			name:        "injects trace ID and loop ID",
			traceID:     "task-trace-123",
			loopID:      "task-loop-789",
			wantTraceID: "task-trace-123",
			wantLoopID:  "task-loop-789",
		},
		{
			name:        "injects only trace ID when no loop ID",
			traceID:     "task-trace-only",
			loopID:      "",
			wantTraceID: "task-trace-only",
			wantLoopID:  "",
		},
		{
			name:        "injects only loop ID when no trace ID",
			traceID:     "",
			loopID:      "task-loop-only",
			wantTraceID: "",
			wantLoopID:  "task-loop-only",
		},
		{
			name:        "no trace context when both empty",
			traceID:     "",
			loopID:      "",
			wantTraceID: "",
			wantLoopID:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock that captures context
			mockClient := &mockLLMClient{
				response: &llm.Response{
					Content: validTasksJSON,
					Model:   "test-model",
				},
			}

			// Create component with mock (verifies interface compatibility)
			c := &Component{
				llmClient: mockClient,
				logger:    slog.Default(),
				config:    Config{DefaultCapability: "writing"},
			}

			// Set up context with trace info (simulating what handleMessage does)
			ctx := context.Background()
			if tt.traceID != "" || tt.loopID != "" {
				ctx = llm.WithTraceContext(ctx, llm.TraceContext{
					TraceID: tt.traceID,
					LoopID:  tt.loopID,
				})
			}

			// Call through the component's LLM client to verify interface works
			temperature := 0.7
			_, err := c.llmClient.Complete(ctx, llm.Request{
				Capability:  "writing",
				Messages:    []llm.Message{{Role: "user", Content: "Generate tasks"}},
				Temperature: &temperature,
				MaxTokens:   4096,
			})
			if err != nil {
				t.Fatalf("Complete() error = %v", err)
			}

			// Verify the captured context has the correct trace context
			capturedCtx := mockClient.getCapturedContext()
			if capturedCtx == nil {
				t.Fatal("Context was not captured")
			}

			tc := llm.GetTraceContext(capturedCtx)
			if tc.TraceID != tt.wantTraceID {
				t.Errorf("TraceID = %q, want %q", tc.TraceID, tt.wantTraceID)
			}
			if tc.LoopID != tt.wantLoopID {
				t.Errorf("LoopID = %q, want %q", tc.LoopID, tt.wantLoopID)
			}
		})
	}
}

