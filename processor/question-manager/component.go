// Package questionmanager owns the QUESTIONS KV bucket and serves the Q&A
// HTTP API for human-in-the-loop question answering.
//
// Agents ask questions via the ask_question tool (writes to QUESTIONS KV,
// dispatches answerer agent). Humans answer via POST /question-manager/questions/{id}/answer
// (writes to QUESTIONS KV). The ask_question tool's KV watch picks up both.
package questionmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream" //nolint:depguard // direct jetstream for KV watcher
)

const (
	componentName = "question-manager"

	// maxAnswerBodySize limits the size of answer request bodies.
	maxAnswerBodySize = 1 << 20 // 1 MB

	// SSE event types.
	sseQuestionCreated  = "question_created"
	sseQuestionAnswered = "question_answered"
	sseQuestionTimeout  = "question_timeout"
	sseHeartbeat        = "heartbeat"
)

// Config holds configuration for the question-manager.
type Config struct {
	// PlanStateBucket is the KV bucket name (default: QUESTIONS).
	Bucket string `json:"bucket" schema:"type:string,description:KV bucket name,category:basic,default:QUESTIONS"`
}

// Component owns the QUESTIONS KV bucket and serves the Q&A HTTP API.
type Component struct {
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger
	store      *workflow.QuestionStore
	prefix     string // URL prefix set during HTTP registration

	running bool
	mu      sync.RWMutex
	cancel  context.CancelFunc
}

// NewComponent creates a new question-manager.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var cfg Config
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if cfg.Bucket == "" {
		cfg.Bucket = workflow.QuestionsBucket
	}

	return &Component{
		config:     cfg,
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error { return nil }

// Start creates the QUESTIONS KV bucket and begins watching for graph publishing.
func (c *Component) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return nil
	}

	store, err := workflow.NewQuestionStore(c.natsClient)
	if err != nil {
		return fmt.Errorf("create question store: %w", err)
	}
	c.store = store
	c.running = true

	// Start KV watcher that publishes every question mutation to the graph.
	// This covers all write paths: ask_question tool, answer_question tool,
	// gap handler, and HTTP answers.
	watchCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	go c.watchQuestionUpdates(watchCtx)

	c.logger.Info("question-manager started", "bucket", c.config.Bucket)
	return nil
}

// RegisterHTTPHandlers registers the Q&A REST + SSE endpoints.
func (c *Component) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	c.prefix = prefix + "questions/"

	mux.HandleFunc(c.prefix, c.handleQuestions)
	mux.HandleFunc(c.prefix+"stream", c.handleStream)

	c.logger.Info("Registered Q&A HTTP handlers", "prefix", c.prefix)
}

// ---------------------------------------------------------------------------
// HTTP: REST handlers
// ---------------------------------------------------------------------------

// handleQuestions routes requests based on method and path.
//
//	GET  /questions/            → list questions
//	GET  /questions/{id}        → get single question
//	POST /questions/{id}/answer → submit human answer
func (c *Component) handleQuestions(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, strings.TrimSuffix(c.prefix, "/"))
	path = strings.TrimPrefix(path, "/")

	switch {
	case path == "" || path == "/":
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		c.handleList(w, r)

	case strings.HasSuffix(path, "/answer"):
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimSuffix(path, "/answer")
		c.handleAnswer(w, r, id)

	default:
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		c.handleGet(w, r, path)
	}
}

// handleList handles GET /questions/ with optional query parameters.
// Query parameters: status (pending|answered|timeout|all), topic, category, limit (1-1000).
func (c *Component) handleList(w http.ResponseWriter, r *http.Request) {
	if c.store == nil {
		writeJSON(w, http.StatusOK, map[string]any{"questions": []*workflow.Question{}, "total": 0})
		return
	}

	ctx := r.Context()

	statusParam := r.URL.Query().Get("status")
	topicParam := r.URL.Query().Get("topic")
	categoryParam := r.URL.Query().Get("category")
	limitParam := r.URL.Query().Get("limit")

	var status workflow.QuestionStatus
	switch statusParam {
	case "pending", "":
		status = workflow.QuestionStatusPending
	case "answered":
		status = workflow.QuestionStatusAnswered
	case "timeout":
		status = workflow.QuestionStatusTimeout
	case "all":
		status = ""
	default:
		writeError(w, http.StatusBadRequest, "invalid status: must be pending, answered, timeout, or all")
		return
	}

	limit := 50
	if limitParam != "" {
		parsed, err := strconv.Atoi(limitParam)
		if err != nil || parsed < 1 || parsed > 1000 {
			writeError(w, http.StatusBadRequest, "invalid limit: must be 1-1000")
			return
		}
		limit = parsed
	}

	questions, err := c.store.List(ctx, status)
	if err != nil {
		c.logger.Error("Failed to list questions", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list questions")
		return
	}

	if topicParam != "" {
		filtered := make([]*workflow.Question, 0)
		for _, q := range questions {
			if matchTopic(q.Topic, topicParam) {
				filtered = append(filtered, q)
			}
		}
		questions = filtered
	}

	if categoryParam != "" {
		filtered := make([]*workflow.Question, 0)
		for _, q := range questions {
			qCat := string(q.Category)
			if qCat == "" {
				qCat = string(workflow.QuestionCategoryKnowledge)
			}
			if qCat == categoryParam {
				filtered = append(filtered, q)
			}
		}
		questions = filtered
	}

	total := len(questions)
	if len(questions) > limit {
		questions = questions[:limit]
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"questions": questions,
		"total":     total,
	})
}

// handleGet handles GET /questions/{id}.
func (c *Component) handleGet(w http.ResponseWriter, r *http.Request, id string) {
	if c.store == nil {
		writeError(w, http.StatusServiceUnavailable, "question store not ready")
		return
	}
	if id == "" {
		writeError(w, http.StatusBadRequest, "question ID required")
		return
	}
	if !strings.HasPrefix(id, "q-") {
		writeError(w, http.StatusBadRequest, "invalid question ID format (must start with 'q-')")
		return
	}

	question, err := c.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) || strings.Contains(err.Error(), "key not found") {
			writeError(w, http.StatusNotFound, "question not found")
			return
		}
		c.logger.Error("Failed to get question", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get question")
		return
	}

	writeJSON(w, http.StatusOK, question)
}

// AnswerRequest is the request body for POST /questions/{id}/answer.
type AnswerRequest struct {
	Answer     string               `json:"answer"`
	Confidence string               `json:"confidence,omitempty"`
	Sources    string               `json:"sources,omitempty"`
	Action     *workflow.AnswerAction `json:"action,omitempty"`
}

// handleAnswer handles POST /questions/{id}/answer.
func (c *Component) handleAnswer(w http.ResponseWriter, r *http.Request, id string) {
	if c.store == nil {
		writeError(w, http.StatusServiceUnavailable, "question store not ready")
		return
	}

	ctx := r.Context()

	if id == "" {
		writeError(w, http.StatusBadRequest, "question ID required")
		return
	}
	if !strings.HasPrefix(id, "q-") {
		writeError(w, http.StatusBadRequest, "invalid question ID format (must start with 'q-')")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAnswerBodySize)

	var req AnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Answer == "" {
		writeError(w, http.StatusBadRequest, "answer is required")
		return
	}

	question, err := c.store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) || strings.Contains(err.Error(), "key not found") {
			writeError(w, http.StatusNotFound, "question not found")
			return
		}
		c.logger.Error("Failed to get question", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get question")
		return
	}

	if question.Status != workflow.QuestionStatusPending {
		writeError(w, http.StatusConflict, fmt.Sprintf("question already %s", question.Status))
		return
	}

	if req.Action != nil {
		if err := req.Action.Validate(); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid action: %v", err))
			return
		}
	}

	answeredBy := r.Header.Get("X-User-ID")
	if answeredBy == "" {
		answeredBy = "anonymous"
	}

	now := time.Now().UTC()
	question.Status = workflow.QuestionStatusAnswered
	question.Answer = req.Answer
	question.AnsweredBy = answeredBy
	question.AnswererType = "human"
	question.Confidence = req.Confidence
	question.Sources = req.Sources
	question.AnsweredAt = &now
	question.Action = req.Action

	if err := c.store.Store(ctx, question); err != nil {
		c.logger.Error("Failed to answer question", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to answer question")
		return
	}

	// Graph publishing handled by the KV watcher (watchQuestionUpdates).
	c.logger.Info("Question answered via HTTP", "question_id", id, "answered_by", answeredBy)
	writeJSON(w, http.StatusOK, question)
}

// ---------------------------------------------------------------------------
// HTTP: SSE stream
// ---------------------------------------------------------------------------

// handleStream handles GET /questions/stream for real-time question events.
// On initial connection, existing questions are replayed as question_created events.
func (c *Component) handleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	flusher.Flush()

	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Error("Failed to get JetStream", "error", err)
		sendSSE(w, flusher, "error", map[string]string{"message": "failed to connect to stream"})
		return
	}

	bucket, err := js.KeyValue(ctx, workflow.QuestionsBucket)
	if err != nil {
		c.logger.Error("Failed to get questions bucket", "error", err)
		sendSSE(w, flusher, "error", map[string]string{"message": "questions not available"})
		return
	}

	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		c.logger.Error("Failed to create bucket watcher", "error", err)
		sendSSE(w, flusher, "error", map[string]string{"message": "failed to watch questions"})
		return
	}
	defer watcher.Stop()

	sendSSE(w, flusher, "connected", map[string]string{"status": "connected"})

	statusFilter := r.URL.Query().Get("status")
	categoryFilter := r.URL.Query().Get("category")

	seenQuestions := make(map[string]*workflow.Question)

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	var eventID uint64

	for {
		select {
		case <-ctx.Done():
			return

		case <-heartbeat.C:
			eventID++
			if err := sendSSEWithID(w, flusher, eventID, sseHeartbeat, map[string]any{}); err != nil {
				return
			}

		case entry, ok := <-watcher.Updates():
			if !ok {
				return
			}
			if entry == nil {
				sendSSE(w, flusher, "sync_complete", map[string]string{"status": "ready"})
				continue
			}
			if entry.Operation() == jetstream.KeyValueDelete {
				delete(seenQuestions, entry.Key())
				continue
			}

			var question workflow.Question
			if err := json.Unmarshal(entry.Value(), &question); err != nil {
				continue
			}

			if statusFilter != "" && string(question.Status) != statusFilter && statusFilter != "all" {
				continue
			}
			if categoryFilter != "" {
				qCat := string(question.Category)
				if qCat == "" {
					qCat = string(workflow.QuestionCategoryKnowledge)
				}
				if qCat != categoryFilter {
					continue
				}
			}

			eventType := sseQuestionCreated
			if prev := seenQuestions[entry.Key()]; prev != nil && prev.Status != question.Status {
				switch question.Status {
				case workflow.QuestionStatusAnswered:
					eventType = sseQuestionAnswered
				case workflow.QuestionStatusTimeout:
					eventType = sseQuestionTimeout
				}
			}

			qCopy := question
			seenQuestions[entry.Key()] = &qCopy

			eventID++
			if err := sendSSEWithID(w, flusher, eventID, eventType, &question); err != nil {
				return
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Component interface
// ---------------------------------------------------------------------------

// Stop gracefully stops the component.
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
	}
	c.running = false
	c.mu.Unlock()
	c.logger.Info("question-manager stopped")
	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        componentName,
		Type:        "processor",
		Description: "Owns QUESTIONS KV and serves Q&A HTTP API for human-in-the-loop answers",
		Version:     "0.1.0",
	}
}

func (c *Component) InputPorts() []component.Port            { return nil }
func (c *Component) OutputPorts() []component.Port           { return nil }
func (c *Component) ConfigSchema() component.ConfigSchema    { return component.ConfigSchema{} }

func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	running := c.running
	c.mu.RUnlock()
	status := "stopped"
	if running {
		status = "running"
	}
	return component.HealthStatus{Healthy: running, Status: status}
}

func (c *Component) DataFlow() component.FlowMetrics { return component.FlowMetrics{} }

// ---------------------------------------------------------------------------
// KV watcher → graph publish
// ---------------------------------------------------------------------------

const graphIngestSubject = "graph.ingest.entity"

// watchQuestionUpdates watches the QUESTIONS KV bucket for all mutations and
// publishes each question as a graph entity. This catches creation (ask_question
// tool, gap handler), agent answers (answer_question tool), and human answers
// (HTTP API) in one place without wiring into each individual code path.
func (c *Component) watchQuestionUpdates(ctx context.Context) {
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Warn("JetStream unavailable, question graph publishing disabled", "error", err)
		return
	}

	bucket, err := js.KeyValue(ctx, workflow.QuestionsBucket)
	if err != nil {
		c.logger.Warn("QUESTIONS bucket not found, question graph publishing disabled", "error", err)
		return
	}

	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		c.logger.Warn("Failed to watch QUESTIONS bucket, question graph publishing disabled", "error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Question graph publisher started")

	for entry := range watcher.Updates() {
		if entry == nil {
			continue // end of initial replay
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}

		var q workflow.Question
		if err := json.Unmarshal(entry.Value(), &q); err != nil {
			c.logger.Warn("Failed to unmarshal question from KV",
				"key", entry.Key(), "error", err)
			continue
		}

		if err := c.publishQuestionEntity(ctx, &q); err != nil {
			c.logger.Warn("Failed to publish question entity to graph",
				"question_id", q.ID, "error", err)
		}
	}
}

// publishQuestionEntity publishes a question as a single batched graph entity.
// All triples are bundled into one EntityPayload and published in a single NATS
// message, matching the pattern used by plan-manager and execution-manager.
func (c *Component) publishQuestionEntity(ctx context.Context, q *workflow.Question) error {
	if c.natsClient == nil {
		return nil
	}

	entityID := workflow.QuestionEntityID(q.ID)

	triples := []message.Triple{
		{Subject: entityID, Predicate: semspec.QuestionContent, Object: q.Question},
		{Subject: entityID, Predicate: semspec.QuestionTopic, Object: q.Topic},
		{Subject: entityID, Predicate: semspec.QuestionFromAgent, Object: q.FromAgent},
		{Subject: entityID, Predicate: semspec.QuestionStatus, Object: string(q.Status)},
		{Subject: entityID, Predicate: semspec.QuestionUrgency, Object: string(q.Urgency)},
		{Subject: entityID, Predicate: semspec.QuestionCreatedAt, Object: q.CreatedAt.Format(time.RFC3339)},
		{Subject: entityID, Predicate: semspec.DCTitle, Object: truncateTitle(q.Question, 100)},
	}

	if q.Context != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionContext, Object: q.Context})
	}
	if q.BlockedLoopID != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionBlockedLoopID, Object: q.BlockedLoopID})
	}
	if q.TraceID != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionTraceID, Object: q.TraceID})
	}
	if q.PlanSlug != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionPlanSlug, Object: q.PlanSlug})
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionPlanID, Object: workflow.PlanEntityID(q.PlanSlug)})
	}
	if q.TaskID != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionTaskID, Object: q.TaskID})
	}
	if q.PhaseID != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionPhaseID, Object: q.PhaseID})
	}
	if q.AssignedTo != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionAssignedTo, Object: q.AssignedTo})
	}
	if q.Answer != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionAnswer, Object: q.Answer})
	}
	if q.AnsweredBy != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionAnsweredBy, Object: q.AnsweredBy})
	}
	if q.AnswererType != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionAnswererType, Object: q.AnswererType})
	}
	if q.AnsweredAt != nil {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionAnsweredAt, Object: q.AnsweredAt.Format(time.RFC3339)})
	}
	if q.Confidence != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionConfidence, Object: q.Confidence})
	}
	if q.Sources != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.QuestionSources, Object: q.Sources})
	}

	payload := workflow.NewEntityPayload(workflow.QuestionEntityType, entityID, triples)
	baseMsg := message.NewBaseMessage(payload.Schema(), payload, componentName)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal question entity: %w", err)
	}

	if err := c.natsClient.PublishToStream(ctx, graphIngestSubject, data); err != nil {
		return fmt.Errorf("publish question to graph: %w", err)
	}
	return nil
}

// truncateTitle truncates a string to maxLen runes for use as a graph title.
func truncateTitle(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func sendSSE(w http.ResponseWriter, flusher http.Flusher, eventType string, data any) error {
	return sendSSEWithID(w, flusher, 0, eventType, data)
}

func sendSSEWithID(w http.ResponseWriter, flusher http.Flusher, id uint64, eventType string, data any) error {
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return nil
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", eventType); err != nil {
		return err
	}
	if id > 0 {
		if _, err := fmt.Fprintf(w, "id: %d\n", id); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", dataBytes); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func matchTopic(topic, pattern string) bool {
	if pattern == "" {
		return false
	}
	if topic == pattern {
		return true
	}
	if !strings.Contains(pattern, "*") && !strings.Contains(pattern, ">") {
		return strings.HasPrefix(topic, pattern)
	}

	topicParts := strings.Split(topic, ".")
	patternParts := strings.Split(pattern, ".")

	ti, pi := 0, 0
	for pi < len(patternParts) && ti < len(topicParts) {
		switch patternParts[pi] {
		case "*":
			ti++
			pi++
		case ">":
			return true
		default:
			if patternParts[pi] != topicParts[ti] {
				return false
			}
			ti++
			pi++
		}
	}
	return ti == len(topicParts) && pi == len(patternParts)
}
