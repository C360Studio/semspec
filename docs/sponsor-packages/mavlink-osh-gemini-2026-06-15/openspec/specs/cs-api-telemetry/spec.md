# Spec: cs-api-telemetry

## Overview

Exposes typed MAVSDK telemetry, status, and event plugins as OGC Connected Systems API DataStreams and Observations.

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

### Connected Systems API Telemetry

The system MUST expose typed MAVSDK telemetry, status, and event plugins as OGC Connected Systems API DataStreams and Observations. A coverage matrix mapping the MAVSDK plugin endpoints to CS API constructs SHALL be documented in the README.

#### Scenario: TelemetryMapper converts MAVSDK Position to CS API Observation

`@unit`

**GIVEN** a TelemetryMapper instance and a MAVSDK Position object with latitude 45.0, longitude -75.0, and altitude 100.0
**WHEN** the mapPosition method is called
**THEN** the mapper returns a CS API Observation object
**AND** the observation location matches the MAVSDK position values

#### Scenario: Driver initialization fails with invalid connection URL

`@unit`

**GIVEN** a MavSdkDriverConfig with an invalid connection URL 'invalid://protocol'
**WHEN** the driver doInit method is invoked
**THEN** the doInit method throws a SensorHubException
**AND** the exception message contains 'Invalid connection URL'

#### Scenario: Driver connects to PX4 SITL and receives HEARTBEAT

`@integration`

**GIVEN** the SITL endpoint at env $SITL_ENDPOINT and the mavsdk_server process provided by the harness
**WHEN** the MAVSDK driver is started with the SITL connection string
**THEN** the driver transitions to the STARTED state
**AND** a HEARTBEAT message is observed via the MAVSDK system connection within 15 seconds

#### Scenario: Driver executes Takeoff command via CS API ControlStream

`@integration`

**GIVEN** a running MAVSDK driver connected to SITL at env $SITL_ENDPOINT
**WHEN** a POST request with a Takeoff command is sent to the driver's ControlStream endpoint
**THEN** the response status code is 200 or 202
**AND** the MAVSDK plugin receives the 'Takeoff' command within 5 seconds

