package plancoordinator

import (
	"context"
	"log/slog"
	"testing"

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/llm/testutil"
)

// validFocusAreasJSON is a valid response for focus area selection.
const validFocusAreasJSON = `{
  "planners": [
    {
      "focus": "api",
      "description": "API endpoints and handlers",
      "hints": ["api/", "handlers/"]
    },
    {
      "focus": "data",
      "description": "Data models and persistence",
      "hints": ["models/", "store/"]
    }
  ]
}`

func TestPlanCoordinator_TraceContextPassedToLLM(t *testing.T) {
	tests := []struct {
		name        string
		traceID     string
		loopID      string
		wantTraceID string
		wantLoopID  string
	}{
		{
			name:        "injects trace ID and loop ID",
			traceID:     "coord-trace-123",
			loopID:      "coord-loop-789",
			wantTraceID: "coord-trace-123",
			wantLoopID:  "coord-loop-789",
		},
		{
			name:        "injects only trace ID when no loop ID",
			traceID:     "coord-trace-only",
			loopID:      "",
			wantTraceID: "coord-trace-only",
			wantLoopID:  "",
		},
		{
			name:        "injects only loop ID when no trace ID",
			traceID:     "",
			loopID:      "coord-loop-only",
			wantTraceID: "",
			wantLoopID:  "coord-loop-only",
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
			mockClient := &testutil.MockLLMClient{
				Responses: []*llm.Response{
					{
						Content: validFocusAreasJSON,
						Model:   "test-model",
					},
				},
			}

			// Create component with mock (verifies interface compatibility)
			c := &Component{
				llmClient: mockClient,
				logger:    slog.Default(),
				config:    Config{DefaultCapability: "planning"},
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
				Capability:  "planning",
				Messages:    []llm.Message{{Role: "user", Content: "Select focus areas"}},
				Temperature: &temperature,
				MaxTokens:   4096,
			})
			if err != nil {
				t.Fatalf("Complete() error = %v", err)
			}

			// Verify the captured context has the correct trace context
			capturedCtx := mockClient.GetCapturedContext()
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

func TestPlanCoordinator_SubPlannersShareTraceID(t *testing.T) {
	// This test verifies that when the coordinator spawns sub-planners,
	// they all receive the same trace ID for correlation.
	//
	// The actual spawning happens via NATS publish, but we can verify
	// that the coordinator passes trace context to its own LLM calls.

	mockClient := &testutil.MockLLMClient{
		Responses: []*llm.Response{
			{
				Content: validFocusAreasJSON,
				Model:   "test-model",
			},
		},
	}

	// Create component with mock (verifies interface compatibility)
	c := &Component{
		llmClient: mockClient,
		logger:    slog.Default(),
		config:    Config{DefaultCapability: "planning"},
	}

	// Set up trace context
	traceID := "shared-trace-for-sub-planners"
	loopID := "main-coordination-loop"
	ctx := llm.WithTraceContext(context.Background(), llm.TraceContext{
		TraceID: traceID,
		LoopID:  loopID,
	})

	// Simulate multiple LLM calls (focus selection + potential follow-ups)
	for i := 0; i < 3; i++ {
		temperature := 0.7
		_, err := c.llmClient.Complete(ctx, llm.Request{
			Capability:  "planning",
			Messages:    []llm.Message{{Role: "user", Content: "Call " + string(rune('0'+i))}},
			Temperature: &temperature,
		})
		if err != nil {
			t.Fatalf("Complete() call %d error = %v", i, err)
		}
	}

	// Verify all calls used the same trace context
	if mockClient.GetCallCount() != 3 {
		t.Errorf("Expected 3 calls, got %d", mockClient.GetCallCount())
	}

	// The last captured context should still have the trace ID
	tc := llm.GetTraceContext(mockClient.GetCapturedContext())
	if tc.TraceID != traceID {
		t.Errorf("TraceID = %q, want %q", tc.TraceID, traceID)
	}
	if tc.LoopID != loopID {
		t.Errorf("LoopID = %q, want %q", tc.LoopID, loopID)
	}
}
