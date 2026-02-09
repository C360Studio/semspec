# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Semspec is a semantic development agent built as a **semstreams extension**. It imports semstreams as a library, registers custom components, and runs them via the component lifecycle.

**Key differentiator**: Persistent knowledge graph eliminates context loss.

## Documentation

| Document | Purpose |
|----------|---------|
| [docs/how-it-works.md](docs/how-it-works.md) | How semspec works (start here) |
| [docs/getting-started.md](docs/getting-started.md) | Setup and first proposal |
| [docs/architecture.md](docs/architecture.md) | System architecture, component registration, semstreams relationship |
| [docs/components.md](docs/components.md) | Component configuration, creating new components |
| [docs/question-routing.md](docs/question-routing.md) | Knowledge gap resolution, SLA, escalation |
| [docs/spec/semspec-vocabulary-spec.md](docs/spec/semspec-vocabulary-spec.md) | Vocabulary specification (BFO/CCO/PROV-O) |

## What Semspec IS

| Directory | Purpose |
|-----------|---------|
| `cmd/semspec/` | Semstreams-based binary entry point |
| `processor/ast-indexer/` | Go AST parsing → graph entity extraction |
| `processor/semspec-tools/` | File/git tool executor component |
| `processor/ast/` | AST parsing library (parser, watcher, entities) |
| `tools/` | Tool executor implementations (file, git) |
| `vocabulary/ics/` | ICS 206-01 source classification predicates |
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
./semspec cli --repo .

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

Semspec **imports semstreams as a library**. See [docs/architecture.md](docs/architecture.md) for details.

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
| semspec-tools | `semspec-tool-*` | `file_*`, `git_*` |
| agentic-tools | `agentic-tools-*` | `graph_query`, internal |

Different consumer names prevent message competition.

## NATS Subjects

| Subject | Transport | Direction | Purpose |
|---------|-----------|-----------|---------|
| `tool.execute.<name>` | JetStream | Input | Tool execution requests (durable) |
| `tool.result.<call_id>` | JetStream | Output | Execution results (durable) |
| `tool.register.<name>` | Core NATS | Output | Tool advertisement (ephemeral) |
| `tool.heartbeat.semspec` | Core NATS | Output | Provider health (ephemeral) |
| `graph.ingest.entity` | JetStream | Output | AST entities (durable) |
| `agent.task.question-answerer` | JetStream | Internal | Question answering tasks |
| `question.answer.<id>` | JetStream | Output | Answer payloads |
| `question.timeout.<id>` | JetStream | Output | SLA timeout events |
| `question.escalate.<id>` | JetStream | Output | Escalation events |

**JetStream subjects** (`tool.execute.>`, `tool.result.>`, `question.*`) are durable and replay-capable.
**Core NATS subjects** (`tool.register.*`, `tool.heartbeat.*`) are ephemeral request/reply.

## Project Structure

```
semspec/
├── cmd/semspec/main.go       # Binary entry point
├── processor/
│   ├── ast-indexer/          # AST indexer component
│   ├── semspec-tools/        # Tool executor component
│   ├── question-answerer/    # LLM question answering
│   ├── question-timeout/     # SLA monitoring and escalation
│   └── ast/                  # AST parsing library
├── workflow/
│   ├── question.go           # Question store (KV)
│   ├── answerer/             # Registry, router, notifier
│   └── gap/                  # Gap detection parser
├── tools/
│   ├── file/executor.go      # file_read, file_write, file_list
│   └── git/executor.go       # git_status, git_branch, git_commit
├── vocabulary/
│   └── ics/                  # ICS 206-01 source classification
├── configs/
│   ├── semspec.json          # Default configuration
│   └── answerers.yaml        # Question routing config
└── docs/                     # Documentation
```

## Adding Components

1. Create `processor/<name>/` with component.go, config.go, factory.go
2. Implement `component.Discoverable` interface
3. Call `yourcomponent.Register(registry)` in main.go
4. Add instance config to `configs/semspec.json`
5. Add schema tags to Config struct (see below)
6. Import component in `cmd/openapi-generator/main.go`

See [docs/components.md](docs/components.md) for detailed guide.

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
    ID: "semspec.proposal.auth-refresh",
    Predicates: map[string]any{
        "semspec.proposal.status": "exploring",
        "dc.terms.title": "Add auth refresh",
    },
})
```

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

See [docs/spec/semspec-vocabulary-spec.md](docs/spec/semspec-vocabulary-spec.md) for full predicate reference.

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

```bash
# Run all E2E scenarios
task e2e:default

# HTTP Gateway scenarios (recommended)
task e2e:status          # /status command via HTTP
task e2e:propose         # /propose with entity creation
task e2e:workflow        # Full propose → design → spec → tasks → check → approve
task e2e:rdf-export      # /export command with RDF formats

# Legacy NATS direct scenarios
task e2e:basic           # workflow-basic scenario
task e2e:constitution    # constitution enforcement

# AST processor scenarios
task e2e:ast-go          # Go AST processor
task e2e:ast-typescript  # TypeScript AST processor

# Integration scenarios
task e2e:brownfield      # existing codebase workflow
task e2e:greenfield      # new project workflow

# Direct runner (after task e2e:up)
./bin/e2e --workspace $(pwd)/test/e2e/workspace all
./bin/e2e list           # List available scenarios

# Infrastructure management
task e2e:up              # Start NATS + semspec containers
task e2e:down            # Stop containers
task e2e:logs            # Tail all logs
task e2e:status          # Check service health
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
1. Check slug spelling in `.semspec/changes/`
2. Verify workflow was created: `/debug workflow <slug>`

**Agent loop stuck**
1. Get loop ID from response or message-logger
2. Check loop state: `/debug loop <loop_id>`
3. Check for timeout/error messages in trace

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
│   ├── status_command.go    # /status command test
│   ├── propose_workflow.go  # /propose with entity creation
│   ├── full_workflow.go     # Complete workflow test
│   ├── rdf_export.go        # /export RDF format test
│   ├── workflow_basic.go    # Legacy NATS workflow
│   ├── constitution.go      # Constitution enforcement
│   ├── ast_go.go            # Go AST processor
│   ├── ast_typescript.go    # TypeScript AST processor
│   ├── brownfield.go        # Existing codebase test
│   └── greenfield.go        # New project test
└── workspace/           # Runtime workspace (cleaned between tests)
```

## Infrastructure

| Service | Port | Purpose |
|---------|------|---------|
| NATS JetStream | 4222 | Messaging |
| NATS Monitoring | 8222 | HTTP monitoring |
| Ollama (optional) | 11434 | LLM inference |
