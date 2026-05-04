# ADR-035 Audit — Silent-Coercion Site Enumeration

**Companion to:** [ADR-035: Strict-Parse Discipline — No Silent Compensation in LLM Output Handling](ADR-035-strict-parse-no-silent-compensation.md)
**Status:** In progress (audit only — no code changes)
**Date opened:** 2026-05-03
**Scope:** Step 2 of the ADR-035 sequencing plan. Enumerate every place we
silently transform LLM output today, attach a disposition for each, and
defer reviewer-verdict surfaces to the gated workstream defined in ADR-035
constraint #5.

## How to read this document

Each entry has:

- **Site** — `path/to/file.go:line` plus the function name.
- **Tolerates** — what shape the code silently fixes today.
- **Callers depend on** — the downstream consumers that would observe a
  behavior change if the site flipped strict.
- **Disposition** — one of:
  - **`keep+named-quirk`** — tolerance is correct for a documented
    shape (or under-specified-field default) that is universally
    accepted across models. The transform is a named, reviewed
    function in a small fixed list; idempotent (no-op when the shape
    isn't present); **and every fire emits a `parse.incident` triple
    per ADR §3 so operators see when a model is producing output that
    needs fixing.** No per-model gating — universal apply, universal
    telemetry.
  - **`reject+hint`** — flip strict; emit a `RETRY HINT:` to the loop on
    rejection so the next iteration learns. CP-1 or CP-2 per ADR §2.
  - **`reject+fatal`** — flip strict; rejection terminates the loop with
    no retry hint. Reserved for cases where a retry hint cannot help
    (programming-error or already-escalated paths).
  - **`defer`** — reviewer-verdict surface; ADR §5 gates these as a
    separate workstream and explicitly forbids touching them in the
    general audit. Listed for inventory, not for disposition.

### Why a universal named-quirks list (not per-model)

The ADR's original framing was per-model `output_quirks: [...]` config
entries. Reviewing the actual risk surface, that's heavier than the
discipline requires:

- The load-bearing hedge-laundering risk (ADR §5) lives at greedy
  prose-to-JSON extraction (A.2) and cross-field verdict synthesis
  (Layer E NormalizeVerdict). Neither shape comes from "strip a
  fenced code block" or "remove trailing commas."
- Shape quirks (fenced JSON, JS line comments, trailing commas) are
  idempotent and observed across every chat-tuned model in the registry.
  Stripping them universally is no riskier than stripping them per
  model, and no model in the registry today has a quirk so dangerous
  it shouldn't apply to other models.
- The two things per-model gating buys are (a) "model X stopped
  needing quirk Y" telemetry and (b) ability to scope a risky quirk
  to one model. (a) is already delivered by emitting an incident
  triple every time a strip fires — operators can grep the SKG for
  per-model fire rates without a config schema. (b) applies to no
  quirk on the list today.

The "loud" property is non-negotiable: every named-quirk fire emits
an incident triple. Silent compensation is what we're moving away
from — replacing it with permissive-but-noisy compensation on a
fixed list is the safe middle. A model that suddenly stops needing
a quirk produces zero fires and the absence is observable; a model
that suddenly starts needing a new quirk produces an unrecognized-
shape rejection (CP-1) until the new quirk is added to the list,
which is reviewed code.

## Sequencing observation

Disposition order within an eventual implementation should be:

1. **`reject+hint` sites first** on non-reviewer surfaces. These are the
   audit's load-bearing flips — they prove the retry-hint pipeline (ADR
   step 3) on real LLM traffic before the discipline touches anything
   verdict-shaped.
2. **`keep+named-quirk` sites second**. The named-quirks list is just
   reviewed Go code — a small fixed enumeration of strip functions plus
   the incident-emit wrapper. No config schema, no registry plumbing,
   no per-model lookup. Mechanical once the incident-emit interface
   is wired.
3. **`reject+fatal` sites third**. Smallest blast radius — they are
   already failure paths, the change is upgrading the failure mode from
   "silent zero value" to "explicit incident".
4. **`defer` sites last**, only after the general discipline is stable
   on the non-reviewer surfaces, and only via the ADR §5 reviewer audit.

---

## Layer A — `workflow/jsonutil/jsonutil.go` (the coercion engine)

**Note on path:** ADR-035's implementation-notes sketch references
`pkg/jsonutil/extract.go`. The actual location is
`workflow/jsonutil/jsonutil.go`. Update the ADR's path reference when
this audit lands or when the package moves.

The package documents itself as "intentionally permissive — strict callers
should validate after parsing." It stacks four silent transformations
inside a single `ExtractJSON()` call. The package is not the disposition
target itself — its callers are — but knowing what it does informs every
caller-side decision.

### A.1 Markdown fence stripping

- **Site:** `workflow/jsonutil/jsonutil.go:24` (`jsonBlockPattern`),
  invoked by `extractRawJSON` (line 76).
- **Tolerates:** ` ```json\n{...}\n``` ` and ` ```{...}``` ` wrappers.
- **Callers depend on:** every caller — none today is asked to opt out.
- **Disposition: `keep+named-quirk`** — `fenced_json_wrapper`. This
  is the canonical universal shape quirk (most chat-tuned models emit
  fenced JSON). Lift to the named-quirks list with a reviewed strip
  function; every fire emits a `parse.incident` triple keyed on
  `(role, model, prompt_version, quirk:fenced_json_wrapper)` so prompt
  regressions become queryable.

### A.2 Greedy `{...}` fallback in prose

- **Site:** `workflow/jsonutil/jsonutil.go:26` (`jsonObjectPattern`),
  invoked at `extractRawJSON:82`.
- **Tolerates:** prose like `"Here is the JSON: {...} hope this helps"`.
- **Callers depend on:** none demonstrably — every caller already has a
  schema to validate against, and every model that emits prose-prefixed
  JSON also emits fenced JSON when prompted. This fallback's job is to
  rescue weakly-prompted older models.
- **Disposition rationale:** this is the path most likely to cause
  hedge-style silent compensation downstream. A reviewer that emits
  "I think probably reject because…" with a `{...}` fragment of intent
  buried in the middle gets parsed as if the fragment were the answer.
  Removing it removes the surface for that defect. Worst case is a
  CP-1 rejection on legacy prompts, fixable by adding the fence to the
  prompt.

### A.3 `cleanJSON` (strip // comments, strip trailing commas)

- **Site:** `workflow/jsonutil/jsonutil.go:90` (`cleanJSON`).
- **Tolerates:** JS-style line comments inside JSON, trailing commas
  before `]`/`}`.
- **Callers depend on:** observed in real responses (see
  `TestExtractJSON/JS_comments_in_values` at line 32 and
  `complex_real-world_response` at line 51 of `jsonutil_test.go` —
  both are real captured outputs, not synthetic).
- **Disposition: `keep+named-quirk`** — two named quirks,
  `js_line_comments` and `trailing_commas`. Both are idempotent
  shape transforms (no-op when not present). Both fire telemetry on
  apply. Splitting them rather than bundling preserves the
  prompt-regression signal — a model that stops needing one quirk
  but keeps needing the other should produce visibly different
  fire-rate patterns.

### A.4 `trimToBalancedJSON` (discard trailing content)

- **Site:** `workflow/jsonutil/jsonutil.go:108`.
- **Tolerates:** Go 1.25+'s rejection of trailing content after a
  top-level JSON value. Prose appended after the JSON is silently
  truncated.
- **Callers depend on:** the Go 1.25 regression case is a real
  fixture (`trailing_backticks_(Go_1.25_regression)` test). The
  trailing prose case overlaps with A.2 — a model emitting `{...}\n\nSome
  more thoughts` would be sliced here regardless of A.2.
- **Disposition rationale:** keep as a Go-version-safety wrapper
  inside the strict path. It's not LLM-output tolerance, it's stdlib
  compat. Move out of the "tolerance" framing and into a
  `parseStrict(content, quirks []string)` contract where this trimming
  always happens last and is documented as a stdlib workaround.

---

## Layer B — `jsonutil` callers (6 sites)

### B.1 `processor/execution-manager/component.go:634` — developer submit_work post-loop parse

- **Tolerates:** all four jsonutil layers; any further failure routes
  to `routeFixableRejection`.
- **Callers depend on:** `exec.FilesModified`, `exec.DeveloperOutput`,
  `exec.DeveloperLLMRequestIDs`. Hard-fail on parse error already
  exists at line 648.
- **Disposition: `reject+hint`** (already does this — strengthen by
  cutting A.2 reliance and routing A.1/A.3 through the named-quirks
  list with telemetry). The routeFixableRejection at line 656 already
  constructs explicit feedback ("Your previous attempt ended without
  calling submit_work"). This site is the closest to ADR-035's target
  shape today; it just needs to inherit the loud-on-A.2 discipline so
  prose-with-buried-JSON becomes a rejection rather than a salvage.

### B.2 `processor/execution-manager/component.go:821` — `parseCodeReviewResult` (code reviewer verdict)

- **Disposition: `defer`** per ADR-035 constraint #5. Listed in
  Layer E for inventory.

### B.3 `processor/lesson-decomposer/result.go:45` — `parseDecomposerResult`

- **Tolerates:** all four jsonutil layers; rejection bubbles up as a
  Go error.
- **Callers depend on:** `buildLesson` (file:68) which has its own
  validation requiring summary + detail + injection_form + at least
  one evidence pointer. The caller chain DOES NOT inject a retry hint
  to the loop; failure is logged and the lesson is dropped.
- **Disposition: `reject+hint`**. The decomposer is best-effort today
  ("never blocks the rejection flow" per the dispatch comment in
  execution-manager:810), so retry-hint plumbing here is a smaller
  blast radius than B.1. Good first surface to validate the CP-1
  retry-hint mechanism on. The hint construction is straightforward:
  if A.2 fired (no fenced JSON found), tell the model to wrap its
  output in ` ```json ... ``` `; if `buildLesson` rejects on missing
  evidence, repeat the evidence requirement to the loop.

### B.4 `processor/requirement-executor/component.go:522` — decomposer DAG parse

- **Tolerates:** all four jsonutil layers; explicit rejection at
  `dagJSON == ""`, on unmarshal error, and on `dag.Validate()`
  failure. Error message routes to `retryOrFailDecomposerLocked`
  which is the existing retry-hint channel.
- **Callers depend on:** `dagResponse.DAG` for downstream node
  dispatch. Coverage gate at line 548 adds further validation.
- **Disposition: `reject+hint`** (already substantively does this —
  inherits the named-quirks list + telemetry wiring like B.1). This
  is the cleanest example of the target shape in the codebase today.

### B.5 `processor/requirement-executor/component.go:725` — node result silent zero-value

- **Tolerates:** **all parse failures silently** — the `if err == nil`
  block at line 725 means a malformed developer node result produces
  a `NodeResult{}` with zero `FilesModified`, zero `Summary`, zero
  `CommitSHA`. No log, no rejection, no incident.
- **Callers depend on:** `exec.NodeResults` aggregation at line 731,
  later used to construct the requirement-level `wfResult` at line 734.
  Empty FilesModified silently propagates through `sendReqNode` at 739.
- **Disposition: `reject+hint`** — this is the highest-risk silent
  coercion in the audit. Failure mode: a partial-DAG execution where a
  node "completed" but its files-modified list never reached the
  requirement aggregator. The downstream review then judges work that
  the executor doesn't know about; on rejection the retry feedback
  can't cite the missing files because the executor never recorded
  them. The fix is small (route through the same retry channel as B.4)
  but the wedge it forecloses is real and silent.
- **Incidental finding:** this is an existing latent bug independent
  of ADR-035. Worth shipping the rejection routing before the broader
  audit-then-flip sequencing — it does not need the model-quirks list
  to land first because it's already accepting whatever jsonutil
  emits, including the empty string.

### B.6 `processor/requirement-executor/component.go:1248` — requirement reviewer verdict

- **Disposition: `defer`** per ADR-035 constraint #5. Listed in
  Layer E for inventory.

---

## Layer C — Parallel parse implementations not using `jsonutil`

These bypass `workflow/jsonutil` entirely and reimplement fence-stripping
or brace-matching inline. The audit treats them the same as Layer B for
disposition — silent compensation is silent compensation regardless of
whether the regex lives in `jsonutil` or inline.

### C.1 `processor/plan-reviewer/component.go:566` — `parseReviewFromResult`

- **Disposition: `defer`** per ADR-035 constraint #5. **Critical
  inventory note:** this site combines parse-recovery with a
  `NormalizeVerdict()` call (lines 576, 614) that derives verdict
  from findings via cross-field logic in
  `workflow/plan_review_result.go:64`. NormalizeVerdict will upgrade
  `approved` → `needs_changes` when findings contain errors AND
  downgrade `needs_changes` → `approved` when no error findings exist.
  This is the canonical hedge-laundering surface ADR-035 §5 names
  ("the parser has become the place where ambiguity gets laundered
  into structure"). Listed in Layer E for the deferred reviewer
  audit.

### C.2 `processor/qa-reviewer/component.go:952` — `parseQAReviewResult`

- **Disposition: `defer`** per ADR-035 constraint #5. Same shape as
  C.1 minus NormalizeVerdict (verdict accepted as-emitted after
  `isValidQAVerdict`). Listed in Layer E.

### C.3 `processor/planner/component.go:699` — `parsePlanFromResult` + local `extractJSON`

- **Tolerates:** local `extractJSON` (file:728) reimplements
  fence/brace matching with no `cleanJSON` step. Strict on `Goal`
  field (line 720) — empty goal rejects.
- **Callers depend on:** `PlanContent` for plan-mutation publish.
  Submit_work-time validator at `tools/terminal/validators.go:51`
  (`ValidatePlanDeliverable`) already covers the shape requirement
  before this site runs, so this is a redundant defense layer.
- **Disposition: `reject+hint`**. Replace local `extractJSON` with
  the strict `jsonutil.ParseStrict(content, modelQuirks)` API once
  Layer A's shape lands. The redundant validation here is fine —
  both layers reject loudly today, neither silently compensates.
  Migration is mechanical.

---

## Layer D — Tool executors (CP-2 surface)

Per ADR §6, CP-2 lives per-tool in `tools/<tool>/executor.go`. This is
the existing argument-validation layer; the audit asks whether each
tolerates a class of malformed input that a `RETRY HINT:` could fix.

### D.1 `tools/bash/executor.go:87-93` — `command` argument

- **Tolerates:** nothing — empty/missing command rejects with
  `"command argument is required"` returned as `ToolResult.Error`.
- **Disposition:** **already strict.** No change. Reference shape
  for what the rest of the tool surface should look like.

### D.2 `tools/decompose/executor.go:37-54` — `decompose_task`

- **Tolerates:** nothing — `goal` empty rejects, `nodes` missing
  rejects, each node's required fields validated (`parseNodes`
  file:135), `dag.Validate()` runs.
- **Disposition:** **already strict.** Strongest CP-2 shape in the
  codebase. Other tool executors should match it.

### D.3 `tools/httptool/executor.go:225-235` — HTML conversion fallback

- **Tolerates:** `e.converter.Convert(body, rawURL)` failure — silently
  returns the raw body to the agent without indicating that
  Readability conversion failed. The agent thinks it received a
  curated `summary`/`markdown` view; it actually received unprocessed
  HTML.
- **Callers depend on:** the agent reading the response and reasoning
  about it. There is no schema here — the LLM consumes natural-language
  output. This means CP-2 isn't quite the right framing; the issue is
  closer to "tool result lies about its format."
- **Disposition: `reject+hint`** at the tool boundary. Construct the
  result with an explicit prefix ("⚠ Readability conversion failed —
  raw body follows.") so the agent knows the format contract was not
  met. This isn't the strict-parse discipline proper; it's the same
  underlying principle (don't silently lie about what was returned).
  Out-of-scope for the ADR-035 first pass — recommend tracking as a
  separate ticket and proceeding with the rest of the audit.
- **Incidental finding:** worth tracking outside the ADR-035 scope.

### D.4 `tools/question/executor.go:103-117` — `ask_question`

- **Tolerates:** empty topic defaults to `"general"` (line 110-112).
  This is a UX default, not a coercion — the topic field has no
  semantic gate to silently bypass.
- **Disposition:** **already strict on what matters.** Empty
  question rejects. Topic default is fine. No change.

### D.5 `tools/terminal/executor.go:65-105` — `submit_work` (terminal tool)

- **Tolerates:** nothing — empty args rejects with deliverable-type
  hint (file:71). Validator dispatched via
  `GetDeliverableValidator(deliverableType)` at file:81; rejection
  surfaces as `ToolResult.Error` with the validator's message —
  semstreams' agentic loop loops the model with that error message.
  This IS the retry-hint mechanism CP-2 calls for, already wired.
- **Disposition:** **already strict at the terminal-tool boundary.**
  The audit's interesting question is whether the per-deliverable
  validators are correctly strict (see D.6).

### D.6 `tools/terminal/validators.go:165-169` — `ValidateReviewDeliverable` rejection_type auto-fill

- **Tolerates:** when `verdict == "rejected"` and `rejection_type` is
  absent, the validator MUTATES the deliverable in place to set
  `rejection_type = "fixable"` and accepts the submission (file:168).
- **Callers depend on:** `DispatchRetry`, lesson extraction,
  persistence — all read `rejection_type` from the same map and
  expect it populated. The author note on file:138-142 calls this
  "mutate-and-pass rather than reject-with-warning so the rest of
  the loop sees the corrected shape downstream."
- **Disposition: `keep+named-quirk`** — `review_missing_rejection_type`.
  Note this is a content-default, not a shape transform — the named
  quirks list intentionally covers both. The justification is the
  same: it's a deterministic, idempotent transform (no-op when
  `rejection_type` is present and valid), the default is conservative
  ("fixable" routes to retry; the alternative "restructure" terminates
  — fixable is the recoverable choice), and it's universal across
  models. Behavior is identical to today; what changes is loudness:
  every fire emits a `parse.incident` triple so operators can spot
  prompt regressions ("qwen3-coder-next still misses rejection_type
  after the persona patch") via SKG queries.
- **Note for reviewer-audit workstream:** this site lives at the
  CP-2 / tool-executor boundary, not at the post-loop verdict-extraction
  boundary, so it's NOT covered by ADR §5's deferral — it is in scope
  for the general audit. The verdict it touches is incidental; the
  field being fixed is structural.

### D.7 `tools/websearch/executor.go:65-71` — `web_search`

- **Tolerates:** empty `max_results` defaults to 5 (file:73-76).
  Same shape as D.4: a UX default, not a coercion.
- **Disposition:** **already strict.** No change.

### D.8 `tools/workflow/graph.go:96-117` — `graph_query` (no entity_id shape validation)

- **Tolerates:** the `query` argument is a free-form GraphQL string.
  When the model calls
  `{ entity(id: "semspec.semsou") { triples ... } }` with a
  truncated entity ID, the executor passes it through to the gateway
  unchanged; the gateway returns `not found:` with empty body. There
  is no boundary check on entity-ID shape, no edit-distance hint, no
  RETRY HINT.
- **Callers depend on:** the agent reading the gateway error and
  fixing the ID on retry. This fails empirically — see
  `project_graph_query_truncated_id_wedge_2026_05_03` (qwen3-moe
  cycle-1 retry, 3+ identical retries until iter=14 wedge).
- **Disposition: `reject+hint`** at the tool boundary. ADR-035 §6
  names this site explicitly: "graph_query (entity_id validation +
  edit-distance hint)." The fix shape is:
  1. Parse the GraphQL query enough to extract the `id:` argument.
  2. Validate against the entity-ID format (dot-separated
     `org.platform.kind.…` per the schema constants in `graphQLSchema`
     comment lines 324-331).
  3. On mismatch, run edit-distance against the
     `entitiesByPrefix("<truncation>", limit)` result and inject
     the closest match as a `RETRY HINT:` suffix on the
     `ToolResult.Error`.
- **Sequencing note:** this is the tool the prior session's wedge
  fired on. Shipping the gate here gives ADR-035 a real-LLM
  regression artefact in the first iteration of step 6 — same shape
  as the git-diff gate did for B.1.

---

## Layer E — Reviewer-verdict surfaces (DEFERRED per ADR-035 §5)

The following sites consume LLM output that includes a verdict-shaped
field. Per ADR-035 §5, these are explicitly OUT OF SCOPE for the general
audit's disposition. They are listed here as inventory only — the
deferred reviewer-audit workstream picks up from this list.

### E.1 `processor/execution-manager/component.go:821` — `parseCodeReviewResult`

- Code-reviewer verdict extraction. Uses `jsonutil.ExtractJSON`,
  validates via `phases.ValidateVerdict`, defaults to a synthetic
  rejected verdict on parse failure (file:825-828). Synthetic-verdict
  fallback is the kind of structural collapse ADR-035 §5 calls out.

### E.2 `processor/requirement-executor/component.go:1248` — requirement reviewer verdict

- Same shape as E.1 minus the synthetic-fallback (returns parseOK=false
  and routes to retry budget at file:1271).

### E.3 `processor/plan-reviewer/component.go:566` — `parseReviewFromResult` + `NormalizeVerdict`

- Highest-risk reviewer surface. Inline brace-walk parser (file:586-608)
  + `workflow.PlanReviewResult.NormalizeVerdict()` cross-field
  override (`workflow/plan_review_result.go:64`) that can flip
  `approved` ⇄ `needs_changes` based on findings content. Per
  ADR-035 §5: "I think probably rejected" prose collapsing to
  `verdict="rejected"` is the named hedge-laundering risk; this
  site's NormalizeVerdict goes further by overriding what the model
  said with what the findings list implies.

### E.4 `processor/qa-reviewer/component.go:952` — `parseQAReviewResult`

- Similar shape to E.3 but no `NormalizeVerdict` step. Inline
  brace-walk parser; verdict accepted as-emitted after
  `isValidQAVerdict` set-membership check.

### E.5 `tools/terminal/validators.go:143` — `ValidateReviewDeliverable` (D.6 cross-link)

- D.6's mutate-and-pass for missing `rejection_type`. Listed here as
  cross-reference: the deferred reviewer-audit workstream should know
  that one ValidateReviewDeliverable disposition (D.6) lands in the
  general audit, but the SAME function's verdict-validation logic
  (file:144-147) is upstream of E.1-E.4 and is therefore relevant to
  the reviewer audit too.

---

## Disposition summary

| Layer | Site | Disposition |
|---|---|---|
| A.1 | jsonutil markdown fence stripping | `keep+named-quirk` (`fenced_json_wrapper`) |
| A.2 | jsonutil greedy `{}` fallback | `reject+hint` (highest hedge-laundering risk in Layer A) |
| A.3 | jsonutil cleanJSON (comments, trailing commas) | `keep+named-quirk` (`js_line_comments`, `trailing_commas`) |
| A.4 | jsonutil trimToBalancedJSON (Go 1.25 compat) | keep as stdlib-compat (rename out of "tolerance") |
| B.1 | exec-mgr developer submit_work parse | `reject+hint` (already substantively does this) |
| B.2 | exec-mgr code-review parse | `defer` (E.1) |
| B.3 | lesson-decomposer parse | `reject+hint` (good first CP-1 surface) |
| B.4 | req-executor decomposer DAG parse | `reject+hint` (already substantively does this) |
| B.5 | req-executor node result | `reject+hint` (latent bug — ship before broader sequencing) |
| B.6 | req-executor reviewer parse | `defer` (E.2) |
| C.1 | plan-reviewer parseReviewFromResult | `defer` (E.3) |
| C.2 | qa-reviewer parseQAReviewResult | `defer` (E.4) |
| C.3 | planner parsePlanFromResult | `reject+hint` (mechanical migration to strict jsonutil) |
| D.1 | bash executor command arg | already strict |
| D.2 | decompose_task executor | already strict (reference shape) |
| D.3 | http_request HTML conversion fallback | `reject+hint` (out-of-scope for ADR-035 first pass; track separately) |
| D.4 | ask_question executor | already strict |
| D.5 | submit_work terminal executor | already strict (CP-2 retry-hint already wired) |
| D.6 | ValidateReviewDeliverable rejection_type auto-fill | `keep+named-quirk` (`review_missing_rejection_type`; content-default flavor) |
| D.7 | web_search executor | already strict |
| D.8 | graph_query no entity_id shape validation | `reject+hint` (ADR §6 names this; ship first in step 6) |
| E.1-E.5 | reviewer verdict extraction surfaces | `defer` (ADR-035 §5 separate workstream) |

## Recommended first-flip sequence for ADR-035 step 4

Given the dispositions above, the cheapest-to-validate first flip is:

1. **B.5** (req-executor node result silent zero-value) — already a bug,
   no model-quirks list dependency, exercises the retry-hint plumbing
   on a low-traffic surface. Real-LLM regression burden: minimal,
   covered by any execution-phase test.
2. **D.8** (graph_query entity_id shape validation) — has a real
   wedge to validate against (qwen3-moe cycle-1 retry,
   `project_graph_query_truncated_id_wedge_2026_05_03`). Real-LLM
   regression burden: same.
3. **B.3** (lesson-decomposer parse) — best-effort path means a missed
   quirk doesn't block the cascade. Good for shaking out the retry-hint
   format question (open item in ADR-035: typed `<retry_hint>` block
   vs. free-form prose).
4. **A.1, A.3, D.6** (`fenced_json_wrapper`, `js_line_comments`,
   `trailing_commas`, `review_missing_rejection_type` land as the
   first named-quirks list entries with their incident-emit wiring) —
   once 1-3 prove the retry-hint pipeline, the named-quirks list and
   its telemetry surface ship together. No config schema is needed;
   the list is reviewed code.
5. **C.3** (planner local extractJSON migration to strict jsonutil) —
   mechanical, lowest blast radius, validates the package-level API.
6. **A.2** (jsonutil greedy fallback) flipped strict — broader blast
   radius; should ride after the named-quirks list is stable and the
   telemetry surface is producing fire-rate data on the workflow
   models that need fences.

The reviewer audit (Layer E) starts only after step 6 is stable per
ADR-035 §5.

## Phase 1 — pre-triple named-quirks landing (deliberate staging)

The named-quirks list ships in two phases. This is a deliberate staging
of the audit's "every fire emits a `parse.incident` triple" requirement,
not a corner-cut. Calling it out here so the next reader doesn't read
the audit letter-strict and call us on a partial implementation.

### Phase 1 (shipping now): Prometheus counters + structured logs

The named quirks land with per-fire telemetry as `prometheus.CounterVec`
exposed at `/metrics` (port 9090, registered through semstreams'
`metric.MetricsRegistry`). Plus a per-fire structured log for
diagnostic windows:

| Quirk | Package | Metric | Log level |
|---|---|---|---|
| `fenced_json_wrapper` | `workflow/jsonutil` | `semspec_jsonutil_quirks_fired_total{quirk="fenced_json_wrapper"}` | Debug |
| `js_line_comments` | `workflow/jsonutil` | `semspec_jsonutil_quirks_fired_total{quirk="js_line_comments"}` | Debug |
| `trailing_commas` | `workflow/jsonutil` | `semspec_jsonutil_quirks_fired_total{quirk="trailing_commas"}` | Debug |
| `greedy_object_fallback` | `workflow/jsonutil` | `semspec_jsonutil_quirks_fired_total{quirk="greedy_object_fallback"}` | Debug |
| `review_missing_rejection_type` | `tools/terminal` | `semspec_review_missing_rejection_type_total` | Warn |

`workflow/jsonutil.RegisterMetrics(reg *metric.MetricsRegistry)` and
the equivalent `tools/terminal.RegisterMetrics` are called once during
process startup from `cmd/semspec/main.go` after the registry is
constructed. Without registration the counters still accumulate
in-memory (so tests and CLI tools work without metrics infra) — they
just don't surface at `/metrics`.

`Stats() map[QuirkID]int64` reads counter values via
`prometheus/client_golang/prometheus/testutil.ToFloat64` so debug
endpoints, Health, and tests can observe aggregate fire rates without
scraping `/metrics`. CounterVec children are pre-warmed at package
init for every known quirk so reads of unfired quirks return 0
instead of panicking.

`greedy_object_fallback` (audit site A.2) is observation-only — the
counter fires whenever the parser extracts JSON from prose-wrapped
input (no fence, but match strictly shorter than trimmed input) so we
can measure how often this hedge-laundering surface fires before
deciding whether to flip strict.

The Debug-level logs let operators grep per-fire detail in
diagnostic windows without flooding production logs (parse-shape
quirks fire on most LLM responses; Warn would be unusable). D.6's
`review_missing_rejection_type` is rarer and content-default — it
gets Warn so operators see every fire.

### Why no triples yet

Triple emission per ADR-035 §3 needs per-call context — `Role`,
`Model`, `PromptVersion`, `RawResponse`, the call_id — to populate the
partition keys in `vocabulary/observability/predicates.go`. Today
`workflow/jsonutil/ExtractJSON(string) string` is a pure utility called
in 6 places (audit Layer B); none pass that context. Wiring it would
force all 6 callers into a new signature in this commit.

`ParseStrict(raw string) ParseResult` (new in Phase 1) returns the list
of quirks that fired alongside the extracted JSON. Callers opt in
incrementally — the first caller to need triple emission migrates from
`ExtractJSON` to `ParseStrict` and writes triples with its own
per-call context.

### Phase 2 trigger

The first caller that wants CP-1 incident telemetry — likely a
reviewer or planner caller hitting a real-LLM regression — migrates
to `ParseStrict` and emits `parse.incident` triples using the
vocabulary predicates from commit `8ff175a`. Phase 2 is per-caller,
not all-at-once, because each caller has different per-call context
sources (some have prompt-version metadata, some don't).

The named-quirks list itself is the reviewable artifact this commit
produces. Triple plumbing is downstream of having the list to attribute
incidents to.

## Suggested ADR-035 amendments

The audit surfaced one framing simplification worth folding back into
the ADR before any code lands. The site-by-site dispositions don't
change; what changes is the shape of the tolerance mechanism.

1. **§1 "identity-based, not category-based"** — rephrase. The
   correct line is "named-shape, not freeform-recovery." Tolerance
   survives only on a fixed list of named, reviewed transforms, each
   idempotent and each emitting an incident triple on every fire.
   The "identity" the ADR cared about wasn't *which model* — it was
   *which named transform* is allowed. Today no quirk is so risky it
   needs per-model scoping, and the per-model gating buys nothing
   the SKG-emit pattern doesn't already deliver.
2. **§A.1 Implementation-notes sketch (endpoint quirks list)** —
   drop the `"openrouter-deepseek-r1": { "output_quirks": [...] }`
   config sketch. Replace with a Go enum + named-strip-function
   table, e.g.:

   ```go
   var namedQuirks = map[QuirkID]StripFunc{
       FencedJSONWrapper:  stripFencedJSON,
       JSLineComments:     stripJSLineComments,
       TrailingCommas:     stripTrailingCommas,
       ReviewMissingRejectionType: fillReviewRejectionType,
   }
   ```

   Adding a quirk = code review + new test fixture; no config schema
   to maintain. Apply universal, fire universal telemetry.
3. **§3 Incident retention** — the schema is already correct for the
   universal-list framing (incident node carries `incident.role`,
   `incident.model`, `incident.prompt_version` partition keys plus
   the quirk identifier). Note explicitly that `outcome` should
   include a `"strict-with-named-quirk"` value alongside `"strict"`
   and `"rejected"` — that's the loud-on-fix surface; it's a
   non-rejection outcome that nonetheless emits an incident triple.
4. **§4 Audit-then-flip sequencing** — step 2 ("audit existing
   tolerant paths") is THIS document. Step 4 sequencing was framed
   around per-checkpoint config knobs (`llm_strict_parse: { cp1, cp2 }`).
   With the universal-list framing, the rollback story simplifies:
   the named-quirks list itself is the rollback surface — adding a
   transform is a code change, removing one is a code change, and
   the per-checkpoint kill-switch is still useful but no longer
   carries the weight of "scope a per-model config rollback."
5. **Constraint #5 (reviewer hedge-laundering)** — unchanged.
   The deferral is correct regardless of framing.

## Open audit items

- **Generators not surfaced here:** `requirement-generator`,
  `scenario-generator`, `architecture-generator` post-loop result
  parsing. They go through the submit_work validator path
  (Layer D.5/D.6) so their CP-2 surface is already covered. If any
  of them have a separate post-loop parse step that wasn't found in
  this audit's grep sweep, append to Layer B.
- **Local Ollama-specific quirks:** ADR §1 cites DeepSeek-R1
  chain-of-thought prefix and OpenAI o1 thinking blocks as
  examples. Neither model is currently in the registry. When/if
  either lands, add the corresponding quirk strip-function
  alongside A.1's `fenced_json_wrapper`.
- **`workflow/answerer/` route extraction:** the Q&A registry
  parses route configs from JSON. That's config-time parsing of
  reviewed code, not LLM-output parsing — out of scope.
- **D.3 `http_request` conversion fallback:** flagged as
  out-of-scope but worth a follow-up ticket. The principle (don't
  silently lie about what was returned) is consistent with ADR-035
  but the surface is not LLM-output parsing per se.

## References

- [ADR-035: Strict-Parse Discipline — No Silent Compensation](ADR-035-strict-parse-no-silent-compensation.md)
- [ADR-034: Watch CLI and Diagnostic Bundles](ADR-034-watch-cli-and-diagnostic-bundles.md)
- `project_dev_wedge_diagnosis_2026_05_03` — CP-2 git-diff gate (B.1
  precedent)
- `project_graph_query_truncated_id_wedge_2026_05_03` — D.8 precedent
- `project_planner_dashed_paths_cascade_2026_05_03` — Layer E
  hedge-laundering precedent (NormalizeVerdict misfire over reviewer
  prose)
