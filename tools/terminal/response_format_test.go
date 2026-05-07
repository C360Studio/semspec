package terminal

import (
	"testing"

	"github.com/c360studio/semstreams/agentic"
	ssmodel "github.com/c360studio/semstreams/model"
)

func TestEndpointSupportsResponseFormat(t *testing.T) {
	tests := []struct {
		name string
		ep   *ssmodel.EndpointConfig
		want bool
	}{
		{"nil endpoint", nil, false},
		{"anthropic", &ssmodel.EndpointConfig{Provider: "anthropic", URL: "https://api.anthropic.com/v1"}, false},
		{"openai gemini compat", &ssmodel.EndpointConfig{Provider: "openai", URL: "https://generativelanguage.googleapis.com/v1beta/openai"}, false},
		{"openai proper", &ssmodel.EndpointConfig{Provider: "openai", URL: "https://api.openai.com/v1"}, true},
		{"vllm via openai provider", &ssmodel.EndpointConfig{Provider: "openai", URL: "http://seminstruct-fast:8083/v1"}, true},
		{"sparky via openai provider", &ssmodel.EndpointConfig{Provider: "openai", URL: "https://sparky.genexergy.org:8000/v1"}, true},
		{"openrouter", &ssmodel.EndpointConfig{Provider: "openrouter", URL: "https://openrouter.ai/api/v1"}, true},
		{"ollama", &ssmodel.EndpointConfig{Provider: "ollama", URL: "http://localhost:11434"}, true},
		{"unknown provider", &ssmodel.EndpointConfig{Provider: "azure", URL: "https://example.azure.com"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EndpointSupportsResponseFormat(tt.ep); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResponseFormatForDeliverable(t *testing.T) {
	t.Run("known deliverable", func(t *testing.T) {
		rf := ResponseFormatForDeliverable("plan")
		if rf == nil {
			t.Fatal("expected non-nil ResponseFormat")
		}
		if rf.Type != agentic.ResponseFormatJSONSchema {
			t.Errorf("Type = %q, want %q", rf.Type, agentic.ResponseFormatJSONSchema)
		}
		if rf.Name != "plan_args" {
			t.Errorf("Name = %q, want plan_args", rf.Name)
		}
		if rf.Strict {
			t.Error("Strict should default false until schemas pass strict-mode audit")
		}
		if len(rf.Schema) == 0 {
			t.Error("Schema should be populated")
		}
		// The schema must validate per the agentic package.
		if err := rf.Validate(); err != nil {
			t.Errorf("Validate failed: %v", err)
		}
	})

	t.Run("default deliverable falls back to developer schema", func(t *testing.T) {
		rf := ResponseFormatForDeliverable("")
		if rf == nil {
			t.Fatal("expected non-nil ResponseFormat for default deliverable")
		}
		if rf.Name != "_args" {
			t.Errorf("Name = %q, want _args", rf.Name)
		}
	})
}

func TestResponseFormatForEndpoint(t *testing.T) {
	t.Run("nil endpoint returns nil", func(t *testing.T) {
		if rf := ResponseFormatForEndpoint(nil, "plan"); rf != nil {
			t.Errorf("expected nil, got %+v", rf)
		}
	})
	t.Run("anthropic endpoint returns nil", func(t *testing.T) {
		ep := &ssmodel.EndpointConfig{Provider: "anthropic"}
		if rf := ResponseFormatForEndpoint(ep, "plan"); rf != nil {
			t.Errorf("expected nil for anthropic, got %+v", rf)
		}
	})
	t.Run("supported endpoint returns schema", func(t *testing.T) {
		ep := &ssmodel.EndpointConfig{Provider: "openai", URL: "http://seminstruct-fast:8083/v1"}
		rf := ResponseFormatForEndpoint(ep, "review")
		if rf == nil {
			t.Fatal("expected non-nil ResponseFormat")
		}
		if rf.Name != "review_args" {
			t.Errorf("Name = %q, want review_args", rf.Name)
		}
	})
}
