# Semspec

A graph-first, spec-driven agentic dev tool. Multi-agent coordination and human-in-the-loop UI included. Built on [SemStreams](https://github.com/c360studio/semstreams).

A persistent knowledge graph carries code entities, decisions, and review history across sessions. Role-scoped lessons learned sharpen each execution cycle. Multi-agent workflows coordinate around the graph with human review at the boundaries that matter.

## Quick Start

**Prerequisites:** [Docker](https://www.docker.com/), an LLM provider (Ollama or API key).
Optional: [Task](https://taskfile.dev/) for convenient commands.

```bash
git clone https://github.com/c360studio/semspec.git
cd semspec
cp .env.example .env
# Edit .env — set at least one LLM provider key, or install Ollama (see below)
```

**Option A: Cloud API** (no GPU required)

```bash
# Set ANTHROPIC_API_KEY, GEMINI_API_KEY, or OPENAI_API_KEY in .env
SEMSPEC_REPO=/path/to/your/project docker compose up -d
```

**Option B: Ollama** (local, no API key)

```bash
ollama pull qwen2.5-coder:7b                                    # 4.7 GB, fits 16 GB RAM
SEMSPEC_REPO=/path/to/your/project docker compose up -d
```

Open **http://localhost:8080**. See [Model Configuration](docs/model-configuration.md) for
larger models and capability tuning.

> **`SEMSPEC_REPO`** is the project you want agents to work on. It gets mounted at
> `/workspace` in the semspec (read-write), sandbox (read-write), and semsource (read-only)
> containers. Omit it to use the semspec repo itself.
>
> Expect two subdirectories to appear under `.semspec/` inside this path:
> - `.semspec/worktrees/task-<id>/` — git worktrees the sandbox creates for isolated agent execution
> - `.semspec/plans/<slug>/` — plan artifacts (plan.md, plan.json) written by semspec
>
> For stricter isolation, point `SEMSPEC_REPO` at a clone or copy of your repo rather than
> your active working tree.

> **File permissions:** The sandbox container defaults to UID 1000. If that doesn't match your
> host user, add your UID to `.env` so files created by agents have correct ownership:
> ```bash
> echo "SANDBOX_UID=$(id -u)" >> .env
> echo "SANDBOX_GID=$(id -g)" >> .env
> ```

### What to Expect

1. **First visit** — The UI redirects to `/settings` and auto-detects your project stack
   (languages, frameworks, tooling).
2. **Configure** — Review the detected settings. Set `org` (your organization name) — this
   field is required and locked after the first plan.
3. **Create a plan** — Navigate to Plans and describe what you want built. The pipeline
   auto-coordinates from there.
4. **Monitor** — Watch real-time agent activity, execution progress, and review verdicts.

See [Project Setup](docs/project-setup.md) for config details.

### Build from Source

Requires Go 1.25+, Docker, and [Task](https://taskfile.dev/).

Semspec runs alongside 5 services (NATS, sandbox, semsource, UI, gateway).
The simplest way to build from source is `task local:up`, which compiles the
Go binary inside Docker and starts the full stack:

```bash
task local:up       # Build semspec from source + start full stack
task local:logs     # Tail logs
task local:down     # Stop
task local:rebuild  # Rebuild just semspec (faster iteration)
```

For bare-metal development (running the binary directly), you still need NATS
and the sandbox running via Docker. The UI and semsource won't be available
in this mode — use the Swagger UI at `http://localhost:8080/docs` instead:

```bash
docker compose up -d nats sandbox
go build -o semspec ./cmd/semspec
SANDBOX_URL=http://localhost:8090 ./semspec --repo /path/to/your/project
```

## Project Setup

Semspec requires a `.semspec/` directory with three config files: `project.json` (stack metadata),
`standards.json` (agent rules), and `checklist.json` (quality gates).

See [Project Setup](docs/project-setup.md) for the full configuration guide, or use the API:

```bash
curl -X POST http://localhost:8080/project-manager/detect    # Auto-detect stack
curl -X POST http://localhost:8080/project-manager/init \    # Generate all three files
  -H "Content-Type: application/json" \
  -d '{"name": "my-project", "description": "..."}'
```

## System Requirements

| Setup | RAM | Disk | GPU |
|-------|-----|------|-----|
| Cloud API only | 4 GB | 2 GB | None |
| Ollama `qwen2.5-coder:7b` | 16 GB | 8 GB | Recommended |
| Ollama `qwen3-coder:30b` | 32 GB+ | 20 GB | Required |

See [Model Configuration](docs/model-configuration.md#development-minimal-resources) for
lightweight setups and [Troubleshooting](docs/model-configuration.md#troubleshooting) for
common errors.

## How It Works

```
plan → architecture → requirements → scenarios → decompose → TDD pipeline [developer → validator → reviewer]
                                                            → requirement review
                                                            → plan rollup review
```

**Plan** — Communicate intent: goal, context, scope. The pipeline is self-coordinating — each
component watches a KV bucket and triggers when it sees the status it owns. Planner drafts,
plan-reviewer validates against standards, architecture-generator produces technology decisions,
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

**Graph** — Persistent institutional memory. Code entities from AST indexing. Standards enforced
during review. Past review decisions and lessons learned carry forward across executions.
Approvals become recognized conventions. Rejected approaches become documented anti-patterns.
Every execution cycle sharpens the next.

## Web UI

Semspec runs as a service with a Web UI at **http://localhost:8080**. The UI provides
real-time plan management, execution monitoring, and agent activity via SSE.

**API Playground**: Swagger UI at `http://localhost:8080/docs`. OpenAPI spec at `/openapi.json`.

## Design Principles

**Graph-first** — Entities and relationships are primary; files are artifacts. Query "what plans affect the auth module?" and get an answer.

**Persistent context** — Every session starts with full project knowledge. No re-explaining.

**Execution-time rigor** — Validation happens when code is written, not hoped for through planning documents. Standards enforced structurally, not assumed.

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
| [Project Setup](docs/project-setup.md) | Standards, quality gates |
| [API Reference](docs/api.md) | REST API surface map — all endpoints, SSE streams |
| [Troubleshooting](docs/model-configuration.md#troubleshooting) | Common model and connection errors |

## License

MIT
