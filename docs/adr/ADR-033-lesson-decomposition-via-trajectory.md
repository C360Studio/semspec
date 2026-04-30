# ADR-033: Lesson Decomposition via Trajectory Introspection

**Status:** Proposed
**Date:** 2026-04-28
**Authors:** Coby, Claude
**Depends on:** Lessons Learned System (replaces ADR-027), ADR-029 (review retry)

## Context

The role-scoped lessons system (replaces ADR-027) classifies reviewer feedback into pre-defined error categories (`configs/error_categories.json`) and injects the top-N most recent lessons into each role's prompt. The mechanism works structurally — lessons land, prompts grow, errors are flagged — but the feedback loop has two Goodhart-shaped failure modes that we are starting to see in long-running runs:

1. **The lesson and its label come from the same compressed view.** `Summary` is `truncateInsight(feedback, 200)` (`processor/execution-manager/team_knowledge.go:36`); `CategoryIDs` come from substring matching against the same feedback string (`MatchSignals`). There is no second pass that asks "what *actually* went wrong here." The category is whatever keyword the reviewer happened to use.

2. **Lessons inject as commandments, not case studies.** The current prompt fragment (`prompt/domain/software.go:946-965`) renders `[AVOID][role] {summary} GUIDANCE: {static text from JSON}`. The model reads this as a target ("address `missing_tests`") and satisfies the surface signal (write `func TestStub(t *testing.T) {}`) without addressing the underlying defect. The static guidance is identical across every lesson sharing a category, which compounds the cargo-cult effect.

Additional structural gaps:

- **No evidence pointer.** The lesson does not cite the trajectory step or the file:line where the failure manifested. There is no way to audit a lesson, no way to retire it when the underlying code is rewritten, no way for the consuming model to ground the warning in something concrete.
- **Negative-only.** Approvals do not produce lessons. Models receive a steady diet of "avoid" with no positive examples, encouraging defensive padding rather than pattern adoption.
- **All blame routes to the proximate role.** A developer rejection is often caused by an upstream defect — ambiguous AC, missing component decision, vague scenario. Today the lesson is filed against `developer` regardless of root cause.
- **No retirement.** Lessons accumulate indefinitely. A lesson from five weeks ago about a code path that has since been rewritten still ships in every developer prompt.
- **Consumer-side wiring is partially broken.** Three latent bugs in the injection path mean lessons don't reach their intended consumers correctly even before the decomposer ships. These are documented as Phase 0 prerequisites — see Migration below. Building the decomposer on top of broken injection would mask its quality gains in tests.

The infrastructure to solve all of this is already paid for: every `taskExecution` carries `TraceID` and `LoopID` (`execution_store.go:386,444`), and semstreams exposes `GET /trajectories/{loopId}` returning the full step list (tool calls, model outputs, errors). `requirement-executor` already attaches `ArtifactRef{Type:"trajectory"}` to exhaustion `PlanDecision`s. Nothing in the lessons pipeline reads it back.

## Decision

Introduce a **lesson decomposer** that runs after every reviewer rejection (and selected approvals), reads the trajectory + worktree state + originating scenario, and emits structured lessons with mandatory evidence citations. Replace the current commandment-style injection with relevance-ranked case studies.

The decision rests on four load-bearing constraints. Removing any of them collapses this design back into the current system with extra fields — exactly the trap to avoid.

### 1. Mandatory evidence citations (Goodhart-defense, structural)

Every lesson must carry at least one of:

- `evidence_steps`: list of `{loop_id, step_index}` tuples pointing into the trajectory.
- `evidence_files`: list of `{path, line_range}` pointing into the failing worktree.

A lesson without an evidence pointer is rejected by the writer (`workflow/lessons/writer.go`) and never reaches the graph. This is non-negotiable. It makes fabrication expensive (the decomposer cannot hand-wave), audit cheap (a reviewer can click through to the cited step), and retirement automatic (when the cited file no longer exists or has been rewritten past recognition, the lesson retires itself).

### 2. Storage/injection split (cost-defense, structural)

`workflow.Lesson` gains two distinct text fields:

- `detail`: long-form root-cause narrative for audit and human review. May be 1KB+. Stored, never injected verbatim.
- `injection_form`: compressed case-study text, hard-capped at **80 tokens**. Generated once at decompose time. This is what gets rendered into prompts.

Decoupling these means the lesson table can grow without inflating the prompt budget. Per-prompt cost stays bounded by `K × 80` regardless of how rich the stored lesson becomes.

### 3. Top-K by relevance, not recency (cost-defense, structural)

The consumer (`software.shared.team-knowledge`) selects lessons by similarity to the *current scenario*, not by `CreatedAt DESC`. Concrete signal: overlap between the lesson's `evidence_files` and the scenario's `scope`/AC files; embedding similarity as a fallback when file overlap is empty.

Default `K = 3` for developer, `K = 2` for upstream roles. With `injection_form ≤ 80t`, this keeps the team-knowledge fragment under ~250 tokens — *cheaper than today's ~650-token recency injection*.

### 4. Render as case studies, not commandments (Goodhart-defense, prompt-side)

Replace the current `[AVOID]` template with a narrative form:

```
Past incident on scenario {scenario_id}: agent at {trajectory_step_ref} did
{action_summary}; reviewer rejected because {root_cause}. Resolution landed
in {evidence_file_ref}. Before submitting, consider whether your current
plan risks the same failure mode.
```

The model is asked to *reason about an analogy* rather than satisfy a category token. The `category_ids` field still exists but is used for routing and metrics only — it is not rendered into the prompt.

### 5. Cross-role attribution

The decomposer assigns `root_cause_role` based on trajectory analysis, not the role that hit the rejection. If the trajectory shows the developer agent reading an AC that is internally contradictory or missing a component decision, the lesson is filed against `scenario-generator` or `architect`, not `developer`. This breaks the current pattern where developer eats every team's debt.

The proximate role still receives a copy of the lesson (because the failure manifested on their watch and may need their attention next time), but the upstream role gets the *primary* attribution and consequence in their team-knowledge.

### 6. Positive lessons from approved-on-first-pass trajectories

When a reviewer approves on first attempt with rating ≥ 4 (semdragon-style review tool already supports this), the decomposer runs against the *successful* trajectory and emits a `positive: true` lesson with the same evidence-citation requirement. These render as `BEST PRACTICE` rather than `AVOID` in the team-knowledge fragment and counter-balance the negative-only diet.

### 7. Retirement loop

A periodic sweep (cron or KV-watch on entity updates) re-evaluates existing lessons:

- If `evidence_files` no longer exist on disk → mark `retired_at`.
- If the cited file:line has been substantially rewritten (semsource diff vs. the commit cited in the lesson) → mark `retired_at`.
- If the lesson has not been selected for injection in N weeks → mark `retired_at` (low-relevance pruning).

Retired lessons stay in the graph for audit but are excluded from `ListLessonsForRole`. Without this loop, the system rots into folklore over a quarter; the decomposer's quality gains evaporate.

## Architecture

```
┌──────────────────┐
│  reviewer        │
│  (verdict +      │──── rejected/approved+rating ──┐
│   feedback)      │                                │
└──────────────────┘                                ▼
                                          ┌──────────────────┐
┌──────────────────┐                      │ lesson-decomposer│
│  /trajectories/  │── trajectory ───────▶│  (LLM call)      │
│  {loop_id}       │                      │                  │
└──────────────────┘                      │ inputs:          │
                                          │  - trajectory    │
┌──────────────────┐                      │  - verdict       │
│  worktree HEAD   │── diff ─────────────▶│  - feedback      │
│  (or merge-base) │                      │  - scenario AC   │
└──────────────────┘                      │  - existing      │
                                          │    lessons       │
┌──────────────────┐                      │                  │
│  scenario AC +   │── context ──────────▶│ output:          │
│  plan + arch     │                      │  Lesson{         │
└──────────────────┘                      │   detail,        │
                                          │   injection_form,│
                                          │   evidence_*,    │
                                          │   root_cause_role│
                                          │   positive,      │
                                          │   category_ids}  │
                                          └────────┬─────────┘
                                                   │
                                                   ▼
                                          ┌──────────────────┐
                                          │ lessons.Writer   │
                                          │ (rejects lesson  │
                                          │  if no evidence) │
                                          └────────┬─────────┘
                                                   │
                                                   ▼
                                          ┌──────────────────┐
                                          │ graph (lesson    │
                                          │  triples + KV)   │
                                          └──────────────────┘
```

## Schema changes

`workflow.Lesson` gains:

```go
type Lesson struct {
    // existing
    ID          string
    Source      string
    ScenarioID  string
    Summary     string      // keep — short title
    CategoryIDs []string    // routing/metrics only, not rendered
    Role        string      // proximate role (where failure surfaced)
    CreatedAt   time.Time

    // new
    Detail          string         // long-form narrative; storage only
    InjectionForm   string         // ≤80 tokens; what gets rendered
    EvidenceSteps   []StepRef      // {loop_id, step_index}
    EvidenceFiles   []FileRef      // {path, line_range, commit_sha}
    RootCauseRole   string         // may differ from Role
    Positive        bool           // best practice vs. avoid
    RetiredAt       *time.Time     // nil until retirement sweep marks it
    LastInjectedAt  *time.Time     // for low-relevance pruning
}
```

New predicates in `agentgraph` to match (`PredicateLessonDetail`, `PredicateLessonInjectionForm`, etc.).

## Component changes

**New:** `processor/lesson-decomposer/`
- Watches `EXECUTION_STATES` for terminal verdicts (rejected, or approved-first-try with rating ≥ 4).
- Fetches trajectory via semstreams `/trajectories/{loop_id}` API.
- Fetches worktree diff (sandbox if available, else last-known via execution entity).
- Runs a single LLM call (decomposer persona — Opus/Sonnet-grade regardless of executing model, since this is rare and benefits from intelligence).
- Emits `Lesson` via `lessons.Writer.RecordLesson`.

**Modified:** `workflow/lessons/writer.go`
- `RecordLesson` rejects lessons without evidence pointers.
- New `RetireLesson(id, reason)` for the retirement sweep.
- `ListLessonsForRole` accepts an optional `relevance hint` (scenario ID + scope files) and ranks accordingly.

**Modified:** `prompt/domain/software.go` — `software.shared.team-knowledge` fragment renders `injection_form`, falls back to `Summary` only for legacy lessons without one. Hard cap: 5 lessons per role per prompt.

**Modified:** `processor/execution-manager/team_knowledge.go` — replaced. The post-rejection hook now publishes a `lesson.decompose.requested` event instead of recording a lesson directly. Lessons are emitted only by the decomposer.

**Same for:** `processor/plan-reviewer/component.go:618-669` (`extractPlanLessons`), `processor/qa-reviewer/component.go:677-689` (`recordQARejectionLesson`).

**New:** retirement sweep — small periodic component (or rule-processor) that walks lesson entities and applies the three retirement criteria.

## Cost analysis

### Per-prompt injection cost (the long-running budget)

| Variant | Per-lesson | Count | Total |
|---|---|---|---|
| Current (recency, full guidance text) | ~65t | 10 | ~650t |
| Naive case-study without storage split | ~95t | 10 | ~950t |
| **This ADR (decomposed + relevance + injection_form)** | **~80t** | **3** | **~240t** |

Net: **~63% cheaper per prompt than today**, because relevance-ranked top-3 with capped injection form beats recency-ranked top-10 with duplicated guidance text. This is why constraints 2 and 3 are non-negotiable.

### Per-decomposition cost (one-shot, on rejection)

Decomposer LLM call inputs (bounded):
- Trajectory step summaries (not raw steps): ~2-4K tokens
- Verdict + feedback: ~500 tokens
- Worktree diff (terminal turns only): ~1-2K tokens
- Scenario AC + relevant existing lessons: ~500 tokens

Total input: **~4-7K tokens per decomposition**. Output: ~500 tokens. Runs once per reviewer rejection (or first-try approval with rating ≥ 4) — call it ~5-15 times per typical plan run. Order-of-magnitude few cents per plan, isolated from the hot loop.

## Consequences

**Positive:**
- Lessons carry causal narrative grounded in trajectory steps, not regex matches over reviewer wording.
- Per-prompt budget *decreases* despite richer information, because relevance + injection_form > recency + duplicated guidance.
- Cross-role attribution surfaces upstream defects that today silently tax the developer.
- Retirement loop prevents the folklore-rot that current systems accumulate over weeks.
- Positive lessons give models a compass, not just a fence.
- Evidence citations make every lesson auditable, including the decomposer's own outputs (defense against decomposer hallucinations).

**Negative:**
- One extra LLM call per rejection. Bounded, isolated from hot loop, but real.
- New component to maintain (`lesson-decomposer`).
- Schema migration: existing lessons lack evidence pointers and `injection_form`. They render via `Summary` fallback until they age out via low-relevance pruning. No backfill — the cost of regenerating evidence for past lessons is not worth it.
- Inverse Goodhart risk: the decomposer's structured output (`root_cause_role`, `category_ids`, etc.) becomes the new optimization target. Mitigations: keep the structure shallow, force free-form `detail`, mandatory evidence citations make fabrication detectable.

**Neutral:**
- The 8-category vocabulary in `error_categories.json` does not grow. Categories remain a coarse routing/metrics signal. Resisting category proliferation is part of the structural defense; tracked as a non-decision here so a future ADR proposing finer-grained categories has to argue against this one.

## Migration

Phased — no big-bang.

0. **Phase 0 — consumer correctness (prerequisite). [SHIPPED 2026-04-29 commit `2053192`]** Three latent bugs in the injection path that must be fixed before the decomposer's quality gains are observable in tests. None of the load-bearing constraints depends on the decomposer; all of them depend on the injection path being correct.

   - **Bug 0.1 [FIXED]: `execution-manager` hardcodes `"developer"` role for lessons.** `processor/execution-manager/component.go` calls `ListLessonsForRole(graphCtx, "developer", 10)` inside `buildAssemblyContext`, which is invoked for *both* the developer prompt and the code-reviewer prompt. The reviewer prompt receives developer-targeted lessons. Fix: use the `role` parameter passed into `buildAssemblyContext` to fetch role-matching lessons. Behavior change is correctness-only — reviewer lessons do not exist yet (see bug 0.3) so the reviewer prompt currently shows wrong content; after the fix it shows nothing, which is still wrong but at least not actively misleading. The fix unblocks bug 0.3.
   - **Bug 0.2 [FIXED]: `plan-reviewer` does not populate `asmCtx.LessonsLearned`.** Every other agentic component (planner, requirement-generator, scenario-generator, architecture-generator, qa-reviewer, execution-manager) wires lessons into the assembly context. plan-reviewer is the lone gap. The team-knowledge fragment never renders for plan-review prompts. Fix: mirror the wire-up pattern from `processor/planner/component.go` — call `lessonWriter.ListLessonsForRole(ctx, "plan-reviewer", 10)` and assign to `asmCtx.LessonsLearned`. Note: today no producer creates `"plan-reviewer"`-targeted lessons, so this fix lands as plumbing-only until bug 0.3 produces the input. Doing the wire-up now means bug 0.3 lands as a one-line producer change.
   - **Bug 0.3 (deferred to Phase 1+): No producer creates lessons targeting reviewer roles.** Today's producers — plan-reviewer's `extractPlanLessons` and execution-manager's `extractLessons` — both file lessons against non-reviewer consumers (planner/requirement-generator/architect/scenario-generator/developer). Reviewers cannot learn from past reviewer mistakes because nothing creates that signal. The right time to address this is alongside the decomposer (Phase 1+), where cross-role attribution is already a load-bearing constraint and the decomposer can analyze trajectories to identify reviewer-specific patterns (e.g., over-rejection, missed-violation). Calling it out here so it is not forgotten when the decomposer ships.

   Phase 0 shipped ahead of all decomposer work because the bugs are small consumer-side fixes, the diagnoses are cheap to verify, and shipping them on a quiet branch lets us baseline current lesson quality before the structural changes start moving the rendered prompts.

0a. **Phase 0.4 — structural-validation lesson producer. [SHIPPED 2026-04-29 commit `5931129`]** A keyword-classifier lesson producer for structural-validation failures, parallel in shape to `extractLessons` but sourced from deterministic toolchain output rather than LLM-authored reviewer feedback. Per failed required `CheckResult`: classify `Name + Stderr + Stdout` (truncated to 800 runes) against `error_categories.json`, write `workflow.Lesson{Source: "structural-validation", Role: "developer"}`, increment shared role-counts so threshold warnings fire whether the signal originated from a reviewer or from `go-test`/`pytest`/lint stderr.

   Why this lands inside Phase 0 rather than waiting for the decomposer: the substrate (exit codes + real toolchain stderr/stdout) is non-LLM-authored, so the keyword classifier this ADR critiques *cannot be Goodhart-gamed by the agent under review* — the agent does not author the failure description. The Goodhart pressure ADR-033 mitigates does not apply here. When Phase 1+ ships, this hook becomes another `lesson.decompose.requested` publisher at the same call site (`dispatchValidatorLocked` validation-failed branch); the lesson schema and consumer side require no change.

   Closes the gap where structural-validation failures re-dispatched the developer in-loop with feedback but never accumulated into the role-scoped lessons graph.

1. **Phase 1 — schema. [SHIPPED 2026-04-29 commit `b9d5a5e`; atomic-triples follow-up shipped same day]** Added new fields to `workflow.Lesson` (`Detail`, `InjectionForm`, `EvidenceSteps`, `EvidenceFiles`, `RootCauseRole`, `Positive`, `RetiredAt`, `LastInjectedAt`) and matching predicates to `agentgraph`. Writer accepts but does not require evidence — Phase 1 logs Debug rather than Warn for missing evidence, since every existing producer (reviewer-feedback, structural-validation, plan-review) lacks evidence and loud Warn would flood logs without signal. Phase 3 will flip this to a hard reject. Multi-valued fields (`CategoryIDs`, `EvidenceSteps`, `EvidenceFiles`) write one atomic triple per element via `ReadEntitiesByPrefixMulti` (per `feedback_no_json_in_triples`); reader keeps a JSON-array fallback so legacy lessons still parse. No behavior change for existing pipelines.
2. **Phase 2 — decomposer.** Ship `lesson-decomposer` component, wired to execution-manager rejections only (smallest blast radius). Lessons from plan-reviewer and qa-reviewer continue using current path. Compare quality manually for a week. Sequenced as 2a (wire) → 2b (LLM):

   - **Phase 2a [SHIPPED 2026-04-29 commit `db8e638`].** Skeleton component + `LessonDecomposeRequested` payload + execution-manager publish trigger. The decomposer subscribes on `workflow.events.lesson.decompose.requested.>`, parses the payload, and logs receipts; no trajectory fetch and no LLM yet. execution-manager publishes alongside `extractLessons` (keyword classifier remains the producer until Phase 3). The `Enabled` config flag keeps the component switchable per-deployment. Why split: nailing the cross-component payload contract before the consumer logic ships avoids schema thrash during 2b; producers can be exercised in mock e2e ahead of real-LLM cost.

   - **Phase 2b [SHIPPED 2026-04-29].** Trajectory fetch via `agentic.query.trajectory` NATS request/reply (`30a27e3`); decomposer persona + prompt template plus `lesson` deliverable schema (`22faa28`); LLM dispatch + result parse via `agent.task.lesson-decomposition` + AGENT_LOOPS watcher + `buildLesson` evidence enforcement (`5466dfa`); mock-LLM wire-up + payload-decoder fix (`ece95a7`). The decomposer now writes evidence-cited lessons with `Source="decomposer"` alongside the keyword-classifier output in plan-stall-reject. Worktree-diff fetch is still stubbed — the decomposer can produce a useful lesson from trajectory + reviewer feedback alone, and the diff helper is deferred to Phase 3 alongside the writer's enforcement flip. Capability `lesson_decomposition` (Opus/Sonnet preferred; falls back to `reviewing`) routes to the dedicated endpoint; the `Enabled` config flag still gates the consumer per-deployment.
3. **Phase 3 — enforce evidence. [SHIPPED 2026-04-29].** Writer rejects evidence-less lessons via `ErrLessonWithoutEvidence` (`b57387b`). Direct producers (plan-reviewer, qa-reviewer, structural-validation) self-populate `EvidenceFiles` rather than routing through the decomposer — their findings are already structured (per-finding role attribution, deterministic check failures, agent-supplied artifact_refs) so the LLM decomposition this ADR originally prescribed for these surfaces would *lose* information rather than add analysis. The decomposer remains the producer for code-review rejections (`60d5d51` removed the keyword-classifier alongside it). Net effect: every lesson now carries a citation the Phase 5 retirement sweep can verify against.
4. **Phase 4 — relevance ranking + injection_form. [SHIPPED 2026-04-29].** Phase 4a (`e3bd5f5`) rendered `InjectionForm` in the team-knowledge prompt fragment with `Summary` fallback; producers thread `les.InjectionForm` through `prompt.LessonEntry`. Phase 4b (`9a09c35`) added `Writer.RotateLessonsForRole` — sorts by `LastInjectedAt` (nil first → oldest first, `CreatedAt DESC` tie-break), bumps `LastInjectedAt` on selected lessons via best-effort triple write so the same N lessons no longer monopolize prompt slots. Seven producers switched from `ListLessonsForRole` to `RotateLessonsForRole`; the read-only path stayed for HTTP and the decomposer's "existing lessons" view. Retired lessons are skipped — once Phase 5's sweep marks them, they exit rotation.
5. **Phase 5 — retirement sweep:** Ship periodic re-evaluation. Prune lessons that fail evidence checks.
6. **Phase 6 — positive lessons:** Wire approval-on-first-pass + rating ≥ 4 path into the decomposer.

Each phase ships independently. Rollback at any phase leaves prior phases intact.

## Alternatives considered

**Keep the keyword classifier, just add `Detail` field.** Cheapest, but does not fix the Goodhart loop — `Detail` would still be authored by truncating reviewer feedback. The decomposer's value is the *second view* of the failure, not the extra field.

**Run decomposition inline in execution-manager.** Avoids a new component but blocks the hot loop on an LLM call and tangles concerns. Decomposition is rare and asynchronous; it should be a separate component.

**Skip the storage/injection split, accept the token cost.** Viable on Sonnet/Opus, hostile to local Ollama setups (per `feedback_prompt_context_budget`). The split is cheap to implement and makes the design model-agnostic.

**Auto-generate categories from clustered lessons (drop the static JSON taxonomy).** Tempting but risks the inverse Goodhart trap at the categorization layer — the model would optimize for cluster centroids. Better to let categories stay coarse and put the intelligence in the per-lesson narrative.

**Use semdragon's red-team agent pattern (separate review pass, not trajectory introspection).** Semdragon's red-team produces structured findings from quest *output*, not the trajectory. That gives richer text but no causal claim — the red-team agent does not know *what the implementer did*, only *what they submitted*. Trajectory-based decomposition is strictly more powerful and uses infrastructure semspec already has.

## Success metrics

- **Lesson quality (manual):** Sample 20 lessons from the new system vs. 20 from the old; ask "would this lesson actually prevent the next occurrence." Target: >70% useful in new vs. <30% in old (current baseline by inspection).
- **Cross-role attribution rate:** % of execution rejections that produce a lesson against an upstream role. Target: 20-40% (matches our intuition that most developer rejections have upstream root causes; if it's <10% the decomposer is rubber-stamping the proximate role; if it's >60% it's overcorrecting).
- **Retirement rate:** % of lessons retired per month. Target: 10-20% steady state. Zero means the sweep is broken or lessons are too vague to invalidate.
- **Per-prompt token usage for `team-knowledge`:** Target: ≤300 tokens p95. Watching for budget creep.
- **First-pass approval rate change:** Soft metric — if the new lessons are doing their job, approval rate on first attempt should improve over a few weeks of accumulation. Easily confounded; treat as directional signal not proof.
