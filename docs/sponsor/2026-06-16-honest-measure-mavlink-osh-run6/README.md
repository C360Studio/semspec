# Semspec — Honest Capability Measure: MAVLink/OSH driver, run #6 (2026‑06‑16)

> **What this is.** A second truthful snapshot of Semspec's autonomous pipeline on the same
> hard task (build an OpenSensorHub MAVLink/MAVSDK driver), captured one day after the first
> honest pack (`docs/sponsor/2026-06-15-honest-measure-mavlink-osh/`). Its purpose is a
> **direct, like‑for‑like comparison**: *did the generated code get better than the last one
> we captured?* Every claim here is backed by quoted run evidence and was independently
> verified — the deliverable was rebuilt from source and its tests re‑executed **by hand**,
> outside the pipeline, before any statement was made.
>
> **The headline:** **yes — materially better on the dimensions the pipeline now measures.**
> Run #6's deliverable carries the OSH module‑discovery registration the last one was missing,
> a fuller OSH module topology, and — for the first time — the end‑to‑end integration‑QA gate
> *actually compiled and ran the project's test suite* (osh‑core resolved from source, no 401)
> instead of dying at dependency resolution. It then **honestly failed closed** on integration
> tests that cannot run in‑sandbox (they need a live SITL simulator). Same MVP scope ceiling,
> same shallow raw‑MAVLink transmit path, plus one *new* defect the new completeness gate
> induced (a duplicated module provider). Honest red, real progress.

## The one‑paragraph version

On 2026‑06‑16, on `main` (all four hardening PRs from the prior session merged: sandbox
`NATS_URL`, OSH‑source substitution, non‑fatal QA subscriber, scratch/argfile scoping),
Semspec autonomously took a single prompt ("OSH MAVLink/MAVSDK driver") through plan →
architecture → scenarios → a **5‑requirement** execution phase, producing **782 lines of
main Java + 616 lines of tests across 6 main classes + 8 test classes** in an idiomatic
OpenSensorHub `UnmannedSystem` module shape. **All 5 requirements completed with real
per‑node evidence** (`phase=completed` + `nodes_completed` each — the 2026‑06‑14
dedup‑false‑complete failure mode did **not** recur). A new `osh-module-provider-registration`
completeness gate **fired 4× mid‑run** and drove the model to produce the
`META-INF/services/...IModuleProvider` registration the previous run lacked. The end‑of‑plan
**integration‑QA gate then compiled the whole deliverable with `osh-core` built from source**
(`BUILD SUCCESSFUL in 53s`, no 401) and ran `./gradlew test` — **13 unit tests passed, 0
failures** — then **correctly rejected the plan** because **6 `@integration` tests skipped**
(they `assumeTrue(SITL_ENDPOINT)` and there is no live PX4 simulator in the sandbox). That is
a **true negative, not a fake green**: the code builds and its unit surface is green, but the
behavior that actually matters (connect to a vehicle, stream telemetry, execute commands) is
**unverified**, and the pipeline declined to call it done.

## Did the code get better than last time? (the core question)

**Baseline = run #4** (the deliverable reviewed in the 2026‑06‑15 pack, assembled branch
`semspec/plan-f76c9d8ba0ac`). **This = run #6** (assembled branch `semspec/plan-2b996a0aabe6`).

| Dimension | Run #4 (last pack) | Run #6 (this pack) | Verdict |
|---|---|---|---|
| OSH module‑discovery registration (`META-INF/services/...IModuleProvider`) | **missing** (flagged weakness #3) | **present** (driven by new gate that fired 4×) | ✅ **better** |
| OSH module topology | single `MavsdkSensor` + outputs/commands | `UnmannedSystem` + `Activator` + `Descriptor` + `Config` + `MavLinkCommNetwork` + `MavlinkDirectStream` | ✅ **fuller / closer to a real module** |
| Did integration QA actually build + test the deliverable? | **no** — died at dependency resolution (osh‑core 401) | **yes** — `BUILD SUCCESSFUL`, osh from source, `./gradlew test` ran | ✅ **better (harness #196)** |
| Real per‑requirement work, no dedup/false‑complete | yes (4 reqs) | yes (**5 reqs**, each `phase=completed` + `nodes_completed`) | ✅ **held** |
| Unit‑test surface | 23 tests / 0 fail (built by hand from source) | 19 tests, **13 executed / 0 fail**, 6 skipped (built **by QA itself**) | ≈ **comparable; now verified in‑pipeline** |
| Scope | vertical‑slice MVP (2 telemetry, 2 commands) | vertical‑slice MVP (telemetry pos+battery; cmds Arm/Takeoff/MissionUpload) | ≈ **same ceiling** |
| Raw‑MAVLink **transmit** path | dummy‑checksum encode, unwired island | `sendMessage` echoes payload (no real frame encode/CRC), `setTransport` never wired | ≈ **same class of weakness** |
| Committed build self‑containment | not self‑contained (401) | still declares external `org.sensorhub:sensorhub-core`; builds **only** because the harness substitutes source | ≈ **deliverable unchanged; harness improved** |
| New defect introduced | — | **duplicate `IModuleProvider`** (`Activator` ≈ `Descriptor`, both registered → module discovered twice) | ⚠️ **new, gate‑induced** |
| Final QA verdict | rejected — build not self‑contained (401) | rejected — mandatory integration tests skipped (no SITL) | both **honest red, deeper layer reached** |

**Net:** run #6 is a genuine step forward. The pipeline now produces a deliverable that
*compiles and unit‑tests green inside its own QA gate*, carries the module registration and
topology a real OSH driver needs, and reaches a **deeper, more meaningful failure** (the
behavior is untested) than run #4's (it didn't build). The residual gaps are stable (MVP
scope, shallow transmit path) and there is one new, clearly‑characterized regression the new
completeness gate caused.

## What is proven (with evidence)

| Claim | Evidence |
|---|---|
| Plan converged through a **5‑requirement** execution phase | assembled branch `semspec/plan-2b996a0aabe6`; reqs `.1`–`.5` each `phase=completed` |
| All requirements completed with **real** node evidence (no dedup/false‑complete) | `requirement.2b996a0aabe6.{1..5} nodes_completed` + `phase=completed` (×5) |
| New module‑registration completeness gate actually fired | `osh-module-provider-registration first_failure_excerpt=` ×4 during the run |
| Module registration is now present | `src/main/resources/META-INF/services/org.sensorhub.api.module.IModuleProvider` exists |
| Integration QA compiled the deliverable with osh **from source** | QA log: `OSH source substitution enabled (includeBuild from source)` → `BUILD SUCCESSFUL in 53s` |
| Unit tests pass; integration tests skip (the honest‑red) | independently re‑run: **19 tests, 13 executed, 0 failures, 0 errors, 6 skipped** (the 6 `@Tag("integration")` SITL tests) |
| QA **failed closed** (no fake green) | `QA verdict needs_changes at level integration` → `execution_exhausted action=escalate_human` → `to=rejected` |
| The skip reason is real | verdict summary: *"rejected … due to skipped mandatory integration tests (MavsdkSmokeTest and UnmannedSystemTest). The tests skip when SITL_ENDPOINT is missing, leaving the core capabilities … entirely untested."* |
| The build green is real, not a warm‑cache artifact | re‑verified by hand in a fresh worktree, isolated `maven.repo.local`, osh from source → `BUILD SUCCESSFUL`, **0 failures** |

## What is honestly *not* done

- **The behavior that matters is unverified.** The 6 integration tests that connect to a
  vehicle, assert telemetry emission, and run takeoff/arm commands **skip without a SITL
  simulator**. Unit tests cover framing, command‑status objects, config, and the provider
  classes — not end‑to‑end flight behavior.
- **Duplicate module provider (new defect).** `UnmannedActivator` and `UnmannedDescriptor`
  are near‑identical `IModuleProvider` implementations both registering the same
  `UnmannedSystem`/`UnmannedConfig`, and **both** are listed in `META-INF/services`. OSH's
  `ServiceLoader` would discover the module twice. The new registration gate forced
  *presence* but not *correctness* (an "Activator" in OSH/OSGi should be a `BundleActivator`,
  not a second descriptor).
- **Raw‑MAVLink transmit is shallow and unwired.** `MavLinkCommNetwork.sendMessage` just
  echoes the stored payload bytes (no real frame encode / CRC‑16/MCRF4XX), and
  `setTransport(...)` is never called in `UnmannedSystem`'s wiring — only the *receive* path
  (UDP listen → v1/v2 decode → publish) is integrated. The QA reviewer additionally flagged a
  *suspected* `NullPointerException` in the raw‑fallback publish path; because the tests that
  exercise it skip, that is the reviewer's static assessment, not a build‑proven failure — but
  it is consistent with the shallow/unwired transmit path found by reading the code.
- **The committed build is still not self‑contained.** `build.gradle` declares
  `org.sensorhub:sensorhub-core:2.0.1` from `mavenLocal()/mavenCentral()` (where OSH is not
  published); it builds **only** because the harness now substitutes osh‑core from source. A
  clean third‑party checkout without OSH source/credentials would still fail to resolve.

Full detail: **[build-and-qa-findings.md](build-and-qa-findings.md)** and
**[code-review.md](code-review.md)**.

## Why this is the measure worth showing

The first honest pack (one day earlier) established the principle: a green pipeline is not a
verified delivery, and the win is **honesty under a fail‑closed gate**, not a greener
checkmark. Run #6 is the follow‑through. Two new completeness gates
(`osh-module-provider-registration`, `java-implementation-completeness`) plus the
source‑substitution fix let the pipeline reach a strictly **deeper, more honest** verdict than
the day before: it built the artifact, ran its tests, told the truth about what those tests do
*not* cover, and even surfaced a defect (the duplicate provider) that the gate it satisfied
introduced. That self‑correcting, evidence‑producing behavior — including catching the
limits of its own new gates — is the capability worth showing.

## Reproduce / audit

- Run: `WITH_EPIC=1 DEBUG=1 task e2e:watch:llm -- gemini mavlink-hard` (code under test:
  `main`, all four hardening PRs merged; slug `2b996a0aabe6`).
- Raw run artifacts: `/tmp/semspec-watch-gemini-mavlink-hard-20260615-231127/`
  (`semspec.log`, `poll.log`, `bundle.tar.gz`, snapshot).
- QA artifact (the in‑pipeline build log): in‑sandbox
  `/workspace/.semspec/qa-artifacts/2b996a0aabe6/.../integration-test.log` (`BUILD SUCCESSFUL`,
  osh from `/tmp/semspec-osh-src`).
- Independent code‑works proof: assembled branch `semspec/plan-2b996a0aabe6` rebuilt by hand
  with osh from writable source + isolated `maven.repo.local` → `BUILD SUCCESSFUL`,
  **19 tests / 13 executed / 0 failures / 6 skipped**.

*Status: draft for internal review. Not published. No success claim beyond what the evidence
above supports.*
