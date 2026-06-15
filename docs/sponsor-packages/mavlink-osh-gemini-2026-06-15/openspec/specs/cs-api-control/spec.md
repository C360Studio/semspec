# Spec: cs-api-control

## Overview

Exposes MAVSDK control plugins (actions, missions, params, etc.) as CS API ControlStreams, including status and result resources for long-running commands.

## Applies To

- `src/main/java/org/sensorhub/impl/sensor/mavsdk/MavSdkDriver.java`
- `src/main/java/org/sensorhub/impl/sensor/mavsdk/MavSdkServerHandler.java`
- `src/main/java/org/sensorhub/impl/sensor/mavsdk/TelemetryMapper.java`
- `src/main/java/org/sensorhub/impl/sensor/mavsdk/ControlMapper.java`
- `src/main/java/org/sensorhub/impl/sensor/mavsdk/RawMavlinkBridge.java`
- `README.md`
- `MAVSDK_CS_Coverage.md`
- `src/main/java/org/sensorhub/impl/sensor/mavsdk/MavLinkCommNetwork.java`
- `src/main/java/org/sensorhub/impl/sensor/mavsdk/MavLinkNetworkConfig.java`
- `src/main/java/org/sensorhub/impl/sensor/mavsdk/MavLinkNetworkProvider.java`
- `src/main/java/org/sensorhub/impl/sensor/mavsdk/UnmannedActivator.java`
- `src/main/java/org/sensorhub/impl/sensor/mavsdk/UnmannedConfig.java`
- `src/main/java/org/sensorhub/impl/sensor/mavsdk/UnmannedDescriptor.java`
- `src/main/java/org/sensorhub/impl/sensor/mavsdk/UnmannedSystem.java`
- `src/main/java/org/sensorhub/impl/sensor/mavsdk/cs/CSControlHandler.java`
- `src/main/java/org/sensorhub/impl/sensor/mavsdk/cs/CSGenericMavlinkHandler.java`
- `src/main/java/org/sensorhub/impl/sensor/mavsdk/cs/CSTelemetryHandler.java`
- `src/main/java/org/sensorhub/impl/sensor/mavsdk/util/MavSdkServerHandler.java`
- `src/main/java/org/sensorhub/impl/sensor/mavsdk/util/PlatformId.java`
- `src/test/java/org/sensorhub/impl/sensor/mavsdk/MavSdkDriverTest.java`
- `build.gradle`

## Requirements

### Connected Systems API Control

The system MUST expose MAVSDK control plugins as CS API ControlStreams. It SHALL expose status and result resources to track long-running commands.

#### Scenario: Driver configuration initializes with default MAVLink settings

`@unit`

**GIVEN** a MavLinkNetworkConfig object with default settings
**WHEN** the configuration is loaded into a MavLinkNetworkProvider
**THEN** the telemetry rate is initialized to 1.0Hz
**AND** the connection string matches the default 'udp://:14540'

#### Scenario: ControlMapper tracks long-running command execution status

`@unit`

**GIVEN** a ControlMapper instance initialized with a MAVSDK System plugin
**WHEN** a TAKE_OFF command is dispatched to the mapper
**THEN** the command is accepted for processing
**AND** the command result status is initialized to IN_PROGRESS

#### Scenario: MAVSDK Driver establishes connection and maps streams in integration environment

`@integration`

**GIVEN** the SITL endpoint at env $SITL_ENDPOINT and a running mavsdk_server process
**WHEN** the MAVSDK driver is started with the SITL connection string
**THEN** the MAVSDK driver reports a 'connected' state to the SensorHub event bus
**AND** telemetry data streams are visible in the Connected Systems API catalog

#### Scenario: Command execution and result tracking via Connected Systems ControlStream

`@integration`

**GIVEN** a running MAVSDK Driver connected to a SITL instance via the integration harness
**WHEN** a POST request is sent to the Arm and Takeoff ControlStream endpoint
**THEN** the system receives a MAVSDK result status of SUCCESS within 30 seconds
**AND** the vehicle altitude in telemetry increases beyond 2 meters

