# OSH MAVSDK Driver

Driver for integrating [MAVLink](https://mavlink.io/) /
[MAVSDK](https://mavsdk.mavlink.io/) autopilot telemetry and control with
[OpenSensorHub](https://opensensorhub.org/) via the OGC Connected Systems
API.

## Build environment

This project is a Gradle-based Java driver and requires:

- **Java 17**. The Gradle build sets both `sourceCompatibility` and
  `targetCompatibility` to Java 17 to match the OpenSensorHub core runtime.
- **Gradle**. Use the checked-in Gradle wrapper (`./gradlew`) when possible so
  the build runs with the version expected by this repository. A system Gradle
  installation can also run the same tasks when it is compatible with the
  wrapper configuration.

## Runtime and build dependencies

The driver depends on two main integration libraries:

- **MAVSDK Java** (`io.mavsdk:mavsdk`) for drone communication. MAVSDK provides
  the telemetry and action/control plugins used to communicate with
  MAVLink-capable autopilots.
- **OpenSensorHub (OSH)** (`org.sensorhub:sensorhub-core` and related OSH
  modules) for the service interface exposed through the OGC Connected Systems
  API.

The OSH artifacts are resolved from the OpenSensorHub package repository, not
from Maven Central. The Gradle build script must include the OSH Maven
repository so `sensorhub-core` can be resolved:

```gradle
repositories {
    mavenCentral()
    maven {
        name = 'osh-core'
        url = uri('https://maven.pkg.github.com/opensensorhub/osh-core')
        credentials {
            username = findProperty('gpr.user') ?: System.getenv('GITHUB_ACTOR')
            password = findProperty('gpr.key') ?: System.getenv('GITHUB_TOKEN')
        }
    }
}
```

Set `GITHUB_ACTOR` and `GITHUB_TOKEN`, or the Gradle properties `gpr.user` and
`gpr.key`, before running tasks that resolve OSH artifacts from that repository.
Without the OSH repository and credentials, Gradle cannot download
`sensorhub-core`.

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

From the repository root, run:

```sh
./gradlew build
```

This compiles the Java sources, runs the test suite, and creates the Gradle
build outputs. If you have a compatible system Gradle installed, the equivalent
command is:

```sh
gradle build
```

Useful verification commands are:

```sh
./gradlew dependencies   # resolve and report declared dependencies
./gradlew test           # compile and run JUnit 5 tests
```

## Harness profiles

The `mavlink.px4-sitl.mavsdk-smoke` profile in
`workflow/harnesscatalog/catalog/mavlink.yaml` provides a live PX4 SITL
+ mavsdk_server qa-runner service. The architect selects it for the
required smoke acceptance test; the raw `mavlink.raw-mavlink-direct`
compatibility profile covers the generic-MAVLink path.
