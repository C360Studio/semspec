# Semspec

A graph-first, spec-driven agentic dev tool. Multi-agent coordination and human-in-the-loop UI included. Built on [SemStreams](https://github.com/c360studio/semstreams).

A persistent knowledge graph carries code entities, decisions, and review history across sessions. Role-scoped lessons learned sharpen each execution cycle. Multi-agent workflows coordinate around the graph with human review at the boundaries that matter.

## Quick Start

**Prerequisites:** Docker, an LLM (Ollama or API key).

```bash
git clone https://github.com/c360studio/semspec.git
cd semspec

# Option A: Ollama (local)
ollama pull qwen3-coder:30b
SEMSPEC_REPO=/path/to/your/project docker compose up -d

# Option B: Cloud API
SEMSPEC_REPO=/path/to/your/project ANTHROPIC_API_KEY=sk-ant-... docker compose up -d
```

Open **http://localhost:8080**. See [Model Configuration](docs/model-configuration.md) for
model setup and [Project Setup](#project-setup) for configuring your repo.

> **File permissions:** The sandbox container defaults to UID 1000. If that doesn't match your
> host user, add your UID to `.env` so files created by agents have correct ownership:
> ```bash
> echo "SANDBOX_UID=$(id -u)" >> .env
> echo "SANDBOX_GID=$(id -g)" >> .env
> ```

### Build from Source

```bash
docker compose up -d nats    # Start NATS infrastructure
go build -o semspec ./cmd/semspec
./semspec --repo /path/to/your/project
```

Requires Go 1.25+.

## Project Setup

Semspec requires a `.semspec/` directory with three config files: `project.json` (stack metadata),
`standards.json` (agent rules), and `checklist.json` (quality gates). Optionally, add SOPs as
Markdown files in `.semspec/sources/docs/` for richer, scoped enforcement.

See [Project Setup](docs/project-setup.md) for the full configuration guide, or use the API:

```bash
curl -X POST http://localhost:8080/api/project/detect    # Auto-detect stack
curl -X POST http://localhost:8080/api/project/init \    # Generate all three files
  -H "Content-Type: application/json" \
  -d '{"name": "my-project", "description": "..."}'
```

## How It Works

```
plan → architecture → requirements → scenarios → decompose → TDD pipeline [developer → validator → reviewer]
                                                            → requirement review
                                                            → plan rollup review
```

**Plan** — Communicate intent: goal, context, scope. The pipeline is self-coordinating — each
component watches a KV bucket and triggers when it sees the status it owns. Planner drafts,
plan-reviewer validates against SOPs, architecture-generator produces technology decisions,
then requirement-generator and scenario-generator run in sequence. No coordinator needed.

**Requirements** — The unit of execution. Each requirement gets decomposed into a TaskDAG at
runtime by inspecting the live codebase. Nodes execute serially in dependency order. Scenarios
are acceptance criteria validated at review time, not independent execution units.

**TDD Pipeline** — Three stages run per DAG node, in order:

1. **Developer** — writes tests and implements until they pass (TDD in a single agent)
2. **Validator** — runs structural validation (linting, type checks, conventions)
3. **Reviewer** — reviews the code and returns a verdict: `approved`, `fixable`, `misscoped`,
   or `too_big`

Rejections route back with specific feedback. Code issues go to the Developer. Misscoped or
oversized tasks escalate to humans.

**Requirement Review** — After all DAG nodes for a requirement complete, a reviewer runs against
the full changeset and returns per-scenario verdicts: `approved`, `needs_changes`, or `escalate`.

**Plan Rollup Review** — After all requirements complete, a rollup reviewer synthesizes all requirement
outcomes into a final summary and overall verdict. The plan transitions through `reviewing_rollup`
before reaching `complete`. The rollup gate counts completed requirements, not scenarios.

**Rules Engine** — Declarative JSON rules in `configs/rules/` react to graph entity state changes.
Components write workflow phases; rules handle terminal transitions — approved tasks trigger the
next DAG node, escalated tasks emit events, errors route to recovery. This keeps orchestrator code
free of terminal-state logic.

**Lessons Learned** — Reviewer rejections are classified against error categories and stored as
role-scoped lessons in the graph. Lessons matching the current error patterns are injected into
future prompts for that role. When any error category exceeds a configured threshold, a warning
is emitted. Approvals also capture positive patterns. Five roles: `planner`, `plan-reviewer`,
`developer`, `reviewer`, `architect`.

**Graph** — Persistent institutional memory. Code entities from AST indexing. SOPs matched to
specific files. Past review decisions and lessons learned carry forward across executions.
Approvals become recognized conventions. Rejected approaches become documented anti-patterns.
Every execution cycle sharpens the next.

## Web UI

Semspec runs as a service with a Web UI at **http://localhost:8080**. The UI provides
real-time plan management, execution monitoring, and agent activity via SSE.

**API Playground**: Swagger UI at `http://localhost:8080/docs`. OpenAPI spec at `/openapi.json`.

## Design Principles

**Graph-first** — Entities and relationships are primary; files are artifacts. Query "what plans affect the auth module?" and get an answer.

**Persistent context** — Every session starts with full project knowledge. No re-explaining.

**Execution-time rigor** — Validation happens when code is written, not hoped for through planning documents. SOPs enforced structurally, not assumed.

**Brownfield-native** — Designed for existing codebases. Most real work is evolving what exists, not greenfield.

**Specialized agents** — Different models for different tasks. BMAD-aligned personas give each
role a distinct identity and system prompt. An architect model for planning, a fast model for
implementation, a careful model for review.

**Domain-aware prompts** — A fragment-based prompt assembler composes role-specific, provider-aware system prompts from domain catalogs. Adding a new domain (e.g., research, data engineering) means writing a fragment catalog — no orchestrator changes required.

## More Info

| Document | Purpose |
|----------|---------|
| [How It Works](docs/how-it-works.md) | System overview, message flow, component groups |
| [Model Configuration](docs/model-configuration.md) | LLM model and capability configuration |
| [Project Setup](docs/project-setup.md) | Standards, quality gates, SOPs |
| [API Reference](docs/api.md) | REST API surface map — all endpoints, SSE streams |

## License

MIT
