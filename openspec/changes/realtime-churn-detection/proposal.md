## Why

Recovery today is **post-mortem and limit-based**: the system only reacts to a wedged agent loop *after* a budget LIMIT is exhausted — `max_tdd_cycles` (default 3), the agentic-loop `max_iterations` (default 20), or a plan-review revision cap (3/3). By then the loop is dead, the worktree is discarded, and the paid tokens are spent. Two failure shapes seen live on `mavlink-hard` runs make the cost concrete:

- **Dev contract thrash** — a developer reverse-engineering an upstream method contract through repeated edit→compile→fail cycles (the 2026-06-13 `ICommandStatus` loop burned ~3.5M tokens). Signature: many reads, ~0 worktree writes, repeated failing compiles on the same target.
- **Architect gate oscillation** — an architect oscillating `generating_architecture ↔ reviewing_architecture` for 3 rounds against a reviewer finding, then dead-rejecting at the revision cap (2026-06-23 run).

The right model is **real-time detection + intervention**, not post-mortem limits — and SemSpec **already has the substrate**: the `semspec watch --live --bail-on` sidecar runs a pure detector framework (`pkg/health`) against live system state every tick and emits `ALERT`/`BAIL` lines. It is **observe-only** today: it can warn and exit, but it cannot intervene in the running system. This change adds (a) a churn detector and (b) the missing observe→actuate path, so a churning loop is caught and helped *while it is still running* instead of after it burns its budget.

A second, load-bearing lesson from the 2026-06-23 run: **churn is frequently a symptom of a wrong gate, not a stuck agent.** That run's architect "churn" was a *false-reject* — the R-arch reviewer flagged companion test files as unowned even though the system auto-owns them via companion-test expansion. So the intervention must be able to conclude "the gate may be wrong" (escalate the finding for human/meta review), not only "the agent is stuck, retry with a hint."

## What Changes

- Add a **churn detector** in `pkg/health` that reads developer/agent trajectory steps and flags the churn signature: high read-to-write ratio over N iterations, repeated near-identical tool calls, or repeated failing compiles on the same target — mid-loop, before budget exhaustion.
- Plumb **trajectory bodies into the detector Bundle** (today only `TrajectoryRefs` step-counts are in-bundle; the per-step `ToolName`/`ToolArguments`/`ToolStatus` data a churn detector needs lives in sibling tarball files, invisible to a pure `Detector.Run(Bundle)`).
- Add a **detector → intervention bridge**: on a confirmed churn diagnosis, trigger recovery *for a still-running loop* — either cancel-and-recover (the only clean interrupt today is the `agent.signal cancel`) or by wiring the currently-dead `feedback` signal for true mid-loop hint injection.
- Fix the **hint-delivery pipe**: the recovery agent's structured `refined_prompt` is parsed but dropped — only `diagnosis` text reaches the dev via `Requirement.RecoveryHint`. A targeted churn hint (e.g. "read `/sources/<ns>` for class X's method contract before coding") must actually reach the next dispatch.
- Add a **"wrong gate" disposition**: when the churn is an agent unable to satisfy a gate it has already structurally satisfied (false-reject), surface a finding for meta/human review rather than looping the agent against an unwinnable criterion.
- Keep it **alert-only first**: ship the detector emitting diagnoses (no intervention) so the churn signature is validated against real trajectories before any actuation is wired.

## Capabilities

### New Capabilities

- `realtime-churn-detection`: A live detector that recognizes a churning agent loop from its trajectory (reads-without-writes, repeated tool calls, repeated same-target compile failures) and emits a graded diagnosis while the loop is still running.
- `churn-intervention`: A path from a churn diagnosis to a mid-loop intervention — cancel-and-recover or mid-loop hint injection — including delivery of a targeted, actionable hint to the next dispatch, and a "the gate may be wrong" escalation when the churn is a false-reject.

### Modified Capabilities

- `execution-observability`: the watch sidecar gains an actuation path (detection → intervention), not just ALERT/BAIL.

## Impact

- `pkg/health` (detector framework, Bundle, orchestrate trajectory capture), `cmd/semspec/watch_live.go` (`liveDetectors()`, ALERT/bail plumbing).
- semstreams agentic-loop signal handling (`agent.signal.*`: the unwired `feedback`/`pause` signals), recovery-agent hint delivery (`refined_prompt` dropped), execution-manager cancel→recovery bridge.
- No breaking API removals intended; alert-only phase is purely additive.
