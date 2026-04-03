# submit_work Deliverable Validation Errors Not Logged

## Status: PARTIALLY FIXED (2026-04-02) — error logged, but deliverable payload not logged

## Severity: Medium (observability gap — blocks diagnosis of LLM retry loops)

## Summary

When `submit_work` deliverable validation fails, the error is returned to the LLM as a
`ToolResult.Error` with `StopLoop=false` for retry. But the error is **never logged
server-side**. This makes it impossible to diagnose why an LLM is stuck in a retry loop
without intercepting the actual NATS tool result messages.

## Evidence

During Gemini @easy testing, the planner called `submit_work` 5 times, burning all
iterations. The semspec logs showed zero information about what validation failed:
- No `ERROR` or `WARN` for deliverable validation
- No log of what the LLM actually sent as the deliverable
- Only visible output was the tool execution subjects in the message logger

## Location

`tools/terminal/executor.go:102-106`:
```go
if err := validator(deliverable); err != nil {
    return agentic.ToolResult{
        CallID: call.ID,
        Error:  fmt.Sprintf("deliverable validation failed: %s", err.Error()),
    }, nil // StopLoop=false — LLM retries
    // ← No log line here
}
```

## Fix

Add a `WARN` log line before returning the validation error:
```go
slog.Warn("submit_work deliverable validation failed",
    "deliverable_type", deliverableType,
    "error", err.Error(),
    "call_id", call.ID)
```

This would immediately tell us whether Gemini sends the wrong field types, missing
fields, double-encoded JSON, etc.

## Found During

UI E2E @easy Gemini test (2026-04-02). Planner stuck in drafting for 5 minutes
with no diagnostic output.
