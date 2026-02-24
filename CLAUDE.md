# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Semspec is a semantic development agent built as a **semstreams extension**. It imports semstreams as a library, registers custom components, and runs them via the component lifecycle.

**Key differentiator**: Persistent knowledge graph eliminates context loss.

## Documentation

| Document | Purpose |
|----------|---------|
| [docs/01-how-it-works.md](docs/01-how-it-works.md) | How semspec works (start here) |
| [docs/02-getting-started.md](docs/02-getting-started.md) | Setup and first plan |
| [docs/03-architecture.md](docs/03-architecture.md) | System architecture, component registration, semstreams relationship |
| [docs/04-components.md](docs/04-components.md) | Component reference (15 components) |
| [docs/05-workflow-system.md](docs/05-workflow-system.md) | Workflow system, plan coordination, validation |
| [docs/06-question-routing.md](docs/06-question-routing.md) | Knowledge gap resolution, SLA, escalation |
| [docs/07-model-configuration.md](docs/07-model-configuration.md) | LLM model and capability configuration |
| [docs/08-observability.md](docs/08-observability.md) | Observability, trajectory tracking, token metrics |
| [docs/09-sop-system.md](docs/09-sop-system.md) | SOP authoring, ingestion, and enforcement |

## What Semspec IS

| Directory | Purpose |
|-----------|---------|
| `cmd/semspec/` | Semstreams-based binary entry point |
| `processor/plan-coordinator/` | Parallel planner orchestration |
| `processor/planner/` | Single-planner path |
| `processor/plan-reviewer/` | SOP-aware plan validation |
| `processor/context-builder/` | Strategy-based LLM context assembly |
| `processor/source-ingester/` | Document/SOP ingestion |
| `processor/task-generator/` | Plan → task decomposition |
| `processor/task-dispatcher/` | Dependency-aware task dispatch |
| `processor/ast-indexer/` | Go/TS AST parsing → graph entities |
| `processor/ast/` | AST parsing library |
| `tools/` | Tool executor implementations (file, git) |
| `vocabulary/` | Predicate vocabularies (source, spec, semspec, ics) |
| `configs/` | Flow configuration files |

## What Semspec is NOT

- **NOT embedded NATS** - Always external via docker-compose
- **NOT custom entity storage** - Use graph components with vocabulary predicates
- **NOT rebuilding agentic processors** - Reuses semstreams components

## Quick Start

```bash
# Start NATS infrastructure
docker compose up -d nats

# Build and run semspec
go build -o semspec ./cmd/semspec
./semspec --repo .

# Or run full stack with Docker
docker compose up -d
```

## Build Commands

```bash
go build -o semspec ./cmd/semspec   # Build binary
go build ./...                       # Build all packages
go test ./...                        # Run all tests
go mod tidy                          # Update dependencies
```

## Semstreams Relationship (CRITICAL)

Semspec **imports semstreams as a library**. See [docs/03-architecture.md](docs/03-architecture.md) for details.

### Use Semstreams Packages

| Package | Purpose |
|---------|---------|
| `natsclient` | NATS connection with circuit breaker |
| `pkg/retry` | Exponential backoff with jitter |
| `pkg/errs` | Error classification (transient/invalid/fatal) |
| `component.Registry` | Component lifecycle management |
| `vocabulary` | Predicate registration and metadata |

### Consumer Naming Convention

| Provider | Consumer Pattern | Tools |
|----------|-----------------|-------|
| agentic-tools | `agentic-tools-*` | `file_*`, `git_*`, `graph_query`, internal |

Tools are registered globally via `_ "github.com/c360studio/semspec/tools"` init imports and
executed by the semstreams `agentic-tools` component.

## NATS Subjects

| Subject | Transport | Purpose |
|---------|-----------|---------|
| `workflow.trigger.plan-coordinator` | JetStream | Plan coordination trigger |
| `workflow.trigger.planner` | JetStream | Single-planner trigger |
| `workflow.trigger.plan-reviewer` | JetStream | Plan review trigger |
| `context.build.<strategy>` | JetStream | Context build requests |
| `context.built.<strategy>` | JetStream | Context build responses |
| `source.ingest.>` | JetStream | Source/SOP ingestion |
| `agent.task.>` | JetStream | Agent task dispatch |
| `tool.execute.<name>` | JetStream | Tool execution requests |
| `tool.result.<call_id>` | JetStream | Execution results |
| `graph.ingest.entity` | JetStream | AST/source entities |
| `question.answer.<id>` | JetStream | Answer payloads |
| `question.timeout.<id>` | JetStream | SLA timeout events |
| `tool.register.<name>` | Core NATS | Tool advertisement (ephemeral) |
| `tool.heartbeat.semspec` | Core NATS | Provider health (ephemeral) |

See [docs/03-architecture.md](docs/03-architecture.md) for the complete NATS subject reference.

## Project Structure

```
semspec/
├── cmd/semspec/main.go       # Binary entry point (15 component registrations)
├── processor/
│   ├── plan-coordinator/     # Parallel planner orchestration
│   ├── planner/              # Single-planner path
│   ├── plan-reviewer/        # SOP-aware plan validation
│   ├── context-builder/      # Strategy-based context assembly
│   ├── source-ingester/      # Document/SOP ingestion
│   ├── task-generator/       # Plan → task decomposition
│   ├── task-dispatcher/      # Dependency-aware task dispatch
│   ├── ast-indexer/          # Go/TS AST parsing
│   ├── question-answerer/    # LLM question answering
│   ├── question-timeout/     # SLA monitoring and escalation
│   ├── workflow-api/         # Workflow execution queries
│   ├── trajectory-api/       # Trajectory/LLM call queries
│   └── ast/                  # AST parsing library
├── workflow/
│   ├── question.go           # Question store (KV)
│   ├── answerer/             # Registry, router, notifier
│   └── gap/                  # Gap detection parser
├── tools/
│   ├── file/executor.go      # file_read, file_write, file_list
│   └── git/executor.go       # git_status, git_branch, git_commit
├── vocabulary/
│   ├── source/               # source.meta.*, source.doc.*, source.web.*
│   ├── spec/                 # spec.meta.*, spec.rel.*, spec.requirement.*
│   ├── semspec/              # semspec.plan.*, agent.*, code.*, dc.terms.*
│   └── ics/                  # ICS 206-01 source classification
├── configs/
│   ├── semspec.json          # Default configuration
│   └── answerers.yaml        # Question routing config
└── docs/                     # Documentation (01-09)
```

## Adding Components

1. Create `processor/<name>/` with component.go, config.go, factory.go
2. Implement `component.Discoverable` interface
3. Call `yourcomponent.Register(registry)` in main.go
4. Add instance config to `configs/semspec.json`
5. Add schema tags to Config struct (see below)
6. Import component in `cmd/openapi-generator/main.go`

See [docs/04-components.md](docs/04-components.md) for detailed guide.

## Schema Generation

Run `task generate:openapi` to regenerate configuration schemas and OpenAPI specs:

```bash
task generate:openapi
# Generates:
#   specs/openapi.v3.yaml    - HTTP API specification
#   schemas/*.v1.json        - Component configuration schemas
```

### Adding Schema Tags to Components

All component Config structs should have schema tags for documentation and validation:

```go
type Config struct {
    StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream name,category:basic,default:AGENT"`
    Timeout    int    `json:"timeout"     schema:"type:int,description:Timeout in seconds,category:advanced,min:1,max:300,default:30"`
    Ports      *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`
}
```

**Schema tag directives:**
- `type:string|int|bool|float|array|object|ports` - Field type (required)
- `description:text` - Human-readable description
- `category:basic|advanced` - UI organization
- `default:value` - Default value
- `min:N`, `max:N` - Numeric constraints
- `enum:a|b|c` - Valid enum values (pipe-separated)

### Registering Components for Schema Generation

Add your component to `cmd/openapi-generator/main.go`:

```go
import (
    yourcomponent "github.com/c360studio/semspec/processor/your-component"
)

var componentRegistry = map[string]struct{...}{
    "your-component": {
        ConfigType:  reflect.TypeOf(yourcomponent.Config{}),
        Description: "Description of what this component does",
        Domain:      "semspec",
    },
}
```

### Environment Variables

Configuration supports environment variable expansion with defaults:

```json
{
  "url": "${LLM_API_URL:-http://localhost:11434}/v1"
}
```

Common environment variables:
- `LLM_API_URL` - OpenAI-compatible API endpoint (Ollama, vLLM, OpenRouter, etc.)
- `NATS_URL` - NATS server URL

## Graph-First Architecture

Graph is source of truth. Use semstreams graph components with vocabulary predicates:

```go
// RIGHT - publish to graph-ingest
nc.Publish("graph.ingest.entity", Entity{
    ID: "semspec.plan.auth-refresh",
    Predicates: map[string]any{
        "semspec.plan.status": "draft",
        "dc.terms.title": "Add auth refresh",
    },
})
```

## Data Source Boundary: Graph vs Filesystem

Rule of thumb: **If it has semantic meaning, it belongs in the graph. If it's structural or ephemeral, use the filesystem.**

| Data Type | Source | Why |
|-----------|--------|-----|
| Architecture docs | **Graph** | Semantic entities with category, scope, domain — discoverable by any strategy |
| SOPs/Standards rules | **Graph** -> extracted to `standards.json` | Pre-extracted for fast injection without re-querying |
| Code patterns (types, functions) | **Graph** | AST-indexed entities with relationships |
| Existing specs/plans | **Graph** | Semantic entities with status, author, content |
| Source documents | **Graph** | Ingested with metadata for keyword/domain filtering |
| Project file tree | **Filesystem** | Structural listing — no semantic content, changes constantly |
| Git diffs | **Filesystem/Git** | Live working tree state — ephemeral by nature |
| Explicitly requested source files | **Filesystem** | User-specified paths, read on demand |
| Convention files (CONTRIBUTING.md) | **Filesystem** (for now) | Static reference — future: ingest as graph entities |

### Anti-patterns

- Hardcoded file path lists in strategies (e.g. `archDocs := []string{"docs/03-architecture.md", ...}`)
- Reading doc content directly from filesystem when it could be a graph entity
- Bypassing graph because "it's faster" — graph queries are timeout-guarded and cached

### Correct Pattern

```go
// Graph-first with filesystem fallback
if req.GraphReady {
    docs, _ := s.gatherers.Graph.QueryEntitiesByPredicate(ctx, "source.doc")
    // filter by scope/domain, hydrate, budget-allocate
} else {
    // Fallback to filesystem only when graph is genuinely unavailable
}
```

### Document Ingestion for Context Assembly

Architecture docs, API references, and design documents should be:
1. Written to `sources/` with YAML frontmatter (`category`, `scope`, `domain`)
2. Ingested via source-ingester (`source.ingest.document` subject)
3. Stored as graph entities with `source.doc.*` predicates
4. Discovered by context-builder strategies via `QueryEntitiesByPredicate("source.doc")`

The `scope` field controls which strategies can discover the document:
- `plan` — planning and plan-review strategies
- `code` — implementation and review strategies
- `all` — all strategies

## Vocabulary System

Semspec uses semstreams vocabulary patterns. **Import vocabulary packages to auto-register predicates via init().**

### Using Vocabulary Packages

```go
import (
    "github.com/c360/semspec/vocabulary/ics"      // Auto-registers on import
    "github.com/c360/semstreams/vocabulary"
)

// Use predicate constants (NOT inline strings)
triples := []message.Triple{
    {Subject: id, Predicate: ics.PredicateSourceType, Object: string(ics.SourceTypePAI)},
    {Subject: id, Predicate: ics.PredicateConfidence, Object: 85},
}

// Query metadata at runtime
meta := vocabulary.GetPredicateMetadata(ics.PredicateSourceType)
```

### Creating Domain Vocabularies

Follow semstreams patterns in `vocabulary/<domain>/`:

```go
// predicates.go
package mydomain

import "github.com/c360/semstreams/vocabulary"

const PredicateFoo = "mydomain.category.foo"

func init() {
    vocabulary.Register(PredicateFoo,
        vocabulary.WithDescription("Description here"),
        vocabulary.WithDataType("string"),
        vocabulary.WithIRI("https://example.org/foo"))  // Optional RDF mapping
}
```

### Available Vocabularies

| Package | Purpose | Predicates |
|---------|---------|------------|
| `vocabulary/ics` | ICS 206-01 source classification | `source.ics.*`, `source.citation.*` |

## Testing Patterns

- Tests create temp directories with `t.TempDir()`
- Git tests use `setupTestRepo()` helper
- Use `context.WithTimeout` for async operations
- Test both success and failure paths

## Task Commands

This project uses [Task](https://taskfile.dev) for build automation. Taskfiles are in `taskfiles/`.

```bash
task --list              # List all available tasks
task build               # Build semspec binary
task test                # Run unit tests
task e2e:default         # Run all E2E tests (full lifecycle)
```

### E2E Testing

E2E tests verify the complete semspec workflow with real NATS infrastructure.

**IMPORTANT**: Use the task commands - they handle infrastructure lifecycle automatically (clean, build, start, run, cleanup). Do NOT manually run `task e2e:up` before scenario tasks.

```bash
# LLM scenarios (handles full lifecycle automatically)
task e2e:llm -- hello-world           # Run hello-world with local LLM
task e2e:llm -- hello-world claude    # Run with Claude provider
task e2e:llm -- todo-app openrouter   # Run todo-app with OpenRouter

# Run all scenarios
task e2e:default

# UI E2E tests (Playwright)
task e2e:ui                        # Run all UI tests
task e2e:ui -- --ui                # Interactive UI mode
```

**Output**: Task commands include `--json` flag for structured output with metrics.

**Debugging mode** (keeps infrastructure running):
```bash
task e2e:debug                    # Start infra and tail logs
task e2e:run -- hello-world       # Run scenario against running infra
task e2e:down                     # Stop when done
```

**Infrastructure management** (rarely needed directly):
```bash
task e2e:status          # Check service health
task e2e:logs            # Tail all logs
task e2e:nuke            # Nuclear cleanup of all Docker resources
```

## Debugging Workflow

When debugging semspec issues, follow this systematic process. DO NOT grep through logs or guess - use the observability tools.

### Step 1: Check Service Health

```bash
# Is the infrastructure running?
task e2e:status

# NATS health
curl http://localhost:8222/healthz

# Check for circuit breaker trips
curl http://localhost:8080/message-logger/entries?limit=5 | jq '.[0]'
```

### Step 2: Reproduce and Capture Trace ID

```bash
# Send the failing command
curl -s -X POST "http://localhost:8080/agentic-dispatch/message" \
  -H "Content-Type: application/json" \
  -d '{"content":"/your-command here"}' | jq .

# Get trace ID from recent messages
curl -s "http://localhost:8080/message-logger/entries?limit=5" | jq '.[0].trace_id'
```

### Step 3: Query the Trace

```bash
# Use /debug trace to see all messages in the request
/debug trace <trace_id>

# Or via HTTP
curl -s "http://localhost:8080/message-logger/trace/<trace_id>" | jq .
```

### Step 4: Inspect Component State

```bash
# Check workflow state
/debug workflow <slug>

# Check agent loop state
/debug loop <loop_id>

# Check KV buckets
curl http://localhost:8080/message-logger/kv/AGENT_LOOPS | jq .
curl http://localhost:8080/message-logger/kv/WORKFLOWS | jq .
```

### Step 5: Export Debug Snapshot (for sharing/persistence)

```bash
# Creates .semspec/debug/<trace_id>.md with full context
/debug snapshot <trace_id> --verbose
```

### Available Endpoints

| Endpoint | Purpose |
|----------|---------|
| `GET /message-logger/entries?limit=N` | Recent messages (newest first) |
| `GET /message-logger/trace/{traceID}` | All messages in a trace |
| `GET /message-logger/kv/{bucket}` | KV bucket contents |
| `GET :8222/jsz?consumers=true` | JetStream consumer state |
| `GET :8222/connz` | NATS connections |

### Debug Commands

| Command | Purpose |
|---------|---------|
| `/debug trace <id>` | Query messages by trace ID |
| `/debug snapshot <id> [--verbose]` | Export trace to .semspec/debug/ |
| `/debug workflow <slug>` | Show workflow state |
| `/debug loop <id>` | Show agent loop state from KV |
| `/debug help` | List all debug subcommands |

### Common Issues

**Command returns but nothing happens**
1. Check message-logger for the request: `curl .../entries?limit=10`
2. Look for error messages in the trace
3. Check if consumer is running: `curl :8222/jsz?consumers=true`

**"workflow not found" errors**
1. Check slug spelling in `.semspec/plans/`
2. Verify workflow was created: `/debug workflow <slug>`

**Agent loop stuck**
1. Get loop ID from response or message-logger
2. Check loop state: `/debug loop <loop_id>`
3. Check for timeout/error messages in trace

## NATS Messaging Patterns (CRITICAL)

Understanding when to use Core NATS vs JetStream is essential for correct behavior.

### Core NATS vs JetStream

| Use Case | Transport | Why |
|----------|-----------|-----|
| Fire-and-forget notifications | Core NATS | No delivery guarantee needed |
| Heartbeats, health checks | Core NATS | Ephemeral, latest-value-wins |
| Tool registration/discovery | Core NATS | Ephemeral announcements |
| Task dispatch with ordering | **JetStream** | Order matters, must not lose messages |
| Workflow triggers | **JetStream** | Durable, replay-capable |
| Context build requests | **JetStream** | Need delivery confirmation |
| Any message with dependencies | **JetStream** | Must confirm delivery before signaling completion |

### JetStream Publish for Ordering Guarantees

**CRITICAL**: Core NATS `Publish()` is **asynchronous** (buffered). Messages may be reordered when flushed. Use JetStream publish when order matters:

```go
// WRONG - Core NATS publish is async, no ordering guarantee
if err := c.natsClient.Publish(ctx, subject, data); err != nil {
    return err
}
// Message may not be delivered yet when this returns!

// RIGHT - JetStream publish waits for acknowledgment
js, err := c.natsClient.JetStream()
if err != nil {
    return fmt.Errorf("get jetstream: %w", err)
}
if _, err := js.Publish(ctx, subject, data); err != nil {
    return fmt.Errorf("publish: %w", err)
}
// Message is confirmed delivered to stream
```

**When to use JetStream publish:**
- Dispatching tasks where dependent tasks wait for completion signal
- Any publish where subsequent logic assumes message was delivered
- Publishing to subjects that are part of a JetStream stream

### Subject Wildcards

NATS supports wildcards for subscriptions and message-logger queries:

| Pattern | Matches | Example |
|---------|---------|---------|
| `context.build` | Exact match only | Only `context.build` |
| `context.build.*` | Single token wildcard | `context.build.implementation`, `context.build.review` |
| `context.build.>` | Multi-token wildcard | `context.build.impl.task1`, `context.build.a.b.c` |

```go
// Query message-logger with wildcards
entries, err := s.http.GetMessageLogEntries(ctx, 100, "context.build.*")
```

## Payload Registry Pattern (CRITICAL)

All message payloads must be registered with semstreams for proper serialization/deserialization.

### Registering Payloads

Create a `payload_registry.go` file in your component package:

```go
package yourcomponent

import "github.com/c360studio/semstreams/component"

func init() {
    // Register payload types on package import
    if err := component.RegisterPayload(&component.PayloadRegistration{
        Domain:      "your-domain",    // e.g., "context", "workflow"
        Category:    "request",        // e.g., "request", "response", "execution"
        Version:     "v1",
        Description: "Description of this payload type",
        Factory: func() any {
            return &YourPayloadType{}
        },
    }); err != nil {
        panic("failed to register payload: " + err.Error())
    }
}
```

### Implementing Payload Interface

Your payload struct must implement `message.Payload`:

```go
type YourPayload struct {
    RequestID string `json:"request_id"`
    // ... other fields
}

// Schema returns the message type - MUST match registration
func (p *YourPayload) Schema() message.Type {
    return message.Type{
        Domain:   "your-domain",  // Must match RegisterPayload
        Category: "request",      // Must match RegisterPayload
        Version:  "v1",           // Must match RegisterPayload
    }
}

func (p *YourPayload) Validate() error {
    if p.RequestID == "" {
        return fmt.Errorf("request_id required")
    }
    return nil
}
```

### Common Payload Errors

**"unregistered payload type: X"**
- The payload type wasn't registered in `init()`
- Check that `payload_registry.go` exists and is imported
- Verify Domain/Category/Version match between registration and `Schema()`

**Payload not deserializing correctly**
- Ensure the Factory returns a pointer: `func() any { return &YourType{} }`
- Check JSON tags match expected field names

### BaseMessage Wrapping

All NATS messages must be wrapped in `message.BaseMessage`:

```go
// Create payload
payload := &YourPayload{RequestID: uuid.New().String()}

// Wrap in BaseMessage using payload's Schema()
baseMsg := message.NewBaseMessage(payload.Schema(), payload, "your-component-name")

// Marshal and publish
data, err := json.Marshal(baseMsg)
if err != nil {
    return fmt.Errorf("marshal: %w", err)
}

if _, err := js.Publish(ctx, subject, data); err != nil {
    return fmt.Errorf("publish: %w", err)
}
```

## Message Logger Usage

The message-logger captures all messages for debugging. Understanding its behavior is critical.

### Entry Order

**IMPORTANT**: Message logger returns entries **newest first** (descending timestamp). When verifying message order:

```go
entries, _ := http.GetMessageLogEntries(ctx, 100, "agent.task.*")

// entries[0] is the NEWEST message
// entries[len-1] is the OLDEST message

// To get chronological order, sort by timestamp:
sort.Slice(entries, func(i, j int) bool {
    return entries[i].Timestamp.Before(entries[j].Timestamp)
})
```

### Filtering by Subject

Use wildcards to filter entries:

```bash
# Exact subject
curl "http://localhost:8080/message-logger/entries?subject=agent.task.development"

# Single-token wildcard
curl "http://localhost:8080/message-logger/entries?subject=context.build.*"

# Multi-token wildcard
curl "http://localhost:8080/message-logger/entries?subject=workflow.>"
```

### Parsing BaseMessage Structure

Messages in the log are wrapped in BaseMessage. Parse accordingly:

```go
var baseMsg struct {
    ID      string `json:"id"`
    Type    struct {
        Domain   string `json:"domain"`
        Category string `json:"category"`
        Version  string `json:"version"`
    } `json:"type"`
    Payload json.RawMessage `json:"payload"`
    Meta    struct {
        CreatedAt  int64  `json:"created_at"`
        Source     string `json:"source"`
    } `json:"meta"`
}

if err := json.Unmarshal(entry.RawData, &baseMsg); err != nil {
    // Handle error
}

// Then unmarshal payload into specific type
var payload YourPayloadType
json.Unmarshal(baseMsg.Payload, &payload)
```

### Buffer Size and Subject Filtering

Configure message-logger in `configs/semspec.json`:

```json
"message-logger": {
    "config": {
        "buffer_size": 10000,
        "monitor_subjects": ["user.>", "agent.>", "tool.>", "graph.>", "context.>", "workflow.>"]
    }
}
```

**Note**: High-volume subjects like `graph.ingest.entity` can fill the buffer quickly. Increase `buffer_size` or filter subjects as needed.

### E2E Test Structure

```
test/e2e/
├── client/              # Test clients (HTTP, NATS, filesystem)
│   ├── http.go          # HTTP gateway client
│   ├── nats.go          # NATS direct client
│   └── filesystem.go    # Filesystem operations
├── config/              # Test configuration constants
├── fixtures/            # Test fixture projects
│   ├── go-project/      # Go fixture for AST tests
│   └── ts-project/      # TypeScript fixture for AST tests
├── scenarios/           # Test scenario implementations
│   ├── plan_workflow.go     # /plan workflow test
│   ├── task_generation.go   # Task generation test
│   ├── task_dispatcher.go   # Task dispatch test
│   ├── rdf_export.go        # /export RDF format test
│   ├── trajectory.go        # Trajectory API test
│   ├── questions_api.go     # Question routing test
│   ├── ast_go.go            # Go AST processor
│   ├── ast_typescript.go    # TypeScript AST processor
│   └── debug_command.go     # Debug commands test
└── workspace/           # Runtime workspace (cleaned between tests)
```

## Infrastructure

| Service | Port | Purpose |
|---------|------|---------|
| NATS JetStream | 4222 | Messaging |
| NATS Monitoring | 8222 | HTTP monitoring |
| Ollama (optional) | 11434 | LLM inference |
