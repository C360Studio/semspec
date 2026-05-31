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

Test run: 1780110015106

*Generated from the architect role's structured deliverable. The architecture is the bridge between the goal and the implementation.*

## Technology choices

| Category | Choice | Rationale |
|---|---|---|
| build_tool | Gradle | Existing project uses Gradle per /workspace/build.gradle and /workspace/.semspec/project.json |
| language | Java 17 | Existing project targets Java 17 per /workspace/build.gradle sourceCompatibility |

## Component boundaries

### mavsdk-driver

Exposes MAVLink/MAVSDK telemetry and control to OpenSensorHub through OGC CS API

- **Upstream refs:** OpenSensorHub Core, OpenSensorHub Connected Systems API, MAVSDK Java, PX4 SITL, raw MAVLink endpoint

## Data flow

MAVLink Vehicle <-> MAVSDK Server (or raw UDP) <-> mavsdk-driver <-> OSH CS API (DataStream/ControlStream) <-> OSH Client

## Architectural decisions

### ARCH-001: Generic MAVLink fallback

**Decision:** Implement native MAVLink parser fallback

**Rationale:** Requirement 3 mandates a generic MAVLink fallback for raw messages without relying on MAVSDK abstractions. Cited: /workspace/README.md

### ARCH-002: Use CS API DataStream for Telemetry

**Decision:** Expose MAVSDK via OGC CS API DataStream and ControlStream

**Rationale:** Requirement to replace legacy OSH interfaces with CS API patterns. Cited: /sources/github-com-opensensorhub-osh-core/sensorhub-service-consys/src/main/java/org/sensorhub/impl/service/consys/obs/DataStreamHandler.java

## Actors

- **System Operator** (human) — triggers: Send command via CS API ControlStream
- **MAVLink Vehicle** (system) — triggers: Send MAVLink telemetry, Send MAVLink HEARTBEAT

## Integrations

| Name | Direction | Protocol | Contract | Error mode |
|---|---|---|---|---|
| MAVSDK peer | bidirectional | gRPC | MAVSDK gRPC proto | Retry on gRPC unavailability; report MAVLink timeouts via SystemEvent |
| raw MAVLink peer | bidirectional | UDP/Serial | MAVLink common dialect | Timeout on heartbeat loss |

## Upstream resolutions

Every external library, API, or framework the implementation depends on. The architect classifies each one's role; service-style integrations are covered by catalog-backed harness profiles.

### OpenSensorHub Core

- **Coordinate:** `org.sensorhub:sensorhub-core:2.0.1`
- **Role:** `runtime_dep`
- **Source ref:** /sources/github-com-opensensorhub-osh-core/common.gradle
- **Used by:** mavsdk-driver
- **API surfaces consumed (1):**
  - `AbstractSensorModule` (class) — `public abstract class AbstractSensorModule<T extends SensorConfig>`
    *cited from /sources/github-com-opensensorhub-osh-core/sensorhub-core/src/main/java/org/sensorhub/impl/sensor/AbstractSensorModule.java*

### OpenSensorHub Connected Systems API

- **Coordinate:** `org.sensorhub:sensorhub-service-consys:2.0.1`
- **Role:** `runtime_dep`
- **Source ref:** /sources/github-com-opensensorhub-osh-core/common.gradle
- **Used by:** mavsdk-driver
- **API surfaces consumed (1):**
  - `DataStreamHandler` (class) — `public class DataStreamHandler`
    *cited from /sources/github-com-opensensorhub-osh-core/sensorhub-service-consys/src/main/java/org/sensorhub/impl/service/consys/obs/DataStreamHandler.java*

### MAVSDK Java

- **Coordinate:** `io.mavsdk:mavsdk:3.14.0`
- **Role:** `runtime_dep`
- **Source ref:** /sources/github-com-opensensorhub-osh-addons/sensors/robotics/sensorhub-driver-mavsdk/build.gradle
- **Used by:** mavsdk-driver
- **API surfaces consumed (1):**
  - `System` (class) — `public class System`
    *cited from /sources/github-com-mavlink-mavsdk-java/sdk/src/main/java/io/mavsdk/System.java*

### PX4 SITL

- **Coordinate:** `mavlink:px4-sitl`
- **Role:** `integration_target`
- **Source ref:** https://docs.px4.io/main/en/simulation/
- **Used by:** mavsdk-driver
- **API surfaces consumed (1):**
  - `HEARTBEAT` (interface) — `MAVLink HEARTBEAT`
    *cited from https://mavlink.io/en/messages/common.html#HEARTBEAT*

### raw MAVLink endpoint

- **Coordinate:** `mavlink:raw-mavlink`
- **Role:** `integration_target`
- **Source ref:** https://mavlink.io/en/
- **Used by:** mavsdk-driver
- **API surfaces consumed (1):**
  - `HEARTBEAT` (interface) — `MAVLink HEARTBEAT`
    *cited from https://mavlink.io/en/messages/common.html#HEARTBEAT*

## Harness profiles

| Profile ID | Used by | Purpose | Covers |
|---|---|---|---|
| `mavlink.px4-sitl.mavsdk-smoke` | mavsdk-driver | Prove MAVSDK control and telemetry paths against a real PX4 SITL MAVLink endpoint | PX4 SITL, MAVSDK server, MAVSDK action plugin, MAVSDK telemetry plugin |
| `mavlink.raw-mavlink-direct` | mavsdk-driver | Prove the generic MAVLink fallback logic can decode and round-trip raw MAVLink frames natively | raw MAVLink endpoint |

## Test surface

### Integration flows

- **mavsdk-telemetry-flow** — components: mavsdk-driver
  - Ensure MAVSDK telemetry events map correctly to OGC CS DataStream values
- **raw-mavlink-fallback-flow** — components: mavsdk-driver
  - Verify the driver successfully parses raw MAVLink HEARTBEAT when acting as a direct endpoint

### End-to-end flows

- Actor **System Operator** — 4 step(s), 2 success criteria

