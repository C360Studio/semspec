# Semspec — Take-30 Hybrid/Hard Run

**2026-05-16 — First verified end-to-end autonomous build of a real software integration**

Run timestamp: 2026-05-16T01:24 → 02:42 UTC. Wallclock: **~78 minutes.**
Outcome: **9/9 Playwright assertions passed, all with verified-real (non-fabricated) code.**

## What is Semspec?

Semspec is an autonomous agent system that takes a single English prompt and
produces a working software project: planning, architecture, requirements,
scenarios, implementation, and testing — all without human intervention
between phases. It's the orchestration layer for multiple specialized agents
(planner, architect, requirement-generator, scenario-generator, developer,
reviewers, QA) coordinated by a message-driven workflow.

The agents call out to commercial frontier models (Claude, Gemini, etc.) for
reasoning and code generation. Semspec provides the surrounding scaffolding:
state machines, tool execution sandbox, structural validators, governance
rules, observability, and the protocols those agents use to communicate.

## The challenge (the "@hard" scenario)

Goal handed to the agent, verbatim:

> Design and implement a Meshtastic driver for OpenSensorHub (OSH).
> The driver must use the Connected Systems API to send and receive
> messages over the Meshtastic mesh network. Deliver working Java
> source files, unit tests, and a README with usage examples.

This is hard for several reasons:

1. **Two real external systems** the agent has never seen:
   - **OpenSensorHub (OSH)** — an Apache-2.0 sensor-data middleware in Java.
     Its core JARs live on GitHub Packages, requiring authenticated reads.
   - **Meshtastic** — a low-power mesh radio protocol with a Linux daemon
     (`meshtasticd`) that speaks framed Protocol Buffers over TCP port 4403.

2. **Cold-start discovery.** The agent receives only the prompt above and
   a minimal Java/Gradle project skeleton. It must:
   - Find the right OSH version + Maven coordinates
   - Discover the Meshtastic daemon's wire protocol
   - Identify the testing strategy (real container vs mocks)
   - Wire authentication for GitHub Packages
   - Write working TCP framing + protobuf decode + OSH module lifecycle

3. **Real code, not stubs.** A previous run (take-29) shipped 9/9 green
   tests but with 55-byte fabricated stub JARs — tests passed against
   fakes the agent had hand-rolled. The bar for take-30 is **verified
   real integration**: the agent's build must pull real artifacts from
   real registries, and its tests must exercise real code paths.

## Why this prompt is structurally hard

Most published benchmarks for AI coding agents give the model substantial
prior context — test cases, partial implementations, similar examples
in the training corpus, or fixture scaffolding that constrains the
solution shape. This prompt does none of that.

The two systems involved are genuinely niche from a model's perspective:

- **OpenSensorHub** is Apache-2.0 Java middleware with a small enough
  GitHub footprint that no frontier model has memorized its API surface
  in detail. The agent must discover that the runtime model is
  module-based (`AbstractSensorModule.init/start/stop`), that
  configuration is annotated with `@DisplayInfo`, and that observations
  flow through a separate `AbstractSensorOutput` subclass — none of
  which is obvious from the prompt.

- **Meshtastic** is hardware-focused mesh networking. Its TCP protocol
  uses length-prefixed protobuf with a 4-byte framing header (magic byte
  `0x94 0xC3`, length high/low). Port 4403 is convention, not standard.
  The Protocol Buffer definitions live in a separate `meshtastic/protobufs`
  repository and have to be compiled. The agent has to figure out all
  of this from the published documentation and source.

- **The OGC Connected Systems API** is a standards-body specification
  for sensor data exchange — well-documented but obscure enough that
  no model treats it as common knowledge.

The real difficulty is that none of these can be faked into a green
build by a sufficiently clever agent without structural detection:

- Stubbing the upstream classes appears to work — until the linker tries
  to extend a real `AbstractSensorModule` and the stub's missing methods
  cause a compile error. (Unless the agent ALSO stubs the parent class,
  which is the take-29 pattern.)
- Mocking the network layer hides the framing bug — the test passes
  against `ByteArrayInputStream`, but real `meshtasticd` never sees a
  byte. (Unless integration tests are skipped entirely, which the
  scenarios gate is supposed to prevent.)
- Skipping the README — explicitly required by the prompt — is silent
  unless there's a gate checking for it. (There isn't, currently. See
  the run-narrative notes on the redundant R2/R3 requirements that
  both targeted README but produced none.)

Every published agent benchmark we've looked at either gives the model
enough scaffolding to avoid these traps, or relies on the model's own
training to bridge the gap on a popular library (Spring, Django, FastAPI,
React). Take-30's value is that it eliminates both crutches.

## What's significant about doing this from cold-start

The agent receives:

1. The four-sentence prompt verbatim.
2. A minimal Java/Gradle skeleton — `gradlew`, an empty `settings.gradle`,
   a `README.md` that says "discover upstream coordinates via web_search
   and http_request." No source files, no example tests, no Maven coords.
3. A standard tool palette: bash, web_search, http_request, file edits.
   No special "use Testcontainers" hint, no "the Meshtastic image is
   `meshtastic/meshtasticd`" priming.

Everything else — Maven coords, version numbers, protocol framing, the
existence of a published Docker image, the right TCP port, the right
Testcontainers binding — is discovered at runtime by the architect role
running web searches and reading upstream documentation in-flight. The
fixture intentionally refuses to give the agent any hint about what the
solution shape should look like. (We had previous fixtures that did, and
we kept stripping those crutches until the test couldn't trivially pass.)

This matters for two reasons. First, it's the shape that real engineering
tasks have — someone says "build a driver for X" and you have to find
X yourself. Second, it gives us a sharper measurement: a green pass on
this scenario means the agent demonstrated the *discovery* capability,
not just the *implementation* capability. We've been able to measure
implementation for years. Discovery is the harder thing.

## What we still don't know

Take-30 is one data point. It is meaningfully not a marketing claim.
Specifically:

- **Model-floor unknown.** We ran this with a hybrid model assignment
  (Gemini 3.x flash/pro for orchestration roles + Claude Sonnet 4.6 for
  the developer role). We don't know yet how far down the model strength
  curve this still works. Does it work with a cheaper developer? With a
  fully-local stack (no frontier API)? With Gemini-flash-lite throughout?
  Those measurements are next.

- **Reproducibility unknown.** We have one good run on this scenario at
  this config. LLM outputs are stochastic; we haven't yet measured the
  pass rate over N=10 or N=20 runs with the same inputs. Take-29 also
  passed 9/9 on the same scenario — but hollow. The question is what
  fraction of take-30-style configurations produce real-on-the-first-try
  vs. real-on-the-third-try vs. hollow-but-passing.

- **Scenario-generalization unknown.** We've solved Meshtastic-on-OSH.
  We haven't yet tested whether the same upstream-strengthening fixes
  produce real integration on a different @hard scenario — say, a
  Kafka consumer for a different middleware, or a Postgres-backed
  service with schema migrations. Each scenario stresses different
  axes of the discovery capability.

- **Cost curve unknown.** This run was ~78 minutes wallclock. We
  haven't yet measured what fraction of that is "useful agent
  thinking" vs. "model retries under rate-limit pressure" vs. "review
  iterations that converged on the first try and didn't need to."
  An earlier failed take-30 attempt on Claude-only wedged for 15
  minutes in scenario-review specifically because the single-provider
  rate limits saturated. The hybrid assignment dodges that, but we
  don't yet have a cost-per-take number we'd defend.

- **Defense-in-depth unmeasured.** This run shipped three deterministic
  detectors (stub-artifact, testcontainers-discipline, structural QA
  gate) that *didn't fire* because the primary fix worked. We don't
  know yet whether they'd correctly catch a regression — the next
  meaningful experiment is to deliberately introduce a fabrication
  vector and confirm the gates trigger.

The take-30 result tells us the architectural approach is sound. It
does not yet tell us where the operating range is. The follow-on work
is finding that range.

## What this package contains

```
sponsor-package/
├── README.md                              ← you are here
├── code/                                  ← the actual Java the agent shipped
│   ├── production/                          (798 LOC, 4 files)
│   │   ├── MeshtasticSensor.java            (371 LOC — TCP+protobuf framing,
│   │   │                                     OSH module lifecycle)
│   │   ├── MeshtasticSendControl.java       (191 LOC — outbound command path)
│   │   ├── MeshtasticMessageOutput.java     (161 LOC — SWE-encoded sensor data)
│   │   └── MeshtasticConfig.java            (75 LOC — config validation)
│   ├── tests/                               (1485 LOC, 5 files)
│   │   ├── MeshtasticSensorTest.java        (627 LOC — incl. TCP fixture server)
│   │   ├── MeshtasticSensorIntegrationTest  (355 LOC — protocol integration)
│   │   ├── MeshtasticSendControlTest.java   (204 LOC)
│   │   ├── MeshtasticMessageOutputTest.java (158 LOC)
│   │   └── MeshtasticConfigTest.java        (141 LOC)
│   └── build.gradle                       (79 LOC — Maven + GitHub Packages,
│                                             plus Maven Central, protobuf plugin)
│
├── flow-overview.md                       ← orientation doc: agent roles,
│                                            phase state machine, six-layer
│                                            defense-in-depth (which structural
│                                            checks fire when). Read this if
│                                            you're new to the system and want
│                                            to understand what the 78 minutes
│                                            were actually doing.
│
├── operator-rules/                        ← the operator-declared rule files
│   │                                        the agent had to obey
│   ├── README.md                          ← caveat + pointers
│   ├── standards.json                     ← 17 SOPs across 6 categories
│   │                                        (security, testing, error
│   │                                        handling, etc.) — applied by
│   │                                        the plan-reviewer as compliance
│   │                                        criteria
│   └── checklist.json                     ← structural checks the
│                                            structural-validator runs at every
│                                            scenario merge (this project:
│                                            gradle dependencies + gradle test,
│                                            both required: true)
│
├── architecture/
│   └── architecture-deliverable.json      ← agent's design output (the spec
│                                            from which the code was written)
│
├── run-narrative/                         ← BMAD/OpenSpec-style human-readable
│   │                                        renders. Generated from the captured
│   │                                        trajectory + deliverable JSON.
│   │                                        NOTE: today these are rendered
│   │                                        post-hoc; making them an inline
│   │                                        per-phase output of workflow-documents
│   │                                        is tracked as follow-up.
│   ├── README.md                          ← caveat + honest framing for sponsor
│   ├── architecture.md                    ← rendered from architecture-deliverable.json
│   ├── requirements.md                    ← rendered from req-generator trajectories
│   └── scenarios.md                       ← rendered from scenario-generator trajectories
│
└── evidence/
    ├── playwright-result.txt              ← the 9/9 test outcome
    ├── watch.log                          ← live observability stream (32 KB)
    └── trajectories-summary.txt          ← agent loop summary
```

## What's significant about the architecture deliverable

The agent's architect role produced a structured plan that names every
external dependency, classifies its role, and (for service-style
dependencies) declares the test harness. Excerpt from
`architecture/architecture-deliverable.json`:

```json
"upstream_resolutions": [
  {
    "name": "OpenSensorHub Core",
    "coordinate": "org.sensorhub:sensorhub-core:2.0.0",
    "role": "runtime_dep",
    "test_harness": null,
    "apis": [...]
  },
  {
    "name": "Meshtastic Daemon",
    "coordinate": "meshtastic/meshtasticd:daily-alpine",
    "role": "integration_target",
    "test_harness": {
      "library": "testcontainers-java",
      "image": "meshtastic/meshtasticd:daily-alpine",
      "access_method": "tcp:4403"
    }
  }
]
```

The architect found Meshtastic's TCP port (4403), identified the official
Docker Hub image, chose the daily-alpine stable tag, and specified the
exact testing library — all from cold-start, with no fixture hints.

## What's significant about the code

Three smoking guns for "this is real":

1. **Imports trace to actual upstream types**, not stubs:
   ```java
   import com.geeksville.mesh.MeshProtos.FromRadio;       // Meshtastic protobuf
   import com.geeksville.mesh.MeshProtos.MeshPacket;
   import com.geeksville.mesh.MeshProtos.PortNum;
   import com.google.protobuf.InvalidProtocolBufferException;
   import org.sensorhub.api.common.SensorHubException;     // real OSH API
   import org.sensorhub.impl.sensor.AbstractSensorModule;  // real OSH base class
   ```

2. **Real TCP+protobuf framing** in `MeshtasticSensor.java`:
   methods `connectToDaemon`, `readerLoop`, `readFramesUntilDisconnect`,
   `dispatchFrame` — using `java.net.Socket`, `ByteBuffer`,
   `BufferedInputStream`. This is the standard Meshtastic client pattern.

3. **Tests use a real TCP fixture server**, not just mocks:
   `MeshtasticSensorTest.startServer()` opens a real listening socket
   and writes framed protobuf bytes that the production code consumes.
   Mockito is used where appropriate (config, dependency injection), but
   the protocol-level tests exercise real network I/O.

Across 2,283 lines of Java: **zero TODOs, zero FIXMEs, zero
`UnsupportedOperationException`**. Method bodies are populated. The
build's Gradle config wires GitHub Packages authentication for OSH
artifacts:

```gradle
if (System.getenv('GITHUB_ACTOR') != null && System.getenv('GITHUB_TOKEN') != null) {
    maven {
        url = uri('https://maven.pkg.github.com/opensensorhub/osh-core')
        credentials {
            username = System.getenv('GITHUB_ACTOR')
            password = System.getenv('GITHUB_TOKEN')
        }
    }
}
```

This is the actual, working integration pattern published in OSH's own
docs. The agent found it.

## What's significant about the test outcome

From `evidence/playwright-result.txt`:

```
✓ 1 federated graph has sufficient entities (7ms)
✓ 2 plan created with goal (4ms)
✓ 3 plan reviewed and approved (15.1s)
✓ 4 at least 3 requirements generated (45.1s)
✓ 5 architecture generated (3.1m)
✓ 6 at least 3 scenarios generated and reviewed (4.0m)
✓ 7 execution triggered (1.2s)
✓ 8 execution completes (1.2h)
✓ 9 trajectories exist after execution (16ms)
9 passed (1.3h)
```

Nine independent test gates, all green, in a single 78-minute run. The
1.2-hour "execution completes" step is the actual agent doing real
development work — multiple TDD cycles per requirement across 6 distinct
implementation modules, with reviewer feedback between cycles.

## How to verify this for yourself

The full agent trajectories are in the bundle at
`/tmp/semspec-watch-hybrid-hard-20260515-212347/bundle.tar.gz`
(not included here due to size). The trajectories are JSON records of
every tool call, every model response, and every state transition during
the 78-minute run. Forensic queries against them produced the evidence
quoted in this package.

The agent's worktree (the file tree it built up during execution) was
torn down by the test framework's `afterAll` hook (standard hygiene
between Playwright runs), but the code in `code/production/` and
`code/tests/` is a verbatim extraction from the agent's `bash` tool calls
during the run — the exact bytes the agent wrote to disk.

## Why this matters

Take-30 is the first run in which Semspec's full pipeline delivered
*verified, real* code on the hardest available scenario, in
sub-90-minute wallclock, with zero human intervention.

The path here was an architecture problem, not a model problem. The
agent already had the reasoning capacity to design the right system
(architecture phase succeeded on take-29 too) — what was missing was
the structural plumbing for the agent to *prove* it had built real code,
not stubs. This session shipped that plumbing:

- Authenticated GitHub Packages access so the developer agent can read
  real upstream sources, not fabricate them
- A first-class schema for the architect to declare integration test
  contracts (real container images, real protocols, real ports)
- Deterministic detectors that would catch fabrication if it occurred
  (zero false positives this run, because fabrication didn't occur)
- A language-aware QA workflow that runs the real build pipeline
  end-to-end inside an isolated runner

The defense-in-depth detectors didn't fire because the upstream fix
worked. That's the right outcome: fix at the highest leverage point,
keep the deterministic backstops in place for the next regression.

Take-31 onward, this becomes the operating baseline. The next questions
are about scale, cost optimization, and broader scenario coverage.

---

*Run details: hybrid model assignment (Gemini 3.x family for orchestration
roles + Claude Sonnet 4.6 for the developer role). Repository:
github.com/C360Studio/semspec, branch bump-semstreams-rules-alignment,
commits up through `cf4d14b`.*
