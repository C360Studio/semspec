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

Test run: 1780110015106

**Status:** ready_for_execution | **Created:** 2026-05-30T03:00:15Z | **Approved:** 2026-05-30T03:01:07Z

## Goal

Implement MAVLink/MAVSDK support for OpenSensorHub through the OGC Connected Systems API, including typed MAVSDK plugin coverage and a generic MAVLink fallback.

## Context

The implementation needs to bridge the MAVLink/MAVSDK Java stack into OpenSensorHub using the OGC Connected Systems (CS) API. It extends the upstream sensorhub-driver-mavsdk addon by replacing legacy OSH interfaces with CS API DataStream, ControlStream, and SystemEvent patterns. The integration must map all pinned MAVSDK plugins to semantic API endpoints, offer a generic raw MAVLink fallback, ensure long-running commands yield proper status/result resources, and produce a documented coverage matrix. OSH artifacts are located at https://maven.pkg.github.com/opensensorhub/osh-core.

## Scope

**Include:**
- `build.gradle`
- `README.md`

## Architecture

### Technology Choices

| Category | Choice | Rationale |
|----------|--------|-----------|
| build_tool | Gradle | Existing project uses Gradle per /workspace/build.gradle and /workspace/.semspec/project.json |
| language | Java 17 | Existing project targets Java 17 per /workspace/build.gradle sourceCompatibility |

### Component Boundaries

**mavsdk-driver** — Exposes MAVLink/MAVSDK telemetry and control to OpenSensorHub through OGC CS API

### Data Flow

MAVLink Vehicle <-> MAVSDK Server (or raw UDP) <-> mavsdk-driver <-> OSH CS API (DataStream/ControlStream) <-> OSH Client

### Architecture Decisions

**ARCH-001: Generic MAVLink fallback**

*Decision:* Implement native MAVLink parser fallback

*Rationale:* Requirement 3 mandates a generic MAVLink fallback for raw messages without relying on MAVSDK abstractions. Cited: /workspace/README.md

**ARCH-002: Use CS API DataStream for Telemetry**

*Decision:* Expose MAVSDK via OGC CS API DataStream and ControlStream

*Rationale:* Requirement to replace legacy OSH interfaces with CS API patterns. Cited: /sources/github-com-opensensorhub-osh-core/sensorhub-service-consys/src/main/java/org/sensorhub/impl/service/consys/obs/DataStreamHandler.java

### Actors

| Name | Type | Triggers |
|------|------|----------|
| System Operator | human | Send command via CS API ControlStream |
| MAVLink Vehicle | system | Send MAVLink telemetry, Send MAVLink HEARTBEAT |

### Integration Points

| Name | Direction | Protocol |
|------|-----------|----------|
| MAVSDK peer | bidirectional | gRPC |
| raw MAVLink peer | bidirectional | UDP/Serial |

## Requirements (3)

### Configure project dependencies for MAVSDK and OSH

The build system must be configured with the OSH and MAVSDK Java dependencies required to bridge the MAVLink/MAVSDK stack into OpenSensorHub. It must specify the correct artifact repositories and dependency versions.

**Status:** active

#### Scenarios

**Given** a build system with an empty local artifact cache and the build.gradle configured for the MAVSDK driver
**When** the project dependencies are refreshed and downloaded during a build task
**Then**
- the project resolves 'org.sensorhub:sensorhub-core' from the OSH repository
- the project resolves 'io.mavsdk:mavsdk' from the MAVSDK repository or Maven Central
- the project resolves 'org.sensorhub:sensorhub-service-consys' for OGC Connected Systems support

**Given** a development environment with the build.gradle configured for Java 17 compatibility
**When** the 'gradle build' command is executed
**Then**
- the project compiles all source files without version mismatch errors
- the output JAR includes MAVSDK and OSH library references in its manifest or shadow-jar config

**Given** a project configuration missing the specific OSH repository URL in build.gradle
**When** a clean build is attempted
**Then**
- the build fails with an 'unresolved dependency' error for 'org.sensorhub:sensorhub-core'
- the error message indicates that the artifact was not found in Maven Central

### Document MAVSDK coverage matrix and tradeoffs

The project documentation must include a coverage matrix mapping MAVSDK plugins to CS API DataStream, ControlStream, SystemEvent, or explicit deferred status. It must also document the tradeoffs between MAVSDK and native-MAVLink fallbacks.

**Status:** active

#### Scenarios

**Given** a user accessing the project documentation for MAVLink support
**When** the user views the coverage matrix section
**Then**
- the documentation contains a mapping for each MAVSDK plugin to its OGC CS API representation (DataStream, ControlStream, or SystemEvent)
- the documentation explicitly lists which MAVSDK plugins are currently in 'deferred' status

**Given** a developer evaluating the driver for a custom MAVLink vehicle
**When** the developer reads the architecture and tradeoff documentation
**Then**
- the documentation lists the benefits and limitations of using the typed MAVSDK plugin versus the generic MAVLink fallback
- the documentation provides guidance on when to use raw MAVLink peer (UDP/Serial) instead of the gRPC-based MAVSDK peer

### Implement MAVSDK driver, generic MAVLink fallback, and tests

The system must expose typed MAVSDK datastreams and controlstreams via the OGC Connected Systems API, support a generic MAVLink fallback for raw messages, and provide a test suite with coverage matrix and SITL smoke tests.

**Status:** active | **Dependencies:** requirement.182fec27c834.1

#### Scenarios

**Given** an OpenSensorHub instance with the MAVSDK driver configured and a connected SITL vehicle sending telemetry
**When** a GET request is sent to the OGC CS API Datastreams endpoint for the vehicle position
**Then**
- the response status is 200 OK
- the response body contains a Datastream with typed position data (latitude, longitude, altitude) mapped from MAVSDK telemetry

**Given** an OpenSensorHub instance with the MAVSDK driver and a MAVLink vehicle providing a non-standard message not in the MAVSDK plugin set
**When** a GET request is sent to the OGC CS API Datastreams endpoint for the raw MAVLink fallback stream
**Then**
- the response status is 200 OK
- the response body contains the raw MAVLink message payload and message ID in a generic Datastream format

**Given** a System Operator authenticated with the OGC Connected Systems API and an active MAVSDK vehicle session
**When** the System Operator posts a "Takeoff" command to the OGC CS API ControlStream endpoint
**Then**
- the response status is 202 Accepted
- the MAVSDK peer receives a gRPC command to perform the requested takeoff action
- the vehicle begins the takeoff procedure in the SITL environment

**Given** an OpenSensorHub instance with the MAVSDK driver and a SITL simulator running
**When** the automated integration test suite is executed against the SITL environment
**Then**
- the test execution completes successfully with 100% pass rate
- the MAVSDK plugin coverage matrix includes verified telemetry and control paths

## Review History

**Iteration:** 1 | **Verdict:** needs_changes

**Summary:** The plan has severe goal coverage gaps (missing requirements for the core implementation) and orphaned scope files. It also has invalid component dependency references.

### Findings

### Violations

- **[ERROR]** Goal Coverage & File Ownership phase=requirements
  - Issue: The plan misses requirements for the core implementation. The goal specifies 'Implement MAVLink/MAVSDK support...' but the requirements only cover build.gradle and README.md. The files in scope.create are completely orphaned.
  - Evidence: Plan goal states 'Implement MAVLink/MAVSDK support...'. plan.requirements contains only req 1 (build.gradle) and req 2 (README.md). Files in scope.create are unowned.
  - Suggestion: Add a requirement to implement the MAVSDK driver, generic MAVLink fallback, and tests, owning all the Java files in scope.create.

- **[ERROR]** Upstream Resolution Discipline phase=architecture target=mavsdk-driver
  - Issue: mavsdk-driver lists 'sensorhub-core', 'sensorhub-service-consys', and 'mavsdk' in dependencies, but these are external libraries that do not match the names in upstream_resolutions. Dependencies should only point to internal components, while upstream_refs covers external integrations.
  - Evidence: architecture.component_boundaries[0].dependencies = ['sensorhub-core', 'sensorhub-service-consys', 'mavsdk']; upstream_resolutions names are 'OpenSensorHub Core', etc.
  - Suggestion: Replace dependencies for mavsdk-driver with an empty array, as it has no internal dependencies.


---
*Generated at 2026-05-30T03:06:31Z*
