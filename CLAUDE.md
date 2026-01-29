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

| Subject | Direction | Purpose |
|---------|-----------|---------|
| `tool.execute.<name>` | Input | Tool execution requests |
| `tool.result.<call_id>` | Output | Execution results |
| `tool.register.<name>` | Output | Tool advertisement |
| `tool.heartbeat.semspec` | Output | Provider health |
| `graph.ingest.entity` | Output | AST entities |

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

## Infrastructure

| Service | Port | Purpose |
|---------|------|---------|
| NATS JetStream | 4222 | Messaging |
| NATS Monitoring | 8222 | HTTP monitoring |
| Ollama (optional) | 11434 | LLM inference |
