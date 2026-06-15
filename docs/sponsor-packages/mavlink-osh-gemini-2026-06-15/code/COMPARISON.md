# Generated vs Gold — comparison setup

Both trees are saved here so the generated driver can be evaluated against the
upstream OpenSensorHub reference. This document frames *what to compare*; the
functional comparison itself is the next step (it was **not** run as part of
this `synthesis`-level QA).

## The two trees

| | `generated/` | `gold-reference/` |
|---|---|---|
| Source | agent output, assembled branch `semspec/plan-dd236a6cb88b` @ `8ddbe90` | upstream `opensensorhub/osh-addons` → `sensors/robotics/sensorhub-driver-mavsdk` (mounted at `/sources/` via `WITH_EPIC`) |
| Java files | 14 (7 impl + 7 test) | 38 |
| Package | `org.sensorhub.impl.sensor.mavsdk` | `org.sensorhub.impl.sensor.mavsdk` |
| Base class | `AbstractSensorModule<UnmannedConfig>` | `AbstractSensorModule<…>` |

## Generated implementation classes (7)

```
MavSdkDriver.java            — module entry (extends AbstractSensorModule)
MavSdkServerHandler.java     — mavsdk_server lifecycle + connection
UnmannedConfig.java          — module config
RawMavlinkBridge.java        — raw MAVLink fallback path
cs/CSTelemetryHandler.java   — MAVSDK telemetry → CS API datastreams
cs/CSControlHandler.java     — CS API controlstreams → MAVSDK commands
cs/CSGenericMavlinkHandler.java — generic MAVLink CS datastream/controlstream
```

Plus 7 matching test classes (unit + `@integration` SITL smoke) and a
`MAVSDK_CS_Coverage.md` matrix mapping 6 integration scenarios to tests.

## The shape difference (the honest headline)

The generated driver is **consolidated**: a handful of capability handlers
(telemetry / control / generic / raw). The upstream gold is **exploded**:
class-per-command and class-per-output (`UnmannedControlTakeoff`,
`UnmannedControlLanding`, …, `UnmannedAttitudeOutput`, `UnmannedVelocityOutput`,
…), which is why it has 38 files. Shared names that appear in both:
`MavSdkServerHandler`, `UnmannedConfig`.

Neither shape is "wrong": the generated driver was scoped to **prove the 6
declared scenarios** (lifecycle, telemetry, control, raw fallback, SITL
heartbeat/position), and ADR-049 deliberately rewards a cohesive mergeable
surface over file-count math. The gold reflects years of accreted per-signal
coverage.

## What the gold comparison should evaluate

1. **Build parity** — does `generated/` compile against `mavsdk-java` +
   `osh-core` (the same deps the gold uses)? (`./gradlew build` with the
   `/sources/` artifacts on the classpath.)
2. **Functional parity on the declared scenarios** — run
   `MavSdkDriverIntegrationTest` against PX4 SITL and confirm HEARTBEAT,
   position, health telemetry, and control-forwarding behave like the gold's
   equivalents.
3. **API surface fidelity** — do the CS-API datastream/controlstream mappings
   match OSH Connected Systems conventions the gold follows?
4. **Coverage honesty** — is `MAVSDK_CS_Coverage.md` an accurate map of what the
   tests actually assert, or aspirational?

Items 1–2 are the bar for calling this functionally gold-comparable; this pack
establishes that the substrate *produced a QA-approved, cleanly-assembled
candidate* — the functional grade against gold is the follow-up.
