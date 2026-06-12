# Real-LLM Expectations

What semspec actually does, with the wallclock and the loop count, and the
things we have not yet measured. This document exists because the alternative
— letting evaluators discover the cost during a trial — erodes trust faster
than naming the cost upfront does.

The numbers below are empirical, not projected. Where we don't have a number
yet, the doc says so.

## 1. The floor today

| Tier | Wallclock | Loop count | Recommended models | Verified runs |
|------|-----------|-----------:|--------------------|--------------:|
| `easy` | ~9 min | ~24–41 | Single frontier provider (Claude, Gemini, or comparable) | N=4 (latest 2026-06-11, post-beta.103) |
| `medium` | not yet measured | — | — | N=0 |
| `hard` | ~78 min | ~50 | Frontier hybrid (two providers, one for orchestration + one for developer) | N=1 (2026-05-16) |

**"Verified" means:** real upstream artifacts pulled from real registries
(not fabricated stubs), real test execution (not just compilation),
non-fabricated integration. The distinction matters — see Section 3.

The single verified `hard` run also required `GITHUB_TOKEN` to be set so
the agent's Gradle build could authenticate against GitHub Packages.
Without it, the agent fell back to fabricated stub JARs and the run
*looked* successful with hollow code (take-29 the day before take-30).

## 2. Why it takes that long

Four mechanics, in order of contribution.

### Cold-start discovery

The agent receives only the prompt and a minimal project skeleton.
Upstream coordinates (Maven groups, Docker images, TCP ports, wire
protocols) are discovered at runtime via `web_search` and `http_request`
tool calls. This is wallclock-expensive but architecturally cheaper than
maintaining a curated upstream catalog the agent reads from.

Concrete example from take-30: the architect ran `curl` against Docker
Hub, parsed 3,341 image tags for the `meshtastic/meshtasticd` repository,
and chose `daily-alpine` as the stable build to test against. That single
discovery loop took roughly 90 seconds. The whole architecture phase took
~3 minutes for the `hard` tier.

### Multi-stage review loops

Every artifact — plan body, requirements, architecture, scenarios, and
each code change during execution — passes through a reviewer agent
before advancement. Rejections trigger retries with explicit feedback.
Each round is one or two LLM calls plus their tool calls.

Loop breakdown from take-30 (50 loops total):

| Phase | Loops |
|-------|------:|
| Plan / requirements / architecture / scenarios review | ~10 |
| Per-requirement decomposition (planning each requirement's work) | ~6 |
| Developer TDD cycles (write tests → run → fix → re-run) | ~28 |
| Code review + scenario validation | ~6 |

The developer TDD cycle dominates because it's the only phase where the
agent is *making the code work* against a real compiler and test runner.

### Deterministic gates

The structural-validator runs the project's `checklist.json` inside the
sandbox after every change. For take-30 that meant `./gradlew dependencies`
(resolves authenticated Maven artifacts) and `./gradlew test` (compiles
and runs the JUnit suite). These add 30 seconds to 2 minutes per cycle
depending on the project's test-suite size. They catch fabrication —
a stub class with missing methods fails to compile, a mock that doesn't
hit the network fails an integration test.

### Per-loop overhead

Each LLM call carries model latency (1–5 s typical), tool execution time
(bash, web_search, http_request — each 0.1–10 s), and state-transition
writes to NATS. With 50 loops on the `hard` tier, that's roughly 4–8
minutes of non-thinking time alone.

## 3. The hollow-vs-real trap

The day before take-30 (the verified `hard` run), take-29 produced the
same 9/9 green outcome from Playwright. The integration was fabricated.

| Signal | Take-29 (hollow) | Take-30 (real) |
|--------|------------------|----------------|
| Playwright outcome | 9/9 pass | 9/9 pass |
| `/tmp/osh-stubs/` shell hits | 32 | 0 |
| Stub JAR (55-byte MANIFEST-only) | Created and copied into worktree | None |
| `~/.m2/repository/...` populated | No (401 from registry) | Yes (real sources.jar) |
| `AbstractSensorModule.class` | Fabricated (empty bodies) | Resolved from real upstream |
| `./gradlew dependencies` | Fell back to flat-dir stubs | `BUILD SUCCESSFUL (347ms)` against authenticated remote |
| TODOs / FIXMEs in production code | Multiple | Zero |

What changed between the two runs was structural, not model-side:

- `GITHUB_TOKEN` passthrough so the sandbox can authenticate against
  GitHub Packages.
- Architect schema gained `role` + `harness_profiles` fields (test
  environment profile selections) so integration targets must be declared,
  not assumed.
- Three deterministic detectors (test environment profile completeness check,
  dev-test/integration-target cross-check, stub-jar size detector)
  added as regression guards.

The detectors didn't need to fire on take-30 — the upstream fix made
fabrication unnecessary — but they remain in place. If a future run
regresses to fabrication, they trip first.

**Operational implication:** when evaluating a semspec run, "tests
green" is necessary but not sufficient. Check that `~/.m2/repository/`
or equivalent has real artifacts. Check that production files have
non-trivial method bodies. The take-29 contrast file in the take-30
sponsor pack documents the full forensic dig.

## 4. What we measured

| Date | Tier | Result | Evidence |
|------|------|--------|----------|
| 2026-05-16 | hard | 78 min, 9/9 verified-real, 2,283 LOC Java | [`docs/sponsor-packages/take-30/`](sponsor-packages/take-30/) |
| 2026-05-22 | easy | 8.7 min, 8/8, ~24 loops | `task e2e:watch:llm -- gemini easy` bundle (extracted to `/tmp/semspec-watch-gemini-easy-*/bundle.tar.gz`) |
| 2026-05-30 | easy | 6.7 min, 8/8 (post-ADR-040) | Mary analyst sub-phase + capability rules + dual OpenSpec emission |
| 2026-05-31 | easy | 7.5 min, 8/8, 39 loops (post-ADR-041) | First validation after all six ADR-041 PRs land. Mary surfaces + Bob tier-tags + reviewer tier-aware contract all fire in trajectories. `/tmp/semspec-watch-gemini-easy-20260531-131619/` |
| 2026-06-11 | easy | 9.3 min, 8/8, 41 loops (post-ADR-044 + beta.103) | First verified-green run after the semstreams beta.103 migration, ADR-044 M:N stories, and the contention fixes (ack_wait/redelivery, triple-write storm, graph-clustering). `errors=0`, zero watch ALERTs, clean teardown; `gemini-3-flash-preview`. `/tmp/semspec-watch-gemini-easy-20260611-195329/bundle.tar.gz` |

Five data points. All N=1 each.

## 5. What we don't yet know

This section will shrink as we measure more. Right now it's a long list.

### Model floor unknown per tier

Take-30 used a hybrid assignment: Gemini 3.x family for orchestration
roles, Claude Sonnet 4.6 for the developer role. We don't yet know:

- Does `hard` work with a cheaper developer model?
- Does `hard` work with a single-provider stack (avoid hybrid)?
- Does `hard` work with fully-local Ollama (no frontier API at all)?
- What's the smallest model that passes `easy`?
- What does the failure mode look like as we walk down the model-strength
  curve? (Silent fabrication? Plan rejection loops? Tool-call hallucination?)

### Reproducibility unknown

N=1 per tier. LLM outputs are stochastic. The pass rate over N=10 or
N=20 runs with the same inputs is the next experiment, and we don't
have it yet. Specifically for `hard`: take-29 passed 9/9 hollow on the
same scenario, take-30 passed 9/9 real. What's the verified-real rate
over N=20? Unknown.

### Scenario generalization unknown

Take-30 solved Meshtastic-on-OpenSensorHub. The structural changes that
got us there (auth passthrough, integration-target schema, stub
detectors) are scenario-agnostic, but we haven't yet tested whether a
different `hard` scenario — a Kafka consumer for a different middleware,
a Postgres-backed service with schema migrations — produces verified-real
on the first try at the same model floor.

### Dollar cost unknown

We have wallclock and loop counts. We do not yet have token totals per
run, so we cannot quote a defensible $/run number. The most we can say
honestly today:

- `easy` runs in 8.7 minutes are clearly bounded by per-message API
  cost on a single provider.
- `hard` runs at 78 minutes with 50 loops are clearly more expensive
  but we have not converted that to a number.

We will not estimate. The next experiment that includes token-meter
data will produce the first defensible cost figure.

### Detector firing rate unknown

The three fabrication detectors (test environment profile completeness,
dev-test cross-check, stub-jar size) were added as regression guards. They
didn't fire on take-30. We have not yet deliberately introduced a
fabrication vector to confirm they trigger. That's the next
meaningful experiment in the defense-in-depth direction.

## 6. Operating recommendations

Based on what we've measured, these are reasonable starting points.
They will shift as the unknowns above get measured.

### For a 10-minute demo

- Use the `easy` tier scenario.
- Single frontier provider, either Claude or Gemini. Setting
  `ANTHROPIC_API_KEY` or wiring a Gemini endpoint (see
  [Model Configuration](model-configuration.md)) is enough.
- Budget 10–15 minutes wallclock end-to-end including the UI
  loading the trajectory view.

### For real work on a non-trivial integration

- Use the `hard` tier scenario or your equivalent.
- Hybrid model assignment: one provider for orchestration (planner,
  reviewers, generators) and one for the developer role. This dodges
  single-provider rate-limit saturation that wedged earlier `hard`
  attempts.
- Budget 60–90 minutes wallclock and expect to provide `GITHUB_TOKEN`
  (or equivalent registry credentials) up front. Without authenticated
  registry access, the agent will fall back to fabrication and you'll
  get the take-29 outcome.
- Plan to spot-check the result — read the production code, confirm
  imports trace to real upstream types, confirm tests exercise real
  network or filesystem rather than just mocks.

### For experimentation on model floor

- Start `easy` with a smaller model to find the bottom of the easy curve.
- Use `task e2e:watch:llm -- <provider> <tier>` so the watch sidecar
  captures the bundle for forensic review.
- Compare against the take-30 evidence shape — same scenario success
  with worse code quality is a hollow-shaped run.

## 7. Where to look when something stalls

| Symptom | First thing to check |
|---------|---------------------|
| Plan stuck in `drafted` or `reviewing_draft` | Plan-reviewer rejected; check the `revision_reason` on the plan via `GET /plan-manager/plans/{slug}` |
| Long silence in agent activity | `semspec watch --live --bail-on critical` — see [Diagnostic Bundles](diagnostic-bundles.md) |
| Tests green but suspicious | Check production files for stub patterns (see Section 3); compare against take-29-vs-take-30 contrast |
| Want to replay or audit a run | Extract `bundle.tar.gz` from the watch directory; trajectories are per-loop JSON with the full `messages` array when `trajectory_detail: "full"` is configured (see [How It Works → Trajectory Capture](how-it-works.md#trajectory-capture--llm-audit-trail)) |
| Rate-limit failures | Switch to hybrid model assignment so different providers carry different roles |

## 8. How this document gets updated

This file is the canonical place for the empirical floor. Update
triggers:

- Any new verified run at any tier (add a row to Section 1 or 4).
- Any measurement that shrinks the "what we don't yet know" list
  (Section 5).
- Any change to the recommended operating envelope (Section 6).

Sponsor packages (`docs/sponsor-packages/take-NN/`) remain the
forensic artifacts for specific runs. This document summarizes
across them.
