# Rejection Journey Timeout — Plan Stalls After Revision Recovery

## Status: OPEN

## Severity: Medium (happy path works, only rejection variant affected)

## Summary

The t1 plan-rejection-journey test times out at 90s waiting for `scenarios_reviewed`.
The mock reviewer correctly rejects the plan (round 1 `needs_changes`), the planner
revises it, and the re-review approves. But the cascade from `reviewed` through
requirements → architecture → scenarios never completes within the timeout.

## Evidence

```
INFO Review agent complete slug=23e189abf6cd round=1 verdict=needs_changes
INFO Plan revision loop — retrying slug=23e189abf6cd round=1 iteration=1
INFO Detected revision plan — dispatching in refinement mode slug=23e189abf6cd
INFO Review agent complete slug=23e189abf6cd round=1 verdict=approved
INFO Review round 1: sent reviewed mutation (auto_approve=false, awaiting human approval)
```

After this the plan reaches `reviewed` and the test clicks "Create Requirements" to
promote. The promote succeeds (200) but the subsequent cascade (requirements → architecture
→ scenarios → review round 2) does not complete before the 90s timeout.

## Context

The happy-path journey (same cascade without rejection) completes in ~1.2s. The rejection
variant uses `hello-world-plan-rejection` mock scenario fixtures. After the submit_work
deliverable refactor, these fixtures may not correctly model the revised plan flow — the
planner revision dispatch may consume fixtures in a different order than expected.

## Found During

UI E2E regression testing (2026-04-02). Happy path passes 8/8, only rejection variant fails.
