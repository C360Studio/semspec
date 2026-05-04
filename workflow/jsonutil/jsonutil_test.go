package jsonutil

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
			name:    "JS comments in values",
			input:   "```json\n{\n  \"scope\": {\n    \"include\": [\n      \"src/routes/api.js\",          // File where routes are defined\n      \"src/controllers/apiController.js\"  // Handler file\n    ]\n  }\n}\n```",
			wantKey: "scope",
		},
		{
			name:    "JS comments and trailing commas",
			input:   "```json\n{\n  \"items\": [\n    \"one\",  // first\n    \"two\",  // second\n  ]\n}\n```",
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
			name:    "complex real-world response",
			input:   "```json\n{\n  \"goal\": \"Add a /goodbye endpoint\",\n  \"context\": \"The API has routes\",\n  \"scope\": {\n    \"include\": [\n      \"src/routes/api.js\",          // File where routes are defined\n      \"src/controllers/apiController.js\"  // File where request handlers are implemented\n    ],\n    \"exclude\": [\n      \"src/client/components\",        // Frontend UI components, not directly related to API\n      \"src/database/models\",           // Database models\n      \"src/config\",                  // Configuration files\n      \"src/middleware\"                // Middleware files\n    ],\n    \"do_not_touch\": [\n      \"src/routes/auth.js\",           // Authentication routes\n      \"src/controllers/authController.js\"  // Authentication controllers\n    ]\n  }\n}\n```\n\n**Dependencies and Concerns:**\n\n1. **Frontend Integration**: Ensure the UI is updated.\n2. **Testing**: Write tests.\n3. **Documentation**: Update docs.",
			wantKey: "goal",
		},
		{
			name:    "trailing backticks (Go 1.25 regression)",
			input:   "{\"verdict\": \"approved\", \"feedback\": \"looks good\"}\n```",
			wantKey: "verdict",
		},
		{
			name:    "JSON between backticks no newline",
			input:   "```{\"x\": 1}```",
			wantKey: "x",
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
			result, _, _ := cleanJSON(tt.input)

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

// ADR-035 audit sites A.1, A.3 — ParseStrict reports which named
// quirks (universal shape transforms) had to be applied to extract
// JSON. Pin the QuirksFired contract per quirk shape so a future
// caller migrating from ExtractJSON → ParseStrict can rely on the
// attribution being correct.

func TestParseStrict_QuirkAttribution(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantJSON  bool      // true if a non-empty JSON string is returned
		wantFired []QuirkID // quirks that should be present in QuirksFired (set membership, not order)
	}{
		{
			name:      "clean JSON — no quirks fire",
			input:     `{"a":1,"b":2}`,
			wantJSON:  true,
			wantFired: nil,
		},
		{
			name:      "fenced JSON — only fence quirk fires",
			input:     "```json\n{\"a\":1}\n```",
			wantJSON:  true,
			wantFired: []QuirkID{QuirkFencedJSONWrapper},
		},
		{
			name:      "JS line comments — only comments quirk fires",
			input:     "{\n  \"a\": 1, // first\n  \"b\": 2\n}",
			wantJSON:  true,
			wantFired: []QuirkID{QuirkJSLineComments},
		},
		{
			name:      "trailing comma — only commas quirk fires",
			input:     `{"items":["one","two",]}`,
			wantJSON:  true,
			wantFired: []QuirkID{QuirkTrailingCommas},
		},
		{
			name:      "fenced + comments + commas — all three fire",
			input:     "```json\n{\n  \"items\": [\n    \"one\",  // first\n    \"two\",  // second\n  ]\n}\n```",
			wantJSON:  true,
			wantFired: []QuirkID{QuirkFencedJSONWrapper, QuirkJSLineComments, QuirkTrailingCommas},
		},
		{
			name:      "no JSON at all — no quirks, empty result",
			input:     "this is just text",
			wantJSON:  false,
			wantFired: nil,
		},
		{
			name:      "empty input — no quirks, empty result",
			input:     "",
			wantJSON:  false,
			wantFired: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseStrict(tt.input)
			if tt.wantJSON && got.JSON == "" {
				t.Errorf("expected non-empty JSON, got empty")
			}
			if !tt.wantJSON && got.JSON != "" {
				t.Errorf("expected empty JSON, got %q", got.JSON)
			}
			fired := make(map[QuirkID]bool, len(got.QuirksFired))
			for _, q := range got.QuirksFired {
				fired[q] = true
			}
			for _, want := range tt.wantFired {
				if !fired[want] {
					t.Errorf("QuirksFired missing %q; got %v", want, got.QuirksFired)
				}
			}
			// Disallow unexpected fires — set membership both ways.
			expected := make(map[QuirkID]bool, len(tt.wantFired))
			for _, q := range tt.wantFired {
				expected[q] = true
			}
			for _, got := range got.QuirksFired {
				if !expected[got] {
					t.Errorf("QuirksFired contains unexpected %q; want only %v", got, tt.wantFired)
				}
			}
			// Pin deterministic ordering: fence → comments → commas.
			// The "fenced + comments + commas" case is the one input
			// where order is observable; assert it explicitly so a
			// future refactor that runs comment-stripping before
			// fence-extraction breaks loudly.
			if len(tt.wantFired) == 3 {
				wantOrder := []QuirkID{QuirkFencedJSONWrapper, QuirkJSLineComments, QuirkTrailingCommas}
				for i, q := range got.QuirksFired {
					if i >= len(wantOrder) || q != wantOrder[i] {
						t.Errorf("QuirksFired ordering = %v, want fence→comments→commas (%v)", got.QuirksFired, wantOrder)
						break
					}
				}
			}
		})
	}
}

// Pin the per-quirk counter increments. Reads Stats() before and after
// a ParseStrict call and asserts only the expected counters moved.
// Counters are package-level so they survive across tests in this run;
// the test computes deltas rather than asserting absolute values.
func TestParseStrict_CountersIncrement(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantDeltas  map[QuirkID]int64
		wantNoMoves []QuirkID
	}{
		{
			name:       "fenced + comments + commas",
			input:      "```json\n{\n  \"items\": [\n    \"one\",  // first\n    \"two\",  // second\n  ]\n}\n```",
			wantDeltas: map[QuirkID]int64{QuirkFencedJSONWrapper: 1, QuirkJSLineComments: 1, QuirkTrailingCommas: 1},
		},
		{
			name:        "clean JSON — no counter movement",
			input:       `{"a":1}`,
			wantNoMoves: []QuirkID{QuirkFencedJSONWrapper, QuirkJSLineComments, QuirkTrailingCommas},
		},
		{
			name:       "fenced only",
			input:      "```json\n{\"a\":1}\n```",
			wantDeltas: map[QuirkID]int64{QuirkFencedJSONWrapper: 1},
			wantNoMoves: []QuirkID{QuirkJSLineComments, QuirkTrailingCommas},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := Stats()
			ParseStrict(tt.input)
			after := Stats()
			for q, want := range tt.wantDeltas {
				delta := after[q] - before[q]
				if delta != want {
					t.Errorf("counter %s delta = %d, want %d", q, delta, want)
				}
			}
			for _, q := range tt.wantNoMoves {
				delta := after[q] - before[q]
				if delta != 0 {
					t.Errorf("counter %s should not have moved, delta = %d", q, delta)
				}
			}
		})
	}
}

// Stats() must include every known quirk even when it hasn't fired —
// "this quirk hasn't fired yet" is a distinct signal from "this quirk
// doesn't exist."
func TestStats_IncludesAllKnownQuirks(t *testing.T) {
	got := Stats()
	expected := []QuirkID{QuirkFencedJSONWrapper, QuirkJSLineComments, QuirkTrailingCommas}
	for _, q := range expected {
		if _, ok := got[q]; !ok {
			t.Errorf("Stats() missing entry for %q", q)
		}
	}
	if len(got) != len(expected) {
		t.Errorf("Stats() returned %d entries, want %d", len(got), len(expected))
	}
}

// ExtractJSONArray fires the same named quirks as ParseStrict so
// per-quirk counters cover array-shaped LLM output too. This was a
// reviewer-flagged gap — the function had its own inline fence regex
// that bypassed the named-quirk path.
func TestExtractJSONArray_FiresQuirks(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantDeltas map[QuirkID]int64
	}{
		{
			name:       "fenced array",
			input:      "```json\n[\"one\",\"two\"]\n```",
			wantDeltas: map[QuirkID]int64{QuirkFencedJSONWrapper: 1},
		},
		{
			name:       "array with comments + trailing commas",
			input:      "[\n  \"one\", // first\n  \"two\",\n]",
			wantDeltas: map[QuirkID]int64{QuirkJSLineComments: 1, QuirkTrailingCommas: 1},
		},
		{
			name:       "clean array — no quirks fire",
			input:      `["one","two"]`,
			wantDeltas: map[QuirkID]int64{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := Stats()
			ExtractJSONArray(tt.input)
			after := Stats()
			for q, want := range tt.wantDeltas {
				delta := after[q] - before[q]
				if delta != want {
					t.Errorf("counter %s delta = %d, want %d (input: %q)", q, delta, want, tt.input)
				}
			}
		})
	}
}

// ExtractJSON must remain a back-compat wrapper around ParseStrict.
// Pin that the string returned by ExtractJSON is identical to
// ParseStrict(input).JSON for representative inputs.
func TestExtractJSON_BackCompatWrapsParseStrict(t *testing.T) {
	inputs := []string{
		`{"a":1}`,
		"```json\n{\"a\":1}\n```",
		"{\"x\":1, // comment\n}",
		"this is not json",
		"",
	}
	for _, input := range inputs {
		want := ParseStrict(input).JSON
		got := ExtractJSON(input)
		if got != want {
			t.Errorf("ExtractJSON(%q) = %q, want %q (must equal ParseStrict.JSON)", input, got, want)
		}
	}
}
