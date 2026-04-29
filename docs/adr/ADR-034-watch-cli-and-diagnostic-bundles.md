# ADR-034: Watch CLI and Diagnostic Bundles

**Status:** Proposed
**Date:** 2026-04-29
**Authors:** Coby, Claude
**Depends on:** semstreams bump (next cycle), ADR-033 (lesson decomposition — same trajectory APIs), existing message-logger + Prometheus surface

## Context

We have rich observability data already exposed by the running stack:

- `/message-logger/entries` — every NATS message, with full `raw_data` payloads
- `/message-logger/kv/{PLAN_STATES,AGENT_LOOPS,...}` — durable state
- `/metrics` — `semstreams_agentic_*` counters, gauges, histograms (loop_active_loops, model_requests_total, loop_context_utilization, request_tokens_in, length_truncations_total, tool_results_truncated_total, ...)
- `GET /trajectories/{loopId}` (semstreams) — full per-loop tool-call + model-output trace

The data is excellent. The problem is that consuming it requires tribal knowledge: knowing which fields to read, what shapes indicate which failure modes, what to capture before teardown for post-mortem. During the 2026-04-29 local-Ollama @easy session we discovered four distinct failure modes by hand-rolling bash heartbeats and grep filters, then cross-checking against the same data sources. Each diagnosis required three or four ad-hoc curl + jq pipelines. Total bash-blob volume ~150 lines, none reusable.

Two concrete forcing functions push this from "useful tool" to "required infrastructure":

1. **Two early adopters running on different local stacks** are hitting different failure modes today. Manual remote debugging — "send me your logs", "what does `ollama show` say", "paste the agent.response payload" — does not scale past a handful of adopters before support time eats the cycle.

2. **ADR-033's lesson decomposer** consumes the same trajectory data this CLI would. Building both leaves us with two consumers of one data source. Building the CLI's data-access layer first makes ADR-033's path easier; building ADR-033 first makes the CLI's tests easier (real lesson outputs to validate against). Either order works; the data-access patterns must be shared.

Four diagnostic shapes already encoded as tribal knowledge in `feedback_e2e_active_monitoring.md` and `feedback_prompts_reasons_not_rules.md`:

- **JSONInText**: `finish_reason=stop` with JSON in `message.content` — model emitting tool descriptions as text instead of using the function-calling channel. Confirmed on qwen2.5-coder:14b across all roles.
- **ThinkingSpiral**: `finish_reason=stop`, no `tool_calls`, high `completion_tokens` (>500) — model burned generation budget on reasoning channel without emitting an action. Confirmed on qwen3:14b for architecture-gen, all 3 retry attempts.
- **WedgeZoneApproaching**: `loop_context_utilization` climbing past 0.5 OR per-iter duration growing super-linearly with prompt size — predicts the 30b-on-Apple-Silicon stall before it bites.
- **BlindRetry**: a retry loop dispatched with no system or user message containing the prior failure context. Currently true for architecture-gen, plan-reviewer, and likely req-gen / scenario-gen / decomposer (per `project_blind_retry_systemic_gap`).

These shapes will grow. The next 5-10 will emerge from real adopter runs, and each needs to be testable code, not bash heuristics.

## Decision

Ship a **`semspec watch`** CLI subcommand on the semspec binary, backed by a `pkg/health/` detector library and a versioned diagnostic-bundle format. Two modes:

- **`semspec watch --live`** (or `--auto`): renders the heartbeat in TTY, fires alerts when a detector matches, terminates on terminal plan state. Supersedes the bash blobs we wrote this session.
- **`semspec watch --bundle <path>`**: one-shot snapshot dump for adopter handoff. Schema'd JSON tarball, self-diagnosing on receipt.

The decision rests on six load-bearing constraints. Removing any of them collapses this back into "another set of helper scripts."

### 1. Bundle schema is versioned and stable (contract, structural)

`bundle.format` is a string `"v1"` at the top level. Adopters will script against the bundle (filtering, comparing across hosts, attaching to tickets); breaking the schema breaks their automation. Schema evolves additively within a major version; breaking changes get `v2` and a parallel writer for one cycle.

Top-level structure:

```json
{
  "bundle": {"format": "v1", "captured_at": "...", "captured_by": "semspec-vX.Y.Z"},
  "host": {"os": "...", "semspec_version": "...", "semstreams_version": "...", "ollama": {...}},
  "config": {"redacted_endpoints": [...], "active_capabilities": {...}},
  "plans": [...],            // PLAN_STATES KV snapshot
  "loops": [...],             // AGENT_LOOPS KV snapshot
  "messages": [...],          // recent N message-logger entries with raw_data
  "metrics": {...},           // parsed Prometheus metrics, structured
  "ollama_state": {...},      // ollama ps + /api/show per active model
  "diagnoses": [...],         // detector output (see #2)
  "trajectory_refs": [...]    // pointers into trajectories captured separately if too large
}
```

### 2. Detectors are typed code, not regex (testability, structural)

Each diagnostic shape is a Go function:

```go
type Diagnosis struct {
    Shape       string   // "ThinkingSpiral" etc.
    Severity    string   // "info" | "warning" | "critical"
    Evidence    []EvidenceRef  // {kind: "agent_response", id: "...", field: "completion_tokens", value: 2264}
    Remediation string   // "Disable thinking via request body think:false, or switch role to a non-thinking model"
    MemoryRef   string   // pointer to the project_*.md note that documents the shape
}

type Detector interface {
    Name() string
    Run(*BundleSnapshot) []Diagnosis
}
```

Detectors compose: the CLI runs all registered detectors against a bundle. Adding a new shape is a single file + table-driven tests. Tests seed from the captured `/tmp/local-easy-*/` dumps from the 2026-04-29 session — those become the canonical fixtures.

### 3. Privacy-aware redaction by default (security, structural)

Bundles will be shared via PRs, tickets, file shares. Default redaction:

- Env vars matching `*KEY*`, `*SECRET*`, `*TOKEN*`, `*PASSWORD*` → `<redacted>`.
- Header values for known auth headers (`Authorization`, `X-API-Key`).
- The full text of user messages in `agent.request` payloads when `--redact-prompts` is set (default false for first version; default true once we hit external adopters).
- Configurable allowlist via `~/.semspec/diagbundle.yaml` for adopters who need to share specific prompt content.

A bundle that has been redacted records `bundle.redactions = ["api_key_env", "auth_headers", ...]` so receivers know what's missing.

### 4. In-process to the semspec binary, not a daemon (operational, structural)

`cmd/semspec/watch.go` lives in the existing binary. No new container, no new service to operate, no new ports. The CLI is a pure consumer of existing endpoints; if the stack is up, watch works. If the stack is down, watch reports stack-down with the same redacted-bundle shape.

Adopters install nothing new; if they have semspec, they have `semspec watch`.

### 5. Reuses existing data sources, no new APIs (composability, structural)

Data sources are exclusively the ones listed in Context. No new endpoints on semspec, no new fields on semstreams, no NATS subscription topology. Future detectors that want richer signal must either (a) work with existing data, (b) propose a new metric/log line as a separate change, or (c) wait for ADR-033's trajectory access patterns.

### 6. Scope discipline (what this is NOT)

- **NOT a service or daemon.** A watch process running 24/7 against a production stack is a different product (alerting/SRE territory) and not what adopters need.
- **NOT a GUI dashboard.** Grafana on `/metrics` already covers ongoing-health visualization. This tool is for active diagnosis of "is this run going wrong right now and why?" — a question text + alerts answer better than charts.
- **NOT a prediction system.** Detectors fire on shapes that have already manifested in the data. Predictive heuristics ("this run looks like it might fail soon") are scope creep until we have enough adopter telemetry to validate them empirically.
- **NOT cross-project portable.** This consumes semspec/semstreams APIs specifically. Generalizing to "any agentic system" is a much bigger ambition that doesn't pay back at our adopter count.
- **NOT a replacement for ADR-033's lesson decomposer.** Lessons span runs and inform future prompts; diagnoses describe one run for human triage. Different scope, different consumers, different lifetimes.

## Consequences

**Adopter support shifts from live debugging to bundle triage.** Adopter runs `semspec watch --bundle /tmp/run.tgz`, attaches it to a ticket, we read the bundle's `diagnoses[]` and either match a known shape (point at the corresponding `project_*.md` memory) or file a new detector. Time-to-diagnosis drops from "schedule a debug session" to "open the bundle."

**Diagnostic shapes leave tribal knowledge.** Today the four shapes live in memory entries and my head. After this lands, each shape is a Go file with table-driven tests against captured fixtures. Drift becomes impossible without breaking tests.

**Cross-host comparison becomes diff-able.** Two adopters' bundles are JSON of identical schema, so `jq` and ordinary diff tools work. We can ask "what's different between adopter-A's success and adopter-B's failure" without manual translation.

**ADR-033's lesson decomposer becomes verifiable.** Run `semspec watch --bundle` on a plan that hit a reviewer rejection; check whether the lesson decomposer emitted an evidence-cited lesson with a non-trivial `injection_form` and a correctly-attributed `root_cause_role`. This was previously something we'd have to manually inspect; the bundle records it as structured data.

**The detector library is a long-lived abstraction.** Once shipped, every new failure mode discovered should land as a new detector + fixture, not as another paragraph in a memory file. Prevents the slow accumulation of tribal-knowledge debt that this session highlighted.

## Sequencing

This ADR sequences **third** in the current implementation cycle:

1. **semstreams bump** — confirms data-source stability; possibly closes some open gaps (`project_inline_think_blocks_gap`, `project_graph_query_local_ollama_contention`, agentic-loop deadline enforcement). Validates against Gemini @easy 8/8.
2. **ADR-033** lesson decomposition — adds the trajectory-consumer infrastructure. The watch CLI consumes the same trajectory APIs; building this first concretizes the access patterns.
3. **THIS** — `semspec watch` CLI + `pkg/health/` + bundle format.

Why not earlier: the diagnostic shapes encode tribal knowledge from runs against the *current* semstreams. If the bump or ADR-033 changes data-source surfaces (new fields, removed ones, restructured trajectory format), shapes built first need rework.

Why not later: every additional early adopter without this multiplies support cost. Two today; if we hit five before this lands, we'll be drowning in manual triage.

## Rejected Alternatives

**A separate `semspec-watch` service/daemon.** Considered for the always-on watch case. Rejected: adds operational surface (port, container, metrics, lifecycle) for a problem that's solved by a CLI that adopters invoke when they want to. Daemon-style monitoring is real but it's the SRE problem, not the adopter-debugging problem.

**Pure documentation / playbook in markdown.** Considered as the cheapest option — write the four shapes up clearly, let adopters run the bash. Rejected: doesn't scale (each adopter needs the same setup time we burned in this session), no tests (shapes drift), no cross-host comparability (every adopter improvises their own format).

**Generalized agentic monitoring tool (provider-agnostic, multi-system).** Tempting because the four shapes are not semspec-specific; they're "what failure looks like in any tool-using LLM agent." Rejected: scope explosion. We can't validate cross-project shapes without cross-project deployment data, and we don't have it. Build for semspec; if a future fork wants to lift the detector library, the typed interfaces in `pkg/health/` make it possible without committing now.

**A UI page in the semspec frontend.** Considered as an alternative to the CLI. Rejected: adopters running locally don't always have the UI up (some run headless against the API only), the UI doesn't help with the bundle-share-via-ticket use case, and it solves the wrong frame (passive viewing vs active capture).

**Build the detector library only, defer the CLI.** Tempting because the library is the long-lived part. Rejected: the CLI shell is small (~300 LOC) and the value is in adopters being able to run *one command*. Library without CLI is not deliverable to adopters; it's an internal Go package only.

## Evidence to seed implementation

Captured during the 2026-04-29 session and preserved at:

- `/tmp/local-easy-0734/` — 30b-on-Apple-Silicon wedge: Ollama crashed mid-loop after iter 5 hit ~14K prompt tokens. Fixture for `WedgeZoneApproaching` and `OllamaCrash` detectors.
- `/tmp/local-easy-v105-0822/` — qwen2.5-coder:14b emitting `{"name":"graph_summary"}` as text content with `finish_reason=stop`. Fixture for `JSONInText` detector.
- `/tmp/local-easy-v106-0833/` — qwen3:14b@temp0.6 cargo-culting entity IDs as bash paths over four iterations. Fixture for `EntityIDAsPath` detector (architecture variant of cargo-cult).
- `/tmp/local-easy-v107-rerun-0930/` — qwen3:14b architecture-gen exhausting 3 retries with `finish_reason=stop` + no tool_calls + completion_tokens=915 each. Fixture for `ThinkingSpiral` and `BlindRetry` detectors.

These are the canonical fixtures. They should be moved into the repo at `pkg/health/testdata/fixtures/` (with prompts redacted appropriately) before the `/tmp` files age out.
