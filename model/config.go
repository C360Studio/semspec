package model

import (
	"encoding/json"
	"fmt"
	"os"
)

// RegistryConfig represents the JSON configuration structure for the model registry.
// This is the format used in configs/semspec.json under "model_registry".
type RegistryConfig struct {
	Capabilities map[string]*CapabilityConfig `json:"capabilities"`
	Endpoints    map[string]*EndpointConfig   `json:"endpoints"`
	Defaults     *DefaultsConfig              `json:"defaults,omitempty"`
}

// LoadFromFile loads a registry configuration from a JSON file.
// The file should contain a "model_registry" key with the configuration.
func LoadFromFile(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	return LoadFromJSON(data)
}

// LoadFromJSON loads a registry from JSON data.
// Accepts either a full config with "model_registry" key or just the registry config.
func LoadFromJSON(data []byte) (*Registry, error) {
	// First try to parse as a full config with model_registry key
	var fullConfig struct {
		ModelRegistry *RegistryConfig `json:"model_registry"`
	}
	if err := json.Unmarshal(data, &fullConfig); err == nil && fullConfig.ModelRegistry != nil {
		return registryFromConfig(fullConfig.ModelRegistry), nil
	}

	// Try parsing as just the registry config
	var regConfig RegistryConfig
	if err := json.Unmarshal(data, &regConfig); err != nil {
		return nil, fmt.Errorf("parse registry config: %w", err)
	}

	return registryFromConfig(&regConfig), nil
}

// registryFromConfig converts a RegistryConfig to a Registry.
func registryFromConfig(cfg *RegistryConfig) *Registry {
	// Convert string keys to Capability type
	caps := make(map[Capability]*CapabilityConfig, len(cfg.Capabilities))
	for k, v := range cfg.Capabilities {
		cap := ParseCapability(k)
		if cap == "" {
			// Use the string directly as capability for unknown types
			cap = Capability(k)
		}
		caps[cap] = v
	}

	defaults := cfg.Defaults
	if defaults == nil {
		defaults = &DefaultsConfig{Model: "default"}
	}

	return &Registry{
		capabilities: caps,
		endpoints:    cfg.Endpoints,
		defaults:     defaults,
	}
}

// ToConfig converts a Registry to a RegistryConfig for serialization.
func (r *Registry) ToConfig() *RegistryConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()

	caps := make(map[string]*CapabilityConfig, len(r.capabilities))
	for k, v := range r.capabilities {
		caps[string(k)] = v
	}

	return &RegistryConfig{
		Capabilities: caps,
		Endpoints:    r.endpoints,
		Defaults:     r.defaults,
	}
}

// MergeFromConfig merges configuration into an existing registry.
// Existing entries are overwritten by the new config.
func (r *Registry) MergeFromConfig(cfg *RegistryConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for k, v := range cfg.Capabilities {
		cap := ParseCapability(k)
		if cap == "" {
			cap = Capability(k)
		}
		r.capabilities[cap] = v
	}

	for k, v := range cfg.Endpoints {
		r.endpoints[k] = v
	}

	if cfg.Defaults != nil {
		r.defaults = cfg.Defaults
	}
}
