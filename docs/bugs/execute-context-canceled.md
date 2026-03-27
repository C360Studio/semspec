# Bug: handleExecutePlan JetStream publish fails with context canceled

**Severity**: High — leaves plan stuck at `implementing` with no execution running
**Component**: `plan-manager` (`processor/plan-manager/http.go:782-841`)
**Found during**: UI E2E with Gemini (@easy tier, 2026-03-27)

## Summary

`handleExecutePlan` performs two operations using `r.Context()`:

1. **Line 813**: `setPlanStatusCached(ctx, plan, StatusImplementing)` — KV write, fast, succeeds
2. **Line 837**: `PublishToStream(ctx, subject, data)` — JetStream publish to `scenario.orchestrate.{slug}`, requires ack round-trip, **fails with `context canceled`**

The status mutation commits before the publish, so when the HTTP context is canceled between the two operations, the plan is left in `implementing` status but the scenario-orchestrator never receives the trigger. Execution never starts. The plan is permanently stuck.

## Reproduction

```
ERROR msg="Failed to trigger execution" error="context canceled"
```

Observed in UI E2E when:
1. User clicks "Start Execution" button in browser
2. SvelteKit POSTs to `/plan-manager/plans/{slug}/execute`
3. KV status write succeeds (fast)
4. Browser/Caddy drops connection before JetStream ack completes
5. `PublishToStream` returns `context canceled`
6. Plan stuck at `implementing`, no orchestration message dispatched

## Root Cause

`ctx` on line 789 derives from `r.Context()`, which is tied to the HTTP request lifecycle. The JetStream publish requires a network round-trip for the ack, and if the caller drops the connection before the ack arrives, the context is canceled.

This is a non-transactional two-step mutation: step 1 (status write) commits, step 2 (JetStream publish) fails, and there's no rollback.

## Suggested Fix

The publish context should be a child of the component's lifecycle context (e.g., `c.ctx` from `Start()`), not the HTTP request context. The trace context can still be injected:

```go
// Derive from component context, not HTTP request — the publish must
// survive the HTTP connection closing.
pubCtx, pubCancel := context.WithTimeout(c.ctx, 10*time.Second)
defer pubCancel()
pubCtx = natsclient.ContextWithTrace(pubCtx, tc)

if err := c.natsClient.PublishToStream(pubCtx, subject, data); err != nil {
```

Key constraint: **never use `context.Background()`** — always derive from a parent that participates in graceful shutdown. `c.ctx` (the component lifecycle context from `Start()`) is the right parent.

If the publish still fails, consider rolling back the status:
```go
if err := c.natsClient.PublishToStream(pubCtx, subject, data); err != nil {
    c.logger.Error("Failed to trigger execution, rolling back status", "error", err)
    _ = c.setPlanStatusCached(pubCtx, plan, workflow.StatusReadyForExecution)
    http.Error(w, "Failed to start execution", http.StatusInternalServerError)
    return
}
```

## Additional Note

Line 784-785 has an empty critical section that should also be addressed:
```go
c.mu.RLock()
c.mu.RUnlock()  // no-op — nothing between lock and unlock
```

## Evidence

Logs from UI E2E run (Gemini, @easy tier):
```
time=2026-03-27T12:40:36.148Z level=INFO  msg="Round 2 human approval: plan ready for execution" slug=a20c490c9504
time=2026-03-27T12:40:37.618Z level=ERROR msg="Failed to trigger execution" error="context canceled"
```

No `"Triggered scenario execution via REST API"` log was ever emitted.
Plan remained at `stage=implementing` with zero agent loops running.
