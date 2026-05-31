# OSH MAVSDK Driver

Driver for integrating [MAVLink](https://mavlink.io/) /
[MAVSDK](https://mavsdk.mavlink.io/) autopilot telemetry and control with
[OpenSensorHub](https://opensensorhub.org/) via the OGC Connected Systems
API.

## Build system

Gradle. OSH itself uses Gradle (`build.gradle`, `common.gradle`,
`release.gradle`); a Maven-based driver fixture would force the agent to
translate Gradle conventions to Maven, which doesn't reflect real OSH
driver development. Java 17, matching `osh-core`'s `sourceCompatibility`.

## Baseline (the prompt's starting point)

The upstream reference implementation lives at
`opensensorhub/osh-addons/sensors/robotics/sensorhub-driver-mavsdk`. The
`@mavlink-hard` Playwright spec instructs the agent to treat that addon
as the baseline — preserve its OSH sensor module patterns, MAVSDK Java
integration, mavsdk_server lifecycle, telemetry outputs, and control
inputs unless the architecture explicitly replaces them.

## Reference material

The driver implements OpenSensorHub's Connected Systems API on top of
MAVSDK Java. The agent reads upstream sources directly when the EPIC
overlay is active:

- OSH addons (includes the baseline MAVSDK driver): `/sources/github-com-opensensorhub-osh-addons`
- MAVSDK Java library: `/sources/github-com-mavlink-mavsdk-java`
- OSH core (Gradle multi-module): `/sources/github-com-opensensorhub-osh-core`
- OGC Connected Systems API spec: `/sources/github-com-opengeospatial-ogcapi-connected-systems`

When `/sources/` is not mounted, fall back to `web_search` +
`http_request` against the canonical GitHub raw URLs. Cite specific
values (a version, a class signature, a config snippet) in the
implementation; do not paste large blocks of upstream source.

## Building

```sh
./gradlew dependencies   # resolve declared deps (the structural-validator
                         # gradle-dependencies check)
./gradlew test           # compile + run JUnit 5 tests
```

The `build.gradle` ships with `repositories` and `dependencies` blocks
intentionally left as TODOs — those are the agent's first task,
informed by reading the upstream `sensorhub-driver-mavsdk/build.gradle`
and the OSH + MAVSDK release configurations.

## Harness profiles

The `mavlink.px4-sitl.mavsdk-smoke` profile in
`workflow/harnesscatalog/catalog/mavlink.yaml` provides a live PX4 SITL
+ mavsdk_server qa-runner service. The architect selects it for the
required smoke acceptance test; the raw `mavlink.raw-mavlink-direct`
compatibility profile covers the generic-MAVLink path.

## MAVSDK plugins and raw MAVLink fallback

The driver should prefer typed MAVSDK Java plugins for behavior that
MAVSDK models explicitly. Typed plugins provide stable Java APIs,
normalized units, connection-state semantics, typed result errors, and
plugin-specific health checks that are easier to expose through Connected
Systems API resources. This path is the default for telemetry and
operator commands that are already covered by MAVSDK plugins.

Raw MAVLink remains a compatibility fallback for messages or commands that
MAVSDK does not expose, for dialect-specific extensions, and for diagnostic
coverage where preserving the original MAVLink frame is more important
than a typed abstraction. The fallback keeps the driver from being blocked
by MAVSDK plugin coverage, but it also moves more responsibility into the
driver: message parsing, command acknowledgements, unit normalization,
retry bounds, authorization boundaries, and error mapping must be handled
locally.

Use the following rule of thumb:

- Use a typed MAVSDK plugin when the plugin exposes the required vehicle
  state or command and the CS API mapping can be expressed without losing
  required semantics.
- Use raw MAVLink fallback when MAVSDK has no typed surface for the message
  or command, when a vehicle-specific dialect must be preserved, or when a
  diagnostic stream needs byte-level evidence.
- Mark a mapping as `Deferred` until the driver has a tested runtime path
  and a CS API representation with a documented rationale.

The plugin set below is taken from the MAVSDK Java `io.mavsdk.System`
accessors in the referenced source tree. The table is intentionally
machine-checkable: every row has a plugin, MAVLink evidence, CS API type,
runtime mapping, and rationale. `Runtime` denotes the required driver
contract for the README-focused acceptance scenario. `Deferred` denotes a
candidate mapping that is not part of the current runtime contract and
therefore includes an explicit rationale.

### MAVSDK to Connected Systems API coverage matrix

| MAVSDK plugin | MAVLink evidence handled | CS API type | Runtime mapping | Rationale |
| --- | --- | --- | --- | --- |
| Action | `MAV_CMD_COMPONENT_ARM_DISARM` arming | ControlStream | Runtime | Arm and disarm are operator commands with MAVSDK Action support, so CS API clients use a ControlStream and receive success only after vehicle acknowledgement. |
| Action | `MAV_CMD_NAV_TAKEOFF`, `MAV_CMD_NAV_LAND`, `MAV_CMD_NAV_RETURN_TO_LAUNCH` | ControlStream | Deferred | Takeoff, land, and RTL belong on the action control surface, but exposure is deferred until health gating, altitude validation, authorization, and SITL acceptance coverage are defined. |
| ActionServer | `COMMAND_LONG` action requests and `COMMAND_ACK` responses | SystemEvent | Deferred | The driver is a vehicle client, not an onboard action server, so server-side command hosting is deferred until OSH is required to emulate a MAVLink component. |
| Calibration | `MAV_CMD_PREFLIGHT_CALIBRATION` | ControlStream | Deferred | Calibration mutates safety-critical vehicle state and requires guided operator workflow, progress reporting, and exclusive vehicle ownership before CS API exposure. |
| Camera | `CAMERA_INFORMATION`, `CAMERA_IMAGE_CAPTURED`, `MAV_CMD_IMAGE_START_CAPTURE` | Observation and ControlStream | Deferred | Camera payload support depends on payload discovery, media storage policy, and privacy controls that are outside the current telemetry and arm-command contract. |
| Core | `HEARTBEAT` connection state | SystemEvent | Deferred | Core connection events can enrich system lifecycle reporting, but Telemetry already owns the required DataStream online status mapping for HEARTBEAT. |
| Failure | `MAV_CMD_INJECT_FAILURE` | ControlStream | Deferred | Failure injection is test-only and hazardous on real vehicles, so it requires explicit simulator scoping and authorization before a CS API control is offered. |
| FollowMe | `FOLLOW_TARGET` | ControlStream | Deferred | Follow-me requires a trusted moving target source, privacy review, and continuous update validation before the driver can expose it as an operator control. |
| Ftp | `FILE_TRANSFER_PROTOCOL` | SystemEvent | Deferred | File transfer is operational support rather than telemetry or vehicle control, and needs path allow-lists plus resource lifecycle limits before CS API mapping. |
| Geofence | `FENCE_POINT`, `FENCE_FETCH_POINT`, `MISSION_ITEM_INT` fence items | ControlStream | Deferred | Geofence upload is a multi-item safety transaction and needs validation, rollback, and task semantics before being represented as a CS API control. |
| Gimbal | `MAV_CMD_DO_MOUNT_CONTROL`, `GIMBAL_DEVICE_ATTITUDE_STATUS` | Observation and ControlStream | Deferred | Gimbal support is vehicle- and payload-dependent, so the driver defers it until payload capability discovery and frame conventions are implemented. |
| Info | `AUTOPILOT_VERSION`, `COMPONENT_INFORMATION` | System metadata | Deferred | Vehicle identity and version metadata should decorate the CS API System resource, but it is deferred until metadata refresh cadence and caching rules are defined. |
| LogFiles | `LOG_REQUEST_LIST`, `LOG_ENTRY`, `LOG_DATA` | SystemEvent | Deferred | Flight-log access needs storage quotas, operator authorization, and asynchronous transfer state that are not part of the current telemetry stream contract. |
| ManualControl | `MANUAL_CONTROL`, `RC_CHANNELS_OVERRIDE` | ControlStream | Deferred | Manual control is latency-sensitive and safety-critical, requiring watchdogs and exclusive pilot authority before CS API exposure. |
| MavlinkDirect | Raw MAVLink frames including dialect-specific messages | Observation, ControlStream, or SystemEvent | Fallback | Direct MAVLink is the native fallback when typed plugins lack coverage; each mapped frame must preserve MAVLink semantics, validate inputs, and bound retries locally. |
| Mission | `MISSION_COUNT`, `MISSION_ITEM_INT`, `MISSION_ACK` | ControlStream | Deferred | Mission upload and execution need multi-message transaction handling and CS API task semantics that are not part of the current telemetry and arming contract. |
| MissionRaw | Raw `MISSION_ITEM_INT` mission protocol | ControlStream | Deferred | Raw mission access preserves MAVLink details but increases validation burden, so it is deferred until mission item schemas and rollback behavior are documented. |
| MissionRawServer | `MISSION_REQUEST_INT`, `MISSION_ITEM_INT`, `MISSION_ACK` server flow | SystemEvent | Deferred | Hosting mission storage makes OSH act as a MAVLink mission server, which is out of scope until component identity and persistence ownership are specified. |
| Mocap | `ATT_POS_MOCAP`, `VISION_POSITION_ESTIMATE`, `ODOMETRY` | ControlStream | Deferred | Motion-capture injection is an external navigation input and needs source trust, timestamp validation, and coordinate-frame policy before exposure. |
| Offboard | `SET_POSITION_TARGET_LOCAL_NED`, `SET_ATTITUDE_TARGET`, `SET_ACTUATOR_CONTROL_TARGET` | ControlStream | Deferred | Offboard setpoints require high-rate streaming, watchdog behavior, and explicit safety ownership before they can be exposed as controls. |
| Param | `PARAM_REQUEST_READ`, `PARAM_SET`, `PARAM_VALUE` | System metadata and ControlStream | Deferred | Parameter reads and writes need schema discovery, type validation, audit history, and write authorization before CS API clients can use them safely. |
| ParamServer | `PARAM_REQUEST_LIST`, `PARAM_SET`, `PARAM_VALUE` server flow | System metadata | Deferred | Parameter server mode would make OSH own MAVLink component parameters, which is deferred until component identity and persistence are defined. |
| ServerUtility | mavsdk_server status and component management messages | SystemEvent | Deferred | Server utility controls the MAVSDK infrastructure rather than vehicle state, so it needs lifecycle ownership rules before CS API exposure. |
| Shell | `SERIAL_CONTROL` shell sessions | SystemEvent | Deferred | Remote shell access exposes privileged vehicle internals and requires strong authorization, auditing, and session lifecycle controls before mapping. |
| Telemetry | `HEARTBEAT` | DataStream status | Runtime | Vehicle liveness and armed state update CS API DataStream online status so clients can observe autopilot availability without parsing raw MAVLink. |
| Telemetry | `GLOBAL_POSITION_INT` GPS coordinates | Observation | Runtime | Latitude, longitude, altitude, and fix-derived telemetry are normalized through typed MAVSDK Telemetry and exposed as CS API Observation samples on DataStreams. |
| Telemetry | `SYS_STATUS`, `BATTERY_STATUS`, `ATTITUDE`, `VFR_HUD`, `GPS_RAW_INT` | Observation | Deferred | Additional telemetry streams map naturally to Observations, but each needs units, sampling cadence, and data-stream schema definitions before entering the runtime contract. |
| TelemetryServer | `HEARTBEAT`, `GLOBAL_POSITION_INT`, `SYS_STATUS` server publication | SystemEvent | Deferred | Telemetry server mode is for publishing OSH-originated vehicle state to MAVLink peers, which is outside the current client-driver responsibility. |
| Transponder | `ADSB_VEHICLE` | Observation | Deferred | ADS-B transponder data is an observation stream, but it needs airspace-specific schema, identity handling, and update-rate policy before exposure. |
| Tune | `PLAY_TUNE`, `PLAY_TUNE_V2` | ControlStream | Deferred | Tune playback is a nonessential actuator command and is deferred until operator permissions and vehicle capability discovery are available. |
| Raw MAVLink fallback | Unmodeled or dialect-specific MAVLink frames | Observation, ControlStream, or SystemEvent | Fallback | Frames without typed MAVSDK coverage are decoded locally and mapped only when the driver can preserve semantics, validate inputs, and bound retries. |
