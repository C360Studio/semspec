# ADR-020 Migration Feedback: DX Improvement Proposals

**Status:** Resolved
**Date:** 2026-02-23
**Authors:** Coby (semspec)
**Context:** Post-migration feedback from semspec's ADR-020 adoption
**Resolution:** 3 of 4 proposals implemented by semstreams team; semspec fully migrated

## Context

Semspec completed migration of all three workflow definitions
(`plan-review-loop`, `task-review-loop`, `task-execution-loop`) to
ADR-020's `inputs`/`outputs` pattern, and from a single `workflow.events`
subject to typed per-event subjects using `natsclient.Subject[T]`.

The migration was successful — builds clean, all tests pass. This
document captures what worked well and four concrete DX improvement
proposals based on real-world usage during that migration.

## What Worked Well

**Typed subjects** — `natsclient.Subject[T]` with `.Pattern` for
JetStream consumer switch cases is a clean API. Event structs with proper
types (e.g., `json.RawMessage` for dynamic fields like `findings` instead
of `any`) produce safe, readable consumer code. Dispatching by subject
rather than switching on an embedded string field is unambiguously better.

```go
// Clean switch on typed subject patterns
switch msg.Subject() {
case workflow.PlanApproved.Pattern:
    event, err := workflow.ParseNATSMessage[workflow.PlanApprovedEvent](msg.Data())
    // ...
case workflow.PlanRevisionNeeded.Pattern:
    event, err := workflow.ParseNATSMessage[workflow.PlanRevisionNeededEvent](msg.Data())
    // ...
}
```

**`inputs` fixing stringification (the core ADR-006 problem)** — The
real payoff. `"tasks": {"from": "task_generator.tasks"}` resolving as a
native `[]workflow.Task` array instead of a stringified blob is exactly
the fix that motivated the migration. Every step that moved to `inputs`
immediately dropped the `json.RawMessage` band-aids that had accumulated
on consumer structs.

**`ParseNATSMessage[T]`** — Handles `GenericJSONPayload` envelopes
transparently, covering all three wire formats (`async_task.v1`,
`core.json.v1`, direct `BaseMessage`). No ceremony for consumers.
Callback field injection works reliably for async steps.

**`from` shorthand** — `"planner.content"` resolving to
`steps.planner.output.content` reduces noise considerably in step
`inputs` blocks.

---

## Proposal 1: Template Interpolation in `inputs`

### Problem

When `inputs` is present on a step, it replaces `action.payload` entirely.
There is no way to mix native-type resolution (via `inputs`) with template
string interpolation (via `${...}`) on the same step.

This forced semspec's `revise_planner` and `revise_tasks` steps to stay
entirely on the old `${...}` pattern because their prompts are template
strings with embedded step output references:

```json
{
  "name": "revise_planner",
  "action": {
    "type": "publish_async",
    "subject": "workflow.async.planner",
    "payload": {
      "prompt": "REVISION REQUEST:\n\n## Summary\n${steps.plan_reviewer.output.summary}\n\n## Findings\n${steps.plan_reviewer.output.formatted_findings}\n\nFix ALL issues.",
      "request_id": "${trigger.payload.request_id}",
      "data": {
        "slug": "${trigger.payload.slug}",
        "title": "${trigger.payload.title}"
      }
    }
  }
}
```

This step cannot use `inputs` at all because the `prompt` field needs
template interpolation. As a consequence, `data.slug` still travels
through the old `action.payload` path, meaning the stringification fix
from ADR-020 cannot apply to revision steps.

**Result**: Mixed patterns within a single workflow — some steps use
ADR-020 `inputs`, others use old-style `action.payload` with `${...}`.
This works but is cognitively jarring and undermines the "unified" goal.
The revision steps are second-class citizens in their own workflow files.

### Suggestion

Support a `template` source type in `inputs` alongside `from`:

```json
{
  "name": "revise_planner",
  "inputs": {
    "slug":       {"from": "trigger.payload.slug"},
    "title":      {"from": "trigger.payload.title"},
    "request_id": {"from": "trigger.payload.request_id"},
    "prompt": {
      "template": "REVISION REQUEST:\n\n## Summary\n${steps.plan_reviewer.output.summary}\n\n## Findings\n${steps.plan_reviewer.output.formatted_findings}\n\nFix ALL issues."
    }
  },
  "action": {
    "type": "publish_async",
    "subject": "workflow.async.planner"
  }
}
```

This lets all steps use `inputs` uniformly. Data fields get native
resolution (no stringification). Template fields get string interpolation.
No mixed patterns within a workflow file.

The implementation change is minimal: when an input entry has a `template`
key instead of a `from` key, run the existing `InterpolateString` logic
on the value and assign the result as a string field. The executor already
has both resolution paths — this just exposes them through the same
`inputs` surface.

---

## Proposal 2: Load-Time Subject Validation for `publish` Actions

### Problem

Typed subjects cannot be published directly from workflow definition steps
— workflow `publish` actions always wrap in `GenericJSONPayload` +
`BaseMessage`. So workflow JSON files use string literals like
`"workflow.events.plan.approved"` while Go consumer code references
`workflow.PlanApproved.Pattern`. If someone renames the Go constant, the
JSON silently diverges.

Semspec had to write custom tests to validate that workflow JSON subject
strings match the Go-defined patterns:

```go
// From workflow/subjects_test.go — manually asserting the coupling
assert.Equal(t, "workflow.events.plan.approved", PlanApproved.Pattern)
```

This coupling gap is exactly what typed subjects were supposed to eliminate.
Instead of the type system enforcing the contract, we have a test that
duplicates the contract.

### Suggestion

Add a load-time validation step that cross-references `publish` action
subjects in workflow definitions against registered `Subject[T]` patterns.
A lightweight registry approach:

```go
// In the workflow loader or validator
func validatePublishSubjects(def *Definition, registry SubjectRegistry) []string {
    var warnings []string
    for _, step := range def.Steps {
        if step.Action.Type == "publish" && step.Action.Subject != "" {
            if !registry.IsRegistered(step.Action.Subject) {
                warnings = append(warnings, fmt.Sprintf(
                    "step %q publishes to unregistered subject %q",
                    step.Name, step.Action.Subject))
            }
        }
    }
    return warnings
}
```

This does not need to be a hard error — workflows legitimately publish to
subjects outside the typed system (e.g., `user.signal.escalate`). A
warning at load time would catch drift without breaking anything.

The registration side is already implied by `natsclient.NewSubject[T]`.
Adding a global subject registry that `NewSubject` populates as a side
effect is low-effort and would make the typed subject catalog queryable.

---

## Proposal 3: Built-in `from` Reference Validation

### Problem

Nothing validates that `from` references in `inputs` point to declared
`outputs` of preceding steps. Semspec had to write its own test to catch
this class of error:

```go
// TestWorkflowDefinitionInputsFromRefs_PlanReviewLoop in workflow/trigger_test.go
// Walks every step input, parses the "from" reference, and checks that the
// referenced step+output pair appears in the outputs declarations.
```

A typo like `"from": "planer.content"` (missing second `n`) silently
resolves to nothing at runtime. The `outputs` declarations exist in the
workflow JSON but are not enforced.

### Suggestion

Add `from` reference validation to the existing `validateWorkflow()` in
the schema package. The outline described in ADR-020 is already correct:

```go
// Build output registry from all steps
outputs := map[string]bool{}
for _, step := range def.Steps {
    for name := range step.Outputs {
        outputs[step.Name+"."+name] = true
    }
}

// Validate all input references resolve
for _, step := range def.Steps {
    for inputName, input := range step.Inputs {
        from := input.From
        if from == "" || strings.HasPrefix(from, "trigger.payload.") ||
            strings.HasPrefix(from, "execution.") {
            continue // valid or externally provided
        }
        // Extract step.output from potentially deeper path (e.g. planner.content.scope.include)
        parts := strings.SplitN(from, ".", 3)
        if len(parts) < 2 {
            errs = append(errs, fmt.Errorf(
                "step %q input %q: invalid from reference %q",
                step.Name, inputName, from))
            continue
        }
        stepOutput := parts[0] + "." + parts[1]
        if !outputs[stepOutput] {
            errs = append(errs, fmt.Errorf(
                "step %q input %q: unresolvable reference %q (no declared output %q)",
                step.Name, inputName, from, stepOutput))
        }
    }
}
```

This was described in ADR-020 but not implemented. Adding it to
`schema.Definition.Validate()` would catch typos and stale references at
load time rather than silently failing at runtime. The cost is minimal —
this is a static pass over a small JSON structure.

---

## Proposal 4: `outputs` Declaration Enforcement (Lower Priority)

### Problem

`outputs` declarations are currently documentation only.
`"outputs": {"content": {}}` declares what a step produces, but nothing
validates that the step's actual result contains a `content` field. The
`interface` field on output entries is optional and unused.

Steps can be misconfigured without any indication until a downstream
`from` reference silently resolves to nothing.

### Suggestion

This is lower priority since runtime enforcement across process boundaries
is inherently limited by JSON round-trips and dynamic LLM outputs. But at
minimum, when a step result arrives, the executor could warn if declared
output names are missing from the result map:

```go
// In executor, after step result arrives
if step.Outputs != nil {
    for name := range step.Outputs {
        if _, exists := result[name]; !exists {
            logger.Warn("step result missing declared output",
                "step", step.Name,
                "output", name)
        }
    }
}
```

This is a development-time safety net, not a correctness guarantee. It
would have surfaced several misconfigured steps during the semspec
migration that were only caught by running end-to-end tests.

---

## Resolution Summary

| Proposal | Status | Notes |
|----------|--------|-------|
| Template interpolation in `inputs` | **Implemented** | `template` field on `InputRef` shipped in semstreams. Semspec migrated all 26 remaining steps to `inputs` — zero `action.payload` with `${...}` in any workflow definition. |
| Subject string validation at load time | **Declined** | Semstreams team decided a global subject registry doesn't fit the architecture. Semspec maintains coupling tests in `workflow/subjects_test.go`. |
| Built-in `from` reference validation | **Implemented** | Load-time validation in `schema.Definition.Validate()`. Catches typos and stale refs. |
| `outputs` declaration enforcement | **Implemented** | Runtime warning when step results are missing declared output fields. |

The migration to ADR-020 is now complete. All three semspec workflows
(`plan-review-loop`, `task-review-loop`, `task-execution-loop`) use
`inputs` exclusively for payload construction. The `template` field
eliminated the mixed-pattern reality — revision steps with templated
prompts now sit alongside data-passing steps with no cognitive overhead.
