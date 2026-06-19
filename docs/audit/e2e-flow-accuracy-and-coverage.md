# E2E Flow Accuracy & Deterministic-Test-Coverage Audit

Status: **audit first pass.** This commit lands the report and applies the doc-side corrections it
identified to `docs/e2e-flow.md` (the 14 doc fixes catalogued below; 11 doc-side land here, the 3
code-only fixes in a later PR). No production code is changed in this PR; the deterministic-test
backlog and production fixes land in subsequent PRs.
Pinned to `main` @ `7de7f430` (#236). This audit exists to satisfy the 2026-06-19 HARD GATE:
**no new paid-LLM e2e run until `docs/e2e-flow.md` is verified accurate AND every Plan state it
describes has a deterministic test.** It also delivers the state-machine inventory + gap list
required by **#221** (audit deterministic state machines for full-auto no-wedge invariants).

## Scope and stance

The premise behind the gate: the recent paid-run findings — #226 (re-dispatch wedge after a
recovery reset leaves requirements `active` not `pending`), #235 (recovery submit schema),
#237 (no semantic file→component placement check at plan-review), #238 (`architecture_revise`
cascade regenerates the whole plan layer, not just the affected requirement) — were **all
statically checkable**. Burning hundreds of dollars and hours of wall-clock to discover a
deterministic plumbing wedge is the problem. The fix is a static net: an accurate flow doc, a
complete legality table, and a behavioral test that drives the *performing component* through
every transition in the recovery/redispatch/change-cascade territory.

Ground truth for this audit is the code, not the doc:

- `workflow/types.go:200-384` — `func (s Status) CanTransitionTo(target)`, the legal Plan-state
  transition table.
- `workflow/types_plan_status_test.go` — the existing transition-**legality** unit test.
- `processor/<name>/` — the component that actually **performs** each transition. A transition
  being *legal* does not mean a component performs it; "legal but never driven through the
  performer" is the dominant gap and the exact paid-run bug class.

## Method

12 flow slices (planning → requirements → architecture → stories → scenarios → R2 review →
dispatch → TDD loop → convergence → QA → terminal/external → recovery). Each slice was audited
on two axes — **doc accuracy** and **deterministic test coverage** — then **adversarially
verified** by a second agent that independently re-derived the legal edges from
`CanTransitionTo` and confirmed/rejected each finding by quoting code, then synthesized. Every
discrepancy and every coverage claim below is backed by a `file:line`.

## Verdict

**BLOCK — not sufficient to gate paid runs as-is.**

`docs/e2e-flow.md` is accurate on the **happy-path spine** (the forward generator/claim chain
and the QA happy path are correctly described and well-evidenced), but it has systematic,
statically-verifiable gaps exactly in the **recovery / redispatch / change-cascade** territory
where the paid-run bugs live:

1. The Plan State Chart (`docs/e2e-flow.md:136-189`) omits **two whole states** —
   `scenarios_reviewed` (the `auto_approve=false` human gate; a grep of the doc returns **zero**
   matches) and the legacy `reviewing_rollup` — plus **all** inbound `--> changed` edges (only
   the outbound `changed --> generating_requirements` at L151 is drawn) and **all** `rejected -->`
   recovery back-edges (5 legal, zero drawn).
2. The System Spine **mislabels** the PR-feedback source as the `Complete` node
   (`L127 Complete -->|PR feedback| Feedback`) when code requires `awaiting_review`
   (`mutations.go:1964`).
3. Stale code/doc: `types.go:312` claims `reviewing_rollup` is "still emitted today" but it is
   **never** assigned as a transition target; `Step 5` (L277-278) claims the requirements
   transition validates "no unexplained contract loss" but `handleRequirementsMutation`
   (`mutations.go:247-316`) runs no such gate.

On the **test axis** the picture is worse: the four claim/generation-failed chokepoints —
`handleClaimMutation`, `handleGenerationFailedMutation`, `handleReviewedMutation`,
`handleApprovedMutation` — have **zero** unit coverage despite owning ~13 forward edges, and the
highest-severity recovery edges (executor `req.* pending->decomposing` claim [#226],
`reviewing_qa->rejected` [#226/#235], Story restructure branch-rebuild, the entire
external-review / github-submitter path which has **no test file**) are `legality_only` or
`no_test`.

The doc's own premise — that these bug classes were statically checkable — **holds**; the doc +
legality table + handler unit tests do not yet form that static net. **Gate verdict: BLOCK until
the doc fixes land AND the high-severity prioritized test gaps are added.**

## Headline coverage numbers

110 distinct transitions in the deduped matrix (≈70 Plan-Status outgoing edges from
`CanTransitionTo`, plus task/Story-DAG + free-string `Stage` edges, plus 2 non-Status dispatch
actions).

| | no_test | legality_only | covered (none) | total |
|--------|--------:|--------------:|---------------:|------:|
| **high** | 13 | 16 | 21 | 50 |
| medium | 8 | 30 | 7 | 45 |
| low | 3 | 11 | 1 | 15 |
| **total** | **24** | **57** | **29** | **110** |

**Only 29 of 110 transitions are fully covered. 29 of the 50 high-severity edges lack
behavioral coverage** — and they cluster precisely in recovery/redispatch/QA/convergence.

Doc accuracy: ≈62 accurate / ≈41 discrepant (missing_from_doc 24, mislabeled 6,
in_doc_not_in_code 3, stale prose 4, missing_state 2).

## Cross-cutting patterns

1. **"Legal but never driven through the performer" is the dominant gap — and it is exactly the
   paid-run bug class.** ~46/103 edges are `legality_only`: `CanTransitionTo` asserts the edge is
   legal (cheap static check) but no test drives the *performing component* through it. #226/#235/
   #237/#238 all live here. A regression that unwires a semantic gate or breaks a recovery handler
   passes every current test.
2. **Four mutation chokepoints have ZERO unit coverage and own a disproportionate number of
   edges:** `handleClaimMutation` (`mutations.go:1163-1197`, ~6 forward claim edges incl. R2),
   `handleGenerationFailedMutation` (`mutations.go:700-728`, **all** generation.failed→rejected
   exhaustion edges across 5+ slices), `handleReviewedMutation` (`mutations.go:1001`),
   `handleApprovedMutation` (`mutations.go:1041`). grep across all `processor/*_test.go` = 0
   invocations each. Testing these four directly hardens ~15 edges at once — the single
   highest-leverage action.
3. **The Plan State Chart systematically under-represents the legal DAG on the recovery/change
   side** — two whole states, all 7 inbound `--> changed` edges, all 5 `rejected -->` back-edges.
   A reader using the chart to reason about recovery has a materially wrong mental model, and
   recovery is where the money is burned.
4. **The change-proposal cascade (`changed`) is driven by ONE generic source-agnostic setter**
   `setPlanStatusCached(StatusChanged)` at `http_plan_decision.go:303`, reachable from 7 source
   states, with **zero** behavioral transition tests. This is #238 territory (cascade scope) and
   is wholly unverified at the transition level.
5. **Tier-2 mock-LLM e2e is NOT a substitute for the deterministic-test gate.** Several edges
   marked covered rely *only* on the mock ladder (`qa_cycle.go`, `execution_phase.go`,
   `plan_phase.go`) — full stack + LLM fixture, slow. The user's gate requires a *deterministic*
   test per state. The entire TDD-loop happy path (`developing→validating→reviewing→approved+merge`)
   exists only in the mock ladder — no deterministic unit coverage at all.
6. **Six confirmed DEAD legal edges** (legal in `CanTransitionTo`, no performer):
   `implementing->reviewing_rollup`, `implementing->reviewing_qa` (Phase 2f future),
   `ready_for_qa->rejected` (infra reject actually routes via `reviewing_qa`),
   `rejected->created` (only `reviewing_draft->created` exists), `rejected->implementing`
   (recovery routes two-hop), `preparing_stories->architecture_generated` (exhaustion rejects;
   doc L319 implies it). Each is over-broad surface to either pin FALSE+tighten or implement+test.
   Additionally, `handleUnarchivePlan` (`http.go:1191`) **bypasses `CanTransitionTo` entirely** —
   the only terminal mutation not routed through `setPlanStatusCached`.
7. **The legality table itself is incomplete.** Confirmed-missing rows include `created->rejected`,
   `reviewed->approved`, `reviewed->rejected`, `reviewing_scenarios->scenarios_reviewed`,
   `scenarios_reviewed->ready_for_execution`, `preparing_stories->stories_generated`,
   `stories_generated->preparing_stories`, `implementing->complete`, `implementing->rejected`,
   `ready_for_execution->implementing`, `complete->archived`, `complete->ready_for_execution`,
   `archived->complete`, `archived->ready_for_execution`, `rejected->ready_for_execution`,
   `rejected->implementing`. The missing rows correlate with the undocumented edges — confirming a
   legality table that mirrors `CanTransitionTo` exactly would catch doc drift.
8. **Doc/code role-attribution mislabels cluster on convergence and PR-feedback.** The System
   Spine sources convergence from the orchestrator (L115) and PR feedback from `Complete` (L127),
   but both are actually plan-manager's own `EXECUTION_STATES` watcher / `awaiting_review`-gated
   handler. The prose at L563 is correct and contradicts the diagrams — the doc disagrees with
   itself on who performs the two most operationally important transitions.

## Doc accuracy fixes (14)

| # | Kind | Location | Fix | Evidence |
|---|------|----------|-----|----------|
| 1 | missing_state | State chart L167-173 + Step 9/10 | Add `scenarios_reviewed` state + edges: `reviewing_scenarios --> scenarios_reviewed` (R2 approved, `auto_approve=false` human gate) and `scenarios_reviewed --> {ready_for_execution, changed, rejected}`. Step 10 must name the state the plan rests in awaiting human approval. | `types.go:291`; performer `plan-reviewer/plan_watcher.go:176`; `handleScenariosReviewedMutation mutations.go:1229`; promote round-2 `http.go:856-864` |
| 2 | missing_edge | State chart L136-189 | Add the 7 inbound `--> changed` change-proposal edges (`requirements_generated, architecture_generated, scenarios_generated, scenarios_reviewed, ready_for_execution, implementing, complete --> changed`); at minimum draw `implementing --> changed` and `complete --> changed`. | `types.go:246/260/286/304/308/317/362`; sole performer `http_plan_decision.go:303` |
| 3 | missing_edge | State chart L186-189 + Spine L131 | Add `rejected -->` recovery back-edges: `rejected --> requirements_generated` (post-QA architecture_revise), `rejected --> ready_for_execution` (retry / scope_incomplete). Flag `rejected->approved` (manual R2). | `types.go:378-380`; performers `reviseArchitectureState mutations.go:2686`, `handleRetryPlan http.go:1363` + `applyScopeIncompleteRecovery mutations.go:2322` |
| 4 | wrong_edge | Spine L127-128 | PR feedback must originate from `awaiting_review`, not the merged `Complete` node. Re-source: `awaiting_review -->|PR feedback| Feedback`. | `mutations.go:1964` (`plan must be in awaiting_review`) |
| 5 | stale_prose | Step 5 L277-278 | Remove "and no unexplained contract loss" from the requirements happy path (or, preferred, wire `unauthorizedContractScopeDrops` into `handleRequirementsMutation` + add a reject test). | `handleRequirementsMutation mutations.go:247-316` runs only DAG/ownership/capability; the contract-loss gate (`mutations.go:866`) is in the **draft** handler |
| 6 | stale_prose | `types.go:312` comment + chart | Fix "reviewing_rollup … still emitted today" — it is never assigned. Either remove the `implementing->reviewing_rollup` legal edge or mark it dead. Also fix `execution_events.go:255` comment. | grep of `processor/` (non-test) for a `StatusReviewingRollup` assignment = NONE (read-guards only) |
| 7 | missing_edge | State chart L175 | Add `implementing --> awaiting_review` (QA none, review gated), `ready_for_execution --> changed`, `ready_for_execution --> rejected`. | `execution_events.go:588-589` + `:409`; `types.go:308/309` |
| 8 | mislabeled | Sequence diagram L405 + Spine L115 | Re-attribute convergence detection to plan-manager (its own `EXECUTION_STATES` watcher), not the orchestrator. Prose L563 is already correct. | `execution_events.go:30/257/403`; no convergence publish in `scenario-orchestrator/*.go` |
| 9 | missing_edge | State chart + Step 11/20 | Document terminal/shelve/unarchive edges: `complete --> archived` (shelve), `archived --> complete` (unarchive), `complete/archived --> ready_for_execution` (retry). grep of doc for "shelve"/"unarchiv" = ZERO. | `types.go:360/363/365/367`; `handleDeletePlan http.go:1133`, `handleUnarchivePlan http.go:1191`, `handleRetryPlan http.go:1363` |
| 10 | missing_edge | State chart L159,179 + taxonomy L756 | Add `stories_generated --> preparing_stories` (accepted story_reprepare R3); broaden taxonomy L756 to name BOTH sources (implementing AND stories_generated). Decide `preparing_stories->architecture_generated` (legal, no performer; L319 implies it). | `applyStoryReprepare mutations.go:2487` (gate `:2457`); exhaustion `component.go:758` → reject |
| 11 | stale_prose | Step 16 L700-718 | Disambiguate the two budget-distinct reviewer retries and add the missing third: reviewer loop OUTCOME failure → developer re-dispatch (burns a TDD cycle), distinct from parse-retry (free). | `component.go:1032-1033` (outcome→developer retry) vs `:1045-1062` (parse-retry, `ReviewRetryCount`) |
| 12 | missing_state | Steps 14-16 L466-517 | Document the task-level `phaseError` terminal (worktree_creation_failed, worktree_lost, merge_failed, INFRASTRUCTURE: prefix, timeout) as a distinct outcome. | `markErrorLocked component.go:1413` (`Stage=phaseError :1422`), 5 distinct callers |
| 13 | wrong_edge | State chart L182 + Step 19 | Resolve `ready_for_qa->rejected` dead edge: legal (`types.go:345`) but no performer (infra failures route `ready_for_qa->reviewing_qa->rejected`). Remove legal edge + add NEGATIVE legality assertion, or wire a performer. | grep `plan.Status = StatusRejected` → only `mutations.go:728/1352/1754`; `handleQAVerdictMutation` guards `reviewing_rollup/reviewing_qa` |
| 14 | wrong_edge | `types.go:374,377` | Tighten two orphan rejected back-edges: `rejected->created` (no performer; only `reviewing_draft->created` exists) and `rejected->implementing` (no single-hop performer; recovery routes two-hop). | `StatusCreated` setter `mutations.go:1534/1544` guards `reviewing_draft`; `handleExecutePlan http.go:906` gates `ready_for_execution` |

## Prioritized deterministic-test backlog (15)

Ordered by value. The single highest-leverage items (#4, #7, #8, #14) directly test the four
zero-coverage chokepoints and harden ~15 edges.

| # | Kind | Edge / target | Why (bug class) | Assertion |
|---|------|---------------|-----------------|-----------|
| 1 | component_unit | `req.* pending->decomposing` → `req_watcher_test.go` | **#226** re-dispatch wedge | Seed pending req; `handleReqPending` drives `pending->decomposing` + dispatch. **Negative: a req at Stage≠pending (e.g. `active` left by a reset) is a NO-OP, not re-claimed** — the exact #226 wedge. |
| 2 | component_unit | `reviewing_qa->rejected` → `qa_verdict_test.go` | **#226/#235** | Drive needs_changes AND rejected through `handleQAVerdictMutation`: assert `stored.Status==rejected`, PlanDecisions use the **recovery-submit schema** (action/diagnosis/contract_impact/scope_changes per #235, NOT review-verdict shape), `RecoveryRequested` carries ContractImpact. |
| 3 | component_unit | `story executing->executing` restructure → `requirement-executor/component_test.go` | restructure wedge class | Feed `{rejected, restructure}` through `handleRequirementReviewerCompleteLocked`: `startRestructureRetryLocked` runs DeleteBranch+CreateBranch, full DAG reset, `sendReqPhase(decomposing)`, `dispatchSynthesizer`. |
| 4 | component_unit | `generating_requirements->requirements_generated` gate wiring → `stories_scenarios_mutation_test.go` | **#237** sibling | Call `handleRequirementsMutation` with (a) DAG cycle, (b) ownership conflict, (c) capability gap → `Success=false` + plan STAYS `generating_requirements`; + happy path → `requirements_generated`. Locks gate wiring so an unwired validator regresses RED. |
| 5 | component_unit | `implementing->requirements_generated` architecture_revise scoped-vs-whole → `architecture_revise_test.go` | **#238** | ADD: a SCOPED revise (AffectedReqIDs set, ContractImpact≠whole) must NOT wipe unrelated stories/scenarios and must NOT route through `reviseArchitectureState` (whole wipe). Assert dirty-closure-only merge. |
| 6 | legality_unit | `rejected->ready_for_execution` → `types_plan_status_test.go` | **#226** | Add `{StatusRejected, StatusReadyForExecution, true}`; decide `rejected->implementing`/`rejected->created` intent (assert FALSE + tighten if no performer). |
| 7 | component_unit | `handleClaimMutation` chokepoint (~6 edges) → `mutations_test.go` | legal-but-undriven | Table-drive each `(source, in-progress target)`: `Success=true`, `plan.Status==target`; double-claim/non-IsInProgress guard rejects. One test closes 6 edges. |
| 8 | component_unit | `reviewing_draft->reviewed` + `handleReviewed/ApprovedMutation` → `mutations_test.go` | R1 convergence | `handleReviewedMutation` from `reviewing_draft` → `reviewed`; `handleApprovedMutation` (auto-approve) from `reviewed` → `approved` + `Approved=true` + `ApprovedAt` set. |
| 9 | component_unit | `awaiting_review->ready_for_execution`/`->complete` PR feedback → `pr_feedback_test.go` (new) + github-submitter | **#226** | `handleGitHubPRFeedbackMutation` resets ONLY affected reqs via `resetRequirementExecutionsByID`, plan→`ready_for_execution`, **resetCount==0 guard FAILS** when affected reqs map to zero resets (reset-leaves-active class). |
| 10 | component_unit | `ready_for_execution->implementing` execute dispatch → `http_execute_test.go` (new) + legality row | central dispatch | `handleExecutePlan` → `implementing` + `scenario.orchestrate` published; empty-req/scenario → 400; JetStream publish failure → rollback to `ready_for_execution`. |
| 11 | component_unit | `stories_generated->preparing_stories` R3 → `story_reprepare_apply_test.go` | recovery back-edge | Call production `applyStoryReprepare` on a `stories_generated` plan → `EffectiveStatus()==preparing_stories` AND execution-reset block SKIPPED (current≠implementing). Add 2 legality rows. (Current test hand-sets status, never calls the producer.) |
| 12 | component_unit | task `developing/validating/reviewing` TDD happy chain → `execution-manager/component_test.go` | mock-ladder-only today | Feed parsed verdicts through `handleReviewerCompleteLocked`: approved→`markApprovedLocked`+merge; fixable→`startDeveloperRetryLocked`+TDDCycle++; restructure→`markEscalatedLocked`. Plus develop-success→validating, validate-pass→reviewing. |
| 13 | component_unit | Story-status mutations `publishStoryComplete/markFailed` `ClaimStoryStatus` → `requirement-executor/component_test.go` | gated behind nats!=nil | Inject `fakeStoryStatusClaimer` (`recover_completed_test.go:313`) so `ClaimStoryStatus` fires in unit mode; assert `executing->complete` and `executing->failed` enforce `CanTransitionTo`. |
| 14 | component_unit | `handleGenerationFailedMutation` (all exhaustion edges) → `mutations_test.go` | zero invocations repo-wide | Table-drive from each source (gen_requirements/gen_architecture/gen_scenarios/preparing_stories) → `rejected` + LastError set + gate holds. |
| 15 | legality_unit | `archived->complete` unarchive BYPASS + terminal retry rows → `types_plan_status_test.go` + `http_unarchive_test.go` (new) | state-machine bypass | Add 4 terminal legality rows; route `handleUnarchivePlan` through `setPlanStatusCached` and test an illegal source 409s (make the state machine the guard). |

## Full Plan-state transition coverage matrix

Sorted by severity, then gap. Performer / test columns truncated; full evidence in the per-slice
audit data. `N/A` legality = task `Stage` / Story-status / dispatch edges (not Plan `Status`).

| Sev | Gap | Edge | Performer | Legality test | Behavioral test |
|-----|-----|------|-----------|---------------|-----------------|
| high | no_test | preparing_stories -> architecture_generated (R3, **UNPERFORMED**) | UNKNOWN (legal types.go:266, no performer) | NONE | NONE |
| high | no_test | EXECUTION_STATES req.* pending -> decomposing (executor claim) | req-executor handleReqPending req_watcher.go:86 | N/A | NONE (req_watcher_test.go:11 selects only) |
| high | no_test | task developing -> validating | exec-mgr handleDeveloperCompleteLocked component.go | N/A | NONE |
| high | no_test | task validating -> reviewing | exec-mgr dispatchValidatorLocked component.go:1903 | N/A | NONE |
| high | no_test | task validating -> developing (dev-retry) | exec-mgr handleValidationFailedLocked | N/A | NONE |
| high | no_test | task reviewing -> approved (+merge) | exec-mgr handleReviewerCompleteLocked (approved) | N/A | PARTIAL (markApproved isolated) |
| high | no_test | task reviewing -> developing (fixable) | exec-mgr handleRejectionLocked->routeFixable | N/A | PARTIAL (component_test.go:1480 LOOP-fail) |
| high | no_test | task reviewing -> escalated (restructure) | exec-mgr handleRejectionLocked component.go:1170 | N/A | NONE |
| high | no_test | story executing -> executing (restructure rebuild) | req-executor startRestructureRetryLocked | N/A | NONE |
| high | no_test | story executing -> failed (reviewer retries exhausted) | req-executor markFailedLocked component.go:2834 | story_task_test.go:44 | PARTIAL |
| high | no_test | complete -> ready_for_execution (retry) | plan-mgr handleRetryPlan http.go:1363 | NONE | NONE |
| high | no_test | archived -> complete (unarchive **BYPASS**) | plan-mgr handleUnarchivePlan http.go:1191 | NONE | NONE |
| high | no_test | archived -> ready_for_execution (retry) | plan-mgr handleRetryPlan http.go:1363 | NONE | NONE |
| high | legality_only | reviewing_draft -> reviewed (R1 approved) | plan_watcher.go:153 -> handleReviewedMutation | types_..._test.go:114 | NONE (handleReviewedMutation 0 tests) |
| high | legality_only | approved -> generating_requirements (reqgen claim) | reqgen plan_watcher.go:79 ClaimPlanStatus | types_..._test.go:121 | NONE (Tier2 indirect) |
| high | legality_only | generating_requirements -> requirements_generated | plan-mgr handleRequirementsMutation mutations.go:305 | types_..._test.go:123 | NONE (validators isolated only) |
| high | legality_only | changed -> generating_requirements (partial regen) | reqgen plan_watcher.go:79 on StatusChanged | types_..._test.go:205 | NONE |
| high | legality_only | requirements_generated -> architecture_generated (SkipArchitecture) | arch-gen component.go:300-304 | types_..._test.go:77 | NONE (grep=0) |
| high | legality_only | architecture_generated -> changed | http_plan_decision.go:303 setPlanStatusCached | types_..._test.go:198 | NONE |
| high | legality_only | stories_generated -> preparing_stories (R3 story_reprepare) | applyStoryReprepare mutations.go:2487 | NONE | PARTIAL (end_to_end hand-sets status) |
| high | legality_only | generating_scenarios -> rejected (exhaustion) | scenario-gen sendGenerationFailed component.go:796 | types_..._test.go:143 | NONE (handleGenerationFailed grep=0) |
| high | legality_only | reviewing_scenarios -> scenarios_reviewed (R2, human gate) | plan_watcher.go:176 -> handleScenariosReviewed | NONE | contract_observability.go:402 (mutation-subject) |
| high | legality_only | scenarios_reviewed -> ready_for_execution (human approve) | handlePromotePlan round-2 http.go:859 | NONE | contract_observability.go:422 |
| high | legality_only | ready_for_execution -> implementing (execute) | handleExecutePlan http.go:906 | NONE | mutations_test.go:42 (auto-start path) |
| high | legality_only | ready_for_qa -> rejected (**DEAD edge**) | NONE | types_..._test.go:240 | NONE |
| high | legality_only | awaiting_review -> complete (external approval) | handleReviewApproveMutation mutations.go:1846 | types_..._test.go:184 | NONE (github-submitter NO test file) |
| high | legality_only | awaiting_review -> ready_for_execution (PR feedback, #226) | handleGitHubPRFeedbackMutation mutations.go:196x | types_..._test.go:185 | NONE |
| high | legality_only | rejected -> ready_for_execution (retry/scope_incomplete, #226) | handleRetryPlan http.go:1363 | NONE | plan_stall_recovery.go:580 (Tier2) |
| high | legality_only | rejected -> implementing (**ORPHAN**) | UNKNOWN | NONE | NONE |
| high | none | reviewing_draft -> created (R1 revision) | handleRevisionMutation mutations.go:1479 | types_..._test.go:160 | mutations_test.go:114-168 |
| high | none | reviewing_draft -> rejected (R1 cap) | escalateRevision mutations.go:1339 + emitRecovery | types_..._test.go:116 | mutations_test.go:215 + recovery_publish |
| high | none | requirements_generated -> generating_architecture (claim) | arch-gen component.go:285 ClaimPlanStatus | types_..._test.go:75 | contract_observability.go:262 + plan_phase |
| high | none | generating_architecture -> architecture_generated | arch-gen publishArchitectureGenerated component.go:839 | types_..._test.go:132 | contract_observability.go:264 + plan_phase |
| high | none | generating_scenarios -> scenarios_generated | handleScenariosMutation mutations.go:685 | types_..._test.go:141 | stories_scenarios_mutation_test.go:243 |
| high | none | scenarios_generated -> reviewing_scenarios (R2 claim) | plan_watcher.go:102 ClaimPlanStatus | types_..._test.go:148 | contract_observability.go:299 |
| high | none | reviewing_scenarios -> ready_for_execution (R2 auto) | plan_watcher.go:170 -> handleReady... | types_..._test.go:152 | mutations_test.go:42 |
| high | none | reviewing_scenarios -> approved (R2 re-entry) | handleRevisionMutation mutations.go:1544 | types_..._test.go:162 | mutations_test.go:170 |
| high | none | reviewing_scenarios -> rejected (R2 cap) | escalateRevision mutations.go:1352 + emitRecovery | types_..._test.go:154 | mutations_test.go:247 |
| high | none | task validating -> escalated (ownership/topology gap, ADR-049/#237) | exec-mgr handleValidationFailedLocked | N/A | validation_routing_test.go:75 |
| high | none | story executing -> complete (Story review approved) | req-executor handleApprovedVerdictLocked | story_task_test.go:39 | component_test.go:2188/2232 |
| high | none | story executing -> executing (Story fixable retry) | req-executor startFixableRetryLocked component.go:21xx | N/A | component_test.go:1995 + :206x |
| high | none | story complete -> ready (cascade re-execute, #238 Story-level) | storyStatusWalkToReady recover_completed | story_task_test.go:46 | recover_completed_test.go:268 |
| high | none | implementing -> ready_for_qa (all complete, QA enabled) | handleConvergenceAllSucceeded execution_events.go | types_..._test.go:234 | plan_qa_worktree_test.go:311 |
| high | none | implementing -> rejected (auto-reject / completeness #226 / assembly) | 3 arms execution_events.go:330 + ... | NONE | STRONG (completeness_gate_test.go) |
| high | none | implementing -> preparing_stories (accepted story_reprepare) | applyStoryReprepare mutations.go:2487 | types_..._test.go:219 | story_reprepare_apply_test.go:46 (producer) |
| high | none | implementing -> requirements_generated (architecture_revise, #238) | applyArchitectureRevise mutations.go:2195 | types_..._test.go:222 | architecture_revise_test.go:18 + :151 |
| high | none | ready_for_qa -> reviewing_qa (QA claim) | handleQAStartMutation mutations.go:1619 | types_..._test.go:238 | qa_cycle.go:132 (Tier2); UNIT GAP |
| high | none | reviewing_qa -> complete (QA approved, no gate) | handleQAVerdictMutation mutations.go:1704 | types_..._test.go:244 | qa_cycle.go:134 (Tier2) |
| high | none | reviewing_qa -> rejected (QA needs changes, #226/#235) | handleQAVerdictMutation mutations.go:1754 + recovery | types_..._test.go:248 | recovery_publish_test.go:31/:156 |
| high | none | rejected -> requirements_generated (post-QA architecture_revise) | reviseArchitectureState mutations.go:2686 | types_..._test.go:174 | architecture_revise_test.go:121 |
| medium | no_test | scenarios_generated -> scenarios_reviewed (direct, legal-but-unused) | handleScenariosReviewedMutation mutations.go:12xx | NONE | NONE |
| medium | no_test | scenarios_reviewed -> rejected (orphan) | UNKNOWN | NONE | NONE |
| medium | no_test | ready_for_execution -> rejected (orchestration failure) | UNKNOWN (execute publish-fail ROLLS BACK) | NONE | NONE |
| medium | no_test | [action] implementing dispatch -> req.* pending | scenario-orchestrator triggerRequirementExecution | N/A | DAG/idempotency/force-redispatch unit tests |
| medium | no_test | task validating -> escalated (budget/validator error) | exec-mgr component.go:1936 markEscalatedLocked | N/A | NONE |
| medium | no_test | task reviewing -> reviewing (parse-retry) | exec-mgr handleReviewerCompleteLocked | N/A | NONE |
| medium | no_test | implementing -> complete (all complete, QA none) | targetForQALevel execution_events.go:587 | NONE | NONE (convergence tests use QALevelUnit) |
| medium | no_test | complete -> archived (shelve) | handleDeletePlan http.go:1133 | NONE | NONE |
| medium | legality_only | created -> exploring | planner component.go:287 ClaimPlanStatus | types_..._test.go:254 | NONE (analyst_test.go:232 routing only) |
| medium | legality_only | created -> drafting | planner component.go:297 ClaimPlanStatus | types_..._test.go:103 | NONE |
| medium | legality_only | exploring -> rejected | planner sendGenerationFailed component.go:1374 | types_..._test.go:258 | NONE |
| medium | legality_only | explored -> drafting | planner routeExplored component.go:333 | types_..._test.go:264 | NONE |
| medium | legality_only | drafting -> drafted | handleDraftedMutation mutations.go:790/843 | types_..._test.go:105 | NONE (cases seed StatusCreated) |
| medium | legality_only | drafting -> rejected | planner sendGenerationFailed | types_..._test.go:107 | NONE |
| medium | legality_only | drafted -> reviewing_draft (R1 claim) | plan_watcher.go:82 ClaimPlanStatus | types_..._test.go:112 | NONE |
| medium | legality_only | drafted -> requirements_generated (review-skip) | handleRequirementsGenerated mutations.go:305 | types_..._test.go:59 | NONE |
| medium | legality_only | reviewed -> approved (auto + promote) | planStore.approve plan_store.go:421/433 | NONE | http_promote_test.go:13-50 |
| medium | legality_only | generating_requirements -> rejected | reqgen sendGenerationFailed component.go:609 | types_..._test.go:125 | NONE |
| medium | legality_only | requirements_generated -> changed | http_plan_decision.go:303 | types_..._test.go:197 | NONE |
| medium | legality_only | requirements_generated -> rejected (skip-path infra) | arch-gen component.go:308 sendGenerationFailed | types_..._test.go:81 | NONE |
| medium | legality_only | generating_architecture -> rejected | arch-gen sendGenerationFailed component.go:889 | types_..._test.go:134 | NONE |
| medium | legality_only | architecture_generated -> rejected | handleGenerationFailedMutation mutations.go:721 | types_..._test.go:90 | NONE |
| medium | legality_only | preparing_stories -> stories_generated | handleStoriesMutation mutations.go:404/426 | NONE | stories_scenarios_mutation_test.go:60 |
| medium | legality_only | preparing_stories -> rejected (exhaustion) | story-preparer component.go:758 sendGenerationFailed | UNKNOWN | NONE |
| medium | legality_only | scenarios_generated -> ready_for_execution (reactive-skip, unrealized) | UNKNOWN | types_..._test.go:95 | NONE |
| medium | legality_only | reviewing_scenarios -> created (R2 plan-level) | handleRevisionMutation mutations.go:1544 | types_..._test.go:164 | r2_reentry_test.go:179 (helper-only) |
| medium | legality_only | reviewing_scenarios -> requirements_generated (R2 arch-level) | handleRevisionMutation mutations.go:1544 | types_..._test.go:166 | r2_reentry_test.go:33 (helper-only) |
| medium | legality_only | reviewing_scenarios -> architecture_generated (R2 stories/scenarios) | handleRevisionMutation mutations.go:1544 | types_..._test.go:168 | r2_reentry_test.go:99/:146 (helper-only) |
| medium | legality_only | ready_for_execution -> changed | http_plan_decision.go:303 | types_..._test.go:201 | NONE |
| medium | legality_only | implementing -> awaiting_review (QA none, gated) | targetForQALevel execution_events.go:588 | types_..._test.go:182 | NONE |
| medium | legality_only | implementing -> reviewing_qa (Phase 2f, **UNPERFORMED**) | NONE | types_..._test.go:236 | NONE |
| medium | legality_only | implementing -> changed | http_plan_decision.go:303 | types_..._test.go:202 | NONE |
| medium | legality_only | reviewing_qa -> awaiting_review (QA approved, gated) | handleQAVerdictMutation mutations.go:1706 | types_..._test.go:246 | NONE (shouldGateReview never forced) |
| medium | legality_only | awaiting_review -> rejected (**IN DOC NOT IN CODE**) | UNKNOWN (no review.reject subject) | types_..._test.go:186 | NONE |
| medium | legality_only | awaiting_review -> archived (operator) | handleDeletePlan http.go:1133 | types_..._test.go:187 | NONE |
| medium | legality_only | complete -> changed | http_plan_decision.go:303 | types_..._test.go:203 | NONE |
| medium | legality_only | rejected -> approved (manual R2 restart) | planStore.approve plan_store.go:433 | types_..._test.go:172 | NONE |
| medium | legality_only | rejected -> created (manual R1, **ORPHAN**) | UNKNOWN | types_..._test.go:170 | NONE |
| medium | none | created -> drafted (revision shortcut) | handleDraftedMutation mutations.go:790/843 | types_..._test.go:275 | scope_shrinkage_test.go:116-167 |
| medium | none | architecture_generated -> preparing_stories (claim) | story-preparer component.go:292 ClaimPlanStatus | types_..._test.go:86 | contract_observability.go:271 (Tier1) |
| medium | none | stories_generated -> generating_scenarios (claim) | scenario-gen plan_watcher.go:60 ClaimPlanStatus | types_..._test.go:139 | contract_observability.go:281 (Tier1) |
| medium | none | task pending -> developing | exec-mgr handleTaskPending task_watcher.go:72 | N/A | integration_test.go:74 (real watcher) |
| medium | none | task reviewing -> developing (OUTCOME failure, burns cycle) | exec-mgr handleReviewerCompleteLocked | N/A | component_test.go:1480 |
| medium | none | task -> error (worktree/merge/timeout) | exec-mgr markErrorLocked component.go:1413 | N/A | component_test.go:874/1014/1524/... |
| medium | none | story failed -> pending (story_reprepare walk) | storyStatusWalkToReady recover_completed | story_task_test.go:47 | recover_completed_test.go:272 |
| low | no_test | created -> rejected (escalation) | handleGenerationFailedMutation mutations.go:700 | NONE | NONE |
| low | no_test | reviewed -> rejected | handleGenerationFailedMutation mutations.go:700 | NONE | NONE |
| low | no_test | [event] lesson decompose publish -> recorded | exec-mgr publishLessonDecomposeRequest | N/A | PARTIAL (decomposer side covered) |
| low | legality_only | explored -> drafted (legacy/skip) | handleDraftedMutation mutations.go:790/843 | types_..._test.go:266 | NONE |
| low | legality_only | explored -> rejected | handleGenerationFailedMutation mutations.go:700 | types_..._test.go:268 | NONE |
| low | legality_only | drafted -> reviewed (legacy) | handleReviewedMutation mutations.go:1001 | types_..._test.go:61 | NONE |
| low | legality_only | drafted -> rejected | handleGenerationFailedMutation mutations.go:700 | types_..._test.go:63 | NONE |
| low | legality_only | scenarios_generated -> reviewed (legal-but-dead) | UNKNOWN | types_..._test.go:93 | NONE |
| low | legality_only | scenarios_generated -> changed | http_plan_decision.go:303 | types_..._test.go:199 | NONE |
| low | legality_only | scenarios_generated -> rejected | handleGenerationFailedMutation mutations.go:728 | types_..._test.go:97 | NONE |
| low | legality_only | reviewing_scenarios -> reviewed (legacy-dead) | UNKNOWN | types_..._test.go:150 | NONE |
| low | legality_only | scenarios_reviewed -> changed | http_plan_decision.go:303 | types_..._test.go:200 | NONE |
| low | legality_only | implementing -> reviewing_rollup (legacy **DEAD**) | NONE | NONE | NONE |
| low | legality_only | changed -> rejected | handleGenerationFailedMutation mutations.go:728 | UNKNOWN | NONE |
| low | none | exploring -> explored | handleExploredMutation mutations.go:742/775 | types_..._test.go:256 | explored_mutation_test.go:42-68 |

## Open-issue reconciliation (#221 et al.)

This audit was reconciled against the full open-issue queue (`gh issue list --state open`,
2026-06-19), code-grounded with `file:line` / grep-count-0 proof. **#221 — "audit deterministic
state machines for full-auto no-wedge invariants" — is the umbrella; this report is its
state-machine inventory + gap list.**

### Reconcile verdict

Every named paid-run regression is now **pinned by deterministic, currently-passing Go unit
tests on `main`** — and none of them rests on the rotting mock ladder (#163/#162). The audit's
15 gaps + matrix correctly cover the redispatch/recovery/`architecture_revise` **edges**. But
the **no-wedge territory is not fully pinned**: #238's whole-phase escalation path, #157's claim
CAS, the row-87 auto-reject arm, and a cluster of **performer-attached invariants** (which an
edge-indexed matrix structurally cannot represent) remain testable-but-untested.

### Corrections to prior intel (verified against current code)

1. **#203's named "still-open blind spot" (`resetRequirementExecutionsByID`) is already FIXED.**
   `mutations.go:2161` delegates to `resetRequirementExecutions(...,"requirements",reqIDs)`;
   `shouldResetExecutionEntry` (`http.go:1509`) resolves `task.<slug>.node-*` by `requirement_id`;
   regression test `reset_execution_taskkeys_test.go:73` passes. Only the *architectural* ask (a
   typed `(entityKind,slug,id)` descriptor) remains — a refactor, not a missing test.
2. **#176 (assembly-conflict fail-fast + reclassify) is substantially DONE** —
   `PlanDecisionKindAssemblyConflict` + `routeAssemblyConflict`/`failPlanOnAssemblyConflict`
   (`execution_events.go:441/471`), pinned by `plan_qa_worktree_test.go:225`. Effectively closeable.
3. **`aa9ddf46`/#234 is a UI-only PR (0 Go tests) and is NOT the #229 fix.** The #229 QA-recovery
   node-advance fix is **PR #230 / `79fc70ad`** (`req_completions_recovery_test.go`).
4. **#221's body claim that `scope_incomplete` is missing from `PlanDecisionKind.IsValid()` is
   STALE** — `types.go:2008` includes it (`plan_decision_kind_test.go:11-25`). Update the issue.
5. **Audit gap 4 mis-aims at #237.** It targets the requirements-mutation gate, which has no
   architecture-component data to validate. The placement lint belongs at the **architecture /
   R1 review** (`drafted->reviewing_draft`), not the requirements mutation. As written, gap 4
   will not close #237 (see corrected gap N14 below).
6. **#238 recurred LIVE on 2026-06-19 even though the scoped path is tested.** The scoped reset
   (`architecture_revise_test.go:151/:438`) passes, but the **whole-phase escalation fallback**
   (`contractImpactAllowsWholePhaseReset`, reachable because #211 forces `ContractImpact.Kind==change`)
   and the recovery-agent `AffectedReqIDs`-population boundary are **untested** (gap 5 expansion).

### Closed-fix regression status — all PINNED (no recurrence risk in the state machine)

| Fix (commit) | Pinned? | Test evidence |
|--------------|:-------:|---------------|
| **#227** (`8dd7424a`) — #226 redispatch (`ForceRequirementIDs` delete+recreate) + closure gates | ✅ | 3 layers RUN+PASS: `scope_incomplete_recovery_test.go:53-64`, `force_redispatch_test.go:9`, `req_create_force_test.go` (Stage `active->pending`, dup-without-Force rejected, sequential req1→req2); `completeness_gate_test.go:97` |
| **#230** (`79fc70ad`) — #229 QA-recovery rehydrate-and-advance | ✅ | `req_completions_recovery_test.go` `TestHandleTaskStateChange_RehydratesReqExecAndAdvancesRecoveredQANode` (CurrentNodeIdx==1, node1 dispatched, VisitedNodes, CommitSHA) |
| **#236** (`7de7f430`) — #235 recovery submit-schema dead-reject | ✅ | `recovery-agent/result_test.go:162` (salvage diagnosis from review-shaped JSON) + `:244` (schema-field reflection, review-only fields absent) + `schemas_test.go:65` (7-action enum) + `strict_mode_test.go:19` |
| **#216** (`915ceb1e`) — `architecture_revise` scoped artifact reset | ✅ (reset slice only) | `architecture_revise_test.go` ScopedReset…/PreservesUnrelatedCompletedExecutions. **Caveat:** pins the RESET path; the **#238 whole-LAYER cascade** + **#211 auto-accept race** remain OPEN in the same edge. |
| **#200** — QA `conditionally_approved` enum (fail-closed on skips) | ✅ | `qa-reviewer/skip_verdict_test.go` (7 subcases incl. approved+skips→needs_changes) + plan-manager `TestHandleQAVerdictMutation_AcceptsConditionallyApproved` |
| **#234** (`aa9ddf46`) — live-run observability | n/a (UI) | 0 Go tests — frontend Playwright; out of state-machine scope. |

### #221 required-invariant checklist

| # | Invariant | Status | Gap to close |
|---|-----------|:------:|--------------|
| INV1 | No `PlanDecisionStatusProposed` waits forever (auto / human-gate / terminal) | **partial** | Add the **watchdog test**: table-drive `IsValid()`'s 7 kinds asserting each auto-accepts OR is human-gated with a visible-paused class — fail if a kind lands in no-owner limbo. Fix write-only `ForbiddenMoves`/`AcceptanceObligations`. |
| INV2 | Every `PlanDecisionKind` valid + routed + cascade/accept-tested | **partial** | `applyScopeIncompleteRecovery` + `applyRequirementChange` have 0 direct test-files (only via accept HTTP handler). Add direct unit tests. |
| INV3 | Every recoverable rejected/stalled state has a tested transition back into dispatch | **partial** | OPEN: #238 whole-phase escalation + `AffectedReqIDs` boundary (recurred 2026-06-19); #157 claim CAS; row-87 `AutoRejectOnExhaustion` arm (behav=NONE). Add the 3 #238 tests, the #157 adversarial claim test, the row-87 test. |
| INV4 | Deferred-terminal timers don't punish intentional human-gated waits | **met** | None for the invariant. The OPEN part (#211: should full-auto auto-accept a policy-safe scoped `architecture_revise`?) is a product-policy decision → ADR + a `should_auto_accept_test.go` case. |
| INV5 | Recovery diagnostics reach the next responsible agent (no dead-reject on a present-in-prose field) | **partial** | #235 PINNED. OPEN: #82 producer call-site seam; #81 `AffectedStoryIDs` population; #31 RecoveryID panic guard; #32 trajectory typed-nil. |
| INV6 | UI phase derived from authoritative state machine, not stale heuristics | **unmet** | Out of Go scope but IN #221's deliverable: a **frontend vitest contract test** mapping every authoritative `Status` (+ active-loop/PlanDecision/recovery) → operator banner, asserting monotonicity. Also: **write this 6-invariant checklist into `docs/e2e-flow.md`** (currently 0 hits). |

### New deterministic-test backlog beyond the audit's 15

These are statically-testable issue defects the edge-indexed matrix could not represent
(performer-attached invariants, deterministic gates, payload-population, panic guards). Ordered
roughly by no-wedge value.

| # | Issue | Kind | Target | Assertion |
|---|-------|------|--------|-----------|
| N1 | #224 | component_unit (+cancel-publisher seam) | `execution-manager/mutations.go` handleReqResetMutation | Resetting a req/task exec with an active non-terminal child loop emits `SignalCancel`; terminal loops skipped; cache+KV still deleted. |
| N2 | #82 | component_unit (+function-field seam) | `requirement-executor/component.go:2224/2144`, `awaiting_recovery.go:368` | `resumeFromRecoveryLocked` fires seam with storeKey + EMPTY NodeResults; `startRestructureRetryLocked` fires full-reset (`:2224`) vs kept-results replace (`:2144`) — pin each site. |
| N3 | #81 | component_unit (recoveryPublisher stub) | `execution-manager/component.go:1347` markEscalatedLocked | Escalation captures a `RecoveryRequested` whose `AffectedStoryIDs` == parent Story IDs (currently grep=0). |
| N4 | #80 | component_unit | `plan-manager/mutations.go:425` | Re-emit Stories C+D over A+B → A/B scenarios dropped, empty-StoryID legacy survives. (hygiene) |
| N5 | #31 | component_unit | `recovery-agent/component.go:802` or `payloads/recovery.go:223` | `RecoveryID="abc"` → no panic, well-formed decision ID. |
| N6 | #32 | component_unit | `internal/trajectory/trajectory.go:107` | Typed-nil client → "nats client required" error, not panic; then drop the call-site guard. |
| N7 | #157 | component_unit (concurrent) | `execution-manager/mutations.go:639` | Add `ExpectedFromStage`; wrong-source claim rejected; two concurrent `pending→developing` claims → exactly one `Success=true`. |
| N8 | #153 | behavioral regression + AST-lint | `execution-manager/execution_store.go:475` | saveTask→raw UpdateTriple bypass→saveTask hash-back → persisted ≠ stale; AST-lint fails raw TaskExecution writes outside execution_store.go. Prereq: fold CurrentStage/ErrorClass/Prompt into OwnedPredicates. |
| N9 | #137 | reflection parity (RED-first) | `tools/terminal/schema_struct_parity_test.go` | Route all 9 remaining deliverables through `assertSchemaStructParity`; fold recovery one-off in; prove it bites ≥1 of the 9. |
| N10 | #206 | per-runner adapters + gate | `cmd/sandbox/qa_subscriber.go:392` + new model | PREREQ: `NormalizedTestResult{suite,testId,status,scenarioTag}` adapters (go/jest/pytest/JUnit) vs real fixtures. THEN: scenario w/ no ran test → finding; orphan test → finding. Don't credit `validateApprovedScenarioVerdicts`. |
| N11 | #204 | component_unit | `structural-validator/stub_artifact_detector.go` | With do-not-stub in `Plan.Constraints`: new local src stub of a prohibited external FQN → finding; absent → clean. (Layer B coverage-fidelity is LLM-quality.) |
| N12 | #175 | component_unit | `workflow/derive_story_scheduling.go` | Two stories both owning `README.md` → a `DependsOn` edge (serialize), not parallel. (reject-if-unowned gate already tested.) |
| N13 | #210 | component_unit (producer + inert-gate) | `plan-manager/plan_store.go:286` | repoPath≠manifest-dir → TopologyFacts stays EMPTY (pins bug); fix → populated. Empty facts + unapproved build.gradle → topology findings == 0 (pins inert-gate). |
| N14 | #237 | component_unit (**corrects gap 4**) | `plan-reviewer/architecture_rules.go` (new rule) | Component `mavsdk-semantic-datastreams` (role=telemetry) owning `ConstAltitudeLLA.java`+`PD.java` → placement finding per file, via path/package/role keyword heuristic. **Wire at architecture/R1 review, NOT requirements mutation.** |
| N15 | #211 | component_unit | `plan-decision-handler/recovery_autoaccept.go:260` | (a) accepted `architecture_revise` enforces `ForbiddenMoves` — FAILS today (exposes write-only field); (b) replace prose `recoveryDecisionMentionsAction` with typed `RecoveryActionKind`. |
| N16 | #162 | component_unit (**closes row 87**) | `plan-manager/execution_events.go:330` | completed=0/failed=1/total=1 → `rejected`; completed=1/failed=1/total=2 → NOT auto-reject. Pins `AutoRejectOnExhaustion` without the flaky mock scenario. |
| N17 | #180 | component_unit | `scenario-orchestrator/component.go:478` | Inject `triggerRequirementExecution` returning "req execution already exists" → `dispatchAndLog` returns nil (pins Debug downgrade, not just the predicate). |

Plus three **#238 whole-phase** tests (expand gap 5): (a) a decision with `AffectedReqIDs=[req2]`
AND `ContractImpact` containing `phase:*` must return scope=`requirements` (closure wins); (b)
`architecture_revise` with EMPTY `AffectedReqIDs` is REJECTED in full-auto; (c) integration test
that a recovery-agent-emitted decision persists `AffectedReqIDs` through propose→accept.

**Out of state-machine scope** (route elsewhere, do not put in the per-state Go backlog): #83
(mutex-discipline doc drift → go-reviewer call-site sweep; author says `-race` can't catch it),
#201 (per-step retry metrics → observability feature), #217/#219/#234 (UI projection → frontend
vitest/Playwright; these ARE the #221 INV6 deliverable).

### Mock-ladder hollow-coverage flags

The matrix's behavioral credits are overwhelmingly pure Go unit tests (grep of
`test/e2e|scenarios/|mock-` in the matrix = 0). The only credits resting on a currently-**RED**
#162 scenario:

- **Row 106** (`rejected->ready_for_execution` stall recovery): Tier-2 credit `plan_stall_recovery.go:580`
  is a broken #162 scenario → **hollow**, but the edge survives on a Go-unit backup
  (`scope_incomplete_recovery_test.go:13`). Annotate as hollow; edge is not uncovered.
- **Row 96** (`implementing->complete` via stall recovery): same broken credit; row already
  behav=NONE so no false comfort — annotate the credit.
- **Verified GREEN (not hollow), stated explicitly so it isn't assumed:** `contract_observability.go`
  (×9 credits), `plan_phase.go` (×3), `qa_cycle.go` (×4) are NOT in the #162 broken-8.
- **Adjacent semantic (not skipped-test) hollowness:** `validateApprovedScenarioVerdicts`
  (`component.go:1995`) is credited as coverage but only checks reviewer-asserted
  `ScenarioVerdict.Passed` *presence*, not that a real test ran — hollow for #206's purpose.
  And matrix row 95 (`qa_verdict_test.go:20`) asserts only the verdict summary, not
  `stored.Status==rejected`.

## Recommended execution order (for the fixes pass)

This stays audit-only; the fixes are a separate, reviewable pass. Suggested order:

1. **Cheap static net first (highest leverage, lowest risk):** the 4 chokepoint unit tests
   (audit gaps 7/8/14 + `handleApprovedMutation`) and the missing **legality rows** (gaps 6/15 +
   pattern 7 list). These harden ~15 edges and make the legality table mirror `CanTransitionTo`.
2. **Doc fixes (14):** correct the state chart (add `scenarios_reviewed`, `changed` fan-in,
   `rejected` back-edges, terminal/unarchive edges), re-source PR-feedback + convergence
   attribution, fix the stale `reviewing_rollup`/contract-loss prose, document `phaseError`.
   Decide each of the **6 dead edges**: pin FALSE + tighten `CanTransitionTo`, or implement+test.
3. **No-wedge territory (the gate's whole point):** gaps 1/2/3 (executor claim #226, QA-reject
   schema #226/#235, restructure rebuild) + the #238 whole-phase tests + N7 (#157 claim CAS) +
   N16 (#162 auto-reject arm) + N14 (#237 placement lint at R1).
4. **#221 deliverables:** the watchdog test (INV1), write the invariant checklist into
   `docs/e2e-flow.md` (INV6), and the frontend UI contract test (INV6, separate track).
5. **Remaining backlog (N1-N6, N8-N13, N15, N17):** performer-attached invariants, schema parity,
   coverage gate, topology-facts wiring — as focused follow-ups.

Nothing in this list requires a paid-LLM run. The whole point of the gate holds: this is all
deterministic, offline-testable work.
