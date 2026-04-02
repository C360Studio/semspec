# Code Review Returns Empty Verdict — Execution Escalates

## Status: FIXED (2026-04-02)

## Severity: High (blocks execution pipeline completion)

## Summary

During execution, the code reviewer agent completes successfully but the extracted verdict
is empty (`verdict=""`, `rejection_type=""`). This causes every iteration to be treated as
a fixable rejection, exhausting the iteration budget and escalating the task.

## Evidence

```
INFO Code review verdict slug=3895ea5557bd verdict="" rejection_type="" iteration=0
INFO Code review verdict slug=3895ea5557bd verdict="" rejection_type="" iteration=1
INFO Code review verdict slug=3895ea5557bd verdict="" rejection_type="" iteration=2
INFO Task execution escalated slug=3895ea5557bd reason="fixable rejections exceeded iteration budget"
INFO Requirement execution failed slug=3895ea5557bd reason="node \"implement-goodbye\" failed: outcome=failed"
```

## Root Cause

Two issues combined:

1. **Half-finished refactor**: `submit_review` was a separate terminal tool while generators had
   migrated to `submit_work` with deliverable validation. Consolidated `submit_review` into
   `submit_work` with a `"review"` deliverable type and `ValidateReviewDeliverable` validator.

2. **Fixture sequence misalignment**: After the tester/builder merge into a single developer,
   mock-coder fixtures were still numbered for the old 3-loop flow (tester→builder→reviewer).
   The merged developer consumed only 2 of 5 expected calls, so the reviewer received
   implementation fixtures (submit_work with no verdict) instead of the review fixture.
   Renumbered to 5-call sequence: 3 developer + 1 task reviewer + 1 requirement reviewer.

## Impact

- t1 test 6 "execution reaches complete" fails (plan stays in `implementing`)
- Full execution pipeline cannot reach `complete` stage
- Tests 7-8 (completed plan in Done filter, trajectories) also fail

## Found During

UI E2E regression testing (2026-04-02). This surfaced after fixing the
architecture-generator config gap — the pipeline now reaches execution but stalls at review.
