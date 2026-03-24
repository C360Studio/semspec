# Semspec

A graph-first, spec-driven agentic dev tool. Multi-agent coordination and human-in-the-loop UI included. Built on [SemStreams](https://github.com/c360studio/semstreams).

AI assistants forget everything between sessions. Semspec doesn't. A persistent knowledge graph carries code entities, decisions, and review history forward. Multi-agent workflows—developer, reviewer, architect—coordinate around that graph. You stay in the loop at the boundaries that matter.

## Quick Start

**Prerequisites:** Docker. That's it.

### Try It (no API keys needed)

```bash
git clone https://github.com/c360studio/semspec.git
cd semspec
task demo
```

Open **http://localhost:3000**. Navigate to **Plans**, click **New Plan**, and type a plan description like "Add user authentication with JWT tokens". The mock LLM generates a plan, which you can approve, generate tasks for, and watch the full pipeline execute — all with canned responses, no API keys. When you're done: `task demo:down`.

> No `task` command? Install it: `brew install go-task` or see [taskfile.dev](https://taskfile.dev/installation/).

### With a Real LLM

```bash
# With Ollama (local, free)
ollama pull qwen2.5-coder:14b
docker compose up -d
```

Or with an API key:
```bash
ANTHROPIC_API_KEY=sk-ant-... docker compose up -d
```

Open **http://localhost:8080** in your browser.

### Build from Source

```bash
docker compose up -d nats    # Start NATS infrastructure
go build -o semspec ./cmd/semspec
./semspec --repo .
```

Requires Go 1.25+. See [docs/02-getting-started.md](docs/02-getting-started.md) for full setup guide.

## Project Initialization

Semspec stores project configuration in a `.semspec/` directory at your repository root. Three files define how agents behave for your project:

| File | Purpose |
|------|---------|
| `project.json` | Detected stack: languages, frameworks, tooling, repository metadata |
| `standards.json` | Rules injected into every agent context — coding standards, conventions, review criteria |
| `checklist.json` | Deterministic quality gates (shell commands) run after each agent task — build, lint, test |

**With the UI**: The project-api provides `GET /api/project/status` and `POST /api/project/detect` endpoints. A setup wizard is planned; until then, use the API or seed the files manually.

**Manual setup** (current recommended path):

```bash
mkdir -p .semspec/sources/docs

# Minimal project.json
cat > .semspec/project.json << 'EOF'
{"name":"my-project","description":"Brief description","version":"1"}
EOF

# Empty standards (add rules as you go)
echo '{"rules":[]}' > .semspec/standards.json

# Empty checklist (add quality gates as you go)
echo '{"checks":[]}' > .semspec/checklist.json
```

**Adding quality gates** to `checklist.json`:

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

**Adding standards** to `standards.json`:

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

SOPs (detailed enforcement rules with frontmatter) go in `.semspec/sources/docs/`. See [SOP System](docs/09-sop-system.md).

## How It Works

```
plan → requirements → decompose → TDD pipeline [tester → builder → validator → reviewer]
                                              → requirement review [red team (optional) → scenario-reviewer]
                                              → plan rollup review
```

**Plan** — Communicate intent: goal, context, scope. Not a detailed specification. A small fix gets
three paragraphs. An architecture change gets thorough treatment. A plan-coordinator orchestrates
parallel planners across focus areas, then synthesizes their output. The planner also runs a
requirement-generator and scenario-generator concurrently, producing structured Requirements and
Scenarios — not tasks. Plan-reviewer then validates the result against project SOPs before the plan
reaches `ready_for_execution`.

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

**Requirement Review** — After all DAG nodes for a requirement complete, a reviewer runs and returns
per-scenario verdicts against the full changeset:

- **Red Team** *(when team-based execution is enabled)* — writes adversarial challenges against the
  full requirement changeset: critique and additional tests designed to find gaps across all tasks
- **Scenario Reviewer** — always runs; reviews the complete requirement changeset against all
  scenarios, scores red team performance when present, and returns a verdict: `approved`,
  `needs_changes`, or `escalate`

**Plan Rollup Review** — After all requirements complete, a rollup reviewer synthesizes all requirement
outcomes into a final summary and overall verdict. The plan transitions through `reviewing_rollup`
before reaching `complete`. The rollup gate counts completed requirements, not scenarios.

**Rules Engine** — Declarative JSON rules in `configs/rules/` react to graph entity state changes.
Components write workflow phases; rules handle terminal transitions — approved tasks trigger the
next DAG node, escalated tasks emit events, errors route to recovery. This keeps orchestrator code
free of terminal-state logic.

**Graph** — Persistent institutional memory. Code entities from AST indexing. SOPs matched to
specific files. Past review decisions, red team findings, and team performance data carry forward
across executions. Approvals become recognized conventions. Rejected approaches become documented
anti-patterns. Every execution cycle sharpens the next.

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

**AST Indexing** — Parses Go, TypeScript, JavaScript, Python, and Java. Extracts functions, types, interfaces, and packages into the graph via semsource.

**Plan Coordination** — Parallel planner orchestration with LLM-driven synthesis. Focus areas enable concurrent planning.

**SOP Enforcement** — Project-specific rules (SOPs) are ingested, stored in the graph, and enforced during plan review.
See [SOP System](docs/09-sop-system.md).

**Context Building** — Strategy-based context assembly from the knowledge graph. Six strategies (planning, plan-review,
implementation, review, exploration, question) with priority-based token budgets and graph readiness probing.

**Prompt Assembler** — Fragment-based prompt composition with domain catalogs (software, research). Each TDD
stage gets role-gated, provider-aware system prompts with dynamic content injection (error trends, team
knowledge, behavioral gates). New domains are additive — one fragment catalog file, no orchestrator changes.

**Plan Review** — Automated review validating plans against SOPs, checking scope paths against actual project files,
producing structured findings with verdicts.

**Requirement Execution** — scenario-orchestrator dispatches pending requirements;
requirement-executor decomposes each into a TaskDAG via `decompose_task` and drives serial node
execution. Scenarios serve as acceptance criteria validated at review time.

**TDD Pipeline** — execution-orchestrator runs the tester → builder → validator → reviewer
sequence per DAG node (4 stages, no red team at task level).

**Requirement Review** — requirement-executor runs a reviewer after all DAG nodes complete,
returning per-scenario verdicts against the full requirement changeset. When teams are enabled, a
red team challenge precedes the reviewer; the red team sees the full changeset holistically.

**Plan Rollup Review** — plan-api triggers a rollup reviewer after all requirements complete. The
plan transitions through `reviewing_rollup` and the reviewer produces a summary and
overall verdict (`approved` or `needs_attention`).

**Task Dispatch** — Dependency-aware DAG node dispatch with parallel context building per task.

**Question Routing** — Knowledge gap resolution with topic-based routing, SLA tracking, and escalation.
See [Question Routing](docs/06-question-routing.md).

**Tools** — 11-tool set using a bash-first approach. Core tools: `bash` (universal shell for
files, git, builds, and tests), `submit_work`, `ask_question`, `decompose_task`, `spawn_agent`,
`review_scenario`. Conditional tools: `graph_search`, `graph_query`, `graph_summary`,
`web_search`, `http_request`.

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
| [Components](docs/04-components.md) | Component reference (18 semspec components) |
| [Workflow System](docs/05-workflow-system.md) | Workflow system and validation |
| [Question Routing](docs/06-question-routing.md) | Knowledge gap resolution, SLA, escalation |
| [Model Configuration](docs/07-model-configuration.md) | LLM model and capability configuration |
| [Observability](docs/08-observability.md) | Trajectory tracking, token metrics |
| [SOP System](docs/09-sop-system.md) | SOP authoring and enforcement |
| [Behavioral Controls](docs/10-behavioral-controls.md) | Behavioral controls for autonomous agents |
| [Execution Pipeline](docs/11-execution-pipeline.md) | NATS subjects, consumers, payload types |
| [Plan API](docs/12-plan-api.md) | REST API for plans, requirements, scenarios, change proposals |
| [Sandbox Security](docs/13-sandbox-security.md) | Sandbox security model: isolation, env filtering, threat model |

## License

MIT
