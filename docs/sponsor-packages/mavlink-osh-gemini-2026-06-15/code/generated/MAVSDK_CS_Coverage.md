# MAVSDK Connected Systems Integration Coverage

This matrix tracks the coverage of MAVSDK scenarios against the Connected Systems API.

| Scenario ID | Type | Covered By | Description |
|-------------|------|------------|-------------|
| 1.1.3 | Integration | `MavSdkDriverIntegrationTest.testScenario_1_1_3_SitlHeartbeat` | Connect to PX4 SITL and receive HEARTBEAT |
| 1.1.4 | Integration | `MavSdkDriverIntegrationTest.testScenario_1_1_4_SitlPosition` | Receive platform position via CS API |
| 2.1.3 | Integration | `MavSdkDriverIntegrationTest.testScenario_2_1_3` | Receive health telemetry from SITL |
| 2.1.4 | Integration | `MavSdkDriverIntegrationTest.testScenario_2_1_4` | Forward control command to SITL |
| 3.1.3 | Integration | `MavSdkDriverIntegrationTest.testScenario_3_1_3` | Receive generic telemetry from SITL |
| 3.1.4 | Integration | `MavSdkDriverIntegrationTest.testScenario_3_1_4` | Forward generic MAVLink control to SITL |

All scenarios use `@Tag("integration")` to bind to the test catalog run.

