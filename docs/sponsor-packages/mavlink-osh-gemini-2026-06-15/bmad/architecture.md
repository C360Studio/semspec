# Architecture: dd236a6cb88b

*Generated from the architect role's structured deliverable. The architecture is the bridge between the goal and the implementation.*

## Technology choices

| Category | Choice | Rationale |
|---|---|---|
| build_tool | Gradle | Existing workspace manifest (workspace/build.gradle) |

## Component boundaries

### mavsdk-driver

Handles OpenSensorHub module lifecycle, telemetry bridging, control commands, and generic MAVLink fallback

- **Internal dependencies:** OpenSensorHub Core, MAVSDK Java
- **Upstream refs:** OpenSensorHub Core, MAVSDK Java, PX4 SITL

## Data flow

MAVLink mesh node -> MAVSDK server -> MAVSDK Java -> mavsdk-driver -> OpenSensorHub CS API

## Architectural decisions

### ARCH-001: Extend OSH AbstractSensorModule

**Decision:** Driver subclasses AbstractSensorModule

**Rationale:** Standard OpenSensorHub driver pattern for system modules (see /sources/github-com-opensensorhub-osh-core/sensorhub-core/src/main/java/org/sensorhub/impl/sensor/AbstractSensorModule.java)

## Actors

- **Mesh node** (system) — triggers: Meshtastic packet
- **OpenSensorHub User** (human) — triggers: HTTP GET telemetry, HTTP POST command

## Integrations

| Name | Direction | Protocol | Contract | Error mode |
|---|---|---|---|---|
| PX4 SITL | bidirectional | MAVLink | https://docs.px4.io/main/en/simulation/ | Reconnect on timeout |

## Upstream resolutions

Every external library, API, or framework the implementation depends on. The architect classifies each one's role; service-style integrations are covered by catalog-backed harness profiles.

### OpenSensorHub Core

- **Coordinate:** `github.com/opensensorhub/osh-core@master`
- **Resolution kind:** `source_build`
- **Role:** `runtime_dep`
- **Source ref:** https://github.com/opensensorhub/osh-core
- **Used by:** mavsdk-driver
- **API surfaces consumed (4):**
  - `AbstractSensorModule` (class) — `public abstract class AbstractSensorModule<T extends SensorConfig> extends AbstractModule<T> implements ISensorModule<T>`
    import: `org.sensorhub.impl.sensor.AbstractSensorModule`
    *cited from https://github.com/opensensorhub/osh-core/blob/master/sensorhub-core/src/main/java/org/sensorhub/impl/sensor/AbstractSensorModule.java*
  - `AbstractModule.doInit` (method) — `protected void doInit() throws SensorHubException`
    import: `org.sensorhub.impl.module.AbstractModule`
    *cited from https://github.com/opensensorhub/osh-core/blob/master/sensorhub-core/src/main/java/org/sensorhub/impl/module/AbstractModule.java*
  - `AbstractModule.doStart` (method) — `protected void doStart() throws SensorHubException`
    import: `org.sensorhub.impl.module.AbstractModule`
    *cited from https://github.com/opensensorhub/osh-core/blob/master/sensorhub-core/src/main/java/org/sensorhub/impl/module/AbstractModule.java*
  - `AbstractModule.doStop` (method) — `protected void doStop() throws SensorHubException`
    import: `org.sensorhub.impl.module.AbstractModule`
    *cited from https://github.com/opensensorhub/osh-core/blob/master/sensorhub-core/src/main/java/org/sensorhub/impl/module/AbstractModule.java*

### MAVSDK Java

- **Coordinate:** `io.mavsdk:mavsdk:1.4.0`
- **Role:** `runtime_dep`
- **Source ref:** https://central.sonatype.com/artifact/io.mavsdk/mavsdk/1.4.0
- **Used by:** mavsdk-driver
- **API surfaces consumed (1):**
  - `System` (class) — `public class System`
    import: `io.mavsdk.System`
    *cited from https://search.maven.org/artifact/io.mavsdk/mavsdk*

### PX4 SITL

- **Coordinate:** `mavlink:px4-sitl`
- **Role:** `integration_target`
- **Source ref:** https://docs.px4.io/main/en/simulation/
- **Used by:** mavsdk-driver
- **API surfaces consumed (1):**
  - `HEARTBEAT` (config_field) — `MAVLink HEARTBEAT`
    *cited from https://mavlink.io/en/messages/common.html#HEARTBEAT*

## Harness profiles

| Profile ID | Used by | Purpose | Covers |
|---|---|---|---|
| `mavlink.px4-sitl.mavsdk-smoke` | mavsdk-driver | prove MAVSDK control and telemetry paths against a real PX4 SITL MAVLink endpoint | PX4 SITL, MAVSDK server |

## Test surface

### Integration flows

- **sitl-telemetry-and-control** — components: mavsdk-driver
  - Validates telemetry decoding and control forwarding to a live PX4 SITL node via MAVSDK.

### End-to-end flows

- Actor **OpenSensorHub User** — 3 step(s), 2 success criteria

