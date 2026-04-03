# Graph Reconciliation JSON Parse Error

## Status: FIXED (168c640 — semstreams alpha.84 fixes prefix query JSON envelope)

## Severity: Medium (falls back to empty cache, may cause stale reads)

## Summary

On startup, plan-manager and project-manager graph reconciliation fails with a JSON parse
error. The graph prefix query returns a non-JSON response that causes unmarshal to fail.

## Evidence

```
WARN Project config graph reconciliation failed, using file versions error="unmarshal prefix response: invalid character 'e' looking for beginning of value"
WARN Plan reconciliation failed (cache will be empty until plans are created/mutated) error="unmarshal prefix response: invalid character 'e' looking for beginning of value"
```

The `invalid character 'e'` suggests the response starts with an error message string
(e.g., "error: ...") instead of valid JSON.

## Impact

Cache starts empty on boot. Plans created before restart are invisible until a new
mutation refreshes the cache. Non-blocking for fresh E2E runs but could cause issues
in long-running sessions.

## Found During

UI E2E regression testing (2026-04-02).
