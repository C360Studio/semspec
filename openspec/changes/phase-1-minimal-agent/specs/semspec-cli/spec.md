## ADDED Requirements

### Requirement: CLI binary orchestration
The `semspec` binary SHALL wire together SemStreams components into a runnable agent.

#### Scenario: Start with embedded NATS
- **WHEN** user runs `semspec` without external NATS configured
- **THEN** system starts embedded NATS server
- **AND** system initializes cli-input, router, and agentic-* components
- **AND** system enters REPL mode

#### Scenario: Start with external NATS
- **WHEN** user runs `semspec --nats-url nats://host:port`
- **THEN** system connects to external NATS server
- **AND** system initializes components using that connection

#### Scenario: One-shot mode
- **WHEN** user runs `semspec "task description"`
- **THEN** system starts components
- **AND** system submits the task
- **AND** system waits for completion
- **AND** system exits with appropriate code

### Requirement: Component lifecycle management
The CLI SHALL manage component startup and shutdown.

#### Scenario: Graceful startup
- **WHEN** system starts
- **THEN** system initializes components in dependency order
- **AND** system waits for all components to be healthy before accepting input

#### Scenario: Graceful shutdown
- **WHEN** user exits (quit/exit/Ctrl+D)
- **THEN** system stops components in reverse order
- **AND** system waits for in-flight operations to complete (with timeout)

#### Scenario: Component failure during startup
- **WHEN** any component fails to initialize
- **THEN** system logs the error
- **AND** system exits with non-zero code

### Requirement: Tool executor registration
The CLI SHALL register tool executors with the agentic-tools component.

#### Scenario: Register file tools
- **WHEN** system initializes
- **THEN** system registers file_read, file_write, file_list tools

#### Scenario: Register git tools
- **WHEN** system initializes
- **THEN** system registers git_status, git_branch, git_commit tools

#### Scenario: Tool allowlist configuration
- **WHEN** config specifies tool allowlist
- **THEN** only allowed tools are available to agents
