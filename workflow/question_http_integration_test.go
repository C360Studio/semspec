//go:build integration

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
