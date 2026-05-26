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
# Set ANTHROPIC_API_KEY in .env — the default config ships with Anthropic +
# Ollama endpoints only. Gemini, OpenAI, OpenRouter, and vLLM each need an
# endpoint added — see docs/model-configuration.md for the one-block addition.
SEMSPEC_REPO=/path/to/your/project docker compose up -d
```

**Option B: Ollama** (local, no API key)

Ollama loads models on demand — pull just the one that fits your hardware:

```bash
# 16 GB RAM (recommended starting point):
ollama pull qwen3:14b           # 8.5 GB — handles all capabilities via the default fallback chain

# 32+ GB RAM (stronger coding-specific quality):
ollama pull qwen3-coder:30b     # 19 GB — coder-specialized model

SEMSPEC_REPO=/path/to/your/project docker compose up -d
```

On a 16 GB system the default config still tries `qwen3-coder:30b` first
(returns model-not-found, falls through to `qwen3:14b` after a brief
delay per dispatch). To skip the fallthrough latency, follow the
[Local-Only setup](docs/model-configuration.md#local-only-no-api-keys)
to point the capability chains directly at the model you pulled — or
swap to `qwen2.5-coder:7b` (4.7 GB) and re-route the chains to the
`ollama-coder` endpoint.

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
4. **Monitor** — While a plan is in flight you get three live surfaces:
   - **In-progress panel** at the top of the plan view names the active phase (drafting,
     reviewing, generating requirements/architecture/scenarios, executing, QA) with an
     elapsed-time counter.
   - **Execution timeline** ghost-renders the Planning + Execution stages before any work
     happens, then fills in interactively as each loop completes.
   - **Activity feed** streams agent-loop events in real time; pin-to-bottom autoscroll
     with a "N new ↓" pill if you scroll up.
5. **Inspect** — Click any agent-loop entry to expand the per-step trajectory. The
   request side (system + user prompts with role chips) renders alongside the response
   (assistant text + tool calls). Production ships at `trajectory_detail: "summary"`
   to keep storage lean; flip to `"full"` on the `agentic-loop` component for the
   complete request payload — see [How It Works](docs/how-it-works.md#trajectory-capture--llm-audit-trail).

See [Project Setup](docs/project-setup.md) for config details.

### Build from Source

Requires Go 1.25+, Docker, and [Task](https://taskfile.dev/).

Semspec runs alongside 6 services (NATS, sandbox, semsource, qa-runner, UI, gateway).
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
  -d '{"name": "my-project", "org": "mycompany", "description": "..."}'
```

> `org` is required (first segment of every entity ID) and locked after the first plan.
> Without it the UI redirects to `/settings` and blocks plan creation.

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
                                                            → qa review
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

**QA Review** — After all requirements complete, qa-reviewer synthesizes requirement outcomes
into a final release-readiness verdict. The plan transitions through `reviewing_qa` (or directly
to `complete` when `qa_level=none`) before reaching `complete`. The gate counts completed
requirements, not scenarios. Inputs vary by `qa_level`: `synthesis` reads plan+impl only;
`unit`/`integration`/`full` first route through `ready_for_qa` so sandbox or qa-runner can run
project tests, then feed results into the reviewer.

> Older plans may show a `reviewing_rollup` status. That stage is kept for in-flight plans on
> upgrade but no new code emits it.

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
| [Real-LLM Expectations](docs/real-llm-expectations.md) | Empirical floor — wallclock, loop counts, what we don't yet know |
| [Model Configuration](docs/model-configuration.md) | LLM model and capability configuration |
| [Project Setup](docs/project-setup.md) | Standards, quality gates |
| [Structured Output Levels](docs/structured-output-levels.md) | L1–L4 wire-format discipline for LLM agent output (response_format, tool-use, thinking mode) |
| [Diagnostic Bundles](docs/diagnostic-bundles.md) | `semspec watch` — live monitoring + shareable bundles for adopter handoff |
| [API Reference](docs/api.md) | REST API surface map — all endpoints, SSE streams |
| [Troubleshooting](docs/model-configuration.md#troubleshooting) | Common model and connection errors |

## License

MIT
