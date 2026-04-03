# Execution Pipeline

Reference for the full semspec execution pipeline — from plan creation through TDD task completion.

## Pipeline Overview

```
┌─────────────────────────────── PLAN PHASE ──────────────────────────────────┐
│                                                                               │
│  /plan <title>                                                                │
│       │                                                                       │
│       ▼                                                                       │
│  plan-manager (status: created → PLAN_STATES KV)                             │
│       │                                                                       │
│       ▼  (KV watch)                                                           │
│  planner ──► Goal/Context/Scope (status: drafted)                             │
│       │                                                                       │
│       ▼  (KV watch)                                                           │
│  plan-reviewer ──► approved / needs_changes                                  │
│       │  (retry up to 3× with findings in context)                            │
│       │                                                                       │
│       ▼  (approved → status: reviewed)                                        │
│  requirement-generator ──► RequirementsGeneratedEvent                         │
│       │  (status: requirements_generated)                                     │
│       │                                                                       │
│       ▼  (KV watch)                                                           │
│  scenario-generator ──► ScenariosGeneratedEvent                               │
│       │  (status: scenarios_generated)                                        │
│       │                                                                       │
│       ▼                                                                       │
│  status: ready_for_execution                                                  │
│                                                                               │
└───────────────────────────────────────────────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────── EXECUTION TRIGGER ───────────────────────────────────┐
│                                                                               │
│  /execute <slug>  OR  auto_approve=true                                       │
│       │                                                                       │
│       ▼                                                                       │
│  plan-manager ──► scenario.orchestrate.<requirementID>                        │
│                     │                                                         │
│                     ▼                                                         │
│         scenario-orchestrator ──► workflow.trigger.requirement-execution-loop │
│                                                                               │
└───────────────────────────────────────────────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────── DECOMPOSITION PHASE ─────────────────────────────────┐
│                                                                               │
│  requirement-executor (per Requirement)                                       │
│       │                                                                       │
│       ├──► agent.task.development ──► agentic-loop (decomposer)              │
│       │         calls decompose_task tool → TaskDAG                          │
│       │         loop completes ──► agent.complete.<loopID>                   │
│       │                                                                       │
│       └──► workflow.trigger.task-execution-loop (per DAG node, ordered)      │
│                                                                               │
└───────────────────────────────────────────────────────────────────────────────┘
                                 │
                                 ▼
┌──────────────────────────── TDD PIPELINE ───────────────────────────────────┐
│                                                                               │
│  execution-manager (per task node)                                            │
│       │                                                                       │
│       ├──► agent.task.testing ──► agentic-loop (tester)                      │
│       │         writes failing tests                                          │
│       │                                                                       │
│       ├──► agent.task.building ──► agentic-loop (builder)                    │
│       │         implements to pass tests                                      │
│       │                                                                       │
│       ├──► agent.task.validation ──► agentic-loop (validator)                │
│       │         structural validation (linting, type checks, conventions)     │
│       │                                                                       │
│       └──► agent.task.reviewer ──► agentic-loop (reviewer)                   │
│                 code review                                                   │
│                 verdict: approved / fixable / misscoped / too_big            │
│                                                                               │
└───────────────────────────────────────────────────────────────────────────────┘
                                 │
                                 ▼ (all DAG nodes complete)
┌──────────────────────── SCENARIO-LEVEL REVIEW ──────────────────────────────┐
│                                                                               │
│  requirement-executor (post-DAG)                                              │
│       │                                                                       │
│       └──► agent.task.scenario-reviewer ──► agentic-loop (requirement-reviewer)│
│                 reviews full requirement changeset + per-scenario verdicts    │
│                 verdict: approved / needs_changes / escalate                  │
│                 publishes: workflow.events.scenario.execution_complete        │
│                                                                               │
└───────────────────────────────────────────────────────────────────────────────┘
                                 │
                                 ▼ (all requirements complete)
┌─────────────────────── PLAN ROLLUP REVIEW ──────────────────────────────────┐
│                                                                               │
│  plan-manager (post-execution)                                                │
│       │                                                                       │
│       ▼                                                                       │
│  status: reviewing_rollup                                                     │
│       │                                                                       │
│       └──► workflow.trigger.plan-rollup-review                                │
│                 rollup-reviewer sees all requirement outcomes + changesets     │
│                 produces summary + overall verdict                            │
│                 verdict: approved / needs_attention                           │
│                 status on approved: complete                                  │
│                                                                               │
└───────────────────────────────────────────────────────────────────────────────┘
```

### Human Review Points

Between plan approval and `/execute`, humans can review, edit, or delete the generated
Requirements and Scenarios via the REST API. This is the primary quality gate before execution
commits resources. When `auto_approve` is enabled, the pipeline skips this gate and flows
directly to execution. See [Plan API Reference](12-plan-api.md) for the full endpoint reference.

## NATS Subject Reference

### Plan Phase

The plan phase is driven by `PLAN_STATES` KV bucket watches. Each component reacts to a specific
status value; there is no coordinator. NATS subjects carry the trigger payloads between
components.

| Subject | Stream | Publisher → Subscriber | Payload | Consumer |
|---------|--------|----------------------|---------|----------|
| `workflow.async.planner` | WORKFLOWS | plan-manager → planner | `TriggerPayload` | `planner` |
| `workflow.async.plan-reviewer` | WORKFLOWS | planner → plan-reviewer | `TriggerPayload` | `plan-reviewer` |
| `workflow.async.requirement-generator` | WORKFLOWS | plan-reviewer → requirement-generator | `TriggerPayload` | `requirement-generator` |
| `workflow.async.scenario-generator` | WORKFLOWS | requirement-generator → scenario-generator | `TriggerPayload` | `scenario-generator` |
| `workflow.events.requirements.generated` | WORKFLOWS | requirement-generator → plan-manager | `RequirementsGeneratedEvent` | `plan-manager-reqs-generated` |
| `workflow.events.scenarios.generated` | WORKFLOWS | scenario-generator → plan-manager | `ScenariosGeneratedEvent` | `plan-manager-scenarios-generated` |

### Execution Trigger Phase

| Subject | Stream | Publisher → Subscriber | Payload | Consumer |
|---------|--------|----------------------|---------|----------|
| `scenario.orchestrate.*` | WORKFLOWS | plan-manager → scenario-orchestrator | `ScenarioOrchestrationTrigger` (BaseMessage) | `scenario-orchestrator` |
| `workflow.trigger.requirement-execution-loop` | WORKFLOWS | scenario-orchestrator → requirement-executor | `RequirementExecutionRequest` (BaseMessage) | `requirement-executor-trigger` |

### Decomposition Phase

| Subject | Stream | Publisher → Subscriber | Payload | Consumer |
|---------|--------|----------------------|---------|----------|
| `agent.task.development` | AGENT | requirement-executor → agentic-loop (decomposer) | `TaskMessage` | — |
| `agent.complete.>` | AGENT | agentic-loop → requirement-executor | `LoopCompletedEvent` | `requirement-executor-loop-completions` |
| `workflow.trigger.task-execution-loop` | WORKFLOWS | requirement-executor → execution-manager | `TriggerPayload` (BaseMessage) | `execution-orchestrator-execution-trigger` |

### TDD Pipeline Phase

| Subject | Stream | Publisher → Subscriber | Payload | Consumer |
|---------|--------|----------------------|---------|----------|
| `agent.task.testing` | AGENT | execution-manager → agentic-loop (tester) | `TaskMessage` | — |
| `agent.task.building` | AGENT | execution-manager → agentic-loop (builder) | `TaskMessage` | — |
| `agent.task.validation` | AGENT | execution-manager → agentic-loop (validator) | `TaskMessage` | — |
| `agent.task.reviewer` | AGENT | execution-manager → agentic-loop (reviewer) | `TaskMessage` | — |
| `agent.complete.>` | AGENT | agentic-loop → execution-manager | `LoopCompletedEvent` | `execution-orchestrator-loop-completions` |

### Scenario-Level Review Phase

| Subject | Stream | Publisher → Subscriber | Payload | Consumer |
|---------|--------|----------------------|---------|----------|
| `agent.task.scenario-reviewer` | AGENT | requirement-executor → agentic-loop (requirement-reviewer) | `TaskMessage` | — |
| `workflow.events.scenario.execution_complete` | WORKFLOWS | requirement-executor → plan-manager | `ScenarioExecutionCompleteEvent` | `plan-api-events` |
| `agent.complete.>` | AGENT | agentic-loop → requirement-executor | `LoopCompletedEvent` | `requirement-executor-loop-completions` |

### Plan Rollup Review Phase

| Subject | Stream | Publisher → Subscriber | Payload | Consumer |
|---------|--------|----------------------|---------|----------|
| `workflow.trigger.plan-rollup-review` | WORKFLOWS | plan-manager → rollup-reviewer | `TriggerPayload` | `plan-rollup-reviewer` |
| `agent.complete.>` | AGENT | agentic-loop → plan-manager | `LoopCompletedEvent` | `plan-api-rollup-completions` |

## Consumer Names

All orchestrators use named JetStream consumers via `ConsumeStreamWithConfig`. Each is registered in
the component's `consumerInfos` slice and stopped cleanly in `Stop()`.

| Component | Consumer Name | Subject Filter | Purpose |
|-----------|--------------|----------------|---------|
| planner | `planner` | `workflow.async.planner` | Inbound plan generation triggers |
| plan-reviewer | `plan-reviewer` | `workflow.async.plan-reviewer` | Inbound plan review triggers |
| requirement-generator | `requirement-generator` | `workflow.async.requirement-generator` | Inbound requirement generation triggers |
| scenario-generator | `scenario-generator` | `workflow.async.scenario-generator` | Inbound scenario generation triggers |
| plan-manager | `plan-manager-reqs-generated` | `workflow.events.requirements.generated` | Requirements ready signal |
| plan-manager | `plan-manager-scenarios-generated` | `workflow.events.scenarios.generated` | Scenarios ready signal |
| scenario-orchestrator | `scenario-orchestrator` | `scenario.orchestrate.*` | Requirement dispatch triggers (Fetch pattern) |
| requirement-executor | `requirement-executor-trigger` | `workflow.trigger.requirement-execution-loop` | Per-requirement execution start |
| requirement-executor | `requirement-executor-loop-completions` | `agent.complete.>` | Decomposer + requirement-review loop completions |
| execution-manager | `execution-orchestrator-execution-trigger` | `workflow.trigger.task-execution-loop` | Per-task TDD start |
| execution-manager | `execution-orchestrator-loop-completions` | `agent.complete.>` | TDD agent loop completions |
| plan-manager | `plan-api-events` | `workflow.events.>` | Scenario completion + all workflow events |
| plan-manager | `plan-api-rollup-completions` | `agent.complete.>` | Rollup reviewer loop completions |

## Payload Registry

All inter-component payloads are registered via `component.RegisterPayload` in `payload_registry.go`
files. The `Schema()` method on each type must match its registration exactly.

| Domain | Category | Version | Type | Used By |
|--------|----------|---------|------|---------|
| `workflow` | `trigger` | `v1` | `TriggerPayload` | planner, plan-reviewer, requirement-generator, scenario-generator, plan-rollup-reviewer |
| `workflow` | `scenario-orchestration` | `v1` | `ScenarioOrchestrationTrigger` | scenario-orchestrator |
| `workflow` | `requirement-execution` | `v1` | `RequirementExecutionRequest` | requirement-executor |
| `workflow` | `scenario-execution` | `v1` | `ScenarioExecutionRequest` | requirement-executor (backward compat) |
| `workflow` | `task-execution` | `v1` | `TriggerPayload` | execution-manager |
| `workflow` | `loop-completed` | `v1` | `LoopCompletedEvent` | requirement-executor, execution-manager, plan-manager |
| `workflow` | `requirements-generated` | `v1` | `RequirementsGeneratedEvent` | plan-manager |
| `workflow` | `scenarios-generated` | `v1` | `ScenariosGeneratedEvent` | plan-manager |
| `workflow` | `scenario-execution-complete` | `v1` | `ScenarioExecutionCompleteEvent` | plan-manager |

## Key Patterns

### BaseMessage Envelope

All inter-component messages are wrapped in `message.BaseMessage`:

```go
payload := &ScenarioOrchestrationTrigger{ScenarioID: id}
baseMsg := message.NewBaseMessage(payload.Schema(), payload, "scenario-orchestrator")
data, _ := json.Marshal(baseMsg)
js.Publish(ctx, subject, data)
```

Receivers unmarshal `BaseMessage` first, then unmarshal `Payload` into the concrete type.

### Named Consumer Lifecycle

Every orchestrator registers consumers with `ConsumeStreamWithConfig` and tracks the returned
`ConsumerInfo` for clean shutdown:

```go
// Start
info, err := s.natsClient.ConsumeStreamWithConfig(ctx, ConsumerConfig{...}, handler)
s.consumerInfos = append(s.consumerInfos, info)

// Stop
for _, info := range s.consumerInfos {
    s.natsClient.StopConsumer(info)
}
```

### Fan-Out on `agent.complete.>`

`agent.complete.>` is consumed by **two** independent named consumers — one per orchestrator level.
Each consumer receives every completion event; each filters by the loop IDs it dispatched, ignoring
the rest. This allows requirement-executor and execution-manager to coexist on the same stream
without coordination.

### decompose_task and StopLoop

The `decompose_task` tool does not publish a separate result message. Instead it calls `StopLoop` on
the running agentic loop, which causes the loop to emit `LoopCompletedEvent` with the validated
`TaskDAG` as its result payload. The requirement-executor reads the DAG from that event and fans out
`workflow.trigger.task-execution-loop` messages — one per DAG node, in dependency order.

### JetStream Publish for Ordering

Task dispatch uses JetStream publish (not core NATS) to guarantee delivery ordering. A DAG node's
`workflow.trigger.task-execution-loop` message must be confirmed stored before its dependents are
dispatched.

```go
js, _ := s.natsClient.JetStream()
_, err = js.Publish(ctx, "workflow.trigger.task-execution-loop", data)
```

## Recurring Patterns

### Coordinator Pattern

Every orchestrator follows the same structure: receive a trigger, fan out work to N agents via
the agentic-loop, collect completions, advance to the next stage.

```
                  trigger
                    │
                    ▼
              ┌─────────────┐
              │ Coordinator  │ ← owns activeExecutions map
              └──────┬──────┘
                     │ fan-out N tasks via agent.task.*
           ┌─────────┼─────────┐
           ▼         ▼         ▼
      agentic-loop  ...  agentic-loop
           │         │         │
           └─────────┼─────────┘
                     │ agent.complete.> (fan-out to all coordinators)
                     ▼
              ┌─────────────┐
              │ Coordinator  │ ← routes by TaskID index
              └──────┬──────┘
                     │ all N complete?
                     ▼
              advance to next stage
```

**Instances of this pattern:**

| Coordinator | Fan-out | Completion routing | Next stage |
|---|---|---|---|
| requirement-executor | 1 decomposer → N DAG nodes (serial) → requirement review | `agent.complete.>` → `taskIDIndex` → `handleNodeCompleteLocked` | next node → requirement-reviewer → complete |
| execution-manager | 3 TDD stages (serial pipeline) | `agent.complete.>` → `taskIDIndex` → stage-specific handler | developer→validator→reviewer→complete |
| plan-manager | 1 rollup reviewer (post all scenarios) | `agent.complete.>` → `taskIDIndex` → `handleRollupCompleteLocked` | approved→complete / needs_attention |

### Named Consumer Per Coordinator

Each coordinator creates its own named JetStream consumer on `agent.complete.>`. This gives
fan-out semantics — every coordinator receives every completion event, then filters by
`WorkflowSlug` and `taskIDIndex` to route to the right execution.

```go
cfg := natsclient.StreamConsumerConfig{
    StreamName:    "AGENT",
    ConsumerName:  "my-coordinator-loop-completions",  // unique per coordinator
    FilterSubject: "agent.complete.>",
    AckPolicy:     "explicit",
    MaxAckPending: 10,
}
```

### Ack-Then-Process

Triggers that start long-running work (LLM calls, multi-stage pipelines) are acked immediately
after validation + state storage. The work runs asynchronously — if the component crashes, the
in-memory state is lost but the trigger is not redelivered (it was acked).

```go
func (c *Component) handleTrigger(ctx context.Context, msg jetstream.Msg) {
    trigger, err := parse(msg.Data())
    if err != nil { msg.Nak(); return }

    c.activeExecutions.Store(entityID, exec)
    msg.Ack()  // ack before long-running work

    // Long-running: LLM calls, agent dispatch, etc.
    c.startCoordination(ctx, exec)
}
```

### BaseMessage Envelope

All inter-component messages use `message.NewBaseMessage()` with a registered payload type.
Raw JSON on the event bus is forbidden — the payload registry provides runtime type safety.

```go
// Publisher
trigger := &payloads.ScenarioOrchestrationTrigger{PlanSlug: slug}
baseMsg := message.NewBaseMessage(trigger.Schema(), trigger, componentName)
data, _ := json.Marshal(baseMsg)
c.natsClient.PublishToStream(ctx, subject, data)

// Receiver
var base message.BaseMessage
json.Unmarshal(msg.Data(), &base)
trigger, ok := base.Payload().(*payloads.ScenarioOrchestrationTrigger)
```

### StopLoop for Terminal Tools

Tools that produce a final result (like `decompose_task`) set `StopLoop: true` on their
`ToolResult`. This makes the tool result content become the `LoopCompletedEvent.Result`
directly, skipping an unnecessary LLM round-trip.

```go
return agentic.ToolResult{
    Content:  dagJSON,
    StopLoop: true,  // tool result → event.Result directly
}
```

## Rules Engine

Rules are declarative JSON files in `configs/rules/` that react to entity state changes in the
`ENTITY_STATES` KV bucket. They handle terminal workflow transitions — publishing downstream events
and writing final status triples — without requiring changes to component Go code.

### Directory Layout

```
configs/rules/
├── semspec-task-execution/
│   ├── handle-approved.json    # reviewer approves → publish execution_complete
│   ├── handle-escalated.json   # budget exceeded or non-fixable → publish escalated
│   └── handle-error.json       # step failure or timeout → publish execution_failed
├── semspec-requirement-execution/
│   ├── handle-completed.json   # requirement reviewer approves → publish execution_complete
│   ├── handle-failed.json      # requirement reviewer rejects or node failed → publish failed
│   └── handle-error.json       # unexpected error → publish requirement.error
├── semspec-plan/
│   ├── handle-approved.json    # rollup reviewer approves → publish plan.approved
│   ├── handle-escalated.json   # review escalated → publish plan.escalated
│   └── handle-error.json       # error → publish plan.error
└── semspec-coordination/
    ├── handle-completed.json   # coordination done → publish coordination.completed
    └── handle-error.json       # error → publish coordination.error
```

### Rule Structure

Each rule is an `expression`-type rule with an entity pattern, conditions, and `on_enter` actions:

```json
{
  "id": "task-execution-approved",
  "type": "expression",
  "name": "Task Execution Approved",
  "entity": {
    "pattern": "*.*.exec.task.run.*",
    "watch_buckets": ["ENTITY_STATES"]
  },
  "conditions": [
    { "field": "workflow.execution.phase", "operator": "eq", "value": "approved" }
  ],
  "logic": "and",
  "on_enter": [
    { "type": "publish", "subject": "workflow.events.task.execution_complete",
      "properties": { "reason": "code_review_approved" } },
    { "type": "update_triple", "predicate": "workflow.execution.status", "object": "completed" }
  ]
}
```

### Entity ID Patterns by Workflow

| Workflow | Entity ID Pattern | Watch Bucket |
|----------|-------------------|--------------|
| Task execution | `*.*.exec.task.run.*` | `ENTITY_STATES` |
| Scenario execution | `*.*.exec.scenario.run.*` | `ENTITY_STATES` |
| Coordination | `*.*.exec.coord.run.*` | `ENTITY_STATES` |
| Review | `*.*.exec.review.run.*` | `ENTITY_STATES` |

### Design Intent

Components write workflow phases to entity triples as execution progresses. Rules react to phase
changes and own all terminal state management: publishing events to downstream consumers and
stamping the final `workflow.execution.status` triple.

This separation keeps component Go code focused on orchestration logic (phase progression) while
rules handle the observable consequences of reaching a terminal state. Adding a new terminal action
— such as notifying an external webhook — requires only a new `on_enter` entry in the relevant
rule file, with no Go changes.

## Lessons Learned System

After the requirement-level reviewer completes, the execution pipeline extracts lessons from
reviewer feedback and stores them scoped to the role that produced the rejected work. These lessons
are injected into future agent prompts, closing the feedback loop across executions without
requiring human intervention or a separate roster system.

### Five Roles

The lessons system is organized around five roles that map directly to pipeline stages:

| Role | Pipeline Stage | Scope |
|------|---------------|-------|
| `planner` | Plan phase (Goal/Context/Scope) | Plan-level LLM calls |
| `plan-reviewer` | Plan review | Plan-level review |
| `developer` | Builder + Tester stages | Per-task TDD implementation |
| `reviewer` | Reviewer stage | Per-task and per-scenario code review |
| `architect` | Decomposer stage | DAG decomposition |

### Lesson Extraction Flow

After the reviewer submits a non-approved verdict via `submit_work`:

```
reviewer verdict (non-approved)
      │
      ▼
classify feedback → error_categories.json vocabulary
      │
      ▼
store lesson scoped to role (developer, reviewer, etc.)
      │
      ▼
pattern count > lesson_threshold?
      │
      ├── yes → notify (log + optional downstream signal)
      └── no  → silent accumulation
```

Error categories are defined in `configs/error_categories.json`. The matcher maps free-form
reviewer feedback text onto category labels (e.g., `missing_tests`, `edge_case_missed`,
`incomplete_implementation`). This vocabulary is stable and shared across all roles.

### Lesson Injection

Before dispatching any agentic task, the prompt assembler queries stored lessons for the target
role. Matching lessons are injected into the `PeerFeedback` fragment slot (priority 350), which
appears after role context and before domain context in the assembled system prompt.

Lessons are filtered by relevance to the current task type and capped to prevent prompt bloat. The
`lesson_threshold` config field (default: `2`) controls when a recurring pattern triggers a
notification — it does not gate injection.

### Review Verdict Fields

The reviewer produces a `TaskCodeReviewResult` (in `workflow/payloads/results.go`):

| Field | Type | Description |
|-------|------|-------------|
| `Verdict` | string | `approved`, `fixable`, `misscoped`, `architectural`, or `too_big` |
| `RejectionType` | string | Populated on non-approved verdicts |
| `Feedback` | string | Qualitative reviewer feedback; source for lesson extraction |

### execution-manager Config

| Field | Default | Purpose |
|-------|---------|---------|
| `lesson_threshold` | `2` | Pattern count before a recurring lesson triggers a notification |

## Prompt Assembly

Every agent in the TDD pipeline receives a system prompt composed by the **prompt assembler** — a
fragment-based composition system in `prompt/`. Rather than hardcoded prompt strings, each stage's
prompt is assembled from domain-specific fragment catalogs filtered by role, provider, and runtime
conditions.

### How It Works

1. Components register fragments from a domain catalog at startup
   (e.g., `registry.RegisterAll(promptdomain.Software()...)`).
2. At dispatch time, the assembler filters fragments by the agent's role (developer, reviewer,
   architect, etc.) and the LLM provider (Anthropic, OpenAI, Ollama).
3. Fragments are sorted by category priority, formatted with provider-specific delimiters
   (XML tags for Anthropic, Markdown headers for OpenAI), and concatenated into a system message.
4. Dynamic `ContentFunc` closures inject runtime data — error trends, role-scoped lessons,
   iteration budgets — without modifying the fragment catalog.

### Fragment Categories (Assembly Order)

| Priority | Category | Content |
|----------|----------|---------|
| 0 | SystemBase | Identity ("You are a...") |
| 100 | ToolDirective | Tool-use mandates (e.g., MUST call submit_work to complete) |
| 200 | ProviderHints | Provider-specific instructions |
| 275 | BehavioralGate | Exploration gates, budget, structural checklist |
| 300 | RoleContext | Role-specific behavioral context |
| 325 | KnowledgeManifest | Graph summary |
| 350 | PeerFeedback | Error trends, role-scoped lessons learned |
| 400 | DomainContext | Task details, plan context |
| 500 | ToolGuidance | Advisory: when/how to use each tool |
| 600 | OutputFormat | Output JSON structure |
| 700 | GapDetection | Gap detection instructions |

### Domain Catalogs

Domains are fragment catalogs in `prompt/domain/`:

| Domain | File | Roles covered |
|--------|------|---------------|
| Software | `domain/software.go` | Developer, Builder, Tester, Planner, Reviewer, PlanReviewer, TaskReviewer, ScenarioReviewer, PlanRollupReviewer, ReqGen, ScenarioGen, PhaseGen, PlanCoordinator |
| Research | `domain/research.go` | Analyst (developer), Synthesizer (planner), Reviewer |

Adding a new domain requires only a new fragment catalog file — no changes to orchestrators or
the assembler itself. Components select a domain at registration time; the assembler handles
the rest.

### Tool Set

Agents receive tools partitioned into core (always present) and conditional (config-gated):

**Core tools — always registered:**

| Tool | Type | Purpose |
|------|------|---------|
| `bash` | Standard | Universal shell: files, git, builds, tests, and everything else |
| `submit_work` | Terminal (StopLoop) | Signals task completion with structured deliverable; loop result becomes `LoopCompletedEvent.Result` |
| `ask_question` | Terminal (StopLoop) | Escalates blockers; prevents premature completion |
| `answer_question` | Standard | Provides an answer to a pending question |
| `decompose_task` | Terminal (StopLoop) | DAG decomposition for requirement executor |
| `spawn_agent` | Standard | Spawns and awaits a child agentic loop (multi-agent hierarchy) |

**Conditional tools — registered when configured:**

| Tool | Condition | Purpose |
|------|-----------|---------|
| `graph_search` | GraphQL endpoint configured | Natural language search with answer synthesis |
| `graph_query` | GraphQL endpoint configured | Raw GraphQL queries against the knowledge graph |
| `graph_summary` | Graph registry available | High-level graph overview |
| `web_search` | Search API key configured | External web search |
| `http_request` | Always when enabled | Fetch URLs, convert HTML to text, persist to graph |

### Bash-First Approach

Agents use `bash` for all file and git operations. Dedicated file and git tools (`file_read`,
`file_write`, `file_list`, `git_*`) have been removed. Alpha testing with semdragon showed that
agents trained on bash handle these operations natively; specialized tools created ambiguity and
wasted iterations on tool selection.

Terminal tools (`submit_work`, `ask_question`, `decompose_task`) set `StopLoop: true` on their
`ToolResult`, which causes the agentic loop to emit `LoopCompletedEvent` immediately — preventing
premature completion from a generic output message.

### Role-Based Tool Filtering

`FilterTools(allTools, role)` gates which tools each role can access:

| Role | Core Tools | Conditional Tools |
|------|-----------|-------------------|
| Developer | `bash`, `submit_work`, `ask_question` | `graph_search`, `graph_query`, `graph_summary` |
| Planner | `bash`, `submit_work`, `ask_question` | `graph_search`, `graph_query`, `graph_summary`, `web_search` |
| Reviewer | `bash`, `submit_work`, `ask_question` | `graph_search`, `graph_query`, `graph_summary` |
| Architect | `bash`, `submit_work`, `ask_question` | `graph_search`, `graph_query`, `graph_summary`, `web_search` |
| Decomposer | `bash`, `decompose_task`, `ask_question` | `graph_search`, `graph_query`, `graph_summary` |

## Serial Decomposition

The requirement-executor converts a `TaskDAG` from the decomposer agent into an ordered execution
sequence, then dispatches nodes one at a time.

### Topological Sort

`topo_sort.go` implements Kahn's BFS algorithm:

1. Build an in-degree map and a dependents adjacency list from `node.DependsOn` edges.
2. Seed the queue with all zero-in-degree nodes, preserving their original slice order (stable
   sort for equal in-degree nodes).
3. Process the queue: append each node to `sorted`, decrement in-degree for all its dependents,
   and enqueue any newly zero-in-degree nodes.
4. Cycle detection: if `len(sorted) != len(dag.Nodes)`, return an error — the cycle prevented
   some nodes from reaching zero in-degree.

The resulting `SortedNodeIDs` slice is stored on `requirementExecution` and never mutated after
creation.

### Serial Execution Tracking

Requirement execution state (in `processor/requirement-executor/execution_state.go`) tracks
progress through the sorted node list:

| Field | Purpose |
|-------|---------|
| `SortedNodeIDs` | Topologically ordered node IDs |
| `NodeIndex` | Map of `nodeID → *TaskNode` for O(1) lookup |
| `CurrentNodeIdx` | Index into `SortedNodeIDs`; `-1` before execution starts |
| `CurrentNodeTaskID` | Agentic task ID of the node currently executing |
| `VisitedNodes` | Set of node IDs that have completed successfully |

On each `handleNodeCompleteLocked()` call:

1. Mark `CurrentNodeIdx` node in `VisitedNodes`.
2. Increment `CurrentNodeIdx`.
3. If `CurrentNodeIdx < len(SortedNodeIDs)`, dispatch the next node to
   `workflow.trigger.task-execution-loop`.
4. If all nodes are visited, dispatch the requirement-level reviewer. On approval, publish
   `workflow.events.scenario.execution_complete`.

Node failures set the entity phase to `failed` → rules engine publishes
`workflow.events.requirement.failed`. No further nodes are dispatched after a failure.

### Scenario-Level Review State

The `scenarioExecution` state also tracks:

| Field | Purpose |
|-------|---------|
| `ScenarioReviewTaskID` | Agentic task ID of the scenario-reviewer loop |

## Plan Rollup Review

After all scenarios reach `execution_complete`, plan-manager transitions the plan to
`reviewing_rollup` and triggers the rollup reviewer.

### Plan Status Flow

```
ready_for_execution
      │
      ▼ (/execute)
implementing
      │
      ▼ (all scenarios complete)
reviewing_rollup
      │
      ├── approved  → complete
      └── needs_attention → complete (with findings recorded)
```

### Rollup Reviewer

The rollup reviewer (`prompt role: plan-rollup-reviewer`) receives:

- All scenario outcomes and verdicts
- Full changeset summary across all scenarios

It produces a `PlanRollupReviewResult` containing:

| Field | Type | Description |
|-------|------|-------------|
| `Verdict` | string | `approved` or `needs_attention` |
| `Summary` | string | Narrative summary of all scenario outcomes |
| `Findings` | `[]RollupFinding` | Per-scenario findings requiring follow-up (needs_attention only) |

`needs_attention` does not block plan completion — it records findings for human follow-up and
advances the plan to `complete`. Only `approved` and `needs_attention` are valid rollup verdicts;
hard failures at the scenario level prevent the plan from reaching `reviewing_rollup`.

### Trigger

`workflow.trigger.plan-rollup-review` carries a `TriggerPayload` with the plan slug. The
plan-manager publishes this after receiving the final `workflow.events.scenario.execution_complete`
event that clears all pending scenarios.
