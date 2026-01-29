package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Model.Default != "qwen2.5-coder:32b" {
		t.Errorf("expected default model qwen2.5-coder:32b, got %s", cfg.Model.Default)
	}
	if cfg.Model.Endpoint != "http://localhost:11434/v1" {
		t.Errorf("expected default endpoint http://localhost:11434/v1, got %s", cfg.Model.Endpoint)
	}
	if cfg.Model.Temperature != 0.2 {
		t.Errorf("expected default temperature 0.2, got %f", cfg.Model.Temperature)
	}
	if !cfg.NATS.Embedded {
		t.Error("expected embedded NATS by default")
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{
			name:    "valid default config",
			modify:  func(c *Config) {},
			wantErr: false,
		},
		{
			name:    "missing model default",
			modify:  func(c *Config) { c.Model.Default = "" },
			wantErr: true,
		},
		{
			name:    "missing model endpoint",
			modify:  func(c *Config) { c.Model.Endpoint = "" },
			wantErr: true,
		},
		{
			name:    "temperature too low",
			modify:  func(c *Config) { c.Model.Temperature = -0.1 },
			wantErr: true,
		},
		{
			name:    "temperature too high",
			modify:  func(c *Config) { c.Model.Temperature = 1.1 },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.modify(cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadFromFile(t *testing.T) {
	// Create temp file with config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	content := `
model:
  default: "test-model"
  endpoint: "http://test:1234/v1"
  temperature: 0.5
  timeout: 10m
repo:
  path: "/test/path"
nats:
  url: "nats://test:4222"
tools:
  allowlist:
    - file_read
    - file_write
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	if cfg.Model.Default != "test-model" {
		t.Errorf("expected model test-model, got %s", cfg.Model.Default)
	}
	if cfg.Model.Endpoint != "http://test:1234/v1" {
		t.Errorf("expected endpoint http://test:1234/v1, got %s", cfg.Model.Endpoint)
	}
	if cfg.Model.Temperature != 0.5 {
		t.Errorf("expected temperature 0.5, got %f", cfg.Model.Temperature)
	}
	if cfg.Model.Timeout != 10*time.Minute {
		t.Errorf("expected timeout 10m, got %v", cfg.Model.Timeout)
	}
	if cfg.Repo.Path != "/test/path" {
		t.Errorf("expected repo path /test/path, got %s", cfg.Repo.Path)
	}
	if cfg.NATS.URL != "nats://test:4222" {
		t.Errorf("expected NATS URL nats://test:4222, got %s", cfg.NATS.URL)
	}
	if len(cfg.Tools.Allowlist) != 2 {
		t.Errorf("expected 2 tools in allowlist, got %d", len(cfg.Tools.Allowlist))
	}
}

func TestConfigMerge(t *testing.T) {
	base := DefaultConfig()
	override := &Config{
		Model: ModelConfig{
			Default: "override-model",
		},
		Repo: RepoConfig{
			Path: "/override/path",
		},
	}

	base.Merge(override)

	if base.Model.Default != "override-model" {
		t.Errorf("expected model override-model, got %s", base.Model.Default)
	}
	// Endpoint should remain from base since override didn't set it
	if base.Model.Endpoint != "http://localhost:11434/v1" {
		t.Errorf("expected endpoint to remain default, got %s", base.Model.Endpoint)
	}
	if base.Repo.Path != "/override/path" {
		t.Errorf("expected repo path /override/path, got %s", base.Repo.Path)
	}
}

func TestConfigSaveToFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "subdir", "config.yaml")

	cfg := DefaultConfig()
	cfg.Model.Default = "saved-model"

	if err := cfg.SaveToFile(configPath); err != nil {
		t.Fatalf("SaveToFile() error = %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("config file was not created")
	}

	// Load and verify
	loaded, err := LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("failed to load saved config: %v", err)
	}
	if loaded.Model.Default != "saved-model" {
		t.Errorf("expected model saved-model, got %s", loaded.Model.Default)
	}
}
