package model

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromJSON(t *testing.T) {
	t.Run("full config with model_registry key", func(t *testing.T) {
		jsonData := []byte(`{
			"model_registry": {
				"capabilities": {
					"writing": {
						"description": "Writing capability",
						"preferred": ["model-a"],
						"fallback": ["model-b"]
					}
				},
				"endpoints": {
					"model-a": {
						"provider": "test",
						"model": "test-model"
					}
				},
				"defaults": {
					"model": "model-a"
				}
			}
		}`)

		r, err := LoadFromJSON(jsonData)
		if err != nil {
			t.Fatalf("failed to load: %v", err)
		}

		if got := r.Resolve(CapabilityWriting); got != "model-a" {
			t.Errorf("expected model-a, got %q", got)
		}
	})

	t.Run("direct registry config", func(t *testing.T) {
		jsonData := []byte(`{
			"capabilities": {
				"coding": {
					"preferred": ["codellama"],
					"fallback": ["qwen"]
				}
			},
			"endpoints": {
				"codellama": {
					"provider": "ollama",
					"model": "codellama"
				}
			}
		}`)

		r, err := LoadFromJSON(jsonData)
		if err != nil {
			t.Fatalf("failed to load: %v", err)
		}

		if got := r.Resolve(CapabilityCoding); got != "codellama" {
			t.Errorf("expected codellama, got %q", got)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		jsonData := []byte(`not valid json`)

		_, err := LoadFromJSON(jsonData)
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})
}

func TestLoadFromFile(t *testing.T) {
	// Create a temporary config file
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	configContent := []byte(`{
		"model_registry": {
			"capabilities": {
				"fast": {
					"preferred": ["quick-model"],
					"fallback": []
				}
			},
			"endpoints": {
				"quick-model": {
					"provider": "local",
					"model": "quick"
				}
			}
		}
	}`)

	if err := os.WriteFile(configPath, configContent, 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	r, err := LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("failed to load from file: %v", err)
	}

	if got := r.Resolve(CapabilityFast); got != "quick-model" {
		t.Errorf("expected quick-model, got %q", got)
	}
}

func TestLoadFromFileNotFound(t *testing.T) {
	_, err := LoadFromFile("/nonexistent/path/config.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestRegistryToConfig(t *testing.T) {
	r := NewDefaultRegistry()
	cfg := r.ToConfig()

	if cfg == nil {
		t.Fatal("expected non-nil config")
	}

	if len(cfg.Capabilities) == 0 {
		t.Error("expected capabilities in config")
	}

	if len(cfg.Endpoints) == 0 {
		t.Error("expected endpoints in config")
	}

	// Check that capability keys are strings
	if _, ok := cfg.Capabilities["writing"]; !ok {
		t.Error("expected 'writing' capability in config")
	}
}

func TestMergeFromConfig(t *testing.T) {
	r := NewDefaultRegistry()

	// Merge new config that updates writing
	cfg := &RegistryConfig{
		Capabilities: map[string]*CapabilityConfig{
			"writing": {
				Description: "Updated writing",
				Preferred:   []string{"new-writer"},
				Fallback:    []string{},
			},
		},
		Endpoints: map[string]*EndpointConfig{
			"new-writer": {
				Provider: "custom",
				Model:    "writer-v2",
			},
		},
	}

	r.MergeFromConfig(cfg)

	// Writing should now resolve to new model
	if got := r.Resolve(CapabilityWriting); got != "new-writer" {
		t.Errorf("expected new-writer after merge, got %q", got)
	}

	// Original planning should still work - verify it returns a valid model
	if got := r.Resolve(CapabilityPlanning); got == "" {
		t.Error("planning capability should resolve to a non-empty model after merge")
	}

	// New endpoint should exist
	if endpoint := r.GetEndpoint("new-writer"); endpoint == nil {
		t.Error("expected new-writer endpoint after merge")
	}

	// Old endpoints should still exist
	if endpoint := r.GetEndpoint("qwen"); endpoint == nil {
		t.Error("expected qwen endpoint to still exist after merge")
	}
}

func TestMergeFromConfigWithDefaults(t *testing.T) {
	r := NewDefaultRegistry()

	cfg := &RegistryConfig{
		Defaults: &DefaultsConfig{
			Model: "custom-default",
		},
	}

	r.MergeFromConfig(cfg)

	// Unknown capability should return new default
	if got := r.Resolve(Capability("unknown")); got != "custom-default" {
		t.Errorf("expected custom-default, got %q", got)
	}
}
