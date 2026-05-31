# Architecture: Starting from the existing OpenSensorHub MAVSDK addon at https://github.com/opensensorhub/osh-addons/tree/master/sensors/robotics/sensorhub-driver-mavsdk, design and implement MAVLink/MAVSDK support for OpenSensorHub through the OGC Connected Systems API.

Treat the upstream addon as the baseline, not a clean-room rewrite. Preserve its OSH sensor module patterns, MAVSDK Java integration, mavsdk_server lifecycle, existing telemetry outputs, and existing control inputs unless the architecture explicitly replaces them.

The implementation must provide full Connected Systems API coverage for MAVSDK plugins. For every plugin exposed by the pinned MAVSDK Java/proto version, produce a coverage matrix mapping the plugin's methods and streams to one of:
- CS API DataStream + Observation
- CS API ControlStream + Command + CommandStatus/CommandResult
- SystemEvent
- explicit unsupported/deferred entry with rationale

Prefer typed MAVSDK plugin integrations for semantic APIs. Also evaluate MAVLink-native access and implement a generic MAVLink fallback using MavlinkDirect or a native MAVLink library where needed for raw messages, custom dialects, or plugin gaps. Do not hand-roll MAVLink framing, do not stub MAVSDK/OSH classes, and do not claim full coverage without a machine-checkable coverage inventory.

Acceptance:
1. The driver starts a real mavsdk_server and connects to a real or simulated MAVLink system.
2. CS API exposes typed datastreams for telemetry/status/info/events and typed controlstreams for actions, missions, offboard/manual control, params, camera/gimbal, geofence, FTP/logs, calibration, RTK, shell/tune, transponder/winch/gripper, server-side plugins where applicable.
3. A generic raw MAVLink datastream/controlstream supports subscribe-all, subscribe-by-message-name, send-message, and load-custom-XML dialect.
4. Long-running commands expose status/result resources, not just fire-and-forget acknowledgements.
5. Tests include schema/coverage tests plus at least one live MAVSDK/SITL smoke test.
6. README documents MAVSDK vs native-MAVLink tradeoffs and the coverage matrix.

Test run: 1780102103316

*Generated from the architect role's structured deliverable. The architecture is the bridge between the goal and the implementation.*

## Technology choices

| Category | Choice | Rationale |
|---|---|---|
| MAVLink Library | DroneFleet MAVLink | Required to implement generic raw MAVLink fallback since MAVSDK Java does not fully expose passthrough in the current version (workspace requirement: generic raw MAVLink fallback). |
| Build System | Gradle | Existing project framework (workspace/build.gradle:20) |

## Component boundaries

### driver

Core MAVSDK driver implementing CS API Observation/DataStreams and Command/ControlStreams

- **Internal dependencies:** raw_mavlink_fallback
- **Upstream refs:** OpenSensorHub Core, OSH Connected Systems API, MAVSDK Java, PX4 SITL

### raw_mavlink_fallback

Provides raw MAVLink fallback stream for unsupported dialects

- **Upstream refs:** DroneFleet MAVLink

## Data flow

MAVLink Vehicle -> MAVSDK Server / Raw MAVLink -> Driver -> OSH CS API -> Operator

## Architectural decisions

### ARCH-001: Use MAVSDK for standard plugins, DroneFleet for raw fallback

**Decision:** Implement standard telemetry/commands using MAVSDK Java and a separate raw message fallback using DroneFleet MAVLink.

**Rationale:** Satisfies the requirement for both typed CS API coverage for MAVSDK plugins and generic raw MAVLink fallback without waiting for MAVSDK passthrough support. Cited: /sources/github-com-mavlink-mavsdk-java/sdk/src/main/java/io/mavsdk/System.java

## Actors

- **Operator** (human) — triggers: Request telemetry, Send command
- **MAVLink Vehicle** (system) — triggers: Send HEARTBEAT, Send telemetry update

## Integrations

| Name | Direction | Protocol | Contract | Error mode |
|---|---|---|---|---|
| PX4 SITL MAVLink | bidirectional | MAVLink | MAVLink Common Dialect | Retry connection on timeout, ignore malformed packets |
| OSH CS API | outbound | CS API (HTTP/WebSocket) | OGC Connected Systems API | Propagate CS API errors to the user |

## Upstream resolutions

Every external library, API, or framework the implementation depends on. The architect classifies each one's role; service-style integrations are covered by catalog-backed harness profiles.

### OpenSensorHub Core

- **Coordinate:** `org.sensorhub:sensorhub-core:2.0-beta2`
- **Role:** `runtime_dep`
- **Source ref:** https://github.com/opensensorhub/osh-core/packages
- **Used by:** driver
- **API surfaces consumed (1):**
  - `AbstractSensorModule` (class) — `public abstract class AbstractSensorModule`
    *cited from /sources/github-com-opensensorhub-osh-core/sensorhub-core/src/main/java/org/sensorhub/impl/sensor/AbstractSensorModule.java*

### OSH Connected Systems API

- **Coordinate:** `org.sensorhub:sensorhub-service-consys:2.0-beta2`
- **Role:** `runtime_dep`
- **Source ref:** https://github.com/opensensorhub/osh-core/packages
- **Used by:** driver
- **API surfaces consumed (1):**
  - `DataStreamHandler` (class) — `public class DataStreamHandler`
    *cited from /sources/github-com-opensensorhub-osh-core/sensorhub-service-consys/src/main/java/org/sensorhub/impl/service/consys/obs/DataStreamHandler.java*

### MAVSDK Java

- **Coordinate:** `io.mavsdk:mavsdk:3.14.0`
- **Role:** `runtime_dep`
- **Source ref:** https://github.com/mavlink/MAVSDK-Java
- **Used by:** driver
- **API surfaces consumed (1):**
  - `io.mavsdk.System` (class) — `public class System`
    *cited from /sources/github-com-mavlink-mavsdk-java/sdk/src/main/java/io/mavsdk/System.java*

### DroneFleet MAVLink

- **Coordinate:** `io.dronefleet.mavlink:mavlink:1.1.11`
- **Role:** `runtime_dep`
- **Source ref:** https://central.sonatype.com/artifact/io.dronefleet.mavlink/mavlink/1.1.11
- **Used by:** raw_mavlink_fallback
- **API surfaces consumed (1):**
  - `io.dronefleet.mavlink.MavlinkConnection` (class) — `public class MavlinkConnection`
    *cited from https://github.com/DroneFleet/mavlink/blob/master/src/main/java/io/dronefleet/mavlink/MavlinkConnection.java*

### PX4 SITL

- **Coordinate:** `mavlink:px4-sitl`
- **Role:** `integration_target`
- **Source ref:** https://docs.px4.io/main/en/simulation/
- **Used by:** driver
- **API surfaces consumed (1):**
  - `HEARTBEAT` (class) — `MAVLink HEARTBEAT`
    *cited from https://mavlink.io/en/messages/common.html#HEARTBEAT*

## Harness profiles

| Profile ID | Used by | Purpose | Covers |
|---|---|---|---|
| `mavlink.px4-sitl.mavsdk-smoke` | driver | prove MAVSDK control and telemetry paths against a real PX4 SITL MAVLink endpoint | PX4 SITL, MAVSDK telemetry plugin, MAVSDK action plugin |
| `mavlink.raw-mavlink-direct` | raw_mavlink_fallback | prove generic raw MAVLink parsing without MAVSDK | HEARTBEAT, SYS_STATUS |

## Test surface

### Integration flows

- **driver-telemetry-integration** — components: driver
  - Verify driver receives telemetry from PX4 SITL via MAVSDK and exposes it to CS API DataStreams
- **raw-mavlink-fallback-integration** — components: raw_mavlink_fallback
  - Verify generic raw MAVLink stream decodes HEARTBEAT from PX4 SITL

### End-to-end flows

- Actor **Operator** — 4 step(s), 2 success criteria
- Actor **MAVLink Vehicle** — 3 step(s), 2 success criteria

