package contextbuilder

import (
	"testing"

	"github.com/c360studio/semspec/processor/context-builder/strategies"
	"github.com/c360studio/semspec/workflow"
)

func TestQAIntegrationMapUrgency(t *testing.T) {
	qa := &QAIntegration{}

	tests := []struct {
		name     string
		input    strategies.QuestionUrgency
		expected workflow.QuestionUrgency
	}{
		{"low", strategies.UrgencyLow, workflow.QuestionUrgencyLow},
		{"normal", strategies.UrgencyNormal, workflow.QuestionUrgencyNormal},
		{"high", strategies.UrgencyHigh, workflow.QuestionUrgencyHigh},
		{"blocking", strategies.UrgencyBlocking, workflow.QuestionUrgencyBlocking},
		{"unknown", strategies.QuestionUrgency("unknown"), workflow.QuestionUrgencyNormal},
		{"empty", strategies.QuestionUrgency(""), workflow.QuestionUrgencyNormal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := qa.mapUrgency(tt.input)
			if got != tt.expected {
				t.Errorf("mapUrgency(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestQAIntegrationAsUnanswered(t *testing.T) {
	qa := &QAIntegration{}

	questions := []strategies.Question{
		{Topic: "test.topic1", Question: "Q1?", Urgency: strategies.UrgencyHigh},
		{Topic: "test.topic2", Question: "Q2?", Urgency: strategies.UrgencyNormal},
	}

	result := qa.asUnanswered(questions)

	if len(result) != len(questions) {
		t.Errorf("asUnanswered() returned %d items, want %d", len(result), len(questions))
	}

	for i, aq := range result {
		if aq.Answered {
			t.Errorf("result[%d].Answered = true, want false", i)
		}
		if aq.Question.Topic != questions[i].Topic {
			t.Errorf("result[%d].Question.Topic = %q, want %q", i, aq.Question.Topic, questions[i].Topic)
		}
	}
}

func TestQAIntegrationEnrichWithAnswers(t *testing.T) {
	qa := &QAIntegration{}

	t.Run("enrich with answered questions", func(t *testing.T) {
		result := &strategies.StrategyResult{
			Documents:           make(map[string]string),
			Questions:           []strategies.Question{{Topic: "test.topic", Question: "Q1?"}},
			InsufficientContext: true,
		}

		answers := []AnsweredQuestion{
			{
				Question: strategies.Question{Topic: "test.topic", Question: "Q1?"},
				Answer:   "This is the answer.",
				Answered: true,
				Source:   "agent",
			},
		}

		enriched := qa.EnrichWithAnswers(result, answers)

		// Check that answer was added to documents (with index suffix)
		docKey := "__qa_answer__test.topic_0"
		if _, ok := enriched.Documents[docKey]; !ok {
			t.Error("Expected answer document to be added")
		}

		// Check that questions were cleared
		if len(enriched.Questions) != 0 {
			t.Errorf("Questions should be empty, got %d", len(enriched.Questions))
		}

		// Check that insufficient context flag was cleared
		if enriched.InsufficientContext {
			t.Error("InsufficientContext should be false after all questions answered")
		}
	})

	t.Run("preserve unanswered questions", func(t *testing.T) {
		result := &strategies.StrategyResult{
			Documents:           make(map[string]string),
			Questions:           []strategies.Question{{Topic: "test.topic", Question: "Q1?"}},
			InsufficientContext: true,
		}

		answers := []AnsweredQuestion{
			{
				Question: strategies.Question{Topic: "test.topic", Question: "Q1?"},
				Answer:   "",
				Answered: false,
				Source:   "",
			},
		}

		enriched := qa.EnrichWithAnswers(result, answers)

		// Check that unanswered question is preserved
		if len(enriched.Questions) != 1 {
			t.Errorf("Questions should have 1 item, got %d", len(enriched.Questions))
		}

		// Check that insufficient context flag is still true
		if !enriched.InsufficientContext {
			t.Error("InsufficientContext should remain true when questions unanswered")
		}
	})

	t.Run("partial answers", func(t *testing.T) {
		result := &strategies.StrategyResult{
			Documents: make(map[string]string),
			Questions: []strategies.Question{
				{Topic: "topic1", Question: "Q1?"},
				{Topic: "topic2", Question: "Q2?"},
			},
			InsufficientContext: true,
		}

		answers := []AnsweredQuestion{
			{
				Question: strategies.Question{Topic: "topic1", Question: "Q1?"},
				Answer:   "Answer to Q1",
				Answered: true,
				Source:   "agent",
			},
			{
				Question: strategies.Question{Topic: "topic2", Question: "Q2?"},
				Answer:   "",
				Answered: false,
				Source:   "",
			},
		}

		enriched := qa.EnrichWithAnswers(result, answers)

		// Check that answered question was added to documents (with index suffix)
		if _, ok := enriched.Documents["__qa_answer__topic1_0"]; !ok {
			t.Error("Expected answer document for topic1")
		}

		// Check that unanswered question is preserved
		if len(enriched.Questions) != 1 {
			t.Errorf("Questions should have 1 item, got %d", len(enriched.Questions))
		}
		if enriched.Questions[0].Topic != "topic2" {
			t.Errorf("Remaining question topic = %q, want topic2", enriched.Questions[0].Topic)
		}

		// Check that insufficient context flag is still true
		if !enriched.InsufficientContext {
			t.Error("InsufficientContext should remain true with unanswered questions")
		}
	})

	t.Run("nil result", func(t *testing.T) {
		result := qa.EnrichWithAnswers(nil, nil)
		if result != nil {
			t.Error("EnrichWithAnswers(nil) should return nil")
		}
	})
}

func TestQAIntegrationMapAnswers(t *testing.T) {
	qa := &QAIntegration{}

	original := []strategies.Question{
		{Topic: "topic1", Question: "Q1?"},
		{Topic: "topic2", Question: "Q2?"},
		{Topic: "topic3", Question: "Q3?"},
	}

	created := []*workflow.Question{
		{ID: "q-1", Topic: "topic1"},
		{ID: "q-2", Topic: "topic2"},
		// topic3 was not created
	}

	answers := map[string]*workflow.AnswerPayload{
		"q-1": {
			QuestionID:   "q-1",
			Answer:       "Answer 1",
			AnsweredBy:   "agent/architect",
			AnswererType: "agent",
		},
		// q-2 has no answer
	}

	result := qa.mapAnswers(original, created, answers)

	if len(result) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(result))
	}

	// First question should be answered
	if !result[0].Answered {
		t.Error("result[0] should be answered")
	}
	if result[0].Answer != "Answer 1" {
		t.Errorf("result[0].Answer = %q, want %q", result[0].Answer, "Answer 1")
	}
	if result[0].Source != "agent" {
		t.Errorf("result[0].Source = %q, want %q", result[0].Source, "agent")
	}

	// Second question should not be answered
	if result[1].Answered {
		t.Error("result[1] should not be answered")
	}

	// Third question was not created, should not be answered
	if result[2].Answered {
		t.Error("result[2] should not be answered (wasn't created)")
	}
}

func TestDefaultQAIntegrationConfig(t *testing.T) {
	config := DefaultQAIntegrationConfig()

	if config.BlockingTimeout == 0 {
		t.Error("BlockingTimeout should not be zero")
	}
	if !config.AllowBlocking {
		t.Error("AllowBlocking should default to true")
	}
	if config.SourceName == "" {
		t.Error("SourceName should not be empty")
	}
}

func TestStrategyQuestion(t *testing.T) {
	q := strategies.Question{
		Topic:    "architecture.scope",
		Question: "What is the scope?",
		Context:  "Planning task",
		Urgency:  strategies.UrgencyHigh,
	}

	if q.Topic != "architecture.scope" {
		t.Errorf("Topic = %q, want %q", q.Topic, "architecture.scope")
	}
	if q.Question != "What is the scope?" {
		t.Errorf("Question = %q, want %q", q.Question, "What is the scope?")
	}
	if q.Urgency != strategies.UrgencyHigh {
		t.Errorf("Urgency = %q, want %q", q.Urgency, strategies.UrgencyHigh)
	}
}

func TestStrategyResultWithQuestions(t *testing.T) {
	result := &strategies.StrategyResult{
		Documents: map[string]string{"test.md": "content"},
		Questions: []strategies.Question{
			{Topic: "topic1", Question: "Q1?", Urgency: strategies.UrgencyHigh},
			{Topic: "topic2", Question: "Q2?", Urgency: strategies.UrgencyBlocking},
		},
		InsufficientContext: true,
	}

	if len(result.Questions) != 2 {
		t.Errorf("Questions count = %d, want 2", len(result.Questions))
	}
	if !result.InsufficientContext {
		t.Error("InsufficientContext should be true")
	}
}
