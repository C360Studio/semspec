# Bug: Retry after fixable rejection dispatches builder instead of developer

**Severity**: High — causes "conflicting instructions" confusion, agents ask questions
**Component**: `execution-manager` (`processor/execution-manager/component.go`)
**Found during**: UI E2E run 19 (2026-03-29)
**Status**: OPEN

## Summary

First dispatch uses `dispatchDeveloperLocked` (correct — developer writes tests + code).
But on retry after fixable review rejection, `startBuilderRetryLocked` calls
`dispatchBuilderLocked` which uses `RoleBuilder`. The builder prompt includes
"Do NOT create or modify test files — testing is another agent's job."

The agent sees conflicting instructions:
- Developer TDD prompt: "Write tests FIRST, then implement"
- Builder restriction: "Do NOT create test files"

The agent correctly asks for clarification, burning 5+ minutes per question timeout.

## Evidence

From run 19:
```
Agent asking question: "I received conflicting instructions regarding test file
creation. My original instructions explicitly state: 'Do NOT create or modify
test files — testing is another agent's job'. However, my previous submission
was rejected because I 'did not add corresponding tests'."
```

## Root Cause

`startBuilderRetryLocked` (line 1395) always calls `dispatchBuilderLocked` (line 1446).
It should dispatch based on the original role — developer or builder.

## Fix

Either:
1. Add a `startDeveloperRetryLocked` that calls `dispatchDeveloperLocked`
2. Or track the original dispatch role on `taskExecution` and use it for retries
3. Or rename `startBuilderRetryLocked` to `startRetryLocked` and pass the role

## Files

- `processor/execution-manager/component.go:1395` — `startBuilderRetryLocked`
- `processor/execution-manager/component.go:1446` — calls `dispatchBuilderLocked`
- `processor/execution-manager/component.go:1162` — rejection handler calls retry
