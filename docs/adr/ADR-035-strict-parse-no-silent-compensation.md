# ADR-035: Strict-Parse Discipline — No Silent Compensation in LLM Output Handling

**Status:** Proposed
**Date:** 2026-05-03
**Authors:** Coby, Claude (Opus 4.7), Claude (4.7 1M)
**Depends on:** ADR-034 (watch CLI / detector library — same alert surface),
  vocabulary registry (`vocabulary/`), agentic-loop response-parse path
  (in semstreams), tool-executor argument validation surface
  (`tools/<tool>/executor.go`)

## Context

We have shipped a pre-reviewer git-diff gate (commit `40886e5`,
2026-05-03) that catches one specific class of LLM-output defect:
hallucinated `submit_work` claims where the developer agent reports
`files_modified=[main.go]` despite never having written to the worktree.
The gate works — caught a real OpenRouter qwen3-coder hallucination in 13ms
on cycle 0 of a real-LLM regression. But fixing the symptom raised the
deeper question:

**The same session surfaced two more wedge classes that share a structural
shape:**

1. `graph_query` called with truncated entity ID `semspec.semsou` (cut at
   14 chars from `semspec.local.semsource.code.workspace...`). graph-gateway
   returned `not found:`. Model didn't read the error. Repeated 3+ times
   until iter=14 wedge. Logged in `project_graph_query_truncated_id_wedge_2026_05_03`.
2. Planner emitted `scope.include=["main.go"]` for a file that didn't
   exist; should have been `scope.create`. Same shape as the cascade fix
   in commit `946628c`, recurring on a different model. Logged in
   `project_planner_dashed_paths_cascade_2026_05_03`.

In both cases the JSON was well-formed. The schema was satisfied. The
content was wrong.

Separately, we surveyed BAML's Schema-Aligned Parsing (SAP) approach —
edit-distance-based recovery from malformed LLM output, oracle-driven by
the target schema. BAML's philosophy is permissive: "be liberal in what
you accept." That works for BAML's audience (developers building one-shot
LLM applications). It fails for ours (a multi-agent system where parser
output drives plan-rejection / verdict / file-write decisions).

The danger BAML's defaults create — and that we have to bound — is
**silent compensation**:

- **Prompt regression goes undetected.** Someone reorders a fragment, the
  model starts emitting markdown-fenced JSON, the parser strips the fence,
  no test fails, no triple changes. Three weeks later something subtle
  breaks because the fields-by-position trick the parser used to recover
  happened to land wrong on one edge case.
- **Model regression goes undetected.** Upgrade `qwen3-coder` to a new
  version, structured-output adherence drops 8%, the parser papers over
  it, the metric never moves. Symptoms surface misattributed.
- **Hedge-laundering.** Reviewer prose says "I think this should
  probably be rejected, though…". A tolerant parser collapses the hedge
  into `verdict="rejected"`. A deterministic gate now branches on
  synthesized certainty. The parser has become the place where ambiguity
  gets laundered into structure, and it never shows up as a decision
  anyone made.

The third one is the load-bearing risk for our system specifically. We
are not BAML's audience — we're a system where a parser's "best-effort"
recovery becomes a verdict that escalates a plan or merges a worktree.
Goodhart on the parser, not on the model.

This ADR is a multi-session work item because the audit alone is
substantial (every place we currently coerce LLM output silently) and
flipping the default is a behavior change that needs a real-LLM
regression cycle per affected surface.

## Decision

Adopt **zero silent compensation** as the system-wide discipline for LLM
output handling. Strict-parse is the contract; tolerance is a defect that
sometimes ships, never normalized.

The discipline applies at two checkpoints in the loop:

| Checkpoint | What it checks | Behavior on failure |
|---|---|---|
| **CP-1: Response → typed value** | Does the LLM output parse to the declared schema? | Reject. Emit `RETRY HINT: <what was wrong>` into the next loop iteration. Retain raw response. Emit incident triple. |
| **CP-2: Typed value → tool call** | Does the typed value pass semantic validation against the tool's contract? | Reject. Emit `RETRY HINT: <what was wrong + closest valid alternatives>`. Retain claim + observation. Emit incident triple. |

Both reject loudly. Both teach the model on the next turn. Both are
visible to the loop, not just the operator.

Tolerance survives in exactly one place: a **per-(model, characterized-quirk)
allowlist** declared in the endpoint config. If a specific model has a
documented output quirk that's part of how it works — DeepSeek-R1's
chain-of-thought prefix, OpenAI o1's thinking blocks — we strip it as
part of the **model's contract**, not the parser's tolerance. Every other
shape is rejected. Adding to the allowlist is reviewed code, not a
runtime decision.

The decision rests on five load-bearing constraints. Removing any of them
collapses this back into "another tolerance option" and recreates the
silent-compensation risk.

### 1. The line is "explicit per-model contract" vs "everything else", not "shape vs semantics"

The earlier framing — silently fix shape, loudly reject semantics — does
not survive contact with hedge-laundering. Extracting `verdict="rejected"`
from prose is "shape" only if you squint; in practice it is a semantic
collapse dressed as parse recovery. The parser is the wrong place to draw
that line.

The correct line is **identity-based, not category-based**: a specific
known model produces a specific known output shape, declared in code,
reviewed when added. Anything else is a defect.

### 2. Rejection is loud to the loop, not just the operator

A triple emitted to the SKG is silent in the only place that matters: the
LLM iteration. The model gets "tool accepted" whether the parser fixed
its output or not, and the next iteration learns nothing.

Every CP-1 and CP-2 rejection injects a `RETRY HINT: <reason>` block into
the next prompt, prepended ahead of the user/system message. This reuses
the existing retry-feedback channel that fixable rejections already use
(`processor/execution-manager/component.go::routeFixableRejection`).
Without this property, the discipline is theatre — the operator sees the
triple, the model never learns, the wedge keeps happening.

### 3. Incident retention is graph-native, not separate metrics infrastructure

When CP-1 or CP-2 fires, the raw response and the rejection reason are
stored as a graph relation:

```
(call_id) -[parse.incident]-> (incident_node)
(incident_node) -[incident.checkpoint]-> "cp1" | "cp2"
(incident_node) -[incident.raw_response]-> <string>
(incident_node) -[incident.reason]-> <string>
(incident_node) -[incident.role]-> "planner" | "developer" | ...
(incident_node) -[incident.model]-> "openrouter-qwen3-moe" | ...
(incident_node) -[incident.prompt_version]-> "v3.2"
```

This makes parse incidents **first-class governance signal**, queryable
via the same `graph_query` tool the agents already use. qa-reviewer
(Murat) can ask "show me planner incidents from the last 7 days where
checkpoint=cp2" and get a real answer. Without graph retention, incidents
are dashboard-only and the analogical-reasoning path the system was built
on (lessons, role failure history, SNCO surfacing) doesn't see them.

### 4. Audit-then-flip sequencing, never flag-day

Today's parser surfaces silently coerce in several places:
`jsonutil.ExtractJSON` strips markdown fences, response-parser fallbacks
extract JSON-in-prose, tool-executor argument validators tolerate empty
objects on missing fields, etc. Each of these has a use case attached. We
do not know what we'd be turning off without auditing each.

Sequencing for any session that picks this up:

1. **Vocabulary predicates land first** (`llm.parse.checkpoint`,
   `llm.parse.incident`, etc.). Pure semantics, no behavior change.
2. **Audit existing tolerant paths** with a written list. Each entry
   becomes one of: (a) keep + add to model-quirk allowlist for the
   specific endpoints that need it; (b) reject + retry-hint; (c) reject +
   no retry-hint (fatal). The audit IS the work — without it, we don't
   know what we're flipping.
3. **Wire the retry-hint mechanism end-to-end** for response-parse
   rejections. Tool-error retry-hint already exists; extend it to
   CP-1.
4. **Flip strict-by-default per-surface**, gated with a config knob so a
   real-LLM regression that exposes a missed quirk is a knob-flip
   rollback, not a code revert.
5. **Watch-sidecar detector** on `(role, model, prompt_version,
   incident_rate)` aggregation. Reuses ADR-034's detector library. New
   detector: `IncidentRateExceeded`. Only useful AFTER incidents are
   being emitted, so it sequences after step 4.
6. **Apply CP-2 semantic validation per-tool**, starting with
   graph_query (`entity_id` validation + edit-distance hint) and planner
   submit_work (`scope.include` paths cross-checked against project
   tree). Each tool gets its own session and its own real-LLM
   regression.

### 5. Hedge-laundering audit is a separate, gated workstream

The single biggest risk of silent compensation in our system today is
verdict extraction from reviewer prose. Plan-reviewer, qa-reviewer, and
code-reviewer all consume model output that includes a verdict-shaped
field; some of those paths use `jsonutil.ExtractJSON` plus type assertion;
some allow free-text fallback. Under the current parser discipline, "I
think probably rejected" can collapse to `verdict="rejected"`.

Before changing any code in this area, audit every verdict-extraction
path. Document each. THEN propose the structural fix — likely require the
model to emit `verdict + confidence + hedge_phrase` as separate fields and
reject at CP-1 if `confidence < threshold` or `hedge_phrase != ""`. Do not
ship until the audit is done. The blast radius of a wrong fix here is
high — every reviewer in the system sees a shifted contract.

This is called out as a separate constraint because it's tempting to fold
it into step 4 of the general audit. Don't. The reviewer prompts are
co-designed with the parser's tolerance; changing both at once compounds
risk. Reviewer audit ships AFTER the general CP-1 discipline is stable
on the non-reviewer surfaces.

## Consequences

### Positive

- **Regression visibility**: prompt edits and model upgrades that degrade
  output adherence become detectable. Today they are not.
- **Hedge-laundering is preventable**: verdict extraction can be made
  contractually strict, so the parser stops being the place ambiguity
  gets laundered into certainty.
- **Governance signal grows**: parse incidents become queryable artifacts
  in the SKG. qa-reviewer and lesson-decomposer can reason over them.
- **Per-model quirks are explicit**: adding tolerance for a specific
  model's output shape becomes a reviewed config change, not a parser
  default that nobody owns.

### Negative / cost

- **Real-LLM regression cycles per affected surface.** Each surface needs
  validation that flipping strict didn't break something we depended on.
  Total wallclock cost spread across multiple sessions is non-trivial.
- **Mid-tier models will fail more loudly.** A model that today produces
  output the parser silently fixes will start emitting CP-1 rejections.
  Some of these will be legitimate model limitations, not regressions.
  The retry-hint discipline mitigates this but doesn't eliminate the
  cost — some wedges that today look like "the parser handled it" will
  surface as "this role + this model needs a stronger prompt."
- **Allowlist maintenance burden.** Every new model added to the
  registry needs its quirks characterized and listed. This is the
  intended cost — implicit tolerance is what we're moving away from —
  but it's a real ongoing tax.
- **Audit work is the bottleneck.** Step 2 (audit existing tolerant
  paths) is mechanical but boring and error-prone. Skipping or
  shortchanging it defeats the discipline.

### Neutral / boundaries

- **This does not change LLM-side fault tolerance.** Models that produce
  malformed output still get retries; the difference is the retry now
  carries actionable feedback instead of the parser silently fixing the
  output.
- **This does not deprecate `jsonutil.ExtractJSON`.** It survives, but
  becomes a strict-only utility plus an explicit tolerance layer keyed on
  the model's quirks list. The fence-stripping behavior moves from
  parser-default to model-config.
- **This is orthogonal to the gate fix already shipped.** The CP-2
  shape covers the git-diff gate's behavior — they're consistent, but
  this ADR doesn't redo the gate.

## Alternatives considered

### A. Adopt BAML's permissive default with operator-only telemetry

What it looks like: keep silent compensation, add a `tolerant-rate`
metric, alert when it crosses a threshold per (role, model,
prompt-version).

Why rejected: this was the prior framing in this session ("silently fix
shape, emit triple"). The trap is that the model never learns — the
telemetry is silent in the loop. Hedge-laundering is the dominant
surface in our system, and operator-only telemetry doesn't prevent the
parser from collapsing prose into verdicts. Goodhart's law applies to
the parser itself once tolerant-rate becomes a metric — the parser
becomes the place where ambiguity gets laundered into structure to keep
the metric green.

### B. Strict-parse with auto-retry inside the parser

What it looks like: parser fails → parser issues a corrective LLM call
internally to clean up the output → typed value returned, no
loop-iteration consumed.

Why rejected: hides the regression signal even more thoroughly than (A).
A single-step internal retry that succeeds on a slightly-better-shaped
prompt looks identical to the model getting it right the first time.
The model never sees a `RETRY HINT:` because the loop never knew there
was an error. This is silent compensation in even-more-disguised form.

### C. Status quo + per-bug fixes

What it looks like: keep doing what we did this session for the
git-diff gate — find a wedge, ship a targeted fix, move on.

Why rejected: hits a structural ceiling. The git-diff gate fixed
hallucinated `files_modified` but did nothing for graph_query truncation
or scope.include/create — both of which surfaced in the same run. Each
new wedge will require the same diagnose-fix-validate cycle, with no
underlying improvement to the system's ability to detect or report
parse-level degradation. We've burned ~12 sessions on five distinct
wedges in this class. This ADR's audit + flip is roughly the same total
cost but bounds the problem.

### D. Schema-aligned parsing as adopted from BAML, with our schemas

What it looks like: port BAML's edit-distance recovery algorithm,
target our protobuf-style schemas instead of TypeScript.

Why rejected: solves the wrong problem. BAML's algorithm is excellent at
recovering well-formed structure from approximately-formed JSON. Our
wedges are not approximately-formed JSON — they're well-formed JSON with
semantically wrong content. The cost-based recovery would cheerfully
parse `{"entity_id": "semspec.semsou"}` and hand us a typed value with
a wrong ID, exactly as it does today.

## Implementation notes (sketch, not contract)

The implementation will span several sessions. This section outlines the
file/code anchors so future-us doesn't have to re-discover them.

**CP-1 lives in semstreams**, in agentic-loop's response-parse path. We
do not own that code; we own the configuration surface (endpoint
quirks list) and the contract semstreams parses against. This ADR's CP-1
discipline likely lands as a semstreams ask: "stop calling tolerant
recovery silently; expose strict + tolerance-by-explicit-quirks-list."

**CP-2 lives in semspec**, in `tools/<tool>/executor.go` per tool. Each
tool's executor gains a `validateArgs(ctx, args) (validated, *RejectionHint)`
step before dispatching. Hint construction is per-tool — graph_query
constructs hints from the alias-index; planner submit_work constructs
hints from the project file tree.

**Vocabulary predicates** belong in `vocabulary/observability.go` (new)
or extend an existing observability vocabulary file. Predicates needed
on day one:

- `llm.parse.checkpoint` — string `"cp1"` or `"cp2"`
- `llm.parse.outcome` — string `"strict"` or `"rejected"`
- `llm.parse.incident` — relation predicate, points to incident node
- `llm.parse.raw_response` — string
- `llm.parse.reason` — string
- `llm.parse.role`, `llm.parse.model`, `llm.parse.prompt_version` —
  partition keys

**Watch-sidecar detector** belongs in `pkg/health/detector_incidentrate.go`,
following the existing `detector_repeattoolfailure.go` shape. Aggregation
window: 30 minutes rolling. Threshold: configurable per role, default 5%
on workflow models / 30% on classifier models.

**Endpoint quirks list** extends the existing endpoint config schema.
Likely shape:

```json
"openrouter-deepseek-r1": {
  "...existing fields...": "...",
  "output_quirks": ["chain_of_thought_prefix", "fenced_json_wrapper"]
}
```

Each quirk name maps to a named, reviewed strip function in the parser.
No string-based runtime customization — the quirk has to be a known type.

## Rollback story

The discipline ships behind a per-checkpoint config knob:
`llm_strict_parse: { cp1: true|false, cp2: true|false }`. Default `true`
once steps 1-4 of the audit are complete. Knob flip restores the prior
silent-compensation behavior on the affected checkpoint. Rollback for a
single bad surface is a single-knob flip, not a revert.

The model-quirks allowlist is additive. Adding a quirk is a config
change. No code revert is ever needed to handle a real-LLM model that
produces an unanticipated shape — operator adds the quirk, files an
incident retroactively, ships a code change to characterize the quirk
properly.

## Open items (not blocking acceptance)

- **CP-1 retry-hint format**: should the hint be free-form prose, or a
  typed `<retry_hint>` block? Lean toward typed; finalize during step 3.
- **Incident retention SLA**: how long do incident nodes live? Plan-phase
  incidents are useful for weeks; per-call incidents may not need that
  retention. Likely tier by severity. Defer until we see real volume.
- **qa-reviewer (Murat) integration**: should incident rate enter the
  release-readiness verdict? Probably yes for plan-phase roles. Defer
  until incidents are flowing.
- **Reviewer audit shape**: deliberately deferred per constraint #5.
  Tracked as a follow-up ADR or a sub-section of step 6 above.

## References

- BAML's Schema-Aligned Parsing — boundaryml.com/blog/schema-aligned-parsing
- ADR-034 (watch CLI / detector library) — same alert and detector surface
- `feedback_failure_mode_taxonomy.md` (agent memory) — the 5-bucket
  failure taxonomy this discipline operates over
- `feedback_retries_must_inject_failure_context.md` (agent memory) —
  load-bearing retry discipline this ADR extends to parse-rejections
- `project_dev_wedge_diagnosis_2026_05_03` — hallucinated `submit_work`
  wedge (CP-2 shape)
- `project_graph_query_truncated_id_wedge_2026_05_03` — `graph_query`
  semantic wedge (CP-2 shape)
- `project_planner_dashed_paths_cascade_2026_05_03` — `scope.include` vs
  `scope.create` wedge (CP-2 shape)
- `docs/model-testing-findings.md` — empirical evidence per model that
  motivated this ADR
- commit `40886e5` — pre-reviewer git-diff gate, the first CP-2 surface
  in the codebase (predates this ADR but is consistent with it)
