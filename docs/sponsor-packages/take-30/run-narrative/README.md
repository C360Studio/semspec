# Run Narrative (Generated)

These markdown documents are **rendered from the run's captured trajectory + structured deliverable data** post-hoc — they were NOT directly authored by the agent during the run. Today the agent emits structured JSON at each phase; the narrative below is what BMAD/OpenSpec-style markdown would look like if we rendered it inline.

## Files

- **`architecture.md`** — Technology choices, component boundaries, data flow, architectural decisions, actors, upstream resolutions with TestHarness, test surface. Generated from `../architecture/architecture-deliverable.json`.
- **`requirements.md`** — 3 requirements with files_owned + dependencies. Generated from `requirement-generator` role's trajectory submissions.
- **`scenarios.md`** — 13 BDD-style scenarios (Given/When/Then). Generated from `scenario-generator` role's trajectory submissions.

## What's missing today (follow-up)

The current `workflow-documents` component writes `plan.md` to the workspace at each phase transition. That document covers a subset of the above but isn't comprehensive. To deliver this style natively (BMAD/OpenSpec parity), the system needs:

1. Per-phase markdown emission as a workflow-documents responsibility (architecture.md, requirements.md, scenarios.md, qa-verdict.md).
2. Agent-authored README enforcement when the goal asks for one — this run's goal explicitly said "Deliver working Java source files, unit tests, **and a README with usage examples**" and the agent shipped no README despite the structural surface being correct otherwise.
3. A single `run-summary.md` synthesizing the run — what the present file approximates.

Tracking as a follow-up; see the project task list.

## Honest framing for the sponsor

The structured data exists end-to-end. The agent emits typed deliverables (TechChoice, ComponentDef, ArchDecision, UpstreamResolution, TestHarness, Requirement, Scenario, QAVerdict). What the system doesn't yet do is auto-render those into the markdown forms a human reviewer (or another agent following BMAD/OpenSpec) wants to consume. That rendering is mechanical given the captured data — the work is wiring it as a workflow-documents output at each phase transition, not generating it. The render-from-JSON script that produced these three documents is ~150 lines of Python.
