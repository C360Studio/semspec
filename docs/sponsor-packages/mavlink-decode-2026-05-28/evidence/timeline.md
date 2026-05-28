# mavlink-decode 2026-05-28 — Run Timeline

All times UTC. Total wallclock: **20.4 min** from plan-created to 8/8 pass.

| Time (offset from start) | Event | Detail |
|---|---|---|
| 00:00 | Plan created | Goal: "Add a Go HTTP service that listens for MAVLink v2 HEARTBEAT frames over UDP on port 14540 and exposes the most recent heartbeat at GET /heartbeat..." |
| 00:00:09 | Plan review approved | (auto-promoted; no findings) |
| 00:00:15 | Requirements generated | 1 requirement covering UDP listener + HTTP endpoint |
| 00:00:42 | Architecture generated | Architect ran `curl pkg.go.dev/.../gomavlib` with multiple grep patterns to discover the API surface. Selected `mavlink.raw-mavlink-direct` profile. **One critical alert** (`RepeatToolFailure`) — false positive, agent was iterating through patterns. |
| 00:01:09 | Scenarios generated + reviewed | 3 scenarios per requirement (revised once, then approved) |
| 00:01:09 | Execution triggered | Plan → `implementing` |
| 00:01:09 → 00:15:35 | Execution phase | **3 TDD cycles**. Developer fought `go get gomavlib/v3` import paths in cycle 1, found correct API in cycle 2, landed working tests in cycle 3 (each cycle ~5 min). |
| 00:15:35 | Execution completes | 1/1 requirement passed |
| 00:15:47 | QA verdict (synthesis tier) | qa-reviewer Murat persona ran LLM-only verdict, no test execution |
| 00:15:47 | Plan → complete | |
| 00:17:00 | Test framework teardown | Playwright `afterAll`. Workspace would have died here had PR #20's `workspace.tar.gz` not been in place. |

## Loop & trajectory counts

| Phase | Loops | Trajectories |
|---|---|---|
| Plan / requirements / architecture / scenarios | ~13 loops | 13 trajectories |
| Developer TDD cycles (3) | ~6 loops | 3 dev trajectories |
| Code reviewer | ~3 loops | 3 review trajectories |
| Decomposer | 1 loop | 1 trajectory |
| QA reviewer | 1 loop | 1 trajectory |

**21 trajectories total**. See `trajectories-summary.txt` for per-trajectory step counts.

## Cost & resource footprint

- ~40 agent loops total (about 20 unique trajectories — some loops dispatched but absorbed into existing trajectory continuations)
- **All-gemini model assignment** (no Claude developer; this scenario fits inside gemini-flash + gemini-pro fallback capability chains)
- ctx_util peaked at **0.02** — easy on context budget; the run wasn't ever close to compaction
- Single host workstation execution
- Watch sidecar overhead negligible (snapshots, alert detection)

## ALERTs raised (1 critical, false positive)

```
ALERT: RepeatToolFailure severity=critical evidence_id=4e917672-da2e-4b22-a9ad-3b3912edbed5
remediation="Loop is calling tool bash with the same error class (exit code 1)
3+ times in a row — the model is not reading the tool's error response..."
```

The detector counts 3 consecutive exit-1 bash calls. Each of the 3 bash
calls was a different `curl pkg.go.dev | grep <pattern>` query — the
architect was discovering gomavlib's API surface by iterating through
grep patterns (over-escaped regex parens initially returned empty; the
agent successively loosened them). All three were *learning* iterations,
not wedge symptoms. The detector is pattern-blind to research-iteration
shape and should be improved to consider tool-argument variance, not
just exit codes. Tracking as follow-up.

## How to read watch.log

`evidence/watch.log` is the live observability stream. Format:

```
[HH:MM:SS] plans=N loops=M msgs=K active_loops=L ctx_util=0.XX errors=Z
```

- `plans` — count of in-flight plans (1 for this whole run)
- `loops` — cumulative agent loop count
- `msgs` — messages currently flowing on the message bus
- `active_loops` — agent loops currently running (vs already terminated)
- `ctx_util` — fraction of context budget used by the active loop
- `errors` — sidecar's own connectivity error count (non-zero only at
  teardown when stack is being torn down)
