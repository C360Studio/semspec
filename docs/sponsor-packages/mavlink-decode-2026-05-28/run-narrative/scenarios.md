# Scenarios (mavlink-decode 2026-05-28)

Rendered from the `scenario-generator` role's submission. Three BDD
scenarios were emitted for REQ-1 after one round of scenario-review
revision (the first emission was ambiguous about state ownership; the
revised set explicitly anchors each scenario on observable HTTP
behavior).

---

## SCEN-1: Heartbeat endpoint returns 404 when no heartbeat has been received

- **Given:** the service is running and no MAVLink heartbeats have been received yet
- **When:** a GET request is sent to `/heartbeat`
- **Then:**
  - the response status code is 404
  - the response body contains a "no heartbeat received" message or equivalent empty state

---

## SCEN-2: Heartbeat endpoint returns the latest received heartbeat as JSON

- **Given:** the service is running and has received a valid MAVLink v2 HEARTBEAT from a UAV on UDP port 14540 with `system_id` 1 and `component_id` 1
- **When:** a GET request is sent to `/heartbeat`
- **Then:**
  - the response status code is 200
  - the JSON response contains `system_id` 1 and `component_id` 1
  - the JSON response contains `autopilot_type`, `base_mode`, and a `received_at` timestamp

---

## SCEN-3: Heartbeat endpoint returns only the most recent heartbeat data

- **Given:** the service is running and has received multiple heartbeats from different system IDs
- **When:** a GET request is sent to `/heartbeat`
- **Then:**
  - the response status code is 200
  - the JSON response contains the fields for the most recently received heartbeat frame only
