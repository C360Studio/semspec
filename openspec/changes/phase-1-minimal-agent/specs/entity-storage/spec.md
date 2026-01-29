## ADDED Requirements

### Requirement: Proposal entity storage
The system SHALL store proposal entities in NATS KV.

#### Scenario: Create proposal
- **WHEN** system creates a new proposal entity
- **THEN** system stores it in `SEMSPEC_PROPOSALS` bucket
- **AND** system assigns a unique ID with format `proposal:{id}`

#### Scenario: Retrieve proposal
- **WHEN** system queries for a proposal by ID
- **THEN** system returns the proposal entity if it exists
- **AND** system returns not found error if it does not exist

#### Scenario: Update proposal
- **WHEN** system updates an existing proposal
- **THEN** system overwrites the entity in KV
- **AND** system preserves entity history via KV revision

### Requirement: Task entity storage
The system SHALL store task entities in NATS KV.

#### Scenario: Create task
- **WHEN** system creates a new task entity
- **THEN** system stores it in `SEMSPEC_TASKS` bucket
- **AND** system assigns a unique ID with format `task:{id}`

#### Scenario: List tasks by proposal
- **WHEN** system queries tasks for a proposal
- **THEN** system returns all tasks linked to that proposal

#### Scenario: Update task status
- **WHEN** system updates task status (pending → in_progress → complete)
- **THEN** system stores updated entity
- **AND** system records status change timestamp

### Requirement: Result entity storage
The system SHALL store result entities in NATS KV.

#### Scenario: Create result
- **WHEN** agentic loop completes a task
- **THEN** system creates result entity in `SEMSPEC_RESULTS` bucket
- **AND** system links result to originating task

#### Scenario: Store result artifacts
- **WHEN** result includes created/modified files
- **THEN** system stores artifact metadata (path, action, hash) in result entity

#### Scenario: Retrieve result
- **WHEN** system queries for a result by ID or task
- **THEN** system returns the result entity with artifact metadata

### Requirement: Entity ID format
All entities SHALL use consistent ID format.

#### Scenario: Generate entity ID
- **WHEN** system creates any entity
- **THEN** ID follows pattern `{type}:{uuid}` (e.g., `proposal:abc123`)

#### Scenario: Parse entity ID
- **WHEN** system receives an entity ID
- **THEN** system can extract type and unique identifier
