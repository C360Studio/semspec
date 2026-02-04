# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Semspec is a semantic development agent built as a **semstreams extension**. It imports semstreams as a library, registers custom components, and runs them via the component lifecycle.

**Key differentiator**: Persistent knowledge graph eliminates context loss.

## Documentation

| Document | Purpose |
|----------|---------|
| [docs/architecture.md](docs/architecture.md) | System architecture, component registration, semstreams relationship |
| [docs/components.md](docs/components.md) | Component configuration, creating new components |
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
# Start infrastructure (in semstreams repo)
cd ../semstreams
docker-compose -f docker/compose/e2e.yml up -d

# Build and run semspec
go build -o semspec ./cmd/semspec
./semspec --config configs/semspec.json --repo /path/to/project

# Or with auto-generated defaults
./semspec --repo .
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

**JetStream subjects** (`tool.execute.>`, `tool.result.>`) are durable and replay-capable.
**Core NATS subjects** (`tool.register.*`, `tool.heartbeat.*`) are ephemeral request/reply.

## Project Structure

```
semspec/
├── cmd/semspec/main.go       # Binary entry point
├── processor/
│   ├── ast-indexer/          # AST indexer component
│   ├── semspec-tools/        # Tool executor component
│   └── ast/                  # AST parsing library
├── tools/
│   ├── file/executor.go      # file_read, file_write, file_list
│   └── git/executor.go       # git_status, git_branch, git_commit
├── vocabulary/
│   └── ics/                  # ICS 206-01 source classification
├── configs/semspec.json      # Default configuration
└── docs/                     # Documentation
```

## Adding Components

1. Create `processor/<name>/` with component.go, config.go, factory.go
2. Implement `component.Discoverable` interface
3. Call `yourcomponent.Register(registry)` in main.go
4. Add instance config to `configs/semspec.json`

See [docs/components.md](docs/components.md) for detailed guide.

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

### Debug Commands

```bash
# Check message flow via message-logger
curl http://localhost:8080/message-logger/entries?limit=50

# Check KV state
curl http://localhost:8080/message-logger/kv/AGENT_LOOPS

# Container logs
docker compose -f docker/compose/e2e.yml logs -f semspec
```

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
