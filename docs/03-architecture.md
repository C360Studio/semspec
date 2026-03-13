# Semspec Architecture

> **New to semspec?** Read [How Semspec Works](01-how-it-works.md) first for a progressive introduction to the system.

Semspec is a **semstreams extension** - it imports semstreams as a library, registers custom components, and runs them
via the component lifecycle.

## System Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  DOCKER COMPOSE (infrastructure)                                             │
│                                                                              │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │  NATS JetStream (required)                                            │   │
│  │                                                                        │   │
│  │  Streams:    AGENT      WORKFLOWS     GRAPH      USER      SOURCES    │   │
│  │  KV Buckets: ENTITY_STATES  CONTEXT_RESPONSES  PLAN_SESSIONS          │   │
│  │              AGENT_LOOPS    WORKFLOW_EXECUTIONS  LLM_CALLS             │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
│  ┌─────────────────────────────────────────────────────┐                   │
│  │  Optional: Ollama (local LLM inference, port 11434)  │                   │
│  └─────────────────────────────────────────────────────┘                   │
└─────────────────────────────────────────────────────────────────────────────┘
                                     ▲
                                     │ NATS
                                     │
┌────────────────────────────────────┴────────────────────────────────────────┐
│  SEMSPEC BINARY  (cmd/semspec/main.go)                                       │
│                                                                              │
│  Startup sequence:                                                           │
│  ├── Global init imports (tools, LLM providers, vocabularies)               │
│  ├── Connect to NATS, ensure streams                                        │
│  ├── Register semstreams components (graph-*, agentic-*, workflow-*)        │
│  ├── Register 15 semspec components                                         │
│  └── Start service manager (HTTP :8080)                                     │
│                                                                              │
│  ┌──────────── Planning ────────────────────────────────────────────────┐   │
│  │  plan-coordinator   Parallel planner orchestration (PLAN_SESSIONS)   │   │
│  │  planner            Single-planner path; fallback or standalone       │   │
│  │  plan-reviewer      SOP-aware plan validation with LLM review         │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
│  ┌──────────── Context ─────────────────────────────────────────────────┐   │
│  │  context-builder    Strategy-based LLM context assembly               │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
│  ┌──────────── Sources ─────────────────────────────────────────────────┐   │
│  │  source-ingester    Document/SOP ingestion with frontmatter parsing   │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
│  ┌──────────── Execution ───────────────────────────────────────────────┐   │
│  │  task-generator     BDD task generation (static) OR advance to       │   │
│  │                     ready_for_execution (reactive_mode=true)         │   │
│  │  task-dispatcher    Dependency-aware task execution via agent loops   │   │
│  │  scenario-orchestrator  Dispatches scenario-execution-loop per       │   │
│  │                         pending Scenario (reactive mode only)        │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
│  ┌──────────── Indexing ────────────────────────────────────────────────┐   │
│  │  ast-indexer        Code entity extraction (Go, TS, JS, Python, Java)│   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
│  ┌──────────── Support ─────────────────────────────────────────────────┐   │
│  │  workflow-api       Workflow execution queries (HTTP)                 │   │
│  │  trajectory-api     LLM call history queries (HTTP)                   │   │
│  │  rdf-export         RDF serialization of graph entities               │   │
│  │  workflow-validator  Document structure validation (request/reply)    │   │
│  │  workflow-documents  File output to .semspec/plans/                   │   │
│  │  question-answerer  LLM question answering for knowledge gaps         │   │
│  │  question-timeout   SLA monitoring and escalation (disabled default)  │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Component Registration Pattern

All 15 semspec components are registered in `cmd/semspec/main.go` alongside the full semstreams component suite.
Tools, LLM providers, and vocabularies register themselves via package-level `init()` functions triggered by blank
imports.

```go
// cmd/semspec/main.go

// Global init imports — register before any component starts
import (
    _ "github.com/c360studio/semspec/tools"           // file, git, github, doc, workflow tools
    _ "github.com/c360studio/semspec/llm/providers"   // anthropic, ollama LLM providers
    _ "github.com/c360studio/semspec/vocabulary/source" // source.* predicate vocabulary
)

func run() {
    registry := component.NewRegistry()

    // All semstreams components: graph-*, agentic-*, workflow-processor, etc.
    componentregistry.Register(registry)

    // Semspec components (15 total)
    astindexer.Register(registry)
    rdfexport.Register(registry)
    workflowvalidator.Register(registry)
    workflowdocuments.Register(registry)
    questionanswerer.Register(registry)
    questiontimeout.Register(registry)
    sourceingester.Register(registry)
    taskgenerator.Register(registry)
    taskdispatcher.Register(registry)
    planner.Register(registry)
    contextbuilder.Register(registry)
    workflowapi.Register(registry)
    trajectoryapi.Register(registry)
    plancoordinator.Register(registry)
    planreviewer.Register(registry)
}
```

## Components vs Workflows

Semspec uses two complementary patterns for LLM-driven processing. Understanding when to use each is critical for
extending the system.

### Components: Single-Shot Processing

**Pattern**: Listen → Process → Persist → Publish

Components are standalone processors that subscribe to a trigger subject, process incoming messages (often with LLM
calls), persist results, and publish completion notifications.

```
┌──────────────────┐     ┌──────────────────┐     ┌──────────────────┐
│  workflow.        │     │    Component     │     │  workflow.        │
│  trigger.planner │────▶│  (planner)       │────▶│  result.planner   │
└──────────────────┘     │                  │     └──────────────────┘
                         │  1. Call LLM     │
                         │  2. Parse JSON   │     ┌──────────────────┐
                         │  3. Validate     │────▶│  plan.json        │
                         │  4. Save file    │     └──────────────────┘
                         └──────────────────┘
```

**Use components when:**

- Calling an LLM and parsing structured output (JSON from markdown-wrapped responses)
- Transforming data between formats
- Domain-specific file I/O (`plan.json`, `tasks.json`)
- Single input → single output operations

**Examples in semspec:**

| Component | Trigger Subject | Processing | Output |
|-----------|-----------------|------------|--------|
| `plan-coordinator` | `workflow.trigger.plan-coordinator` | Orchestrates parallel planners | Merged plan |
| `planner` | `workflow.trigger.planner` | LLM → Goal/Context/Scope | `plan.json` |
| `plan-reviewer` | `workflow.trigger.plan-reviewer` | SOP-aware LLM review | Review verdict |
| `task-generator` | `workflow.trigger.task-generator` | LLM → BDD tasks | `tasks.json` |
| `context-builder` | `context.build.>` | Strategy-based context assembly | Context payload |
| `workflow-validator` | `workflow.validate.*` | Parse markdown → validate | Validation result |

### Workflows: Multi-Step Orchestration

**Pattern**: Define steps in JSON → workflow-processor executes them

Workflows are state machines defined declaratively in JSON. They coordinate multiple agents with conditional routing,
retry logic, and failure handling.

```
┌─────────────┐     ┌─────────────┐     ┌─────────────────────┐
│  developer  │────▶│  reviewer   │────▶│  verdict_check      │
└─────────────┘     └─────────────┘     └──────────┬──────────┘
                                                   │
                    ┌──────────────────────────────┼──────────────────┐
                    │                              │                  │
                    ▼                              ▼                  ▼
            ┌───────────────┐            ┌───────────────┐    ┌───────────────┐
            │   approved    │            │  retry_dev    │    │   escalate    │
            │   (complete)  │            │  (loop back)  │    │   (to user)   │
            └───────────────┘            └───────────────┘    └───────────────┘
```

**Use workflows when:**

- Multiple agents need coordination (developer ↔ reviewer)
- Conditional routing based on step outputs
- Retry logic with feedback loops
- Complex failure handling across steps

**Example**: The `plan-and-execute` workflow implements an adversarial loop where the developer implements, the
reviewer evaluates, and the system routes based on verdict (`approved`, `fixable`, `misscoped`, `too_big`).

### Why Not Just Use Workflows?

**Q: Could planner/task-generator be workflow steps instead of components?**

**A: No.** `agentic-loop` (which workflow steps delegate to) returns **raw text only**. It cannot:

1. Extract JSON from markdown code blocks
2. Parse into typed Go structs
3. Validate domain-specific rules
4. Merge with existing data
5. Save to specific file formats

Components fill this gap. They handle the "last mile" processing that generic executors cannot.

### Decision Framework

```
Need to process LLM output?
├── YES: Need structured parsing?
│   ├── YES: Single-shot operation?
│   │   ├── YES → COMPONENT (processor/)
│   │   └── NO  → WORKFLOW with component steps
│   └── NO  → Use agentic-loop directly
└── NO: Multiple coordinated agents?
    ├── YES → WORKFLOW (configs/workflows/)
    └── NO  → Simple command handler
```

### Both Patterns Together

Workflows can trigger components when specialized processing is needed:

```json
{
  "name": "generate_plan",
  "action": {
    "type": "publish",
    "subject": "workflow.trigger.planner"
  }
}
```

The workflow handles orchestration; the component handles processing.

## Orchestrator State Model

Orchestrator components (review-orchestrator, execution-orchestrator, scenario-executor, plan-coordinator,
change-proposal-handler) manage execution state through two complementary mechanisms:

### Graph Triples (Durable)

Each execution is represented as a **Graphable entity** with a 6-part entity ID
(`local.semspec.workflow.<type>.execution.<instance>`) published to `graph.ingest.entity`. Entity triples
include both **property triples** (scalar values like phase, iteration, verdict) and **relationship triples**
(links to plan, task, scenario, and project entities via 6-part entity IDs as Object values).

Phase changes are also written to `ENTITY_STATES` KV via `graph.mutation.triple.add` request/reply, which
the rule processor watches to fire terminal state transitions (approved, escalated, error, completed, failed).

Entities are published at two points:
1. **Execution start** — initial entity with starting phase (e.g., "generating", "decomposing")
2. **Terminal state** — final entity with outcome phase, before in-memory cleanup

### In-Memory Cache (Ephemeral)

Each orchestrator maintains a `sync.Map` of active executions keyed by entity ID, plus a `taskIDIndex`
mapping agentic task IDs back to entity IDs for O(1) completion event routing. This cache holds operational
state needed between the start and terminal entity publishes (prompts, intermediate LLM outputs, task ID
correlations, timeout timers).

### Crash Recovery

On component restart, in-flight executions are lost from memory. The graph retains:
- The start entity (with initial phase and all trigger metadata)
- Any intermediate phase triples written to ENTITY_STATES
- Relationship triples linking the execution to its plan, task, or scenario

Recovery requires querying the graph for entities with non-terminal phases and re-triggering the workflow.
Terminal state entities always persist before cleanup, so completed/failed/escalated executions survive
restarts.

### Vocabulary

All workflow predicates are registered in `vocabulary/workflow/` following the 3-part
`domain.category.property` format. Property predicates use scalar Object values; relationship predicates
use `entity_id` data type where Object is a 6-part entity ID, creating a directed graph edge.

## Reactive Execution Architecture (ADR-025)

ADR-025 introduces a reactive execution model alongside the existing static model. The two modes
are selected via the `reactive_mode` flag on `task-generator`.

### Static vs Reactive Execution Paths

```mermaid
flowchart TD
    A[Plan approved] --> B[task-generator]
    B -->|reactive_mode=false| C[Generate Tasks\nWrite tasks.json]
    B -->|reactive_mode=true| D[Set status:\nready_for_execution]
    C --> E[task-dispatcher\nStatic dispatch]
    D --> F[scenario-orchestrator]
    F --> G[scenario-execution-loop\nper Scenario]
    G --> H[LLM: decompose_task\nProduces TaskDAG]
    H --> I[dag-execution-loop\nDispatch ready nodes]
    I --> J[Agent tasks run\nIn dependency order]
```

### Scenario Orchestrator

The `scenario-orchestrator` component is the entry point for reactive execution. It receives an
orchestration trigger (`scenario.orchestrate.<planSlug>`) listing pending or dirty Scenarios and
fires a `scenario-execution-loop` workflow trigger for each one, subject to `max_concurrent`.

```
scenario.orchestrate.<planSlug>
  │
  ▼
scenario-orchestrator
  ├── (concurrent, bounded by max_concurrent)
  ├── workflow.trigger.scenario-execution-loop → Scenario 1
  ├── workflow.trigger.scenario-execution-loop → Scenario 2
  └── workflow.trigger.scenario-execution-loop → Scenario N
```

The orchestrator is deliberately minimal: it dispatches then ACKs. All decomposition and execution
logic lives in the downstream reactive workflows.

### Agent Spawn Hierarchy

Agents in reactive mode can spawn child agents via the `spawn_agent` tool. Each spawn is recorded
in the knowledge graph using the `agentgraph` package, enabling tree queries at runtime.

```mermaid
flowchart TD
    O[Orchestrator loop] -->|spawn_agent| S1[Scenario executor loop]
    S1 -->|decompose_task| D[TaskDAG]
    D -->|dag-execution-loop| N1[Node A loop]
    D -->|dag-execution-loop| N2[Node B loop]
    N1 -->|spawn_agent| C1[Child loop]
    style O fill:#334,color:#fff
    style S1 fill:#334,color:#fff
    style N1 fill:#334,color:#fff
    style N2 fill:#334,color:#fff
    style C1 fill:#334,color:#fff
```

Spawn depth is capped at `maxDepth` (default: 5). The `query_agent_tree` tool lets any agent
inspect the hierarchy: its own children, the full subtree, or the status of a specific loop.

### New Tool Executors (Reactive Mode)

Four new tool executors are registered when reactive mode is in use:

| Tool | Package | Description |
|------|---------|-------------|
| `decompose_task` | `tools/decompose` | Validates a TaskDAG provided by the LLM and returns it |
| `spawn_agent` | `tools/spawn` | Publishes a child TaskMessage, waits for completion |
| `create_tool` | `tools/create` | Validates a FlowSpec defining a new tool (MVP: passthrough) |
| `query_agent_tree` | `tools/tree` | Queries agent hierarchy via `agentgraph.Helper` |

All four follow the `agentic.ToolExecutor` contract: validation errors return `ToolResult.Error`
(forwarded to the LLM as feedback); infrastructure errors return Go errors (logged by the
dispatcher as fatal).

### Agent Graph Vocabulary (`agentgraph` Package)

The `agentgraph` package stores agent hierarchy as graph triples using predicates from
`vocabulary/semspec/predicates.go`:

| Predicate | Direction | Meaning |
|-----------|-----------|---------|
| `agentic.loop.spawned` | parent loop → child loop | Records a spawn relationship |
| `agentic.loop.task` | loop → task entity | Loop owns this task |
| `agentic.task.depends_on` | task → prerequisite task | DAG dependency edge |
| `agentic.loop.role` | loop → string | Functional role of the loop |
| `agentic.loop.model` | loop → string | LLM model used by the loop |
| `agentic.loop.status` | loop → string | Current lifecycle status |

Entity IDs follow the 6-part format: `semspec.local.agentic.orchestrator.{type}.{instance}`.

### Cancellation Signals

When a ChangeProposal is accepted during reactive execution, running loops are cancelled via
`CancellationSignal` messages published to `agent.signal.cancel.<loopID>`. The affected
`scenario-execution-loop` or `dag-execution-loop` observes this signal and transitions to a
terminal failed state. The scenario-orchestrator re-queues affected Scenarios for fresh execution.

```
ChangeProposal accepted
  │
  ├── dirty cascade: mark affected Tasks/Scenarios as dirty
  └── publish CancellationSignal → agent.signal.cancel.<loopID>
                                           │
                                   dag-execution-loop
                                   (transitions to failed)
                                           │
                                   scenario-execution-loop
                                   (transitions to failed)
```

## Graph Node Hierarchy (ADR-024)

The knowledge graph stores all planning artifacts as typed nodes with directed edges. ADR-024
added Requirements, Scenarios, and ChangeProposals as first-class nodes.

```
Plan
  +-- Requirement(s)          (plan-scoped intent)
  |     +-- Scenario(s)       (Given/When/Then as graph entities)
  |           +-- Task(s)     (SATISFIES edge; many-to-many)
  |                 +-- Execution
  +-- Phase(s)                (organizational view; references Tasks)
  +-- ChangeProposal(s)       (lifecycle node; mutates Requirements on acceptance)
```

### Node Types

| Node | ID Format | Key Fields |
|------|-----------|-----------|
| Plan | `semspec.plan.{slug}` | status, goal, context, scope |
| Requirement | `requirement.{plan_slug}.{seq}` | title, description, status (active/deprecated/superseded) |
| Scenario | `scenario.{plan_slug}.{req_seq}.{seq}` | given, when, then[], status (pending/passing/failing/skipped) |
| Task | `semspec.plan.task.{slug}.{id}` | scenarioIDs[], status (includes `dirty`, `blocked`) |
| ChangeProposal | `change-proposal.{plan_slug}.{seq}` | affectedReqIDs[], status lifecycle |
| Phase | `semspec.plan.phase.{slug}.{id}` | task references (unchanged) |

### Node Edges

| Edge | From | To | Direction |
|------|------|----|-----------|
| `BELONGS_TO` | Requirement | Plan | Many-to-one |
| `HAS_SCENARIO` | Requirement | Scenario | One-to-many |
| `SATISFIED_BY` | Scenario | Task | Many-to-many |
| `VALIDATED_BY` | Scenario | Execution | One-to-many |
| `SUPERSEDED_BY` | Requirement | Requirement | Via ChangeProposal |
| `MUTATES` | ChangeProposal | Requirement | One-to-many |
| `INVALIDATES` | ChangeProposal | Task | Computed on acceptance |

### HTTP API Endpoints (ADR-024)

The `workflow-api` component exposes new endpoints for the three new node types:

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/plans/{slug}/requirements` | List requirements for a plan |
| `POST` | `/plans/{slug}/requirements` | Create a requirement |
| `GET` | `/plans/{slug}/requirements/{id}` | Get a single requirement |
| `GET` | `/plans/{slug}/scenarios` | List scenarios for a plan |
| `GET` | `/plans/{slug}/scenarios/{id}` | Get a single scenario |
| `GET` | `/plans/{slug}/change-proposals` | List change proposals |
| `POST` | `/plans/{slug}/change-proposals` | Submit a new ChangeProposal |
| `GET` | `/plans/{slug}/change-proposals/{id}` | Get a single proposal |
| `POST` | `/plans/{slug}/change-proposals/{id}/accept` | Accept a proposal (triggers cascade) |
| `POST` | `/plans/{slug}/change-proposals/{id}/reject` | Reject a proposal |

## Semstreams Relationship

Semspec **imports semstreams as a library** and extends it with custom components.

### What Semstreams Provides

| Package / Component | Purpose | How Semspec Uses It |
|---------------------|---------|---------------------|
| `component.Registry` | Component lifecycle management | Creates and manages all components |
| `componentregistry.Register()` | Registers all semstreams components | Gives access to graph, agentic, etc. |
| `natsclient` | NATS connection with circuit breaker | All NATS operations |
| `config.Loader` | Flow configuration loading | Loads `configs/semspec.json` |
| `config.StreamsManager` | JetStream stream management | Creates all streams |
| `pkg/errs` | Error classification | Retry decisions (Nak vs Term) |
| `agentic.ToolCall/ToolResult` | Tool message types | Tool execution protocol |
| `message.Triple` | Graph triple format | AST entity storage |
| `agentic-tools` | Tool dispatcher component | Executes registered tools |
| `workflow-processor` | Workflow state machine executor | Runs declarative workflows |

### Tool Registration

Semspec tools are registered globally via the `tools` package `init()` function—not via a dedicated component.
The semstreams `agentic-tools` component executes them:

```go
// tools/register.go
func init() {
    fileExec := NewRecordingExecutor(file.NewExecutor(absRepoRoot))
    gitExec  := NewRecordingExecutor(git.NewExecutor(absRepoRoot))
    // ...

    for _, tool := range fileExec.ListTools() {
        agentictools.RegisterTool(tool.Name, fileExec)
    }
}
```

`RecordingExecutor` wraps each executor to capture timing, parameters, and results in the `TOOL_CALLS` KV bucket,
enabling trajectory tracking via `trajectory-api`.

### Deployment Models

| Model | Components Running | Use Case |
|-------|-------------------|----------|
| **Minimal** | `ast-indexer` + semstreams `agentic-*` | Code indexing only |
| **With Semstreams** | All above + `graph-*` + `workflow-processor` + semspec processors | Full agentic planning |
| **Full Stack** | All above + `service-manager` + HTTP gateway + UI | Production deployment |

## Tool Dispatch Flow

Tools are registered globally and dispatched by the semstreams `agentic-tools` component. Semspec provides no
separate tool-executor component—the `tools` blank import wires everything at startup.

```
agentic-loop                    NATS                       agentic-tools
     │                            │                            │
     │ ──tool.execute.file_read──▶│──────────────────────────▶│
     │                            │                            │
     │                            │                  Execute(ctx, call)
     │                            │                  Record to TOOL_CALLS
     │                            │                            │
     │ ◀──tool.result.{call_id}───│◀─────────────────────────│
```

**Registered tool groups:**

| Package | Tools |
|---------|-------|
| `tools/file` | `file_read`, `file_write`, `file_list`, `file_delete` |
| `tools/git` | `git_status`, `git_branch`, `git_commit`, `git_diff` |
| `tools/github` | `github_pr_create`, `github_issue_create`, `github_pr_list` |
| `tools/doc` | `doc_list`, `doc_read` |
| `tools/workflow` | Registered via separate `init()` |
| `tools/decompose` | `decompose_task` — validates LLM-provided TaskDAG (reactive mode) |
| `tools/spawn` | `spawn_agent` — spawns and awaits a child agent loop (reactive mode) |
| `tools/create` | `create_tool` — validates a FlowSpec for dynamic tool creation (reactive mode) |
| `tools/tree` | `query_agent_tree` — queries agent hierarchy via agentgraph (reactive mode) |

## NATS Subject Patterns

All streams are created at startup by `config.StreamsManager`. The full subject space is:

| Subject | Stream | Direction | Purpose |
|---------|--------|-----------|---------|
| `tool.execute.<name>` | AGENT | Input | Tool execution requests |
| `tool.result.<call_id>` | AGENT | Output | Execution results |
| `tool.register.<name>` | Core NATS | Output | Tool advertisement (ephemeral) |
| `agent.task.development` | AGENT | Internal | Task execution by agentic-loop |
| `agent.task.question-answerer` | AGENT | Internal | Question answering tasks |
| `context.build.>` | AGENT | Input | Context build requests |
| `context.built.<request_id>` | AGENT | Output | Context build responses |
| `question.ask.>` | AGENT | Input | Knowledge gap questions |
| `question.answer.>` | AGENT | Output | Question answers |
| `question.timeout.>` | AGENT | Output | SLA timeout events |
| `question.escalate.>` | AGENT | Output | Escalation events |
| `graph.ingest.entity` | GRAPH | Output | Entities for graph storage |
| `graph.export.rdf` | GRAPH | Output | RDF serialized output |
| `workflow.trigger.plan-coordinator` | WORKFLOWS | Input | Parallel plan orchestration |
| `workflow.trigger.planner` | WORKFLOWS | Input | Single-planner path |
| `workflow.trigger.plan-reviewer` | WORKFLOWS | Input | Plan review |
| `workflow.trigger.task-generator` | WORKFLOWS | Input | Task generation |
| `workflow.trigger.task-dispatcher` | WORKFLOWS | Input | Task dispatch |
| `workflow.trigger.change-proposal-loop` | WORKFLOWS | Input | ChangeProposal OODA loop |
| `workflow.result.<component>.<slug>` | WORKFLOWS | Output | Component completion signals |
| `workflow.validate.*` | WORKFLOWS | Input | Document validation |
| `output.workflow.documents` | WORKFLOWS | Input | Document export |
| `requirement.created` | WORKFLOWS | Output | New requirement published |
| `requirement.updated` | WORKFLOWS | Output | Requirement mutated by ChangeProposal |
| `scenario.created` | WORKFLOWS | Output | New scenario published |
| `scenario.status.updated` | WORKFLOWS | Output | Scenario status changed |
| `task.dirty` | WORKFLOWS | Output | Dirty cascade: affected task IDs |
| `change_proposal.created` | WORKFLOWS | Output | New ChangeProposal submitted |
| `change_proposal.accepted` | WORKFLOWS | Output | Proposal accepted; cascade complete |
| `change_proposal.rejected` | WORKFLOWS | Output | Proposal rejected; no graph mutations |
| `source.ingest.>` | SOURCES | Input | Document/SOP ingestion |
| `source.status.>` | SOURCES | Output | Ingestion status |
| `user.message.>` | USER | Input | User messages (agentic-dispatch) |
| `scenario.orchestrate.*` | WORKFLOW | Input | Scenario orchestration trigger (per plan slug) |
| `workflow.trigger.scenario-execution-loop` | WORKFLOW | Input | Per-Scenario execution trigger |
| `workflow.trigger.dag-execution` | WORKFLOW | Input | DAG execution trigger |
| `workflow.async.scenario-decomposer` | WORKFLOW | Internal | Decompose request to agentic loop |
| `scenario.decomposed.*` | WORKFLOW | Internal | Decompose result (per scenario ID) |
| `dag.node.complete.*` | WORKFLOW | Internal | Single DAG node completed |
| `dag.node.failed.*` | WORKFLOW | Internal | Single DAG node failed |
| `dag.execution.complete.*` | WORKFLOW | Output | Entire DAG completed successfully |
| `dag.execution.failed.*` | WORKFLOW | Output | DAG failed (at least one node failed) |
| `scenario.complete.*` | WORKFLOW | Output | Scenario execution completed |
| `scenario.failed.*` | WORKFLOW | Output | Scenario execution failed |
| `agent.signal.cancel.*` | Core NATS | Input | Cancellation signal to a running loop |

**JetStream subjects** are durable and replay-capable. **Core NATS subjects** (`tool.register.*`) are ephemeral
request/reply with no persistence.

## Provenance Flow

Tool executors emit PROV-O triples to enable "who changed what when" queries:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  PROVENANCE FLOW: Tool Execution → Graph                                     │
│                                                                              │
│  1. USER REQUEST                                                            │
│     User → agentic-loop (via /message HTTP or CLI)                         │
│             │                                                               │
│             │ prov:wasAssociatedWith                                        │
│             ▼                                                               │
│  2. LOOP CREATES TOOL CALL                                                  │
│     Loop → tool.execute.file_write                                          │
│             │                                                               │
│             │ agent.activity.loop                                           │
│             ▼                                                               │
│  3. TOOL EXECUTOR RUNS                                                      │
│     agentic-tools executes file_write via RecordingExecutor                 │
│             │                                                               │
│             │ Emits provenance triples:                                     │
│             │ • prov.generation.activity → tool_call_id                    │
│             │ • prov.attribution.agent   → loop_id                         │
│             │ • prov.time.generated      → timestamp                       │
│             ▼                                                               │
│  4. GRAPH STORES PROVENANCE                                                 │
│     graph-ingest receives triples                                           │
│     graph-index makes queryable                                             │
│             │                                                               │
│             ▼                                                               │
│  5. QUERY PROVENANCE                                                        │
│     "What files did loop X create?"                                        │
│     "Who modified auth.go since Tuesday?"                                  │
│     "Show audit trail for this proposal"                                   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Provenance Predicates

| Predicate | Type | Direction | Usage |
|-----------|------|-----------|-------|
| `prov.generation.activity` | entity → tool_call | Output | File was generated by this tool call |
| `prov.attribution.agent` | entity → loop | Output | Entity attributed to this loop |
| `prov.usage.entity` | tool_call → entity | Input | Tool call used this entity as input |
| `prov.time.generated` | entity → datetime | Metadata | When entity was created |
| `prov.time.started` | activity → datetime | Metadata | When activity started |
| `prov.time.ended` | activity → datetime | Metadata | When activity ended |

## Related Documentation

| Document | Description |
|----------|-------------|
| [How Semspec Works](01-how-it-works.md) | Progressive introduction to the system |
| [Getting Started](02-getting-started.md) | Quick start guide |
| [Components](04-components.md) | Component configuration and creation guide |
| [Workflow System](05-workflow-system.md) | LLM-driven workflows, model selection, validation |
| [Question Routing](06-question-routing.md) | Knowledge gap resolution, SLA, escalation |
