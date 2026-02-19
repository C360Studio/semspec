package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
)

func TestMatchTopic(t *testing.T) {
	tests := []struct {
		name    string
		topic   string
		pattern string
		want    bool
	}{
		{
			name:    "exact match",
			topic:   "requirements.scope",
			pattern: "requirements.scope",
			want:    true,
		},
		{
			name:    "no match",
			topic:   "requirements.scope",
			pattern: "requirements.design",
			want:    false,
		},
		{
			name:    "prefix match",
			topic:   "requirements.scope.auth",
			pattern: "requirements.scope",
			want:    true,
		},
		{
			name:    "single wildcard match",
			topic:   "requirements.scope",
			pattern: "requirements.*",
			want:    true,
		},
		{
			name:    "single wildcard no match",
			topic:   "design.scope",
			pattern: "requirements.*",
			want:    false,
		},
		{
			name:    "multi wildcard match",
			topic:   "requirements.scope.auth.login",
			pattern: "requirements.>",
			want:    true,
		},
		{
			name:    "multi wildcard at end",
			topic:   "api.semstreams.loop",
			pattern: "api.>",
			want:    true,
		},
		{
			name:    "mixed wildcards",
			topic:   "api.users.create",
			pattern: "api.*.>",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchTopic(tt.topic, tt.pattern)
			if got != tt.want {
				t.Errorf("matchTopic(%q, %q) = %v, want %v", tt.topic, tt.pattern, got, tt.want)
			}
		})
	}
}

func TestQuestionHTTPHandler_HandleList(t *testing.T) {
	// This test requires a real NATS connection, so we test the handler logic
	// in isolation where possible

	t.Run("invalid status returns error", func(t *testing.T) {
		// Create a mock handler that doesn't need real NATS
		h := &QuestionHTTPHandler{}

		// Create request with invalid status
		req := httptest.NewRequest(http.MethodGet, "/questions?status=invalid", nil)
		w := httptest.NewRecorder()

		h.handleList(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
		}

		var resp map[string]string
		json.NewDecoder(w.Body).Decode(&resp)
		if !strings.Contains(resp["error"], "invalid status") {
			t.Errorf("expected error about invalid status, got %q", resp["error"])
		}
	})

	t.Run("invalid limit returns error", func(t *testing.T) {
		h := &QuestionHTTPHandler{}

		req := httptest.NewRequest(http.MethodGet, "/questions?limit=invalid", nil)
		w := httptest.NewRecorder()

		h.handleList(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
		}
	})

	t.Run("limit out of range returns error", func(t *testing.T) {
		h := &QuestionHTTPHandler{}

		req := httptest.NewRequest(http.MethodGet, "/questions?limit=5000", nil)
		w := httptest.NewRecorder()

		h.handleList(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
		}
	})
}

func TestQuestionHTTPHandler_HandleGet(t *testing.T) {
	t.Run("missing ID returns error", func(t *testing.T) {
		h := &QuestionHTTPHandler{}

		req := httptest.NewRequest(http.MethodGet, "/questions/", nil)
		w := httptest.NewRecorder()

		h.handleGet(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
		}
	})

	t.Run("invalid ID format returns error", func(t *testing.T) {
		h := &QuestionHTTPHandler{}

		req := httptest.NewRequest(http.MethodGet, "/questions/invalid-id", nil)
		req.SetPathValue("id", "invalid-id")
		w := httptest.NewRecorder()

		h.handleGet(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
		}

		var resp map[string]string
		json.NewDecoder(w.Body).Decode(&resp)
		if !strings.Contains(resp["error"], "must start with 'q-'") {
			t.Errorf("expected error about ID format, got %q", resp["error"])
		}
	})
}

func TestQuestionHTTPHandler_HandleAnswer(t *testing.T) {
	t.Run("missing ID returns error", func(t *testing.T) {
		h := &QuestionHTTPHandler{}

		req := httptest.NewRequest(http.MethodPost, "/questions//answer", strings.NewReader(`{"answer":"test"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.handleAnswer(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
		}
	})

	t.Run("invalid ID format returns error", func(t *testing.T) {
		h := &QuestionHTTPHandler{}

		req := httptest.NewRequest(http.MethodPost, "/questions/invalid-id/answer", strings.NewReader(`{"answer":"test"}`))
		req.SetPathValue("id", "invalid-id")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.handleAnswer(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
		}
	})

	t.Run("empty answer returns error", func(t *testing.T) {
		h := &QuestionHTTPHandler{}

		req := httptest.NewRequest(http.MethodPost, "/questions/q-abc123/answer", strings.NewReader(`{"answer":""}`))
		req.SetPathValue("id", "q-abc123")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.handleAnswer(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
		}

		var resp map[string]string
		json.NewDecoder(w.Body).Decode(&resp)
		if !strings.Contains(resp["error"], "answer is required") {
			t.Errorf("expected error about answer required, got %q", resp["error"])
		}
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		h := &QuestionHTTPHandler{}

		req := httptest.NewRequest(http.MethodPost, "/questions/q-abc123/answer", strings.NewReader(`not json`))
		req.SetPathValue("id", "q-abc123")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.handleAnswer(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
		}
	})
}

func TestDetermineEventType(t *testing.T) {
	h := &QuestionHTTPHandler{}

	t.Run("new question is question_created", func(t *testing.T) {
		current := &Question{Status: QuestionStatusPending}
		eventType := h.determineEventType(current, nil)
		if eventType != SSEEventQuestionCreated {
			t.Errorf("expected %s, got %s", SSEEventQuestionCreated, eventType)
		}
	})

	t.Run("pending to answered is question_answered", func(t *testing.T) {
		previous := &Question{Status: QuestionStatusPending}
		current := &Question{Status: QuestionStatusAnswered}
		eventType := h.determineEventType(current, previous)
		if eventType != SSEEventQuestionAnswered {
			t.Errorf("expected %s, got %s", SSEEventQuestionAnswered, eventType)
		}
	})

	t.Run("pending to timeout is question_timeout", func(t *testing.T) {
		previous := &Question{Status: QuestionStatusPending}
		current := &Question{Status: QuestionStatusTimeout}
		eventType := h.determineEventType(current, previous)
		if eventType != SSEEventQuestionTimeout {
			t.Errorf("expected %s, got %s", SSEEventQuestionTimeout, eventType)
		}
	})

	t.Run("same status is question_created", func(t *testing.T) {
		previous := &Question{Status: QuestionStatusPending}
		current := &Question{Status: QuestionStatusPending}
		eventType := h.determineEventType(current, previous)
		if eventType != SSEEventQuestionCreated {
			t.Errorf("expected %s, got %s", SSEEventQuestionCreated, eventType)
		}
	})
}

func TestQuestion_Lifecycle(t *testing.T) {
	// Test the Question struct lifecycle
	t.Run("NewQuestion creates valid question", func(t *testing.T) {
		q := NewQuestion("test-agent", "requirements.scope", "What is the scope?", "Need clarification")

		if !strings.HasPrefix(q.ID, "q-") {
			t.Errorf("expected ID to start with 'q-', got %s", q.ID)
		}
		if q.FromAgent != "test-agent" {
			t.Errorf("expected FromAgent 'test-agent', got %s", q.FromAgent)
		}
		if q.Topic != "requirements.scope" {
			t.Errorf("expected Topic 'requirements.scope', got %s", q.Topic)
		}
		if q.Question != "What is the scope?" {
			t.Errorf("expected Question 'What is the scope?', got %s", q.Question)
		}
		if q.Context != "Need clarification" {
			t.Errorf("expected Context 'Need clarification', got %s", q.Context)
		}
		if q.Status != QuestionStatusPending {
			t.Errorf("expected Status 'pending', got %s", q.Status)
		}
		if q.Urgency != QuestionUrgencyNormal {
			t.Errorf("expected Urgency 'normal', got %s", q.Urgency)
		}
		if q.CreatedAt.IsZero() {
			t.Error("expected CreatedAt to be set")
		}
	})
}

func TestListQuestionsResponse_JSON(t *testing.T) {
	now := time.Now().UTC()
	resp := ListQuestionsResponse{
		Questions: []*Question{
			{
				ID:        "q-abc123",
				FromAgent: "test",
				Topic:     "requirements",
				Question:  "Test?",
				Status:    QuestionStatusPending,
				Urgency:   QuestionUrgencyNormal,
				CreatedAt: now,
			},
		},
		Total: 1,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ListQuestionsResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Total != 1 {
		t.Errorf("expected Total 1, got %d", decoded.Total)
	}
	if len(decoded.Questions) != 1 {
		t.Errorf("expected 1 question, got %d", len(decoded.Questions))
	}
	if decoded.Questions[0].ID != "q-abc123" {
		t.Errorf("expected ID 'q-abc123', got %s", decoded.Questions[0].ID)
	}
}

func TestAnswerRequest_JSON(t *testing.T) {
	req := AnswerRequest{
		Answer:     "The scope is limited to auth.",
		Confidence: "high",
		Sources:    "ADR-001",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded AnswerRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Answer != req.Answer {
		t.Errorf("expected Answer %q, got %q", req.Answer, decoded.Answer)
	}
	if decoded.Confidence != req.Confidence {
		t.Errorf("expected Confidence %q, got %q", req.Confidence, decoded.Confidence)
	}
	if decoded.Sources != req.Sources {
		t.Errorf("expected Sources %q, got %q", req.Sources, decoded.Sources)
	}
}

func TestWriteError(t *testing.T) {
	h := &QuestionHTTPHandler{}
	w := httptest.NewRecorder()

	h.writeError(w, http.StatusBadRequest, "test error")

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", w.Header().Get("Content-Type"))
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "test error" {
		t.Errorf("expected error 'test error', got %q", resp["error"])
	}
}

func TestWriteJSON(t *testing.T) {
	h := &QuestionHTTPHandler{}
	w := httptest.NewRecorder()

	data := map[string]string{"status": "ok"}
	h.writeJSON(w, http.StatusOK, data)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", w.Header().Get("Content-Type"))
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Errorf("expected status 'ok', got %q", resp["status"])
	}
}

// Integration test helper for SSE streaming
// This requires real NATS and would be run in E2E tests
func TestSSEHeaders(t *testing.T) {
	// Verify SSE constants are defined correctly
	if SSEEventQuestionCreated != "question_created" {
		t.Errorf("expected SSEEventQuestionCreated to be 'question_created'")
	}
	if SSEEventQuestionAnswered != "question_answered" {
		t.Errorf("expected SSEEventQuestionAnswered to be 'question_answered'")
	}
	if SSEEventQuestionTimeout != "question_timeout" {
		t.Errorf("expected SSEEventQuestionTimeout to be 'question_timeout'")
	}
	if SSEEventHeartbeat != "heartbeat" {
		t.Errorf("expected SSEEventHeartbeat to be 'heartbeat'")
	}
}

// Benchmark topic matching
func BenchmarkMatchTopic(b *testing.B) {
	topic := "api.semstreams.loop.info"
	pattern := "api.*.>"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matchTopic(topic, pattern)
	}
}

// Verify RegisterHTTPHandlers doesn't panic
func TestRegisterHTTPHandlers(t *testing.T) {
	h := &QuestionHTTPHandler{}
	mux := http.NewServeMux()

	// Should not panic
	h.RegisterHTTPHandlers("/questions", mux)
	t.Log("RegisterHTTPHandlers registered without panic")
}

// TestMatchTopicEdgeCases tests edge cases in topic matching
func TestMatchTopicEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		topic   string
		pattern string
		want    bool
	}{
		{
			name:    "empty topic",
			topic:   "",
			pattern: "requirements.*",
			want:    false,
		},
		{
			name:    "empty pattern",
			topic:   "requirements.scope",
			pattern: "",
			want:    false,
		},
		{
			name:    "single char topic",
			topic:   "a",
			pattern: "a",
			want:    true,
		},
		{
			name:    "single char pattern mismatch",
			topic:   "a",
			pattern: "b",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchTopic(tt.topic, tt.pattern)
			if got != tt.want {
				t.Errorf("matchTopic(%q, %q) = %v, want %v", tt.topic, tt.pattern, got, tt.want)
			}
		})
	}
}

// TestLogHelperReturnsDefault verifies log() returns default logger when nil
func TestLogHelperReturnsDefault(t *testing.T) {
	h := &QuestionHTTPHandler{} // nil logger
	logger := h.log()
	if logger == nil {
		t.Error("expected non-nil logger from log()")
	}
}

// ============================================================================
// Integration Tests (require embedded NATS via testcontainers)
// ============================================================================

func TestHandleList_Success(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	handler, err := NewQuestionHTTPHandler(tc.Client, nil)
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	// Store some test questions
	questions := []*Question{
		{ID: "q-list-success-1", FromAgent: "agent-1", Topic: "topic.a", Question: "Q1", Status: QuestionStatusPending, Urgency: QuestionUrgencyNormal, CreatedAt: time.Now().UTC()},
		{ID: "q-list-success-2", FromAgent: "agent-2", Topic: "topic.b", Question: "Q2", Status: QuestionStatusPending, Urgency: QuestionUrgencyHigh, CreatedAt: time.Now().UTC()},
	}

	for _, q := range questions {
		if err := handler.store.Store(ctx, q); err != nil {
			t.Fatalf("Store() error = %v", err)
		}
	}

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/questions?status=pending", nil)
	w := httptest.NewRecorder()

	handler.handleList(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp ListQuestionsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(resp.Questions) != 2 {
		t.Errorf("expected 2 questions, got %d", len(resp.Questions))
	}
	if resp.Total != 2 {
		t.Errorf("expected total 2, got %d", resp.Total)
	}
}

func TestHandleList_WithTopicFilter(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	handler, err := NewQuestionHTTPHandler(tc.Client, nil)
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	// Store questions with different topics
	questions := []*Question{
		{ID: "q-topic-1", FromAgent: "agent-1", Topic: "api.users.create", Question: "Q1", Status: QuestionStatusPending, Urgency: QuestionUrgencyNormal, CreatedAt: time.Now().UTC()},
		{ID: "q-topic-2", FromAgent: "agent-2", Topic: "api.users.delete", Question: "Q2", Status: QuestionStatusPending, Urgency: QuestionUrgencyNormal, CreatedAt: time.Now().UTC()},
		{ID: "q-topic-3", FromAgent: "agent-3", Topic: "requirements.auth", Question: "Q3", Status: QuestionStatusPending, Urgency: QuestionUrgencyNormal, CreatedAt: time.Now().UTC()},
	}

	for _, q := range questions {
		if err := handler.store.Store(ctx, q); err != nil {
			t.Fatalf("Store() error = %v", err)
		}
	}

	// Request with topic filter using wildcard
	req := httptest.NewRequest(http.MethodGet, "/questions?status=all&topic=api.users.*", nil)
	w := httptest.NewRecorder()

	handler.handleList(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp ListQuestionsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Should only return questions matching api.users.*
	if len(resp.Questions) != 2 {
		t.Errorf("expected 2 questions matching api.users.*, got %d", len(resp.Questions))
	}
}

func TestHandleGet_Success(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	handler, err := NewQuestionHTTPHandler(tc.Client, nil)
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	// Store a test question
	q := &Question{
		ID:        "q-get-success-123",
		FromAgent: "test-agent",
		Topic:     "requirements.scope",
		Question:  "What is the scope?",
		Context:   "Test context",
		Status:    QuestionStatusPending,
		Urgency:   QuestionUrgencyNormal,
		CreatedAt: time.Now().UTC(),
	}
	if err := handler.store.Store(ctx, q); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/questions/q-get-success-123", nil)
	req.SetPathValue("id", "q-get-success-123")
	w := httptest.NewRecorder()

	handler.handleGet(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var got Question
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if got.ID != q.ID {
		t.Errorf("expected ID %q, got %q", q.ID, got.ID)
	}
	if got.FromAgent != q.FromAgent {
		t.Errorf("expected FromAgent %q, got %q", q.FromAgent, got.FromAgent)
	}
	if got.Topic != q.Topic {
		t.Errorf("expected Topic %q, got %q", q.Topic, got.Topic)
	}
}

func TestHandleGet_NotFound(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())

	handler, err := NewQuestionHTTPHandler(tc.Client, nil)
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/questions/q-nonexistent", nil)
	req.SetPathValue("id", "q-nonexistent")
	w := httptest.NewRecorder()

	handler.handleGet(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestHandleAnswer_Success(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	handler, err := NewQuestionHTTPHandler(tc.Client, nil)
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	// Store a pending question
	q := &Question{
		ID:        "q-answer-success-123",
		FromAgent: "test-agent",
		Topic:     "api.auth",
		Question:  "Which auth method?",
		Status:    QuestionStatusPending,
		Urgency:   QuestionUrgencyNormal,
		CreatedAt: time.Now().UTC(),
	}
	if err := handler.store.Store(ctx, q); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	// Create answer request
	answerReq := AnswerRequest{
		Answer:     "Use OAuth 2.0 with PKCE",
		Confidence: "high",
		Sources:    "RFC 7636",
	}
	body, _ := json.Marshal(answerReq)

	req := httptest.NewRequest(http.MethodPost, "/questions/q-answer-success-123/answer", strings.NewReader(string(body)))
	req.SetPathValue("id", "q-answer-success-123")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", "test-user")
	w := httptest.NewRecorder()

	handler.handleAnswer(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var got Question
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if got.Status != QuestionStatusAnswered {
		t.Errorf("expected status %q, got %q", QuestionStatusAnswered, got.Status)
	}
	if got.Answer != answerReq.Answer {
		t.Errorf("expected answer %q, got %q", answerReq.Answer, got.Answer)
	}
	if got.Confidence != answerReq.Confidence {
		t.Errorf("expected confidence %q, got %q", answerReq.Confidence, got.Confidence)
	}
	if got.AnsweredBy != "test-user" {
		t.Errorf("expected answered_by %q, got %q", "test-user", got.AnsweredBy)
	}
	if got.AnsweredAt == nil {
		t.Error("expected answered_at to be set")
	}
}

func TestHandleAnswer_AlreadyAnswered(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	handler, err := NewQuestionHTTPHandler(tc.Client, nil)
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	// Store an already-answered question
	now := time.Now().UTC()
	q := &Question{
		ID:           "q-already-answered",
		FromAgent:    "test-agent",
		Topic:        "api.auth",
		Question:     "Which auth method?",
		Status:       QuestionStatusAnswered,
		Urgency:      QuestionUrgencyNormal,
		CreatedAt:    now,
		Answer:       "Existing answer",
		AnsweredBy:   "previous-user",
		AnswererType: "human",
		AnsweredAt:   &now,
	}
	if err := handler.store.Store(ctx, q); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	// Try to answer again
	answerReq := AnswerRequest{Answer: "New answer"}
	body, _ := json.Marshal(answerReq)

	req := httptest.NewRequest(http.MethodPost, "/questions/q-already-answered/answer", strings.NewReader(string(body)))
	req.SetPathValue("id", "q-already-answered")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.handleAnswer(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected status %d (Conflict), got %d", http.StatusConflict, w.Code)
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if !strings.Contains(resp["error"], "already") {
		t.Errorf("expected error about already answered, got %q", resp["error"])
	}
}

func TestHandleAnswer_NotFound(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())

	handler, err := NewQuestionHTTPHandler(tc.Client, nil)
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	answerReq := AnswerRequest{Answer: "Some answer"}
	body, _ := json.Marshal(answerReq)

	req := httptest.NewRequest(http.MethodPost, "/questions/q-nonexistent/answer", strings.NewReader(string(body)))
	req.SetPathValue("id", "q-nonexistent")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.handleAnswer(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestHandleList_WithLimit(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	handler, err := NewQuestionHTTPHandler(tc.Client, nil)
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	// Store 5 questions
	for i := 0; i < 5; i++ {
		q := &Question{
			ID:        fmt.Sprintf("q-limit-%d", i),
			FromAgent: "agent",
			Topic:     "topic",
			Question:  fmt.Sprintf("Q%d", i),
			Status:    QuestionStatusPending,
			Urgency:   QuestionUrgencyNormal,
			CreatedAt: time.Now().UTC(),
		}
		if err := handler.store.Store(ctx, q); err != nil {
			t.Fatalf("Store() error = %v", err)
		}
	}

	// Request with limit=2
	req := httptest.NewRequest(http.MethodGet, "/questions?limit=2", nil)
	w := httptest.NewRecorder()

	handler.handleList(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp ListQuestionsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(resp.Questions) != 2 {
		t.Errorf("expected 2 questions with limit=2, got %d", len(resp.Questions))
	}
	if resp.Total != 5 {
		t.Errorf("expected total 5, got %d", resp.Total)
	}
}

func TestHandleAnswer_DefaultAnsweredBy(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	handler, err := NewQuestionHTTPHandler(tc.Client, nil)
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	// Store a pending question
	q := &Question{
		ID:        "q-default-user",
		FromAgent: "test-agent",
		Topic:     "api.auth",
		Question:  "Test?",
		Status:    QuestionStatusPending,
		Urgency:   QuestionUrgencyNormal,
		CreatedAt: time.Now().UTC(),
	}
	if err := handler.store.Store(ctx, q); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	// Answer without X-User-ID header
	answerReq := AnswerRequest{Answer: "Answer"}
	body, _ := json.Marshal(answerReq)

	req := httptest.NewRequest(http.MethodPost, "/questions/q-default-user/answer", strings.NewReader(string(body)))
	req.SetPathValue("id", "q-default-user")
	req.Header.Set("Content-Type", "application/json")
	// No X-User-ID header
	w := httptest.NewRecorder()

	handler.handleAnswer(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var got Question
	json.NewDecoder(w.Body).Decode(&got)

	if got.AnsweredBy != "anonymous" {
		t.Errorf("expected answered_by 'anonymous', got %q", got.AnsweredBy)
	}
}
