# Semspec

Semspec is a spec-driven development agent with persistent memory.

The problem it addresses: AI coding assistants are powerful but forget everything between sessions. When you're working on a project over days or weeks, or handing off between different agents, that context loss is painful. You end up re-explaining the codebase, re-stating decisions, re-discovering what was already figured out.

Semspec stores everything in a knowledge graph—code entities, specs, proposals, decisions, relationships. Agents query the graph instead of starting from scratch. One agent explores the codebase and notes how auth works; a different agent picks that up later without asking again.

## Quick Start

**Prerequisites:** Go 1.25+, Docker

### Option A: Docker Compose

```bash
git clone https://github.com/c360studio/semspec.git
cd semspec
docker compose up -d
```

Open http://localhost:8080 in your browser.

> If the Docker image isn't published yet, use Option B.

### Option B: Build from Source

```bash
git clone https://github.com/c360studio/semspec.git
cd semspec

# Start NATS infrastructure
docker compose up -d nats

# Build and run
go build -o semspec ./cmd/semspec
./semspec --repo .
```

Open http://localhost:8080 in your browser.

See [docs/getting-started.md](docs/getting-started.md) for LLM setup and detailed walkthrough.

## Commands

Semspec provides a workflow-driven command set for spec-driven development.

| Command | Description |
|---------|-------------|
| `/plan <description>` | Create a new plan with goal, context, scope |
| `/approve <slug>` | Approve a plan for execution |
| `/tasks <slug>` | Generate implementation tasks from plan |
| `/execute <slug>` | Execute approved tasks |
| `/check <slug>` | Validate against constitution |
| `/archive <slug>` | Archive completed changes |
| `/changes [slug]` | List or show change status |
| `/ask <topic> <question>` | Ask a question routed by topic |
| `/github <action>` | GitHub issue synchronization |
| `/context [query\|slug]` | Query knowledge graph for context |
| `/debug <subcommand>` | Debug tools (trace, workflow, loop, snapshot) |
| `/help [command]` | Show available commands |

Run `/help` in the Web UI to see all commands and their details.

## Running Semspec

Semspec runs as a long-lived service with HTTP endpoints and a Web UI.

```bash
./semspec --repo .
# Open http://localhost:8080
```

The Web UI provides real-time activity updates via SSE, making it the ideal interface for async agent workflows. See [ADR-007](docs/architecture/adr-007-no-cli.md) for why we chose Web UI over CLI.

## What's Working

**AST Indexing** — Parses source files and extracts entities (functions, types, classes) into the graph. Supports Go, TypeScript, and JavaScript.

**Tools** — File and git operations that agents can call:
- `file_read`, `file_write`, `file_list`
- `git_status`, `git_branch`, `git_commit`

**Workflow** — Full spec-driven workflow with filesystem storage in `.semspec/changes/{slug}/`.

**Constitution** — Define project rules (coding standards, architectural constraints) and check code against them.

**Question Routing** — Knowledge gap resolution with topic-based routing to agents, teams, or humans. SLA tracking and escalation. See [docs/question-routing.md](docs/question-routing.md).

**GitHub Sync** — Create epic issues and task checklists from specs.

## What's In Progress

**Graph Entity Publishing** — Code entities (from AST indexing) are published to the graph. Workflow entities (proposals, specs, tasks) are stored in the filesystem via workflow-documents component but not yet published to the graph for cross-referencing.

**Multi-Agent Coordination** — Specialized agents for different tasks (architect plans, implementer codes, reviewer validates). Right model for the right job, with the graph as shared memory.

**Training Flywheel** — Capture trajectories and feedback to improve models over time. Good completions become training data.

## Relationship to Semstreams

Semspec is built on [semstreams](https://github.com/c360/semstreams). The relationship is **both library and framework**:

**Library aspects** (semspec calls semstreams):
- `natsclient` — NATS connection management
- `config` — Configuration loading and validation
- `metric` — Metrics registry
- `pkg/retry`, `pkg/errs` — Utility packages

**Framework aspects** (semstreams calls semspec):
- `service.Manager` — Manages service lifecycle (Start/Stop), provides HTTP health endpoints
- `component.Registry` — Manages component lifecycle, semspec registers its own components (ast-indexer)

This means semspec uses semstreams utilities directly while also plugging into its lifecycle management. The service manager provides `/health`, `/readyz`, and `/metrics` endpoints automatically on port 8080.

## Project Layout

```
semspec/
├── cmd/semspec/              # Main binary
├── processor/
│   ├── ast-indexer/          # Source file parsing → graph
│   ├── question-answerer/    # LLM question answering
│   ├── question-timeout/     # SLA monitoring, escalation
│   ├── rdf-export/           # RDF serialization output
│   ├── workflow-validator/   # Document validation service
│   └── ast/                  # Shared parsing code
├── output/
│   └── workflow-documents/   # Writes workflow docs to filesystem
├── tools/                    # Tool implementations (file, git)
├── workflow/                 # Question store, routing, validation
├── configs/                  # Example configs
└── docs/                     # Architecture and specs
```

## Design Principles

These come from studying what works and what doesn't in existing tools (SpecKit, OpenSpec, BMAD, Aider):

**Graph-first** — Entities and relationships are primary; files are artifacts. You can query "what specs affect the auth module?" and get an answer.

**Persistent context** — Every session starts with full project knowledge. No more re-explaining.

**Fluid workflows** — Explore freely, commit when ready. Human checkpoints where they matter, not enforced phase gates.

**Brownfield-native** — Designed for existing codebases. Most real work is evolving what exists, not greenfield.

**Specialized agents** — Different models for different tasks. An architect model for planning, a fast model for implementation, a careful model for review.

## More Info

- [docs/architecture.md](docs/architecture.md) — How it fits together
- [docs/roadmap.md](docs/roadmap.md) — What's planned
- [docs/spec/semspec-research-synthesis.md](docs/spec/semspec-research-synthesis.md) — Research behind the design

## License

MIT
