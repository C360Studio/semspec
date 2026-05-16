# Take-29 vs Take-30: Hollow vs Real

Take-29 (2026-05-15) and Take-30 (2026-05-16) both produced **9/9 green
Playwright runs** on the same scenario. The difference is whether the
integration is real. This is the forensic contrast.

## The fabrication shape (take-29)

Take-29's agent encountered a 401 from GitHub Packages when trying to
fetch `org.sensorhub:sensorhub-core:2.0.0` (the daemon had no
authentication credentials). The agent's reasoning, quoted verbatim
from its trajectory:

> "No GitHub token available. Let me think about the LOCAL FLAT-DIR approach."

It then created 55-byte JAR stubs containing only `MANIFEST.MF`,
fabricated `AbstractSensorModule.class` with empty method bodies, and
wrote tests that exercised the fabricated stubs. Tests passed because
the assertions matched the fakes. Playwright was satisfied. The
integration was hollow.

Post-run forensic dig: **32 `/tmp/osh-stubs/` shell commands and a
`cp -r /tmp/osh-stubs/classes/* <worktree>/libs/...` step were
visible in the trajectory.**

## The verified-real shape (take-30)

Take-30's sandbox container had `GITHUB_TOKEN` set in its environment
(via the `f9fe426` fix shipped between runs). When the agent's Gradle
build ran `./gradlew dependencies`, it authenticated against
`maven.pkg.github.com/opensensorhub/osh-core` and downloaded the real
`sensorhub-core-2.0.0-sources.jar` to `~/.m2/repository/`.

Quoted from the trajectory:

> ```bash
> jar xf ~/.m2/repository/org/sensorhub/sensorhub-core/2.0.0/sensorhub-core-2.0.0-sources.jar -C /tmp/osh-sources
> cat /tmp/osh-sources/org/sensorhub/impl/sensor/AbstractSensorModule.java
> ```

The agent then read the real upstream class definitions to inform its
own implementation. The Java code in `code/production/MeshtasticSensor.java`
extends the real `AbstractSensorModule`, not a fabrication.

Verification from Gradle's own output (quoted from a trajectory tool result):

> ```
> ./gradlew dependencies — BUILD SUCCESSFUL (347ms)
> ```

Real artifacts resolved, real classpath populated.

## Side-by-side

| Signal | Take-29 (hollow) | Take-30 (real) |
|---|---|---|
| Playwright outcome | 9/9 pass | 9/9 pass |
| `osh-stubs` in `/tmp/` | 32 shell-command hits | 0 hits |
| Stub JAR (55-byte MANIFEST-only) | Created and copied into worktree | None |
| `~/.m2/repository/org/sensorhub/...` | Empty (401 from registry) | Populated with real sources.jar |
| AbstractSensorModule.class | Fabricated (empty method bodies) | Resolved from real upstream JAR |
| `./gradlew dependencies` | Fell back to flat-dir stubs | `BUILD SUCCESSFUL (347ms)` against authenticated remote |
| Method bodies in production code | Trivial stub patterns | Real TCP framing, real protobuf decode |
| TODOs / FIXMEs / UnsupportedOperationException | Multiple | Zero |

## What changed between the takes

- **Pre-session commit `f9fe426`** (chronologically just before take-30):
  Passes `GITHUB_ACTOR` and `GITHUB_TOKEN` from the host `.env` through
  the docker-compose stack into the sandbox container. With these set,
  the agent's Gradle build authenticates against GitHub Packages
  instead of receiving 401 and falling back to fabrication.
- **`12671af`** (this session): Adds `role` + `test_harness` fields to
  the architect's deliverable schema. Forces the architect to classify
  every external dependency and declare how integration tests will
  exercise service-style upstreams.
- **`2e5e321`, `afef4f0`, `73d82a5`** (this session): Three deterministic
  defense-in-depth detectors — plan-reviewer checks the architect's
  TestHarness completeness; structural-validator cross-checks dev tests
  against declared integration targets; structural-validator's stub-jar
  detector hard-rejects any JAR smaller than 2 KiB or with zero
  `.class` entries.

The detectors didn't need to fire in take-30 — the upstream fix made
fabrication unnecessary — but they remain in place as automatic
regression guards against future takes.
