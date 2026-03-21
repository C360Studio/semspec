# Execution Pipeline

Reference for the full semspec execution pipeline — from plan creation through TDD task completion.

## Pipeline Overview

```
┌─────────────────────────────── PLAN PHASE ──────────────────────────────────┐
│                                                                               │
│  /plan <title>                                                                │
│       │                                                                       │
│       ▼                                                                       │
│  plan-api ──► plan-coordinator                                                │
│                     │                                                         │
│                     ├──► planner (async, parallel)                            │
│                     ├──► requirement-generator (async)                        │
│                     └──► scenario-generator (async)                           │
│                                │                                              │
│                                ▼                                              │
│                          plan-reviewer ──► approved / needs_changes           │
│                                │                                              │
│                                ▼ (approved)                                   │
│                    status: ready_for_execution                                │
│                                                                               │
└───────────────────────────────────────────────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────── EXECUTION TRIGGER ───────────────────────────────────┐
│                                                                               │
│  /execute <slug>  OR  auto_approve=true                                       │
│       │                                                                       │
│       ▼                                                                       │
│  plan-api ──► scenario.orchestrate.<scenarioID>                               │
│                     │                                                         │
│                     ▼                                                         │
│             scenario-orchestrator ──► workflow.trigger.scenario-execution-loop│
│                                                                               │
└───────────────────────────────────────────────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────── DECOMPOSITION PHASE ─────────────────────────────────┐
│                                                                               │
│  scenario-executor (per Scenario)                                             │
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
│  execution-orchestrator (per task node)                                       │
│       │                                                                       │
│       ├──► agent.task.testing ──► agentic-loop (tester)                      │
│       │         writes failing tests                                          │
│       │                                                                       │
│       ├──► agent.task.building ──► agentic-loop (builder)                    │
│       │         implements to pass tests                                      │
│       │                                                                       │
│       ├──► agent.task.validation ──► agentic-loop (validator)                │
│       │         structural validation (workflow.async.structural-validator)   │
│       │                                                                       │
│       ├──► agent.task.red-team ──► agentic-loop (red team) [optional]        │
│       │         adversarial challenges: issues + test files                  │
│       │         dispatched when teams enabled + red team available            │
│       │         graceful fallback: skipped if no red team                    │
│       │                                                                       │
│       └──► agent.task.reviewer ──► agentic-loop (reviewer)                   │
│                 code review (workflow.async.task-code-reviewer)               │
│                 receives red team challenge data when available               │
│                 verdict: approved / fixable / misscoped / too_big            │
│                 scores red team: accuracy / thoroughness / fairness (1-5)    │
│                                                                               │
└───────────────────────────────────────────────────────────────────────────────┘
```

## NATS Subject Reference

### Plan Phase

| Subject | Stream | Publisher → Subscriber | Payload | Consumer |
|---------|--------|----------------------|---------|----------|
| `workflow.trigger.plan-coordinator` | WORKFLOWS | plan-api → plan-coordinator | `TriggerPayload` | `plan-coordinator-coordination-trigger` |
| `workflow.async.planner` | WORKFLOWS | plan-coordinator → planner | `TriggerPayload` | `planner` |
| `workflow.async.requirement-generator` | WORKFLOWS | plan-coordinator → requirement-generator | `TriggerPayload` | `requirement-generator` |
| `workflow.async.scenario-generator` | WORKFLOWS | plan-coordinator → scenario-generator | `TriggerPayload` | `scenario-generator` |
| `workflow.async.plan-reviewer` | WORKFLOWS | plan-coordinator → plan-reviewer | `TriggerPayload` | `plan-reviewer` |
| `workflow.events.requirements.generated` | WORKFLOWS | requirement-generator → plan-coordinator | `RequirementsGeneratedEvent` | `plan-coordinator-reqs-generated` |
| `workflow.events.scenarios.generated` | WORKFLOWS | scenario-generator → plan-coordinator | `ScenariosGeneratedEvent` | `plan-coordinator-scenarios-generated` |
| `agent.complete.>` | AGENT | agentic-loop → plan-coordinator | `LoopCompletedEvent` | `plan-coordinator-loop-completions` |

### Execution Trigger Phase

| Subject | Stream | Publisher → Subscriber | Payload | Consumer |
|---------|--------|----------------------|---------|----------|
| `scenario.orchestrate.*` | WORKFLOWS | plan-api / plan-coordinator → scenario-orchestrator | `ScenarioOrchestrationTrigger` (BaseMessage) | `scenario-orchestrator` |
| `workflow.trigger.scenario-execution-loop` | WORKFLOWS | scenario-orchestrator → scenario-executor | `ScenarioExecutionRequest` (BaseMessage) | `scenario-executor-scenario-trigger` |

### Decomposition Phase

| Subject | Stream | Publisher → Subscriber | Payload | Consumer |
|---------|--------|----------------------|---------|----------|
| `agent.task.development` | AGENT | scenario-executor → agentic-loop (decomposer) | `TaskMessage` | — |
| `agent.complete.>` | AGENT | agentic-loop → scenario-executor | `LoopCompletedEvent` | `scenario-executor-loop-completions` |
| `workflow.trigger.task-execution-loop` | WORKFLOWS | scenario-executor → execution-orchestrator | `TriggerPayload` (BaseMessage) | `execution-orchestrator-execution-trigger` |

### TDD Pipeline Phase

| Subject | Stream | Publisher → Subscriber | Payload | Consumer |
|---------|--------|----------------------|---------|----------|
| `agent.task.testing` | AGENT | execution-orchestrator → agentic-loop (tester) | `TaskMessage` | — |
| `agent.task.building` | AGENT | execution-orchestrator → agentic-loop (builder) | `TaskMessage` | — |
| `agent.task.validation` | AGENT | execution-orchestrator → agentic-loop (validator) | `TaskMessage` | — |
| `agent.task.red-team` | AGENT | execution-orchestrator → agentic-loop (red team) | `TaskMessage` | — |
| `agent.task.reviewer` | AGENT | execution-orchestrator → agentic-loop (reviewer) | `TaskMessage` | — |
| `workflow.async.structural-validator` | WORKFLOWS | execution-orchestrator → structural-validator | `TriggerPayload` | `structural-validator` |
| `workflow.async.task-code-reviewer` | WORKFLOWS | execution-orchestrator → task-code-reviewer | `TriggerPayload` | `task-code-reviewer` |
| `agent.complete.>` | AGENT | agentic-loop → execution-orchestrator | `LoopCompletedEvent` | `execution-orchestrator-loop-completions` |

## Consumer Names

All orchestrators use named JetStream consumers via `ConsumeStreamWithConfig`. Each is registered in
the component's `consumerInfos` slice and stopped cleanly in `Stop()`.

| Component | Consumer Name | Subject Filter | Purpose |
|-----------|--------------|----------------|---------|
| plan-coordinator | `plan-coordinator-coordination-trigger` | `workflow.trigger.plan-coordinator` | Inbound plan triggers |
| plan-coordinator | `plan-coordinator-loop-completions` | `agent.complete.>` | Planner loop completions |
| plan-coordinator | `plan-coordinator-reqs-generated` | `workflow.events.requirements.generated` | Requirements ready signal |
| plan-coordinator | `plan-coordinator-scenarios-generated` | `workflow.events.scenarios.generated` | Scenarios ready signal |
| scenario-orchestrator | `scenario-orchestrator` | `scenario.orchestrate.*` | Scenario dispatch triggers (Fetch pattern) |
| scenario-executor | `scenario-executor-scenario-trigger` | `workflow.trigger.scenario-execution-loop` | Per-scenario execution start |
| scenario-executor | `scenario-executor-loop-completions` | `agent.complete.>` | Decomposer loop completions |
| execution-orchestrator | `execution-orchestrator-execution-trigger` | `workflow.trigger.task-execution-loop` | Per-task TDD start |
| execution-orchestrator | `execution-orchestrator-loop-completions` | `agent.complete.>` | TDD agent loop completions |

## Payload Registry

All inter-component payloads are registered via `component.RegisterPayload` in `payload_registry.go`
files. The `Schema()` method on each type must match its registration exactly.

| Domain | Category | Version | Type | Used By |
|--------|----------|---------|------|---------|
| `workflow` | `trigger` | `v1` | `TriggerPayload` | plan-coordinator, planner, plan-reviewer |
| `workflow` | `scenario-orchestration` | `v1` | `ScenarioOrchestrationTrigger` | scenario-orchestrator |
| `workflow` | `scenario-execution` | `v1` | `ScenarioExecutionRequest` | scenario-executor |
| `workflow` | `task-execution` | `v1` | `TriggerPayload` | execution-orchestrator |
| `workflow` | `loop-completed` | `v1` | `LoopCompletedEvent` | plan-coordinator, scenario-executor, execution-orchestrator |
| `workflow` | `requirements-generated` | `v1` | `RequirementsGeneratedEvent` | plan-coordinator |
| `workflow` | `scenarios-generated` | `v1` | `ScenariosGeneratedEvent` | plan-coordinator |

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

`agent.complete.>` is consumed by **three** independent named consumers — one per orchestrator level.
Each consumer receives every completion event; each filters by the loop IDs it dispatched, ignoring
the rest. This allows plan-coordinator, scenario-executor, and execution-orchestrator to coexist on
the same stream without coordination.

### decompose_task and StopLoop

The `decompose_task` tool does not publish a separate result message. Instead it calls `StopLoop` on
the running agentic loop, which causes the loop to emit `LoopCompletedEvent` with the validated
`TaskDAG` as its result payload. The scenario-executor reads the DAG from that event and fans out
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
| plan-coordinator | N planners (parallel by focus area) | `agent.complete.>` → `taskIDIndex` → `handlePlannerCompleteLocked` | synthesize → requirement-gen → scenario-gen → review |
| scenario-executor | 1 decomposer → N DAG nodes (serial) | `agent.complete.>` → `taskIDIndex` → `handleNodeCompleteLocked` | next node or scenario-complete |
| execution-orchestrator | 4 TDD stages (serial pipeline) | `agent.complete.>` → `taskIDIndex` → stage-specific handler | tester→builder→validator→reviewer→complete |

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
├── semspec-scenario-execution/
│   ├── handle-completed.json   # all nodes done → publish scenario.completed
│   ├── handle-failed.json      # node failed → publish scenario.failed
│   └── handle-error.json       # unexpected error → publish scenario.error
├── semspec-plan/
│   ├── handle-approved.json    # review approved → publish plan.approved
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
    "pattern": "local.semspec.workflow.task-execution.execution.*",
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
| Task execution | `local.semspec.workflow.task-execution.execution.*` | `ENTITY_STATES` |
| Scenario execution | `local.semspec.workflow.scenario-execution.execution.*` | `ENTITY_STATES` |
| Plan | `local.semspec.workflow.plan.execution.*` | `ENTITY_STATES` |
| Coordination | `local.semspec.workflow.coordination.execution.*` | `ENTITY_STATES` |

### Design Intent

Components write workflow phases to entity triples as execution progresses. Rules react to phase
changes and own all terminal state management: publishing events to downstream consumers and
stamping the final `workflow.execution.status` triple.

This separation keeps component Go code focused on orchestration logic (phase progression) while
rules handle the observable consequences of reaching a terminal state. Adding a new terminal action
— such as notifying an external webhook — requires only a new `on_enter` entry in the relevant
rule file, with no Go changes.

## Red Team Challenges

When team-based execution is enabled, the execution-orchestrator inserts a red team stage between
validation and review. The red team writes adversarial challenges against the blue team's
implementation before the reviewer evaluates both teams' work.

### Dispatch Flow

After the validator stage passes, `dispatchRedTeamLocked()` selects an opposing team via
`SelectRedTeam(ctx, blueTeamID)`, which excludes any team that performed the implementation. If
no red team is available, the function logs a warning and falls back directly to
`dispatchReviewerLocked()` — the pipeline always completes regardless of team availability.

```
validator passes
      │
      ▼
teamsEnabled() && BlueTeamID != ""?
      │
      ├── yes → SelectRedTeam(blueTeamID)
      │              │
      │              ├── team found → dispatch to agent.task.red-team
      │              │                  wait for agent.complete.>
      │              │                  handleRedTeamCompleteLocked()
      │              │                  → dispatchReviewerLocked()
      │              │
      │              └── no team → dispatchReviewerLocked() (fallback)
      │
      └── no → dispatchReviewerLocked()
```

### Red Team Task

The red team agent receives the original task prompt via `agent.task.red-team`. It produces a
`RedTeamChallengeResult` (in `workflow/payloads/red_team.go`) containing:

- `Issues` — a list of `RedTeamIssue` entries, each with description, severity (`critical`,
  `major`, `minor`, `nit`), optional file path, and suggested fix
- `OverallScore` (1–5) — the red team's self-assessed critique confidence
- `Summary` — a brief narrative of findings
- `TestFiles` — optional adversarial test files (boosts thoroughness score)
- `TestsPassed` — whether the adversarial tests pass against the current implementation

At least one issue or one test file is required; empty results are rejected by `Validate()`.

### Result Handling

`handleRedTeamCompleteLocked()` parses the loop completion result into a
`RedTeamChallengeResult`. Parse failures are non-fatal: the function logs a warning and proceeds
to the reviewer without red team data. This prevents a malformed red team response from blocking
the entire TDD pipeline.

On successful parse, `exec.RedTeamChallenge` is populated and the reviewer receives the challenge
data in its context. The `exec.RedTeamTaskID` field is set before dispatch for routing loop
completion events.

### Key Fields on `taskExecution`

| Field | Purpose |
|-------|---------|
| `BlueTeamID` | Team that performed the implementation |
| `RedTeamID` | Team selected to challenge the implementation |
| `RedTeamAgentID` | Specific agent from the red team doing the critique |
| `RedTeamTaskID` | Agentic task ID for routing loop-completion events |
| `RedTeamChallenge` | Parsed `*payloads.RedTeamChallengeResult` from the challenge stage |
| `RedTeamKnowledge` | Pre-built team knowledge block injected into the red team prompt |

## Team-Based Review and Scoring

Team-based execution organizes agents into named teams that compete and learn across task
executions. The reviewer evaluates both the blue team's implementation and the red team's critique,
producing scores for both.

### Team Roles

- **Blue team** — tester + builder roles; performs the TDD implementation pipeline
- **Red team** — writes adversarial challenges (issues + optional test files) against the blue
  team's work
- **Reviewer** — independent; evaluates implementation quality and critique quality

Teams are enabled when `config.Teams.Enabled` is true and `config.Teams.Roster` contains at least
two entries (`teamsEnabled()` check).

### Review Verdict and Red Team Scoring

The reviewer produces a `TaskCodeReviewResult` (in `workflow/payloads/results.go`) with the
standard verdict fields plus red team scores when a challenge was present:

| Field | Type | Description |
|-------|------|-------------|
| `Verdict` | string | `approved`, `fixable`, `misscoped`, `architectural`, or `too_big` |
| `RejectionType` | string | Populated on non-approved verdicts |
| `Feedback` | string | Qualitative reviewer feedback |
| `RedAccuracy` | int (1–5) | Were the red team's issues real and accurate? |
| `RedThoroughness` | int (1–5) | Did the red team find what actually matters? |
| `RedFairness` | int (1–5) | Was the severity proportionate? |
| `RedFeedback` | string | Qualitative feedback on the critique itself |

Zero values for the three red team scores indicate the reviewer did not assess the red team
(e.g., no red team ran, or team mode is off).

### Team Knowledge Flow

`buildTeamKnowledgeBlock()` in `team_knowledge.go` injects two prompt sections into each agent's
task prompt:

1. **Team motivation** — always included; frames the agent as part of a named team working toward
   a shared goal, with the "Team Trophy" as an incentive for quality over nitpicking.
2. **Team lessons** — filtered insights from previous executions, capped at 10 entries and
   filtered by skill and error categories relevant to the current task.

After the reviewer completes, `extractTeamInsights()` classifies the feedback into error
categories via the error category matcher and stores new `TeamInsight` entries:

- Feedback routing to the **blue team**: categorized as `builder` skill by default; reclassified
  as `tester` skill when the matched error categories include `missing_tests` or
  `edge_case_missed`.
- Feedback routing to the **red team**: stored only when `OverallScore <= 2`, capturing a lesson
  about critique quality.

### Team and Agent Benching

Individual agents are benched by the persistent agent roster after exceeding the reviewer
rejection threshold. Team benching occurs when a majority (`>= len/2 + 1`) of a team's members
are individually benched — `checkTeamBenching()` calls `SetTeamStatus(ctx, teamID, TeamBenched)`
when the threshold is crossed.

Red team statistics are updated after every reviewer completion via
`UpdateTeamRedTeamStatsIncremental(ctx, redTeamID, accuracy, thoroughness, fairness)`. This
incremental update preserves the rolling average without requiring a full entity reload.

## Prompt Assembly

Every agent in the TDD pipeline receives a system prompt composed by the **prompt assembler** — a
fragment-based composition system in `prompt/`. Rather than hardcoded prompt strings, each stage's
prompt is assembled from domain-specific fragment catalogs filtered by role, provider, and runtime
conditions.

### How It Works

1. Components register fragments from a domain catalog at startup
   (e.g., `registry.RegisterAll(promptdomain.Software()...)`).
2. At dispatch time, the assembler filters fragments by the agent's role (tester, builder,
   reviewer, etc.) and the LLM provider (Anthropic, OpenAI, Ollama).
3. Fragments are sorted by category priority, formatted with provider-specific delimiters
   (XML tags for Anthropic, Markdown headers for OpenAI), and concatenated into a system message.
4. Dynamic `ContentFunc` closures inject runtime data — error trends, team knowledge, iteration
   budgets — without modifying the fragment catalog.

### Fragment Categories (Assembly Order)

| Priority | Category | Content |
|----------|----------|---------|
| 0 | SystemBase | Identity ("You are a...") |
| 100 | ToolDirective | Tool-use mandates (MUST call file_write) |
| 200 | ProviderHints | Provider-specific instructions |
| 275 | BehavioralGate | Exploration gates, budget, structural checklist |
| 300 | RoleContext | Role-specific behavioral context |
| 325 | KnowledgeManifest | Graph summary |
| 350 | PeerFeedback | Error trends, team lessons learned |
| 400 | DomainContext | Task details, plan context |
| 500 | ToolGuidance | Advisory: when/how to use each tool |
| 600 | OutputFormat | Output JSON structure |
| 700 | GapDetection | Gap detection instructions |

### Domain Catalogs

Domains are fragment catalogs in `prompt/domain/`:

| Domain | File | Roles covered |
|--------|------|---------------|
| Software | `domain/software.go` | Developer, Builder, Tester, Planner, Reviewer, PlanReviewer, TaskReviewer, ReqGen, ScenarioGen, PhaseGen, PlanCoordinator |
| Research | `domain/research.go` | Analyst (developer), Synthesizer (planner), Reviewer |

Adding a new domain requires only a new fragment catalog file — no changes to orchestrators or
the assembler itself. Components select a domain at registration time; the assembler handles
the rest.

### Role-Based Tool Filtering

`FilterTools(allTools, role)` gates which tools each role can access:

| Role | Tools |
|------|-------|
| Builder | `file_read`, `file_write`, `file_list`, `git_status`, `git_diff` |
| Tester | `file_read`, `file_write`, `file_list`, `exec` |
| Planner | `file_read`, `file_list`, `git_log`, `graph_query`, `workflow_graph_summary` |
| Reviewer | `file_read`, `file_list`, `git_diff`, `review_scenario` |

## Serial Decomposition

The scenario-executor converts a `TaskDAG` from the decomposer agent into an ordered execution
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

The resulting `SortedNodeIDs` slice is stored on `scenarioExecution` and never mutated after
creation.

### Serial Execution Tracking

Scenario execution state (in `processor/scenario-executor/execution_state.go`) tracks progress
through the sorted node list:

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
4. If all nodes are visited, set the entity phase to `completed` → rules engine publishes
   `workflow.events.scenario.completed`.

Node failures set the entity phase to `failed` → rules engine publishes
`workflow.events.scenario.failed`. No further nodes are dispatched after a failure.
