# Semspec — mavlink-osh-hard 2026-05-29 (hybrid gpt-5.5 + gemini-pro)

**Real-LLM verification of the autonomous QA-recovery cascade against a
HYBRID model registry (gemini-pro planner + gpt-5.5 dev/reviewer) on
the OpenSensorHub MAVSDK driver epic.**

Run timestamp: 2026-05-30T00:48:24Z → 02:42:30Z. Wallclock: **114 minutes.**
Outcome: **6/8 Playwright assertions passed**; tests 7 & 8 hit the
1-hour-per-requirement hard cap before convergence. Both failure modes
diagnosed and config-tuned in the same session (see `operator-rules/`).

This is a companion to the 2026-05-28 mavlink-decode pack — that one
verified catalog-backed harness selection on a small Go scope; this one
exercises the same selection at OSH driver / Java / Gradle epic scope
AND demonstrates the autonomous QA recovery chain firing end-to-end on
a hybrid model config.

## What this run verifies

1. **Catalog-backed harness profile selection reproduces across model
   configs.** The architect (gemini-pro) on this run picked
   `[mavlink.px4-sitl.mavsdk-smoke, mavlink.raw-mavlink-direct]` —
   bit-identical to what gemini-only mavlink-decode picked on
   2026-05-28. PR #18's catalog mechanism is robust.
2. **gpt-5.5 dev role produces real Java/Gradle code.** 13 reviewer
   verdicts across the run, **8 approved + 5 rejected (all
   fixable)** — most rejections resolved on the next TDD cycle. The
   "OpenAI for code" hybrid configuration works in practice once the
   `reasoning_effort + tools` API constraint is removed from the
   endpoint (see `operator-rules/`).
3. **Autonomous QA-recovery cascade fires across model configs.** The
   2026-05-29 morning validation used gemini for every role. This
   afternoon's run reproduces the same PR #33 (auto-accept) + PR #34
   (budget gate) chain end-to-end with the dev/reviewer pair on
   gpt-5.5 instead. Same cascade subjects, same auto-accept timing,
   same downstream cascade reaching `Resuming requirement from
   awaiting-recovery — re-decomposing`.
4. **Full pre-execution cascade resilient under ADR-029 retry.** The
   scenarios reviewer's first-pass rejection drove a re-decomposition
   cycle that came back through requirements → architecture →
   scenarios → reviewing and passed on cycle 2. Total cascade time
   270.3s. Same shape as run #1 (the aborted-config run) at 195.8s.

## What this run did NOT verify

- Plan reaching stage `complete`. Both requirements hit the
  `requirement-executor.timeout_seconds: 3600` (1h hard cap) before
  the executor recorded their completion. Requirement 1 had actually
  produced `workflow_step=requirement outcome=success` at the 70-min
  mark but the 1h cap reaped it anyway.
- The live PX4 SITL MAVSDK smoke test (acceptance criterion #5). The
  architect selected the profile but the dev was still iterating on
  the coverage-matrix Gradle task when budgets hit.

Both gaps are addressed by the config tuning shipped in the same
session — see [`operator-rules/config-tuning-applied.md`](operator-rules/config-tuning-applied.md).

## The scenario (the "@mavlink-hard" prompt)

Goal handed to the agent, verbatim — see
[`run-narrative/prompt.md`](run-narrative/prompt.md).

Source: derived from the upstream
[opensensorhub/osh-addons](https://github.com/opensensorhub/osh-addons)
reference at `sensors/robotics/sensorhub-driver-mavsdk`.

## Run topology

- Provider: `hybrid-gpt5` (`configs/e2e-hybrid-gpt5.json`)
  - Planner / plan-reviewer / architecture / scenario-gen / QA: `gemini-3.1-pro-preview` (`reasoning_effort: medium`)
  - Dev / reviewer / coding fallback: `gpt-5.5` (no `reasoning_effort` — see operator note)
  - Fast capabilities: `gemini-3-flash-preview`
- Fixture: `test/e2e/fixtures/osh-driver-mavsdk` (Java/Gradle/OSH driver skeleton, baseline commit `2741077`)
- EPIC overlay: 5 external semsources pre-cloned at `/sources/`
  (osh-core, ogc, meshtastic, **mavsdk-java**, **osh-addons** — last 2 new this PR)
- Harness catalog: `workflow/harnesscatalog/catalog/mavlink.yaml`
  (4 profiles, `mavlink.px4-sitl.mavsdk-smoke` selected + `mavlink.raw-mavlink-direct`)
- qa_level: `integration`
- Recovery config (run #2): `max_recovery_restarts=1`, `recovery_timeout_seconds=60`,
  `requirement-executor.timeout_seconds=3600`
- Recovery config (run #3 retune): `max_recovery_restarts=2`, `recovery_timeout_seconds=120`,
  `requirement-executor.timeout_seconds=7200` — see operator-rules

## Test results

| # | Test | Result | Wallclock |
|---|---|---|---|
| 1 | plan created with goal | ✓ | 11ms |
| 2 | plan reviewed and approved | ✓ | 12.1s |
| 3 | requirements generated (2 reqs) | ✓ | 15.0s |
| 4 | architecture + harness profiles selected | ✓ | 1.8m (108s) |
| 5 | scenarios generated and reviewed (10 scenarios) | ✓ | 4.5m (270s) |
| 6 | execution triggered | ✓ | 1.2s |
| 7 | execution completes | ✗ (timeout) | hit 1h/req cap |
| 8 | trajectories exist after execution | (waiting on 7) | — |

## Sponsor-pack key claims

| Claim | Evidence path |
|---|---|
| Catalog selection reproducible across models | `evidence/playwright-result.md` (line `harness_profiles=...`) |
| Full TDD pipeline (dev → validator → reviewer) on gpt-5.5 | `evidence/review-verdicts.md` |
| Autonomous QA recovery cascade fires end-to-end | `evidence/recovery-trace.md` |
| Real Java/Gradle code produced by gpt-5.5 dev | `evidence/workspace.tar.gz` |
| Outer 1h/req cap is the binding budget (not model quality) | `evidence/timeline.md` (2026-05-30T02:42:29 `execution timed out`) |

## Run #3 follow-up (tuned-budget retry, same session)

After identifying the 4 budget knobs in `operator-rules/config-tuning-applied.md`,
run #3 was fired the same session. **Result: 6/8 pass (same headline)
but the bottleneck moved.** Pre-execution cascade was 2× faster (6.5
min to implementing vs 12 min). 9 approved + 7 fixable rejected
verdicts (more node work than run #2). 1 requirement officially
`completed` in plan-manager. **The bottleneck turned out to be
requirement granularity, not budget** — the third requirement
bundled "Implement driver + fallback + tests" which exceeds any
per-requirement budget envelope.

Full narrative + recommended fix for run #4: [`run-narrative/run3-followup.md`](run-narrative/run3-followup.md).

Run #3 evidence in [`evidence/run3/`](evidence/run3/):
- `hydrated/` — 5 plan artifacts (plan.json, plan.md, architecture.md, requirements.md, scenarios.md)
- `review-verdicts.md` — 16-row verdict table
- `recovery-trace.md` — plan decisions (3 total)
- `timeline.md` — execution-manager + cascade events
- `watch.log`
- `workspace-source-only.tar.gz` (92K — meaningful files only)
- `agent-source-final-node/` — full source of one approved node (build.gradle, README.md, BuildGradleConfigurationTest.java)

## Companion documents (run #2)

### Hydrated plan artifacts (what the agent actually emitted)

Extracted from `/workspace/.semspec/plans/7cb2e6e5ae4b/`:

- [`run-narrative/hydrated/plan.json`](run-narrative/hydrated/plan.json) — full structured plan (26K) with harness_profiles incl. per-profile `purpose` + `covers` + `used_by`
- [`run-narrative/hydrated/plan.md`](run-narrative/hydrated/plan.md) — human-readable plan (12K)
- [`run-narrative/hydrated/architecture.md`](run-narrative/hydrated/architecture.md) — architect's full output (7K)
- [`run-narrative/hydrated/requirements.md`](run-narrative/hydrated/requirements.md) — requirements decomposition (5K)
- [`run-narrative/hydrated/scenarios.md`](run-narrative/hydrated/scenarios.md) — 10 BDD scenarios (8K)

### Run narrative (curated)

- [`run-narrative/prompt.md`](run-narrative/prompt.md) — exact goal
- [`run-narrative/architecture.md`](run-narrative/architecture.md) — selected harness profiles + rationale + cross-run reproducibility table
- [`run-narrative/recovery-cascade.md`](run-narrative/recovery-cascade.md) — narrated trace of the cascade firing

### Evidence (machine-extracted)

- [`evidence/review-verdicts.md`](evidence/review-verdicts.md) — 13-row verdict table
- [`evidence/recovery-trace.md`](evidence/recovery-trace.md) — plan decisions + cascade events
- [`evidence/timeline.md`](evidence/timeline.md) — key execution-manager events
- [`evidence/playwright-result.md`](evidence/playwright-result.md) — spec stage transitions
- [`evidence/watch.log`](evidence/watch.log) — sidecar heartbeat + alerts
- [`evidence/workspace.tar.gz`](evidence/workspace.tar.gz) — sandbox /workspace at run end (2.5 MB; full Java/Gradle worktree tree)

### Agent-produced code (selected)

From node-4ee1d9c3 (a successfully-approved coverage-matrix node):

- [`code/agent-source/build.gradle`](code/agent-source/build.gradle) — agent-filled-in build.gradle (8K vs 2K skeleton)
- [`code/agent-source/CoverageMatrixTest.java`](code/agent-source/CoverageMatrixTest.java) — JUnit test the agent wrote (4K)
- [`code/agent-source/CoverageMatrixGradleTaskTest.java`](code/agent-source/CoverageMatrixGradleTaskTest.java) — Gradle TestKit task test (6K)
- [`code/agent-source/README-from-node4ee1d9c3.md`](code/agent-source/README-from-node4ee1d9c3.md) — agent's evolved README (12K vs 2K skeleton)

Per-worktree READMEs (one per task node, 7 files):

- [`code/worktree-readmes/`](code/worktree-readmes/) — snapshot of each node's README at its respective approval/rejection point

### Operator rules

- [`operator-rules/config-tuning-applied.md`](operator-rules/config-tuning-applied.md) — 4 knob diffs for run #3
- [`operator-rules/openai-constraint.md`](operator-rules/openai-constraint.md) — the `reasoning_effort + tools` discovery
