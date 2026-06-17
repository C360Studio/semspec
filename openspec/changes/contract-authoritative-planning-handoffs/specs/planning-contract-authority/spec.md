## ADDED Requirements

### Requirement: Contract packet creation
The system SHALL create an authoritative contract packet for each new plan before downstream planning,
architecture, Story, scenario, execution, recovery, or QA work begins.

#### Scenario: Contract packet is created from a new user brief
- **WHEN** a plan is created from a user prompt
- **THEN** the plan records a contract packet containing sponsor intent, non-negotiable constraints, acceptance obligations, source references, and initial scope provenance

#### Scenario: Contract packet records brownfield obligations
- **WHEN** the user prompt or project context says work must extend an existing baseline
- **THEN** the contract packet records the baseline obligation as a must-preserve constraint rather than leaving it only in prose

### Requirement: Contract packet survives BMAD handoffs
The system SHALL include a role-appropriate projection of the same contract packet in every BMAD/OpenSpec
handoff that can alter plan shape or implementation output.

#### Scenario: Architect receives preserved baseline constraints
- **WHEN** the architecture-generator dispatches the architect role
- **THEN** the prompt includes the contract packet identity, preserved baseline obligations, scope provenance, and forbidden architecture replacements

#### Scenario: Developer receives must-deliver obligations
- **WHEN** the execution-manager dispatches developer work for a Story or task
- **THEN** the prompt includes allowed files, required deliverables, forbidden moves, accepted amendments, and the current contract packet identity

#### Scenario: Recovery receives original and current contract
- **WHEN** recovery-agent is asked to diagnose a wedge
- **THEN** the prompt includes the root contract, accepted amendments, current artifact shape, and concrete failure evidence

### Requirement: Downstream artifacts declare contract impact
The system MUST require artifacts that materially change architecture, Story coverage, scenario coverage, scope,
or recovery state to declare whether they preserve, refine, or change the authoritative contract.

#### Scenario: Silent baseline replacement is rejected
- **WHEN** an architecture or Story output replaces an existing integration baseline with a clean-room design
- **THEN** validation rejects the artifact unless an accepted PlanDecision explicitly amended the baseline obligation

#### Scenario: Prose-only preservation is insufficient
- **WHEN** a downstream artifact mentions the original goal but omits enforceable constraints or acceptance obligations from its machine-readable fields
- **THEN** validation reports a contract-fidelity error before execution proceeds

### Requirement: Contract evidence is traceable
The system SHALL expose contract packet identity, amendments, and validation findings through plan APIs and graph
facts so UI, QA, diagnostics, and tests can trace why the current plan shape is allowed.

#### Scenario: UI can link a phase decision to the contract
- **WHEN** the UI displays a plan phase, recovery decision, or QA verdict
- **THEN** it can show the contract packet version and accepted amendments that governed that state

#### Scenario: Diagnostics compare current scope to root contract
- **WHEN** a diagnostic bundle is generated for a rejected or wedged plan
- **THEN** it includes enough contract evidence to compare current scope, topology, and acceptance obligations to the original brief
