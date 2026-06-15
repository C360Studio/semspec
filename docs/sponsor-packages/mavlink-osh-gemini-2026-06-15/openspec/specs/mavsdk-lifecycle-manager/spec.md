# Spec: mavsdk-lifecycle-manager

## Overview

Manages the startup, connection, and teardown of the mavsdk_server process alongside the OpenSensorHub module lifecycle.

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

### MAVSDK Lifecycle Manager

The system MUST manage the startup, connection, and teardown of the mavsdk_server process alongside the OpenSensorHub module lifecycle, and connect to a real or simulated MAVLink system.

#### Scenario: Driver initializes with user-defined connection string

`@unit`

**GIVEN** a MavSdkDriver instance and an UnmannedConfig with a specific MAVLink connection string 'udp://127.0.0.1:14540'
**WHEN** the driver's doInit method is called
**THEN** the driver initializes its MavSdkServerHandler with the specified connection string
**AND** the driver transitions to the INITIALIZED state without spawning external processes

#### Scenario: Driver handles mavsdk_server startup failure gracefully

`@unit`

**GIVEN** a MavSdkDriver instance where the mavsdk_server process fails to start within the configured timeout
**WHEN** the driver's doStart method is called
**THEN** a SensorHubException is thrown during driver startup
**AND** the driver state remains STOPPED or transitions to ERROR

#### Scenario: Driver connects to PX4 SITL and receives heartbeats

`@integration`

**GIVEN** the PX4 SITL environment is active at $SITL_ENDPOINT
**WHEN** the MavSdkDriver is started with the SITL endpoint configuration
**THEN** the driver reports a successful connection to the MAVLink system
**AND** a MAVLink HEARTBEAT message is detected through the telemetry stream within 10 seconds

#### Scenario: Telemetry mapper provides valid position data from SITL

`@integration`

**GIVEN** the MavSdkDriver is connected to a running MAVLink system via PX4 SITL
**WHEN** a telemetry request for the platform's position is made through the Connected Systems API
**THEN** the Connected Systems API response includes a valid position data point (lat/lon/alt)

