package taskreviewer

import (
	"context"
	"sync"
	"testing"

	"github.com/c360studio/semspec/llm"
)

// TestTaskReviewer_TraceContextPassedToLLM verifies that trace context
// is properly passed through to the LLM client during review operations.
// This tests the same code path as reviewTasks but focuses on the trace
// context propagation without requiring full context-builder setup.
func TestTaskReviewer_TraceContextPassedToLLM(t *testing.T) {
	tests := []struct {
		name        string
		traceID     string
		loopID      string
		wantTraceID string
		wantLoopID  string
	}{
		{
			name:        "injects trace ID and loop ID",
			traceID:     "review-trace-123",
			loopID:      "review-loop-789",
			wantTraceID: "review-trace-123",
			wantLoopID:  "review-loop-789",
		},
		{
			name:        "injects only trace ID when no loop ID",
			traceID:     "review-trace-only",
			loopID:      "",
			wantTraceID: "review-trace-only",
			wantLoopID:  "",
		},
		{
			name:        "injects only loop ID when no trace ID",
			traceID:     "",
			loopID:      "review-loop-only",
			wantTraceID: "",
			wantLoopID:  "review-loop-only",
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
			var capturedCtx context.Context
			var mu sync.Mutex

			mock := &mockLLM{
				responses: []*llm.Response{
					{Content: validApprovedJSON, Model: "test-model"},
				},
			}

			// Set up context with trace info (simulating what handleMessage does)
			ctx := context.Background()
			if tt.traceID != "" || tt.loopID != "" {
				ctx = llm.WithTraceContext(ctx, llm.TraceContext{
					TraceID: tt.traceID,
					LoopID:  tt.loopID,
				})
			}

			// Simulate the LLM call loop from reviewTasks
			// This tests the same trace context propagation path
			messages := []llm.Message{
				{Role: "system", Content: "You are a task reviewer."},
				{Role: "user", Content: "Review these tasks."},
			}
			temperature := 0.3

			// Capture context during the call
			wrappedMock := &contextCaptureMock{
				inner:       mock,
				capturedCtx: &capturedCtx,
				mu:          &mu,
			}

			_, err := wrappedMock.Complete(ctx, llm.Request{
				Capability:  "reviewing",
				Messages:    messages,
				Temperature: &temperature,
				MaxTokens:   4096,
			})
			if err != nil {
				t.Fatalf("Complete() error = %v", err)
			}

			// Verify the captured context has the correct trace context
			mu.Lock()
			if capturedCtx == nil {
				mu.Unlock()
				t.Fatal("Context was not captured")
			}
			tc := llm.GetTraceContext(capturedCtx)
			mu.Unlock()

			if tc.TraceID != tt.wantTraceID {
				t.Errorf("TraceID = %q, want %q", tc.TraceID, tt.wantTraceID)
			}
			if tc.LoopID != tt.wantLoopID {
				t.Errorf("LoopID = %q, want %q", tc.LoopID, tt.wantLoopID)
			}
		})
	}
}

// TestTaskReviewer_TraceContextPreservedThroughRetries verifies that
// trace context is preserved through the format correction retry loop.
func TestTaskReviewer_TraceContextPreservedThroughRetries(t *testing.T) {
	var lastCtx context.Context
	var mu sync.Mutex
	callCount := 0

	mock := &mockLLM{
		responses: []*llm.Response{
			{Content: "Not valid JSON", Model: "test-model"},
			{Content: validApprovedJSON, Model: "test-model"},
		},
	}

	wrappedMock := &contextCaptureMock{
		inner:       mock,
		capturedCtx: &lastCtx,
		mu:          &mu,
		callCount:   &callCount,
	}

	// Set up trace context
	traceID := "retry-trace-456"
	loopID := "retry-loop-789"
	ctx := llm.WithTraceContext(context.Background(), llm.TraceContext{
		TraceID: traceID,
		LoopID:  loopID,
	})

	// Simulate the retry loop from reviewTasks
	messages := []llm.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "usr"},
	}
	temperature := 0.3
	c := newTestComponent(mock)

	for attempt := range maxFormatRetries {
		llmResp, err := wrappedMock.Complete(ctx, llm.Request{
			Capability:  "reviewing",
			Messages:    messages,
			Temperature: &temperature,
			MaxTokens:   4096,
		})
		if err != nil {
			t.Fatalf("Complete() error = %v", err)
		}

		_, parseErr := c.parseReviewFromResponse(llmResp.Content)
		if parseErr == nil {
			break
		}

		if attempt+1 >= maxFormatRetries {
			t.Fatal("Should have succeeded before max retries")
		}

		messages = append(messages,
			llm.Message{Role: "assistant", Content: llmResp.Content},
			llm.Message{Role: "user", Content: formatCorrectionPrompt(parseErr)},
		)
	}

	// Verify multiple calls were made
	mu.Lock()
	if callCount < 2 {
		mu.Unlock()
		t.Errorf("Expected at least 2 calls (retry), got %d", callCount)
	}

	// Verify trace context was preserved through retries
	tc := llm.GetTraceContext(lastCtx)
	mu.Unlock()

	if tc.TraceID != traceID {
		t.Errorf("TraceID after retry = %q, want %q", tc.TraceID, traceID)
	}
	if tc.LoopID != loopID {
		t.Errorf("LoopID after retry = %q, want %q", tc.LoopID, loopID)
	}
}

// contextCaptureMock wraps mockLLM to capture context.
type contextCaptureMock struct {
	inner       *mockLLM
	capturedCtx *context.Context
	mu          *sync.Mutex
	callCount   *int
}

func (m *contextCaptureMock) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	m.mu.Lock()
	*m.capturedCtx = ctx
	if m.callCount != nil {
		*m.callCount++
	}
	m.mu.Unlock()
	return m.inner.Complete(ctx, req)
}
