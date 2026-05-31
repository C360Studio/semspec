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

Test run: 1780102103316

**Status:** ready_for_execution | **Created:** 2026-05-30T00:48:23Z | **Approved:** 2026-05-30T00:49:25Z

## Goal

Design and implement MAVLink/MAVSDK support for OpenSensorHub using the OGC Connected Systems API by adapting the upstream `sensorhub-driver-mavsdk`. Provide typed CS API coverage for MAVSDK plugins (telemetry to DataStreams, commands to ControlStreams), implement generic raw MAVLink fallback, and build a checkable coverage matrix.

## Context

OpenSensorHub's MAVSDK driver needs to be upgraded from legacy SWE patterns to the OGC Connected Systems API. The new implementation will preserve the existing `mavsdk_server` lifecycle and integration patterns from the upstream `sensorhub-driver-mavsdk` module, while modernizing the interface to use CS API Observation, DataStream, and Command models. Additionally, a fallback raw MAVLink stream is required for unsupported custom dialects, and a mapping of all MAVSDK plugins to CS API interfaces must be documented.

## Scope

**Include:**
- `README.md`
- `build.gradle`
- `settings.gradle`

## Architecture

### Technology Choices

| Category | Choice | Rationale |
|----------|--------|-----------|
| MAVLink Library | DroneFleet MAVLink | Required to implement generic raw MAVLink fallback since MAVSDK Java does not fully expose passthrough in the current version (workspace requirement: generic raw MAVLink fallback). |
| Build System | Gradle | Existing project framework (workspace/build.gradle:20) |

### Component Boundaries

**driver** — Core MAVSDK driver implementing CS API Observation/DataStreams and Command/ControlStreams (depends on: raw_mavlink_fallback)

**raw_mavlink_fallback** — Provides raw MAVLink fallback stream for unsupported dialects

### Data Flow

MAVLink Vehicle -> MAVSDK Server / Raw MAVLink -> Driver -> OSH CS API -> Operator

### Architecture Decisions

**ARCH-001: Use MAVSDK for standard plugins, DroneFleet for raw fallback**

*Decision:* Implement standard telemetry/commands using MAVSDK Java and a separate raw message fallback using DroneFleet MAVLink.

*Rationale:* Satisfies the requirement for both typed CS API coverage for MAVSDK plugins and generic raw MAVLink fallback without waiting for MAVSDK passthrough support. Cited: /sources/github-com-mavlink-mavsdk-java/sdk/src/main/java/io/mavsdk/System.java

### Actors

| Name | Type | Triggers |
|------|------|----------|
| Operator | human | Request telemetry, Send command |
| MAVLink Vehicle | system | Send HEARTBEAT, Send telemetry update |

### Integration Points

| Name | Direction | Protocol |
|------|-----------|----------|
| PX4 SITL MAVLink | bidirectional | MAVLink |
| OSH CS API | outbound | CS API (HTTP/WebSocket) |

## Requirements (3)

### Project build and dependencies setup

The build system must configure Gradle settings and dependencies to include MAVSDK Java, MAVLink raw message libraries, and the OpenSensorHub OGC Connected Systems API, establishing the foundational project structure.

**Status:** active

#### Scenarios

**Given** a build environment with Gradle and the project source code
**When** the developer runs the Gradle build command
**Then**
- the build executes successfully without errors
- the classpath includes the MAVSDK-Java library version 0.x or higher
- the classpath includes the OpenSensorHub Connected Systems API artifacts

**Given** a running PX4 SITL MAVLink vehicle and an OSH instance with the MAVSDK driver configured
**When** the MAVLink Vehicle sends telemetry data to the driver
**Then**
- the MAVLink vehicle sends a HEARTBEAT message to the driver
- the driver publishes a telemetry update to the OSH CS API DataStream
- the DataStream update contains valid GPS coordinates from the vehicle

**Given** an Operator authenticated in the OSH system and a connected MAVLink Vehicle
**When** the Operator submits a 'Takeoff' command via the OSH CS API ControlStream
**Then**
- the OSH CS API receives the command request
- the driver translates the request into a MAVSDK/MAVLink command message
- the MAVLink Vehicle acknowledges the command receipt via a HEARTBEAT or command-ack

### Documentation and MAVSDK coverage matrix

The README must document the tradeoffs between using typed MAVSDK plugins and native MAVLink. It must also contain a machine-checkable coverage matrix mapping all MAVSDK plugins to CS API Observation/DataStreams, Command/ControlStreams, SystemEvents, or explicitly deferred entries with rationale.

**Status:** active

#### Scenarios

**Given** a MAVLink Vehicle emitting HEARTBEAT and GLOBAL_POSITION_INT messages connected to the OSH Driver
**When** the vehicle sends a telemetry update over MAVLink
**Then**
- the OSH CS API Observation stream contains updated GPS coordinates matching the MAVLink source
- the OSH CS API DataStream status reflects the vehicle's online state from the HEARTBEAT plugin

**Given** an Operator with access to the OSH CS API ControlStream for a MAVLink Vehicle
**When** the Operator submits an 'arm' command to the vehicle's ControlStream
**Then**
- the MAVLink Vehicle receives a MAV_CMD_COMPONENT_ARM_DISARM command packet
- the OSH CS API returns a success confirmation once the MAVSDK command is acknowledged by the vehicle

**Given** the project README.md file in the repository root
**When** the documentation is inspected for completeness and checkability
**Then**
- the documentation describes the performance and abstraction tradeoffs between typed MAVSDK plugins and raw MAVLink fallback
- a coverage matrix exists mapping MAVSDK plugins (e.g., Telemetry, Action, Mission) to CS API types (Observation, ControlStream, SystemEvent)
- the coverage matrix includes rationales for any 'Deferred' MAVSDK plugins

### Core MAVSDK Driver implementation

The system must implement the core driver logic to start a mavsdk_server, connect to a 'MAVLink Vehicle', and map MAVSDK telemetry and commands to the OGC Connected Systems API. An 'Operator' must be able to use these mapped interfaces for telemetry and control. It must also provide a generic raw MAVLink fallback stream and include integration tests verifying these interactions.

**Status:** active | **Dependencies:** requirement.7cb2e6e5ae4b.1

#### Scenarios

**Given** a MAVLink Vehicle emitting HEARTBEAT and GLOBAL_POSITION_INT messages and a configured OpenSensorHub instance with the MAVSDK driver enabled
**When** the Operator requests the telemetry DataStream for the connected vehicle via the OGC Connected Systems API
**Then**
- the OGC Connected Systems API returns a status of 200 for the telemetry DataStream request
- the response body contains a latitude value matching the MAVLink Vehicle global position data
- the response body contains a longitude value matching the MAVLink Vehicle global position data

**Given** an Operator authenticated with the OpenSensorHub instance and a MAVLink Vehicle in a standby state connected via the MAVSDK driver
**When** the Operator sends an 'Arm' command to the vehicle's ControlStream via the OGC Connected Systems API
**Then**
- the OGC Connected Systems API returns a status code of 202 Accepted
- the MAVLink Vehicle receives a MAV_CMD_COMPONENT_ARM_DISARM command via the MAVSDK bridge
- the vehicle state changes to Armed in subsequent telemetry updates

**Given** a MAVLink Vehicle sending custom MAVLink messages not natively supported by MAVSDK plugins
**When** the Operator subscribes to the 'raw-mavlink' fallback DataStream via the OGC Connected Systems API
**Then**
- the OGC Connected Systems API returns a 200 status for the raw stream request
- the response body contains the HEX-encoded raw MAVLink packet data

**Given** a MAVLink Vehicle that has lost its connection to the MAVSDK server proxy
**When** the Operator attempts to fetch telemetry from the OGC Connected Systems API
**Then**
- the OGC Connected Systems API returns a status of 503 Service Unavailable or marks the stream as inactive
- the response body contains an error message indicating 'Connection to MAVLink Vehicle lost'

## Review History

**Iteration:** 1 | **Verdict:** needs_changes

**Summary:** Plan is missing the core driver implementation requirement and its corresponding scenario.

### Findings

### Violations

- **[ERROR]** Goal Coverage phase=requirements
  - Issue: The plan lacks a requirement to implement the core driver logic. Files declared in scope.create are left completely unassigned.
  - Evidence: scope.create contains 10 files (e.g., UnmannedSystem.java) which are absent from the files_owned lists of all requirements.
  - Suggestion: Add a requirement covering the core implementation of the driver, assigning it ownership of the 10 implementation and test files defined in scope.create.

- **[ERROR]** Requirement-Scenario Coverage phase=scenarios
  - Issue: With the addition of the driver implementation requirement, a corresponding scenario is required.
  - Evidence: Every requirement must have at least one scenario. The required new implementation requirement will need one.
  - Suggestion: Add a new scenario verifying the telemetry or command functionality to accompany the new driver implementation requirement.

- **[WARNING]** Scenario-Actor Coverage phase=scenarios
  - Issue: The architecture declares actors 'MAVLink Vehicle' and 'Operator', but neither is exercised in the current scenarios.
  - Evidence: Current scenarios focus entirely on build processes and documentation validation.
  - Suggestion: Ensure that scenarios test interactions involving 'MAVLink Vehicle' and 'Operator'.


---
*Generated at 2026-05-30T00:56:02Z*
