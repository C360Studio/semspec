package trajectoryapi

import (
	"testing"
	"time"

	"github.com/c360studio/semspec/vocabulary/semspec"
)

func TestEntityToCallRecord(t *testing.T) {
	startTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	endTime := time.Date(2024, 1, 15, 10, 30, 5, 0, time.UTC)

	entity := graphEntity{
		ID: "local.semspec.llm.call.project.req-123",
		Triples: []graphTriple{
			{Predicate: semspec.PredicateActivityType, Object: "model_call"},
			{Predicate: semspec.ActivityLoop, Object: "loop-456"},
			{Predicate: semspec.DCIdentifier, Object: "trace-789"},
			{Predicate: semspec.LLMRequestID, Object: "req-123"},
			{Predicate: semspec.LLMCapability, Object: "planning"},
			{Predicate: semspec.ActivityModel, Object: "claude-3-sonnet"},
			{Predicate: semspec.LLMProvider, Object: "anthropic"},
			{Predicate: semspec.ActivityTokensIn, Object: float64(1000)},
			{Predicate: semspec.ActivityTokensOut, Object: float64(500)},
			{Predicate: semspec.ActivityDuration, Object: float64(5000)},
			{Predicate: semspec.LLMFinishReason, Object: "stop"},
			{Predicate: semspec.LLMContextBudget, Object: float64(128000)},
			{Predicate: semspec.LLMContextTruncated, Object: true},
			{Predicate: semspec.LLMRetries, Object: float64(1)},
			{Predicate: semspec.LLMFallback, Object: "claude-3-haiku"},
			{Predicate: semspec.LLMMessagesCount, Object: float64(5)},
			{Predicate: semspec.LLMResponsePreview, Object: "Here is the plan..."},
			{Predicate: semspec.ActivityStartedAt, Object: startTime.Format(time.RFC3339)},
			{Predicate: semspec.ActivityEndedAt, Object: endTime.Format(time.RFC3339)},
		},
	}

	record := entityToCallRecord(entity)

	// Verify all fields are correctly extracted
	if record.LoopID != "loop-456" {
		t.Errorf("LoopID = %q, want %q", record.LoopID, "loop-456")
	}
	if record.TraceID != "trace-789" {
		t.Errorf("TraceID = %q, want %q", record.TraceID, "trace-789")
	}
	if record.RequestID != "req-123" {
		t.Errorf("RequestID = %q, want %q", record.RequestID, "req-123")
	}
	if record.Capability != "planning" {
		t.Errorf("Capability = %q, want %q", record.Capability, "planning")
	}
	if record.Model != "claude-3-sonnet" {
		t.Errorf("Model = %q, want %q", record.Model, "claude-3-sonnet")
	}
	if record.Provider != "anthropic" {
		t.Errorf("Provider = %q, want %q", record.Provider, "anthropic")
	}
	if record.PromptTokens != 1000 {
		t.Errorf("PromptTokens = %d, want %d", record.PromptTokens, 1000)
	}
	if record.CompletionTokens != 500 {
		t.Errorf("CompletionTokens = %d, want %d", record.CompletionTokens, 500)
	}
	if record.TotalTokens != 1500 {
		t.Errorf("TotalTokens = %d, want %d", record.TotalTokens, 1500)
	}
	if record.DurationMs != 5000 {
		t.Errorf("DurationMs = %d, want %d", record.DurationMs, 5000)
	}
	if record.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", record.FinishReason, "stop")
	}
	if record.ContextBudget != 128000 {
		t.Errorf("ContextBudget = %d, want %d", record.ContextBudget, 128000)
	}
	if !record.ContextTruncated {
		t.Error("ContextTruncated = false, want true")
	}
	if record.Retries != 1 {
		t.Errorf("Retries = %d, want %d", record.Retries, 1)
	}
	if len(record.FallbacksUsed) != 1 || record.FallbacksUsed[0] != "claude-3-haiku" {
		t.Errorf("FallbacksUsed = %v, want [claude-3-haiku]", record.FallbacksUsed)
	}
	if record.MessagesCount != 5 {
		t.Errorf("MessagesCount = %d, want %d", record.MessagesCount, 5)
	}
	if record.ResponsePreview != "Here is the plan..." {
		t.Errorf("ResponsePreview = %q, want %q", record.ResponsePreview, "Here is the plan...")
	}
	if !record.StartedAt.Equal(startTime) {
		t.Errorf("StartedAt = %v, want %v", record.StartedAt, startTime)
	}
	if !record.CompletedAt.Equal(endTime) {
		t.Errorf("CompletedAt = %v, want %v", record.CompletedAt, endTime)
	}
}

func TestIsLLMCallEntity(t *testing.T) {
	tests := []struct {
		name   string
		entity graphEntity
		want   bool
	}{
		{
			name: "model_call entity",
			entity: graphEntity{
				Triples: []graphTriple{
					{Predicate: semspec.PredicateActivityType, Object: "model_call"},
				},
			},
			want: true,
		},
		{
			name: "tool_call entity",
			entity: graphEntity{
				Triples: []graphTriple{
					{Predicate: semspec.PredicateActivityType, Object: "tool_call"},
				},
			},
			want: false,
		},
		{
			name: "entity without type",
			entity: graphEntity{
				Triples: []graphTriple{
					{Predicate: semspec.ActivityModel, Object: "claude-3"},
				},
			},
			want: false,
		},
		{
			name: "empty entity",
			entity: graphEntity{
				Triples: []graphTriple{},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isLLMCallEntity(tt.entity)
			if got != tt.want {
				t.Errorf("isLLMCallEntity() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseGraphEntity(t *testing.T) {
	entityMap := map[string]any{
		"id": "test.entity.123",
		"triples": []any{
			map[string]any{
				"predicate": "test.predicate",
				"object":    "test value",
			},
			map[string]any{
				"predicate": "test.number",
				"object":    float64(42),
			},
		},
	}

	entity := parseGraphEntity(entityMap)

	if entity.ID != "test.entity.123" {
		t.Errorf("ID = %q, want %q", entity.ID, "test.entity.123")
	}
	if len(entity.Triples) != 2 {
		t.Fatalf("len(Triples) = %d, want %d", len(entity.Triples), 2)
	}
	if entity.Triples[0].Predicate != "test.predicate" {
		t.Errorf("Triples[0].Predicate = %q, want %q", entity.Triples[0].Predicate, "test.predicate")
	}
	if entity.Triples[0].Object != "test value" {
		t.Errorf("Triples[0].Object = %v, want %v", entity.Triples[0].Object, "test value")
	}
}

func TestGetString(t *testing.T) {
	predicates := map[string]any{
		"string_val":  "hello",
		"float_val":   float64(123.45),
		"int_val":     42,
		"missing_key": nil,
	}

	if got := getString(predicates, "string_val"); got != "hello" {
		t.Errorf("getString(string_val) = %q, want %q", got, "hello")
	}
	if got := getString(predicates, "float_val"); got != "123.45" {
		t.Errorf("getString(float_val) = %q, want %q", got, "123.45")
	}
	if got := getString(predicates, "nonexistent"); got != "" {
		t.Errorf("getString(nonexistent) = %q, want empty", got)
	}
}

func TestGetInt(t *testing.T) {
	predicates := map[string]any{
		"float_val":  float64(42),
		"int_val":    100,
		"string_val": "200",
	}

	if got := getInt(predicates, "float_val"); got != 42 {
		t.Errorf("getInt(float_val) = %d, want %d", got, 42)
	}
	if got := getInt(predicates, "int_val"); got != 100 {
		t.Errorf("getInt(int_val) = %d, want %d", got, 100)
	}
	if got := getInt(predicates, "string_val"); got != 200 {
		t.Errorf("getInt(string_val) = %d, want %d", got, 200)
	}
	if got := getInt(predicates, "nonexistent"); got != 0 {
		t.Errorf("getInt(nonexistent) = %d, want %d", got, 0)
	}
}

func TestGetBool(t *testing.T) {
	predicates := map[string]any{
		"bool_true":   true,
		"bool_false":  false,
		"string_true": "true",
		"string_TRUE": "TRUE",
	}

	if got := getBool(predicates, "bool_true"); !got {
		t.Error("getBool(bool_true) = false, want true")
	}
	if got := getBool(predicates, "bool_false"); got {
		t.Error("getBool(bool_false) = true, want false")
	}
	if got := getBool(predicates, "string_true"); !got {
		t.Error("getBool(string_true) = false, want true")
	}
	if got := getBool(predicates, "string_TRUE"); !got {
		t.Error("getBool(string_TRUE) = false, want true")
	}
	if got := getBool(predicates, "nonexistent"); got {
		t.Error("getBool(nonexistent) = true, want false")
	}
}

func TestEntityToCallRecord_WithError(t *testing.T) {
	entity := graphEntity{
		ID: "local.semspec.llm.call.project.req-err",
		Triples: []graphTriple{
			{Predicate: semspec.PredicateActivityType, Object: "model_call"},
			{Predicate: semspec.LLMRequestID, Object: "req-err"},
			{Predicate: semspec.ActivityError, Object: "rate limit exceeded"},
			{Predicate: semspec.ActivitySuccess, Object: false},
		},
	}

	record := entityToCallRecord(entity)

	if record.Error != "rate limit exceeded" {
		t.Errorf("Error = %q, want %q", record.Error, "rate limit exceeded")
	}
	if record.RequestID != "req-err" {
		t.Errorf("RequestID = %q, want %q", record.RequestID, "req-err")
	}
}

func TestEntityToCallRecord_MultipleFallbacks(t *testing.T) {
	entity := graphEntity{
		ID: "local.semspec.llm.call.project.req-multi",
		Triples: []graphTriple{
			{Predicate: semspec.PredicateActivityType, Object: "model_call"},
			{Predicate: semspec.LLMFallback, Object: "claude-3-haiku"},
			{Predicate: semspec.LLMFallback, Object: "gpt-4"},
			{Predicate: semspec.LLMFallback, Object: "llama-3"},
		},
	}

	record := entityToCallRecord(entity)

	if len(record.FallbacksUsed) != 3 {
		t.Fatalf("len(FallbacksUsed) = %d, want %d", len(record.FallbacksUsed), 3)
	}
	expected := []string{"claude-3-haiku", "gpt-4", "llama-3"}
	for i, want := range expected {
		if record.FallbacksUsed[i] != want {
			t.Errorf("FallbacksUsed[%d] = %q, want %q", i, record.FallbacksUsed[i], want)
		}
	}
}

func TestSanitizeGraphQLString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "normal string unchanged",
			input: "loop-123-abc",
			want:  "loop-123-abc",
		},
		{
			name:  "null byte removed",
			input: "loop\x00id",
			want:  "loopid",
		},
		{
			name:  "backslash escaped",
			input: "loop\\id",
			want:  "loop\\\\id",
		},
		{
			name:  "both null and backslash",
			input: "loop\x00\\id",
			want:  "loop\\\\id",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "uuid format unchanged",
			input: "550e8400-e29b-41d4-a716-446655440000",
			want:  "550e8400-e29b-41d4-a716-446655440000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeGraphQLString(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeGraphQLString(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
