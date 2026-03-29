# Bug: SSE client disconnect WARN spam in logs

**Severity**: Low — cosmetic log noise
**Component**: activity SSE, `agentic-dispatch`
**Status**: OPEN

## Summary

Every SSE client disconnect produces two log lines:
```
INFO  "activity SSE client disconnected" reason="context canceled"
WARN  "failed to stop activity watcher" error="nats: invalid subscription"
```

The WARN is misleading — context cancellation is normal for SSE (client navigated
away or reconnected). The watcher cleanup failure after disconnect should be
logged at DEBUG, not WARN.

Playwright's polling causes frequent connect/disconnect cycles, making this
very noisy during E2E runs.
