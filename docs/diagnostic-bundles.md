# Diagnostic Bundles & Live Watch (`semspec watch`)

When a semspec run goes sideways, you have two questions: *what's happening
right now?* and *what just happened, so I can show someone?* The
`semspec watch` subcommand answers both.

It runs as part of the existing `semspec` binary — no new container, no
new ports, no daemon. Per [ADR-034](adr/ADR-034-watch-cli-and-diagnostic-bundles.md)
it's an in-process consumer of the endpoints semspec already exposes.

## Two modes

```bash
semspec watch --bundle <path>     # one-shot snapshot for offline handoff
semspec watch --live              # streaming heartbeat with detector alerts
```

Use `--bundle` when something already failed and you want a tarball you
can attach to a ticket or send to the maintainer. Use `--live` while a
run is going to catch failure shapes early and bail before they burn
more tokens.

## Quick start

### Capture a bundle (for sharing)

```bash
semspec watch --bundle /tmp/run-2026-04-30.tar.gz \
  --http http://localhost:3000 \
  --nats nats://localhost:4223
```

Writes a single `.tar.gz` containing `bundle.json` plus per-loop
trajectory files. Defaults assume a UI E2E stack on port 3000; for
production swap `--http http://localhost:8080`.

### Watch a run (with auto-bail)

```bash
semspec watch --live \
  --http http://localhost:3000 \
  --nats nats://localhost:4223 \
  --bail-on critical \
  --max-duration 30m
```

Polls every 10s, runs detectors, emits an `ALERT:` line the first
time each new diagnosis appears, and exits with `BAIL:` if any
detector hits the severity threshold you set with `--bail-on`.

### The integrated e2e wrapper

For real-LLM scenarios run via Playwright the wrapper task pre-builds
the binary, starts the stack, runs `--live` as a sidecar with
`--snapshot-interval 60s` for plan-preserving snapshots, runs the
test, and dumps a final bundle:

```bash
task e2e:watch:llm -- gemini easy
task e2e:watch:llm -- hybrid easy   # gemini-pro + sonnet + flash
task e2e:watch:llm -- claude medium
task e2e:watch:llm -- local easy    # local Ollama
```

Artifacts land in `/tmp/semspec-watch-<provider>-<tier>-<ts>/`:
- `watch.log` — full live stream
- `bundle.tar.gz` — final post-run capture
- `snapshot-<UTC>.tar.gz` — periodic snapshots; **the latest one
  predates Playwright's afterAll cleanup, so it has the live plan
  the final bundle does not**

## Reading the live stream

The startup banner shows what's running:

```
semspec watch --live · interval=10s · http=http://localhost:3000 · bail_on=critical · snapshot=1m0s→/tmp/...
```

Each tick (default every 10s) writes one heartbeat line:

```
[13:46:42] plans=1 loops=1 msgs=11 active_loops=1 ctx_util=0.00 errors=0
```

| Field | What it means |
|---|---|
| `plans` | rows in PLAN_STATES — usually 0 (no run) or 1 (one active plan) |
| `loops` | rows in AGENT_LOOPS — counts both live loops and `COMPLETE_*` markers |
| `msgs` | message-logger entries matching the subject filter (`agent.*` + `tool.*` by default) |
| `active_loops` | `semstreams_agentic_loop_active_loops` gauge — currently running loops |
| `ctx_util` | most recent `loop_context_utilization` reading; > 0.5 hints at wedge-zone |
| `errors` | per-source capture errors this tick (cumulative across sources within the tick) |

When a new error source appears for the first time, the heartbeat
suffix calls it out:

```
[13:13:14] ... errors=1 [new: kv:AGENT_LOOPS]
```

Subsequent ticks with the same source stay silent in the suffix —
grep for `[new:` to find first appearances.

## Detector alerts

When a detector matches, you'll see:

```
ALERT: EmptyStopAfterToolCalls severity=critical evidence_id=2042 remediation="..."
```

`evidence_id` is the message-logger sequence; the alert deduplicates
across ticks so each Shape+sequence pair fires exactly once per
session.

The v1 detector set:

| Shape | Severity | What it means | Remediation hint |
|---|---|---|---|
| `EmptyStopAfterToolCalls` | critical | Model returned `finish_reason=stop` with empty content + no tool calls AFTER calling tools earlier in the same loop. Loop is wedged. | Inject the prior failure context on retry — see `feedback_retries_must_inject_failure_context.md` |
| `JSONInText` | critical | Model emitted a tool call as JSON text in `content` (`{"name": "..."}`) instead of using the function-calling channel. Tool will NOT execute. | Reinforce the function-calling channel in the system prompt or downgrade to a model with stronger native tool support |
| `ThinkingSpiral` | warning | `finish_reason=stop`, no tool calls, `completion_tokens > 500`. Model burned generation budget on reasoning instead of acting. | Try a model with native `reasoning_content` channel support, or strengthen the persona's tool-use mandate |

Detectors are pure functions over the bundle data — no I/O, no network.
You can run them locally against a captured bundle (see "Reading a
bundle offline" below) without a running stack.

## --bail-on

`--bail-on info|warning|critical` exits the live loop the moment any
diagnosis at or above that severity fires. Common usage:

- `--bail-on critical` — kill the run only on hard wedges (default
  for the e2e wrapper)
- `--bail-on warning` — kill on burn-prone shapes (ThinkingSpiral,
  WedgeZoneApproaching when it ships)
- omitted — don't bail, just alert

If `--bail-on` triggers you'll see:

```
BAIL: severity=critical reached --bail-on=critical threshold; exiting
```

## What's in a bundle

A bundle tarball contains:

```
bundle.json                       <- top-level state + diagnoses
trajectories/<loop_id>.json       <- one file per captured loop
```

`bundle.json` shape (abbreviated):

```json
{
  "bundle":   { "format": "v1", "captured_at": "...", "captured_by": "...",
                "redactions": ["sensitive_field_values", "auth_header_values"] },
  "host":     { "os": "darwin", "semspec_version": "...", "ollama": {...} },
  "config":   { "active_capabilities": {...}, "redacted_endpoints": [...] },
  "plans":    [...],            // PLAN_STATES KV entries with key/revision/created/value
  "loops":    [...],            // AGENT_LOOPS KV entries
  "messages": [...],            // most recent agent.* + tool.* entries (subject-filtered)
  "metrics":  { "loop_active_loops": 12, "model_requests_total": 38, ... },
  "ollama":   { "running": [...], "last_error": "" },
  "diagnoses": [...],           // detector output
  "trajectory_refs": [...]      // pointers to trajectories/*.json files
}
```

`format: "v1"` is load-bearing — schema evolves additively within v1;
breaking changes mint v2 and ship a parallel writer for one cycle.

### Reading a bundle offline

`bundle.json` is normal JSON; trajectories are normal JSON. So:

```bash
tar -xzOf bundle.tar.gz bundle.json | jq '.diagnoses'
tar -xzOf bundle.tar.gz bundle.json | jq '.metrics'
tar -tzf bundle.tar.gz | head    # list contents

# Extract everything for poking around
mkdir /tmp/inspect && cd /tmp/inspect
tar -xzf /path/to/bundle.tar.gz

# A specific trajectory
jq '.steps | length' trajectories/<loop-uuid>.json
```

## Redactions

The bundle automatically scrubs string values for any JSON object
field whose name contains `key`, `secret`, `token`, `password`,
`passwd`, `authorization`, `api-key`, or `api_key` (case-insensitive).
Numeric values are left intact (so `usage.completion_tokens` stays
alive — important for ThinkingSpiral). The `bundle.redactions`
manifest lists the categories applied so the receiver knows what's
missing.

The heavier `--redact-prompts` (full prompt-content scrub with
allowlist file) is reserved for v2 once external adopters require it.
For internal handoff today, the default redaction is enough — but
**do read the prompt content of agent.request payloads before
attaching to a public ticket** if your prompts may contain proprietary
context.

## Periodic snapshots

`--snapshot-interval <duration>` writes a complete bundle to
`--out-dir` every interval. File names are sortable
(`snapshot-YYYYMMDD-HHMMSS.tar.gz`), so:

```bash
ls /tmp/.../snapshot-*.tar.gz | tail -1   # most recent
```

This solves a real problem: many test harnesses (including
Playwright's afterAll) clean up plan state on success. By the time a
post-run `--bundle` capture fires, the plan is already gone. The
periodic snapshot keeps a series of pre-cleanup states; the latest
one before teardown is the operator's gold copy.

Snapshots that would be empty (no plans, no loops, no messages, no
trajectories) are skipped — the file count reflects real state, not
post-cleanup zombies.

## Common flags reference

```
--bundle <path>           One-shot capture; mutex with --live
--live                    Streaming mode; mutex with --bundle
--http <url>              Gateway URL (default http://localhost:8080)
--nats <url>              NATS URL (empty = skip trajectories)
--limit <n>               Per-subject message-logger entry cap
                          (default 5000 — large enough that subject
                          filter has matches even on graph-busy runs)
--skip-ollama             Skip ollama probe (auto-disabled if binary
                          not on PATH)
--bail-on <severity>      Live mode: exit when info|warning|critical
                          fires
--max-duration <dur>      Live mode: cap total run time
--snapshot-interval <dur> Live mode: periodic bundle write
--out-dir <path>          Where snapshots land (required when
                          snapshot-interval is set)
--interval <dur>          Live mode poll cadence (default 10s)
```

## Operator hygiene

- **Don't pre-source `.env` when invoking `task` commands** — the root
  `Taskfile.yml` declares `dotenv: ['.env']` so Task loads it
  automatically. `task e2e:watch:llm -- hybrid easy` is the clean
  repeatable form.
- **Two streams, not four**: when watching a run interactively, tail
  `watch.log` (primary signal) and Playwright stdout (orthogonal —
  scenario pass/fail). Only fall back to `docker logs` if `watch.log`
  heartbeats stop emitting (means the watch sidecar can't reach the
  stack — diagnose semspec liveness directly). The four-stream bash
  blob from earlier debugging sessions is what this tool replaces.

## Troubleshooting

**"messages: HTTP 500" or similar** — the gateway you're pointing at
isn't running, or the path is wrong (UI E2E uses Caddy on :3000;
backend E2E uses :8180; production uses :8080). Check
`docker compose ps` to confirm the stack is up.

**Heartbeat shows `errors=N` rising every tick** — grep for `[new:`
in the watch.log to find the first source that failed. If the source
name is `kv:<bucket>` the message-logger isn't exposing that bucket;
if it's `metrics` semspec's Prometheus endpoint isn't reachable.

**Watch sidecar doesn't exit when I Ctrl-C the task** — the wrapper
sends SIGTERM, waits 3s, then SIGKILL. If you've started `semspec
watch` directly (not via the task wrapper) and Ctrl-C doesn't take
effect within ~10s, find the PID and send SIGKILL. Cause: Go's HTTP
client doesn't always return immediately on ctx cancel when the
target server has gone missing.

**`plans=0` in my final bundle** — Playwright's `afterAll` deletes
the plan from `PLAN_STATES` before the post-run capture fires. The
`--snapshot-interval 60s` flag (set by default in `task
e2e:watch:llm`) preserves a pre-cleanup snapshot. Read
`snapshot-<latest>.tar.gz` instead of `bundle.tar.gz` for the live
plan context.

**A bundle has empty `metrics`** — almost certainly the gateway you
pointed at doesn't proxy `/metrics` (UI E2E used to have this gap;
fixed 2026-04-30). Verify with
`curl <http>/metrics | head -5`.

## Linked

- [ADR-034](adr/ADR-034-watch-cli-and-diagnostic-bundles.md) — design
  rationale and load-bearing constraints
- [ADR-034 Implementation Plan](adr/ADR-034-implementation-plan.md) —
  commit cadence and what's deferred
- `pkg/health/testdata/fixtures/` — captured fixtures used by the
  detector tests; useful as reference data for what each shape
  looks like in a real run
