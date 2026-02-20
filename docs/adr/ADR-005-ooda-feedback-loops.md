# ADR-005: OODA Feedback Loops via Workflow-Processor

**Status:** Draft
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

## Semstreams API Change: `publish_async` Action Type

The existing `publish_agent` action type is misnamed — the async callback pattern it implements is not agent-specific. It:
1. Publishes a message to a subject
2. Injects `callback_subject` into the payload
3. Parks the workflow execution until `AsyncStepResult` arrives on the callback

The only agent-specific part is constructing an `agentic.TaskMessage` with role/model/prompt fields.

**Proposal:** Add `publish_async` action type to semstreams that does the same publish-and-park but with an arbitrary payload (no `TaskMessage` construction). This lets any component participate as an async workflow step.

**Changes to semstreams (3 files):**

1. `processor/workflow/schema/schema.go` — Add `"publish_async"` to `validTypes`
2. `processor/workflow/actions/publish_async.go` — New action:
   - Takes `Subject` + `Payload` from `ActionDef`
   - Generates `task_id`, injects `task_id` and `callback_subject` into payload
   - Publishes via JetStream (`PublishToStream`)
   - Returns `task_id` in output for correlation
3. `processor/workflow/executor.go` — Add `case "publish_async"` using same park-and-wait as `publish_agent` (lines 262-282)

**Receiving component contract:**
- Component trigger payload may contain `callback_subject` and `task_id`
- If present, component publishes `AsyncStepResult` to `callback_subject` when done
- `AsyncStepResult.Output` carries the component's structured result (e.g., plan content, review findings)
- The workflow-processor's interpolation makes this available as `${steps.<name>.output.*}`

**Backward compatibility:** `publish_agent` remains unchanged. `publish_async` is additive. Components without callback support ignore the fields and behave as before (legacy callers like HTTP handlers still work).

**Bug — missing `"in"` condition operator:** The `"in"` operator is used in `plan-and-execute.json` (line 75: `check_misscoped` step) but not implemented in semstreams `interpolate.go:EvaluateCondition`. The `default` case at line 355 returns an error, which the executor treats as condition=false (fail-safe skip at `executor.go:166-172`). This means rejection types `["misscoped", "architectural"]` silently skip the `check_misscoped` step and fall through to `escalate`. **This must be fixed in semstreams Phase 2** alongside `publish_async`.

Affected workflow steps in `plan-and-execute.json`:
- `check_misscoped` (line 71-89): `"operator": "in", "value": ["misscoped", "architectural"]` — always skipped
- `check_too_big` is reached but uses `"eq"` so it works correctly

Schema validation (`schema.go:290`) also doesn't include `"in"` in valid operators, so this should have been caught at workflow load time but wasn't (the `validOperators` map is only checked in `ConditionDef.Validate()`, not in `EvaluateCondition`).

## Implementation Phases

### Phase 1: Fix pub/sub semantics (low risk, high value)
Convert all workflow-result publishes from Core NATS to JetStream. This is a mechanical change — replace `c.natsClient.Publish()` with `js.Publish()` in 6 components. No behavioral change, but establishes correct delivery guarantees.

### Phase 2: Semstreams changes (low-medium risk)
Two additive changes to semstreams:

**a) `publish_async` action type** — 3 files, no existing behavior modified. Also a good time to add a deprecation note on `publish_agent` pointing to the two actions (`publish_async` for components, `publish_agent` for agentic-loop convenience).

**b) `"in"` condition operator** — Add to `interpolate.go:EvaluateCondition` and `schema.go` valid operators. This is a bug fix — `plan-and-execute.json` already uses it but the operator silently fails, causing `check_misscoped` to always skip. Implementation: check if `cond.Value` is a `[]any`, iterate and `compareEqual` each element against the resolved value.

### Phase 3: Add callback support to semspec component triggers (medium risk)
Add optional `callback_subject` and `task_id` fields to `TriggerPayload`. Update planner and plan-reviewer to check for callback and publish `AsyncStepResult` when present. Backward compatible — fields are optional, existing callers unaffected.

### Phase 4: Create plan-review-loop workflow (medium risk)
Write `configs/workflows/plan-review-loop.json` using `publish_async` to trigger planner and plan-reviewer components. The workflow-processor's condition evaluation and loop handling drive the OODA feedback loop. Review findings flow to the planner via `${steps.plan-reviewer.output.findings}`.

### Phase 5: Refactor HTTP handlers to workflow gateways (higher risk)
Simplify `handleCreatePlan` and `handlePromotePlan` to trigger workflows instead of manually orchestrating. The `determinePlanStage` function reads workflow execution state instead of inferring from plan booleans.

### Phase 6: Generalize the pattern
Apply the same approach to task execution (already partially correct via `plan-and-execute.json`), source ingestion review, and any future LLM-with-review flows.

## Consequences

### Positive
- Feedback loops work event-driven — review findings automatically flow back to planner
- Workflow definitions are declarative and visible — easier to understand and modify
- Correct pub/sub semantics — JetStream for durability, Core NATS for ephemeral
- Future agents have correct patterns to follow (dogfood value)
- State is in workflow execution KV — observable via existing debug tools
- `max_iterations` handled by workflow-processor, not E2E test retry logic

### Negative
- Requires a small semstreams change (`publish_async` action type) — but it's additive and generic
- Components need callback support — requires adding optional fields to trigger payloads
- Phase 5 is a significant refactor of workflow-api HTTP handlers
- Workflow definitions add a level of indirection vs. inline Go code

### Risks
- Workflow interpolation operates on `json.RawMessage` output — component results need to serialize cleanly for `${steps.<name>.output.*}` to work

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
