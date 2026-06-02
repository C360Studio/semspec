# Spec: telemetry-datastream

## Overview

Expose MAVSDK telemetry, info, and event plugins as OGC Connected Systems API DataStreams and Observations.

## Applies To

- `src/main/java/org/sensorhub/driver/mavsdk/MavsdkDriver.java`
- `src/main/java/org/sensorhub/driver/mavsdk/MavsdkServerProcess.java`
- `src/main/java/org/sensorhub/driver/mavsdk/TelemetryMapper.java`
- `src/main/java/org/sensorhub/driver/mavsdk/ControlMapper.java`

## Requirements

### Telemetry DataStreams via OGC CS API

The system MUST expose MAVSDK telemetry, information, and event plugins as OGC Connected Systems API DataStreams and Observations. A machine-checkable coverage matrix MUST be provided that maps each pinned plugin's streams to CS API paradigms or explicitly justifies its deferral.

#### Scenario: TelemetryMapper converts MAVSDK Position to OGC Observation Point

`@unit`

**GIVEN** a TelemetryMapper instance and a MAVSDK Position object with latitude 47.397, longitude 8.545, and absolute altitude 488.0
**WHEN** the mapper is called to transform the position telemetry
**THEN** the mapper returns an OGC Observation containing a Point geometry with coordinates [8.545, 47.397, 488.0]
**AND** the Observation result includes the correct UOM for altitude as 'm'

#### Scenario: Position DataStream emits observations from PX4 SITL via CS API

`@integration` · harness: `mavlink.px4-sitl.mavsdk-smoke`

**GIVEN** the SITL endpoint at env $SITL_ENDPOINT and the mavsdk-osh-driver configured to connect to it
**WHEN** an OGC CS Client subscribes to the position DataStream and the vehicle is armed and hovering
**THEN** the OGC CS API endpoint /datastreams/position/observations returns a valid observation within 15 seconds
**AND** the observation data matches the current SITL vehicle coordinates

#### Scenario: TelemetryMapper handles unmapped MAVSDK plugin data gracefully

`@unit`

**GIVEN** a TelemetryMapper initialized with an empty mapping configuration
**WHEN** an unsupported MAVSDK telemetry event is passed to the mapper
**THEN** the mapper returns an empty optional or throws a DataMappingException
**AND** no malformed OGC Observation is produced

