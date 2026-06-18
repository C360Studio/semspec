## 1. Contract Model And Fixtures

- [x] 1.1 Add workflow types for the root contract packet, amendment ledger, contract impact, and topology facts
- [x] 1.2 Persist the contract packet with new plans through plan-manager without changing legacy plan loading
- [x] 1.3 Write graph/vocabulary predicates for contract identity, constraints, topology facts, amendments, and validation findings
- [x] 1.4 Add unit tests proving new plans get a contract packet before analyst/planner handoff
- [x] 1.5 Add a generic brownfield fixture plus the MAVLink/OSH clean-room regression fixture

## 2. BMAD/OpenSpec Handoff Propagation

- [x] 2.1 Add role-specific contract projection builders for planner, architect, requirement-generator, story-preparer, scenario-generator, developer, reviewer, recovery, and QA
- [x] 2.2 Update prompt/domain renderers to include contract packet identity, constraints, topology obligations, and accepted amendments
- [x] 2.3 Add prompt rendering tests that prove forbidden moves and must-deliver obligations reach each role
- [x] 2.4 Update OpenSpec/BMAD artifact emission so contract packet references are visible in generated artifacts
- [x] 2.5 Update ADR-040 or add a follow-up ADR describing contract authority as the governing layer over BMAD/OpenSpec projections

## 3. Brownfield Topology Validation

- [x] 3.1 Implement topology detectors for repository root, build roots, package/module manifests, and known workspace/composite-build markers
- [x] 3.2 Add plan-reviewer or structural-validator checks for architecture outputs that violate topology facts
- [x] 3.3 Add Story ownership checks that reject standalone or baseline-erasing file ownership before developer execution
- [x] 3.4 Add developer-output checks that reject forbidden build roots, standalone project files, or topology-incompatible artifacts
- [x] 3.5 Add QA failure classification for build-configuration and topology failures before recovery chooses an action

## 4. Scope Governance And Recovery

- [x] 4.1 Add contract-impact fields to PlanDecision creation, recovery-agent output parsing, and auto-accept policy
- [x] 4.2 Add validation that compares current scope and Story coverage against the root contract plus accepted amendments
- [x] 4.3 Add scope-shrinkage guardrails that require explicit amendment provenance for dropped obligations
- [x] 4.4 Update recovery accept effects so dirty marking and execution resets use the smallest correct dependency closure
- [x] 4.5 Add tests proving unrelated completed work survives late architecture/story recovery
- [x] 4.6 Add tests proving whole-phase reset requires explicit contract impact evidence

## 5. Execution Observability API

- [x] 5.1 Add or extend plan-manager summary APIs for current phase, active loops, execution progress, waits, recovery, lessons, QA, and staleness
- [x] 5.2 Normalize feed events so execution rows, recovery decisions, orphaned rows, and stale rows have distinct machine-readable kinds
- [x] 5.3 Expose lesson-decomposer and lesson-curator activity with current-run versus future-run effect labels
- [x] 5.4 Expose cost accounting with measured usage, configured provider rate source, and unknown-rate fallback
- [x] 5.5 Regenerate OpenAPI and UI generated TypeScript types after API shape changes

## 6. UI Implementation

- [x] 6.1 Update the plan banner to derive from authoritative phase summaries and show execution as a first-class phase
- [x] 6.2 Update left navigation and detail panes so Plans, Graph, and Files views remain clickable and state-coherent during live runs
- [x] 6.3 Add execution detail showing Stories, tasks, loops, waits, blockers, terminal outcomes, and QA evidence
- [x] 6.4 Add recovery and PlanDecision detail showing diagnosis, affected nodes, contract impact, and auto-accept status
- [x] 6.5 Add lesson activity UI showing lesson cost and whether lessons affect current or future runs
- [x] 6.6 Add stale/disconnected indicators with last successful update timestamps

## 7. Verification And Rollout

- [x] 7.1 Add unit tests for contract packet persistence, prompt projection, topology validation, and scope-governance rules
- [x] 7.2 Add UI component tests for phase banner, execution detail, recovery detail, lesson activity, and stale data states
- [x] 7.3 Add E2E mock scenarios for scope shrinkage, targeted recovery, topology rejection, and execution observability
- [x] 7.4 Run `go test ./...` after backend slices land
- [x] 7.5 Run UI checks and relevant Playwright scenarios after frontend slices land
- [ ] 7.6 Run the MAVLink/OSH hard-run ladder once the artifact, backend, and UI slices are merged

## 8. PR Review Enforcement Follow-Ups

- [x] 8.1 Populate runtime plan contract packets with brownfield topology facts before architecture and Story gates rely on them
- [x] 8.2 Capture the first real drafted scope as the contract baseline and enforce scope-shrink checks on recovery acceptance paths
- [x] 8.3 Add a legal post-QA architecture recovery transition and regression coverage for `rejected -> requirements_generated`
- [x] 8.4 Floor recovery contract-impact policy by action kind so scope/topology-changing actions cannot self-report as preserve/refine
- [x] 8.5 Fix UI false-green and stale observability state regressions from the PR review
- [x] 8.6 Fix scoped `architecture_revise` so a single dirty requirement closure does not rewrite unrelated Stories/Scenarios

## 9. Recovery State Machine Audit Follow-Ups

- [x] 9.1 Make `scope_incomplete` a first-class PlanDecision kind across validation, cascade, and auto-accept policy
- [x] 9.2 Apply `scope_incomplete` accept effects that write recovery guidance and move rejected plans into active retry state
- [x] 9.3 Prevent requirement-executor from treating `scope_incomplete` as generic completed-QA recovery
- [x] 9.4 Add deterministic tests for scope-incomplete validation, auto-accept, accept effects, cascade, prompt guidance, and executor handling
- [x] 9.5 Add or update tests for architecture-revise full-auto timeout policy so human-gated decisions cannot self-fail invisibly
- [x] 9.6 Run the relevant mock E2E handoff scenario after unit coverage passes
- [x] 9.7 Fix mock E2E execution-phase reporting so intermediate snapshots do not overwrite final plan status details
- [x] 9.8 Stabilize mock E2E startup by bypassing unused graph model services and persisting the semembed model cache for real graph stacks

## 10. Deterministic State Mutation Audit Follow-Ups

- [x] 10.1 Audit PlanDecision accept paths for half-applied state when reset or cascade effects fail
- [x] 10.2 Ensure plan-store reads and writes isolate cached plans from caller mutation
- [x] 10.3 Add regression coverage for failed PlanDecision accept effects leaving persisted plan state unchanged
- [x] 10.4 Fix any cache/KV mutation atomicity defects found by the audit
- [x] 10.5 Run focused plan-manager tests, repo tests, lint, and relevant mock E2E coverage

## 11. Progressive Deliverable Closure Gates

- [x] 11.1 Add deterministic Story/Requirement gates that compare accepted `scope.create` obligations against delivered files before marking execution complete
- [x] 11.2 Retry the current Story with missing-file feedback when a reviewer approves scenarios but declared deliverables are absent
- [x] 11.3 Fail closed with actionable recovery evidence when deliverable closure cannot be inspected or retries are exhausted
- [x] 11.4 Add unit coverage for path normalization, Story-scoped missing deliverables, clean approvals, and retry/exhaustion behavior
- [x] 11.5 Run focused requirement-executor tests plus the relevant mock E2E handoff coverage

## 12. Scope-Incomplete Redispatch Recovery

- [x] 12.1 Carry affected requirement IDs from accepted `scope_incomplete` decisions into the scenario-orchestrator retry trigger
- [x] 12.2 Force-recreate only those affected requirement execution rows during redispatch so stale `req.*` rows cannot wedge the retry
- [x] 12.3 Return reset/delete failures from execution-manager instead of reporting successful recovery with durable execution state still present
- [x] 12.4 Add focused unit coverage for trigger force IDs, req.create force recreation, and ordinary duplicate-dispatch rejection
- [x] 12.5 Run focused plan-manager, scenario-orchestrator, execution-manager, and mock execution-phase coverage
