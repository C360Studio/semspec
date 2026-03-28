# Bug: requirement-executor doesn't process TDD pipeline completion events

**Severity**: Blocker — plans stuck at `implementing` forever
**Component**: `requirement-executor` (`processor/requirement-executor/`)
**Found during**: UI E2E T2 @easy with Gemini (2026-03-28, run 4)
**Status**: PARTIALLY FIXED

## Summary

All 4 TDD pipelines complete successfully (`Published TDD pipeline completion event`
with `outcome=success`), but requirement-executor never processes these completions.
Requirements stay at `executing`, plan stuck at `implementing`.

## Root Causes (two separate issues)

### 1. EXECUTION_STATES watcher race — FIXED (22d70df)

plan-manager's `watchExecutionCompletions` gave up permanently if the EXECUTION_STATES
bucket wasn't created yet by execution-manager (3ms startup gap). Fixed with
`retry.Quick()` in `processor/plan-manager/execution_events.go`.

**Before**: watcher logged "plan completion watcher disabled" and never started.
**After**: retries up to 10 times with exponential backoff, watcher starts reliably.

This was the blocker for the requirement-retry E2E. After the fix, single-requirement
plans complete correctly: implementing → complete.

### 2. Mock fixture counter concurrency — OPEN (code-execution variant only)

The mock LLM uses a per-model call counter. When multiple requirements execute
concurrently, they all share the same `mock-coder` counter. With 3 requirements
each needing different fixture sequences, they race and get wrong responses:

- Requirement 1's decomposer gets requirement 2's builder fixture
- Requirement 2's tester gets requirement 3's reviewer fixture
- Net effect: requirement 1 never completes (wrong tool call in response)

**Workaround**: Single-requirement scenarios (like `hello-world-requirement-retry`)
avoid this entirely. The requirement-retry variant is the canonical execution E2E.

**Proper fix**: Either serialize requirement execution in mock mode, or give the
mock LLM per-task-ID fixture routing instead of per-model counters.

## Evidence

From run 4 logs (Gemini UI E2E):
```
18:53:13 Published TDD pipeline completion event task_id=node-76d39... outcome=success
18:53:45 Published TDD pipeline completion event task_id=node-ebbe7... outcome=success
18:54:16 Published TDD pipeline completion event task_id=node-ebbb4... outcome=success
18:54:56 Published TDD pipeline completion event task_id=node-49d1a... outcome=success
```

From backend E2E (hello-world-code-execution, 2026-03-28):
```
18:28:41 Requirement execution trigger received requirement_id=requirement.1
18:28:41 Requirement execution trigger received requirement_id=requirement.2
18:28:41 Requirement execution trigger received requirement_id=requirement.3
18:28:44 Requirement execution completed requirement_id=requirement.3  # completed
18:33:41 Requirement execution completed requirement_id=requirement.2  # completed (5min later)
         # requirement.1 — dispatched decomposer, never progressed
```

## Files

- `processor/plan-manager/execution_events.go` — EXECUTION_STATES watcher (FIXED)
- `processor/requirement-executor/component.go` — completion event listener
- `processor/execution-manager/component.go` — `publishTDDCompletionEvent()`
- `cmd/mock-llm/main.go` — per-model counter (root cause #2)
