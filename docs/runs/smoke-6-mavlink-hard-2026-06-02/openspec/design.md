# Design: Starting from the existing OpenSensorHub MAVSDK addon at https://github.com/opensensorhub/osh-addons/tree/master/sensors/robotics/sensorhub-driver-mavsdk, design and implement MAVLink/MAVSDK support for OpenSensorHub through the OGC Connected Systems API.

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

Test run: 1780368353973

## Technology Choices

| Category | Choice | Rationale |
|---|---|---|
| language | Java 17 | Project explicitly sets sourceCompatibility = JavaVersion.VERSION_17 in build.gradle. |
| robotics_framework | MAVSDK Java | Provides typed Java bindings for MAVLink over gRPC to a mavsdk_server instance. Required by prompt. |
| iot_framework | OpenSensorHub | Target platform for the OGC Connected Systems API driver. |

## Data Flow

OGC CS Client <-> OGC ConSys API <-> MAVSDK OSH Driver <-> MAVSDK Java Client <-> gRPC <-> mavsdk_server <-> MAVLink UDP <-> PX4 SITL Autopilot

## Components

### mavsdk-osh-driver

**Responsibility**: Provides MAVLink/MAVSDK support to OpenSensorHub. Maps MAVSDK plugins to OGC CS API paradigms and manages the mavsdk_server lifecycle.

**Dependencies**: MAVSDK Java, OpenSensorHub Core

**Upstream refs**: MAVSDK Java, OpenSensorHub Core, PX4 SITL

## Decisions

### ARCH-001: Manage mavsdk_server as a subprocess

**Decision**: The driver will spawn and manage the native mavsdk_server executable as a child process.

**Rationale**: MAVSDK Java does not bundle a desktop-compatible embedded server (it provides an 'aar' for Android), so the desktop/server driver must manage the binary lifecycle directly. (Ref: https://github.com/mavlink/MAVSDK-Java/blob/main/README.md)

### ARCH-002: Extend AbstractSensorModule

**Decision**: The driver will extend AbstractSensorModule.

**Rationale**: Standard OSH driver pattern for integrating external systems. (Ref: https://github.com/opensensorhub/osh-core/blob/master/sensorhub-core/src/main/java/org/sensorhub/impl/sensor/AbstractSensorModule.java)

## Test Harness Profiles

- `mavlink.px4-sitl.mavsdk-smoke` — used by mavsdk-osh-driver: Prove MAVSDK control and telemetry paths against a real PX4 SITL MAVLink endpoint.

## Integration Points

| Name | Direction | Protocol | Contract | Error Mode |
|---|---|---|---|---|
| PX4 SITL | bidirectional | MAVLink UDP | MAVLink Common Dialect | Retry connection on timeout |

