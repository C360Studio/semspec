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
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newTestComponent creates a Component suitable for unit tests.
// NATSClient is nil — publishQuestionEntity is a no-op, store is unset.
func newTestComponent() *Component {
	return &Component{
		config: Config{Bucket: workflow.QuestionsBucket},
		logger: slog.Default(),
	}
}

// newTestQuestion creates a question with required fields populated.
func newTestQuestion(id string) *workflow.Question {
	return &workflow.Question{
		ID:        id,
		FromAgent: "test-agent",
		Topic:     "requirements.scope",
		Question:  "What is the auth scope?",
		Context:   "Need clarification on boundaries",
		Status:    workflow.QuestionStatusPending,
		Urgency:   workflow.QuestionUrgencyNormal,
		CreatedAt: time.Now().UTC(),
	}
}

// decodeJSON is a test helper that decodes a JSON response body.
func decodeJSON[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(w.Body).Decode(&v); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	return v
}

// ---------------------------------------------------------------------------
// Component lifecycle
// ---------------------------------------------------------------------------

func TestMeta(t *testing.T) {
	c := newTestComponent()
	m := c.Meta()
	if m.Name != "question-manager" {
		t.Errorf("Meta().Name = %q, want %q", m.Name, "question-manager")
	}
	if m.Type != "processor" {
		t.Errorf("Meta().Type = %q, want %q", m.Type, "processor")
	}
}

func TestHealth_Stopped(t *testing.T) {
	c := newTestComponent()
	h := c.Health()
	if h.Healthy {
		t.Error("Health().Healthy should be false when not running")
	}
	if h.Status != "stopped" {
		t.Errorf("Health().Status = %q, want %q", h.Status, "stopped")
	}
}

func TestHealth_Running(t *testing.T) {
	c := newTestComponent()
	c.running = true
	h := c.Health()
	if !h.Healthy {
		t.Error("Health().Healthy should be true when running")
	}
	if h.Status != "running" {
		t.Errorf("Health().Status = %q, want %q", h.Status, "running")
	}
}

func TestInitialize(t *testing.T) {
	c := newTestComponent()
	if err := c.Initialize(); err != nil {
		t.Fatalf("Initialize() unexpected error: %v", err)
	}
}

func TestStop(t *testing.T) {
	c := newTestComponent()
	c.running = true
	if err := c.Stop(5 * time.Second); err != nil {
		t.Fatalf("Stop() unexpected error: %v", err)
	}
	if c.running {
		t.Error("running should be false after Stop()")
	}
}

// ---------------------------------------------------------------------------
// HTTP routing
// ---------------------------------------------------------------------------

func TestHandleQuestions_Routing(t *testing.T) {
	c := newTestComponent()
	c.prefix = "/question-manager/questions/"

	t.Run("GET root lists questions", func(t *testing.T) {
		// store is nil → returns empty list
		req := httptest.NewRequest(http.MethodGet, "/question-manager/questions/", nil)
		w := httptest.NewRecorder()
		c.handleQuestions(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})

	t.Run("POST root is method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/question-manager/questions/", nil)
		w := httptest.NewRecorder()
		c.handleQuestions(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
		}
	})

	t.Run("POST answer routes correctly", func(t *testing.T) {
		// store nil → 503
		body := strings.NewReader(`{"answer":"test"}`)
		req := httptest.NewRequest(http.MethodPost, "/question-manager/questions/q-123/answer", body)
		w := httptest.NewRecorder()
		c.handleQuestions(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
		}
	})

	t.Run("GET with id routes to handleGet", func(t *testing.T) {
		// store nil → 503
		req := httptest.NewRequest(http.MethodGet, "/question-manager/questions/q-123", nil)
		w := httptest.NewRecorder()
		c.handleQuestions(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
		}
	})

	t.Run("DELETE is method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/question-manager/questions/q-123", nil)
		w := httptest.NewRecorder()
		c.handleQuestions(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
		}
	})
}

// ---------------------------------------------------------------------------
// handleList
// ---------------------------------------------------------------------------

func TestHandleList_StoreNil_ReturnsEmpty(t *testing.T) {
	c := newTestComponent()
	req := httptest.NewRequest(http.MethodGet, "/questions/", nil)
	w := httptest.NewRecorder()

	c.handleList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	resp := decodeJSON[map[string]any](t, w)
	if resp["total"] != float64(0) {
		t.Errorf("total = %v, want 0", resp["total"])
	}
}

// Note: handleList validation for status/limit params is only reachable when
// store is non-nil (the nil-store check returns an empty 200 first). These
// paths are covered by integration tests with real NATS.

func TestHandleList_ValidStatusParams(t *testing.T) {
	// All valid status values should reach the store (nil → empty list).
	for _, status := range []string{"pending", "answered", "timeout", "all", ""} {
		t.Run("status="+status, func(t *testing.T) {
			c := newTestComponent()
			url := "/questions/"
			if status != "" {
				url += "?status=" + status
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()
			c.handleList(w, req)
			// store nil → returns empty list (200)
			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// handleGet
// ---------------------------------------------------------------------------

func TestHandleGet_StoreNil(t *testing.T) {
	c := newTestComponent()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/questions/q-123", nil)
	c.handleGet(w, req, "q-123")

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleGet_EmptyID(t *testing.T) {
	c := newTestComponent()
	c.store = &workflow.QuestionStore{} // non-nil to pass store check
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/questions/", nil)
	c.handleGet(w, req, "")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleGet_InvalidIDFormat(t *testing.T) {
	c := newTestComponent()
	c.store = &workflow.QuestionStore{} // non-nil to pass store check
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/questions/bad-id", nil)
	c.handleGet(w, req, "bad-id")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	resp := decodeJSON[map[string]string](t, w)
	if !strings.Contains(resp["error"], "must start with 'q-'") {
		t.Errorf("error = %q, want to contain ID format hint", resp["error"])
	}
}

// ---------------------------------------------------------------------------
// handleAnswer — validation paths
// ---------------------------------------------------------------------------

func TestHandleAnswer_StoreNil(t *testing.T) {
	c := newTestComponent()
	body := strings.NewReader(`{"answer":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/questions/q-123/answer", body)
	w := httptest.NewRecorder()
	c.handleAnswer(w, req, "q-123")

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleAnswer_EmptyID(t *testing.T) {
	c := newTestComponent()
	c.store = &workflow.QuestionStore{} // non-nil so we pass the nil check
	body := strings.NewReader(`{"answer":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/questions//answer", body)
	w := httptest.NewRecorder()
	c.handleAnswer(w, req, "")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleAnswer_InvalidIDFormat(t *testing.T) {
	c := newTestComponent()
	c.store = &workflow.QuestionStore{}
	body := strings.NewReader(`{"answer":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/questions/bad-id/answer", body)
	w := httptest.NewRecorder()
	c.handleAnswer(w, req, "bad-id")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleAnswer_EmptyAnswer(t *testing.T) {
	c := newTestComponent()
	c.store = &workflow.QuestionStore{}
	body := strings.NewReader(`{"answer":""}`)
	req := httptest.NewRequest(http.MethodPost, "/questions/q-123/answer", body)
	w := httptest.NewRecorder()
	c.handleAnswer(w, req, "q-123")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	resp := decodeJSON[map[string]string](t, w)
	if !strings.Contains(resp["error"], "answer is required") {
		t.Errorf("error = %q, want 'answer is required'", resp["error"])
	}
}

func TestHandleAnswer_InvalidJSON(t *testing.T) {
	c := newTestComponent()
	c.store = &workflow.QuestionStore{}
	body := strings.NewReader(`not json`)
	req := httptest.NewRequest(http.MethodPost, "/questions/q-123/answer", body)
	w := httptest.NewRecorder()
	c.handleAnswer(w, req, "q-123")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// Note: TestHandleAnswer_InvalidAction requires a working store (store.Get
// is called before action validation). Covered by integration tests.

// ---------------------------------------------------------------------------
// publishQuestionEntity
// ---------------------------------------------------------------------------

func TestPublishQuestionEntity_NilNATSClient(t *testing.T) {
	c := newTestComponent() // natsClient is nil
	q := newTestQuestion("q-minimal")

	// Should return nil (no-op) when NATSClient is nil.
	err := c.publishQuestionEntity(context.Background(), q)
	if err != nil {
		t.Errorf("publishQuestionEntity() = %v, want nil", err)
	}
}

func TestPublishQuestionEntity_FullyPopulated(t *testing.T) {
	c := newTestComponent()
	now := time.Now().UTC()
	q := &workflow.Question{
		ID:            "q-full",
		FromAgent:     "agent-1",
		Topic:         "api.auth",
		Question:      "How do we handle OAuth refresh tokens?",
		Context:       "Building auth middleware",
		Status:        workflow.QuestionStatusAnswered,
		Urgency:       workflow.QuestionUrgencyHigh,
		CreatedAt:     now,
		BlockedLoopID: "loop-abc",
		TraceID:       "trace-xyz",
		PlanSlug:      "auth-plan",
		TaskID:        "task-1",
		PhaseID:       "phase-1",
		AssignedTo:    "human-reviewer",
		Answer:        "Use rotating refresh tokens with 7-day expiry",
		AnsweredBy:    "coby",
		AnswererType:  "human",
		AnsweredAt:    &now,
		Confidence:    "high",
		Sources:       "ADR-001, RFC 6749",
	}

	// Should return nil (no-op) — natsClient is nil so PublishToStream is never called.
	err := c.publishQuestionEntity(context.Background(), q)
	if err != nil {
		t.Errorf("publishQuestionEntity() = %v, want nil", err)
	}
}

// ---------------------------------------------------------------------------
// matchTopic
// ---------------------------------------------------------------------------

func TestMatchTopic(t *testing.T) {
	tests := []struct {
		name    string
		topic   string
		pattern string
		want    bool
	}{
		{"exact match", "requirements.scope", "requirements.scope", true},
		{"no match", "requirements.scope", "requirements.design", false},
		{"prefix match", "requirements.scope.auth", "requirements.scope", true},
		{"single wildcard match", "requirements.scope", "requirements.*", true},
		{"single wildcard no match", "design.scope", "requirements.*", false},
		{"multi wildcard match", "requirements.scope.auth.login", "requirements.>", true},
		{"multi wildcard at end", "api.semstreams.loop", "api.>", true},
		{"mixed wildcards", "api.users.create", "api.*.>", true},
		{"empty topic", "", "requirements.*", false},
		{"empty pattern", "requirements.scope", "", false},
		{"single char exact", "a", "a", true},
		{"single char mismatch", "a", "b", false},
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

// ---------------------------------------------------------------------------
// truncateTitle
// ---------------------------------------------------------------------------

func TestTruncateTitle(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short string", "hello", 100, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"truncated", "hello world this is long", 10, "hello w..."},
		{"unicode safe", "こんにちは世界テスト", 5, "こん..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateTitle(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateTitle(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// JSON / SSE helpers
// ---------------------------------------------------------------------------

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	resp := decodeJSON[map[string]string](t, w)
	if resp["status"] != "ok" {
		t.Errorf("body status = %q, want ok", resp["status"])
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, http.StatusBadRequest, "test error")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	resp := decodeJSON[map[string]string](t, w)
	if resp["error"] != "test error" {
		t.Errorf("error = %q, want 'test error'", resp["error"])
	}
}

func TestSendSSE(t *testing.T) {
	w := httptest.NewRecorder()
	flusher := w // httptest.ResponseRecorder implements Flusher

	err := sendSSE(w, flusher, "test_event", map[string]string{"key": "val"})
	if err != nil {
		t.Fatalf("sendSSE() error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "event: test_event\n") {
		t.Errorf("body missing event line: %q", body)
	}
	if !strings.Contains(body, `"key":"val"`) {
		t.Errorf("body missing data: %q", body)
	}
	// sendSSE with id=0 should not write an id line
	if strings.Contains(body, "id:") {
		t.Errorf("body should not contain id line for id=0: %q", body)
	}
}

func TestSendSSEWithID(t *testing.T) {
	w := httptest.NewRecorder()
	flusher := w

	err := sendSSEWithID(w, flusher, 42, "numbered", map[string]string{"n": "1"})
	if err != nil {
		t.Fatalf("sendSSEWithID() error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "event: numbered\n") {
		t.Errorf("body missing event line: %q", body)
	}
	if !strings.Contains(body, "id: 42\n") {
		t.Errorf("body missing id line: %q", body)
	}
}

// ---------------------------------------------------------------------------
// RegisterHTTPHandlers
// ---------------------------------------------------------------------------

func TestRegisterHTTPHandlers(t *testing.T) {
	c := newTestComponent()
	mux := http.NewServeMux()

	// Should not panic.
	c.RegisterHTTPHandlers("/question-manager", mux)

	if c.prefix != "/question-manager/questions/" {
		t.Errorf("prefix = %q, want /question-manager/questions/", c.prefix)
	}
}

func TestRegisterHTTPHandlers_TrailingSlash(t *testing.T) {
	c := newTestComponent()
	mux := http.NewServeMux()

	c.RegisterHTTPHandlers("/question-manager/", mux)

	if c.prefix != "/question-manager/questions/" {
		t.Errorf("prefix = %q, want /question-manager/questions/", c.prefix)
	}
}
