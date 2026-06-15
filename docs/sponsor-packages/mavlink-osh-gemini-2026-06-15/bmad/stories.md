# Stories: dd236a6cb88b

*Generated from the story-preparer role (Sarah). **1 stories** ready the per-Requirement work for the executor pipeline, each carrying its own Tasks checklist and FilesOwned scope.*

## MAVSDK Lifecycle Manager

*Requirement `requirement.dd236a6cb88b.1` — 1 story(ies)*

### MAVSDK Driver and CS API Integration

*`story.dd236a6cb88b.1.1`*

Delivers the complete MAVSDK driver, mapping MAVSDK plugins to the Connected Systems API and providing a raw MAVLink fallback.

**Component:** mavsdk-driver

**Covers requirements:** requirement.dd236a6cb88b.1, requirement.dd236a6cb88b.2, requirement.dd236a6cb88b.3, requirement.dd236a6cb88b.4

**Covers capabilities:** mavsdk-lifecycle-manager, cs-api-telemetry, cs-api-control, raw-mavlink-bridge

**Files owned:**

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

**Tasks:**

- `task.dd236a6cb88b.1.1.1` — Write failing test and implement mavsdk_server lifecycle manager and module startup.
- `task.dd236a6cb88b.1.1.2` — Write tests and implement CS API DataStreams and Observations for telemetry.
- `task.dd236a6cb88b.1.1.3` — Write tests and implement CS API ControlStreams and status tracking for control plugins.
- `task.dd236a6cb88b.1.1.4` — Write tests and implement generic raw MAVLink fallback and custom XML dialect support.
- `task.dd236a6cb88b.1.1.5` — Write SITL integration smoke tests and compile coverage matrix documentation.

## Connected Systems API Telemetry

*Requirement `requirement.dd236a6cb88b.2` — 1 story(ies)*

### MAVSDK Driver and CS API Integration

*`story.dd236a6cb88b.1.1`*

Delivers the complete MAVSDK driver, mapping MAVSDK plugins to the Connected Systems API and providing a raw MAVLink fallback.

**Component:** mavsdk-driver

**Covers requirements:** requirement.dd236a6cb88b.1, requirement.dd236a6cb88b.2, requirement.dd236a6cb88b.3, requirement.dd236a6cb88b.4

**Covers capabilities:** mavsdk-lifecycle-manager, cs-api-telemetry, cs-api-control, raw-mavlink-bridge

**Files owned:**

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

**Tasks:**

- `task.dd236a6cb88b.1.1.1` — Write failing test and implement mavsdk_server lifecycle manager and module startup.
- `task.dd236a6cb88b.1.1.2` — Write tests and implement CS API DataStreams and Observations for telemetry.
- `task.dd236a6cb88b.1.1.3` — Write tests and implement CS API ControlStreams and status tracking for control plugins.
- `task.dd236a6cb88b.1.1.4` — Write tests and implement generic raw MAVLink fallback and custom XML dialect support.
- `task.dd236a6cb88b.1.1.5` — Write SITL integration smoke tests and compile coverage matrix documentation.

## Connected Systems API Control

*Requirement `requirement.dd236a6cb88b.3` — 1 story(ies)*

### MAVSDK Driver and CS API Integration

*`story.dd236a6cb88b.1.1`*

Delivers the complete MAVSDK driver, mapping MAVSDK plugins to the Connected Systems API and providing a raw MAVLink fallback.

**Component:** mavsdk-driver

**Covers requirements:** requirement.dd236a6cb88b.1, requirement.dd236a6cb88b.2, requirement.dd236a6cb88b.3, requirement.dd236a6cb88b.4

**Covers capabilities:** mavsdk-lifecycle-manager, cs-api-telemetry, cs-api-control, raw-mavlink-bridge

**Files owned:**

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

**Tasks:**

- `task.dd236a6cb88b.1.1.1` — Write failing test and implement mavsdk_server lifecycle manager and module startup.
- `task.dd236a6cb88b.1.1.2` — Write tests and implement CS API DataStreams and Observations for telemetry.
- `task.dd236a6cb88b.1.1.3` — Write tests and implement CS API ControlStreams and status tracking for control plugins.
- `task.dd236a6cb88b.1.1.4` — Write tests and implement generic raw MAVLink fallback and custom XML dialect support.
- `task.dd236a6cb88b.1.1.5` — Write SITL integration smoke tests and compile coverage matrix documentation.

## Raw MAVLink Bridge

*Requirement `requirement.dd236a6cb88b.4` — 1 story(ies)*

### MAVSDK Driver and CS API Integration

*`story.dd236a6cb88b.1.1`*

Delivers the complete MAVSDK driver, mapping MAVSDK plugins to the Connected Systems API and providing a raw MAVLink fallback.

**Component:** mavsdk-driver

**Covers requirements:** requirement.dd236a6cb88b.1, requirement.dd236a6cb88b.2, requirement.dd236a6cb88b.3, requirement.dd236a6cb88b.4

**Covers capabilities:** mavsdk-lifecycle-manager, cs-api-telemetry, cs-api-control, raw-mavlink-bridge

**Files owned:**

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

**Tasks:**

- `task.dd236a6cb88b.1.1.1` — Write failing test and implement mavsdk_server lifecycle manager and module startup.
- `task.dd236a6cb88b.1.1.2` — Write tests and implement CS API DataStreams and Observations for telemetry.
- `task.dd236a6cb88b.1.1.3` — Write tests and implement CS API ControlStreams and status tracking for control plugins.
- `task.dd236a6cb88b.1.1.4` — Write tests and implement generic raw MAVLink fallback and custom XML dialect support.
- `task.dd236a6cb88b.1.1.5` — Write SITL integration smoke tests and compile coverage matrix documentation.

