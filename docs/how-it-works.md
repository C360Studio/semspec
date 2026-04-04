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

## Execution Model

Planning produces Requirements and Scenarios. Task decomposition happens at runtime for each
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

Task decomposition happens at execution time because the best breakdown depends on what the code
looks like now — not at planning time. The agent inspects the live codebase and chooses the right
task structure for each Requirement when it is ready to execute.

### Agent Tool Set

Semspec uses an 11-tool bash-first approach. All file, git, and shell operations go through `bash`.
Specialized tools exist only for things bash cannot do.

**Always-available tools:**

| Tool | Description |
|------|-------------|
| `bash` | Universal shell — files, git, builds, tests, any shell command |
| `submit_work` | Signal task completion with structured deliverable (terminal: StopLoop=true) |
| `ask_question` | Signal a blocker requiring human or agent answer (terminal: StopLoop=true) |
| `answer_question` | Provide an answer to a pending question |
| `decompose_task` | Decompose a goal into a validated TaskDAG; loop exits with DAG as result |
| `spawn_agent` | Spawn a child agent loop for a subtask; block until it completes |

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

The `dag-execution-loop` and `requirement-execution-loop` reactive workflows are defined as
declarative JSON rules in `configs/rules/`.

## The Semstreams Relationship

Semspec is an **extension** of semstreams, not a standalone tool.

```
┌─────────────────────────────────────────────────────────┐
│  semstreams (infrastructure library)                     │
│  ├── NATS messaging                                      │
│  ├── agentic-loop (LLM reasoning with tool use)         │
│  ├── agentic-model (LLM API calls)                      │
│  ├── graph-* (knowledge graph storage)                  │
│  └── component lifecycle                                 │
└─────────────────────────────────────────────────────────┘
                          ▲
                          │ imports as library
                          │
┌─────────────────────────────────────────────────────────┐
│  semspec (this project)                                  │
│  ├── Planning    (planner, plan-reviewer,                │
│  │               architecture-generator,                 │
│  │               requirement-generator, scenario-gen)   │
│  ├── Execution   (scenario-orchestrator,                 │
│  │               requirement-executor, execution-manager,│
│  │               rollup-reviewer, change-proposal-handler)│
│  └── Support     (plan-manager, project-manager,        │
│                   question-manager, etc.)                │
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

## What Happens When You Create a Plan

This is the complete message flow when you create a plan (via the UI or `POST /plan-api/plans`).

The planning pipeline is driven by **KV watches on the PLAN_STATES bucket**. Each component
watches for the status it owns and self-triggers — there is no coordinator orchestrating the
sequence. A write to PLAN_STATES is the trigger (the KV Twofer pattern).

### Step 1: Plan created

The user submits a plan description via the Web UI or REST API. The `plan-manager` creates a plan
record in PLAN_STATES with status `created`.

### Step 2: Planner drafts the plan

The `planner` component watches PLAN_STATES for status `created`. It assembles context via the
`PlanningStrategy`, calls the LLM, and writes the plan's goal, context, and scope back to
PLAN_STATES with status `drafted`.

```
PlanningStrategy (context-builder)
  Step 1: File tree             ← filesystem read, always fast
  Step 2: Codebase summary      ← graph query, timeout-guarded
  Step 3: Architecture docs     ← graph entities or filesystem fallback
  Step 4: Existing specs        ← graph query, timeout-guarded
  Step 5: Relevant code patterns← graph query, timeout-guarded
  Step 6: Requested files       ← filesystem, caller-specified
  Step 7: Planning SOPs         ← graph query, best-effort
```

The planner writes structured output (Goal, Context, Scope) to PLAN_STATES. Artifact files are
written by `workflow-documents` for git-friendliness, but KV is always authoritative.

### Step 3: Plan review

The `plan-reviewer` watches PLAN_STATES for status `drafted`. It assembles context via the
`PlanReviewStrategy`, which fetches SOPs with an all-or-nothing budget policy — the review never
proceeds without the applicable SOPs loaded.

```
plan-reviewer
  ├── Assembles context: PlanReviewStrategy
  │     Step 1: SOPs (all-or-nothing — fail if SOPs exceed budget)
  │     Step 2: Plan content
  │     Step 3: File tree
  ├── LLM: validates plan against each SOP requirement
  └── Writes verdict to PLAN_STATES:
        "reviewed"        → passes; ready for human or auto-approval
        "revision_needed" → violations found; planner retries
```

**Verdict: revision_needed** — The `planner` re-triggers from `revision_needed` status, generates
a revised plan incorporating the violation findings as LLM context, and sets status back to
`drafted`. This loop repeats up to three times.

**Verdict: reviewed** — If `auto_approve=true` is set, the plan-reviewer promotes directly to
`approved`. Otherwise, a human approves via the UI or API.

### Step 4: Architecture generation

The `architecture-generator` watches PLAN_STATES for status `approved`. It dispatches an
architect agent that produces technology decisions and component boundaries, then publishes
the result via `submit_work`. The `plan-manager` stores the architecture and sets status to
`generating_requirements`.

### Step 5: Requirement generation

The `requirement-generator` watches PLAN_STATES for the requirements trigger. It calls the LLM to
generate structured Requirements from the plan and publishes a `RequirementsGeneratedEvent`.
The `plan-manager` receives the event, stores the Requirements, and sets status to
`requirements_generated`.

### Step 6: Scenario generation

The `scenario-generator` watches for status `requirements_generated`. For each Requirement it
generates BDD Scenarios (Given/When/Then) and publishes events. The `plan-manager` accumulates
the Scenarios and sets status to `scenarios_generated` when all Requirements are covered.

### Step 7: Scenario review

The `plan-reviewer` watches for status `scenarios_generated` and performs a second review pass,
validating the Scenarios against SOPs and Requirements. On approval, status advances to
`scenarios_reviewed` and then to `ready_for_execution`.

### Full flow summary

```
User creates plan: "Add user authentication"
  │
  ▼
plan-manager: PLAN_STATES ← { status: "created" }
  │
  ▼ (planner watches status=created)
planner: PlanningStrategy → LLM → plan.json written
  │
  ▼
plan-manager: PLAN_STATES ← { status: "drafted" }
  │
  ▼ (plan-reviewer watches status=drafted)
plan-reviewer: PlanReviewStrategy (SOPs all-or-nothing + plan + file tree)
  │
  ├── revision_needed → PLAN_STATES ← { status: "revision_needed" }
  │     │
  │     └── planner retries (max 3)
  │
  └── reviewed → PLAN_STATES ← { status: "reviewed" }
        │
        ▼ (auto_approve=true OR human approves)
      PLAN_STATES ← { status: "approved" }
        │
        ▼ (architecture-generator watches status=approved)
      LLM → Architecture decisions published → plan-manager stores
        │
        ▼
      PLAN_STATES ← { status: "architecture_generated" }
        │
        ▼ (requirement-generator watches)
      LLM → Requirements published → plan-manager stores
        │
        ▼
      PLAN_STATES ← { status: "requirements_generated" }
        │
        ▼ (scenario-generator watches)
      LLM → Scenarios per Requirement → plan-manager accumulates
        │
        ▼
      PLAN_STATES ← { status: "scenarios_generated" }
        │
        ▼ (plan-reviewer second pass)
      PLAN_STATES ← { status: "ready_for_execution" }
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
│           ├── plan.md         ← Human-readable plan (generated at milestones)
│           └── plan.json       ← Goal, Context, Scope (JSON artifact)
└── ... your code ...
```

These files are git-friendly. Commit them to preserve context across sessions and team members.

## Component Groups

Semspec registers 16 components at startup alongside the full semstreams component suite.

```
┌──────────── Planning ────────────────────────────────────────────────┐
│  planner                Watches PLAN_STATES=created, drafts plan      │
│  plan-reviewer          Watches PLAN_STATES=drafted/scenarios_generated│
│                          validates against SOPs; sets reviewed or     │
│                          revision_needed; promotes to approved when   │
│                          auto_approve=true                            │
│  architecture-generator Watches PLAN_STATES=approved, generates       │
│                          technology decisions and component boundaries│
│  requirement-generator  Watches PLAN_STATES, generates structured     │
│                          Requirements via LLM                        │
│  scenario-generator     Generates BDD Scenarios from Requirements    │
└──────────────────────────────────────────────────────────────────────┘

┌──────────── Execution ───────────────────────────────────────────────┐
│  scenario-orchestrator  Dispatches requirement-execution-loop per    │
│                          pending Requirement                         │
│  requirement-executor   Decomposes Requirements into DAGs, serial    │
│                          node dispatch, and per-requirement review   │
│  execution-manager      TDD pipeline per DAG node:                  │
│                          developer → validator → reviewer            │
│  rollup-reviewer        QA rollup after all requirements complete;   │
│                          integration validation and cross-req testing│
│  change-proposal-handler  ChangeProposal OODA loop and cascade      │
└──────────────────────────────────────────────────────────────────────┘

┌──────────── Support ─────────────────────────────────────────────────┐
│  plan-manager         Requirement/Scenario/ChangeProposal HTTP API;  │
│                        owns PLAN_STATES writes and event handling    │
│  project-manager      Project management HTTP API                    │
│  workflow-validator   Document structure validation (request/reply)  │
│  workflow-documents   File output to .semspec/plans/                 │
│  structural-validator  Structural integrity checks                   │
│  question-manager     Question routing, SLA tracking, LLM answering  │
└──────────────────────────────────────────────────────────────────────┘
```

**Note**: `context-builder`, `task-dispatcher`, and other infrastructure components are
semstreams components, not registered by semspec directly. Source indexing (AST parsing,
git, docs) is handled by the external semsource service.

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

See [Project Setup](project-setup.md) for authoring standards, quality gates, and SOPs.

### Workflow entities (plans, tasks, sessions)

Each plan and task becomes a graph entity with predicates describing its status, content, and
relationships. Agents query these when assembling planning context via `graph_search`, so later
plans benefit from awareness of earlier decisions.

## LLM Configuration

See [Model Configuration](model-configuration.md) for the full capability-to-model mapping
and setup instructions.
