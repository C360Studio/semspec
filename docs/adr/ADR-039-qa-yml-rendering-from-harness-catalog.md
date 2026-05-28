# ADR-039: qa.yml Rendering from Harness Catalog — Synchronous Sibling-Container Services

**Status:** Proposed (2026-05-28)
**Deciders:** Coby, Claude
**Related:** ADR-031 (QA test execution — the gate this ADR extends),
PR #18 (catalog-backed harness profiles — the metadata source this ADR
consumes), PR #20 (mavlink-decode tier — the verification scope that
exposed the gap), `docs/sponsor-packages/mavlink-decode-2026-05-28/`
(QA limitations section that motivated this ADR).

## Context

PR #18 added catalog-backed `architecture.harness_profiles[]` selections
to the architect's deliverable. PR #20's mavlink-decode verification on
2026-05-28 confirmed the architect picks the right profile tier for
scope (compatibility for HEARTBEAT-decode; would have picked required
for SITL-requiring scope). The catalog metadata already enumerates
everything a CI run would need to actually exercise the integration:

```yaml
# workflow/harnesscatalog/catalog/mavlink.yaml (excerpt)
- id: mavlink.px4-sitl.mavsdk-smoke
  tier: required
  runner_support:
    - local-docker
    - github-actions-via-testcontainers
    - act
  images:
    - name: px4io/px4-sitl:latest
      purpose: PX4 software-in-the-loop autopilot target.
  ports:
    - name: mavlink-udp
      container_port: 14540
      protocol: udp
  evidence_anchors:
    - mavlink.px4-sitl.mavsdk-smoke
    - px4io/px4-sitl
    - 14540
    - mavsdk_core_connected
    - HEARTBEAT
```

The gap that PR #20's run exposed: **none of this metadata is
threaded into the qa-runner's qa.yml execution.** The structural-
validator's `go test ./...` ran the agent's unit tests (which decode
captured frames over loopback UDP) but no qa pathway brought up a real
`px4io/px4-sitl` container for tier-required profiles. The pieces all
exist; the wire from catalog → qa.yml is missing.

### Today's qa-reviewer behavior (verified on the run)

| `qa.level` | Test execution | Service containers spawned |
|---|---|---|
| `none` | (skipped) | — |
| `synthesis` (default) | LLM verdict only — no test execution | — |
| `unit` | `go test ./...` (project's own test suite) | — |
| `integration` | Runs `.github/workflows/qa.yml` via nektos/act | **No service block emitted today** |
| `full` | + Playwright e2e | **No service block emitted today** |

`qa-runner` (per ADR-031) is itself a container running `nektos/act`.
For `qa.level=integration` and above to actually bring up a sibling
SITL container, two things have to change:

1. `qa-runner` needs access to the host Docker daemon (so it can spawn
   service containers as siblings on the host network — see "Sibling
   container vs DinD" below).
2. The rendered `qa.yml` needs `services:` blocks corresponding to
   the architect's `harness_profiles[]` selections, populated from the
   catalog entry's `images` / `ports` / `env` / `readiness`.

This ADR commits to building that wiring synchronously — qa-reviewer
blocks while services come up and tests run, the operator sees the
state via existing observability surfaces, and timeouts are tuned to
expected per-service boot costs.

## Decision

**Step 1 (this ADR): render qa.yml services from catalog at the
required- and compatibility-tier paths, run as sibling containers via
the qa-runner's host Docker socket, synchronously gate the
`reviewing_qa` stage on the result.**

Mechanically:

1. **Catalog renderer.** A new internal package
   (`workflow/harnesscatalog/qarender`) converts a list of
   `ResolvedSelection` entries into a `services:` block compatible with
   GitHub Actions / nektos/act. Each profile entry's `images[0]`
   becomes a service; `ports[]` map to GHA `ports:` syntax;
   `env`/`readiness`/`test_guidance` thread into the rendered service
   metadata.

   **Per-profile orchestration (added during Phase 1a implementation,
   2026-05-28).** The original framing put GHA `services:` as the
   primary mechanism with testcontainers as a fallback. Implementation
   surfaced a cleaner shape: the choice is per-profile, declared by a
   first-class `orchestration` field on the catalog `Profile` struct.
   Values are `services` (renderer emits a qa.yml service block from
   `images`/`ports`/`env`/`readiness`), `testcontainers` (renderer
   skips; dev agent owns the integration stack in test code), or
   `pure-fixture` (renderer skips; in-process or captured-frame
   fixtures, no container). When the field is empty the renderer infers
   `services` if `images[]` is non-empty and `pure-fixture` otherwise.
   This makes the orchestration choice visible to the architect at
   profile-selection time rather than hidden in the rendered output.
   Alternative B below (testcontainers) remains the right tool for
   profiles whose stack genuinely varies per test; the field just
   surfaces that choice in metadata rather than treating it as a
   fallback.

2. **qa-runner socket mount.** `docker/compose/qa-runner.yml` (or
   wherever the qa-runner is defined) mounts `/var/run/docker.sock`
   read-write so spawned act jobs can `docker run` sibling service
   containers on the host. Containers are visible as siblings under
   the host's docker daemon, joined to the act job's network for
   reachability.

3. **qa.yml emission.** `project-manager` (or `qa-reviewer` —
   placement TBD during implementation) writes the rendered qa.yml to
   the workspace at the `reviewing_qa` entry, replacing any existing
   one. The agent's project-owned qa.yml is treated as the **template**;
   the renderer injects the services block at the top of the jobs
   stanza. Operator can disable injection per-project via
   `project.qa.skip_service_injection: true` if they prefer to manage
   services manually.

4. **Synchronous block with progress surfacing.**
   `qa-reviewer` waits for qa-runner to complete (success or timeout).
   Progress is surfaced via:
   - `watch.log` heartbeats (`active_loops` includes the qa-reviewer loop)
   - Per-step alerts when `qa.yml` step name contains
     `services: <name>` (e.g., "starting service: sitl", "service
     ready: sitl")
   - The existing `/agentic-loop/trajectories/<loop_id>` API exposes
     the qa-reviewer's bash invocations including service-bringup logs

5. **Timeout budget.** Per ADR-031, qa-reviewer inherits its parent
   plan's timeout. This ADR adds a per-service-startup budget guideline
   matched to the profile's `cost` field:
   - `cost: low` (raw-mavlink-direct, similar) — 30s service-bringup,
     60s per test, 5min total qa cycle
   - `cost: medium` (px4-sitl variants) — 60s service-bringup, 120s per
     test, 10min total qa cycle
   - `cost: high` (px4-gazebo-peripherals, similar) — 180s
     service-bringup, 300s per test, 20min total qa cycle
   - These map to per-tier defaults in `project.qa.timeouts`,
     overridable per-project. If exceeded, qa-reviewer returns
     `needs_changes` with timeout evidence; operator can re-trigger
     after addressing the underlying slowness.

### State transitions (no PLAN_STATES changes)

```
implementing
  ↓
reviewing_qa  ← qa-runner spawns service containers, runs qa.yml steps,
                qa-reviewer evaluates verdict. SYNCHRONOUS BLOCK.
                Timeout enforced. Cancellation possible via existing
                /plans/{slug}/cancel.
  ↓
complete | failed | implementing (revision)
```

No new PLAN_STATES stages. No `pending_external_verdict` shape. The
operator-facing state is the existing `reviewing_qa`, with progress
visible through existing channels.

### Sibling container vs DinD — why DooD here

| Option | How | Pro | Con |
|---|---|---|---|
| **Sibling (DooD)** — chosen | Mount `/var/run/docker.sock` into qa-runner; spawned services are siblings on host | Fast (no nested daemon cold start), simple network model (services on host network or named act network), works with stock `act` | qa-runner has effective root on host. Bind-mount escape possible if qa-runner were compromised |
| **DinD** | qa-runner runs its own privileged Docker daemon, services nested inside | Stronger isolation from host | Slow (cold daemon every job), privileged container, nested-overlay-fs quirks, "DinD problems" the user already knows about |
| **`services:` on host GHA runner** (no qa-runner) | Drop the qa-runner container layer; run act directly on host | Simplest infra | Loses qa-runner's act/version pinning and reproducibility guarantees from ADR-031 |

We accept the DooD trust trade-off because:
- qa-runner runs **operator-defined qa.yml**, not agent-defined code.
  The agent's untrusted code lives in the sandbox container which
  does NOT get socket access.
- Semspec's target deployment is local-dev and small-team CI today,
  not shared multi-tenant runners. The threat model that makes DooD
  unacceptable (untrusted PR contributors, multi-tenant runner farm)
  is not our 2026 H2 scope.
- If the threat model changes, swapping DooD for a sandboxed runner
  (e.g., Firecracker microVM with its own daemon) is a localized
  qa-runner change, not a state-machine change.

### What "synchronous block" means for the operator UX

The user feedback driving this ADR was: *"semspec continues to look
and feel like an issue-to-PR app."* Issue-to-PR usage means the user
files an issue, walks away, comes back to a green PR. They don't want
to live-monitor a `pending_external_verdict` queue. They DO want:

- Clear in-flight state visible at `GET /plan-manager/plans/<slug>`
  (existing `reviewing_qa` stage is sufficient)
- Predictable wallclock: "this scope will be ~10 min in qa" not
  "indeterminate, will notify"
- Timeouts that bite when something is genuinely wrong, not when
  service boot is just slow on a particular machine

The per-cost-tier timeout guideline above gives operators a knob
calibrated to the actual cost the catalog declares. A `cost: medium`
profile that exceeds its 10min budget is genuinely wedged, not just
unlucky.

## Consequences

### Positive

- **Closes the integration-test gap.** Required-tier profiles
  (`mavlink.px4-sitl.mavsdk-smoke`, etc.) actually exercise their
  declared integration target during qa. PR #20's "designed-for but
  not built-or-tested" footnote becomes "built and verified."
- **No PLAN_STATES schema change.** The existing `reviewing_qa` stage
  carries the new behavior. Operators using the plan API don't have to
  learn new states or callback patterns.
- **Catalog metadata pays off.** The `images` / `ports` / `env` /
  `readiness` fields that already exist on profile entries become
  load-bearing instead of documentation.
- **Operator opt-out preserved.** Projects with hand-tuned qa.yml that
  conflicts with rendered services can set
  `project.qa.skip_service_injection: true`. Architect's profile
  selection still informs the verdict; the operator just owns the
  service lifecycle directly.
- **Sponsor narrative tightens.** "semspec dispatches the qa.yml that
  matches the architect's tier choice, including real service
  containers when the profile requires them" is a coherent story for
  the issue-to-PR shape.

### Negative

- **qa-runner needs host Docker socket.** The trust trade-off
  documented above. Operators deploying semspec in environments where
  this is unacceptable have to either run qa.level ≤ unit or supply
  their own isolated runner image.
- **First-run service-image pull cost.** A `px4io/px4-sitl:latest` pull
  is ~500MB on cold cache. Pre-pull during qa-runner image build is
  the obvious mitigation but adds image-build complexity.
- **qa.yml templates become a contract.** Once we inject services
  blocks, the operator's qa.yml has to leave a known place for the
  injection. We'll need to document the expected qa.yml shape.
- **Network model coupling.** Services on the host docker network can
  collide with other host-network services. Service-name conventions
  and port-mapping defaults need to be operator-overridable.

### Risks

- **Service-readiness false-passes.** A `readiness` probe that
  passes too early (TCP socket open, but daemon not yet accepting
  MAVLink) silently makes tests flaky. Mitigated by gating profile
  authorship: the catalog requires `readiness` entries match real
  service semantics, not just `nc -z`.
- **Operator qa.yml conflicts.** A project that already declares
  `services:` in qa.yml will collide with the rendered injection.
  Mitigated by skip_service_injection flag + a structural-validator
  pre-check that warns if both are present.
- **Test isolation across plans.** Sibling containers persist on the
  host until torn down. Two plans running concurrently against the same
  profile could race on port 14540. Mitigated by per-plan port
  randomization (the renderer adds a plan-hash suffix to host port,
  service-internal port stays canonical).

## Alternatives Considered

### A. GHA `services:` block emitted into qa.yml (chosen)

Selected. Idiomatic GHA pattern. nektos/act supports it natively.
Catalog metadata already enumerates exactly what `services:` needs.

### B. Testcontainers in agent-generated test code

Have the developer agent write Go test code that uses
`testcontainers-go` to spawn services from inside the test process.
Library is mature, gomavlib already uses it for its own integration
tests, dynamic per-test config is possible.

Rejected as the primary mechanism (kept as a fallback for advanced
cases) because: putting service-bringup logic in agent-generated code
moves the integration concern from operator-declared infrastructure
(qa.yml) to agent-generated test code. The agent has to discover the
testcontainers API, get the image reference right, get the readiness
probe right — each is another vector for the kind of fabrication PR
#18's evidence anchors are meant to constrain. The qa.yml-rendered
services block puts the lifecycle in the operator's hands (via the
catalog) where it structurally belongs.

If a profile genuinely needs per-test orchestration that `services:`
can't express (e.g., multi-container topologies with dynamic config),
the architect can still pick a Testcontainers-friendly profile and
the developer can use testcontainers-go in the tests — the two
patterns coexist.

### C. Docker-in-Docker for qa-runner

Rejected. Slower cold-start (nested daemon every run), requires
privileged container, well-known overlay-fs and networking quirks.
The user's explicit prior experience matches the industry consensus
that DinD is the wrong default for CI test orchestration.

### D. Self-hosted runners with pre-installed SITL

Rejected for the target deployment. Local dev and small-team CI
shouldn't require dedicated runner fleet management. Self-hosted is a
valid escape hatch for teams with hardware-specific tests (e.g.,
actual MAVLink radio in the loop) but not the right default.

### E. Deferred deep-qa tier (`pending_external_verdict` stage)

Considered and **explicitly out of scope for semspec.** This was the
"big structural option" floated in the
`mavlink-decode-2026-05-28/README.md` QA limitations section. The
shape would be:

- New `qa.level=deferred`
- New PLAN_STATES stage `pending_external_verdict`
- Verdict callback subject `qa.external_verdict.<plan_slug>`
- External runner (Buildkite, CodeBuild, scheduled GHA) executes
  expensive tests asynchronously and posts back

**Why we rejected it for semspec:**

Semspec's product shape is **opinionated issue-to-PR autonomy**. The
operator files an issue, walks away, comes back to a PR-ready result.
A deferred-verdict model fragments that into "PR ready, but external
verdict pending — check back in 4 hours." That's a different product:
it suits CI orchestration platforms (Buildkite is exactly this shape)
but not the issue-to-PR autonomy semspec is committing to.

Synchronous blocking with calibrated timeouts gives the operator a
predictable wallclock budget and a single moment of "PR is ready" or
"plan failed, here's why." If a particular project's integration tests
genuinely cost hours, that project's operator can either accept the
hours-long wallclock per plan, lower their `qa.level`, or run the
expensive tests outside semspec entirely (their existing CI catches
them on PR merge). The deferred tier would optimize for the case
semspec is choosing NOT to be a fit for.

Concretely: a SITL-class test budget of 10–20 minutes per plan fits
the issue-to-PR shape. A SITL-class test budget of hours doesn't.
Forcing the architecture to handle both adds state-machine complexity
and operator-surface complexity for a use case that's better served
by a different tool.

If usage data later shows we're wrong about the cost distribution —
that integration tests genuinely run hours and operators want to
continue using semspec for them — the deferred tier becomes a real
ADR. It is not one today.

## Implementation phasing

| Phase | Work | Dependency |
|---|---|---|
| 1a | Catalog renderer (`workflow/harnesscatalog/qarender`) | None — pure transformation against existing types |
| 1b | qa-runner socket mount + integration test verifying sibling-container spawn | None |
| 1c | qa.yml emission at `reviewing_qa` entry (project-manager or qa-reviewer) + skip_service_injection flag | 1a |
| 2 | Per-cost-tier timeout defaults + override in project config | 1c |
| 3 | Verification scenario: required-tier mavlink profile end-to-end | 1c, 2 |
| 4 | Sponsor-pack update (mavlink-decode-2026-05-28's QA limitations section moves from "designed-for-not-built" to "shipped and verified") | 3 |

Phase 3 is the gate that makes this real. Without it we're shipping
infrastructure without proof. The verification scenario will need a
fixture that pre-stages the SITL image (image caching is itself a
qa-runner concern worth handling deliberately).

## References

- ADR-031 — QA Test Execution (the qa-reviewer + qa-runner
  architecture this ADR extends)
- PR #18 — `feat(workflow): add catalog-backed harness profiles`
  (the catalog metadata this ADR consumes)
- PR #20 — mavlink-decode tier + workspace snapshot (the verification
  that exposed the gap)
- `docs/sponsor-packages/mavlink-decode-2026-05-28/README.md` — QA
  limitations section that motivated this ADR
- `workflow/harnesscatalog/catalog/mavlink.yaml` — the four profiles
  whose `runner_support`, `images`, `ports`, `evidence_anchors` this
  ADR makes load-bearing
