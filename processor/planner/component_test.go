package planner

import (
	"encoding/json"
	"testing"
)

func TestParsePlanFromResult(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantGoal    string
		wantContext string
		wantInclude []string
		wantErr     bool
	}{
		{
			name: "valid plan",
			input: `{
				"goal": "Add user authentication",
				"context": "The API needs secure access",
				"scope": {
					"include": ["api/auth/", "api/middleware/"],
					"exclude": ["api/public/"]
				}
			}`,
			wantGoal:    "Add user authentication",
			wantContext: "The API needs secure access",
			wantInclude: []string{"api/auth/", "api/middleware/"},
		},
		{
			name: "plan with status field",
			input: `{
				"status": "committed",
				"goal": "Implement caching",
				"context": "Performance optimization needed",
				"scope": {
					"include": ["cache/"]
				}
			}`,
			wantGoal:    "Implement caching",
			wantContext: "Performance optimization needed",
			wantInclude: []string{"cache/"},
		},
		{
			name: "minimal plan",
			input: `{
				"goal": "Simple task",
				"context": "",
				"scope": {}
			}`,
			wantGoal:    "Simple task",
			wantContext: "",
			wantInclude: nil,
		},
		{
			name: "json in code block",
			input: "Here's the plan:\n```json\n" + `{"goal": "Fenced", "context": "ctx", "scope": {}}` + "\n```\nDone.",
			wantGoal:    "Fenced",
			wantContext: "ctx",
		},
		{
			name:    "missing goal",
			input:   `{"context": "No goal here", "scope": {}}`,
			wantErr: true,
		},
		{
			name:    "invalid json",
			input:   `{not valid json}`,
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePlanFromResult(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parsePlanFromResult() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if got.Goal != tt.wantGoal {
				t.Errorf("Goal = %q, want %q", got.Goal, tt.wantGoal)
			}
			if got.Context != tt.wantContext {
				t.Errorf("Context = %q, want %q", got.Context, tt.wantContext)
			}
			if len(got.Scope.Include) != len(tt.wantInclude) {
				t.Errorf("Scope.Include = %v, want %v", got.Scope.Include, tt.wantInclude)
			} else {
				for i, v := range got.Scope.Include {
					if v != tt.wantInclude[i] {
						t.Errorf("Scope.Include[%d] = %q, want %q", i, v, tt.wantInclude[i])
					}
				}
			}
		})
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantEmpty bool
	}{
		{
			name:  "raw json",
			input: `{"goal": "test"}`,
		},
		{
			name:  "json in text",
			input: `Here is the plan: {"goal": "embedded"} and more text`,
		},
		{
			name:  "nested braces",
			input: `{"goal": "test", "scope": {"include": ["a"]}}`,
		},
		{
			name:      "no json",
			input:     "This is just text without any JSON",
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSON(tt.input)
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("expected empty, got %q", got)
				}
				return
			}
			if got == "" {
				t.Fatal("expected JSON, got empty")
			}
			var parsed map[string]any
			if err := json.Unmarshal([]byte(got), &parsed); err != nil {
				t.Errorf("result is not valid JSON: %v", err)
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "valid default config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "missing stream name",
			config: Config{
				StreamName:     "",
				ConsumerName:   "test",
				TriggerSubject: "test",
			},
			wantErr: true,
		},
		{
			name: "missing consumer name",
			config: Config{
				StreamName:     "test",
				ConsumerName:   "",
				TriggerSubject: "test",
			},
			wantErr: true,
		},
		{
			name: "missing trigger subject",
			config: Config{
				StreamName:     "test",
				ConsumerName:   "test",
				TriggerSubject: "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.StreamName != "WORKFLOW" {
		t.Errorf("StreamName = %q, want %q", config.StreamName, "WORKFLOW")
	}
	if config.ConsumerName != "planner" {
		t.Errorf("ConsumerName = %q, want %q", config.ConsumerName, "planner")
	}
	if config.TriggerSubject != "workflow.async.planner" {
		t.Errorf("TriggerSubject = %q, want %q", config.TriggerSubject, "workflow.async.planner")
	}
	if config.DefaultCapability != "planning" {
		t.Errorf("DefaultCapability = %q, want %q", config.DefaultCapability, "planning")
	}
	if config.Ports == nil {
		t.Error("Ports should not be nil")
	}
	if len(config.Ports.Inputs) != 1 {
		t.Errorf("Ports.Inputs length = %d, want 1", len(config.Ports.Inputs))
	}
	if len(config.Ports.Outputs) != 1 {
		t.Errorf("Ports.Outputs length = %d, want 1", len(config.Ports.Outputs))
	}
}
