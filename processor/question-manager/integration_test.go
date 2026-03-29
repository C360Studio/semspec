//go:build integration

package questionmanager

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/natsclient"
)

// newIntegrationComponent creates a Component backed by a real NATS testcontainer.
// The QUESTIONS KV bucket is created automatically by NewQuestionStore.
func newIntegrationComponent(t *testing.T) *Component {
	t.Helper()
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())

	store, err := workflow.NewQuestionStore(tc.Client)
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}

	return &Component{
		config:     Config{Bucket: workflow.QuestionsBucket},
		natsClient: tc.Client,
		logger:     slog.Default(),
		store:      store,
		running:    true,
	}
}

// seedQuestion stores a question and returns it.
func seedQuestion(t *testing.T, c *Component, q *workflow.Question) *workflow.Question {
	t.Helper()
	if err := c.store.Store(context.Background(), q); err != nil {
		t.Fatalf("store.Store(%s): %v", q.ID, err)
	}
	return q
}

// ---------------------------------------------------------------------------
// handleList — store-backed
// ---------------------------------------------------------------------------

func TestIntegration_HandleList_Success(t *testing.T) {
	c := newIntegrationComponent(t)
	ctx := context.Background()

	seedQuestion(t, c, &workflow.Question{
		ID: "q-list-1", FromAgent: "a1", Topic: "api.auth", Question: "Q1",
		Status: workflow.QuestionStatusPending, Urgency: workflow.QuestionUrgencyNormal, CreatedAt: time.Now().UTC(),
	})
	seedQuestion(t, c, &workflow.Question{
		ID: "q-list-2", FromAgent: "a2", Topic: "api.users", Question: "Q2",
		Status: workflow.QuestionStatusPending, Urgency: workflow.QuestionUrgencyHigh, CreatedAt: time.Now().UTC(),
	})

	req := httptest.NewRequest(http.MethodGet, "/questions/?status=pending", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	c.handleList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	total := int(resp["total"].(float64))
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
}

func TestIntegration_HandleList_InvalidStatus(t *testing.T) {
	c := newIntegrationComponent(t)

	req := httptest.NewRequest(http.MethodGet, "/questions/?status=invalid", nil)
	w := httptest.NewRecorder()
	c.handleList(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestIntegration_HandleList_InvalidLimit(t *testing.T) {
	c := newIntegrationComponent(t)

	for _, limit := range []string{"abc", "0", "-1", "5000"} {
		t.Run("limit="+limit, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/questions/?limit="+limit, nil)
			w := httptest.NewRecorder()
			c.handleList(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestIntegration_HandleList_TopicFilter(t *testing.T) {
	c := newIntegrationComponent(t)

	seedQuestion(t, c, &workflow.Question{
		ID: "q-topic-1", FromAgent: "a1", Topic: "api.users.create", Question: "Q1",
		Status: workflow.QuestionStatusPending, Urgency: workflow.QuestionUrgencyNormal, CreatedAt: time.Now().UTC(),
	})
	seedQuestion(t, c, &workflow.Question{
		ID: "q-topic-2", FromAgent: "a2", Topic: "requirements.auth", Question: "Q2",
		Status: workflow.QuestionStatusPending, Urgency: workflow.QuestionUrgencyNormal, CreatedAt: time.Now().UTC(),
	})

	req := httptest.NewRequest(http.MethodGet, "/questions/?status=all&topic=api.>", nil)
	w := httptest.NewRecorder()
	c.handleList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	total := int(resp["total"].(float64))
	if total != 1 {
		t.Errorf("total = %d, want 1 (only api.users.create matches api.>)", total)
	}
}

func TestIntegration_HandleList_CategoryFilter(t *testing.T) {
	c := newIntegrationComponent(t)

	seedQuestion(t, c, &workflow.Question{
		ID: "q-cat-1", FromAgent: "a1", Topic: "t1", Question: "Q1",
		Category: workflow.QuestionCategoryKnowledge,
		Status: workflow.QuestionStatusPending, Urgency: workflow.QuestionUrgencyNormal, CreatedAt: time.Now().UTC(),
	})
	seedQuestion(t, c, &workflow.Question{
		ID: "q-cat-2", FromAgent: "a2", Topic: "t2", Question: "Q2",
		Category: workflow.QuestionCategoryDecision,
		Status: workflow.QuestionStatusPending, Urgency: workflow.QuestionUrgencyNormal, CreatedAt: time.Now().UTC(),
	})

	req := httptest.NewRequest(http.MethodGet, "/questions/?status=all&category=decision", nil)
	w := httptest.NewRecorder()
	c.handleList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	total := int(resp["total"].(float64))
	if total != 1 {
		t.Errorf("total = %d, want 1 (only decision category)", total)
	}
}

func TestIntegration_HandleList_LimitTruncates(t *testing.T) {
	c := newIntegrationComponent(t)

	for i := 0; i < 5; i++ {
		seedQuestion(t, c, &workflow.Question{
			ID: "q-lim-" + string(rune('a'+i)), FromAgent: "a1", Topic: "t1", Question: "Q",
			Status: workflow.QuestionStatusPending, Urgency: workflow.QuestionUrgencyNormal, CreatedAt: time.Now().UTC(),
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/questions/?status=pending&limit=2", nil)
	w := httptest.NewRecorder()
	c.handleList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)

	questions := resp["questions"].([]any)
	total := int(resp["total"].(float64))
	if len(questions) != 2 {
		t.Errorf("returned %d questions, want 2 (limit)", len(questions))
	}
	if total != 5 {
		t.Errorf("total = %d, want 5 (all matching)", total)
	}
}

// ---------------------------------------------------------------------------
// handleGet — store-backed
// ---------------------------------------------------------------------------

func TestIntegration_HandleGet_Success(t *testing.T) {
	c := newIntegrationComponent(t)

	seedQuestion(t, c, &workflow.Question{
		ID: "q-get-ok", FromAgent: "a1", Topic: "api.auth", Question: "What scope?",
		Status: workflow.QuestionStatusPending, Urgency: workflow.QuestionUrgencyNormal, CreatedAt: time.Now().UTC(),
	})

	req := httptest.NewRequest(http.MethodGet, "/questions/q-get-ok", nil)
	w := httptest.NewRecorder()
	c.handleGet(w, req, "q-get-ok")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var q workflow.Question
	json.NewDecoder(w.Body).Decode(&q)
	if q.ID != "q-get-ok" {
		t.Errorf("ID = %q, want q-get-ok", q.ID)
	}
	if q.Question != "What scope?" {
		t.Errorf("Question = %q, want 'What scope?'", q.Question)
	}
}

func TestIntegration_HandleGet_NotFound(t *testing.T) {
	c := newIntegrationComponent(t)

	req := httptest.NewRequest(http.MethodGet, "/questions/q-nonexistent", nil)
	w := httptest.NewRecorder()
	c.handleGet(w, req, "q-nonexistent")

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// ---------------------------------------------------------------------------
// handleAnswer — store-backed
// ---------------------------------------------------------------------------

func TestIntegration_HandleAnswer_Success(t *testing.T) {
	c := newIntegrationComponent(t)

	seedQuestion(t, c, &workflow.Question{
		ID: "q-ans-ok", FromAgent: "a1", Topic: "api.auth", Question: "What scope?",
		Status: workflow.QuestionStatusPending, Urgency: workflow.QuestionUrgencyNormal, CreatedAt: time.Now().UTC(),
	})

	body := strings.NewReader(`{"answer":"Limited to user auth","confidence":"high","sources":"ADR-001"}`)
	req := httptest.NewRequest(http.MethodPost, "/questions/q-ans-ok/answer", body)
	req.Header.Set("X-User-ID", "test-user")
	w := httptest.NewRecorder()
	c.handleAnswer(w, req, "q-ans-ok")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var q workflow.Question
	json.NewDecoder(w.Body).Decode(&q)
	if q.Status != workflow.QuestionStatusAnswered {
		t.Errorf("Status = %q, want answered", q.Status)
	}
	if q.Answer != "Limited to user auth" {
		t.Errorf("Answer = %q, want 'Limited to user auth'", q.Answer)
	}
	if q.AnsweredBy != "test-user" {
		t.Errorf("AnsweredBy = %q, want test-user", q.AnsweredBy)
	}
	if q.AnswererType != "human" {
		t.Errorf("AnswererType = %q, want human", q.AnswererType)
	}
	if q.Confidence != "high" {
		t.Errorf("Confidence = %q, want high", q.Confidence)
	}

	// Verify persisted in KV.
	persisted, err := c.store.Get(context.Background(), "q-ans-ok")
	if err != nil {
		t.Fatalf("store.Get after answer: %v", err)
	}
	if persisted.Status != workflow.QuestionStatusAnswered {
		t.Errorf("persisted Status = %q, want answered", persisted.Status)
	}
}

func TestIntegration_HandleAnswer_AlreadyAnswered(t *testing.T) {
	c := newIntegrationComponent(t)
	now := time.Now().UTC()

	seedQuestion(t, c, &workflow.Question{
		ID: "q-already", FromAgent: "a1", Topic: "t1", Question: "Q?",
		Status: workflow.QuestionStatusAnswered, Urgency: workflow.QuestionUrgencyNormal,
		CreatedAt: now, Answer: "old", AnsweredBy: "prev", AnsweredAt: &now,
	})

	body := strings.NewReader(`{"answer":"new answer"}`)
	req := httptest.NewRequest(http.MethodPost, "/questions/q-already/answer", body)
	w := httptest.NewRecorder()
	c.handleAnswer(w, req, "q-already")

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestIntegration_HandleAnswer_NotFound(t *testing.T) {
	c := newIntegrationComponent(t)

	body := strings.NewReader(`{"answer":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/questions/q-missing/answer", body)
	w := httptest.NewRecorder()
	c.handleAnswer(w, req, "q-missing")

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestIntegration_HandleAnswer_DefaultAnonymous(t *testing.T) {
	c := newIntegrationComponent(t)

	seedQuestion(t, c, &workflow.Question{
		ID: "q-anon", FromAgent: "a1", Topic: "t1", Question: "Q?",
		Status: workflow.QuestionStatusPending, Urgency: workflow.QuestionUrgencyNormal, CreatedAt: time.Now().UTC(),
	})

	body := strings.NewReader(`{"answer":"yes"}`)
	req := httptest.NewRequest(http.MethodPost, "/questions/q-anon/answer", body)
	// No X-User-ID header → should default to "anonymous"
	w := httptest.NewRecorder()
	c.handleAnswer(w, req, "q-anon")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var q workflow.Question
	json.NewDecoder(w.Body).Decode(&q)
	if q.AnsweredBy != "anonymous" {
		t.Errorf("AnsweredBy = %q, want anonymous", q.AnsweredBy)
	}
}

func TestIntegration_HandleAnswer_WithValidAction(t *testing.T) {
	c := newIntegrationComponent(t)

	seedQuestion(t, c, &workflow.Question{
		ID: "q-action", FromAgent: "a1", Topic: "t1", Question: "Need package?",
		Status: workflow.QuestionStatusPending, Urgency: workflow.QuestionUrgencyNormal, CreatedAt: time.Now().UTC(),
	})

	body := strings.NewReader(`{"answer":"yes","action":{"type":"install_package","parameters":{"name":"zap"}}}`)
	req := httptest.NewRequest(http.MethodPost, "/questions/q-action/answer", body)
	w := httptest.NewRecorder()
	c.handleAnswer(w, req, "q-action")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var q workflow.Question
	json.NewDecoder(w.Body).Decode(&q)
	if q.Action == nil {
		t.Fatal("Action should not be nil")
	}
	if q.Action.Type != "install_package" {
		t.Errorf("Action.Type = %q, want install_package", q.Action.Type)
	}
}

func TestIntegration_HandleAnswer_InvalidAction(t *testing.T) {
	c := newIntegrationComponent(t)

	seedQuestion(t, c, &workflow.Question{
		ID: "q-bad-action", FromAgent: "a1", Topic: "t1", Question: "Q?",
		Status: workflow.QuestionStatusPending, Urgency: workflow.QuestionUrgencyNormal, CreatedAt: time.Now().UTC(),
	})

	body := strings.NewReader(`{"answer":"yes","action":{"type":"unknown_action"}}`)
	req := httptest.NewRequest(http.MethodPost, "/questions/q-bad-action/answer", body)
	w := httptest.NewRecorder()
	c.handleAnswer(w, req, "q-bad-action")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if !strings.Contains(resp["error"], "invalid action") {
		t.Errorf("error = %q, want to contain 'invalid action'", resp["error"])
	}
}

// ---------------------------------------------------------------------------
// Start with real NATS
// ---------------------------------------------------------------------------

func TestIntegration_Start(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	c := &Component{
		config:     Config{Bucket: workflow.QuestionsBucket},
		natsClient: tc.Client,
		logger:     slog.Default(),
	}

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if !c.running {
		t.Error("running should be true after Start()")
	}
	if c.store == nil {
		t.Error("store should be non-nil after Start()")
	}

	// Double-start should be no-op.
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("double Start() error: %v", err)
	}
}
