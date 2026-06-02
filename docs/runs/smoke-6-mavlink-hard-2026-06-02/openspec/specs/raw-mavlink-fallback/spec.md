# Spec: raw-mavlink-fallback

## Overview

Provide a generic MAVLink fallback mechanism for raw message pub/sub and custom dialects via CS API streams, bypassing typed MAVSDK plugins when necessary.

## Applies To

- `src/main/java/org/sensorhub/driver/mavsdk/MavsdkDriver.java`
- `src/main/java/org/sensorhub/driver/mavsdk/MavsdkServerProcess.java`
- `src/main/java/org/sensorhub/driver/mavsdk/TelemetryMapper.java`
- `src/main/java/org/sensorhub/driver/mavsdk/ControlMapper.java`

## Requirements

### Raw MAVLink fallback streams

The system MUST provide a generic MAVLink fallback mechanism for raw message publish/subscribe and custom XML dialect loading through CS API streams. The README MUST document the tradeoffs between this native MAVLink approach and the typed MAVSDK integrations.

#### Scenario: Custom XML dialect loading is passed to the server process

`@unit`

**GIVEN** a MAVSDK driver configuration with a custom XML dialect path provided
**WHEN** the driver prepares the server process command line args
**THEN** the driver initializes the MAVSDK server process with the --custom-xml-path argument matching the provided path
**AND** the dialect loader confirms the custom message types are registered in the internal message dictionary

#### Scenario: Raw MAVLink message subscription via CS API stream against SITL

`@integration` · harness: `mavlink.px4-sitl.mavsdk-smoke`

**GIVEN** the OGC CS API stream configured for raw MAVLink data with a specific message ID filter
**WHEN** a raw MAVLink message is broadcast by the SITL autopilot on the monitored UDP port
**THEN** the CS API stream receives the raw MAVLink packets for the requested message ID within 5 seconds
**AND** the raw payload matches the bitstream expected from the SITL simulated telemetry

#### Scenario: Raw MAVLink message publishing to SITL via CS API ControlStream

`@integration` · harness: `mavlink.px4-sitl.mavsdk-smoke`

**GIVEN** the MAVSDK driver is connected to the SITL endpoint via the test harness environment variables
**WHEN** a raw MAVLink packet is sent through the OGC CS ControlStream endpoint
**THEN** the SITL autopilot receives the raw MAVLink packet and acknowledges receipt if applicable
**AND** the MAVSDK driver logs the successful dispatch of the raw message to the autopilot link

#### Scenario: Graceful fallback when custom dialect path is invalid or missing

`@unit`

**GIVEN** a configuration object with a missing dialect path but raw fallback enabled
**WHEN** the MavsdkDriver is initialized
**THEN** the driver successfully initializes using the standard common.xml dialect
**AND** the driver logs a warning that custom message decoding will be limited to the standard dialect

