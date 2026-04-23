# ADR-026: Auto-Cascade from Plan Approval to Execution + UI Refactor

**Status:** Implemented
**Date:** 2026-03-05
**Authors:** Coby, Claude
**Depends on:** ADR-024 (Graph Topology Refactor), ADR-025 (Reactive Execution Model)
**Context:** Backend implements Requirements/Scenarios/DAG execution; UI still shows legacy phases/tasks/approve flow

---

## Problem Statement

ADR-024 introduced Requirements, Scenarios, and PlanDecisions as first-class graph entities. ADR-025 defined reactive execution where `decompose_task` produces Tasks at execution time, not during planning. The backend types, reactive workflows, and payload registrations all exist.

But the pipeline is disconnected in two critical places:

1. **Dead end at plan approval.** `handlePlanApprovedEvent` in `processor/workflow-api/events.go:197` persists the approved plan and stops. `RequirementGeneratorRequest` and `ScenarioGeneratorRequest` payloads exist in `workflow/reactive/payloads_graph_refactor.go` but nothing dispatches them. The cascade from approval to requirement generation never fires.

2. **UI still shows the old flow.** `ui/src/routes/plans/[slug]/+page.svelte` renders phases, tasks, and an action bar with Generate Phases -> Approve -> Generate Tasks -> Approve -> Execute. The `RequirementPanel.svelte` and `ScenarioDetail.svelte` components exist but aren't wired into the plan detail page's main flow.

The result: a new developer sees a UI that doesn't match the architecture docs, and the backend can't execute the flow described in ADR-024/025.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Auto-cascade trigger | Event-driven from `handlePlanApprovedEvent` | Single wiring point; reuses existing event infrastructure |
| Requirement/Scenario generation | Sequential, not parallel | Scenarios depend on Requirements; can't parallelize |
| Human gates | Plan approval only; requirements/scenarios are reviewable but don't block | Reduce friction; PlanDecisions handle mid-stream corrections |
| UI action bar | Replace multi-step approve flow with single Execute button | Matches ADR-025: planning produces Requirements + Scenarios, execution handles the rest |
| Phase display | Remove from primary flow; available as retrospective view post-execution | Phases are derived views per ADR-025, not prescriptive |
| Legacy compatibility | Remove old Generate Phases/Tasks buttons entirely | No migration path needed; no production users on old flow |

## Target Flow

### Backend: Plan Approval Cascade

```
User approves plan (plan-review-loop verdict: approved)
  -> handlePlanApprovedEvent fires
    -> Persist approval (existing)
    -> Publish RequirementGeneratorRequest to workflow.async.requirement-generator
      -> LLM generates Requirements from plan goal/context/scope
      -> Requirements written to graph + disk
      -> Plan status: requirements_generated
      -> For each Requirement:
        -> Publish ScenarioGeneratorRequest to workflow.async.scenario-generator
          -> LLM generates Scenarios (Given/When/Then) per Requirement
          -> Scenarios written to graph + disk
      -> Plan status: scenarios_generated -> ready_for_execution
      -> Publish plan.ready event
```

### Backend: Execution

```
User clicks Execute (or auto-execute if configured)
  -> scenario-execution-loop picks up each unmet Scenario
    -> Agent calls decompose_task(scenario) -> produces TaskDAG
    -> dag-execution-loop executes DAG nodes with dependency ordering
    -> Scenario marked passing/failing
  -> All Scenarios passing -> Plan complete
```

### UI: Plan Detail Page

```
Plan Detail
  +-- Plan summary (goal, context, scope)          <- existing
  +-- Review verdict banner                         <- existing
  +-- Requirements panel                            <- wire in RequirementPanel.svelte
  |     +-- Expandable Scenarios per Requirement    <- wire in ScenarioDetail.svelte
  +-- Action bar:
  |     - [Execute] when status = ready_for_execution
  |     - [Generating...] spinner during cascade
  |     - Status badge showing current cascade step
  +-- Execution view (when executing):
        +-- Agent loops per Scenario
        +-- DAG visualization per active loop
        +-- Live task progress
```

## Implementation

### Phase 1: Backend Wiring (Auto-Cascade)

**Files to modify:**

`processor/workflow-api/events.go` — In `handlePlanApprovedEvent`, after persisting approval:
```go
// Trigger requirement generation cascade
req := &RequirementGeneratorRequest{
    PlanSlug:  event.Slug,
    ProjectID: plan.ProjectID,
    TraceID:   event.TraceID,
}
if err := c.publishAsync(ctx, "workflow.async.requirement-generator", req); err != nil {
    c.logger.Error("Failed to trigger requirement generation", "slug", event.Slug, "error", err)
}
```

**New event handler** — `handleRequirementsGeneratedEvent`:
- Listens for `workflow.events.requirements.generated`
- For each Requirement, publishes `ScenarioGeneratorRequest`
- When all Scenarios generated, transitions plan to `ready_for_execution`

**Files to create:**
- `processor/workflow-api/events_cascade.go` — cascade event handlers (requirements generated, scenarios generated, ready for execution)

**Files to verify/update:**
- `workflow/reactive/payloads_graph_refactor.go` — confirm payload registrations are correct
- `workflow/types.go` — confirm status transitions include `approved -> requirements_generated -> scenarios_generated -> ready_for_execution`
- `configs/semspec.json` — add consumer configs for requirement-generator and scenario-generator subjects

### Phase 2: Requirement/Scenario Generators

If these processors don't exist yet, implement them:

`processor/requirement-generator/` — Component that:
- Subscribes to `workflow.async.requirement-generator`
- Builds context from plan (goal, context, scope)
- Calls LLM to decompose plan into Requirements
- Writes Requirements to graph + disk via plan manager
- Publishes `workflow.events.requirements.generated`

`processor/scenario-generator/` — Component that:
- Subscribes to `workflow.async.scenario-generator`
- Builds context from Requirement + plan scope
- Calls LLM to generate Given/When/Then Scenarios
- Writes Scenarios to graph + disk
- Publishes `workflow.events.scenarios.generated`

### Phase 3: UI Refactor

**`ui/src/routes/plans/[slug]/+page.svelte`:**
- Remove `handleGeneratePhases`, `handleApprovePhase`, `handleApproveTask`, `handleApproveAllTasks`
- Remove `phases` state and phase-related fetching
- Add `requirements` and `scenarios` state fetched via `api.requirements.list(slug)` and `api.scenarios.listByRequirement()`
- Wire `RequirementPanel.svelte` into main layout
- Simplify `ActionBar` to show Execute button when `status === 'ready_for_execution'`

**`ui/src/lib/components/plan/ActionBar.svelte`:**
- Remove Generate Phases, Approve Phase, Generate Tasks, Approve Tasks buttons
- Add: status-aware display showing cascade progress (generating requirements... generating scenarios... ready)
- Single Execute button appears when plan reaches `ready_for_execution`

**`ui/src/lib/components/plan/PlanNavTree.svelte`:**
- Replace phase-first tree with requirement-first tree:
  ```
  Plan
    +-- Requirement 1
    |     +-- Scenario 1.1
    |     +-- Scenario 1.2
    +-- Requirement 2
          +-- Scenario 2.1
  ```
- During execution: expand to show agent loops and DAG nodes under each Scenario

**`ui/src/lib/types/plan.ts`** — Update `derivePlanPipeline`:
- Add stages for `requirements_generated`, `scenarios_generated`, `ready_for_execution`
- Remove or deprecate `phases_generated`, `phases_approved`, `tasks_generated`, `tasks_approved` stages

**`ui/src/lib/stores/plans.svelte.ts`:**
- Remove `generateTasks()` method (tasks are created at execution time by `decompose_task`)
- Add `execute()` method that triggers scenario-execution-loop
- Add methods for fetching requirements and scenarios

### Phase 4: E2E Tests

**Mock LLM fixtures:**
- Add `mock-requirement-generator` and `mock-scenario-generator` fixture files
- Update `hello-world` scenario to include requirement/scenario generation in the cascade
- Update mock stats assertions for new LLM call sequence

**Playwright tests:**
- Update plan detail page tests to verify requirement/scenario display
- Add test for cascade: approve plan -> verify requirements appear -> verify scenarios appear -> verify Execute button
- Remove tests for Generate Phases/Tasks buttons

## What We Are NOT Changing

- Plan creation and review flow (plan-review-loop stays as-is)
- `decompose_task` tool implementation (ADR-025 scope)
- `dag-execution-loop` and `scenario-execution-loop` reactive workflows
- PlanDecision lifecycle (ADR-024 scope)
- Graph-first architecture
- NATS JetStream messaging
- KV bucket state management

## Status Mapping (Old -> New)

| Old Status | New Status | Notes |
|------------|------------|-------|
| `phases_generated` | `requirements_generated` | Requirements replace phases as the first post-approval artifact |
| `phases_approved` | `scenarios_generated` | Scenarios replace phase approval as the second gate |
| `tasks_generated` | `ready_for_execution` | No pre-generated tasks; plan is ready when scenarios exist |
| `tasks_approved` | (removed) | No task approval gate; governance handles execution-time validation |
| `implementing` | `implementing` | Unchanged |
| `complete` | `complete` | Unchanged |

## Risks

| Risk | Mitigation |
|------|------------|
| Cascade failure mid-way (requirements generated but scenario generation fails) | Each step is idempotent; retry from last successful status. Plan status tracks progress. |
| Users want to review requirements before scenarios are generated | Add optional `require_requirement_approval` config flag (default: false). Not in initial implementation. |
| Removing phase/task UI breaks existing demo fixtures | Update all mock LLM fixtures in the same PR. Demo mode must work end-to-end. |
| Requirement/scenario generators don't exist as components yet | Phase 2 creates them. Phase 1 (wiring) can be tested with mock event publishing. |

## Priority Order

1. ~~Phase 1 — Backend wiring unblocks the cascade~~ ✅ `ee99010`
2. ~~Phase 2 — Generators make the cascade actually produce output~~ ✅ (pre-existing: `processor/requirement-generator/`, `processor/scenario-generator/`)
3. ~~Phase 3 — UI shows what the backend produces~~ ✅ `69f135c`
4. ~~Phase 4 — E2E tests validate the full loop~~ ✅ `c3d965d` (Playwright tests; mock LLM fixtures pending full-stack integration test)
