# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Semspec is a semantic development agent built on SemStreams. It provides:

1. **CLI binary** (Go) - connects to semstreams via NATS and registers file/git tool executors
2. **Tool executors** (Go package) - file and git operations for agentic workflows
3. **Web UI** (SvelteKit, future) - human interface talking to semstreams service manager via HTTP/SSE

**Key differentiator**: Persistent knowledge graph eliminates context loss. Queries like "what code implements auth refresh?" or "what did we decide about token expiry?" return instant answers.

## What Semspec IS

| Component | Technology | Purpose |
|-----------|------------|---------|
| `cmd/semspec/` | Go binary | Tool registration service connecting to semstreams via NATS |
| `tools/` | Go package | Tool executors (file, git operations) |
| `ui/` | SvelteKit | Web interface talking to service manager (future) |
| `docs/spec/` | Markdown | Vocabulary specs for graph entities |

Semspec is a **binary that depends on semstreams** (not the other way around). This keeps semstreams decoupled and semspec as the application layer.

## What Semspec is NOT

- **NOT embedded NATS** - Always external infrastructure via docker-compose
- **NOT custom entity storage** - Use graph components with vocabulary predicates
- **NOT rebuilding agentic processors** - They exist in semstreams

## Architecture

```
┌──────────────────────────────────────────────────────────────────────────────┐
│  DOCKER COMPOSE (local infrastructure)                                        │
│  ┌──────────────────────────────────────────────────────────────────────────┐│
│  │  SEMSTREAMS                                                               ││
│  │  ┌──────────┐  ┌───────────────┐  ┌──────────────┐  ┌──────────────┐    ││
│  │  │   NATS   │  │ graph-ingest  │  │ agentic-loop │  │agentic-model │    ││
│  │  │ JetStream│  │ graph-index   │  │              │  │   (Ollama)   │    ││
│  │  └──────────┘  └───────────────┘  └──────────────┘  └──────────────┘    ││
│  │                                                                           ││
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐                   ││
│  │  │ agentic-tools│  │    router    │  │   service    │◄── HTTP/SSE       ││
│  │  │              │  │  input/cli   │  │   manager    │                   ││
│  │  └──────────────┘  └──────────────┘  └──────────────┘                   ││
│  └──────────────────────────────────────────────────────────────────────────┘│
└──────────────────────────────────────────────────────────────────────────────┘
                                    ▲
                                    │ NATS (tool.execute.*, tool.result.*)
                                    │
┌───────────────────────────────────┴──────────────────────────────────────────┐
│  SEMSPEC BINARY (this repo)                                                   │
│  ┌──────────────────────────────────────────────────────────────────────────┐│
│  │  cmd/semspec/main.go                                                      ││
│  │  ├── Connects to NATS                                                    ││
│  │  ├── Subscribes to tool.execute.file_*, tool.execute.git_*              ││
│  │  ├── Executes tools via tools/file and tools/git                        ││
│  │  └── Publishes results to tool.result.*                                 ││
│  └──────────────────────────────────────────────────────────────────────────┘│
│  ┌──────────────────────────────────────────────────────────────────────────┐│
│  │  tools/                                                                   ││
│  │  ├── file/executor.go    file_read, file_write, file_list               ││
│  │  └── git/executor.go     git_status, git_branch, git_commit             ││
│  └──────────────────────────────────────────────────────────────────────────┘│
└──────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    │ HTTP API + SSE (future)
                                    ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│  SEMSPEC WEB UI (SvelteKit, future)                                          │
└──────────────────────────────────────────────────────────────────────────────┘
```

## Semstreams Relationship (CRITICAL)

Semspec builds ON TOP OF semstreams. It does NOT embed or rebuild semstreams infrastructure.

### What Semstreams Provides (DO NOT REBUILD)

| Component | Purpose |
|-----------|---------|
| NATS JetStream | Messaging & persistence |
| graph-ingest | Entity/triple ingestion |
| graph-index | Entity querying |
| agentic-loop | Agent state machine orchestration |
| agentic-model | LLM calls (Ollama) |
| agentic-tools | Tool dispatch (internal tools) |
| input/cli | CLI input handling (stdin REPL) |
| service manager | HTTP API + SSE for web UI |
| router | Command routing with registration |
| Config loading | Flow-based configuration |

### NEVER Do These Things

- **Embed NATS** - Prevents clustering, loses persistence, can't scale
- **Create custom entity storage** - Use graph components with vocabulary predicates
- **Rebuild agentic processors** - They exist in semstreams
- **Build config loader** - Semstreams handles configuration

## Local-First Infrastructure

Semspec is designed for edge/offline operation. NATS is required but runs locally via docker-compose.

### Starting Infrastructure

```bash
# In semstreams repo
cd ../semstreams
docker-compose -f docker/compose/e2e.yml up -d    # Core: NATS + semstreams

# Optional ML services
docker-compose -f docker/compose/services.yml --profile embedding up -d
docker-compose -f docker/compose/services.yml --profile inference up -d
```

### Running Semspec

```bash
# Build and run
go build -o semspec ./cmd/semspec
./semspec --nats-url nats://localhost:4222 --repo /path/to/project

# Or with defaults (localhost NATS, current directory)
./semspec

# With environment variable
NATS_URL=nats://localhost:4222 ./semspec --repo .
```

### CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--nats-url` | `nats://localhost:4222` | NATS server URL |
| `--repo` | `.` | Repository path to operate on |
| `--stream` | `AGENT` | JetStream stream name |
| `--log-level` | `info` | Log level (debug, info, warn, error) |

### Infrastructure Stack

| Service | Port | Purpose |
|---------|------|---------|
| NATS JetStream | 4222 | Messaging + KV persistence |
| NATS Monitoring | 8222 | HTTP monitoring UI |
| Semstreams HTTP | 8080 | Service manager API |
| Semstreams Metrics | 9090 | Prometheus metrics |
| semembed (optional) | 8081 | Text embeddings |
| Ollama (optional) | 11434 | LLM inference |

## Tool Registration via NATS

Semspec registers tools by subscribing to tool execution subjects directly on NATS, bypassing the need to call `RegisterToolExecutor()` on the in-container semstreams component.

### NATS Subject Patterns

| Subject | Direction | Purpose |
|---------|-----------|---------|
| `tool.execute.<name>` | agentic-loop → semspec | Request tool execution |
| `tool.result.<call_id>` | semspec → agentic-loop | Return tool result |
| `tool.register.<name>` | semspec → agentic-tools | Advertise external tool |

### Tool Dispatch Flow

```
agentic-loop                    NATS                         Semspec
     │                            │                            │
     │ ──tool.execute.file_read──▶│──────────────────────────▶│
     │                            │                            │
     │                            │                  Execute(ctx, call)
     │                            │                            │
     │ ◀──tool.result.{call_id}───│◀─────────────────────────│
```

## Graph-First Architecture

**Decision**: Graph is source of truth, markdown rendered for human review.

- Graph stores all entities (proposals, specs, tasks) as the canonical source
- Markdown is rendered on-demand for human review in the UI
- Human approves in UI -> graph updates
- No file watching required
- Clean separation: machines work with graph, humans review markdown

### Entity Storage Pattern

Semspec entities (Proposals, Tasks, etc.) are stored via semstreams graph components using the vocabulary from `docs/spec/semspec-vocabulary-spec.md`:

```go
// WRONG - don't build custom storage
type Proposal struct { ... }
store.CreateProposal(ctx, proposal)

// RIGHT - publish to graph-ingest with vocabulary predicates
nc.Publish("graph.ingest.entity", Entity{
    ID: "semspec.proposal.auth-refresh",
    Predicates: map[string]any{
        "semspec.proposal.status": "exploring",
        "dc.terms.title": "Add auth refresh",
        "prov.attribution.agent": "user:coby",
    },
})
```

## Build & Development Commands

```bash
# Build semspec binary
go build -o semspec ./cmd/semspec

# Run tests
go test ./...

# Run single package tests
go test ./tools/file/...
go test ./tools/git/...

# Run with verbose output
go test -v ./...

# Build just the tools package (library)
go build ./tools/...
```

## Tool Executors

Tools implement the pattern:
```go
type ToolExecutor interface {
    Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error)
    ListTools() []agentic.ToolDefinition
}
```

**File tools**: Path validation ensures all access stays within repo root (prevents directory traversal).

**Git tools**: Validates conventional commit format: `type(scope)?: description` where type is one of `feat|fix|docs|style|refactor|test|chore|perf|ci|build|revert`.

## Semstreams Agentic-Tools Internals

Understanding how tool registration works in semstreams is useful for debugging.

### Key Files in Semstreams

| File | Purpose |
|------|---------|
| `processor/agentic-tools/executor.go` | ToolExecutor interface + ExecutorRegistry |
| `processor/agentic-tools/component.go` | Component lifecycle, RegisterToolExecutor method |
| `processor/agentic-tools/config.go` | Configuration schema |
| `agentic/tools.go` | ToolCall, ToolResult, ToolDefinition types |

### ToolExecutor Interface (from semstreams)

```go
// In semstreams: processor/agentic-tools/executor.go
type ToolExecutor interface {
    Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error)
    ListTools() []agentic.ToolDefinition
}
```

## Testing Patterns

- Tests create temp directories with `t.TempDir()` for isolation
- Git tests use `setupTestRepo()` helper to create real git repos
- Use `context.WithTimeout` for controlled async operations
- Test both success and failure paths

## Project Structure

```
semspec/
├── cmd/semspec/               # Binary entry point
│   └── main.go                # CLI, NATS connection, tool registration
│
├── tools/                     # Go package - tool executors
│   ├── file/
│   │   ├── executor.go        # FileExecutor implements ToolExecutor
│   │   └── executor_test.go
│   └── git/
│       ├── executor.go        # GitExecutor implements ToolExecutor
│       └── executor_test.go
│
├── ui/                        # SvelteKit web UI (future)
│   ├── src/
│   │   ├── lib/
│   │   │   ├── api/           # HTTP client for service manager
│   │   │   ├── stores/        # Svelte 5 runes stores
│   │   │   └── components/    # UI components
│   │   └── routes/            # SvelteKit pages
│   ├── package.json
│   └── svelte.config.js
│
├── docs/
│   ├── spec/
│   │   ├── semspec-research-synthesis.md  # Valid research
│   │   └── semspec-vocabulary-spec.md     # Gov client requirement
│   └── archive/                            # Pre-semstreams specs
│       └── README.md
│
├── go.mod
├── go.sum
├── CLAUDE.md
└── README.md
```

## Vocabulary & Ontology

Semspec uses a formal vocabulary aligned with BFO (Basic Formal Ontology), CCO (Common Core Ontologies), and PROV-O for government/enterprise interoperability. See `docs/spec/semspec-vocabulary-spec.md` for full details.

Internal predicates use three-part dotted notation for NATS wildcard queries: `domain.category.property` (e.g., `semspec.proposal.status`, `agent.loop.role`).
