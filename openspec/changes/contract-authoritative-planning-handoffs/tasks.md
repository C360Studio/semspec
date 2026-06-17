## 1. Contract Model And Fixtures

- [ ] 1.1 Add workflow types for the root contract packet, amendment ledger, contract impact, and topology facts
- [ ] 1.2 Persist the contract packet with new plans through plan-manager without changing legacy plan loading
- [ ] 1.3 Write graph/vocabulary predicates for contract identity, constraints, topology facts, amendments, and validation findings
- [ ] 1.4 Add unit tests proving new plans get a contract packet before analyst/planner handoff
- [ ] 1.5 Add a generic brownfield fixture plus the MAVLink/OSH clean-room regression fixture

## 2. BMAD/OpenSpec Handoff Propagation

- [ ] 2.1 Add role-specific contract projection builders for planner, architect, requirement-generator, story-preparer, scenario-generator, developer, reviewer, recovery, and QA
- [ ] 2.2 Update prompt/domain renderers to include contract packet identity, constraints, topology obligations, and accepted amendments
- [ ] 2.3 Add prompt rendering tests that prove forbidden moves and must-deliver obligations reach each role
- [ ] 2.4 Update OpenSpec/BMAD artifact emission so contract packet references are visible in generated artifacts
- [ ] 2.5 Update ADR-040 or add a follow-up ADR describing contract authority as the governing layer over BMAD/OpenSpec projections

## 3. Brownfield Topology Validation

- [ ] 3.1 Implement topology detectors for repository root, build roots, package/module manifests, and known workspace/composite-build markers
- [ ] 3.2 Add plan-reviewer or structural-validator checks for architecture outputs that violate topology facts
- [ ] 3.3 Add Story ownership checks that reject standalone or baseline-erasing file ownership before developer execution
- [ ] 3.4 Add developer-output checks that reject forbidden build roots, standalone project files, or topology-incompatible artifacts
- [ ] 3.5 Add QA failure classification for build-configuration and topology failures before recovery chooses an action

## 4. Scope Governance And Recovery

- [ ] 4.1 Add contract-impact fields to PlanDecision creation, recovery-agent output parsing, and auto-accept policy
- [ ] 4.2 Add validation that compares current scope and Story coverage against the root contract plus accepted amendments
- [ ] 4.3 Add scope-shrinkage guardrails that require explicit amendment provenance for dropped obligations
- [ ] 4.4 Update recovery accept effects so dirty marking and execution resets use the smallest correct dependency closure
- [ ] 4.5 Add tests proving unrelated completed work survives late architecture/story recovery
- [ ] 4.6 Add tests proving whole-phase reset requires explicit contract impact evidence

## 5. Execution Observability API

- [ ] 5.1 Add or extend plan-manager summary APIs for current phase, active loops, execution progress, waits, recovery, lessons, QA, and staleness
- [ ] 5.2 Normalize feed events so execution rows, recovery decisions, orphaned rows, and stale rows have distinct machine-readable kinds
- [ ] 5.3 Expose lesson-decomposer and lesson-curator activity with current-run versus future-run effect labels
- [ ] 5.4 Expose cost accounting with measured usage, configured provider rate source, and unknown-rate fallback
- [ ] 5.5 Regenerate OpenAPI and UI generated TypeScript types after API shape changes

## 6. UI Implementation

- [ ] 6.1 Update the plan banner to derive from authoritative phase summaries and show execution as a first-class phase
- [ ] 6.2 Update left navigation and detail panes so Plans, Graph, and Files views remain clickable and state-coherent during live runs
- [ ] 6.3 Add execution detail showing Stories, tasks, loops, waits, blockers, terminal outcomes, and QA evidence
- [ ] 6.4 Add recovery and PlanDecision detail showing diagnosis, affected nodes, contract impact, and auto-accept status
- [ ] 6.5 Add lesson activity UI showing lesson cost and whether lessons affect current or future runs
- [ ] 6.6 Add stale/disconnected indicators with last successful update timestamps

## 7. Verification And Rollout

- [ ] 7.1 Add unit tests for contract packet persistence, prompt projection, topology validation, and scope-governance rules
- [ ] 7.2 Add UI component tests for phase banner, execution detail, recovery detail, lesson activity, and stale data states
- [ ] 7.3 Add E2E mock scenarios for scope shrinkage, targeted recovery, topology rejection, and execution observability
- [ ] 7.4 Run `go test ./...` after backend slices land
- [ ] 7.5 Run UI checks and relevant Playwright scenarios after frontend slices land
- [ ] 7.6 Run the MAVLink/OSH hard-run ladder once the artifact, backend, and UI slices are merged
