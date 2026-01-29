# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Semspec is a semantic development agent built on SemStreams. It provides:

1. **Tool executors** (Go package) - file and git operations registered with semstreams agentic-tools
2. **Web UI** (SvelteKit) - human interface talking to semstreams service manager via HTTP/SSE

**Key differentiator**: Persistent knowledge graph eliminates context loss. Queries like "what code implements auth refresh?" or "what did we decide about token expiry?" return instant answers.

## What Semspec IS

| Component | Technology | Purpose |
|-----------|------------|---------|
| `tools/` | Go package | Tool executors registered with agentic-tools |
| `ui/` | SvelteKit | Web interface talking to service manager |
| `docs/spec/` | Markdown | Vocabulary specs for graph entities |

## What Semspec is NOT

- **NOT a CLI binary** - Semstreams has `input/cli` for terminal interaction
- **NOT a NATS client** - Web UI uses HTTP to service manager
- **NOT embedded NATS** - Always external infrastructure
- **NOT custom entity storage** - Use graph components with vocabulary predicates
- **NOT a REPL** - Semstreams provides this

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  SEMSTREAMS INFRASTRUCTURE                                                   │
│  ┌──────────┐  ┌───────────────┐  ┌──────────────┐  ┌──────────────┐       │
│  │   NATS   │  │ graph-ingest  │  │ agentic-loop │  │agentic-model │       │
│  │ JetStream│  │ graph-index   │  │              │  │   (Ollama)   │       │
│  └──────────┘  └───────────────┘  └──────────────┘  └──────────────┘       │
│                                                                              │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐                       │
│  │ agentic-tools│  │    router    │  │   service    │◄── HTTP/SSE           │
│  │              │  │  input/cli   │  │   manager    │                       │
│  └──────┬───────┘  └──────────────┘  └──────────────┘                       │
│         │                                                                    │
│         │  SEMSPEC TOOLS (Go package)                                        │
│         └── file_read, file_write, file_list                                │
│             git_status, git_branch, git_commit                              │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    │ HTTP API + SSE
                                    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  SEMSPEC WEB UI (SvelteKit)                                                  │
│  • Chat view (talks to router via HTTP)                                     │
│  • Dashboard (loop status, activity feed)                                   │
│  • Tasks (proposals, specs)                                                 │
│  • History (trajectories, export)                                           │
│  • Settings                                                                  │
└─────────────────────────────────────────────────────────────────────────────┘
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
| agentic-tools | Tool dispatch |
| input/cli | CLI input handling (stdin REPL) |
| service manager | HTTP API + SSE for web UI |
| router | Command routing with registration |
| Config loading | Flow-based configuration |

### NEVER Do These Things

- **Build a CLI binary** - Semstreams has `input/cli`
- **Embed NATS** - Prevents clustering, loses persistence, can't scale
- **Create custom entity storage** - Use graph components with vocabulary predicates
- **Rebuild agentic processors** - They exist in semstreams
- **Build config loader** - Semstreams handles configuration
- **Build a NATS client** - Web UI uses HTTP to service manager

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
# Run tests (tools package)
go test ./...

# Run single package tests
go test ./tools/file/...
go test ./tools/git/...

# Run with verbose output
go test -v ./...

# Build tools package (library, no binary)
go build ./tools/...
```

## Tool Registration

The tools package exports executors that semstreams imports or registers:

```go
// In semstreams config or registration code:
import (
    "github.com/c360/semspec/tools/file"
    "github.com/c360/semspec/tools/git"
)

// Register with agentic-tools component
toolsComponent.RegisterToolExecutor(file.NewExecutor(repoPath))
toolsComponent.RegisterToolExecutor(git.NewExecutor(repoPath))
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

## Testing Patterns

- Tests create temp directories with `t.TempDir()` for isolation
- Git tests use `setupTestRepo()` helper to create real git repos
- Use `context.WithTimeout` for controlled async operations
- Test both success and failure paths

## Project Structure

```
semspec/
├── tools/                    # Go package - tool executors
│   ├── file/
│   │   ├── executor.go       # FileExecutor implements ToolExecutor
│   │   └── executor_test.go
│   └── git/
│       ├── executor.go       # GitExecutor implements ToolExecutor
│       └── executor_test.go
│
├── ui/                       # SvelteKit web UI (future)
│   ├── src/
│   │   ├── lib/
│   │   │   ├── api/          # HTTP client for service manager
│   │   │   ├── stores/       # Svelte 5 runes stores
│   │   │   └── components/   # UI components
│   │   └── routes/           # SvelteKit pages
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
├── go.mod                    # Just for tools package
├── CLAUDE.md
└── README.md
```

## Vocabulary & Ontology

Semspec uses a formal vocabulary aligned with BFO (Basic Formal Ontology), CCO (Common Core Ontologies), and PROV-O for government/enterprise interoperability. See `docs/spec/semspec-vocabulary-spec.md` for full details.

Internal predicates use three-part dotted notation for NATS wildcard queries: `domain.category.property` (e.g., `semspec.proposal.status`, `agent.loop.role`).
