# Bug: Trajectory tool_arguments and tool_result may be empty

**Severity**: Medium — affects debugging and E2E monitoring
**Component**: `agentic-loop` (semstreams), UI trajectory display
**Found during**: E2E monitoring sessions (2026-03-28/29, runs 1-14)
**Status**: NEEDS INVESTIGATION

## Summary

During active E2E monitoring, tool call data appeared empty when queried via
message-logger (`curl /message-logger/entries` returned `data: {}`). The
trajectory API (`/agentic-loop/trajectories/{loopId}`) was not tested directly
during runs — it may have the data or may also be empty.

The UI `TrajectoryEntryCard.svelte` renders `tool_arguments` and `tool_result`
correctly when present. The question is whether the backend populates these fields.

## What to Check

1. Hit `GET /agentic-loop/trajectories/{loopId}` during an E2E run
2. Inspect `tool_call` steps — are `tool_arguments` and `tool_result` populated?
3. If empty: check semstreams agentic-loop trajectory storage (AGENT_CONTENT KV)
4. If populated: the message-logger path was the wrong place to look (different data)

## Existing Test

`ui/e2e/plan-journey.spec.ts:160` — "trajectories exist with steps after execution"

This test verifies steps exist but does NOT check tool data content. Should be
strengthened to verify that tool_call steps have non-empty `tool_name` and
`tool_arguments`.

## Proposed Test Enhancement

```typescript
// After getting trajectory
const toolStep = traj.steps.find(s => s.step_type === 'tool_call');
if (toolStep) {
    expect(toolStep.tool_name).toBeTruthy();
    expect(toolStep.tool_arguments).toBeTruthy();
}
```

## Files

- `ui/e2e/plan-journey.spec.ts:160` — trajectory test (strengthen)
- `ui/src/lib/components/trajectory/TrajectoryEntryCard.svelte` — display (works if data present)
- `ui/src/lib/types/trajectory.ts` — types (correct)
