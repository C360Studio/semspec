package model

import (
	"encoding/json"
	"sync"
)

// Registry manages model selection based on capabilities.
// It maps capabilities to preferred models with fallback chains.
type Registry struct {
	mu           sync.RWMutex
	capabilities map[Capability]*CapabilityConfig
	endpoints    map[string]*EndpointConfig
	defaults     *DefaultsConfig
}

// CapabilityConfig defines model preferences for a capability.
type CapabilityConfig struct {
	// Description explains what this capability is for.
	Description string `json:"description"`

	// Preferred lists models in order of preference.
	// The first available model is used.
	Preferred []string `json:"preferred"`

	// Fallback lists backup models if all preferred fail.
	Fallback []string `json:"fallback"`
}

// EndpointConfig defines an available model endpoint.
type EndpointConfig struct {
	// Provider is the model provider (anthropic, ollama, openai).
	Provider string `json:"provider"`

	// URL is the API endpoint URL (for non-Anthropic providers).
	URL string `json:"url,omitempty"`

	// Model is the actual model identifier to send to the provider.
	Model string `json:"model"`

	// MaxTokens is the context window size.
	MaxTokens int `json:"max_tokens,omitempty"`
}

// DefaultsConfig holds default model settings.
type DefaultsConfig struct {
	// Model is the default model when no capability matches.
	Model string `json:"model"`
}

// NewRegistry creates a new model registry with the given configuration.
func NewRegistry(caps map[Capability]*CapabilityConfig, endpoints map[string]*EndpointConfig) *Registry {
	return &Registry{
		capabilities: caps,
		endpoints:    endpoints,
		defaults: &DefaultsConfig{
			Model: "default",
		},
	}
}

// NewDefaultRegistry creates a registry with sensible defaults.
// Used when no configuration is provided.
func NewDefaultRegistry() *Registry {
	return &Registry{
		capabilities: map[Capability]*CapabilityConfig{
			CapabilityPlanning: {
				Description: "High-level reasoning, architecture decisions",
				Preferred:   []string{"claude-opus", "claude-sonnet"},
				Fallback:    []string{"qwen", "llama3.2"},
			},
			CapabilityWriting: {
				Description: "Documentation, proposals, specifications",
				Preferred:   []string{"claude-sonnet"},
				Fallback:    []string{"claude-haiku", "qwen"},
			},
			CapabilityCoding: {
				Description: "Code generation, implementation",
				Preferred:   []string{"claude-sonnet"},
				Fallback:    []string{"codellama", "qwen"},
			},
			CapabilityReviewing: {
				Description: "Code review, quality analysis",
				Preferred:   []string{"claude-sonnet"},
				Fallback:    []string{"claude-haiku", "qwen"},
			},
			CapabilityFast: {
				Description: "Quick responses, simple tasks",
				Preferred:   []string{"claude-haiku"},
				Fallback:    []string{"qwen"},
			},
		},
		endpoints: map[string]*EndpointConfig{
			"claude-opus": {
				Provider:  "anthropic",
				Model:     "claude-opus-4-5-20251101",
				MaxTokens: 200000,
			},
			"claude-sonnet": {
				Provider:  "anthropic",
				Model:     "claude-sonnet-4-20250514",
				MaxTokens: 200000,
			},
			"claude-haiku": {
				Provider:  "anthropic",
				Model:     "claude-haiku-3-5-20241022",
				MaxTokens: 200000,
			},
			"qwen": {
				Provider:  "ollama",
				URL:       "http://localhost:11434/v1",
				Model:     "qwen2.5-coder:14b",
				MaxTokens: 128000,
			},
			"llama3.2": {
				Provider:  "ollama",
				URL:       "http://localhost:11434/v1",
				Model:     "llama3.2",
				MaxTokens: 128000,
			},
			"codellama": {
				Provider:  "ollama",
				URL:       "http://localhost:11434/v1",
				Model:     "codellama",
				MaxTokens: 16384,
			},
		},
		defaults: &DefaultsConfig{
			Model: "qwen",
		},
	}
}

// Resolve returns the preferred model for a capability.
// Returns the first model in the preferred list.
// Fallback handling is done by agentic-model on failure (lazy approach).
func (r *Registry) Resolve(cap Capability) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if cfg, ok := r.capabilities[cap]; ok && len(cfg.Preferred) > 0 {
		return cfg.Preferred[0]
	}
	return r.defaults.Model
}

// GetFallbackChain returns all models for a capability in order of preference.
// Used by agentic-model when primary fails to try alternatives.
func (r *Registry) GetFallbackChain(cap Capability) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if cfg, ok := r.capabilities[cap]; ok {
		chain := make([]string, 0, len(cfg.Preferred)+len(cfg.Fallback))
		chain = append(chain, cfg.Preferred...)
		chain = append(chain, cfg.Fallback...)
		return chain
	}
	return []string{r.defaults.Model}
}

// ForRole returns the resolved model for a role's default capability.
// Use this when no explicit capability or model is specified.
func (r *Registry) ForRole(role string) string {
	cap := CapabilityForRole(role)
	return r.Resolve(cap)
}

// GetFallbackChainForRole returns the full fallback chain for a role.
func (r *Registry) GetFallbackChainForRole(role string) []string {
	cap := CapabilityForRole(role)
	return r.GetFallbackChain(cap)
}

// GetEndpoint returns the endpoint configuration for a model name.
// Returns nil if the model is not configured.
func (r *Registry) GetEndpoint(modelName string) *EndpointConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.endpoints[modelName]
}

// SetCapability updates or adds a capability configuration.
func (r *Registry) SetCapability(cap Capability, cfg *CapabilityConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.capabilities == nil {
		r.capabilities = make(map[Capability]*CapabilityConfig)
	}
	r.capabilities[cap] = cfg
}

// SetEndpoint updates or adds an endpoint configuration.
func (r *Registry) SetEndpoint(name string, cfg *EndpointConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.endpoints == nil {
		r.endpoints = make(map[string]*EndpointConfig)
	}
	r.endpoints[name] = cfg
}

// SetDefault sets the default model.
func (r *Registry) SetDefault(model string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.defaults == nil {
		r.defaults = &DefaultsConfig{}
	}
	r.defaults.Model = model
}

// ListCapabilities returns all configured capabilities.
func (r *Registry) ListCapabilities() []Capability {
	r.mu.RLock()
	defer r.mu.RUnlock()

	caps := make([]Capability, 0, len(r.capabilities))
	for cap := range r.capabilities {
		caps = append(caps, cap)
	}
	return caps
}

// ListEndpoints returns all configured endpoint names.
func (r *Registry) ListEndpoints() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.endpoints))
	for name := range r.endpoints {
		names = append(names, name)
	}
	return names
}

// MarshalJSON implements json.Marshaler for the registry.
func (r *Registry) MarshalJSON() ([]byte, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return json.Marshal(struct {
		Capabilities map[Capability]*CapabilityConfig `json:"capabilities"`
		Endpoints    map[string]*EndpointConfig       `json:"endpoints"`
		Defaults     *DefaultsConfig                  `json:"defaults,omitempty"`
	}{
		Capabilities: r.capabilities,
		Endpoints:    r.endpoints,
		Defaults:     r.defaults,
	})
}

// UnmarshalJSON implements json.Unmarshaler for the registry.
func (r *Registry) UnmarshalJSON(data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var tmp struct {
		Capabilities map[Capability]*CapabilityConfig `json:"capabilities"`
		Endpoints    map[string]*EndpointConfig       `json:"endpoints"`
		Defaults     *DefaultsConfig                  `json:"defaults,omitempty"`
	}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	r.capabilities = tmp.Capabilities
	r.endpoints = tmp.Endpoints
	r.defaults = tmp.Defaults
	return nil
}
