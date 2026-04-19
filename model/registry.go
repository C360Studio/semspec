package model

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// Registry manages model selection based on capabilities.
// It maps capabilities to preferred models with fallback chains.
type Registry struct {
	mu           sync.RWMutex
	capabilities map[Capability]*CapabilityConfig
	endpoints    map[string]*EndpointConfig
	defaults     *DefaultsConfig
	health       *healthState
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

	// SupportsTools indicates whether this endpoint supports tool/function calling.
	// When false, tool definitions are stripped from requests.
	SupportsTools bool `json:"supports_tools,omitempty"`

	// ToolFormat specifies the tool calling format: "anthropic" or "openai".
	// Empty means auto-detect from provider.
	ToolFormat string `json:"tool_format,omitempty"`

	// APIKeyEnv is the environment variable name containing the API key.
	// When empty, providers fall back to their default env var (e.g., OPENAI_API_KEY).
	APIKeyEnv string `json:"api_key_env,omitempty"`

	// ReasoningEffort controls thinking depth for models that support it (e.g., Gemini).
	// Values: "low", "medium", "high". Empty means provider default.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`

	// RequestsPerMinute limits the rate of requests to this endpoint.
	// 0 means no rate limiting. Honored by semstreams agentic-model
	// (via its own EndpointThrottle) and the local llm.ConcurrencyGovernor.
	RequestsPerMinute int `json:"requests_per_minute,omitempty"`

	// MaxConcurrent limits concurrent in-flight requests to this endpoint.
	// 0 means no concurrency limit. For local LLMs (Ollama), set to 1
	// to enforce serial inference.
	MaxConcurrent int `json:"max_concurrent,omitempty"`

	// RequestTimeout is the per-request HTTP timeout for this endpoint.
	// Go duration string (e.g., "300s", "5m"). Empty or "0" means no
	// per-endpoint timeout — the caller's context deadline governs duration.
	// Use this as a safety cap for cloud endpoints; leave unset for slow
	// local LLMs that may take hours per response.
	RequestTimeout string `json:"request_timeout,omitempty"`
}

// GetRequestTimeout parses RequestTimeout as a Go duration.
// Returns 0 if the field is empty, "0", or unparseable.
func (e *EndpointConfig) GetRequestTimeout() time.Duration {
	if e.RequestTimeout == "" || e.RequestTimeout == "0" {
		return 0
	}
	d, err := time.ParseDuration(e.RequestTimeout)
	if err != nil {
		slog.Warn("Invalid request_timeout in endpoint config, ignoring",
			"model", e.Model, "value", e.RequestTimeout, "error", err)
		return 0
	}
	if d <= 0 {
		return 0
	}
	return d
}

// DefaultsConfig holds default model settings.
type DefaultsConfig struct {
	// Model is the default model when no capability matches.
	Model string `json:"model"`

	// MaxConcurrentGlobal caps total concurrent LLM requests across all endpoints.
	// 0 means no global limit. Applied by llm.ConcurrencyGovernor.
	MaxConcurrentGlobal int `json:"max_concurrent_global,omitempty"`
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
// Defaults to local Ollama models for offline-first operation.
// Uses LLM_API_URL environment variable if set (e.g., for Docker).
func NewDefaultRegistry() *Registry {
	// Default to localhost, but allow override via environment
	ollamaURL := os.Getenv("LLM_API_URL")
	if ollamaURL == "" {
		ollamaURL = "http://localhost:11434"
	}
	// Ensure URL has /v1 suffix for OpenAI-compatible API
	if !strings.HasSuffix(ollamaURL, "/v1") {
		ollamaURL = strings.TrimSuffix(ollamaURL, "/") + "/v1"
	}

	return &Registry{
		capabilities: map[Capability]*CapabilityConfig{
			CapabilityPlanning: {
				Description: "High-level reasoning, architecture decisions",
				Preferred:   []string{"qwen"},
				Fallback:    []string{"qwen3", "llama3-2"},
			},
			CapabilityWriting: {
				Description: "Documentation, plans, specifications",
				Preferred:   []string{"qwen"},
				Fallback:    []string{"qwen3-fast", "llama3-2"},
			},
			CapabilityCoding: {
				Description: "Code generation, implementation",
				Preferred:   []string{"qwen"},
				Fallback:    []string{"codellama", "llama3-2"},
			},
			CapabilityReviewing: {
				Description: "Code review, quality analysis",
				Preferred:   []string{"qwen"},
				Fallback:    []string{"qwen3-fast", "llama3-2"},
			},
			CapabilityFast: {
				Description: "Quick responses, simple tasks",
				Preferred:   []string{"qwen3-fast"},
				Fallback:    []string{"qwen"},
			},
		},
		endpoints: map[string]*EndpointConfig{
			"qwen": {
				Provider:      "ollama",
				URL:           ollamaURL,
				Model:         "qwen3-coder:30b",
				SupportsTools: true,
				ToolFormat:    "openai",
				MaxTokens:     131072,
			},
			"qwen3": {
				Provider:      "ollama",
				URL:           ollamaURL,
				Model:         "qwen3:14b",
				SupportsTools: true,
				MaxTokens:     32768,
			},
			"qwen3-fast": {
				Provider:  "ollama",
				URL:       ollamaURL,
				Model:     "qwen3:1.7b",
				MaxTokens: 32768,
			},
			"llama3-2": {
				Provider:  "ollama",
				URL:       ollamaURL,
				Model:     "llama3.2",
				MaxTokens: 131072,
			},
			"codellama": {
				Provider:  "ollama",
				URL:       ollamaURL,
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
	capVal := CapabilityForRole(role)
	return r.Resolve(capVal)
}

// GetFallbackChainForRole returns the full fallback chain for a role.
func (r *Registry) GetFallbackChainForRole(role string) []string {
	capVal := CapabilityForRole(role)
	return r.GetFallbackChain(capVal)
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

// GetDefaults returns the defaults configuration. Returns nil if not set.
func (r *Registry) GetDefaults() *DefaultsConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.defaults
}

// Validate checks the registry configuration for consistency.
// It verifies that:
// - Endpoint names are well-formed (non-empty, no dots)
// - All models referenced in capabilities exist in endpoints
// - The default model exists in endpoints
//
// Endpoint names become the 6th segment of model-endpoint entity IDs
// ({org}.{platform}.agent.model-registry.endpoint.{name}), so dots or
// empty strings would produce invalid entity IDs and panic later inside
// agentic-loop's graph writer. Catch it here at config load instead.
func (r *Registry) Validate() error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var errs []string

	// Check endpoint names don't contain dots (entity ID separator)
	for name := range r.endpoints {
		if name == "" {
			errs = append(errs, "endpoint name must not be empty")
			continue
		}
		if strings.Contains(name, ".") {
			errs = append(errs, fmt.Sprintf("endpoint name %q must not contain dots (dots are entity ID separators — rename to e.g. %q)", name, strings.ReplaceAll(name, ".", "-")))
		}
	}

	// Check that all capability model references exist
	for capVal, cfg := range r.capabilities {
		for _, modelName := range cfg.Preferred {
			if _, ok := r.endpoints[modelName]; !ok {
				errs = append(errs, fmt.Sprintf("capability %q preferred model %q not found in endpoints", capVal, modelName))
			}
		}
		for _, modelName := range cfg.Fallback {
			if _, ok := r.endpoints[modelName]; !ok {
				errs = append(errs, fmt.Sprintf("capability %q fallback model %q not found in endpoints", capVal, modelName))
			}
		}
	}

	// Check that default model exists
	if r.defaults != nil && r.defaults.Model != "" {
		if _, ok := r.endpoints[r.defaults.Model]; !ok {
			errs = append(errs, fmt.Sprintf("default model %q not found in endpoints", r.defaults.Model))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("registry validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return nil
}

// ListCapabilities returns all configured capabilities.
func (r *Registry) ListCapabilities() []Capability {
	r.mu.RLock()
	defer r.mu.RUnlock()

	caps := make([]Capability, 0, len(r.capabilities))
	for capVal := range r.capabilities {
		caps = append(caps, capVal)
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

// GetToolCapableEndpoints returns the fallback chain for a capability,
// filtered to only endpoints that support tool calling.
// Returns empty slice if no tool-capable endpoints are available.
func (r *Registry) GetToolCapableEndpoints(cap Capability) []string {
	chain := r.GetFallbackChain(cap)

	r.mu.RLock()
	defer r.mu.RUnlock()

	var capable []string
	for _, name := range chain {
		if ep := r.endpoints[name]; ep != nil && ep.SupportsTools {
			capable = append(capable, name)
		}
	}
	return capable
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
