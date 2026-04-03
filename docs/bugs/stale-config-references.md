# Stale Component References in E2E Config

## Status: FIXED (168c640 — removed rdf-export + reactive-workflow from all configs)

## Severity: Low (warnings only, non-blocking)

## Summary

E2E semspec config references deleted component factories that no longer exist:
- `reactive-workflow` — removed during reactive engine removal
- `rdf-export` — removed during deleted components cleanup

## Evidence

```
ERROR Failed to create component from config instance=reactive-workflow factory=reactive-workflow error="unknown component factory 'reactive-workflow'"
ERROR Failed to create component from config instance=rdf-export factory=rdf-export error="unknown component factory 'rdf-export'"
```

## Fix

Remove these entries from `configs/e2e-mock-ui.json` (and any other E2E config files).

## Found During

UI E2E regression testing (2026-04-02).
