package llm

import (
	"net/http"
	"sync"
)

// Provider defines the interface for LLM provider implementations.
type Provider interface {
	// Name returns the provider identifier (e.g., "anthropic", "ollama").
	Name() string

	// BuildURL constructs the full API endpoint URL.
	BuildURL(baseURL string) string

	// SetHeaders adds provider-specific headers to the request.
	// The endpoint config provides the APIKeyEnv field for dynamic key resolution.
	SetHeaders(req *http.Request, apiKeyEnv string)

	// BuildRequestBody creates the JSON request body for the provider.
	// temperature is nil to use provider default, or a pointer to explicit value.
	// tools and toolChoice are optional - pass nil/empty if not using tools.
	// opts holds optional endpoint-level parameters (reasoning_effort, etc.).
	BuildRequestBody(model string, messages []Message, temperature *float64, maxTokens int,
		tools []ToolDefinition, toolChoice string, opts *RequestOpts) ([]byte, error)

	// ParseResponse extracts the response from provider-specific JSON.
	ParseResponse(body []byte, model string) (*Response, error)
}

// RequestOpts holds optional endpoint-level parameters passed to BuildRequestBody.
type RequestOpts struct {
	// ReasoningEffort controls thinking depth ("low", "medium", "high").
	ReasoningEffort string
}

// providerRegistry holds registered providers.
var (
	providerRegistry = make(map[string]Provider)
	providerMu       sync.RWMutex
)

// RegisterProvider adds a provider to the registry.
func RegisterProvider(p Provider) {
	providerMu.Lock()
	defer providerMu.Unlock()
	providerRegistry[p.Name()] = p
}

// GetProvider retrieves a provider by name.
func GetProvider(name string) Provider {
	providerMu.RLock()
	defer providerMu.RUnlock()
	return providerRegistry[name]
}

// ListProviders returns all registered provider names.
func ListProviders() []string {
	providerMu.RLock()
	defer providerMu.RUnlock()

	names := make([]string, 0, len(providerRegistry))
	for name := range providerRegistry {
		names = append(names, name)
	}
	return names
}
