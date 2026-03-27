# Bug: submit_review tool not registered — reviewer verdicts always fail

**Severity**: High — code reviewer verdict always empty, execution always escalates
**Component**: semspec `tools/register.go` (tool registration)
**Found during**: UI E2E mock T1 tests (2026-03-27)
**Status**: FIXED (a56eba7)

## Summary

The `submit_review` terminal tool was defined in `terminal.Executor.ListTools()`
and handled in its `Execute()` method, but was never registered with
`agentictools.RegisterTool()`. When the reviewer agent called `submit_review`,
the agentic-tools component returned `"tool not found"` — the loop retried
until exhausting iterations, then escalated.

Additionally, `submit_review` was missing from:
- The tool filter (`prompt/tool_filter.go`) — reviewers had `submit_work` instead
- The available tool lists in execution-manager and requirement-executor

## Evidence

### Mock LLM served the fixture correctly
```
[call 14] model=mock-coder call_index=8/9
[call 14] responded with 1 tool_calls for model=mock-coder
```

### Loop completed but Result was empty
```
INFO msg="Loop completed" loop_id=... outcome=success
INFO msg="Code review verdict" verdict="" rejection_type="" iteration=0
```

This happened because the loop never reached a successful `StopLoop` completion —
the "tool not found" error was fed back to the LLM, which retried `submit_review`
in a futile loop until the iteration budget was exhausted.

## Root Cause

`tools/register.go` registered `submit_work` via `termExec` but never registered
`submit_review`:
```go
// Before fix:
termExec := terminal.NewExecutor()
_ = agentictools.RegisterTool("submit_work", termExec)
// submit_review was never registered
```

The reviewer tool filter also listed `submit_work` instead of `submit_review`,
so even if registered, it wouldn't have appeared in the reviewer's tool list.

## Fix (a56eba7)

1. Added `agentictools.RegisterTool("submit_review", termExec)` in `tools/register.go`
2. Changed reviewer tool filters to include `submit_review` instead of `submit_work`
3. Added `submit_review` to `availableToolNames()` in both execution-manager and requirement-executor

## Note on semstreams

The initial investigation suspected a semstreams bug where `ToolResult.Content`
wasn't flowing to `LoopCompletedEvent.Result`. This was incorrect — the semstreams
code at `agentic-loop/handlers.go:805-806` correctly maps `toolResult.Content` to
`LoopCompletedEvent.Result` when `StopLoop` is true. The empty result was caused
by the tool never executing successfully (not found → error → retry → exhaustion).

## Open: Still failing after registration fix (2026-03-27 evening)

After the registration fix (a56eba7), a no-cache rebuild still shows `verdict=""`
on all 3 reviewer iterations. Evidence from the latest run:

**Binary confirms `submit_review` is compiled in:**
```
$ docker exec ui-semspec-1 strings /usr/local/bin/semspec | grep submit_review
submit_review
When your review is complete, call submit_review with your verdict:
```

**Mock LLM served the `submit_review` fixture at call_index 8/9:**
```
[call 14] model=mock-coder call_index=8/9
[call 14] responded with 1 tool_calls for model=mock-coder
```

**No "tool not found" errors in logs** (unlike before the fix).

**But verdict is still empty across all iterations:**
```
INFO msg="Code review verdict" verdict="" rejection_type="" iteration=0
INFO msg="Code review verdict" verdict="" rejection_type="" iteration=1
INFO msg="Code review verdict" verdict="" rejection_type="" iteration=2
INFO msg="Task execution escalated" reason="fixable rejections exceeded iteration budget"
```

**Startup warning (may be relevant):**
```
WARN msg="ENTITY_STATES bucket not available — agentic tools will not register"
```

**Possible causes:**
1. Tool filter not presenting `submit_review` to the reviewer agent — the mock
   fixture returns `submit_review` but if the tool isn't in the reviewer's tool
   list, the LLM's tool call is ignored and the loop completes without StopLoop.
2. The `submit_review` mock fixture (mock-coder.8.json) may arrive at the wrong
   call index if the TDD pipeline call count differs from expected.
3. The ENTITY_STATES startup race may prevent tool registration despite the code
   being present — need to verify tools register via the in-memory executor
   registry, not KV.
