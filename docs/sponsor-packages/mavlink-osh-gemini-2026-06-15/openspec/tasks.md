# Tasks: dd236a6cb88b

## mavsdk-lifecycle-manager

- [x] MAVSDK Lifecycle Manager (`requirement.dd236a6cb88b.1`)
  - [ ] the driver's doInit method is called
  - [ ] the driver's doStart method is called
  - [ ] the MavSdkDriver is started with the SITL endpoint configuration
  - [ ] a telemetry request for the platform's position is made through the Connected Systems API

## cs-api-telemetry

- [x] Connected Systems API Telemetry (`requirement.dd236a6cb88b.2`)
  - [ ] the mapPosition method is called
  - [ ] the driver doInit method is invoked
  - [ ] the MAVSDK driver is started with the SITL connection string
  - [ ] a POST request with a Takeoff command is sent to the driver's ControlStream endpoint

## cs-api-control

- [x] Connected Systems API Control (`requirement.dd236a6cb88b.3`)
  - [ ] the configuration is loaded into a MavLinkNetworkProvider
  - [ ] a TAKE_OFF command is dispatched to the mapper
  - [ ] the MAVSDK driver is started with the SITL connection string
  - [ ] a POST request is sent to the Arm and Takeoff ControlStream endpoint

## raw-mavlink-bridge

- [x] Raw MAVLink Bridge (`requirement.dd236a6cb88b.4`)
  - [ ] the doInit method is called on the driver
  - [ ] a MAVSDK position update is processed by the mapper
  - [ ] the bridge is initialized

