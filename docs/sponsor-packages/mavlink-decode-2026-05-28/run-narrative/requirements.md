# Requirements (mavlink-decode 2026-05-28)

Rendered from the `requirement-generator` role's submission. One
requirement was emitted for this scope (the prompt was bounded enough
that the generator chose not to partition further).

---

## REQ-1: MAVLink UDP listener and HTTP heartbeat API

The system must use `github.com/bluenviron/gomavlib` to listen for
MAVLink v2 HEARTBEAT frames over UDP on port 14540. It must expose an
HTTP endpoint at `GET /heartbeat` that returns the most recently
received heartbeat as JSON, containing the fields `system_id`,
`component_id`, `autopilot_type`, `base_mode`, and `received_at`.
Unit tests must verify frame decoding using mock captured data from
`testdata/`.

**Files owned:**

- `go.mod`
- `main.go`
- `main_test.go`
- `testdata/heartbeat.bin`

**Depends on:** (none)
