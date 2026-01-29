# Semspec

Semspec is a semantic development agent built on SemStreams. It provides:

1. **Tool executors** (Go package) - file and git operations registered with semstreams agentic-tools
2. **Web UI** (SvelteKit) - human interface talking to semstreams service manager via HTTP/SSE

**Key differentiator**: Persistent knowledge graph eliminates context loss. Queries like "what code implements auth refresh?" or "what did we decide about token expiry?" return instant answers.

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

## What Semspec Is NOT

Semspec is intentionally thin. It does NOT:

- **Provide a CLI binary** - Semstreams has `input/cli` for terminal interaction
- **Embed NATS** - Connects to semstreams infrastructure
- **Include config loading** - Configuration via semstreams flow system
- **Have entity storage** - Use semstreams graph components
- **Do agentic orchestration** - Use semstreams agentic-loop processor

## Prerequisites

- **Semstreams**: Must be running (`docker-compose -f docker/e2e.yml up -d`)
- **Go 1.22+**: For building tools package from source
- **Ollama**: For LLM inference (configured in semstreams)
- **Node.js 20+**: For web UI development

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
│
├── docs/
│   ├── spec/
│   │   ├── semspec-research-synthesis.md  # Research findings
│   │   └── semspec-vocabulary-spec.md     # Ontology spec
│   └── archive/                            # Historical docs
│
├── go.mod
├── CLAUDE.md
└── README.md
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

## Tool Registration

The tools package exports executors that semstreams imports:

```go
import (
    "github.com/c360/semspec/tools/file"
    "github.com/c360/semspec/tools/git"
)

// Register with agentic-tools component
toolsComponent.RegisterToolExecutor(file.NewExecutor(repoPath))
toolsComponent.RegisterToolExecutor(git.NewExecutor(repoPath))
```

## Development

### Building Tools Package

```bash
go build ./tools/...
```

### Running Tests

```bash
go test ./...
```

### Verbose Test Output

```bash
go test -v ./tools/...
```

## Using Semspec

### Terminal Access (via Semstreams)

Use semstreams' `input/cli` processor for terminal interaction:

```bash
cd ../semstreams
docker-compose -f docker/e2e.yml up -d

# Use the input/cli processor
# (see semstreams documentation)
```

### Web UI (Future)

The web UI will provide:
- Chat interface to the agent
- Dashboard with loop status and activity
- Task management (proposals, specs)
- History and trajectory export
- Settings management

## License

MIT
