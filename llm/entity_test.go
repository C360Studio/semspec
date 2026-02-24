package llm

import (
	"testing"
	"time"

	"github.com/c360studio/semspec/vocabulary/semspec"
)

func TestLLMCallEntity_EntityID(t *testing.T) {
	record := &CallRecord{
		RequestID: "req-123",
	}
	entity := NewLLMCallEntity(record, "myorg", "myproject")

	expected := "myorg.semspec.llm.call.myproject.req-123"
	if got := entity.EntityID(); got != expected {
		t.Errorf("EntityID() = %q, want %q", got, expected)
	}
}

func TestLLMCallEntity_Triples_BasicFields(t *testing.T) {
	now := time.Now()
	record := &CallRecord{
		RequestID:        "req-123",
		TraceID:          "trace-456",
		LoopID:           "loop-789",
		Capability:       "planning",
		Model:            "test-model",
		Provider:         "anthropic",
		PromptTokens:     100,
		CompletionTokens: 50,
		DurationMs:       500,
		FinishReason:     "stop",
		StartedAt:        now,
		CompletedAt:      now.Add(500 * time.Millisecond),
	}

	entity := NewLLMCallEntity(record, "local", "semspec")
	triples := entity.Triples()

	// Check that basic predicates are present
	predicateFound := make(map[string]bool)
	for _, triple := range triples {
		predicateFound[triple.Predicate] = true
	}

	requiredPredicates := []string{
		semspec.PredicateActivityType,
		semspec.ActivityModel,
		semspec.ActivityTokensIn,
		semspec.ActivityTokensOut,
		semspec.ActivityDuration,
		semspec.ActivitySuccess,
		semspec.ActivityStartedAt,
		semspec.ActivityEndedAt,
		semspec.LLMCapability,
		semspec.LLMProvider,
		semspec.LLMFinishReason,
		semspec.LLMRequestID,
		semspec.ActivityLoop,      // LoopID is set
		semspec.DCIdentifier,      // TraceID is set
	}

	for _, pred := range requiredPredicates {
		if !predicateFound[pred] {
			t.Errorf("expected predicate %q not found in triples", pred)
		}
	}
}

func TestLLMCallEntity_Triples_OptionalFields(t *testing.T) {
	record := &CallRecord{
		RequestID:        "req-123",
		Capability:       "planning",
		Model:            "test-model",
		Provider:         "anthropic",
		ContextBudget:    8192,
		ContextTruncated: true,
		Retries:          3,
		FallbacksUsed:    []string{"model-a", "model-b"},
		Messages:         []Message{{Role: "user", Content: "Hello"}},
		Response:         "Hi there!",
		StartedAt:        time.Now(),
		CompletedAt:      time.Now(),
	}

	entity := NewLLMCallEntity(record, "local", "semspec")
	triples := entity.Triples()

	// Check optional predicates
	predicateFound := make(map[string]bool)
	predicateCount := make(map[string]int)
	for _, triple := range triples {
		predicateFound[triple.Predicate] = true
		predicateCount[triple.Predicate]++
	}

	optionalPredicates := []string{
		semspec.LLMContextBudget,
		semspec.LLMContextTruncated,
		semspec.LLMRetries,
		semspec.LLMFallback,
		semspec.LLMMessagesCount,
		semspec.LLMResponsePreview,
	}

	for _, pred := range optionalPredicates {
		if !predicateFound[pred] {
			t.Errorf("expected optional predicate %q not found in triples", pred)
		}
	}

	// Verify multiple fallback entries
	if predicateCount[semspec.LLMFallback] != 2 {
		t.Errorf("expected 2 fallback triples, got %d", predicateCount[semspec.LLMFallback])
	}
}

func TestLLMCallEntity_Triples_ErrorRecord(t *testing.T) {
	record := &CallRecord{
		RequestID:  "req-123",
		Capability: "planning",
		Model:      "test-model",
		Provider:   "anthropic",
		Error:      "connection refused",
		StartedAt:  time.Now(),
		CompletedAt: time.Now(),
	}

	entity := NewLLMCallEntity(record, "local", "semspec")
	triples := entity.Triples()

	// Check that error predicate is present and success is false
	var foundError, foundSuccess bool
	var errorObj, successObj any
	for _, triple := range triples {
		if triple.Predicate == semspec.ActivityError {
			foundError = true
			errorObj = triple.Object
		}
		if triple.Predicate == semspec.ActivitySuccess {
			foundSuccess = true
			successObj = triple.Object
		}
	}

	if !foundError {
		t.Error("expected ActivityError predicate not found")
	} else if errorObj != "connection refused" {
		t.Errorf("expected error object 'connection refused', got %v", errorObj)
	}

	if !foundSuccess {
		t.Error("expected ActivitySuccess predicate not found")
	} else if successObj != false {
		t.Errorf("expected success object false, got %v", successObj)
	}
}

func TestLLMCallEntity_Triples_ResponsePreviewTruncation(t *testing.T) {
	// Create a response longer than 500 chars
	longResponse := ""
	for i := 0; i < 600; i++ {
		longResponse += "x"
	}

	record := &CallRecord{
		RequestID:   "req-123",
		Capability:  "planning",
		Model:       "test-model",
		Provider:    "anthropic",
		Response:    longResponse,
		StartedAt:   time.Now(),
		CompletedAt: time.Now(),
	}

	entity := NewLLMCallEntity(record, "local", "semspec")
	triples := entity.Triples()

	for _, triple := range triples {
		if triple.Predicate == semspec.LLMResponsePreview {
			preview := triple.Object.(string)
			if len(preview) > 510 { // 500 + "..." = 503, give some margin
				t.Errorf("response preview should be truncated, got length %d", len(preview))
			}
			if preview[len(preview)-3:] != "..." {
				t.Errorf("truncated preview should end with '...', got %q", preview[len(preview)-10:])
			}
		}
	}
}

func TestLLMCallEntity_Triples_NoOptionalFieldsWhenEmpty(t *testing.T) {
	record := &CallRecord{
		RequestID:   "req-123",
		Capability:  "planning",
		Model:       "test-model",
		Provider:    "anthropic",
		StartedAt:   time.Now(),
		CompletedAt: time.Now(),
		// No optional fields set
	}

	entity := NewLLMCallEntity(record, "local", "semspec")
	triples := entity.Triples()

	// These predicates should NOT be present when fields are empty/zero
	absentPredicates := []string{
		semspec.ActivityLoop,       // LoopID empty
		semspec.DCIdentifier,       // TraceID empty
		semspec.ActivityError,      // Error empty
		semspec.LLMContextBudget,   // ContextBudget 0
		semspec.LLMContextTruncated, // ContextTruncated false
		semspec.LLMRetries,          // Retries 0
		semspec.LLMFallback,         // FallbacksUsed empty
		semspec.LLMMessagesCount,    // Messages empty
		semspec.LLMResponsePreview,  // Response empty
	}

	predicateFound := make(map[string]bool)
	for _, triple := range triples {
		predicateFound[triple.Predicate] = true
	}

	for _, pred := range absentPredicates {
		if predicateFound[pred] {
			t.Errorf("predicate %q should not be present when field is empty/zero", pred)
		}
	}
}
