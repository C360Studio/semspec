package source

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/c360studio/semspec/llm"
	_ "github.com/c360studio/semspec/llm/providers"
	"github.com/c360studio/semspec/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyzer_Analyze(t *testing.T) {
	// Create mock LLM server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"model": "test-model",
			"choices": []map[string]any{
				{
					"message": map[string]string{
						"role": "assistant",
						"content": `{"category":"sop","applies_to":["*.go"],"severity":"error","summary":"Go error handling guidelines","requirements":["Always wrap errors with context","Never ignore errors"]}`,
					},
					"finish_reason": "stop",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	registry := model.NewRegistry(
		map[model.Capability]*model.CapabilityConfig{
			model.CapabilityFast: {
				Preferred: []string{"test-model"},
			},
		},
		map[string]*model.EndpointConfig{
			"test-model": {
				Provider: "ollama",
				URL:      server.URL,
				Model:    "test-model",
			},
		},
	)

	client := llm.NewClient(registry)
	analyzer := NewAnalyzer(client)

	result, err := analyzer.Analyze(context.Background(), "# Error Handling\n\nAlways wrap errors.")
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "sop", result.Category)
	assert.Equal(t, []string{"*.go"}, result.AppliesTo)
	assert.Equal(t, "error", result.Severity)
	assert.Equal(t, "Go error handling guidelines", result.Summary)
	assert.Len(t, result.Requirements, 2)
}

func TestParseAnalysisResponse_RawJSON(t *testing.T) {
	content := `{"category":"spec","applies_to":["*.ts"],"severity":"","summary":"TypeScript guidelines","requirements":[]}`

	result, err := parseAnalysisResponse(content)
	require.NoError(t, err)

	assert.Equal(t, "spec", result.Category)
	assert.Equal(t, []string{"*.ts"}, result.AppliesTo)
}

func TestParseAnalysisResponse_JSONInCodeBlock(t *testing.T) {
	content := "Here's the analysis:\n\n```json\n{\"category\":\"api\",\"applies_to\":[],\"severity\":\"\",\"summary\":\"API docs\",\"requirements\":[]}\n```"

	result, err := parseAnalysisResponse(content)
	require.NoError(t, err)

	assert.Equal(t, "api", result.Category)
}

func TestParseAnalysisResponse_JSONInPlainCodeBlock(t *testing.T) {
	content := "```\n{\"category\":\"reference\",\"applies_to\":[],\"severity\":\"\",\"summary\":\"Ref\",\"requirements\":[]}\n```"

	result, err := parseAnalysisResponse(content)
	require.NoError(t, err)

	assert.Equal(t, "reference", result.Category)
}

func TestParseAnalysisResponse_InvalidCategory(t *testing.T) {
	content := `{"category":"invalid","applies_to":[],"severity":"","summary":"","requirements":[]}`

	_, err := parseAnalysisResponse(content)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid category")
}

func TestParseAnalysisResponse_InvalidJSON(t *testing.T) {
	// Complete but malformed JSON - json.Decoder cannot parse this,
	// so extractJSON returns empty string resulting in "no JSON found"
	content := `{"category": "sop", "applies_to": invalid}`

	_, err := parseAnalysisResponse(content)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no JSON found")
}

func TestParseAnalysisResponse_NoJSON(t *testing.T) {
	content := "This response has no JSON at all."

	_, err := parseAnalysisResponse(content)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no JSON found")
}

func TestParseAnalysisResponse_InvalidSeverityNormalized(t *testing.T) {
	content := `{"category":"sop","applies_to":["*.go"],"severity":"critical","summary":"Test","requirements":[]}`

	result, err := parseAnalysisResponse(content)
	require.NoError(t, err)

	// Invalid severity should be normalized to "info"
	assert.Equal(t, "info", result.Severity)
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "raw JSON",
			content: `{"key":"value"}`,
			want:    `{"key":"value"}`,
		},
		{
			name:    "JSON with text before",
			content: `Here is the result: {"key":"value"}`,
			want:    `{"key":"value"}`,
		},
		{
			name:    "JSON in code block",
			content: "```json\n{\"key\":\"value\"}\n```",
			want:    `{"key":"value"}`,
		},
		{
			name:    "nested objects",
			content: `{"outer":{"inner":"value"}}`,
			want:    `{"outer":{"inner":"value"}}`,
		},
		{
			name:    "no JSON",
			content: "No JSON here",
			want:    "",
		},
		{
			name:    "braces in string values",
			content: `{"summary":"This handles {config} variables","category":"spec"}`,
			want:    `{"summary":"This handles {config} variables","category":"spec"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSON(tt.content)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTruncateForAnalysis(t *testing.T) {
	t.Run("short content unchanged", func(t *testing.T) {
		content := "Short content"
		result := truncateForAnalysis(content, 100)
		assert.Equal(t, content, result)
	})

	t.Run("long content truncated", func(t *testing.T) {
		content := "First paragraph.\n\nSecond paragraph.\n\nThird paragraph that is much longer."
		result := truncateForAnalysis(content, 40)

		assert.Contains(t, result, "[Content truncated")
		assert.Less(t, len(result), len(content))
	})

	t.Run("truncates at paragraph boundary", func(t *testing.T) {
		content := "First paragraph.\n\nSecond paragraph here."
		result := truncateForAnalysis(content, 30)

		// Should truncate at paragraph boundary
		assert.Contains(t, result, "First paragraph.")
	})
}

func TestIsValidCategory(t *testing.T) {
	valid := []string{"sop", "spec", "datasheet", "reference", "api"}
	for _, cat := range valid {
		assert.True(t, isValidCategory(cat), "should be valid: %s", cat)
	}

	invalid := []string{"", "invalid", "SOP", "SPEC"}
	for _, cat := range invalid {
		assert.False(t, isValidCategory(cat), "should be invalid: %s", cat)
	}
}

func TestIsValidSeverity(t *testing.T) {
	valid := []string{"error", "warning", "info"}
	for _, sev := range valid {
		assert.True(t, isValidSeverity(sev), "should be valid: %s", sev)
	}

	invalid := []string{"", "critical", "ERROR", "high"}
	for _, sev := range invalid {
		assert.False(t, isValidSeverity(sev), "should be invalid: %s", sev)
	}
}
