# Design — Real-time churn detection + intervention

All file:line references verified against main `8aca8a5f` (2026-06-23).

## Existing substrate (reuse)

- **Detector framework** — `pkg/health/detector.go`: pure `Detector.Run(*Bundle) []Diagnosis` (no I/O/clock — deterministic for golden tests). `Severity` = info/warning/critical/undetermined. `Diagnosis{Shape, Severity, Evidence, Remediation, MemoryRef}`. `RunAll` aggregates.
- **Live loop** — `cmd/semspec/watch_live.go`: `liveDetectors()` (~:302) wires 7 detectors, evaluated every tick; ALERT emission + dedupe (~:218-234, `seen[alertKey]`); `--bail-on <sev>` exits when tick max severity ≥ threshold; heartbeat prints `plans loops msgs active_loops ctx_util errors`.
- **Closest existing detectors** (neither catches real churn):
  - `RapidShallowToolCalls` — ≥6 tool-call responses in one loop with 0 `submit_work` (counts responses, not reads-vs-writes, no iteration #).
  - `RepeatToolFailure` — same `(loop, tool, error-class)` failure ≥3× consecutively (only *failing* tool results; misses "succeeding calls, no worktree progress").

## The churn signature lives in `agentic.Trajectory`

- `agentic/trajectory.go`: `TrajectoryStep{StepType("model_call"|"tool_call"|"context_compaction"), ToolName, ToolArguments, ToolResult, ToolStatus("success"|"failed"), ErrorMessage, TokensIn/Out, ...}`; `Trajectory{LoopID, Steps[], Outcome}`.
- This is enough to compute: read-vs-write tool ratio, repeated near-identical `ToolArguments`, repeated failing compiles on the same target — correlated with iteration position from `LoopEntity.Iterations`/`MaxIterations` (`agentic/state.go:52-53`).
- **Gap 1 (plumbing):** trajectory *bodies* are NOT in the Bundle — `orchestrate.go` captures them to sibling tarball files; the Bundle carries only `TrajectoryRefs` (step *count* + outcome). A pure `Detector.Run(*Bundle)` cannot see steps today. Either thread trajectory bodies into the Bundle, or run the churn detector with direct trajectory access (outside the pure-detector contract).
- **Gap 2 (semantics):** no tool is classified "read" vs "worktree write" anywhere — this change must define that mapping (e.g. `bash cat/grep/ls` + file-read = read; edit/write/apply-patch = write).

## Recovery is post-mortem only — the actuation gap

- Recovery fires **only after a loop terminates**, gated on `max_tdd_cycles` (=3, `execution-manager/config.go`) / agentic-loop `max_iterations` (=20). Publishers of `recovery.requested.<slug>`: execution-manager `markEscalatedLocked`, plan-manager `escalateRevision`/`fireQAVerdictRecovery`, requirement-executor retry-exhaustion — **every site is post-termination**. No mid-loop publisher exists.
- The agentic-loop is event-driven; `HandleModelResponse` re-reads the loop entity each iteration (`handlers.go:~883`) — a natural between-iteration checkpoint.
- **Interrupts:** only `agent.signal cancel` has real mid-flight effect (drains tool calls, `CancelLoop`). `pause` sets `PauseRequested` but it is **never read** (dead). `feedback`/`retry` signal types pass `Validate` but have **no handler** (dropped as "Unsupported signal type"). So mid-loop hint injection is **unwired** today.
- **Hint pipe bug:** recovery's `refined_prompt` is parsed/validated but never propagated — `deriveDecision` returns `diagnosis` (not `refined_prompt`); only `diagnosis` reaches the dev via `applyRecoveryHint` → `Requirement.RecoveryHint` → "MANAGER RECOVERY GUIDANCE" block. A churn hint must land in `diagnosis` to be delivered (or fix the `refined_prompt` drop).

## Build plan (incremental, lowest-risk first)

1. **Define the churn signature** from real trajectory data (start with the 2026-06-23 architect trajectory `cad2673f-...` and any dev-loop trajectory). Pin thresholds (read:write ratio, repeat count, iteration floor) against real runs, not guesses.
2. **Alert-only detector** in `pkg/health` (+ Gap 1 plumbing). Validates the signal with zero behavior risk; rides the existing ALERT/`--bail-on` path.
3. **Intervention bridge** (only after the signal is trusted): cancel-and-recover (publish `agent.signal cancel`, then bridge cancellation → `recovery.requested` — `handleDeveloperCompleteLocked` currently routes `OutcomeCancelled` through cycle-consuming retry, so this bridge is new), or wire the dead `feedback` signal for true mid-loop injection. Fix the `refined_prompt` drop so hints land.
4. **"Wrong gate" disposition.** Churn is often a false-reject symptom (2026-06-23: R-arch flagged companion tests the system auto-owns — a prompt↔system mismatch, not architect incapacity). When the churning agent has already structurally satisfied the gate, escalate the *gate* for review instead of looping the agent.

## Open questions

- Pure-detector purity vs. trajectory access: thread bodies into the Bundle (keeps detectors pure/testable) or special-case the churn detector with live trajectory fetch?
- Cancel-and-recover vs. mid-loop feedback injection — which interrupt model? Cancel is wired but discards the worktree; feedback injection preserves it but is net-new and the loop's pause checkpoint is currently dead.
- Detector authority: should the sidecar (an external observer) be allowed to actuate the system, or should the churn detector run *inside* a component (execution-manager) with the sidecar staying observe-only?
