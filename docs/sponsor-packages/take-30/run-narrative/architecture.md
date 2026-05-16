# Architecture

*Generated from `architecture/architecture-deliverable.json` — the structured output of the agent's architect role.*

## Technology choices

| Category | Choice | Rationale |
|---|---|---|
| build_tool | Gradle | Existing project framework (workspace/build.gradle) |
| language | Java 17 | Matches OSH core target compatibility (workspace/build.gradle:20) |

## Component boundaries

### driver

Handles Meshtastic protocol translation and implements the OSH Connected Systems API
- **Internal dependencies:** OpenSensorHub Core, Meshtastic Daemon
- **Upstream refs:** OpenSensorHub Core, OpenSensorHub Service Consys

## Data flow

Mesh node -> Meshtastic Daemon -> Driver -> OSH Event Bus

## Architectural decisions

### ARCH-001: OSH Module Extension

**Decision:** Extend AbstractSensorModule for the driver

**Rationale:** Standard OSH driver pattern for integrating external systems (verified in osh-core/sensorhub-core).

### ARCH-002: Integration Testing Strategy

**Decision:** Use testcontainers with meshtasticd for integration testing

**Rationale:** Ensures the driver can parse real Meshtastic TCP streams without mocking the binary protocol.

## Actors

- **Mesh node** (system) — triggers: Meshtastic packet received
- **OSH Client** (system) — triggers: Connected Systems API request

## Integrations

| Name | Direction | Protocol | Contract | Error mode |
|---|---|---|---|---|
| Meshtastic Network | bidirectional | TCP | Meshtastic Protobufs | Reconnect on TCP connection loss |

## Upstream resolutions

Every external library, API, or framework the implementation depends on. The architect role classifies each one's role and (for service-style integrations) declares the test-harness contract.

### OpenSensorHub Core

- **Coordinate:** `org.sensorhub:sensorhub-core:2.0.0`
- **Role:** `runtime_dep`
- **Source ref:** https://raw.githubusercontent.com/opensensorhub/osh-core/master/common.gradle
- **Used by:** driver
- **API surfaces consumed (1):**
  - `org.sensorhub.impl.sensor.AbstractSensorModule` (class) — public abstract class AbstractSensorModule<T extends SensorConfig> extends AbstractModule<T> implements ISensorModule<T>
    *cited from https://github.com/opensensorhub/osh-core/blob/master/sensorhub-core/src/main/java/org/sensorhub/impl/sensor/AbstractSensorModule.java*

### OpenSensorHub Service Consys

- **Coordinate:** `org.sensorhub:sensorhub-service-consys:2.0.0`
- **Role:** `runtime_dep`
- **Source ref:** https://raw.githubusercontent.com/opensensorhub/osh-core/master/common.gradle
- **Used by:** driver
- **API surfaces consumed (1):**
  - `org.sensorhub.impl.service.consys.ConnectedSystemsService` (class) — public class ConnectedSystemsService
    *cited from https://github.com/opensensorhub/osh-core/blob/master/sensorhub-service-consys/src/main/java/org/sensorhub/impl/service/consys/ConnectedSystemsService.java*

### Meshtastic Daemon

- **Coordinate:** `meshtastic/meshtasticd:daily-alpine`
- **Role:** `integration_target`
- **Source ref:** https://hub.docker.com/r/meshtastic/meshtasticd/tags
- **Test harness:** `testcontainers-java` against image `meshtastic/meshtasticd:daily-alpine` on `tcp:4403`
- **Used by:** driver
- **API surfaces consumed (1):**
  - `TCP API` (interface) — TCP Stream Protobuf API
    *cited from https://meshtastic.org/docs/software/developers/protobufs/api/*

## Test surface

### Integration flows

- **meshtastic-tcp-handshake** — components: driver
  - Validates that the driver can connect to a real meshtasticd instance, authenticate, and listen for node info and chat packets.

### End-to-end flows

- Actor **Mesh node** — 3 steps, 1 success criteria
