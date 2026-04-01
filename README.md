# Semspec

A graph-first, spec-driven agentic dev tool. Multi-agent coordination and human-in-the-loop UI included. Built on [SemStreams](https://github.com/c360studio/semstreams).

A persistent knowledge graph carries code entities, decisions, and review history across sessions. Role-scoped lessons learned sharpen each execution cycle. Multi-agent workflows coordinate around the graph with human review at the boundaries that matter.

## Quick Start

**Prerequisites:** Docker.

### Demo Mode (no API keys, no Ollama)

Requires [Task](https://taskfile.dev/installation/) (`brew install go-task`):

```bash
git clone https://github.com/c360studio/semspec.git
cd semspec
task demo
```

Open **http://localhost:3000**. Navigate to **Plans**, click **New Plan**, and type a plan description. The mock LLM generates a plan with canned responses — you can approve, execute, and watch the full pipeline. When done: `task demo:down`.

Demo mode runs against the semspec repo itself, so project config already exists. When you point semspec at your own project, you'll need to set up `.semspec/` first — see [Project Setup](#project-setup) below.

### With a Real LLM

```bash
# 1. Start Ollama and pull models
ollama pull qwen2.5-coder:14b

# 2. Set up your project (see Project Setup below)
cd /path/to/your/project
mkdir -p .semspec/sources/docs
# Create project.json, standards.json, checklist.json (details below)

# 3. Start the stack pointing at your repo
cd /path/to/semspec
SEMSPEC_REPO=/path/to/your/project docker compose up -d
```

Or with an API key instead of Ollama:
```bash
SEMSPEC_REPO=/path/to/your/project ANTHROPIC_API_KEY=sk-ant-... docker compose up -d
```

Open **http://localhost:8080**.

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

Requires Go 1.25+. See [docs/02-getting-started.md](docs/02-getting-started.md) for full setup.

## Project Setup

Semspec requires a `.semspec/` directory in the target repository with three configuration files. There is no setup wizard yet — you create these manually or via the project-manager endpoints.

| File | Purpose | Required |
|------|---------|----------|
| `project.json` | Detected stack: languages, frameworks, tooling | Yes |
| `standards.json` | Rules injected into agent context — coding standards, review criteria | Yes (can be empty) |
| `checklist.json` | Deterministic quality gates — shell commands run after each agent task | Yes (can be empty) |

Without these files, semspec will start but agents won't have project-specific context or quality gates.

### Minimal Setup

```bash
cd /path/to/your/project
mkdir -p .semspec/sources/docs

# Project metadata
cat > .semspec/project.json << 'EOF'
{
  "name": "my-project",
  "description": "Brief description of what this project does",
  "version": "1",
  "languages": [{"name": "Go", "primary": true}],
  "tooling": {}
}
EOF

# Empty standards — add rules as you learn what matters
echo '{"rules":[]}' > .semspec/standards.json

# Empty checklist — add quality gates for your stack
echo '{"checks":[]}' > .semspec/checklist.json
```

### Quality Gates (`checklist.json`)

Quality gates are shell commands that run after each agent task. A failing `required` check
blocks progression to review. Tailor these to your stack:

```json
{
  "checks": [
    {
      "name": "go-build",
      "command": "go build ./...",
      "trigger": ["*.go"],
      "category": "compile",
      "required": true,
      "timeout": "120s",
      "description": "Verify Go code compiles"
    },
    {
      "name": "go-test",
      "command": "go test ./...",
      "trigger": ["*.go", "*_test.go"],
      "category": "test",
      "required": true,
      "timeout": "120s",
      "description": "Run Go tests"
    }
  ]
}
```

Check categories: `compile`, `lint`, `typecheck`, `test`, `format`, `setup`.

### Standards (`standards.json`)

Standards are rules injected into every agent's context. Start empty and add rules as you
discover what agents get wrong:

```json
{
  "rules": [
    {
      "id": "error-handling",
      "text": "All errors must be handled or explicitly propagated. No silently swallowed errors.",
      "severity": "error",
      "category": "code-quality",
      "origin": "manual"
    }
  ]
}
```

Rule severities: `error` (blocks approval), `warning` (flagged but allowed), `info` (informational).

### SOPs (Optional)

For richer enforcement rules with examples and file-scoped applicability, add Markdown files
with YAML frontmatter to `.semspec/sources/docs/`. See [SOP System](docs/09-sop-system.md).

### API-Driven Setup

The project-manager also provides endpoints for automated setup:

```bash
curl -X POST http://localhost:8080/api/project/detect    # Auto-detect stack
curl -X POST http://localhost:8080/api/project/init \    # Generate all three files
  -H "Content-Type: application/json" \
  -d '{"name": "my-project", "description": "..."}'
```

## How It Works

```
plan → requirements → decompose → TDD pipeline [tester → builder → validator → reviewer]
                                              → requirement review [red team (optional) → scenario-reviewer]
                                              → plan rollup review
```

**Plan** — Communicate intent: goal, context, scope. Not a detailed specification. A small fix gets
three paragraphs. An architecture change gets thorough treatment. The pipeline is driven by KV
watches on the PLAN_STATES bucket: the planner triggers on status `created`, drafts the plan, and
writes status `drafted`; the plan-reviewer triggers on `drafted`, validates against SOPs, and sets
`reviewed` or `revision_needed`; on approval the requirement-generator and scenario-generator run
in sequence, each triggered by the status the previous stage wrote. There is no coordinator — each
component self-triggers when it sees the status it owns (the KV Twofer pattern). The plan reaches
`ready_for_execution` after the plan-reviewer approves the generated Scenarios.

**Requirements** — The unit of execution. Scenarios are acceptance criteria attached to a
requirement, validated at review time — not independent execution units. `/execute` triggers the
scenario-orchestrator, which dispatches each pending requirement to the requirement-executor. At
execution time, a decomposer agent inspects the live codebase and calls `decompose_task` to produce
a TaskDAG for that requirement. Nodes in the DAG are executed serially in topological order, so each
task sees the output of its dependencies.

**TDD Pipeline** — Four stages run per DAG node, in order:

1. **Tester** — writes failing tests that define the acceptance criteria
2. **Builder** — implements until the tests pass
3. **Validator** — runs structural validation (linting, type checks, conventions)
4. **Reviewer** — reviews the code and returns a verdict: `approved`, `fixable`, `misscoped`,
   or `too_big`

Rejections route back with specific feedback. Test failures go to the Tester. Code issues go to the
Builder. Misscoped or oversized tasks escalate to humans.

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

Semspec runs as a service with a Web UI at **http://localhost:8080**. The UI provides real-time updates via SSE—essential for async agent workflows where results arrive later.

Commands are entered in the chat interface:

| Command | Description |
|---------|-------------|
| `/plan <description>` | Create a plan with goal, context, scope |
| `/approve <slug>` | Approve a plan and trigger task generation |
| `/execute <slug>` | Execute approved tasks |
| `/debug <subcommand>` | Debug trace, workflow, loop state |
| `/help [command]` | Show available commands |

## What's Working

**AST Indexing** — Parses Go, TypeScript, JavaScript, Python, and Java. Extracts functions, types, interfaces, and packages into the graph via semsource.

**Plan Pipeline** — KV-watch-driven planning pipeline: planner drafts, plan-reviewer validates
against SOPs, requirement-generator and scenario-generator run in sequence. Each component
self-triggers on the PLAN_STATES status it owns — no coordinator required.

**SOP Enforcement** — Project-specific rules (SOPs) are ingested, stored in the graph, and enforced during plan review.
See [SOP System](docs/09-sop-system.md).

**Context Building** — Strategy-based context assembly from the knowledge graph. Six strategies (planning, plan-review,
implementation, review, exploration, question) with priority-based token budgets and graph readiness probing.

**Prompt Assembler** — Fragment-based prompt composition with domain catalogs (software, research). Each TDD
stage gets role-gated, provider-aware system prompts with dynamic content injection (error trends, lessons
learned, behavioral gates). New domains are additive — one fragment catalog file, no orchestrator changes.

**Plan Review** — Automated review validating plans against SOPs, checking scope paths against actual project files,
producing structured findings with verdicts.

**Requirement Execution** — scenario-orchestrator dispatches pending requirements;
requirement-executor decomposes each into a TaskDAG via `decompose_task` and drives serial node
execution. Scenarios serve as acceptance criteria validated at review time.

**TDD Pipeline** — execution-manager runs the tester → builder → validator → reviewer
sequence per DAG node (4 stages, no red team at task level).

**Requirement Review** — requirement-executor runs a reviewer after all DAG nodes complete,
returning per-scenario verdicts against the full requirement changeset.

**Plan Rollup Review** — plan-manager triggers a rollup reviewer after all requirements complete.
The plan transitions through `reviewing_rollup` and the reviewer produces a summary and
overall verdict (`approved` or `needs_attention`).

**Task Dispatch** — Dependency-aware DAG node dispatch with parallel context building per task.

**Question Routing** — Knowledge gap resolution with topic-based routing via `question-router`,
SLA tracking via `question-timeout`, and LLM-backed answering via `question-answerer`.
See [Question Routing](docs/06-question-routing.md).

**Tools** — 12-tool set using a bash-first approach. Core tools: `bash` (universal shell for
files, git, builds, and tests), `submit_work`, `submit_review`, `ask_question`, `answer_question`,
`decompose_task`, `spawn_agent`. Conditional tools: `graph_search`, `graph_query`,
`graph_summary`, `web_search`, `http_request`.

**Graph Gateway** — GraphQL and MCP endpoints for querying the knowledge graph.

## Design Principles

**Graph-first** — Entities and relationships are primary; files are artifacts. Query "what plans affect the auth module?" and get an answer.

**Persistent context** — Every session starts with full project knowledge. No re-explaining.

**Execution-time rigor** — Validation happens when code is written, not hoped for through planning documents. SOPs enforced structurally, not assumed.

**Brownfield-native** — Designed for existing codebases. Most real work is evolving what exists, not greenfield.

**Specialized agents** — Different models for different tasks. An architect model for planning, a fast model for implementation, a careful model for review.

**Domain-aware prompts** — A fragment-based prompt assembler composes role-specific, provider-aware system prompts from domain catalogs. Adding a new domain (e.g., research, data engineering) means writing a fragment catalog — no orchestrator changes required.

## More Info

| Document | Purpose |
|----------|---------|
| [How It Works](docs/01-how-it-works.md) | System overview, message flow, component groups |
| [Getting Started](docs/02-getting-started.md) | Setup, project init, and first plan |
| [Architecture](docs/03-architecture.md) | Technical architecture, component registration |
| [Components](docs/04-components.md) | Component reference (16 semspec components) |
| [Workflow System](docs/05-workflow-system.md) | Workflow system and validation |
| [Question Routing](docs/06-question-routing.md) | Knowledge gap resolution, SLA, escalation |
| [Model Configuration](docs/07-model-configuration.md) | LLM model and capability configuration |
| [Observability](docs/08-observability.md) | Trajectory tracking, token metrics |
| [SOP System](docs/09-sop-system.md) | SOP authoring and enforcement |
| [Behavioral Controls](docs/10-behavioral-controls.md) | Behavioral controls for autonomous agents |
| [Execution Pipeline](docs/11-execution-pipeline.md) | NATS subjects, consumers, payload types |
| [Plan API](docs/12-plan-api.md) | REST API for plans, requirements, scenarios, change proposals |
| [Sandbox Security](docs/13-sandbox-security.md) | Sandbox security model: isolation, env filtering, threat model |
| [CQRS Patterns](docs/14-cqrs-patterns.md) | Payload registry, single-writer managers, KV Twofer |
| [UI Architecture](docs/15-ui-architecture.md) | Data flow, SSE stores, reactivity patterns |

## License

MIT
