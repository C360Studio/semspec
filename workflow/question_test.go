package workflow

import (
	"testing"
)

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
