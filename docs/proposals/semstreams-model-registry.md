# Proposal: Unified Model Registry for Agentic Components

**Author:** Semspec Team
**Date:** 2026-02-27
**Status:** Proposed
**Target:** Semstreams agentic-* components

## Summary

Add a unified `model_registry` configuration section to semstreams that provides centralized model endpoint definitions, capability-based routing, and tool capability metadata. This eliminates configuration duplication and enables intelligent model selection across all agentic components.

## Problem Statement

### Current Architecture Issues

1. **Configuration Duplication**: Model endpoints must be defined in multiple places:
   - `agentic-model.config.endpoints` - URL and model name
   - `agentic-loop.config.context.model_limits` - Context window sizes
   - `agentic-dispatch.config.default_model` - Default model name
   - Application-level registry (e.g., semspec's `model_registry`) - Capabilities and metadata

2. **No Capability-Based Routing**: Components receive a model name string with no way to:
   - Select models based on task type (planning vs coding vs reviewing)
   - Fall back to alternative models on failure
   - Filter to models with specific capabilities (e.g., tool support)

3. **Missing Tool Capability Metadata**: `agentic-model` has no knowledge of whether an endpoint supports function/tool calling:
   - Tools are sent to models that don't support them
   - No validation or graceful degradation
   - Different providers use different tool formats (Anthropic vs OpenAI)

4. **Scattered Context Limits**: `max_tokens` must be manually synchronized between endpoint definitions and `agentic-loop.config.context.model_limits`.

### Current Data Flow (Fragmented)

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│ agentic-dispatch│    │  agentic-loop   │    │  agentic-model  │
│                 │    │                 │    │                 │
│ default_model:  │───▶│ model_limits:   │    │ endpoints:      │
│   "qwen"        │    │   qwen: 131072  │    │   qwen:         │
│                 │    │   claude: 200000│    │     url: ...    │
│                 │    │                 │    │     model: ...  │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                                      │
                              No tool metadata ───────┘
                              No capability routing
                              No fallback chains
```

## Proposed Solution

### New Top-Level Configuration Section

Add `model_registry` as a top-level configuration section alongside `nats`, `services`, and `components`:

```json
{
  "model_registry": {
    "capabilities": {
      "planning": {
        "description": "High-level reasoning, architecture decisions",
        "preferred": ["claude-sonnet", "qwen"],
        "fallback": ["qwen-fast"]
      },
      "coding": {
        "description": "Code generation and implementation",
        "preferred": ["claude-sonnet"],
        "fallback": ["qwen"],
        "requires_tools": true
      },
      "reviewing": {
        "description": "Code review and quality analysis",
        "preferred": ["claude-sonnet", "qwen"],
        "fallback": ["qwen-fast"]
      },
      "fast": {
        "description": "Quick responses, simple tasks",
        "preferred": ["qwen-fast", "claude-haiku"],
        "fallback": []
      }
    },
    "endpoints": {
      "claude-sonnet": {
        "provider": "anthropic",
        "model": "claude-sonnet-4-20250514",
        "max_tokens": 200000,
        "supports_tools": true,
        "tool_format": "anthropic",
        "api_key_env": "ANTHROPIC_API_KEY"
      },
      "claude-haiku": {
        "provider": "anthropic",
        "model": "claude-haiku-3-5-20241022",
        "max_tokens": 200000,
        "supports_tools": true,
        "tool_format": "anthropic",
        "api_key_env": "ANTHROPIC_API_KEY"
      },
      "qwen": {
        "provider": "ollama",
        "url": "${LLM_API_URL:-http://localhost:11434}/v1",
        "model": "qwen3-coder:30b",
        "max_tokens": 131072,
        "supports_tools": true,
        "tool_format": "openai"
      },
      "qwen-fast": {
        "provider": "ollama",
        "url": "${LLM_API_URL:-http://localhost:11434}/v1",
        "model": "qwen3:1.7b",
        "max_tokens": 32768,
        "supports_tools": false
      }
    },
    "defaults": {
      "model": "qwen",
      "capability": "planning"
    }
  }
}
```

### New Data Flow (Unified)

```
┌──────────────────────────────────────────────────────────────────┐
│                      model_registry                               │
│                                                                  │
│  capabilities:           endpoints:              defaults:       │
│    planning → [...]        claude-sonnet: {...}    model: qwen   │
│    coding → [...]          qwen: {...}                           │
│    reviewing → [...]       qwen-fast: {...}                      │
│                                                                  │
│  Full metadata: supports_tools, tool_format, max_tokens, etc.   │
└──────────────────────────────────────────────────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        ▼                     ▼                     ▼
┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐
│ agentic-dispatch│  │  agentic-loop   │  │  agentic-model  │
│                 │  │                 │  │                 │
│ registry.       │  │ registry.       │  │ registry.       │
│  Resolve()      │  │  GetMaxTokens() │  │  GetEndpoint()  │
│  → model name   │  │  → context size │  │  → full config  │
└─────────────────┘  └─────────────────┘  └─────────────────┘
```

## Type Definitions

### Go Types

```go
// pkg/model/registry.go

// Registry provides model selection and configuration.
type Registry struct {
    mu           sync.RWMutex
    capabilities map[string]*CapabilityConfig
    endpoints    map[string]*EndpointConfig
    defaults     *DefaultsConfig
}

// CapabilityConfig defines model preferences for a capability.
type CapabilityConfig struct {
    // Description explains what this capability is for (documentation).
    Description string `json:"description,omitempty"`

    // Preferred lists models in order of preference.
    // The first available model is used.
    Preferred []string `json:"preferred"`

    // Fallback lists backup models if all preferred fail.
    Fallback []string `json:"fallback,omitempty"`

    // RequiresTools filters the chain to only tool-capable endpoints.
    // When true, non-tool-capable models are excluded from resolution.
    RequiresTools bool `json:"requires_tools,omitempty"`
}

// EndpointConfig defines an available model endpoint.
type EndpointConfig struct {
    // Provider identifies the API type: "anthropic", "ollama", "openai", "openrouter"
    Provider string `json:"provider"`

    // URL is the API endpoint (required for ollama/openai, ignored for anthropic)
    URL string `json:"url,omitempty"`

    // Model is the model identifier sent to the provider
    Model string `json:"model"`

    // MaxTokens is the context window size in tokens
    MaxTokens int `json:"max_tokens"`

    // SupportsTools indicates whether this endpoint supports function/tool calling
    SupportsTools bool `json:"supports_tools,omitempty"`

    // ToolFormat specifies the tool calling format: "anthropic" or "openai"
    // Empty string means auto-detect from provider.
    ToolFormat string `json:"tool_format,omitempty"`

    // APIKeyEnv is the environment variable containing the API key
    // Required for anthropic/openai/openrouter, ignored for ollama
    APIKeyEnv string `json:"api_key_env,omitempty"`
}

// DefaultsConfig holds default model settings.
type DefaultsConfig struct {
    // Model is the default model when no capability matches
    Model string `json:"model"`

    // Capability is the default capability when none specified
    Capability string `json:"capability,omitempty"`
}
```

### Registry Interface

```go
// pkg/model/registry.go

// RegistryReader provides read-only access to the model registry.
// This interface is passed to components that need model information.
type RegistryReader interface {
    // Resolve returns the preferred model for a capability.
    // Returns the first model in the preferred list that is available.
    // If requiresTools is set on the capability, filters to tool-capable models.
    Resolve(capability string) string

    // GetFallbackChain returns all models for a capability in order of preference.
    // Includes both preferred and fallback models.
    GetFallbackChain(capability string) []string

    // GetToolCapableFallbackChain returns the fallback chain filtered to
    // only endpoints that support tool calling.
    GetToolCapableFallbackChain(capability string) []string

    // GetEndpoint returns the full endpoint configuration for a model name.
    // Returns nil if the model is not configured.
    GetEndpoint(name string) *EndpointConfig

    // GetMaxTokens returns the context window size for a model.
    // Returns 0 if the model is not configured.
    GetMaxTokens(name string) int

    // GetDefault returns the default model name.
    GetDefault() string

    // ListCapabilities returns all configured capability names.
    ListCapabilities() []string

    // ListEndpoints returns all configured endpoint names.
    ListEndpoints() []string
}

// Validate checks the registry configuration for consistency.
// Returns an error if:
// - Any capability references a non-existent endpoint
// - The default model doesn't exist
// - RequiresTools capability has no tool-capable endpoints
func (r *Registry) Validate() error
```

## Component Integration

### agentic-model Changes

```go
// processor/agentic-model/component.go

type Component struct {
    config   *Config
    registry model.RegistryReader  // NEW: injected registry
    // ...
}

// ResolveEndpoint resolves a model name to full endpoint configuration.
// Resolution order:
// 1. Check model_aliases in local config (backwards compat)
// 2. Check registry endpoints
// 3. Check local config endpoints (backwards compat)
// 4. Return error if not found
func (c *Component) ResolveEndpoint(modelName string) (*model.EndpointConfig, error) {
    // Check aliases first (backwards compat)
    if c.config.ModelAliases != nil {
        if target, ok := c.config.ModelAliases[modelName]; ok {
            modelName = target
        }
    }

    // Try registry first
    if c.registry != nil {
        if ep := c.registry.GetEndpoint(modelName); ep != nil {
            return ep, nil
        }
    }

    // Fallback to local config (backwards compat)
    if ep, ok := c.config.Endpoints[modelName]; ok {
        return convertLocalEndpoint(ep), nil
    }

    return nil, fmt.Errorf("model %q not found in registry or local config", modelName)
}

// handleAgentRequest processes an incoming agent request.
func (c *Component) handleAgentRequest(ctx context.Context, req *agentic.AgentRequest) (*agentic.AgentResponse, error) {
    endpoint, err := c.ResolveEndpoint(req.Model)
    if err != nil {
        return nil, err
    }

    // NEW: Tool capability validation
    if len(req.Tools) > 0 && !endpoint.SupportsTools {
        c.logger.Warn("stripping tools - endpoint doesn't support tool calling",
            "model", req.Model,
            "endpoint_model", endpoint.Model,
            "tool_count", len(req.Tools))
        req.Tools = nil
    }

    // Build client based on provider and tool format
    client := c.getOrCreateClient(endpoint)

    return client.ChatCompletion(ctx, req)
}
```

### agentic-loop Changes

```go
// processor/agentic-loop/component.go

type Component struct {
    config   *Config
    registry model.RegistryReader  // NEW: injected registry
    // ...
}

// getModelLimit returns the context window size for a model.
// Resolution order:
// 1. Check registry max_tokens
// 2. Check local config model_limits (backwards compat)
// 3. Return default from local config
func (c *Component) getModelLimit(modelName string) int {
    // Try registry first
    if c.registry != nil {
        if limit := c.registry.GetMaxTokens(modelName); limit > 0 {
            return limit
        }
    }

    // Fallback to local config (backwards compat)
    if c.config.Context.ModelLimits != nil {
        if limit, ok := c.config.Context.ModelLimits[modelName]; ok {
            return limit
        }
        if limit, ok := c.config.Context.ModelLimits["default"]; ok {
            return limit
        }
    }

    return 128000 // Safe default
}
```

### agentic-dispatch Changes

```go
// processor/agentic-dispatch/component.go

type Component struct {
    config   *Config
    registry model.RegistryReader  // NEW: injected registry
    // ...
}

// resolveModel determines the model to use for a task.
// Resolution order:
// 1. Explicit model in request
// 2. Registry capability resolution (if capability specified)
// 3. Local config default_model (backwards compat)
// 4. Registry default model
func (c *Component) resolveModel(req *DispatchRequest) string {
    // Explicit model takes precedence
    if req.Model != "" {
        return req.Model
    }

    // Capability-based resolution
    if req.Capability != "" && c.registry != nil {
        return c.registry.Resolve(req.Capability)
    }

    // Local config default (backwards compat)
    if c.config.DefaultModel != "" {
        return c.config.DefaultModel
    }

    // Registry default
    if c.registry != nil {
        return c.registry.GetDefault()
    }

    return ""
}
```

## Registry Initialization

### Loading from Config

```go
// pkg/config/loader.go

type Config struct {
    Version       string                     `json:"version"`
    Platform      PlatformConfig             `json:"platform"`
    NATS          NATSConfig                 `json:"nats"`
    ModelRegistry *model.Registry            `json:"model_registry,omitempty"`  // NEW
    Services      map[string]ServiceConfig   `json:"services"`
    Components    map[string]ComponentConfig `json:"components"`
    Streams       map[string]StreamConfig    `json:"streams,omitempty"`
}

// Load parses config and initializes the model registry.
func Load(path string) (*Config, error) {
    cfg, err := loadAndParse(path)
    if err != nil {
        return nil, err
    }

    // Validate model registry if present
    if cfg.ModelRegistry != nil {
        if err := cfg.ModelRegistry.Validate(); err != nil {
            return nil, fmt.Errorf("model_registry validation failed: %w", err)
        }
    }

    return cfg, nil
}
```

### Injecting into Components

```go
// pkg/component/manager.go

func (m *Manager) initializeComponent(name string, cfg ComponentConfig) (Component, error) {
    factory := m.factories[cfg.Type]

    // Build component with registry if available
    opts := []ComponentOption{}
    if m.config.ModelRegistry != nil {
        opts = append(opts, WithModelRegistry(m.config.ModelRegistry))
    }

    return factory.Create(name, cfg.Config, opts...)
}
```

## Backwards Compatibility

The design maintains full backwards compatibility:

1. **No registry configured**: Components use their existing local config
2. **Partial registry**: Components check registry first, fall back to local config
3. **Full registry**: Local config can be empty, everything from registry

### Migration Path

**Phase 1: Add registry support (non-breaking)**
- Add `model_registry` config section parsing
- Add `RegistryReader` interface
- Inject registry into components (optional)
- Components check registry first, then local config

**Phase 2: Deprecate local endpoint config**
- Log deprecation warnings when using local config
- Document migration to registry

**Phase 3: Remove local config (breaking, major version)**
- Remove `agentic-model.config.endpoints`
- Remove `agentic-model.config.model_aliases`
- Remove `agentic-loop.config.context.model_limits`

## Example Configurations

### Minimal (Single Model)

```json
{
  "model_registry": {
    "endpoints": {
      "default": {
        "provider": "ollama",
        "url": "http://localhost:11434/v1",
        "model": "llama3.2",
        "max_tokens": 128000
      }
    },
    "defaults": {
      "model": "default"
    }
  }
}
```

### Production (Multi-Provider with Capabilities)

```json
{
  "model_registry": {
    "capabilities": {
      "planning": {
        "description": "Architecture and high-level reasoning",
        "preferred": ["claude-opus", "claude-sonnet"],
        "fallback": ["qwen"]
      },
      "coding": {
        "description": "Code generation with tool use",
        "preferred": ["claude-sonnet"],
        "fallback": ["qwen"],
        "requires_tools": true
      },
      "reviewing": {
        "description": "Code review and analysis",
        "preferred": ["claude-sonnet", "qwen"],
        "fallback": ["qwen-fast"]
      },
      "fast": {
        "description": "Quick tasks, low latency",
        "preferred": ["claude-haiku", "qwen-fast"],
        "fallback": []
      }
    },
    "endpoints": {
      "claude-opus": {
        "provider": "anthropic",
        "model": "claude-opus-4-5-20251101",
        "max_tokens": 200000,
        "supports_tools": true,
        "tool_format": "anthropic",
        "api_key_env": "ANTHROPIC_API_KEY"
      },
      "claude-sonnet": {
        "provider": "anthropic",
        "model": "claude-sonnet-4-20250514",
        "max_tokens": 200000,
        "supports_tools": true,
        "tool_format": "anthropic",
        "api_key_env": "ANTHROPIC_API_KEY"
      },
      "claude-haiku": {
        "provider": "anthropic",
        "model": "claude-haiku-3-5-20241022",
        "max_tokens": 200000,
        "supports_tools": true,
        "tool_format": "anthropic",
        "api_key_env": "ANTHROPIC_API_KEY"
      },
      "qwen": {
        "provider": "ollama",
        "url": "${LLM_API_URL:-http://localhost:11434}/v1",
        "model": "qwen3-coder:30b",
        "max_tokens": 131072,
        "supports_tools": true,
        "tool_format": "openai"
      },
      "qwen-fast": {
        "provider": "ollama",
        "url": "${LLM_API_URL:-http://localhost:11434}/v1",
        "model": "qwen3:1.7b",
        "max_tokens": 32768,
        "supports_tools": false
      }
    },
    "defaults": {
      "model": "qwen",
      "capability": "planning"
    }
  }
}
```

### Testing (Mock Models)

```json
{
  "model_registry": {
    "capabilities": {
      "planning": { "preferred": ["mock-planner"] },
      "coding": { "preferred": ["mock-coder"], "requires_tools": true },
      "reviewing": { "preferred": ["mock-reviewer"] }
    },
    "endpoints": {
      "mock-planner": {
        "provider": "ollama",
        "url": "http://mock-llm:11434/v1",
        "model": "mock-planner",
        "max_tokens": 32768
      },
      "mock-coder": {
        "provider": "ollama",
        "url": "http://mock-llm:11434/v1",
        "model": "mock-coder",
        "max_tokens": 32768,
        "supports_tools": true
      },
      "mock-reviewer": {
        "provider": "ollama",
        "url": "http://mock-llm:11434/v1",
        "model": "mock-reviewer",
        "max_tokens": 32768
      }
    },
    "defaults": { "model": "mock-planner" }
  }
}
```

## Testing Requirements

### Unit Tests

1. Registry parsing and validation
2. Capability resolution (preferred, fallback, requires_tools)
3. Endpoint lookup
4. Tool capability filtering
5. Backwards compatibility with local config

### Integration Tests

1. agentic-model resolves from registry
2. agentic-loop gets model limits from registry
3. agentic-dispatch uses capability resolution
4. Tool stripping for non-capable models
5. Fallback on model failure

## Implementation Checklist

- [ ] Add `model.Registry` type with JSON marshaling
- [ ] Add `model.RegistryReader` interface
- [ ] Add registry validation (capability → endpoint references)
- [ ] Parse `model_registry` in config loader
- [ ] Add `WithModelRegistry` component option
- [ ] Update agentic-model to use registry
- [ ] Update agentic-loop to use registry for model limits
- [ ] Update agentic-dispatch to support capability-based resolution
- [ ] Add tool capability validation in agentic-model
- [ ] Unit tests for registry
- [ ] Integration tests for component integration
- [ ] Documentation updates

## Questions for Discussion

1. **Health tracking**: Should the registry track endpoint health and automatically skip unhealthy endpoints during resolution?

2. **Dynamic updates**: Should the registry support runtime updates (e.g., via NATS KV watch)?

3. **Cost metadata**: Should endpoints include cost information for budget-aware routing?

4. **Rate limiting**: Should the registry track rate limits per endpoint?

---

## Appendix: Semspec Benefits

Once this is implemented in semstreams, semspec can:

1. **Remove duplicate endpoint definitions** from all config files
2. **Remove `model/registry.go`** - use semstreams registry directly
3. **Simplify developer component** - registry handles tool capability
4. **Simplify context-builder** - registry provides model limits
5. **Reduce config maintenance** - single source of truth

Current semspec config duplication that would be eliminated:

| Config File | Duplicate Sections |
|-------------|-------------------|
| semspec.json | `model_registry` + `agentic-model.endpoints` |
| e2e.json | `model_registry` + `agentic-model.endpoints` |
| e2e-claude.json | `model_registry` + `agentic-model.endpoints` |
| e2e-mock.json | `model_registry` + `agentic-model.endpoints` |

**Total: ~400 lines of duplicate JSON configuration**
