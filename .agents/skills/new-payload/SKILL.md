---
name: new-payload
description: Step-by-step checklist for adding a new payload type to the registry. Use when creating new message types for NATS communication between components.
argument-hint: [PayloadTypeName - description]
---

# New Payload Type Checklist

## What payload type are you adding?

$ARGUMENTS

## Step 1: Define the Type

Create your struct in `workflow/payloads/types.go` or a component-local `payloads.go`:

```go
type YourPayload struct {
    RequestID string `json:"request_id"`
    PlanSlug  string `json:"plan_slug"`
    TraceID   string `json:"trace_id,omitempty"`
    // ... your fields
}
```

## Step 2: Implement the Payload Interface

Every payload must implement `Schema()` and `Validate()`:

```go
import "github.com/c360studio/semstreams/message"

func (p *YourPayload) Schema() message.Type {
    return message.Type{
        Domain:   "workflow",       // or "agentic", "context", etc.
        Category: "your-category",  // e.g., "execution-request"
        Version:  "v1",
    }
}

func (p *YourPayload) Validate() error {
    if p.RequestID == "" {
        return fmt.Errorf("request_id required")
    }
    return nil
}
```

## Step 3: Register in init()

Add to `workflow/payloads/registry.go` (shared payloads) or a component-local
`payload_registry.go`:

```go
import "github.com/c360studio/semstreams/component"

func init() {
    err := component.RegisterPayload(&component.PayloadRegistration{
        Domain:      "workflow",
        Category:    "your-category",
        Version:     "v1",
        Description: "Description of what this payload carries",
        Factory:     func() any { return &YourPayload{} },
    })
    if err != nil {
        panic("failed to register YourPayload: " + err.Error())
    }
}
```

## Step 4: Publish via BaseMessage

All NATS messages must be wrapped in `message.BaseMessage`:

```go
payload := &YourPayload{RequestID: uuid.New().String(), PlanSlug: slug}

baseMsg := message.NewBaseMessage(payload.Schema(), payload, "your-component")

data, err := json.Marshal(baseMsg)
if err != nil {
    return fmt.Errorf("marshal: %w", err)
}

// Use JetStream publish when ordering matters
js, _ := c.natsClient.JetStream()
if _, err := js.Publish(ctx, subject, data); err != nil {
    return fmt.Errorf("publish: %w", err)
}
```

## Step 5: Define a Typed Subject (optional but recommended)

For compile-time type safety at the messaging layer:

```go
import "github.com/c360studio/semstreams/natsclient"

var SubjectYourTrigger = natsclient.NewSubject[YourPayload](
    "workflow.trigger.your-component",
)

// Publish — validates and wraps automatically
err := SubjectYourTrigger.Publish(ctx, client, YourPayload{...})

// Subscribe — handler receives typed T
sub, err := SubjectYourTrigger.Subscribe(ctx, client,
    func(ctx context.Context, payload YourPayload) error {
        return handle(ctx, payload)
    },
)
```

## Step 6: Write Round-Trip Test

```go
func TestYourPayload_RoundTrip(t *testing.T) {
    original := &YourPayload{RequestID: "test-1", PlanSlug: "my-plan"}

    baseMsg := message.NewBaseMessage(original.Schema(), original, "test")
    data, err := json.Marshal(baseMsg)
    require.NoError(t, err)

    var decoded message.BaseMessage
    err = json.Unmarshal(data, &decoded)
    require.NoError(t, err)

    result, ok := decoded.Payload.(*YourPayload)
    require.True(t, ok, "expected *YourPayload, got %T", decoded.Payload)
    assert.Equal(t, original.RequestID, result.RequestID)
}
```

## Verification Checklist

- [ ] Domain/Category/Version match between registration and `Schema()`
- [ ] `payload_registry.go` or `registry.go` has `init()` registration
- [ ] Package is imported (blank import if needed) so `init()` runs
- [ ] Factory returns a pointer: `func() any { return &YourType{} }`
- [ ] Round-trip test passes
- [ ] Published via `message.NewBaseMessage` (not raw JSON)
- [ ] JetStream publish used when ordering matters (not Core NATS)

## Common Mistakes

| Symptom | Cause | Fix |
|---------|-------|-----|
| `unregistered payload type` at runtime | init() not running | Ensure package is imported (blank import) |
| Deserializes as `*message.GenericPayload` | Domain/Category/Version mismatch | Match constants between registration and Schema() |
| Payload never appears in registry | Package not imported | Add blank import in `cmd/semspec/main.go` or `workflow/payloads/registry.go` |
| JSON missing `type` envelope | Published raw instead of via BaseMessage | Use `message.NewBaseMessage()` |

## Reference

Existing semspec payloads to follow:
- `workflow/payloads/types.go` — RequirementExecutionRequest, TriggerPayload, ValidationRequest
- `workflow/payloads/registry.go` — Central registration
- `processor/requirement-executor/payload_registry.go` — Component-local registration
