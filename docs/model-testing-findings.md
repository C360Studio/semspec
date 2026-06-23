# Model Testing Findings

Empirical findings from real-LLM regression runs against the @easy / @medium /
@hard E2E suites. Tracks what works, what doesn't, and which fix shapes
produced which deltas — so future contributors don't reinvent the wheel.

This is a *living log*, not a benchmark. Add a section every time you take a
new model through a regression. Note the date, semstreams version, prompt-pack
revision, and concrete evidence (loop IDs, watch-sidecar evidence_ids,
post-mortem snapshot paths).

For configuration mechanics (endpoints, capabilities, env vars), see
[model-configuration.md](./model-configuration.md). For run mechanics (Task
commands, watch sidecar), see [diagnostic-bundles.md](./diagnostic-bundles.md)
and [project-setup.md](./project-setup.md) §E2E.

## How to read this doc

Each model section reports against a 4-stage rubric:

1. **Plan phase** — planner → plan-reviewer(R1) → req-gen → plan-reviewer(R-req)
   → arch-gen → plan-reviewer(R-arch) → story-prep → scenario-gen →
   plan-reviewer(R2) → ready_for_execution. The R-req/R-arch rounds are the
   mandatory ADR-051 per-phase reviews. Currently the cleanest stage on most
   providers; budgeted ~30-90s for @easy.
2. **Execution phase** — TDD per requirement: developer → validator → reviewer.
   The hard part. Wedges live here.
3. **QA phase** — release-readiness verdict (Murat). Less data so far.
4. **Cleanup / post-merge** — usually clean.

Phase outcomes use these tags:

- ✅ green: passes consistently with budget headroom
- ⚠️ flaky: passes but has known wedges, bumped retries or fixture quirks
- ❌ wedged: hits a structural wedge, doesn't recover within budget
- 🚫 blocked: provider-side issue (downtime, rate limits, capacity)

## Failure-mode taxonomy (cross-reference)

Wedges mostly fall into 5 buckets. Reference these by ID rather than
re-describing.

| Bucket | Shape | Canonical example |
|---|---|---|
| **#1 malformed-JSON** | Tool args don't parse — empty object, missing keys, truncated | qwen3-moe calling `graph_query` with truncated entity ID `semspec.semsou` |
| **#2 wrong-fields** | Tool args parse but use wrong field names or values | qwen3-coder-next outputting `cmd/server/main.go` for a one-file project |
| **#3 bad-content** | Output schema-valid but semantically wrong | req-gen producing two requirements that own the same file (file-ownership conflict) |
| **#4 loops-and-refusals** | Loop wedges: hits iter cap, no submit_work, repeats failing tool calls, or empty stop after tool calls | RepeatToolFailure on graph_query; iter=50 cap with no terminal tool call |
| **#5 hallucinated-claims** | submit_work succeeds but the worktree shows no diff — model claims work it never did | qwen3-moe `cat main.go` × 3 then submit with confident /health prose. Pre-reviewer git-diff gate (2026-05-03) catches this in 13ms. |

See `feedback_failure_mode_taxonomy.md` in agent memory for the live taxonomy
notes.

## Findings by model

### OpenRouter — qwen2.5-72b-instruct (dense)

**Endpoint config** (`configs/e2e-openrouter.json`, endpoint
`openrouter-qwen2-5-72b-dense`, model `qwen/qwen-2.5-72b-instruct`):

- `disable_keepalives: true`
- `request_timeout: 180s`
- `max_output_tokens: 8000` (capped tighter than MoE's 32000 — qwen2.5-72b
  can ramble)

**Plan phase** ❌ — escalated at `review_iteration=2` with
`verdict=needs_changes`. Plan-reviewer findings:

1. `scope.include=["main.go"]` rejected because main.go doesn't exist in the
   project file tree — should have used `scope.create`. Real planner bug,
   same shape as `project_planner_dashed_paths_cascade_2026_05_03` bug #1.
2. "Goal is vague and not actionable" — but the goal IS the test fixture
   goal verbatim ("Add a /health endpoint to the Go HTTP service…"). The
   reviewer is being more demanding than MoE about fixture phrasing.
3. "Context does not provide enough background" — same fixture-rejection
   shape as #2.

**Execution phase** — never reached. Run aborted at plan-phase escalation
after ~3:51 wallclock (Playwright stopped after the first failed test).

**A/B comparison vs qwen3-moe** (2026-05-03 v11 immediate prior run):

| Aspect | qwen3-moe | qwen2.5-72b dense |
|---|---|---|
| Planner scope.include/create discipline | ✗ same bug | ✗ same bug |
| Plan-reviewer rigor | loose — let bug through | strict — caught bug |
| Plan-phase wallclock | ~1:10 to ready_for_execution | ~3:51 to escalation |
| Execution phase reached | ✓ yes | ✗ no |
| Cycle-0 hallucinated submit_work | ✓ caught by gate | n/a |
| graph_query truncated entity ID wedge | ✗ cycle-1 wedge | n/a |
| Playwright tests passing | 6/8 | 1/8 |
| Net: produces working /health code | ✗ no | ✗ no |

**Reading**: the MoE-vs-dense hypothesis (MoE causes the wedges) is **not
supported** by this run. The actual seam is the developer / planner role
prompts being too weak for mid-tier models, *and* the plan-reviewer's
fixture-goal complaints being a separate issue. Both models fail to produce
working code; they just fail in different places.

**Followup hypotheses worth testing** (open):

- The plan-reviewer goal/context complaints (#2 and #3) reproduce on dense
  but not on MoE — is the reviewer prompt too strict for the test fixture's
  minimalist goal/context? Or is qwen2.5-72b just over-applying the
  "completeness" rubric? Try a regression with a richer fixture goal to
  see if dense-reviewer accepts.
- The `scope.include` vs `scope.create` confusion is recurring — across
  cascade fix, this run, and the prior MoE run. The fix landed in commit
  `946628c` and is in the prompt; mid-tier models still don't follow it.
  Possible fix shape: validate `scope.include` paths against the project
  file tree at planner submit_work boundary, reject if any path doesn't
  exist with hint "did you mean scope.create?".

**History**:

- 2026-05-03 v12 initial: 1/8 Playwright (plan-phase escalation). Bundle
  evidence in `/tmp/semspec-watch-openrouter-easy-20260503-164229/`.

### OpenRouter — qwen3-moe (qwen3-next-80b-a3b MoE)

**Endpoint config** (`configs/e2e-openrouter.json`):

- `disable_keepalives: true` (mitigates the post-tool-call wedge from beta.34)
- `request_timeout: 180s`
- `requests_per_minute: 60`

**Plan phase** ✅ — runs in ~1-2 min for @easy; planner / plan-reviewer /
req-gen / arch-gen / scenario-gen all submit cleanly. Confirmed 2026-05-03 v11:
plan→reviewed→requirements→architecture→scenarios→reviewed in ~1:10 with 0
errors.

**Execution phase** ❌ — two distinct wedges observed in the same task:

1. **Cycle 0 (hallucinated submit)** — bucket-#5. Model runs `cat main.go` × N
   read-only, then submits `files_modified=["main.go"]` with confident
   implementation prose. **Pre-reviewer git-diff gate catches this in 13ms**
   (shipped 2026-05-03; see
   `processor/execution-manager/component.go::developerWorkClean`). Saves
   ~150-200s and ~80-120K tokens that the v10-pre-fix run was burning per
   occurrence.
2. **Cycle 1+ (graph_query truncation)** — bucket-#1 + bucket-#4. Model receives
   the gate's feedback ("you claimed but didn't write — use bash"), then calls
   `graph_query` with a *truncated* entity ID (e.g. `semspec.semsou` —
   14-char cut from `semspec.semsource.code.workspace.file.main-go`).
   graph-gateway returns `not found:`. Model doesn't read the error,
   retries the same truncated ID 3+ times, wedges at iter=14/50.
   `RepeatToolFailure` + `GraphToolFailure` detectors fire on the watch
   sidecar.

   Open fix shapes:

   - Hoist a FULL entity-ID JSON example into the `graph_query` tool persona
     (prose rules lose to JSON examples for mid-tier models).
   - `RETRY HINT:` prefix on tool error responses so mid-tier models stop
     pattern-matching errors as happy-path output.
   - Pre-flight validate entity_id shape at the executor boundary.

   Tracked in agent memory: `project_graph_query_truncated_id_wedge_2026_05_03.md`.

**QA phase** — no data; haven't reached this stage cleanly on this model.

**Token economics** — when wedged, cycle 1 burns ~4-5 minutes wallclock and
substantial input tokens before failing. Worth aborting via active monitoring
protocol rather than letting it hit `request_timeout` naturally.

**History**:

- 2026-05-03 v10 (pre-gate): every dev loop hallucinated; 5 dev + 5 reviewer
  cycles wasted per requirement, ~80-120K tokens per occurrence.
  Diagnosed in `project_dev_wedge_diagnosis_2026_05_03.md`.
- 2026-05-03 v11 (post-gate): plan-phase 6/8 Playwright green; cycle 0 caught
  by gate in 13ms; cycle 1 hit graph_query truncation wedge.
  Validation evidence in agent memory same file. Aborted at ~8 min wallclock.

### Google Gemini Flash (frontier)

**Plan phase** ✅ — fast and reliable. Confirmed 2026-05-02:
8/8 Playwright @easy in 4.7 min after fan-in prompt fix
(`project_gemini_easy_2026_05_02_post_prompt_fix.md`).

**Execution phase** ⚠️ — frontier RLHF does NOT paper over the developer-role
submit_work seam. Bucket-#4 wedges observed on `/health` task even with correct
code already in worktree (`project_gemini_developer_bucket4_2026_05_02.md`).
Iter=50 cap hit on cycles 0/2/3 across one run.

**QA phase** ⚠️ — has produced plan-level merge conflicts when two parallel
requirements rewrote the same file
(`project_plan_merge_conflict_2026_04_28.md`). Dial #1 (planner-side scope
partition) chosen but doesn't fix all cases.

**History**:

- 2026-04-29 baseline: first post-Plan-B real-LLM 8/8 in 5.7 min; surfaced
  plan-level merge conflict (non-deterministic).
- 2026-05-02 (post-graph-internal-LLM-stack): 8/8 in 4.6 min, errors=0 alerts=0.
- 2026-05-02 (post-fan-in-prompt-fix): 8/8 in 4.7 min, errors=0. Gemini chose
  "consolidate" pattern (1 req owning main.go + main_test.go) where sparky
  chose "chain" pattern.

### Sparky — qwen3.6-27b (dense)

**Endpoint config**: hosted on local DGX. `request_timeout: 120s` (bumped from
90s after 2026-05-02 arch-gen failures).

**Plan phase** ✅ — planner + plan-reviewer clean, req-gen produces well-formed
output AFTER the 2026-05-02 fan-in prompt fix. Confirmed @easy /health: 2 reqs
on chain pattern, validator-accepted in 81s on attempt #1.

**Execution phase** ⚠️ — has reproduced bucket-#3 file-ownership conflicts in
the past. After fan-in prompt fix, cleaner. Limited recent data because the
DGX has been intermittently down.

**Historical issues**:

- arch-gen 90s timeout × 3 → plan rejected (2026-05-02). Bumped to 120s.
- Provider-side keepalive wedge same shape as openrouter; mitigated with
  `disable_keepalives: true` per beta.34.

**Status note** (2026-05-02): "sparky DGX is currently DOWN, connection
refused at genexergy.org:8000."

### Anthropic — claude-sonnet (frontier dense)

Less recent data. Used as the cloud-preferred chain in
[model-configuration.md](./model-configuration.md). Tends to handle the
plan-phase + req-gen + arch-gen consistently. Wedges on the developer seam are
less common than mid-tier models but not zero — see
`project_gemini_developer_bucket4_2026_05_02.md` for an analogous shape on
gemini-flash.

### Local Ollama — qwen3 / qwen3-coder

**Status** ⚠️ — single-GPU contention is the load-bearing constraint. See
`project_local_concurrency_limits.md`: needs `max_concurrent=2`; 5+ parallel
LLM calls queue and timeout. `project_graph_query_local_ollama_contention.md`:
graph-query has hardcoded 60s LLM timeout vs agentic-loop's 900s; collides on
single-GPU Ollama. Workaround: bump `request_timeout` on graph-query
component to ≥300s.

**Use cases that work**: Tier-2 mock-LLM ladder (deterministic fixtures),
single-loop dev iteration. Not a full @easy regression target until the
contention story improves.

## Open hypotheses

These are guesses we haven't validated yet. Each should land in this doc with
evidence after a regression run.

### MoE vs dense at the developer seam

**Hypothesis**: The qwen3-moe wedges (bucket-#1 graph_query truncation,
bucket-#5 hallucinated-submit on cycle 0) may be MoE-specific failure modes —
attention routing under tool-error context falling through to wrong experts,
or the gating network producing degenerate outputs when the prompt has long
feedback context (~20K chars). A dense ~70B model in the same parameter
class might handle the developer seam more consistently.

**Why it's only a guess**: gemini-flash is dense and ALSO hits bucket-#4
developer wedges. So the developer seam is fragile across architectures, not
just MoE. The MoE-specific signal would be the truncated-entity-ID failure —
unclear if dense models would exhibit it at the same rate.

**Candidate dense models to test**:

- `qwen2.5-72b-instruct` (Alibaba dense; closest direct comparison to
  qwen3-next-80b-a3b's MoE)
- `llama-3.3-70b-instruct` (Meta dense)
- `mistral-large-2` (Mistral dense, 123B)
- `qwen2.5-coder-32b-instruct` (smaller dense, but coding-tuned and very
  strong on tool use)

**Test plan if pursued**: drop the candidate into `configs/e2e-openrouter.json`
under a new endpoint, run `task e2e:watch:llm -- openrouter easy`, capture
full evidence (watch.log, snapshot tarball, msgs.json), report under a new
"OpenRouter — &lt;model&gt;" subsection above.

### Tool-result ergonomics for mid-tier models

**Hypothesis**: Mid-tier models (anything below frontier-RLHF tier) treat
tool-result payloads as opaque text and pattern-match on shape rather than
content. They miss errors when those errors look structurally like happy-path
results. A `RETRY HINT:` prefix or a typed `error:` JSON envelope would force
attention.

**Why it's only a guess**: We've seen the failure shape (qwen3-moe ignoring
`graph query failed: graphql error: not found:` 3+ times) but haven't tested
whether reformatting the error response actually changes behavior. Cheap to
try — single tool change, run a regression.

### Persona prompt length saturation

**Hypothesis**: ~20K-char system prompts (the developer assembly with
feedback context) approach the prompt-anchoring limit for mid-tier models —
they "lose" earlier rules when later context dominates. The fact that the
gate's feedback works on cycle 0 (short prompt) but doesn't prevent cycle 1
graph_query collapse (long prompt with feedback prepended) is consistent with
this.

**Test plan**: trim the persona to a tighter core, measure cycle-1 retry
quality on qwen3-moe. Or A/B with a shorter feedback message.

## Cardinality variance is a model-quality observation, not a test gate

Empirical observation 2026-05-05 hybrid @hard, gemini-pro on
`requirement_generation`:

- Run A (commit 198e13c, planner scope-include validator landed,
  pre-bash-timeout fix): plan reached `implementing` with **6
  requirements** before wedging on a separate `mvn test` timeout.
- Run B (commit dcdc5d1, post-bash-timeout fix, identical prompt):
  plan reached `requirements_generated` cleanly with **1 requirement**.
  Same provider, same model, same prompt, same workspace. No
  intervening changes to req-gen prompt or schema.

Both 1 and 6 are technically defensible decompositions of "implement
a Meshtastic driver":

- 1 req = "implement the entire driver" (treats it as one cohesive
  unit of work).
- 6 reqs = decomposed by component (driver class, connection,
  data parser, etc.).

The lesson is **don't pin a hard cardinality threshold in tests** —
`expect(reqs).toBeGreaterThanOrEqual(N)` for any N>1 will be flaky
across providers AND across runs of the same provider. We dropped
the `≥3` gate to `≥1` in `ui/e2e/epic-meshtastic-llm.spec.ts` so the
@hard scenario exercises the full pipeline (architecture → scenarios
→ execution) even when the model collapses scope into a single
requirement.

If a downstream observation suggests "this model under-decomposes
plans more than peer models," log it here as a model-quality finding
— not by tightening test gates. Tightening test gates only catches
the symptom on whichever side of the variance fires that day.

Followup hypothesis (not yet acted on): more capable models tend
toward finer-grained decomposition, which is intuitive but un-pinned
empirically. If we collect `requirement_count` over many runs per
model we can validate or refute it. Worth instrumenting before
investing in any req-gen prompt strengthening.

## Adding a new entry

When you take a model through a regression:

1. Add or update the section under "Findings by model" — name it with the
   provider plus the model identifier as it appears in the endpoint config.
2. Note date + semstreams version in the History subsection.
3. Reference watch-sidecar evidence_ids and post-mortem snapshot paths.
4. If you find a new failure shape, propose its bucket assignment in the
   taxonomy and link an agent-memory file (the durable record).
5. If your fix changed behavior, note WHICH fix and the delta. "Bumped retries
   from 2 to 3" is useful; "tried a few things" is not.

Resist the temptation to convert this doc into a benchmark scoreboard. The
goal is to capture *why* a model worked or didn't, not to rank them.
