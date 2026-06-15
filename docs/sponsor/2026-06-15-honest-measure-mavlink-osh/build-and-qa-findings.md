# Build & QA findings — sandbox / Gradle / integration‑QA

These are the issues this run surfaced in the **execution + QA infrastructure** (not the
generated code — that's reviewed separately in [code-review.md](code-review.md)). They are
ordered by impact. Each is backed by quoted evidence and has a concrete fix.

---

## 1. The committed deliverable is not self‑contained (real defect; the run's true‑negative)

**Symptom.** End‑of‑plan integration QA ran the project's real test command and failed:

```
Running sandbox QA … mode=integration test_command="./gradlew test"
Sandbox QA complete … mode=integration passed=false
QA verdict — plan rejected … level=integration … test command failed (exit 1)
> Could not resolve org.sensorhub:sensorhub-core:2.0.1
  > Could not GET '…maven.pkg.github.com/opensensorhub/osh-core/.../sensorhub-core-2.0.1.pom'.
    Received status code 401 from server: Unauthorized
```

**Root cause.** The assembled `build.gradle` resolves `org.sensorhub:sensorhub-core` from
**GitHub Packages**, which 401s with the available credentials. The per‑node developer
builds had avoided this by building `osh-core` **from source** (the `WITH_EPIC` design
mounts the upstream at `/sources`), but that wiring **never propagated into the committed
`build.gradle`/`settings.gradle`**.

**Proven independent of any cache** (so it is not a monitoring artifact):

```
curl -u USERNAME:$GITHUB_TOKEN \
  https://maven.pkg.github.com/opensensorhub/osh-core/.../sensorhub-core-2.0.1.pom
→ HTTP 401   (also 401 with a non‑placeholder actor)
```

**Why source‑build silently failed in QA.** `/sources` is mounted **read‑only**
(`semsource-clones:/sources:ro`). A Gradle composite `includeBuild('/sources/…osh-core')`
cannot write build outputs into a read‑only path, so the composite silently fails and
Gradle falls back to the declared repository → 401. The developer worked around this
per‑node by **copying osh‑core to a writable `/tmp/osh-core`** and `includeBuild`‑ing that.

**Verification that the code is otherwise sound** (we reproduced the dev's wiring on the
assembled branch):

```
cp -r /sources/…osh-core /tmp/osh-core
settings.gradle:  includeBuild("/tmp/osh-core") { dependencySubstitution {
                    substitute module("org.sensorhub:sensorhub-core") using project(":sensorhub-core") } }
gradle test  →  BUILD SUCCESSFUL in 26s ; 23 tests, 0 failures, 0 errors (3 skipped)
```

**Fixes (pick one or more).**
1. **Make the committed build self‑contained** — propagate the writable‑source composite
   wiring (or a vendored osh‑core) into the deliverable's `settings.gradle`, so it builds
   from a clean checkout without external auth. This is the proper fix and is what the
   integration QA is implicitly demanding.
2. Provide credentials with `read:packages` for the `opensensorhub` org if GitHub Packages
   is the intended resolution path (and confirm the package is actually published).
3. Have QA stage a **writable** osh‑core source tree for composite builds.

---

## 2. Integration QA was wedged by a one‑line sandbox config gap

**Symptom.** After all 4 requirements completed, the plan sat at `ready_for_qa` for ~15
minutes with no QA activity. The sandbox log showed:

```
sandbox: "NATS not configured — running HTTP-only (set -nats-url or NATS_URL to enable QA subscriber)"
```

**Root cause.** plan‑manager *did* publish the QA request
(`Published QARequestedEvent mode=integration test_command="./gradlew test"`), but the
**UI‑e2e sandbox had no `NATS_URL`**, so its QA subscriber never started — nothing consumed
the request, and the qa‑reviewer's `qa-completed` consumer waited forever. The *backend*‑e2e
sandbox (`docker/compose/e2e.yml`) sets `NATS_URL=nats://nats:4222`, which is why the mock
`qa-cycle-integration` scenario passes but the real `watch:llm` run wedged.

**Fix (applied, staged in working tree, needs commit/PR).** Add to the sandbox service in
`ui/docker-compose.e2e-llm.yml`:

```yaml
environment:
  NATS_URL: nats://nats:4222
```

After this, the sandbox QA subscriber started, picked up the pending request, and ran the
integration QA — proving the rest of the #193 fail‑closed plumbing works end‑to‑end.

---

## 3. Integration QA must run COLD, or it can false‑pass

**Observation.** The integration QA ran in a **cold** sandbox (we had recreated it to apply
fix #2), which forced a clean dependency resolution and **exposed** the 401. A **warm**
gradle cache — populated by the dev's per‑node source builds — would have served
`sensorhub-core:2.0.1` from cache and let `./gradlew test` **pass**, hiding the
not‑self‑contained defect. That is a false‑green in miniature: green only because of
artifacts the dev's environment happened to warm.

**Fix.** Ensure the integration QA build runs against a **clean cache** (or at least one
not warmed by the dev loops), so "does it build from clean?" is actually tested. Self‑
containment is a property of the committed config, and only a cold build verifies it.

---

## 4. Secondary observability / harness items (non‑blocking)

| Item | Evidence | Fix |
|---|---|---|
| `active-poll` mislabels graph‑ingest WARNs **and** successful review submits as `REVIEWER_REJECT` | `[POLL] REVIEWER_REJECT line="… component=graph-ingest … graph mutation rejected"`; `… "submit_work accepted … deliverable_type=review"` | tighten the classifier in `taskfiles/scripts/active-poll.sh`; never tag a successful submit or a graph‑ingest WARN as a reject |
| Computed `stage` field mislabels `preparing_stories`/revision states as `drafting`/`generating_architecture` | active‑poll showed `→ drafting` while the plan was really in `preparing_stories` (revision loop, `target_status=requirements_generated`); planner did **not** re‑run | fix the stage derivation so revision/story‑prep states aren't reported as planner stages |
| `tool.execute` consumer `ack_wait` < long Gradle builds → redelivery | `ALERT Redelivery … consumer=agentic-tools-tool-execute-all … 1 message redelivered` during a ~16s cold build | size `ack_wait` for worst‑case dev `bash` (gradle cold builds run minutes); derive from the tool/exec timeout (#140 class) |
| ADR‑049 ownership fast‑fails on **dev scratch files**, not just deliverables | `Ownership/planning gap … fix_handler.sh, patch_handler.sh` (helper scripts the agent wrote to automate its own fixing) | scope ADR‑049 enforcement to deliverable files (source under `src/` / declared extensions), and/or auto‑clean untracked scratch before the dev gate |
| Per‑node test execution is checklist‑dependent for non‑Go languages | structural validator's automatic test fallback is **Go‑only** (`runGoTestOnModifiedIn`); Java relies on `checklist.json` having a `gradle-test` entry (silent‑skip if missing) | make per‑node test execution a deterministic structural step derived from project language + `qa_test_command`; warn/fail loudly when a known‑language project has no test‑execution check |
| `WEDGE_DETECT` false‑positives on terminal `stage=rejected` | `WEDGE_DETECT … stage=rejected stuck_for=625s` | exclude terminal stages (rejected/complete/archived) from wedge detection |

---

## Net

The execution pipeline and the #193 fail‑closed integration‑QA gate **work**: real tests
run per node and at the plan level, and the gate honestly rejected a deliverable that
couldn't build clean. The two issues that mattered were a **one‑line infra gap** (sandbox
`NATS_URL`) and a **real build‑self‑containment defect** in the generated deliverable —
both now precisely characterized with reproductions and fixes.
