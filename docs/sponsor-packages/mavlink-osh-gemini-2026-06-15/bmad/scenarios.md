# Scenarios: dd236a6cb88b

*Generated from the scenario-generator role's output. **15 scenarios** verify the implementation, grouped by the requirement they cover.*

## MAVSDK Lifecycle Manager

*Requirement `requirement.dd236a6cb88b.1` — 4 scenario(s)*

### the driver's doInit method is called

*ID: `scenario.dd236a6cb88b.1.1.1`*

**Given** a MavSdkDriver instance and an UnmannedConfig with a specific MAVLink connection string 'udp://127.0.0.1:14540'

**When** the driver's doInit method is called

**Then:**

- the driver initializes its MavSdkServerHandler with the specified connection string
- the driver transitions to the INITIALIZED state without spawning external processes

### the driver's doStart method is called

*ID: `scenario.dd236a6cb88b.1.1.2`*

**Given** a MavSdkDriver instance where the mavsdk_server process fails to start within the configured timeout

**When** the driver's doStart method is called

**Then:**

- a SensorHubException is thrown during driver startup
- the driver state remains STOPPED or transitions to ERROR

### the MavSdkDriver is started with the SITL endpoint configuration

*ID: `scenario.dd236a6cb88b.1.1.3`*

**Given** the PX4 SITL environment is active at $SITL_ENDPOINT

**When** the MavSdkDriver is started with the SITL endpoint configuration

**Then:**

- the driver reports a successful connection to the MAVLink system
- a MAVLink HEARTBEAT message is detected through the telemetry stream within 10 seconds

### a telemetry request for the platform's position is made through the Connected Systems API

*ID: `scenario.dd236a6cb88b.1.1.4`*

**Given** the MavSdkDriver is connected to a running MAVLink system via PX4 SITL

**When** a telemetry request for the platform's position is made through the Connected Systems API

**Then:**

- the Connected Systems API response includes a valid position data point (lat/lon/alt)

## Connected Systems API Telemetry

*Requirement `requirement.dd236a6cb88b.2` — 4 scenario(s)*

### the mapPosition method is called

*ID: `scenario.dd236a6cb88b.2.1.1`*

**Given** a TelemetryMapper instance and a MAVSDK Position object with latitude 45.0, longitude -75.0, and altitude 100.0

**When** the mapPosition method is called

**Then:**

- the mapper returns a CS API Observation object
- the observation location matches the MAVSDK position values

### the driver doInit method is invoked

*ID: `scenario.dd236a6cb88b.2.1.2`*

**Given** a MavSdkDriverConfig with an invalid connection URL 'invalid://protocol'

**When** the driver doInit method is invoked

**Then:**

- the doInit method throws a SensorHubException
- the exception message contains 'Invalid connection URL'

### the MAVSDK driver is started with the SITL connection string

*ID: `scenario.dd236a6cb88b.2.1.3`*

**Given** the SITL endpoint at env $SITL_ENDPOINT and the mavsdk_server process provided by the harness

**When** the MAVSDK driver is started with the SITL connection string

**Then:**

- the driver transitions to the STARTED state
- a HEARTBEAT message is observed via the MAVSDK system connection within 15 seconds

### a POST request with a Takeoff command is sent to the driver's ControlStream endpoint

*ID: `scenario.dd236a6cb88b.2.1.4`*

**Given** a running MAVSDK driver connected to SITL at env $SITL_ENDPOINT

**When** a POST request with a Takeoff command is sent to the driver's ControlStream endpoint

**Then:**

- the response status code is 200 or 202
- the MAVSDK plugin receives the 'Takeoff' command within 5 seconds

## Connected Systems API Control

*Requirement `requirement.dd236a6cb88b.3` — 4 scenario(s)*

### the configuration is loaded into a MavLinkNetworkProvider

*ID: `scenario.dd236a6cb88b.3.1.1`*

**Given** a MavLinkNetworkConfig object with default settings

**When** the configuration is loaded into a MavLinkNetworkProvider

**Then:**

- the telemetry rate is initialized to 1.0Hz
- the connection string matches the default 'udp://:14540'

### a TAKE_OFF command is dispatched to the mapper

*ID: `scenario.dd236a6cb88b.3.1.2`*

**Given** a ControlMapper instance initialized with a MAVSDK System plugin

**When** a TAKE_OFF command is dispatched to the mapper

**Then:**

- the command is accepted for processing
- the command result status is initialized to IN_PROGRESS

### the MAVSDK driver is started with the SITL connection string

*ID: `scenario.dd236a6cb88b.3.1.3`*

**Given** the SITL endpoint at env $SITL_ENDPOINT and a running mavsdk_server process

**When** the MAVSDK driver is started with the SITL connection string

**Then:**

- the MAVSDK driver reports a 'connected' state to the SensorHub event bus
- telemetry data streams are visible in the Connected Systems API catalog

### a POST request is sent to the Arm and Takeoff ControlStream endpoint

*ID: `scenario.dd236a6cb88b.3.1.4`*

**Given** a running MAVSDK Driver connected to a SITL instance via the integration harness

**When** a POST request is sent to the Arm and Takeoff ControlStream endpoint

**Then:**

- the system receives a MAVSDK result status of SUCCESS within 30 seconds
- the vehicle altitude in telemetry increases beyond 2 meters

## Raw MAVLink Bridge

*Requirement `requirement.dd236a6cb88b.4` — 3 scenario(s)*

### the doInit method is called on the driver

*ID: `scenario.dd236a6cb88b.4.1.1`*

**Given** a MavSdkDriver instance configured with a mock MAVSDK System and Telemetry plugin

**When** the doInit method is called on the driver

**Then:**

- the driver initializes without errors
- the telemetry handler is registered with the system lifecycle

### a MAVSDK position update is processed by the mapper

*ID: `scenario.dd236a6cb88b.4.1.2`*

**Given** a MavSdkDriver instance with a TelemetryMapper that simulates a MAVSDK position update

**When** a MAVSDK position update is processed by the mapper

**Then:**

- the mapped OSH DataPackage contains the expected latitude and longitude values

### the bridge is initialized

*ID: `scenario.dd236a6cb88b.4.1.3`*

**Given** a RawMavlinkBridge instance configured with a custom XML dialect path

**When** the bridge is initialized

**Then:**

- the bridge parser loads the custom dialect definitions successfully

