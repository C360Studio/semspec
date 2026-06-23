# How Semspec Works

This document explains what happens when you use semspec, from infrastructure to command execution.
Start here before reading the architecture or component guides.

For the comprehensive happy-path, retry-path, and SemTeams starter-spec view, see
[SemSpec End-to-End Flow](e2e-flow.md).

## What is Semspec?

Semspec is a spec-driven development agent with a **persistent knowledge graph**. It helps you:

- Create structured plans, designs, and specifications
- Track code entities (functions, types, packages) across your codebase
- Enforce project standards during planning and code review
- Query accumulated context that persists across sessions

**The key insight**: Traditional AI coding assistants lose context between sessions. Semspec stores
everything in a knowledge graph, so your AI assistant remembers your codebase, your plans, and your
team's coding standards.

## Execution Model

Planning produces Requirements, Architecture, Stories, and Scenarios. Runtime execution then
dispatches Requirements while implementation work happens through Story-owned task DAGs:

```
Plan approved -> Requirements -> Architecture -> Stories -> Scenarios -> ready_for_execution
                                                                       |
                                                               scenario-orchestrator
                                                                       |
                                              +------------------------+------------------------+
                                              v                        v                        v
                                      Requirement 1             Requirement 2             Requirement N
                                              |
                                      Story task DAG
                                              |
                                developer -> validator -> reviewer
                                              |
                                      Story scenario review
```

Task synthesis happens at execution time from Stories because the best node sequence depends on
the accepted Story/file ownership surface and the current requirement branch. The executor does
not invent new scope during implementation; it works the Story boundaries created during planning.

### Contract Authority And Brownfield Topology

Planning artifacts are not the authority by themselves. When a Plan is created, `plan-manager`
creates a stable contract packet that captures the original brief, constraints, scope snapshot,
topology facts, and later accepted amendments. BMAD/OpenSpec prompts receive role-specific
projections of that same packet so planner, architect, story, developer, reviewer, recovery, and
QA loops carry the same non-negotiable context.

The root contract is immutable. Material changes are amendments attached through PlanDecisions
with contract-impact evidence. Scope shrinkage is rejected unless the dropped obligation has
accepted amendment provenance, and whole-phase resets require evidence that the whole phase is
invalid. Recovery should dirty the smallest correct requirement/story/scenario closure instead of
discarding unrelated work.

Brownfield topology is part of that contract. Build roots, package manifests, workspace files,
module boundaries, and standalone-project markers are treated as topology-controlled paths. A
developer can extend the baseline, but cannot quietly replace it with a clean-room project shape.

### Agent Tool Set

Semspec takes a bash-first approach: all file, git, and shell operations go
through `bash`. Specialized tools exist only for things bash cannot do.
Registration happens once at startup in `tools/register.go`.

**Always registered (semspec runs with NATS):**

| Tool | Description |
|------|-------------|
| `bash` | Universal shell — files, git, builds, tests, any shell command |
| `submit_work` | Signal task completion with structured deliverable (terminal: StopLoop=true) |
| `decompose_task` | Decompose a goal into a validated TaskDAG; loop exits with DAG as result |
| `http_request` | Fetch URLs; persists content to the graph as `source.web.*` entities |
| `ask_question` | Signal a blocker requiring human or agent answer (terminal: StopLoop=true) |
| `answer_question` | Provide an answer to a pending question |
| `graph_search` | Graph gateway query — synthesized answer from `globalSearch` |
| `graph_query` | Graph gateway query — raw GraphQL for entity lookup |
| `graph_summary` | Graph gateway query — knowledge graph overview (call once first) |

**Conditional:**

| Tool | Condition |
|------|-----------|
| `web_search` | `BRAVE_SEARCH_API_KEY` set |

### PlanDecision Cancellation

When a PlanDecision is accepted during reactive execution, running scenario loops are cancelled
via `CancellationSignal` messages on `agent.signal.cancel.<loopID>`. Affected Scenarios are
re-queued for fresh execution with the updated behavioral contracts.

The current execution path is component-owned: `scenario-orchestrator` dispatches ready
requirements, `requirement-executor` synthesizes Story task DAGs, and `execution-manager` owns the
TDD task pipeline.

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
│  │               qa-reviewer, plan-decision-handler)    │
│  └── Support     (plan-manager, project-manager,        │
│                   question-manager, etc.)                │
└─────────────────────────────────────────────────────────┘
```

**What this means for you**:

1. Semspec imports semstreams as a Go library
2. Docker Compose runs the shared infrastructure (NATS, optional Ollama)
3. The semspec binary registers and runs 22 semspec-specific components

## What You Need Running

With Docker Compose (recommended):

| Container | Purpose |
|-----------|---------|
| **nats** | JetStream message bus for all inter-component communication |
| **semspec** | All 22 semspec components + semstreams infrastructure |
| **sandbox** | Isolated code execution environment for agents; also runs `qa_level=unit` tests |
| **semsource** | Source code indexing (AST, git, docs) → knowledge graph |
| **ui** | SvelteKit frontend (SSR) |
| **gateway** | Caddy reverse proxy — routes API to semspec, UI to frontend |

```bash
# Start the full stack
docker compose up -d

# Open http://localhost:8080
```

An LLM provider is also required — either Ollama running on the host or a
cloud API key set in `.env`. See the [Quick Start](../README.md#quick-start).

For development (building from source):

```bash
# Build semspec locally, start full stack via Docker
task local:up
```

## What Happens When You Create a Plan

This is the complete message flow when you create a plan (via the UI or `POST /plan-api/plans`).

The planning pipeline is driven by **KV watches on the PLAN_STATES bucket**. Each component
watches for the status it owns and self-triggers — there is no coordinator orchestrating the
sequence. A write to PLAN_STATES is the trigger (the KV Twofer pattern).

### Step 1: Plan created

The user submits a plan description via the Web UI or REST API. The `plan-manager` creates a plan
record in PLAN_STATES with status `created` and initializes the Plan contract packet. The packet
keeps the original brief, initial scope, constraints, topology facts, and amendment ledger tied to
one stable contract ID.

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
  Step 7: Project standards     ← from .semspec/standards.json
```

The planner writes structured output (Goal, Context, Scope) to PLAN_STATES. Artifact files are
written by `workflow-documents` for git-friendliness, but KV is always authoritative.

### Step 3: Plan review

The `plan-reviewer` watches PLAN_STATES for status `drafted`. It assembles context including
the plan content, project standards, and file tree, then validates via LLM.

```
plan-reviewer
  ├── Assembles context: PlanReviewStrategy
  │     Step 1: Project standards
  │     Step 2: Plan content
  │     Step 3: File tree
  ├── LLM: validates plan against standards
  └── Writes verdict to PLAN_STATES:
        "reviewed"        → passes; ready for human or auto-approval
        "revision_needed" → violations found; planner retries
```

**Verdict: revision_needed** — The `planner` re-triggers from `revision_needed` status, generates
a revised plan incorporating the violation findings as LLM context, and sets status back to
`drafted`. This loop repeats up to three times.

**Verdict: reviewed** — If `auto_approve=true` is set, the plan-reviewer promotes directly to
`approved`. Otherwise, a human approves via the UI or API.

### Step 4: Requirement generation

The `requirement-generator` watches PLAN_STATES for approved plans. It calls the LLM to generate
structured Requirements from the plan and the contract packet, then publishes a
`RequirementsGeneratedEvent`. The `plan-manager` validates dependency shape, capability coverage,
and scope continuity before setting status to `requirements_generated`.

### Step 5: Architecture generation

The `architecture-generator` watches PLAN_STATES for status `requirements_generated`. It dispatches
an architect agent that produces technology decisions, component boundaries, implementation files,
and topology-preserving guidance, then publishes the result via `submit_work`. The `plan-manager`
stores the architecture and sets status to `architecture_generated`.

### Step 6: Story preparation

The `story-preparer` watches for status `architecture_generated`. It maps Requirements and
Architecture into Stories with file ownership, dependencies, and scope create/include obligations.
The `plan-manager` validates coverage, cycles, ownership, and contract continuity before setting
status to `stories_generated`.

### Step 7: Scenario generation

The `scenario-generator` watches for status `stories_generated`. For each Story it generates
BDD Scenarios (Given/When/Then) and publishes events. The `plan-manager` accumulates the Scenarios
and sets status to `scenarios_generated` when all executable Stories are covered.

### Step 8: Scenario and Story review

The `plan-reviewer` watches for status `scenarios_generated` and performs this review pass (R2),
validating the Scenarios, Stories, requirements, architecture, and contract obligations. On
approval, status advances to `ready_for_execution`.

The plan-reviewer runs four mandatory rounds (ADR-051): R1 (draft, above), R-req at
`requirements_generated`, R-arch at `architecture_generated`, and R2 here. The two per-phase
rounds catch judgment-class defects (over-bundling, missing acceptance criteria, facade
boundaries, upstream-resolution gaps) immediately after each artifact is generated and re-run only
the offending phase, instead of deferring them to R2 or execution.

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
plan-reviewer: PlanReviewStrategy (standards + plan + file tree)
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
        ▼ (requirement-generator watches)
      LLM → Requirements published → plan-manager stores + validates contract coverage
        │
        ▼
      PLAN_STATES ← { status: "requirements_generated" }
        │
        ▼ (architecture-generator watches)
      LLM → Architecture decisions + topology guidance published → plan-manager stores
        │
        ▼
      PLAN_STATES ← { status: "architecture_generated" }
        │
        ▼ (story-preparer watches)
      LLM → Stories, file ownership, dependency schedule → plan-manager stores
        │
        ▼
      PLAN_STATES ← { status: "stories_generated" }
        │
        ▼ (scenario-generator watches)
      LLM → Scenarios per Story → plan-manager accumulates
        │
        ▼
      PLAN_STATES ← { status: "scenarios_generated" }
        │
        ▼ (plan-reviewer scenario/story/contract pass)
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
│   ├── project.json            ← Project metadata
│   ├── standards.json          ← Agent rules
│   ├── checklist.json          ← Quality gates
│   └── plans/
│       └── add-user-authentication/
│           ├── plan.md         ← Human-readable plan (generated at milestones)
│           └── plan.json       ← Goal, Context, Scope (JSON artifact)
└── ... your code ...
```

These files are git-friendly. Commit them to preserve context across sessions and team members.

## Component Groups

Semspec registers its project components at startup alongside the full semstreams component suite.

```
┌──────────── Planning ────────────────────────────────────────────────┐
│  planner                Watches PLAN_STATES=created, drafts plan      │
│  plan-reviewer          Watches PLAN_STATES=drafted/scenarios_generated│
│                          validates against standards; sets reviewed or│
│                          revision_needed; promotes to approved when   │
│                          auto_approve=true                            │
│  requirement-generator  Watches approved/changed plans, generates     │
│                          structured Requirements via LLM              │
│  architecture-generator Watches requirements_generated plans, creates │
│                          technology decisions and component boundaries│
│  story-preparer         Watches architecture_generated plans, creates │
│                          Stories and dependency scheduling            │
│  scenario-generator     Generates Story-scoped Scenarios              │
└──────────────────────────────────────────────────────────────────────┘

┌──────────── Execution ───────────────────────────────────────────────┐
│  scenario-orchestrator  Dispatches ready Requirements by dependency  │
│                          and Story availability                      │
│  requirement-executor   Synthesizes Story task DAGs, handles serial  │
│                          node dispatch, and Story review             │
│  execution-manager      TDD pipeline per DAG node:                  │
│                          developer → validator → reviewer            │
│  qa-reviewer            Release-readiness verdict (Murat persona);   │
│                          scoped by qa_level                           │
│                          (synthesis/unit/integration/full)            │
│                          unit/integration use sandbox QA; full/e2e    │
│                          remains operator CI via emitted qa.yml        │
│  plan-decision-handler  PlanDecision OODA loop and cascade           │
│  recovery-agent         Wedge recovery — manager-role agents that    │
│                          unstick stuck loops via trajectory analysis │
└──────────────────────────────────────────────────────────────────────┘

┌──────────── Support ─────────────────────────────────────────────────┐
│  plan-manager         Requirement/Scenario/PlanDecision HTTP API;    │
│                        owns PLAN_STATES writes, contract packets,    │
│                        phase summaries, and event handling           │
│  project-manager      Project management HTTP API                    │
│  workflow-validator   Document structure validation (request/reply)  │
│  workflow-documents   File output to .semspec/plans/                 │
│  structural-validator  Structural, ownership, and topology checks    │
│  question-manager     Question routing, SLA tracking, LLM answering  │
│  lesson-decomposer    Splits reviewer rejections into atomic triples │
│                        for the role-scoped lessons pipeline          │
│  lesson-curator       Rotates and retires lessons by recency and     │
│                        observed efficacy                             │
└──────────────────────────────────────────────────────────────────────┘

┌──────────── Optional integrations ───────────────────────────────────┐
│  researcher-manager   Spawns research sub-loops on demand            │
│  github-watcher       Mirrors GitHub issues into plans               │
│  github-submitter     Pushes plan outcomes back to GitHub PRs        │
└──────────────────────────────────────────────────────────────────────┘
```

The Optional integrations are registered but not wired into `configs/semspec.json`
by default — enable per-deployment by adding their instance config. The other 19
components are configured out of the box.

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

### Workflow entities (plans, tasks, sessions)

Each plan and task becomes a graph entity with predicates describing its status, content, and
relationships. Agents query these when assembling planning context via `graph_search`, so later
plans benefit from awareness of earlier decisions.

## Trajectory Capture & LLM Audit Trail

Every agent loop produces a **trajectory** — an ordered timeline of steps the loop took:
the LLM request, tool calls, tool results, and the next LLM request that built on them.
Trajectories are how the UI shows agent activity in real time and how diagnostic bundles
preserve forensic detail after the fact.

### `trajectory_detail` levels

The `agentic-loop` component supports two capture levels:

| Level | What's captured per step | Storage cost | Use case |
|-------|--------------------------|--------------|----------|
| `summary` *(default)* | Step type, model, timestamps, token counts, tool-call summaries | Low | Production — keeps the NATS object store lean |
| `full` | Everything above plus the complete `messages` array (system + user + assistant + tool messages) | Moderate (~50–150 KB per loop) | Demos, diagnostic bundles, audit-trail-driven reviews |

```json
"agentic-loop": {
  "config": {
    "trajectory_detail": "full",
    ...
  }
}
```

The e2e configs (`e2e-gemini.json`, `e2e-claude.json`, `e2e-hybrid.json`,
`e2e-openrouter.json`, `e2e-sparky.json`, `e2e-local.json`, `e2e-mock-ui.json`) all set
`"full"` so demos and operator runs surface the request side. Production `semspec.json`
and the backend-only `e2e-mock.json` stay at `"summary"`.

### Where the data lives

| Bucket | Stores | Notes |
|--------|--------|-------|
| `KV_AGENT_LOOPS` | Loop metadata (status, role, task ref) | Always populated |
| `OBJ_AGENT_CONTENT` | Trajectory step JSON blobs | Object store — sized for the full `messages` payload when `trajectory_detail: "full"` |
| `KV_AGENT_CONTENT` | (legacy / unused at present) | Stays empty — don't grep here for trajectory content |

### How the UI surfaces trajectories

- **Plan phase summary** (`phase_summary` on plan API responses) — authoritative current phase,
  active-loop count, execution counts, waits, recovery, QA, lesson activity, and freshness. The
  banner and detail panels use this instead of inferring current state from feed rows.
- **Activity feed** (`/agentic-dispatch/activity` SSE) — streams `loop_created`,
  `loop_updated`, `loop_deleted`, stale/disconnected, orphaned execution, and lesson activity
  events so the live feed updates without polling.
- **Execution timeline** — groups loops into Planning vs Execution stages from plan phase state
  and ghost-renders expected stages before any loop exists.
- **Recovery and lesson details** — show PlanDecision status, affected nodes, contract impact,
  whether the system is waiting for action, and whether lesson work can affect the current run or
  only future prompts.
- **Cost evidence** — token and cost displays use measured usage plus provider-rate metadata; when
  rate data is unavailable, the UI marks the estimate instead of presenting false precision.
- **TrajectoryEntryCard** — clicking a loop expands the per-step view. When the loop
  was captured at `trajectory_detail: "full"`, the card renders the **Request** side
  (one row per message with role chip + scrollable content) alongside the **Response**
  side (assistant text + tool calls). This is the audit trail.

### Diagnostic bundles

`semspec watch --bundle` (see [Diagnostic Bundles](diagnostic-bundles.md)) extracts
trajectories from `OBJ_AGENT_CONTENT` into a tarball — one JSON per loop — so you can
ship a forensic snapshot to a reviewer or replay it locally with `jq`.

For wallclock and loop-count expectations across `easy` / `medium` / `hard` tiers,
see [Real-LLM Expectations](real-llm-expectations.md).

## LLM Configuration

See [Model Configuration](model-configuration.md) for the full capability-to-model mapping
and setup instructions.
