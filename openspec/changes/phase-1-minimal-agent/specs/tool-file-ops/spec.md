## ADDED Requirements

### Requirement: File read tool
The system SHALL provide a `file_read` tool for agents to read file contents.

#### Scenario: Read existing file
- **WHEN** agent calls `file_read` with path to existing file
- **THEN** tool returns file contents as string

#### Scenario: Read non-existent file
- **WHEN** agent calls `file_read` with path to non-existent file
- **THEN** tool returns error indicating file not found

#### Scenario: Read file outside repo
- **WHEN** agent calls `file_read` with path outside configured repo
- **THEN** tool returns error indicating access denied

### Requirement: File write tool
The system SHALL provide a `file_write` tool for agents to create or modify files.

#### Scenario: Write new file
- **WHEN** agent calls `file_write` with path and content for new file
- **THEN** tool creates file with specified content
- **AND** tool creates parent directories if needed
- **AND** tool returns success confirmation

#### Scenario: Overwrite existing file
- **WHEN** agent calls `file_write` with path to existing file
- **THEN** tool overwrites file with new content
- **AND** tool returns success confirmation

#### Scenario: Write outside repo
- **WHEN** agent calls `file_write` with path outside configured repo
- **THEN** tool returns error indicating access denied

### Requirement: File list tool
The system SHALL provide a `file_list` tool for agents to list directory contents.

#### Scenario: List directory
- **WHEN** agent calls `file_list` with directory path
- **THEN** tool returns list of files and subdirectories

#### Scenario: List with pattern
- **WHEN** agent calls `file_list` with directory and glob pattern
- **THEN** tool returns only matching files

#### Scenario: List non-existent directory
- **WHEN** agent calls `file_list` with non-existent path
- **THEN** tool returns error indicating directory not found

### Requirement: Path validation
All file tools SHALL validate paths are within the configured repository root.

#### Scenario: Reject path traversal
- **WHEN** agent provides path containing `..` that escapes repo root
- **THEN** tool returns error indicating access denied
- **AND** tool does NOT perform the operation

#### Scenario: Accept absolute path within repo
- **WHEN** agent provides absolute path within repo root
- **THEN** tool normalizes path and proceeds with operation
