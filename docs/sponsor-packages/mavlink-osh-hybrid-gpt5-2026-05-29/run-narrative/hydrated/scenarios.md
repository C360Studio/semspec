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

Test run: 1780102103316

*Generated from the scenario-generator role's output. **10 scenarios** verify the implementation, grouped by the requirement they cover.*

## Project build and dependencies setup

*Requirement `requirement.7cb2e6e5ae4b.1` — 3 scenario(s)*

### the developer runs the Gradle build command

*ID: `scenario.7cb2e6e5ae4b.1.1`*

**Given** a build environment with Gradle and the project source code

**When** the developer runs the Gradle build command

**Then:**

- the build executes successfully without errors
- the classpath includes the MAVSDK-Java library version 0.x or higher
- the classpath includes the OpenSensorHub Connected Systems API artifacts

### the MAVLink Vehicle sends telemetry data to the driver

*ID: `scenario.7cb2e6e5ae4b.1.2`*

**Given** a running PX4 SITL MAVLink vehicle and an OSH instance with the MAVSDK driver configured

**When** the MAVLink Vehicle sends telemetry data to the driver

**Then:**

- the MAVLink vehicle sends a HEARTBEAT message to the driver
- the driver publishes a telemetry update to the OSH CS API DataStream
- the DataStream update contains valid GPS coordinates from the vehicle

### the Operator submits a 'Takeoff' command via the OSH CS API ControlStream

*ID: `scenario.7cb2e6e5ae4b.1.3`*

**Given** an Operator authenticated in the OSH system and a connected MAVLink Vehicle

**When** the Operator submits a 'Takeoff' command via the OSH CS API ControlStream

**Then:**

- the OSH CS API receives the command request
- the driver translates the request into a MAVSDK/MAVLink command message
- the MAVLink Vehicle acknowledges the command receipt via a HEARTBEAT or command-ack

## Documentation and MAVSDK coverage matrix

*Requirement `requirement.7cb2e6e5ae4b.2` — 3 scenario(s)*

### the vehicle sends a telemetry update over MAVLink

*ID: `scenario.7cb2e6e5ae4b.2.1`*

**Given** a MAVLink Vehicle emitting HEARTBEAT and GLOBAL_POSITION_INT messages connected to the OSH Driver

**When** the vehicle sends a telemetry update over MAVLink

**Then:**

- the OSH CS API Observation stream contains updated GPS coordinates matching the MAVLink source
- the OSH CS API DataStream status reflects the vehicle's online state from the HEARTBEAT plugin

### the Operator submits an 'arm' command to the vehicle's ControlStream

*ID: `scenario.7cb2e6e5ae4b.2.2`*

**Given** an Operator with access to the OSH CS API ControlStream for a MAVLink Vehicle

**When** the Operator submits an 'arm' command to the vehicle's ControlStream

**Then:**

- the MAVLink Vehicle receives a MAV_CMD_COMPONENT_ARM_DISARM command packet
- the OSH CS API returns a success confirmation once the MAVSDK command is acknowledged by the vehicle

### the documentation is inspected for completeness and checkability

*ID: `scenario.7cb2e6e5ae4b.2.3`*

**Given** the project README.md file in the repository root

**When** the documentation is inspected for completeness and checkability

**Then:**

- the documentation describes the performance and abstraction tradeoffs between typed MAVSDK plugins and raw MAVLink fallback
- a coverage matrix exists mapping MAVSDK plugins (e.g., Telemetry, Action, Mission) to CS API types (Observation, ControlStream, SystemEvent)
- the coverage matrix includes rationales for any 'Deferred' MAVSDK plugins

## Core MAVSDK Driver implementation

*Requirement `requirement.7cb2e6e5ae4b.3` — 4 scenario(s)*

### the Operator requests the telemetry DataStream for the connected vehicle via the OGC Connected Systems API

*ID: `scenario.7cb2e6e5ae4b.3.1`*

**Given** a MAVLink Vehicle emitting HEARTBEAT and GLOBAL_POSITION_INT messages and a configured OpenSensorHub instance with the MAVSDK driver enabled

**When** the Operator requests the telemetry DataStream for the connected vehicle via the OGC Connected Systems API

**Then:**

- the OGC Connected Systems API returns a status of 200 for the telemetry DataStream request
- the response body contains a latitude value matching the MAVLink Vehicle global position data
- the response body contains a longitude value matching the MAVLink Vehicle global position data

### the Operator sends an 'Arm' command to the vehicle's ControlStream via the OGC Connected Systems API

*ID: `scenario.7cb2e6e5ae4b.3.2`*

**Given** an Operator authenticated with the OpenSensorHub instance and a MAVLink Vehicle in a standby state connected via the MAVSDK driver

**When** the Operator sends an 'Arm' command to the vehicle's ControlStream via the OGC Connected Systems API

**Then:**

- the OGC Connected Systems API returns a status code of 202 Accepted
- the MAVLink Vehicle receives a MAV_CMD_COMPONENT_ARM_DISARM command via the MAVSDK bridge
- the vehicle state changes to Armed in subsequent telemetry updates

### the Operator subscribes to the 'raw-mavlink' fallback DataStream via the OGC Connected Systems API

*ID: `scenario.7cb2e6e5ae4b.3.3`*

**Given** a MAVLink Vehicle sending custom MAVLink messages not natively supported by MAVSDK plugins

**When** the Operator subscribes to the 'raw-mavlink' fallback DataStream via the OGC Connected Systems API

**Then:**

- the OGC Connected Systems API returns a 200 status for the raw stream request
- the response body contains the HEX-encoded raw MAVLink packet data

### the Operator attempts to fetch telemetry from the OGC Connected Systems API

*ID: `scenario.7cb2e6e5ae4b.3.4`*

**Given** a MAVLink Vehicle that has lost its connection to the MAVSDK server proxy

**When** the Operator attempts to fetch telemetry from the OGC Connected Systems API

**Then:**

- the OGC Connected Systems API returns a status of 503 Service Unavailable or marks the stream as inactive
- the response body contains an error message indicating 'Connection to MAVLink Vehicle lost'

