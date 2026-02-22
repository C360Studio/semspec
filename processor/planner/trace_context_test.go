package planner

import (
	"context"
	"log/slog"
	"testing"

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/llm/testutil"
	"github.com/c360studio/semspec/workflow"
)

func TestPlanner_InjectsTraceContext(t *testing.T) {
	tests := []struct {
		name        string
		trigger     *workflow.TriggerPayload
		wantTraceID string
		wantLoopID  string
	}{
		{
			name: "injects trace ID and loop ID",
			trigger: &workflow.TriggerPayload{
				TraceID:   "test-trace-123",
				RequestID: "req-456",
				LoopID:    "loop-789",
				Data: &workflow.TriggerData{
					Slug:  "test-plan",
					Title: "Test Plan Title",
				},
			},
			wantTraceID: "test-trace-123",
			wantLoopID:  "loop-789",
		},
		{
			name: "injects only trace ID when no loop ID",
			trigger: &workflow.TriggerPayload{
				TraceID:   "test-trace-only",
				RequestID: "req-abc",
				Data: &workflow.TriggerData{
					Slug:  "test-plan",
					Title: "Test Plan Title",
				},
			},
			wantTraceID: "test-trace-only",
			wantLoopID:  "",
		},
		{
			name: "injects only loop ID when no trace ID",
			trigger: &workflow.TriggerPayload{
				RequestID: "req-xyz",
				LoopID:    "loop-only",
				Data: &workflow.TriggerData{
					Slug:  "test-plan",
					Title: "Test Plan Title",
				},
			},
			wantTraceID: "",
			wantLoopID:  "loop-only",
		},
		{
			name: "no trace context when both empty",
			trigger: &workflow.TriggerPayload{
				RequestID: "req-empty",
				Data: &workflow.TriggerData{
					Slug:  "test-plan",
					Title: "Test Plan Title",
				},
			},
			wantTraceID: "",
			wantLoopID:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock LLM client that returns valid plan JSON
			mockClient := &testutil.MockLLMClient{
				Responses: []*llm.Response{
					{
						Content: `{"goal": "Test goal", "context": "Test context", "scope": {"include": []}}`,
						Model:   "test-model",
					},
				},
			}

			// Create component with mock LLM client
			c := &Component{
				llmClient: mockClient,
				config:    DefaultConfig(),
				logger:    slog.Default(),
			}

			// Call generatePlan which should inject trace context
			ctx := context.Background()
			if tt.trigger.TraceID != "" || tt.trigger.LoopID != "" {
				ctx = llm.WithTraceContext(ctx, llm.TraceContext{
					TraceID: tt.trigger.TraceID,
					LoopID:  tt.trigger.LoopID,
				})
			}

			// Call generatePlanFromMessages directly (simulating what handleMessage does)
			_, err := c.generatePlanFromMessages(ctx, "planning", "system prompt", "user prompt")
			if err != nil {
				t.Fatalf("generatePlanFromMessages() error = %v", err)
			}

			// Verify the captured context has the correct trace context
			capturedCtx := mockClient.GetCapturedContext()
			if capturedCtx == nil {
				t.Fatal("LLM client was not called")
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

func TestPlanner_TraceContextPassedThroughMultipleRetries(t *testing.T) {
	// Create mock with multiple responses to simulate retry behavior
	mockClient := &testutil.MockLLMClient{
		Responses: []*llm.Response{
			{Content: "This is not valid JSON", Model: "test-model"},
			{Content: `{"goal": "Test goal", "context": "Test context", "scope": {}}`, Model: "test-model"},
		},
	}

	c := &Component{
		llmClient: mockClient,
		config:    DefaultConfig(),
		logger:    slog.Default(),
	}

	// Set up trace context
	traceID := "retry-trace-123"
	loopID := "retry-loop-456"
	ctx := llm.WithTraceContext(context.Background(), llm.TraceContext{
		TraceID: traceID,
		LoopID:  loopID,
	})

	_, err := c.generatePlanFromMessages(ctx, "planning", "system prompt", "user prompt")
	if err != nil {
		t.Fatalf("generatePlanFromMessages() error = %v", err)
	}

	// Verify trace context was preserved through retries
	if mockClient.GetCallCount() < 2 {
		t.Errorf("Expected at least 2 calls (initial + retry), got %d", mockClient.GetCallCount())
	}

	tc := llm.GetTraceContext(mockClient.GetCapturedContext())
	if tc.TraceID != traceID {
		t.Errorf("TraceID after retry = %q, want %q", tc.TraceID, traceID)
	}
	if tc.LoopID != loopID {
		t.Errorf("LoopID after retry = %q, want %q", tc.LoopID, loopID)
	}
}
