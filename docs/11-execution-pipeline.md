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
│  plan-api ──► scenario.orchestrate.<requirementID>                            │
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
│  execution-orchestrator (per task node)                                       │
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
│       ├──► agent.task.red-team ──► agentic-loop (red team) [teams only]      │
│       │         sees full requirement changeset across all tasks              │
│       │         holistic critique: issues + adversarial tests                 │
│       │         graceful fallback: skipped if no red team available           │
│       │                                                                       │
│       └──► agent.task.scenario-reviewer ──► agentic-loop (requirement-reviewer)│
│                 reviews full requirement changeset + per-scenario verdicts    │
│                 receives red team challenge data when teams are enabled       │
│                 verdict: approved / needs_changes / escalate                  │
│                 publishes: workflow.events.scenario.execution_complete        │
│                                                                               │
└───────────────────────────────────────────────────────────────────────────────┘
                                 │
                                 ▼ (all requirements complete)
┌─────────────────────── PLAN ROLLUP REVIEW ──────────────────────────────────┐
│                                                                               │
│  plan-api (post-execution)                                                    │
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
commits resources. When `auto_approve` is enabled on plan-coordinator, the pipeline skips this
gate and flows directly to execution. See [Plan API Reference](12-plan-api.md) for the full
endpoint reference.

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
| `workflow.trigger.requirement-execution-loop` | WORKFLOWS | scenario-orchestrator → requirement-executor | `RequirementExecutionRequest` (BaseMessage) | `requirement-executor-trigger` |

### Decomposition Phase

| Subject | Stream | Publisher → Subscriber | Payload | Consumer |
|---------|--------|----------------------|---------|----------|
| `agent.task.development` | AGENT | requirement-executor → agentic-loop (decomposer) | `TaskMessage` | — |
| `agent.complete.>` | AGENT | agentic-loop → requirement-executor | `LoopCompletedEvent` | `requirement-executor-loop-completions` |
| `workflow.trigger.task-execution-loop` | WORKFLOWS | requirement-executor → execution-orchestrator | `TriggerPayload` (BaseMessage) | `execution-orchestrator-execution-trigger` |

### TDD Pipeline Phase

| Subject | Stream | Publisher → Subscriber | Payload | Consumer |
|---------|--------|----------------------|---------|----------|
| `agent.task.testing` | AGENT | execution-orchestrator → agentic-loop (tester) | `TaskMessage` | — |
| `agent.task.building` | AGENT | execution-orchestrator → agentic-loop (builder) | `TaskMessage` | — |
| `agent.task.validation` | AGENT | execution-orchestrator → agentic-loop (validator) | `TaskMessage` | — |
| `agent.task.reviewer` | AGENT | execution-orchestrator → agentic-loop (reviewer) | `TaskMessage` | — |
| `agent.complete.>` | AGENT | agentic-loop → execution-orchestrator | `LoopCompletedEvent` | `execution-orchestrator-loop-completions` |

### Scenario-Level Review Phase

| Subject | Stream | Publisher → Subscriber | Payload | Consumer |
|---------|--------|----------------------|---------|----------|
| `agent.task.red-team` | AGENT | requirement-executor → agentic-loop (red team) [teams only] | `TaskMessage` | — |
| `agent.task.scenario-reviewer` | AGENT | requirement-executor → agentic-loop (requirement-reviewer) | `TaskMessage` | — |
| `workflow.events.scenario.execution_complete` | WORKFLOWS | requirement-executor → plan-api | `ScenarioExecutionCompleteEvent` | `plan-api-scenario-completions` |
| `agent.complete.>` | AGENT | agentic-loop → requirement-executor | `LoopCompletedEvent` | `requirement-executor-loop-completions` |

### Plan Rollup Review Phase

| Subject | Stream | Publisher → Subscriber | Payload | Consumer |
|---------|--------|----------------------|---------|----------|
| `workflow.trigger.plan-rollup-review` | WORKFLOWS | plan-api → rollup-reviewer | `TriggerPayload` | `plan-rollup-reviewer` |
| `agent.complete.>` | AGENT | agentic-loop → plan-api | `LoopCompletedEvent` | `plan-api-rollup-completions` |

## Consumer Names

All orchestrators use named JetStream consumers via `ConsumeStreamWithConfig`. Each is registered in
the component's `consumerInfos` slice and stopped cleanly in `Stop()`.

| Component | Consumer Name | Subject Filter | Purpose |
|-----------|--------------|----------------|---------|
| plan-coordinator | `plan-coordinator-coordination-trigger` | `workflow.trigger.plan-coordinator` | Inbound plan triggers |
| plan-coordinator | `plan-coordinator-loop-completions` | `agent.complete.>` | Planner loop completions |
| plan-coordinator | `plan-coordinator-reqs-generated` | `workflow.events.requirements.generated` | Requirements ready signal |
| plan-coordinator | `plan-coordinator-scenarios-generated` | `workflow.events.scenarios.generated` | Scenarios ready signal |
| scenario-orchestrator | `scenario-orchestrator` | `scenario.orchestrate.*` | Requirement dispatch triggers (Fetch pattern) |
| requirement-executor | `requirement-executor-trigger` | `workflow.trigger.requirement-execution-loop` | Per-requirement execution start |
| requirement-executor | `requirement-executor-loop-completions` | `agent.complete.>` | Decomposer + requirement-review loop completions |
| execution-orchestrator | `execution-orchestrator-execution-trigger` | `workflow.trigger.task-execution-loop` | Per-task TDD start |
| execution-orchestrator | `execution-orchestrator-loop-completions` | `agent.complete.>` | TDD agent loop completions |
| plan-api | `plan-api-scenario-completions` | `workflow.events.scenario.execution_complete` | Requirement execution completion signal |
| plan-api | `plan-api-rollup-completions` | `agent.complete.>` | Rollup reviewer loop completions |

## Payload Registry

All inter-component payloads are registered via `component.RegisterPayload` in `payload_registry.go`
files. The `Schema()` method on each type must match its registration exactly.

| Domain | Category | Version | Type | Used By |
|--------|----------|---------|------|---------|
| `workflow` | `trigger` | `v1` | `TriggerPayload` | plan-coordinator, planner, plan-reviewer, plan-rollup-reviewer |
| `workflow` | `scenario-orchestration` | `v1` | `ScenarioOrchestrationTrigger` | scenario-orchestrator |
| `workflow` | `requirement-execution` | `v1` | `RequirementExecutionRequest` | requirement-executor |
| `workflow` | `scenario-execution` | `v1` | `ScenarioExecutionRequest` | requirement-executor (backward compat) |
| `workflow` | `task-execution` | `v1` | `TriggerPayload` | execution-orchestrator |
| `workflow` | `loop-completed` | `v1` | `LoopCompletedEvent` | plan-coordinator, requirement-executor, execution-orchestrator, plan-api |
| `workflow` | `requirements-generated` | `v1` | `RequirementsGeneratedEvent` | plan-coordinator |
| `workflow` | `scenarios-generated` | `v1` | `ScenariosGeneratedEvent` | plan-coordinator |
| `workflow` | `scenario-execution-complete` | `v1` | `ScenarioExecutionCompleteEvent` | plan-api |

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
the rest. This allows plan-coordinator, requirement-executor, and execution-orchestrator to coexist
on the same stream without coordination.

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
| plan-coordinator | N planners (parallel by focus area) | `agent.complete.>` → `taskIDIndex` → `handlePlannerCompleteLocked` | synthesize → requirement-gen → scenario-gen → review |
| requirement-executor | 1 decomposer → N DAG nodes (serial) → requirement review | `agent.complete.>` → `taskIDIndex` → `handleNodeCompleteLocked` | next node → [red team] → requirement-reviewer → complete |
| execution-orchestrator | 4 TDD stages (serial pipeline) | `agent.complete.>` → `taskIDIndex` → stage-specific handler | tester→builder→validator→reviewer→complete |
| plan-api | 1 rollup reviewer (post all scenarios) | `agent.complete.>` → `taskIDIndex` → `handleRollupCompleteLocked` | approved→complete / needs_attention |

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
| Requirement execution | `local.semspec.workflow.requirement-execution.execution.*` | `ENTITY_STATES` |
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

When team-based execution is enabled, the requirement-executor inserts a red team stage between DAG
completion and the requirement-level reviewer. The red team sees the **full requirement changeset** —
all files modified across every task in the requirement — and writes adversarial challenges before
the reviewer evaluates the complete implementation.

The red team no longer runs at the per-task level. The per-task pipeline is always:
tester → builder → validator → reviewer (4 stages, no red team).

### Dispatch Flow

After all DAG nodes complete, `dispatchRequirementRedTeamLocked()` selects an opposing team via
`SelectRedTeam(ctx, blueTeamID)`, which excludes any team that performed the implementation. If
no red team is available, the function logs a warning and falls back directly to
`dispatchRequirementReviewerLocked()` — the pipeline always completes regardless of team availability.

```
all DAG nodes complete
      │
      ▼
teamsEnabled() && BlueTeamID != ""?
      │
      ├── yes → SelectRedTeam(blueTeamID)
      │              │
      │              ├── team found → dispatch to agent.task.red-team
      │              │                  wait for agent.complete.>
      │              │                  handleRequirementRedTeamCompleteLocked()
      │              │                  → dispatchRequirementReviewerLocked()
      │              │
      │              └── no team → dispatchRequirementReviewerLocked() (fallback)
      │
      └── no → dispatchScenarioReviewerLocked()
```

### Red Team Task

The red team agent receives the full requirement changeset via `agent.task.red-team`. It produces a
`RedTeamChallengeResult` (in `workflow/payloads/red_team.go`) containing:

- `Issues` — a list of `RedTeamIssue` entries, each with description, severity (`critical`,
  `major`, `minor`, `nit`), optional file path, and suggested fix
- `OverallScore` (1–5) — the red team's self-assessed critique confidence
- `Summary` — a brief narrative of findings
- `TestFiles` — optional adversarial test files (boosts thoroughness score)
- `TestsPassed` — whether the adversarial tests pass against the current implementation

At least one issue or one test file is required; empty results are rejected by `Validate()`.

### Result Handling

`handleRequirementRedTeamCompleteLocked()` parses the loop completion result into a
`RedTeamChallengeResult`. Parse failures are non-fatal: the function logs a warning and proceeds
to the reviewer without red team data. This prevents a malformed red team response from blocking
the entire pipeline.

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
executions. The scenario reviewer evaluates both the blue team's full scenario implementation and
the red team's holistic critique, producing scores for both.

### Team Roles

- **Blue team** — tester + builder roles; performs the TDD implementation pipeline per task node
- **Red team** — writes adversarial challenges (issues + optional test files) against the blue
  team's complete requirement changeset (requirement-level, not per-task)
- **Requirement reviewer** — independent; evaluates the full requirement implementation and
  critique quality, including per-scenario verdict verdicts

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
| 100 | ToolDirective | Tool-use mandates (e.g., MUST call submit_work to complete) |
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
| Software | `domain/software.go` | Developer, Builder, Tester, Planner, Reviewer, PlanReviewer, TaskReviewer, ScenarioReviewer, PlanRollupReviewer, ReqGen, ScenarioGen, PhaseGen, PlanCoordinator |
| Research | `domain/research.go` | Analyst (developer), Synthesizer (planner), Reviewer |

Adding a new domain requires only a new fragment catalog file — no changes to orchestrators or
the assembler itself. Components select a domain at registration time; the assembler handles
the rest.

### Tool Set

Agents receive 11 tools, partitioned into core (always present) and conditional (config-gated):

**Core tools — always registered:**

| Tool | Type | Purpose |
|------|------|---------|
| `bash` | Standard | Universal shell: files, git, builds, tests, and everything else |
| `submit_work` | Terminal (StopLoop) | Signals task completion; loop result becomes `LoopCompletedEvent.Result` |
| `ask_question` | Terminal (StopLoop) | Escalates blockers; prevents premature completion |
| `decompose_task` | Terminal (StopLoop) | DAG decomposition for requirement executor |
| `spawn_agent` | Standard | Spawns and awaits a child agentic loop (multi-agent hierarchy) |
| `review_scenario` | Standard | Submits a scenario review verdict |

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
| Builder | `bash`, `submit_work`, `ask_question` | `graph_search`, `graph_query`, `graph_summary` |
| Tester | `bash`, `submit_work`, `ask_question` | `graph_search`, `graph_query`, `graph_summary` |
| Planner | `bash`, `submit_work`, `ask_question` | `graph_search`, `graph_query`, `graph_summary`, `web_search` |
| Reviewer | `bash`, `review_scenario`, `ask_question` | `graph_search`, `graph_query`, `graph_summary` |
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
4. If all nodes are visited, dispatch the requirement-level review stage (red team if teams enabled,
   then requirement-reviewer). On requirement-reviewer approval, publish
   `workflow.events.scenario.execution_complete`.

Node failures set the entity phase to `failed` → rules engine publishes
`workflow.events.requirement.failed`. No further nodes are dispatched after a failure.

### Scenario-Level Review State

The `scenarioExecution` state also tracks:

| Field | Purpose |
|-------|---------|
| `ScenarioReviewTaskID` | Agentic task ID of the scenario-reviewer loop |
| `ScenarioRedTeamTaskID` | Agentic task ID of the red team loop (teams only) |
| `ScenarioRedTeamChallenge` | Parsed `*payloads.RedTeamChallengeResult` from scenario red team |

## Plan Rollup Review

After all scenarios reach `execution_complete`, plan-api transitions the plan to
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
- Any red team findings surfaced at the scenario level

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
plan-api publishes this after receiving the final `workflow.events.scenario.execution_complete`
event that clears all pending scenarios.
