package llm

import (
	"encoding/json"
	"testing"
)

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantKey string // if non-empty, check this key exists in parsed JSON
		wantErr bool
	}{
		{
			name:    "plain JSON",
			input:   `{"goal": "test"}`,
			wantKey: "goal",
		},
		{
			name:    "markdown code block",
			input:   "```json\n{\"goal\": \"test\"}\n```",
			wantKey: "goal",
		},
		{
			name:    "markdown block with trailing text",
			input:   "```json\n{\"goal\": \"test\"}\n```\n\n**Some extra text here**",
			wantKey: "goal",
		},
		{
			name: "JS comments in values",
			input: "```json\n{\n  \"scope\": {\n    \"include\": [\n      \"src/routes/api.js\",          // File where routes are defined\n      \"src/controllers/apiController.js\"  // Handler file\n    ]\n  }\n}\n```",
			wantKey: "scope",
		},
		{
			name: "JS comments and trailing commas",
			input: "```json\n{\n  \"items\": [\n    \"one\",  // first\n    \"two\",  // second\n  ]\n}\n```",
			wantKey: "items",
		},
		{
			name:    "URL in string not stripped",
			input:   `{"url": "http://example.com/path"}`,
			wantKey: "url",
		},
		{
			name:    "URL in string with comment after",
			input:   "{\"url\": \"http://example.com/path\"} // trailing",
			wantKey: "url",
		},
		{
			name: "complex real-world response",
			input: "```json\n{\n  \"goal\": \"Add a /goodbye endpoint\",\n  \"context\": \"The API has routes\",\n  \"scope\": {\n    \"include\": [\n      \"src/routes/api.js\",          // File where routes are defined\n      \"src/controllers/apiController.js\"  // File where request handlers are implemented\n    ],\n    \"exclude\": [\n      \"src/client/components\",        // Frontend UI components, not directly related to API\n      \"src/database/models\",           // Database models\n      \"src/config\",                  // Configuration files\n      \"src/middleware\"                // Middleware files\n    ],\n    \"do_not_touch\": [\n      \"src/routes/auth.js\",           // Authentication routes\n      \"src/controllers/authController.js\"  // Authentication controllers\n    ]\n  }\n}\n```\n\n**Dependencies and Concerns:**\n\n1. **Frontend Integration**: Ensure the UI is updated.\n2. **Testing**: Write tests.\n3. **Documentation**: Update docs.",
			wantKey: "goal",
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
		{
			name:    "no JSON at all",
			input:   "This is just text with no JSON.",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractJSON(tt.input)

			if tt.wantErr {
				if result != "" {
					t.Errorf("expected empty result, got: %s", result)
				}
				return
			}

			if result == "" {
				t.Fatal("expected JSON result, got empty string")
			}

			// Verify it's valid JSON
			var parsed map[string]any
			if err := json.Unmarshal([]byte(result), &parsed); err != nil {
				t.Fatalf("result is not valid JSON: %v\nresult: %s", err, result)
			}

			if tt.wantKey != "" {
				if _, ok := parsed[tt.wantKey]; !ok {
					t.Errorf("expected key %q in parsed JSON, got keys: %v", tt.wantKey, keysOf(parsed))
				}
			}
		})
	}
}

func TestExtractJSONArray(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantLen int
	}{
		{
			name:    "plain array",
			input:   `["one", "two"]`,
			wantLen: 2,
		},
		{
			name:    "markdown code block array",
			input:   "```json\n[\"one\", \"two\"]\n```",
			wantLen: 2,
		},
		{
			name:    "array with comments",
			input:   "```json\n[\n  \"one\",  // first\n  \"two\"   // second\n]\n```",
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractJSONArray(tt.input)
			if result == "" {
				t.Fatal("expected result, got empty string")
			}

			var parsed []any
			if err := json.Unmarshal([]byte(result), &parsed); err != nil {
				t.Fatalf("result is not valid JSON array: %v\nresult: %s", err, result)
			}

			if len(parsed) != tt.wantLen {
				t.Errorf("expected array length %d, got %d", tt.wantLen, len(parsed))
			}
		})
	}
}

func TestStripLineComment(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no comment",
			input:    `  "key": "value",`,
			expected: `  "key": "value",`,
		},
		{
			name:     "trailing comment",
			input:    `  "key": "value",  // a comment`,
			expected: `  "key": "value",`,
		},
		{
			name:     "URL in string preserved",
			input:    `  "url": "http://example.com",`,
			expected: `  "url": "http://example.com",`,
		},
		{
			name:     "URL with trailing comment",
			input:    `  "url": "http://example.com",  // the url`,
			expected: `  "url": "http://example.com",`,
		},
		{
			name:     "whole line comment",
			input:    `  // This is a comment`,
			expected: ``,
		},
		{
			name:     "escaped quote in string",
			input:    `  "path": "a\"b//c",  // comment`,
			expected: `  "path": "a\"b//c",`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripLineComment(tt.input)
			if got != tt.expected {
				t.Errorf("stripLineComment(%q)\ngot:  %q\nwant: %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestCleanJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "trailing comma in array",
			input: `{"items": ["one", "two",]}`,
		},
		{
			name:  "trailing comma in object",
			input: `{"a": 1, "b": 2,}`,
		},
		{
			name:  "comments and trailing commas",
			input: "{\n  \"items\": [\n    \"one\",  // first\n    \"two\",  // second\n  ]\n}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanJSON(tt.input)

			var parsed any
			if err := json.Unmarshal([]byte(result), &parsed); err != nil {
				t.Fatalf("cleaned JSON is invalid: %v\nresult: %s", err, result)
			}
		})
	}
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
