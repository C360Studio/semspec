# OSH MAVSDK Driver

Driver for integrating [MAVLink](https://mavlink.io/) /
[MAVSDK](https://mavsdk.mavlink.io/) autopilot telemetry and control with
[OpenSensorHub](https://opensensorhub.org/) via the OGC Connected Systems
API.

## Build system

Gradle. OSH itself uses Gradle (`build.gradle`, `common.gradle`,
`release.gradle`); a Maven-based driver fixture would force the agent to
translate Gradle conventions to Maven, which doesn't reflect real OSH
driver development. Java 17, matching `osh-core`'s `sourceCompatibility`.

## Baseline (the prompt's starting point)

The upstream reference implementation lives at
`opensensorhub/osh-addons/sensors/robotics/sensorhub-driver-mavsdk`. The
`@mavlink-hard` Playwright spec instructs the agent to treat that addon
as the baseline — preserve its OSH sensor module patterns, MAVSDK Java
integration, mavsdk_server lifecycle, telemetry outputs, and control
inputs unless the architecture explicitly replaces them.

## Reference material

The driver implements OpenSensorHub's Connected Systems API on top of
MAVSDK Java. The agent reads upstream sources directly when the EPIC
overlay is active:

- OSH addons (includes the baseline MAVSDK driver): `/sources/github-com-opensensorhub-osh-addons`
- MAVSDK Java library: `/sources/github-com-mavlink-MAVSDK-Java`
- OSH core (Gradle multi-module): `/sources/github-com-opensensorhub-osh-core`
- OGC Connected Systems API spec: `/sources/github-com-opengeospatial-ogcapi-connected-systems`

When `/sources/` is not mounted, fall back to `web_search` +
`http_request` against the canonical GitHub raw URLs. Cite specific
values (a version, a class signature, a config snippet) in the
implementation; do not paste large blocks of upstream source.

## Building

```sh
./gradlew dependencies   # resolve declared deps (the structural-validator
                         # gradle-dependencies check)
./gradlew test           # compile + run JUnit 5 tests
```

The `build.gradle` ships with `repositories` and `dependencies` blocks
intentionally left as TODOs — those are the agent's first task,
informed by reading the upstream `sensorhub-driver-mavsdk/build.gradle`
and the OSH + MAVSDK release configurations.

## Harness profiles

The `mavlink.px4-sitl.mavsdk-smoke` profile in
`workflow/harnesscatalog/catalog/mavlink.yaml` provides a live PX4 SITL
+ mavsdk_server qa-runner service. The architect selects it for the
required smoke acceptance test; the raw `mavlink.raw-mavlink-direct`
compatibility profile covers the generic-MAVLink path.
