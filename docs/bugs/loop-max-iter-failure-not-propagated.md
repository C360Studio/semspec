# Bug: Agentic loop max_iterations failure doesn't propagate to execution-manager

**Severity**: High — plan stuck at implementing when last requirement's loop hits max iterations
**Component**: agentic-loop (semstreams), execution-manager
**Found during**: UI E2E run 20 (2026-03-29)
**Status**: OPEN

## Summary

When an agentic loop hits max_iterations (40), it logs "Loop failed" with
`reason=handler_error`, but the completion event never reaches execution-manager.
The task stays at `executing` forever, the requirement never completes, and the
plan never transitions from `implementing`.

## Evidence

From run 20: 4/5 requirements completed. The 5th requirement's developer loop
hit max iterations:

```
19:22:31 Loop processing failed error="max iterations (40) reached" loop_id=97b4c917
19:22:31 Loop failed loop_id=97b4c917 reason=handler_error
```

But execution-manager never received the completion:
- No "Loop completion received via KV" for this task
- No "Task execution escalated"
- Requirement stays at `executing`
- Plan stays at `implementing`

## Expected Behavior

When the loop fails due to max iterations:
1. Loop writes a completion event to AGENT_LOOPS KV with `outcome: "failed"`
2. Execution-manager's KV watcher picks it up
3. Execution-manager escalates the task
4. Requirement-executor marks the requirement as failed
5. Plan-manager transitions plan to rejected/complete

## Root Cause Hypothesis

The loop failure with `reason=handler_error` may not write to the COMPLETE_*
key pattern that the KV watcher expects. The watcher looks for success completions
but may not handle error/failure completions.

## Files

- Semstreams agentic-loop — loop failure handler (writes to KV?)
- `processor/execution-manager/loop_completions.go` — KV watcher pattern
