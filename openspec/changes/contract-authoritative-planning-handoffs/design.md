## Context

SemSpec already has the major process pieces: BMAD-aligned personas, OpenSpec-shaped capabilities,
PLAN_STATES, EXECUTION_STATES, Stories, PlanDecisions, recovery actions, scope completeness checks, QA
evidence, and UI feed surfaces. The failure mode is not absence of process. The failure mode is that
hard constraints can become prose-only between artifacts.

The MAVLink/OSH run exposed the gap:

- The prompt required work inside an existing brownfield baseline.
- Planning and recovery could narrow the current plan contract without preserving why the earlier contract changed.
- Developer work could present a clean-room standalone project shape.
- QA caught the composite build failure late.
- UI did not clearly explain the execution, recovery, waiting, and lesson activity that led there.

This change makes the contract explicit and comparable across BMAD/OpenSpec handoffs. BMAD and
OpenSpec remain the human-facing workflow vocabulary. SemSpec's Plan, Execution, and SKG facts become
the authoritative contract substrate.

## Goals / Non-Goals

**Goals:**

- Preserve the original sponsor/project brief as an immutable contract packet.
- Carry the same contract packet into planner, architect, product, story, scenario, developer, reviewer,
  recovery, and QA prompts.
- Require downstream artifacts to explain whether they preserve, refine, or change contract obligations.
- Validate brownfield topology before expensive execution and before QA accepts work.
- Govern scope shrinkage and recovery cascades through explicit PlanDecisions and targeted dirty marking.
- Make UI/API state truthful enough that an operator knows what is happening without asking.

**Non-Goals:**

- Replace PLAN_STATES, EXECUTION_STATES, or the Story execution model.
- Replace BMAD roles or OpenSpec artifact layout with SemSpec-only vocabulary.
- Build language-specific MAVLink or Java rules into the core contract model.
- Solve long-term lesson hydration across runs. This change only surfaces lesson activity and effect.
- Guarantee every validation can be deterministic on day one. LLM reviewers may remain as semantic gates,
  but deterministic gates must own structural invariants where the data exists.

## Decisions

### 1. Add contract authority as Plan-owned state, not a parallel workflow

The plan-manager remains the single writer for plan-level state. Add a Plan-owned contract packet, or an
equivalent versioned child entity, containing:

- original brief text or digest plus source references
- extracted non-negotiable constraints
- brownfield topology obligations
- baseline file/module/package/build inventory summaries
- acceptance obligations and forbidden moves
- lifecycle and scope provenance
- current amendment ledger

Alternative considered: a separate contract-manager component. Rejected for the first implementation because
it would introduce another owner for plan truth. A future component can be split out after the entity shape is
stable.

### 2. Use immutable root contract plus amendment ledger

The initial contract is immutable. Later changes are amendments attached through PlanDecisions. Agents may
propose a change to scope, topology, or acceptance obligations, but they do not silently mutate the baseline
contract.

This makes recovery safer: `architecture_revise`, `story_reprepare`, `split_req`, and `narrow_scope` can all
carry a contract impact summary. Auto-accept remains possible only when the impact is within policy.

Alternative considered: store only the current plan shape and compare to the previous plan version. That still
lets the original brief fade out after enough revisions.

### 3. Render role-specific contract packets at every handoff

Each BMAD role receives a concise projection of the same authoritative packet:

- Mary sees sponsor intent, open questions, constraints, and brownfield obligations.
- Winston sees topology, existing module boundaries, and forbidden architecture replacements.
- John and Sarah see capability, requirement, Story, scope, and acceptance obligations.
- Amelia and reviewers see allowed files, must-deliver obligations, forbidden moves, and amendment context.
- Recovery and QA see the root contract, amendments, topology evidence, and why the current shape changed.

The projection can be short. The identity and amendment references must be stable so UI and traces can link back
to the same source.

### 4. Validate topology through pluggable detectors

Brownfield topology validation is generic. A detector emits topology facts such as:

- repository root and allowed build roots
- build files and included modules
- package/class/module naming conventions
- baseline extension points
- forbidden standalone project markers for the current repo
- QA substitution or composite-build expectations

The first regression fixture can use the OSH composite Gradle failure, but the core model must support Go
modules, Node packages, Python packages, Rust crates, and mixed repos.

### 5. Compare every downstream artifact to the root contract

Plan-reviewer and structural-validator should validate the current artifact against the immutable contract, not
only against the current mutable scope. Examples:

- current scope cannot drop named baseline obligations without an accepted amendment
- Stories cannot cover a clean-room component when the contract says extend an existing baseline
- developer output cannot add a standalone build root that conflicts with the topology packet
- QA build failures that match topology defects must route to topology recovery, not generic test failure

### 6. Make recovery target dirty nodes instead of clearing by phase

PlanDecision effects should dirty the smallest correct closure:

- requirement/story/scenario nodes directly affected by the decision
- dependent nodes that consume changed outputs
- execution entries tied to those nodes
- phase summaries derived from the invalidated artifacts

Whole-phase wipes remain valid only when the contract impact proves the entire phase is obsolete.

### 7. Normalize UI state from authoritative summaries

The UI should not infer the current truth from stale feed rows. Add or extend summary surfaces that expose:

- current plan stage and active BMAD/OpenSpec phase
- active loop IDs, roles, states, and last update times
- execution Stories/tasks, waits, blockers, and terminal outcomes
- recovery decisions and whether they were proposed, auto-accepted, waiting, rejected, or applied
- lesson decomposer/curator activity and whether lessons affect current or future runs
- QA evidence and failure classification
- stale/disconnected data indicators

The banner, left nav, feed, graph, files, and detail views should all read from the same normalized state.

## Risks / Trade-offs

- Contract packet gets too large -> use role-specific projections with stable contract IDs and expandable UI detail.
- Agents overfit to "contract changed" language -> require closed-set impact fields and deterministic diff checks.
- Topology detection becomes language-specific sprawl -> keep detectors pluggable and require generic topology facts.
- Recovery becomes too conservative -> allow auto-accept for policy-safe amendments with explicit contract impact.
- UI becomes noisy -> summarize current truth first, then provide drill-down for loops, traces, lessons, and artifacts.
- Existing in-flight plans lack contract packets -> treat them as legacy and do not backfill enforcement blindly.

## Migration Plan

1. Introduce the contract packet shape and persistence behind compatibility defaults.
2. Populate packets for new plans from prompt, project config, existing scope, and topology detection.
3. Add prompt projections for one role at a time, starting with planner, architect, developer, recovery, and QA.
4. Add deterministic validators in warning mode, then make structural violations blocking after fixtures pass.
5. Add UI summary endpoints and migrate banner/feed/detail views onto them.
6. Add MAVLink/OSH clean-room topology and scope-collapse fixtures as regression tests.

Rollback is straightforward while enforcement is warning-only: stop rendering contract projections and ignore new
fields. After blocking gates land, rollback must also disable those gates for newly-created plans.

## Open Questions

- Should the immutable brief store full prompt text, a digest plus artifact reference, or both?
- What threshold makes scope shrinkage require human review rather than auto-accept?
- Which topology detectors are required for the first implementation slice?
- Should lesson hydration become a separate OpenSpec change after observability exposes its current cost and value?
