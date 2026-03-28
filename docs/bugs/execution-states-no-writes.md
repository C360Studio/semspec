# Bug: execution-manager never writes to EXECUTION_STATES after KV Twofer migration

**Severity**: Blocker — requirement-executor stuck waiting for completions
**Component**: `requirement-executor` (`processor/requirement-executor/`)
**Found during**: UI E2E T2 @easy with Gemini (2026-03-28, run 5)
**Status**: FIXED

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
