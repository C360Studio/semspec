# Bug: Plan doesn't transition from implementing → complete

**Severity**: Medium — execution succeeds but plan stays stuck at `implementing`
**Component**: scenario-orchestrator or rule-processor
**Found during**: UI E2E mock T1 tests (2026-03-27)

## Summary

The full TDD pipeline completes successfully:
- Decomposer produces DAG with 1 node
- Tester writes tests, Builder implements, Validator passes (3 checks)
- Code reviewer calls `submit_review(verdict=approved)`
- Task execution approved, node completed, requirement completed

But the plan remains at `implementing`. No transition to `complete` fires.

## Evidence

```
INFO msg="Code review verdict" verdict=approved iteration=0
INFO msg="Task execution approved" slug=f23ee7f2d486
INFO msg="Node completed" node_id=implement-goodbye completed=1 total=1
INFO msg="Requirement execution completed" slug=f23ee7f2d486 requirement_id=requirement.f23ee7f2d486.1
```

Plan 90 seconds later:
```
f23ee7f2d486 stage=implementing
```

## Expected

After all requirements complete, the plan should transition:
`implementing → reviewing_rollup → complete` (or `implementing → complete`)

## Likely Cause

The scenario-orchestrator dispatched the requirement but may not be watching
for its completion. Or the rule-processor for scenario execution
(`handle-completed.json`) isn't matching the completion event. The chain is:

1. requirement-executor publishes completion event
2. scenario-orchestrator or rule-processor detects all requirements done
3. Plan status transitions to `complete`

Step 2→3 isn't happening.
