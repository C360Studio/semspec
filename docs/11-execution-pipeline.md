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
│       └──► agent.task.reviewer ──► agentic-loop (reviewer)                   │
│                 code review (workflow.async.task-code-reviewer)               │
│                 verdict: approved / fixable / misscoped / too_big            │
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
