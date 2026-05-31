# Scenarios: Starting from the existing OpenSensorHub MAVSDK addon at https://github.com/opensensorhub/osh-addons/tree/master/sensors/robotics/sensorhub-driver-mavsdk, design and implement MAVLink/MAVSDK support for OpenSensorHub through the OGC Connected Systems API.

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

*Generated from the scenario-generator role's output. **9 scenarios** verify the implementation, grouped by the requirement they cover.*

## Configure project dependencies for MAVSDK and OSH

*Requirement `requirement.182fec27c834.1` — 3 scenario(s)*

### the project dependencies are refreshed and downloaded during a build task

*ID: `scenario.182fec27c834.1.1`*

**Given** a build system with an empty local artifact cache and the build.gradle configured for the MAVSDK driver

**When** the project dependencies are refreshed and downloaded during a build task

**Then:**

- the project resolves 'org.sensorhub:sensorhub-core' from the OSH repository
- the project resolves 'io.mavsdk:mavsdk' from the MAVSDK repository or Maven Central
- the project resolves 'org.sensorhub:sensorhub-service-consys' for OGC Connected Systems support

### the 'gradle build' command is executed

*ID: `scenario.182fec27c834.1.2`*

**Given** a development environment with the build.gradle configured for Java 17 compatibility

**When** the 'gradle build' command is executed

**Then:**

- the project compiles all source files without version mismatch errors
- the output JAR includes MAVSDK and OSH library references in its manifest or shadow-jar config

### a clean build is attempted

*ID: `scenario.182fec27c834.1.3`*

**Given** a project configuration missing the specific OSH repository URL in build.gradle

**When** a clean build is attempted

**Then:**

- the build fails with an 'unresolved dependency' error for 'org.sensorhub:sensorhub-core'
- the error message indicates that the artifact was not found in Maven Central

## Document MAVSDK coverage matrix and tradeoffs

*Requirement `requirement.182fec27c834.2` — 2 scenario(s)*

### the user views the coverage matrix section

*ID: `scenario.182fec27c834.2.1`*

**Given** a user accessing the project documentation for MAVLink support

**When** the user views the coverage matrix section

**Then:**

- the documentation contains a mapping for each MAVSDK plugin to its OGC CS API representation (DataStream, ControlStream, or SystemEvent)
- the documentation explicitly lists which MAVSDK plugins are currently in 'deferred' status

### the developer reads the architecture and tradeoff documentation

*ID: `scenario.182fec27c834.2.2`*

**Given** a developer evaluating the driver for a custom MAVLink vehicle

**When** the developer reads the architecture and tradeoff documentation

**Then:**

- the documentation lists the benefits and limitations of using the typed MAVSDK plugin versus the generic MAVLink fallback
- the documentation provides guidance on when to use raw MAVLink peer (UDP/Serial) instead of the gRPC-based MAVSDK peer

## Implement MAVSDK driver, generic MAVLink fallback, and tests

*Requirement `requirement.182fec27c834.3` — 4 scenario(s)*

### a GET request is sent to the OGC CS API Datastreams endpoint for the vehicle position

*ID: `scenario.182fec27c834.3.1`*

**Given** an OpenSensorHub instance with the MAVSDK driver configured and a connected SITL vehicle sending telemetry

**When** a GET request is sent to the OGC CS API Datastreams endpoint for the vehicle position

**Then:**

- the response status is 200 OK
- the response body contains a Datastream with typed position data (latitude, longitude, altitude) mapped from MAVSDK telemetry

### a GET request is sent to the OGC CS API Datastreams endpoint for the raw MAVLink fallback stream

*ID: `scenario.182fec27c834.3.2`*

**Given** an OpenSensorHub instance with the MAVSDK driver and a MAVLink vehicle providing a non-standard message not in the MAVSDK plugin set

**When** a GET request is sent to the OGC CS API Datastreams endpoint for the raw MAVLink fallback stream

**Then:**

- the response status is 200 OK
- the response body contains the raw MAVLink message payload and message ID in a generic Datastream format

### the System Operator posts a "Takeoff" command to the OGC CS API ControlStream endpoint

*ID: `scenario.182fec27c834.3.3`*

**Given** a System Operator authenticated with the OGC Connected Systems API and an active MAVSDK vehicle session

**When** the System Operator posts a "Takeoff" command to the OGC CS API ControlStream endpoint

**Then:**

- the response status is 202 Accepted
- the MAVSDK peer receives a gRPC command to perform the requested takeoff action
- the vehicle begins the takeoff procedure in the SITL environment

### the automated integration test suite is executed against the SITL environment

*ID: `scenario.182fec27c834.3.4`*

**Given** an OpenSensorHub instance with the MAVSDK driver and a SITL simulator running

**When** the automated integration test suite is executed against the SITL environment

**Then:**

- the test execution completes successfully with 100% pass rate
- the MAVSDK plugin coverage matrix includes verified telemetry and control paths

