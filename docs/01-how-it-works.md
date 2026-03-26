# How Semspec Works

This document explains what happens when you use semspec, from infrastructure to command execution.
Start here before reading the architecture or component guides.

## What is Semspec?

Semspec is a spec-driven development agent with a **persistent knowledge graph**. It helps you:

- Create structured plans, designs, and specifications
- Track code entities (functions, types, packages) across your codebase
- Enforce project standards (SOPs) during planning and code review
- Query accumulated context that persists across sessions

**The key insight**: Traditional AI coding assistants lose context between sessions. Semspec stores
everything in a knowledge graph, so your AI assistant remembers your codebase, your plans, and your
team's coding standards.

## Reactive Execution Model (ADR-025)

Semspec supports two execution modes, selected by the `reactive_mode` flag on the `task-generator`
component.

### Reactive Mode (default: `reactive_mode=true`)

Planning produces Requirements and Scenarios only. Task decomposition happens at runtime for each
Requirement:

```
Plan approved → Requirements → Scenarios → ready_for_execution
                                                  │
                                          scenario-orchestrator
                                                  │
                          ┌───────────────────────┼───────────────────────┐
                          ▼                       ▼                       ▼
              requirement-execution-loop  requirement-execution-loop  requirement-execution-loop
                   (Requirement 1)             (Requirement 2)           (Requirement N)
                          │
                 LLM: decompose_task
                          │
                       TaskDAG
                          │
                  dag-execution-loop
                          │
                ┌─────────┼─────────┐
                ▼         ▼         ▼
             node A     node B    node C
                          │(after A)
```

**Why reactive mode exists**: Requirements describe *desired behavior*; Scenarios are the acceptance
criteria that verify it. The best decomposition into implementation tasks depends on what the code
looks like at execution time — not at planning time. Reactive mode lets the agent inspect the live
codebase and choose the right task structure for each Requirement when it is ready to execute.

### Static Mode (`reactive_mode=false`)

Planning produces a fully decomposed task graph upfront:

```
Plan approved → Requirements → Scenarios → Phases → Tasks → tasks.json → dispatch
```

All tasks are known before any execution begins. Task dependencies are resolved at dispatch time
by `task-dispatcher`. Use when you need deterministic task graphs or want human review of tasks
before execution.

### Agent Tool Set

Semspec uses an 11-tool bash-first approach. All file, git, and shell operations go through `bash`.
Specialized tools exist only for things bash cannot do.

**Always-available tools:**

| Tool | Description |
|------|-------------|
| `bash` | Universal shell — files, git, builds, tests, any shell command |
| `submit_work` | Signal task completion (terminal: StopLoop=true) |
| `ask_question` | Signal a blocker requiring human or agent answer (terminal: StopLoop=true) |
| `decompose_task` | Decompose a goal into a validated TaskDAG; loop exits with DAG as result |
| `spawn_agent` | Spawn a child agent loop for a subtask; block until it completes |
| `review_scenario` | Submit a scenario review verdict with structured findings |

**Conditional tools** (enabled when the relevant service or API key is configured):

| Tool | Condition |
|------|-----------|
| `graph_search` | Graph gateway available — synthesized answer from `globalSearch` |
| `graph_query` | Graph gateway available — raw GraphQL for entity lookup |
| `graph_summary` | Graph gateway available — knowledge graph overview (call once first) |
| `web_search` | `BRAVE_SEARCH_API_KEY` set |
| `http_request` | Always registered; persists fetched content to graph as `source.web.*` entities |

### ChangeProposal Cancellation

When a ChangeProposal is accepted during reactive execution, running scenario loops are cancelled
via `CancellationSignal` messages on `agent.signal.cancel.<loopID>`. Affected Scenarios are
re-queued for fresh execution with the updated behavioral contracts.

See [Workflow System](05-workflow-system.md#reactive-workflows-adr-025) for the detailed rule
descriptions of the `dag-execution-loop` and `requirement-execution-loop` reactive workflows.

## The Semstreams Relationship

Semspec is an **extension** of semstreams, not a standalone tool.

```
┌─────────────────────────────────────────────────────────┐
│  semstreams (infrastructure library)                     │
│  ├── NATS messaging                                      │
│  ├── agentic-loop (LLM reasoning with tool use)         │
│  ├── agentic-model (LLM API calls)                      │
│  ├── graph-* (knowledge graph storage)                  │
│  ├── workflow-processor (declarative workflow runner)   │
│  └── component lifecycle                                 │
└─────────────────────────────────────────────────────────┘
                          ▲
                          │ imports as library
                          │
┌─────────────────────────────────────────────────────────┐
│  semspec (this project)                                  │
│  ├── Planning    (planner, plan-manager, plan-reviewer,  │
│  │               requirement-generator, scenario-gen)   │
│  ├── Execution   (scenario-orchestrator, requirement-executor,  │
│  │               execution-manager, change-proposal-handler)    │
│  └── Support     (plan-manager, project-manager, validators,    │
│                   question-router, Q&A, etc.)                   │
└─────────────────────────────────────────────────────────┘
```

**What this means for you**:

1. Semspec imports semstreams as a Go library
2. Docker Compose runs the shared infrastructure (NATS, optional Ollama)
3. The semspec binary registers and runs all 16 semspec-specific components

## What You Need Running

With Docker Compose (recommended):

| Component | Container | Purpose |
|-----------|-----------|---------|
| **NATS JetStream** | `nats` | Message bus for all communication |
| **Semspec** | `semspec` | All components, code parsing, tool execution |
| **Ollama** | External (host) | LLM inference (optional if using Claude API) |

```bash
# Start NATS and semspec together
docker compose up -d

# Open http://localhost:8080
```

For development (building from source):

```bash
# Start just NATS from Docker Compose
docker compose up -d nats

# Run semspec locally
./semspec --repo .
```

## What Happens When You Run /plan

This is the complete message flow for `/plan Add user authentication`.

### Step 1: Command dispatch

The user types the command in the Web UI. `agentic-dispatch` receives it and publishes a trigger
to `workflow.trigger.plan-coordinator` on the NATS WORKFLOWS stream.

```
┌─ WEB UI ───────────────────────────────────────────────────┐
│  /plan Add user authentication                              │
└────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─ AGENTIC-DISPATCH ─────────────────────────────────────────┐
│  Publishes to: workflow.trigger.plan-coordinator            │
│  Payload: { title: "Add user authentication", slug: ... }  │
└────────────────────────────────────────────────────────────┘
```

### Step 2: Plan coordinator initializes

The `plan-coordinator` component receives the trigger and creates a planning session in the
`PLAN_SESSIONS` KV bucket. It then requests context from the `context-builder`.

### Step 3: Context assembly

The `context-builder` runs the `PlanningStrategy`, assembling context in seven steps within a
32,000-token budget. Steps are ordered by priority: high-priority items consume budget first.

```
PlanningStrategy (context-builder)
  Step 1: File tree             ← filesystem read, always fast
  Step 2: Codebase summary      ← graph query, timeout-guarded
  Step 3: Architecture docs     ← filesystem reads from docs/
  Step 4: Existing specs        ← graph query, timeout-guarded
  Step 5: Relevant code patterns← graph query, timeout-guarded
  Step 6: Requested files       ← filesystem, caller-specified
  Step 7: Planning SOPs         ← graph query, best-effort
```

On first use, the context-builder probes the graph pipeline with exponential backoff. If the graph
is not yet ready, graph-backed steps are skipped cleanly — the file tree and filesystem steps always
run.

### Step 4: Parallel planning

The `plan-coordinator` asks the LLM to identify one to three focus areas for the plan, then spawns
parallel planning goroutines — one per focus area. Each goroutine calls the LLM independently and
produces a partial plan JSON.

```
plan-coordinator
  ├── LLM: determine focus areas (1-3)
  ├── goroutine: focus area 1 → partial plan JSON
  ├── goroutine: focus area 2 → partial plan JSON   (if needed)
  └── goroutine: focus area 3 → partial plan JSON   (if needed)
```

### Step 5: Plan synthesis and file write

When all planners complete, the `plan-coordinator` synthesizes the partial plans into a single
coherent plan via one final LLM call, then writes the result to disk:

```
.semspec/plans/add-user-authentication/
  ├── metadata.json    ← status, timestamps, session ID
  └── plan.json        ← Goal, Context, Scope (structured JSON)
```

### Step 6: Plan review

The `plan-coordinator` immediately triggers the `plan-reviewer`. The reviewer assembles its own
context using the `PlanReviewStrategy`, which fetches SOPs with an all-or-nothing budget policy —
the review never proceeds without the applicable SOPs loaded.

```
plan-reviewer
  ├── Requests context: PlanReviewStrategy
  │     Step 1: SOPs (all-or-nothing — fail if SOPs exceed budget)
  │     Step 2: Plan content
  │     Step 3: File tree
  ├── LLM: validates plan against each SOP requirement
  └── Returns verdict: "approved" OR "needs_changes" + findings
```

**Verdict: needs_changes** — The `plan-coordinator` regenerates the plan with the violation findings
included as LLM context, then re-submits for review. This loop repeats up to three times with a
five-second backoff between attempts.

**Verdict: approved** — The `plan-coordinator` publishes a completion signal to
`workflow.result.plan-coordinator.<slug>` and the user is notified.

### Full flow summary

```
User: /plan Add user authentication
  │
  ▼
agentic-dispatch → workflow.trigger.plan-coordinator
  │
  ▼
plan-coordinator: creates PLAN_SESSIONS entry
  │
  ▼
context-builder: PlanningStrategy (file tree + graph + SOPs)
  │
  ▼
plan-coordinator: LLM determines focus areas (1-3)
  │
  ├── parallel goroutine: LLM → partial plan JSON
  ├── parallel goroutine: LLM → partial plan JSON
  └── ...
  │
  ▼
plan-coordinator: LLM synthesizes → plan.json written
  │
  ▼
plan-reviewer: PlanReviewStrategy (SOPs all-or-nothing + plan + file tree)
  │
  ▼
plan-reviewer: LLM validates each SOP requirement
  │
  ├── needs_changes → plan-coordinator retries (max 3, 5s backoff)
  └── approved → workflow.result.plan-coordinator.<slug>
        │
        ▼
      User notified → .semspec/plans/<slug>/plan.json ready
```

## File Structure

After running the planning workflow, your project contains:

```
your-project/
├── .semspec/
│   ├── sources/
│   │   └── docs/               ← SOPs and source documents
│   │       ├── testing-sop.md
│   │       └── api-conventions.md
│   └── plans/
│       └── add-user-authentication/
│           ├── metadata.json   ← Status, timestamps, session ID
│           ├── plan.json       ← Goal, Context, Scope (JSON)
│           └── tasks.json      ← BDD implementation tasks (JSON, after approval)
└── ... your code ...
```

These files are git-friendly. Commit them to preserve context across sessions and team members.

## Component Groups

Semspec registers 16 components at startup alongside the full semstreams component suite.

```
┌──────────── Planning ────────────────────────────────────────────────┐
│  plan-coordinator     Parallel planner orchestration                 │
│  planner              Single-planner fallback path                   │
│  plan-reviewer        SOP-aware plan validation with LLM review      │
│  requirement-generator  Generates structured Requirements from plan  │
│  scenario-generator   Generates BDD Scenarios from Requirements      │
└──────────────────────────────────────────────────────────────────────┘

┌──────────── Execution ───────────────────────────────────────────────┐
│  scenario-orchestrator  Dispatches requirement-execution-loop per    │
│                          pending Requirement                         │
│  requirement-executor   Decomposes Requirements into DAGs, serial    │
│                          node dispatch, and per-scenario review      │
│  execution-manager      TDD pipeline per DAG node:                  │
│                          tester → builder → validator → reviewer    │
│  change-proposal-handler  ChangeProposal OODA loop and cascade      │
└──────────────────────────────────────────────────────────────────────┘

┌──────────── Support ─────────────────────────────────────────────────┐
│  plan-manager         Requirement/Scenario/ChangeProposal HTTP API   │
│  project-manager      Project management HTTP API                    │
│  workflow-validator   Document structure validation (request/reply)  │
│  workflow-documents   File output to .semspec/plans/                 │
│  structural-validator  Structural integrity checks                   │
│  question-answerer    LLM question answering for knowledge gaps      │
│  question-router      Routes questions to registered answerers       │
│  question-timeout     SLA monitoring and escalation                  │
└──────────────────────────────────────────────────────────────────────┘
```

**Note**: `context-builder`, `task-generator`, `task-dispatcher`, `source-ingester`, and
`ast-indexer` are now semstreams or semsource components, not registered by semspec directly.
Source indexing is handled by the external semsource service.

## The Knowledge Graph

Semspec stores three types of entities in the knowledge graph, which persists across sessions.

### Code entities (from semsource)

The semsource service watches your repository and extracts code entities continuously:

- Functions and methods
- Types and interfaces
- Packages and imports

These are published to `graph.ingest.entity` via JetStream and indexed for graph queries. Agents
read them when assembling codebase summaries and relevant code patterns via `graph_search` and
`graph_query` tools.

### Source documents and SOPs (from semsource)

Standard Operating Procedures and reference documents stored in `.semspec/sources/docs/` are
ingested into the graph as source entities by semsource. The plan-reviewer retrieves them
automatically during plan review — no configuration required beyond placing the files.

See [SOP System](09-sop-system.md) for authoring SOPs and the full enforcement lifecycle.

### Workflow entities (plans, tasks, sessions)

Each plan and task becomes a graph entity with predicates describing its status, content, and
relationships. Agents query these when assembling planning context via `graph_search`, so later
plans benefit from awareness of earlier decisions.

## LLM Configuration

Semspec requires an LLM. It supports local models via Ollama and cloud models via the Anthropic API.

### Option 1: Ollama (default)

```bash
ollama serve
ollama pull qwen2.5-coder:14b
```

The default configuration uses `qwen` as the baseline model. See
[Model Configuration](07-model-configuration.md) for the full capability-to-model mapping.

### Option 2: Anthropic API (optional)

```bash
export ANTHROPIC_API_KEY=sk-ant-...
```

When an API key is present, planning and reviewing tasks prefer Claude models automatically.
Ollama models serve as the fallback chain.

### Capability-based model selection

Semspec routes tasks to models based on capability, not by specifying model names directly:

| Capability | Used by | Default (local) |
|------------|---------|-----------------|
| `planning` | planner, task-generator | qwen3 → qwen |
| `reviewing` | plan-reviewer | qwen3 → qwen |
| `coding` | task execution developer role | qwen → qwen3 |
| `writing` | documentation tasks | qwen3 → qwen |
| `fast` | classification, quick tasks | qwen3-fast → qwen |

Override the model for a specific command:

```bash
/plan Add auth --model qwen         # Use specific model
/plan Add auth --capability fast    # Use fast capability
```

## Interfaces

### Web UI (primary)

The web interface is the main way humans interact with semspec:

```bash
./semspec --repo .
# Open http://localhost:8080 in your browser
```

The UI provides:

- **Chat**: Primary interaction — type commands, see responses
- **Entity Browser**: Search and explore the knowledge graph
- **Tasks**: Monitor active loops, pause and resume workflows
- **Real-time Activity**: SSE-powered live updates

### HTTP API (programmatic)

For integration with other tools:

| Endpoint | Purpose |
|----------|---------|
| `POST /agentic-dispatch/message` | Send commands |
| `GET /agentic-dispatch/loops` | List active loops |
| `GET /agentic-dispatch/activity` | SSE event stream |

## Debugging

### Using /debug

The `/debug` command provides trace correlation and snapshot export:

```bash
# Query all messages in a trace
/debug trace 0af7651916cd43dd8448eb211c80319c

# Export debug snapshot to file
/debug snapshot 0af7651916cd43dd8448eb211c80319c --verbose
# Creates: .semspec/debug/{trace_id}.md

# Check workflow state
/debug workflow add-user-auth

# Check agent loop state from KV
/debug loop loop_456
```

Run `/debug help` for the full command reference.

### Check message flow

```bash
# View recent messages
curl http://localhost:8080/message-logger/entries?limit=50

# Filter by subject (workflow triggers only)
curl "http://localhost:8080/message-logger/entries?subject=workflow.trigger.*&limit=20"

# Query messages by trace ID
curl http://localhost:8080/message-logger/trace/{traceID}

# Check KV state for plan sessions
curl http://localhost:8080/message-logger/kv/PLAN_SESSIONS
```

### Find trace IDs

```bash
curl http://localhost:8080/message-logger/entries?limit=10 | jq '.[].trace_id'
```

### Check container logs

```bash
docker compose logs -f semspec
```

### Check NATS health

```bash
curl http://localhost:8222/healthz
```

## Next Steps

| Document | Purpose |
|----------|---------|
| [Getting Started](02-getting-started.md) | Quick setup and first plan |
| [Architecture](03-architecture.md) | Technical deep-dive: components, NATS subjects, tool dispatch |
| [Components](04-components.md) | Component configuration and adding new components |
| [Workflow System](05-workflow-system.md) | Validation, model selection, and orchestration details |
| [SOP System](09-sop-system.md) | Authoring and enforcing Standard Operating Procedures |
| [Question Routing](06-question-routing.md) | Knowledge gap resolution, SLA, escalation |
| [Model Configuration](07-model-configuration.md) | LLM setup, capabilities, and fallback chains |
