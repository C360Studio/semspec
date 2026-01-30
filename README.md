# Semspec

Semspec is a semantic development agent built as a **semstreams extension**. It imports semstreams as a library, registers custom components, and runs them via the component lifecycle.

**Key differentiator**: Persistent knowledge graph eliminates context loss. Queries like "what code implements auth refresh?" or "what did we decide about token expiry?" return instant answers.

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

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  SEMSTREAMS (imported as library)                                           │
│  ┌──────────┐  ┌───────────────┐  ┌──────────────┐  ┌──────────────┐       │
│  │   NATS   │  │ graph-ingest  │  │ graph-query  │  │graph-gateway │       │
│  │ JetStream│  │ graph-index   │  │              │  │ (HTTP/GraphQL)│       │
│  └──────────┘  └───────────────┘  └──────────────┘  └──────────────┘       │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
        ┌───────────────────────────┼───────────────────────────┐
        │                           │                           │
        ▼                           ▼                           ▼
┌───────────────┐         ┌─────────────────┐         ┌─────────────────┐
│  ast-indexer  │         │  semspec-tools  │         │  constitution   │
│               │         │                 │         │                 │
│ Go AST parsing│         │ file_read/write │         │ Project rules   │
│ Entity extract│         │ git_status/etc  │         │ HTTP endpoints  │
└───────────────┘         └─────────────────┘         └─────────────────┘
        │                           │                           │
        └───────────────────────────┼───────────────────────────┘
                                    │
                                    ▼
                          graph.ingest.entity
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  SEMSPEC WEB UI (SvelteKit)                                                  │
│  • Chat interface                                                            │
│  • Entity queries via graph-gateway                                         │
│  • Constitution management via /api/constitution/                           │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Components

| Component | Purpose |
|-----------|---------|
| `ast-indexer` | Parses Go AST, extracts entities (functions, types, etc.) to graph |
| `semspec-tools` | Tool executor for file and git operations |
| `constitution` | Project constitution rules with HTTP API |

## Project Structure

```
semspec/
├── cmd/semspec/           # Binary entry point
│   └── main.go
├── processor/
│   ├── ast-indexer/       # AST indexer component
│   ├── semspec-tools/     # Tool executor component
│   ├── constitution/      # Constitution component + HTTP handlers
│   └── ast/               # AST parsing library
├── tools/
│   ├── file/              # file_read, file_write, file_list
│   └── git/               # git_status, git_branch, git_commit
├── vocabulary/
│   └── ics/               # ICS 206-01 source classification
├── configs/
│   └── semspec.json       # Default configuration
├── ui/                    # SvelteKit web UI
└── docs/
    ├── architecture.md    # System architecture
    ├── components.md      # Component guide
    └── spec/              # Specifications
```

## Available Tools

### File Operations

| Tool | Description |
|------|-------------|
| `file_read` | Read contents of a file |
| `file_write` | Write contents to a file |
| `file_list` | List files in a directory |

### Git Operations

| Tool | Description |
|------|-------------|
| `git_status` | Get git repository status |
| `git_branch` | Create or switch branches |
| `git_commit` | Commit changes (validates conventional commit format) |

## Constitution HTTP API

The constitution component exposes HTTP endpoints for managing project rules:

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/constitution/` | Get current constitution |
| GET | `/api/constitution/rules` | Get all rules |
| GET | `/api/constitution/rules/{section}` | Get rules by section |
| POST | `/api/constitution/check` | Check content against rules |
| POST | `/api/constitution/reload` | Reload from file |

## NATS Subjects

| Subject | Transport | Purpose |
|---------|-----------|---------|
| `tool.execute.<name>` | JetStream | Tool execution requests |
| `tool.result.<call_id>` | JetStream | Execution results |
| `graph.ingest.entity` | JetStream | AST entities for graph storage |
| `tool.register.<name>` | Core NATS | Tool advertisement |

## Prerequisites

- **NATS JetStream**: External via docker-compose (in semstreams repo)
- **Go 1.22+**: For building the binary
- **Node.js 20+**: For web UI development
- **Ollama** (optional): For LLM inference

## Development

```bash
# Build
go build -o semspec ./cmd/semspec

# Run tests
go test ./...

# Build UI
cd ui && npm install && npm run build
```

## Documentation

| Document | Purpose |
|----------|---------|
| [docs/architecture.md](docs/architecture.md) | System architecture, semstreams relationship |
| [docs/components.md](docs/components.md) | Component configuration guide |
| [docs/spec/semspec-vocabulary-spec.md](docs/spec/semspec-vocabulary-spec.md) | Vocabulary specification |

## License

MIT
