# Spec: async-command-status

## Overview

Track asynchronous, long-running MAVSDK commands and expose their progress via CS API CommandStatus and CommandResult resources.

## Applies To

- `src/main/java/org/sensorhub/driver/mavsdk/MavsdkDriver.java`
- `src/main/java/org/sensorhub/driver/mavsdk/MavsdkServerProcess.java`
- `src/main/java/org/sensorhub/driver/mavsdk/TelemetryMapper.java`
- `src/main/java/org/sensorhub/driver/mavsdk/ControlMapper.java`

## Requirements

### Asynchronous command status and results

The system SHALL track asynchronous, long-running MAVSDK commands instead of treating them as fire-and-forget. The progress and completion states MUST be exposed via CS API CommandStatus and CommandResult resources.

#### Scenario: ControlMapper updates CommandStatus for successful MAVSDK action

`@unit`

**GIVEN** a ControlMapper instance and a mock CommandStatus tracker
**WHEN** a long-running command (e.g., Takeoff) is initiated and the internal MAVSDK observer emits a success event
**THEN** the CommandStatus status transitions to 'ACCEPTED'
**AND** the CommandStatus status eventually transitions to 'COMPLETED' upon receipt of the success callback

#### Scenario: ControlMapper reports failure in CommandStatus when MAVSDK action fails

`@unit`

**GIVEN** a ControlMapper instance and a mock CommandStatus tracker
**WHEN** a command is initiated and the internal MAVSDK observer emits a 'Command Denied' error event
**THEN** the CommandStatus status transitions to 'FAILED'
**AND** the CommandResult contains the error message 'Action denied by vehicle'

#### Scenario: Asynchronous takeoff command emits progress and completion to OGC CS API

`@integration` · harness: `mavlink.px4-sitl.mavsdk-smoke`

**GIVEN** the SITL endpoint at env $SITL_ENDPOINT, and the MAVSDK driver active and connected to the drone
**WHEN** an OGC CS Client sends a ControlStream request for 'Takeoff' to the driver endpoint
**THEN** the CS API returns a CommandStatus resource for the 'Takeoff' request with status 'IN_PROGRESS'
**AND** the CommandStatus status transitions to 'COMPLETED' after the drone reaches takeoff altitude

