# Spec: control-datastream

## Overview

Bridge MAVSDK action, mission, and component control plugins to OGC Connected Systems API ControlStreams.

## Applies To

- `src/main/java/org/sensorhub/driver/mavsdk/MavsdkDriver.java`
- `src/main/java/org/sensorhub/driver/mavsdk/MavsdkServerProcess.java`
- `src/main/java/org/sensorhub/driver/mavsdk/TelemetryMapper.java`
- `src/main/java/org/sensorhub/driver/mavsdk/ControlMapper.java`

## Requirements

### Control ControlStreams via OGC CS API

The system MUST bridge MAVSDK action, mission, and component control plugins to OGC Connected Systems API ControlStreams. Like telemetry, these typed streams MUST be documented in the coverage matrix to demonstrate systematic integration of the semantic API.

#### Scenario: ControlMapper maps Takeoff command to MAVSDK Action plugin

`@unit`

**GIVEN** a ControlMapper initialized with a mock MAVSDK Action plugin and a target CS API ControlStream definition for 'Takeoff'
**WHEN** the mapping function is invoked for the Takeoff action
**THEN** the mapper produces a CS API Command structure with double-typed 'altitude' parameter
**AND** the mapper's execution function invokes the MAVSDK Action 'takeoff' method when the command is received

#### Scenario: Issue Takeoff command via OGC CS ControlStream against SITL

`@integration` · harness: `mavlink.px4-sitl.mavsdk-smoke`

**GIVEN** the PX4 SITL and MAVSDK Server started by the test harness at $MAVSDK_SERVER_ADDRESS
**WHEN** a POST request containing a Takeoff command is sent to the OGC CS ControlStream endpoint
**THEN** the OGC CS API endpoint returns a 202 Accepted response
**AND** the MAVSDK Action plugin reports the drone has transitioned to 'Taking Off' state
**AND** the drone altitude in SITL increases above 1 meter within 30 seconds

#### Scenario: ControlMapper handles invalid mission parameters gracefully

`@unit`

**GIVEN** a ControlMapper configured for MAVSDK Mission uploads with a valid CS API Mission schema
**WHEN** a Mission command is received with an empty waypoint array
**THEN** the mapper rejects the command with a 'Missing Parameter' error for the waypoint list

#### Scenario: Failed ControlStream command returns MAVSDK error details

`@integration` · harness: `mavlink.px4-sitl.mavsdk-smoke`

**GIVEN** the PX4 SITL environment at $MAVSDK_SERVER_ADDRESS with the drone currently in 'Hold' mode
**WHEN** a 'Land' command is issued via OGC CS API while the drone is already on the ground
**THEN** the OGC CS API returns a 500 Internal Server Error or appropriate error JSON
**AND** the error message indicates 'No landable surface' or 'Command denied' from MAVSDK

