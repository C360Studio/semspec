# Flow overview — how a run actually happens

This document explains the cast of agent roles, the phase flow they
coordinate through, and the deterministic structural checks that gate
each phase. It's the answer to "what is the system actually doing
during those 78 minutes?"

The goal here is orientation, not implementation detail. If you want
to look at code, the repository is at github.com/C360Studio/semspec and
the take-30 commits are in PR #2.

---

## Cast of agent roles

Semspec is a multi-agent system. Each agent has a single role with a
constrained tool palette and a specific kind of structured output it's
allowed to emit. No agent is general-purpose; specialization is part of
how the system stays predictable.

### Planning roles

These take the user's English prompt and convert it into a structured
plan that downstream roles can act on.

- **Planner** — Reads the prompt + the project file tree and produces
  a `Plan` (goal + context + scope). Cannot write code, cannot call
  external APIs beyond web search. Goal is to commit the system to a
  specific shape of work before anyone writes anything.

- **Requirement generator** — Takes the approved plan and partitions
  the work into 3–8 `Requirement`s. Each requirement declares
  `files_owned` (the files this requirement is allowed to modify) and
  optionally `depends_on` (other requirements that must finish first).
  The partition is what lets requirements run in parallel worktrees
  later.

- **Architecture generator (architect)** — Produces the
  `ArchitectureDocument`: technology choices, component boundaries,
  data flow, decisions, actors, integrations, **upstream resolutions
  with harness profile selections**, and the implied test surface. This is the role
  that reads upstream documentation (via `web_search` + `http_request`)
  to discover Maven coordinates, container images, protocol details
  — the role take-30's success hinges on most. The architect also
  has `bash` access for reading workspace manifests but cannot write
  code.

- **Scenario generator** — Takes each requirement and emits BDD
  `Scenario`s in Given/When/Then form. Each scenario is linked to its
  owning requirement and is treated as a verifiable acceptance
  criterion by the developer downstream.

### Review roles

These read the planning roles' output and either approve, reject, or
request specific revisions.

- **Plan reviewer** — Applies a checklist of 9 criteria against the
  plan + architecture + requirements + scenarios as they accumulate.
  Emits structured `PlanReviewFinding`s with `Action: VERB <value>
  (TO|FROM|IN) <field>` directives so the regen pass knows exactly
  what to change. (The "Action directive" pattern came out of take-24,
  where prose-only findings produced regen passes that moved in the
  wrong direction.)

- **Code reviewer** — Reviews the developer's submitted work per
  requirement. Verdict is approved / rejected / needs_changes with
  specific feedback. Operates inside the TDD cycle.

- **QA reviewer (Murat)** — Reads the QA executor result (passed/
  failed + failures + artifacts) and renders the release-readiness
  verdict. Decides whether to ship, request changes, or escalate.
  Scoped by `qa_level` (synthesis / unit / integration / full).

### Implementation roles

These do the actual code work.

- **Task decomposer** — Breaks a single requirement into a DAG of
  `nodes`, each of which becomes a separate developer dispatch.
  Tightly coupled to the developer role — its output is the
  developer's input.

- **Developer** — The agent that actually writes code. Runs inside a
  sandbox container with bash, file edits, web search, docker (this
  session's addition). Operates inside a TDD cycle: write failing
  test → write code → run tests → submit_work or iterate.

### Auxiliary roles (used in some scenarios)

- **Researcher** (shelved on this branch — built but disabled per a
  May 15 design pivot). Was meant for sub-agent delegation of upstream
  discovery; current evidence suggests the architect doing discovery
  directly is sufficient.

- **Lesson decomposer** — When a review rejects code, this role
  extracts an audited "lesson" with citations, intended to surface in
  future agent prompts so the system doesn't repeat the same mistake.

---

## Phase flow

The plan moves through a state machine. Each state transition is
visible in the watch log timestamps (see `evidence/watch.log`).

```
[user prompt arrives]
        |
        v
   StatusDrafting        ← planner dispatched
        |
        v
   StatusReviewingDraft  ← plan-reviewer dispatched
        |
        v
   StatusApproved        ← plan body green
        |
        v
   StatusGeneratingRequirements
        |
        v
   StatusRequirementsGenerated
        |
        v
   StatusGeneratingArchitecture  ← architect dispatched (cold-start
        |                          discovery happens here — web_search,
        |                          http_request, bash on /tmp/osh-core
        |                          for take-30 specifically)
        v
   StatusArchitectureGenerated
        |
        v
   StatusGeneratingScenarios
        |
        v
   StatusScenariosGenerated
        |
        v
   StatusReviewingScenarios     ← plan-reviewer applies criteria 1–9
        |                          incl. 7a (upstream_resolutions) and
        |                          7b (harness profile discipline)
        v
   StatusScenariosReviewed
        |
        v
   StatusReadyForExecution
        |
        v
   StatusImplementing           ← per-requirement TDD cycles run in
        |                          parallel worktrees. Each requirement
        |                          gets its own git branch. Multiple
        |                          dev → code-reviewer cycles per req.
        v
   StatusReadyForQA             ← all reqs merged to plan branch
        |
        v
   StatusReviewingQA            ← qa-reviewer (Murat) dispatched
        |
        v
   StatusComplete | StatusAwaitingReview | StatusRejected
```

For take-30: the `Implementing` phase alone was 68 minutes of the 78
minute total — that's where the developer agent did the bulk of the
work. The plan/architecture/requirements/scenarios phases together
were 8 minutes; QA was 2 minutes.

---

## Defense in depth: the structural-check layers

The system has six layers of deterministic gating. The point of the
layering is that an LLM-side defense (a reviewer applying a criterion)
can fail or be persuaded around; a code-side gate cannot. Each layer
catches a different class of mistake.

### Layer 1: Wire-shape validators

**Where:** `tools/terminal/validators.go`, fires inside the `submit_work`
tool call before the deliverable is ever accepted.

**Fires on:** every agent submission of a structured deliverable.

**Catches:**

- Plan submissions where `scope.include` lists files that don't exist
  on disk (the agent gets a directive RETRY HINT telling them to move
  the path to `scope.create`)
- Requirements with empty `files_owned` (when multi-req); duplicate
  paths across requirements without `depends_on` linkage; missing
  fields
- Architecture submissions missing actors, integrations, decisions,
  upstream_resolutions, test_surface; or with malformed shapes
  (actor `type` not in {human, system, scheduler, event}; integration
  `direction` not in {inbound, outbound, bidirectional}; etc.)
- This session added: empty integrations + human actor declared (lazy
  architect); empty test_surface + integrations declared (no coverage
  for declared boundaries); UpstreamResolution with `role: integration_target`
  + missing `harness_profiles` selection (the harness profile discipline)

**Failure mode:** the submit_work call is rejected; the agent must
re-emit. No state transitions on failure.

### Layer 2: Plan-reviewer prose criteria

**Where:** `prompt/domain/software_render.go` lines 785–796 (criteria
1 through 9). The plan-reviewer LLM applies these as a structured
checklist; emits `PlanReviewFinding`s with `Action: VERB value (TO|
FROM|IN) field` directives.

**Fires on:** every plan-reviewer dispatch — once after planner, once
after requirements, once after architecture, once after scenarios.
Re-fires on each revision iteration until approved or revision cap
hit.

**Catches:**

- Goal coverage gaps (requirements don't address the stated goal)
- Requirements without scenarios
- Dependency graph cycles / orphan references
- **Criterion 3a: file-ownership partition** — two requirements
  claiming the same path without explicit `depends_on` (otherwise
  parallel worktrees deadlock on plan-level merge)
- Orphan scenarios (no requirement_id link)
- Scope misalignment
- Architecture coherence (technology choices contradicting manifests,
  components overlapping, actors with empty triggers, integrations
  contradicting components)
- **Criterion 7a: upstream resolution discipline** — every external
  library named anywhere must have a corresponding
  `upstream_resolutions[]` entry with concrete coordinate, source_ref,
  and API surfaces with citations
- **Criterion 7b: harness profile discipline (this session)** — every
  resolution with `role: integration_target` must be covered by a valid
  `harness_profiles[]` selection. The profile ID resolves to system-owned
  runner details, evidence anchors, and required assertions. Catches the
  goodhart shape where the architect declares the target but doesn't bind
  it to a testable harness.

**Failure mode:** revision iteration triggers regen with the
findings inlined into the next prompt as Action directives.

### Layer 3: Tool-call governance

**Where:** `configs/e2e-hybrid.json` `rule-processor.inline_rules`,
fires inside the agentic-loop's tool dispatch pipeline.

**Fires on:** every agent tool call (mainly bash). Evaluated
synchronously before the tool result is delivered back to the agent.

**Catches:**

- `cd /workspace` (bare, without worktree subdir) — keeps dev work
  inside the scoped worktree, not the main workspace
- Bash redirects into `/workspace/` (`>` and `tee`) — same reason
- `docker run --privileged` — privileged container is the obvious
  escape from the DooD sandbox
- `docker` commands binding `/var/run/docker.sock` (nested socket
  mount → sibling container with full privs)
- `docker run -v /:` (host root mount)
- `docker run --network host` (bypasses container network isolation)

**Failure mode:** the tool call returns a "rejected: <reason>"
result to the agent without executing. The agent typically self-
corrects.

### Layer 4: Structural-validator

**Where:** `processor/structural-validator/`, dispatched after each
scenario's developer cycle merges back to the plan branch.

**Fires on:** every scenario merge during the Implementing phase.
Files the developer modified are passed in for selective check
triggering.

**Catches:**

- Project's `checklist.json` checks (this fixture: `./gradlew
  dependencies` must resolve, `./gradlew test` must pass — both
  `required: true`). The project owner controls this list.
- Go-specific: `go test` on modified packages; the always-on
  "tests-must-exist-for-changed-non-test-Go-files" gate (caught
  take-21's missing-test slip).
- `CheckAntiMock` (advisory): rejects when mock-type declarations
  outnumber test functions.
- **`CheckHarnessProfileDiscipline` (catalog-backed required-profile evidence)**: for
  each selected required harness profile, the dev's modified test files
  must reference the catalog evidence anchors (profile ID plus the
  image/port/assertion strings the profile owns). Catches the goodhart
  pattern where the architect selects the right harness but the dev
  silently skips it.
- **`CheckStubArtifacts` (this session, required: true, hard
  rejection)**: any `.jar` file in the modified set is opened as a
  zip; rejected if it has <2 KiB total uncompressed size, zero
  `.class` entries, or is an invalid zip. This is the deterministic
  backstop for the take-19 / take-29 fabrication shape (55-byte
  MANIFEST-only stub JARs).

**Failure mode:** the scenario merge is rejected; the developer
cycle re-runs with the failing check's output as feedback. Hard-fail
checks (`required: true`) terminate the scenario after the retry
budget is exhausted, raising a `PlanDecision` for human review.

### Layer 5: QA gate

**Where:** `cmd/qa-runner/runner.go` invoking `nektos/act` against
`.github/workflows/qa.yml` (this session: now auto-scaffolded by
plan-manager so fixtures don't need to ship one).

**Fires on:** plan transitioning to `ReadyForQA` at `qa_level=
integration` or `qa_level=full`. Sandbox runs at `qa_level=unit`
via a separate subscriber.

**Catches:**

- The full GitHub Actions workflow runs in a clean-room container
  (act spawns `catthehacker/ubuntu` runners). This means the test
  suite is exercised against the freshly-checked-out plan branch
  with no leftover dev-environment state.
- Real integration tests against real upstream containers
  (Testcontainers in the test code; the act runner has docker
  socket access).
- Captures artifacts (test reports, coverage, log excerpts) into
  `.semspec/qa-artifacts/<slug>/<run-id>/` for the human reviewer.

**Failure mode:** publishes `QACompletedEvent` with `passed: false`
and the captured failures. qa-reviewer reads it and decides verdict
(`needs_changes` raises `PlanDecision`s for changes; `rejected`
escalates to human).

### Layer 6: Operator SOPs (standards.json)

**Where:** `<workspace>/.semspec/standards.json` (committed to the
fixture / project repo). Plan-reviewer reads it and applies each
item as an SOP compliance check.

**Fires on:** plan reviewer dispatches (same as Layer 2, but the
input is operator-declared rather than built-in).

**Catches:**

- Operator-declared compliance rules. For the take-30 fixture, 17
  items spanning security (no secrets in code, parameterized
  queries, input validation, safe error messages, auth checks),
  testing (TDD discipline, coverage targets), error handling
  (return-don't-log), code style, observability, documentation.
- See `operator-rules/standards.json` for the full content.

**Failure mode:** plan-reviewer raises a finding tagged with the
SOP ID; the responsible regen role addresses it.

---

## How take-30 was protected by each layer

Tracing the take-30 outcome backward through the layers (top is most
LLM-dependent, bottom is most deterministic):

| Layer | Did it fire? | What did it catch? |
|---|---|---|
| 6: Operator SOPs | yes | Standard compliance review on each phase |
| 5: QA gate | yes | Final clean-room re-run of all tests; passed |
| 4: Structural-validator | yes | `gradle dependencies` + `gradle test` per scenario; stub-detector and harness-profile-discipline armed but never triggered |
| 3: Tool-call governance | yes | No `cd /workspace` / docker-escape attempts logged |
| 2: Plan-reviewer criteria | yes | Architecture / requirements / scenarios approved on first iteration, no 7a/7b violations |
| 1: Wire-shape validators | yes | All submissions passed wire-shape on first attempt |

The take-30 path was: every layer fired, no layer raised a violation
(other than minor non-blocking advisory ones). The defense-in-depth
worked because the upstream fix (GITHUB_TOKEN passthrough) made
fabrication unnecessary, not because the lower layers caught a
fabrication attempt.

That's a useful distinction: this run does NOT validate the lower-
layer detectors against a real fabrication regression. Validating
those detectors deliberately is in PR #2's test-plan checklist as a
follow-on measurement.

---

## What the operator rules in this package show

- **`operator-rules/standards.json`** — 17 SOPs across 6 categories.
  These are the rules the operator declared for this project. They're
  enforced by the plan-reviewer as Layer 6 above.

- **`operator-rules/checklist.json`** — 2 structural checks for this
  Java project (`gradle dependencies` and `gradle test`, both
  `required: true`). These are run by the structural-validator as
  part of Layer 4. The operator declares the checks; the framework
  enforces them.

The same machinery supports arbitrary additional checks per project
(`go vet`, `npm audit`, `cargo clippy`, `terraform validate`,
custom scripts) — the framework treats checklist items as opaque
commands to run with a timeout and a required/advisory bit.
