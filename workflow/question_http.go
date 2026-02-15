package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// maxAnswerBodySize limits the size of answer request bodies to prevent DoS.
const maxAnswerBodySize = 1 << 20 // 1 MB

// QuestionHTTPHandler provides HTTP endpoints for Q&A operations.
// Implements REST endpoints for listing, viewing, and answering questions,
// plus an SSE stream for real-time question events.
type QuestionHTTPHandler struct {
	store  *QuestionStore
	nc     *natsclient.Client
	logger *slog.Logger
}

// NewQuestionHTTPHandler creates a new HTTP handler for questions.
func NewQuestionHTTPHandler(nc *natsclient.Client, logger *slog.Logger) (*QuestionHTTPHandler, error) {
	store, err := NewQuestionStore(nc)
	if err != nil {
		return nil, fmt.Errorf("create question store: %w", err)
	}

	// Use default logger if none provided
	if logger == nil {
		logger = slog.Default()
	}

	return &QuestionHTTPHandler{
		store:  store,
		nc:     nc,
		logger: logger,
	}, nil
}

// log returns the logger, defaulting to slog.Default if nil.
func (h *QuestionHTTPHandler) log() *slog.Logger {
	if h.logger == nil {
		return slog.Default()
	}
	return h.logger
}

// RegisterHTTPHandlers registers the question API endpoints.
// The prefix should be "/questions" (without trailing slash).
func (h *QuestionHTTPHandler) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	// Ensure prefix doesn't have trailing slash for consistent routing
	prefix = strings.TrimSuffix(prefix, "/")

	// GET /questions - List questions with optional filters
	mux.HandleFunc("GET "+prefix, h.handleList)

	// GET /questions/stream - SSE stream for real-time events
	mux.HandleFunc("GET "+prefix+"/stream", h.handleStream)

	// GET /questions/{id} - Get single question
	mux.HandleFunc("GET "+prefix+"/{id}", h.handleGet)

	// POST /questions/{id}/answer - Submit an answer
	mux.HandleFunc("POST "+prefix+"/{id}/answer", h.handleAnswer)
}

// ListQuestionsResponse is the response for GET /questions.
type ListQuestionsResponse struct {
	Questions []*Question `json:"questions"`
	Total     int         `json:"total"`
}

// AnswerRequest is the request body for POST /questions/{id}/answer.
type AnswerRequest struct {
	Answer     string `json:"answer"`
	Confidence string `json:"confidence,omitempty"`
	Sources    string `json:"sources,omitempty"`
}

// handleList handles GET /questions with optional query parameters.
// Query parameters:
//   - status: pending, answered, timeout, all (default: pending)
//   - topic: filter by topic pattern (e.g., "requirements.*")
//   - limit: max results (default: 50)
func (h *QuestionHTTPHandler) handleList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters
	statusParam := r.URL.Query().Get("status")
	topicParam := r.URL.Query().Get("topic")
	limitParam := r.URL.Query().Get("limit")

	// Parse status filter
	var status QuestionStatus
	switch statusParam {
	case "pending", "":
		status = QuestionStatusPending
	case "answered":
		status = QuestionStatusAnswered
	case "timeout":
		status = QuestionStatusTimeout
	case "all":
		status = "" // No filter
	default:
		h.writeError(w, http.StatusBadRequest, "invalid status: must be pending, answered, timeout, or all")
		return
	}

	// Parse limit
	limit := 50
	if limitParam != "" {
		parsed, err := strconv.Atoi(limitParam)
		if err != nil || parsed < 1 || parsed > 1000 {
			h.writeError(w, http.StatusBadRequest, "invalid limit: must be 1-1000")
			return
		}
		limit = parsed
	}

	// Get questions from store
	questions, err := h.store.List(ctx, status)
	if err != nil {
		h.log().Error("Failed to list questions", "error", err)
		h.writeError(w, http.StatusInternalServerError, "failed to list questions")
		return
	}

	// Filter by topic if specified
	if topicParam != "" {
		filtered := make([]*Question, 0)
		for _, q := range questions {
			if matchTopic(q.Topic, topicParam) {
				filtered = append(filtered, q)
			}
		}
		questions = filtered
	}

	// Apply limit
	total := len(questions)
	if len(questions) > limit {
		questions = questions[:limit]
	}

	h.writeJSON(w, http.StatusOK, ListQuestionsResponse{
		Questions: questions,
		Total:     total,
	})
}

// handleGet handles GET /questions/{id}.
func (h *QuestionHTTPHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract ID from path
	id := r.PathValue("id")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "question ID required")
		return
	}

	// Validate ID format
	if !strings.HasPrefix(id, "q-") {
		h.writeError(w, http.StatusBadRequest, "invalid question ID format (must start with 'q-')")
		return
	}

	question, err := h.store.Get(ctx, id)
	if err != nil {
		// Check if it's a not found error using proper JetStream error
		if errors.Is(err, jetstream.ErrKeyNotFound) || strings.Contains(err.Error(), "key not found") {
			h.writeError(w, http.StatusNotFound, "question not found")
			return
		}
		h.log().Error("Failed to get question", "id", id, "error", err)
		h.writeError(w, http.StatusInternalServerError, "failed to get question")
		return
	}

	h.writeJSON(w, http.StatusOK, question)
}

// handleAnswer handles POST /questions/{id}/answer.
func (h *QuestionHTTPHandler) handleAnswer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract ID from path
	id := r.PathValue("id")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "question ID required")
		return
	}

	// Validate ID format
	if !strings.HasPrefix(id, "q-") {
		h.writeError(w, http.StatusBadRequest, "invalid question ID format (must start with 'q-')")
		return
	}

	// Limit request body size to prevent DoS
	r.Body = http.MaxBytesReader(w, r.Body, maxAnswerBodySize)

	// Parse request body
	var req AnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Answer == "" {
		h.writeError(w, http.StatusBadRequest, "answer is required")
		return
	}

	// Get the question to verify it exists and is pending
	question, err := h.store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) || strings.Contains(err.Error(), "key not found") {
			h.writeError(w, http.StatusNotFound, "question not found")
			return
		}
		h.log().Error("Failed to get question", "id", id, "error", err)
		h.writeError(w, http.StatusInternalServerError, "failed to get question")
		return
	}

	if question.Status != QuestionStatusPending {
		h.writeError(w, http.StatusConflict, fmt.Sprintf("question already %s", question.Status))
		return
	}

	// Get user ID from request header (set by auth middleware) or default
	answeredBy := r.Header.Get("X-User-ID")
	if answeredBy == "" {
		answeredBy = "anonymous"
	}

	// Answer the question
	if err := h.store.Answer(ctx, id, req.Answer, answeredBy, "human", req.Confidence, req.Sources); err != nil {
		// Check for concurrent modification (optimistic locking failure)
		if strings.Contains(err.Error(), "concurrent modification") || strings.Contains(err.Error(), "wrong last sequence") {
			h.writeError(w, http.StatusConflict, "question was modified by another request")
			return
		}
		h.log().Error("Failed to answer question", "id", id, "error", err)
		h.writeError(w, http.StatusInternalServerError, "failed to answer question")
		return
	}

	// Publish answer event for any waiting workflows
	subject := fmt.Sprintf("question.answer.%s", id)
	answerPayload := map[string]any{
		"question_id":   id,
		"answer":        req.Answer,
		"answered_by":   answeredBy,
		"answerer_type": "human",
		"confidence":    req.Confidence,
		"sources":       req.Sources,
	}
	answerData, err := json.Marshal(answerPayload)
	if err != nil {
		h.log().Warn("Failed to marshal answer event", "question_id", id, "error", err)
	} else if err := h.nc.PublishToStream(ctx, subject, answerData); err != nil {
		h.log().Warn("Failed to publish answer event", "question_id", id, "error", err)
		// Don't fail - the answer is stored, routing is optional
	}

	h.log().Info("Question answered via HTTP",
		"question_id", id,
		"answered_by", answeredBy,
	)

	// Return the updated question
	question, _ = h.store.Get(ctx, id)
	h.writeJSON(w, http.StatusOK, question)
}

// SSE event types for the questions stream.
const (
	SSEEventQuestionCreated  = "question_created"
	SSEEventQuestionAnswered = "question_answered"
	SSEEventQuestionTimeout  = "question_timeout"
	SSEEventHeartbeat        = "heartbeat"
)

// handleStream handles GET /questions/stream for SSE events.
// Query parameters:
//   - status: filter events by question status (optional)
//
// Note: On initial connection, existing questions are replayed as question_created
// events. A sync_complete event signals the end of the initial replay.
func (h *QuestionHTTPHandler) handleStream(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Flush headers
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	flusher.Flush()

	// Get JetStream context
	js, err := h.nc.JetStream()
	if err != nil {
		h.log().Error("Failed to get JetStream", "error", err)
		h.sendSSEEvent(w, flusher, "error", map[string]string{"message": "failed to connect to stream"})
		return
	}

	// Get the QUESTIONS KV bucket
	bucket, err := js.KeyValue(ctx, QuestionsBucket)
	if err != nil {
		h.log().Error("Failed to get questions bucket", "error", err)
		h.sendSSEEvent(w, flusher, "error", map[string]string{"message": "questions not available"})
		return
	}

	// Create a watcher for the bucket
	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		h.log().Error("Failed to create bucket watcher", "error", err)
		h.sendSSEEvent(w, flusher, "error", map[string]string{"message": "failed to watch questions"})
		return
	}
	defer watcher.Stop()

	// Send connected event
	if err := h.sendSSEEvent(w, flusher, "connected", map[string]string{"status": "connected"}); err != nil {
		h.log().Debug("Client disconnected during connect", "error", err)
		return
	}

	// Parse status filter
	statusFilter := r.URL.Query().Get("status")

	// Track seen questions to detect changes
	seenQuestions := make(map[string]*Question)

	// Heartbeat ticker
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	// Event counter for SSE IDs (use uint64 to avoid overflow)
	var eventID uint64

	// Process updates
	updates := watcher.Updates()
	for {
		select {
		case <-ctx.Done():
			return

		case <-heartbeat.C:
			eventID++
			if err := h.sendSSEEventWithID(w, flusher, eventID, SSEEventHeartbeat, map[string]any{}); err != nil {
				h.log().Debug("Client disconnected during heartbeat", "error", err)
				return
			}

		case entry, ok := <-updates:
			if !ok {
				// Watcher closed
				return
			}

			// nil entry signals end of initial values
			if entry == nil {
				if err := h.sendSSEEvent(w, flusher, "sync_complete", map[string]string{"status": "ready"}); err != nil {
					h.log().Debug("Client disconnected during sync", "error", err)
					return
				}
				continue
			}

			// Skip deletions
			if entry.Operation() == jetstream.KeyValueDelete {
				delete(seenQuestions, entry.Key())
				continue
			}

			// Parse the question
			var question Question
			if err := json.Unmarshal(entry.Value(), &question); err != nil {
				h.log().Warn("Failed to parse question", "key", entry.Key(), "error", err)
				continue
			}

			// Apply status filter
			if statusFilter != "" && string(question.Status) != statusFilter && statusFilter != "all" {
				continue
			}

			// Determine event type
			eventType := h.determineEventType(&question, seenQuestions[entry.Key()])

			// Update seen map
			qCopy := question
			seenQuestions[entry.Key()] = &qCopy

			// Send event
			eventID++
			if err := h.sendSSEEventWithID(w, flusher, eventID, eventType, &question); err != nil {
				h.log().Debug("Client disconnected during event", "error", err)
				return
			}
		}
	}
}

// determineEventType determines the SSE event type based on question state changes.
func (h *QuestionHTTPHandler) determineEventType(current, previous *Question) string {
	if previous == nil {
		// New question
		return SSEEventQuestionCreated
	}

	// Check for status changes
	if previous.Status != current.Status {
		switch current.Status {
		case QuestionStatusAnswered:
			return SSEEventQuestionAnswered
		case QuestionStatusTimeout:
			return SSEEventQuestionTimeout
		}
	}

	// Default to created for other updates
	return SSEEventQuestionCreated
}

// sendSSEEvent sends an SSE event without an ID.
func (h *QuestionHTTPHandler) sendSSEEvent(w http.ResponseWriter, flusher http.Flusher, eventType string, data any) error {
	return h.sendSSEEventWithID(w, flusher, 0, eventType, data)
}

// sendSSEEventWithID sends an SSE event with optional ID.
// Returns an error if the write fails (e.g., client disconnected).
func (h *QuestionHTTPHandler) sendSSEEventWithID(w http.ResponseWriter, flusher http.Flusher, id uint64, eventType string, data any) error {
	dataBytes, err := json.Marshal(data)
	if err != nil {
		h.log().Warn("Failed to marshal SSE data", "error", err)
		return nil // Don't return marshal errors as connection issues
	}

	// Write event type
	if _, err := fmt.Fprintf(w, "event: %s\n", eventType); err != nil {
		return fmt.Errorf("write event type: %w", err)
	}

	// Write ID if provided
	if id > 0 {
		if _, err := fmt.Fprintf(w, "id: %d\n", id); err != nil {
			return fmt.Errorf("write event id: %w", err)
		}
	}

	// Write data
	if _, err := fmt.Fprintf(w, "data: %s\n\n", dataBytes); err != nil {
		return fmt.Errorf("write event data: %w", err)
	}

	flusher.Flush()
	return nil
}

// writeJSON writes a JSON response.
func (h *QuestionHTTPHandler) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.log().Warn("Failed to write JSON response", "error", err)
	}
}

// writeError writes an error response.
func (h *QuestionHTTPHandler) writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// matchTopic checks if a topic matches a pattern.
// Supports wildcards: * matches one segment, > matches multiple segments.
func matchTopic(topic, pattern string) bool {
	// Empty pattern or topic
	if pattern == "" {
		return false
	}

	// Exact match
	if topic == pattern {
		return true
	}

	// Check for wildcard patterns
	if !strings.Contains(pattern, "*") && !strings.Contains(pattern, ">") {
		// Exact prefix match
		return strings.HasPrefix(topic, pattern)
	}

	topicParts := strings.Split(topic, ".")
	patternParts := strings.Split(pattern, ".")

	ti, pi := 0, 0
	for pi < len(patternParts) && ti < len(topicParts) {
		switch patternParts[pi] {
		case "*":
			// Match exactly one segment
			ti++
			pi++
		case ">":
			// Match remaining segments
			return true
		default:
			// Exact segment match
			if patternParts[pi] != topicParts[ti] {
				return false
			}
			ti++
			pi++
		}
	}

	// Both must be exhausted for a match (unless pattern ended with >)
	return ti == len(topicParts) && pi == len(patternParts)
}
