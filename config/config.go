// Package config provides configuration loading and management for Semspec.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the complete Semspec configuration
type Config struct {
	Model ModelConfig `yaml:"model"`
	Repo  RepoConfig  `yaml:"repo"`
	NATS  NATSConfig  `yaml:"nats"`
	Tools ToolsConfig `yaml:"tools"`
}

// ModelConfig configures the LLM model settings
type ModelConfig struct {
	// Default is the default model to use (e.g., "qwen2.5-coder:32b")
	Default string `yaml:"default"`
	// Endpoint is the Ollama API endpoint (default: http://localhost:11434/v1)
	Endpoint string `yaml:"endpoint"`
	// Temperature controls randomness (0.0-1.0, default: 0.2)
	Temperature float64 `yaml:"temperature"`
	// Timeout is the maximum time to wait for model responses
	Timeout time.Duration `yaml:"timeout"`
}

// RepoConfig configures the repository settings
type RepoConfig struct {
	// Path is the repository root path (auto-detected from git if empty)
	Path string `yaml:"path"`
}

// NATSConfig configures the NATS connection
type NATSConfig struct {
	// URL is the NATS server URL (empty = use embedded server)
	URL string `yaml:"url"`
	// Embedded indicates whether to use embedded NATS
	Embedded bool `yaml:"embedded"`
}

// ToolsConfig configures tool executor settings
type ToolsConfig struct {
	// Allowlist is the list of allowed tool names (empty = allow all)
	Allowlist []string `yaml:"allowlist"`
}

// DefaultConfig returns a Config with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Model: ModelConfig{
			Default:     "qwen2.5-coder:32b",
			Endpoint:    "http://localhost:11434/v1",
			Temperature: 0.2,
			Timeout:     5 * time.Minute,
		},
		Repo: RepoConfig{
			Path: "", // Auto-detect
		},
		NATS: NATSConfig{
			URL:      "",
			Embedded: true,
		},
		Tools: ToolsConfig{
			Allowlist: nil, // Allow all
		},
	}
}

// Validate checks that the configuration is valid
func (c *Config) Validate() error {
	if c.Model.Default == "" {
		return fmt.Errorf("model.default is required")
	}
	if c.Model.Endpoint == "" {
		return fmt.Errorf("model.endpoint is required")
	}
	if c.Model.Temperature < 0 || c.Model.Temperature > 1 {
		return fmt.Errorf("model.temperature must be between 0 and 1")
	}
	return nil
}

// LoadFromFile loads configuration from a YAML file
func LoadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	config := DefaultConfig()
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return config, nil
}

// SaveToFile saves configuration to a YAML file
func (c *Config) SaveToFile(path string) error {
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// Merge merges another config into this one (other takes precedence for non-zero values)
func (c *Config) Merge(other *Config) {
	if other == nil {
		return
	}

	// Model
	if other.Model.Default != "" {
		c.Model.Default = other.Model.Default
	}
	if other.Model.Endpoint != "" {
		c.Model.Endpoint = other.Model.Endpoint
	}
	if other.Model.Temperature != 0 {
		c.Model.Temperature = other.Model.Temperature
	}
	if other.Model.Timeout != 0 {
		c.Model.Timeout = other.Model.Timeout
	}

	// Repo
	if other.Repo.Path != "" {
		c.Repo.Path = other.Repo.Path
	}

	// NATS
	if other.NATS.URL != "" {
		c.NATS.URL = other.NATS.URL
		c.NATS.Embedded = false
	}

	// Tools
	if len(other.Tools.Allowlist) > 0 {
		c.Tools.Allowlist = other.Tools.Allowlist
	}
}
