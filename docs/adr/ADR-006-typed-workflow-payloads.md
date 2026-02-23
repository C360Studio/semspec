# ADR-006: Typed Workflow Payloads

**Status:** Proposed
**Date:** 2026-02-23
**Authors:** Coby, Claude
**Supersedes:** None
**Context:** Persistent stringification bugs in workflow inter-step data passing

## Problem Statement

The semstreams workflow-processor passes data between steps via string interpolation of opaque JSON
(`${steps.X.output.Y}`). This approach has produced three distinct data corruption bugs in semspec
workflows, all sharing the same root cause.

### Specific Evidence

**1. `findings` field stringification** — The plan-reviewer's `findings` (a `[]TaskReviewFinding`
array) was JSON-stringified during interpolation from one workflow step to the next. When the
workflow-api consumed the `workflow.events` message, it could not unmarshal the string back into the
expected type. Fix: changed the Go field from `string` to `json.RawMessage` (a band-aid).

**2. `tasks` field stringification** — The task-generator produces `tasks: []workflow.Task`. The
task-review-loop passes `"tasks": "${steps.task_generator.output.tasks}"` to the task-reviewer.
Error: `json: cannot unmarshal string into Go struct field Alias.tasks of type []workflow.Task`. The
array became a string during KV storage round-tripping of the Execution state.

**3. `plan_content` stringification** — The planner's structured plan JSON was stringified when
passed to the plan-reviewer via `${steps.planner.output.content}`. Required migrating `PlanContent`
from `string` to `json.RawMessage`.

All three bugs have the same root cause: the workflow system treats step outputs as opaque JSON
blobs, serializes them through string interpolation and KV storage, and type information is lost in
the process. ADR-005 Phase 8 added type-preserving interpolation, which works for pure expressions
(`"field": "${path}"` preserves native types). But the system remains fragile because:

- **KV round-tripping** — Execution state is serialized to NATS KV between steps. `json.RawMessage`
  fields can be double-escaped during marshal→store→load→unmarshal cycles.
- **BaseMessage wrapping** — Components wrap outputs in BaseMessage. The `tryUnwrapBaseMessage`
  helper extracts the payload, but this is implicit and error-prone.
- **No type contract** — The workflow JSON definition does not declare what types flow between
  steps. The consumer component discovers the shape of its input at runtime, with no compile-time
  or load-time validation.

### The Architectural Insight

**Workflows are mini-flows inside a larger flow, and we control both sides.**

The payload registry already exists — every component registers its trigger and result types. The
workflow system knows (or could know) that step `planner` produces `workflow.task-generator-result.v1`
and step `task_reviewer` consumes `workflow.task-review-trigger.v1`.

Yet the current system follows this path for every inter-step payload:

1. Serialize the planner's typed `Result` struct to JSON
2. Wrap it in BaseMessage for type metadata
3. Workflow executor unwraps BaseMessage, stores raw JSON in `StepResult.Output`
4. Interpolate `${steps.planner.output.tasks}` by walking the raw JSON
5. Build a new JSON blob for the next step
6. Wrap it in `AsyncTaskPayload.Data`
7. Wrap that in BaseMessage
8. Publish to NATS
9. Consumer unwraps BaseMessage → unwraps AsyncTaskPayload → unmarshals into its typed trigger

That is 9 serialization boundaries. Every one is a place where types can be lost. The payload
registry makes most of this unnecessary.

## Decision

### 1. Add `input_type` and `output_type` to workflow step definitions

Workflow JSON steps declare the registered payload types they consume and produce:

```json
{
  "name": "task_generator",
  "input_type": "workflow.trigger.v1",
  "output_type": "workflow.task-generator-result.v1",
  "action": {
    "type": "publish_async",
    "subject": "workflow.async.task-generator",
    "payload_mapping": {
      "request_id": "${trigger.payload.request_id}",
      "slug": "${trigger.payload.slug}",
      "prompt": "${trigger.payload.prompt}"
    }
  }
}
```

When `output_type` is declared, the executor uses the payload registry to deserialize step output
into the correct Go type before storing it. When `input_type` is declared, the executor can validate
the assembled payload before publishing.

### 2. Add `payload_mapping` as a typed alternative to `payload`

The current `payload` field is a raw JSON template with `${...}` expressions. This is the source
of all the stringification bugs.

`payload_mapping` is a new field that maps target fields to source paths. The executor resolves
each path against typed step results and assembles the output payload using the registered type's
structure:

```json
"payload_mapping": {
  "request_id": "${trigger.payload.request_id}",
  "trace_id": "${trigger.payload.trace_id}",
  "slug": "${trigger.payload.slug}",
  "tasks": "${steps.task_generator.output.tasks}",
  "scope_patterns": "${trigger.payload.scope_patterns}"
}
```

The key difference from `payload`: when `input_type` is declared, the executor knows `tasks` should
be `[]workflow.Task` (from the registered type's schema). It can validate and preserve the type
during assembly. When `input_type` is not declared, `payload_mapping` behaves identically to
`payload` (backward compatible).

### 3. Add `pass_through` for direct field forwarding

For the common case where a step's output fields map 1:1 to the next step's input fields, support
`pass_through`:

```json
{
  "name": "task_reviewer",
  "action": {
    "type": "publish_async",
    "subject": "workflow.async.task-reviewer",
    "pass_through": ["request_id", "trace_id", "slug"],
    "payload_mapping": {
      "tasks": "${steps.task_generator.output.tasks}",
      "scope_patterns": "${trigger.payload.scope_patterns}"
    }
  }
}
```

`pass_through` fields are copied from the trigger payload without serialization.

### 4. Validate payload contracts at workflow load time

When the workflow-processor loads a workflow definition, if `input_type` and `output_type` are
declared, validate:

- The types are registered in the payload registry
- `payload_mapping` keys exist in the target type (warn on unknown fields)
- Source paths reference valid step names

This catches wiring errors at startup instead of at runtime.

### 5. Preserve backward compatibility

- `payload` (raw JSON template) continues to work unchanged
- `payload_mapping` is opt-in per step
- `input_type` and `output_type` are optional annotations
- Existing workflows work without modification
- Migration is incremental: convert one step at a time

## Semstreams Changes

### a) Step schema: `input_type`, `output_type`, `payload_mapping`, `pass_through`

Extend `wfschema.StepDef` and `wfschema.ActionDef`:

```go
type StepDef struct {
    Name       string    `json:"name"`
    InputType  string    `json:"input_type,omitempty"`  // registered payload type
    OutputType string    `json:"output_type,omitempty"` // registered payload type
    Action     ActionDef `json:"action"`
    // ... existing fields
}

type ActionDef struct {
    Type           string            `json:"type"`
    Subject        string            `json:"subject,omitempty"`
    Payload        json.RawMessage   `json:"payload,omitempty"`         // existing: raw template
    PayloadMapping map[string]string `json:"payload_mapping,omitempty"` // new: typed field mapping
    PassThrough    []string          `json:"pass_through,omitempty"`    // new: forwarded fields
    // ... existing fields
}
```

### b) Typed step result storage

When `output_type` is declared and the step output matches the registered type, store the
deserialized Go value alongside the raw JSON. This avoids re-parsing from `json.RawMessage` on
each interpolation access:

```go
type StepResult struct {
    // ... existing fields
    Output     json.RawMessage `json:"output,omitempty"`
    OutputType *message.Type   `json:"output_type,omitempty"`
    // New: typed output for registered payload types (not serialized to KV)
    TypedOutput message.Payload `json:"-"`
}
```

### c) Typed payload assembly in executor

When `payload_mapping` is used with a declared `input_type`:

1. Create the target payload via the registry factory
2. For each mapping entry, resolve the source path against typed step results
3. Set the field on the target payload using reflection or a field setter interface
4. Validate the assembled payload via its `Validate()` method
5. Marshal to JSON for NATS publish

### d) Workflow validation at load time

Add `ValidatePayloadContracts()` to the workflow loader. For each step with declared types:

- Check types exist in the payload registry
- Warn on `payload_mapping` keys that do not match target type fields
- Validate step chain: output_type of step N should be compatible with input expectations of
  step N's consumers

## Implementation Phases

### Phase 1: Schema extension (semstreams)

Add `input_type`, `output_type`, `payload_mapping`, `pass_through` to step and action schema.
Pure additions — no behavioral changes. Existing workflows unchanged.

### Phase 2: Typed payload assembly (semstreams)

Implement `payload_mapping` resolution in the executor. When `input_type` is declared, use the
registry to create typed payloads. When not declared, fall back to existing JSON interpolation.

### Phase 3: Load-time validation (semstreams)

Add workflow contract validation. Warn (do not fail) on type mismatches to allow incremental
adoption.

### Phase 4: Migrate plan-review-loop (semspec)

Convert `plan-review-loop.json` from `payload` to `payload_mapping` with declared types. This
is the simplest workflow (2 async steps).

### Phase 5: Migrate task-review-loop (semspec)

Convert `task-review-loop.json`. This exercises the `tasks: []workflow.Task` array that caused
the stringification bug described in evidence item 2.

### Phase 6: Migrate task-execution-loop (semspec)

Convert the most complex workflow. Exercises agent dispatch, structural validation, and reviewer
feedback chains.

### Phase 7: Deprecate raw `payload` for async steps

Mark `payload` as deprecated for `publish_async` steps (still supported for `publish` fire-and-
forget events where typing is less critical). Log warnings when used.

## Consequences

### Positive

- **Eliminates stringification bugs** — typed payloads do not go through string interpolation
- **Compile-time-like safety** — payload contracts validated at workflow load time
- **Fewer serialization boundaries** — typed outputs avoid the
  marshal→store→load→unmarshal→interpolate→marshal→publish→unmarshal chain
- **Self-documenting workflows** — `input_type` and `output_type` declare the data contract
  between steps
- **Incremental adoption** — existing workflows work unchanged; migrate one step at a time

### Negative

- **Reflection or interface overhead** — assembling typed payloads from mappings requires either
  reflection or a field-setter interface on payload types
- **Semstreams complexity** — workflow-processor gains type awareness, increasing its scope
- **Two payload assembly paths** — `payload` (raw template) and `payload_mapping` (typed)
  coexist during migration

### Risks

- **Payload registry must be complete** — all types used in workflows must be registered. Missing
  registrations cause load-time errors. Mitigated by Phase 3 validation with warnings (not
  failures).
- **Field setter interface design** — reflection-based field setting is fragile. May need a
  `PayloadBuilder` interface that payload types implement. Design TBD in Phase 2.

## Alternatives Considered

### A. Fix interpolation to never stringify (status quo++)

Improve `InterpolateJSON` and KV serialization to handle all edge cases.

**Rejected because:** This is whack-a-mole. Every new structured field type is a potential
stringification bug. The fundamental issue is that the system does not know the types it is moving
between steps.

### B. Use protobuf/MessagePack for step outputs

Binary serialization with schemas would eliminate JSON round-trip issues.

**Rejected because:** Overengineered for this system. JSON is the lingua franca of NATS messages.
The issue is not JSON itself — it is untyped JSON being processed through string templates.

### C. Pass step outputs by reference (object store)

Store large outputs in NATS Object Store, pass references between steps.

**Rejected because:** Solves the size problem (on roadmap) but not the type problem. References
still need to be deserialized into the correct type at the consumer.

### D. Eliminate workflow JSON, use Go code for orchestration

Replace declarative workflow definitions with imperative Go orchestration code.

**Rejected because:** Loses the declarative benefits (visibility, modifiability, debug tooling).
The workflow-processor's step routing, condition evaluation, and loop handling are valuable. The
issue is limited to payload passing, not orchestration.

## References

- ADR-005: OODA Feedback Loops via Workflow-Processor (established the current async callback
  pattern)
- `processor/workflow/interpolate.go` in semstreams — current interpolation implementation
- `processor/workflow/executor.go` in semstreams — `tryUnwrapBaseMessage`, `buildStepResult`
- `processor/workflow/actions/publish_async.go` in semstreams — `AsyncTaskPayload` wrapping
- `workflow/callback.go` in semspec — `PublishCallbackSuccess` with BaseMessage wrapping
- `workflow/trigger.go` in semspec — `ParseNATSMessage` generic unwrapper
