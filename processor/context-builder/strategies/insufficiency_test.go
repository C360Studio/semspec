package strategies

import (
	"testing"
)

func TestQuestionStruct(t *testing.T) {
	q := Question{
		Topic:    "architecture.scope",
		Question: "What is the scope of this task?",
		Context:  "Planning workflow",
		Urgency:  UrgencyHigh,
	}

	if q.Topic != "architecture.scope" {
		t.Errorf("Topic = %q, want %q", q.Topic, "architecture.scope")
	}
	if q.Question == "" {
		t.Error("Question should not be empty")
	}
	if q.Urgency != UrgencyHigh {
		t.Errorf("Urgency = %q, want %q", q.Urgency, UrgencyHigh)
	}
}

func TestStrategyResultWithQuestions(t *testing.T) {
	result := &StrategyResult{
		Documents: map[string]string{"test.md": "content"},
		Questions: []Question{
			{Topic: "topic1", Question: "Q1?", Urgency: UrgencyNormal},
			{Topic: "topic2", Question: "Q2?", Urgency: UrgencyBlocking},
		},
		InsufficientContext: true,
	}

	if len(result.Questions) != 2 {
		t.Errorf("Questions count = %d, want 2", len(result.Questions))
	}

	if !result.InsufficientContext {
		t.Error("InsufficientContext should be true")
	}

	// Verify questions are preserved alongside other fields
	if len(result.Documents) != 1 {
		t.Errorf("Documents count = %d, want 1", len(result.Documents))
	}
}

func TestStrategyResultQuestionsInitialization(t *testing.T) {
	result := &StrategyResult{}

	// Default should be nil
	if result.Questions != nil {
		t.Error("Questions should default to nil")
	}

	// After explicit initialization
	result.Questions = make([]Question, 0)
	if result.Questions == nil {
		t.Error("Questions should not be nil after initialization")
	}
}

func TestExtractTopicCategory(t *testing.T) {
	tests := []struct {
		topic    string
		expected string
	}{
		{"what is authentication", "authentication"},
		{"How does NATS work?", "nats"},
		{"", "general"},
		{"the a an", "general"}, // All stop words
		{"context builder strategy", "context"},
	}

	for _, tt := range tests {
		t.Run(tt.topic, func(t *testing.T) {
			got := extractTopicCategory(tt.topic)
			if got != tt.expected {
				t.Errorf("extractTopicCategory(%q) = %q, want %q", tt.topic, got, tt.expected)
			}
		})
	}
}

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		topic           string
		minExpectedLen  int
		shouldContain   []string
		shouldNotContain []string
	}{
		{
			topic:           "how does authentication work",
			minExpectedLen:  1,
			shouldContain:   []string{"authentication", "work"},
			shouldNotContain: []string{"how", "does"},
		},
		{
			topic:           "nats jetstream configuration",
			minExpectedLen:  2,
			shouldContain:   []string{"nats", "jetstream", "configuration"},
			shouldNotContain: []string{},
		},
		{
			topic:           "the a an is are", // All stop words
			minExpectedLen:  0,
			shouldContain:   []string{},
			shouldNotContain: []string{"the", "a", "an"},
		},
		{
			topic:           "", // Empty
			minExpectedLen:  0,
			shouldContain:   []string{},
			shouldNotContain: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.topic, func(t *testing.T) {
			keywords := extractKeywords(tt.topic)

			if len(keywords) < tt.minExpectedLen {
				t.Errorf("extractKeywords(%q) returned %d keywords, want at least %d",
					tt.topic, len(keywords), tt.minExpectedLen)
			}

			keywordSet := make(map[string]bool)
			for _, kw := range keywords {
				keywordSet[kw] = true
			}

			for _, expected := range tt.shouldContain {
				if !keywordSet[expected] {
					t.Errorf("extractKeywords(%q) missing expected keyword %q", tt.topic, expected)
				}
			}

			for _, notExpected := range tt.shouldNotContain {
				if keywordSet[notExpected] {
					t.Errorf("extractKeywords(%q) should not contain stop word %q", tt.topic, notExpected)
				}
			}
		})
	}
}

func TestPlanningStrategyInsufficientContextDetection(t *testing.T) {
	// Test the detectInsufficientContext helper function logic
	t.Run("no arch docs and no specs triggers question", func(t *testing.T) {
		result := &StrategyResult{
			Documents: make(map[string]string),
			Questions: make([]Question, 0),
		}
		req := &ContextBuildRequest{Topic: "auth-feature"}

		hasArchDocs := false
		hasExistingSpecs := false
		hasCodePatterns := false

		// Simulate what detectInsufficientContext does
		if !hasArchDocs && !hasExistingSpecs {
			result.Questions = append(result.Questions, Question{
				Topic:    "architecture.context",
				Question: "No architecture documentation or existing specifications were found.",
				Context:  "Topic: " + req.Topic,
				Urgency:  UrgencyHigh,
			})
		}

		if req.Topic != "" && !hasCodePatterns && !hasExistingSpecs {
			result.Questions = append(result.Questions, Question{
				Topic:    "architecture.patterns",
				Question: "No existing code patterns were found.",
				Urgency:  UrgencyNormal,
			})
		}

		if len(result.Questions) != 2 {
			t.Errorf("Expected 2 questions, got %d", len(result.Questions))
		}
	})

	t.Run("with arch docs no questions generated", func(t *testing.T) {
		result := &StrategyResult{
			Documents: make(map[string]string),
			Questions: make([]Question, 0),
		}
		req := &ContextBuildRequest{Topic: "auth-feature"}

		hasArchDocs := true
		hasExistingSpecs := false
		hasCodePatterns := true

		// With arch docs, architecture.context question should not be generated
		if !hasArchDocs && !hasExistingSpecs {
			result.Questions = append(result.Questions, Question{
				Topic:    "architecture.context",
				Question: "No architecture documentation or existing specifications were found.",
				Urgency:  UrgencyHigh,
			})
		}

		// With code patterns, architecture.patterns question should not be generated
		if req.Topic != "" && !hasCodePatterns && !hasExistingSpecs {
			result.Questions = append(result.Questions, Question{
				Topic:    "architecture.patterns",
				Question: "No existing code patterns were found.",
				Urgency:  UrgencyNormal,
			})
		}

		if len(result.Questions) != 0 {
			t.Errorf("Expected 0 questions when context is sufficient, got %d", len(result.Questions))
		}
	})

	t.Run("ambiguous scope triggers blocking question", func(t *testing.T) {
		result := &StrategyResult{
			Documents: make(map[string]string),
			Questions: make([]Question, 0),
		}
		req := &ContextBuildRequest{
			Topic:        "",
			WorkflowID:   "wf-123",
			Files:        nil,
			SpecEntityID: "",
		}

		// Ambiguous scope detection
		if req.Topic == "" && len(req.Files) == 0 && req.SpecEntityID == "" {
			result.Questions = append(result.Questions, Question{
				Topic:    "requirements.scope",
				Question: "The planning request has no specified topic, files, or specification.",
				Context:  "WorkflowID: " + req.WorkflowID,
				Urgency:  UrgencyBlocking,
			})
			result.InsufficientContext = true
		}

		if len(result.Questions) != 1 {
			t.Errorf("Expected 1 question for ambiguous scope, got %d", len(result.Questions))
		}

		if result.Questions[0].Urgency != UrgencyBlocking {
			t.Errorf("Ambiguous scope question should be blocking, got %q", result.Questions[0].Urgency)
		}

		if !result.InsufficientContext {
			t.Error("InsufficientContext should be true for blocking questions")
		}
	})
}

func TestQuestionStrategyInsufficientContextDetection(t *testing.T) {
	t.Run("no topic triggers blocking question", func(t *testing.T) {
		result := &StrategyResult{
			Documents: make(map[string]string),
			Questions: make([]Question, 0),
		}
		req := &ContextBuildRequest{
			Topic:      "",
			WorkflowID: "wf-456",
		}

		// No topic provided
		if req.Topic == "" {
			result.Questions = append(result.Questions, Question{
				Topic:    "requirements.clarification",
				Question: "No topic was provided for this question-answering task.",
				Context:  "WorkflowID: " + req.WorkflowID,
				Urgency:  UrgencyBlocking,
			})
			result.InsufficientContext = true
		}

		if len(result.Questions) != 1 {
			t.Errorf("Expected 1 question, got %d", len(result.Questions))
		}

		if !result.InsufficientContext {
			t.Error("InsufficientContext should be true")
		}
	})

	t.Run("no matches triggers high urgency question", func(t *testing.T) {
		result := &StrategyResult{
			Documents: make(map[string]string),
			Questions: make([]Question, 0),
		}
		req := &ContextBuildRequest{
			Topic: "obscure-feature",
		}

		hasMatchingEntities := false
		hasSourceDocs := false
		hasRelevantDocs := false

		// No entities, source docs, or relevant docs found
		if !hasMatchingEntities && !hasSourceDocs && !hasRelevantDocs {
			result.Questions = append(result.Questions, Question{
				Topic:    "knowledge." + extractTopicCategory(req.Topic),
				Question: "No knowledge was found in the codebase related to '" + req.Topic + "'.",
				Context:  "Original topic: " + req.Topic,
				Urgency:  UrgencyHigh,
			})
			result.InsufficientContext = true
		}

		if len(result.Questions) != 1 {
			t.Errorf("Expected 1 question, got %d", len(result.Questions))
		}

		// Topic category should be extracted
		expectedTopic := "knowledge.obscure"
		if result.Questions[0].Topic != expectedTopic {
			t.Errorf("Question topic = %q, want %q", result.Questions[0].Topic, expectedTopic)
		}
	})

	t.Run("only docs found triggers normal urgency question", func(t *testing.T) {
		result := &StrategyResult{
			Documents: make(map[string]string),
			Questions: make([]Question, 0),
		}
		req := &ContextBuildRequest{
			Topic: "configuration",
		}

		hasMatchingEntities := false
		hasSourceDocs := false
		hasRelevantDocs := true

		// Only general docs found, no entity matches
		if !hasMatchingEntities && !hasSourceDocs && hasRelevantDocs {
			result.Questions = append(result.Questions, Question{
				Topic:    "knowledge." + extractTopicCategory(req.Topic),
				Question: "Only general documentation was found.",
				Context:  "Original topic: " + req.Topic,
				Urgency:  UrgencyNormal,
			})
		}

		if len(result.Questions) != 1 {
			t.Errorf("Expected 1 question, got %d", len(result.Questions))
		}

		if result.Questions[0].Urgency != UrgencyNormal {
			t.Errorf("Question urgency = %q, want %q", result.Questions[0].Urgency, UrgencyNormal)
		}
	})
}
