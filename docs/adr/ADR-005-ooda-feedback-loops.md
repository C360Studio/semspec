# ADR-005: OODA Feedback Loops via Workflow-Processor

**Status:** Complete
**Date:** 2026-02-20
**Authors:** Coby, Claude
**Supersedes:** None
**Context:** Plan review retry loop, pub/sub semantics audit

## Problem Statement

Several semspec components have established anti-patterns that bypass the semstreams workflow-processor:

1. **Broken feedback loops** — When the plan-reviewer rejects a plan, the result goes to an HTTP handler's synchronous wait consumer and dies. No event-driven mechanism re-triggers the planner with the rejection findings. The E2E test retries by calling the same HTTP endpoint 3 times, re-reviewing the *identical* plan.

2. **Wrong pub/sub semantics** — Components publish durable workflow results using Core NATS `Publish()` instead of JetStream. These happen to land in JetStream streams (because stream subjects overlap), but there are no ordering or delivery guarantees. Core NATS should be reserved for ephemeral request/reply operations.

3. **HTTP-driven orchestration** — The workflow-api component manually orchestrates multi-step flows (create plan → wait → promote → review → approve) through HTTP handlers. This logic belongs in declarative workflow definitions that the semstreams workflow-processor already supports.

These patterns conflict with the semstreams architecture where:
- **Workflow-processor** handles multi-step orchestration with conditions, loops, and feedback
- **Components** handle single-step structured processing (parse LLM output, validate, save)
- **JetStream** provides durable, ordered messaging for workflows
- **Core NATS** provides ephemeral request/reply (like the natsclient HTTP-style method)

## Mental Model: OODA Loops

The system models how a naval aviation unit plans, briefs, executes, and debriefs. Each phase is an OODA loop:

```
Observe ──→ Orient ──→ Decide ──→ Act
   ↑                                │
   └──────── feedback ──────────────┘
```

The plan workflow is an OODA loop:
- **Observe** — context-builder gathers codebase state from the knowledge graph
- **Orient** — planner generates Goal/Context/Scope
- **Decide** — plan-reviewer evaluates against SOPs and project file tree
- **Act** — approve (exit loop) or revise (loop back with findings to Orient)

Task execution is an OODA loop (already implemented correctly in `plan-and-execute.json`):
- **Observe** — context-builder gathers task-relevant context
- **Orient** — developer agent implements
- **Decide** — reviewer agent evaluates
- **Act** — approve, retry with feedback, escalate, or decompose

Every LLM call with a review step follows this pattern. The workflow-processor's step routing, condition evaluation, and loop iteration handling are the native primitives for this.

## Decision

### 1. Implement feedback loops as workflow definitions

Create `plan-review-loop.json` using the same declarative pattern as `plan-and-execute.json`:

```
planner → plan-reviewer → verdict_check
                              │ needs_changes (+ iteration < max)
                          revise_planner (with ${steps.plan-reviewer.output.findings})
                              → plan-reviewer → ...
                              │ max iterations exceeded
                          escalate_to_user
```

The workflow-processor handles the loop back, iteration counting, and feedback threading via interpolation (`${steps.<name>.output.*}`).

### 2. Make components workflow-compatible

Components (planner, plan-reviewer, etc.) must participate in workflows by implementing the async callback pattern:

**Current (anti-pattern):**
```go
// Component publishes result to Core NATS — nobody listening, no callback
c.natsClient.Publish(ctx, "workflow.result.planner."+slug, data)
```

**Correct:**
```go
// Component checks for callback subject in trigger
if trigger.CallbackSubject != "" {
    // Publish AsyncStepResult to workflow callback
    result := &workflow.AsyncStepResult{
        TaskID:      trigger.TaskID,
        ExecutionID: trigger.ExecutionID,
        Status:      "success",
        Output:      outputJSON,
    }
    js.Publish(ctx, trigger.CallbackSubject, marshaledResult)
} else {
    // Legacy: publish to result subject for non-workflow callers
    js.Publish(ctx, resultSubject, data)
}
```

This means component triggers need optional fields:
- `callback_subject` — where to send `AsyncStepResult` (set by workflow-processor)
- `task_id` — correlation ID for matching results to pending steps
- `execution_id` — workflow execution this belongs to

### 3. Fix pub/sub semantics

**Rule:** All workflow-related publishes use JetStream. Core NATS is reserved for ephemeral request/reply.

| Component | Current | Correct | Subject |
|-----------|---------|---------|---------|
| `planner` | `natsClient.Publish()` | `js.Publish()` | `workflow.result.planner.*` |
| `plan-coordinator` | `natsClient.Publish()` | `js.Publish()` | `workflow.result.plan-coordinator.*` |
| `task-generator` | `natsClient.Publish()` | `js.Publish()` | result notification |
| `task-dispatcher` | `natsClient.Publish()` (line 807) | `js.Publish()` | result notification |
| `context-builder` | `natsClient.Publish()` | `js.Publish()` | `context.built.*` |
| `rdf-export` | `natsClient.Publish()` | `js.Publish()` | export output |

**Context-builder special case:** Uses Core NATS `Publish()` for `context.built.*` responses. The `contexthelper` uses JetStream `Publish()` for requests. This asymmetry is wrong — if the request goes through JetStream, the response should too (both subjects are in the AGENT stream).

### 4. HTTP endpoints become workflow gateways

HTTP handlers transition from orchestrators to gateways:

**Current (anti-pattern):**
```
POST /plans          → create plan → trigger planner → return
POST /plans/:slug/promote → trigger reviewer → wait → approve/reject → return
```
The HTTP handler manually coordinates the review, waits synchronously, and handles retry logic.

**Correct:**
```
POST /plans          → create plan → trigger plan-review-loop workflow → return
GET  /plans/:slug    → read workflow execution state → return current stage
POST /plans/:slug/approve → validate workflow is at approval gate → approve → return
```
The workflow-processor handles the plan → review → revise loop. The HTTP endpoint is a gateway for humans and external services. The internal flow is event-driven.

## Semstreams Changes

### ✅ `publish_async` Action Type (Done)

Added `publish_async` action type to semstreams. Same publish-and-park as `publish_agent` but with an arbitrary payload (no `TaskMessage` construction). Any component can now participate as an async workflow step.

**Receiving component contract:**
- Component trigger payload may contain `callback_subject` and `task_id`
- If present, component publishes `AsyncStepResult` to `callback_subject` when done
- `AsyncStepResult.Output` carries the component's structured result (e.g., plan content, review findings)
- The workflow-processor's interpolation makes this available as `${steps.<name>.output.*}`

### ✅ `"in"` Condition Operator (Done)

Added `"in"` operator to `EvaluateCondition` and schema validation. Previously, `plan-and-execute.json` used `"in"` but the operator silently failed (executor treated the error as condition=false), causing `check_misscoped` to always skip.

### ✅ KV Version-Based Cache Invalidation (Done, pending push)

`registry.Register()` now compares `Definition.Version` against the KV-stored version. Skip write on match, update on mismatch. Agents no longer need manual KV clears after workflow file version bumps.

### ✅ Type-Preserving JSON Interpolation (Done)

`InterpolateJSON` now uses recursive JSON walking instead of string-based regex substitution. Non-scalar values (objects, arrays, numbers) are preserved as native JSON types.

**Behavior:**
- **Pure interpolation** (`"items": "${path}"`) — preserves native type (array stays array, object stays object)
- **Embedded interpolation** (`"msg": "Found ${path} items"`) — string concatenation (existing behavior)

**Hybrid type extraction:** The workflow executor now tries to unwrap `BaseMessage`-wrapped responses via `tryUnwrapBaseMessage()`. When successful, it stores `OutputType` metadata on `StepResult` for downstream type-aware processing. Semspec components wrap results in `BaseMessage` (via `workflow/callback.go`) when the output implements `message.Payload`.

**Migration applied:**
- `PlanReviewTrigger.PlanContent` changed from `string` to `json.RawMessage` — now correctly receives the planner's structured plan object instead of a broken stringified version
- `ScopePatterns []string` — already correct type, arrays now preserved
- Numeric comparisons (`${execution.iteration}` with `lt`/`gt`/`eq`) — condition operators already handle numeric types correctly

## Implementation Phases

### Phase 1: Fix pub/sub semantics ✅
Converted all workflow-result publishes from Core NATS to JetStream across 6 components. Correct delivery guarantees established.

### Phase 2: Semstreams changes ✅

| Item | Status |
|------|--------|
| **a) `publish_async` action type** | ✅ Done |
| **b) `"in"` condition operator** | ✅ Done |
| **c) Type-preserving JSON interpolation** | ✅ Done (semstreams aa1687a, 5f354fc) |
| **d) KV version-based cache invalidation** | ✅ Done |

### Phase 3: Add callback support to semspec component triggers ✅
Added `CallbackFields` (callback_subject, task_id, execution_id) to all trigger/result payloads. All components publish `AsyncStepResult` via callbacks. Backward compatible — fields are optional.

### Phase 4: Create plan-review-loop workflow ✅
Wrote `configs/workflows/plan-review-loop.json` using `publish_async`. Workflow-processor handles the OODA feedback loop. Review findings flow to planner via step interpolation. Note: structured data interpolation (objects/arrays) depends on Phase 2c.

### Phase 5: Refactor HTTP handlers to workflow gateways ✅
Simplified `handleCreatePlan` and `handlePromotePlan` to trigger workflows. `determinePlanStage` reads workflow execution state.

### Phase 6: Generalize the pattern ✅
Applied callback-only pattern to all 5 LLM-calling components (planner, plan-reviewer, task-generator, task-dispatcher, plan-coordinator). No legacy BaseMessage fallback paths.

### Phase 7: BaseMessage wrapping for typed interpolation ✅
Updated `PublishCallbackSuccess` to wrap typed outputs in `BaseMessage` when the result implements `message.Payload`. This enables the workflow executor's hybrid unwrapper to extract type metadata for proper interpolation via the payload registry. All 5 component Result types implement `message.Payload` and are registered in the payload registry.

### Phase 8: Type-preserving interpolation migration ✅
Updated semstreams dependency to include type-preserving `InterpolateJSON` (commits aa1687a, 5f354fc). Migrated `PlanReviewTrigger.PlanContent` from `string` to `json.RawMessage` to accept structured plan objects from pure interpolation. No other migration needed — `ScopePatterns []string` already correct, condition operators already handle numeric types.

## Consequences

### Positive
- Feedback loops work event-driven — review findings automatically flow back to planner
- Workflow definitions are declarative and visible — easier to understand and modify
- Correct pub/sub semantics — JetStream for durability, Core NATS for ephemeral
- Future agents have correct patterns to follow (dogfood value)
- State is in workflow execution KV — observable via existing debug tools
- `max_iterations` handled by workflow-processor, not E2E test retry logic

### Negative
- Workflow definitions add a level of indirection vs. inline Go code

### Risks
- Workflow definitions add a level of indirection — debugging requires understanding both the workflow JSON and component code. Mitigated by workflow debug commands (`/debug workflow`, `/debug trace`).

## Alternatives Considered

### A. HTTP-driven feedback (current plan before this ADR)
Add `POST /plans/:slug/revise` endpoint. E2E test calls promote → revise → promote in a loop.

**Rejected because:** This makes HTTP the orchestration layer. The feedback loop is driven by external retry logic, not event-driven. It works for humans but doesn't close the loop internally.

### B. Plan-coordinator handles revision internally
Extend plan-coordinator to subscribe to review results and re-trigger planners.

**Rejected because:** This couples the coordinator to the reviewer. The workflow-processor already provides generic orchestration with conditions and loops — no need to rebuild it in a component.

### C. New dedicated "plan-lifecycle" component
A component that manages the full plan → review → revise state machine.

**Rejected because:** This is exactly what the workflow-processor does. Building another state machine executor is the anti-pattern we're trying to fix.

## References

- `configs/workflows/plan-and-execute.json` — correct OODA loop pattern for task execution
- `processor/workflow/` in semstreams — workflow-processor implementation
- `processor/workflow/actions/` — available action types
- `processor/workflow/types.go` — `AsyncStepResult` for async callback pattern
- [docs/03-architecture.md](../03-architecture.md) — decision framework for workflows vs. components
