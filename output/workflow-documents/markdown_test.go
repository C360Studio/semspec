package workflowdocuments

import (
	"strings"
	"testing"
)

func TestTransformer_Transform(t *testing.T) {
	transformer := NewTransformer()

	tests := []struct {
		name     string
		content  DocumentContent
		expected []string // Substrings that must be present
	}{
		{
			name: "basic proposal",
			content: DocumentContent{
				Title: "Add User Authentication",
				Sections: map[string]any{
					"why":          "Users need secure access to protected resources.",
					"what_changes": "Add authentication middleware and user table.",
				},
				Status: "proposed",
			},
			expected: []string{
				"# Add User Authentication",
				"## Why",
				"Users need secure access",
				"## What Changes",
				"Add authentication middleware",
				"**Status:** proposed",
			},
		},
		{
			name: "nested sections",
			content: DocumentContent{
				Title: "API Design",
				Sections: map[string]any{
					"impact": map[string]any{
						"code_affected": []any{"api/middleware", "db/migrations"},
						"specs_affected": []any{"api-spec.md"},
					},
				},
			},
			expected: []string{
				"# API Design",
				"## Impact",
				"### Code Affected",
				"api/middleware",
				"db/migrations",
				"### Specs Affected",
			},
		},
		{
			name: "list items",
			content: DocumentContent{
				Title: "Task Breakdown",
				Sections: map[string]any{
					"tasks": []any{
						"Create user table",
						"Add auth middleware",
						"Write unit tests",
					},
				},
			},
			expected: []string{
				"# Task Breakdown",
				"## Tasks",
				"- Create user table",
				"- Add auth middleware",
				"- Write unit tests",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := transformer.Transform(tt.content)

			for _, exp := range tt.expected {
				if !strings.Contains(result, exp) {
					t.Errorf("expected %q to be in output:\n%s", exp, result)
				}
			}
		})
	}
}

func TestTransformer_ToTitleCase(t *testing.T) {
	transformer := NewTransformer()

	tests := []struct {
		input    string
		expected string
	}{
		{"why", "Why"},
		{"what_changes", "What Changes"},
		{"code_affected", "Code Affected"},
		{"testing_required", "Testing Required"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := transformer.toTitleCase(tt.input)
			if result != tt.expected {
				t.Errorf("toTitleCase(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestTransformer_TransformSpec(t *testing.T) {
	transformer := NewTransformer()

	content := DocumentContent{
		Title: "Authentication Spec",
		Sections: map[string]any{
			"scenarios": []any{
				map[string]any{
					"name":  "Successful login",
					"given": "a registered user with valid credentials",
					"when":  "they submit the login form",
					"then":  "they receive a session token",
				},
			},
		},
	}

	result := transformer.TransformSpec(content)

	expected := []string{
		"# Authentication Spec",
		"## Scenarios",
		"**Successful login**",
		"**GIVEN** a registered user",
		"**WHEN** they submit",
		"**THEN** they receive",
	}

	for _, exp := range expected {
		if !strings.Contains(result, exp) {
			t.Errorf("expected %q to be in output:\n%s", exp, result)
		}
	}
}

func TestTransformer_OrderSections(t *testing.T) {
	transformer := NewTransformer()

	sections := map[string]any{
		"impact":       "some impact",
		"why":          "some reason",
		"what_changes": "some changes",
		"custom":       "custom section",
	}

	ordered := transformer.orderSections(sections)

	// Check that preferred sections come first in correct order
	if len(ordered) != 4 {
		t.Fatalf("expected 4 sections, got %d", len(ordered))
	}

	// why should be first, what_changes second, impact third
	if ordered[0].name != "why" {
		t.Errorf("expected first section to be 'why', got %q", ordered[0].name)
	}
	if ordered[1].name != "what_changes" {
		t.Errorf("expected second section to be 'what_changes', got %q", ordered[1].name)
	}
	if ordered[2].name != "impact" {
		t.Errorf("expected third section to be 'impact', got %q", ordered[2].name)
	}
	// custom should be last (not in preferred order)
	if ordered[3].name != "custom" {
		t.Errorf("expected fourth section to be 'custom', got %q", ordered[3].name)
	}
}
