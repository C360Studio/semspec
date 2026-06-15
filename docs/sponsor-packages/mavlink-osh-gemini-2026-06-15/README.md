# Semspec — mavlink-osh-hard 2026-06-15 (gemini-3.1-pro)

**First end-to-end completion of the OpenSensorHub MAVSDK driver epic.**
The autonomous substrate took a one-paragraph goal to a QA-approved,
cleanly-assembled Java/Gradle driver — plan → architecture → scenarios →
4 requirements executed → clean plan-level merge → QA verdict `approved` →
status `complete`. No human intervention during the run.

Run timestamp: **2026-06-15T01:42:24Z → 02:50:58Z**. Wallclock: **~68 min**
(Playwright harness 1.1h including infra).
Outcome: **8/8 Playwright assertions passed**, plan reached terminal
status **`complete`**, QA reviewer verdict **`approved`**.

This is the payoff for [`docs/sponsor-brief-2026-06-16.md`](../../sponsor-brief-2026-06-16.md),
which framed the MAVLink hard run as "the near-term proof point" and said:
*"do not claim final victory until its result is in."* The result is in.
Every prior pack in this directory (`take-30`, `mavlink-osh-hybrid-gpt5`,
`mavlink-decode`) stopped short of `complete` — most hit a per-requirement
time cap or an assembly conflict mid-flight. **This is the first run to land
the whole pipeline.**

## TL;DR

| | |
|---|---|
| **Outcome** | `complete` — QA `approved` — 8/8 Playwright |
| **Scope** | OpenSensorHub MAVSDK→Connected-Systems driver (Java / Gradle) |
| **Model** | gemini-3.1-pro-preview (plan/arch/scenarios/dev/review/QA), gemini-3-flash fast |
| **Plan shape** | 4 requirements · 15 scenarios · 1 cohesive component · 2 plan-review rounds |
| **Execution** | 5 TDD nodes · 12 code-review verdicts (5 approved / 7 fixable) · 1 autonomous recovery |
| **Assembly** | 4 requirements merged into `semspec/plan-dd236a6cb88b` @ `8ddbe90` — **zero conflicts** |
| **Shipped** | 14 Java files (7 impl + 7 test) + Gradle build + coverage matrix (28 files) |
| **Recovery** | 1 model hallucination caught by a deterministic gate, diagnosed, and self-corrected |
| **Cost posture** | single run, planning + 1 dev loop (the expensive part) — no thrash, no token blowout |

## What this run verifies

1. **The full BMAD pipeline runs autonomously to `complete`.** Mary→John→
   Bob→Winston→Sarah→Amelia→Murat — analyst through QA — with two human
   approval gates auto-satisfied by the plan-reviewer. The system's own
   per-phase artifacts are hydrated under [`bmad/`](bmad/) (plan, architecture,
   requirements, scenarios, stories, qa-summary, run-summary).

2. **ADR-049 cohesive ownership holds, and assembly is clean.** Winston
   produced **one** `mavsdk-driver` component covering all four capabilities
   (lifecycle, telemetry, control, raw-MAVLink) instead of over-splitting into
   four components that fight over the same driver class. The 4 requirements'
   branches merged into the plan branch with **zero conflicts** — the exact
   failure that terminated the 2026-06-14 over-split run, now structurally
   eliminated. See [`bmad/architecture.md`](bmad/architecture.md).

3. **The autonomous recovery cascade corrected a real model error mid-run.**
   The developer hallucinated the package `…impl.driver.mavsdk` (inferred from
   the component name) instead of the declared `…impl.sensor.mavsdk`. The
   **ADR-049 Move-3 ownership gate caught the out-of-territory files
   deterministically**, recovery diagnosed the cause precisely, chose
   `refine_prompt`, re-dispatched with the correct package — and the developer
   shipped to the right paths. Story walked `executing→failed→pending→ready→
   executing` (the PR #188 reset) with **no false-complete, no deadlock, no
   human escalation**. Full trace: [`evidence/recovery-trace.md`](evidence/recovery-trace.md).
   This is the "failures route somewhere specific" thesis from the sponsor
   brief, proven on a live error.

4. **OpenSpec artifacts are emitted for free.** The same plan rendered as
   OpenSpec `proposal.md` / `design.md` / `tasks.md` plus four capability
   `spec.md` files — hydrated under [`openspec/`](openspec/). A sponsor using
   OpenSpec can read the change in a format they already recognize.

5. **TDD with a real reviewer produces buildable Java.** 12 code-review
   verdicts across 5 nodes: 5 approved, 7 rejected-but-fixable (resolved on the
   next cycle). Each node's worktree merged only after approval. Tally:
   [`evidence/review-verdicts.md`](evidence/review-verdicts.md).

6. **The M:N reservation pattern works.** One owner Story ran the dev loop;
   the other three requirements fast-completed as non-owners via dedup —
   **one dev loop, not four** (ADR-044). All 4 requirements recorded complete
   within the same second once the owner finished.

## What this run did NOT verify (honest caveats)

- **N=1.** This is one successful run, not a pass-rate. The same epic failed
  three times earlier in the same session on three *different* walls (see "The
  road here"). We do not yet have a stable pass rate or dollar figure.
- **Functional gold comparison is pending.** The generated driver is saved for
  comparison against the upstream OSH driver (see [`code/`](code/) and
  [`code/COMPARISON.md`](code/COMPARISON.md)), but we have **not** yet built or
  run the generated driver against live PX4 SITL. QA here was `synthesis`-level
  (Murat read the assembled source tree and judged requirement fulfilment); it
  did not execute the integration tests.
- **The generated driver is leaner than gold.** 7 impl classes vs the upstream
  driver's 38 — it satisfies the 6 declared integration scenarios with a
  consolidated design, not the upstream's class-per-command/per-output surface.
  Whether that leaner surface is *functionally* equivalent is the open
  question the gold comparison will answer.
- **One environment failure, not a product failure.** An earlier attempt this
  session wedged because the Docker build cache filled the host disk (78 GB
  accumulated across repeated image rebuilds). That is operator hygiene, not a
  substrate defect — but it is why disk is now a first-class monitoring signal.

## The road here (why this run is the one that landed)

The same epic hit — and the substrate diagnosed — four distinct walls before
this run completed. Each became a specific, fixable signal rather than an
ambiguous late wedge:

| Wall | Run | Root cause | Fix |
|---|---|---|---|
| Over-split assembly conflict | 2026-06-14 (`8beac…`) | file-count proxy split 1 driver into 4 components fighting one file | ADR-049 component ownership topology |
| DAG `file_scope` cap | `2a6c27…` | cohesive component owned 52 files; synthesis cap was 50 → auto-reject before any dev loop | **PR #190**: `maxFileScopeEntries` 50→100 (territory ≠ work size) |
| Docker disk exhaustion | `531f15…` | 78 GB build-cache filled host disk → sandbox builds failed on writes → dev loop froze | reclaimed ~155 GB; disk now monitored |
| The build itself | `dd236a…` (this run) | — | gemini built it; QA approved |

The disk-wedge run (`531f15…`) is itself notable: before it wedged it **validated
the PR #188 recovery fix and the cap fix live** — so by the time this run
started, every plumbing fix was already proven, leaving only the model-capability
question: *can gemini actually build the driver?* It can.

## Run topology

- Provider: `gemini` (`configs/e2e-gemini.json`)
  - plan / plan-review / architecture / scenario-gen / dev / review / QA: `gemini-3.1-pro-preview`
  - fast capabilities: `gemini-3-flash-preview`
- Fixture: `test/e2e/fixtures/osh-driver-mavsdk` (Java/Gradle/OSH skeleton, baseline `37bd431`)
- EPIC overlay (`WITH_EPIC=1`): external semsources pre-cloned at `/sources/`
  (osh-core, osh-addons, mavsdk-java, ogc, meshtastic)
- Harness catalog: `workflow/harnesscatalog/catalog/mavlink.yaml` — 1 profile selected
- qa_level: `synthesis`
- Carries: **PR #190** (`fix/dag-filescope-cap-adr049`, `maxFileScopeEntries=100`)
- Command: `WITH_EPIC=1 DEBUG=1 task e2e:watch:llm -- gemini mavlink-hard`

## Package contents

```
README.md                  ← this file (narrative + evidence index)
bmad/                      ← HYDRATED BMAD prose the substrate generated
  plan.md  architecture.md  requirements.md  scenarios.md  stories.md
  qa-summary.md  run-summary.md  plan.json
openspec/                  ← HYDRATED OpenSpec change + 4 capability specs
  proposal.md  design.md  tasks.md  specs/{cs-api-control,cs-api-telemetry,
  mavsdk-lifecycle-manager,raw-mavlink-bridge}/spec.md
code/
  generated/               ← what the agent shipped (28 files, 14 Java) — assembled branch @ 8ddbe90
  gold-reference/          ← upstream OSH sensorhub-driver-mavsdk (38 Java) — the GOLD
  COMPARISON.md            ← generated vs gold, what to evaluate
evidence/
  playwright-result.md     ← 8/8 assertions
  timeline.md              ← phase-by-phase timeline
  recovery-trace.md        ← the hallucination → gate → refine_prompt → correction
  review-verdicts.md       ← TDD code-review tally
  completion-chain.log     ← raw completion log slice (merge → QA → complete)
  coverage-matrix.md       ← MAVSDK_CS_Coverage.md (the deliverable QA cited)
  shipped-file-tree.txt    ← full file list on the assembled branch
```

## Why it matters to a sponsor

The sponsor brief promised "supervised delivery under real constraints." This
run is the demonstration: a one-paragraph integration goal became a governed,
auditable, QA-approved deliverable — and the one model error that occurred was
**caught by a deterministic gate and corrected autonomously**, not papered over
or escalated to a human. The value is not that an LLM wrote Java; it is that the
substrate turned a hard framework-integration task into a *supervised* one, with
a persistent trail at every step (`bmad/`, `openspec/`, `evidence/`).

## Sound bite

Semspec took "write an OpenSensorHub MAVSDK driver" from a prompt to a
QA-approved, cleanly-merged Java driver — autonomously, recovering from its own
package-name hallucination along the way. First full landing of the hard epic.
