# ADR-021: Test Verifier Agent

**Status:** Proposed
**Date:** 2026-02-28
**Authors:** Coby, Claude
**Supersedes:** None
**Context:** Need for independent integration test generation without enforcing TDD on developers

## Problem Statement

The current task execution workflow lacks independent verification of cross-boundary code changes.
Developers write their own unit tests, but there is no systematic way to verify that changes
integrate correctly with the rest of the system.

### Current Pain Points

1. **TDD enforcement is burdensome** — Forcing developers to write tests before implementation
   adds friction without always providing value (e.g., documentation tasks, simple refactors).

2. **Same-context testing is limited** — When the developer agent writes both code and tests,
   it has the same context and assumptions. Bugs in reasoning propagate to both.

3. **Integration gaps** — Unit tests verify internal correctness but not boundary behavior.
   Cross-package, cross-service, and API contract issues slip through.

4. **No independent verification** — The structural-validator runs deterministic checks (linting,
   formatting), but there is no agent-based verification with different context.

### The Tension Principle

When two agents work from different contexts, they create productive tension:

- **Developer agent**: Has full implementation context, understands the "how" and "why"
- **Verifier agent**: Has only task spec and code output, must infer correct behavior from spec

If the task spec was ambiguous, the verifier might write tests that fail — surfacing the gap
between intent and implementation. This is a feature, not a bug.

## Decision

### 1. Add `RequiresVerification` field to Task

```go
type Task struct {
    // ... existing fields

    // RequiresVerification indicates this task needs integration testing by the verifier agent.
    // Set by task-generator based on boundary-crossing heuristics.
    // Editable by user during task approval.
    RequiresVerification bool `json:"requires_verification,omitempty"`
}
```

### 2. Task-generator sets the flag using heuristics

The task-generator LLM prompt includes instructions to set `requires_verification: true` when
the task exhibits boundary-crossing characteristics:

| Heuristic | Example |
|-----------|---------|
| Multiple packages | Task modifies files in `processor/planner/` and `processor/task-generator/` |
| API contract changes | Task adds/modifies HTTP handlers, NATS subjects, or GraphQL resolvers |
| Shared type modifications | Task changes types in `workflow/types.go` used by multiple components |
| Cross-service calls | Task adds calls between components (e.g., context-builder → graph-gateway) |
| Database schema changes | Task modifies migrations or entity structures |
| Configuration changes | Task changes `configs/semspec.json` structure affecting multiple components |

The LLM is instructed to err on the side of setting the flag — false positives (unnecessary
verification) are cheaper than false negatives (missed integration bugs).

### 3. Human can override during task approval

During the task approval step (`/approve tasks`), the user can:

- View which tasks have `requires_verification: true`
- Toggle the flag for individual tasks
- Bulk-enable or bulk-disable verification

This respects developer autonomy while providing a safety net by default.

### 4. Test-verifier component

A new component `test-verifier` runs as part of the task execution workflow:

```
developing → validated (structural) → verified (test-verifier) → reviewed → evaluated
```

**Trigger conditions:**
- Structural validation passed
- `task.RequiresVerification == true`

**Context isolation:**
The verifier requests its own context via `context-builder` with strategy `test-verification`:

| Included | Excluded |
|----------|----------|
| Task description and acceptance criteria | Developer's reasoning/chain-of-thought |
| Code diff (files modified by developer) | Full file history |
| Test framework docs (go test, vitest) | Implementation context docs |
| Existing test patterns in codebase | SOPs (those are for planning, not testing) |

**Output:**
- Integration test file(s) written to `test/integration/` or adjacent to modified code
- Test execution results (pass/fail with output)
- `VerificationResult` payload with verdict: `passed`, `failed`, `skipped`

### 5. Workflow integration

The test-verifier participates in the `task-execution-loop` reactive workflow:

```json
{
  "name": "test-verifier",
  "type": "processor",
  "enabled": true,
  "config": {
    "stream_name": "WORKFLOW",
    "consumer_name": "test-verifier",
    "trigger_subject": "workflow.async.test-verifier",
    "result_subject_prefix": "workflow.result.test-verifier",
    "llm_timeout": "180s",
    "default_capability": "coding",
    "context_subject_prefix": "context.build",
    "context_response_bucket": "CONTEXT_RESPONSES",
    "context_timeout": "60s"
  }
}
```

New workflow phases:

```go
const (
    TaskExecVerifying          = "verifying"
    TaskExecVerifyingDispatched = "verifying_dispatched"
    TaskExecVerified           = "verified"
    TaskExecVerificationSkipped = "verification_skipped"
    TaskExecVerificationFailed  = "verification_failed"
)
```

**State transitions:**

```
validated → verifying (if requires_verification)
validated → verification_skipped (if !requires_verification)
verifying → verifying_dispatched → verified (tests pass)
verifying → verifying_dispatched → verification_failed (tests fail)
verified → reviewing
verification_skipped → reviewing
verification_failed → revision (back to developer with feedback)
```

### 6. Failure handling

When verification fails:

1. Test output and failure details are captured in `task.VerificationFeedback`
2. Task transitions to revision state (back to developer)
3. Developer sees: "Integration tests failed: [specific failures]"
4. Developer can fix code or dispute the test (escalation path)

Escalation after N failed verification attempts triggers human review with both developer
output and verifier tests visible.

## Implementation Phases

### Phase 1: Task schema extension

Add `RequiresVerification` field to `workflow.Task`. Update task-generator prompt to set the
flag based on heuristics. No workflow changes yet.

### Phase 2: Test-verifier component

Create `processor/test-verifier/` following the structural-validator pattern:
- Implement `Participant` interface
- Request test-verification context
- Generate integration tests via LLM
- Execute tests and capture results
- Publish `VerificationResult`

### Phase 3: Workflow integration

Add verification phases to `task-execution-loop`. Wire test-verifier as workflow participant.
Handle skip path for tasks without verification requirement.

### Phase 4: Context strategy

Add `test-verification` strategy to context-builder:
- Include task spec, acceptance criteria, code diff
- Include test framework docs and existing test patterns
- Exclude developer context, SOPs, full implementation docs

### Phase 5: UI integration

Update workflow-api to expose verification status. Add verification toggle to task approval UI.
Show verification results in task detail view.

## Consequences

### Positive

- **Independent verification** — Catches integration bugs that same-context testing misses
- **Context isolation** — Verifier's different perspective surfaces spec ambiguities
- **Opt-in complexity** — Simple tasks skip verification; complex tasks get extra scrutiny
- **Human override** — Developers retain control over verification scope
- **No forced TDD** — Developers write unit tests as they see fit; verifier handles integration

### Negative

- **Additional LLM calls** — Verification adds latency and cost to complex tasks
- **Test maintenance** — Generated integration tests may need human curation
- **False positives** — Verifier may write tests based on misunderstanding the spec

### Risks

- **Heuristic accuracy** — Task-generator may misjudge which tasks need verification.
  Mitigated by erring toward true and allowing human override.

- **Test quality** — LLM-generated tests may be brittle or test implementation details.
  Mitigated by providing good test patterns in context and reviewing generated tests.

- **Workflow complexity** — Adding another async step increases execution time.
  Mitigated by skip path for simple tasks.

## Alternatives Considered

### A. Enforce TDD for all tasks

Require developers to write tests before implementation.

**Rejected because:** Adds friction without value for many task types (docs, config changes,
simple refactors). Developers resist mandated process.

### B. Developer writes integration tests too

Expand developer's scope to include integration tests.

**Rejected because:** Same-context problem. Developer's assumptions propagate to tests.
No independent verification.

### C. Human-only code review

Rely on human reviewers to catch integration issues.

**Rejected because:** Humans miss subtle integration bugs. Review fatigue. Not scalable.

### D. Post-merge integration testing

Run integration tests after code is merged.

**Rejected because:** Catches issues too late. Merge conflicts. Harder to attribute failures.

## References

- `processor/structural-validator/` — Pattern for workflow participant validation
- `processor/task-reviewer/` — Pattern for LLM-based review with context isolation
- `workflow/types.go` — Task struct definition
- ADR-006 — Typed workflow payloads (context for workflow integration)
