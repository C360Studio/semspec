# Feature: Switch default dispatch from tester to developer

**Severity**: Required — enables single-agent TDD mode
**Component**: `execution-manager` (`processor/execution-manager/component.go`)
**Status**: OPEN

## Summary

`dispatchFirstStage` (line ~891) currently dispatches `dispatchTesterLocked` as the
default. Switch to `dispatchDeveloperLocked` to use the single developer agent
that does TDD internally (writes tests + implementation).

The developer role prompts have been updated for TDD. Team rosters in all configs
changed from tester+builder+reviewer to developer+reviewer. Tool filter updated.

## Change Required

```go
func (c *Component) dispatchFirstStage(ctx context.Context, exec *taskExecution) {
    switch exec.TaskType {
    case workflow.TaskTypeRefactor:
        c.dispatchBuilderLocked(ctx, exec)
    default:
        c.dispatchDeveloperLocked(ctx, exec)  // was: dispatchTesterLocked
    }
}
```

Also ensure `handleDeveloperCompleteLocked` dispatches the structural validator
(same flow as `handleBuilderCompleteLocked`).

## Files

- `processor/execution-manager/component.go` — `dispatchFirstStage`, `handleDeveloperCompleteLocked`
