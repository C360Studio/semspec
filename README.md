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

```bash
ollama pull qwen3:14b           # 8.5 GB — handles all general capabilities
ollama pull qwen3:1.7b          # 1.4 GB — optional, for the `fast` capability

SEMSPEC_REPO=/path/to/your/project docker compose up -d
```

> **Be honest about what local-only buys you.** `qwen3:14b` is the
> realistic floor for local dev — fine for well-defined, simple tasks
> on demo scenarios (`easy` tier). Complex multi-step prompts
> (`medium`/`hard` tiers) likely exceed its capability today. We're
> still empirically calibrating where that floor sits per tier — see
> [Real-LLM Expectations](docs/real-llm-expectations.md). For
> production work, an API key on a frontier model is the realistic
> path; Ollama is for evaluation and iteration on your spec quality.

Power users who want stronger coding-specific quality can pull
`qwen3-coder:30b` (19 GB, needs 32+ GB RAM) and re-route the coding
capability — see
[Model Configuration](docs/model-configuration.md#adding-a-stronger-coding-model).

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
  -d '{"name": "my-project", "org": "mycompany", "description": "..."}'
```

> `org` is required (first segment of every entity ID) and locked after the first plan.
> Without it the UI redirects to `/settings` and blocks plan creation.

## System Requirements

| Setup | RAM | Disk | GPU |
|-------|-----|------|-----|
| Cloud API only | 4 GB | 2 GB | None |
| Ollama `qwen3:14b` (default local) | 16 GB | 10 GB | Recommended |
| Ollama `qwen3:14b` + `qwen3:1.7b` (default + fast) | 16 GB | 12 GB | Recommended |

Heavier local models (e.g. `qwen3-coder:30b` at 32+ GB RAM) are not
default — operators who want them know how to add them. See
[Model Configuration](docs/model-configuration.md) for the full
capability/endpoint reference and
[Real-LLM Expectations](docs/real-llm-expectations.md) for the empirical
floor we've measured per tier.

See [Model Configuration](docs/model-configuration.md#development-minimal-resources) for
lightweight setups and [Troubleshooting](docs/model-configuration.md#troubleshooting) for
common errors.

## How It Works

For the full state chart, happy paths, retry paths, and SemTeams starter contract, see
[End-to-End Flow](docs/e2e-flow.md).

```
plan -> requirements -> architecture -> stories -> scenarios -> execute
                                                   -> TDD pipeline [developer -> validator -> reviewer]
                                                   -> Story / requirement review
                                                   -> qa review
```

Every new Plan also receives a Plan-owned contract packet before downstream BMAD/OpenSpec
handoffs. That packet preserves the original brief, non-negotiable constraints, brownfield
topology obligations, accepted amendments, and must-deliver scope so later agents cannot silently
replace the baseline or shrink the request.

**Plan** — Communicate intent: goal, context, scope. The pipeline is self-coordinating — each
component watches a KV bucket and triggers when it sees the status it owns. Planner drafts,
plan-reviewer validates against standards, requirement-generator produces dependency-aware
requirements, architecture-generator produces technology decisions, story-preparer slices the
work into Stories, and scenario-generator writes Story-scoped evidence. No coordinator needed.

**Requirements and Stories** — Requirements are the scheduling and traceability unit. Stories are
the implementation slices: they bind requirements and capabilities to concrete files, ownership,
and dependencies. At runtime, requirement-executor synthesizes Story task DAGs; task nodes execute
serially in dependency order. Scenarios are acceptance criteria validated at review time, not
independent execution units.

**TDD Pipeline** — Three stages run per DAG node, in order:

1. **Developer** — writes tests and implements until they pass (TDD in a single agent)
2. **Validator** — runs structural validation (linting, type checks, conventions)
3. **Reviewer** — reviews the code and returns a verdict such as `approved`, `fixable`, or
   `restructure`

Rejections route back with specific feedback. Code issues go to the Developer. Restructure
feedback, ownership planning gaps, or exhausted TDD budgets escalate to recovery.

**Requirement Review** — After Story task nodes complete, a reviewer runs against the Story
changeset and scenarios. Approved Stories advance the requirement; fixable feedback reruns the
Story DAG; restructure feedback rebuilds the requirement branch.

**QA Review** — After all requirements complete, qa-reviewer synthesizes requirement outcomes
into a final release-readiness verdict. The plan transitions through `ready_for_qa` and
`reviewing_qa` (or directly to `complete` when `qa_level=none`) before reaching `complete`. The
gate counts completed requirements, not scenarios. Inputs vary by `qa_level`: `synthesis` reads
plan+impl only; `unit` and `integration` include sandbox test results before feeding the reviewer.
`full`/e2e proof remains operator-owned via the emitted `qa.yml`.

> Older plans may show a `reviewing_rollup` status. That stage is kept for in-flight plans on
> upgrade but no new code emits it.

**Event-Driven Components** — Components react to durable state changes and publish regular
semstreams events. `scenario-orchestrator` dispatches ready requirements, `requirement-executor`
synthesizes Story task DAGs, and `execution-manager` owns the TDD task pipeline. Recovery remains
explicit through PlanDecisions instead of hidden terminal-state logic. Scope shrinkage, topology
changes, and whole-phase resets require accepted contract-impact evidence.

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
real-time plan management, execution monitoring, and agent activity via SSE. Plan banners and
detail panels read from the plan-manager `phase_summary`, not from stale feed rows, so execution,
recovery, QA, lesson activity, stale/disconnected state, and cost evidence all have one
authoritative source.

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
