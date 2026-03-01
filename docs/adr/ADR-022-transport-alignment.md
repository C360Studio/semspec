# ADR-022: Transport Alignment

**Status:** Accepted
**Date:** 2026-03-01
**Authors:** Coby, Claude
**Supersedes:** None
**Context:** Post-audit transport correctness across all semspec components

## Problem Statement

Semspec components use three transport mechanisms — Core NATS, JetStream, and KV — but the
selection rules were not formally documented. Without explicit rules, individual components made
local decisions that were almost correct but contained gaps that caused subtle delivery failures
under load or restart.

The semstreams library has formalized two patterns that govern correct transport selection:

- **KV Twofer** — reactive workflows watch a KV bucket for state. Any trigger that must read
  existing state before acting uses a `TriggerMessageAndState` that atomically delivers the
  inbound message *and* looks up the current KV value. This eliminates the race between message
  arrival and state availability.
- **Streams vs KV Watches** — durable ordered delivery uses JetStream; current-value state uses
  KV watches; ephemeral notifications use Core NATS.

This ADR documents the audit results against those patterns, the gaps found, and the fixes applied.

## Decision Framework

The transport selection rule is a direct consequence of what the data represents:

| Data character | Transport | Rationale |
|----------------|-----------|-----------|
| Current state / facts | KV | Consumers want the latest value, not a history |
| Requests / commands | JetStream | Delivery guarantee, ordering, at-least-once |
| Ephemeral announcements | Core NATS | Latest-value-wins, no durability needed |

A corollary: **Core NATS `Publish()` is asynchronous and buffered**. Messages are flushed later
and may be reordered. Any publish where subsequent logic assumes the message was delivered must
use JetStream publish, which blocks until the stream acknowledges receipt.

## Audit Results

The following table shows what each component uses and whether it is correct after fixes.

| Component | State transport | Trigger transport | Result publish | Verdict |
|-----------|----------------|-------------------|----------------|---------|
| reactive-workflow | KV (twofer) | JetStream | KV transition | Correct |
| planner | `StateManager.Transition()` → KV | JetStream consumer | KV transition | Correct |
| plan-reviewer | `StateManager.Transition()` → KV | JetStream consumer | KV transition | Correct |
| context-builder | KV (`CONTEXT_RESPONSES`) | JetStream requests | JetStream responses | Correct |
| task-generator | — | JetStream consumer | KV transition | Correct |
| task-dispatcher | — | JetStream consumer | JetStream publish | Fixed |
| plan-coordinator | In-memory map | JetStream consumer | JetStream publish | Fixed |
| source-ingester | — | JetStream consumer | Graph ingest | Correct |
| ast-indexer | — | JetStream consumer | Graph ingest | Correct |
| tool heartbeats | Core NATS | Core NATS | — | Correct |

### What is correct

**Reactive engine twofer** — The semstreams reactive engine correctly implements the KV Twofer
for all workflow participants. When a JetStream message arrives, the engine atomically fetches the
current KV state before invoking rule conditions. Components that implement `Participant` get this
behavior for free via `StateManager`.

**Trigger subjects on JetStream** — Every workflow trigger uses a JetStream consumer with
`AckExplicitPolicy`. Unprocessed triggers survive component restarts.

**Context-builder request/response** — Requests arrive on JetStream (`context.build.<strategy>`).
Responses are written to KV (`CONTEXT_RESPONSES` bucket). Consumers fetch the KV entry directly.
This is the correct pattern: the request has delivery semantics, the response has current-value
semantics.

**`StateManager.Transition()` for participant pattern** — Planner and plan-reviewer write all
state changes through `StateManager.Transition()`, which publishes to KV. The reactive engine
watches KV and fires rules. Components never need to publish to JetStream for their own state.

## Gaps Found and Fixed

### Gap 1: plan-coordinator and task-dispatcher used Core NATS for result publish

Both components called `natsClient.Publish()` (Core NATS) to emit their completion signals:

```go
// BEFORE - Core NATS: fire-and-forget, no delivery guarantee
if err := c.natsClient.Publish(ctx, resultSubject, data); err != nil {
    return fmt.Errorf("publish result: %w", err)
}
// Returns immediately; message may never reach the stream
```

The result subjects (`workflow.result.plan-coordinator.*`, `workflow.result.task-dispatcher.*`)
are in the `WORKFLOW` JetStream stream. Core NATS `Publish()` is asynchronous — it queues into
a client-side buffer and flushes later. Under concurrent load, the flush can be reordered with
respect to the ACK on the inbound trigger message. Downstream consumers that watch for the
result subject see nothing.

**Fix:** Both components now use JetStream publish for result notifications:

```go
// AFTER - JetStream: blocks until stream acknowledges receipt
js, err := c.natsClient.JetStream()
if err != nil {
    return fmt.Errorf("get jetstream for result: %w", err)
}
if _, err := js.Publish(ctx, resultSubject, data); err != nil {
    return fmt.Errorf("publish result: %w", err)
}
// Message is confirmed in the stream before this returns
```

This is consistent with the fix documented in ADR-005 Phase 1, which converted the same
anti-pattern across six other components.

### Gap 2: plan-coordinator held session state in memory

The plan-coordinator tracked concurrent planner sessions in a `map[string]*workflow.PlanSession`
guarded by a `sync.RWMutex`:

```go
type Component struct {
    sessions   map[string]*workflow.PlanSession
    sessionsMu sync.RWMutex
    // ...
}
```

This is purely in-memory. A process restart during a multi-planner coordination session — which
can take 60–120 seconds per planner — drops all in-flight planner results. The LLM tokens spent
on those calls are wasted and the trigger message is NAK'd back to the stream for retry, which
restarts the entire session from scratch.

**Fix:** Migrate plan-coordinator to a reactive workflow with KV-backed session state. The
coordination session becomes a `workflow.Participant` that writes transitions via
`StateManager.Transition()`. The reactive engine watches KV and resumes correctly after restart
because state is durable in the KV bucket.

This also aligns plan-coordinator with the established `Participant` pattern that planner and
plan-reviewer already use. The in-memory `sessions` map is removed entirely.

## Correct Patterns: Reference Summary

### JetStream publish for results and commands

```go
js, err := c.natsClient.JetStream()
if err != nil {
    return fmt.Errorf("get jetstream: %w", err)
}
if _, err := js.Publish(ctx, subject, data); err != nil {
    return fmt.Errorf("publish: %w", err)
}
```

### KV for state (via StateManager)

```go
// StateManager.Transition() handles KV write and reactive engine notification
if err := c.stateManager.Transition(ctx, state, phases.PlannerDone); err != nil {
    return fmt.Errorf("transition state: %w", err)
}
```

### Core NATS for ephemeral announcements only

```go
// Heartbeat: fine as Core NATS — ephemeral, latest-value-wins
c.natsClient.Publish(ctx, "tool.heartbeat.semspec", heartbeat)
```

## Consequences

### Positive

- **Delivery correctness** — Result notifications from plan-coordinator and task-dispatcher
  are now confirmed before the inbound ACK, eliminating the lost-result race.
- **Crash recovery** — Migrating plan-coordinator to KV-backed state means a restart during
  a long coordination session resumes from the last committed state rather than starting over.
- **Pattern consistency** — All components that emit workflow results now use JetStream publish,
  matching the rule established in ADR-005.

### Negative

- **plan-coordinator complexity** — Migrating from in-memory coordination to reactive workflow
  adds KV state management. The `Participant` interface provides the structure, but the
  migration is non-trivial.

### Risks

- **KV session cardinality** — Each active coordination session writes KV entries. With many
  concurrent plans, KV bucket size grows. Mitigated by TTL on session entries and cleanup
  on completion.

## Alternatives Considered

### A. Keep in-memory state, add crash recovery via checkpoint

Write a checkpoint to KV at each phase boundary (after focus-area selection, after each planner
result, after synthesis). On startup, scan KV for incomplete sessions and resume.

**Rejected because:** This replicates the reactive workflow pattern ad-hoc. The `Participant`
interface provides this structure correctly. Building a parallel pattern increases surface area
for divergence.

### B. Make plan-coordinator stateless by serializing to the trigger payload

Pass all intermediate planner results back through the JetStream message, growing the payload
across retries.

**Rejected because:** NATS JetStream has a maximum message size (default 1MB). Multi-planner
results with full plan content can exceed this. Payload growth also means the trigger message
contains derived data, violating the principle that facts belong in KV.

## References

- ADR-005 — OODA Feedback Loops (established the JetStream-over-Core-NATS rule for results)
- Semstreams "KV Twofer and Streams vs KV Watches" pattern documentation
- `processor/plan-coordinator/component.go` — Component implementation with sessions map
- `processor/task-dispatcher/component.go` — `publishResult()` method (Gap 1 fix location)
- `workflow/phases/` — Phase constants used by `StateManager.Transition()`
