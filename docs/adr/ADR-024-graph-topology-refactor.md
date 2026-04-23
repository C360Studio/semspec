# ADR-024: Graph Topology Refactor — Requirements, Scenarios, and Change Proposals

**Status:** Approved
**Date:** 2026-03-05
**Authors:** Coby, Claude
**Supersedes:** None
**Context:** BMAD/OpenSpec evaluation exposed three structural gaps in semspec's graph model

## Problem Statement

SemSpec was modeled as a linear waterfall: Plan -> Phase -> Task -> Execute. An early adopter
evaluating semspec as a BMAD/OpenSpec replacement exposed three gaps:

1. **No mid-stream change handling** — when a requirement shifts during execution, there is no
   first-class way to propose, review, and cascade that change through affected work.
2. **Given/When/Then embedded in tasks** — behavioral contracts are documentation fields, not
   graph entities. They cannot be queried, gated on, or invalidated by upstream changes.
3. **Phases treated as containers** — phases are organizational views but the current model
   conflates scheduling ("when") with intent ("what for"), making cascade logic impossible.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| `blocked` TaskStatus | Explicit status in graph | UI must show it, agents must reason about it |
| Requirement generation timing | Separate step after plan approval, before phases | Keep concerns separated for smaller models |
| Scenario-to-Task cardinality | Many-to-many (Task SATISFIES multiple Scenarios) | Integration tasks are real; 1:1 creates busywork |
| Migration strategy | CLI command, run once, not idempotent | Greenfield only, rerun against clean state on failure |
| LLM prompt pipeline | Configurable, default to pipeline | Target is qwen 14b class; quality > latency |
| PlanDecision scope | One proposal can MUTATE multiple Requirements | Real changes rarely respect 1:1 boundaries |
| PlanDecision processing | Reactive workflow (OODA loop) | Consistent with ADR-005 pattern; cascade is a rule action |

## Target Node Hierarchy

```
Plan
  +-- Requirement(s)          <- Plan-scoped, not Phase-scoped
  |     +-- Scenario(s)       <- Given/When/Then as graph entities
  |           +-- Task(s)     <- SATISFIES edge (many-to-many)
  |                 +-- Execution
  +-- Phase(s)                <- Organizational view, references Tasks
  +-- PlanDecision(s)       <- Lifecycle node, mutates Requirements on acceptance
```

## Generation Sequence

```
Plan created
  -> Plan approved
    -> Generate Requirements (NEW)
      -> Generate Scenarios per Requirement (NEW)
        -> Generate Phases (existing, unchanged)
          -> Generate Tasks per Scenario (modified)
            -> Assign Tasks to Phases (modified)
```

Requirements describe intent. Scenarios describe observable behavior. Tasks describe
implementation work. Phases describe scheduling. Each concern has a separate LLM call
because smaller models produce better output with focused prompts.

## New Node Types

### Requirement

```go
type Requirement struct {
    ID          string            // "requirement.{plan_slug}.{sequence}"
    PlanID      string
    Title       string
    Description string
    Status      RequirementStatus // active, deprecated, superseded
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

Edges:
- `BELONGS_TO` -> Plan
- `HAS_SCENARIO` -> Scenario (one or many)
- `SUPERSEDED_BY` -> Requirement (via PlanDecision)

### Scenario

```go
type Scenario struct {
    ID            string         // "scenario.{plan_slug}.{requirement_seq}.{sequence}"
    RequirementID string
    Given         string         // precondition state
    When          string         // triggering action
    Then          []string       // expected outcomes (multiple assertions)
    Status        ScenarioStatus // pending, passing, failing, skipped
    CreatedAt     time.Time
    UpdatedAt     time.Time
}
```

Edges:
- `BELONGS_TO` -> Requirement
- `SATISFIED_BY` -> Task (many-to-many via Task.ScenarioIDs)
- `VALIDATED_BY` -> Execution result

### PlanDecision

```go
type PlanDecision struct {
    ID             string               // "plan-decision.{plan_slug}.{sequence}"
    PlanID         string
    Title          string
    Rationale      string
    Status         PlanDecisionStatus // proposed, under_review, accepted, rejected, archived
    ProposedBy     string               // agent role or "user"
    AffectedReqIDs []string             // multiple Requirements per proposal
    CreatedAt      time.Time
    ReviewedAt     *time.Time
    DecidedAt      *time.Time
}
```

Edges:
- `BELONGS_TO` -> Plan
- `MUTATES` -> Requirement (one or many)
- `ADDS_SCENARIO` -> Scenario
- `REMOVES_SCENARIO` -> Scenario
- `INVALIDATES` -> Task (computed on acceptance)

### Task Changes

- Remove: embedded `AcceptanceCriteria` field (after migration)
- Add: `ScenarioIDs []string` (many-to-many SATISFIES edge)
- Add: `dirty` status (set by PlanDecision acceptance cascade)
- Add: `blocked` status (explicit, set by dependency resolution)
- Keep: `PhaseID` reference (already reference-based)

### New Plan Statuses

Insert between `approved` and `phases_generated`:

```
created -> drafted -> reviewed -> approved
  -> requirements_generated -> scenarios_generated    (NEW)
  -> phases_generated -> phases_approved
  -> tasks_generated -> tasks_approved
  -> implementing -> complete
```

## PlanDecision Reactive Workflow

The PlanDecision lifecycle is an OODA loop processed by the reactive engine, consistent
with ADR-005. This is NOT inline API logic and NOT a standalone processor component.

### OODA Mapping

```
Observe:  Proposal created (user or agent publishes to trigger subject)
Orient:   Review proposal against current graph state (LLM reviewer or human)
Decide:   Accept or reject
Act:      Cascade dirty status through graph, publish events
```

### Reactive Rules

```
KV key pattern: plan-decision.*
Trigger subject: workflow.trigger.plan-decision-loop

Rules:
  accept-trigger          -> populate state from proposal payload
  dispatch-review         -> workflow.async.plan-decision-reviewer (LLM or human gate)
  review-completed        -> evaluate verdict
  handle-accepted         -> execute cascade (graph traversal + dirty marking)
  handle-rejected         -> archive proposal, no graph mutations
  handle-escalation       -> user.signal.escalate
  handle-error            -> user.signal.error
```

### Cascade Logic (handle-accepted rule action)

1. Load PlanDecision from KV state
2. For each Requirement in `AffectedReqIDs`:
   a. Publish `requirement.updated` event
   b. Query `HAS_SCENARIO` edges to find affected Scenarios
   c. For each Scenario: query `SATISFIED_BY` edges to find affected Tasks
3. For each affected Task:
   a. Set status to `dirty`
   b. Persist updated task
4. Publish `task.dirty` event with all affected task IDs
5. Publish `plan_decision.accepted` event
6. Set proposal status to `archived`

### What Triggers a PlanDecision?

Three sources, all publish to the same trigger subject:

1. **User via UI** — manual proposal creation from Requirement panel
2. **Agent during execution** — developer agent detects a misscoped requirement and proposes
   a change instead of escalating (new escalation category: `propose_change`)
3. **Reviewer during review** — code reviewer identifies a behavioral gap that should be a
   new Requirement, not just a task rejection

Source (2) and (3) are future work. Source (1) is Phase 5 scope.

## NATS Events

### New Subjects

```
requirement.created                { planId, requirementId }
requirement.updated                { planId, requirementId, changeProposalId }
scenario.created                   { requirementId, scenarioId }
scenario.status.updated            { scenarioId, status }
task.dirty                         { taskIds[], scenarioIds[], requirementIds[], changeProposalId }
plan_decision.created            { planId, proposalId }
plan_decision.accepted           { planId, proposalId, affectedTaskIds[] }
plan_decision.rejected           { planId, proposalId }
workflow.trigger.plan-decision-loop   { proposalId, planId }
workflow.async.plan-decision-reviewer { proposalId, planId, affectedReqIDs }
```

### Modified Subjects

- `task.created` — add `scenarioIds` to payload
- `task.status.updated` — add `scenarioIds` for traceability

## Implementation Phases

### Phase 1: Foundation (Node Types + Vocabulary)

Files touched:
- `workflow/types.go` — Requirement, Scenario, PlanDecision structs + status enums
- `workflow/types.go` — `dirty`, `blocked` TaskStatus + transitions
- `workflow/types.go` — `requirements_generated`, `scenarios_generated` PlanStatus + transitions
- `vocabulary/semspec/predicates.go` — requirement, scenario, plan-decision predicates
- `workflow/plan.go` — SaveRequirements, LoadRequirements, SaveScenarios, LoadScenarios, etc.

Validation: unit tests for type transitions, serialization round-trips.

### Phase 2: API Surface (HTTP + TypeScript + Payloads)

Files touched:
- `processor/workflow-api/http_requirement.go` — Requirement CRUD handlers (new file)
- `processor/workflow-api/http_scenario.go` — Scenario CRUD handlers (new file)
- `processor/workflow-api/http_plan_decision.go` — PlanDecision lifecycle handlers (new file)
- `processor/workflow-api/http.go` — route registration
- `workflow/reactive/payloads.go` — new payload structs
- `workflow/reactive/payloads_registry.go` — payload registration
- `ui/src/lib/types/requirement.ts` — TypeScript types (new file)
- `ui/src/lib/types/scenario.ts` — TypeScript types (new file)
- `ui/src/lib/types/plan-decision.ts` — TypeScript types (new file)
- `ui/src/lib/api/client.ts` — API client methods

Validation: handler tests, TypeScript compiles.

### Phase 3: Migration (CLI Command)

Files touched:
- `cmd/semspec/migrate.go` — `semspec migrate extract-scenarios` command (new file)

Logic:
1. For each Task with AcceptanceCriteria:
   a. Create placeholder Requirement from Task's phase/plan context
   b. Create Scenario(s) from each AcceptanceCriterion
   c. Set Task.ScenarioIDs
   d. Clear Task.AcceptanceCriteria
2. Validate: every migrated Task has at least one ScenarioID
3. Run once against clean state. On failure: fix bug, rerun.

### Phase 4: Planning Flow (LLM Pipeline)

Files touched:
- `workflow/prompts/requirement_generator.go` — new prompt (new file)
- `workflow/prompts/scenario_generator.go` — new prompt (new file)
- `workflow/prompts/task_generator.go` — modified: takes Scenarios as input, no embedded criteria
- `processor/task-generator/component.go` — orchestrate multi-step pipeline
- `processor/task-generator/config.go` — `pipeline_mode` config field
- `workflow/prompts/developer.go` — pull Scenarios from graph edges, not inline field

Validation: mock LLM e2e test with new fixture format.

### Phase 5: PlanDecision Lifecycle + Cascade

Files touched:
- `workflow/reactive/plan_decision.go` — reactive workflow rules (new file)
- `workflow/reactive/plan_decision_actions.go` — cascade logic (new file)
- `configs/semspec.json` — plan-decision-loop configuration

Validation: integration test — create proposal -> accept -> verify dirty cascade.

### Phase 6: UI Updates

Files touched:
- `ui/src/lib/components/plan/RequirementPanel.svelte` — new component
- `ui/src/lib/components/plan/ScenarioDetail.svelte` — new component
- `ui/src/lib/components/plan/PlanDecisionFlow.svelte` — new component
- `ui/src/lib/components/plan/TaskDetail.svelte` — show linked Scenarios
- `ui/src/lib/components/plan/PlanNavTree.svelte` — dirty indicator + requirement view
- `ui/src/lib/components/board/PlanCard.svelte` — dirty task count

## What We Are NOT Changing

- Explore -> plan -> execute top-level flow
- NATS JetStream as messaging backbone
- KV bucket state management
- Agent role model
- Execution logic (agents executing tasks)
- Three-layer validation approach
- Phase-aware task dispatch (existing phase-task reference model is fine)

## Risks

| Risk | Mitigation |
|------|------------|
| Multi-step LLM pipeline increases planning latency | Pipeline is default but configurable; single-shot available |
| PlanDecision cascade could dirty many tasks | Cascade publishes a single batched event; UI handles bulk dirty state |
| Many-to-many Task-Scenario edges complicate queries | Indexed via ScenarioIDs on Task; graph traversal is bounded by plan scope |
| Migration assumes greenfield | Acceptable — no legacy data to protect |

## Priority Order

1. Phase 1 — foundation for everything else
2. Phase 2 — API surface enables parallel UI work
3. Phase 3 — migration unblocks new planning flow
4. Phase 4 — planning flow is the user-facing payoff
5. Phase 5 — PlanDecision is the BMAD/OpenSpec differentiator
6. Phase 6 — UI shows graph state; graph must be correct first
