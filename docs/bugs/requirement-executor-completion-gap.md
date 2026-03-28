# Bug: requirement-executor doesn't process TDD pipeline completion events

**Severity**: Blocker — plans stuck at `implementing` forever
**Component**: `requirement-executor` (`processor/requirement-executor/`)
**Found during**: UI E2E T2 @easy with Gemini (2026-03-28, run 4)
**Status**: OPEN

## Summary

All 4 TDD pipelines complete successfully (`Published TDD pipeline completion event`
with `outcome=success`), but requirement-executor never processes these completions.
Requirements stay at `executing`, plan stuck at `implementing`.

## Evidence

From run 4 logs:
```
18:53:13 Published TDD pipeline completion event task_id=node-76d39... outcome=success
18:53:45 Published TDD pipeline completion event task_id=node-ebbe7... outcome=success
18:54:16 Published TDD pipeline completion event task_id=node-ebbb4... outcome=success
18:54:56 Published TDD pipeline completion event task_id=node-49d1a... outcome=success
```

But requirement-executor logs show no `Node completed` or `requirement completed` messages
after the TDD completions. The last requirement-executor activity was dispatching nodes
at 18:50:35.

## Possible Causes

1. **EXECUTION_STATES watcher race** (partially fixed per backend team notes) — the
   watcher may have started before the bucket existed and given up
2. **Completion event subject mismatch** — execution-manager publishes to one subject,
   requirement-executor listens on another
3. **Consumer delivery issue** — JetStream consumer for requirement-executor may have
   lost its position or be filtering wrong subjects

## Context

- This was previously identified as a potential issue (backend lesson #1: EXECUTION_STATES
  startup race). The fix with retry.Quick() may not be sufficient, or may not cover the
  requirement-executor's listener path.
- In run 2, the same symptom occurred — plan stuck at `implementing`
- In run 3, tasks timed out before this could be observed

## Files

- `processor/requirement-executor/component.go` — completion event listener
- `processor/execution-manager/component.go` — `publishTDDCompletionEvent()`
- Both need to agree on subject pattern for completion events
