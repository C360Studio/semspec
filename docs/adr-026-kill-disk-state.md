# ADR-026: Kill Disk State — KV Twofer Is Truth

**Status**: Implemented
**Date**: 2026-03-24
**Author**: Coby + Claude

## Context

Workflow state (plans, requirements, scenarios, change proposals) lived in `.semspec/*.json` files on disk, mediated by a Manager God object. This reimplemented what NATS KV already provides and bypassed the semstreams platform entirely.

## Decision

Store workflow state as plain JSON in a dedicated KV bucket via `natsclient.KVStore`. Publish key semantic facts to ENTITY_STATES as a write-through side effect for graph/rules visibility. Delete the Manager struct — components call standalone functions directly.

## Architecture

### Storage: Plain JSON in dedicated KV bucket

Complex structs (Plan, Requirement, Scenario, ChangeProposal) are stored as JSON — not as triples. ENTITY_STATES stores triples (flat subject-predicate-object facts). Complex structs don't belong as triples.

```go
// Write: json.Marshal → kv.Put
data, _ := json.Marshal(plan)
kv.Put(ctx, PlanEntityID(slug), data)

// Read: kv.Get → json.Unmarshal
entry, _ := kv.Get(ctx, PlanEntityID(slug))
json.Unmarshal(entry.Value, &plan)
```

No triple conversion, no field-by-field extraction, no data loss on round-trip. Same KV Twofer benefits (state + events via Watch + history via revisions).

### Write-through: ENTITY_STATES side effect

Every Save publishes key semantic triples to ENTITY_STATES for graph queries and rule evaluation:

```go
// Source of truth: dedicated KV bucket
kvPut(ctx, kv, PlanEntityID(plan.Slug), plan)

// Side effect: semantic facts for graph/rules
publishEntitySideEffect(ctx, PlanEntityID(plan.Slug), EntityType, PlanTriples(plan))
```

`PlanTriples` produces key indexed facts (status, slug, project, goal, context). The full struct lives in the dedicated bucket.

### No Manager: Standalone functions

Components call standalone package-level functions with `*natsclient.KVStore`:

```go
plan, err := workflow.LoadPlan(ctx, kv, slug)
err := workflow.SavePlan(ctx, kv, plan)
err := workflow.SetPlanStatus(ctx, kv, plan, workflow.StatusApproved)
reqs, err := workflow.LoadRequirements(ctx, kv, slug)
```

No centralized Manager. Components get KVStore from their natsClient in Start().

### Semstreams patterns enforced

- **KV Twofer**: The write IS the event. KV Watch notifies rules.
- **Facts → KV, Requests → JetStream**: Entity state is a fact (KV). "Generate requirements" is a request (JetStream).
- **natsclient.KVStore**: Direct KV ops — no custom utilities.
- **Dedicated bucket for complex state**: ENTITY_STATES for triples, dedicated buckets for structs.
- **Write-through**: Dedicated bucket = source of truth, ENTITY_STATES = graph visibility.

### What stays on disk (legitimate filesystem use)

- Config: `project.json`, `checklist.json`, `standards.json`
- Constitution: `constitution.md`
- Artifact exports: spec markdown, archive summaries

## Implementation Summary

| Commit | Change |
|--------|--------|
| `4c59d4a` | KV Twofer core — state methods use ENTITY_STATES |
| `b19730c` | Remove graph publish dual-writes (-199 lines) |
| `c3fc3e2` | Remove dead task lifecycle code (-3,424 lines) |
| `80ca158` | Delete Manager struct |
| `8a0ecf3` | Gate KV-dependent tests behind //go:build integration |
| `c3e1475` | Plain JSON KV storage (not EntityState triples) |
| `e7935c8` | Wire ENTITY_STATES write-through side-effect |

## Verification

- `go build ./...` — clean
- `go test ./...` — 56 packages pass (no integration tag)
- `go test -tags integration ./workflow/ -run TestKV_` — 19 tests pass against real NATS testcontainer

## Consequences

- All workflow state requires NATS KV — must be running
- Integration tests use `natsclient.NewTestClient` with testcontainers
- `graph_marshal.go` kept for building side-effect triples
- `graphutil.WriteTriple` (114 operational state calls in orchestrators) flagged for separate cleanup
- Task/TaskStatus types kept in types.go (referenced by prompt templates)
