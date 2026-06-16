# Build & QA findings — run #6 (what the hardening fixed, and the new honest‑red)

These are the **execution + QA infrastructure** observations from run #6 (the generated code
is reviewed separately in [code-review.md](code-review.md)). The theme: the four findings the
prior pack opened are now **closed**, the integration‑QA gate reached a strictly deeper layer,
and the honest‑red moved from *"won't build"* to *"behavior untested."*

---

## 1. The prior pack's blockers are closed (regression‑style confirmation)

The 2026‑06‑15 pack documented four issues that prevented integration QA from honestly
exercising the deliverable. Run #6 ran on `main` with all four fixes merged; each is confirmed
resolved by this run:

| Prior finding | Fix (PR) | Run #6 confirmation |
|---|---|---|
| Integration QA wedged: UI‑e2e sandbox had no `NATS_URL`, QA subscriber never started | sandbox `NATS_URL` (#195) | QA subscriber attached; QA ran to a verdict |
| Committed build not self‑contained → osh‑core **401** at QA time | OSH source substitution (#196) | QA log: `OSH source substitution enabled (includeBuild from source)` → `BUILD SUCCESSFUL`, **no 401** |
| Cold‑start race: QA subscriber `os.Exit(1)` when `WORKFLOW` stream absent | non‑fatal background subscriber (#197) | no cold‑start crash; subscriber came up and consumed the request |
| ADR‑049 ownership fast‑fails on dev **scratch files** | scratch/argfile scoping (#198) | **0 scratch/argfile ownership gaps** this run |

**Net:** the plumbing the prior pack characterized as broken now works end‑to‑end. Integration
QA compiled the full deliverable (osh‑core from source) and ran the project's real test command.

---

## 2. The new honest‑red: build green, integration tests skip → fail closed

**Symptom.** Integration QA compiled everything and ran `./gradlew test`:

```
QA cache isolation enabled: GRADLE_USER_HOME=/tmp/semspec-qa-…/gradle  MAVEN_OPTS=-Dmaven.repo.local=/tmp/semspec-qa-…/m2/repository
OSH source substitution enabled (includeBuild from source): …osh-addons, …osh-core
…
> Task :test
BUILD SUCCESSFUL in 53s
```

…and then **rejected the plan**:

```
QA verdict needs_changes at level integration
verdict=needs_changes level=integration
execution_exhausted action=escalate_human affected_reqs=[all 5]   → STAGE_CHANGE … to=rejected
```

**Reason (quoted verdict).**

> "rejected … due to skipped mandatory integration tests (MavsdkSmokeTest and
> UnmannedSystemTest). The tests skip when SITL_ENDPOINT is missing, leaving the core
> capabilities (mavsdk‑bootstrap, telemetry‑mapping) entirely untested. Furthermore, the raw
> MAVLink fallback implementation contains a defect that throws NullPointerExceptions during
> event publishing."

**Independently verified** (not trusting the pipeline's green): the assembled branch was
rebuilt by hand in a fresh worktree, osh‑core from writable source, isolated
`-Dmaven.repo.local`:

```
./gradlew cleanTest test  →  BUILD SUCCESSFUL
19 tests, 13 executed, 0 failures, 0 errors, 6 skipped
  skipped = MavsdkSmokeTest (5) + UnmannedSystemTest (1)   — all @Tag("integration"), assumeTrue(SITL_ENDPOINT)
```

This is a **true negative**: the deliverable builds and its unit surface is green, but the
behavior that matters is gated behind a live PX4/SITL simulator that the sandbox doesn't have,
so the pipeline declined to call it done.

---

## 3. The next layer: SITL‑gated tests are e2e — keep the red, fix the *messaging* (ADR‑045)

**Observation.** The 6 skipped tests are genuine end‑to‑end behavior checks that require a live
MAVLink endpoint (`SITL_ENDPOINT`). They `assumeTrue(...)` → JUnit reports them **skipped**, the
gradle build **succeeds**, but the QA gate (correctly, by its current rule —
`qa_subscriber.go:257`, `passed = exit0 && !timedOut && len(skippedEvidence)==0`) treats
*skipped mandatory integration tests* as not‑passing and fails closed. The fixture
(`osh-driver-mavsdk`) has `qa_level: integration` and `qa_test_command: ./gradlew test`.

**Why this is the honest ceiling, not a bug.** Per ADR‑045, *integration* QA runs in the
sandbox; *full/e2e* (anything needing live external systems — a flight simulator here) is
**operator CI**, not sandbox QA. These SITL tests are e2e‑class. With them counted by the
sandbox gate, **the run can never pass in the sandbox** regardless of code quality — the gate is
asking for a simulator the sandbox can't provide.

**Decision: keep the honest‑red; do not weaken the gate to force a green.** The `skip == fail`
rule is the #193 no‑false‑green invariant and stays exactly as is. We deliberately do **not**
exclude the e2e tests from the integration run to manufacture a pass — for a *driver*, a
sandbox "green" that verified only framing/config unit tests would be a softer false‑green, and
the red is the more truthful signal. Two changes follow, neither of which touches pass/fail:

1. **Fix the verdict *messaging* (next code change).** The gate message
   (`integration QA skipped test(s): <files>`) and the qa‑reviewer summary read like code
   defects. They should instead say *"e2e behavior deferred: these tests require a live
   `SITL_ENDPOINT` and must run in operator CI"* — surfacing the JUnit `<skipped message=…>`
   reason so the verdict explains *why* it's red and *where* the behavior gets verified, not
   that the code is broken.
2. **Stand up operator‑CI SITL (the real path to behavior verification).** Per ADR‑045's
   full/e2e tier, provision a PX4/SITL `SITL_ENDPOINT` in operator CI and run the live tests
   there. That is where this fixture's behavior actually gets proven; the sandbox honestly
   reports "built + unit‑green, behavior pending e2e."

Until operator‑CI SITL exists, the sandbox red is the correct, explicit state — we make it
*legible* (messaging) rather than *re‑discovered* each run.

---

## 4. The new completeness gate works — and shows its own limit

**`osh-module-provider-registration` fired 4× during the run** (`osh-module-provider-registration
first_failure_excerpt=…` ×4), rejecting dev work until a
`META-INF/services/...IModuleProvider` registration appeared. That is the gate doing exactly
its job — and it closed the prior pack's weakness #3 (missing registration).

**But the gate enforces presence, not correctness.** The model satisfied it by producing **two**
near‑identical `IModuleProvider`s (`UnmannedActivator` ≈ `UnmannedDescriptor`) and registering
both, so OSH would discover the module twice (see [code-review.md](code-review.md) §1). The
useful harness lesson: a *presence* gate needs a *shape* assertion behind it — e.g. exactly one
provider per module class, provider points at a concrete `AbstractSensorModule`, and the
provider is reachable/loadable — or it will be satisfied by duplication.

---

## 5. Secondary observability / harness items (non‑blocking)

| Item | Evidence | Fix |
|---|---|---|
| QA `skipped` excerpt is the gradle deprecation warning tail, not the skipped test list | verdict excerpt shows `…AccessControlException … deprecated…` instead of the SITL assumption message | capture the JUnit `<skipped message=…>` reason into the QA excerpt so the verdict explains *why* tests skipped |
| Per‑node test execution for Java still rides on `checklist.json` having a gradle‑test entry | (carried from prior pack; still Go‑only auto‑fallback in structural validator) | make per‑node test execution deterministic from project language + `qa_test_command`; warn loudly when a known‑language project lacks a test check |
| `WEDGE_DETECT`/`active-poll` noise on terminal `stage=rejected` | poll noise after `to=rejected` | exclude terminal stages from wedge/poll alerting (carried) |
| Build self‑containment is provided by the harness, not the deliverable | QA builds only because osh is substituted from source; committed `build.gradle` still declares external osh‑core | optional: have the dev loop emit a self‑contained composite (`settings.gradle includeBuild` + substitution) into the deliverable so it builds from a clean checkout, or vendor osh‑core |

---

## Net

The hardening landed: integration QA now **builds the deliverable from source and runs its
tests**, and the four prior blockers are closed. The honest‑red advanced from *"the artifact
doesn't build"* (run #4) to *"the artifact builds and is unit‑green, but its core behavior is
untested because the integration tests need a live simulator"* (run #6) — a deeper, more useful
truth. The next steps keep the gate strict and are infrastructural, not gate‑weakening:
**make the SITL‑skip verdict legible** (message it as "e2e behavior deferred to operator CI",
not as a code defect — pass/fail unchanged), **stand up operator‑CI SITL** (ADR‑045 full/e2e
tier) as the real path to verifying behavior, and **add a shape assertion behind the new
registration gate** (so presence can't be satisfied by a duplicate provider).
