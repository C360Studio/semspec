# ADR-034 Implementation Plan

**Status:** Draft
**Date:** 2026-04-30
**Pairs with:** [ADR-034](./ADR-034-watch-cli-and-diagnostic-bundles.md)

## Why this exists

ADR-034 is large enough that committing to it as one session-sized chunk
risks mid-implementation drift. This plan breaks it into:

- A frozen v1 scope (what ships in this implementation cycle)
- A concrete bundle schema (load-bearing constraint #1 in the ADR)
- Per-detector specs sharp enough that the test seeds itself from a
  named fixture
- A commit cadence each step is independently shippable

It also captures what's deferred to v2 and *why*, so future contributors
don't relitigate scope decisions inside the implementation phase.

## v1 scope — 5 detectors + bundle capture + minimal CLI

Every detector in v1 has a real captured fixture in
`pkg/health/testdata/fixtures/` (plus the new `post-adr033-*` dumps
captured today). Detectors without fixture seed coverage land in v2.

### Detector inventory

| Detector | Polarity | Fixture | What it matches |
|---|---|---|---|
| `EmptyStopAfterToolCalls` | negative | `post-adr033-easy-0709/empty-result-attempt1-0721/` + `archgen-empty-0731/` | `agent.response` with `finish_reason=stop` AND `message.tool_calls=[]` AND `message.content==""` AND at least one prior `agent.response` in the same loop had `tool_calls!=[]` |
| `JSONInText` | negative | `local-easy-v105-0822/run.log` | `agent.response` with `finish_reason=stop` AND `message.content` parses as a JSON object containing a `name` field (model emitted tool call as text) |
| `ThinkingSpiral` | negative | `local-easy-v107-rerun-0930/messages-final.json` | `agent.response` with `finish_reason=stop` AND `message.tool_calls=[]` AND `usage.completion_tokens > 500` (model burned generation budget on reasoning channel without acting) |
| `RequirementTimeoutGPUContention` | negative | `post-adr033-easy-0709/req1-timeout-0759/` | semspec.log shows `Requirement execution timed out` AND for ≥80% of the requirement's wall clock `loop_active_loops > 8` (binding constraint was throughput, not the pipeline) |
| `Phase6PositiveLessonChain` | positive | `post-adr033-gemini-positive-0855/` (today) | After a `Code review verdict approved` log line with `tdd_cycle=0`, within 60s a `Dispatched lesson-decomposer agent` AND within 120s a `Decomposer lesson recorded` with `evidence_steps>0 OR evidence_files>0` |

The positive detector is unusual but load-bearing: it's the
inverse-health check that proves Phase 6 is actually working in a given
deployment. When it fires, the lesson chain is healthy. When it
doesn't fire across N successful runs, something on the chain is
broken silently.

### CLI surface

Two commands on the existing `semspec` binary:

```
semspec watch --bundle <path>     # one-shot dump (the load-bearing case)
semspec watch --live              # TTY heartbeat (read by humans)
```

`--bundle` is the load-bearing case for v1 because the adopter-debug
use-case (ADR-034 §"Why now") is bundle-via-ticket. `--live` is the
nicer-to-have but lower priority — adopters who can't get a bundle
working can ship semspec.log + curl outputs the way the bash blob
already does.

## What's deferred to v2

Not negligence — each one has a specific reason it's not v1.

| Deferred | Why |
|---|---|
| `WedgeZoneApproaching` | Needs time-series matching across multiple metric snapshots. Bundle has start/mid/end snapshots already, but the detector logic to compute trajectory crossings adds enough complexity to deserve its own commit. Fixture exists (`local-easy-0734/`); revisit when the bundle's metric-history shape settles. |
| `OllamaCrash` | Ollama-specific (matches POST 5xx + ollama process disappearance). Cross-provider detectors are higher value. Fixture exists; revisit if multi-adopter use is Ollama-heavy. |
| `EntityIDAsPath` | Narrow cargo-cult variant. Probably better as a sub-shape of a more general "tool argument cargo-cult" detector that we don't have enough fixtures for yet. Fixture exists in `local-easy-v106-0833/`. |
| `BlindRetry` | Today's runs validated `feedback_retries_must_inject_failure_context` is correctly wired across reqgen/archgen/dev/code-reviewer/plan-reviewer/qa-reviewer. The detector would only fire on a *regression* of that fix — useful but not on the urgent path. Worth landing once the wire-up has had a few months to mature. |
| `GraphQueryTimeout` | Tracked separately in `project_graph_query_local_ollama_contention` as a bug fix in semstreams (bump `request_timeout` on graph-query component to ≥300s). Better to fix the bug than detect it. |

## Bundle schema v1

```go
// Package health — pkg/health/bundle.go

type Bundle struct {
    Bundle   BundleMeta             `json:"bundle"`
    Host     HostInfo               `json:"host"`
    Config   ConfigSnapshot         `json:"config"`
    Plans    []Plan                 `json:"plans"`     // PLAN_STATES KV
    Loops    []Loop                 `json:"loops"`     // AGENT_LOOPS KV
    Messages []Message              `json:"messages"`  // most-recent N message-logger entries
    Metrics  MetricsSnapshot        `json:"metrics"`   // parsed from /metrics
    Ollama   *OllamaState           `json:"ollama,omitempty"`
    Diagnoses []Diagnosis           `json:"diagnoses"` // detector output
    TrajectoryRefs []TrajectoryRef  `json:"trajectory_refs,omitempty"`
}

type BundleMeta struct {
    Format       string    `json:"format"`        // "v1"
    CapturedAt   time.Time `json:"captured_at"`
    CapturedBy   string    `json:"captured_by"`   // "semspec-vX.Y.Z"
    Redactions   []string  `json:"redactions"`    // ["api_key_env", "auth_headers", ...]
}

type HostInfo struct {
    OS               string            `json:"os"`           // runtime.GOOS
    SemspecVersion   string            `json:"semspec_version"`
    SemstreamsVersion string           `json:"semstreams_version"`
    Ollama           *OllamaHostInfo   `json:"ollama,omitempty"`
}

type Diagnosis struct {
    Shape       string        `json:"shape"`        // "ThinkingSpiral", etc.
    Severity    string        `json:"severity"`     // "info" | "warning" | "critical"
    Evidence    []EvidenceRef `json:"evidence"`
    Remediation string        `json:"remediation"`
    MemoryRef   string        `json:"memory_ref,omitempty"`
}

type EvidenceRef struct {
    Kind  string `json:"kind"`  // "agent_response", "metric_sample", "log_line"
    ID    string `json:"id,omitempty"`
    Field string `json:"field,omitempty"`
    Value any    `json:"value,omitempty"`
}

type Detector interface {
    Name() string
    Run(*Bundle) []Diagnosis
}
```

`format: "v1"` is the load-bearing string. Schema evolves additively
within v1; breaking changes mint v2 and ship a parallel writer for one
cycle.

## Commit cadence

Each commit is independently shippable. After commit 4 the tool is
useful (one detector running over real bundles); after commit 6 we have
the v1 detector set; after commits 7-8 the live mode + redaction round
out the spec.

| # | Commit | What lands |
|---|---|---|
| 1 | `feat(health): bundle schema v1 + Detector interface` | Pure types in `pkg/health/`. No I/O. JSON round-trip tests. |
| 2 | `feat(health): bundle capture from existing endpoints` | `pkg/health/capture.go` — pulls from `/message-logger/*`, `/metrics`, `/trajectories/{loopId}`, KV buckets. Does NOT run detectors. Outputs Bundle to a tarball. |
| 3 | `feat(cmd/semspec): semspec watch --bundle subcommand` | CLI shim that calls `health.Capture(...)` + writes to disk. Smallest possible code that ties the binary to the package. |
| 4 | `feat(health): EmptyStopAfterToolCalls detector + fixture test` | First detector; canary for the test pattern. Table-driven against `pkg/health/testdata/fixtures/post-adr033-easy-0709/empty-result-attempt1-0721/`. |
| 5 | `feat(health): JSONInText + ThinkingSpiral detectors` | Two text-shape detectors that share machinery (parse `agent.response.payload.message`). Tests against `local-easy-v105-0822/` and `local-easy-v107-rerun-0930/`. |
| 6 | `feat(health): RequirementTimeoutGPUContention + Phase6PositiveLessonChain` | Last two v1 detectors. The positive one needs the fixture from today's run — copy `post-adr033-gemini-positive-0855/` into testdata first. |
| 7 | `feat(health): redaction layer (default env vars + auth headers)` | `pkg/health/redact.go` runs over the captured Bundle before write. Records `bundle.redactions` so receivers know what's missing. |
| 8 | `feat(cmd/semspec): semspec watch --live TTY mode` | Optional polish — live heartbeat + alert on detector match. Cheaper to ship now than later because the detector library is already there. |

## Open questions to resolve during commit 1

- **Where does semstreams version come from?** Likely `runtime/debug.ReadBuildInfo()` against the imported semstreams module. Worth confirming during commit 1; if it can't be read, the field is empty + the bundle still validates.
- **Do we capture trajectories inline or as refs?** Bundle size grows fast if trajectories are inline. Compromise: store the loop IDs in `trajectory_refs[]` and capture trajectories as separate files in the tarball under `trajectories/<loop_id>.json`. Decision goes in commit 1's bundle.go doc comment.
- **What's the canonical bundle file extension?** `.semspec-bundle.tar.gz` is verbose. `.ssbundle` is opaque. Going with `.tar.gz` — it's `tar -tzf`-able by hand without the CLI.

## Work estimate

The plan as written is **one focused session of ~6-8 hours of effective
implementation time** if the bundle schema lands cleanly in commit 1.
If the schema needs revision after commit 4 (fixture-driven discovery
that the detector needs a field the schema doesn't expose), commits 5-6
slip into a second session.

Risk-of-slip flags:
- Commit 2 (capture) is the largest by LOC because each data source has
  its own fetch path. If it grows past ~400 LOC, split into 2a (KV +
  metrics) and 2b (message-logger + trajectories).
- Commit 4 is the canary — first time the test pattern executes against
  a real fixture. If the fixture turns out to need redaction *now* (not
  v2), pull commit 7 forward.

## What this plan deliberately does not do

- **Not a v2 roadmap.** Deferred detectors are listed but not scheduled.
  Once v1 ships and adopter telemetry comes back, prioritisation
  changes.
- **Not a UI dashboard.** ADR-034 §6 explicitly rejects this. `--live`
  is text + alerts, not Grafana.
- **Not a daemon.** ADR-034 §4 — in-process to the binary, not a
  service. Bundle is captured by an explicit CLI invocation.
- **Not provider-agnostic.** Detectors today are semspec/semstreams
  specific. That's a v3 ambition, not a v1 deliverable.

## Linked

- [ADR-034](./ADR-034-watch-cli-and-diagnostic-bundles.md)
- [ADR-033](./ADR-033-lesson-decomposition-via-trajectory.md) — same
  trajectory data sources; the lesson-decomposer is the inverse health
  check.
- `pkg/health/testdata/fixtures/README.md` — fixture-to-detector map
- `feedback_e2e_active_monitoring.md` — bash patterns the bundle/live
  mode replaces
