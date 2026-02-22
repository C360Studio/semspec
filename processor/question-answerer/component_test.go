package questionanswerer

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/llm/testutil"
	"github.com/c360studio/semspec/workflow/answerer"
)

func TestNewComponent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "valid default config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "valid custom config",
			config: Config{
				StreamName:            "TEST_STREAM",
				ConsumerName:          "test-consumer",
				TaskSubject:           "test.task.question",
				DefaultCapability:     "analysis",
				ContextSubjectPrefix:  "ctx.build",
				ContextResponseBucket: "TEST_CONTEXT",
				ContextTimeout:        "60s",
			},
			wantErr: false,
		},
		{
			name: "missing stream name",
			config: Config{
				StreamName:   "",
				ConsumerName: "test",
				TaskSubject:  "test",
			},
			wantErr: true,
		},
		{
			name: "missing consumer name",
			config: Config{
				StreamName:   "test",
				ConsumerName: "",
				TaskSubject:  "test",
			},
			wantErr: true,
		},
		{
			name: "missing task subject",
			config: Config{
				StreamName:   "test",
				ConsumerName: "test",
				TaskSubject:  "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rawConfig, err := json.Marshal(tt.config)
			if err != nil {
				t.Fatalf("failed to marshal config: %v", err)
			}

			// Note: We can't call NewComponent without a real NATS client,
			// so we just test config validation
			var parsedConfig Config
			if err := json.Unmarshal(rawConfig, &parsedConfig); err != nil {
				t.Fatalf("unmarshal config: %v", err)
			}

			err = parsedConfig.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHandleMessage_TraceContextInjection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		task        *answerer.QuestionAnswerTask
		wantTraceID string
		wantLoopID  string
	}{
		{
			name: "injects trace ID and loop ID",
			task: &answerer.QuestionAnswerTask{
				TaskID:     "task-123",
				QuestionID: "q-456",
				Topic:      "api.endpoints",
				Question:   "What endpoint should I use?",
				AgentName:  "developer",
				TraceID:    "test-trace-123",
				LoopID:     "loop-789",
			},
			wantTraceID: "test-trace-123",
			wantLoopID:  "loop-789",
		},
		{
			name: "injects only trace ID when no loop ID",
			task: &answerer.QuestionAnswerTask{
				TaskID:     "task-abc",
				QuestionID: "q-def",
				Topic:      "database.schema",
				Question:   "Which table should I use?",
				AgentName:  "developer",
				TraceID:    "test-trace-only",
			},
			wantTraceID: "test-trace-only",
			wantLoopID:  "",
		},
		{
			name: "injects only loop ID when no trace ID",
			task: &answerer.QuestionAnswerTask{
				TaskID:     "task-xyz",
				QuestionID: "q-uvw",
				Topic:      "testing.strategy",
				Question:   "What tests should I write?",
				AgentName:  "developer",
				LoopID:     "loop-only",
			},
			wantTraceID: "",
			wantLoopID:  "loop-only",
		},
		{
			name: "no trace context when both empty",
			task: &answerer.QuestionAnswerTask{
				TaskID:     "task-empty",
				QuestionID: "q-empty",
				Topic:      "general",
				Question:   "General question?",
				AgentName:  "developer",
			},
			wantTraceID: "",
			wantLoopID:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create mock LLM client that captures context
			mockClient := &testutil.MockLLMClient{
				Responses: []*llm.Response{
					{
						Content:    "This is the answer to your question.",
						Model:      "test-model",
						TokensUsed: 50,
					},
				},
			}

			// Create component with mock LLM client
			c := &Component{
				llmClient: mockClient,
				config:    DefaultConfig(),
				logger:    slog.Default(),
			}

			// Inject trace context like handleMessage does
			ctx := context.Background()
			if tt.task.TraceID != "" || tt.task.LoopID != "" {
				ctx = llm.WithTraceContext(ctx, llm.TraceContext{
					TraceID: tt.task.TraceID,
					LoopID:  tt.task.LoopID,
				})
			}

			_, err := c.generateAnswer(ctx, tt.task)
			if err != nil {
				t.Fatalf("generateAnswer() error = %v", err)
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

func TestGenerateAnswer_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		task           *answerer.QuestionAnswerTask
		mockResponse   *llm.Response
		wantContains   string
		wantCapability string
	}{
		{
			name: "successful answer generation with default capability",
			task: &answerer.QuestionAnswerTask{
				TaskID:     "task-001",
				QuestionID: "q-001",
				Topic:      "api.design",
				Question:   "How should I structure the REST API?",
				Context:    "Building a user management system",
				AgentName:  "architect",
			},
			mockResponse: &llm.Response{
				Content:    "You should use RESTful principles with resource-based URLs.",
				Model:      "test-model",
				TokensUsed: 75,
			},
			wantContains:   "RESTful principles",
			wantCapability: "planning",
		},
		{
			name: "answer with custom capability",
			task: &answerer.QuestionAnswerTask{
				TaskID:     "task-002",
				QuestionID: "q-002",
				Topic:      "code.optimization",
				Question:   "How can I optimize this query?",
				Capability: "analysis",
				AgentName:  "developer",
			},
			mockResponse: &llm.Response{
				Content:    "Add indexes on the frequently queried columns.",
				Model:      "test-model",
				TokensUsed: 60,
			},
			wantContains:   "indexes",
			wantCapability: "analysis",
		},
		{
			name: "answer with context provided",
			task: &answerer.QuestionAnswerTask{
				TaskID:     "task-003",
				QuestionID: "q-003",
				Topic:      "security.auth",
				Question:   "Which authentication method should I use?",
				Context:    "API is public-facing with mobile clients",
				AgentName:  "security-expert",
			},
			mockResponse: &llm.Response{
				Content:    "Use OAuth 2.0 with JWT tokens for mobile clients.",
				Model:      "test-model",
				TokensUsed: 80,
			},
			wantContains:   "OAuth 2.0",
			wantCapability: "planning",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockClient := &testutil.MockLLMClient{
				Responses: []*llm.Response{tt.mockResponse},
			}

			c := &Component{
				llmClient: mockClient,
				config:    DefaultConfig(),
				logger:    slog.Default(),
			}

			ctx := context.Background()
			answer, err := c.generateAnswer(ctx, tt.task)
			if err != nil {
				t.Fatalf("generateAnswer() error = %v", err)
			}

			if answer == "" {
				t.Error("expected non-empty answer")
			}

			if tt.wantContains != "" && !contains(answer, tt.wantContains) {
				t.Errorf("answer = %q, want to contain %q", answer, tt.wantContains)
			}

			// Verify mock was called
			if mockClient.GetCallCount() != 1 {
				t.Errorf("LLM client called %d times, want 1", mockClient.GetCallCount())
			}
		})
	}
}

func TestGenerateAnswer_LLMError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		task    *answerer.QuestionAnswerTask
		mockErr error
		wantErr bool
	}{
		{
			name: "LLM client returns error",
			task: &answerer.QuestionAnswerTask{
				TaskID:     "task-fail-001",
				QuestionID: "q-fail-001",
				Topic:      "api.endpoints",
				Question:   "What endpoint?",
				AgentName:  "developer",
			},
			mockErr: errors.New("LLM service unavailable"),
			wantErr: true,
		},
		{
			name: "LLM timeout error",
			task: &answerer.QuestionAnswerTask{
				TaskID:     "task-fail-002",
				QuestionID: "q-fail-002",
				Topic:      "database.schema",
				Question:   "Schema design?",
				AgentName:  "architect",
			},
			mockErr: context.DeadlineExceeded,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockClient := &testutil.MockLLMClient{
				Err: tt.mockErr,
			}

			c := &Component{
				llmClient: mockClient,
				config:    DefaultConfig(),
				logger:    slog.Default(),
			}

			ctx := context.Background()
			_, err := c.generateAnswer(ctx, tt.task)

			if (err != nil) != tt.wantErr {
				t.Errorf("generateAnswer() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBuildPromptWithContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		task         *answerer.QuestionAnswerTask
		graphContext string
		wantContains []string
	}{
		{
			name: "prompt with all fields",
			task: &answerer.QuestionAnswerTask{
				TaskID:     "task-001",
				QuestionID: "q-001",
				Topic:      "api.design",
				Question:   "How should I structure the API?",
				Context:    "Building a user management system",
			},
			graphContext: "Entity: UserService\nPredicate: implements authentication",
			wantContains: []string{
				"api.design",
				"How should I structure the API?",
				"Building a user management system",
				"Codebase Context",
				"UserService",
			},
		},
		{
			name: "prompt without provided context",
			task: &answerer.QuestionAnswerTask{
				TaskID:   "task-002",
				Topic:    "testing.strategy",
				Question: "What tests should I write?",
			},
			graphContext: "",
			wantContains: []string{
				"testing.strategy",
				"What tests should I write?",
			},
		},
		{
			name: "prompt with graph context only",
			task: &answerer.QuestionAnswerTask{
				Topic:    "code.review",
				Question: "Is this pattern correct?",
			},
			graphContext: "Pattern: Repository\nUsage: data access layer",
			wantContains: []string{
				"code.review",
				"Is this pattern correct?",
				"Codebase Context",
				"Repository",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &Component{}
			prompt := c.buildPromptWithContext(tt.task, tt.graphContext)

			if prompt == "" {
				t.Fatal("expected non-empty prompt")
			}

			for _, want := range tt.wantContains {
				if !contains(prompt, want) {
					t.Errorf("prompt missing %q\nGot:\n%s", want, prompt)
				}
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "valid default config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "missing stream name",
			config: Config{
				StreamName:   "",
				ConsumerName: "test",
				TaskSubject:  "test",
			},
			wantErr: true,
		},
		{
			name: "missing consumer name",
			config: Config{
				StreamName:   "test",
				ConsumerName: "",
				TaskSubject:  "test",
			},
			wantErr: true,
		},
		{
			name: "missing task subject",
			config: Config{
				StreamName:   "test",
				ConsumerName: "test",
				TaskSubject:  "",
			},
			wantErr: true,
		},
		{
			name: "all required fields present",
			config: Config{
				StreamName:   "AGENT",
				ConsumerName: "qa-consumer",
				TaskSubject:  "agent.task.qa",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()

	if config.StreamName != "AGENT" {
		t.Errorf("StreamName = %q, want %q", config.StreamName, "AGENT")
	}
	if config.ConsumerName != "question-answerer" {
		t.Errorf("ConsumerName = %q, want %q", config.ConsumerName, "question-answerer")
	}
	if config.TaskSubject != "agent.task.question-answerer" {
		t.Errorf("TaskSubject = %q, want %q", config.TaskSubject, "agent.task.question-answerer")
	}
	if config.DefaultCapability != "planning" {
		t.Errorf("DefaultCapability = %q, want %q", config.DefaultCapability, "planning")
	}
	if config.ContextSubjectPrefix != "context.build" {
		t.Errorf("ContextSubjectPrefix = %q, want %q", config.ContextSubjectPrefix, "context.build")
	}
	if config.ContextResponseBucket != "CONTEXT_RESPONSES" {
		t.Errorf("ContextResponseBucket = %q, want %q", config.ContextResponseBucket, "CONTEXT_RESPONSES")
	}
	if config.ContextTimeout != "30s" {
		t.Errorf("ContextTimeout = %q, want %q", config.ContextTimeout, "30s")
	}
	if config.Ports == nil {
		t.Error("Ports should not be nil")
	}
	if len(config.Ports.Inputs) != 1 {
		t.Errorf("Ports.Inputs length = %d, want 1", len(config.Ports.Inputs))
	}
	if len(config.Ports.Outputs) != 1 {
		t.Errorf("Ports.Outputs length = %d, want 1", len(config.Ports.Outputs))
	}
}

func TestGetContextTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		timeout string
		want    time.Duration
	}{
		{
			name:    "valid duration string",
			timeout: "30s",
			want:    30 * time.Second,
		},
		{
			name:    "valid minute duration",
			timeout: "2m",
			want:    2 * time.Minute,
		},
		{
			name:    "empty string defaults to 30s",
			timeout: "",
			want:    30 * time.Second,
		},
		{
			name:    "invalid duration defaults to 30s",
			timeout: "invalid",
			want:    30 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			config := Config{
				ContextTimeout: tt.timeout,
			}

			got := config.GetContextTimeout()
			if got != tt.want {
				t.Errorf("GetContextTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMeta(t *testing.T) {
	t.Parallel()

	c := &Component{}
	meta := c.Meta()

	if meta.Name != "question-answerer" {
		t.Errorf("Name = %q, want %q", meta.Name, "question-answerer")
	}
	if meta.Type != "processor" {
		t.Errorf("Type = %q, want %q", meta.Type, "processor")
	}
	if meta.Version != "0.1.0" {
		t.Errorf("Version = %q, want %q", meta.Version, "0.1.0")
	}
	if meta.Description == "" {
		t.Error("Description should not be empty")
	}
}

func TestHealth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		running       bool
		answersFailed int64
		wantHealthy   bool
		wantStatus    string
	}{
		{
			name:          "running and healthy",
			running:       true,
			answersFailed: 0,
			wantHealthy:   true,
			wantStatus:    "running",
		},
		{
			name:          "running with errors",
			running:       true,
			answersFailed: 5,
			wantHealthy:   true,
			wantStatus:    "running",
		},
		{
			name:          "stopped",
			running:       false,
			answersFailed: 0,
			wantHealthy:   false,
			wantStatus:    "stopped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &Component{
				running:   tt.running,
				startTime: time.Now().Add(-1 * time.Hour),
			}
			c.answersFailed.Store(tt.answersFailed)

			health := c.Health()

			if health.Healthy != tt.wantHealthy {
				t.Errorf("Healthy = %v, want %v", health.Healthy, tt.wantHealthy)
			}
			if health.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", health.Status, tt.wantStatus)
			}
			if health.ErrorCount != int(tt.answersFailed) {
				t.Errorf("ErrorCount = %d, want %d", health.ErrorCount, tt.answersFailed)
			}
		})
	}
}

func TestTraceContextPassedThroughContextBuilder(t *testing.T) {
	t.Parallel()

	// Test that trace context is preserved when going through the context builder
	traceID := "ctx-trace-123"
	loopID := "ctx-loop-456"

	task := &answerer.QuestionAnswerTask{
		TaskID:     "task-ctx",
		QuestionID: "q-ctx",
		Topic:      "api.design",
		Question:   "How to structure API?",
		AgentName:  "architect",
		TraceID:    traceID,
		LoopID:     loopID,
	}

	mockClient := &testutil.MockLLMClient{
		Responses: []*llm.Response{
			{
				Content:    "Use RESTful design principles.",
				Model:      "test-model",
				TokensUsed: 60,
			},
		},
	}

	c := &Component{
		llmClient: mockClient,
		config:    DefaultConfig(),
		logger:    slog.Default(),
	}

	// Create context with trace info
	ctx := llm.WithTraceContext(context.Background(), llm.TraceContext{
		TraceID: traceID,
		LoopID:  loopID,
	})

	_, err := c.generateAnswer(ctx, task)
	if err != nil {
		t.Fatalf("generateAnswer() error = %v", err)
	}

	// Verify trace context was preserved in LLM call
	capturedCtx := mockClient.GetCapturedContext()
	if capturedCtx == nil {
		t.Fatal("LLM client was not called")
	}

	tc := llm.GetTraceContext(capturedCtx)
	if tc.TraceID != traceID {
		t.Errorf("TraceID = %q, want %q", tc.TraceID, traceID)
	}
	if tc.LoopID != loopID {
		t.Errorf("LoopID = %q, want %q", tc.LoopID, loopID)
	}
}

// contains is a helper function to check if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || indexOf(s, substr) >= 0)
}

// indexOf returns the index of substr in s, or -1 if not found.
func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
