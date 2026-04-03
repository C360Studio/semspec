# Plan-Manager Nil planStore Panic — All HTTP Endpoints Crash

## Status: FIXED (2026-04-03)

## Severity: Critical (all plan operations crash with nil pointer)

## Summary

Plan-manager's `planStore` is nil at runtime. Every HTTP request panics:

```
panic: runtime error: invalid memory address or nil pointer dereference
plan-manager.(*planStore).list(0x0)        → plan_store.go:141
plan-manager.(*planStore).exists(...)      → plan_store.go:154
```

This crashes both `handleListPlans` (GET) and `handleCreatePlan` (POST), returning 502
via Go's HTTP panic recovery.

## Evidence

Multiple panics in semspec container logs. The UI test fails immediately at plan creation
with `Create plan failed (502)`.

## Likely Cause

The planStore field on the Component struct is not being initialized during Start() or
factory creation. This may be a regression from recent refactoring (prompt rewrite commit
fe33ac0 or earlier).

## Found During

UI E2E @easy Gemini test (2026-04-03). First request to plan-manager panics.
