## ADDED Requirements

### Requirement: Project configuration file
The system SHALL load project configuration from a YAML file.

#### Scenario: Load from project root
- **WHEN** system starts in a directory with `semspec.yaml`
- **THEN** system loads configuration from that file

#### Scenario: Load from parent directories
- **WHEN** system starts in a subdirectory without `semspec.yaml`
- **THEN** system searches parent directories for `semspec.yaml`
- **AND** system uses the first one found

#### Scenario: No project config
- **WHEN** no `semspec.yaml` exists in directory tree
- **THEN** system uses default configuration
- **AND** system logs warning about missing config

### Requirement: Model configuration
The system SHALL configure Ollama model settings.

#### Scenario: Configure default model
- **WHEN** config specifies `model.default`
- **THEN** system uses that model for all agent requests

#### Scenario: Configure model endpoint
- **WHEN** config specifies `model.endpoint`
- **THEN** system connects to Ollama at that URL (default: http://localhost:11434/v1)

#### Scenario: Configure temperature
- **WHEN** config specifies `model.temperature`
- **THEN** system applies that temperature to model requests

#### Scenario: Ollama not available
- **WHEN** system cannot connect to Ollama endpoint
- **THEN** system displays clear error with setup instructions
- **AND** system exits with non-zero code

### Requirement: Repository configuration
The system SHALL configure the repository root for file operations.

#### Scenario: Explicit repo path
- **WHEN** config specifies `repo.path`
- **THEN** system uses that path as repository root

#### Scenario: Auto-detect git root
- **WHEN** config does not specify `repo.path`
- **THEN** system detects git repository root from current directory

#### Scenario: No git repository
- **WHEN** current directory is not in a git repository
- **THEN** system uses current directory as repository root
- **AND** system logs warning about git features being limited

### Requirement: User configuration
The system SHALL support user-level configuration.

#### Scenario: Load user config
- **WHEN** `~/.config/semspec/config.yaml` exists
- **THEN** system loads it as base configuration
- **AND** project config overrides user config

#### Scenario: First run without config
- **WHEN** no user or project config exists
- **THEN** system creates default user config at `~/.config/semspec/config.yaml`
