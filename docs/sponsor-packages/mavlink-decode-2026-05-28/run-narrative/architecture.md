# Architecture (mavlink-decode 2026-05-28)

Rendered from `../architecture/architecture-deliverable.json`. The
architect role produced this deliverable in **27.1 seconds** of LLM time
after reading the requirement and querying `pkg.go.dev` to discover the
gomavlib API surface.

---

## Technology choices

(The architect emitted no `technology_choices` for this scope. This is
valid output — see the spec assertion comment: tech_choices is OPTIONAL
because trivial scopes don't warrant invented technology detail. The
required surface is actors / integrations / harness_profiles /
upstream_resolutions / test_surface, all of which are populated below.)

## Component boundaries

| Name | Responsibility | Depends on | Upstream refs |
|---|---|---|---|
| **MAVLink Listener** | Listens on UDP port 14540, parses MAVLink v2 frames, and updates shared state with the latest HEARTBEAT. | (none) | `gomavlib`, `Raw MAVLink Endpoint` |
| **HTTP API** | Exposes the latest HEARTBEAT as JSON at `GET /heartbeat`. | MAVLink Listener | (none) |

## Actors

| Name | Type | Triggers |
|---|---|---|
| UAV | system | MAVLink HEARTBEAT over UDP |
| GCS | system | `GET /heartbeat` |

## Integrations

| Name | Direction | Protocol | Contract | Error mode |
|---|---|---|---|---|
| MAVLink Network | inbound | UDP / MAVLink | MAVLink v2 HEARTBEAT | Silent drop on decode error |
| HTTP Heartbeat API | inbound | HTTP | `GET /heartbeat → JSON` | 503 if no heartbeat received yet |

## ★ Catalog-backed harness profile selection (PR #18 verification)

This is the load-bearing field for this run. The architect was shown all
4 MAVLink catalog profiles in its prompt and selected exactly one:

```json
{
  "profile_id": "mavlink.raw-mavlink-direct",
  "used_by": ["MAVLink Listener"],
  "purpose": "Proves the service can decode HEARTBEAT from raw MAVLink frames.",
  "covers": ["Raw MAVLink Endpoint"]
}
```

`mavlink.raw-mavlink-direct` is `tier: compatibility` — visible to the
architect/developer for selection but NOT hard-gated by structural
validation (no required-evidence-anchor enforcement). This was the
deliberate choice for this scenario: exercise the selection wiring
without committing to SITL container infrastructure inside the test
sandbox.

Critically: the architect did NOT pick the more aggressive
`mavlink.px4-sitl.mavsdk-smoke` (`tier: required`) despite seeing it in
the prompt. Selecting that one would have hard-gated the requirement
on Testcontainers + PX4 SITL evidence anchors — too much for this scope.

## Upstream resolutions

### 1. `gomavlib` (role: `runtime_dep`)

Library used in-process for MAVLink parsing. The architect populated
typed API entries with citation URLs:

| Symbol | Kind | Signature | Lifecycle |
|---|---|---|---|
| `gomavlib.Node.Initialize` | method | `func (n *Node) Initialize() error` | `Node{} → Initialize() → Events() → Close()` |
| `gomavlib.EndpointUDPServer` | type | `type EndpointUDPServer struct { Address string }` | — |
| `gomavlib.EventFrame` | interface | `type EventFrame interface { ... }` | — |
| `minimal.MessageHeartbeat` | type | `type MessageHeartbeat struct { Type, Autopilot, BaseMode, CustomMode, SystemStatus, MavlinkVersion }` | — |

Coordinate: `github.com/bluenviron/gomavlib/v3`
Source ref: <https://pkg.go.dev/github.com/bluenviron/gomavlib/v3>

### 2. `Raw MAVLink Endpoint` (role: `integration_target`)

The UAV peer that emits HEARTBEAT frames. Classified as
`integration_target` because it's a separate process speaking a wire
protocol, even though test code substitutes captured frames instead of
starting a real autopilot:

| Symbol | Notes |
|---|---|
| `HEARTBEAT` | Sent at 1Hz over UDP by MAVLink nodes |

Coordinate: `mavlink:raw-mavlink-direct`
Source ref: <https://mavlink.io/en/messages/common.html#HEARTBEAT>

## Test surface

### Integration flows (1)

| Name | Components | Description |
|---|---|---|
| `decode-heartbeat-udp` | MAVLink Listener | Send raw MAVLink HEARTBEAT frames to the UDP listener and verify they are decoded correctly into the shared state. |

### E2E flows (1)

| Actor | Steps | Success criteria |
|---|---|---|
| GCS | 1. Send MAVLink HEARTBEAT to UDP port 14540<br>2. `GET /heartbeat` | Returns 200 OK; JSON contains `system_id`, `component_id`, `autopilot_type`, `base_mode`, and `received_at` matching the sent heartbeat |
