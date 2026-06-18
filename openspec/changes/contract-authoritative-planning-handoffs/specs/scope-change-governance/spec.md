## ADDED Requirements

### Requirement: Scope changes require provenance
The system MUST record explicit provenance for every material change to scope, acceptance obligations, topology
obligations, or covered capabilities after the initial contract packet is created.

#### Scenario: Scope shrinkage creates an auditable decision
- **WHEN** a planning, review, QA, or recovery step proposes removing files, capabilities, requirements, scenarios, or acceptance obligations from the current plan
- **THEN** the system records the change as a PlanDecision or accepted amendment with rationale, proposer, affected nodes, and contract impact

#### Scenario: Unapproved scope shrinkage is blocked
- **WHEN** the current plan drops contract-required work without an accepted amendment
- **THEN** validation blocks progression and names the dropped obligations

#### Scenario: Scope expansion preserves do-not-touch constraints
- **WHEN** a step expands scope to address review or recovery feedback
- **THEN** the system validates that the expansion does not violate do-not-touch or forbidden-topology constraints from the root contract

### Requirement: Recovery dirties targeted dependency closure
The system SHALL dirty and reset the smallest correct requirement, Story, scenario, and execution closure for each
accepted recovery or scope-change decision.

#### Scenario: Late requirement defect does not wipe unrelated work
- **WHEN** a late Story discovers that one requirement needs an architecture or dependency contract fix
- **THEN** only affected nodes and their dependent closure are marked dirty, while unrelated completed work remains complete

#### Scenario: Whole-phase reset requires explicit justification
- **WHEN** recovery proposes wiping architecture, Stories, scenarios, or all executions
- **THEN** the system requires evidence that the entire phase is invalid rather than using whole-phase reset as the default

#### Scenario: Reset failure blocks half-applied recovery
- **WHEN** execution state cannot be reset for the targeted closure
- **THEN** the PlanDecision accept operation fails without partially mutating plan state

### Requirement: Scope completeness compares against declared obligations
The system MUST compare delivered artifacts against the current accepted scope and the root contract obligations, not
only against the latest mutable scope list.

#### Scenario: Missing deliverables are caught before final assembly
- **WHEN** a Story or requirement has approved scenario evidence but has not delivered every accepted
  `scope.create` path assigned to its `files_owned` closure
- **THEN** execution rejects or retries that Story or requirement before it can be marked complete or advance to
  final branch assembly

#### Scenario: Delivered files are fewer than accepted obligations
- **WHEN** execution converges with missing files, modules, or artifacts that remain required by the accepted contract
- **THEN** the plan fails closed before QA and records a recoverable completeness decision

#### Scenario: Current scope was wrongly narrowed before execution
- **WHEN** current scope is much smaller than the root contract and no amendment explains the reduction
- **THEN** validation treats the narrowed scope as a planning defect even if all current `scope.create` files were delivered

### Requirement: Scope-change policy supports autonomous mode
The system SHALL allow safe automatic acceptance of recovery decisions only when the proposed contract impact is
within configured autonomous policy.

#### Scenario: Autonomous run cannot wait silently on human-only decision
- **WHEN** a full-auto run reaches a recovery decision that requires human approval
- **THEN** the UI and plan state expose the wait reason, decision ID, and why auto-accept policy refused it

#### Scenario: Policy-safe recovery auto-accepts with trace
- **WHEN** recovery proposes a targeted change within autonomous policy
- **THEN** the system may auto-accept it and records the policy rule, affected closure, and contract impact

### Requirement: Recoverable PlanDecision kinds are owned end to end
The system MUST define validation, auto-accept policy, accept effects, cascade semantics, prompt propagation,
timeout behavior, and UI phase summary behavior for every PlanDecision kind that can leave a plan in a
recoverable state.

#### Scenario: Scope incomplete is not a half-wired decision
- **WHEN** the Level-0 completeness gate records a `scope_incomplete` PlanDecision
- **THEN** the decision is valid, policy-checkable, accepted or human-gated deterministically, and has explicit
  cascade behavior rather than falling through another kind's default path

#### Scenario: Scope recovery carries missing deliverables into execution
- **WHEN** a `scope_incomplete` decision is accepted
- **THEN** the retry state includes the missing declared files and the reason they are still required in the next
  developer-facing recovery guidance

#### Scenario: Scope recovery redispatches reopened requirements
- **WHEN** an accepted `scope_incomplete` decision re-queues affected requirements for execution
- **THEN** the orchestration retry identifies those affected requirements as force redispatches, and execution state
  either recreates them as fresh pending executions or fails the recovery instead of treating stale rows as benign
  idempotent dispatches

#### Scenario: Recoverable accepted decisions do not leave a rejected active plan
- **WHEN** an accepted recovery decision starts a retry or planning re-entry
- **THEN** the plan status and phase summary move to the active recovery or execution state instead of remaining
  terminal/rejected while backend work is running

#### Scenario: Human-gated recovery cannot self-timeout invisibly
- **WHEN** full-auto mode reaches a recovery decision that policy refuses to auto-accept
- **THEN** timeout handling and UI state keep the plan waiting on that explicit decision instead of silently
  terminal-failing the requirement before the operator can act
