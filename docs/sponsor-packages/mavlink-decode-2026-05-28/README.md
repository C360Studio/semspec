# Semspec — mavlink-decode 2026-05-28

**Real-LLM verification of catalog-backed harness profile selection
(PR #18) on a Go + MAVLink scenario.**

Run timestamp: 2026-05-28T13:30 → 13:51 UTC. Wallclock: **20.4 minutes.**
Outcome: **8/8 Playwright assertions passed.** Verdict provider: Gemini
(all roles), `qa.level=synthesis`.

## What this run verifies

PR #18 (`feat(workflow): add catalog-backed harness profiles`, merged
2026-05-28) added a required `architecture.harness_profiles[]` field
where the architect selects from a system-owned catalog instead of
emitting freeform `test_harness` prose. The catalog currently ships
4 MAVLink profiles (raw-mavlink-direct, px4-sitl.mavsdk-smoke,
ardupilot-sitl.compat, px4-gazebo-peripherals).

PR #18 verification had two paths:

1. **Empty-array path** — architect emits `harness_profiles: []` for
   scenarios that don't need a profile (e.g., hello-world `/health`).
   Verified earlier on 2026-05-28 with `task e2e:watch:llm -- gemini
   easy` (7.4 min, 8/8 pass; see prior memory entry).
2. **Populated-array path** — architect selects a real catalog profile
   for a scenario that genuinely needs one. **This run verifies that
   path.**

A new fixture (`test/e2e/fixtures/mavlink-heartbeat-go`) and Playwright
spec (`ui/e2e/plan-lifecycle-llm-mavlink.spec.ts`) ship in PR #20
alongside the workspace-snapshot fix that made this sponsor pack's
`code/` section possible. (Pre-PR-20, the sandbox container was torn
down by the test framework's `afterAll` hook, taking the agent's
generated source with it. The code below was reconstructed from
trajectory heredoc tool args — a fragile process PR #20 makes
unnecessary.)

## The scenario (the "@mavlink-decode" prompt)

Goal handed to the agent, verbatim:

> Add a Go HTTP service that listens for MAVLink v2 HEARTBEAT frames
> over UDP on port 14540 and exposes the most recent heartbeat at
> GET /heartbeat as JSON containing 'system_id', 'component_id',
> 'autopilot_type', 'base_mode', and 'received_at'.
>
> Use a real Go MAVLink library (e.g., github.com/bluenviron/gomavlib)
> for frame parsing — do not hand-roll the MAVLink wire format.
>
> Include unit tests that decode captured MAVLink HEARTBEAT frames from
> testdata/ files and assert the parsed fields.

The prompt is bounded enough to fit inside an `easy`-class budget but
involves three things hello-world doesn't:

1. **A specific third-party library** (`gomavlib`) the agent must
   discover the API of at run-time — its prompt has no scaffolding
   describing `Node{}`, `Endpoints`, `EventFrame`, etc.
2. **A wire-level protocol** (MAVLink v2 over UDP) the agent must
   produce both decoding code *and* test fixtures for.
3. **An integration target shape** — the UAV peer sending HEARTBEAT is
   a separate process. This is exactly the kind of structural
   classification PR #18 introduced (`role: integration_target`
   requires a covering `harness_profiles[]` selection).

## What's significant about this run

### The architect picked the right profile

```json
{
  "profile_id": "mavlink.raw-mavlink-direct",
  "used_by": ["MAVLink Listener"],
  "purpose": "Proves the service can decode HEARTBEAT from raw MAVLink frames.",
  "covers": ["Raw MAVLink Endpoint"]
}
```

The architect was shown ALL 4 catalog profiles in its prompt. It chose
**`mavlink.raw-mavlink-direct`** (`tier: compatibility`) and did NOT
pick the more aggressive `mavlink.px4-sitl.mavsdk-smoke` (`tier:
required`) — which would have hard-gated the requirement on PX4 SITL
evidence anchors. That's the right call for unit-level scope.

The architect ALSO correctly distinguished `runtime_dep` from
`integration_target` in upstream resolutions:

- `gomavlib` — `runtime_dep`, in-process Go library
- `Raw MAVLink Endpoint` — `integration_target`, the UAV peer

This is exactly the structural classification PR #18 enables.

### The developer wrote real code that used the discovered API

`code/main.go` uses `gomavlib.NodeConf` with `EndpointUDPServer`, the
`minimal.Dialect`, `OutVersion: V2`, a goroutine consuming
`node.Events()`, and a type assertion to `*minimal.MessageHeartbeat`
in the handler loop. These are the actual API shapes the architect
documented in `upstream_resolutions` (with citation URLs to
`pkg.go.dev`).

`code/main_test.go` includes the comment
`// Evidence anchors: mavlink.raw-mavlink-direct, HEARTBEAT` — the
developer explicitly threaded the profile's evidence anchors into the
test source as a comment. This wasn't enforced by the structural
validator (compatibility tier doesn't hard-gate) — the developer chose
to do it.

### The tests use real UDP I/O, not in-memory mocks

`code/main_test.go` opens a real UDP server (`net.Dial("udp",
"127.0.0.1:14541")`), writes captured `testdata/heartbeat1.bin` and
`heartbeat2.bin` bytes over the wire, waits 100ms for the listener
goroutine to process, then asserts the JSON response via
`httptest.NewRecorder()`. This is end-to-end on a real network socket
inside the test process, not a parser-only unit test.

The testdata `.bin` files were produced by an intermediate
`generate.go` helper the developer wrote and then deleted at 13:49:36
after producing the bytes (visible in trajectory bash log). That's a
reasonable workflow — generate canonical frames once, delete the
generator, ship the binary fixtures.

## What's still significant about the architecture deliverable

Excerpt from `architecture/architecture-deliverable.json`:

```json
"upstream_resolutions": [
  {
    "name": "gomavlib",
    "coordinate": "github.com/bluenviron/gomavlib/v3",
    "role": "runtime_dep",
    "source_ref": "https://pkg.go.dev/github.com/bluenviron/gomavlib/v3",
    "apis": [
      {"symbol": "gomavlib.Node.Initialize",
       "signature": "func (n *Node) Initialize() error",
       "lifecycle": "Node{} -> Initialize() -> Events() -> Close()",
       "citation": "https://pkg.go.dev/.../#Node.Initialize"},
      ... 3 more
    ]
  },
  {
    "name": "Raw MAVLink Endpoint",
    "coordinate": "mavlink:raw-mavlink-direct",
    "role": "integration_target",
    "source_ref": "https://mavlink.io/en/messages/common.html#HEARTBEAT"
  }
]
```

This is the ADR-035 upstream-resolutions pattern (the "find the API
once at architecture time so the developer doesn't have to re-discover
mid-cycle") working as intended. The developer trajectory shows the
agent referenced these citation URLs without needing to re-curl
`pkg.go.dev`.

## ⚠️ QA limitations — the honest story about test gates

The user-facing question that should land in any sponsor briefing
on this run: **was the agent's code actually tested?** Mostly yes, but
with sharp limits worth surfacing.

### What ran during the run

| Gate | What ran | Where in pipeline | Tests exercised |
|---|---|---|---|
| `structural-validator` `go-build` | `go build ./...` (120s) | Per scenario merge | Compilation only |
| `structural-validator` `go-vet` | `go vet ./...` (60s) | Per scenario merge | Static analysis only |
| `structural-validator` `go-test` | `go test ./...` (300s) | Per scenario merge | **`main_test.go` did run** — real `go test` against the agent's code |
| `qa-reviewer` (Murat persona) | LLM verdict, no execution | At `reviewing_qa` stage | **None** (`qa.level=synthesis` default) |

So the agent's `main_test.go` (which sends real UDP HEARTBEATs over
localhost and asserts the JSON response) DID execute, multiple times,
across the 3 TDD cycles. That's why we have confidence the code
compiles and the unit-level decoder works.

### What did NOT run

| Test class | Why it didn't run | Evidence in the code |
|---|---|---|
| `main_integration_test.go` | Has `//go:build integration` tag; `go test ./...` doesn't include it | See `code/main_integration_test.go` |
| `main_e2e_test.go` | Has `//go:build integration \|\| e2e` tag; same | See `code/main_e2e_test.go` |
| Real PX4 SITL exercise | Compatibility-tier profile selection; no SITL container booted | Catalog profile `mavlink.raw-mavlink-direct` does not require SITL |
| Multi-MAVLink-dialect compatibility | Not in scope | — |

### Where SITL would have belonged (and why it didn't run)

The catalog has a `tier: required` profile precisely designed for SITL:
`mavlink.px4-sitl.mavsdk-smoke` (see
`workflow/harnesscatalog/catalog/mavlink.yaml`). Its required assertions:

- Observe a MAVLink heartbeat from the SITL target
- Assert MAVSDK reports a connected vehicle before plugin calls run
- Exercise at least one control plugin and one telemetry/data-stream plugin path

Its `runner_support` explicitly includes `github-actions-via-testcontainers`
and `act` — the wiring expects to run SITL inside the qa-runner via
Testcontainers when `qa.level >= integration`.

**The architect didn't pick it because:**

1. The prompt asked for HEARTBEAT decode only — no command/control,
   no telemetry-plugin assertions
2. Selecting required-tier would have committed the structural-validator
   to evidence-anchor enforcement (every modified test file must reference
   `mavlink.px4-sitl.mavsdk-smoke`, `px4io/px4-sitl`, `14540`,
   `mavsdk_core_connected`, `HEARTBEAT`)
3. The fixture sandbox doesn't have PX4 SITL on the workspace image; the
   required-tier profile expects the test process itself to start SITL via
   Testcontainers

That last point is the rub. The compatibility-tier choice was correct
for THIS scope — but the question of whether we can ACTUALLY exercise
required-tier under realistic CI conditions is **unverified**.

### What we'd want next (and what we're concerned about)

For a real production MAVLink driver, the qa-reviewer at
`qa.level=full` would be the natural slot for SITL execution. The
qa-runner (per ADR-031) runs `.github/workflows/qa.yml` via `nektos/act`
inside its own container. To run PX4 SITL on top of that:

- **Option A: Testcontainers-Go inside qa-runner.** The qa-runner mounts
  the host Docker socket so Testcontainers can spawn the `px4io/px4-sitl`
  container as a sibling. Profile metadata already enumerates this:
  `runner_support: [local-docker, github-actions-via-testcontainers, act]`.
  Cost: ~500MB PX4 image pull on first cold cache (mitigatable with
  pre-pull); ~30s SITL boot + handshake per test run.
- **Option B: Skip SITL in CI, schedule it offline.** A separate
  "deep-qa" tier (not yet implemented) that runs nightly or per-PR-merge,
  not per-TDD-cycle. Adds a second qa pathway that surfaces results back
  through `qa.level=full` verdicts.

**Why we haven't shipped either yet** — both add real complexity:

- Option A requires the qa-runner image to either bundle the SITL image
  or do a controlled pull, and the qa.yml file to be SITL-aware. There's
  no existing fixture (and probably shouldn't be — qa.yml is intended to
  be project-owned, not semspec-owned). Selecting required-tier without
  the qa.yml wiring being in place means the structural-validator
  hard-fails plans that the rest of the pipeline could have shipped.
- Option B is a multi-tier verdict surface change: qa-reviewer would
  need to render a tentative verdict (synthesis or unit pass) while
  recording that a deep-qa run is pending. The plan-state machine
  doesn't have a "pending external verdict" stage. Adding it is a
  meaningful design change to PLAN_STATES, not a small wire.

**Where this leaves things:** the catalog metadata is correct and the
architect's selection logic respects tier semantics. The wiring from
required-tier profile selection through qa-runner SITL execution is
**designed-for but not built-or-tested**. The next step is either:

1. A scenario specifically targeting `mavlink.px4-sitl.mavsdk-smoke`
   selection, with a fixture that pre-stages the SITL image, to verify
   the required-tier hard-gate doesn't false-fail when SITL is
   available. (Most valuable as proof of design soundness.)
2. The deep-qa tier design (Option B above) as a separate ADR if we
   conclude that per-cycle SITL is structurally too expensive for the
   inner dev loop.

Today, neither is gated on this run. This run's value is that we now
KNOW the architect picks the right tier for a given scope — and that
the system correctly didn't try to run SITL for a HEARTBEAT-decode
scope it didn't need. Selecting compatibility tier was the right
behavior, and the deterministic backstops (structural-validator with
`go test ./...`) caught the unit-level concerns the agent could
plausibly have gotten wrong.

## What's still significant about the run outcome

From `evidence/playwright-result.txt`:

```
✓ 1 plan created with goal                                (8ms)
✓ 2 plan reviewed and approved                            (9.1s)
✓ 3 requirements generated                                (6.0s)
✓ 4 architecture generated with harness profile selection (27.1s)
✓ 5 scenarios generated and reviewed                      (27.0s)
✓ 6 execution triggered                                   (87ms)
✓ 7 execution completes                                   (14.6m)
✓ 8 trajectories exist after execution                    (14ms)
8 passed (20.4m)
```

Stage transitions:

```
(start) → implementing       at t+0s
implementing → reviewing_qa  at t+866s   (TDD cycles complete)
reviewing_qa → complete      at t+878s   (qa.level=synthesis verdict)
```

The 14.6-minute "execution completes" step is **3 TDD cycles** of
developer + reviewer running through the implementation. Cycle 1 hit
`go.mod` / `go get` issues fighting the gomavlib import path; cycle 2
found the correct API path; cycle 3 landed the final tests. Each cycle
~5 min. CLAUDE.md notes `max_tdd_cycles=3` — this run lived at the
ceiling, which is normal for a scope this much richer than hello-world.

## What we still don't know

This is one run. Specifically unmeasured:

- **Required-tier hard-gate behavior.** No scenario yet exercises a
  `tier: required` profile selection through to structural-validator
  evidence-anchor enforcement. See QA limitations section above.
- **Reproducibility.** N=1 on this specific config (gemini all roles,
  mavlink-decode tier). Whether the architect consistently picks
  raw-mavlink-direct vs. occasionally hallucinating a different
  profile, we haven't measured.
- **Required-tier discovery friction.** What does the architect do
  when presented with a scenario where ONLY a required-tier profile
  would cover (e.g., a MAVSDK plugin assertion)? Does it pick the
  required tier knowing it commits the structural-validator to
  evidence enforcement? Or does it punt to compatibility tier and let
  the plan fail elsewhere? Untested.
- **`RepeatToolFailure` false positive.** This run raised 1 critical
  alert during the architect's `curl pkg.go.dev | grep <pattern>`
  research iteration. The detector counts 3 consecutive exit-1 bash
  calls as a wedge but doesn't notice each call varied its query. A
  smarter detector would consider tool-argument variance.
- **What the qa-reviewer LLM verdict said.** `qa.level=synthesis` ran
  the Murat persona which produced a verdict. The trajectory ID is
  captured in the bundle; the verdict body itself isn't extracted into
  this pack. If sponsor cares, it's in the bundle.

## What this package contains

```
sponsor-package/
├── README.md                          ← you are here
├── code/                              ← reconstructed final-state files
│   ├── main.go                          (2533 bytes — UDP listener + HTTP server)
│   ├── main_test.go                     (2659 bytes — real UDP I/O via testdata/*.bin)
│   ├── main_integration_test.go         (1760 bytes — `//go:build integration`,
│   │                                     did NOT run, see QA limitations)
│   ├── main_e2e_test.go                 (2485 bytes — `//go:build integration||e2e`,
│   │                                     did NOT run, see QA limitations)
│   └── go.mod                           (skeleton; full version with gomavlib
│                                          dep is in the bundle's go.mod write
│                                          history but the latest heredoc
│                                          extracted to here is the skeleton)
│
├── architecture/
│   └── architecture-deliverable.json  ← agent's full architecture submission
│                                        including upstream_resolutions and
│                                        the load-bearing harness_profiles[]
│
├── operator-rules/                    ← operator-declared compliance rules
│   ├── README.md
│   ├── standards.json                 ← empty rules[] — minimum baseline
│   └── checklist.json                 ← go-build, go-vet, go-test (the only
│                                        gates that actually executed code)
│
├── run-narrative/                     ← BMAD/OpenSpec-style human renders
│   ├── README.md
│   ├── architecture.md                ← rendered from architecture-deliverable.json
│   ├── requirements.md                ← 1 requirement
│   └── scenarios.md                   ← 3 BDD scenarios (post-revision set)
│
└── evidence/
    ├── playwright-result.txt          ← 8/8 with stage transitions
    ├── watch.log                      ← 60 lines of heartbeats + 1 ALERT
    ├── trajectories-summary.txt       ← per-trajectory step / bash / model counts
    └── timeline.md                    ← annotated minute-by-minute breakdown
```

## How to verify this for yourself

The full agent trajectories are in
`/tmp/semspec-watch-gemini-mavlink-decode-20260528-093035/bundle.tar.gz`
(not included due to size; ~1.1 MB containing 21 trajectories +
bundle.json). Forensic queries against them produced the evidence
quoted in this package.

Once PR #20 lands, future runs will also include
`workspace.tar.gz` — the actual sandbox `/workspace` tree as it stood
the moment before teardown. For this run we reconstructed `code/` from
trajectory heredoc tool args (see "Honest framing" in
`run-narrative/README.md`).

To repro:

```bash
task e2e:watch:llm -- gemini mavlink-decode
```

after PR #20 merges. Cost ~20 min wallclock + Gemini API tokens for the
~40 agent loops.

## Why this matters

This is the first run that verifies the **populated-array path** of
PR #18's catalog-backed harness profiles under a real LLM. The empty-
array path (architect emits `[]` for hello-world scope) was already
green; the open question was whether the architect would correctly
*select* a catalog profile when scope warranted, vs. either:

- inventing a profile ID (gemini did NOT — schema "Do not invent IDs"
  instruction held), or
- picking an over-aggressive required-tier profile out of pattern-match
  to "MAVLink" (gemini did NOT — picked compatibility-tier
  raw-mavlink-direct for HEARTBEAT-only scope), or
- failing to populate `harness_profiles[]` at all (gemini correctly
  populated it with one entry).

All three failure modes were structurally possible; none occurred. The
architect's tier-discrimination was load-bearing for this verification
and the agent demonstrated it.

The QA limitations section above is the honest counterweight: we
verified the **selection logic** end-to-end, but the **required-tier
hard-gate path** through qa-runner SITL execution is still designed-for-
not-built. That's the next thread to pull, and a real engineering
investment, not a one-line wire.

---

*Run details: all-Gemini model assignment (no Claude developer; this
scenario fits inside gemini-flash + gemini-pro capability chains).
Repository: github.com/C360Studio/semspec, branch
`e2e/mavlink-decode-tier-and-workspace-snapshot`, PR #20.*
