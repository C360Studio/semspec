# Design: dd236a6cb88b

## Technology Choices

| Category | Choice | Rationale |
|---|---|---|
| build_tool | Gradle | Existing workspace manifest (workspace/build.gradle) |

## Data Flow

MAVLink mesh node -> MAVSDK server -> MAVSDK Java -> mavsdk-driver -> OpenSensorHub CS API

## Components

### mavsdk-driver

**Responsibility**: Handles OpenSensorHub module lifecycle, telemetry bridging, control commands, and generic MAVLink fallback

**Dependencies**: OpenSensorHub Core, MAVSDK Java

**Upstream refs**: OpenSensorHub Core, MAVSDK Java, PX4 SITL

## Decisions

### ARCH-001: Extend OSH AbstractSensorModule

**Decision**: Driver subclasses AbstractSensorModule

**Rationale**: Standard OpenSensorHub driver pattern for system modules (see /sources/github-com-opensensorhub-osh-core/sensorhub-core/src/main/java/org/sensorhub/impl/sensor/AbstractSensorModule.java)

## Test Harness Profiles

- `mavlink.px4-sitl.mavsdk-smoke` — used by mavsdk-driver: prove MAVSDK control and telemetry paths against a real PX4 SITL MAVLink endpoint

## Integration Points

| Name | Direction | Protocol | Contract | Error Mode |
|---|---|---|---|---|
| PX4 SITL | bidirectional | MAVLink | https://docs.px4.io/main/en/simulation/ | Reconnect on timeout |

