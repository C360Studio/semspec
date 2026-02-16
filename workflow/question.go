package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

func init() {
	// Register AnswerPayload type for message deserialization
	_ = component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "question",
		Category:    "answer",
		Version:     "v1",
		Description: "Question answer payload",
		Factory:     func() any { return &AnswerPayload{} },
	})
}

// QuestionsBucket is the KV bucket name for storing questions.
const QuestionsBucket = "QUESTIONS"

// QuestionStatus represents the status of a question.
type QuestionStatus string

const (
	QuestionStatusPending  QuestionStatus = "pending"
	QuestionStatusAnswered QuestionStatus = "answered"
	QuestionStatusTimeout  QuestionStatus = "timeout"
)

// QuestionUrgency represents the urgency level of a question.
type QuestionUrgency string

const (
	QuestionUrgencyLow      QuestionUrgency = "low"
	QuestionUrgencyNormal   QuestionUrgency = "normal"
	QuestionUrgencyHigh     QuestionUrgency = "high"
	QuestionUrgencyBlocking QuestionUrgency = "blocking"
)

// Question represents a knowledge gap that needs resolution.
// Questions are stored in the QUESTIONS KV bucket and can be
// asked by agents and answered by agents, teams, or humans.
type Question struct {
	// ID uniquely identifies this question (format: q-{uuid})
	ID string `json:"id"`

	// FromAgent identifies who asked the question (e.g., "design-writer")
	FromAgent string `json:"from_agent"`

	// Topic is hierarchical (e.g., "api.semstreams.loop-info")
	// Used for routing to the appropriate answerer
	Topic string `json:"topic"`

	// Question is the actual question text
	Question string `json:"question"`

	// Context provides background information for the answerer
	Context string `json:"context,omitempty"`

	// BlockedLoopID is the loop waiting for this answer (if any)
	BlockedLoopID string `json:"blocked_loop_id,omitempty"`

	// TraceID correlates this question with other messages in the same request flow.
	// Used for distributed tracing and debugging via /debug trace.
	TraceID string `json:"trace_id,omitempty"`

	// Urgency indicates how urgent the question is
	Urgency QuestionUrgency `json:"urgency"`

	// Status is the current state of the question
	Status QuestionStatus `json:"status"`

	// CreatedAt is when the question was asked
	CreatedAt time.Time `json:"created_at"`

	// Deadline is when the question should be answered by (optional)
	Deadline *time.Time `json:"deadline,omitempty"`

	// AssignedTo is the answerer assigned to this question (e.g., "agent/architect", "team/security")
	AssignedTo string `json:"assigned_to,omitempty"`

	// AssignedAt is when the question was assigned
	AssignedAt time.Time `json:"assigned_at,omitempty"`

	// AnsweredAt is when the question was answered (if answered)
	AnsweredAt *time.Time `json:"answered_at,omitempty"`

	// Answer is the response to the question (if answered)
	Answer string `json:"answer,omitempty"`

	// AnsweredBy identifies who answered (agent, team, or user ID)
	AnsweredBy string `json:"answered_by,omitempty"`

	// AnswererType is "agent", "team", or "human"
	AnswererType string `json:"answerer_type,omitempty"`

	// Confidence is the answerer's confidence level ("high", "medium", "low")
	Confidence string `json:"confidence,omitempty"`

	// Sources describes where the answer came from
	Sources string `json:"sources,omitempty"`
}

// NewQuestion creates a new question with a generated ID.
func NewQuestion(fromAgent, topic, question, context string) *Question {
	return &Question{
		ID:        fmt.Sprintf("q-%s", uuid.New().String()[:8]),
		FromAgent: fromAgent,
		Topic:     topic,
		Question:  question,
		Context:   context,
		Urgency:   QuestionUrgencyNormal,
		Status:    QuestionStatusPending,
		CreatedAt: time.Now().UTC(),
	}
}

// QuestionStore provides operations for storing and retrieving questions.
type QuestionStore struct {
	nc     *natsclient.Client
	bucket jetstream.KeyValue
}

// NewQuestionStore creates a new question store.
func NewQuestionStore(nc *natsclient.Client) (*QuestionStore, error) {
	js, err := nc.JetStream()
	if err != nil {
		return nil, fmt.Errorf("get jetstream: %w", err)
	}

	ctx := context.Background()

	// CreateOrUpdateKeyValue is idempotent and handles race conditions
	bucket, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      QuestionsBucket,
		Description: "Knowledge gap questions from agents",
		TTL:         30 * 24 * time.Hour, // 30 days
	})
	if err != nil {
		return nil, fmt.Errorf("create/update kv bucket: %w", err)
	}

	return &QuestionStore{
		nc:     nc,
		bucket: bucket,
	}, nil
}

// Store saves a question to the KV bucket.
func (s *QuestionStore) Store(ctx context.Context, q *Question) error {
	data, err := json.Marshal(q)
	if err != nil {
		return fmt.Errorf("marshal question: %w", err)
	}

	_, err = s.bucket.Put(ctx, q.ID, data)
	if err != nil {
		return fmt.Errorf("put question: %w", err)
	}

	return nil
}

// Get retrieves a question by ID.
func (s *QuestionStore) Get(ctx context.Context, id string) (*Question, error) {
	entry, err := s.bucket.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get question: %w", err)
	}

	var q Question
	if err := json.Unmarshal(entry.Value(), &q); err != nil {
		return nil, fmt.Errorf("unmarshal question: %w", err)
	}

	return &q, nil
}

// List retrieves all questions, optionally filtered by status.
func (s *QuestionStore) List(ctx context.Context, status QuestionStatus) ([]*Question, error) {
	keys, err := s.bucket.Keys(ctx)
	if err != nil {
		// Empty bucket returns ErrNoKeysFound - this is not an error
		if errors.Is(err, jetstream.ErrNoKeysFound) {
			return []*Question{}, nil
		}
		return nil, fmt.Errorf("list keys: %w", err)
	}

	var questions []*Question
	for _, key := range keys {
		// Check for context cancellation to avoid processing after request cancelled
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		entry, err := s.bucket.Get(ctx, key)
		if err != nil {
			continue // Skip errors for individual keys
		}

		var q Question
		if err := json.Unmarshal(entry.Value(), &q); err != nil {
			continue
		}

		// Filter by status if specified
		if status != "" && q.Status != status {
			continue
		}

		questions = append(questions, &q)
	}

	return questions, nil
}

// Answer marks a question as answered.
func (s *QuestionStore) Answer(ctx context.Context, id, answer, answeredBy, answererType, confidence, sources string) error {
	q, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	q.Status = QuestionStatusAnswered
	q.Answer = answer
	q.AnsweredBy = answeredBy
	q.AnswererType = answererType
	q.Confidence = confidence
	q.Sources = sources
	q.AnsweredAt = &now

	return s.Store(ctx, q)
}

// Delete removes a question from the store.
func (s *QuestionStore) Delete(ctx context.Context, id string) error {
	return s.bucket.Delete(ctx, id)
}

// AnswerPayload represents an answer to a question.
// Published to question.answer.{id} subjects.
type AnswerPayload struct {
	// QuestionID is the ID of the question being answered.
	QuestionID string `json:"question_id"`

	// AnsweredBy identifies who answered (agent, team, or user ID).
	AnsweredBy string `json:"answered_by"`

	// AnswererType is "agent", "team", or "human".
	AnswererType string `json:"answerer_type"`

	// Answer is the response text.
	Answer string `json:"answer"`

	// Confidence is the answerer's confidence level ("high", "medium", "low").
	Confidence string `json:"confidence,omitempty"`

	// Sources describes where the answer came from.
	Sources string `json:"sources,omitempty"`
}

// AnswerType is the message type for answer payloads.
var AnswerType = message.Type{
	Domain:   "question",
	Category: "answer",
	Version:  "v1",
}

// Schema returns the message type for this payload.
func (p *AnswerPayload) Schema() message.Type {
	return AnswerType
}

// Validate validates the payload.
func (p *AnswerPayload) Validate() error {
	if p.QuestionID == "" {
		return fmt.Errorf("question_id is required")
	}
	if p.Answer == "" {
		return fmt.Errorf("answer is required")
	}
	return nil
}

// MarshalJSON marshals the payload to JSON.
func (p *AnswerPayload) MarshalJSON() ([]byte, error) {
	type Alias AnswerPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON unmarshals the payload from JSON.
func (p *AnswerPayload) UnmarshalJSON(data []byte) error {
	type Alias AnswerPayload
	return json.Unmarshal(data, (*Alias)(p))
}
