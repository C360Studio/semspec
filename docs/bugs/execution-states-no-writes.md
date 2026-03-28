# Bug: execution-manager never writes to EXECUTION_STATES after KV Twofer migration

**Severity**: Blocker — requirement-executor stuck waiting for completions
**Component**: `requirement-executor` (`processor/requirement-executor/`)
**Found during**: UI E2E T2 @easy with Gemini (2026-03-28, run 5)
**Status**: PARTIALLY FIXED — writes happen now but watcher doesn't fire

## Root Cause

`dispatchDecomposerLocked` set `exec.DecomposerTaskID` in memory but never
sent a mutation to execution-manager to persist it to EXECUTION_STATES KV.
When the decomposer loop completed, execution-manager's `handleReqLoopCompleted`
called `reqKeyByTaskID(event.TaskID)` which scans KV entries — but
`DecomposerTaskID` in KV was empty, so no match was found.

The reviewer and red-team dispatchers already sent mutations with their task IDs.
Only decomposer was missing.

## Fix

Added `sendReqPhase` call with `decomposer_task_id` in `dispatchDecomposerLocked`,
matching the pattern already used by `dispatchRequirementReviewerLocked` and
`dispatchRequirementRedTeamLocked`.

## Run 6 Update — writes happen, watcher doesn't fire

EXECUTION_STATES now has 3 entries (one per requirement) with keys like
`req.d265805ff1b2.requirement.d265805ff1b2.1`. The KV writes are working.

But requirement-executor's watcher (`watching EXECUTION_STATES req.>`) never fires.
Decomposers complete in AGENT_LOOPS (3 done), but requirement-executor never logs
"Decomposition complete" or "Dispatched node".

The issue is now in the watcher → handler path. Either:
1. The KV watcher event isn't matching the key pattern
2. The handler receives the event but doesn't recognize it as a decomposer completion
3. The `handleReqLoopCompleted` isn't being called or is silently failing
4. The watcher started before the initial KV puts, and NATS KV doesn't replay
   existing keys to new watchers (only updates)

Hypothesis for #4: if requirement-executor creates and writes the initial KV entry
in `dispatchDecomposerLocked`, then the watcher (started earlier) already saw the
Put. When decomposer completes and execution-manager updates the same key, the
watcher should fire on the Update. But if the update doesn't change the key (or
writes to a different key), the watcher won't fire.
