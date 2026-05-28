# mavlink-heartbeat-go

E2E test fixture for verifying the PR #18 catalog-backed harness-profile
selection path under real LLM. The agent is expected to:

1. Pick `mavlink.raw-mavlink-direct` from the harness catalog (compatibility
   tier — does not hard-gate evidence anchors, so this exercises the
   architect's selection logic without the full required-tier wiring).
2. Add `github.com/bluenviron/gomavlib/v3` (or equivalent) as a `runtime_dep`
   upstream resolution.
3. Implement a Go service that listens for MAVLink v2 HEARTBEAT frames on a
   UDP port and exposes the most-recent heartbeat at an HTTP endpoint as JSON.
4. Author unit tests against captured HEARTBEAT frames in `testdata/`,
   satisfying the profile's required assertions (decode HEARTBEAT; round-trip
   at least one command frame is not required for this scope).

Initial state is a skeleton `main.go` and an empty `go.mod`. Reset by
`task reset-fixtures` returns to this state between runs.

## QA depth

`.semspec/project.json` pins `qa_level: integration` so the qa-runner
actually executes the rendered `.github/workflows/qa.yml` via nektos/act
after implementation converges. Without this the project falls back to
`synthesis` (LLM verdict only, no test execution) and the integration-
test path never runs — defeating the point of a fixture that exists to
verify the catalog-driven harness selection path.

Because the architect picks `mavlink.raw-mavlink-direct` (orchestration
= pure-fixture), the qa.yml emitted by ADR-039 Phase 1c is the standard
language-aware Go scaffold without any `services:` block — captured
HEARTBEAT frames in `testdata/` are authoritative for this scope, and
no sibling container is spawned. A SITL-demanding scope variant would
trigger the architect to pick `mavlink.px4-sitl.mavsdk-smoke` (required
tier, orchestration=services) and Phase 1c would render the px4-sitl
service block automatically.
