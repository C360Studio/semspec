# Architecture — Harness Profile Selection

The architect (gemini-3.1-pro-preview) was given the OSH/MAVSDK prompt
and the 4-profile MAVLink catalog from
`workflow/harnesscatalog/catalog/mavlink.yaml`. After 108s of reasoning,
it produced:

- **2 actors**
- **1 integration target**
- **harness_profiles: [mavlink.px4-sitl.mavsdk-smoke, mavlink.raw-mavlink-direct]**

This is the EXACT pair we expected and predicted in the spec doc:

- **`mavlink.px4-sitl.mavsdk-smoke`** (required tier) — covers
  acceptance criteria #1 ("real mavsdk_server connects to MAVLink
  system") and #5 ("live MAVSDK/SITL smoke test"). The catalog entry
  spawns a `px4io/px4-sitl:latest` service in qa.yml, exposing UDP
  14540 for the MAVLink endpoint.
- **`mavlink.raw-mavlink-direct`** (compatibility tier) — covers
  acceptance #3 ("generic raw MAVLink datastream supports
  subscribe-all, subscribe-by-message-name, send-message, and
  load-custom-XML dialect"). pure-fixture orchestration; no extra
  container needed.

## Cross-run reproducibility

| Run | Architect model | Profiles selected | Match? |
|---|---|---|---|
| 2026-05-28 mavlink-decode (gemini @ Go scope) | gemini-3.1-pro-preview | `[mavlink.raw-mavlink-direct]` | ✓ correct for that scope |
| 2026-05-29 mavlink-osh-hard run #1 (gemini + gpt-5.5 @ OSH/Java scope) | gemini-3.1-pro-preview | `[mavlink.px4-sitl.mavsdk-smoke, mavlink.raw-mavlink-direct]` | ✓ correct for required-tier coverage |
| 2026-05-29 mavlink-osh-hard run #2 (gemini + gpt-5.5 @ OSH/Java scope, retried after config fix) | gemini-3.1-pro-preview | `[mavlink.px4-sitl.mavsdk-smoke, mavlink.raw-mavlink-direct]` | ✓ bit-identical to run #1 |

Three runs, three correctly-scoped selections. PR #18's
catalog-backed selection mechanism is reproducible and adapts to
prompt scope.

## What the architect did NOT pick (and why that's correct)

- `mavlink.ardupilot-sitl.compat` — would have over-promised
  compatibility breadth the prompt didn't request.
- `mavlink.px4-gazebo-peripherals` — heavy/expensive profile only
  appropriate when the prompt names peripheral coverage. The OSH/MAVSDK
  prompt names camera/gimbal as part of the typed-MAVSDK datastreams,
  but doesn't require the gazebo-backed peripheral simulation.

The architect read the catalog's `test_guidance` field on each profile
and respected the "Use this profile for compatibility claims, not as
the default MAVSDK smoke gate" hint.
