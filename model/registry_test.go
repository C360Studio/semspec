package model

import (
	"encoding/json"
	"testing"
)

func TestNewDefaultRegistry(t *testing.T) {
	r := NewDefaultRegistry()

	// Check that capabilities are configured
	caps := r.ListCapabilities()
	if len(caps) != 5 {
		t.Errorf("expected 5 capabilities, got %d", len(caps))
	}

	// Check that endpoints are configured
	endpoints := r.ListEndpoints()
	if len(endpoints) < 3 {
		t.Errorf("expected at least 3 endpoints, got %d", len(endpoints))
	}
}

func TestRegistryResolve(t *testing.T) {
	r := NewDefaultRegistry()

	tests := []struct {
		capability Capability
		expected   string
	}{
		{CapabilityPlanning, "qwen"},
		{CapabilityWriting, "qwen"},
		{CapabilityCoding, "qwen"},
		{CapabilityReviewing, "qwen"},
		{CapabilityFast, "qwen3-fast"},
		{Capability("unknown"), "qwen"}, // Falls back to default
	}

	for _, tt := range tests {
		t.Run(string(tt.capability), func(t *testing.T) {
			got := r.Resolve(tt.capability)
			if got != tt.expected {
				t.Errorf("Resolve(%q) = %q, want %q", tt.capability, got, tt.expected)
			}
		})
	}
}

func TestRegistryGetFallbackChain(t *testing.T) {
	r := NewDefaultRegistry()

	chain := r.GetFallbackChain(CapabilityPlanning)

	// Should have both preferred and fallback models
	if len(chain) < 2 {
		t.Errorf("expected at least 2 models in chain, got %d", len(chain))
	}

	// First should be preferred model
	if chain[0] != "qwen" {
		t.Errorf("first in chain should be qwen, got %q", chain[0])
	}

	// Should include fallbacks
	hasQwen3 := false
	for _, m := range chain {
		if m == "qwen3" {
			hasQwen3 = true
			break
		}
	}
	if !hasQwen3 {
		t.Error("expected qwen3 in fallback chain")
	}
}

func TestRegistryForRole(t *testing.T) {
	r := NewDefaultRegistry()

	tests := []struct {
		role     string
		expected string
	}{
		{"general", "qwen3-fast"},  // fast capability
		{"planner", "qwen"},        // planning capability
		{"developer", "qwen"},      // coding capability
		{"reviewer", "qwen"},       // reviewing capability
		{"writer", "qwen"},         // writing capability
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			got := r.ForRole(tt.role)
			if got != tt.expected {
				t.Errorf("ForRole(%q) = %q, want %q", tt.role, got, tt.expected)
			}
		})
	}
}

func TestRegistryGetFallbackChainForRole(t *testing.T) {
	r := NewDefaultRegistry()

	chain := r.GetFallbackChainForRole("planner")

	// planner uses planning capability
	if len(chain) < 2 {
		t.Errorf("expected at least 2 models in chain, got %d", len(chain))
	}

	if chain[0] != "qwen" {
		t.Errorf("first in chain should be qwen, got %q", chain[0])
	}
}

func TestRegistryGetEndpoint(t *testing.T) {
	r := NewDefaultRegistry()

	endpoint := r.GetEndpoint("qwen")
	if endpoint == nil {
		t.Fatal("expected qwen endpoint to exist")
	}

	if endpoint.Provider != "ollama" {
		t.Errorf("expected provider ollama, got %q", endpoint.Provider)
	}

	if endpoint.Model == "" {
		t.Error("expected model to be set")
	}

	// Test non-existent endpoint
	missing := r.GetEndpoint("nonexistent")
	if missing != nil {
		t.Error("expected nil for nonexistent endpoint")
	}
}

func TestRegistrySetCapability(t *testing.T) {
	r := NewDefaultRegistry()

	// Add a new capability
	r.SetCapability(Capability("custom"), &CapabilityConfig{
		Description: "Custom capability",
		Preferred:   []string{"model-a"},
		Fallback:    []string{"model-b"},
	})

	got := r.Resolve(Capability("custom"))
	if got != "model-a" {
		t.Errorf("expected model-a for custom capability, got %q", got)
	}
}

func TestRegistrySetEndpoint(t *testing.T) {
	r := NewDefaultRegistry()

	// Add a new endpoint
	r.SetEndpoint("custom-model", &EndpointConfig{
		Provider:  "custom",
		URL:       "http://custom.example.com",
		Model:     "custom-v1",
		MaxTokens: 4096,
	})

	endpoint := r.GetEndpoint("custom-model")
	if endpoint == nil {
		t.Fatal("expected custom-model endpoint to exist")
	}

	if endpoint.URL != "http://custom.example.com" {
		t.Errorf("unexpected URL: %q", endpoint.URL)
	}
}

func TestRegistrySetDefault(t *testing.T) {
	r := NewDefaultRegistry()

	r.SetDefault("my-default")

	// Unknown capability should return default
	got := r.Resolve(Capability("unknown"))
	if got != "my-default" {
		t.Errorf("expected my-default for unknown capability, got %q", got)
	}
}

func TestRegistryJSONRoundtrip(t *testing.T) {
	original := NewDefaultRegistry()

	// Marshal to JSON
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Unmarshal back
	restored := &Registry{}
	if err := json.Unmarshal(data, restored); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Verify capabilities match
	origCaps := original.ListCapabilities()
	restCaps := restored.ListCapabilities()
	if len(origCaps) != len(restCaps) {
		t.Errorf("capability count mismatch: %d vs %d", len(origCaps), len(restCaps))
	}

	// Verify resolution still works
	if got := restored.Resolve(CapabilityWriting); got != "qwen" {
		t.Errorf("expected qwen for writing, got %q", got)
	}
}

func TestNewRegistry(t *testing.T) {
	caps := map[Capability]*CapabilityConfig{
		CapabilityWriting: {
			Preferred: []string{"model-a"},
			Fallback:  []string{"model-b"},
		},
	}
	endpoints := map[string]*EndpointConfig{
		"model-a": {Provider: "test", Model: "test-model"},
	}

	r := NewRegistry(caps, endpoints)

	if got := r.Resolve(CapabilityWriting); got != "model-a" {
		t.Errorf("expected model-a, got %q", got)
	}

	if endpoint := r.GetEndpoint("model-a"); endpoint == nil {
		t.Error("expected model-a endpoint to exist")
	}
}
