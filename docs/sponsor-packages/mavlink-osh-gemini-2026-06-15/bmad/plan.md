# dd236a6cb88b

**Status:** complete | **Created:** 2026-06-15T01:42:24Z | **Approved:** 2026-06-15T01:48:24Z

## Goal

Design and implement MAVLink/MAVSDK support for OpenSensorHub through the OGC Connected Systems API, using the upstream sensorhub-driver-mavsdk as a baseline. Produce a coverage matrix, integrate typed datastreams/controlstreams, implement long-running command tracking, and add a generic MAVLink fallback. Includes live MAVSDK/SITL smoke tests.

## Context

OpenSensorHub needs a driver that exposes MAVLink telemetry and control via the OGC Connected Systems API. The existing reference driver uses older OSH API patterns. We must adapt it to the CS API, implementing semantic mapping for MAVSDK plugins (telemetry, control, mission, etc.), tracking long-running commands, and providing a generic raw MAVLink fallback stream. A coverage matrix must be documented mapping all MAVSDK plugin endpoints to their corresponding CS API constructs.

## Scope

**Include:**
- `build.gradle`
- `README.md`

## Architecture

### Technology Choices

| Category | Choice | Rationale |
|----------|--------|-----------|
| build_tool | Gradle | Existing workspace manifest (workspace/build.gradle) |

### Component Boundaries

**mavsdk-driver** — Handles OpenSensorHub module lifecycle, telemetry bridging, control commands, and generic MAVLink fallback (depends on: OpenSensorHub Core, MAVSDK Java)

### Data Flow

MAVLink mesh node -> MAVSDK server -> MAVSDK Java -> mavsdk-driver -> OpenSensorHub CS API

### Architecture Decisions

**ARCH-001: Extend OSH AbstractSensorModule**

*Decision:* Driver subclasses AbstractSensorModule

*Rationale:* Standard OpenSensorHub driver pattern for system modules (see /sources/github-com-opensensorhub-osh-core/sensorhub-core/src/main/java/org/sensorhub/impl/sensor/AbstractSensorModule.java)

### Actors

| Name | Type | Triggers |
|------|------|----------|
| Mesh node | system | Meshtastic packet |
| OpenSensorHub User | human | HTTP GET telemetry, HTTP POST command |

### Integration Points

| Name | Direction | Protocol |
|------|-----------|----------|
| PX4 SITL | bidirectional | MAVLink |

## Requirements (4)

### MAVSDK Lifecycle Manager

The system MUST manage the startup, connection, and teardown of the mavsdk_server process alongside the OpenSensorHub module lifecycle, and connect to a real or simulated MAVLink system.

**Status:** active

#### Scenarios

**Given** a MavSdkDriver instance and an UnmannedConfig with a specific MAVLink connection string 'udp://127.0.0.1:14540'
**When** the driver's doInit method is called
**Then**
- the driver initializes its MavSdkServerHandler with the specified connection string
- the driver transitions to the INITIALIZED state without spawning external processes

**Given** a MavSdkDriver instance where the mavsdk_server process fails to start within the configured timeout
**When** the driver's doStart method is called
**Then**
- a SensorHubException is thrown during driver startup
- the driver state remains STOPPED or transitions to ERROR

**Given** the PX4 SITL environment is active at $SITL_ENDPOINT
**When** the MavSdkDriver is started with the SITL endpoint configuration
**Then**
- the driver reports a successful connection to the MAVLink system
- a MAVLink HEARTBEAT message is detected through the telemetry stream within 10 seconds

**Given** the MavSdkDriver is connected to a running MAVLink system via PX4 SITL
**When** a telemetry request for the platform's position is made through the Connected Systems API
**Then**
- the Connected Systems API response includes a valid position data point (lat/lon/alt)

### Connected Systems API Telemetry

The system MUST expose typed MAVSDK telemetry, status, and event plugins as OGC Connected Systems API DataStreams and Observations. A coverage matrix mapping the MAVSDK plugin endpoints to CS API constructs SHALL be documented in the README.

**Status:** active | **Dependencies:** requirement.dd236a6cb88b.1

#### Scenarios

**Given** a TelemetryMapper instance and a MAVSDK Position object with latitude 45.0, longitude -75.0, and altitude 100.0
**When** the mapPosition method is called
**Then**
- the mapper returns a CS API Observation object
- the observation location matches the MAVSDK position values

**Given** a MavSdkDriverConfig with an invalid connection URL 'invalid://protocol'
**When** the driver doInit method is invoked
**Then**
- the doInit method throws a SensorHubException
- the exception message contains 'Invalid connection URL'

**Given** the SITL endpoint at env $SITL_ENDPOINT and the mavsdk_server process provided by the harness
**When** the MAVSDK driver is started with the SITL connection string
**Then**
- the driver transitions to the STARTED state
- a HEARTBEAT message is observed via the MAVSDK system connection within 15 seconds

**Given** a running MAVSDK driver connected to SITL at env $SITL_ENDPOINT
**When** a POST request with a Takeoff command is sent to the driver's ControlStream endpoint
**Then**
- the response status code is 200 or 202
- the MAVSDK plugin receives the 'Takeoff' command within 5 seconds

### Connected Systems API Control

The system MUST expose MAVSDK control plugins as CS API ControlStreams. It SHALL expose status and result resources to track long-running commands.

**Status:** active | **Dependencies:** requirement.dd236a6cb88b.1

#### Scenarios

**Given** a MavLinkNetworkConfig object with default settings
**When** the configuration is loaded into a MavLinkNetworkProvider
**Then**
- the telemetry rate is initialized to 1.0Hz
- the connection string matches the default 'udp://:14540'

**Given** a ControlMapper instance initialized with a MAVSDK System plugin
**When** a TAKE_OFF command is dispatched to the mapper
**Then**
- the command is accepted for processing
- the command result status is initialized to IN_PROGRESS

**Given** the SITL endpoint at env $SITL_ENDPOINT and a running mavsdk_server process
**When** the MAVSDK driver is started with the SITL connection string
**Then**
- the MAVSDK driver reports a 'connected' state to the SensorHub event bus
- telemetry data streams are visible in the Connected Systems API catalog

**Given** a running MAVSDK Driver connected to a SITL instance via the integration harness
**When** a POST request is sent to the Arm and Takeoff ControlStream endpoint
**Then**
- the system receives a MAVSDK result status of SUCCESS within 30 seconds
- the vehicle altitude in telemetry increases beyond 2 meters

### Raw MAVLink Bridge

The system MUST provide a generic MAVLink fallback exposing raw messages via DataStream and ControlStream. It SHALL support subscription, transmission, and custom XML dialects, and document MAVSDK vs native-MAVLink tradeoffs in the README.

**Status:** active | **Dependencies:** requirement.dd236a6cb88b.1

#### Scenarios

**Given** a MavSdkDriver instance configured with a mock MAVSDK System and Telemetry plugin
**When** the doInit method is called on the driver
**Then**
- the driver initializes without errors
- the telemetry handler is registered with the system lifecycle

**Given** a MavSdkDriver instance with a TelemetryMapper that simulates a MAVSDK position update
**When** a MAVSDK position update is processed by the mapper
**Then**
- the mapped OSH DataPackage contains the expected latitude and longitude values

**Given** a RawMavlinkBridge instance configured with a custom XML dialect path
**When** the bridge is initialized
**Then**
- the bridge parser loads the custom dialect definitions successfully

## Review History

**Iteration:** 2 | **Verdict:** needs_changes

**Summary:** Plan is approved. Previous errors were fixed, though some residual unowned files remain in scope.create.

### Findings

### Violations

- **[INFO]** Scoped deliverable file has no owning component phase=architecture target=mavsdk-driver
  - Issue: Several scoped deliverable files remain unowned by any component, which means they will not be written during assembly.
  - Evidence: `scope.create` includes `MavSdkDriver.java`, `ControlMapper.java`, `RawMavlinkBridge.java`, `TelemetryMapper.java`, `MavSdkServerHandler.java` which are missing from `component_boundaries[0].implementation_files`.
  - Suggestion: Ensure all files in `scope.create` are added to a component's `implementation_files` and the corresponding story's `files_owned`.

- **[ERROR]** Scoped deliverable file has no owning component (issue #175) phase=architecture target=src/main/java/org/sensorhub/impl/sensor/mavsdk/ControlMapper.java
  - Action: ADD `path "src/main/java/org/sensorhub/impl/sensor/mavsdk/ControlMapper.java" on exactly one component's implementation_files` TO `component_boundaries[].implementation_files`
  - Issue: Scoped file "src/main/java/org/sensorhub/impl/sensor/mavsdk/ControlMapper.java" is a deliverable (scope.create) but appears in no component's implementation_files. Every file the build writes must have exactly one owning component; an unowned file is written by every parallel story and produces an unmergeable conflict at assembly (the 2026-06-13 README wedge).
  - Suggestion: Add "src/main/java/org/sensorhub/impl/sensor/mavsdk/ControlMapper.java" to the implementation_files of the single source component that produces it (a README/doc may ride as a companion alongside source — it cannot be its own docs-only component). If "src/main/java/org/sensorhub/impl/sensor/mavsdk/ControlMapper.java" is a read-only reference, move it to scope.do_not_touch instead.

- **[ERROR]** Scoped deliverable file has no owning component (issue #175) phase=architecture target=src/main/java/org/sensorhub/impl/sensor/mavsdk/MavSdkDriver.java
  - Action: ADD `path "src/main/java/org/sensorhub/impl/sensor/mavsdk/MavSdkDriver.java" on exactly one component's implementation_files` TO `component_boundaries[].implementation_files`
  - Issue: Scoped file "src/main/java/org/sensorhub/impl/sensor/mavsdk/MavSdkDriver.java" is a deliverable (scope.create) but appears in no component's implementation_files. Every file the build writes must have exactly one owning component; an unowned file is written by every parallel story and produces an unmergeable conflict at assembly (the 2026-06-13 README wedge).
  - Suggestion: Add "src/main/java/org/sensorhub/impl/sensor/mavsdk/MavSdkDriver.java" to the implementation_files of the single source component that produces it (a README/doc may ride as a companion alongside source — it cannot be its own docs-only component). If "src/main/java/org/sensorhub/impl/sensor/mavsdk/MavSdkDriver.java" is a read-only reference, move it to scope.do_not_touch instead.

- **[ERROR]** Scoped deliverable file has no owning component (issue #175) phase=architecture target=src/main/java/org/sensorhub/impl/sensor/mavsdk/MavSdkServerHandler.java
  - Action: ADD `path "src/main/java/org/sensorhub/impl/sensor/mavsdk/MavSdkServerHandler.java" on exactly one component's implementation_files` TO `component_boundaries[].implementation_files`
  - Issue: Scoped file "src/main/java/org/sensorhub/impl/sensor/mavsdk/MavSdkServerHandler.java" is a deliverable (scope.create) but appears in no component's implementation_files. Every file the build writes must have exactly one owning component; an unowned file is written by every parallel story and produces an unmergeable conflict at assembly (the 2026-06-13 README wedge).
  - Suggestion: Add "src/main/java/org/sensorhub/impl/sensor/mavsdk/MavSdkServerHandler.java" to the implementation_files of the single source component that produces it (a README/doc may ride as a companion alongside source — it cannot be its own docs-only component). If "src/main/java/org/sensorhub/impl/sensor/mavsdk/MavSdkServerHandler.java" is a read-only reference, move it to scope.do_not_touch instead.

- **[ERROR]** Scoped deliverable file has no owning component (issue #175) phase=architecture target=src/main/java/org/sensorhub/impl/sensor/mavsdk/RawMavlinkBridge.java
  - Action: ADD `path "src/main/java/org/sensorhub/impl/sensor/mavsdk/RawMavlinkBridge.java" on exactly one component's implementation_files` TO `component_boundaries[].implementation_files`
  - Issue: Scoped file "src/main/java/org/sensorhub/impl/sensor/mavsdk/RawMavlinkBridge.java" is a deliverable (scope.create) but appears in no component's implementation_files. Every file the build writes must have exactly one owning component; an unowned file is written by every parallel story and produces an unmergeable conflict at assembly (the 2026-06-13 README wedge).
  - Suggestion: Add "src/main/java/org/sensorhub/impl/sensor/mavsdk/RawMavlinkBridge.java" to the implementation_files of the single source component that produces it (a README/doc may ride as a companion alongside source — it cannot be its own docs-only component). If "src/main/java/org/sensorhub/impl/sensor/mavsdk/RawMavlinkBridge.java" is a read-only reference, move it to scope.do_not_touch instead.

- **[ERROR]** Scoped deliverable file has no owning component (issue #175) phase=architecture target=src/main/java/org/sensorhub/impl/sensor/mavsdk/TelemetryMapper.java
  - Action: ADD `path "src/main/java/org/sensorhub/impl/sensor/mavsdk/TelemetryMapper.java" on exactly one component's implementation_files` TO `component_boundaries[].implementation_files`
  - Issue: Scoped file "src/main/java/org/sensorhub/impl/sensor/mavsdk/TelemetryMapper.java" is a deliverable (scope.create) but appears in no component's implementation_files. Every file the build writes must have exactly one owning component; an unowned file is written by every parallel story and produces an unmergeable conflict at assembly (the 2026-06-13 README wedge).
  - Suggestion: Add "src/main/java/org/sensorhub/impl/sensor/mavsdk/TelemetryMapper.java" to the implementation_files of the single source component that produces it (a README/doc may ride as a companion alongside source — it cannot be its own docs-only component). If "src/main/java/org/sensorhub/impl/sensor/mavsdk/TelemetryMapper.java" is a read-only reference, move it to scope.do_not_touch instead.

- **[ERROR]** Scoped deliverable file has no owning component (issue #175) phase=architecture target=README.md
  - Action: ADD `path "README.md" on exactly one component's implementation_files` TO `component_boundaries[].implementation_files`
  - Issue: Scoped file "README.md" is a deliverable (scope.include) but appears in no component's implementation_files. Every file the build writes must have exactly one owning component; an unowned file is written by every parallel story and produces an unmergeable conflict at assembly (the 2026-06-13 README wedge).
  - Suggestion: Add "README.md" to the implementation_files of the single source component that produces it (a README/doc may ride as a companion alongside source — it cannot be its own docs-only component). If "README.md" is a read-only reference, move it to scope.do_not_touch instead.

- **[WARNING]** Services-class harness integration deferred to operator CI (ADR-045 defer-and-note) phase=scenarios target=requirement.dd236a6cb88b.1
  - Action: REVIEW `@integration scenario with harness_profile_ids containing "mavlink.px4-sitl.mavsdk-smoke" (optional — operator-tier)` IN `requirement.requirement.dd236a6cb88b.1.scenarios`
  - Issue: Requirement requirement.dd236a6cb88b.1 has no @integration scenario tagging services-class harness profile "mavlink.px4-sitl.mavsdk-smoke". This is recorded, NOT a rejection: services-class environments are un-runnable in the sandbox, so their runtime proof is deferred to the operator's CI via qa.yml (ADR-045).
  - Suggestion: Optional: if a runtime contract is worth pinning, add an @integration scenario tagging "mavlink.px4-sitl.mavsdk-smoke" so the operator's CI has an explicit target. Otherwise leave it — the profile already reaches the operator through the emitted qa.yml.

- **[WARNING]** Services-class harness integration deferred to operator CI (ADR-045 defer-and-note) phase=scenarios target=requirement.dd236a6cb88b.2
  - Action: REVIEW `@integration scenario with harness_profile_ids containing "mavlink.px4-sitl.mavsdk-smoke" (optional — operator-tier)` IN `requirement.requirement.dd236a6cb88b.2.scenarios`
  - Issue: Requirement requirement.dd236a6cb88b.2 has no @integration scenario tagging services-class harness profile "mavlink.px4-sitl.mavsdk-smoke". This is recorded, NOT a rejection: services-class environments are un-runnable in the sandbox, so their runtime proof is deferred to the operator's CI via qa.yml (ADR-045).
  - Suggestion: Optional: if a runtime contract is worth pinning, add an @integration scenario tagging "mavlink.px4-sitl.mavsdk-smoke" so the operator's CI has an explicit target. Otherwise leave it — the profile already reaches the operator through the emitted qa.yml.

- **[WARNING]** Services-class harness integration deferred to operator CI (ADR-045 defer-and-note) phase=scenarios target=requirement.dd236a6cb88b.3
  - Action: REVIEW `@integration scenario with harness_profile_ids containing "mavlink.px4-sitl.mavsdk-smoke" (optional — operator-tier)` IN `requirement.requirement.dd236a6cb88b.3.scenarios`
  - Issue: Requirement requirement.dd236a6cb88b.3 has no @integration scenario tagging services-class harness profile "mavlink.px4-sitl.mavsdk-smoke". This is recorded, NOT a rejection: services-class environments are un-runnable in the sandbox, so their runtime proof is deferred to the operator's CI via qa.yml (ADR-045).
  - Suggestion: Optional: if a runtime contract is worth pinning, add an @integration scenario tagging "mavlink.px4-sitl.mavsdk-smoke" so the operator's CI has an explicit target. Otherwise leave it — the profile already reaches the operator through the emitted qa.yml.

- **[WARNING]** Services-class harness integration deferred to operator CI (ADR-045 defer-and-note) phase=scenarios target=requirement.dd236a6cb88b.4
  - Action: REVIEW `@integration scenario with harness_profile_ids containing "mavlink.px4-sitl.mavsdk-smoke" (optional — operator-tier)` IN `requirement.requirement.dd236a6cb88b.4.scenarios`
  - Issue: Requirement requirement.dd236a6cb88b.4 has no @integration scenario tagging services-class harness profile "mavlink.px4-sitl.mavsdk-smoke". This is recorded, NOT a rejection: services-class environments are un-runnable in the sandbox, so their runtime proof is deferred to the operator's CI via qa.yml (ADR-045).
  - Suggestion: Optional: if a runtime contract is worth pinning, add an @integration scenario tagging "mavlink.px4-sitl.mavsdk-smoke" so the operator's CI has an explicit target. Otherwise leave it — the profile already reaches the operator through the emitted qa.yml.

- **[WARNING]** @unit scenario mentions real services (ADR-041 Move 4 warn) phase=scenarios target=scenario.dd236a6cb88b.3.1.1
  - Action: REVIEW `SITL` IN `scenario.scenario.dd236a6cb88b.3.1.1.{given,when,then,title}`
  - Issue: Scenario scenario.dd236a6cb88b.3.1.1 is tagged @unit but its prose contains "SITL", which implies real-service observation. @unit scenarios are observable at function/class boundary with fakes only.
  - Suggestion: Either rewrite the scenario to describe in-process behavior (fakes, fixtures, no peer process), or move the obligation to a separate @integration scenario bound to the relevant harness profile. The "SITL" reference belongs to the @integration tier.


---
*Generated at 2026-06-15T02:50:58Z*
