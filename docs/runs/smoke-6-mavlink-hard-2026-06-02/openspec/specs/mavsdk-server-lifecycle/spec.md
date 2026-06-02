# Spec: mavsdk-server-lifecycle

## Overview

Manage the embedded mavsdk_server binary process, establish the MAVLink system connection, and maintain the MAVSDK Java client instance lifecycle.

## Applies To

- `src/main/java/org/sensorhub/driver/mavsdk/MavsdkDriver.java`
- `src/main/java/org/sensorhub/driver/mavsdk/MavsdkServerProcess.java`
- `src/main/java/org/sensorhub/driver/mavsdk/TelemetryMapper.java`
- `src/main/java/org/sensorhub/driver/mavsdk/ControlMapper.java`

## Requirements

### MAVSDK server and client lifecycle management

The system MUST manage the embedded mavsdk_server binary process, establishing and maintaining the connection to a MAVLink system. The driver SHALL start the server, connect a MAVSDK Java client instance, and successfully pass a live MAVSDK/SITL smoke test.

#### Scenario: mavsdk_server process command line generation

`@unit`

**GIVEN** a configuration object with a valid local path to a mavsdk_server binary and a valid connection URL
**WHEN** the server process manager prepares the startup command
**THEN** the command line arguments include the specified connection URL
**AND** the process builder is initialized with the correct executable path

#### Scenario: driver attempts reconnection when mavsdk_server process terminates unexpectedly

`@unit`

**GIVEN** a MAVSDK driver instance configured to retry connection on failure
**WHEN** the mavsdk_server process exits with a non-zero code
**THEN** the driver status indicates it is attempting to reconnect
**AND** the process manager attempts to restart the mavsdk_server process

#### Scenario: successful connection to PX4 SITL via mavsdk_server

`@integration` · harness: `mavlink.px4-sitl.mavsdk-smoke`

**GIVEN** the PX4 SITL endpoint at environment variable $SITL_UDP_ENDPOINT
**WHEN** the MAVSDK driver is initialized and started with the SITL endpoint
**THEN** the MAVSDK Java client reports a successful connection to the system
**AND** the driver status transitions to 'CONNECTED' within 30 seconds
**AND** a MAVLink HEARTBEAT message is detected through the MAVSDK connection

