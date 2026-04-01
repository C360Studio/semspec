# ADR-031: QA Test Execution Stage

**Status:** Proposed
**Date:** 2026-04-01
**Authors:** Coby, Claude
**Depends on:** ADR-030 (QA capability constants), [docs/13-sandbox-security.md](../13-sandbox-security.md)

## Context

BMAD includes Murat (Master Test Architect) whose role is integration and end-to-end testing —
verifying that the assembled system works as a whole, not just that individual units compile. Semspec
has no equivalent stage.

The existing `structural-validator` runs per-task and performs deterministic checklist validation:
it checks that required files exist, that tests were written, and that the build passes. It does not
run test suites across the merged worktree, and it has no understanding of cross-requirement
interactions.

The gap: after all requirements have been implemented and individually reviewed, there is no
plan-level stage that asks "does scenario A's implementation break scenario B?" This question can
only be answered by running the real test suite against the combined result.

### What structural-validator already covers

- Per-task file existence checks
- Per-task build success (`go build ./...`)
- Per-task unit test pass (`go test ./...` scoped to changed packages)
- Deterministic — no LLM involvement

### What is missing

- Cross-requirement interaction testing (full suite, not per-task)
- Integration test execution across the merged worktree
- E2E test execution (if configured for the project)
- LLM-guided test strategy: which suites to run, how to interpret failures, whether a
  failure is a genuine regression or a pre-existing issue

---

## Decision

Add a QA test execution stage at the end of the pipeline, after the rollup review, before
plan completion.

### Pipeline position

```
all scenarios complete → reviewing_rollup → qa_testing → complete
```

New statuses:

- `qa_testing` — qa-executor has claimed `reviewing_rollup_complete`
- `qa_passed` — all test suites passed; plan transitions to `complete`
- `qa_failed` — test suite failures found; qa-executor files findings and plan transitions
  to `qa_failed` (terminal pending human intervention or retry)

### QA agent capabilities

The qa-executor dispatches an agent with `RoleQA` / `CapabilityQA` that:

1. Runs the project's full test suite (`go test ./...`, `npm test`, etc.) via sandbox
2. Runs e2e tests if the project config includes an `e2e_command`
3. Interprets failures: distinguishes regressions introduced by the current plan from
   pre-existing failures
4. Verifies cross-requirement interactions — does scenario A's worktree conflict with
   scenario B's?
5. Produces a structured `QAReport` with pass/fail status, failed test names, and triage notes

The qa-executor has sandbox access for all test execution. Code never runs directly in the
semspec process.

### Relationship to structural-validator

| | structural-validator | qa-executor |
|---|---|---|
| Scope | Per-task | Plan-level (merged worktree) |
| Trigger | After each task completes | After rollup review |
| Test strategy | Deterministic checklist | LLM-guided (which suites, how to triage) |
| LLM involvement | None | Yes (CapabilityQA) |
| Cross-req interaction | No | Yes |
| Build check | Yes | Yes (re-verifies merged result) |

They are complementary. structural-validator is a fast local gate; qa-executor is the final
integration gate before plan completion.

### New component: `processor/qa-executor/`

Follows the standard manager pattern:

```
qa-executor {
    Watches PLAN_STATES for reviewing_rollup_complete
    Claims → qa_testing
    Dispatches agent.task.qa with RoleQA
    Watches AGENT_LOOPS KV for completion
    On pass  → plan.mutation.qa_passed  → plan-manager sets complete
    On fail  → plan.mutation.qa_failed  → plan-manager sets qa_failed
}
```

The qa-executor is a pure dispatcher — it owns no entity state beyond its claim on `qa_testing`.
QA results are stored as `plan.QAReport` (similar to `plan.Architecture`).

### BMAD mapping

Murat (Master Test Architect) maps to the `qa` role in semspec:

- `RoleQA` constant: `prompt/fragment.go` (already added in ADR-030)
- `CapabilityQA` constant: `model/capability.go` (already added in ADR-030)
- Persona: configured per-role in `semspec.json` (same as all other BMAD personas)

---

## What This ADR Does NOT Cover

| Out of scope | Reason |
|---|---|
| qa-executor component implementation | Separate PR after sandbox API expansion |
| Sandbox API expansion for test suite execution | May require new sandbox endpoints; separate spike |
| E2E test infrastructure integration | Depends on project config structure (ADR TBD) |
| CI/CD integration | Out of scope for semspec core |
| Retry loop for qa_failed | Deferred — human review of test failures is the right first step |

---

## Consequences

**Positive:**

- Plans that reach `complete` have passed a full integration test sweep, not just per-task checks
- Cross-requirement regression detection is automated
- BMAD alignment complete — Murat role is now represented
- `RoleQA` and `CapabilityQA` constants are already in place; implementation is additive

**Negative:**

- Additional pipeline stage adds latency before plan completion
- Test suite execution time is project-dependent — large suites may be slow
- `qa_failed` is a new terminal state requiring human intervention (no automated retry in v1)

**Risks:**

- Sandbox API may not support the test execution patterns needed (full suite, e2e commands).
  Mitigated by the pre-implementation sandbox API expansion spike.
- Pre-existing test failures could block every plan regardless of the current change scope.
  The QA agent prompt must explicitly address triage of pre-existing vs regression failures.

---

## Implementation Sequence

1. This ADR — design and scope agreement
2. Sandbox API expansion spike — verify test suite execution is supportable
3. `qa-executor` component — watches `PLAN_STATES`, dispatches QA agent, files results
4. `workflow/types.go` additions — `StatusQATesting`, `StatusQAPassed`, `StatusQAFailed`,
   `QAReport` struct, valid transitions
5. `plan-manager` mutations — `plan.mutation.qa_passed`, `plan.mutation.qa_failed`
6. Pipeline integration testing — `task e2e:mock -- execution-phase` updated to include QA stage

---

## Files (when implemented)

| File | Change |
|---|---|
| `workflow/types.go` | `StatusQATesting`, `StatusQAPassed`, `StatusQAFailed`, `QAReport` struct |
| `processor/plan-manager/mutations.go` | `plan.mutation.qa_passed`, `plan.mutation.qa_failed` handlers |
| `processor/qa-executor/` | New component — standard factory/component/config pattern |
| `prompt/domain/software.go` | QA role fragments |
| `configs/semspec.json` | Register qa-executor component instance |
| `test/e2e/scenarios/execution_phase.go` | Add QA stage assertions |
