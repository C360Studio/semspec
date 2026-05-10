# OSH Meshtastic Driver

Driver for integrating [Meshtastic](https://meshtastic.org/) mesh networking
with [OpenSensorHub](https://opensensorhub.org/) via the OGC Connected Systems
API.

## Build system

Gradle. OSH itself uses Gradle (`build.gradle`, `common.gradle`,
`release.gradle`); a Maven-based driver fixture would force the agent to
translate Gradle conventions to Maven, which doesn't reflect real OSH
driver development. Java 17, matching `osh-core`'s `sourceCompatibility`.

## Reference material on disk

Indexed upstream sources are mounted read-only at:

- `/sources/osh/github-com-opensensorhub-osh-core/` — OSH core (Gradle
  multi-module). Read `build.gradle`, `common.gradle`, `release.gradle`
  for group/version/publishing, and `sensorhub-core/`, `sensorhub-service-consys/`
  for the API surface this driver implements against.
- `/sources/ogc/github-com-opengeospatial-ogcapi-connected-systems/` —
  OGC Connected Systems API specification.
- `/sources/meshtastic/github-com-meshtastic-meshtastic/` — Meshtastic
  protocol and mesh networking documentation.

These are reference-only — copy specific values out (a coord, a class
signature, a config snippet) into the files you write under your worktree;
do not copy whole directories from `/sources/` into the worktree.

## Building

```sh
./gradlew dependencies   # resolve declared deps (the structural-validator
                         # gradle-dependencies check)
./gradlew test           # compile + run JUnit 5 tests
```

The `build.gradle` ships with `repositories` and `dependencies` blocks
intentionally left as TODOs — those are the agent's first task,
informed by reading the upstream `build.gradle` / `release.gradle` to
discover the publishing target.
