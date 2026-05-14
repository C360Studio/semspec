# OSH Meshtastic Driver

Driver for integrating [Meshtastic](https://meshtastic.org/) mesh networking
with [OpenSensorHub](https://opensensorhub.org/) via the OGC Connected Systems
API.

## Build system

Gradle. OSH itself uses Gradle (`build.gradle`, `common.gradle`,
`release.gradle`); a Maven-based driver fixture would force the agent to
translate Gradle conventions to Maven, which doesn't reflect real OSH
driver development. Java 17, matching `osh-core`'s `sourceCompatibility`.

## Reference material

The driver implements OpenSensorHub's Connected Systems API. The agent is
expected to discover the upstream coordinates (group/version/publishing
target), API surface, and configuration patterns via `web_search` and
`http_request` against the canonical upstream sources:

- OSH core: `github.com/opensensorhub/osh-core` (Gradle multi-module).
  `sensorhub-core` and `sensorhub-service-consys` are the relevant
  subprojects.
- OGC Connected Systems API spec: `github.com/opengeospatial/ogcapi-connected-systems`.
- Meshtastic protocol: `github.com/meshtastic/meshtastic`.

Use `web_search` + `http_request` to read upstream `build.gradle`,
`release.gradle`, `common.gradle`, and source files directly from the
canonical GitHub raw URLs. Cite specific values (a version, a class
signature, a config snippet) in your implementation; do not paste large
blocks of upstream source.

## Building

```sh
./gradlew dependencies   # resolve declared deps (the structural-validator
                         # gradle-dependencies check)
./gradlew test           # compile + run JUnit 5 tests
```

The `build.gradle` ships with `repositories` and `dependencies` blocks
intentionally left as TODOs — those are the agent's first task,
informed by reading the upstream `build.gradle` / `release.gradle` (via
`http_request` against `raw.githubusercontent.com`) to discover the
publishing target.
