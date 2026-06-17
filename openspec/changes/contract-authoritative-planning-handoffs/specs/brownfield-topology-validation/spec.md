## ADDED Requirements

### Requirement: Baseline topology detection
The system SHALL capture a topology contract for brownfield projects before architecture, Story preparation,
developer execution, or QA can rely on repository structure.

#### Scenario: Existing build topology is recorded
- **WHEN** a project has existing build files, module includes, package manifests, or workspace configuration
- **THEN** the topology contract records the allowed roots, build-system shape, module relationships, and baseline extension points

#### Scenario: Topology detection is generic
- **WHEN** the project uses Go, Java, Node, Python, Rust, or a mixed repository layout
- **THEN** topology validation emits common topology facts without hardcoding the capability to one language or one sponsor prompt

### Requirement: Clean-room project shapes are rejected
The system MUST reject plans, Stories, or developer output that replace a brownfield baseline with a standalone
clean-room project shape unless the contract was explicitly amended to allow that replacement.

#### Scenario: Standalone Gradle root conflicts with composite QA
- **WHEN** a brownfield Java integration plan creates top-level Gradle wrapper or settings files that conflict with the baseline composite build
- **THEN** topology validation fails before QA treats the failure as a generic integration-test problem

#### Scenario: Existing baseline class inventory is ignored
- **WHEN** an architecture proposes new replacement classes while omitting required existing baseline extension classes
- **THEN** validation flags a baseline-preservation violation before developer execution

#### Scenario: Build root added outside allowed topology
- **WHEN** developer output adds a build root, package root, or module root outside the topology contract
- **THEN** structural validation rejects the work and reports the exact conflicting path and topology rule

### Requirement: QA topology failures route correctly
The system SHALL classify build-configuration and topology failures from QA using the topology contract before
recovery chooses a next action.

#### Scenario: QA failure is build configuration, not test behavior
- **WHEN** QA fails during build configuration before integration tests execute
- **THEN** the plan records a topology or build-configuration failure and recovery receives that classification

#### Scenario: Recovery receives source substitution evidence
- **WHEN** QA runs with source substitution, composite build setup, service injection, or harness profile changes
- **THEN** recovery and UI receive those QA environment facts alongside the failure text

### Requirement: Topology validation runs before expensive execution
The system SHALL run deterministic topology checks at the earliest phase where enough information exists.

#### Scenario: Architecture violates topology
- **WHEN** the architect emits component boundaries that conflict with the topology contract
- **THEN** the plan-reviewer rejects the architecture before Story preparation

#### Scenario: Story ownership violates topology
- **WHEN** Story file ownership omits required baseline files or invents standalone ownership outside the topology contract
- **THEN** validation rejects the Story plan before developer execution

#### Scenario: Developer output violates topology
- **WHEN** developer work introduces files that structural validation can compare to topology facts
- **THEN** the work is rejected before branch assembly or QA burns more time
