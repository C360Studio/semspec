# Tasks: Starting from the existing OpenSensorHub MAVSDK addon at https://github.com/opensensorhub/osh-addons/tree/master/sensors/robotics/sensorhub-driver-mavsdk, design and implement MAVLink/MAVSDK support for OpenSensorHub through the OGC Connected Systems API.

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

## mavsdk-server-lifecycle

- [ ] MAVSDK server and client lifecycle management (`requirement.3e7e6046bbe8.1`)
  - [ ] the server process manager prepares the startup command
  - [ ] the mavsdk_server process exits with a non-zero code
  - [ ] the MAVSDK driver is initialized and started with the SITL endpoint

## telemetry-datastream

- [ ] Telemetry DataStreams via OGC CS API (`requirement.3e7e6046bbe8.2`)
  - [ ] the mapper is called to transform the position telemetry
  - [ ] an OGC CS Client subscribes to the position DataStream and the vehicle is armed and hovering
  - [ ] an unsupported MAVSDK telemetry event is passed to the mapper

## control-datastream

- [ ] Control ControlStreams via OGC CS API (`requirement.3e7e6046bbe8.3`)
  - [ ] the mapping function is invoked for the Takeoff action
  - [ ] a POST request containing a Takeoff command is sent to the OGC CS ControlStream endpoint
  - [ ] a Mission command is received with an empty waypoint array
  - [ ] a 'Land' command is issued via OGC CS API while the drone is already on the ground

## async-command-status

- [ ] Asynchronous command status and results (`requirement.3e7e6046bbe8.4`)
  - [ ] a long-running command (e.g., Takeoff) is initiated and the internal MAVSDK observer emits a success event
  - [ ] a command is initiated and the internal MAVSDK observer emits a 'Command Denied' error event
  - [ ] an OGC CS Client sends a ControlStream request for 'Takeoff' to the driver endpoint

## raw-mavlink-fallback

- [ ] Raw MAVLink fallback streams (`requirement.3e7e6046bbe8.5`)
  - [ ] the driver prepares the server process command line args
  - [ ] a raw MAVLink message is broadcast by the SITL autopilot on the monitored UDP port
  - [ ] a raw MAVLink packet is sent through the OGC CS ControlStream endpoint
  - [ ] the MavsdkDriver is initialized

