# Bug: execution-manager never writes to EXECUTION_STATES after KV Twofer migration

**Severity**: Blocker — requirement-executor stuck waiting for completions
**Component**: `execution-manager` (`processor/execution-manager/`)
**Found during**: UI E2E T2 @easy with Gemini (2026-03-28, run 5)
**Status**: OPEN

## Summary

After the KV Twofer migration (dd65105), execution-manager no longer writes task
completions to the EXECUTION_STATES KV bucket. Requirement-executor watches
`EXECUTION_STATES req.>` but sees nothing. Decomposers complete their agentic loops
(AGENT_LOOPS shows 3 done) but requirement-executor never receives the completions
and never dispatches nodes.

## Evidence

```
# Watchers started correctly
19:59:11 Requirement completion watcher started (watching EXECUTION_STATES req.>)

# Decomposers dispatched
20:00:02 Dispatched decomposer entity_id=...requirement-cc0e1f810724-3
20:00:02 Dispatched decomposer entity_id=...requirement-cc0e1f810724-1
20:00:02 Dispatched decomposer entity_id=...requirement-cc0e1f810724-2

# Agentic loops complete (from AGENT_LOOPS KV)
Done: 3  Failed: 0  Active: 0

# But ZERO writes to EXECUTION_STATES during execution
grep 'EXECUTION_STATES.*put\|EXECUTION_STATES.*update' → empty

# No "Decomposition complete" or "Dispatched node" from requirement-executor
```

## Root Cause

The refactor in dd65105 (migrate requirement-executor completions to KV Twofer)
likely removed or changed the code path where execution-manager writes decomposer
completion state to EXECUTION_STATES. The old path used `agent.complete.>` JetStream
consumer (removed in 6d453bc). The new path should write to EXECUTION_STATES KV but
doesn't appear to be doing so for decomposer completions.

## Expected Behavior

When a decomposer agentic loop completes:
1. execution-manager receives the loop completion
2. execution-manager writes task state to EXECUTION_STATES KV
3. requirement-executor's watcher picks up the write
4. requirement-executor processes the decomposition result and dispatches nodes

## Files

- `processor/execution-manager/component.go` — syncToStore / handleDecomposerComplete
- `processor/requirement-executor/component.go` — EXECUTION_STATES watcher
