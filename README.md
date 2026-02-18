# Semspec

A graph-first, spec-driven agentic dev tool. Multi-agent coordination and human-in-the-loop UI included. Built on [SemStreams](https://github.com/c360studio/semstreams).

AI assistants forget everything between sessions. Semspec doesn't. A persistent knowledge graph carries code entities, decisions, and review history forward. Multi-agent workflows—developer, reviewer, architect—coordinate around that graph. You stay in the loop at the boundaries that matter.

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

**Plan** — Communicate intent: goal, context, scope. Not a detailed specification. A small fix gets three paragraphs. An architecture change gets thorough treatment. A plan-coordinator orchestrates parallel planners across focus areas, then synthesizes their output into a coherent plan. When approved, validation runs against project SOPs before tasks are generated.

**Tasks** — Sized to fit context windows. Each task gets a curated context package assembled from the graph: the specific code entities it will touch, the conventions that govern them, the constraints that apply. Every token earns its place through a semantic query, not a copy-paste.

**Execute** — Two adversarial roles. The *developer* has write access and optimizes for task completion. The *reviewer* has read-only access and optimizes for "would I trust this in production." The tension between them is where quality comes from. Rejections route back with specific feedback, trigger task decomposition, or escalate to humans—different failure modes get different recovery paths.

**Graph** — Persistent institutional memory. Code entities from AST indexing. SOPs matched to specific files. Historical patterns. Past review decisions. Question answers and escalations. Corrections sharpen the SOPs. Approvals become recognized conventions. Rejected approaches become documented anti-patterns. Every execution cycle makes the next one better.

## Web UI

Semspec runs as a service with a Web UI at **http://localhost:8080**. The UI provides real-time updates via SSE—essential for async agent workflows where results arrive later.

Commands are entered in the chat interface:

| Command | Description |
|---------|-------------|
| `/plan <description>` | Create a plan with goal, context, scope |
| `/approve <slug>` | Approve a plan and trigger task generation |
| `/execute <slug>` | Execute approved tasks |
| `/export <slug>` | Export plan as RDF |
| `/debug <subcommand>` | Debug trace, workflow, loop state |
| `/help [command]` | Show available commands |

## What's Working

**AST Indexing** — Parses Go and TypeScript. Extracts functions, types, interfaces, and packages into the graph.

**Plan Coordination** — Parallel planner orchestration with LLM-driven synthesis. Focus areas enable concurrent planning.

**SOP Enforcement** — Project-specific rules (SOPs) are ingested, stored in the graph, and enforced during plan review.
See [SOP System](docs/09-sop-system.md).

**Context Building** — Strategy-based context assembly from the knowledge graph. Six strategies (planning, plan-review,
implementation, review, exploration, question) with priority-based token budgets and graph readiness probing.

**Plan Review** — Automated review validating plans against SOPs, checking scope paths against actual project files,
producing structured findings with verdicts.

**Task Dispatch** — Dependency-aware task dispatch with parallel context building for each task.

**Question Routing** — Knowledge gap resolution with topic-based routing, SLA tracking, and escalation.
See [Question Routing](docs/06-question-routing.md).

**Tools** — File and git operations for agent use: `file_read`, `file_write`, `file_list`, `git_status`, `git_branch`,
`git_commit`.

**Graph Gateway** — GraphQL and MCP endpoints for querying the knowledge graph.

## Design Principles

**Graph-first** — Entities and relationships are primary; files are artifacts. Query "what plans affect the auth module?" and get an answer.

**Persistent context** — Every session starts with full project knowledge. No re-explaining.

**Execution-time rigor** — Validation happens when code is written, not hoped for through planning documents. SOPs enforced structurally, not assumed.

**Brownfield-native** — Designed for existing codebases. Most real work is evolving what exists, not greenfield.

**Specialized agents** — Different models for different tasks. An architect model for planning, a fast model for implementation, a careful model for review.

## More Info

- [docs/01-how-it-works.md](docs/01-how-it-works.md) — System overview
- [docs/02-getting-started.md](docs/02-getting-started.md) — Setup and first plan
- [docs/03-architecture.md](docs/03-architecture.md) — Technical architecture
- [docs/04-components.md](docs/04-components.md) — Component reference
- [docs/05-workflow-system.md](docs/05-workflow-system.md) — Workflow system and validation
- [docs/06-question-routing.md](docs/06-question-routing.md) — Knowledge gap resolution
- [docs/07-model-configuration.md](docs/07-model-configuration.md) — LLM model configuration
- [docs/08-trajectory-comparison.md](docs/08-trajectory-comparison.md) — Trajectory analysis
- [docs/09-sop-system.md](docs/09-sop-system.md) — SOP authoring and enforcement

## License

MIT
