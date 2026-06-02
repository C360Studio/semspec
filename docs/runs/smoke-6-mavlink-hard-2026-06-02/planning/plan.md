# Starting from the existing OpenSensorHub MAVSDK addon at https://github.com/opensensorhub/osh-addons/tree/master/sensors/robotics/sensorhub-driver-mavsdk, design and implement MAVLink/MAVSDK support for OpenSensorHub through the OGC Connected Systems API.

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

**Status:** ready_for_execution | **Created:** 2026-06-02T02:45:53Z | **Approved:** 2026-06-02T02:47:35Z

## Goal

Design and implement MAVLink/MAVSDK support for OpenSensorHub via the OGC Connected Systems API, incorporating typed streams, a raw MAVLink fallback, long-running command support, and a complete coverage matrix with SITL testing.

## Context

OpenSensorHub requires a fully compliant OGC Connected Systems API driver for MAVLink-based unmanned systems. The current upstream implementation provides a baseline but needs comprehensive mapping of MAVSDK plugins to CS API paradigms (DataStreams, ControlStreams, Observation, Command) as well as raw MAVLink fallback and reliable lifecycle management for mavsdk_server. A machine-checkable coverage matrix is necessary to ensure semantic APIs are systematically mapped or explicitly deferred.

## Scope

**Include:**
- `build.gradle`
- `README.md`

## Architecture

### Technology Choices

| Category | Choice | Rationale |
|----------|--------|-----------|
| language | Java 17 | Project explicitly sets sourceCompatibility = JavaVersion.VERSION_17 in build.gradle. |
| robotics_framework | MAVSDK Java | Provides typed Java bindings for MAVLink over gRPC to a mavsdk_server instance. Required by prompt. |
| iot_framework | OpenSensorHub | Target platform for the OGC Connected Systems API driver. |

### Component Boundaries

**mavsdk-osh-driver** — Provides MAVLink/MAVSDK support to OpenSensorHub. Maps MAVSDK plugins to OGC CS API paradigms and manages the mavsdk_server lifecycle. (depends on: MAVSDK Java, OpenSensorHub Core)

### Data Flow

OGC CS Client <-> OGC ConSys API <-> MAVSDK OSH Driver <-> MAVSDK Java Client <-> gRPC <-> mavsdk_server <-> MAVLink UDP <-> PX4 SITL Autopilot

### Architecture Decisions

**ARCH-001: Manage mavsdk_server as a subprocess**

*Decision:* The driver will spawn and manage the native mavsdk_server executable as a child process.

*Rationale:* MAVSDK Java does not bundle a desktop-compatible embedded server (it provides an 'aar' for Android), so the desktop/server driver must manage the binary lifecycle directly. (Ref: https://github.com/mavlink/MAVSDK-Java/blob/main/README.md)

**ARCH-002: Extend AbstractSensorModule**

*Decision:* The driver will extend AbstractSensorModule.

*Rationale:* Standard OSH driver pattern for integrating external systems. (Ref: https://github.com/opensensorhub/osh-core/blob/master/sensorhub-core/src/main/java/org/sensorhub/impl/sensor/AbstractSensorModule.java)

### Actors

| Name | Type | Triggers |
|------|------|----------|
| MAVLink Autopilot | system | Sends telemetry updates, Sends command acknowledgements |
| OGC CS Client | system | Subscribes to DataStreams, Issues ControlStreams commands |

### Integration Points

| Name | Direction | Protocol |
|------|-----------|----------|
| PX4 SITL | bidirectional | MAVLink UDP |

## Requirements (5)

### MAVSDK server and client lifecycle management

The system MUST manage the embedded mavsdk_server binary process, establishing and maintaining the connection to a MAVLink system. The driver SHALL start the server, connect a MAVSDK Java client instance, and successfully pass a live MAVSDK/SITL smoke test.

**Status:** active

#### Scenarios

**Given** a configuration object with a valid local path to a mavsdk_server binary and a valid connection URL
**When** the server process manager prepares the startup command
**Then**
- the command line arguments include the specified connection URL
- the process builder is initialized with the correct executable path

**Given** a MAVSDK driver instance configured to retry connection on failure
**When** the mavsdk_server process exits with a non-zero code
**Then**
- the driver status indicates it is attempting to reconnect
- the process manager attempts to restart the mavsdk_server process

**Given** the PX4 SITL endpoint at environment variable $SITL_UDP_ENDPOINT
**When** the MAVSDK driver is initialized and started with the SITL endpoint
**Then**
- the MAVSDK Java client reports a successful connection to the system
- the driver status transitions to 'CONNECTED' within 30 seconds
- a MAVLink HEARTBEAT message is detected through the MAVSDK connection

### Telemetry DataStreams via OGC CS API

The system MUST expose MAVSDK telemetry, information, and event plugins as OGC Connected Systems API DataStreams and Observations. A machine-checkable coverage matrix MUST be provided that maps each pinned plugin's streams to CS API paradigms or explicitly justifies its deferral.

**Status:** active | **Dependencies:** requirement.3e7e6046bbe8.1

#### Scenarios

**Given** a TelemetryMapper instance and a MAVSDK Position object with latitude 47.397, longitude 8.545, and absolute altitude 488.0
**When** the mapper is called to transform the position telemetry
**Then**
- the mapper returns an OGC Observation containing a Point geometry with coordinates [8.545, 47.397, 488.0]
- the Observation result includes the correct UOM for altitude as 'm'

**Given** the SITL endpoint at env $SITL_ENDPOINT and the mavsdk-osh-driver configured to connect to it
**When** an OGC CS Client subscribes to the position DataStream and the vehicle is armed and hovering
**Then**
- the OGC CS API endpoint /datastreams/position/observations returns a valid observation within 15 seconds
- the observation data matches the current SITL vehicle coordinates

**Given** a TelemetryMapper initialized with an empty mapping configuration
**When** an unsupported MAVSDK telemetry event is passed to the mapper
**Then**
- the mapper returns an empty optional or throws a DataMappingException
- no malformed OGC Observation is produced

### Control ControlStreams via OGC CS API

The system MUST bridge MAVSDK action, mission, and component control plugins to OGC Connected Systems API ControlStreams. Like telemetry, these typed streams MUST be documented in the coverage matrix to demonstrate systematic integration of the semantic API.

**Status:** active | **Dependencies:** requirement.3e7e6046bbe8.1

#### Scenarios

**Given** a ControlMapper initialized with a mock MAVSDK Action plugin and a target CS API ControlStream definition for 'Takeoff'
**When** the mapping function is invoked for the Takeoff action
**Then**
- the mapper produces a CS API Command structure with double-typed 'altitude' parameter
- the mapper's execution function invokes the MAVSDK Action 'takeoff' method when the command is received

**Given** the PX4 SITL and MAVSDK Server started by the test harness at $MAVSDK_SERVER_ADDRESS
**When** a POST request containing a Takeoff command is sent to the OGC CS ControlStream endpoint
**Then**
- the OGC CS API endpoint returns a 202 Accepted response
- the MAVSDK Action plugin reports the drone has transitioned to 'Taking Off' state
- the drone altitude in SITL increases above 1 meter within 30 seconds

**Given** a ControlMapper configured for MAVSDK Mission uploads with a valid CS API Mission schema
**When** a Mission command is received with an empty waypoint array
**Then**
- the mapper rejects the command with a 'Missing Parameter' error for the waypoint list

**Given** the PX4 SITL environment at $MAVSDK_SERVER_ADDRESS with the drone currently in 'Hold' mode
**When** a 'Land' command is issued via OGC CS API while the drone is already on the ground
**Then**
- the OGC CS API returns a 500 Internal Server Error or appropriate error JSON
- the error message indicates 'No landable surface' or 'Command denied' from MAVSDK

### Asynchronous command status and results

The system SHALL track asynchronous, long-running MAVSDK commands instead of treating them as fire-and-forget. The progress and completion states MUST be exposed via CS API CommandStatus and CommandResult resources.

**Status:** active | **Dependencies:** requirement.3e7e6046bbe8.3

#### Scenarios

**Given** a ControlMapper instance and a mock CommandStatus tracker
**When** a long-running command (e.g., Takeoff) is initiated and the internal MAVSDK observer emits a success event
**Then**
- the CommandStatus status transitions to 'ACCEPTED'
- the CommandStatus status eventually transitions to 'COMPLETED' upon receipt of the success callback

**Given** a ControlMapper instance and a mock CommandStatus tracker
**When** a command is initiated and the internal MAVSDK observer emits a 'Command Denied' error event
**Then**
- the CommandStatus status transitions to 'FAILED'
- the CommandResult contains the error message 'Action denied by vehicle'

**Given** the SITL endpoint at env $SITL_ENDPOINT, and the MAVSDK driver active and connected to the drone
**When** an OGC CS Client sends a ControlStream request for 'Takeoff' to the driver endpoint
**Then**
- the CS API returns a CommandStatus resource for the 'Takeoff' request with status 'IN_PROGRESS'
- the CommandStatus status transitions to 'COMPLETED' after the drone reaches takeoff altitude

### Raw MAVLink fallback streams

The system MUST provide a generic MAVLink fallback mechanism for raw message publish/subscribe and custom XML dialect loading through CS API streams. The README MUST document the tradeoffs between this native MAVLink approach and the typed MAVSDK integrations.

**Status:** active | **Dependencies:** requirement.3e7e6046bbe8.1

#### Scenarios

**Given** a MAVSDK driver configuration with a custom XML dialect path provided
**When** the driver prepares the server process command line args
**Then**
- the driver initializes the MAVSDK server process with the --custom-xml-path argument matching the provided path
- the dialect loader confirms the custom message types are registered in the internal message dictionary

**Given** the OGC CS API stream configured for raw MAVLink data with a specific message ID filter
**When** a raw MAVLink message is broadcast by the SITL autopilot on the monitored UDP port
**Then**
- the CS API stream receives the raw MAVLink packets for the requested message ID within 5 seconds
- the raw payload matches the bitstream expected from the SITL simulated telemetry

**Given** the MAVSDK driver is connected to the SITL endpoint via the test harness environment variables
**When** a raw MAVLink packet is sent through the OGC CS ControlStream endpoint
**Then**
- the SITL autopilot receives the raw MAVLink packet and acknowledges receipt if applicable
- the MAVSDK driver logs the successful dispatch of the raw message to the autopilot link

**Given** a configuration object with a missing dialect path but raw fallback enabled
**When** the MavsdkDriver is initialized
**Then**
- the driver successfully initializes using the standard common.xml dialect
- the driver logs a warning that custom message decoding will be limited to the standard dialect

---
*Generated at 2026-06-02T02:54:45Z*
