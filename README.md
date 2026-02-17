# Semspec

AI coding assistants are powerful but forget everything between sessions. You re-explain the codebase, re-state decisions, re-discover what was already figured out. Semspec solves this with a persistent knowledge graph and execution-time validation—rigor where it matters, not planning ceremony.

## Quick Start

**Prerequisites:** Go 1.25+, Docker

### Docker Compose

```bash
git clone https://github.com/c360studio/semspec.git
cd semspec
docker compose up -d
```

Open **http://localhost:8080** in your browser.

### Build from Source

```bash
git clone https://github.com/c360studio/semspec.git
cd semspec

docker compose up -d nats    # Start NATS infrastructure
go build -o semspec ./cmd/semspec
./semspec --repo .
```

Open **http://localhost:8080** in your browser.

See [docs/02-getting-started.md](docs/02-getting-started.md) for LLM setup.

## How It Works

```
plan → tasks → execute [developer ↔ reviewer] × n
```

**Plan** — Communicate intent: goal, context, scope. Not a detailed specification. A small fix gets three paragraphs. An architecture change gets thorough treatment. When approved, validation runs against project SOPs before tasks are generated.

**Tasks** — Sized to fit context windows. Each task gets a curated context package assembled from the graph: the specific code entities it will touch, the conventions that govern them, the constraints that apply. Every token earns its place through a semantic query, not a copy-paste.

**Execute** — Two adversarial roles. The *developer* has write access and optimizes for task completion. The *reviewer* has read-only access and optimizes for "would I trust this in production." The tension between them is where quality comes from. Rejections route back with specific feedback, trigger task decomposition, or escalate to humans—different failure modes get different recovery paths.

**Graph** — Persistent institutional memory. Code entities from AST indexing. SOPs matched to specific files. Historical patterns. Past review decisions. Corrections sharpen the SOPs. Approvals become recognized conventions. Rejected approaches become documented anti-patterns. Every execution cycle makes the next one better.

## Web UI

Semspec runs as a service with a Web UI at **http://localhost:8080**. The UI provides real-time updates via SSE—essential for async agent workflows where results arrive later.

Commands are entered in the chat interface:

| Command | Description |
|---------|-------------|
| `/plan <description>` | Create a plan with goal, context, scope |
| `/approve <slug>` | Approve a plan for execution |
| `/tasks <slug>` | View tasks for a plan |
| `/execute <slug>` | Execute approved tasks |
| `/help [command]` | Show available commands |

## What's Working

**AST Indexing** — Parses Go, TypeScript, JavaScript. Extracts entities into the graph.

**Tools** — File and git operations agents can call: `file_read`, `file_write`, `file_list`, `git_status`, `git_branch`, `git_commit`.

**Workflow** — Plan-driven workflow stored in `.semspec/plans/{slug}/`.

**Constitution** — Project rules (coding standards, architectural constraints) enforced at validation.

**Question Routing** — Knowledge gap resolution with topic-based routing. SLA tracking and escalation. See [docs/06-question-routing.md](docs/06-question-routing.md).

## Design Principles

**Graph-first** — Entities and relationships are primary; files are artifacts. Query "what plans affect the auth module?" and get an answer.

**Persistent context** — Every session starts with full project knowledge. No re-explaining.

**Execution-time rigor** — Validation happens when code is written, not hoped for through planning documents. SOPs enforced structurally, not assumed.

**Brownfield-native** — Designed for existing codebases. Most real work is evolving what exists, not greenfield.

**Specialized agents** — Different models for different tasks. An architect model for planning, a fast model for implementation, a careful model for review.

## More Info

- [docs/01-how-it-works.md](docs/01-how-it-works.md) — System overview
- [docs/03-architecture.md](docs/03-architecture.md) — Technical architecture

## License

MIT
