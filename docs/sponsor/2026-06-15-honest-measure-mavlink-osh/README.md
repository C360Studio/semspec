# Semspec — Honest Capability Measure: MAVLink/OSH driver run (2026‑06‑15)

> **What this is.** A truthful snapshot of what Semspec's autonomous pipeline did on a
> hard, real‑world task (build an OpenSensorHub MAVLink/MAVSDK driver), captured the same
> day we *retracted* an over‑optimistic pack (PR #191 → reverted by #192). Every claim
> here is backed by quoted run evidence and was independently verified — the deliverable
> was built and its tests were executed before any statement was made.
>
> **The headline is not "we shipped a MAVLink driver."** It is: **the pipeline now
> produces real, building, test‑passing code *and* refuses to falsely declare success on
> an imperfect deliverable.** That integrity — earned by deliberately hunting our own
> false‑greens — is the measure worth showing.

## The one‑paragraph version

On 2026‑06‑15, after merging three hardening PRs (#192/#193/#194), Semspec autonomously
took a single prompt ("OSH MAVLink/MAVSDK driver") through plan → architecture → scenarios
→ a 4‑requirement execution phase, producing ~1,640 lines of genuinely idiomatic
OpenSensorHub + MAVSDK Java across 16 files. Every requirement completed with real
per‑node evidence (each unit's `./gradlew test` ran and passed at the dev gate, *before*
any LLM review). The end‑of‑plan **integration QA gate then fired for the first time and
correctly failed the run** — because the *assembled* build was not self‑contained (it
resolved `osh-core` from an authenticated package registry that returned 401). That is a
**true negative, not a fake green**: we proved the 401 independently, and we proved that
the underlying code is sound (with `osh-core` built from source the assembled deliverable
builds and **23 tests pass, 0 failures**). The pipeline caught a real defect instead of
shipping around it.

## What is proven (with evidence)

| Claim | Evidence |
|---|---|
| Plan converged through to a 4‑requirement execution phase | `stage: implementing`; `execution_summary completed=4 failed=0` |
| All requirements completed with **real** node evidence (no dedup/false‑complete) | `Requirement execution completed … nodes_completed=3` ×4 |
| **Executable evidence per node, pre‑review** | `Structural validation completed passed=true checks_run=6` (runs `./gradlew test` in‑sandbox before the LLM reviewer) |
| Integration QA gate executed real tests | `Running sandbox QA … mode=integration test_command="./gradlew test"` → `Sandbox QA complete passed=false` |
| QA **failed closed** (no fake green) | `QA verdict — plan rejected … level=integration … test command failed (exit 1)` |
| The failure is a **real** defect, not a tooling artifact | direct `curl -u … maven.pkg.github.com/.../sensorhub-core-2.0.1.pom` → **HTTP 401**, cache‑independent |
| Declared work was actually done | declared `scope.create` = 16 files; created = 16 files; **0 missing** |
| The code itself is sound when built correctly | osh‑core from source → `BUILD SUCCESSFUL`; **23 tests, 0 failures, 0 errors** (3 SITL/coverage skipped), 33 classes compiled |

## What is honestly *not* done

- **The committed build is not self‑contained.** It declares `org.sensorhub:sensorhub-core`
  from GitHub Packages (which 401s with available credentials); the developer's per‑node
  "build osh‑core from source" workaround never propagated into the committed
  `build.gradle`/`settings.gradle`. **The deliverable does not build from a clean checkout.**
- **The deliverable is a vertical‑slice MVP**, not the full driver: 2 telemetry outputs
  (position, battery) and 2 commands (takeoff, land), down from a more ambitious design.
- **One component is half‑stubbed and unwired** (`MavlinkDirectHandler`: real frame
  *decode*, but a dummy‑checksum *encode* and no integration into the sensor).
- **No OSH module‑discovery registration** (`META-INF/services/...IModuleProvider`), so the
  module is not auto‑loadable in a real deployment without manual wiring.

Full detail: **[build-and-qa-findings.md](build-and-qa-findings.md)** and
**[code-review.md](code-review.md)**.

## Why this is the measure worth showing

Two weeks ago this same scenario produced a *green* pipeline whose deliverable was
non‑usable — and we built a sponsor pack around it before catching the lie (retracted in
PR #192). The fix was not "make the run greener." It was to make the pipeline **honest**:

- real executable evidence at every node (not read‑only synthesis),
- M:N completion gated on committed node evidence (no zero‑work completes),
- a fail‑closed integration QA gate that runs the project's real test command,
- and an operator (here, with active monitoring) who **verifies before claiming** —
  building the artifact, running its tests, and diffing declared‑vs‑created files.

This run is the result: a system that did a large amount of real work, told the truth
about the one place the work was incomplete, and left a precise, reproducible trail. That
is a more valuable capability than a green checkmark.

## Reproduce / audit

- Run: `WITH_EPIC=1 DEBUG=1 task e2e:watch:llm -- gemini mavlink-hard` (code under test: `main` @ `#194`).
- Raw run artifacts + the full findings ledger: `/tmp/semspec-watch-gemini-mavlink-hard-20260615-150752/` (`findings.md`, `semspec.log`, watch bundle, `final-plan`).
- Code‑works proof (diagnostic): assembled branch `semspec/plan-f76c9d8ba0ac`, osh‑core via writable‑source `includeBuild` + `dependencySubstitution`, `gradle test` → green.

*Status: draft for internal review. Not published. No success claim beyond what the evidence above supports.*
