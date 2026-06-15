# Spec: raw-mavlink-bridge

## Overview

Provides a generic MAVLink fallback exposing raw messages via DataStream/ControlStream, supporting subscription, transmission, and custom XML dialects.

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

### Raw MAVLink Bridge

The system MUST provide a generic MAVLink fallback exposing raw messages via DataStream and ControlStream. It SHALL support subscription, transmission, and custom XML dialects, and document MAVSDK vs native-MAVLink tradeoffs in the README.

#### Scenario: Driver initialization registers telemetry handlers

`@unit`

**GIVEN** a MavSdkDriver instance configured with a mock MAVSDK System and Telemetry plugin
**WHEN** the doInit method is called on the driver
**THEN** the driver initializes without errors
**AND** the telemetry handler is registered with the system lifecycle

#### Scenario: TelemetryMapper correctly maps MAVSDK Position to OSH DataPackage

`@unit`

**GIVEN** a MavSdkDriver instance with a TelemetryMapper that simulates a MAVSDK position update
**WHEN** a MAVSDK position update is processed by the mapper
**THEN** the mapped OSH DataPackage contains the expected latitude and longitude values

#### Scenario: RawMavlinkBridge loads custom MAVLink XML dialects

`@unit`

**GIVEN** a RawMavlinkBridge instance configured with a custom XML dialect path
**WHEN** the bridge is initialized
**THEN** the bridge parser loads the custom dialect definitions successfully

