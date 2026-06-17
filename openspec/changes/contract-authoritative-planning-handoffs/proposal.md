## Why

Recent hard-run failures showed that SemSpec can preserve a prompt as prose while losing it as an executable contract during BMAD/OpenSpec handoffs. The system needs an authoritative contract layer so planning, recovery, execution, QA, and UI all agree on what is allowed, what changed, and why.

## What Changes

- Add a contract-authority model that pins sponsor intent, brownfield topology, non-negotiable constraints, and acceptance obligations across all planning and execution stages.
- Add scope-change governance so downstream agents and recovery loops cannot silently shrink scope, replace baseline architecture, or wipe unrelated completed work.
- Add brownfield topology validation that rejects clean-room project shapes, standalone build roots, and baseline-erasing implementation plans before expensive developer execution or QA.
- Add execution-state observability so the UI exposes the current BMAD/OpenSpec phase, active/recent loops, recovery decisions, waits, lessons, and QA evidence without stale or orphaned rows.
- Keep BMAD/OpenSpec compatibility: SemSpec uses BMAD roles and OpenSpec artifacts as human-readable workflow surfaces, while PLAN_STATES, EXECUTION_STATES, and SKG facts remain the source of authority.
- No breaking API removals are intended; existing plans should continue to run, with new validations applied to newly-created or newly-recovered work.

## Capabilities

### New Capabilities

- `planning-contract-authority`: Preserves the immutable brief and derived contract packets across BMAD/OpenSpec planning, story, scenario, execution, and recovery handoffs.
- `scope-change-governance`: Records and validates all scope changes, scope shrinkage, dirty-node marking, and recovery cascades against the original contract.
- `brownfield-topology-validation`: Validates repository, module, build, class/package, and integration topology before dev and QA can accept work as structurally plausible.
- `execution-observability`: Presents truthful live UI/API state for planning, execution, recovery, lessons, waits, QA, and terminal outcomes.

### Modified Capabilities

- None.

## Impact

- Workflow domain types for plans, stories, requirements, contract packets, recovery decisions, execution state, lessons, and QA evidence.
- Planner, architecture-generator, requirement-generator, story-preparer, scenario-generator, requirement-executor, recovery-agent, structural-validator, plan-reviewer, execution-manager, qa-reviewer, and plan-manager handoff logic.
- Prompt rendering and role-specific context packets for BMAD-aligned agents.
- Graph/vocabulary predicates for immutable contract, scope provenance, topology evidence, and recovery lineage.
- UI/API surfaces that currently summarize planning/execution and detail view feeds.
- E2E fixtures and hard-run diagnostics, using the MAVLink/OSH clean-room failure as one regression scenario without making the design Java- or MAVLink-specific.
