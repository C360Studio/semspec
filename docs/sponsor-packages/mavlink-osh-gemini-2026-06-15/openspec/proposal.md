# Proposal: dd236a6cb88b

## Why

Design and implement MAVLink/MAVSDK support for OpenSensorHub through the OGC Connected Systems API, using the upstream sensorhub-driver-mavsdk as a baseline. Produce a coverage matrix, integrate typed datastreams/controlstreams, implement long-running command tracking, and add a generic MAVLink fallback. Includes live MAVSDK/SITL smoke tests.

## What Changes

### New Capabilities

- `mavsdk-lifecycle-manager` — Manages the startup, connection, and teardown of the mavsdk_server process alongside the OpenSensorHub module lifecycle.
- `cs-api-telemetry` — Exposes typed MAVSDK telemetry, status, and event plugins as OGC Connected Systems API DataStreams and Observations.
- `cs-api-control` — Exposes MAVSDK control plugins (actions, missions, params, etc.) as CS API ControlStreams, including status and result resources for long-running commands.
- `raw-mavlink-bridge` — Provides a generic MAVLink fallback exposing raw messages via DataStream/ControlStream, supporting subscription, transmission, and custom XML dialects.

## Capability Dependencies

- `cs-api-telemetry` depends on `mavsdk-lifecycle-manager`
- `cs-api-control` depends on `mavsdk-lifecycle-manager`
- `raw-mavlink-bridge` depends on `mavsdk-lifecycle-manager`

## Open Questions

- Should the generic raw MAVLink bridge utilize MavlinkDirect or another native MAVLink Java library?
- How should the explicit unsupported/deferred MAVSDK plugins be structurally mapped in the coverage matrix?

