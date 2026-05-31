---
name: new-component
description: Scaffold a new semstreams processor component with all required files following project conventions
---

# New Component Scaffold

Create a new semstreams processor component in `processor/<name>/`. The argument is the component name (kebab-case, e.g., `my-processor`).

## Files to Create

### 1. `processor/<name>/config.go`

Config struct with JSON + schema tags, Validate(), and DefaultConfig():

```go
package <pkgname>

import (
    "fmt"
    "github.com/c360studio/semstreams/component"
)

type Config struct {
    Ports      *component.PortConfig `json:"ports"       schema:"type:ports,description:Port configuration,category:basic"`
    StreamName string                `json:"stream_name" schema:"type:string,description:JetStream stream name,category:advanced,default:AGENT"`
    // Add component-specific fields with schema tags
}

func (c *Config) Validate() error {
    // Validate required fields
    return nil
}

func DefaultConfig() Config {
    return Config{
        Ports: &component.PortConfig{
            // Define input/output ports
        },
        StreamName: "AGENT",
    }
}
```

### 2. `processor/<name>/component.go`

Component struct implementing `component.Discoverable`:

```go
package <pkgname>

import (
    "context"
    "encoding/json"
    "fmt"
    "log/slog"
    "reflect"
    "time"

    "github.com/c360studio/semstreams/component"
    "github.com/c360studio/semstreams/natsclient"
)

var configSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

type Component struct {
    name       string
    config     Config
    natsClient *natsclient.Client
    logger     *slog.Logger
    platform   component.PlatformMeta
}

func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
    var config Config
    if err := json.Unmarshal(rawConfig, &config); err != nil {
        return nil, fmt.Errorf("failed to unmarshal config: %w", err)
    }
    if config.Ports == nil {
        config = DefaultConfig()
        if err := json.Unmarshal(rawConfig, &config); err != nil {
            return nil, fmt.Errorf("failed to unmarshal config: %w", err)
        }
    }
    if err := config.Validate(); err != nil {
        return nil, fmt.Errorf("invalid config: %w", err)
    }
    return &Component{
        name:       "<name>",
        config:     config,
        natsClient: deps.NATSClient,
        logger:     deps.GetLogger(),
        platform:   deps.Platform,
    }, nil
}

func (c *Component) Initialize() error { return nil }

func (c *Component) Start(ctx context.Context) error {
    // Implementation
    return nil
}

func (c *Component) Stop(_ time.Duration) error {
    return nil
}

// Discoverable interface
func (c *Component) Meta() component.Metadata {
    return component.Metadata{
        Name:        "<name>",
        Type:        "processor",
        Description: "<description>",
        Version:     "0.1.0",
    }
}

func (c *Component) InputPorts() []component.Port  { return []component.Port{} }
func (c *Component) OutputPorts() []component.Port  { return []component.Port{} }
func (c *Component) ConfigSchema() component.ConfigSchema { return configSchema }
func (c *Component) Health() component.HealthStatus { return component.HealthStatus{Healthy: true} }
func (c *Component) DataFlow() component.FlowMetrics { return component.FlowMetrics{} }
```

### 3. `processor/<name>/factory.go`

Registration with the component registry:

```go
package <pkgname>

import (
    "fmt"
    "github.com/c360studio/semstreams/component"
)

type RegistryInterface interface {
    RegisterWithConfig(component.RegistrationConfig) error
}

func Register(registry RegistryInterface) error {
    if registry == nil {
        return fmt.Errorf("registry cannot be nil")
    }
    return registry.RegisterWithConfig(component.RegistrationConfig{
        Name:        "<name>",
        Factory:     NewComponent,
        Schema:      configSchema,
        Type:        "processor",
        Protocol:    "<protocol>",
        Domain:      "semspec",
        Description: "<description>",
        Version:     "0.1.0",
    })
}
```

### 4. `processor/<name>/payloads.go` (if component publishes messages)

Payload type with init() registration:

```go
package <pkgname>

import (
    "errors"
    "github.com/c360studio/semstreams/component"
    "github.com/c360studio/semstreams/message"
)

func init() {
    err := component.RegisterPayload(&component.PayloadRegistration{
        Domain:      "<domain>",
        Category:    "<category>",
        Version:     "v1",
        Description: "<description>",
        Factory:     func() any { return &YourPayload{} },
    })
    if err != nil {
        panic("failed to register payload: " + err.Error())
    }
}

var YourPayloadType = message.Type{Domain: "<domain>", Category: "<category>", Version: "v1"}

type YourPayload struct {
    // Fields with json tags
}

func (p *YourPayload) Schema() message.Type  { return YourPayloadType }
func (p *YourPayload) Validate() error       { return nil }
```

## After Scaffolding — Integration Steps

1. **Register in main.go**: Add `<pkgname>.Register(registry)` to `cmd/semspec/main.go`
2. **Add to config**: Add instance config to `configs/semspec.json`
3. **Register for schema generation**: Add to `cmd/openapi-generator/main.go`
4. **Regenerate schemas**: Run `task generate:openapi`

## Reference Implementation

Use `processor/ast-indexer/` as the canonical reference for all patterns.

## Key Rules

- Package name: kebab-to-camelcase (e.g., `my-processor` → `package myprocessor`)
- Config fields MUST have both `json` and `schema` tags
- Payload Domain/Category/Version MUST match between init() registration and Schema() method
- Payload Factory MUST return a pointer: `func() any { return &Type{} }`
- Use `component.GenerateConfigSchema(reflect.TypeOf(Config{}))` for schema generation
- Consumer names must follow convention to avoid message competition
- Use JetStream publish (not Core NATS) when message ordering matters
- Always pass `context.Context` as first parameter to I/O functions
- Wrap errors with context: `fmt.Errorf("operation: %w", err)`
