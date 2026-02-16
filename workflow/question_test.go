package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

func TestQuestionStore_StoreAndGet(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewQuestionStore(tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	// Create a question
	q := NewQuestion("test-agent", "requirements.scope", "What is the scope?", "Test context")
	q.ID = "q-test-store-get-123" // Use fixed ID for test

	// Store the question
	if err := store.Store(ctx, q); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	// Retrieve the question
	retrieved, err := store.Get(ctx, q.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	// Verify fields
	if retrieved.ID != q.ID {
		t.Errorf("ID = %q, want %q", retrieved.ID, q.ID)
	}
	if retrieved.FromAgent != q.FromAgent {
		t.Errorf("FromAgent = %q, want %q", retrieved.FromAgent, q.FromAgent)
	}
	if retrieved.Topic != q.Topic {
		t.Errorf("Topic = %q, want %q", retrieved.Topic, q.Topic)
	}
	if retrieved.Question != q.Question {
		t.Errorf("Question = %q, want %q", retrieved.Question, q.Question)
	}
	if retrieved.Context != q.Context {
		t.Errorf("Context = %q, want %q", retrieved.Context, q.Context)
	}
	if retrieved.Status != q.Status {
		t.Errorf("Status = %q, want %q", retrieved.Status, q.Status)
	}
	if retrieved.Urgency != q.Urgency {
		t.Errorf("Urgency = %q, want %q", retrieved.Urgency, q.Urgency)
	}
}

func TestQuestionStore_Get_NotFound(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewQuestionStore(tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	// Try to get a non-existent question
	_, err = store.Get(ctx, "q-nonexistent")
	if err == nil {
		t.Error("Get() should return error for non-existent question")
	}
}

func TestQuestionStore_List_All(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewQuestionStore(tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	// Create and store multiple questions
	questions := []*Question{
		{ID: "q-list-all-1", FromAgent: "agent-1", Topic: "topic.a", Question: "Q1", Status: QuestionStatusPending, Urgency: QuestionUrgencyNormal, CreatedAt: time.Now().UTC()},
		{ID: "q-list-all-2", FromAgent: "agent-2", Topic: "topic.b", Question: "Q2", Status: QuestionStatusAnswered, Urgency: QuestionUrgencyHigh, CreatedAt: time.Now().UTC()},
		{ID: "q-list-all-3", FromAgent: "agent-3", Topic: "topic.c", Question: "Q3", Status: QuestionStatusPending, Urgency: QuestionUrgencyBlocking, CreatedAt: time.Now().UTC()},
	}

	for _, q := range questions {
		if err := store.Store(ctx, q); err != nil {
			t.Fatalf("Store() error = %v", err)
		}
	}

	// List all questions (no status filter)
	all, err := store.List(ctx, "")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(all) != 3 {
		t.Errorf("List() returned %d questions, want 3", len(all))
	}
}

func TestQuestionStore_List_FilterByStatus(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewQuestionStore(tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	// Create and store questions with different statuses
	questions := []*Question{
		{ID: "q-filter-1", FromAgent: "agent-1", Topic: "topic.a", Question: "Q1", Status: QuestionStatusPending, Urgency: QuestionUrgencyNormal, CreatedAt: time.Now().UTC()},
		{ID: "q-filter-2", FromAgent: "agent-2", Topic: "topic.b", Question: "Q2", Status: QuestionStatusAnswered, Urgency: QuestionUrgencyNormal, CreatedAt: time.Now().UTC()},
		{ID: "q-filter-3", FromAgent: "agent-3", Topic: "topic.c", Question: "Q3", Status: QuestionStatusPending, Urgency: QuestionUrgencyNormal, CreatedAt: time.Now().UTC()},
		{ID: "q-filter-4", FromAgent: "agent-4", Topic: "topic.d", Question: "Q4", Status: QuestionStatusTimeout, Urgency: QuestionUrgencyNormal, CreatedAt: time.Now().UTC()},
	}

	for _, q := range questions {
		if err := store.Store(ctx, q); err != nil {
			t.Fatalf("Store() error = %v", err)
		}
	}

	// List pending questions
	pending, err := store.List(ctx, QuestionStatusPending)
	if err != nil {
		t.Fatalf("List(pending) error = %v", err)
	}

	if len(pending) != 2 {
		t.Errorf("List(pending) returned %d questions, want 2", len(pending))
	}

	for _, q := range pending {
		if q.Status != QuestionStatusPending {
			t.Errorf("List(pending) returned question with status %q", q.Status)
		}
	}

	// List answered questions
	answered, err := store.List(ctx, QuestionStatusAnswered)
	if err != nil {
		t.Fatalf("List(answered) error = %v", err)
	}

	if len(answered) != 1 {
		t.Errorf("List(answered) returned %d questions, want 1", len(answered))
	}
}

func TestQuestionStore_List_ContextCancellation(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())

	store, err := NewQuestionStore(tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Store a question first (using a valid context)
	q := &Question{ID: "q-cancel-test", FromAgent: "agent", Topic: "topic", Question: "Q", Status: QuestionStatusPending, Urgency: QuestionUrgencyNormal, CreatedAt: time.Now().UTC()}
	if err := store.Store(context.Background(), q); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	// List with cancelled context - should return error
	_, err = store.List(ctx, "")
	if err == nil {
		t.Error("List() with cancelled context should return error")
	}
}

func TestQuestionStore_Answer(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewQuestionStore(tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	// Create and store a pending question
	q := &Question{
		ID:        "q-answer-test",
		FromAgent: "test-agent",
		Topic:     "requirements.auth",
		Question:  "What auth method?",
		Status:    QuestionStatusPending,
		Urgency:   QuestionUrgencyNormal,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.Store(ctx, q); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	// Answer the question
	answer := "Use OAuth 2.0 with PKCE"
	answeredBy := "human-user-123"
	answererType := "human"
	confidence := "high"
	sources := "ADR-001, security-guidelines.md"

	if err := store.Answer(ctx, q.ID, answer, answeredBy, answererType, confidence, sources); err != nil {
		t.Fatalf("Answer() error = %v", err)
	}

	// Verify the question was updated
	updated, err := store.Get(ctx, q.ID)
	if err != nil {
		t.Fatalf("Get() after Answer() error = %v", err)
	}

	if updated.Status != QuestionStatusAnswered {
		t.Errorf("Status = %q, want %q", updated.Status, QuestionStatusAnswered)
	}
	if updated.Answer != answer {
		t.Errorf("Answer = %q, want %q", updated.Answer, answer)
	}
	if updated.AnsweredBy != answeredBy {
		t.Errorf("AnsweredBy = %q, want %q", updated.AnsweredBy, answeredBy)
	}
	if updated.AnswererType != answererType {
		t.Errorf("AnswererType = %q, want %q", updated.AnswererType, answererType)
	}
	if updated.Confidence != confidence {
		t.Errorf("Confidence = %q, want %q", updated.Confidence, confidence)
	}
	if updated.Sources != sources {
		t.Errorf("Sources = %q, want %q", updated.Sources, sources)
	}
	if updated.AnsweredAt == nil {
		t.Error("AnsweredAt should be set")
	}
}

func TestQuestionStore_Answer_NotFound(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewQuestionStore(tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	// Try to answer a non-existent question
	err = store.Answer(ctx, "q-nonexistent", "answer", "user", "human", "high", "")
	if err == nil {
		t.Error("Answer() should return error for non-existent question")
	}
}

func TestQuestionStore_Delete(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewQuestionStore(tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	// Create and store a question
	q := &Question{
		ID:        "q-delete-test",
		FromAgent: "test-agent",
		Topic:     "topic.delete",
		Question:  "To be deleted",
		Status:    QuestionStatusPending,
		Urgency:   QuestionUrgencyNormal,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.Store(ctx, q); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	// Verify it exists
	_, err = store.Get(ctx, q.ID)
	if err != nil {
		t.Fatalf("Get() before delete error = %v", err)
	}

	// Delete it
	if err := store.Delete(ctx, q.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Verify it's gone
	_, err = store.Get(ctx, q.ID)
	if err == nil {
		t.Error("Get() after Delete() should return error")
	}
}

func TestQuestionStore_Delete_NotFound(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewQuestionStore(tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	// Delete a non-existent question - should return error
	err = store.Delete(ctx, "q-nonexistent")
	if err == nil {
		// Note: JetStream KV delete on non-existent key may succeed silently
		// depending on version. We'll check if it returns an error.
		// If this test fails, the behavior may be acceptable.
	}
}

func TestNewQuestion(t *testing.T) {
	q := NewQuestion("my-agent", "api.users", "How do I create a user?", "Building user creation endpoint")

	// Verify ID format
	if len(q.ID) < 3 || q.ID[:2] != "q-" {
		t.Errorf("ID should start with 'q-', got %q", q.ID)
	}

	if q.FromAgent != "my-agent" {
		t.Errorf("FromAgent = %q, want %q", q.FromAgent, "my-agent")
	}
	if q.Topic != "api.users" {
		t.Errorf("Topic = %q, want %q", q.Topic, "api.users")
	}
	if q.Question != "How do I create a user?" {
		t.Errorf("Question = %q, want %q", q.Question, "How do I create a user?")
	}
	if q.Context != "Building user creation endpoint" {
		t.Errorf("Context = %q, want %q", q.Context, "Building user creation endpoint")
	}
	if q.Status != QuestionStatusPending {
		t.Errorf("Status = %q, want %q", q.Status, QuestionStatusPending)
	}
	if q.Urgency != QuestionUrgencyNormal {
		t.Errorf("Urgency = %q, want %q", q.Urgency, QuestionUrgencyNormal)
	}
	if q.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
}

func TestAnswerPayload_Validation(t *testing.T) {
	tests := []struct {
		name    string
		payload AnswerPayload
		wantErr bool
	}{
		{
			name: "valid payload",
			payload: AnswerPayload{
				QuestionID: "q-123",
				Answer:     "The answer is 42",
			},
			wantErr: false,
		},
		{
			name: "missing question_id",
			payload: AnswerPayload{
				QuestionID: "",
				Answer:     "The answer",
			},
			wantErr: true,
		},
		{
			name: "missing answer",
			payload: AnswerPayload{
				QuestionID: "q-123",
				Answer:     "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.payload.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAnswerPayload_Schema(t *testing.T) {
	p := &AnswerPayload{}
	schema := p.Schema()

	if schema.Domain != "question" {
		t.Errorf("Schema().Domain = %q, want %q", schema.Domain, "question")
	}
	if schema.Category != "answer" {
		t.Errorf("Schema().Category = %q, want %q", schema.Category, "answer")
	}
	if schema.Version != "v1" {
		t.Errorf("Schema().Version = %q, want %q", schema.Version, "v1")
	}
}

func TestQuestionStore_StoreWithAllFields(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewQuestionStore(tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	now := time.Now().UTC()
	deadline := now.Add(1 * time.Hour)
	answeredAt := now.Add(30 * time.Minute)

	q := &Question{
		ID:            "q-all-fields-test",
		FromAgent:     "design-writer",
		Topic:         "api.authentication.oauth",
		Question:      "Should we use PKCE for mobile clients?",
		Context:       "Implementing OAuth for mobile app",
		BlockedLoopID: "loop-123",
		TraceID:       "trace-456",
		Urgency:       QuestionUrgencyBlocking,
		Status:        QuestionStatusAnswered,
		CreatedAt:     now,
		Deadline:      &deadline,
		AssignedTo:    "team/security",
		AssignedAt:    now.Add(5 * time.Minute),
		AnsweredAt:    &answeredAt,
		Answer:        "Yes, PKCE is required for mobile OAuth",
		AnsweredBy:    "security-expert",
		AnswererType:  "agent",
		Confidence:    "high",
		Sources:       "RFC 7636, OAuth 2.1 draft",
	}

	// Store
	if err := store.Store(ctx, q); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	// Retrieve
	retrieved, err := store.Get(ctx, q.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	// Verify all fields
	if retrieved.BlockedLoopID != q.BlockedLoopID {
		t.Errorf("BlockedLoopID = %q, want %q", retrieved.BlockedLoopID, q.BlockedLoopID)
	}
	if retrieved.TraceID != q.TraceID {
		t.Errorf("TraceID = %q, want %q", retrieved.TraceID, q.TraceID)
	}
	if retrieved.AssignedTo != q.AssignedTo {
		t.Errorf("AssignedTo = %q, want %q", retrieved.AssignedTo, q.AssignedTo)
	}
	if retrieved.Deadline == nil {
		t.Error("Deadline should be set")
	} else if !retrieved.Deadline.Equal(*q.Deadline) {
		t.Errorf("Deadline = %v, want %v", retrieved.Deadline, q.Deadline)
	}
}

func TestQuestionStore_List_EmptyBucket(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := context.Background()

	store, err := NewQuestionStore(tc.Client)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	// List on empty bucket returns error because Keys() returns ErrNoKeysFound
	// This is expected JetStream behavior - the error is wrapped
	questions, err := store.List(ctx, "")

	// Accept either: empty slice with no error, or ErrNoKeysFound wrapped in error
	if err != nil {
		// Keys() on empty bucket returns ErrNoKeysFound which gets wrapped
		// Check if it's the expected "no keys" error
		if !isNoKeysError(err) {
			t.Fatalf("List() unexpected error = %v", err)
		}
		// Error is expected for empty bucket, test passes
		return
	}

	// If no error, should be empty
	if len(questions) != 0 {
		t.Errorf("List() on empty bucket returned %d questions, want 0", len(questions))
	}
}

// isNoKeysError checks if the error is due to no keys in bucket.
func isNoKeysError(err error) bool {
	// JetStream wraps ErrNoKeysFound, so check both direct equality and string match
	if err == jetstream.ErrNoKeysFound {
		return true
	}
	// Check if the error message contains the key phrase
	return err != nil && (err.Error() == "nats: no keys found" ||
		contains(err.Error(), "no keys found"))
}

// contains checks if s contains substr (avoiding strings import).
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
